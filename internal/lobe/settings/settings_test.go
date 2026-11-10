package settings

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// base is a client's manifest as tentaqles writes it, with no lobe key; other_providers is the last
// top-level key, so a lobe key added by Edit must land after it.
const base = `# tentaqles workspace manifest
schema: tentaqles-client-v2
client: acme
git:
    email: dev@acme.test
identities:
    claude: {}
other_providers: [In-house]
`

func parseManifest(t *testing.T, raw string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(raw), "test")
	if err != nil {
		t.Fatalf("manifest.Parse: %v\n%s", err, raw)
	}
	return m
}

// withLobe is base plus a lobe block with the given body (each line indented by four spaces).
func withLobe(body string) string { return base + "lobe:\n" + body }

// the defaults of core page 5.6 and F-06 step 1, in print order.
var defaults = []Setting{
	{Capture, "on", SourceDefault},
	{Recall, "on", SourceDefault},
	{Lessons, "on", SourceDefault},
	{Graph, "on", SourceDefault},
	{Enforce, "advice", SourceDefault},
	{Share, "private", SourceDefault},
	{Model, "on", SourceDefault},
	{Documents, "off", SourceDefault},
	{RetentionDays, "14", SourceDefault},
}

// the wrong values of F-06 step 1, one key at a time: the value a manifest holds, and the safe value read.
var wrong = []struct{ key, bad, safe string }{
	{Capture, "maybe", "off"},
	{Recall, "maybe", "off"},
	{Lessons, "maybe", "off"},
	{Graph, "maybe", "off"},
	{Enforce, "maybe", "off"},
	{Share, "public", "private"},
	{Model, "maybe", "off"},
	{Documents, "maybe", "off"},
	{RetentionDays, "lots", "14"},
}

func TestLobeSettingsDefaults(t *testing.T) {
	ws := &resolve.Workspace{Manifest: parseManifest(t, base)}
	got := From(ws)
	if len(got) != 9 {
		t.Fatalf("%d settings, want 9", len(got))
	}
	ok := 0
	for i, want := range defaults {
		if got[i] == want {
			ok++
		} else {
			t.Errorf("setting %d = %+v, want %+v", i, got[i], want)
		}
	}
	t.Logf("%d of 9 defaults", ok)
	if got.Invalid() {
		t.Error("no lobe key reads as invalid")
	}
	if FromManifest(nil)[0] != defaults[0] || From(nil)[8] != defaults[8] {
		t.Error("a nil manifest or workspace should give the defaults")
	}
}

func TestLobeSettingsSafeValues(t *testing.T) {
	ok := 0
	for _, w := range wrong {
		raw := withLobe("    " + w.key + ": " + w.bad + "\n")
		got := FromManifest(parseManifest(t, raw))
		s, found := got.Get(w.key)
		if !found || s.Value != w.safe || s.Source != SourceInvalid {
			t.Errorf("%s: %+v, want value %q from invalid", w.key, s, w.safe)
			continue
		}
		for _, other := range got {
			if other.Key != w.key && other.Source != SourceDefault {
				t.Errorf("%s: a wrong %s also marked %+v", w.key, w.key, other)
			}
		}
		if !got.Invalid() {
			t.Errorf("%s: Invalid() is false", w.key)
		}
		ok++
	}
	t.Logf("%d of 9 safe values", ok)
	if ok != 9 {
		t.Fatalf("%d of 9", ok)
	}
}

// A lobe value that is not a mapping makes every key invalid (F-06 step 2).
func TestLobeSettings_NotAMappingIsAllInvalid(t *testing.T) {
	got := FromManifest(parseManifest(t, base+"lobe: 7\n"))
	for _, s := range got {
		if s.Source != SourceInvalid {
			t.Errorf("%+v: want invalid", s)
		}
	}
	if len(got) != 9 || !got.Invalid() {
		t.Fatalf("%v", got.Lines())
	}
}

// on and off read bare or quoted, and true, false, yes and no read as on and off.
func TestLobeSettings_OnOffForms(t *testing.T) {
	cases := map[string]string{
		"    capture: on\n":      "on",
		"    capture: \"off\"\n": "off",
		"    capture: true\n":    "on",
		"    capture: no\n":      "off",
		"    capture: yes\n":     "on",
		"    capture: false\n":   "off",
	}
	for body, want := range cases {
		got := FromManifest(parseManifest(t, withLobe(body)))
		if s, _ := got.Get(Capture); s.Value != want || s.Source != SourceManifest {
			t.Errorf("%q: %+v, want %s from manifest", body, s, want)
		}
	}
}

func TestLobeSettings_Lines(t *testing.T) {
	got := FromManifest(parseManifest(t, withLobe("    model: off\n    documents: on\n")))
	want := []string{
		"capture=on (default)",
		"recall=on (default)",
		"lessons=on (default)",
		"graph=on (default)",
		"enforce=advice (default)",
		"share=private (default)",
		"model=off (manifest)",
		"documents=on (manifest, needs model)",
		"retention_days=14 (default)",
	}
	lines := got.Lines()
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
	if len(lines) != 9 {
		t.Errorf("%d lines", len(lines))
	}
}

func TestLobeSettings_Reminders(t *testing.T) {
	both := FromManifest(parseManifest(t, withLobe("    recall: off\n    enforce: block\n")))
	if r := Reminders(both); len(r) != 2 || !strings.Contains(r[0], "recall is off") || !strings.Contains(r[1], "blocking ships") {
		t.Errorf("reminders %q", r)
	}
	if r := Reminders(FromManifest(parseManifest(t, base))); len(r) != 0 {
		t.Errorf("defaults give reminders %q", r)
	}
	docs := FromManifest(parseManifest(t, withLobe("    documents: on\n    graph: off\n")))
	if r := Reminders(docs); len(r) != 1 || !strings.Contains(r[0], "while graph is off") {
		t.Errorf("documents reminders %q", r)
	}
}

func TestLobeSettings_AllowedAndValid(t *testing.T) {
	if got := strings.Join(Allowed(Enforce), ","); got != "off,advice,block" {
		t.Errorf("enforce allows %s", got)
	}
	if got := strings.Join(Allowed(Capture), ","); got != "on,off" {
		t.Errorf("capture allows %s", got)
	}
	if Allowed(RetentionDays) != nil {
		t.Error("retention_days has a range, not a list")
	}
	if !ValidValue(RetentionDays, "365") || ValidValue(RetentionDays, "366") || ValidValue(RetentionDays, "014") {
		t.Error("retention_days range or form")
	}
	if ValidValue(Capture, "true") || ValidValue(Capture, "On") {
		t.Error("Edit takes on and off exactly; aliases are read only")
	}
	if len(Keys()) != 9 || Keys()[0] != Capture || Keys()[8] != RetentionDays {
		t.Errorf("keys %v", Keys())
	}
}
