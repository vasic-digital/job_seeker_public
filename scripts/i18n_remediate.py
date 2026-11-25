#!/usr/bin/env python3
"""i18n_remediate.py <lang> - after a FAILED independent review (evidence/i18n/<lang>/review.json), drop the cache entries of every key the
reviewer pointed at, so the scheduler re-translates exactly those keys (add or fix a hint in scripts/i18n_hints.json first when the cause is
ambiguity; a changed hint re-queues the key by itself). A key is "pointed at" when (a) the issue text names it (the leading "[first..last]"
slice range is NOT a naming), or (b) the issue quotes a string ('X', "X", «X») that occurs in that key's translation inside the issue's own
slice range. Terms in scripts/i18n_keep_english.json are never matched by (b). Prints the keys. The scheduler must be RESTARTED afterwards
(it holds the cache of the language it is working on in memory). Env: I18N_WORK. Exit 0, 2 if nothing was named."""
import json, os, re, sys, tempfile
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RANGE = re.compile(r"^\[(\S+?)\.\.(\S+?)\]\s*")
QUOTED = re.compile(r"'([^']{4,})'|\"([^\"]{4,})\"|«([^»]{4,})»")

def named_keys(issues, en, tr, keep=()):
    keys = sorted(en); out = set()
    for iss in issues:
        m = RANGE.match(iss); body = iss[m.end():] if m else iss
        for k in en:
            if re.search(r"(?<![\w.])" + re.escape(k) + r"(?![\w.]*\w)", body): out.add(k)
        scope = keys
        if m and m.group(1) in en and m.group(2) in en: scope = [k for k in keys if m.group(1) <= k <= m.group(2)]
        for q in QUOTED.finditer(body):
            s = next(g for g in q.groups() if g)
            if s in keep or s not in " ".join(tr.get(k, "") for k in scope): continue
            out.update(k for k in scope if s in tr.get(k, ""))
    return sorted(out)

def main(lang):
    en = json.load(open(ROOT + "/internal/i18n/messages/en.json")); tr = json.load(open(f"{ROOT}/internal/i18n/messages/{lang}.json"))
    keep = set(json.load(open(ROOT + "/scripts/i18n_keep_english.json"))["terms"])
    rv = json.load(open(f"{ROOT}/evidence/i18n/{lang}/review.json"))
    issues = list(rv.get("issues", [])) + [i for s in rv.get("slices", []) for i in (s.get("issues") or [])]
    named = named_keys(issues, en, tr, keep)
    cp = os.path.join(os.environ.get("I18N_WORK") or os.path.join(tempfile.gettempdir(), "jsk-i18n"), f"keys-{lang}.json")
    c = json.load(open(cp)); dropped = [k for k in named if c.pop(k, None) is not None]
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(cp), prefix=".tmp-")
    with os.fdopen(fd, "w") as f: json.dump(c, f, ensure_ascii=False); f.flush(); os.fsync(f.fileno())
    os.replace(tmp, cp); print(lang, "dropped", len(dropped), dropped); return 0 if dropped else 2

if __name__ == "__main__": sys.exit(main(sys.argv[1]))
