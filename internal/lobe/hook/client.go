package hook

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// loginMatches reports whether the agent's login names the client's own login folder. For host claude
// the login is the CLAUDE_CONFIG_DIR variable; an unset or empty variable matches nothing. Links are
// not resolved: the two paths are made absolute and compared as they are spelled.
func loginMatches(host, client string) bool {
	if host != hostClaude {
		return false
	}
	login := os.Getenv("CLAUDE_CONFIG_DIR")
	if login == "" {
		return false
	}
	return samePath(login, paths.IdentityDir(client, "claude"))
}

// samePath compares two folders whole: case-insensitively on Windows, exactly elsewhere.
func samePath(a, b string) bool {
	x, y := absPath(a), absPath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(x, y)
	}
	return x == y
}

// absPath is the absolute, cleaned form of p, which drops a trailing separator. On Windows a slash
// is a separator too. Nothing is resolved through links.
func absPath(p string) string {
	if runtime.GOOS == "windows" {
		p = filepath.FromSlash(p)
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}
