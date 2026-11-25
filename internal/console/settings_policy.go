package console

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/position"
)

// JSK-T207 (FR-090): apply-mode and provider-favourites settings. They only RESTRICT the apply path (position.Policy never grants), so
// changing them needs no step-up; every change is audited. A stored apply_mode wins over Deps.DefaultApplyMode (JOBSEEKER_APPLY_MODE).

// PolicyView is what the settings page shows about the apply policy.
type PolicyView struct {
	Mode       string // effective mode; "" when the stored value is unknown (the apply path then refuses everything)
	Stored     bool
	Invalid    string // the unknown stored value, shown so the operator can repair it
	Default    string
	Favourites string // comma separated, in order
	FavInvalid string // the unreadable stored value, shown so the operator can repair it (never rendered as an empty list)
	Strict     bool
	Modes      []string
}

func (c *Console) policyView(ctx context.Context) PolicyView {
	v := PolicyView{Default: c.DefaultApplyMode, Modes: []string{position.ModeAllAware, position.ModeSingleAuto, position.ModeSingleFavourites}}
	if v.Default == "" {
		v.Default = position.ModeAllAware
	}
	v.Mode = v.Default
	get := func(k string) (string, bool) {
		var s string
		err := c.DB.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, k).Scan(&s)
		return s, err == nil
	}
	if s, ok := get(position.SettingApplyMode); ok {
		v.Stored = true
		if position.ValidApplyMode(s) {
			v.Mode = s
		} else {
			v.Mode, v.Invalid = "", s
		}
	}
	if s, ok := get(position.SettingFavourites); ok {
		if l, err := position.ParseFavourites(s); err == nil {
			v.Favourites = strings.Join(l, ", ")
		} else {
			v.FavInvalid = s // shown as invalid, never as an empty list: the apply path refuses while it is unreadable
		}
	}
	if s, ok := get(position.SettingFavStrict); ok {
		v.Strict = s == "1"
	}
	return v
}

func putSetting(ctx context.Context, tx *sql.Tx, k, v string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

// setApplyMode stores apply_mode. An unknown mode is refused (400) and nothing is written.
func (c *Console) setApplyMode(g *gin.Context) {
	m := g.PostForm("mode")
	if !position.ValidApplyMode(m) {
		c.settingsErr(g, http.StatusBadRequest, "validation")
		return
	}
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		if err := putSetting(ctx, tx, position.SettingApplyMode, m); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "apply_mode_changed", Evidence: map[string]any{"mode": m}})
	})
	if err != nil {
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}

// setFavourites stores the ordered favourites list (names of known providers, no duplicates) and the strict flag.
func (c *Console) setFavourites(g *gin.Context) {
	ctx := g.Request.Context()
	var names []string
	seen := map[string]bool{}
	for _, n := range strings.Split(g.PostForm("favourites"), ",") {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		if seen[n] {
			c.settingsErr(g, http.StatusBadRequest, "validation")
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	for _, n := range names {
		var one int
		if c.DB.Read().QueryRowContext(ctx, `SELECT 1 FROM provider WHERE name=?`, n).Scan(&one) != nil {
			c.settingsErr(g, http.StatusBadRequest, "validation")
			return
		}
	}
	strict := "0"
	if g.PostForm("strict") == "1" {
		strict = "1"
	}
	js, _ := json.Marshal(names)
	if names == nil {
		js = []byte("[]")
	}
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		if err := putSetting(ctx, tx, position.SettingFavourites, string(js)); err != nil {
			return err
		}
		if err := putSetting(ctx, tx, position.SettingFavStrict, strict); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "provider_favourites_changed", Evidence: map[string]any{"count": int64(len(names)), "strict": strict}})
	})
	if err != nil {
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}
