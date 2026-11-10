// Package fingerprint gives a failed tool call a short id for "the same error" (S-08). Every function here is pure:
// no store, no clock, no disk, and no work at start-up.
package fingerprint

import "strings"

// Class returns the tool class of S-08 step 1 for a tool name: shell, edit, read, web, mcp:<server>, or other.
// The name is matched exactly; a name that starts mcp__ with no second __ after the server is other.
func Class(tool string) string {
	switch tool {
	case "Bash", "PowerShell":
		return "shell"
	case "Edit", "Write", "MultiEdit", "NotebookEdit", "apply_patch":
		return "edit"
	case "Read", "Grep", "Glob":
		return "read"
	case "WebFetch", "WebSearch":
		return "web"
	}
	if rest, ok := strings.CutPrefix(tool, "mcp__"); ok {
		if server, _, found := strings.Cut(rest, "__"); found && server != "" {
			return "mcp:" + server
		}
	}
	return "other"
}
