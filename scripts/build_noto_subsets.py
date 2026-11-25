#!/usr/bin/env python3
"""Build / check the shipped Noto woff2 subsets (FONT-001, JSK-T127 follow-up).

The shipped faces under web/static/design/fonts/noto/ are defined by DATA, not by the bytes: for every face the code point
set lives in noto/codepoints/<face>.txt (hex ranges). The target set of a face is
        seed (the committed file)  UNION  every catalogue character of the face's language(s) that lies in the face's script
                                          ranges AND exists in the source font's cmap
and the CJK 'rest' face is its seed MINUS the 'core' face, so the two stay disjoint (a page loads only what it shows).
A catalogue character that the source font lacks is reported (never silently dropped) and is only acceptable when another
shipped font covers it (gates/verify_font_coverage.sh enforces that).

Source fonts are the Debian packages recorded in noto/SOURCES.tsv (fonts-noto-core, fonts-noto-cjk) at the SAME version; any
other installed version is rc 2 (never a silent rebuild from different bytes). Subsetting uses fontTools with FIXED options and
is deterministic: --reproduce builds each changed face twice and requires identical sha256.

Usage (from the repo root):
  python3 -I scripts/build_noto_subsets.py --check       no writes. rc 0 consistent, 1 drift, 2 cannot determine
  python3 -I scripts/build_noto_subsets.py --build       rewrite codepoints/*.txt, rebuild ONLY faces whose set changed,
                                                         rewrite SOURCES.tsv and the generated:noto block of fonts.css
  python3 -I scripts/build_noto_subsets.py --build --rebuild-all   rebuild every face from source
  add --reproduce to --build to prove determinism; run scripts/gen_font_notices.py afterwards for THIRD_PARTY_NOTICES.md

PINNING: the shipped set is pinned by web/static/design/fonts/noto/MANIFEST.sha256 (every file under fonts/noto/ plus the
generated:noto block of fonts.css), NOT by the umbrella sync pin (MANIFEST.sha256), because job_seeker owns these faces. --build
re-pins at its end through `jsctl sync-design --pin-noto`, and --check runs `jsctl sync-design --check` first, so a regeneration
cannot be left with a stale pin and a stale pin cannot pass the font check (needs `go`; absent -> rc 2).
"""
import csv, glob, hashlib, json, os, re, subprocess, sys, tempfile
from fontTools.ttLib import TTFont
from fontTools import subset

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DESIGN = os.path.join(ROOT, "web/static/design")
NOTO = os.path.join(DESIGN, "fonts/noto")
CPS = os.path.join(NOTO, "codepoints")
CSS = os.path.join(DESIGN, "fonts/fonts.css")
MSGS = os.path.join(ROOT, "internal/i18n/messages")
SRC_TSV = os.path.join(NOTO, "SOURCES.tsv")
CJK_TTC = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"
CORE = "/usr/share/fonts/truetype/noto/"
LAYOUT = ["kern", "locl", "vert", "vrt2", "ccmp"]  # fixed: the same feature set for every rebuilt face
BUDGET_BYTES = 3_600_000  # all shipped Noto woff2 together (embedded in the binary); --check fails above it

CJK_RANGES = [(0x2E80, 0x9FFF), (0x1100, 0x11FF), (0x3130, 0x318F), (0xAC00, 0xD7AF), (0xFF00, 0xFFEF)]
AR_RANGES = [(0x0600, 0x06FF), (0x0750, 0x077F), (0x08A0, 0x08FF), (0xFB50, 0xFDFF), (0xFE70, 0xFEFF), (0x200C, 0x200F)]
DEVA_RANGES = [(0x0900, 0x097F), (0x1CD0, 0x1CF9), (0x20B9, 0x20B9), (0xA830, 0xA839), (0xA8E0, 0xA8FF)]

