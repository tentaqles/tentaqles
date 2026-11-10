package settings

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// outsideLobe is the text of a manifest with its lobe block removed: the block is the line "lobe:" and the
// lines after it up to the next line that starts at column 0.
func outsideLobe(text string) string {
	var out []string
	in := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "lobe:") {
			in = true
			continue
		}
		if in && line != "" && !strings.HasPrefix(line, " ") {
			in = false
		}
		if !in {
			out = append(out, line)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// topKeys lists the top-level keys of a manifest in order.
func topKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var keys []string
	top := doc.Content[0]
	for i := 0; i < len(top.Content); i += 2 {
		keys = append(keys, top.Content[i].Value)
	}
	return keys
}

func edit(t *testing.T, raw []byte, key, value string) []byte {
	t.Helper()
	out, err := Edit(raw, key, value)
	if err != nil {
		t.Fatalf("Edit(%s=%s): %v", key, value, err)
	}
	return out
}

// Only the lobe block changes; a set that changes nothing returns the same bytes; default removes the key
// and then the empty mapping; the new mapping is the last top-level key.
func TestLobeSet_ChangesOnlyTheLobeKey(t *testing.T) {
	raw := []byte(base)

	off := edit(t, raw, Capture, "off")
	if outsideLobe(string(off)) != outsideLobe(base) {
		t.Errorf("text outside lobe changed:\n%s", off)
	}
	if !strings.Contains(string(off), "lobe:") || bytes.Equal(off, raw) {
		t.Fatalf("no lobe block written:\n%s", off)
	}
	if got := FromManifest(parseManifest(t, string(off))); got[0].Value != "off" || got[0].Source != SourceManifest {
		t.Errorf("capture reads back as %+v", got[0])
	}
	if k := topKeys(t, off); k[len(k)-1] != "lobe" {
		t.Errorf("lobe is not the last top-level key: %v", k)
	}

	again := edit(t, off, Capture, "off")
	if !bytes.Equal(again, off) {
		t.Errorf("same value changed the bytes:\n%s", again)
	}

	both := edit(t, off, Model, "off")
	if outsideLobe(string(both)) != outsideLobe(base) || !strings.Contains(string(both), "capture") || !strings.Contains(string(both), "model") {
		t.Errorf("second key:\n%s", both)
	}

	// default on one key keeps the other.
	one := edit(t, both, Capture, "default")
	if got := FromManifest(parseManifest(t, string(one))); got[0].Source != SourceDefault || got[6].Value != "off" {
		t.Errorf("after default capture: %v", got.Lines())
	}
	if strings.Contains(string(one), "capture") {
		t.Errorf("capture still written:\n%s", one)
	}

	// default on the last key removes the empty lobe mapping, and so the lobe key.
	none := edit(t, one, Model, "default")
	if !bytes.Equal(none, raw) {
		t.Errorf("default on the last key:\n%s\nwant the manifest without a lobe key:\n%s", none, raw)
	}
	if strings.Contains(string(none), "lobe") {
		t.Errorf("empty lobe mapping left:\n%s", none)
	}

	// default on a key that is not set changes nothing.
	if same := edit(t, raw, Capture, "default"); !bytes.Equal(same, raw) {
		t.Errorf("default on an absent key changed the bytes")
	}
}

// Setting a value the key already holds in another form (true for on) changes nothing; a wrong value on the
// key is replaced; a lobe value that is not a mapping is refused rather than overwritten.
func TestLobeSet_SameValueAndRefusals(t *testing.T) {
	raw := []byte(withLobe("    capture: true\n"))
	if out := edit(t, raw, Capture, "on"); !bytes.Equal(out, raw) {
		t.Errorf("true then on changed the bytes:\n%s", out)
	}
	bad := []byte(withLobe("    capture: maybe\n"))
	if out := edit(t, bad, Capture, "off"); !strings.Contains(string(out), "off") || strings.Contains(string(out), "maybe") {
		t.Errorf("wrong value not replaced:\n%s", out)
	}
	if _, err := Edit([]byte(base), "nothing", "on"); err == nil {
		t.Error("unknown key accepted")
	}
	for _, v := range []string{"maybe", "true", "Block", ""} {
		if _, err := Edit([]byte(base), Capture, v); err == nil {
			t.Errorf("value %q accepted for capture", v)
		}
	}
	if _, err := Edit([]byte(base), Enforce, "on"); err == nil {
		t.Error("on accepted for enforce")
	}
	if _, err := Edit([]byte(base+"lobe: 7\n"), Capture, "off"); err == nil {
		t.Error("a lobe value that is not a mapping was overwritten")
	}
}

// The written bytes read back the same, whether the encoder quoted on and off or not.
func TestLobeSet_OnOffReadBack(t *testing.T) {
	for _, v := range []string{"on", "off"} {
		out := edit(t, []byte(base), Graph, v)
		if !strings.Contains(string(out), "graph: \""+v+"\"\n") {
			t.Errorf("%s is not written in double quotes:\n%s", v, out)
		}
		if got := FromManifest(parseManifest(t, string(out))); got[3].Value != v {
			t.Errorf("%s reads back as %s:\n%s", v, got[3].Value, out)
		}
	}
	out := edit(t, []byte(base), RetentionDays, "30")
	if got := FromManifest(parseManifest(t, string(out))); got[8].Value != "30" || got[8].Source != SourceManifest {
		t.Errorf("retention_days 30 reads back as %+v:\n%s", got[8], out)
	}
}

// ownFormat is a manifest as tentaqles writes it: 4-space indent, block sequences, flow mappings for empty
// values (the format of reach and workspace).
const ownFormat = `# tentaqles workspace manifest
schema: tentaqles-client-v2
client: acme
git:
    email: dev@acme.test
identities:
    claude: {}
other_providers:
    - In-house
`

// On a manifest in tentaqles' own format, an edit changes only the lobe lines: every other line keeps its
// bytes. default restores the bytes the manifest had before the first edit.
func TestLobeSet_OwnFormatOnlyLobeLinesChange(t *testing.T) {
	raw := []byte(ownFormat)
	off := edit(t, raw, Capture, "off")
	if outsideLobe(string(off)) != outsideLobe(ownFormat) {
		t.Errorf("a line outside lobe changed:\nbefore:\n%s\nafter:\n%s", ownFormat, off)
	}
	both := edit(t, off, Enforce, "block")
	if outsideLobe(string(both)) != outsideLobe(ownFormat) {
		t.Errorf("a second set changed a line outside lobe:\n%s", both)
	}
	back := edit(t, edit(t, both, Capture, "default"), Enforce, "default")
	if !bytes.Equal(back, raw) {
		t.Errorf("default did not restore the manifest's bytes:\n%s", back)
	}
}
