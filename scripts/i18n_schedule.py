#!/usr/bin/env python3
"""i18n_schedule.py - quota-aware scheduler around the canonical HelixTranslate pipeline (JSK-T129..T157, §11.4.255).
Nothing is translated here: every translation is one call of the umbrella _tools/translate-pipeline.sh. This script adds
  * a provider/model ROTATION (scripts/i18n_providers.json), one target per call, pipeline fallback pinned to the SAME target so
    a failure never silently spends another provider's quota;
  * a persistent STATE file (cooldowns learned from a small quota probe after a failure, per-target counters);
  * a per-KEY cache (catalogue growth only costs the new keys), per-language assembly, evidence/i18n/<lang>/pipeline.json;
  * serial execution, backoff, never chained failures: a target in cooldown is skipped, and when every target is cooling for
    longer than --max-wait the run stops with a precise report (exit 3 = quota exhausted, resume = same command).
Env: UMBRELLA (umbrella checkout), HELIX_ENVFILE (key file, names only are ever printed), I18N_WORK (state/cache dir).
Usage: i18n_schedule.py [--dry-run] [--max-wait 300] [--lang ru,sr,...] [--targets id,id]   exit 0 all done, 2 error, 3 quota exhausted/partial."""
import argparse, hashlib, json, os, re, subprocess, sys, tempfile, time, urllib.request, urllib.error
ap = argparse.ArgumentParser(); ap.add_argument("--lang", default="ru,sr,be,de,es,fr,zh,ja,ko,ar,fa,hi,tr,kk")
ap.add_argument("--dry-run", action="store_true"); ap.add_argument("--targets", default="", help="comma list of target ids to rotate (default: all); lets one language use only translator providers that differ from its reviewer"); ap.add_argument("--max-wait", type=int, default=300)
ap.add_argument("--sleep-until-reset", type=float, default=0, metavar="HOURS", help="when every target is cooling/capped, sleep (state saved, bounded by HOURS in total) and continue instead of exiting 3"); ap.add_argument("--tries", type=int, default=2); ap.add_argument("--max-content-fails", type=int, default=3); a = ap.parse_args()
root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
U = os.environ.get("UMBRELLA") or os.path.join(root, "..")
P = os.path.join(U, "_tools", "translate-pipeline.sh")
W = os.environ.get("I18N_WORK") or os.path.join(tempfile.gettempdir(), "jsk-i18n"); os.makedirs(W, exist_ok=True)
ST = os.path.join(W, "sched-state.json")
md = os.path.join(root, "internal/i18n/messages")
cfg = json.load(open(os.path.join(root, "scripts/i18n_providers.json")))
T = cfg["targets"]; TALL = {t["id"]: t for t in T}   # full roster: assembly must resolve cache entries from ANY configured target, not only the --targets subset
en = json.load(open(md + "/en.json")); inv = set(json.load(open(md + "/_invariant.json")))
hints = json.load(open(os.path.join(root, "scripts/i18n_hints.json")))
PIPE_LANG = {"sr": "sr_cyrl"}   # the catalogue ships Serbian in CYRILLIC; the pipeline maps plain "sr" to Latin and "sr_cyrl" to the Cyrillic script profile
verb = re.compile(r"%[-+# 0-9.]*[a-zA-Z%]")
SCRIPT = {"sr": r"[Ѐ-ӿ]", "ru": r"[Ѐ-ӿ]", "be": r"[Ѐ-ӿ]", "kk": r"[Ѐ-ӿ]", "ar": r"[؀-ۿ]", "fa": r"[؀-ۿ]",
          "zh": r"[一-鿿]", "ja": r"[぀-ヿ一-鿿]", "ko": r"[가-힯]", "hi": r"[ऀ-ॿ]"}
for _l in open(os.environ.get("HELIX_ENVFILE", os.path.expanduser("~/.helixtranslate.env"))):  # key NAMES only are ever printed
    _m = re.match(r"^(?:export )?([A-Z0-9_]+)=(.*)$", _l.strip())
    if _m: os.environ.setdefault(_m.group(1), _m.group(2).strip().strip("\"'"))
