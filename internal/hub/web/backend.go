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

// MultiBackend serves several workspaces from one process (spec §5): one store and one runner per
// workspace, opened on first use; an unavailable workspace is listed and never opened.
type MultiBackend struct {
	ctx     context.Context
	cancel  context.CancelFunc
	entries []Entry
	factory Factory
	mu      sync.Mutex
	closed  bool
	svcs    map[string]*runner.Service
	done    map[string]chan struct{}
}

func NewMultiBackend(ctx context.Context, entries []Entry, f Factory) *MultiBackend {
	cctx, cancel := context.WithCancel(ctx)
	es := append([]Entry(nil), entries...)
	sort.Slice(es, func(i, j int) bool { return es[i].Info.Name < es[j].Info.Name })
	return &MultiBackend{ctx: cctx, cancel: cancel, entries: es, factory: f, svcs: map[string]*runner.Service{}, done: map[string]chan struct{}{}}
}

func (b *MultiBackend) Workspaces() []WorkspaceInfo {
	out := make([]WorkspaceInfo, len(b.entries))
	for i, e := range b.entries {
		out[i] = e.Info
	}
	return out
}

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
	defer b.mu.Unlock()
	if svc, ok := b.svcs[name]; ok {
		return svc, nil
	}
	if b.closed {
		return nil, fmt.Errorf("%w: the hub is stopping", ErrUnavailable)
	}
	svc, err := b.factory(entry.WS)
	if err != nil {
		return nil, err
	}
	b.svcs[name] = svc
	done := make(chan struct{})
	b.done[name] = done
	go func() {
		defer close(done)
		svc.Start(b.ctx)
	}()
	return svc, nil
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

// Close stops every runner (cancelling live rounds, which requeue) and closes the stores.
func (b *MultiBackend) Close() {
	b.cancel()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for name, svc := range b.svcs {
		<-b.done[name]
		svc.Store().Close()
	}
	b.svcs = map[string]*runner.Service{}
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
