package pdgate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// All values below are synthetic markers, never personal data.
const overlayYAML = `schema: v1
salary:
  floor: 123456
  salary_currency: ZZX
  salary_period: yearly
identity:
  token: zzq-needle-alpha
blocked:
  - zzq-blocked-co
  - "ab"
flag: true
empty: ""
`

func writeOverlay(t *testing.T, body string) string {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func byKey(ns []needle) map[string]needle {
	m := map[string]needle{}
	for _, n := range ns {
		m[n.Key] = n
	}
	return m
}

func TestLoadNeedlesFlattensAndClassifies(t *testing.T) {
	ns, err := LoadNeedles(writeOverlay(t, overlayYAML))
	if err != nil {
		t.Fatal(err)
	}
	m := byKey(ns)
	for _, k := range []string{"flag", "empty"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s must not become a needle (booleans and empty strings are skipped)", k)
		}
	}
	cases := []struct {
		key, val string
		combo    bool
	}{
		{"salary.floor", "123456", false},
		{"salary.salary_currency", "zzx", true}, // low-entropy key name
		{"salary.salary_period", "yearly", true},
		{"schema", "v1", true},
		{"identity.token", "zzq-needle-alpha", false},
		{"blocked[0]", "zzq-blocked-co", false},
		{"blocked[1]", "ab", true}, // shorter than 4 characters
	}
	for _, c := range cases {
		n, ok := m[c.key]
		if !ok {
			t.Errorf("missing needle %s", c.key)
			continue
		}
		if n.Val != c.val || n.Combo != c.combo {
			t.Errorf("%s = {%q combo=%v}, want {%q combo=%v}", c.key, n.Val, n.Combo, c.val, c.combo)
		}
	}
	for i := 1; i < len(ns); i++ {
		if ns[i-1].Key > ns[i].Key {
			t.Fatalf("needles not sorted: %s before %s", ns[i-1].Key, ns[i].Key)
		}
	}
}

func TestLoadNeedlesCannotDetermine(t *testing.T) {
	if _, err := LoadNeedles(""); !errors.Is(err, ErrNoOverlay) {
		t.Errorf("empty dir: %v", err)
	}
	if _, err := LoadNeedles(t.TempDir()); !errors.Is(err, ErrNoOverlay) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := LoadNeedles(writeOverlay(t, "flag: true\nempty: \"\"\n")); !errors.Is(err, ErrNoOverlay) {
		t.Errorf("overlay with no usable scalar must be ErrNoOverlay, got %v", err)
	}
	_, err := LoadNeedles(writeOverlay(t, "a: [unclosed\n  b: {"))
	if err == nil || errors.Is(err, ErrNoOverlay) || !strings.Contains(err.Error(), "not valid YAML") {
		t.Errorf("invalid YAML: %v", err)
	}
}

func TestFindingStringNeverCarriesContent(t *testing.T) {
	got := Finding{Kind: "needle", Path: "docs/a.md", Line: 7, Key: "identity.token"}.String()
	if got != "needle\tdocs/a.md:7\toverlay key identity.token" {
		t.Errorf("needle: %q", got)
	}
	if got := (Finding{Kind: "forbidden-file", Path: "x/y.pdf"}).String(); got != "forbidden-file\tx/y.pdf" {
		t.Errorf("forbidden: %q", got)
	}
}

func TestScanBytesPolicyTable(t *testing.T) {
	ns, err := LoadNeedles(writeOverlay(t, overlayYAML))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string
		want []Finding
	}{
		{"clean text", "nothing to see here\nsecond line\n", nil},
		{"high entropy anywhere, case-insensitive", "ok\nline with ZZQ-Needle-ALPHA inside\n", []Finding{{Kind: "needle", Path: "p", Line: 2, Key: "identity.token"}}},
		{"two keys on distinct lines", "123456\nzzq-blocked-co\n", []Finding{{Kind: "needle", Path: "p", Line: 1, Key: "salary.floor"}, {Kind: "needle", Path: "p", Line: 2, Key: "blocked[0]"}}},
		{"low entropy value alone is ignored", "salary_currency = zzx\nyearly pay\n", []Finding{{Kind: "needle", Path: "p", Line: 1, Key: "salary.salary_currency"}}},
		{"low entropy value without key name is ignored", "zzx\nyearly\nv1\nab\n", nil},
		{"combo with key name on same line", "salary_period: yearly\n", []Finding{{Kind: "needle", Path: "p", Line: 1, Key: "salary.salary_period"}}},
		{"short needle needs its key name", "ab\n", nil},
		{"binary content: only high entropy, no line number", "\x00\x01zzq-needle-alpha\x02", []Finding{{Kind: "needle", Path: "p", Key: "identity.token"}}},
		{"binary content: low entropy ignored", "\x00zzx currency yearly", nil},
	}
	for _, c := range cases {
		got := scanBytes("p", []byte(c.body), ns)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", c.name, got, c.want)
		}
	}
}

