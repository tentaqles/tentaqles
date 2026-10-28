package web

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

type Entry struct {
	Info WorkspaceInfo
	WS   *resolve.Workspace
}

type Factory func(ws *resolve.Workspace) (*runner.Service, error)

// Sibling builds a workspace's companion loop (the board sync engine, spec B §4) when its runner
// opens. It is called once, before the runner starts, and returns the loop to run beside
// svc.Start (nil = none). The loop stops with the runner, before the store closes.
type Sibling func(ws *resolve.Workspace, svc *runner.Service) func(ctx context.Context)

// MultiBackend serves several workspaces from one process (spec §5): one store and one runner per
// workspace, opened on first use; an unavailable workspace is listed and never opened.
type MultiBackend struct {
	ctx     context.Context
	cancel  context.CancelFunc
	entries []Entry
	factory Factory
	sibling Sibling
	mu      sync.Mutex
	closed  bool
	svcs    map[string]*runner.Service
	done    map[string]chan struct{}
	opening map[string]*opening
}

// opening is a first open in flight: later callers for the same workspace wait on done outside
// the lock, so a slow open of one workspace never blocks the others and no runner starts twice.
type opening struct {
	done chan struct{}
	svc  *runner.Service
	err  error
}

func NewMultiBackend(ctx context.Context, entries []Entry, f Factory) *MultiBackend {
	cctx, cancel := context.WithCancel(ctx)
	es := append([]Entry(nil), entries...)
	sort.Slice(es, func(i, j int) bool { return es[i].Info.Name < es[j].Info.Name })
	return &MultiBackend{ctx: cctx, cancel: cancel, entries: es, factory: f, svcs: map[string]*runner.Service{},
		done: map[string]chan struct{}{}, opening: map[string]*opening{}}
}

// SetSibling installs the companion loop builder; call it before the first Service.
func (b *MultiBackend) SetSibling(s Sibling) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sibling = s
}

func (b *MultiBackend) Workspaces() []WorkspaceInfo {
	out := make([]WorkspaceInfo, len(b.entries))
	for i, e := range b.entries {
		out[i] = e.Info
	}
	return out
}

func errStopping() error { return fmt.Errorf("%w: the hub is stopping", ErrUnavailable) }

func (b *MultiBackend) Service(name string) (*runner.Service, error) {
	var entry *Entry
	for i := range b.entries {
		if b.entries[i].Info.Name == name {
			entry = &b.entries[i]
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("%w %q", ErrUnknownWorkspace, name)
	}
	if !entry.Info.Available {
		return nil, fmt.Errorf("%w: %s is %s", ErrUnavailable, name, entry.Info.Reason)
	}
	b.mu.Lock()
	if svc, ok := b.svcs[name]; ok {
		b.mu.Unlock()
		return svc, nil
	}
	if b.closed {
		b.mu.Unlock()
		return nil, errStopping()
	}
	if op, ok := b.opening[name]; ok {
		b.mu.Unlock()
		<-op.done
		return op.svc, op.err
	}
	op := &opening{done: make(chan struct{})}
	b.opening[name] = op
	b.mu.Unlock()

	svc, err := b.factory(entry.WS)

	b.mu.Lock()
	delete(b.opening, name)
	switch {
	case err != nil:
		op.err = err // a later call tries again
	case b.closed:
		svc.Store().Close() // Close ran while this store was opening
		op.err = errStopping()
	default:
		op.svc = svc
		b.svcs[name] = svc
		done := make(chan struct{})
		b.done[name] = done
		var side func(context.Context)
		if b.sibling != nil {
			side = b.sibling(entry.WS, svc)
		}
		go func() {
			defer close(done)
			var wg sync.WaitGroup
			if side != nil {
				wg.Add(1)
				go func() {
					defer wg.Done()
					side(b.ctx)
				}()
			}
			svc.Start(b.ctx)
			wg.Wait()
		}()
	}
	b.mu.Unlock()
	close(op.done)
	return op.svc, op.err
}

func (b *MultiBackend) Opened() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.svcs))
	for n := range b.svcs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Close stops every runner (cancelling live rounds, which requeue) and closes the stores. It waits
// for opens in flight, which see the hub closed and close their own store.
func (b *MultiBackend) Close() {
	b.cancel()
	b.mu.Lock()
	b.closed = true
	svcs, done := b.svcs, b.done
	b.svcs, b.done = map[string]*runner.Service{}, map[string]chan struct{}{}
	var pending []chan struct{}
	for _, op := range b.opening {
		pending = append(pending, op.done)
	}
	b.mu.Unlock()
	for _, ch := range pending {
		<-ch
	}
	for name, svc := range svcs {
		<-done[name]
		svc.Store().Close()
	}
}

// DiscoverAll lists every workspace under the registered bases; untrusted ones are unavailable.
func DiscoverAll(cfg *registry.Config) []Entry {
	all, _ := resolve.ListWorkspaces(cfg)
	out := make([]Entry, 0, len(all))
	for i := range all {
		w := all[i]
		info := WorkspaceInfo{Name: w.Name, Available: trust.IsTrusted(w.Hash)}
		if !info.Available {
			info.Reason = "untrusted (run: tq allow " + w.Name + ")"
		}
		out = append(out, Entry{Info: info, WS: &w})
	}
	return out
}

// DiscoverCwd is the default topology: the trusted workspace containing cwd.
func DiscoverCwd(cwd string, cfg *registry.Config) ([]Entry, error) {
	res := resolve.Resolve(cwd, cfg)
	switch {
	case res.Workspace != nil:
		return []Entry{{Info: WorkspaceInfo{Name: res.Workspace.Name, Available: true}, WS: res.Workspace}}, nil
	case res.Untrusted != nil:
		return nil, fmt.Errorf("workspace %s is untrusted; run: tq allow %s", res.Untrusted.Name, res.Untrusted.Name)
	}
	return nil, fmt.Errorf("no workspace here (%s); cd into a client workspace, pass --workspace, or use tq hub serve --all", res.Reason)
}
