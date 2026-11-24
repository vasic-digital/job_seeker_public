// Package config loads configuration by environment-variable NAME and fails
// closed on unsafe values (FR-050, FR-053).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDataDirInGit is returned when JOBSEEKER_DATA_DIR resolves inside a git
// work tree, where a stray `git add` could publish the database.
var ErrDataDirInGit = errors.New("data dir is inside a git work tree")

// CheckDataDirOutsideGit refuses a data directory that, after symlink
// resolution, has any ancestor (or itself) containing a `.git` entry (a
// directory for a normal clone, a file for a worktree or submodule).
// The error never contains file content.
func CheckDataDirOutsideGit(dir string) error {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("data dir not resolvable: %w", err)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return fmt.Errorf("data dir not absolute: %w", err)
	}
	for cur := real; ; cur = filepath.Dir(cur) {
		if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
			return fmt.Errorf("%w (marker at %s)", ErrDataDirInGit, cur)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot determine git membership: %w", err)
		}
		if cur == filepath.Dir(cur) {
			return nil
		}
	}
}
