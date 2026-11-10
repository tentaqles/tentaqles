package sense

import (
	"encoding/json"
	"strings"
	"unicode"
)

// backgroundStarts are the texts Claude Code writes for a shell command that went to the background,
// as a prefix of the result (after its leading white space is trimmed). The source is the Trail code's
// src/trace/run-calls.ts (its BACKGROUNDED pattern, the wentToBackground function) at commit
// e02b754c1d4e1de82b841522e2d8cae811702870, the checkout that was read. Seen on Claude Code v2.1.321
// (owner's capture, 2026-11-10).
//
// The first text is the start of a run_in_background command. The second is a run past its timeout,
// "Command did not complete within its <n>ms timeout and was moved to the background" (or <n>s), which
// is matched as its start, the number, the unit and backgroundTimeoutRest.
var backgroundStarts = []string{
	"Command running in background with ID: ",
	"Command did not complete within its ",
}

// backgroundTimeoutRest is the end of the timeout text, after its number and its unit.
const backgroundTimeoutRest = "timeout and was moved to the background"

// backgroundTaskField is the field of a shell tool's object result that names the background task. The
// owner's capture (Claude Code, 2026-11-10) showed a run_in_background Bash call's PostToolUse result as
// an object with stdout, stderr, interrupted, isImage, noOutputExpected and this field, and none of the
// texts above: a non-empty string here is what marks the start.
const backgroundTaskField = "backgroundTaskId"

// BackgroundStart reports whether a shell call's result only says the command went to the background:
// it started, it did not yet work. Only the shell tools are checked. A result is a JSON string, or an
// object: an object with a non-empty string backgroundTaskId is a start, and so is one whose string
// fields hold one of the texts (each string field is looked at). Any other result is not a background
// start.
func BackgroundStart(tool string, response json.RawMessage) bool {
	if !ShellTool(tool) || len(response) == 0 {
		return false
	}
	var s string
	if err := json.Unmarshal(response, &s); err == nil {
		return isBackgroundText(s)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(response, &m); err != nil {
		return false
	}
	if v, ok := m[backgroundTaskField]; ok {
		var id string
		if err := json.Unmarshal(v, &id); err == nil && strings.TrimSpace(id) != "" {
			return true
		}
	}
	for _, v := range m {
		var field string
		if err := json.Unmarshal(v, &field); err == nil && isBackgroundText(field) {
			return true
		}
	}
	return false
}

// isBackgroundText reports whether text starts with one of the background texts.
func isBackgroundText(text string) bool {
	t := strings.TrimLeftFunc(text, unicode.IsSpace)
	if strings.HasPrefix(t, backgroundStarts[0]) {
		return true
	}
	rest, ok := strings.CutPrefix(t, backgroundStarts[1])
	if !ok {
		return false
	}
	// The number, then the unit: "s" or "ms" (the source's \d+m?s).
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return false
	}
	rest = strings.TrimPrefix(rest[i:], "m")
	return strings.HasPrefix(rest, "s "+backgroundTimeoutRest)
}
