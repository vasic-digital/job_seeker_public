package i18n

import "testing"

func fx() map[string]map[string]string {
	return map[string]map[string]string{
		"en": {"a": "Inbox", "b": "Score %d of %d", "c": "Job Seeker"},
		"ru": {"a": "Входящие", "b": "Оценка %d из %d", "c": "Job Seeker"},
	}
}

func kinds(fs []Finding) map[string]bool {
	o := map[string]bool{}
	for _, f := range fs {
		o[f.Kind] = true
	}
	return o
}

func TestAuditCleanControl(t *testing.T) {
	if f := FromMaps(fx()).Audit(map[string]bool{"c": true}); len(f) != 0 {
		t.Fatalf("clean control must have no findings: %v", f)
	}
}

func TestAuditCatchesEachDefect(t *testing.T) {
	inv := map[string]bool{"c": true}
	cases := map[string]func(m map[string]map[string]string){
		"missing":      func(m map[string]map[string]string) { delete(m["ru"], "a") },
		"untranslated": func(m map[string]map[string]string) { m["ru"]["a"] = "Inbox" },
		"empty":        func(m map[string]map[string]string) { m["ru"]["a"] = "" },
		"placeholder":  func(m map[string]map[string]string) { m["ru"]["b"] = "Оценка %d" },
		"extra":        func(m map[string]map[string]string) { m["ru"]["zz"] = "x" },
	}
	for want, mut := range cases {
		m := fx()
		mut(m)
		if !kinds(FromMaps(m).Audit(inv))[want] {
			t.Errorf("mutation %q not caught", want)
		}
	}
	m := fx()
	if len(FromMaps(m).Audit(nil)) == 0 {
		t.Error("an undeclared identical brand string must be untranslated")
	}
}

func TestRequiredAndDir(t *testing.T) {
	if len(Required) != 15 || Required[0] != Source {
		t.Fatal("15 required languages, en first")
	}
	for _, l := range Required {
		if (Dir(l) == "rtl") != (l == "ar" || l == "fa") {
			t.Fatalf("dir %s", l)
		}
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Pending()) == 0 && len(c.Shipped()) != 15 {
		t.Fatal("pending/shipped inconsistent")
	}
}
