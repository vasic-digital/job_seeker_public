#!/usr/bin/env python3
"""Derive THIRD_PARTY_NOTICES.md rows from `go list -m -json all` + the module cache.
Usage: python3 scripts/gen_third_party_notices.py > rows.tsv   (module, version, licence)
Licence is classified by text markers in the module's LICENSE/COPYING file; anything
unrecognised is reported as 'see <file>' or 'UNDETERMINED' (never guessed)."""
import json, os, re, subprocess, sys

MARKERS = [
    ("Apache License", "Apache-2.0"),
    ("Mozilla Public License", "MPL-2.0"),
    ("Permission is hereby granted, free of charge", "MIT"),
    ("Redistributions in binary form must reproduce", "BSD-style (see file)"),
    ("Permission to use, copy, modify, and/or distribute", "ISC"),
]

def classify(d):
    for f in sorted(os.listdir(d)):
        if re.match(r"(?i)(licen[sc]e|copying)", f):
            t = open(os.path.join(d, f), errors="ignore").read(4000)
            for k, v in MARKERS:
                if k in t:
                    if v.startswith("BSD"):
                        v = "BSD-3-Clause" if "Neither the name" in t else "BSD-2-Clause"
                    return v
            return "see " + f
    return "UNDETERMINED"

def main():
    used = set(subprocess.run(["go", "list", "-deps", "-test", "-f", "{{with .Module}}{{.Path}}{{end}}", "./..."],
                              capture_output=True, text=True, check=True).stdout.split())
    s = subprocess.run(["go", "list", "-m", "-json", "all"], capture_output=True, text=True, check=True).stdout
    dec, i = json.JSONDecoder(), 0
    while i < len(s):
        while i < len(s) and s[i].isspace():
            i += 1
        if i >= len(s):
            break
        o, i = dec.raw_decode(s, i)
        if o.get("Main") or o["Path"] not in used:
            continue
        d = o.get("Dir")
        print(o["Path"], o["Version"], classify(d) if d else "UNDETERMINED", sep="\t")

if __name__ == "__main__":
    main()
