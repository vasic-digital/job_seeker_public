package console

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// maxIntakeForm bounds the whole request body: the text field is capped at 200,000 characters by providers.ManualPosting, URL-encoding and the other
// fields add overhead, and anything larger is refused unread.
const maxIntakeForm = 1 << 20

// intakePage renders the paste form (JSK-T200, FR-086). The pasted text is never echoed back, not even on a refusal.
func (c *Console) intakePage(g *gin.Context, status int, code, urlV, titleV, companyV string) {
	p := c.newPage(g, "intake.title", "/intake")
	p.Error = code
	p.Intake = &intakeView{URL: urlV, Title: titleV, Company: companyV}
	c.render(g, status, "intake", p)
}

type intakeView struct{ URL, Title, Company string }

func (c *Console) intakeGet(g *gin.Context) { c.intakePage(g, http.StatusOK, "", "", "", "") }

// intakePost is the console twin of `jsctl intake`: the same providers.ManualPosting validation and ingest.Runner.IngestManual store, so the two surfaces
// cannot disagree about what is refused. No network request is made. A refusal is 400 "validation" and stores nothing; success redirects (PRG) to the
// Position the Listing resolved to, or to the Listing itself when it has none.
func (c *Console) intakePost(g *gin.Context) {
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, maxIntakeForm)
	in := providers.ManualInput{URL: g.PostForm("url"), Title: g.PostForm("title"), Company: g.PostForm("company"), Text: g.PostForm("text")}
	if c.Keys == nil {
		c.intakePage(g, http.StatusInternalServerError, "internal", "", "", "")
		return
	}
	now := c.Now()
	if _, err := providers.ManualPosting(in, now); err != nil { // validate before touching the store
		c.intakePage(g, http.StatusBadRequest, "validation", in.URL, in.Title, in.Company)
		return
	}
	r := ingest.NewManual(ingest.RunnerOptions{DB: c.DB, Keys: c.Keys, Now: c.Now, Positions: &position.Resolver{P: position.DefaultParams(), Keys: c.Keys}})
	id, _, err := r.IngestManual(g.Request.Context(), in)
	if errors.Is(err, providers.ErrManualInput) {
		c.intakePage(g, http.StatusBadRequest, "validation", in.URL, in.Title, in.Company)
		return
	}
	if err != nil {
		c.intakePage(g, http.StatusInternalServerError, "internal", "", "", "")
		return
	}
	var pos sql.NullInt64
	_ = c.DB.Read().QueryRowContext(g.Request.Context(), `SELECT position_id FROM posting WHERE id=?`, id).Scan(&pos)
	if pos.Valid {
		g.Redirect(http.StatusSeeOther, fmt.Sprintf("/positions/%d", pos.Int64))
		return
	}
	g.Redirect(http.StatusSeeOther, fmt.Sprintf("/listings/%d", id))
}
