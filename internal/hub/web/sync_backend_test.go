package web

import (
	"context"
	"errors"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The MultiBackend hands the pages the engine its sync sibling built, and runs its loop beside
// the runner; a workspace without one answers ErrNoTracker, an unavailable one is never opened.
func TestMultiBackendSyncEngine(t *testing.T) {
	cfg, _ := fourWorkspaces(t)
	factory := func(ws *resolve.Workspace) (*runner.Service, error) {
		st, err := store.Open(ws.Name, []string{"all"})
		if err != nil {
			return nil, err
		}
		return runner.New(runner.Config{Workspace: ws.Name, Root: ws.Root, Store: st, Actor: "hub:tester"})
	}
	b := NewMultiBackend(context.Background(), DiscoverAll(cfg), factory)
	defer b.Close()
	ran := make(chan struct{}, 4)
	b.SetSyncSibling(func(ws *resolve.Workspace, svc *runner.Service) SyncRunner {
		if ws.Name != "acme" {
			return nil
		}
		eng, err := hubsync.NewEngine(hubsync.EngineConfig{Workspace: ws.Name, Store: svc.Store(),
			Tracker: &manifest.Tracker{Provider: "github"},
			Adapter: func(context.Context) (hubsync.Adapter, error) { return &uiTracker{}, nil }})
		if err != nil {
			t.Error(err)
			return nil
		}
		return loopProbe{eng, ran}
	})
	eng, err := b.SyncEngine("acme")
	if err != nil || eng == nil {
		t.Fatalf("acme: %v", err)
	}
	if ov, err := eng.Overview(); err != nil || ov.Provider != "github" {
		t.Fatalf("%+v %v", ov, err)
	}
	<-ran
	if _, err := b.SyncEngine("globex"); !errors.Is(err, hubsync.ErrNoTracker) {
		t.Fatalf("globex: %v", err)
	}
	for _, info := range b.Workspaces() {
		if !info.Available {
			if _, err := b.SyncEngine(info.Name); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("%s: %v", info.Name, err)
			}
		}
	}
}

// loopProbe reports that the backend started the engine's loop.
type loopProbe struct {
	*hubsync.Engine
	ran chan struct{}
}

func (p loopProbe) Run(ctx context.Context) {
	p.ran <- struct{}{}
	<-ctx.Done()
}
