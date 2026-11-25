#!/usr/bin/env python3
"""qa-helixqa-verify.py - machine verdict over a `helixqa autonomous` run of the authenticated console (JSK-T084/T111).

usage: qa-helixqa-verify.py OUT_DIR [--min-cases N] [--min-distinct N] [--table FILE.tsv]
Reads OUT_DIR/session-*/pipeline-report.json (latest), OUT_DIR/helixqa.rc and the saved frames, and requires ALL of:
  * helixqa exit code 0 and report status complete
  * every structured case PASSED (no blocked / failed / skipped), at least --min-cases cases
  * every step vision-gated with vision_status == verified, a vision provider named, a non-empty frame sha256
  * every cited frame sha256 matches a saved file under screenshots/ (the frame the verdict names EXISTS, with those bytes)
  * at least --min-distinct DISTINCT step frames (the console was actually navigated) and report frames_distinct > 1
Exit 0 all satisfied, 1 a requirement failed (named), 2 cannot determine (no report). 2 is never a pass.
It never reads or prints a cookie."""
import glob
import hashlib
import json
import os
import sys


def main(argv):
    out = argv[1] if len(argv) > 1 else ""
    opt = {"--min-cases": 7, "--min-distinct": 6, "--table": ""}
    i = 2
    while i < len(argv):
        if argv[i] in opt and i + 1 < len(argv):
            opt[argv[i]] = type(opt[argv[i]])(argv[i + 1])
            i += 2
        else:
            print("usage error: %s" % argv[i], file=sys.stderr)
            return 2
    reps = sorted(glob.glob(os.path.join(out, "session-*", "pipeline-report.json")))
    if not reps:
        print("cannot determine: no pipeline-report.json under %s" % out, file=sys.stderr)
        return 2
    rep = json.load(open(reps[-1]))
    sess = os.path.dirname(reps[-1])
    bad = []
    try:
        rc = int(open(os.path.join(out, "helixqa.rc")).read().strip())
    except Exception:
        print("cannot determine: no helixqa.rc", file=sys.stderr)
        return 2
    if rc != 0:
        bad.append("helixqa exit code %d" % rc)
    if rep.get("status") != "complete":
        bad.append("report status %r" % rep.get("status"))
    cases = (rep.get("structured") or {}).get("cases") or []
    if len(cases) < opt["--min-cases"]:
        bad.append("only %d structured cases (< %d)" % (len(cases), opt["--min-cases"]))
    files = {}
    for f in glob.glob(os.path.join(sess, "screenshots", "*.png")):
        files[hashlib.sha256(open(f, "rb").read()).hexdigest()] = os.path.basename(f)
    rows, shas = [], set()
    for c in cases:
        if c.get("verdict") != "PASSED":
            bad.append("%s verdict %s" % (c.get("id"), c.get("verdict")))
        for s in c.get("steps") or []:
            sha = s.get("frame_sha256") or ""
            ok = True
            if not s.get("vision_gated"):
                bad.append("%s step not vision-gated" % c["id"]); ok = False
            if s.get("vision_status") != "verified":
                bad.append("%s vision_status %s" % (c["id"], s.get("vision_status"))); ok = False
            if not s.get("vision_provider"):
                bad.append("%s no vision provider named" % c["id"]); ok = False
            if not sha:
                bad.append("%s no frame sha256" % c["id"]); ok = False
            elif sha not in files:
                bad.append("%s cites frame %s... which is not a saved file" % (c["id"], sha[:12])); ok = False
            if sha:
                shas.add(sha)
            rows.append((c["id"], c.get("name", ""), sha, files.get(sha, "MISSING"), s.get("vision_provider", ""),
                         s.get("vision_status", ""), "PASS" if ok and c.get("verdict") == "PASSED" else "FAIL"))
    if len(shas) < opt["--min-distinct"]:
        bad.append("only %d distinct step frames (< %d): the console was not navigated" % (len(shas), opt["--min-distinct"]))
    if int(rep.get("frames_distinct") or 0) <= 1:
        bad.append("frames_distinct %s" % rep.get("frames_distinct"))
    if opt["--table"]:
        with open(opt["--table"], "w") as t:
            t.write("case\tname\tframe_sha256\tframe_file\tvision_provider\tvision_status\tresult\n")
            for r in rows:
                t.write("\t".join(r) + "\n")
    print("cases=%d steps=%d distinct_step_frames=%d frames_distinct=%s" % (len(cases), len(rows), len(shas), rep.get("frames_distinct")))
    for b in bad:
        print("FAIL: " + b)
    if bad:
        return 1
    print("PASS: every requirement met")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