# face file -> (codepoint list name, source font, ttc index, catalogue langs, script ranges, tier, weight family label)
FACES = {
    "noto-sans-arabic-400.woff2": ("arabic", CORE + "NotoSansArabic-Regular.ttf", None, ["ar", "fa"], AR_RANGES, "all", "Noto Sans Arabic 400"),
    "noto-sans-arabic-700.woff2": ("arabic", CORE + "NotoSansArabic-Bold.ttf", None, ["ar", "fa"], AR_RANGES, "all", "Noto Sans Arabic 700"),
    "noto-sans-devanagari-400.woff2": ("devanagari", CORE + "NotoSansDevanagari-Regular.ttf", None, ["hi"], DEVA_RANGES, "all", "Noto Sans Devanagari 400"),
    "noto-sans-devanagari-700.woff2": ("devanagari", CORE + "NotoSansDevanagari-Bold.ttf", None, ["hi"], DEVA_RANGES, "all", "Noto Sans Devanagari 700"),
}
for fam, idx, lang in (("sc", 2, "zh"), ("jp", 0, "ja"), ("kr", 1, "ko")):
    FACES["noto-sans-%s-400-core.woff2" % fam] = (fam + "-core", CJK_TTC, idx, [lang], CJK_RANGES, "core", "Noto Sans %s 400" % fam.upper())
    FACES["noto-sans-%s-400-rest.woff2" % fam] = (fam + "-rest", CJK_TTC, idx, [lang], CJK_RANGES, "rest", "Noto Sans %s 400" % fam.upper())
FAMILY_CSS = {"sc": "Noto Sans SC", "jp": "Noto Sans JP", "kr": "Noto Sans KR"}


def sha(p):
    return hashlib.sha256(open(p, "rb").read()).hexdigest()


def fmt_ranges(cps):
    cps = sorted(cps)
    out, i = [], 0
    while i < len(cps):
        j = i
        while j + 1 < len(cps) and cps[j + 1] == cps[j] + 1:
            j += 1
        out.append("%04X" % cps[i] if i == j else "%04X-%04X" % (cps[i], cps[j]))
        i = j + 1
    return out


def parse_ranges(tokens):
    s = set()
    for t in tokens:
        t = t.strip().lstrip("Uu+")
        if not t:
            continue
        a, _, b = t.partition("-")
        s |= set(range(int(a, 16), int(b or a, 16) + 1))
    return s


def read_cps(name):
    p = os.path.join(CPS, name + ".txt")
    if not os.path.isfile(p):
        return None
    return parse_ranges(l.split("#")[0] for l in open(p, encoding="utf-8") if l.strip())


def write_cps(name, cps):
    os.makedirs(CPS, exist_ok=True)
    with open(os.path.join(CPS, name + ".txt"), "w", encoding="utf-8") as f:
        f.write("# code points of the %s face, hex ranges; the DATA that defines the face (scripts/build_noto_subsets.py)\n" % name)
        f.write("\n".join(fmt_ranges(cps)) + "\n")


def load_font(path, idx):
    return TTFont(path, fontNumber=idx) if idx is not None else TTFont(path)


def catalogue_chars(langs, ranges):
    out = set()
    for l in langs:
        p = os.path.join(MSGS, l + ".json")
        if not os.path.isfile(p):
            raise SystemExit("UNDETERMINED: catalogue missing: " + p)
        for v in json.load(open(p, encoding="utf-8")).values():
            for ch in str(v):
                if any(a <= ord(ch) <= b for a, b in ranges):
                    out.add(ord(ch))
    return out


