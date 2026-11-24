package privacy

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/store"
)

var (
	ErrWeakPass      = errors.New("privacy: export passphrase must be at least 12 characters")
	ErrSamePass      = errors.New("privacy: the export passphrase must differ from the database passphrase")
	ErrBadArchive    = errors.New("privacy: not a job_seeker export archive")
	ErrTargetNotFree = errors.New("privacy: import target already holds postings or applications")
)

const magic = "JSKEXPORT1\n"

// sealed lists the encrypted columns by table (data-model.md); export decrypts them, import re-seals them
// under the TARGET database key, so the archive depends on the export passphrase and nothing else.
var sealed = map[string][]string{
	"posting": {"body_text"}, "score": {"components_json", "gates_json", "explain_json"}, "application": {"provider_ref"},
	"draft": {"path_ref"}, "approval": {"reason"}, "submission": {"payload"}, "tracking": {"note"},
}

// tables in dependency order. audit_log is exported for the operator's record and NEVER imported (a copied
// chain would not extend the target's chain).
var tables = []string{"posting", "mapping", "score", "application", "draft", "approval", "submission", "tracking"}

type bin struct {
	B64 string `json:"$b64"`
}

// Options for Export.
type ExportOptions struct {
	Pass    []byte        // export passphrase
	DBPass  []byte        // database passphrase, only to refuse equality
	Company string        // per-company subset ("" = everything); exact, case-insensitive match
	Params  crypto.Params // zero = crypto.DefaultParams
	Actor   string
	Now     func() time.Time
}

type payload struct {
	Version  int                         `json:"version"`
	Created  string                      `json:"created"`
	Company  string                      `json:"company,omitempty"`
	Provider map[string]string           `json:"providers"` // old id -> name
	Tables   map[string][]map[string]any `json:"tables"`
	Audit    []map[string]any            `json:"audit_log"`
}

func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func toJSONValue(v any) any {
	if b, ok := v.([]byte); ok {
		return bin{base64.StdEncoding.EncodeToString(b)}
	}
	return v
}

