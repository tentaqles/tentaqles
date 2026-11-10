package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// TestLobeStatusJSON: one compact line with the five keys, and a foreign store.json is refused with an
// error that names the file.
func TestLobeStatusJSON(t *testing.T) {
	f := newHome(t)
	openAcme(t)
	writeDay(t, "20261109", `{"v":1,"kind":"fail"}`, `{"v":1,"kind":"fail"}`, `{"v":1,"kind":"fail"}`)

	var buf bytes.Buffer
	if err := Status(deps(false, nil, f.client("acme")), "", true, nil, false, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("--json must print one line, got %q", out)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(doc) != 5 {
		t.Fatalf("keys = %d, want 5 (client, store, settings, events, hooks)", len(doc))
	}
	for _, k := range []string{"client", "store", "settings", "events", "hooks"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("missing key %q in %s", k, out)
		}
	}
	for _, want := range []string{`"client":"acme"`, `"state":"ok"`, `"total":3`, `"fail":3`, `"claude":"none"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s is missing %s", out, want)
		}
	}
	var settings map[string]any
	if err := json.Unmarshal(doc["settings"], &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings) != 9 || settings["capture"] != "on" {
		t.Fatalf("settings = %v, want the nine keys with capture on", settings)
	}
	if settings["retention_days"] != float64(14) {
		t.Fatalf("retention_days = %#v, want the number 14", settings["retention_days"])
	}
	if !strings.Contains(out, `"retention_days":14`) {
		t.Fatalf("retention_days is not a JSON number in %s", out)
	}

	// A store.json that names another client: exit 1 in the command, and an error that names the file.
	mustWrite(t, filepath.Join(paths.LobeDir("acme"), "store.json"),
		`{"v":1,"client":"globex","created":"2026-01-01T00:00:00Z"}`+"\n")
	buf.Reset()
	err := Status(deps(false, nil, f.client("acme")), "", true, nil, false, &buf)
	if err == nil {
		t.Fatal("a foreign store.json was not refused")
	}
	if !strings.Contains(err.Error(), "store.json") {
		t.Fatalf("refusal does not name store.json: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("a refused status printed %q", buf.String())
	}
}

// TestLobeStatusCounts: counts by kind, and one broken line is skipped and counted.
func TestLobeStatusCounts(t *testing.T) {
	f := newHome(t)
	openAcme(t)
	writeDay(t, "20261109",
		`{"v":1,"kind":"fail"}`,
		`{"v":1,"kind":"ok"}`,
		`{"v":1,"kind":"hidden-fail"}`,
		`{"v":1,"kind":"fail"}`,
		`{"v":`)
	var buf bytes.Buffer
	if err := Status(deps(false, nil, f.client("acme")), "", true, nil, false, &buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"events":{"total":4,"fail":2,"ok":1,"hidden_fail":1,"days":1,"skipped":1}`,
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("%s is missing %s", buf.String(), want)
		}
	}
}

// TestLobeStatusHooks: the hooks state of the plugin's hooks file.
func TestLobeStatusHooks(t *testing.T) {
	ok := []byte(`{"hooks":{"PostToolUseFailure":[{"matcher":"*","hooks":[{"type":"command","command":"bash \"${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh\" post-tool-use-failure"}]}]}}`)
	noLobe := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash tq_hook.sh"}]}]}}`)
	cases := []struct {
		name  string
		data  []byte
		found bool
		want  string
	}{
		{"lobe entry", ok, true, "ok"},
		{"installed without the lobe", noLobe, true, "missing"},
		{"unreadable file", nil, true, "missing"},
		{"no plugin", nil, false, "none"},
	}
	for _, c := range cases {
		if got := hooksState(c.data, c.found); got != c.want {
			t.Errorf("%s: hooksState = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestLobeStatusLoginLine: the text names CLAUDE_CONFIG_DIR unless it is this client's own login folder.
func TestLobeStatusLoginLine(t *testing.T) {
	f := newHome(t)
	openAcme(t)
	cwd := f.client("acme")

	var unset bytes.Buffer
	if err := Status(deps(false, nil, cwd), "", false, nil, false, &unset); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unset.String(), "CLAUDE_CONFIG_DIR is unset") {
		t.Fatalf("unset login gives no line about CLAUDE_CONFIG_DIR:\n%s", unset.String())
	}

	own := map[string]string{"CLAUDE_CONFIG_DIR": paths.IdentityDir("acme", "claude")}
	var owned bytes.Buffer
	if err := Status(deps(false, own, cwd), "", false, nil, false, &owned); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(owned.String(), "CLAUDE_CONFIG_DIR") {
		t.Fatalf("the client's own login folder still gives a line:\n%s", owned.String())
	}
	for _, want := range []string{"client: acme", "store: ok", "enforce=advice (default)", "hooks: claude none"} {
		if !strings.Contains(owned.String(), want) {
			t.Fatalf("text status is missing %q:\n%s", want, owned.String())
		}
	}
}

// TestLobeStatusRefusesOtherLoginInsideAgent: inside an agent session a login that is not this client's
// refuses the status, as it refuses events.
func TestLobeStatusRefusesOtherLoginInsideAgent(t *testing.T) {
	f := newHome(t)
	openAcme(t)
	env := map[string]string{"CLAUDE_CONFIG_DIR": paths.IdentityDir("globex", "claude")}
	var buf bytes.Buffer
	if err := Status(deps(true, env, f.client("acme")), "", false, nil, false, &buf); err == nil {
		t.Fatal("status under another login was not refused")
	}
	if buf.Len() != 0 {
		t.Fatalf("refused status printed %q", buf.String())
	}
}