def targets():
    """face file -> (target set, [catalogue chars the source font lacks])"""
    res, cmaps = {}, {}
    for f, (name, src, idx, langs, ranges, tier, _) in FACES.items():
        if not os.path.isfile(src):
            raise SystemExit("UNDETERMINED: source font missing: " + src)
        key = (src, idx)
        if key not in cmaps:
            cmaps[key] = set(load_font(*key).getBestCmap())
        seed = read_cps(name)
        if seed is None:
            raise SystemExit("UNDETERMINED: no codepoints/%s.txt (run with --seed once)" % name)
        cat = catalogue_chars(langs, ranges)
        lacking = sorted(cat - cmaps[key])
        res[f] = [seed | (cat & cmaps[key]), lacking]
    for fam in FAMILY_CSS:  # CJK: core and rest are disjoint; core wins
        core, rest = "noto-sans-%s-400-core.woff2" % fam, "noto-sans-%s-400-rest.woff2" % fam
        res[rest][0] = res[rest][0] - res[core][0]
    return res


def pkg_versions():
    out = {}
    for p in ("fonts-noto-core", "fonts-noto-cjk"):
        try:
            out[p] = subprocess.run(["dpkg-query", "-W", "-f=${Version}", p], capture_output=True, text=True, check=True).stdout
        except Exception:
            out[p] = None
    return out


def sources_rows():
    return {r["file"]: r for r in csv.DictReader(open(SRC_TSV, encoding="utf-8"), delimiter="\t")}


def build_face(f, cps, outdir):
    name, src, idx, *_ = FACES[f]
    o = subset.Options()
    o.flavor = "woff2"; o.layout_features = LAYOUT; o.notdef_outline = True; o.name_IDs = [0, 1, 2, 3, 4, 5, 6]
    o.hinting = False; o.glyph_names = False
    font = load_font(src, idx)
    s = subset.Subsetter(o); s.populate(unicodes=sorted(cps)); s.subset(font)
    font.flavor = "woff2"
    if "head" in font:  # determinism: fontTools stamps head.modified with the wall clock otherwise
        font.recalcTimestamp = False
        font["head"].modified = font["head"].created
    p = os.path.join(outdir, f)
    font.save(p)
    return p


def rewrite_css(new, old_cps_by_face):
    css = open(CSS, encoding="utf-8").read()
    for fam, label in FAMILY_CSS.items():
        for tier in ("core", "rest"):
            f = "noto-sans-%s-400-%s.woff2" % (fam, tier)
            pat = re.compile(r"(font-family: '%s';[^}]*?src: url\('noto/%s'\)[^}]*?unicode-range: )([^;]*)(;)" % (re.escape(label), re.escape(f)), re.S)
            m = pat.search(css)
            if not m:
                raise SystemExit("UNDETERMINED: no @font-face for " + f)
            if tier == "core":
                rng = ", ".join("U+" + r for r in fmt_ranges(new[f][0]))
            else:  # keep the block ranges the rest face already declares, minus whatever core now owns
                old = parse_ranges(m.group(2).split(","))
                core = new["noto-sans-%s-400-core.woff2" % fam][0]
                rng = ", ".join("U+" + r for r in fmt_ranges(old - core))
            css = css[:m.start(2)] + rng + css[m.end(2):]
    open(CSS, "w", encoding="utf-8").write(css)


def jsctl(*args):
    """Run `jsctl sync-design <args>` from the repo root; returns (rc, combined output)."""
    try:
        r = subprocess.run(["go", "run", "./cmd/jsctl", "sync-design"] + list(args), cwd=ROOT, capture_output=True, text=True)
    except FileNotFoundError:
        return 2, "go toolchain not found (needed for the design pin check)"
    return r.returncode, (r.stdout + r.stderr).strip()


