package evnorm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRules() Rules {
	return Rules{Repo: "/ho" + "me/alice/Projects/umb/job_seeker-wt-x", Umbrella: "/ho" + "me/alice/Projects/umb", Home: "/ho" + "me/alice", User: "alice", Host: "buildbox7", Tmp: []string{"/run/user/1000"}}
}

func TestNormaliseRules(t *testing.T) {
	r := testRules()
	cases := []struct{ name, in, want string }{
		{"repo", "cd " + r.Repo + "/deploy", "cd <repo>/deploy"},
		{"umbrella sibling", r.Umbrella + "/job_seeker/x.go", "<umbrella>/job_seeker/x.go"},
		{"home", r.Home + "/.cache/go", "$HOME/.cache/go"},
		{"foreign linux home", "/ho" + "me/bob/work/x", "$HOME/work/x"},
		{"foreign macos home", "/Us" + "ers/carol/x", "$HOME/x"},
		{"user@", "alice@box", "<USER>@box"},
		{"USER=", "USER=alice x", "USER=<USER> x"},
		{"host after @", "me@buildbox7:~", "me@<HOST>:~"},
		{"HOSTNAME=", "HOSTNAME=buildbox7", "HOSTNAME=<HOST>"},
		{"bare words are prose", "owner alice on buildbox7", "owner alice on buildbox7"},
		{"user inside word kept", "malice alicewonder", "malice alicewonder"},
		{"mktemp dir", "dir /tmp/tmp.Ab12cd/certs", "dir <TMP>/certs"},
		{"go temp dir", "/tmp/TestX1234567/001/f", "<TMP>/001/f"},
		{"per-user tmp", "/run/user/1000/x", "<TMP>/x"},
		{"fixed tmp name kept", "/tmp/.js_probe_src and /tmp", "/tmp/.js_probe_src and /tmp"},
		{"windows-ish kept", `C:\tmp\x`, `C:\tmp\x`},
		{"placeholders untouched", "$HOME/x <repo>/y <umbrella>/z <TMP>/q", "$HOME/x <repo>/y <umbrella>/z <TMP>/q"},
		{"home sibling of repo prefix", "/ho" + "me/alice/Projects/umb/job_seeker-wt-xyz/a", "<umbrella>/job_seeker-wt-xyz/a"},
	}
	for _, c := range cases {
		if got := r.Normalise(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
		if again := r.Normalise(r.Normalise(c.in)); again != r.Normalise(c.in) {
			t.Errorf("%s: not idempotent: %q", c.name, again)
		}
	}
}

func TestFindings(t *testing.T) {
	r := testRules()
	if f := r.Findings("clean $HOME/x <repo>/y\n"); len(f) != 0 {
		t.Fatalf("clean text flagged: %v", f)
	}
	f := r.Findings("ok\nsee /ho" + "me/bob/x\nand /tmp/tmp.Zz9Yx8/a\n")
	if len(f) != 2 || f[0].Line != 2 || f[1].Line != 3 {
		t.Fatalf("want findings on lines 2 and 3, got %v", f)
	}
}

func TestWriteFileNormalises(t *testing.T) {
	r := testRules()
	p := filepath.Join(t.TempDir(), "e.txt")
	if err := r.WriteFile(p, []byte("at "+r.Repo+"/x USER=alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "alice") || !strings.Contains(string(b), "<repo>/x") {
		t.Fatalf("not normalised: %q", b)
	}
}

func TestShortUserNotReplaced(t *testing.T) {
	r := Rules{User: "al"}
	if got := r.Normalise("al went"); got != "al went" {
		t.Fatalf("a 2-char user name must not be replaced as a word: %q", got)
	}
}

func TestDetectUsesGivenRepoAndNormalisesIt(t *testing.T) {
	r := Detect("/srv/work/umb/job_seeker-wt-q")
	if r.Repo != "/srv/work/umb/job_seeker-wt-q" || r.Umbrella != "/srv/work/umb" {
		t.Fatalf("repo/umbrella not derived: %+v", r)
	}
	if got := r.Normalise("cd /srv/work/umb/job_seeker-wt-q/x and /srv/work/umb/other"); got != "cd <repo>/x and <umbrella>/other" {
		t.Fatalf("got %q", got)
	}
	if r.Home == "" {
		t.Skip("no home dir on this host")
	}
	if got := r.Normalise(r.Home + "/a"); got != "$HOME/a" {
		t.Fatalf("home not scrubbed: %q", got)
	}
}

func TestDefaultWriteFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "e.txt")
	h, _ := os.UserHomeDir()
	if err := WriteFile(p, []byte("p "+h+"/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if h != "" && h != "/" && strings.Contains(string(b), h) {
		t.Fatalf("home leaked: %q", b)
	}
}

func TestCommonWordNamesAreNotCorrupted(t *testing.T) {
	r := Rules{User: "test", Host: "ubuntu"}
	in := "run the test suite on ubuntu"
	if got := r.Normalise(in); got != in {
		t.Fatalf("prose rewritten: %q", got)
	}
}
