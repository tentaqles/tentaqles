package hook

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/lobe/pathcmp"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// loginMatches reports whether the agent's login names the client's own login folder. For host claude
// the login is the CLAUDE_CONFIG_DIR variable; for host codex it is CODEX_HOME. Each host reads only its
// own variable. An unset or empty variable matches nothing. Links are not resolved: the two paths are made
// absolute and compared as they are spelled.
func loginMatches(host, client string) bool {
	var variable string
	switch host {
	case hostClaude:
		variable = "CLAUDE_CONFIG_DIR"
	case hostCodex:
		variable = "CODEX_HOME"
	default:
		return false
	}
	login := os.Getenv(variable)
	if login == "" {
		return false
	}
	return pathcmp.SamePath(login, paths.IdentityDir(client, host))
}
