#!/usr/bin/env bash
# verify_argorder.sh - JSK-005: printf ARGUMENT ORDER of every multi-verb console message. verify_i18n.sh proves each
# translation has the same verb TYPES; it cannot see a translation that reads "<total> ... <matched>" while the call site
# passes (matched, total). internal/i18n/messages/_argorder.json declares each argument's meaning (from the template call
# site) and the order each language's sentence reads them; this gate renders every translation with sentinel values and
# requires the rendered order to equal the declaration. HONEST LIMIT: a catalogue cannot know what its sentence means, so
# the declaration is a human assertion (one line per language, checked by the independent translation reviewer); the gate
# proves the assertion and the real rendering AGREE - it cannot detect a declaration that lies about the sentence. %[n]d is supported (Go fmt; i18n.Msg is fmt.Sprintf).
# Exit: 0 clean, 1 defect, 2 could not determine. --prove-failure seeds defects into a throwaway copy and requires rc=1.
set -u
cd "$(dirname "$0")/.."
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
bin="$(mktemp -d)"; trap 'rm -rf "$bin"' EXIT
go build -o "$bin/i18nargorder" ./cmd/i18nargorder || { echo "UNDETERMINED: build failed"; exit 2; }
if [ "${1:-}" = "--prove-failure" ]; then
  src=internal/i18n/messages; fail=0
  mk() { rm -rf "$bin/m"; mkdir -p "$bin/m"; cp "$src"/*.json "$bin/m/"; }
  chk() { "$bin/i18nargorder" -dir "$bin/m" >/dev/null 2>&1; echo $?; }
  want() { got="$(chk)"; [ "$got" = "$2" ] && echo "ok   $1 rc=$got" || { echo "BAD  $1 rc=$got want $2"; fail=1; }; }
  edit() { python3 - "$bin/m" "$@" <<'P'
import json,sys
d,lang,key,val=sys.argv[1:5]
p=d+"/"+lang+".json"; m=json.load(open(p)); m[key]=val; json.dump(m,open(p,"w"),ensure_ascii=False)
P
  }
  decl() { python3 - "$bin/m" "$@" <<'P2'
import json,sys
d,key,lang,roles=sys.argv[1:5]
p=d+"/_argorder.json"; s=json.load(open(p)); s[key]["reading"][lang]=roles.split(","); json.dump(s,open(p,"w"))
P2
  }
  mk; want "control (shipped catalogues)" 0
  # M1: the 2026-11-24 defect - ru text reads total-first with PLAIN verbs; the declaration truthfully says total-first
  mk; edit ru report.coverage.counts "Всего терминов: %d, из них в резюме: %d; отсутствует %d, из них %d нет в профиле."; decl report.coverage.counts ru terms,matched,missing,unclaimable; want "M1 total-first plain verbs (declared honestly)" 1
  # M2: same text reordered with indexed verbs but declaration NOT updated -> reading mismatch is still caught
  mk; edit ru report.coverage.counts "Всего терминов: %[2]d, из них в резюме: %[1]d; отсутствует %[3]d, из них %[4]d нет в профиле."; want "M2 reordered but declaration not updated" 1
  # M3: declaration updated to match indexed reordering -> clean (proves %[n]d is accepted and honoured)
  python3 - "$bin/m" <<'P'
import json,sys
p=sys.argv[1]+"/_argorder.json"; s=json.load(open(p))
s["report.coverage.counts"]["reading"]["ru"]=["terms","matched","missing","unclaimable"]; json.dump(s,open(p,"w"))
P
  want "M3 control: indexed reorder with declaration updated" 0
  mk; edit de report.diff.counts "%d Zeilen entfernt und %d hinzugefügt (Vergleich mit dem am %s gesendeten Lebenslauf)."; decl report.diff.counts de removed,added,sent_at; want "M4 added/removed swapped (diff.counts)" 1
  mk; edit es report.diff.counts "%d líneas añadidas y %d eliminadas."; want "M5 argument dropped" 1
  mk; edit kk report.diff.counts "%d жол қосылды, %s жойылды, %d"; want "M6 verb type swapped" 1
  mk; python3 - "$bin/m" <<'P'
import json,sys
p=sys.argv[1]+"/_argorder.json"; s=json.load(open(p)); del s["report.diff.counts"]["reading"]["sr"]; json.dump(s,open(p,"w"))
P
  want "M7 language without declared reading" 1
  mk; python3 - "$bin/m" <<'P'
import json,sys
p=sys.argv[1]+"/_argorder.json"; s=json.load(open(p)); del s["report.coverage.counts"]; json.dump(s,open(p,"w"))
P
  want "M8 multi-verb key missing from _argorder.json" 1
  mk; rm "$bin/m/_argorder.json"; want "M9 spec file absent is rc 2" 2
  mk; echo '{' > "$bin/m/_argorder.json"; want "M10 unparseable spec" 1
  [ "$fail" = 0 ] && echo "PROVE-FAILURE OK" || echo "PROVE-FAILURE BAD"; exit "$fail"
fi
exec "$bin/i18nargorder" "$@"
