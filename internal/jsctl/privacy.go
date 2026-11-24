package jsctl

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/privacy"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const privacyUsage = `  export --out FILE [--company NAME]   encrypted archive under JOBSEEKER_EXPORT_PASSPHRASE (not the data key)
  import --in FILE                 restore an archive into a database holding no postings or applications
  delete-record --posting N        delete one posting and its application subtree
  delete-all --confirm ` + privacy.ConfirmPhrase + `   delete every row and file, destroy the data key
  rotate-key --new-passphrase-file F   re-wrap the data key (data stays readable)
  restore-drill [--file F] [--scratch DIR]   restore a snapshot into scratch and run smoke checks (default: newest)`

func privacyCmd(ctx context.Context, name string, args []string, e Env) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.Err)
	out := fs.String("out", "", "")
	in := fs.String("in", "", "")
	company := fs.String("company", "", "")
	posting := fs.Int64("posting", 0, "")
	confirm := fs.String("confirm", "", "")
	newPass := fs.String("new-passphrase-file", "", "")
	file := fs.String("file", "", "")
	scratch := fs.String("scratch", "", "")
	if fs.Parse(args) != nil {
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
		dbPass := []byte(c.DBKey.Reveal())
		unlock := func() (*crypto.Keyring, bool) {
			kr, err := boot.OpenKeyring(ctx, db, dbPass)
			if err != nil {
				fmt.Fprintf(e.Err, "%s: cannot unlock the data key\n", name)
				return nil, false
			}
			return kr, true
		}
		switch name {
		case "export":
			if *out == "" {
				fmt.Fprintln(e.Err, privacyUsage)
				return 2
			}
			kr, ok := unlock()
			if !ok {
				return 1
			}
			defer kr.Close()
			b, err := privacy.Export(ctx, db, kr, privacy.ExportOptions{Pass: []byte(c.ExportPass.Reveal()), DBPass: dbPass, Company: *company, Actor: "operator"})
			if err != nil {
				fmt.Fprintf(e.Err, "export: %v\n", err)
				return 1
			}
			f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				fmt.Fprintln(e.Err, "export: cannot create the output file (it must not exist)")
				return 1
			}
			_, werr := f.Write(b)
			if cerr := f.Close(); werr != nil || cerr != nil {
				fmt.Fprintln(e.Err, "export: write failed")
				return 1
			}
			fmt.Fprintf(e.Out, "export: wrote %d bytes, mode 0600\n", len(b))
			return 0
		case "import":
			if *in == "" {
				fmt.Fprintln(e.Err, privacyUsage)
				return 2
			}
			b, err := os.ReadFile(*in)
			if err != nil {
				fmt.Fprintln(e.Err, "import: cannot read the archive")
				return 1
			}
			kr, ok := unlock()
			if !ok {
				return 1
			}
			defer kr.Close()
			n, err := privacy.Import(ctx, db, kr, b, []byte(c.ExportPass.Reveal()), "operator")
			if err != nil {
				fmt.Fprintf(e.Err, "import: %v\n", err)
				return 1
			}
			fmt.Fprintf(e.Out, "import: %d rows restored\n", n)
			return 0
		case "delete-record", "delete-all":
			kr, ok := unlock()
			if !ok {
				return 1
			}
			defer kr.Close()
			d := &privacy.Deleter{DB: db, Keys: kr, DataDir: c.DataDir, DraftDir: filepath.Join(c.DataDir, "drafts"), BackupDir: app.BackupDir(c),
				Dirs: []string{app.SnapshotDir(c), filepath.Join(c.DataDir, "drafts")}}
			var res privacy.Result
			var err error
			if name == "delete-record" {
				if *posting == 0 {
					fmt.Fprintln(e.Err, privacyUsage)
					return 2
				}
				res, err = d.DeletePosting(ctx, *posting, "operator")
			} else {
				res, err = d.DeleteAll(ctx, "operator", *confirm)
			}
			if err != nil {
				fmt.Fprintf(e.Err, "%s: %v\n", name, err)
				return 1
			}
			fmt.Fprintf(e.Out, "%s: postings=%d applications=%d files=%d key_destroyed=%v backups_remaining=%d\n%s\n", name, res.Postings, res.Applications, res.Files, res.KeyDestroyed, res.BackupsRemaining, privacy.BackupHorizon)
			return 0
		case "rotate-key":
			if *newPass == "" {
				fmt.Fprintln(e.Err, privacyUsage)
				return 2
			}
			fi, err := os.Stat(*newPass)
			if err != nil || fi.Mode().Perm()&0o077 != 0 {
				fmt.Fprintln(e.Err, "rotate-key: the passphrase file must exist with mode 0600 or stricter")
				return 1
			}
			b, _ := os.ReadFile(*newPass)
			np := []byte(strings.TrimRight(string(b), "\r\n"))
			if len(np) < 16 {
				fmt.Fprintln(e.Err, "rotate-key: the new passphrase must be at least 16 characters")
				return 1
			}
			if err := privacy.RotateKey(ctx, db, dbPass, np, crypto.DefaultParams(), "operator"); err != nil {
				fmt.Fprintln(e.Err, "rotate-key: failed (wrong current passphrase or store error)")
				return 1
			}
			fmt.Fprintln(e.Out, "rotate-key: data key re-wrapped; point JOBSEEKER_DB_KEY at the new passphrase before the next start")
			return 0
		case "restore-drill":
			f := *file
			bd := app.BackupDir(c)
			if f == "" {
				ents, _ := os.ReadDir(bd)
				var names []string
				for _, en := range ents {
					if strings.Contains(en.Name(), ".snapshot-v") {
						names = append(names, en.Name())
					}
				}
				if len(names) == 0 {
					fmt.Fprintln(e.Err, "restore-drill: no snapshot in the backup directory (run jsctl backup)")
					return 2
				}
				sort.Strings(names)
				f = filepath.Join(bd, names[len(names)-1])
			}
			sc := *scratch
			if sc == "" {
				sc = c.DataDir
			}
			rep, err := privacy.RestoreDrill(ctx, f, sc)
			if err != nil {
				fmt.Fprintf(e.Err, "restore-drill: %v\n", err)
				return 1
			}
			fmt.Fprintln(e.Out, rep.String())
			return 0
		}
		return 2
	})
}
