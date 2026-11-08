package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkerBytes_ExactContent(t *testing.T) {
	got := string(MarkerBytes("acme"))
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "# ") || !strings.Contains(lines[0], "names only") {
		t.Fatalf("marker must be one comment line plus two fields:\n%s", got)
	}
	if lines[1] != "schema: "+MarkerSchema || lines[2] != "client: acme" {
		t.Fatalf("%s", got)
	}
}

func TestPeek_TellsAMarker(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	k, c, err := Peek(write("marker.yaml", string(MarkerBytes("acme"))))
	if err != nil || k != KindMarker || c != "acme" {
		t.Fatalf("%v %q %v", k, c, err)
	}
	k, _, err = Peek(write("manifest.yaml", "schema: tentaqles-client-v2\nclient: acme\n"))
	if err != nil || k != KindUnknown {
		t.Fatalf("a manifest is no marker: %v %v", k, err)
	}
	k, _, err = Peek(write("other.yaml", "schema: nope\n"))
	if err != nil || k != KindUnknown {
		t.Fatalf("%v %v", k, err)
	}
	if _, _, err := Peek(write("empty-client.yaml", "schema: "+MarkerSchema+"\n")); !errors.Is(err, ErrMarkerClient) {
		t.Fatalf("a marker without a client is an error, got %v", err)
	}
	if _, _, err := Peek(write("bad.yaml", "schema: [\n")); err == nil {
		t.Fatal("unparseable YAML must be an error")
	}
	if _, _, err := Peek(filepath.Join(dir, "missing.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%v", err)
	}
}

// A marker round-trips a name that YAML would otherwise mangle.
func TestMarkerBytes_RoundTripsAnOddName(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.yaml")
	if err := os.WriteFile(p, MarkerBytes("a: b"), 0o600); err != nil {
		t.Fatal(err)
	}
	k, c, err := Peek(p)
	if err != nil || k != KindMarker || c != "a: b" {
		t.Fatalf("%v %q %v", k, c, err)
	}
}
