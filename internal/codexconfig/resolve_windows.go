//go:build windows

package codexconfig

import (
	"strings"

	"golang.org/x/sys/windows"
)

// resolveFinal returns the final path of an existing file, with symlinks and junctions
// resolved, the way Rust's fs::canonicalize (which Codex uses) resolves it. The \\?\
// prefix is dropped, as Codex does in its hook keys.
func resolveFinal(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			s := windows.UTF16ToString(buf[:n])
			switch {
			case strings.HasPrefix(s, `\\?\UNC\`):
				return `\\` + s[len(`\\?\UNC\`):], nil
			case strings.HasPrefix(s, `\\?\`):
				return s[len(`\\?\`):], nil
			}
			return s, nil
		}
		buf = make([]uint16, n+1)
	}
}
