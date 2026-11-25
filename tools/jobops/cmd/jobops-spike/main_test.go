package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func runWith(t *testing.T, args ...string) int {
	t.Helper()
	oldArgs, oldFS := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = oldArgs, oldFS }()
	flag.CommandLine = flag.NewFlagSet("jobops-spike", flag.ContinueOnError)
	os.Args = append([]string{"jobops-spike"}, args...)
	return run()
}

func TestShapeRecordsKeysAndTypesNeverValues(t *testing.T) {
	var v any
	_ = json.Unmarshal([]byte(`{"b":"secret-value","a":[{"n":1,"ok":true,"x":null}],"e":[]}`), &v)
	got := shape(v)
	if got != `{a:[{n:num,ok:bool,x:null}],b:str,e:[]}` {
		t.Fatalf("shape = %s", got)
	}
	if strings.Contains(got, "secret-value") {
		t.Fatal("a value leaked into the shape")
	}
	if shape(struct{}{}) != "?" {
		t.Error("unknown type must be marked ?")
	}
}

func TestNonLoopbackOrMalformedBaseIsCannotDetermine(t *testing.T) {
	for _, b := range []string{"", "https://127.0.0.1:1", "http://192.0.2.1:80", "http://user@127.0.0.1:1", "http://example.invalid", "://x"} {
		if rc := runWith(t, "-base", b); rc != 2 {
			t.Errorf("-base %q: rc %d, want 2 (loopback exception only)", b, rc)
		}
	}
}

func TestUnreachableSidecarIsCannotDetermine(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // the port is now closed: a real connection refusal
	if rc := runWith(t, "-base", base); rc != 2 {
		t.Fatalf("rc %d, want 2", rc)
	}
}

// The handler below is a test double of the jobops sidecar. It proves ONLY this probe's own logic (which calls it makes, in
// what order, and how it scores them); it is never evidence about the real jobops API (11.4.289).
func double(t *testing.T, importStatus int) (*httptest.Server, *[]string) {
	var calls []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/manual-jobs/import":
			w.WriteHeader(importStatus)
			_, _ = io.WriteString(w, `{"data":{"id":"job1"}}`)
		default:
			_, _ = fmt.Fprint(w, `{"data":{}}`)
		}
	})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s, &calls
}

func TestAllExpectationsMetCallsOnlyTheContractedEndpoints(t *testing.T) {
	s, calls := double(t, 200)
	if rc := runWith(t, "-base", s.URL); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	joined := strings.Join(*calls, "\n")
	for _, want := range []string{"GET /health", "GET /api/jobs", "POST /api/manual-jobs/import", "GET /api/jobs/job1/events", "GET /api/jobs/job1/tasks"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing call %q in\n%s", want, joined)
		}
	}
	for _, c := range *calls {
		path := strings.SplitN(c, " ", 2)[1]
		for _, forbidden := range []string{"/apply", "/api/pipeline/run", "/search-plan", "/rescore", "/pdf", "/api/manual-jobs/fetch", "/api/manual-jobs/infer"} {
			if path == forbidden || strings.HasSuffix(path, forbidden) {
				t.Errorf("the probe called %q without being asked to", c)
			}
		}
	}
}

func TestApplyOnlyWithTheThrowawayFlagAndOnlyOnItsOwnRow(t *testing.T) {
	s, calls := double(t, 200)
	if rc := runWith(t, "-base", s.URL, "-apply-throwaway"); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	n := 0
	for _, c := range *calls {
		if strings.HasSuffix(c, "/apply") {
			n++
			if c != "POST /api/jobs/job1/apply" {
				t.Errorf("apply on %q, want the throwaway row", c)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d apply calls, want exactly 1", n)
	}
}

func TestUnexpectedStatusFailsTheRun(t *testing.T) {
	s, _ := double(t, 500)
	if rc := runWith(t, "-base", s.URL); rc != 1 {
		t.Fatalf("a 500 on import must make the run rc 1, got %d", rc)
	}
}
