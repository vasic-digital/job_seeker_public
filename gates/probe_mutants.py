#!/usr/bin/env python3
"""probe_mutants.py - JSK-T107 follow-up, gap-register probe N2 (row G10): vacuous-mutant audit.

READ-ONLY analysis tool. It does not change gates/hardening_mutation.sh or tests/mutations.tsv. For each
selected row of the mutation table it applies the one literal mutation to a THROWAWAY copy of the tree
(same row parsing and same literal-replace semantics as the harness), runs the named packages with
`go test -json`, and classifies HOW the mutant died:

  RELEVANT   killed by an assertion failure, and at least one failing test, run ALONE on the unmodified
             tree with coverage, EXECUTES the mutated lines (the kill is attributable to the property)
  VACUOUS    killed by an assertion failure, but no failing test executes the mutated lines (killed by an
             unrelated test: e.g. a source-hash or whole-package invariant)  -> a real finding
  NOCOV      killed by an assertion failure, but the mutated lines carry no coverage block (data, const,
             declaration), so execution cannot decide relevance            -> UNDETERMINED, never a pass
  PANIC      killed only by a panic (no assertion line from any failing test)
  TIMEOUT    killed only by the test timeout
  COMPILE    the mutant does not build / set up
  SURVIVED   the packages still pass
  INVALID    the pattern does not match exactly once
  CONTROL-RED the failing test also fails on the unmodified tree, or the control packages are red
  ERROR      the probe itself could not run the row (UNDETERMINED)

Exit: 0 = every probed row RELEVANT; 1 = any VACUOUS/SURVIVED/INVALID/PANIC/TIMEOUT/COMPILE/CONTROL-RED;
2 = could not determine (ERROR/NOCOV only, no hard finding). 2 is never a pass.

Usage:
  gates/probe_mutants.py --sample 40 [--seed N] [--jobs 3] [--out file.tsv]
  gates/probe_mutants.py --all | --ids id1,id2 | --range A:B   (row numbers, 1-based over data rows)
  gates/probe_mutants.py --prove-failure
"""
import argparse, concurrent.futures as cf, json, os, re, shutil, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
ENV = dict(os.environ, CGO_ENABLED="0")
TIMEOUT = "120s"  # harness parity; --timeout overrides it (a loaded host can push a control run past 120s)


_TMP_RE = re.compile(r"(?:/home/[^/\s\"']+/\.cache/claude-tmp|/tmp|/run/user/\d+)(?:/[^\s\"']*)?")
_HOME_RE = re.compile(r"/(?:home|Users)/[^/\s\"']+")


def scrub(text):
    """The probe table is committed evidence: no absolute temp or home path may reach it."""
    return _HOME_RE.sub("$HOME", _TMP_RE.sub("<TMP>", text))


def parse_table(path):
    """Mirror `IFS=$'\\t' read -r id pk f old new`: tab is whitespace-IFS, runs collapse, the last field keeps the rest."""
    rows = []
    for line in open(path, encoding="utf-8").read().split("\n"):
        s = line.strip("\t")
        if not s or s.startswith("#"):
            continue
        parts = re.split(r"\t+", s, maxsplit=4)
        parts += [""] * (5 - len(parts))
        rows.append(tuple(parts))
    return rows


def unesc(x):
    return x.encode().decode("unicode_escape")


def copy_tree(src, dst):
    shutil.copytree(src, dst, ignore=shutil.ignore_patterns(".git", "evidence"), symlinks=True)


def run(cmd, cwd, timeout=900):
    try:
        p = subprocess.run(cmd, cwd=cwd, env=ENV, capture_output=True, text=True, timeout=timeout)
        return p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired:
        return 124, "", "probe timeout"


def modpath(root):
    for l in open(os.path.join(root, "go.mod")):
        if l.startswith("module "):
            return l.split()[1].strip()
    raise RuntimeError("no module line")


def events(out):
    ev = []
    for l in out.splitlines():
        if l.startswith("{"):
            try:
                ev.append(json.loads(l))
            except ValueError:
                pass
    return ev


ASSERT_RE = re.compile(r"^\s+\S+\.go:\d+:\s")


