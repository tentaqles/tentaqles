package sense

import (
	"bytes"
	"encoding/json"
	"math"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// codexText is the text of a Codex tool response (S-02 step 2), read whole:
//
//   - a JSON string is the text itself;
//   - a list of parts gives the "text" values of its elements, joined by line breaks;
//   - an object gives its "content", read the same way.
//
// Each form is read by the result-text reader of Hidden (resultPieces), so the two agree on the bytes.
// Unlike Hidden's window, the whole text is kept: it is the error of a failed command as it is. A
// response of any other form is read as plain text, and a response that cannot be read gives "".
func codexText(response json.RawMessage) string {
	v := bytes.TrimSpace(response)
	if len(v) > 0 && v[0] == '{' {
		fields, ok := members(v)
		if !ok {
			return ""
		}
		for _, f := range fields {
			if string(f.key) == "content" {
				return wholeText(f.val)
			}
		}
		return ""
	}
	return wholeText(v)
}

// wholeText is the whole text of one JSON value, read as Hidden reads a result, without the window.
func wholeText(v []byte) string {
	pieces, _ := resultPieces(v)
	return render(pieces, firstSpans(pieces, math.MaxInt))
}

// FromCodexPostTool builds the event of a finished call in Codex (S-02, steps 14 and 15). Codex sends no
// exit code on its post-tool event, so the caller passes the code it found in Codex's session file
// (code, haveCode: codexExit). The response is read here and dropped; no byte of it reaches the event
// except the error text. The cases, in order:
//
//   - Bash with code 1, no output and a grep (GrepFoundNothing): an ok event with Exit 1, as S-01 writes it.
//   - Bash with a code that is not 0: a fail event, Exit the code, the error the whole output text
//     (scrubbed), the full scrubbed input.
//   - Bash with code 0: Hidden reads the output. A hidden failure is a hidden-fail event with Exit 0, the
//     scrubbed error Hidden gives and the full input. Otherwise an ok event with Exit 0, no error and the
//     success input (okInput).
//   - Bash with no code: Hidden reads the output. A hidden failure is a hidden-fail event with no Exit.
//     Otherwise nothing is written and noExit is true, so the caller counts one no-exit drop.
//   - Any other tool (Edit, apply_patch, an MCP tool and the rest): only Hidden's rule A applies, the error
//     flag. A flagged response is a hidden-fail event with no Exit and the full input. An unflagged one
//     writes nothing, with no drop.
//
// tail is the tail of today's event file, read by the caller; tailOK is false when it could not be read.
// The event's host is codex. Its id is the id of the call (eventlog.ID), or, without a call id, the id
// without one (eventlog.IDWithoutCall), as the Claude builders do. write is false for every case that
// writes nothing.
func FromCodexPostTool(c Call, response json.RawMessage, code int, haveCode bool, pl Place, now time.Time,
	tail []eventlog.TailEvent, tailOK bool) (ev eventlog.Event, write bool, noExit bool) {
	tool := toolName(c.Tool)
	if !ShellTool(c.Tool) {
		if errText, hidden := Hidden(c.Tool, c.Input, response); hidden {
			input := scrub.Input(NormalInput(c.Input))
			return codexEvent(c, pl, now, tool, eventlog.KindHiddenFail, nil, input, scrub.Text(errText)), true, false
		}
		return eventlog.Event{}, false, false
	}
	switch {
	case haveCode && code == 1 && GrepFoundNothing(c.Tool, c.Input, codexText(response)):
		// A grep that found nothing is an answer, not a failure, as S-01 writes it: an ok line with exit 1,
		// no error and the success input.
		one := 1
		input := okInput(tool, c.Input, c.Session, tail, tailOK)
		return codexEvent(c, pl, now, tool, eventlog.KindOK, &one, input, scrub.Clean{}), true, false
	case haveCode && code != 0:
		exit := code
		input := scrub.Input(NormalInput(c.Input))
		errOut := scrub.Text(codexText(response))
		return codexEvent(c, pl, now, tool, eventlog.KindFail, &exit, input, errOut), true, false
	case haveCode:
		zero := 0
		if errText, hidden := Hidden(c.Tool, c.Input, response); hidden {
			input := scrub.Input(NormalInput(c.Input))
			return codexEvent(c, pl, now, tool, eventlog.KindHiddenFail, &zero, input, scrub.Text(errText)), true, false
		}
		input := okInput(tool, c.Input, c.Session, tail, tailOK)
		return codexEvent(c, pl, now, tool, eventlog.KindOK, &zero, input, scrub.Clean{}), true, false
	}
	if errText, hidden := Hidden(c.Tool, c.Input, response); hidden {
		input := scrub.Input(NormalInput(c.Input))
		return codexEvent(c, pl, now, tool, eventlog.KindHiddenFail, nil, input, scrub.Text(errText)), true, false
	}
	return eventlog.Event{}, false, true
}

// codexEvent is the event line of one Codex call, with the given kind, exit, input and error. The id is
// the call's id, or the id without one when the call has no id.
func codexEvent(c Call, pl Place, now time.Time, tool, kind string, exit *int, input, errOut scrub.Clean) eventlog.Event {
	var id string
	if c.CallID != "" {
		id = eventlog.ID("codex", c.Session, c.CallID)
	} else {
		id = eventlog.IDWithoutCall("codex", c.Session, tool, kind, input.String(), now.UnixNano())
	}
	return eventlog.Event{
		V:       1,
		ID:      id,
		At:      now.UTC().Format(time.RFC3339),
		Session: c.Session,
		Host:    "codex",
		Tool:    tool,
		Kind:    kind,
		Repo:    pl.Repo,
		Cwd:     pl.Cwd,
		Input:   input,
		Error:   errOut,
		Exit:    exit,
	}
}
