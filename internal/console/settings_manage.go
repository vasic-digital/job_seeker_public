package console

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/audit"
)

// JSK-T207 (FR-090): provider enable/disable, employer policies and the per-Position apply override. Everything here only
// RESTRICTS the apply path except the per-Position "all" override, which loosens a single-provider mode for one Position
// and therefore needs a fresh step-up. Every change is audited. Handoff re-checks policy_sha, so a change made after an
// approval refuses that approval (policy_changed).

type providerRow struct {
	ID                  int64
	Name, Kind, Circuit string
	Enabled             bool
}

type employerRow struct {
	ID                       int64
	Name                     string
	One                      bool
	MaxPer, Window, Cooldown string // "" = not set
	Note                     string
	HasPolicy                bool
}

const maxEmployerRows = 200

func (c *Console) providerRows(ctx context.Context) []providerRow {
	rs, err := c.DB.Read().QueryContext(ctx, `SELECT v.id, v.name, v.enabled, v.circuit_state, coalesce(pp.apply_kind,'unknown') FROM provider v LEFT JOIN provider_profile pp ON pp.provider_id=v.id ORDER BY v.name`)
	if err != nil {
		return nil
	}
	defer rs.Close()
	var out []providerRow
	for rs.Next() {
		var r providerRow
		var en int
		if rs.Scan(&r.ID, &r.Name, &en, &r.Circuit, &r.Kind) == nil {
			r.Enabled = en == 1
			out = append(out, r)
		}
	}
	return out
}

func (c *Console) employerRows(ctx context.Context) []employerRow {
	rs, err := c.DB.Read().QueryContext(ctx, `SELECT e.id, e.display_name, p.employer_id IS NOT NULL, coalesce(p.one_per_position,0), p.max_per_window, p.window_days, p.cooldown_days, coalesce(p.note,'')
		FROM employer e LEFT JOIN employer_policy p ON p.employer_id=e.id ORDER BY (p.employer_id IS NULL), e.display_name LIMIT ?`, maxEmployerRows)
	if err != nil {
		return nil
	}
	defer rs.Close()
	var out []employerRow
	for rs.Next() {
		var r employerRow
		var one int
		var mp, wd, cd sql.NullInt64
		if rs.Scan(&r.ID, &r.Name, &r.HasPolicy, &one, &mp, &wd, &cd, &r.Note) == nil {
			r.One = one == 1
			r.MaxPer, r.Window, r.Cooldown = nstr(mp), nstr(wd), nstr(cd)
			out = append(out, r)
		}
	}
	return out
}

func nstr(n sql.NullInt64) string {
	if !n.Valid {
		return ""
	}
	return strconv.FormatInt(n.Int64, 10)
}

// setProviderEnabled flips provider.enabled for a known provider (404 otherwise) and audits it.
func (c *Console) setProviderEnabled(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	on := g.PostForm("on") == "1"
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM provider WHERE id=?`, id).Scan(&name); err != nil {
			return err
		}
		v := 0
		if on {
			v = 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE provider SET enabled=? WHERE id=?`, v, id); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "provider_enabled_changed", Evidence: map[string]any{"provider": name, "enabled": int64(v)}})
	})
	if err != nil {
		if err == sql.ErrNoRows {
			c.settingsErr(g, http.StatusNotFound, "not_found")
			return
		}
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}

// parseLimit reads a non-negative integer form field; blank means "not set".
func parseLimit(s string) (sql.NullInt64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullInt64{}, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 || n > 3650 {
		return sql.NullInt64{}, false
	}
	return sql.NullInt64{Int64: n, Valid: true}, true
}

// validEmployerLimits: a per-window cap needs a window to count in, and a window without a cap means nothing.
func validEmployerLimits(maxPer, window sql.NullInt64) bool {
	if maxPer.Valid && maxPer.Int64 > 0 && !(window.Valid && window.Int64 > 0) {
		return false
	}
	return true
}

