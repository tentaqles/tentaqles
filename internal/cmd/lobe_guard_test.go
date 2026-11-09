package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// The guard fails closed and does not wait on the lobe: with the client's lobe store lock held by
// another writer, the guard still denies a wrong-identity commit at once, and still allows a local command.
func TestLobeStall_GuardStillDenies(t *testing.T) {
	requireGit(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  email: dev@acme.example\n")
	t.Setenv("TQ_WS", "acme") // the terminal of the acme folder: no env-drift finding (after isolateHome)
	gitInitWithEmail(t, ws, "other@example.com")

	st, err := store.Open("acme", store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLock()
	unlock, err := st.Lock(lockCtx)
	if err != nil {
		t.Fatalf("lock the lobe store: %v", err)
	}
	defer unlock()

	done := make(chan struct{})
	var code int
	var errOut string
	go func() {
		defer close(done)
		code, _, errOut = runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git commit -m x"))
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the guard waited on the held lobe lock")
	}
	if code != 2 || !strings.Contains(errOut, "BLOCKED") {
		t.Fatalf("wrong-identity commit with the lobe lock held: code=%d stderr=%q, want 2 and BLOCKED", code, errOut)
	}

	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "ls")); code != 0 {
		t.Fatalf("ls with the lobe lock held: code=%d stderr=%q, want 0", code, errOut)
	}
}