state = json.load(open(ST)) if os.path.exists(ST) else {"cool": {}, "stats": {}}
def wjson(path, obj, **kw):
    """atomic write: temp in same dir, fsync, os.replace (a kill never truncates progress)"""
    d = os.path.dirname(path) or "."
    fd, tmp = tempfile.mkstemp(dir=d, prefix=".tmp-")
    with os.fdopen(fd, "w") as f:
        json.dump(obj, f, **kw); f.write("\n"); f.flush(); os.fsync(f.fileno())
    os.replace(tmp, path)
def save(): wjson(ST, state, indent=1)
def h(k): return hashlib.sha256(json.dumps([hints.get(k, en[k]), en[k]]).encode()).hexdigest()[:16]
keys = [k for k in sorted(en) if k not in inv]
def cache_path(l): return os.path.join(W, f"keys-{l}.json")
def probe_cooldown(t):
    """1-token direct request to classify a failure. Returns seconds to wait (0 = provider answers fine, so the failure was content)."""
    key = os.environ.get(t["key_env"], "")
    rq = urllib.request.Request(t["probe"] + "/chat/completions", data=json.dumps({"model": t["model"], "max_tokens": 16,
        "messages": [{"role": "user", "content": "ok"}]}).encode(), headers={"Authorization": "Bearer " + key, "Content-Type": "application/json", "User-Agent": "curl/8"})
    try:
        urllib.request.urlopen(rq, timeout=40); return 0, "ok"
    except urllib.error.HTTPError as e:
        body = e.read().decode()[:400]; ra = e.headers.get("retry-after")
        if e.code == 429:
            m = re.search(r"try again in ((?:\d+h)?(?:\d+m)?(?:[\d.]+s)?)", body)
            s = 0
            if m:
                for n, u in re.findall(r"([\d.]+)([hms])", m.group(1)): s += float(n) * {"h": 3600, "m": 60, "s": 1}[u]
            if ra: s = max(s, float(ra))
            if re.search(r"/ month|per month", body):   # monthly trial quota (cohere): wait for the next calendar month (UTC), not 30 s
                n = time.gmtime(); y, mo = (n.tm_year + (n.tm_mon == 12), n.tm_mon % 12 + 1)
                import calendar; return calendar.timegm((y, mo, 1, 0, 5, 0)) - time.time(), "429 monthly quota"
            kind = "TPD" if re.search(r"per day|TPD|RPD", body) else "TPM/RPM"
            return max(s, 30) + 5, f"429 {kind}"
        return 6 * 3600, f"http {e.code} (needs credit/tier/fix)"
    except Exception as e:
        return 120, "net " + str(e)[:40]
def _midnight_utc(): n = time.time(); return n - n % 86400 + 86400
def _day_used(t):
    d = state.setdefault("day", {}).get(t["id"]); today = time.strftime("%F", time.gmtime())
    return d["n"] if d and d["d"] == today else 0
def _day_add(t, n):
    today = time.strftime("%F", time.gmtime()); d = state.setdefault("day", {}).get(t["id"])
    if not d or d["d"] != today: d = state["day"][t["id"]] = {"d": today, "n": 0}
    d["n"] += n
def avail(t):
    """cooldown over AND the rate-aware daily key budget (day_keys, a conservative estimate of the provider's tokens/day) not spent"""
    if state["cool"].get(t["id"], 0) > time.time(): return False
    if t.get("day_keys") and _day_used(t) >= t["day_keys"]:
        state["cool"][t["id"]] = _midnight_utc() + 60; return False
    return True
