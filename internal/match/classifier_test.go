package match

import "testing"

func rules(t *testing.T) *Rules {
	t.Helper()
	r, err := LoadRules(rulesDir())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestClassifySpecScenarios(t *testing.T) {
	r := rules(t)
	cases := []struct {
		name   string
		p      Posting
		status Status
		area   string
	}{
		{"android-tv", Posting{Title: "Senior Android TV Engineer (Leanback)", Body: "Build ExoPlayer based apps for Google TV."}, StatusMapped, "android-tv"},
		{"data eng", Posting{Title: "Data Engineer", Body: "Spark, Airflow and SQL pipelines."}, StatusUnmapped, ""},
		{"devops", Posting{Title: "DevOps Engineer", Body: "Terraform, Docker, AWS."}, StatusUnmapped, ""},
		{"ios only", Posting{Title: "iOS Developer", Body: "Swift and UIKit."}, StatusUnmapped, ""},
		{"golang", Posting{Title: "Senior Golang Backend Engineer", Body: "gRPC, Kubernetes, PostgreSQL."}, StatusMapped, "go"},
		{"flutter beats mobile", Posting{Title: "Flutter Mobile Developer", Body: "Dart, Riverpod, iOS and Android."}, StatusMapped, "flutter"},
		{"aosp beats android", Posting{Title: "Android Platform Engineer (AOSP)", Body: "HAL, Binder, SELinux, Android framework."}, StatusMapped, "android-aosp"},
		{"ru android", Posting{Title: "Андроид разработчик", Body: "Kotlin, Jetpack Compose, Coroutines, Retrofit."}, StatusMapped, "android"},
		{"sr latin", Posting{Title: "Android programer", Body: "Kotlin, Jetpack Compose, Room, Retrofit."}, StatusMapped, "android"},
		{"recruiter excluded", Posting{Title: "Technical Recruiter", Body: "Android, Kotlin, Flutter, Go developers wanted."}, StatusUnmapped, ""},
		{"ru recruiter", Posting{Title: "Рекрутер", Body: "Android Kotlin"}, StatusUnmapped, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := r.Classify(c.p)
			if m.Status != c.status || m.AreaID != c.area {
				t.Fatalf("got status=%s area=%q s1=%v top=%v", m.Status, m.AreaID, m.S1, m.Top3)
			}
			if m.RulesetSHA == "" {
				t.Fatal("ruleset_sha missing")
			}
		})
	}
}

func TestUnmappedNeverForcedButShowsTop3(t *testing.T) {
	m := rules(t).Classify(Posting{Title: "Mobile QA", Body: "Android iOS testing"})
	if m.AreaID != "" || m.Status == StatusMapped {
		t.Fatalf("must not be mapped: %+v", m)
	}
	if len(m.Top3) > 3 {
		t.Fatal("top3 bound")
	}
}

func TestThresholdsAreData(t *testing.T) {
	r := rules(t)
	p := Posting{Title: "Flutter Developer"} // Flutter title: 6x2 = 12 -> mapped
	if m := r.Classify(p); m.Status != StatusMapped {
		t.Fatalf("expected mapped %+v", m)
	}
	r2 := *r
	r2.Thresholds.HighS1 = 100 // raising the data threshold changes the outcome with no code change
	if m := r2.Classify(p); m.Status != StatusAmbiguous || m.AreaID != "" {
		t.Fatalf("threshold must be data-driven: %+v", m)
	}
}

func TestAIOverGoOnlyWhenTitleOrTagHit(t *testing.T) {
	r := rules(t)
	body := Posting{Title: "Backend Engineer", Body: "Golang gRPC Kubernetes, some LLM and RAG experience helps."}
	if m := r.Classify(body); m.AreaID == "ai-engineering" {
		t.Fatalf("AI tokens only in body must not out-rank go: %+v", m)
	}
	title := Posting{Title: "LLM Engineer (RAG)", Body: "Golang gRPC Kubernetes."}
	if m := r.Classify(title); m.AreaID != "ai-engineering" {
		t.Fatalf("AI in title must win over go: %+v", m)
	}
}

func TestClassifierDeterministicAndExplainable(t *testing.T) {
	r := rules(t)
	p := Posting{Title: "Senior Android TV Engineer (Leanback)", Body: "ExoPlayer, Media3, HLS"}
	a, b := r.Classify(p), r.Classify(p)
	if a.Canonical() != b.Canonical() {
		t.Fatal("not deterministic")
	}
	if len(a.Hits) == 0 || a.Hits[0].RuleID == "" || a.Hits[0].Span[1] <= a.Hits[0].Span[0] {
		t.Fatalf("explain payload missing rule ids / spans: %+v", a.Hits)
	}
}

func TestNoNewAreasAndTokensSupersetOfCareerSnapshot(t *testing.T) {
	r := rules(t)
	if len(r.Areas) != 10 {
		t.Fatalf("exactly the 10 areas, got %d", len(r.Areas))
	}
	missing, err := r.SnapshotDrift(rulesDir() + "/career_snapshot.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("rule tokens must be a superset of the career snapshot; missing: %v", missing)
	}
}

func TestSnapshotDriftDetected(t *testing.T) {
	r := rules(t)
	d := t.TempDir() + "/s.yaml"
	_ = writeFile(d, "areas:\n  go:\n    - \"zzz-nonexistent-token\"\nskills: []\n")
	missing, err := r.SnapshotDrift(d)
	if err != nil || len(missing) != 1 {
		t.Fatalf("drift must be reported, got %v %v", missing, err)
	}
}

func TestRulesetSHAChangesWithRules(t *testing.T) {
	d := t.TempDir()
	for _, f := range []string{"area_rules.yaml", "score.yaml"} {
		b, _ := readFile("../../rules/" + f)
		_ = writeFile(d+"/"+f, string(b))
	}
	a, _ := LoadRules(d)
	b, _ := readFile(d + "/score.yaml")
	_ = writeFile(d+"/score.yaml", string(b)+"\n# change\n")
	c, _ := LoadRules(d)
	if a.SHA == c.SHA || a.SHA == "" {
		t.Fatal("ruleset sha must reflect both files")
	}
}
