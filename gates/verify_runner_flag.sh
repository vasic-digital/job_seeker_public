#!/usr/bin/env bash
# verify_runner_flag.sh - the ecosystem --runner=NAME contract on job_seeker's container tooling (jsstack and
# gates/live_h3.sh), proven on the REAL host (Containers module docs/RUNNER_FLAG.md): unknown runner -> rc 2;
# forced runner not installed -> rc 2 with NO fallback runtime answering; installed runner -> rc 0 and the
# answering runtime IS that runner; flag beats env. Needs one installed and one absent runner among
# podman/docker/nerdctl, else 2 (never a pass). Exit: 0 pass, 1 fail, 2 could not determine.
# --prove-failure: source mutations (restore auto-detect, drop the refusal, drop live_h3's forwarding) must
#   each make this gate FAIL; the unmutated tree is the control (paired mutation, section 1.1).
set -u
root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"; bak="$tmp/bak"; mkdir -p "$bak"
EVF=evidence/JSK-T032/live-h3.txt
restore() { [ -f "$bak/live-h3.txt" ] && cp "$bak/live-h3.txt" "$root/$EVF"; for f in tools/containers/cmd/jsstack/main.go gates/live_h3.sh; do [ -f "$bak/$(basename "$f")" ] && cp "$bak/$(basename "$f")" "$root/$f"; done; rm -rf "$tmp"; }
trap restore EXIT; trap 'exit 2' INT TERM
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
# G04: go.mod replaces point at ../../.deps/<name> (untracked, gitignored symlinks). Self-provision them via the
# non-destructive resolver (it only creates/refreshes links inside THIS checkout's .deps/, never touches another
# checkout and removes only a stale link whose dependency no longer resolves). If provisioning fails the answer is
# rc 2 naming the dependency and the remedy - never a pass.
provision_deps() {
  local out; out="$(bash "$root/scripts/resolve_deps.sh" link 2>&1)"
  if [ ! -f "$root/.deps/containers/go.mod" ]; then
    echo "UNDETERMINED: dependency 'containers' not resolvable (needed by go.mod replace ../../.deps/containers)"
    printf '%s\n' "$out" | sed 's/^/  resolve_deps: /'
    echo "  remedy: set JSK_DEP_CONTAINERS=<checkout at the helix-deps.yaml pin>, or run: bash scripts/resolve_deps.sh bootstrap"
    return 2
  fi
}
present=""; absent=""
for r in podman docker nerdctl; do
  if command -v "$r" >/dev/null 2>&1; then [ -n "$present" ] || present="$r"; else [ -n "$absent" ] || absent="$r"; fi
done
[ -n "$present" ] && [ -n "$absent" ] || { echo "UNDETERMINED: need one installed and one absent runner among podman/docker/nerdctl"; exit 2; }

