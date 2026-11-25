#!/usr/bin/env python3
"""i18n_review_chunked.py - run the canonical umbrella reviewer (_tools/review_translation.py, section 11.4.256) over SLICES of the
catalogue and merge the verdicts. Why: free-tier reviewer providers cap a single request (groq: 7000-8000 tokens/min input) far below
the whole catalogue (en + translation is ~9000+ tokens before the prompt), so a one-shot review of 310 keys returns HTTP 413/429 and
no verdict. Each slice is an independent reviewer call on a sub-catalogue (same prompt, same glossary gate); the merged verdict is
PASS only when EVERY slice is PASS, scores are the per-dimension MINIMUM, issues are prefixed with their key range.
Usage: i18n_review_chunked.py --reviewer <path/to/review_translation.py> --src en.json --translated xx.json --lang xx
         --provider P [--model M] --out review.json [--slice 25] [--pause 30] [--retries 2]
Exit 0 PASS, 1 FAIL, 2 ERROR (any slice without a verdict). Keys come from the environment by NAME only; nothing secret is printed."""
import argparse, json, os, re, subprocess, sys, tempfile, time
KEEP = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "i18n_keep_english.json")))["terms"]
CUE = re.compile(r"untranslated|not translated|left in english|remains? in english|no traducid\w*|sin traducir|non traduit\w*|pas traduit\w*|nicht [uü]bersetzt|не переведен\w*|未翻译|翻訳されていない", re.I)
FILL = {"word", "words", "term", "terms", "english", "in", "the", "is", "are", "not", "a", "an", "en", "el", "la", "y", "no", "se", "ha", "left", "as", "key", "label", "text", "string", "message", "button", "still", "remains", "remain"}
def waived(issue):
    """waive an issue ONLY when it is a single complaint that a deliberately-kept English term (scripts/i18n_keep_english.json) is untranslated:
    an 'untranslated' cue, at least one quoted string, EVERY quoted string a keep term, and NOTHING else of substance (after removing the
    quoted keep terms, key names, the cue phrase and filler words no letters may remain). Combined or any other complaints are never waived."""
    q = [next(g for g in m.groups() if g) for m in re.finditer(r"'([^']+)'|\"([^\"]+)\"|«([^»]+)»", issue)]
    if not (CUE.search(issue) and q and all(x in KEEP for x in q)): return False
    rest = re.sub(r"'[^']+'|\"[^\"]+\"|«[^»]+»", " ", issue); rest = CUE.sub(" ", rest); rest = re.sub(r"[\w:-]+(?:\.[\w:-]+)+", " ", rest)
    return not [w for w in re.findall(r"[^\W\d_]+", rest.lower()) if w not in FILL]
ap = argparse.ArgumentParser()
for n in ("reviewer", "src", "translated", "lang", "provider", "out"): ap.add_argument("--" + n, required=True)
ap.add_argument("--model", default=""); ap.add_argument("--keys", default=os.environ.get("REVIEW_KEYS", ""), help="comma list: DELTA review of only these keys (the verdict records scope=keys; use only for keys added/changed on top of an earlier full PASS)"); ap.add_argument("--slice", type=int, default=int(os.environ.get("REVIEW_SLICE", "25")))
ap.add_argument("--pause", type=int, default=int(os.environ.get("REVIEW_SLICE_PAUSE", "30"))); ap.add_argument("--retries", type=int, default=2)
a = ap.parse_args()
en = json.load(open(a.src, encoding="utf-8")); tr = json.load(open(a.translated, encoding="utf-8"))
inv_p = os.path.join(os.path.dirname(a.src), "_invariant.json")
inv = set(json.load(open(inv_p))) if os.path.exists(inv_p) else set()
keys = [k for k in sorted(en) if k not in inv]
missing = [k for k in keys if k not in tr]
if missing:
    print(json.dumps({"verdict": "ERROR", "error": f"{len(missing)} key(s) absent from the translation, first {missing[:3]}"})); sys.exit(2)
