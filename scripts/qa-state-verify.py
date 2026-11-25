#!/usr/bin/env python3
"""qa-state-verify.py - machine verdict over the STATE-CHANGING helixqa bank run (JSK-T084/T111, umbrella 11.4.68 sink-side evidence).

usage: qa-state-verify.py OUT_DIR [--table FILE.tsv]
Two independent evidence sources, BOTH required:
  1. the helixqa report: exit code 0, status complete, every case PASSED, every vision-gated step verified by a named vision provider on a
     frame whose sha256 is a saved file, the FINAL step of every case vision-gated, at least 8 distinct frames;
  2. OUT_DIR/readback.json, written by the test harness from the daemon's own encrypted database after the daemon stopped: the rows the
     console actions must have produced (approve -> dry_run_recorded with one approval and one would_send submission; reject ->
     rejected_by_human with one approval row; prepare -> awaiting_human_approval; map -> mapped; dismiss -> discarded), no handoff row
     (nothing was sent), live mode off, audit chain walk OK, and the audit transitions naming each action.
A model verdict alone never passes, and neither does a database row alone.
Exit 0 all satisfied, 1 a requirement failed (named), 2 cannot determine. 2 is never a pass. It never reads or prints a cookie."""
import glob, hashlib, json, os, sys

def main(argv):
    out = argv[1] if len(argv) > 1 else ""
    table = argv[argv.index("--table") + 1] if "--table" in argv else ""
    reps = sorted(glob.glob(os.path.join(out, "session-*", "pipeline-report.json")))
    rbp = os.path.join(out, "readback.json")
    if not reps or not os.path.exists(rbp):
        print("cannot determine: need session-*/pipeline-report.json and readback.json under %s" % out, file=sys.stderr); return 2
    try:
        rc = int(open(os.path.join(out, "helixqa.rc")).read().strip())
    except Exception:
        print("cannot determine: no helixqa.rc", file=sys.stderr); return 2
    rep = json.load(open(reps[-1])); sess = os.path.dirname(reps[-1]); rb = json.load(open(rbp))
    bad = []
    if rc != 0: bad.append("helixqa exit code %d" % rc)
    if rep.get("status") != "complete": bad.append("report status %r" % rep.get("status"))
    cases = (rep.get("structured") or {}).get("cases") or []
    if len(cases) < 5: bad.append("only %d cases (< 5)" % len(cases))
    files = {hashlib.sha256(open(f, "rb").read()).hexdigest(): os.path.basename(f) for f in glob.glob(os.path.join(sess, "screenshots", "*.png"))}
    rows, shas = [], set()
    for c in cases:
        if c.get("verdict") != "PASSED": bad.append("%s verdict %s" % (c.get("id"), c.get("verdict")))
        st = c.get("steps") or []
        if not st or not st[-1].get("vision_gated"): bad.append("%s final step is not vision-gated" % c.get("id"))
        for s in st:
            if not s.get("passed"): bad.append("%s step %r did not pass" % (c.get("id"), s.get("step_name")))
            if not s.get("vision_gated"): continue
            sha = s.get("frame_sha256") or ""; ok = True
            if s.get("vision_status") != "verified": bad.append("%s vision_status %s" % (c["id"], s.get("vision_status"))); ok = False
            if not s.get("vision_provider"): bad.append("%s no vision provider" % c["id"]); ok = False
            if not sha or sha not in files: bad.append("%s frame %s not a saved file" % (c["id"], sha[:12])); ok = False
            if sha: shas.add(sha)
            rows.append((c["id"], s.get("step_name", ""), sha, files.get(sha, "MISSING"), s.get("vision_provider", ""), s.get("vision_status", ""), "PASS" if ok else "FAIL"))
    if len(shas) < 8: bad.append("only %d distinct frames (< 8)" % len(shas))
    app = {a["id"]: a for a in rb.get("applications", [])}
    def want(cond, msg):
        if not cond: bad.append("readback: " + msg)
    a1, a2, a3 = app.get(1, {}), app.get(2, {}), app.get(3, {})
    want(a1.get("state") == "dry_run_recorded", "application 1 state %r, want dry_run_recorded (approve)" % a1.get("state"))
    want(a1.get("approval_rows") == 1 and a1.get("submission_rows") == 1 and a1.get("submission_kinds") == "would_send", "application 1 approval/submission rows %r/%r/%r" % (a1.get("approval_rows"), a1.get("submission_rows"), a1.get("submission_kinds")))
    want(a2.get("state") == "rejected_by_human" and a2.get("approval_rows") == 1 and a2.get("submission_rows") == 0, "application 2 %r (reject)" % a2)
    want(a3.get("state") == "awaiting_human_approval" and a3.get("submission_rows") == 0, "application 3 %r (prepare)" % a3)
    ps = rb.get("posting_states", {})
    want(ps.get("5") in ("mapped", "scored") or rb.get("posting5_mapping_status") == "mapped", "posting 5 not mapped: %r / %r" % (ps.get("5"), rb.get("posting5_mapping_status")))
    want(rb.get("posting5_mapping_status") == "mapped", "posting 5 mapping status %r" % rb.get("posting5_mapping_status"))
    want(ps.get("7") == "discarded", "posting 7 state %r, want discarded (dismiss)" % ps.get("7"))
    want(rb.get("handoff_rows") == 0, "a handoff row exists: something was sent")
    want(rb.get("live_mode_setting") in ("", "0", None), "live mode %r" % rb.get("live_mode_setting"))
    want(rb.get("audit_chain_ok") is True, "audit chain walk failed: %s" % rb.get("audit_chain_error"))
    tr = " | ".join(rb.get("audit_transitions", []))
    for needle in ("app1 awaiting_human_approval->", "app2 awaiting_human_approval->rejected_by_human", "app3 pending_draft->", "posting7 scored->discarded"):
        want(needle in tr, "audit lacks %r" % needle)
    if table:
        with open(table, "w") as t:
            t.write("case\tstep\tframe_sha256\tframe_file\tvision_provider\tvision_status\tresult\n")
            for r in rows: t.write("\t".join(r) + "\n")
    print("cases=%d vision_steps=%d distinct_frames=%d readback_apps=%d" % (len(cases), len(rows), len(shas), len(app)))
    for b in bad: print("FAIL: " + b)
    if bad: return 1
    print("PASS: every requirement met"); return 0

if __name__ == "__main__": sys.exit(main(sys.argv))
