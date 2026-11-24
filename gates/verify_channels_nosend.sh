#!/usr/bin/env bash
# verify_channels_nosend.sh - JSK-T232 channel no-send gate (FR-098, FR-027, SC-033). Asserts, on the real tree:
#   1. the channel suite passes (every registered channel refuses a zero and a forged Approved, none declares direct_send),
#   2. the only production callers of ext.NewApproved are the allow-listed files (the definition and the approval service),
#   3. no channel package declares "direct_send": true, imports net/smtp, net/rpc or os/exec, or names a mail-provider send endpoint,
#   4. the channel registry is built with ext.ChannelVet.
# Exit 0 pass, 1 finding, 2 cannot determine. --prove-failure: throwaway copies of the tree, each with ONE defect
# (second NewApproved caller, direct_send true, net/smtp import, send endpoint string, Vet removed, a channel that
# accepts a zero Approved); the control must pass and every defect must fail; a tree without channels is rc 2.
# --root DIR checks another tree; --static skips the go test (used inside the proof for the static defects).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
root="$here"; prove=0; static=0
while [ $# -gt 0 ]; do case "$1" in --root) root="${2:-}"; shift;; --prove-failure) prove=1;; --static) static=1;; esac; shift; done
command -v python3 >/dev/null || { echo "UNDETERMINED: python3 missing"; exit 2; }

check() { # root static
python3 - "$1" <<'PY' || return $?
import os, re, sys
root = sys.argv[1]
chan = os.path.join(root, "internal/ext/channel")
bi = os.path.join(root, "internal/ext/builtin/builtin.go")
if not os.path.isdir(chan) or not os.path.isfile(bi) or not os.path.isfile(os.path.join(root, "internal/ext/ports.go")):
    print("UNDETERMINED: channel tree unreadable"); sys.exit(2)
allow = {"internal/ext/ports.go", "internal/draft/service.go"}
findings, callers, pkgs = [], [], set()
for dp, dn, fn in os.walk(root):
    rel = os.path.relpath(dp, root).replace(os.sep, "/")
    if rel.split("/")[0] in ("docs", "submodules", "tools", "web", "specs", "evidence", ".git"):
        dn[:] = []; continue
    for f in fn:
        if not f.endswith(".go") or f.endswith("_test.go"): continue
        p = rel + "/" + f if rel != "." else f
        t = open(os.path.join(dp, f)).read()
        if re.search(r"\bNewApproved\(", t):
            callers.append(p)
            if p not in allow: findings.append("NewApproved called outside the approval service: " + p)
        if rel.startswith("internal/ext/channel/") and rel.count("/") == 3:
            pkgs.add(rel)
            if re.search(r'"direct_send"\s*:\s*true', t): findings.append(p + ": declares direct_send true")
            for imp in ("net/smtp", "net/rpc", "os/exec"):
                if '"%s"' % imp in t: findings.append("%s: imports %s" % (p, imp))
            if re.search(r"messages/send|drafts/send|messages:send|/sendmail|sendMail", t, re.I): findings.append(p + ": names a mail send endpoint")
if "internal/ext/ports.go" not in callers: findings.append("the NewApproved definition was not found")
if not pkgs: print("UNDETERMINED: no channel package found"); sys.exit(2)
if "Channels.Vet = ext.ChannelVet" not in open(bi).read(): findings.append("builtin.go does not build the channel registry with ext.ChannelVet")
if findings:
    print("FAIL: %d finding(s)" % len(findings))
    for f in findings: print("  - " + f)
    sys.exit(1)
print("OK: %d channel package(s), NewApproved callers %s, no send surface, Vet wired" % (len(pkgs), sorted(callers)))
PY
rc=$?; [ $rc -ne 0 ] && return $rc
if [ "$static" -eq 0 ]; then
  command -v go >/dev/null || { echo "UNDETERMINED: go missing"; return 2; }
  (cd "$1" && go test -count=1 -run 'TestEveryRegisteredChannelRefusesZeroApprovedAndNeverSends|TestNewApprovedCallersAreOnlyTheAllowList' ./internal/ext/channel/ >/dev/null 2>&1) || { echo "FAIL: channel suite failed"; return 1; }
fi
return 0
}

if [ "$prove" -eq 0 ]; then check "$root"; exit $?; fi
t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
mk() { mkdir -p "$t/$1"; (cd "$here" && tar --exclude=.git --exclude=evidence --exclude=docs --exclude=specs --exclude=web -cf - .) | tar -xf - -C "$t/$1"; }
bad=""
mk control; check "$t/control" >/dev/null; c=$?
want() { # name expected-rc root [static]
  local n="$1" w="$2" r="$3"; shift 3; static=${1:-1}; check "$r" >/dev/null; [ $? -eq "$w" ] || bad="$bad $n"
}
mk d1; printf 'package draft\n\nimport "github.com/vasic-digital/job_seeker/internal/ext"\n\nfunc leak() { _, _ = ext.NewApproved(1, "x", "h", "h", "https://a.example/") }\n' > "$t/d1/internal/ext/channel/leak.go"
mkdir -p "$t/d1/internal/ext/channel/second"; mv "$t/d1/internal/ext/channel/leak.go" "$t/d1/internal/ext/channel/second/leak.go"; sed -i 's/^package draft/package second/' "$t/d1/internal/ext/channel/second/leak.go"
want second-NewApproved-caller 1 "$t/d1"
mk d2; sed -i 's/"direct_send": false, "needs_human": true/"direct_send": true, "needs_human": true/' "$t/d2/internal/ext/channel/openurl/openurl.go"; cmp -s "$here/internal/ext/channel/openurl/openurl.go" "$t/d2/internal/ext/channel/openurl/openurl.go" && bad="$bad d2-nochange"
want direct-send-true 1 "$t/d2"
mk d3; printf 'package mailto\n\nimport _ "net/smtp"\n' > "$t/d3/internal/ext/channel/mailto/smtp.go"
want net-smtp-import 1 "$t/d3"
mk d4; printf 'package mailto\n\nconst x = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"\n' > "$t/d4/internal/ext/channel/mailto/ep.go"
want send-endpoint 1 "$t/d4"
mk d5; sed -i 's/s.Channels.Vet = ext.ChannelVet//' "$t/d5/internal/ext/builtin/builtin.go"
want vet-removed 1 "$t/d5"
mk d6; sed -i 's/if !a.Valid() {/if false {/' "$t/d6/internal/ext/channel/openurl/openurl.go"
cmp -s "$here/internal/ext/channel/openurl/openurl.go" "$t/d6/internal/ext/channel/openurl/openurl.go" && bad="$bad d6-nochange"
want zero-approved-accepted 1 "$t/d6" 0
mk d7; rm -rf "$t/d7/internal/ext/channel"
want no-channels 2 "$t/d7"
if [ "$c" -eq 0 ] && [ -z "$bad" ]; then echo "PROVE-FAILURE OK: control rc=0; 6 defects (second NewApproved caller, direct_send, net/smtp, send endpoint, Vet removed, zero Approved accepted) rejected rc=1; no channels rc=2"; exit 0; fi
echo "PROVE-FAILURE BAD: control=$c survivors:$bad"; exit 1
