package templates

import (
	"errors"
	"strings"
	"testing"
)

// JSK-T160 (F-05, F-06): deterministic typed slots and the placeholder-leak guard. Synthetic names only.

func TestFillSubstitutesTypedSlotsOnly(t *testing.T) {
	v := SlotValues{Company: "Acme Robotics", Role: "Senior Go Engineer", Date: "2026-11-24"}
	got, err := Fill("{{date}}\n\nDear {{company}} team,\nI am applying for {{role}} at {{company}}.", v)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-11-24\n\nDear Acme Robotics team,\nI am applying for Senior Go Engineer at Acme Robotics." {
		t.Fatalf("%q", got)
	}
	for _, bad := range []string{"{{salary}}", "{{ company }}x{{", "{{company"} {
		if out, err := Fill("Hello "+bad, v); err == nil && strings.Contains(out, "{{") {
			t.Errorf("%q leaked through Fill: %q", bad, out)
		}
	}
	if _, err := Fill("{{salary}}", v); !errors.Is(err, ErrSlotUnknown) {
		t.Fatalf("unknown slot: %v", err)
	}
}

func TestFillRefusesUnusableValues(t *testing.T) {
	ok := SlotValues{Company: "Acme", Role: "Engineer", Date: "2026-11-24"}
	for name, v := range map[string]SlotValues{
		"empty company":          {Role: "Engineer", Date: "2026-11-24"},
		"empty role":             {Company: "Acme", Date: "2026-11-24"},
		"bad date":               {Company: "Acme", Role: "Engineer", Date: "24.11.2026"},
		"placeholder in company": {Company: "{{company}}", Role: "Engineer", Date: "2026-11-24"},
		"bracket placeholder":    {Company: "[Company]", Role: "Engineer", Date: "2026-11-24"},
		"TODO company":           {Company: "TODO", Role: "Engineer", Date: "2026-11-24"},
		"control char":           {Company: "Ac\x00me", Role: "Engineer", Date: "2026-11-24"},
		"too long":               {Company: strings.Repeat("A", 300), Role: "Engineer", Date: "2026-11-24"},
	} {
		if _, err := Fill("{{company}} {{role}} {{date}}", v); !errors.Is(err, ErrSlotValue) {
			t.Errorf("%s: err %v want ErrSlotValue", name, err)
		}
	}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLeaksGoldenBadAndGoldenGood(t *testing.T) {
	v := SlotValues{Company: "Acme Robotics", Role: "Engineer", Date: "2026-11-24"}
	others := []string{"Borealis Labs", "Cirrus"}
	good := "Dear Acme Robotics team, I would like to join as an engineer."
	if l := Leaks(good, v, others); len(l) != 0 {
		t.Fatalf("clean text flagged: %v", l)
	}
	cases := map[string]string{
		"placeholder":   "Dear {{company}} team, hello",
		"company_count": "Dear team, hello", // company absent
		"other_company": "Dear Acme Robotics team, and Borealis Labs too",
	}
	for want, txt := range cases {
		found := false
		for _, l := range Leaks(txt, v, others) {
			found = found || l == want
		}
		if !found {
			t.Errorf("%q: %q not reported (%v)", txt, want, Leaks(txt, v, others))
		}
	}
	for _, txt := range []string{"Dear Acme Robotics, [Company] hello", "Dear Acme Robotics, <role>", "Dear Acme Robotics TODO", "Dear Acme Robotics XXX", "Dear Acme Robotics, <date>", "Dear Acme Robotics, lorem ipsum"} {
		if l := Leaks(txt, v, others); len(l) == 0 || l[0] != "placeholder" {
			t.Errorf("%q not flagged as placeholder: %v", txt, l)
		}
	}
	if l := Leaks("Acme Robotics and Acme Robotics again", v, nil); len(l) != 1 || l[0] != "company_count" {
		t.Errorf("exactly once: %v", l)
	}
	if l := Leaks("Dear Acme Robotics, I know TODOs are normal in software", v, nil); len(l) != 0 {
		t.Errorf("a word that merely contains TODO is not a placeholder: %v", l)
	}
}

func TestLeaksIsCaseInsensitive(t *testing.T) {
	v := SlotValues{Company: "Acme Robotics", Role: "Engineer", Date: "2026-11-24"}
	has := func(l []string, w string) bool {
		for _, x := range l {
			if x == w {
				return true
			}
		}
		return false
	}
	if l := Leaks("Dear ACME ROBOTICS team and BOREALIS LABS too", v, []string{"Borealis Labs"}); !has(l, "other_company") {
		t.Errorf("an upper-cased other company must still be a leak: %v", l)
	}
	if l := Leaks("Dear Acme Robotics team, and acme robotics again", v, nil); !has(l, "company_count") {
		t.Errorf("a differently cased second mention must count: %v", l)
	}
	if l := Leaks("Dear ACME ROBOTICS team", v, nil); len(l) != 0 {
		t.Errorf("one mention in any case is clean: %v", l)
	}
}
