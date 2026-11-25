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

func TestCognateAllowlist(t *testing.T) {
	m := fx()
	m["ru"]["a"] = "Inbox"
	if !kinds(FromMaps(m).AuditCognates(map[string]bool{"c": true}, nil))["untranslated"] {
		t.Fatal("unlisted identical string must still fail")
	}
	ok := Cognates{"ru": {"a": "spelled identically in the target language"}}
	if f := FromMaps(m).AuditCognates(map[string]bool{"c": true}, ok); len(f) != 0 {
		t.Fatalf("listed cognate with reason must pass: %v", f)
	}
	if !kinds(FromMaps(m).AuditCognates(map[string]bool{"c": true}, Cognates{"ru": {"a": " "}}))["cognate-without-reason"] {
		t.Fatal("entry without a reason must be a finding")
	}
	other := Cognates{"de": {"a": "only excuses de"}}
	if !kinds(FromMaps(m).AuditCognates(map[string]bool{"c": true}, other))["untranslated"] {
		t.Fatal("an entry for another language must not excuse ru")
	}
	m["ru"]["b"] = "Score %d"
	if !kinds(FromMaps(m).AuditCognates(map[string]bool{"c": true}, Cognates{"ru": {"b": "x reason"}}))["placeholder"] {
		t.Fatal("a cognate entry must not mask a placeholder defect")
	}
}
