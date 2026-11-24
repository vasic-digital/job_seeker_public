package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestExtRouteServesListingGETOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	RegisterExt(e, func() any { return []map[string]string{{"kind": "eligibility"}} })
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/api/ext", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"eligibility"`) || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("%d %s %v", w.Code, w.Body, w.Header())
	}
	w = httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("POST", "/api/ext", nil))
	if w.Code == http.StatusOK {
		t.Fatal("POST accepted")
	}
}
