//go:build windows

package cmd

import "golang.org/x/sys/windows"

// shortPath is the 8.3 form of an existing path, or "" when the volume has none.
func shortPath(p string) string {
	from, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return ""
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetShortPathName(from, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
