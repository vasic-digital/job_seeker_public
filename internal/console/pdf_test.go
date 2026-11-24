package console

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/privacy"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

func leftovers(t *testing.T) int {
	d, err := privacy.TmpfsDir()
	if err != nil {
		t.Skip("no tmpfs on this host")
	}
	m, _ := filepath.Glob(filepath.Join(d, "jobseeker-*"))
	return len(m)
}

// JSK-T094: the PDF viewer serves from a tmpfs-staged copy (range requests work) and leaves nothing behind.
func TestServePDFStagesOnTmpfsServesRangesAndCleansUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte("%PDF-1.4 synthetic persona file for the viewer test")
	p := filepath.Join(t.TempDir(), "cv-go-en.pdf")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	f := templates.File{Name: "cv-go-en.pdf", Path: p, SHA256: sha}
	before := leftovers(t)
	c := &Console{}

	rec := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(rec)
	g.Request = httptest.NewRequest(http.MethodGet, "/f", nil)
	g.Request.Header.Set("Range", "bytes=0-3")
	if err := c.servePDF(g, f, sha); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "%PDF" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-SHA256") != sha {
		t.Fatalf("range response wrong: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if leftovers(t) != before {
		t.Fatal("a staged temp directory was left on tmpfs")
	}

	// a differing pin is refused before anything is written or staged
	rec = httptest.NewRecorder()
	g, _ = gin.CreateTestContext(rec)
	g.Request = httptest.NewRequest(http.MethodGet, "/f", nil)
	if err := c.servePDF(g, f, "00"+sha[2:]); err == nil || rec.Body.Len() != 0 || leftovers(t) != before {
		t.Fatalf("hash mismatch was not refused cleanly: %v", err)
	}
}
