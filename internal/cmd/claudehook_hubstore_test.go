package cmd

import (
	"strings"
	"testing"
)

// tq/hub-store-write end to end (spec D §16, assumption 6): active while
// cloud or db is on, inactive otherwise, and the manifest's guard.disable
// can't switch it off.
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
	if code, out := hook("", write); strings.Contains(out, "tq/hub-store-write") {
		t.Fatalf("packs off: the rule must be inactive: %d %s", code, out)
	}
	if code, out := hook("packs:\n  db: { enabled: true }\n", "cat ~/.tentaqles/hub/acme/serve.json"); code != 0 || strings.Contains(out, "tq/hub-store-write") {
		t.Fatalf("a read is not a write: %d %s", code, out)
	}
}
