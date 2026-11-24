package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v %s", err, out)
	}
}

func TestDataDirInsideGitWorkTreeRefused(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	data := filepath.Join(repo, "deep", "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CheckDataDirOutsideGit(data); err == nil {
		t.Fatal("data dir inside a git work tree must be refused")
	}
}

func TestDataDirOutsideGitAccepted(t *testing.T) {
	d := t.TempDir() // temp dir is not inside a repository
	if err := CheckDataDirOutsideGit(d); err != nil {
		t.Fatalf("plain dir must be accepted: %v", err)
	}
}

func TestDataDirErrorsNeverEchoPathContent(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	err := CheckDataDirOutsideGit(repo)
	if err == nil {
		t.Fatal("expected refusal")
	}
}

func TestDataDirMissingIsError(t *testing.T) {
	if err := CheckDataDirOutsideGit(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing dir must be an error, not a pass")
	}
}

func TestSymlinkIntoRepoRefused(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	other := t.TempDir()
	link := filepath.Join(other, "lnk")
	if err := os.Symlink(repo, link); err != nil {
		t.Skip(err)
	}
	if err := CheckDataDirOutsideGit(link); err == nil {
		t.Fatal("symlink resolving into a work tree must be refused")
	}
}
