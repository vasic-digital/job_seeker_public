//go:build linux

package audit

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	fsIocGetFlags = 0x80086601 // FS_IOC_GETFLAGS (64-bit long)
	fsAppendFl    = 0x00000020 // FS_APPEND_FL
)

// strengthOf reports "mechanism" only when the filesystem enforces append-only (chattr +a) on the
// anchor file; otherwise the guarantee is operator policy.
func strengthOf(f *os.File) string {
	var flags int64
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), fsIocGetFlags, uintptr(unsafe.Pointer(&flags)))
	if errno == 0 && flags&fsAppendFl != 0 {
		return "mechanism"
	}
	return "policy"
}