build() { # -> $tmp/bin with jsstack, jsgate-probe, jsgate-serve
  mkdir -p "$tmp/bin"
  ( cd "$root/tools/containers" && CGO_ENABLED=0 go build -o "$tmp/bin/jsstack" ./cmd/jsstack && CGO_ENABLED=0 go build -o "$tmp/bin/jsgate-probe" ./cmd/jsgate-probe ) >"$tmp/build.log" 2>&1 \
    && ( cd "$root" && CGO_ENABLED=0 go build -o "$tmp/bin/jsgate-serve" ./cmd/jsgate-serve ) >>"$tmp/build.log" 2>&1
}
fails=0
chk() { if [ "$2" = "$3" ]; then echo "  PASS $1"; else echo "  FAIL $1 (rc=$2 want $3)"; fails=$((fails+1)); fi; }
run_checks() {
  local js="$tmp/bin/jsstack" out rc
  unset CONTAINERS_RUNNER
  "$js" probe --runner=bogus >/dev/null 2>&1; chk "jsstack unknown runner" $? 2
  out="$("$js" probe --runner="$absent" 2>&1)"; rc=$?; chk "jsstack --runner=$absent (not installed) refused" "$rc" 2
  case "$out" in *runtime=*) echo "  FAIL a fallback runtime answered for $absent"; fails=$((fails+1)) ;; esac
  out="$("$js" probe --runner="$present" 2>&1)"; rc=$?; chk "jsstack --runner=$present accepted" "$rc" 0
  case "$out" in *"runtime=$present "*) echo "  PASS answering runtime is $present" ;; *) echo "  FAIL answering runtime not $present: $out"; fails=$((fails+1)) ;; esac
  # --help must print usage and exit 0 WITHOUT running anything (before: live_h3.sh --help started the live gate).
  out="$("$js" --help 2>&1)"; rc=$?; chk "jsstack --help rc 0" "$rc" 0
  case "$out" in *--runner*) echo "  PASS jsstack --help documents --runner" ;; *) echo "  FAIL jsstack --help does not document --runner"; fails=$((fails+1)) ;; esac
  out="$(JS_BIN_DIR="$tmp/nonexistent" bash "$root/gates/live_h3.sh" --help 2>&1)"; rc=$?; chk "live_h3.sh --help rc 0 (no binaries needed, nothing run)" "$rc" 0
  case "$out" in *--runner*) echo "  PASS live_h3.sh --help documents --runner" ;; *) echo "  FAIL live_h3.sh --help does not document --runner"; fails=$((fails+1)) ;; esac
  # runtime layer alone: an EXISTING container is visible to the installed runner, so a forced absent runner must
  # still be refused (a silent fallback to auto-detect would answer 0 here). Needs one container on the host.
  local ex; ex="$("$present" ps -a --format '{{.Names}}' 2>/dev/null | head -1)"
  if [ -n "$ex" ]; then
    "$js" status -container "$ex" --runner="$present" >/dev/null 2>&1; chk "status of existing container via --runner=$present" $? 0
    "$js" status -container "$ex" --runner="$absent" >/dev/null 2>&1; chk "runtime layer alone (status) refuses --runner=$absent" $? 2
  else echo "  SKIP runtime-layer check: no container exists on this host to look up"; fi
  # The compose file MUST be a real, readable one: with a missing file jsstack exits 2 on "cannot read compose file"
  # BEFORE it builds the orchestrator, so the check passed for the wrong reason and mutant M2 (compose ignores the
  # forced runner) survived. With a real file, only the orchestrator's refusal of the forced runner yields rc 2.
  printf 'services:\n  x:\n    image: scratch\n' > "$tmp/runner-flag-compose.yml"
  out="$("$js" down -file "$tmp/runner-flag-compose.yml" --runner="$absent" 2>&1)"; rc=$?; chk "compose layer alone (down) refuses --runner=$absent" "$rc" 2
  case "$out" in *"cannot read compose file"*) echo "  FAIL compose check refused for the wrong reason (unreadable file): $out"; fails=$((fails+1)) ;; esac
  CONTAINERS_RUNNER=bogus "$js" probe >/dev/null 2>&1; chk "env CONTAINERS_RUNNER=bogus refused" $? 2
  CONTAINERS_RUNNER=bogus "$js" probe --runner=auto >/dev/null 2>&1; chk "flag overrides a bad env value" $? 0
  JS_BIN_DIR="$tmp/bin" timeout 90 bash "$root/gates/live_h3.sh" --runner=bogus >/dev/null 2>&1; chk "live_h3.sh unknown runner" $? 2
  out="$(JS_BIN_DIR="$tmp/bin" timeout 90 bash "$root/gates/live_h3.sh" --runner="$absent" 2>&1)"; rc=$?; chk "live_h3.sh --runner=$absent refused before any container" "$rc" 2
  # rc 2 alone is not proof: a live run that dropped the forced runner can also end in 2 for an unrelated reason.
  case "$out" in *"forced runner is refused"*) ;; *) echo "  FAIL live_h3.sh rc 2 but not the forced-runner refusal (flag not honoured)"; fails=$((fails+1)) ;; esac
  JS_BIN_DIR="$tmp/bin" timeout 90 bash "$root/gates/live_h3.sh" --runner >/dev/null 2>&1; chk "live_h3.sh valueless --runner" $? 2
}
if [ "${1:-}" = "--prove-failure" ]; then
  for f in tools/containers/cmd/jsstack/main.go gates/live_h3.sh; do cp "$root/$f" "$bak/$(basename "$f")"; done
  [ -f "$root/$EVF" ] && cp "$root/$EVF" "$bak/live-h3.txt"   # a surviving mutant may run the real gate, which rewrites it
  caught=0; total=0
  # P1 (G04): dependency provisioning failure must be rc 2 with a message naming the dependency + remedy, never 0/1.
  total=$((total+1))
  pout="$(JSK_DEP_CONTAINERS="$tmp/does-not-exist" bash "$0" 2>&1)"; prc=$?
  if [ "$prc" = 2 ] && printf '%s' "$pout" | grep -q "dependency 'containers' not resolvable" && printf '%s' "$pout" | grep -q "remedy:"; then
    caught=$((caught+1)); echo "  KILLED   P1-deps-unresolvable-is-rc2-with-message"
  else echo "  SURVIVED P1-deps-unresolvable (rc=$prc): $pout"; fi
  bash "$root/scripts/resolve_deps.sh" link >/dev/null 2>&1   # P1 removed the stale link; restore it
  mut() { # name file sed-expr
    total=$((total+1)); cp "$bak/$(basename "$2")" "$root/$2"; sed -i "$3" "$root/$2"
    if cmp -s "$bak/$(basename "$2")" "$root/$2"; then echo "  MUTATION $1 changed nothing"; return; fi
    rm -rf "$tmp/bin"; fails=0
    if ! build; then echo "  (build failed for $1)"; cat "$tmp/build.log"; cp "$bak/$(basename "$2")" "$root/$2"; return; fi
    run_checks >/dev/null 2>&1
    if [ "$fails" -gt 0 ]; then caught=$((caught+1)); echo "  KILLED   $1"; else echo "  SURVIVED $1"; fi
    cp "$bak/$(basename "$2")" "$root/$2"
  }
  mut M1-autodetect-ignores-flag tools/containers/cmd/jsstack/main.go 's/runtime.SelectRuntime(ctx, choice)/runtime.AutoDetect(ctx)/'
  mut M2-compose-ignores-runner tools/containers/cmd/jsstack/main.go 's/, choice.Name)/, "")/'
  mut M3-resolve-ignores-env tools/containers/cmd/jsstack/main.go 's/runtime.ResolveRunner(val, found, os.Getenv)/runtime.ResolveRunner(val, found, func(string) string { return "" })/'
  mut M4-flag-parse-errors-swallowed tools/containers/cmd/jsstack/main.go 's/if err != nil {\n\t\tfmt.Fprintf(os.Stderr, "jsstack: %v/XX/;0,/\treturn exitUndetermined/s//\treturn exitOK/'
  mut M5-live_h3-drops-forwarding gates/live_h3.sh 's/export CONTAINERS_RUNNER="${1#--runner=}"; shift/shift/'
  echo "prove-failure: $caught/$total mutations killed"
  [ "$caught" = "$total" ] && exit 0 || exit 1
fi
provision_deps || exit 2
build || { echo "UNDETERMINED: build failed"; cat "$tmp/build.log"; exit 2; }
run_checks
if [ "$fails" -eq 0 ]; then echo "runner-flag gate: OK"; exit 0; fi
echo "runner-flag gate: $fails FAILED"; exit 1
