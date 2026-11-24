package gold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the TOOLING logic only (parsing, hashing, thresholds). Fixtures here are not gold data
// and are never counted as evidence for SC-003; the gold run is gates/verify_gold.sh on the real corpus.

func TestExtractManifestRoundTripAndTamper(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "remoteok"), 0o755)
	os.WriteFile(filepath.Join(d, "remoteok", "20261124T102808Z-p1.json"),
		[]byte(`[{"legal":"notice"},{"id":"7","position":"Go Engineer","company":"X","tags":["go"],"description":"<p>Build services</p>"}]`), 0o600)
	items, skipped, err := ExtractSnapshots(d)
	if err != nil || len(items) != 1 || skipped != 1 || items[0].FetchDate != "2026-11-24" {
		t.Fatalf("extract: %v %v %d", items, err, skipped)
	}
	m := Manifest(items)
	if err := VerifyManifest(items, m); err != nil {
		t.Fatal(err)
	}
	items[0].Title = "Other"
	if err := WriteCorpus(d, items); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCorpus(d); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("tampered corpus accepted: %v", err)
	}
}

func TestVerdictThresholds(t *testing.T) {
	rep := Report{Areas: map[string]AreaStat{"go": {Gold: 40, Predicted: 40, TP: 30, Precision: 0.75, Recall: 0.75}, "flutter": {Gold: 3}}, NonEng: 60, NonEngUnmapped: 59}
	f, p := Verdict(rep, nil)
	if len(f) != 2 || len(p) != 1 { // precision<0.90 on go, non-eng<100%; flutter PARTIAL
		t.Fatalf("findings %v partials %v", f, p)
	}
	base := Report{Areas: map[string]AreaStat{"flutter": {Precision: 1, Recall: 1}}}
	rep.Areas["flutter"] = AreaStat{Gold: 3, Precision: 0.5, Recall: 1}
	if f, _ := Verdict(rep, &base); len(f) != 3 {
		t.Fatalf("regression not caught: %v", f)
	}
}

func TestExpected(t *testing.T) {
	if (Label{RoleClass: "non-engineering", Area: "go"}).Expected() != "" || (Label{RoleClass: "engineering", Area: "none"}).Expected() != "" ||
		(Label{RoleClass: "engineering", Area: "go"}).Expected() != "go" {
		t.Fatal("expected-area mapping wrong")
	}
}