if a.keys:
    want = [k for k in a.keys.split(",") if k]; bad = [k for k in want if k not in keys]
    if bad or not want: print(json.dumps({"verdict": "ERROR", "error": f"--keys names key(s) not in the catalogue: {bad[:3]}"})); sys.exit(2)
    keys = sorted(want)
slices = [keys[i:i + a.slice] for i in range(0, len(keys), a.slice)]
DIMS = ("accuracy", "fluency", "completeness", "naturalness", "register_appropriate")
per, issues = [], []
for si, ks in enumerate(slices):
    with tempfile.TemporaryDirectory() as td:
        json.dump({k: en[k] for k in ks}, open(td + "/en.json", "w", encoding="utf-8"), ensure_ascii=False, indent=1)
        json.dump({k: tr[k] for k in ks}, open(td + "/tr.json", "w", encoding="utf-8"), ensure_ascii=False, indent=1)
        cmd = [sys.executable, a.reviewer, "--src", td + "/en.json", "--translated", td + "/tr.json", "--lang", a.lang, "--provider", a.provider, "--out", td + "/out.json"]
        if a.model: cmd += ["--model", a.model]
        r = None
        for t in range(a.retries + 1):
            p = subprocess.run(cmd, capture_output=True, text=True)
            r = json.load(open(td + "/out.json")) if os.path.exists(td + "/out.json") else {"verdict": "ERROR", "error": "no output"}
            if r.get("verdict") in ("PASS", "FAIL"): break
            time.sleep(65)   # per-minute token windows are 60 s
    iss = [i for i in (r.get("issues") or []) if isinstance(i, str)]; wv = [i for i in iss if waived(i)]; iss = [i for i in iss if i not in wv]
    rv = r.get("verdict")
    ok_scores = all(isinstance(r.get(d), (int, float)) and r.get(d) >= 4 for d in DIMS) and r.get("script_ok") is not False and r.get("target_script_ok") is not False
    nonstr = [i for i in (r.get("issues") or []) if not isinstance(i, str)]
    if rv == "FAIL" and wv and not iss and not nonstr and ok_scores: rv = "PASS"   # every complaint of this slice was a documented keep-English term; the raw verdict stays recorded below
    per.append({"slice": si, "first": ks[0], "last": ks[-1], "n": len(ks), "verdict": rv, "reviewer_verdict": r.get("verdict"), "issues": iss, "waived": wv, "error": r.get("error") if r.get("verdict") == "ERROR" else None,
                **{d: r.get(d) for d in DIMS}, "script_ok": r.get("script_ok"), "target_script_ok": r.get("target_script_ok")})
    for i in iss: issues.append(f"[{ks[0]}..{ks[-1]}] {i}")
    print(f"slice {si + 1}/{len(slices)} {r.get('verdict')}", file=sys.stderr, flush=True)
    _model = r.get("_model") or globals().get("_model", "?")
    if si + 1 < len(slices): time.sleep(a.pause)
errs = [s for s in per if s["verdict"] not in ("PASS", "FAIL")]
vals = lambda d: [s[d] for s in per if isinstance(s.get(d), (int, float))]
out = {"verdict": "ERROR" if errs else "PASS" if all(s["verdict"] == "PASS" for s in per) else "FAIL",
       **{d: (min(vals(d)) if vals(d) else None) for d in DIMS},
       "script_ok": all(s["script_ok"] is not False for s in per), "target_script_ok": all(s["target_script_ok"] is not False for s in per),
       "issues": issues, "waived_issues": sum(len(x["waived"]) for x in per), "slices": per, "slice_size": a.slice, "_lang": a.lang, "_provider": a.provider, "_model": a.model or _model, "_chunked": True, "scope": ("keys:" + ",".join(keys)) if a.keys else "full"}
os.makedirs(os.path.dirname(os.path.abspath(a.out)), exist_ok=True)
json.dump(out, open(a.out, "w", encoding="utf-8"), ensure_ascii=False)
print(json.dumps({k: out[k] for k in ("verdict",) + DIMS}, ensure_ascii=False))
sys.exit({"PASS": 0, "FAIL": 1}.get(out["verdict"], 2))