// Export writes the encrypted archive and one data_export audit row (counts only).
func Export(ctx context.Context, db *store.DB, kr *crypto.Keyring, o ExportOptions) ([]byte, error) {
	if len(o.Pass) < 12 {
		return nil, ErrWeakPass
	}
	if bytes.Equal(o.Pass, o.DBPass) {
		return nil, ErrSamePass
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Params == (crypto.Params{}) {
		o.Params = crypto.DefaultParams()
	}
	pl := payload{Version: 1, Created: o.Now().UTC().Format(time.RFC3339), Company: o.Company, Provider: map[string]string{}, Tables: map[string][]map[string]any{}}
	rd := db.Read()
	prov, err := rd.QueryContext(ctx, `SELECT id, name FROM provider`)
	if err != nil {
		return nil, err
	}
	for prov.Next() {
		var id int64
		var n string
		if err := prov.Scan(&id, &n); err != nil {
			prov.Close()
			return nil, err
		}
		pl.Provider[fmt.Sprint(id)] = n
	}
	prov.Close()

	where := map[string]string{
		"posting": "1=1", "mapping": "posting_id IN (SELECT id FROM posting WHERE %s)", "score": "posting_id IN (SELECT id FROM posting WHERE %s)",
		"application": "posting_id IN (SELECT id FROM posting WHERE %s)",
	}
	pw := "1=1"
	var args []any
	if o.Company != "" {
		pw = "lower(company) = lower(?)"
		args = []any{o.Company}
	}
	appSub := "application_id IN (SELECT id FROM application WHERE posting_id IN (SELECT id FROM posting WHERE " + pw + "))"
	total := 0
	for _, t := range tables {
		var q string
		switch t {
		case "posting":
			q = "SELECT * FROM posting WHERE " + pw
		case "mapping", "score", "application":
			q = "SELECT * FROM " + t + " WHERE " + fmt.Sprintf(where[t], pw)
		default:
			q = "SELECT * FROM " + t + " WHERE " + appSub
		}
		rows, err := rd.QueryContext(ctx, q+" ORDER BY id", args...)
		if err != nil {
			return nil, err
		}
		rs, err := scanRows(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			id, _ := r["id"].(int64)
			for _, c := range sealed[t] {
				if b, ok := r[c].([]byte); ok && len(b) > 0 {
					pt, err := kr.Open(t, c, id, b)
					if err != nil {
						return nil, fmt.Errorf("privacy: %s.%s of row %d cannot be opened; export refused", t, c, id)
					}
					r[c] = pt
				}
			}
			for k, v := range r {
				r[k] = toJSONValue(v)
			}
		}
		if t == "posting" {
			ids := map[int64]bool{}
			for _, r := range rs {
				id, _ := r["id"].(int64)
				ids[id] = true
			}
			for _, r := range rs {
				if d, ok := r["dup_of"].(int64); ok && !ids[d] {
					r["dup_of"] = nil // the referenced posting is outside a per-company subset
				}
			}
		}
		pl.Tables[t] = rs
		total += len(rs)
	}
	if o.Company == "" {
		rows, err := rd.QueryContext(ctx, `SELECT * FROM audit_log ORDER BY id`)
		if err != nil {
			return nil, err
		}
		rs, err := scanRows(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		pl.Audit = rs
	}
	var raw bytes.Buffer
	zw := gzip.NewWriter(&raw)
	if err := json.NewEncoder(zw).Encode(pl); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	env, ekr, err := crypto.NewEnvelope(o.Pass, o.Params)
	if err != nil {
		return nil, err
	}
	defer ekr.Close()
	blob, err := ekr.Seal("export", "archive", 0, raw.Bytes())
	if err != nil {
		return nil, err
	}
	hdr, _ := json.Marshal(map[string]any{"kdf": env.KDF, "params": env.ParamsJSON, "salt": env.Salt, "wrapped": env.WrappedDEK, "version": env.Version})
	var out bytes.Buffer
	out.WriteString(magic)
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(hdr)))
	out.Write(l[:])
	out.Write(hdr)
	out.Write(blob)
	actor := o.Actor
	if actor == "" {
		actor = "operator"
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "data_export", Evidence: map[string]any{"rows": total, "subset": o.Company != ""}})
	})
	return out.Bytes(), err
}

func decodeValue(v any) any {
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["$b64"].(string); ok && len(m) == 1 {
			b, _ := base64.StdEncoding.DecodeString(s)
			return b
		}
	}
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}

