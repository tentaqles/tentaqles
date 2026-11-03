package manifest

import (
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