func TestPostingLikeAndAllowed(t *testing.T) {
	for p, want := range map[string]bool{
		"data/postings/a.txt": true, "a/Snapshots/x.bin": true, "raw/x": true, "x/raw/y": true,
		"docs/posting-1.json": true, "docs/posting.html": true, "docs/posting.md": false, "docs/notes.json": false, "postings": false,
	} {
		if got := postingLike(p); got != want {
			t.Errorf("postingLike(%q) = %v, want %v", p, got, want)
		}
	}
	if !allowed("evidence/a.png", []string{"x/", "evidence/"}) || allowed("evidence/a.png", []string{"", "docs/"}) || allowed("a", nil) {
		t.Error("allowed prefix semantics wrong (an empty prefix must never allow everything)")
	}
}

func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	d := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", d}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	for p, body := range files {
		full := filepath.Join(d, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	return d
}

func needles(t *testing.T) []needle {
	t.Helper()
	ns, err := LoadNeedles(writeOverlay(t, overlayYAML))
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

func kinds(fs []Finding) map[string][]string {
	m := map[string][]string{}
	for _, f := range fs {
		m[f.Kind] = append(m[f.Kind], f.Path)
	}
	return m
}

func TestScanNegativeCorpusIsClean(t *testing.T) {
	repo := gitRepo(t, map[string]string{"README.md": "hello\n", "src/a.go": "package a\n", "docs/notes.txt": "123 is not the needle\n"})
	got, err := Scan(Config{Repo: repo, Needles: needles(t)})
	if err != nil || len(got) != 0 {
		t.Fatalf("clean tree: findings=%+v err=%v", got, err)
	}
}

func TestScanPositiveCorpus(t *testing.T) {
	repo := gitRepo(t, map[string]string{
		"docs/leak.md":          "line one\nowner token zzq-needle-alpha here\n",
		"data/postings/job.txt": "plain body\n",
		"img/logo.png":          "not really a png",
		"evidence/ok.pdf":       "%PDF synthetic",
		"README.md":             "clean\n",
	})
	got, err := Scan(Config{Repo: repo, Needles: needles(t), AllowPrefix: []string{"evidence/"}})
	if err != nil {
		t.Fatal(err)
	}
	k := kinds(got)
	if !reflect.DeepEqual(k["needle"], []string{"docs/leak.md"}) {
		t.Errorf("needle findings: %v", k["needle"])
	}
	forb := map[string]bool{}
	for _, p := range k["forbidden-file"] {
		forb[p] = true
	}
	if !forb["data/postings/job.txt"] || !forb["img/logo.png"] || forb["evidence/ok.pdf"] || len(forb) != 2 {
		t.Errorf("forbidden-file findings: %v (the allow-listed prefix must be exempt)", k["forbidden-file"])
	}
	for _, f := range got {
		if f.Kind == "needle" && (f.Line != 2 || f.Key != "identity.token") {
			t.Errorf("needle finding: %+v", f)
		}
		if strings.Contains(f.String(), "zzq-needle-alpha") {
			t.Errorf("finding text leaks the needle value: %q", f.String())
		}
	}
}

func TestScanPrivatePathAndExtra(t *testing.T) {
	repo := gitRepo(t, map[string]string{"private/secret.txt": "x\n", "pub/a.txt": "y\n"})
	abs, _ := filepath.Abs(repo)
	extra := t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "layer.log"), []byte("a\nb zzq-blocked-co\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Scan(Config{Repo: repo, Needles: needles(t), PrivateDirs: []string{filepath.Join(abs, "private")}, Extra: []string{extra}})
	if err != nil {
		t.Fatal(err)
	}
	k := kinds(got)
	if !reflect.DeepEqual(k["private-path"], []string{"private/secret.txt"}) {
		t.Errorf("private-path: %v", k["private-path"])
	}
	if len(k["needle"]) != 1 || filepath.Base(k["needle"][0]) != "layer.log" {
		t.Errorf("extra scan: %v", k["needle"])
	}
}

func TestScanCannotDetermine(t *testing.T) {
	if _, err := Scan(Config{Repo: t.TempDir()}); !errors.Is(err, ErrNoOverlay) {
		t.Errorf("no needles: %v", err)
	}
	if _, err := Scan(Config{Repo: filepath.Join(t.TempDir(), "absent"), Needles: needles(t)}); err == nil || !strings.Contains(err.Error(), "cannot determine") {
		t.Errorf("not a git repo: %v", err)
	}
	repo := gitRepo(t, map[string]string{"a.txt": "x\n"})
	if _, err := Scan(Config{Repo: repo, Needles: needles(t), Extra: []string{filepath.Join(t.TempDir(), "missing")}}); err == nil || !strings.Contains(err.Error(), "cannot determine") {
		t.Errorf("unreadable extra: %v", err)
	}
}

func TestScanSkipsDeletedTrackedFile(t *testing.T) {
	repo := gitRepo(t, map[string]string{"gone.txt": "zzq-needle-alpha\n", "keep.txt": "ok\n"})
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := Scan(Config{Repo: repo, Needles: needles(t)})
	if err != nil || len(got) != 0 {
		t.Fatalf("deleted file: %+v %v", got, err)
	}
}
