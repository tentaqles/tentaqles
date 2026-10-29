package guardcfg

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func TestNeverDisableIgnoresEveryLayer(t *testing.T) {
	m := &manifest.Manifest{Guard: manifest.Guard{Disable: []string{"tq/hub-store-write", "tq/iac-apply"}}}
	e := Merge(File{Disable: []string{"tq/hub-store-write"}}, nil, m, "acme")
	if e.Off("tq/hub-store-write") {
		t.Fatal("tq/hub-store-write must stay on")
	}
	for _, id := range e.Disable {
		if id == "tq/hub-store-write" {
			t.Fatal("listed as disabled")
		}
	}
	if !e.Off("tq/iac-apply") {
		t.Fatal("other ids still switch off")
	}
	if !NeverDisable("tq/hub-store-write") || NeverDisable("tq/iac-apply") {
		t.Fatal("NeverDisable")
	}
}
