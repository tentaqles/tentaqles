package ml

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// Final review m4: tq/model-promote declares the default tq guard list shows.
func TestModelPromoteGuardDefault(t *testing.T) {
	for _, g := range packs.Guards() {
		if g.ID == "tq/model-promote" {
			if g.Default != "deny" {
				t.Fatalf("%q", g.Default)
			}
			return
		}
	}
	t.Fatal("not registered")
}
