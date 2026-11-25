package console

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/position"
)

// JSK-T209 (FR-091): merge candidate queue, and merge/split corrections after the fact. None of these changes outbound state, so step-up is not
// required; every one is audited by the position package, and every one is refused while an application on the Position is live_approved.

// positionStatus maps a position-layer error to the closed HTTP code set.
func positionStatus(err error) (int, string) {
	switch {
	case errors.Is(err, position.ErrPositionBusy):
		return http.StatusConflict, "position_busy"
	case errors.Is(err, position.ErrCannotLink), errors.Is(err, position.ErrBothLocked):
		return http.StatusConflict, "merge_blocked"
	case errors.Is(err, position.ErrNotOpen), errors.Is(err, position.ErrAlreadyOne), errors.Is(err, position.ErrNothingToSplit):
		return http.StatusConflict, "illegal_transition"
	case errors.Is(err, position.ErrNoCandidate):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, position.ErrBadReason), errors.Is(err, position.ErrBadAction), errors.Is(err, position.ErrBadMerge):
		return http.StatusBadRequest, "validation"
	}
	return http.StatusInternalServerError, "internal"
}

func (c *Console) mergesPage(g *gin.Context) { c.renderMerges(g, http.StatusOK, "") }

func (c *Console) renderMerges(g *gin.Context, status int, code string) {
	v, err := position.OpenCandidates(g.Request.Context(), c.DB.Read(), 100)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	p := c.newPage(g, "queue.merges.title", "/queue")
	p.Merges, p.Error = v, code
	c.render(g, status, "merges", p)
}

// reasonOf returns the operator's reason code or a fixed default; position.reReason still validates it.
func reasonOf(g *gin.Context, def string) string {
	if r := g.PostForm("reason"); r != "" {
		return r
	}
	return def
}

func (c *Console) mergeDecide(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	var action string
	switch g.Param("action") {
	case "merge":
		action = position.ActMerge
	case "keep-apart":
		action = position.ActKeepApart
	case "skip":
		action = position.ActSkip
	default:
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		return position.DecideCandidate(ctx, tx, "operator", id, action, reasonOf(g, "queue_review"))
	})
	if err != nil {
		st, code := positionStatus(err)
		c.renderMerges(g, st, code)
		return
	}
	g.Redirect(http.StatusSeeOther, "/queue/merges")
}

// positionMerge is the correction `POST /positions/{id}/merge {into, reason}`: Position {id} is absorbed into {into}.
func (c *Console) positionMerge(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	into, err := strconv.ParseInt(g.PostForm("into"), 10, 64)
	if err != nil || into <= 0 {
		c.renderMerges(g, http.StatusBadRequest, "validation")
		return
	}
	ctx := g.Request.Context()
	err = c.DB.Write(ctx, func(tx *sql.Tx) error {
		return position.MergeHuman(ctx, tx, "operator", into, id, reasonOf(g, "operator_correction"))
	})
	if err != nil {
		st, code := positionStatus(err)
		c.renderMerges(g, st, code)
		return
	}
	g.Redirect(http.StatusSeeOther, "/queue/merges")
}

// listingSplit is `POST /listings/{id}/split {reason}`.
func (c *Console) listingSplit(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		_, e := position.Split(ctx, tx, "operator", id, reasonOf(g, "operator_correction"), "")
		return e
	})
	if err != nil {
		st, code := positionStatus(err)
		c.renderMerges(g, st, code)
		return
	}
	g.Redirect(http.StatusSeeOther, "/queue/merges")
}
