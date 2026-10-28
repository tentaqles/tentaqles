package sync

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

// Check verifies auth through the engine's adapter and records the account and the credential
// source for the Connections page (spec B §10); it never stores the token.
func TestCheckRecordsAccountAndSource(t *testing.T) {
	r := newRig(t)
	if _, ok := r.eng.Connection(); ok {
		t.Fatal("a connection before any check")
	}
	c, err := r.eng.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Account != "tester" || c.Provider != "github" || c.CheckedAt == "" || c.Error != "" {
		t.Fatalf("%+v", c)
	}
	got, ok := r.eng.Connection()
	if !ok || got != c {
		t.Fatalf("stored %+v %v, want %+v", got, ok, c)
	}
}

func TestCheckRecordsTheSourceTheChainUsed(t *testing.T) {
	isolateHome(t)
	st, err := store.Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	Register("acme-conn", func(cfg Config) (Adapter, error) { return &fake{cfg: cfg}, nil })
	t.Cleanup(func() { unregister("acme-conn") })
	tok := cred.Token{Value: "s3cr3t-value"}
	restore := cred.SetDefault(cred.ResolverFunc(func(context.Context, cred.Request) (cred.Transport, error) {
		return cred.Static(cred.SourceKeychain, tok), nil
	}))
	t.Cleanup(restore)
	eng, err := NewEngine(EngineConfig{Workspace: "acme", Store: st, Tracker: &manifest.Tracker{Provider: "acme-conn"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := eng.Check(context.Background())
	if err != nil || c.Source != string(cred.SourceKeychain) || c.Account != "tester" {
		t.Fatalf("%+v %v", c, err)
	}
	raw, _, _ := st.Setting(settingConnection)
	if strings.Contains(raw, "s3cr3t") {
		t.Fatal("the token reached the store")
	}

	// No background credential: the MCP fallback is the source, and the error is recorded.
	cred.SetDefault(cred.ResolverFunc(func(context.Context, cred.Request) (cred.Transport, error) {
		return nil, cred.ErrNeedsSession
	}))
	c, err = eng.Check(context.Background())
	if !errors.Is(err, cred.ErrNeedsSession) || c.Source != string(cred.SourceMCP) || c.Account != "" || c.Error == "" {
		t.Fatalf("%+v %v", c, err)
	}
	if got, ok := eng.Connection(); !ok || got.Error == "" {
		t.Fatalf("the failed check was not stored: %+v", got)
	}
}
