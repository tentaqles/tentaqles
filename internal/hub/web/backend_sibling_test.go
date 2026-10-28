package web

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The sync engine runs as a sibling of svc.Start (spec B §4): built once per opened workspace,
// started with the runner and stopped with it, before the store closes.
func TestSiblingStartsAndStopsWithTheRunner(t *testing.T) {
	cfg, _ := fourWorkspaces(t)
	factory := func(ws *resolve.Workspace) (*runner.Service, error) {
		st, err := store.Open(ws.Name, []string{"all"})
		if err != nil {
			return nil, err
		}
		return runner.New(runner.Config{Workspace: ws.Name, Root: ws.Root, Store: st, Actor: "hub:tester"})
	}
	b := NewMultiBackend(context.Background(), DiscoverAll(cfg), factory)
	var built, running, stopped atomic.Int32
	b.SetSibling(func(ws *resolve.Workspace, svc *runner.Service) func(context.Context) {
		built.Add(1)
		if ws.Name != "acme" {
			return nil // a workspace without a tracker has no sibling
		}
		return func(ctx context.Context) {
			running.Add(1)
			<-ctx.Done()
			// The store is still open while the sibling winds down.
			if _, err := svc.Store().SchemaVersion(); err != nil {
				t.Errorf("store closed under the sibling: %v", err)
			}
			stopped.Add(1)
		}
	})
	for _, name := range []string{"acme", "globex"} {
		if _, err := b.Service(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Service("acme"); err != nil { // a second open reuses the first
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for running.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if built.Load() != 2 || running.Load() != 1 {
		t.Fatalf("built %d running %d", built.Load(), running.Load())
	}
	b.Close()
	if stopped.Load() != 1 {
		t.Fatalf("Close returned before the sibling stopped (%d)", stopped.Load())
	}
}
