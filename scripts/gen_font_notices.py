#!/usr/bin/env python3
"""Derive the "Fonts" section of THIRD_PARTY_NOTICES.md from the font files actually shipped under web/static/design.

Usage:  python3 scripts/gen_font_notices.py            rewrite the section between the BEGIN/END generated:fonts markers
        python3 scripts/gen_font_notices.py --check    exit 1 when the section differs from what the tree implies, 2 when it
                                                       cannot be derived (never a pass)

One row per shipped font file: path (relative to web/static/design), family, version, licence, licence file, sha256, source.
The Noto rows read version and Debian package from web/static/design/fonts/noto/SOURCES.tsv (written by the umbrella's
design-system/fonts/tools/build_noto_subsets.py and synced here byte-for-byte). A font file whose family is not known to
FAMILIES below is reported as UNKNOWN and fails the run: a new font must be classified by a person, never guessed.
The licence gate (gates/verify_font_licences.sh) then verifies each row against the file and the licence text."""
import hashlib, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DESIGN = os.path.join(ROOT, "web", "static", "design")
NOTICES = os.path.join(ROOT, "THIRD_PARTY_NOTICES.md")
B, E = "<!-- BEGIN generated:fonts -->", "<!-- END generated:fonts -->"
EXT = (".woff2", ".woff", ".ttf", ".otf", ".ttc", ".eot")
GF = "fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header)"
# path prefix -> (family, licence file, source text); version is UNKNOWN where the tree records none
FAMILIES = [
    ("fonts/inter/", "Inter", "fonts/inter/OFL.txt", GF),
    ("fonts/jetbrains-mono/", "JetBrains Mono", "fonts/jetbrains-mono/OFL.txt", GF),
    ("fonts/space-grotesk/", "Space Grotesk", "fonts/space-grotesk/OFL.txt", GF),
    ("brand/display/bricolage-", "Bricolage Grotesque", "brand/display/OFL-Bricolage.txt", GF),
]


def sha(p):
    return hashlib.sha256(open(p, "rb").read()).hexdigest()


def noto_sources():
    p = os.path.join(DESIGN, "fonts", "noto", "SOURCES.tsv")
    if not os.path.isfile(p):
        return {}
    L = open(p, encoding="utf-8").read().splitlines()
    h = L[0].split("\t")
    return {r.split("\t")[0]: dict(zip(h, r.split("\t"))) for r in L[1:] if r}


def rows():
    out, bad = [], []
    ns = noto_sources()
    for dp, dn, fn in os.walk(DESIGN):
        dn.sort()
        for f in sorted(fn):
            if not f.lower().endswith(EXT):
                continue
            p = os.path.join(dp, f)
            rel = os.path.relpath(p, DESIGN).replace(os.sep, "/")
            if rel.startswith("fonts/noto/"):
                s = ns.get(f)
                if s is None:
                    bad.append(rel + ": not in fonts/noto/SOURCES.tsv")
                    continue
                fam = "%s %s %s" % (s["family"], s["weight"], s["tier"])
                lic = "fonts/noto/OFL.txt" if s["source_package"] == "fonts-noto-core" else "fonts/noto/OFL-CJK.txt"
                ver = "%s (source `%s`)" % (s["font_version"], s["source_file"])
                src = "Debian `%s` %s, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (%s codepoints)" % (
                    s["source_package"], s["source_package_version"], s["codepoints"])
            else:
                m = next((x for x in FAMILIES if rel.startswith(x[0])), None)
                if m is None:
                    bad.append(rel + ": family not classified in scripts/gen_font_notices.py FAMILIES")
                    continue
                fam, lic, src, ver = m[1], m[2], m[3], "UNKNOWN (not recorded in the tree)"
            out.append("| `%s` | %s | %s | OFL-1.1 | `%s` | %s | %s |" % (rel, fam, ver, lic, sha(p), src))
    return sorted(out), bad


def block(r):
    return "\n".join([B, "", "| File | Family / weight | Version | Licence | Licence file | sha256 | Source |", "|---|---|---|---|---|---|---|"]
                     + r + ["", E])


def main():
    if not os.path.isdir(DESIGN) or not os.path.isfile(NOTICES):
        print("UNDETERMINED: web/static/design or THIRD_PARTY_NOTICES.md missing", file=sys.stderr)
        return 2
    r, bad = rows()
    for b in bad:
        print("UNKNOWN " + b, file=sys.stderr)
    if bad:
        return 2
    cur = open(NOTICES, encoding="utf-8").read()
    if B not in cur or E not in cur:
        print("UNDETERMINED: markers %s / %s not found in THIRD_PARTY_NOTICES.md" % (B, E), file=sys.stderr)
        return 2
    new = re.sub(re.escape(B) + r".*?" + re.escape(E), lambda m: block(r), cur, flags=re.S)
    if "--check" in sys.argv:
        if new != cur:
            print("DRIFT: THIRD_PARTY_NOTICES.md fonts section differs from the shipped fonts; run scripts/gen_font_notices.py")
            return 1
        print("OK fonts section matches the %d shipped font file(s)" % len(r))
        return 0
    open(NOTICES, "w", encoding="utf-8").write(new)
    print("wrote %d font row(s)" % len(r))
    return 0


if __name__ == "__main__":
    sys.exit(main())
