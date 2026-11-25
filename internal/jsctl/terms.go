package jsctl

import (
	"context"
	"database/sql"
	"flag"
	"fmt"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
	"github.com/vasic-digital/job_seeker/internal/extwire"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const termsUsage = `  ext terms list                   per provider: REVIEWED, UNREVIEWED or CHANGED (a blocked provider is any but REVIEWED)
  ext terms accept --provider NAME record that you reviewed the provider's CURRENT legal notice (audited)
  ext enforce-approve on|off           gate Prepare and Approve (default ON, fail closed; "off" is an audited opt-out; ingest is always gated)
  ext enable|disable KIND ID       switch a registered plugin on or off (audited), e.g. ext enable tracker_hook reply_detect`

// extSwitchCmd handles the operator surface of the compliance and hook wiring (JSK-T239, JSK-T241). handled=false: not mine.
func extSwitchCmd(ctx context.Context, args []string, e Env) (rc int, handled bool) {
	switch {
	case len(args) >= 3 && args[0] == "ext" && args[1] == "terms":
		return withDB(ctx, e, func(db *store.DB, _ *config.Config) int { return termsCmd(ctx, args[2:], e, db) }), true
	case len(args) == 3 && args[0] == "ext" && args[1] == "enforce-approve" && (args[2] == "on" || args[2] == "off"):
		return withDB(ctx, e, func(db *store.DB, _ *config.Config) int {
			v := "0"
			if args[2] == "on" {
				v = "1"
			}
			if err := db.Write(ctx, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, draft.SettingEnforce, v); err != nil {
					return err
				}
				return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "ext_switch", Evidence: map[string]any{"kind": "compliance_stage", "plugin": "draft_approve", "on": v == "1"}})
			}); err != nil {
				fmt.Fprintln(e.Err, "ext: switch not recorded")
				return 1
			}
			fmt.Fprintf(e.Out, "dry-run enforcement %s\n", args[2])
			return 0
		}), true
	case len(args) >= 2 && args[0] == "ext" && (args[1] == "enable" || args[1] == "disable"):
		if len(args) != 4 {
			fmt.Fprintln(e.Err, termsUsage)
			return 2, true
		}
		set, err := builtin.New()
		if err != nil {
			fmt.Fprintln(e.Err, "ext: registration failed")
			return 1, true
		}
		if !set.Catalog().Plugins[args[2]+"/"+args[3]] {
			fmt.Fprintln(e.Err, "ext: no such plugin (see: ext list)")
			return 2, true
		}
		return withDB(ctx, e, func(db *store.DB, _ *config.Config) int {
			s := &extwire.DBSettings{DB: db}
			if err := s.SetEnabled(ctx, ext.Kind(args[2]), args[3], args[1] == "enable", "operator"); err != nil {
				fmt.Fprintln(e.Err, "ext: switch not recorded")
				return 1
			}
			fmt.Fprintf(e.Out, "%s/%s %sd\n", args[2], args[3], args[1])
			return 0
		}), true
	}
	return 0, false
}

func termsCmd(ctx context.Context, args []string, e Env, db *store.DB) int {
	t := &extwire.TermsStore{DB: db}
	switch args[0] {
	case "list":
		rows, err := t.List(ctx)
		if err != nil {
			fmt.Fprintln(e.Err, "ext: cannot list providers")
			return 1
		}
		for _, r := range rows {
			fmt.Fprintf(e.Out, "%s\t%s\n", r.Provider, r.State)
		}
		return 0
	case "accept":
		fs := flag.NewFlagSet("ext terms accept", flag.ContinueOnError)
		fs.SetOutput(e.Err)
		p := fs.String("provider", "", "provider name")
		if fs.Parse(args[1:]) != nil || *p == "" {
			fmt.Fprintln(e.Err, termsUsage)
			return 2
		}
		if err := t.Review(ctx, *p, "operator"); err != nil {
			fmt.Fprintln(e.Err, "ext terms:", err)
			return 1
		}
		fmt.Fprintf(e.Out, "terms accepted for %s\n", *p)
		return 0
	}
	fmt.Fprintln(e.Err, termsUsage)
	return 2
}
