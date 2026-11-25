package exttest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

func get(t *testing.T, c *egress.Client, url string) (int, string, error) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := c.HTTP().Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func TestClientServesAllowedHostsThroughTheRealPolicy(t *testing.T) {
	c := Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("host=" + r.Host)) }), "jobs.example.invalid", "api.example.invalid")
	for _, h := range []string{"jobs.example.invalid", "api.example.invalid"} {
		code, body, err := get(t, c, "https://"+h+"/x")
		if err != nil || code != 200 || body != "host="+h {
			t.Errorf("%s: %d %q %v", h, code, body, err)
		}
	}
}

func TestClientRefusesWhatThePolicyForbids(t *testing.T) {
	c := Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("must not be reached")) }), "jobs.example.invalid")
	for _, u := range []string{"https://other.example.invalid/", "http://jobs.example.invalid/", "https://jobs.example.invalid:8443/", "https://user@jobs.example.invalid/"} {
		if code, body, err := get(t, c, u); !errors.Is(err, egress.ErrDenied) {
			t.Errorf("%s: got %d %q err=%v, want ErrDenied", u, code, body, err)
		}
	}
}

func TestClientHandlerIsReachedPerRequestAndHeadersPassThrough(t *testing.T) {
	n := 0
	c := Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("X-Seen-Path", r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}), "jobs.example.invalid")
	req, _ := http.NewRequest(http.MethodGet, "https://jobs.example.invalid/a/b", nil)
	resp, err := c.HTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot || resp.Header.Get("X-Seen-Path") != "/a/b" || n != 1 {
		t.Fatalf("status %d header %q handler calls %d", resp.StatusCode, resp.Header.Get("X-Seen-Path"), n)
	}
}