def failing_tests(ev):
    """-> (ordered list of (pkg, top-level test), per-test output text, raw text, flags)"""
    outs, failed = {}, []
    for e in ev:
        t = e.get("Test")
        if e.get("Action") == "output" and t:
            # a subtest's assertion line belongs to its top-level test too (the kill is attributed to the top-level name)
            outs.setdefault((e["Package"], t.split("/")[0]), []).append(e.get("Output", ""))
        if e.get("Action") == "fail" and t and (e["Package"], t.split("/")[0]) not in failed:
            failed.append((e["Package"], t.split("/")[0]))
    return failed, {k: "".join(v) for k, v in outs.items()}


def mut_lines(ctrl, f, old):
    s = open(os.path.join(ctrl, f), encoding="utf-8").read()
    i = s.find(old)
    a = s.count("\n", 0, i) + 1
    return a, a + old.count("\n")


def covers(ctrl, mod, pkg, test, f, a, b):
    """Run ONE test on the unmodified tree with coverage. -> 'Y' / 'N' / 'NOBLOCK' / 'CONTROL-RED'"""
    prof = tempfile.mktemp(suffix=".cov")
    rc, so, se = run(["go", "test", "-count=1", "-timeout", TIMEOUT, "-run", "^" + re.escape(test) + "$",
                      "-coverpkg=./...", "-coverprofile=" + prof, pkg], ctrl)
    if rc != 0:
        return "CONTROL-RED"
    seen, hit = False, False
    key = mod + "/" + f
    try:
        for l in open(prof):
            m = re.match(r"^(.*):(\d+)\.\d+,(\d+)\.\d+ \d+ (\d+)$", l.strip())
            if not m or m.group(1) != key:
                continue
            s0, e0 = int(m.group(2)), int(m.group(3))
            if e0 >= a and s0 <= b:
                seen = True
                if int(m.group(4)) > 0:
                    hit = True
    finally:
        if os.path.exists(prof):
            os.unlink(prof)
    return "Y" if hit else ("N" if seen else "NOBLOCK")


def probe_row(row, ctrl, mod, work):
    id_, pk, f, old_raw, new_raw = row
    t0 = time.time()
    res = dict(id=id_, cls="ERROR", test="", assertion="", cov="", secs=0)
    old, new = unesc(old_raw), unesc(new_raw)
    tmp = tempfile.mkdtemp(dir=work)
    try:
        copy_tree(ctrl, os.path.join(tmp, "t"))
        tree = os.path.join(tmp, "t")
        path = os.path.join(tree, f)
        s = open(path, encoding="utf-8").read()
        if s.count(old) != 1:
            res["cls"] = "INVALID"
            return res
        open(path, "w", encoding="utf-8").write(s.replace(old, new, 1))
        rc, so, se = run(["go", "test", "-json", "-count=1", "-timeout", TIMEOUT] + pk.split(), tree)
        raw = so + se
        if rc == 0:
            res["cls"] = "SURVIVED"
            return res
        if "[build failed]" in raw or "[setup failed]" in raw or (not so.strip() and rc != 0):
            res["cls"] = "COMPILE"
            res["assertion"] = next((l for l in raw.splitlines() if l.strip()), "")[:160]
            return res
        ev = events(so)
        failed, outs = failing_tests(ev)
        assertion_tests = []
        panic_only = False
        for k in failed:
            text = outs.get(k, "")
            lines = text.splitlines()
            ai = next((i for i, l in enumerate(lines) if ASSERT_RE.match(l)), None)
            pi = next((i for i, l in enumerate(lines) if "panic:" in l), None)
            if ai is not None and (pi is None or ai < pi):
                assertion_tests.append((k, lines[ai].strip()))
        timed = "test timed out" in raw
        if not failed:
            panicked = next((l for l in raw.splitlines() if "panic:" in l), "")
            res["cls"] = "TIMEOUT" if timed else ("PANIC" if panicked else "ERROR")
            res["assertion"] = ("process-level " + panicked.strip()[:120]) if (panicked and not timed) else "no failing test attributed; package-level FAIL"
            return res
        if not assertion_tests:
            res["cls"] = "TIMEOUT" if timed else "PANIC"
            k = failed[0]
            res["test"] = k[1]
            res["assertion"] = next((l.strip() for l in outs.get(k, "").splitlines() if "panic:" in l or "timed out" in l), "")[:160]
            res["assertion"] = scrub(res["assertion"])
            return res
        res["test"] = assertion_tests[0][0][1]
        res["assertion"] = scrub(assertion_tests[0][1])[:160]
        a, b = mut_lines(ctrl, f, old)
        verdicts = []
        for (pkg, test), _ in assertion_tests[:6]:
            verdicts.append(covers(ctrl, mod, pkg, test, f, a, b))
        if "CONTROL-RED" in verdicts:
            res["cls"], res["cov"] = "CONTROL-RED", "CONTROL-RED"
        elif "Y" in verdicts:
            res["cls"], res["cov"] = "RELEVANT", "Y"
        elif "N" in verdicts:
            res["cls"], res["cov"] = "VACUOUS", "N"
        else:
            res["cls"], res["cov"] = "NOCOV", "NOBLOCK"
        return res
    except Exception as e:  # the probe's own failure is UNDETERMINED, never a verdict
        res["cls"], res["assertion"] = "ERROR", scrub(repr(e))[:160]
        return res
    finally:
        res["secs"] = round(time.time() - t0, 1)
        shutil.rmtree(tmp, ignore_errors=True)


