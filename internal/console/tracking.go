package console

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

// dueFollowUps lists the applications whose follow-up date has arrived (tracker.DueFollowUps) for the inbox.
func (c *Console) dueFollowUps(ctx context.Context) []dueView {
	if c.Tracker == nil {
		return nil
	}
	ds, err := c.Tracker.DueFollowUps(ctx, c.Now())
	if err != nil {
		return nil
	}
	var out []dueView
	for _, d := range ds {
		v := dueView{AppID: d.ApplicationID, Status: d.LatestStatus, At: d.FollowUpAt.Format("2006-01-02")}
		_ = c.DB.Read().QueryRowContext(ctx, `SELECT p.title, coalesce(p.company,'') FROM posting p WHERE p.id=?`, d.PostingID).Scan(&v.Title, &v.Company)
		out = append(out, v)
	}
	return out
}

func (c *Console) trackerErr(g *gin.Context, err error) {
	st, code := http.StatusInternalServerError, "internal"
	switch {
	case errors.Is(err, tracker.ErrNoApp):
		st, code = http.StatusNotFound, "not_found"
	case errors.Is(err, tracker.ErrBadStatus), errors.Is(err, tracker.ErrBadSource), errors.Is(err, tracker.ErrActor):
		st, code = http.StatusBadRequest, "validation"
	case errors.Is(err, tracker.ErrConflict), errors.Is(err, tracker.ErrNotTrackable), errors.Is(err, tracker.ErrNoChange):
		st, code = http.StatusConflict, "illegal_transition"
	}
	c.renderTracking(g, st, code)
}

func (c *Console) trackingPage(g *gin.Context) { c.renderTracking(g, http.StatusOK, "") }

func (c *Console) renderTracking(g *gin.Context, status int, errCode string) {
	id, ok := pid(g)
	if !ok {
		return
	}
	if c.Tracker == nil {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	ctx := g.Request.Context()
	a, _, err := c.loadApp(ctx, id)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	tl, err := c.Tracker.Timeline(ctx, id)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	p := c.newPage(g, "tracking.title", "/applications")
	p.A, p.Timeline, p.Statuses, p.Error = a, tl, tracker.Statuses, errCode
	var fu *string
	_ = c.DB.Read().QueryRowContext(ctx, `SELECT follow_up_at FROM application WHERE id=?`, id).Scan(&fu)
	if fu != nil && len(*fu) >= 10 {
		p.FollowUp = (*fu)[:10]
	}
	if len(tl) > 0 {
		p.Latest = tl[len(tl)-1].ID
	}
	c.render(g, status, "tracking", p)
}

// trackingRecord appends a manual status to the timeline. The form carries the newest row id the page showed, so a
// concurrent edit fails with a conflict instead of a lost update (FR-035).
func (c *Console) trackingRecord(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	if c.Tracker == nil {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	in := tracker.Input{ApplicationID: id, Status: g.PostForm("status"), Note: strings.TrimSpace(g.PostForm("note")), Actor: "operator"}
	if v := g.PostForm("latest"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			c.renderTracking(g, http.StatusBadRequest, "validation")
			return
		}
		in.ExpectLatest = &n
	}
	if _, err := c.Tracker.Record(g.Request.Context(), in); err != nil {
		c.trackerErr(g, err)
		return
	}
	g.Redirect(http.StatusSeeOther, "/applications/"+strconv.FormatInt(id, 10)+"/tracking")
}

// followUp sets the follow-up date (YYYY-MM-DD) or clears it when the field is empty.
func (c *Console) followUp(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	if c.Tracker == nil {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	var at *time.Time
	if d := strings.TrimSpace(g.PostForm("date")); d != "" {
		t, err := tracker.ParseDate(d)
		if err != nil {
			c.renderTracking(g, http.StatusBadRequest, "validation")
			return
		}
		at = &t
	}
	if err := c.Tracker.SetFollowUp(g.Request.Context(), id, at, "operator"); err != nil {
		c.trackerErr(g, err)
		return
	}
	g.Redirect(http.StatusSeeOther, "/applications/"+strconv.FormatInt(id, 10)+"/tracking")
}
