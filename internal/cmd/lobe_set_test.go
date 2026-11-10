package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// lobeManifestBody is the acme manifest of the lobe tests: no lobe key, so every setting is a default.
const lobeManifestBody = "git:\n    email: dev@acme.test\nidentities:\n    claude: {}\nother_providers: [In-house]\n"

// lobeClient makes the acme client under a fresh base and sets the test's working folder to the
// client's folder.
func lobeClient(t *testing.T) {
	t.Helper()
	base := newBase(t)
	chdir(t, writeClient(t, base, "acme", lobeManifestBody))
}

// lobeManifestPath is the manifest of acme as the command reads it.
func lobeManifestPath(t *testing.T) string {
	t.Helper()
	ws, err := findWorkspace("acme")
	if err != nil {
		t.Fatal(err)
	}
	return ws.ManifestPath
}

// runLobe runs tentaqles with args and stdin as the typed answer; it returns the output and the error.
func runLobe(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func readBytesT(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// outsideLobeBlock is the text with its lobe block removed: the line "lobe:" and the indented lines
// after it.
func outsideLobeBlock(text string) string {
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

// TestLobePersonVerbsRefuseInAgent: lobe set exits non-zero under CLAUDECODE=1 and changes 0 bytes.
func TestLobePersonVerbsRefuseInAgent(t *testing.T) {
	lobeClient(t)
	mp := lobeManifestPath(t)
	before := readBytesT(t, mp)
	t.Setenv("CLAUDECODE", "1")
	out, err := runLobe(t, "", "lobe", "set", "capture", "off", "--yes")
	if err == nil {
		t.Fatalf("lobe set ran inside an agent:\n%s", out)
	}
	if !strings.Contains(err.Error(), "for a person") {
		t.Errorf("the refusal does not say it is for a person's terminal: %v", err)
	}
	if !bytes.Equal(readBytesT(t, mp), before) {
		t.Fatal("the manifest changed under CLAUDECODE=1")
	}
}

// A set with --yes changes only lobe.capture, and the new value reads back.
func TestLobeSet_YesChangesOnlyLobe(t *testing.T) {
	lobeClient(t)
	mp := lobeManifestPath(t)
	before := string(readBytesT(t, mp))
	out, err := runLobe(t, "", "lobe", "set", "capture", "off", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "lobe.capture: on -> off") {
		t.Errorf("the change is not listed:\n%s", out)
	}
	after := string(readBytesT(t, mp))
	if outsideLobeBlock(after) != outsideLobeBlock(before) {
		t.Errorf("text outside lobe changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(after, "lobe:") || after == before {
		t.Fatalf("no lobe block written:\n%s", after)
	}
	settingsOut, err := runLobe(t, "", "lobe", "settings")
	if err != nil {
		t.Fatalf("%v\n%s", err, settingsOut)
	}
	if !strings.Contains(settingsOut, "capture=off (manifest)") {
		t.Errorf("capture does not read back as off:\n%s", settingsOut)
	}
}

// The same value again prints Nothing changed and writes the same bytes.
func TestLobeSet_SameValueNothingChanged(t *testing.T) {
	lobeClient(t)
	mp := lobeManifestPath(t)
	if out, err := runLobe(t, "", "lobe", "set", "capture", "off", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	before := readBytesT(t, mp)
	out, err := runLobe(t, "", "lobe", "set", "capture", "off", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Nothing changed.") {
		t.Errorf("no Nothing changed line:\n%s", out)
	}
	if !bytes.Equal(readBytesT(t, mp), before) {
		t.Fatal("the same value changed the bytes")
	}
}

// A wrong value or an unknown key exits 1 and writes nothing.
func TestLobeSet_BadValueChangesNothing(t *testing.T) {
	lobeClient(t)
	mp := lobeManifestPath(t)
	before := readBytesT(t, mp)
	for _, args := range [][]string{
		{"lobe", "set", "capture", "maybe", "--yes"},
		{"lobe", "set", "capture", "true", "--yes"},
		{"lobe", "set", "nothing", "on", "--yes"},
		{"lobe", "set", "enforce", "on", "--yes"},
	} {
		if out, err := runLobe(t, "", args...); err == nil {
			t.Errorf("%v accepted:\n%s", args, out)
		}
		if !bytes.Equal(readBytesT(t, mp), before) {
			t.Fatalf("%v changed the manifest", args)
		}
	}
}

// default removes the key, and the manifest is the one it was before the set.
func TestLobeSet_DefaultRestores(t *testing.T) {
	lobeClient(t)
	mp := lobeManifestPath(t)
	before := readBytesT(t, mp)
	if out, err := runLobe(t, "", "lobe", "set", "capture", "off", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := runLobe(t, "", "lobe", "set", "capture", "default", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !bytes.Equal(readBytesT(t, mp), before) {
		t.Fatalf("default did not restore the manifest:\n%s", readBytesT(t, mp))
	}
	out, err := runLobe(t, "", "lobe", "settings")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "capture=on (default)") {
		t.Errorf("capture is not the default:\n%s", out)
	}
}

// Without --yes, an answer other than yes changes nothing; yes changes it.
func TestLobeSet_NoYesDeclinedChangesNothing(t *testing.T) {
	lobeClient(t)
	mp := lobeManifestPath(t)
	before := readBytesT(t, mp)
	for _, answer := range []string{"no\n", "\n", "y\n"} {
		out, err := runLobe(t, answer, "lobe", "set", "capture", "off")
		if err == nil {
			t.Fatalf("answer %q went through:\n%s", answer, out)
		}
		if !strings.Contains(out, "lobe.capture: on -> off") {
			t.Errorf("the change is not listed before the question:\n%s", out)
		}
		if !bytes.Equal(readBytesT(t, mp), before) {
			t.Fatalf("answer %q changed the manifest", answer)
		}
	}
	if out, err := runLobe(t, "yes\n", "lobe", "set", "capture", "off"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if bytes.Equal(readBytesT(t, mp), before) {
		t.Fatal("a typed yes changed nothing")
	}
}

// lobe settings prints the nine lines in the fixed order.
func TestLobeSettings_NineLinesInOrder(t *testing.T) {
	lobeClient(t)
	out, err := runLobe(t, "", "lobe", "settings")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	keys := []string{"capture", "recall", "lessons", "graph", "enforce", "share", "model", "documents", "retention_days"}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(keys) {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), len(keys), out)
	}
	for i, k := range keys {
		if !strings.HasPrefix(lines[i], k+"=") {
			t.Errorf("line %d = %q, want key %s", i, lines[i], k)
		}
	}
	if lines[4] != "enforce=advice (default)" {
		t.Errorf("enforce line = %q", lines[4])
	}
}

// --json prints one compact object on one line with the nine keys; retention_days is a number.
func TestLobeSettings_JSONOneLine(t *testing.T) {
	lobeClient(t)
	out, err := runLobe(t, "", "lobe", "settings", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	line := strings.TrimRight(out, "\n")
	if strings.Contains(line, "\n") || strings.Count(out, "\n") != 1 {
		t.Fatalf("not one line:\n%s", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, line)
	}
	if len(got) != 9 {
		t.Errorf("%d keys, want 9: %s", len(got), line)
	}
	if got["capture"] != "on" || got["enforce"] != "advice" || got["retention_days"] != float64(14) {
		t.Errorf("values: %s", line)
	}
	if strings.Index(line, `"capture"`) > strings.Index(line, `"retention_days"`) {
		t.Errorf("keys out of order: %s", line)
	}
}

// The reminder line appears when recall is off and enforce is block.
func TestLobeSet_ReminderLine(t *testing.T) {
	lobeClient(t)
	if out, err := runLobe(t, "", "lobe", "set", "recall", "off", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err := runLobe(t, "", "lobe", "set", "enforce", "block", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "recall is off, but enforce is block") {
		t.Errorf("no reminder for recall off with enforce block:\n%s", out)
	}
	if !strings.Contains(out, "acts as advice until blocking ships") {
		t.Errorf("no reminder for enforce block:\n%s", out)
	}
}

// changingAnswer is a typed answer that changes the manifest first, as another process would while the
// question waits: its first read writes body to path, then it gives the answer.
type changingAnswer struct {
	path, answer string
	body         []byte
	done         bool
}

func (r *changingAnswer) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		if err := os.WriteFile(r.path, r.body, 0o600); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.answer)
	r.answer = r.answer[n:]
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// A manifest that changes while lobe set waits for the typed yes is not overwritten: the command exits
// with an error that says the manifest changed and to run it again, and the other bytes stay.
func TestLobeSet_ManifestChangedWhileConfirmingWritesNothing(t *testing.T) {
	lobeClient(t)
	mp := lobeManifestPath(t)
	before := readBytesT(t, mp)
	changed := append(append([]byte{}, before...), []byte("# written by someone else meanwhile\n")...)

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&changingAnswer{path: mp, body: changed, answer: "yes\n"})
	root.SetArgs([]string{"lobe", "set", "capture", "off"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("lobe set wrote over a manifest that changed meanwhile:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "changed meanwhile") || !strings.Contains(err.Error(), "run the command again") {
		t.Errorf("the message does not say the manifest changed and to run the command again: %v", err)
	}
	if !bytes.Equal(readBytesT(t, mp), changed) {
		t.Fatalf("the manifest was overwritten:\n%s", readBytesT(t, mp))
	}
}
