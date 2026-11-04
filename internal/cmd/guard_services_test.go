package cmd

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// TestGuardWriterKeepsServiceBlocks: `tq guard ... --ws` rewrites only the guard block; the
// chat, email, data_platform and agents blocks (spec B §6) survive it unchanged.
func TestGuardWriterKeepsServiceBlocks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", `chat: { provider: teams }
email: { provider: outlook }
data_platform: { provider: snowflake }
agents: [claude, codex]
`)
	mp := filepath.Join(ws, manifest.FileName)
	before, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGuardCmd(t, "guard", "set", "tq/cloud-delete", "deny", "--ws", "acme"); err != nil {
		t.Fatal(err)
	}
	after, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Chat, after.Chat) || !reflect.DeepEqual(before.Email, after.Email) ||
		!reflect.DeepEqual(before.DataPlatform, after.DataPlatform) || !reflect.DeepEqual(before.Agents, after.Agents) {
		t.Fatalf("service blocks changed:\nbefore %+v\nafter  %+v", before.Chat, after.Chat)
	}
	if after.Guard.Actions["tq/cloud-delete"] != "deny" {
		t.Fatalf("%+v", after.Guard)
	}
	h, _ := trust.HashFile(mp)
	if !trust.IsTrusted(h) {
		t.Fatal("not re-trusted")
	}
}
