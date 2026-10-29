//go:build windows

package packs

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var procGetFinalPathNameByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// finalPath returns the long, link-free path of the open file (from the
// handle, so 8.3 short names, junctions and symlinks are resolved) and its
// hard-link count (1 for a directory).
func finalPath(f *os.File, _ string) (string, int, error) {
	h := syscall.Handle(f.Fd())
	buf := make([]uint16, 1024)
	for {
		n, _, err := procGetFinalPathNameByHandle.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
		if n == 0 {
			return "", 0, err
		}
		if int(n) < len(buf) {
			buf = buf[:n]
			break
		}
		if n > 32768 {
			return "", 0, errors.New("path too long")
		}
		buf = make([]uint16, n+1)
	}
	p := syscall.UTF16ToString(buf)
	switch {
	case strings.HasPrefix(p, `\\?\UNC\`):
		p = `\\` + p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\?\`):
		p = p[len(`\\?\`):]
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return "", 0, err
	}
	links := int(info.NumberOfLinks)
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		links = 1
	}
	return p, links, nil
}
