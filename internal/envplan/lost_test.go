package envplan

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// Under a harness too (a shell that has TQ_WS and no state) a missing state is lost:
// otherwise one client's homes and mask would carry into the next.
func TestLostState_HarnessMissingStateIsLost(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	sh := map[string]string{"TQ_WS": "acme"}
	if !LostState(Env{Lookup: env(sh)}) {
		t.Fatal("a harness shell without a state is not lost")
	}
	sh[StateVar] = "hello world"
	if !LostState(Env{Lookup: env(sh)}) {
		t.Fatal("a corrupt state under a harness is not lost")
	}
}

// A shell whose __TQ_STATE is truncated, not base64, empty, garbage or missing while TQ_WS is
// set fails closed: tq's variables and every mask folder go, the user's own variables stay,
// the next identity starts clean, and the notice says the old values are lost.
func TestLostState_FailsClosed(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	shared := filepath.Join(testutil.TempDir(t), "shared")
	fakeExe(t, shared, "aws.exe")
	fakeExe(t, shared, "node.exe")
	cx := companyX(t, true)
	if _, err := LayerFor(cx).WriteMask(); err != nil {
		t.Fatal(err)
	}
	step := func(sh map[string]string, w *resolve.Workspace) *Notice {
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
		want, n := Plan(w, prev, e)
		ops, _ := DiffWant(want, e.Lookup, prev, name)
		if lost {
			FinishForget(&ops, real, e)
			n = ops.Notice
		}
		shellSim(sh, ops)
		return n
	}
	good := map[string]string{"PATH": shared, "MYAPP": "keep", "AWS_PROFILE": "prod"}
	step(good, cx)
	if !strings.Contains(good["PATH"], MaskDir("company-x")) || good["TQ_WS"] != "company-x" || good["CLAUDE_CONFIG_DIR"] == "" {
		t.Fatalf("not entered: %v", good)
	}
	if LostState(Env{Lookup: env(good)}) {
		t.Fatal("a good state is lost")
	}
	st := good[StateVar]
	for _, c := range []struct {
		name    string
		state   string
		missing bool
	}{
		{"truncated", st[:len(st)/2], false},
		{"not base64", "z1.!!!@@@###", false},
		{"empty", "", false},
		{"garbage", "hello world", false},
		{"missing", "", true},
	} {
		sh := map[string]string{}
		for k, v := range good {
			sh[k] = v
		}
		if c.missing {
			delete(sh, StateVar)
		} else {
			sh[StateVar] = c.state
		}
		if !LostState(Env{Lookup: env(sh)}) {
			t.Fatalf("%s: not seen as lost", c.name)
		}
		n := step(sh, nil) // to a neutral folder
		if n == nil || n.Kind != NoticeStateLost {
			t.Errorf("%s: notice %v", c.name, n)
		}
		for _, k := range []string{"TQ_WS", "TQ_WS_ROOT", "CLAUDE_CONFIG_DIR", "AZURE_CONFIG_DIR", StateVar} {
			if _, ok := sh[k]; ok {
				t.Errorf("%s: %s survives", c.name, k)
			}
		}
		if strings.Contains(sh["PATH"], "path-mask") || !strings.Contains(sh["PATH"], shared) {
			t.Errorf("%s: PATH = %q", c.name, sh["PATH"])
		}
		if sh["MYAPP"] != "keep" {
			t.Errorf("%s: a user variable changed", c.name)
		}
		// The shell is clean again: the next prompt changes nothing and says nothing.
		if n := step(sh, nil); n != nil {
			t.Errorf("%s: second prompt notice %v", c.name, n)
		}
		// Straight into another identity from a lost state: company-x's homes are gone.
		sh2 := map[string]string{}
		for k, v := range good {
			sh2[k] = v
		}
		sh2[StateVar] = "hello world"
		aws := &resolve.Workspace{Name: "awsco", Root: filepath.Join(testutil.TempDir(t), "awsco"), Manifest: &manifest.Manifest{
			Schema: "tentaqles-client-v2", Client: "awsco", Identities: map[string]manifest.Identity{"aws": {}}}}
		step(sh2, aws)
		if sh2["TQ_WS"] != "awsco" || sh2["CLAUDE_CONFIG_DIR"] != "" || strings.Contains(sh2["PATH"], MaskDir("company-x")) || sh2["AZURE_CONFIG_DIR"] != "" {
			t.Errorf("%s: awsco inherits company-x: %v", c.name, sh2)
		}
	}
}

// A state that decodes but does not record what tq set (one edited by hand) leaves
// no tq variable behind, even through an identity it re-enters; a user's own value of a
// catalog variable is never touched, and a full state is never swept.
func TestUnrecorded(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	mine := filepath.Join(testutil.TempDir(t), "my-aws-config")
	cxClaude := filepath.Join(filepath.Dir(MaskDir("company-x")), "claude")
	st := State{WS: "company-x", Prev: map[string]*string{"TQ_WS": nil}}
	base := map[string]string{"TQ_WS": "company-x", StateVar: st.Encode(), "CLAUDE_CONFIG_DIR": cxClaude,
		"AWS_CONFIG_FILE": mine, "PATH": MaskDir("company-x") + string(filepath.ListSeparator) + testutil.TempDir(t)}
	run := func(sh map[string]string, w *resolve.Workspace) *Notice {
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: NativeSyntax(), PathOK: true}
		real := e
		prev := DecodeState(sh[StateVar])
		lost := false
		if prev.WS != "" {
			if u, found := Unrecorded(e, prev); found {
				e, lost = u, true
			}
		}
		name := ""
		if w != nil {
			name = w.Name
		}
		want, n := Plan(w, prev, e)
		ops, _ := DiffWant(want, e.Lookup, prev, name)
		if lost {
			FinishForget(&ops, real, e)
			n = ops.Notice
		}
		shellSim(sh, ops)
		return n
	}
	for _, into := range []*resolve.Workspace{nil, companyX(t, true)} {
		sh := map[string]string{}
		for k, v := range base {
			sh[k] = v
		}
		if n := run(sh, into); n == nil || n.Kind != NoticeStateLost {
			t.Fatalf("no notice: %v", n)
		}
		if into == nil && sh["AWS_CONFIG_FILE"] != mine { // inside company-x it is out of scope, unset
			t.Fatal("the user's own AWS_CONFIG_FILE was touched")
		}
		if n := run(sh, into); n != nil {
			t.Fatalf("second prompt: %v", n)
		}
		run(sh, nil) // leave
		if _, ok := sh["CLAUDE_CONFIG_DIR"]; ok {
			t.Fatalf("company-x's CLAUDE_CONFIG_DIR survives leaving: %v", sh)
		}
		if strings.Contains(sh["PATH"], "path-mask") || sh["AWS_CONFIG_FILE"] != mine {
			t.Fatalf("after leaving: %v", sh)
		}
	}
}
