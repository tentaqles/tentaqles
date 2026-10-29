package data

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func TestContractPIIGolden(t *testing.T) {
	ctGolden(t, "contract-pii", runContractPII)
}

// ctSyntheticEmail is built at run time so no address sits in the source.
func ctSyntheticEmail() string { return "tester" + "@" + "acme.test" }

// ctPIICase copies a golden case to a temp dir with the email example
// replaced (no tq:allow-pii marker) and returns its Input.
func ctPIICase(t *testing.T, name, classification string) *packs.Input {
	t.Helper()
	src := filepath.Join("testdata", "contract-pii", "classified-ok")
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join(src, "new", "contracts", "customers.odcs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(b), "          - "+ctSyntheticEmail()+"  # tq:allow-pii", "          - "+ctSyntheticEmail(), 1)
	body = strings.Replace(body, "classification: pii\n        examples", "classification: "+classification+"\n        examples", 1)
	if body == string(b) {
		t.Fatal("fixture did not change")
	}
	if err := os.MkdirAll(filepath.Join(dir, "new", "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new", "contracts", "customers.odcs.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(src, "config.yaml"))
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	return ctInput(t, dir)
}

func TestContractPIIExampleHitIsAnError(t *testing.T) {
	r := runContractPII(context.Background(), ctPIICase(t, "hit", "pii"))
	if r.Status != packs.Fail {
		t.Fatalf("status %s: %+v", r.Status, r.Findings)
	}
	var hit *packs.Finding
	for i, f := range r.Findings {
		if f.Rule == "pii-example" {
			hit = &r.Findings[i]
		}
		if strings.Contains(f.Message, "tester") || strings.Contains(f.Message, "acme.test") {
			t.Fatalf("a finding quotes the value: %s", f)
		}
	}
	if hit == nil || hit.Level != packs.LevelError || hit.Line != 21 {
		t.Fatalf("want a pii-example error on line 21: %+v", r.Findings)
	}
}

func TestContractPIIUnclassifiedExampleWarns(t *testing.T) {
	r := runContractPII(context.Background(), ctPIICase(t, "warn", "public"))
	found := false
	for _, f := range r.Findings {
		if f.Rule == "unclassified-example" {
			found = f.Level == packs.LevelWarn
		}
	}
	if !found {
		t.Fatalf("want an unclassified-example warn: %+v", r.Findings)
	}
}

func TestContractClassMapping(t *testing.T) {
	for in, want := range map[string]string{"pii": "pii", "PII": "pii", "personal": "pii", "restricted": "sensitive",
		"special-category": "sensitive", "pci": "financial", "financial": "financial", "children": "children",
		"public": "", "internal": "", "confidential": "", "3": ""} {
		if got := contractClass(in); got != want {
			t.Errorf("%s -> %q, want %q", in, got, want)
		}
	}
}