def run_pipeline(t, lang, ks):
    with tempfile.TemporaryDirectory() as td:
        fi, fo = td + "/in.txt", td + "/out.txt"
        open(fi, "w", encoding="utf-8").write("\n".join("%03d. %s" % (i + 1, hints.get(k, en[k])) for i, k in enumerate(ks)) + "\n")
        env = dict(os.environ, TRANSLATE_EVIDENCE_DIR=td + "/log", TRANSLATE_PRIMARY_PROVIDER=t["provider"], TRANSLATE_PRIMARY_MODEL=t["model"],
                   TRANSLATE_FALLBACK_PROVIDER=t["provider"], TRANSLATE_FALLBACK_MODEL=t["model"])
        r = subprocess.run(["bash", P, "--in", fi, "--out", fo, "--lang", PIPE_LANG.get(lang, lang)], env=env, capture_output=True, text=True)
        if r.returncode != 0 or not os.path.exists(fo): return None, "pipeline rc=%d" % r.returncode
        got = {}
        for l in open(fo, encoding="utf-8").read().split("\n"):
            m = re.match(r"^\s*(\d+)\s*[.)]\s*(.+?)\s*$", l)
            if m:
                n = int(m.group(1))
                if n in got: return None, "duplicate tag"
                got[n] = m.group(2)
        if sorted(got) != list(range(1, len(ks) + 1)): return None, "tag set differs"
        o = {k: got[i + 1] for i, k in enumerate(ks)}
        for k in ks:
            # a hint's own parenthetical instruction must never leak into the translation (seen: "Modo (encabezamiento de columna..., una palabra)")
            if "(" in hints.get(k, "") and "(" not in en[k] and re.search(r"[(\uff08]", o[k]): return None, "hint parenthetical leaked into " + k
            if verb.findall(o[k]) != verb.findall(en[k]): return None, "printf verbs (order-aware, like i18naudit) changed in " + k
            if lang in SCRIPT and not re.search(SCRIPT[lang], o[k]) and re.search(r"[A-Za-z]{4}", re.sub(r"robots\.txt|%[a-z]", "", en[k])): return None, "target script absent in " + k
        return o, "ok"
if a.targets:
    _w = a.targets.split(","); _bad = [x for x in _w if x not in {t["id"] for t in T}]
    if _bad: print("UNDETERMINED: unknown target id(s):", _bad); sys.exit(2)
    T = [t for t in T if t["id"] in _w]
langs = a.lang.split(",")
PINNED = bool(a.targets)
def targets_for(lang):
    """explicit --targets wins; else the per-language plan (providers.json lang_targets); else every enabled target"""
    if PINNED: return T
    plan = cfg.get("lang_targets", {}).get(lang)
    ids = plan if plan else [t["id"] for t in T if t.get("enabled", True)]
    return [TALL[i] for i in ids if i in TALL]
if a.dry_run:
    print("DRY RUN - no request is made. umbrella pipeline:", P, "present" if os.access(P, os.X_OK) else "MISSING")
    for t in T: print(f"enabled={t.get('enabled', True)} day_keys={t.get('day_keys', '-')} used_today={_day_used(t)} target {t['id']:20} chunk={t['chunk']} pause={t['pause']}s cooldown_left={max(0, int(state['cool'].get(t['id'], 0) - time.time()))}s")
    tot = 0
    for l in langs:
        c = json.load(open(cache_path(l))) if os.path.exists(cache_path(l)) else {}
        miss = [k for k in keys if k not in c or c[k]["h"] != h(k)]; tot += len(miss)
        print(f"{l}: {len(keys) - len(miss)}/{len(keys)} cached, {len(miss)} to translate, {-(-len(miss) // (targets_for(l) or T)[0]['chunk'])} call(s) via {[t['id'] for t in targets_for(l)]}")
    print("total keys to translate:", tot); sys.exit(0)