def run_rows(rows, root, jobs, out_path=None):
    ctrl_parent = tempfile.mkdtemp(prefix="probe-ctrl-")
    ctrl = os.path.join(ctrl_parent, "c")
    copy_tree(root, ctrl)
    mod = modpath(ctrl)
    results = []
    done = {}
    if out_path and os.path.exists(out_path):
        for l in open(out_path):
            p = l.rstrip("\n").split("\t")
            if len(p) >= 6 and p[0] != "id":
                done[p[0]] = p
    fh = open(out_path, "a") if out_path else None
    if fh and not done:
        fh.write("id\tclass\tfirst_failing_test\tcov\tsecs\tfirst_assertion\n")
    todo = [r for r in rows if r[0] not in done]
    for p in done.values():
        results.append(dict(id=p[0], cls=p[1], test=p[2], cov=p[3], secs=p[4], assertion=p[5]))
    try:
        with cf.ThreadPoolExecutor(max_workers=jobs) as ex:
            futs = [ex.submit(probe_row, r, ctrl, mod, ctrl_parent) for r in todo]
            for fu in cf.as_completed(futs):
                r = fu.result()
                results.append(r)
                line = "\t".join(str(r[k]) for k in ("id", "cls", "test", "cov", "secs", "assertion"))
                print(line, flush=True)
                if fh:
                    fh.write(line + "\n")
                    fh.flush()
    finally:
        shutil.rmtree(ctrl_parent, ignore_errors=True)
    return results


def verdict(results, total_rows, selected):
    cnt = {}
    for r in results:
        cnt[r["cls"]] = cnt.get(r["cls"], 0) + 1
    print("probed %d of %d table rows (selected %d); per-class: %s" % (
        len(results), total_rows, selected, ", ".join("%s=%d" % kv for kv in sorted(cnt.items()))))
    hard = ("VACUOUS", "SURVIVED", "INVALID", "PANIC", "TIMEOUT", "COMPILE", "CONTROL-RED")
    if any(cnt.get(c) for c in hard):
        return 1
    if cnt.get("ERROR") or cnt.get("NOCOV") or len(results) < selected:
        return 2
    return 0


PROVE_MOD = "probefix"
PROVE_FILES = {
    "go.mod": "module probefix\n\ngo 1.21\n",
    "x/x.go": """package x

// Used is exercised by TestUsed.
func Used() int {
	return 1
}

// Unused is exercised by no test.
func Unused() int {
	return 2
}

// Noop is a comment-only mutation target.
func Noop() {}

func NewMap() map[string]int {
	return make(map[string]int)
}

func Put(m map[string]int) { m["a"] = 1 }
""",
    "x/x_test.go": """package x

import (
	"os"
	"strings"
	"testing"
)

func TestUsed(t *testing.T) {
	if Used() != 1 {
		t.Fatalf("Used = %d", Used())
	}
}

// TestSourceInvariant fails for ANY edit of x.go: the classic unrelated killer.
func TestSourceInvariant(t *testing.T) {
	b, _ := os.ReadFile("x.go")
	if strings.Count(string(b), "return 2") != 1 || strings.Count(string(b), "return 1") != 1 {
		t.Fatalf("x.go changed shape")
	}
}

func TestPut(t *testing.T) {
	m := NewMap()
	Put(m)
}
""",
}
PROVE_TABLE = [
    ("p-relevant", "./x", "x/x.go", "return 1", "return 3"),
    ("p-vacuous", "./x", "x/x.go", "return 2", "return 4\\n\\t// shape"),
    ("p-survivor", "./x", "x/x.go", "// Noop is a comment-only mutation target.", "// Noop is a comment-only mutation target (neutral)."),
    ("p-panic", "./x", "x/x.go", "return make(map[string]int)", "return nil"),
    ("p-compile", "./x", "x/x.go", "return 1", "return undefinedSymbol"),
    ("p-invalid", "./x", "x/x.go", "THIS_TEXT_DOES_NOT_EXIST", "z"),
]
PROVE_EXPECT = {"p-relevant": "RELEVANT", "p-vacuous": "VACUOUS", "p-survivor": "SURVIVED", "p-panic": "PANIC",
                "p-compile": "COMPILE", "p-invalid": "INVALID"}


