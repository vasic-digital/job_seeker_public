package privacy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrNoTmpfs means no RAM-backed directory is available; plaintext PDFs and snapshots are then refused
// rather than written to disk (FR-054).
var ErrNoTmpfs = errors.New("privacy: no tmpfs directory available for plaintext temp files")

const tmpfsMagic = 0x01021994

// IsTmpfs reports whether dir is on tmpfs.
func IsTmpfs(dir string) bool {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return false
	}
	return int64(st.Type) == tmpfsMagic
}

// TmpfsDir returns $XDG_RUNTIME_DIR or /dev/shm, whichever is tmpfs.
func TmpfsDir() (string, error) {
	for _, d := range []string{os.Getenv("XDG_RUNTIME_DIR"), "/dev/shm"} {
		if d != "" && IsTmpfs(d) {
			return d, nil
		}
	}
	return "", ErrNoTmpfs
}

// Temp is a 0600 file on tmpfs holding plaintext that must not reach disk.
type Temp struct {
	Path string
	size int
}

// NewTemp writes data to a new O_EXCL 0600 file inside a private 0700 directory under dir (a tmpfs
// directory; "" selects TmpfsDir). A dir that is not tmpfs is refused.
func NewTemp(dir, name string, data []byte) (*Temp, error) {
	if dir == "" {
		d, err := TmpfsDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	if !IsTmpfs(dir) {
		return nil, fmt.Errorf("%w: %s is not tmpfs", ErrNoTmpfs, "the given directory")
	}
	priv, err := os.MkdirTemp(dir, "jobseeker-")
	if err != nil {
		return nil, err
	}
	p := filepath.Join(priv, filepath.Base(name))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.Remove(priv)
		return nil, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.RemoveAll(priv)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.RemoveAll(priv)
		return nil, err
	}
	return &Temp{Path: p, size: len(data)}, nil
}

// Close overwrites the content with zeros and removes the file and its private directory.
func (t *Temp) Close() error {
	f, err := os.OpenFile(t.Path, os.O_WRONLY, 0)
	if err == nil {
		_, _ = f.Write(make([]byte, t.size))
		_ = f.Sync()
		f.Close()
	}
	return os.RemoveAll(filepath.Dir(t.Path))
}
