package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// fakeTracker is an adapter whose Check reports a fixed account.
type fakeTracker struct {
	hubsync.Adapter
	cfg *hubsync.Config
}

func (f fakeTracker) Check(context.Context) (string, error) {
	if f.cfg.Transport == nil {
		return "", errors.New("no transport")
	}
	return "tester", nil
}

func trackerWS(provider string) *resolve.Workspace {
	return &resolve.Workspace{Name: "acme", Manifest: &manifest.Manifest{Client: "acme",
		Tracker: &manifest.Tracker{Provider: provider, Scope: map[string]string{"org": "acme"}}}}
}

func TestDoctorTrackerCheck_NoAdapterSkipsTheCredential(t *testing.T) {
	resolved := false
	restore := cred.SetDefault(cred.ResolverFunc(func(context.Context, cred.Request) (cred.Transport, error) {
		resolved = true
		return nil, errors.New("must not run")
	}))
	t.Cleanup(restore)
	_, err := doctorTrackerCheck(context.Background(), trackerWS("onboard-test-unregistered"), nil)
	if !errors.Is(err, hubsync.ErrNoAdapter) {
		t.Fatalf("err = %v", err)
	}
	if resolved {
		t.Fatal("no adapter: no credential is fetched")
	}
}

// fakeTrackerGot is where the registered fake factory records its config. The registry has no
// unregister, so the factory is registered once per process and writes through this variable,
// which each run resets (the test stays green under -count=N).
var fakeTrackerGot *hubsync.Config

func TestDoctorTrackerCheck_ResolvesThenChecks(t *testing.T) {
	const name = "onboard-test-fake"
	fakeTrackerGot = nil
	if _, ok := hubsync.Lookup(name); !ok {
		hubsync.Register(name, func(cfg hubsync.Config) (hubsync.Adapter, error) {
			c := cfg
			fakeTrackerGot = &c
			return fakeTracker{cfg: &c}, nil
		})
	}
	var req cred.Request
	restore := cred.SetDefault(cred.ResolverFunc(func(_ context.Context, r cred.Request) (cred.Transport, error) {
		req = r
		return cred.Static(cred.SourceKeychain, cred.Token{Value: "x"}), nil
	}))
	t.Cleanup(restore)
	env := []string{"GH_CONFIG_DIR=/tmp/acme/gh"}
	acct, err := doctorTrackerCheck(context.Background(), trackerWS(name), env)
	if err != nil || acct != "tester" {
		t.Fatalf("acct %q err %v", acct, err)
	}
	if req.Workspace != "acme" || req.Provider != name || req.Preferred != cred.SourceCLI || len(req.Env) != 1 {
		t.Fatalf("request %+v", req)
	}
	if got := fakeTrackerGot; got == nil || got.ScopeKey != "org=acme" || got.Transport == nil {
		t.Fatalf("config %+v", got)
	}

	restore2 := cred.SetDefault(cred.NoopResolver{})
	t.Cleanup(restore2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := doctorTrackerCheck(ctx, trackerWS(name), env); !errors.Is(err, cred.ErrNeedsSession) {
		t.Fatalf("no credential: err = %v", err)
	}
}
