package bundle

import (
	"path/filepath"
	"strings"
)

// validName reports whether s is safe to use as a single path segment
// (skill name) or JSON object key (MCP server name): non-empty, contains no
// path separators, isn't "." or "..", equals its own filepath.Base, and
// contains no control characters.
func validName(s string) bool {
	if s == "" {
		return false
	}
	if s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, "/\\") {
		return false
	}
	if filepath.Base(s) != s {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