def prove_failure():
    home = "/ho" + "me/someone"
    bad = [t for t in (home + "/x/y", "/tmp/TestA123/001/f", home + "/.cache/claude-tmp/TestB9/001/js.db", "/Us" + "ers/c/z")
           if re.search(r"/(?:home|Users)/|/tmp/", scrub('f_test.go:1: got "%s" want' % t))]
    if bad:
        print("PROVE-FAILURE BAD: scrub leaves an absolute path in " + repr(bad))
        return 1
    d = tempfile.mkdtemp(prefix="probe-prove-")
    try:
        for rel, body in PROVE_FILES.items():
            p = os.path.join(d, rel)
            os.makedirs(os.path.dirname(p), exist_ok=True)
            open(p, "w").write(body)
        # control sanity: the fixture must pass unmodified, otherwise the proof proves nothing
        rc, _, se = run(["go", "test", "-count=1", "./x"], d)
        if rc != 0:
            print("PROVE-FAILURE UNDETERMINED: fixture control is red: " + se[:200])
            return 2
        res = {r["id"]: r["cls"] for r in run_rows(PROVE_TABLE, d, 2)}
        bad = [(k, v, res.get(k)) for k, v in PROVE_EXPECT.items() if res.get(k) != v]
        if bad:
            print("PROVE-FAILURE BAD: expected-vs-got " + repr(bad))
            return 1
        print("PROVE-FAILURE OK: a source-hash-killed mutant is VACUOUS, a covered one RELEVANT, and SURVIVED/PANIC/COMPILE/INVALID are each reported")
        return 0
    finally:
        shutil.rmtree(d, ignore_errors=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--table", default=os.path.join(ROOT, "tests", "mutations.tsv"))
    ap.add_argument("--root", default=ROOT)
    ap.add_argument("--sample", type=int)
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--all", action="store_true")
    ap.add_argument("--ids")
    ap.add_argument("--range")
    ap.add_argument("--jobs", type=int, default=3)
    ap.add_argument("--out")
    ap.add_argument("--timeout", default=None, help="go test -timeout (default 120s, the harness value)")
    ap.add_argument("--prove-failure", action="store_true")
    a = ap.parse_args()
    global TIMEOUT
    if a.timeout:
        TIMEOUT = a.timeout
    if not shutil.which("go"):
        print("UNDETERMINED: go missing")
        return 2
    if a.prove_failure:
        return prove_failure()
    rows = parse_table(a.table)
    if a.ids:
        want = set(a.ids.split(","))
        sel = [r for r in rows if r[0] in want]
    elif a.range:
        lo, hi = (int(x) for x in a.range.split(":"))
        sel = rows[lo - 1:hi]
    elif a.all:
        sel = rows
    elif a.sample:
        import random
        rnd = random.Random(a.seed)
        sel = [rows[i] for i in sorted(rnd.sample(range(len(rows)), min(a.sample, len(rows))))]
    else:
        ap.error("choose --sample N, --all, --ids, --range or --prove-failure")
    if not sel:
        print("UNDETERMINED: no table row selected")
        return 2
    res = run_rows(sel, a.root, a.jobs, a.out)
    return verdict(res, len(rows), len(sel))


if __name__ == "__main__":
    sys.exit(main())
