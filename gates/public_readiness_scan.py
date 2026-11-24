#!/usr/bin/env python3
"""Scanner for gates/verify_public_readiness.sh. argv[1] = repo dir. Exit 0 clean, 1 findings, 2 cannot determine."""
import os, re, subprocess, sys

repo = sys.argv[1] if len(sys.argv) > 1 else "."
try:
    files = subprocess.run(["git", "-C", repo, "ls-files", "-z"], capture_output=True, check=True).stdout.split(b"\0")
except Exception as e:
    print("UNDETERMINED: not a readable git repo: %s" % e); sys.exit(2)

SKIP_EXT = {".png", ".jpg", ".jpeg", ".gif", ".pdf", ".woff", ".woff2", ".ico", ".zip", ".gz", ".db", ".sum", ".webp"}
HOME = re.compile(r"(?<![A-Za-z0-9_$])/(?:home|Users)/[A-Za-z0-9_][A-Za-z0-9_.-]*/")
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}")
SYN_DOM = re.compile(r"(?:^|\.)(?:example\.(?:com|org|net)|example|test|invalid|localhost|local|lan)$", re.I)
PEM = re.compile("-----BEGIN [A-Z ]*PRIVATE " + "KEY")
CRED = re.compile(r"""(?ix)(?:password|passwd|passphrase|secret|token|api[_-]?key|apikey|private[_-]?key|access[_-]?key)["']?\s*[:=]\s*["']?([^\s"'`,;)}\]]{8,})""")
CRED_BAD_VALUE = re.compile(r"(?ix)^(?:\$|<|\{|%|\*|nil|null|none|true|false|redacted|changeme|placeholder|synthetic|example)|canary|example|synthetic|placeholder|redacted|xxxx|\.\.\.|^[a-z_]+\(|^[A-Za-z_.]+(?:\(|\[)|\.(?:go|py|md|sh)$")
TOKEN = re.compile(r"\b(?:ghp_[A-Za-z0-9]{30,}|gho_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|sk-(?:ant-|proj-)?[A-Za-z0-9_-]{32,}|AIza[0-9A-Za-z_-]{35})\b")
AUTHOR_KEY = re.compile(r'"author"\s*:\s*"([^"]*)"')
AUTHOR_TAG = re.compile(r'"author_([A-Za-z0-9_-]+)"')
ALLOW = "public-readiness:allow"
DATA_EXT = {".json", ".tsv", ".csv", ".jsonl", ".ndjson", ".txt"}

def email_ok(m):
    local, dom = m.rsplit("@", 1)
    if SYN_DOM.search(dom): return True
    if local.lower() == "git": return True   # scp-style ssh remote (git@host:owner/repo)
    if dom.lower().startswith("noreply.") or local.lower() in ("noreply", "no-reply"): return True
    if re.match(r"(?i)(sk-|restrict-|ssh-|ecdsa-|ed25519)", local) and dom.lower() in ("openssh.com", "ssh.com"): return True
    if re.match(r"v?\d", dom) or re.search(r"-go\d", dom): return True   # module@version strings, e.g. toolchain@v0.0.1-go1.26.linux
    return False

findings = []
for raw in files:
    if not raw: continue
    path = raw.decode("utf-8", "replace")
    ext = os.path.splitext(path)[1].lower()
    if ext in SKIP_EXT: continue
    full = os.path.join(repo, path)
    if os.path.islink(full) or not os.path.isfile(full): continue
    try:
        data = open(full, "rb").read()
    except OSError:
        continue
    if b"\0" in data[:4096]: continue
    if path == "gates/verify_public_readiness.sh" or path == "gates/public_readiness_scan.py": continue
    for n, line in enumerate(data.decode("utf-8", "replace").splitlines(), 1):
        if ALLOW in line: continue
        for m in HOME.finditer(line): findings.append((path, n, "H", "absolute home path " + m.group(0)))
        for m in EMAIL.finditer(line):
            if not email_ok(m.group(0)): findings.append((path, n, "E", "non-synthetic email " + m.group(0)))
        if PEM.search(line): findings.append((path, n, "K", "private key PEM block"))
        for m in CRED.finditer(line):
            v = m.group(1)
            # a real secret literal mixes in a digit; prose, identifiers and header names do not
            if re.search(r"\d", v) and not v[0] in "+&#[" and not CRED_BAD_VALUE.search(v) and not v.lower().startswith("secret-"): findings.append((path, n, "C", "credential assignment with a literal value (value withheld)"))
        if TOKEN.search(line): findings.append((path, n, "C", "provider token prefix (value withheld)"))
        if ext in DATA_EXT and not path.endswith(".go"):
            for m in AUTHOR_KEY.finditer(line):
                if m.group(1) != "whoishiring": findings.append((path, n, "A", "HN-style author field"))
            for m in AUTHOR_TAG.finditer(line):
                if m.group(1) != "whoishiring": findings.append((path, n, "A", "HN-style author tag"))

if findings:
    for p, n, r, msg in findings: print("%s:%d: [%s] %s" % (p, n, r, msg))
    print("FAIL: %d public-readiness finding(s) in %d file(s)" % (len(findings), len({f[0] for f in findings})))
    sys.exit(1)
print("OK: public-readiness scan clean (%d tracked files, rules H E K C A)" % len([f for f in files if f]))
