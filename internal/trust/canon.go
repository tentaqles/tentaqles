package trust

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// Key is the record key of a covered path: absolute and cleaned, the
// symlinks of its longest existing prefix resolved (paths.Resolve), forward
// slashes, and lower-cased on Windows, whose file names are
// case-insensitive. The same file reached through another spelling (a
// TQ_HOME typed in another case, C:/ against C:\, a linked folder) is one
// key, so a record signed from one shell verifies from another. A virtual
// "#tq" path keeps its suffix. Opening a key opens the file it names.
func Key(p string) string {
	suffix := ""
	if f, ok := strings.CutSuffix(p, blockSuffix); ok {
		p, suffix = f, blockSuffix
	}
	k := filepath.ToSlash(paths.Resolve(p))
	if runtime.GOOS == "windows" {
		k = strings.ToLower(k)
	}
	return k + suffix
}
