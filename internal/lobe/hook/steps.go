package hook

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/sense"
)

// step is one step of an event. A step that returns an error ends the run.
type step struct {
	id  string
	run func(ctx context.Context, r *run) error
}

// The events with real steps in this version.
const (
	evPostToolUseFailure = "post-tool-use-failure"
	evPostToolUse        = "post-tool-use"
)

// knownEvent reports whether event is one of the events of host's hook contract: Claude Code's seven, or
// Codex's six (no post-tool-use-failure). A host that is neither has no events.
func knownEvent(host, event string) bool {
	switch host {
	case hostClaude:
		switch event {
		case "session-start", "user-prompt-submit", "pre-tool-use", "post-tool-use", evPostToolUseFailure,
			"stop", "session-end":
			return true
		}
	case hostCodex:
		switch event {
		case "session-start", "user-prompt-submit", "pre-tool-use", "post-tool-use", "stop", "session-end":
			return true
		}
	}
	return false
}

// cutoffFor is the cutoff of an event. An unknown event uses the capture cutoff.
func cutoffFor(event string) time.Duration {
	switch event {
	case "session-start", "user-prompt-submit", "pre-tool-use", "post-tool-use", evPostToolUseFailure:
		return injectCutoff
	case "stop", "session-end":
		return captureCutoff
	}
	return captureCutoff
}

// stepsFor is the steps of an event of host: its real steps, then the test seam's steps. Codex has one
// step, on post-tool-use (S-02); its other events have none, so the stop step (recordRejected, which reads
// Claude Code's transcript) never runs for Codex.
func stepsFor(host, event string) []step {
	var out []step
	if host == hostCodex {
		if event == evPostToolUse {
			out = append(out, step{id: "S-02", run: recordCodexPost})
		}
		if extraSteps != nil {
			out = append(out, extraSteps(event)...)
		}
		return out
	}
	switch event {
	case evPostToolUseFailure:
		out = append(out, step{id: "S-01", run: recordFailure})
	case evPostToolUse:
		out = append(out, step{id: "S-03", run: recordSuccess})
	case evStop:
		out = append(out, step{id: "S-01R", run: recordRejected})
	}
	if extraSteps != nil {
		out = append(out, extraSteps(event)...)
	}
	return out
}

// callOf is the sense.Call of the run's payload and folder.
func callOf(r *run) sense.Call {
	return sense.Call{
		Session:     r.pl.session,
		Cwd:         r.cwd,
		Tool:        r.pl.tool,
		CallID:      r.pl.toolUseID,
		Input:       r.pl.toolInput,
		Error:       r.pl.errText,
		IsInterrupt: r.pl.interrupt,
	}
}

// recordFailure (S-01) records a failed call of any tool: Bash, PowerShell, Edit, Write, an MCP tool
// and the rest. The order is fixed. A call the person interrupted, a call that was refused (the tool
// never ran) and the memory's own tool are not recorded: sense.Skip says which, and the step returns
// nil with nothing written. The work calls the same check before it opens the store, so a skipped
// call leaves no store folder either; this check stays here so the step is right on its own. Any
// other call goes through sense.Locate, sense.FromClaudeFailure and sense.Record; there a grep that
// found nothing may become an ok line (the grep rule), and the shell rule (the exit code) applies to
// the shell tools only.
func recordFailure(ctx context.Context, r *run) error {
	call := callOf(r)
	if _, skip := sense.Skip(call); skip {
		return nil
	}
	// The tail is read only for a grep that found nothing: the one failure written as ok, whose input
	// follows the success rule (S-03 step 5). Any other failure keeps its full input and needs no tail.
	var tail []eventlog.TailEvent
	tailOK := false
	if sense.GrepFoundNothing(call.Tool, call.Input, call.Error) {
		tail, tailOK = r.todayTail()
	}
	place := sense.Locate(r.ws.Root, r.cwd)
	ev := sense.FromClaudeFailure(call, place, r.now, tail, tailOK)
	_, err := sense.Record(ctx, r.st, ev, call)
	return err
}

// recordCodexPost (S-02) records a finished call in Codex. The skip rules are the memory's own tools only
// (the work has already applied them before the store opened). For the shell tool the exit code is read
// from Codex's session file, through the login the run accepted (CODEX_HOME); any other tool, and a shell
// call with no code found, has none. sense.FromCodexPostTool decides what is written. A call with no code
// that writes nothing counts one no-exit drop. The response is dropped after use.
func recordCodexPost(ctx context.Context, r *run) error {
	call := successCall(r)
	if _, skip := sense.Skip(call); skip {
		return nil
	}
	code, have := 0, false
	if sense.ShellTool(r.pl.tool) {
		code, have = codexExit(os.Getenv("CODEX_HOME"), r.pl.transcript, r.pl.toolUseID)
	}
	tail, tailOK := r.todayTail()
	place := sense.Locate(r.ws.Root, r.cwd)
	ev, write, noExit := sense.FromCodexPostTool(call, r.pl.toolResponse, code, have, place, r.now, tail, tailOK)
	// The response is dropped here: no later step of this run reads it.
	r.pl.toolResponse = nil
	if noExit {
		// A lost drop costs one count, so its error is ignored.
		_ = eventlog.AppendDrop(ctx, r.st, eventlog.DropNoExit, r.now)
	}
	if !write {
		return nil
	}
	_, err := sense.Record(ctx, r.st, ev, call)
	return err
}

// recordSuccess (S-03) records a successful call of any tool. The skip rules of a success are the
// memory's own tools only: an interrupt and a refusal are failures, and the call here has no error. The
// work already left a background start and the skipped calls before the store was opened, so this step
// meets only what it records. Its order is fixed by sense.FromClaudePostTool: the background check, the
// drop of the response, the projection or the full input, then the scrub (in Record).
func recordSuccess(ctx context.Context, r *run) error {
	call := successCall(r)
	if _, skip := sense.Skip(call); skip {
		return nil
	}
	tail, tailOK := r.todayTail()
	place := sense.Locate(r.ws.Root, r.cwd)
	ev, ok := sense.FromClaudePostTool(call, r.pl.toolResponse, place, r.now, tail, tailOK)
	// The response is dropped here: no later step of this run reads it.
	r.pl.toolResponse = nil
	if !ok {
		return nil
	}
	_, err := sense.Record(ctx, r.st, ev, call)
	return err
}

// successCall is the sense.Call of a success: the payload's tool, input and folder, with no error and
// no interrupt flag, which are failure fields.
func successCall(r *run) sense.Call {
	return sense.Call{
		Session: r.pl.session,
		Cwd:     r.cwd,
		Tool:    r.pl.tool,
		CallID:  r.pl.toolUseID,
		Input:   r.pl.toolInput,
	}
}

// todayTail reads the tail of today's event file (UTC, at the run's time) without the store lock, for
// the full-input exception (S-03 step 5). ok is false when the file could not be read or the store is not
// open; the caller then keeps the full input. A missing file is an empty tail, which is ok.
func (r *run) todayTail() (tail []eventlog.TailEvent, ok bool) {
	if r.st == nil {
		return nil, false
	}
	file, err := eventlog.FileFor(r.now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, false
	}
	tail, err = eventlog.TailEvents(filepath.Join(r.st.Dir(), filepath.FromSlash(file)), eventlog.EventTailBytes)
	if err != nil {
		return nil, false
	}
	return tail, true
}
