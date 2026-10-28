package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	gosync "sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// The engine (spec B §4, §5, §11): one per workspace. A pass pulls the items the tracker changed
// since the watermark into linked cards, turns hub events (lane moves, approvals, pushes) into
// outbox rows, and sends each row under its write policy: auto sends it, ask creates a hub
// permission decision (send / skip), off drops it. A move that sync makes goes through
// Store.MoveCard with the actor "sync" and never starts a run.

// ActorSync is the activity actor of every write sync makes on the board.
const ActorSync = "sync"

var (
	// ErrNoTracker: the manifest has no tracker block.
	ErrNoTracker = errors.New("no tracker block in the workspace manifest")
	// ErrDisabled: sync.enabled is false.
	ErrDisabled = errors.New("board sync is off (sync.enabled = false)")
	// ErrBusy: another process (a serving hub, or tq hub sync now) is syncing this workspace now.
	ErrBusy = errors.New("another tq process is syncing this workspace right now; try again in a minute")
)

const (
	maxTries          = 3                // per call, per tick (spec §7)
	maxBackoff        = 60 * time.Second // a longer Retry-After waits for the next tick
	maxAttempts       = 5                // ticks an outbox row is tried before it is failed
	defaultOutboxPass = 15 * time.Second
	tickTimeout       = 3 * time.Minute
	leaseTTL          = 5 * time.Minute
	approveGrace      = 3 * time.Second // a done move waits this long for the review.approve that may follow
	wakeDebounce      = 500 * time.Millisecond
)

// EngineConfig is what NewEngine needs.
type EngineConfig struct {
	Workspace string
	Store     *store.Store
	Tracker   *manifest.Tracker
	// ExitClasses are the classes outbound text is redacted with (the PII exit rule); "secrets" is
	// always added. Empty = every detector.
	ExitClasses []string
	// Env is the workspace identity environment, for adapters that run a CLI. nil = none.
	Env func() []string
	// Adapter builds the tracker adapter for one pass. nil = the credential chain (cred.Resolve)
	// then the registered factory (New).
	Adapter func(ctx context.Context) (Adapter, error)
	Now     func() time.Time
	// Sleep waits between rate-limited tries; nil = a timer that stops with ctx.
	Sleep func(ctx context.Context, d time.Duration) error
	// OutboxEvery is how often Run sends the outbox between polls (default 15s).
	OutboxEvery time.Duration
	Logf        func(format string, args ...any)
}

// Engine is one workspace's board sync.
type Engine struct {
	cfg      EngineConfig
	st       *store.Store
	provider string
	scope    string
	exit     []string
	holder   string

	mu       gosync.Mutex // one pass at a time in this process
	statuses []Status     // the tracker's statuses, from the last lane-map build
	mapped   bool
	wake     chan struct{}
}

// NewEngine builds the engine; it does not touch the tracker.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.Store == nil {
		return nil, errors.New("sync engine: no store")
	}
	if cfg.Tracker == nil || strings.TrimSpace(cfg.Tracker.Provider) == "" {
		return nil, ErrNoTracker
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepCtx
	}
	if cfg.OutboxEvery <= 0 {
		cfg.OutboxEvery = defaultOutboxPass
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	e := &Engine{cfg: cfg, st: cfg.Store, provider: cfg.Tracker.Provider, scope: cfg.Tracker.ScopeKey(),
		holder: store.NewID("s"), wake: make(chan struct{}, 1)}
	e.exit = exitClasses(cfg.ExitClasses)
	if cfg.Adapter == nil {
		e.cfg.Adapter = e.defaultAdapter
	}
	return e, nil
}

func exitClasses(in []string) []string {
	out := []string{}
	for _, c := range in {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []string{"all"}
	}
	if _, err := redact.Resolve(out); err != nil {
		return []string{"all"} // fail closed
	}
	return append(out, "secrets")
}

func (e *Engine) Provider() string { return e.provider }
func (e *Engine) Scope() string    { return e.scope }

func (e *Engine) defaultAdapter(ctx context.Context) (Adapter, error) {
	c := ConfigFromManifest(e.cfg.Workspace, e.cfg.Tracker)
	if e.cfg.Env != nil {
		c.Env = e.cfg.Env()
	}
	tr, err := cred.Resolve(ctx, c.CredRequest())
	if err != nil {
		return nil, err
	}
	c.Transport = tr
	return New(c)
}

// outbound is the text that may leave the machine: redacted with the exit classes. The
// tq:allow-pii marker exempts nothing on the way out.
func (e *Engine) outbound(s string) string {
	if s == "" {
		return s
	}
	return redact.Redact(strings.ReplaceAll(s, redact.AllowMarker, ""), e.exit)
}

