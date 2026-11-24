// Command jobops-spike is the JSK-T097 measuring probe: it exercises the contract's read calls (plus text-only
// import and, only with -apply-throwaway, ONE apply against the throwaway row it created itself) against a
// live jobops sidecar and prints the status code and the JSON SHAPE (key sets and types, never values) of
// every response, so the API's actual shape is recorded instead of assumed (spec: "jobops' API is untyped").
// Stdlib only; never calls pipeline/run, search-plan, rescore, pdf, manual-jobs/fetch or manual-jobs/infer.
// Exit codes: 0 all calls answered as expected, 1 an expectation failed, 2 could not determine (unreachable).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

func main() { os.Exit(run()) }

func shape(v any) string {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+":"+shape(t[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		if len(t) == 0 {
			return "[]"
		}
		return "[" + shape(t[0]) + "]"
	case string:
		return "str"
	case float64:
		return "num"
	case bool:
		return "bool"
	case nil:
		return "null"
	}
	return "?"
}

type result struct {
	status int
	body   any
}

func run() int {
	base := flag.String("base", "", "sidecar base URL (loopback only)")
	apply := flag.Bool("apply-throwaway", false, "also POST /api/jobs/:id/apply on the throwaway row this run created")
	flag.Parse()
	u, perr := url.Parse(*base)
	if perr != nil || u.Scheme != "http" || u.User != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		fmt.Fprintln(os.Stderr, "jobops-spike: COULD NOT DETERMINE: -base must be http://127.0.0.1:PORT (loopback exception only)")
		return 2
	}
	cl := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	token := ""
	do := func(method, path string, in any) (result, error) {
		var rd io.Reader
		if in != nil {
			b, _ := json.Marshal(in)
			rd = bytes.NewReader(b)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, *base+path, rd)
		if err != nil {
			return result{}, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := cl.Do(req)
		if err != nil {
			return result{}, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		var v any
		_ = json.Unmarshal(raw, &v)
		return result{resp.StatusCode, v}, nil
	}
	fails := 0
	show := func(name string, r result, err error, wantStatus int) result {
		if err != nil {
			fmt.Printf("%-34s ERROR %v\n", name, err)
			fails++
			return r
		}
		verdict := "ok"
		if r.status != wantStatus {
			verdict = fmt.Sprintf("UNEXPECTED (want %d)", wantStatus)
			fails++
		}
		fmt.Printf("%-34s %d %s shape=%s\n", name, r.status, verdict, shape(r.body))
		return r
	}
	r, err := do("GET", "/health", nil)
	_ = r
	if err != nil {
		fmt.Fprintf(os.Stderr, "jobops-spike: COULD NOT DETERMINE: %v\n", err)
		return 2
	}
	show("GET /health", r, nil, 200)
	// JWT login with the credentials from the env name JOBSEEKER_JOBOPS_CRED ("user:pass"); value never printed.
	if cred := os.Getenv("JOBSEEKER_JOBOPS_CRED"); cred != "" {
		u, p, _ := strings.Cut(cred, ":")
		lr, lerr := do("POST", "/api/auth/login", map[string]any{"username": u, "password": p})
		show("POST /api/auth/login", lr, lerr, 200)
		if m, ok := lr.body.(map[string]any); ok {
			if d, ok := m["data"].(map[string]any); ok {
				token, _ = d["token"].(string)
			}
		}
		if token == "" {
			fmt.Println("login produced no token (shape above); authenticated calls will 401")
		}
	}
	for _, p := range []string{"/api/app/status", "/api/jobs/revision", "/api/jobs", "/api/pipeline/status", "/api/pipeline/runs"} {
		r, err := do("GET", p, nil)
		show("GET "+p, r, err, 200)
	}
	imp, err := do("POST", "/api/manual-jobs/import", map[string]any{
		"skipTailoring": true,
		"job": map[string]any{
			"source": "jobseeker-spike", "sourceJobId": fmt.Sprintf("spike-%d", time.Now().UnixNano()),
			"title": "SPIKE THROWAWAY ROW", "employer": "Example Org", "jobUrl": "https://example.org/spike",
			"jobDescription": "Throwaway posting text for the jobops spike. No real data.",
		},
	})
	show("POST /api/manual-jobs/import", imp, err, 200)
	id := ""
	if m, ok := imp.body.(map[string]any); ok {
		if d, ok := m["data"].(map[string]any); ok {
			id, _ = d["id"].(string)
		}
	}
	if id == "" {
		fmt.Println("no throwaway id returned; per-job calls skipped")
		fails++
	} else {
		for _, p := range []string{"/api/jobs/" + id, "/api/jobs/" + id + "/events", "/api/jobs/" + id + "/tasks"} {
			r, err := do("GET", p, nil)
			show("GET "+strings.Replace(p, id, ":id", 1), r, err, 200)
		}
		if *apply {
			before, _ := do("GET", "/api/jobs/"+id, nil)
			ar, err := do("POST", "/api/jobs/"+id+"/apply", map[string]any{})
			show("POST /api/jobs/:id/apply", ar, err, 200)
			after, _ := do("GET", "/api/jobs/"+id, nil)
			pick := func(r result, k string) any {
				if m, ok := r.body.(map[string]any); ok {
					if d, ok := m["data"].(map[string]any); ok {
						return d[k]
					}
				}
				return nil
			}
			fmt.Printf("apply effect: status %v -> %v, appliedAt set=%v\n", pick(before, "status"), pick(after, "status"), pick(after, "appliedAt") != nil)
		}
	}
	if fails > 0 {
		fmt.Printf("jobops-spike: %d expectation(s) failed\n", fails)
		return 1
	}
	fmt.Println("jobops-spike: all expectations met")
	return 0
}
