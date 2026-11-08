package envplan

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
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
