package cmd

import (
	"context"
	"fmt"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// doctorTrackerCheck is tq doctor's tracker check (spec B §8): the adapter registered for the
// workspace's tracker, given the credential the resolver chain finds (cli, keychain, env), runs
// Check. No adapter in this build means no credential is fetched at all. The token never leaves
// the transport; errors name the source, never the value.
func doctorTrackerCheck(ctx context.Context, w *resolve.Workspace, env []string) (string, error) {
	cfg := hubsync.ConfigFromManifest(w.Name, w.Manifest.Tracker)
	cfg.Env = env
	if _, ok := hubsync.Lookup(cfg.Provider); !ok {
		return "", fmt.Errorf("%w for %q", hubsync.ErrNoAdapter, cfg.Provider)
	}
	t, err := cred.Resolve(ctx, cfg.CredRequest())
	if err != nil {
		return "", err
	}
	cfg.Transport = t
	a, err := hubsync.New(cfg)
	if err != nil {
		return "", err
	}
	acct, err := a.Check(ctx)
	if err != nil {
		return "", fmt.Errorf("%s check (credential from %s): %w", cfg.Provider, t.Source(), err)
	}
	return acct, nil
}
