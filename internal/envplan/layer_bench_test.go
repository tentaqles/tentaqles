package envplan

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The cost the layer adds to the prompt hook (ADR-0008), on the machine's own PATH (read,
// never printed): go test ./internal/envplan -run '^$' -bench Layer.

// BenchmarkLayer_Compile is LayerFor: the scope compiled from the catalog, at every prompt in
// a guard: v2 folder.
func BenchmarkLayer_Compile(b *testing.B) {
	b.Setenv("TQ_HOME", testutil.TempDir(b))
	ws := companyX(b, true)
	_ = Desired(ws) // the catalog loaded once per process, as in tq env
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = LayerFor(ws)
	}
}

// BenchmarkLayer_FilterProcessPATH is the full pass over the process PATH: one directory read
// per entry, paid on entering the folder and after a PATH change.
func BenchmarkLayer_FilterProcessPATH(b *testing.B) {
	b.Setenv("TQ_HOME", testutil.TempDir(b))
	l := LayerFor(companyX(b, true))
	path := os.Getenv("PATH")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = l.FilterPath(path, NativeSyntax())
	}
}

// BenchmarkLayer_PromptFastPath is a prompt inside the folder with the PATH unchanged: Plan
// and DiffWant, no directory read.
func BenchmarkLayer_PromptFastPath(b *testing.B) {
	b.Setenv("TQ_HOME", testutil.TempDir(b))
	ws := companyX(b, true)
	sh := map[string]string{"PATH": os.Getenv("PATH")}
	e := Env{Lookup: env(sh), Names: environNames(os.Environ()), Syntax: NativeSyntax(), PathOK: true}
	want, _ := Plan(ws, State{}, e)
	ops, _ := DiffWant(want, e.Lookup, State{}, ws.Name)
	shellSim(sh, ops)
	st := DecodeState(sh[StateVar])
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w, _ := Plan(ws, st, e)
		if o, _ := DiffWant(w, e.Lookup, st, ws.Name); o.Changed {
			b.Fatal("the fast path changed something")
		}
	}
}
