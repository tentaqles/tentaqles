package doctor

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

func TestPackChecksHook(t *testing.T) {
	prev := PackFindings
	t.Cleanup(func() { PackFindings = prev })
	var got []Finding
	add := func(level, code, ws, msg, fix string) { got = append(got, Finding{level, code, ws, msg, fix}) }
	w := &resolve.Workspace{Name: "acme"}

	PackFindings = nil
	packChecks(w, add)
	if len(got) != 0 {
		t.Fatal("no packs linked: nothing")
	}
	PackFindings = func(w *resolve.Workspace) []Finding {
		return []Finding{{Level: "info", Code: "packs-suggest", Workspace: w.Name, Msg: "data pack: a dbt project (dbt_project.yml)"}}
	}
	packChecks(w, add)
	if len(got) != 1 || got[0].Code != "packs-suggest" || got[0].Workspace != "acme" {
		t.Fatalf("%+v", got)
	}
	PackFindings = func(*resolve.Workspace) []Finding { panic("probe broke") }
	packChecks(w, add)
	if len(got) != 2 || got[1].Code != "packs-doctor-failed" || got[1].Level != "error" {
		t.Fatalf("a panicking probe becomes one finding: %+v", got)
	}
}