def main(argv):
    mode = "check" if "--check" in argv else "build" if "--build" in argv else "seed" if "--seed" in argv else None
    if mode is None:
        print(__doc__); return 2
    if mode == "seed":  # one-time: derive the lists from the faces as shipped
        for f, (name, *_ ) in FACES.items():
            if read_cps(name) is None:
                write_cps(name, set(TTFont(os.path.join(NOTO, f)).getBestCmap()))
                print("seeded", name)
        return 0
    pre = []
    if mode == "check":  # the pin check needs no host fonts: report it before anything can return rc 2
        rc, out = jsctl("--check")
        if rc == 2:
            print("UNDETERMINED: " + out); return 2
        if rc != 0:
            pre = ["design pin: " + l for l in out.splitlines()]
    ver = pkg_versions(); rows = sources_rows()
    for f, r in rows.items():
        have = ver.get(r["source_package"])
        if have != r["source_package_version"]:
            if pre:  # a finding outranks could-not-determine
                print("\n".join("FINDING " + b for b in pre)); return 1
            print("UNDETERMINED: %s installed %r != recorded %r for %s" % (r["source_package"], have, r["source_package_version"], f)); return 2
    tg = targets(); bad = []
    for f, (cps, lacking) in tg.items():
        shipped = set(TTFont(os.path.join(NOTO, f)).getBestCmap())
        if lacking:
            print("NOTE %s: catalogue chars absent from the source font (must be covered by another shipped font): %s" % (f, " ".join("U+%04X" % c for c in lacking)))
        if mode == "check":
            if rows.get(f) and rows[f]["sha256"] != sha(os.path.join(NOTO, f)):
                bad.append("%s: sha256 differs from the SOURCES.tsv row (regenerate with --build)" % f)
            if cps != shipped:
                bad.append("%s: target %d cps != shipped %d (missing %s, extra %s)" % (f, len(cps), len(shipped),
                           ["U+%04X" % c for c in sorted(cps - shipped)][:8], ["U+%04X" % c for c in sorted(shipped - cps)][:8]))
            if read_cps(FACES[f][0]) != cps:
                bad.append("%s: codepoints/%s.txt does not equal the derived target" % (f, FACES[f][0]))
    total = sum(os.path.getsize(os.path.join(NOTO, f)) for f in FACES)
    if mode == "check":
        if total > BUDGET_BYTES:
            bad.append("budget: %d bytes of Noto woff2 > %d" % (total, BUDGET_BYTES))
        bad = pre + bad
        if bad:
            print("\n".join("FINDING " + b for b in bad)); return 1
        print("OK %d faces match their code point data, total %d bytes (budget %d)" % (len(FACES), total, BUDGET_BYTES)); return 0
    rebuild_all = "--rebuild-all" in argv
    changed = []
    with tempfile.TemporaryDirectory() as t1, tempfile.TemporaryDirectory() as t2:
        for f, (cps, _) in tg.items():
            shipped = set(TTFont(os.path.join(NOTO, f)).getBestCmap())
            if cps == shipped and not rebuild_all:
                continue
            p = build_face(f, cps, t1)
            if "--reproduce" in argv:
                q = build_face(f, cps, t2)
                if sha(p) != sha(q):
                    print("FINDING non-deterministic build: " + f); return 1
                print("reproducible %s sha256=%s" % (f, sha(p)))
            os.replace(p, os.path.join(NOTO, f)); changed.append(f)
    for f, (cps, _) in tg.items():
        write_cps(FACES[f][0], cps)
    new = {f: (c, l) for f, (c, l) in tg.items()}
    rewrite_css(new, None)
    lines = open(SRC_TSV, encoding="utf-8").read().rstrip("\n").split("\n")
    hdr = lines[0].split("\t"); out = [lines[0]]
    for l in lines[1:]:
        c = dict(zip(hdr, l.split("\t")))
        f = c["file"]; p = os.path.join(NOTO, f)
        c["codepoints"] = str(len(TTFont(p).getBestCmap())); c["bytes"] = str(os.path.getsize(p)); c["sha256"] = sha(p)
        out.append("\t".join(c[h] for h in hdr))
    open(SRC_TSV, "w", encoding="utf-8").write("\n".join(out) + "\n")
    rc, out = jsctl("--pin-noto")
    if rc != 0:
        print("UNDETERMINED: could not re-pin the Noto set: " + out); return 2
    print(out)
    print("rebuilt:", ", ".join(changed) or "(none)")
    print("total noto woff2 bytes:", sum(os.path.getsize(os.path.join(NOTO, f)) for f in FACES))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