// Import reads an archive into a database that holds no postings or applications, re-sealing sealed
// columns under kr. Providers are matched by name (created if absent); ids of imported rows are preserved.
func Import(ctx context.Context, db *store.DB, kr *crypto.Keyring, archive, pass []byte, actor string) (n int, err error) {
	// An archive is untrusted input: a hostile or damaged one must yield an error, never a panic. The
	// explicit checks below cover the known shapes; this is the backstop for the rest (the transaction is
	// rolled back because the write callback returns the error).
	defer func() {
		if r := recover(); r != nil {
			n, err = 0, ErrBadArchive
		}
	}()
	if kr == nil || db == nil {
		return 0, errors.New("privacy: import needs a database and a keyring")
	}
	if !bytes.HasPrefix(archive, []byte(magic)) || len(archive) < len(magic)+4 {
		return 0, ErrBadArchive
	}
	rest := archive[len(magic):]
	hl := int(binary.BigEndian.Uint32(rest[:4]))
	if hl <= 0 || 4+hl > len(rest) {
		return 0, ErrBadArchive
	}
	var h struct {
		KDF     string `json:"kdf"`
		Params  string `json:"params"`
		Salt    []byte `json:"salt"`
		Wrapped []byte `json:"wrapped"`
		Version int    `json:"version"`
	}
	if json.Unmarshal(rest[4:4+hl], &h) != nil {
		return 0, ErrBadArchive
	}
	ekr, err := crypto.Unlock(&crypto.KeyEnvelope{KDF: h.KDF, ParamsJSON: h.Params, Salt: h.Salt, WrappedDEK: h.Wrapped, Version: h.Version}, pass)
	if err != nil {
		return 0, err
	}
	defer ekr.Close()
	plain, err := ekr.Open("export", "archive", 0, rest[4+hl:])
	if err != nil {
		return 0, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return 0, ErrBadArchive
	}
	var pl payload
	dec := json.NewDecoder(io.LimitReader(zr, 1<<30))
	if err := dec.Decode(&pl); err != nil || pl.Version != 1 {
		return 0, ErrBadArchive
	}
	imported := 0
	err = db.Write(ctx, func(tx *sql.Tx) (werr error) {
		defer func() {
			if r := recover(); r != nil {
				werr = ErrBadArchive
			}
		}()
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM posting) + (SELECT count(*) FROM application)`).Scan(&existing); err != nil {
			return err
		}
		if existing != 0 {
			return ErrTargetNotFree
		}
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		pmap := map[int64]int64{}
		for oldID, name := range pl.Provider {
			var id int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM provider WHERE name=?`, name).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				r, err := tx.ExecContext(ctx, `INSERT INTO provider(name, base_url, kind) VALUES (?, 'https://invalid.example', 'imported')`, name)
				if err != nil {
					return err
				}
				id, _ = r.LastInsertId()
			} else if err != nil {
				return err
			}
			var o int64
			fmt.Sscan(oldID, &o)
			pmap[o] = id
		}
		for _, t := range tables {
			cols, err := tableColumns(ctx, tx, t)
			if err != nil {
				return err
			}
			for _, r := range pl.Tables[t] {
				var names []string
				var qs []string
				var vals []any
				for k, raw := range r {
					if !cols[k] {
						return fmt.Errorf("privacy: archive column %s.%s is unknown to this schema", t, k)
					}
					names = append(names, k)
					qs = append(qs, "?")
					vals = append(vals, decodeValue(raw))
				}
				ix := indexOf(names, "id")
				if ix < 0 {
					return fmt.Errorf("%w: a %s row has no id", ErrBadArchive, t)
				}
				idf, ok := vals[ix].(int64)
				if !ok {
					return fmt.Errorf("%w: a %s row has a non-integer id", ErrBadArchive, t)
				}
				for i, k := range names {
					switch {
					case t == "posting" && k == "provider_id":
						pv, ok := vals[i].(int64)
						if !ok {
							return fmt.Errorf("%w: posting provider_id is not an integer", ErrBadArchive)
						}
						mapped, ok := pmap[pv]
						if !ok {
							return fmt.Errorf("%w: posting refers to an unlisted provider", ErrBadArchive)
						}
						vals[i] = mapped
					case contains(sealed[t], k):
						if b, ok := vals[i].([]byte); ok && len(b) > 0 {
							s, err := kr.Seal(t, k, idf, b)
							if err != nil {
								return err
							}
							vals[i] = s
						}
					}
				}
				if _, err := tx.ExecContext(ctx, "INSERT INTO "+t+"("+strings.Join(names, ",")+") VALUES ("+strings.Join(qs, ",")+")", vals...); err != nil {
					return fmt.Errorf("privacy: importing %s failed", t)
				}
				imported++
			}
		}
		if actor == "" {
			actor = "operator"
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "data_import", Evidence: map[string]any{"rows": imported}})
	})
	if err != nil {
		return 0, err
	}
	return imported, nil
}

func tableColumns(ctx context.Context, tx *sql.Tx, t string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+t+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]bool{}
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &nn, &dflt, &pk); err != nil {
			return nil, err
		}
		m[name] = true
	}
	return m, rows.Err()
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func indexOf(l []string, s string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return 0
}