// setEmployerPolicy stores or (when every field is empty) removes an employer's policy.
func (c *Console) setEmployerPolicy(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	maxPer, ok1 := parseLimit(g.PostForm("max_per_window"))
	window, ok2 := parseLimit(g.PostForm("window_days"))
	cool, ok3 := parseLimit(g.PostForm("cooldown_days"))
	note := strings.TrimSpace(g.PostForm("note"))
	if !ok1 || !ok2 || !ok3 || !validEmployerLimits(maxPer, window) || len([]rune(note)) > 200 {
		c.settingsErr(g, http.StatusBadRequest, "validation")
		return
	}
	one := 0
	if g.PostForm("one_per_position") == "1" {
		one = 1
	}
	empty := one == 0 && !maxPer.Valid && !window.Valid && !cool.Valid && note == ""
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		var x int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM employer WHERE id=?`, id).Scan(&x); err != nil {
			return err
		}
		if empty {
			if _, err := tx.ExecContext(ctx, `DELETE FROM employer_policy WHERE employer_id=?`, id); err != nil {
				return err
			}
		} else if _, err := tx.ExecContext(ctx, `INSERT INTO employer_policy(employer_id,one_per_position,max_per_window,window_days,cooldown_days,note) VALUES(?,?,?,?,?,?)
			ON CONFLICT(employer_id) DO UPDATE SET one_per_position=excluded.one_per_position,max_per_window=excluded.max_per_window,window_days=excluded.window_days,cooldown_days=excluded.cooldown_days,note=excluded.note`,
			id, one, nullable(maxPer), nullable(window), nullable(cool), nz(note)); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "employer_policy_changed", Evidence: map[string]any{"employer_id": id, "cleared": empty, "one_per_position": int64(one)}})
	})
	if err != nil {
		if err == sql.ErrNoRows {
			c.settingsErr(g, http.StatusNotFound, "not_found")
			return
		}
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// setPositionOverride sets the per-Position apply override (inherit|all|single|none). single needs one of the Position's own
// listings; all (it loosens a single-provider mode) needs a fresh step-up; a reason is required for everything but inherit.
func (c *Console) setPositionOverride(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	mode := g.PostForm("override")
	reason := strings.TrimSpace(g.PostForm("reason"))
	var listing int64
	switch mode {
	case "inherit":
		reason = ""
	case "all", "single", "none":
		if reason == "" || len([]rune(reason)) > 200 {
			c.positionErr(g, id, http.StatusBadRequest, "validation")
			return
		}
		if mode == "single" {
			n, err := strconv.ParseInt(g.PostForm("listing"), 10, 64)
			if err != nil || n <= 0 {
				c.positionErr(g, id, http.StatusBadRequest, "validation")
				return
			}
			listing = n
		}
	default:
		c.positionErr(g, id, http.StatusBadRequest, "validation")
		return
	}
	if mode == "all" && !stepUpFresh(g, c.Now()) {
		c.positionErr(g, id, http.StatusForbidden, "step_up_required")
		return
	}
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		var x int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM position WHERE id=? AND state<>'merged'`, id).Scan(&x); err != nil {
			return err
		}
		if mode == "single" {
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM posting WHERE id=? AND position_id=? AND state<>'expired'`, listing, id).Scan(&x); err != nil {
				return errBadListing
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE position SET apply_override=?, apply_override_listing=?, apply_override_reason=? WHERE id=?`, mode, nz64(listing), nz(reason), id); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "position_override_set", Evidence: map[string]any{"position_id": id, "override": mode, "listing_id": listing}})
	})
	switch {
	case err == errBadListing:
		c.positionErr(g, id, http.StatusBadRequest, "validation")
	case err == sql.ErrNoRows:
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
	case err != nil:
		c.positionErr(g, id, http.StatusInternalServerError, "internal")
	default:
		g.Redirect(http.StatusSeeOther, fmt.Sprintf("/positions/%d", id))
	}
}

var errBadListing = fmt.Errorf("listing is not a live listing of this position")

func nz64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
