package manifest

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

// TestLoadRecordsDroppedBlocks: a manifest that still carries blocks tq 0.9.0
// no longer reads (D-29) loads, and the blocks are listed in Ignored in
// DroppedBlocks order; a manifest without them lists nothing.
func TestLoadRecordsDroppedBlocks(t *testing.T) {
	withBlocks := write(t, `
schema: tentaqles-client-v1
client: acme
git: { email: a@acme.com, user: acme-bot, provider: github }
cloud: { provider: azure }
data:
  classes: [internal]
guard:
  unattended:
    workspace: true
`)
	m, err := Load(withBlocks)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.Ignored, []string{"guard", "data"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Ignored = %v, want %v", got, want)
	}

	clean := write(t, `
schema: tentaqles-client-v1
client: acme
git: { email: a@acme.com, user: acme-bot, provider: github }
cloud: { provider: azure }
`)
	m, err = Load(clean)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Ignored) != 0 {
		t.Fatalf("Ignored = %v, want none", m.Ignored)
	}
}

// TestLoadRecordsDroppedClaudeKeys: a manifest written before the permission
// settings were retired, with claude.permission_mode (any value, even one the
// old validation refused) and claude.permissions, loads; both keys are listed
// in Ignored after the top-level blocks, the rest of the claude block is still
// read, and loading leaves the file byte for byte as it was (it is
// hash-trusted).
func TestLoadRecordsDroppedClaudeKeys(t *testing.T) {
	body := `
schema: tentaqles-client-v2
client: acme
git: { email: a@acme.com }
identities: { claude: {} }
guard:
  unattended: { workspace: true }
claude:
  permission_mode: yolo
  permissions:
    allow: ["Bash(make:*)"]
    deny: ["Bash(terraform apply:*)"]
    ask: ["Bash(git push:*)"]
  env: { FOO: bar }
`
	p := write(t, body)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.Ignored, []string{"guard", "claude.permission_mode", "claude.permissions"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Ignored = %v, want %v", got, want)
	}
	if m.Claude.Env["FOO"] != "bar" {
		t.Fatalf("claude.env not read: %+v", m.Claude)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, []byte(body)) {
		t.Fatal("Load rewrote the manifest")
	}
}
