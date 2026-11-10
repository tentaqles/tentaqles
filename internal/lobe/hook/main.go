// Package hook answers `tentaqles lobe-hook <host> <event>`: a hook of an AI coding agent that
// records a failed tool call in the memory of the client the call belongs to. It fails open. Whatever
// goes wrong it prints nothing and exits 0, and every run is bounded by a cutoff and a watchdog, so
// the hook can never block or slow the agent.
package hook

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/sense"
	"github.com/tentaqles/tentaqles/internal/lobe/settings"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

const (
	hostClaude = "claude"
	noHooksVar = "TQ_LOBE_NO_HOOKS"
)

// The cutoffs of a run and the slack the watchdog adds to them. Tests shorten them.
var (
	captureCutoff = 1 * time.Second
	injectCutoff  = 1500 * time.Millisecond
	watchdogSlack = 250 * time.Millisecond
)

// exitProcess ends the process from the watchdog; tests replace it.
var exitProcess = os.Exit

// afterArm is a test seam: it runs on Main's own goroutine right after the watchdog is armed, so a
// test can make Main stall. Nil in production.
var afterArm func()

// nowFn is the clock of a run; tests may replace it.
var nowFn = time.Now

// extraSteps is a test seam: the steps it returns for an event run after that event's real steps.
// Nil in production.
var extraSteps func(event string) []step

// Main handles a `lobe-hook` run. args are the program's arguments after its name. handled is false
// when the first argument is not "lobe-hook", and the caller goes on as before. When handled is true
// the code is always 0. Nothing is written to stdout in this version; the parameter is kept for later.
func Main(args []string, stdin io.Reader, stdout io.Writer) (handled bool, code int) {
	if len(args) == 0 || args[0] != "lobe-hook" {
		return false, 0
	}
	// From here on the answer is always 0. A panic of Main itself is swallowed by the deferred recover.
	handled, code = true, 0
	defer func() {
		_ = recover()
		handled, code = true, 0
	}()
	_ = stdout
	if len(args) < 3 {
		return true, 0
	}
	host, event := args[1], args[2]
	cutoff := cutoffFor(event)
	watchdog := time.AfterFunc(cutoff+watchdogSlack, func() { exitProcess(0) })
	defer watchdog.Stop()
	if afterArm != nil {
		afterArm()
	}
	if os.Getenv(noHooksVar) == "1" {
		return true, 0
	}
	if host != hostClaude || !knownEvent(event) {
		return true, 0
	}
	steps := stepsFor(event)
	if len(steps) == 0 {
		return true, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), cutoff)
	defer cancel()
	r := &run{host: host, event: event, ctx: ctx, now: nowFn()}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer func() {
			if rec := recover(); rec != nil {
				r.report(classPanic)
			}
		}()
		r.work(stdin, steps)
		r.setDone()
	}()
	timer := time.NewTimer(cutoff)
	defer timer.Stop()
	select {
	case <-finished:
	case <-timer.C:
		r.report(classDeadline)
	}
	return true, 0
}

// run is one lobe-hook run: the call's payload, its folder and client, the open store, and the step
// that is running. The work goroutine sets the fields; Main reads the step and the store when the
// cutoff passes first, so mu guards those.
type run struct {
	host, event string
	ctx         context.Context
	now         time.Time

	// Set by the work goroutine only.
	pl  payload
	cwd string
	ws  *resolve.Workspace
	// The stop event's read: the calls to record, and the cursor to write once they are recorded.
	rejected  []sense.Call
	newOffset int64
	newHead   string
	hasOffset bool

	mu       sync.Mutex
	st       *store.Store
	current  string
	reported bool
	done     bool
}

// work reads the payload, finds the client and its login, opens the store and runs the steps. Any
// failure before the store is open stops the run with nothing written.
func (r *run) work(stdin io.Reader, steps []step) {
	pl, ok := readPayload(stdin)
	if !ok {
		return
	}
	r.pl = pl
	cwd := pl.cwd
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return
		}
		cwd = wd
	}
	r.cwd = cwd
	cfg, err := registry.Load()
	if err != nil {
		return
	}
	res := resolve.Resolve(cwd, cfg)
	if res.Workspace == nil {
		return
	}
	r.ws = res.Workspace
	if !loginMatches(r.host, r.ws.Name) {
		return
	}
	// capture (F-06, S-01 step 2): while it is off, no event reads or writes anything, so this comes
	// before the transcript read, the failure skip and store.Open.
	if !captureOn(r.ws) {
		if r.event == evStop {
			// Nothing from the off period is recovered later: the session's cursor moves to the end of
			// the transcript, so the first stop after capture is on again starts there.
			r.skipToEnd()
		}
		return
	}
	// A call the design skips leaves nothing, not even the client's store folder: the skip decision is
	// made before the store is opened (recordFailure makes the same check, too).
	if r.event == evPostToolUseFailure {
		if _, skip := sense.Skip(callOf(r)); skip {
			return
		}
	}
	// A success (S-03) is skipped for the memory's own tools, and a background start writes nothing: both
	// are decided here, before the store is opened, so the skipped call leaves no store folder either.
	if r.event == evPostToolUse {
		if _, skip := sense.Skip(successCall(r)); skip {
			return
		}
		if sense.BackgroundStart(r.pl.tool, r.pl.toolResponse) {
			return
		}
	}
	// The stop event reads the transcript first, through a read-only store. With no store and nothing
	// to record, the run stops here, so no store folder is created.
	if r.event == evStop {
		ro, ok := r.readRejected()
		if !ok {
			return
		}
		if ro == nil && len(r.rejected) == 0 && len(steps) <= 1 {
			return
		}
	}
	st, err := store.Open(r.ws.Name, store.ReadWrite)
	if err != nil {
		return
	}
	r.mu.Lock()
	r.st = st
	r.mu.Unlock()
	for _, s := range steps {
		r.setCurrent(s.id)
		if err := s.run(r.ctx, r); err != nil {
			r.report(classify(err))
			return
		}
	}
}

// captureOn reads the client's capture setting from the manifest the resolver loaded. A workspace with no
// loaded manifest gives false: the run stops with nothing written. A wrong value reads as off.
func captureOn(ws *resolve.Workspace) bool {
	if ws == nil || ws.Manifest == nil {
		return false
	}
	s, ok := settings.From(ws).Get(settings.Capture)
	return ok && sense.CaptureOn(s.Value)
}

// setCurrent names the step that is running.
func (r *run) setCurrent(id string) {
	r.mu.Lock()
	r.current = id
	r.mu.Unlock()
}

// setDone marks that the work finished normally, so a late cutoff writes nothing.
func (r *run) setDone() {
	r.mu.Lock()
	r.done = true
	r.mu.Unlock()
}

// report writes the run's one error line, unless one was written already, the work is done, or the
// store is not open yet. The work goroutine calls it when a step fails or panics; Main calls it when
// the cutoff passes first.
func (r *run) report(class string) {
	r.mu.Lock()
	if r.reported || r.done || r.st == nil {
		r.mu.Unlock()
		return
	}
	r.reported = true
	st, id := r.st, r.current
	r.mu.Unlock()
	writeError(st, r.host, r.event, id, class, r.now)
}
