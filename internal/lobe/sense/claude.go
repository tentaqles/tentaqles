// Package sense turns one failed tool call of an AI coding agent into an event of a client's log
// and records it. It starts no process, runs no git command and reads no host configuration
// variable: the hook that calls it gives it the payload, the place of the call and the store.
package sense

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/shellwords"
)

// Call is one tool call as a host reported it. The hook fills it from the payload.
type Call struct {
	Session string          // the host's session id; may be empty
	Cwd     string          // the working folder of the call, as the payload gave it
	Tool    string          // the tool's name; may be empty
	CallID  string          // the host's id of the call (tool_use_id); may be empty
	Input   json.RawMessage // the tool input: a JSON object, or a JSON string that holds the object
	Error   string          // the error text
	// IsInterrupt is the payload's is_interrupt: the person stopped the call while it ran.
	IsInterrupt bool
}

// unknownTool is the tool name of a call whose payload gave none.
const unknownTool = "unknown"

// exitPrefix starts the first line of a shell tool's error when the command failed.
const exitPrefix = "Exit code "

// Skip reports whether a failed call is not recorded at all, and why: "interrupt", "refused" or
// "own". An empty reason means the call is recorded. The rules apply in that order: an interrupted
// call, a refused call (IsRefused on the error), then the memory's own tool (IsOwnTool).
func Skip(c Call) (reason string, skip bool) {
	switch {
	case c.IsInterrupt:
		return "interrupt", true
	case IsRefused(c.Error):
		return "refused", true
	case IsOwnTool(c.Tool, c.Input):
		return "own", true
	}
	return "", false
}

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

// FromClaudeFailure builds the event of a failed call in Claude Code. Every tool is recorded: the
// shell tools (Bash, PowerShell) have their "Exit code N" first line taken off into Exit; for any
// other tool (Edit, Write, MultiEdit, NotebookEdit, Read, Grep, Glob, WebFetch, WebSearch,
// mcp__<server>__<tool> and the rest) the error is stored whole, after scrubbing, and Exit stays nil.
// The input is the scrubbed tool input as a JSON object, whatever the tool; an empty tool name is
// recorded as "unknown".
//
// A grep that found nothing is written as ok (GrepFoundNothing). Its input is then the success form
// (okInput): the projection, unless the full-input exception applies (KeepsFullInput). tail is the tail
// of today's event file, read by the caller; tailOK is false when it could not be read, and the input
// is then kept in full. A failure's own input is always the full one.
func FromClaudeFailure(c Call, pl Place, now time.Time, tail []eventlog.TailEvent, tailOK bool) eventlog.Event {
	tool := toolName(c.Tool)
	exit, rest := splitExit(c.Error, ShellTool(tool))
	kind := eventlog.KindFail
	var input scrub.Clean
	if GrepFoundNothing(tool, c.Input, c.Error) {
		// A grep that found nothing is an answer, not a failure: the line is written as ok, with no
		// error, and the exit code 1 when the error stated it.
		kind, rest, exit = eventlog.KindOK, "", nil
		if strings.TrimSpace(c.Error) != "" {
			one := 1
			exit = &one
		}
		input = okInput(tool, c.Input, c.Session, tail, tailOK)
	} else {
		input = scrub.Input(NormalInput(c.Input))
	}
	var id string
	if c.CallID != "" {
		id = eventlog.ID("claude", c.Session, c.CallID)
	} else {
		id = eventlog.IDWithoutCall("claude", c.Session, tool, kind, input.String(), now.UnixNano())
	}
	return eventlog.Event{
		V:       1,
		ID:      id,
		At:      now.UTC().Format(time.RFC3339),
		Session: c.Session,
		Host:    "claude",
		Tool:    tool,
		Kind:    kind,
		Repo:    pl.Repo,
		Cwd:     pl.Cwd,
		Input:   input,
		Error:   scrub.Text(rest),
		Exit:    exit,
	}
}

