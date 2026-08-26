package envplan

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func ws(name string, ids ...string) *resolve.Workspace {
	m := &manifest.Manifest{Schema: "tentaqles-client-v2", Client: name, Identities: map[string]manifest.Identity{}}
	for _, i := range ids {
		m.Identities[i] = manifest.Identity{}
	}
	return &resolve.Workspace{Name: name, Root: filepath.Join("base", name), Manifest: m}
}

func TestDesired_MapsProvidersToVars(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join("home", ".tentaqles"))
	d := Desired(ws("acme", "claude", "gh", "aws"))
	want := map[string]string{
		"TQ_WS":                       "acme",
		"TQ_WS_ROOT":                  filepath.Join("base", "acme"),
		"CLAUDE_CONFIG_DIR":           filepath.Join("home", ".tentaqles", "identities", "acme", "claude"),
		"GH_CONFIG_DIR":               filepath.Join("home", ".tentaqles", "identities", "acme", "gh"),
		"AWS_CONFIG_FILE":             filepath.Join("home", ".tentaqles", "identities", "acme", "aws", "config"),
		"AWS_SHARED_CREDENTIALS_FILE": filepath.Join("home", ".tentaqles", "identities", "acme", "aws", "credentials"),
	}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("got %v\nwant %v", d, want)
	}
}

func TestDesired_Neutral_Empty(t *testing.T) {
	if d := Desired(nil); len(d) != 0 {
		t.Fatal(d)
	}
}

func TestDiff_Enter_SetsAndRecordsPrev(t *testing.T) {
	desired := map[string]string{"CLAUDE_CONFIG_DIR": "/a/claude", "TQ_WS": "a"}
	ops, st := Diff(desired, env(map[string]string{"CLAUDE_CONFIG_DIR": "/orig"}), State{}, "a")
	if !ops.Changed || ops.Set["CLAUDE_CONFIG_DIR"] != "/a/claude" || ops.Set["TQ_WS"] != "a" {
		t.Fatalf("%+v", ops)
	}
	if st.WS != "a" || *st.Prev["CLAUDE_CONFIG_DIR"] != "/orig" || st.Prev["TQ_WS"] != nil {
		t.Fatalf("%+v", st)
	}
	if _, ok := ops.Set[StateVar]; !ok {
		t.Fatal("state var must be set")
	}
}

func TestDiff_Leave_RestoresPrev(t *testing.T) {
	orig := "/orig"
	prev := State{WS: "a", Prev: map[string]*string{"CLAUDE_CONFIG_DIR": &orig, "TQ_WS": nil}}
	ops, st := Diff(map[string]string{}, env(map[string]string{"CLAUDE_CONFIG_DIR": "/a/claude", "TQ_WS": "a"}), prev, "")
	if ops.Set["CLAUDE_CONFIG_DIR"] != "/orig" {
		t.Fatalf("%+v", ops)
	}
	sort.Strings(ops.Unset)
	if !reflect.DeepEqual(ops.Unset, []string{"TQ_WS", StateVar}) {
		t.Fatalf("%v", ops.Unset)
	}
	if len(st.Prev) != 0 || st.WS != "" {
		t.Fatalf("%+v", st)
	}
}

func TestDiff_SwitchAToB_SingleStep(t *testing.T) {
	prev := State{WS: "a", Prev: map[string]*string{"CLAUDE_CONFIG_DIR": nil, "AZURE_CONFIG_DIR": nil, "TQ_WS": nil}}
	cur := env(map[string]string{"CLAUDE_CONFIG_DIR": "/a/claude", "AZURE_CONFIG_DIR": "/a/az", "TQ_WS": "a"})
	ops, st := Diff(map[string]string{"CLAUDE_CONFIG_DIR": "/b/claude", "TQ_WS": "b"}, cur, prev, "b")
	if ops.Set["CLAUDE_CONFIG_DIR"] != "/b/claude" || ops.Set["TQ_WS"] != "b" {
		t.Fatalf("%+v", ops)
	}
	if !reflect.DeepEqual(ops.Unset, []string{"AZURE_CONFIG_DIR"}) {
		t.Fatalf("%v", ops.Unset)
	}
	if _, ok := st.Prev["AZURE_CONFIG_DIR"]; ok {
		t.Fatal("restored var must leave state")
	}
	if ops.From != "a" || ops.To != "b" {
		t.Fatalf("%+v", ops)
	}
}

func TestDiff_Idempotent_NoChange(t *testing.T) {
	prev := State{WS: "a", Prev: map[string]*string{"TQ_WS": nil}}
	ops, _ := Diff(map[string]string{"TQ_WS": "a"}, env(map[string]string{"TQ_WS": "a"}), prev, "a")
	if ops.Changed {
		t.Fatalf("%+v", ops)
	}
}

func TestState_RoundTrip(t *testing.T) {
	v := "x"
	s := State{WS: "a", Prev: map[string]*string{"K": &v, "L": nil}}
	d := DecodeState(s.Encode())
	if d.WS != "a" || *d.Prev["K"] != "x" || d.Prev["L"] != nil {
		t.Fatalf("%+v", d)
	}
	if d := DecodeState("garbage!!"); d.WS != "" || len(d.Prev) != 0 {
		t.Fatal("garbage must decode to empty")
	}
}