// Report is what one pass did.
type Report struct {
	Provider  string `json:"provider"`
	Scope     string `json:"scope"`
	Pulled    int    `json:"pulled"`    // items the tracker listed
	Created   int    `json:"created"`   // cards created for them
	Updated   int    `json:"updated"`   // linked cards refreshed
	Moved     int    `json:"moved"`     // inbound lane moves
	Conflicts int    `json:"conflicts"` // both sides moved; the tracker won
	Queued    int    `json:"queued"`    // outbox rows queued from hub events
	Sent      int    `json:"sent"`
	Asked     int    `json:"asked"`   // permission decisions created
	Skipped   int    `json:"skipped"` // answered skip, or skipped against a newer remote state
	Dropped   int    `json:"dropped"` // policy off
	Failed    int    `json:"failed"`
	Retried   int    `json:"retried"` // rate-limited tries that were retried
	Watermark string `json:"watermark,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (e *Engine) enabled() bool { return e.st.SyncEnabled(true) }

// Tick is one full pass: hub events to the outbox, the pull, then the outbox send. tq hub sync now
// and the poll loop call it.
func (e *Engine) Tick(ctx context.Context) (Report, error) {
	return e.pass(ctx, true)
}

// Flush is an outbox-only pass (no pull): hub events to the outbox, then the send.
func (e *Engine) Flush(ctx context.Context) (Report, error) {
	return e.pass(ctx, false)
}

func (e *Engine) pass(ctx context.Context, pull bool) (rep Report, err error) {
	rep = Report{Provider: e.provider, Scope: e.scope}
	if !e.enabled() {
		return rep, ErrDisabled
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	ok, err := e.acquire()
	if err != nil {
		return rep, err
	}
	if !ok {
		return rep, ErrBusy
	}
	defer e.release()
	defer func() {
		if p := recover(); p != nil { // a sync bug never takes the hub down
			err = fmt.Errorf("sync pass panicked: %v", p)
			rep.Error = err.Error()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, tickTimeout)
	defer cancel()

	// Hub events first: a local move made before this pull carries the remote state it was made
	// against, so a pull that brings a newer remote state makes the outbound move skip (§11).
	if err := e.produce(&rep); err != nil {
		e.cfg.Logf("sync %s: outbox: %v", e.cfg.Workspace, err)
	}
	var ad Adapter
	adapter := func() (Adapter, error) {
		if ad != nil {
			return ad, nil
		}
		a, err := e.cfg.Adapter(ctx)
		if err != nil {
			return nil, err
		}
		ad = a
		return ad, nil
	}
	var pullErr error
	if pull {
		pullErr = e.pull(ctx, adapter, &rep)
		if pullErr != nil {
			rep.Error = pullErr.Error()
			_ = e.st.RecordSyncError(e.provider, e.scope, pullErr.Error())
		}
	}
	sendErr := e.send(ctx, adapter, &rep)
	if sendErr != nil && pullErr == nil {
		rep.Error = sendErr.Error()
		_ = e.st.RecordSyncError(e.provider, e.scope, sendErr.Error())
	}
	if pullErr != nil {
		return rep, pullErr
	}
	return rep, sendErr
}

// Wake asks Run for an outbox pass soon (a hub event happened in this process). It never blocks.
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Interval is the poll interval from the settings.
func (e *Engine) Interval() time.Duration { return time.Duration(e.st.SyncInterval()) * time.Second }

// Run is the poll loop: a full pass at once and every sync.interval, an outbox pass every
// OutboxEvery and soon after a store change in this process. It returns when ctx ends or the store
// closes. Errors are recorded in sync_state, never returned.
func (e *Engine) Run(ctx context.Context) {
	changes, unsub := e.st.Subscribe()
	defer unsub()
	poll := time.NewTimer(0)
	defer poll.Stop()
	outbox := time.NewTicker(e.cfg.OutboxEvery)
	defer outbox.Stop()
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	defer debounce.Stop()
	soon := func() { debounce.Reset(wakeDebounce) }
	quiet := func(rep Report, err error) {
		if err != nil && !errors.Is(err, ErrDisabled) && !errors.Is(err, ErrBusy) && !errors.Is(err, context.Canceled) {
			e.cfg.Logf("sync %s: %v", e.cfg.Workspace, err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-changes:
			if !ok {
				return
			}
			switch c.Kind {
			case "activity", "decision", "outbox":
				soon()
			}
		case <-e.wake:
			soon()
		case <-debounce.C:
			quiet(e.Flush(ctx))
		case <-outbox.C:
			quiet(e.Flush(ctx))
		case <-poll.C:
			quiet(e.Tick(ctx))
			poll.Reset(e.Interval())
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retry calls fn up to maxTries times while the tracker rate-limits, honouring Retry-After and
// otherwise backing off exponentially (1s, 2s). A Retry-After beyond maxBackoff waits for the
// next tick instead.
func retry[T any](ctx context.Context, e *Engine, rep *Report, fn func() (T, error)) (T, error) {
	var zero T
	for i := 0; ; i++ {
		v, err := fn()
		var rl *RateLimitError
		if err == nil || !errors.As(err, &rl) || i == maxTries-1 {
			return v, err
		}
		d := rl.RetryAfter
		if d <= 0 {
			d = time.Second << i
		}
		if d > maxBackoff {
			return v, err
		}
		rep.Retried++
		if serr := e.cfg.Sleep(ctx, d); serr != nil {
			return zero, serr
		}
	}
}

func isRateLimit(err error) bool {
	var rl *RateLimitError
	return errors.As(err, &rl)
}
