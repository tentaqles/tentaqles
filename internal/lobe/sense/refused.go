package sense

import (
	"strings"
	"unicode"
)

// refusedTexts are the texts Claude Code writes for a call that never ran because the person or a
// hook refused it. Each is matched as a prefix of the error, after its leading white space is
// trimmed, case-sensitively. The source is the Trail code's src/trace/run-calls.ts (its REFUSED
// list, the hostRefused function) at commit e02b754c1d4e1de82b841522e2d8cae811702870, the HEAD of
// the checkout that was read. The source names no Claude Code version, so none is recorded.
//
// On the owner's Claude Code (phase 0, 2026-11-09, docs/prd/spikes/phase0-q2-claude-code-hooks.md) a
// declined call fires PermissionDenied and no PostToolUseFailure, and a cancelled tool fires no
// failure event. So this table is a guard for hosts or versions that report a refusal as a failure,
// and it is kept for them.
//
// The source's regular expressions with a variable part (a tool name, a hook name, a command) are
// not in this table, because a prefix cannot express them; their fixed starts are listed where the
// fixed start is enough to tell a refusal.
var refusedTexts = []string{
	// run-calls.ts, REFUSED[0]: the person answered no to the permission prompt.
	"The user doesn't want to proceed with this tool use.",
	// run-calls.ts, REFUSED[1] (the fixed part): a permission was denied for this action or command.
	"Permission for this action was denied",
	"Permission for this command was denied",
	"Permission for this action has been denied",
	"Permission for this command has been denied",
	// run-calls.ts, REFUSED[3]: a PreToolUse hook blocked the call, or no such tool exists.
	"<tool_use_error>Blocked:",
	"<tool_use_error>Error: No such tool available",
	// run-calls.ts, REFUSED[5] (the fixed start only; the source also needs "Refusing to run it"
	// later in the text): the call was refused in a session isolated in a worktree.
	"This session is isolated in the worktree",
	// run-calls.ts, REFUSED[7]: the host asked for permissions the call needs and was not given them.
	"Claude requested permissions to use",
}

// IsRefused reports whether errText starts with one of the refusal texts: the person or a hook
// refused the call, so the tool never ran. Leading white space is ignored.
func IsRefused(errText string) bool {
	t := strings.TrimLeftFunc(errText, unicode.IsSpace)
	for _, p := range refusedTexts {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}
