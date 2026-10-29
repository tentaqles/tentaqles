//go:build windows

package packs

import (
	"syscall"
	"testing"
)

// shortName returns the 8.3 form of p ("" when the volume has none).
func shortName(t *testing.T, p string) string {
	t.Helper()
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}
