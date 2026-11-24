#!/usr/bin/env bash
# i18n_probe.sh - JSK-T124: can the canonical HelixTranslate pipeline (§11.4.255) translate a catalogue from THIS host now?
# Exit 0 reachable (a real engine round trip returned a translation), 2 NOT reachable / could not determine. Never 1, and
# never a pass on a name merely existing: it asks the pipeline to translate a one-key sample and checks the output.
# Env: UMBRELLA (default ../), PROBE_LANG (default sr).
set -u
U="${UMBRELLA:-$(cd "$(dirname "$0")/../.." && pwd)}"
P="$U/_tools/translate-pipeline.sh"
[ -x "$P" ] || { echo "UNDETERMINED: umbrella pipeline not found at $P"; exit 2; }
t="$(mktemp -d)"; trap 'rm -rf "$t"' EXIT
printf '{"nav.inbox":"Inbox"}\n' > "$t/in.json"
echo "engine shim: $U/_tools/helixtranslate-container.sh"
for h in thinker.local amber.local; do getent hosts "$h" >/dev/null && echo "host $h resolves" || echo "host $h UNRESOLVABLE"; done
podman images 2>/dev/null | grep -qi helixtranslate && echo "local helixtranslate image present" || echo "local helixtranslate image ABSENT"
TRANSLATE_EVIDENCE_DIR="$t/log" bash "$P" --in "$t/in.json" --out "$t/out.json" --lang "${PROBE_LANG:-sr}" >"$t/run.txt" 2>&1; rc=$?
tail -3 "$t/run.txt"
if [ "$rc" -eq 0 ] && [ -s "$t/out.json" ] && ! cmp -s "$t/in.json" "$t/out.json"; then
  echo "REACHABLE: pipeline returned a translation"; cat "$t/out.json"; exit 0
fi
echo "UNDETERMINED: canonical pipeline did not translate (rc=$rc); catalogues stay pending, nothing is hand- or model-translated outside it"
exit 2
