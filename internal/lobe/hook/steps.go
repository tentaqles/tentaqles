package hook

import (
	"context"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/sense"
)

// step is one step of an event. A step that returns an error ends the run.
type step struct {
	id  string
	run func(ctx context.Context, r *run) error
}

// evPostToolUseFailure is the one event with real steps in this version.
const evPostToolUseFailure = "post-tool-use-failure"

// knownEvent reports whether event is one of the seven events of the hook contract.
func knownEvent(event string) bool {
	switch event {
	case "session-start", "user-prompt-submit", "pre-tool-use", "post-tool-use", evPostToolUseFailure,
		"stop", "session-end":
		return true
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

// stepsFor is the steps of an event: its real steps, then the test seam's steps.
func stepsFor(event string) []step {
	var out []step
	if event == evPostToolUseFailure {
		out = append(out, step{id: "S-01", run: recordFailure})
	}
	if extraSteps != nil {
		out = append(out, extraSteps(event)...)
	}
	return out
}

// recordFailure (S-01) records a failed call. Only shell tools are captured in this version.
func recordFailure(ctx context.Context, r *run) error {
	if !sense.ShellTool(r.pl.tool) {
		return nil
	}
	call := sense.Call{
		Session: r.pl.session,
		Cwd:     r.cwd,
		Tool:    r.pl.tool,
		CallID:  r.pl.toolUseID,
		Input:   r.pl.toolInput,
		Error:   r.pl.errText,
	}
	place := sense.Locate(r.ws.Root, r.cwd)
	ev := sense.FromClaudeFailure(call, place, r.now)
	_, err := sense.Record(ctx, r.st, ev, call)
	return err
}
