// Package sense turns one failed tool call of an AI coding agent into an event of a client's log
// and records it. It starts no process, runs no git command and reads no host configuration
// variable: the hook that calls it gives it the payload, the place of the call and the store.
package sense

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// Call is one tool call as a host reported it. The hook fills it from the payload.
type Call struct {
	Session string          // the host's session id; may be empty
	Cwd     string          // the working folder of the call, as the payload gave it
	Tool    string          // the tool's name; may be empty
	CallID  string          // the host's id of the call (tool_use_id); may be empty
	Input   json.RawMessage // the tool input: a JSON object, or a JSON string that holds the object
	Error   string          // the error text
}

// unknownTool is the tool name of a call whose payload gave none.
const unknownTool = "unknown"

// exitPrefix starts the first line of a shell tool's error when the command failed.
const exitPrefix = "Exit code "

// ShellTool reports whether tool is a shell tool: "Bash" or "PowerShell".
func ShellTool(tool string) bool {
	return tool == "Bash" || tool == "PowerShell"
}

// NormalInput returns the tool input as a JSON object when it arrived as a JSON string that holds
// one; any other input is returned as it is. An empty input becomes `{}`.
func NormalInput(raw json.RawMessage) json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return json.RawMessage("{}")
	}
	if t[0] == '"' {
		var s string
		if err := json.Unmarshal(t, &s); err == nil {
			var m map[string]json.RawMessage
			if err := json.Unmarshal([]byte(s), &m); err == nil && m != nil {
				return json.RawMessage(s)
			}
		}
	}
	return raw
}

// FromClaudeFailure builds the event of a failed call in Claude Code.
func FromClaudeFailure(c Call, pl Place, now time.Time) eventlog.Event {
	tool := c.Tool
	if tool == "" {
		tool = unknownTool
	}
	exit, rest := splitExit(c.Error, ShellTool(tool))
	input := scrub.Input(NormalInput(c.Input))
	var id string
	if c.CallID != "" {
		id = eventlog.ID("claude", c.Session, c.CallID)
	} else {
		id = eventlog.IDWithoutCall("claude", c.Session, tool, eventlog.KindFail, input.String(), now.UnixNano())
	}
	return eventlog.Event{
		V:       1,
		ID:      id,
		At:      now.UTC().Format(time.RFC3339),
		Session: c.Session,
		Host:    "claude",
		Tool:    tool,
		Kind:    eventlog.KindFail,
		Repo:    pl.Repo,
		Cwd:     pl.Cwd,
		Input:   input,
		Error:   scrub.Text(rest),
		Exit:    exit,
	}
}

// splitExit takes the exit code off the error of a shell tool: when the first line is "Exit code "
// and digits, optionally with a trailing carriage return, it returns the number and the error
// without that line and its line break. Any other error comes back unchanged with a nil exit.
func splitExit(errText string, shell bool) (*int, string) {
	if !shell {
		return nil, errText
	}
	first, rest, _ := strings.Cut(errText, "\n")
	line := strings.TrimSuffix(first, "\r")
	if !strings.HasPrefix(line, exitPrefix) {
		return nil, errText
	}
	digits := line[len(exitPrefix):]
	if digits == "" || !allDigits(digits) {
		return nil, errText
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return nil, errText
	}
	return &n, rest
}

// allDigits reports whether s is one or more ASCII digits.
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
