#!/usr/bin/env bash
# verify_public_readiness.sh - public-readiness privacy gate. Scans every TRACKED text file of the repo and FAILS (rc 1) on:
#   H  an absolute home-directory path (a developer's /home/<user>/ or /Users/<user>/ path),
#   E  an email address that is not synthetic (allowed: example.com/org/net, *.example, *.test, *.invalid, *.localhost,
#      git@github.com ssh remotes, noreply addresses, and protocol identifiers such as sk-ssh-ed25519@openssh.com),
#   K  a private-key PEM block,
#   C  a credential assignment with a non-empty literal value (password/secret/token/api_key = "long literal"), or a
#      well-known provider token prefix,
#   A  an HN-style author field (an "author" key or an "author_<name>" tag) in a data fixture; the official
#      whoishiring bot is the only allowed value.
# A line carrying the marker `public-readiness:allow` is skipped (use sparingly, with a reason on the same line).
# Never prints a matched credential value: a C finding prints file:line and the rule only. Other rules print the match.
# Exit 0 clean, 1 findings, 2 cannot determine (no git repo, python3 missing). 2 is never a pass.
# --prove-failure: plants each violation class in a throwaway git repo and requires rc 1 and the right rule letter,
# requires a clean control (synthetic email, placeholder paths) to pass, requires the allow marker to skip, requires rc 2 outside a repo.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v python3 >/dev/null || { echo "UNDETERMINED: python3 missing"; exit 2; }
SCAN="$here/gates/public_readiness_scan.py"
[ -f "$SCAN" ] || { echo "UNDETERMINED: $SCAN missing"; exit 2; }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  r="$t/repo"; mkdir "$r"; git -C "$r" init -q; git -C "$r" config user.email t@example.test; git -C "$r" config user.name t
  fail=0
  plant() { # name content-file-name content want-rc want-rule
    printf '%s\n' "$3" > "$r/$2"; git -C "$r" add -- "$2"
    out="$(python3 "$SCAN" "$r" 2>&1)"; rc=$?
    if [ "$rc" -ne "$4" ] || { [ -n "$5" ] && ! printf '%s' "$out" | grep -q "\[$5\]"; }; then echo "  BAD  $1: rc=$rc want $4 rule=$5"; fail=1; else echo "  ok   $1 (rc $rc)"; fi
    if printf '%s' "$out" | grep -q 'ZZsecretvalue99'; then echo "  BAD  $1: output printed a credential value"; fail=1; fi
    git -C "$r" rm -qf --cached -- "$2"; rm -f "$r/$2"
  }
  H1="/ho""me/alice/work/x"; H2="/Us""ers/bob/x"
  plant "control: placeholders, synthetic email, ssh remote" ok.txt "path \$HOME/x <repo>/y mail jobs@example.com git@github.com:o/r.git sk-ssh-ed25519@openssh.com" 0 ""
  plant "control: whoishiring bot author allowed" ok.json '{"author":"whoishiring","_tags":["story","author_whoishiring"]}' 0 ""
  plant "H linux home path" h1.txt "see $H1/file" 1 H
  plant "H macos home path" h2.txt "see $H2/file" 1 H
  plant "E real-looking email" e.txt "contact someone@gmail.com now" 1 E
  plant "E real company domain" e2.txt "mail hr@bigcorp.io" 1 E
  plant "K PEM private key" k.txt "-----BEGIN ""RSA PRIVATE KEY-----" 1 K
  plant "C password assignment" c1.txt 'password = "ZZsecretvalue99xx"' 1 C
  plant "C yaml token assignment" c2.yaml 'api_key: ZZsecretvalue99xxyy' 1 C
  plant "C provider token prefix" c3.txt "x ghp_""ZZsecretvalue99ZZsecretvalue99ZZsecr" 1 C
  plant "C hyphenated provider key" c6.txt "k sk-""ant-api03-ZZsecretvalue99ZZsecretvalue99ZZsecretvalue99" 1 C
  plant "control: git@host ssh remote allowed" ok2.txt "remote git@codeberg.org:o/r.git" 0 ""
  plant "E domain merely containing -go" e3.txt "mail bob@ray-gordon.com" 1 E
  plant "C empty value is fine" c4.yaml 'api_key: ""' 0 ""
  plant "C env reference is fine" c5.sh 'token="$JOBSEEKER_TOKEN_VALUE"' 0 ""
  plant "A HN author field" a1.json '{"author":"someuser","comment_text":"x"}' 1 A
  plant "A HN author tag" a2.json '{"_tags":["comment","author_someuser"]}' 1 A
  plant "allow marker skips a line" m.txt "mail someone@gmail.com public-readiness:allow synthetic" 0 ""
  (cd "$t" && python3 "$SCAN" "$t/not-a-repo" >/dev/null 2>&1); rc=$?
  if [ "$rc" -eq 2 ]; then echo "  ok   not a repo -> UNDETERMINED (rc 2)"; else echo "  BAD  not a repo rc=$rc want 2"; fail=1; fi
  [ "$fail" -eq 0 ] && { echo "PROVE-FAILURE OK: every violation class (H,E,K,C,A) is caught, controls and allow marker pass, rc 2 outside a repo"; exit 0; }
  echo "PROVE-FAILURE BAD"; exit 1
fi
python3 "$SCAN" "$here"; exit $?
