package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

type step func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error)

type fakeAdapter struct {
	mu    sync.Mutex
	reqs  []adapter.RunRequest
	steps []step
}

func (f *fakeAdapter) Start(ctx context.Context, req adapter.RunRequest, log io.Writer) (adapter.RunResult, error) {
	f.mu.Lock()
	i := len(f.reqs)
	f.reqs = append(f.reqs, req)
	var s step
	if i < len(f.steps) {
		s = f.steps[i]
	} else if len(f.steps) > 0 {
		s = f.steps[len(f.steps)-1]
	}
	f.mu.Unlock()
	fmt.Fprintf(log, "fake round %d\n", i)
	if s == nil {
		return adapter.RunResult{StopReason: "success", CostKnown: true}, nil
	}
	return s(ctx, req)
}

func (f *fakeAdapter) calls() []adapter.RunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]adapter.RunRequest(nil), f.reqs...)
}

type fakeGate struct {
	mu        sync.Mutex
	states    map[string]*gate.State
	slugs     map[string][]string // dir → slugs, for discovery
	answers   []string
	answerErr error
}

func newFakeGate() *fakeGate {
	return &fakeGate{states: map[string]*gate.State{}, slugs: map[string][]string{}}
}

func (g *fakeGate) set(slug string, st *gate.State) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cp := *st
	cp.Slug = slug
	g.states[slug] = &cp
}

func (g *fakeGate) State(_ context.Context, dir, slug string, _ bool) (*gate.State, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.states[slug]
	if !ok {
		return nil, &gate.GateError{Code: 1, Msg: "no plan at docs/checkpoints/" + slug + ".json"}
	}
	cp := *st
	cp.Raw, _ = json.Marshal(st)
	return &cp, nil
}

func (g *fakeGate) Answer(_ context.Context, dir, slug, id, choice, note, actor string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answers = append(g.answers, strings.Join([]string{slug, id, choice, actor}, " "))
	if g.answerErr != nil {
		return "", g.answerErr
	}
	if !g.resolve(slug, id, choice, note, actor) {
		return "", &gate.GateError{Code: 1, Msg: "no pending decision '" + id + "' in '" + slug + "'."}
	}
	return "decision " + id + ": " + choice + " by " + actor, nil
}

// resolve moves a pending decision to the answered list, as the real gate does. Caller holds g.mu.
func (g *fakeGate) resolve(slug, id, choice, note, actor string) bool {
	st, ok := g.states[slug]
	if !ok {
		return false
	}
	found := false
	var keep []gate.Decision
	for _, p := range st.PendingDecisions {
		if p.ID == id {
			found = true
			st.Decisions = append(st.Decisions, gate.Decision{ID: p.ID, Kind: p.Kind, Options: p.Options, Status: "answered",
				Choice: choice, Note: note, Actor: actor, AnsweredAt: "2026-10-21T12:00:00+00:00"})
			if p.Kind == "result" && choice == "approve" {
				st.Finished = true
			}
			continue
		}
		keep = append(keep, p)
	}
	st.PendingDecisions = keep
	return found
}

// terminalAnswer is the human answering in their own terminal with build-gate answer.
func (g *fakeGate) terminalAnswer(slug, id, choice string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resolve(slug, id, choice, "", "terminal")
}

func (g *fakeGate) Slugs(dir string, _ time.Time) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.slugs[dir]
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type harness struct {
	t     *testing.T
	st    *store.Store
	svc   *Service
	ad    *fakeAdapter
	gt    *fakeGate
	clock *fakeClock
	root  string
	mu    sync.Mutex
	notes []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		t.Setenv(k, "")
	}
	root := testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "apps", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &harness{t: t, st: st, ad: &fakeAdapter{}, gt: newFakeGate(), root: root,
		clock: &fakeClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}}
	svc, err := New(Config{Workspace: "acme", Root: root, Store: st, Gate: h.gt,
		Adapters: map[string]adapter.Adapter{store.AdapterClaude: h.ad},
		Env: func() []string {
			return []string{"PATH=/bin", "TQ_BUILD_TOKEN=leak", "TQ_BUILD_ACTOR=hub:x", "TENTAQLES_HEADLESS_CHILD=1"}
		},
		Notify: func(_ context.Context, title, body string) error {
			h.mu.Lock()
			h.notes = append(h.notes, title+": "+body)
			h.mu.Unlock()
			return nil
		},
		Actor: "hub:tester", Now: h.clock.Now, RetryDelay: 2 * time.Minute, PollEvery: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	t.Cleanup(svc.Wait)
	return h
}

func (h *harness) card(kind string, tags ...string) store.Card {
	h.t.Helper()
	c, err := h.st.AddCard(store.NewCard{Title: "Export invoices", Body: "CSV per month.", Kind: kind, Tags: tags, ProjectDir: "apps/billing"}, "renato")
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func (h *harness) enqueue(c store.Card) store.Run {
	h.t.Helper()
	r, _, err := h.svc.Enqueue(context.Background(), c.ID, "test")
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *harness) tick() {
	h.t.Helper()
	if err := h.svc.Tick(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	h.svc.Wait()
}

func (h *harness) run(id string) store.Run {
	h.t.Helper()
	r, err := h.st.Run(id)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *harness) comments(cardID string) string {
	cs, _ := h.st.Comments(cardID)
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.AuthorKind + ": " + c.Body + "\n")
	}
	return b.String()
}

func (h *harness) notifications() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.notes...)
}

func result(text string) step {
	return func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{StopReason: "success", Transcript: text, CostKnown: true, SessionID: "s1"}, nil
	}
}

func withState(g *fakeGate, slug string, st *gate.State, then step) step {
	return func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		g.set(slug, st)
		return then(ctx, req)
	}
}
