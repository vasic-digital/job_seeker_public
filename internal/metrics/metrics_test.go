package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnknownLabelValuesCollapseToOther(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("js_x_total", "x", Label{"result", []string{"ok", "fail"}})
	c.Inc("ok")
	c.Inc("203.0.113.9") // a client address must never become a label value
	c.Inc("/applications/7/approve")
	out := r.Render()
	if strings.Contains(out, "203.0.113.9") || strings.Contains(out, "applications") {
		t.Fatalf("a personal value leaked into the exposition:\n%s", out)
	}
	if !strings.Contains(out, `js_x_total{result="other"} 2`) || !strings.Contains(out, `js_x_total{result="ok"} 1`) {
		t.Fatalf("unexpected exposition:\n%s", out)
	}
}

func TestHandlerServesOnlyMetrics(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("js_y_total", "y").Inc()
	h := r.Handler()
	for path, want := range map[string]int{"/metrics": 200, "/": 404, "/healthz": 404, "/metrics/x": 404} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s: %d want %d", path, w.Code, want)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/metrics", nil))
	if w.Code != 405 {
		t.Errorf("POST: %d", w.Code)
	}
}

func TestNamesAreEnforced(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("bad name must panic")
		}
	}()
	NewRegistry().NewCounter("requests", "x")
}
