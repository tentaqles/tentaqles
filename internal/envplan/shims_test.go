package envplan

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The browser shims in the environment layer: a client that lists a browser has
// BROWSER set to its shim and its shims folder first on PATH; leaving restores both exactly;
// a client with no browsers: list has neither.

func browserClient(t testing.TB) *resolve.Workspace {
	t.Helper()
	w := companyX(t, true)
	w.Manifest.Browsers = []manifest.Browser{{Name: "chrome", Profile: "Profile 2"}, {Name: "edge", Profile: "Default"}}
	return w
}

func TestBrowserShims(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	_, azDir, toolsDir := globalDirs(t)
	ws := browserClient(t)
	shims := ShimsDir("company-x")

	// BROWSER names the shim program under $TQ_HOME/shims/.
	d := Desired(ws)
	b := d["BROWSER"]
	want := filepath.Join(shims, shimFile(ShimOpen))
	if runtime.GOOS == "windows" {
		want = filepath.ToSlash(want) // tools split BROWSER like a shell line
	}
	if !strings.EqualFold(strings.Trim(b, `"`), want) {
		t.Fatalf("BROWSER = %q, want the shim %q", b, want)
	}
	if !strings.HasPrefix(strings.ToLower(filepath.ToSlash(strings.Trim(b, `"`))), strings.ToLower(filepath.ToSlash(paths.ShimsRoot()))+"/") {
		t.Fatalf("BROWSER = %q is not under $TQ_HOME/shims", b)
	}

	msys := "/x/az:/x/tools"
	cases := []struct {
		syn  PathSyntax
		path string
	}{
		{NativeSyntax(), join(azDir, toolsDir)},
		{MSYSSyntax(msys, strings.Join([]string{azDir, toolsDir}, ";")), msys},
	}
	for _, c := range cases {
		syn := c.syn
		orig := map[string]string{"PATH": c.path, "BROWSER": "firefox", "HOME": "/home/me"}
		sh := map[string]string{}
		for k, v := range orig {
			sh[k] = v
		}
		step := func(w *resolve.Workspace, prev State) State {
			e := Env{Lookup: env(sh), Names: names(sh), Syntax: syn, PathOK: true}
			want, _ := Plan(w, prev, e)
			name := ""
			if w != nil {
				name = w.Name
			}
			ops, _ := DiffWant(want, e.Lookup, prev, name)
			shellSim(sh, ops)
			return DecodeState(sh[StateVar])
		}
		st := step(ws, State{})
		if sh["BROWSER"] != b {
			t.Fatalf("%s: BROWSER = %q after entering, want %q", syn.Sep, sh["BROWSER"], b)
		}
		first := strings.Split(sh["PATH"], syn.Sep)[0]
		if syn.ToOS(first) == "" || !samePath(filepath.Clean(syn.ToOS(first)), filepath.Clean(shims)) {
			t.Fatalf("%s: the first PATH entry is %q, want the shims folder %q (PATH %q)", syn.Sep, first, shims, sh["PATH"])
		}
		// The next prompt in the same folder changes nothing.
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: syn, PathOK: true}
		w2, _ := Plan(ws, st, e)
		if ops, _ := DiffWant(w2, e.Lookup, st, "company-x"); ops.Changed {
			t.Fatalf("%s: a second prompt changes %v / %v", syn.Sep, ops.Set, ops.Unset)
		}
		// Leaving restores BROWSER and PATH byte for byte.
		step(nil, st)
		if !reflect.DeepEqual(sh, orig) {
			t.Fatalf("%s: after leaving:\n got %v\nwant %v", syn.Sep, sh, orig)
		}
	}

	// The programs: written once, ready, named for the identity, and removed again.
	if ShimsReady("company-x") {
		t.Fatal("ready before they are written")
	}
	if ch, err := WriteShims("company-x"); err != nil || !ch {
		t.Fatalf("WriteShims = %v, %v", ch, err)
	}
	if !ShimsReady("company-x") {
		t.Fatal("not ready after WriteShims")
	}
	if ch, err := WriteShims("company-x"); err != nil || ch {
		t.Fatalf("a second WriteShims changed %v (%v)", ch, err)
	}
	for _, n := range []string{ShimOpen, "open", "xdg-open"} {
		ws, name, mode := ShimName(filepath.Join(shims, shimFile(n)))
		if ws != "company-x" || name != n || mode == ShimNone {
			t.Fatalf("ShimName(%s) = %q %q %v", n, ws, name, mode)
		}
	}
	if _, _, mode := ShimName(filepath.Join(shims, shimFile("git"))); mode != ShimNone {
		t.Fatal("git in the shims folder is a shim")
	}
	if _, _, mode := ShimName(filepath.Join(testutil.TempDir(t), shimFile("open"))); mode != ShimNone {
		t.Fatal("an open outside a shims folder is a shim")
	}
	// A file added to the folder makes it not ready; a write puts it right.
	if err := os.WriteFile(filepath.Join(shims, "evil"), []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if ShimsReady("company-x") {
		t.Fatal("ready with a foreign file in the folder")
	}
	if ch, err := WriteShims("company-x"); err != nil || !ch {
		t.Fatalf("WriteShims did not remove the foreign file: %v %v", ch, err)
	}
	if had, err := RemoveShims("company-x"); err != nil || !had {
		t.Fatalf("RemoveShims = %v, %v", had, err)
	}
}

