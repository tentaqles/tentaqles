package sense

import (
	"encoding/json"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/shellwords"
)

// ownProgram is the program name of the memory's own command line: tentaqles, or its alias tq.
const (
	ownProgramLong  = "tentaqles"
	ownProgramShort = "tq"
	ownSubcommand   = "lobe"
	mcpPrefix       = "mcp__"
	ownMCPPrefix    = "lobe_"
)

// IsOwnTool reports whether a call is the memory's own tool use, so it is not recorded (the memory
// must not learn from its own output): a shell call whose command is `tentaqles lobe ...` or `tq
// lobe ...` (see ownShellCommand), or an MCP tool whose own name starts with lobe_ (see ownMCPTool).
// The command is read from the tool input as CallText reads it.
func IsOwnTool(tool string, input json.RawMessage) bool {
	if ShellTool(tool) {
		return ownShellCommand(shellCommand(NormalInput(input)))
	}
	return ownMCPTool(tool)
}

// ownShellCommand reports whether command is the memory's own: its statements, split as the guard
// splits them, are checked after a leading `cd` is dropped. The shell redirections (`2>&1`, `> out`)
// are removed first, so they do not split the command or add a word. The command is own when every
// remaining statement runs tentaqles or tq and its second word is lobe. An empty command, or one with
// no statement left after the cd, is not own.
func ownShellCommand(command string) bool {
	stmts := shellwords.Statements(shellwords.StripRedirects(command))
	if len(stmts) > 0 && shellwords.Program(stmts[0]) == "cd" {
		stmts = stmts[1:]
	}
	if len(stmts) == 0 {
		return false
	}
	for _, st := range stmts {
		prog := shellwords.Program(st)
		if prog != ownProgramLong && prog != ownProgramShort {
			return false
		}
		w := shellwords.CommandWords(st)
		if len(w) < 2 || w[1] != ownSubcommand {
			return false
		}
	}
	return true
}

// ownMCPTool reports whether tool is an MCP tool of the memory: its name is mcp__<server>__<tool>
// and the part after the second `__` starts with lobe_.
func ownMCPTool(tool string) bool {
	if !strings.HasPrefix(tool, mcpPrefix) {
		return false
	}
	rest := tool[len(mcpPrefix):]
	i := strings.Index(rest, "__")
	if i < 0 {
		return false
	}
	return strings.HasPrefix(rest[i+len("__"):], ownMCPPrefix)
}