if not os.access(P, os.X_OK): print("UNDETERMINED: pipeline missing at", P); sys.exit(2)
done_langs, blocked = [], []
width, cfail = {}, {}   # per first-key: chunk width cap (bisection) and content-failure count; a lone key failing repeatedly blocks the language
idx = 0; slept = 0
for lang in langs:
    cp = cache_path(lang); c = json.load(open(cp)) if os.path.exists(cp) else {}
    TL = targets_for(lang)
    if not TL: print(f"UNDETERMINED: no target configured for {lang}"); blocked.append(lang); continue
    while True:
        miss = [k for k in keys if k not in c or c[k]["h"] != h(k)]
        if not miss: break
        # pick the FIRST available target of the language plan (priority order; a failing target cools down and the next one takes over until it is back); wait out SHORT cooldowns only
        for _ in range(2):
            av = [t for t in TL if avail(t)]
            if av: break
            wait = min(state["cool"].get(t["id"], 0) for t in TL) - time.time()
            if wait > a.max_wait: break
            print(f"all targets cooling; waiting {int(wait)}s", flush=True); time.sleep(max(wait, 1))
        if not av and a.sleep_until_reset and slept < a.sleep_until_reset * 3600:
            wait = max(60, min(min(state["cool"].get(t["id"], 0) for t in TL) - time.time(), 3600)); save()
            print(f"{lang}: every target cooling/capped; sleeping {int(wait)}s (slept {slept/3600:.1f}h of max {a.sleep_until_reset}h; resets: " + str({t['id']: time.strftime('%F %T', time.localtime(state['cool'].get(t['id'], 0))) for t in TL}) + ")", flush=True)
            time.sleep(wait); slept += wait; continue
        if not av:
            print("QUOTA EXHAUSTED; cooldown until:", {k: time.strftime("%F %T", time.localtime(v)) for k, v in state["cool"].items() if k in {t["id"] for t in TL}}); blocked.append(lang); break
        t = av[0]; ks = miss[:min(t["chunk"], width.get(miss[0], 99))]
        res, why = None, ""
        for tr in range(a.tries):
            res, why = run_pipeline(t, lang, ks)
            s = state["stats"].setdefault(t["id"], {"calls": 0, "ok": 0}); s["calls"] += 1
            if res: s["ok"] += 1; break
            time.sleep(10)
        if res:
            for k in ks: c[k] = {"t": res[k], "h": h(k), "via": t["id"]}
            _day_add(t, len(ks)); wjson(cp, c, ensure_ascii=False); save()
            print(f"{lang} +{len(ks)} keys via {t['id']} ({len(c)}/{len(keys)})", flush=True); time.sleep(t["pause"])
        else:
            sec, kind = probe_cooldown(t)
            print(f"{lang} FAIL via {t['id']}: {why}; probe: {kind}; cooldown {int(sec)}s", flush=True)
            if sec: state["cool"][t["id"]] = time.time() + sec
            else:  # provider answered fine, so the CONTENT failed: bisect the chunk; a lone key that keeps failing blocks the language
                state["cool"][t["id"]] = time.time() + 60
                cfail[ks[0]] = cfail.get(ks[0], 0) + 1
                if len(ks) > 1 and cfail[ks[0]] >= 2: width[ks[0]] = max(1, len(ks) // 2); cfail[ks[0]] = 0
                elif len(ks) == 1 and cfail[ks[0]] >= a.max_content_fails:
                    print(f"BLOCKED {lang}: key {ks[0]} failed validation {cfail[ks[0]]}x ({why}); not retrying (quota protection)"); blocked.append(lang); break
            save(); idx += 1
    else:
        pass
    if lang in blocked: continue
    out = {k: en[k] for k in inv}; out.update({k: c[k]["t"] for k in keys})
    wjson(f"{md}/{lang}.json", dict(sorted(out.items())), ensure_ascii=False, indent=1)
    via = sorted({c[k]["via"] for k in keys}); tg = TALL
    _unk = [v for v in via if v not in tg]
    if _unk: print(f"BLOCKED {lang}: cache holds keys from target(s) {_unk} that are not in scripts/i18n_providers.json (re-add them or drop those cache entries)"); blocked.append(lang); continue
    d = os.path.join(root, "evidence/i18n", lang); os.makedirs(d, exist_ok=True)
    wjson(d + "/pipeline.json", {"lang": lang, "source": "internal/i18n/messages/en.json", "pipeline": "umbrella _tools/translate-pipeline.sh (HelixTranslate), via i18n_schedule.py",
               "providers": sorted({tg[v]["provider"] + ":" + tg[v]["model"] for v in via}), "targets": via, "keys": len(keys)}, indent=1)
    done_langs.append(lang); print("ASSEMBLED", lang, via, flush=True)
print("DONE", done_langs, "BLOCKED", blocked); sys.exit(3 if blocked else 0)
