package cmd

import (
	"strings"
	"testing"
)

// tq/hub-store-write end to end (spec D §16, assumption 6): denied while
// cloud or db is on, and with the packs off (the core rule); the manifest's
// guard.disable can't switch it off.
func TestHookHubStoreWrite(t *testing.T) {
	const write = `sqlite3 ~/.tentaqles/hub/acme/board.db "update tasks set state='done'"`
	hook := func(manifest, cmd string) (int, string) {
		t.Helper()
		ws := setupTrustedWorkspaceWithManifest(t, "acme", manifest)
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd))
		return code, out
	}
	for _, m := range []string{
		"packs:\n  cloud: { enabled: true }\n",
		"packs:\n  db: { enabled: true }\n",
		"packs:\n  db: { enabled: true }\nguard: { disable: [tq/hub-store-write] }\n",
		"packs:\n  cloud: { enabled: true }\nguard: { disable: [tq/hub-store-write] }\n",
	} {
		code, out := hook(m, write)
		if code != 2 || !strings.Contains(out, "tq/hub-store-write") {
			t.Fatalf("%q: want deny by tq/hub-store-write, got %d %s", m, code, out)
		}
	}
	// Since F round 1 tq/hub-store-write is also a core rule (no Pack), and
	// the hook-time state-dir check denies any shell reach into hub/ or
	// packs/ (F round 2): with the packs off the write is still denied, and
	// so is a read of the store. (D-GR's pack block was dropped in integration round 3.)
	if code, out := hook("", write); code != 2 || !strings.Contains(out, "tq/hub-store-write") {
		t.Fatalf("packs off: the core rule still denies: %d %s", code, out)
	}
	if code, out := hook("packs:\n  db: { enabled: true }\n", "cat ~/.tentaqles/hub/acme/serve.json"); code != 2 || !strings.Contains(out, "protected part") {
		t.Fatalf("a shell read of the hub store is denied by the state-dir rule: %d %s", code, out)
	}
}
