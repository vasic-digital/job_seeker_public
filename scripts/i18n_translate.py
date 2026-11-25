#!/usr/bin/env python3
"""i18n_translate.py - JSK-T129..T142: translate internal/i18n/messages/en.json into one language ONLY through the
canonical HelixTranslate pipeline (umbrella _tools/translate-pipeline.sh, §11.4.255). Nothing is translated here.
The provider TPM limit (groq 8000/min) is respected by chunking and sleeping; a chunk that fails or returns a changed key
set is retried with backoff. Invariant keys (_invariant.json) are copied, not translated. Writes the catalogue and the
per-chunk provider evidence evidence/i18n/<lang>/pipeline.json. Exit 0 ok, 2 could not translate.
Usage: UMBRELLA=<umbrella checkout> i18n_translate.py <lang> [--chunk 10] [--pause 45]"""
import hashlib, json, os, subprocess, sys, tempfile, time, re, argparse
ap = argparse.ArgumentParser(); ap.add_argument("lang"); ap.add_argument("--chunk", type=int, default=10)
ap.add_argument("--pause", type=int, default=45); ap.add_argument("--tries", type=int, default=6); a = ap.parse_args()
root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
U = os.environ.get("UMBRELLA") or os.path.join(root, "..")
P = os.path.join(U, "_tools", "translate-pipeline.sh")
if not os.access(P, os.X_OK): print("UNDETERMINED: pipeline missing at", P); sys.exit(2)
md = os.path.join(root, "internal/i18n/messages")
en = json.load(open(md + "/en.json")); inv = set(json.load(open(md + "/_invariant.json")))
verb = re.compile(r"%[-+# 0-9.]*[a-zA-Z%]")
hints = json.load(open(os.path.join(root, "scripts/i18n_hints.json")))  # disambiguating pipeline INPUT for terse labels; reviewed against the ORIGINAL English
cache = os.path.join(os.environ.get("I18N_WORK", tempfile.gettempdir()), f"jsk-i18n-{a.lang}.partial.json")
done = json.load(open(cache)) if os.path.exists(cache) else {}
out, ev = {}, []
keys = [k for k in en if k not in inv]
for k in inv: out[k] = en[k]
chunks = [keys[i:i + a.chunk] for i in range(0, len(keys), a.chunk)]
for ci, ks in enumerate(chunks):
    sub = {k: en[k] for k in ks}
    if str(ci) in done and set(done[str(ci)]["t"]) == set(ks) and done[str(ci)].get("h") == hashlib.sha256(json.dumps([[hints.get(k, en[k]), en[k]] for k in ks]).encode()).hexdigest():
        out.update(done[str(ci)]["t"]); ev.append(done[str(ci)]["ev"]); print(f"{a.lang} chunk {ci+1}/{len(chunks)} cached", flush=True); continue
    for t in range(a.tries):
        with tempfile.TemporaryDirectory() as td:
            fi, fo = td + "/in.txt", td + "/out.txt"
            # The engine is line-based and translates EVERYTHING on a line (it mangles JSON keys) and may add stray lines
            # ("Russian translation:"): only VALUES go in as numbered lines; output is re-keyed by the tag, untagged lines are
            # ignored, and a missing/duplicated tag or a changed printf verb set is a retry.
            open(fi, "w", encoding="utf-8").write("\n".join("%03d. %s" % (i + 1, hints.get(k, en[k])) for i, k in enumerate(ks)) + "\n")
            env = dict(os.environ, TRANSLATE_EVIDENCE_DIR=td + "/log")
            r = subprocess.run(["bash", P, "--in", fi, "--out", fo, "--lang", a.lang], env=env, capture_output=True, text=True)
            prov = re.search(r"PROVIDER_USED=(\S+)", r.stdout)
            ok = False
            if r.returncode == 0 and os.path.exists(fo):
                got = {}
                for l in open(fo, encoding="utf-8").read().split("\n"):
                    m = re.match(r"^\s*(\d+)\s*[.)]\s*(.+?)\s*$", l)
                    if m:
                        n = int(m.group(1))
                        if n in got: got = {}; break
                        got[n] = m.group(2)
                if sorted(got) == list(range(1, len(ks) + 1)):
                    o = {k: got[i + 1] for i, k in enumerate(ks)}
                    ok = all(sorted(verb.findall(o[k])) == sorted(verb.findall(sub[k])) for k in sub)
            if ok:
                out.update(o); ev.append({"chunk": ci, "keys": len(ks), "provider": prov.group(1) if prov else "?", "tries": t + 1})
                done[str(ci)] = {"t": o, "ev": ev[-1], "h": hashlib.sha256(json.dumps([[hints.get(k, en[k]), en[k]] for k in ks]).encode()).hexdigest()}; json.dump(done, open(cache, "w"), ensure_ascii=False); break
        time.sleep(a.pause * (t + 1))
    else:
        print(f"UNDETERMINED: chunk {ci} failed after {a.tries} tries (chunks 0..{ci-1} cached, rerun resumes)"); sys.exit(2)
    print(f"{a.lang} chunk {ci+1}/{len(chunks)} via {ev[-1]['provider']} (try {ev[-1]['tries']})", flush=True)
    time.sleep(a.pause)
json.dump(dict(sorted(out.items())), open(f"{md}/{a.lang}.json", "w"), ensure_ascii=False, indent=1); open(f"{md}/{a.lang}.json", "a").write("\n")
d = os.path.join(root, "evidence/i18n", a.lang); os.makedirs(d, exist_ok=True)
json.dump({"lang": a.lang, "source": "internal/i18n/messages/en.json", "pipeline": "umbrella _tools/translate-pipeline.sh (HelixTranslate)", "chunks": ev,
           "providers": sorted({e["provider"] for e in ev})}, open(d + "/pipeline.json", "w"), indent=1)
print("OK", a.lang, "providers", sorted({e["provider"] for e in ev}))
