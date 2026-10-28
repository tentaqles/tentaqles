package envplan

import (
	"path/filepath"
	"testing"
)

// az and azure-devops are one CLI under two catalog ids. A workspace that enables both must get
// one AZURE_CONFIG_DIR, and the same one on every run: the first identity in sorted order (az).
func TestDesired_SharedCLIVariableKeepsTheFirstIdentity(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join("home", ".tentaqles"))
	want := filepath.Join("home", ".tentaqles", "identities", "acme", "az")
	for i := 0; i < 20; i++ {
		d := Desired(ws("acme", "azure-devops", "az"))
		if d["AZURE_CONFIG_DIR"] != want {
			t.Fatalf("AZURE_CONFIG_DIR = %q, want %q", d["AZURE_CONFIG_DIR"], want)
		}
	}
	alone := Desired(ws("acme", "azure-devops"))
	if got := alone["AZURE_CONFIG_DIR"]; got != filepath.Join("home", ".tentaqles", "identities", "acme", "azure-devops") {
		t.Fatalf("azure-devops alone: %q", got)
	}
}
