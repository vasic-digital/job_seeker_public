// Package gold builds, verifies and benchmarks the hand-labelled gold set of REAL postings (JSK-T057..T059,
// FR-023, FR-022, FR-077). Posting text lives only in a private corpus directory outside the repository;
// the tracked artefacts are a provenance manifest (provider, id, fetch date, sha256) and the label files.
package gold

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// Item is one real posting as stored in the private corpus (corpus.jsonl).
type Item struct {
	Key       string   `json:"key"` // provider:id
	Provider  string   `json:"provider"`
	ID        string   `json:"id"`
	FetchDate string   `json:"fetch_date"` // YYYY-MM-DD from the snapshot name
	Title     string   `json:"title"`
	Company   string   `json:"company"`
	Location  string   `json:"location"`
	Tags      []string `json:"tags"`
	Body      string   `json:"body"`
	SHA256    string   `json:"sha256"` // over the canonical JSON of title, tags and body
}

func (it Item) hash() string {
	b, _ := json.Marshal(struct {
		T string   `json:"t"`
		G []string `json:"g"`
		B string   `json:"b"`
	}{it.Title, it.Tags, it.Body})
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

var snapName = regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})T\d{6}Z-p\d+\.json$`)

func fetchDate(name string) (string, error) {
	m := snapName.FindStringSubmatch(filepath.Base(name))
	if m == nil {
		return "", fmt.Errorf("gold: snapshot file name %q has no timestamp", filepath.Base(name))
	}
	return m[1] + "-" + m[2] + "-" + m[3], nil
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	}
	return ""
}

// tagList copes with the providers' two tag encodings (array of strings, or absent).
func tagList(v any) []string {
	var out []string
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if s := strings.TrimSpace(str(x)); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// ExtractSnapshots reads raw provider snapshots (<dir>/<provider>/<ts>-pN.json) and returns the postings in a
// stable order. Nothing is invented: a record without an id or title is skipped and counted.
func ExtractSnapshots(dir string) (items []Item, skipped int, err error) {
	seen := map[string]bool{}
	provs, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	for _, pd := range provs {
		if !pd.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, pd.Name(), "*.json"))
		sort.Strings(files)
		for _, f := range files {
			fd, err := fetchDate(f)
			if err != nil {
				return nil, 0, err
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				return nil, 0, err
			}
			var recs []map[string]any
			var top any
			dec := json.NewDecoder(strings.NewReader(string(raw)))
			dec.UseNumber()
			if err := dec.Decode(&top); err != nil {
				return nil, 0, fmt.Errorf("gold: %s: %w", filepath.Base(f), err)
			}
			switch t := top.(type) {
			case []any:
				for _, x := range t {
					if m, ok := x.(map[string]any); ok {
						recs = append(recs, m)
					}
				}
			case map[string]any:
				if jobs, ok := t["jobs"].([]any); ok {
					for _, x := range jobs {
						if m, ok := x.(map[string]any); ok {
							recs = append(recs, m)
						}
					}
				}
			}
			for _, m := range recs {
				it := Item{Provider: pd.Name(), FetchDate: fd}
				it.ID = str(m["id"])
				switch pd.Name() {
				case "remoteok":
					it.Title, it.Company, it.Location = str(m["position"]), str(m["company"]), str(m["location"])
				default:
					it.Title, it.Company, it.Location = str(m["title"]), str(m["company_name"]), str(m["candidate_required_location"])
				}
				it.Tags = tagList(m["tags"])
				it.Body = providers.SanitizeHTML(str(m["description"]))
				if it.ID == "" || strings.TrimSpace(it.Title) == "" {
					skipped++ // e.g. remoteok's legal-notice first element
					continue
				}
				it.Key = it.Provider + ":" + it.ID
				if seen[it.Key] {
					continue
				}
				seen[it.Key] = true
				it.SHA256 = it.hash()
				items = append(items, it)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, skipped, nil
}

// WriteCorpus writes corpus.jsonl (mode 0600) into dir.
func WriteCorpus(dir string, items []Item) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "corpus.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, it := range items {
		b, _ := json.Marshal(it)
		w.Write(b)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadCorpus loads corpus.jsonl and re-verifies every sha256 (a corpus that no longer matches its manifest is
// rejected, not silently benchmarked).
func ReadCorpus(dir string) ([]Item, error) {
	f, err := os.Open(filepath.Join(dir, "corpus.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Item
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var it Item
		if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
			return nil, err
		}
		if it.hash() != it.SHA256 {
			return nil, fmt.Errorf("gold: %s content hash mismatch", it.Key)
		}
		out = append(out, it)
	}
	return out, sc.Err()
}

// Manifest is the tracked provenance file: provider, id, fetch date, sha256 (FR-077). No posting text.
func Manifest(items []Item) string {
	var b strings.Builder
	b.WriteString("provider\tid\tfetch_date\tsha256\n")
	for _, it := range items {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", it.Provider, it.ID, it.FetchDate, it.SHA256)
	}
	return b.String()
}

// VerifyManifest checks the corpus against the tracked manifest, both directions.
func VerifyManifest(items []Item, manifest string) error {
	want := map[string]string{}
	for i, ln := range strings.Split(strings.TrimSpace(manifest), "\n") {
		if i == 0 {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) != 4 {
			return fmt.Errorf("gold: manifest line %d malformed", i+1)
		}
		want[f[0]+":"+f[1]] = f[3]
	}
	for _, it := range items {
		h, ok := want[it.Key]
		if !ok {
			return fmt.Errorf("gold: %s not in manifest", it.Key)
		}
		if h != it.SHA256 {
			return fmt.Errorf("gold: %s sha256 differs from manifest", it.Key)
		}
		delete(want, it.Key)
	}
	if len(want) > 0 {
		return fmt.Errorf("gold: %d manifest row(s) have no corpus item", len(want))
	}
	return nil
}

// Posting converts a corpus item to the classifier input.
func (it Item) Posting(id int64) match.Posting {
	return match.Posting{ID: id, Title: it.Title, Body: it.Body, Tags: it.Tags, Company: it.Company, Location: it.Location}
}

var ErrNoCorpus = errors.New("gold: corpus not present")

func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