// FromClaudePostTool builds the event of a successful call in Claude Code (S-03, S-04). The steps run in
// this order; the caller has already applied the skip rules of a success (the own tools, see the hook's
// recordSuccess):
//
//  1. Background start: when the result only says the command went to the background
//     (BackgroundStart), no event is built and the second result is false.
//  2. Hidden failure (S-04): when Hidden says the call failed although the host marked it a success,
//     the event has kind hidden-fail, the full scrubbed input, and the scrubbed error that Hidden gives.
//     Its exit stays nil.
//  3. The result is dropped: nothing below reads response, and no byte of it reaches an event.
//  4. The input: the scrubbed full input when the full-input exception applies (KeepsFullInput),
//     otherwise the scrubbed projection (Project). Both go through scrub.Input, so a success and a
//     failure of one object input have the same bytes.
//
// tail is the tail of today's event file, read by the caller before the call; tailOK is false when it
// could not be read. A success event has kind ok, host claude, no error, no fingerprint and no exit: a
// PostToolUse payload states no exit code. A hidden-fail event has no fingerprint yet; Record gives it
// one. id, at, repo and cwd are as for a failure.
func FromClaudePostTool(c Call, response json.RawMessage, pl Place, now time.Time, tail []eventlog.TailEvent, tailOK bool) (eventlog.Event, bool) {
	if BackgroundStart(c.Tool, response) {
		return eventlog.Event{}, false
	}
	errText, hidden := Hidden(c.Tool, c.Input, response)
	response = nil
	tool := toolName(c.Tool)
	kind := eventlog.KindOK
	var input scrub.Clean
	var errOut scrub.Clean
	if hidden {
		kind = eventlog.KindHiddenFail
		input = scrub.Input(NormalInput(c.Input))
		errOut = scrub.Text(errText)
	} else {
		input = okInput(tool, c.Input, c.Session, tail, tailOK)
	}
	var id string
	if c.CallID != "" {
		id = eventlog.ID("claude", c.Session, c.CallID)
	} else {
		id = eventlog.IDWithoutCall("claude", c.Session, tool, kind, input.String(), now.UnixNano())
	}
	return eventlog.Event{
		V:       1,
		ID:      id,
		At:      now.UTC().Format(time.RFC3339),
		Session: c.Session,
		Host:    "claude",
		Tool:    tool,
		Kind:    kind,
		Repo:    pl.Repo,
		Cwd:     pl.Cwd,
		Input:   input,
		Error:   errOut,
	}, true
}

// toolName is the tool's name, or unknownTool when the payload gave none.
func toolName(tool string) string {
	if tool == "" {
		return unknownTool
	}
	return tool
}

// okInput is the input of an ok event: the scrubbed full input when KeepsFullInput says so, else the
// scrubbed projection. Project reads the input as a JSON object itself; scrub.Input then writes both
// forms in one encoding.
func okInput(tool string, input json.RawMessage, session string, tail []eventlog.TailEvent, tailOK bool) scrub.Clean {
	if KeepsFullInput(session, tool, tail, tailOK) {
		return scrub.Input(NormalInput(input))
	}
	return scrub.Input(Project(tool, input))
}

// KeepsFullInput is the exception of S-03 step 5 (decision A-18): a success keeps its full scrubbed input
// when the tail of today's event file holds an earlier failure of the same session and tool, a fail or a
// hidden-fail (S-04). A reference line counts, since it carries those fields. A tail that could not be read keeps only the
// projection (owner, 2026-11-10: otherwise every success, a Write's whole content included, would be
// stored in full whenever the log's end is unreadable; that one call then loses only its pairing). An
// empty session never matches.
func KeepsFullInput(session, tool string, tail []eventlog.TailEvent, tailOK bool) bool {
	if !tailOK {
		return false
	}
	if session == "" {
		return false
	}
	for _, e := range tail {
		if (e.Kind == eventlog.KindFail || e.Kind == eventlog.KindHiddenFail) && e.Session == session && e.Tool == tool {
			return true
		}
	}
	return false
}

// grepPrograms are the programs whose exit code 1 means "nothing matched".
var grepPrograms = []string{"grep", "egrep", "fgrep", "zgrep", "rg", "ag", "ack"}

// GrepFoundNothing reports whether a failed call is a grep that found nothing: a shell call whose
// error, trimmed, is empty or exactly "Exit code 1", and whose first statement that is not a cd
// runs a grep program, or git with grep as its next word. The shell redirections are removed first
// (shellwords.StripRedirects). The rule looks at the first statement that is not a cd, so
// `grep x f; npm test` with `Exit code 1` is written as ok although the second statement may be what
// failed; this is the design's stated rule and a known narrow misclassification.
func GrepFoundNothing(tool string, input json.RawMessage, errText string) bool {
	if !ShellTool(tool) {
		return false
	}
	e := strings.TrimSpace(errText)
	if e != "" && e != exitPrefix+"1" {
		return false
	}
	for _, st := range shellwords.Statements(shellwords.StripRedirects(shellCommand(NormalInput(input)))) {
		prog := shellwords.Program(st)
		if prog == "cd" {
			continue
		}
		if slices.Contains(grepPrograms, prog) {
			return true
		}
		w := shellwords.CommandWords(st)
		return prog == "git" && len(w) > 1 && w[1] == "grep"
	}
	return false
}

// splitExit takes the exit code off the error of a shell tool: when the first line is "Exit code "
// and digits, optionally with a trailing carriage return, it returns the number and the error
// without that line and its line break. Any other error comes back unchanged with a nil exit. A
// tool that is not a shell tool never has an exit code: its error is returned whole, even when it
// starts with "Exit code ".
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
