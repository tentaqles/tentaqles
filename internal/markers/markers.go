// Package markers is the one list of environment variables that mark a
// harness or an agent session. It is a leaf: internal/paths (TQ_HOME is
// ignored under a marker, ADR-0002) and internal/humanonly (the env-marker
// detector, ADR-0003) both read it, and neither may import the other.
package markers

import (
	"os"
	"strings"
)

// Markers are the environment variables that mark a harness or an agent
// session: a headless tq run and its children, the Claude Code plugin and
// Claude Code itself, a hub task of an older tq, Codex's sandbox and thread,
// OpenCode and Kimi Code. CLAUDE_CONFIG_DIR and CODEX_HOME are not markers:
// tq sets them in human terminals.
var Markers = []string{
	"TENTAQLES_HEADLESS", "TENTAQLES_HEADLESS_CHILD",
	"CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT",
	"TQ_HUB_TASK",
	"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "CODEX_THREAD_ID",
	"OPENCODE", "KIMI_CODE",
}

// Set returns the first marker set (to a non-blank value) in the
// environment, and whether one is.
func Set() (string, bool) {
	for _, k := range Markers {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return k, true
		}
	}
	return "", false
}