// A client with no browsers: list: no BROWSER, no shims entry on PATH (REQ-13).
func TestBrowser_NoBlockNoChange(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	_, azDir, toolsDir := globalDirs(t)
	plain := companyX(t, true)
	if _, set := Desired(plain)["BROWSER"]; set {
		t.Fatal("BROWSER set for a client with no browsers")
	}
	path := join(azDir, toolsDir)
	sh := map[string]string{"PATH": path, "BROWSER": "firefox"}
	e := Env{Lookup: env(sh), Names: names(sh), Syntax: NativeSyntax(), PathOK: true}
	want, _ := Plan(plain, State{}, e)
	if want.Set["PATH"] != path {
		t.Fatalf("PATH changed for a client with no browsers: %q", want.Set["PATH"])
	}
	if _, set := want.Set["BROWSER"]; set {
		t.Fatal("BROWSER changed for a client with no browsers")
	}
	// The same client with browsers differs only by BROWSER and the shims entry first.
	with := browserClient(t)
	got, _ := Plan(with, State{}, e)
	wantPath := filepath.Clean(ShimsDir("company-x")) + string(os.PathListSeparator) + path
	if got.Set["PATH"] != wantPath {
		t.Fatalf("PATH = %q, want %q", got.Set["PATH"], wantPath)
	}
	if l := LayerFor(plain); l.shims != "" {
		t.Fatal("a layer with no browsers has a shims folder")
	}
}

// A terminal already in the client when its manifest gains a browsers: list gets the shims
// folder on PATH at the next prompt (round 1, L-04: the fast path kept the old PATH), and
// loses it when the list goes; leaving still restores PATH exactly.
func TestBrowserShims_ManifestChangesUnderAnOpenTerminal(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	_, azDir, toolsDir := globalDirs(t)
	plain := companyX(t, true)
	with := browserClient(t)
	orig := map[string]string{"PATH": join(azDir, toolsDir)}
	sh := map[string]string{"PATH": orig["PATH"]}
	step := func(w *resolve.Workspace, prev State) State {
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: NativeSyntax(), PathOK: true}
		want, _ := Plan(w, prev, e)
		name := ""
		if w != nil {
			name = w.Name
		}
		ops, _ := DiffWant(want, e.Lookup, prev, name)
		shellSim(sh, ops)
		return DecodeState(sh[StateVar])
	}
	hasShims := func() bool { return strings.Contains(sh["PATH"], ShimsDir("company-x")) }

	st := step(plain, State{})
	if hasShims() {
		t.Fatalf("a client with no browsers has the shims on PATH: %q", sh["PATH"])
	}
	st = step(plain, st) // an idle prompt: the fast path
	st = step(with, st)  // the manifest gained browsers
	if !hasShims() || sh["BROWSER"] == "" {
		t.Fatalf("the open terminal did not get the shims: %v", sh)
	}
	if first := strings.Split(sh["PATH"], string(os.PathListSeparator))[0]; !samePath(filepath.Clean(first), filepath.Clean(ShimsDir("company-x"))) {
		t.Fatalf("the first PATH entry is %q", first)
	}
	st = step(with, st) // idle again: nothing to do
	e := Env{Lookup: env(sh), Names: names(sh), Syntax: NativeSyntax(), PathOK: true}
	if w2, _ := Plan(with, st, e); func() bool { ops, _ := DiffWant(w2, e.Lookup, st, "company-x"); return ops.Changed }() {
		t.Fatal("an idle prompt changes something")
	}
	st = step(plain, st) // and the manifest lost them again
	if hasShims() || sh["BROWSER"] != "" {
		t.Fatalf("the shims survive the removal of the list: %v", sh)
	}
	step(nil, st)
	if !reflect.DeepEqual(sh, orig) {
		t.Fatalf("after leaving:\n got %v\nwant %v", sh, orig)
	}
}

// A shell whose state is lost fails closed: the shim BROWSER and the shims folder on PATH go,
// the user's own BROWSER stays.
func TestBrowserShims_LostState(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	_, azDir, toolsDir := globalDirs(t)
	ws := browserClient(t)
	path := join(azDir, toolsDir)
	step := func(sh map[string]string, w *resolve.Workspace) {
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: NativeSyntax(), PathOK: true}
		real := e
		raw, _ := e.Lookup(StateVar)
		prev := DecodeState(raw)
		lost := LostState(e)
		if lost {
			e, prev = Forget(e), State{}
		}
		name := ""
		if w != nil {
			name = w.Name
		}
		want, _ := Plan(w, prev, e)
		ops, _ := DiffWant(want, e.Lookup, prev, name)
		if lost {
			FinishForget(&ops, real, e)
		}
		shellSim(sh, ops)
	}
	sh := map[string]string{"PATH": path, "BROWSER": "firefox"}
	step(sh, ws)
	if !strings.Contains(sh["BROWSER"], "shims") {
		t.Fatalf("not entered: %v", sh)
	}
	delete(sh, StateVar) // the state is lost
	step(sh, nil)
	if sh["BROWSER"] == Desired(ws)["BROWSER"] || strings.Contains(sh["PATH"], ShimsDir("company-x")) {
		t.Fatalf("the shim survives a lost state: %v", sh)
	}
	// the user's own BROWSER is not tq's
	own := map[string]string{"PATH": path, "BROWSER": "firefox", "TQ_WS": "company-x"}
	step(own, nil)
	if own["BROWSER"] != "firefox" {
		t.Fatalf("the user's BROWSER was removed: %v", own)
	}
}
