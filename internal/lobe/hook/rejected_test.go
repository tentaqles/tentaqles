package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// transcriptLine marshals v as one JSON line of a transcript, with its line break.
func transcriptLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// useLine is an assistant line holding one tool_use block.
func useLine(t *testing.T, id, name string, input map[string]any) string {
	t.Helper()
	return transcriptLine(t, map[string]any{
		"type":      "assistant",
		"sessionId": "s1",
		"message": map[string]any{
			"role": "assistant",
			"content": []any{map[string]any{
				"type": "tool_use", "id": id, "name": name, "input": input,
			}},
		},
	})
}

// resultLine is a user line holding one tool_result block. wrapped is true for a result Claude Code
// rejected before the call ran (the text is wrapped in tool_use_error); false gives a plain error.
func resultLine(t *testing.T, id, text string, wrapped bool) string {
	t.Helper()
	content := text
	if wrapped {
		content = "<tool_use_error>" + text + "</tool_use_error>"
	}
	return transcriptLine(t, map[string]any{
		"type":      "user",
		"sessionId": "s1",
		"message": map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type": "tool_result", "content": content, "is_error": true, "tool_use_id": id,
			}},
		},
	})
}

// rejectedEdit is a rejected Edit: its tool_use and its wrapped result, as two lines.
func rejectedEdit(t *testing.T, id, oldString string) string {
	t.Helper()
	return useLine(t, id, "Edit", map[string]any{
		"file_path": `C:\x\notes.txt`, "old_string": oldString, "new_string": "x",
	}) + resultLine(t, id, "String to replace not found in file.\nString: "+oldString, true)
}

// appendText appends text to the file p, creating it and the folders above it when missing.
func appendText(t *testing.T, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// stopJSON is the Stop payload of a session whose transcript is at transcript.
func stopJSON(t *testing.T, cwd, transcript string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"session_id":      "s1",
		"hook_event_name": "Stop",
		"cwd":             cwd,
		"transcript_path": transcript,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// acmeTranscript is the transcript of acme's session s1, in acme's login folder.
func acmeTranscript() string {
	return filepath.Join(paths.IdentityDir("acme", "claude"), "projects", "p", "s1.jsonl")
}

func TestLobeRejectedStop(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()

	// (a) One rejected Edit gives one line.
	appendText(t, T, rejectedEdit(t, "toolu_a1", "ZZZ"))
	if out := runHook(t, "stop", stopJSON(t, app, T)); out != "" {
		t.Fatalf("stop wrote %q to stdout, want nothing", out)
	}
	ls := lines(t, "acme")
	if len(ls) != 1 {
		t.Fatalf("(a) %d lines, want 1", len(ls))
	}
	got := decodeLogged(t, ls)[0]
	if got.Kind != "fail" || got.Exit != nil {
		t.Errorf("(a) line kind %q exit %v, want fail and no exit", got.Kind, got.Exit)
	}
	if !strings.HasPrefix(got.Error, "String to replace not found in file.") {
		t.Errorf("(a) error = %q", got.Error)
	}
	if want := eventlog.ID("claude", "s1", "toolu_a1"); got.ID != want {
		t.Errorf("(a) id = %s, want %s", got.ID, want)
	}

	// (b) The same transcript again adds nothing: the cursor has passed it.
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("(b) %d lines, want 1", n)
	}

	// (c) A plain Bash failure is not counted; a rejected Write is.
	appendText(t, T, useLine(t, "toolu_b1", "Bash", map[string]any{"command": "npm run buld"})+
		resultLine(t, "toolu_b1", "Exit code 1\nnpm error Missing script: buld", false))
	appendText(t, T, useLine(t, "toolu_a2", "Write", map[string]any{
		"file_path": `C:\x\b.txt`, "content": "hi",
	})+resultLine(t, "toolu_a2", "File has not been read yet.", true))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 2 {
		t.Fatalf("(c) %d lines, want 2", n)
	}

	// (d) A call already logged by post-tool-use-failure is not written again.
	runHook(t, evPost, payloadJSON("s1", app, "Edit", "toolu_a3", "", "String to replace not found in file."))
	if n := len(lines(t, "acme")); n != 3 {
		t.Fatalf("(d) after the failure hook: %d lines, want 3", n)
	}
	appendText(t, T, rejectedEdit(t, "toolu_a3", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 3 {
		t.Fatalf("(d) after stop: %d lines, want 3, not 4", n)
	}

	// (e) A secret in the edit's old_string is redacted, and appears in no file of the store.
	secret := "ghp_" + strings.Repeat("aB3", 12)
	appendText(t, T, rejectedEdit(t, "toolu_a4", secret))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 4 {
		t.Fatalf("(e) %d lines, want 4", n)
	}
	err := filepath.WalkDir(paths.LobeDir("acme"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), "ghp_") {
			t.Errorf("(e) %s holds the secret", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// (f) The memory's own command, rejected, is not logged.
	appendText(t, T, useLine(t, "toolu_a5", "Bash", map[string]any{"command": "tq lobe status"})+
		resultLine(t, "toolu_a5", "Unknown failure.", true))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 4 {
		t.Fatalf("(f) %d lines, want 4", n)
	}

	// (g) A part line at the end waits for its line break, then is read once.
	use := useLine(t, "toolu_a6", "Edit", map[string]any{"file_path": `C:\x\notes.txt`, "old_string": "QQQ", "new_string": "x"})
	full := use + resultLine(t, "toolu_a6", "String to replace not found in file.\nString: QQQ", true)
	half := full[:len(use)/2]
	appendText(t, T, half)
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 4 {
		t.Fatalf("(g) with a part line: %d lines, want 4", n)
	}
	appendText(t, T, full[len(half):])
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 5 {
		t.Fatalf("(g) after the line is completed: %d lines, want 5", n)
	}

	// (h) The cursor is under cursors/.
	cursors, err := filepath.Glob(filepath.Join(paths.LobeDir("acme"), "cursors", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cursors) != 1 {
		t.Errorf("(h) %d cursor files, want 1", len(cursors))
	}
}

func TestLobeTranscriptOnlyUnderLogin(t *testing.T) {
	cases := []struct {
		name string
		// place writes the transcript and returns the path the payload names.
		place func(t *testing.T, f fixture) string
	}{
		{"another client's login folder", func(t *testing.T, f fixture) string {
			p := filepath.Join(paths.IdentityDir("globex", "claude"), "projects", "p", "s.jsonl")
			appendText(t, p, rejectedEdit(t, "toolu_t1", "ZZZ"))
			return p
		}},
		{"sibling folder projectsX", func(t *testing.T, f fixture) string {
			p := filepath.Join(paths.IdentityDir("acme", "claude"), "projectsX", "p", "s.jsonl")
			appendText(t, p, rejectedEdit(t, "toolu_t2", "ZZZ"))
			return p
		}},
		{"dot-dot out of projects", func(t *testing.T, f fixture) string {
			login := paths.IdentityDir("acme", "claude")
			appendText(t, filepath.Join(login, "elsewhere", "s.jsonl"), rejectedEdit(t, "toolu_t3", "ZZZ"))
			// Joined by hand: filepath.Join would clean the .. away before the hook sees it.
			return login + string(filepath.Separator) + "projects" + string(filepath.Separator) +
				".." + string(filepath.Separator) + "elsewhere" + string(filepath.Separator) + "s.jsonl"
		}},
		{"the projects folder itself", func(t *testing.T, f fixture) string {
			p := filepath.Join(paths.IdentityDir("acme", "claude"), "projects")
			mustMkdir(t, p)
			return p
		}},
		{"a link to a transcript outside", func(t *testing.T, f fixture) string {
			outside := filepath.Join(f.tmp, "outside.jsonl")
			appendText(t, outside, rejectedEdit(t, "toolu_t5", "ZZZ"))
			link := filepath.Join(paths.IdentityDir("acme", "claude"), "projects", "p", "link.jsonl")
			mustMkdir(t, filepath.Dir(link))
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlinks are not available here: %v", err)
			}
			return link
		}},
		{"a junction projects\\p to a folder outside", func(t *testing.T, f fixture) string {
			requireWindows(t)
			outside := filepath.Join(f.tmp, "outside-p")
			appendText(t, filepath.Join(outside, "s.jsonl"), rejectedEdit(t, "toolu_j1", "ZZZ"))
			login := paths.IdentityDir("acme", "claude")
			mkJunction(t, filepath.Join(login, "projects", "p"), outside)
			return filepath.Join(login, "projects", "p", "s.jsonl")
		}},
		{"projects itself a junction to a folder outside", func(t *testing.T, f fixture) string {
			requireWindows(t)
			outside := filepath.Join(f.tmp, "outside-projects")
			appendText(t, filepath.Join(outside, "p", "s.jsonl"), rejectedEdit(t, "toolu_j2", "ZZZ"))
			login := paths.IdentityDir("acme", "claude")
			mkJunction(t, filepath.Join(login, "projects"), outside)
			return filepath.Join(login, "projects", "p", "s.jsonl")
		}},
		{"CLAUDE_CONFIG_DIR a junction to acme's login folder", func(t *testing.T, f fixture) string {
			// The login folder is spelled differently from acme's, so loginMatches refuses it before the
			// path check runs. The case stays as proof that nothing is written through the junction.
			requireWindows(t)
			alt := filepath.Join(f.tmp, "alt-login")
			mkJunction(t, alt, paths.IdentityDir("acme", "claude"))
			t.Setenv("CLAUDE_CONFIG_DIR", alt)
			p := filepath.Join(alt, "projects", "p", "s.jsonl")
			appendText(t, p, rejectedEdit(t, "toolu_j3", "ZZZ"))
			return p
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHome(t)
			setLogin(t, f, "acme")
			T := c.place(t, f)
			before := identityTrees(t, f)
			runHook(t, "stop", stopJSON(t, f.app("acme"), T))
			if after := identityTrees(t, f); after != before {
				t.Errorf("an identity folder changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if n := len(lines(t, "acme")); n != 0 {
				t.Errorf("%d event lines, want 0", n)
			}
		})
	}
}

func TestLobeRejectedNoEmptyStore(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	T := acmeTranscript()
	appendText(t, T, useLine(t, "toolu_ok", "Bash", map[string]any{"command": "npm test"})+
		transcriptLine(t, map[string]any{
			"type":      "user",
			"sessionId": "s1",
			"message": map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type": "tool_result", "content": "all tests passed", "is_error": false, "tool_use_id": "toolu_ok",
				}},
			},
		}))
	runHook(t, "stop", stopJSON(t, f.app("acme"), T))
	if exists(paths.LobeDir("acme")) {
		t.Fatalf("a lobe folder was created for a transcript with no rejected call")
	}
}

// requireWindows skips the test on systems where a junction is not a link.
func requireWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("a junction is a Windows link")
	}
}

// mkJunction makes link a junction to target with mklink /J, or skips the test when mklink fails.
func mkJunction(t *testing.T, link, target string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(link))
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J failed: %v: %s", err, out)
	}
}

// todayFile is the path of today's day file (UTC) of acme's store.
func todayFile(t *testing.T) string {
	t.Helper()
	rel, err := eventlog.FileFor(time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(paths.LobeDir("acme"), filepath.FromSlash(rel))
}

// idCount is how many event lines of client's store carry the id.
func idCount(t *testing.T, client, id string) int {
	t.Helper()
	n := 0
	for _, e := range decodeLogged(t, lines(t, client)) {
		if e.ID == id {
			n++
		}
	}
	return n
}

// cursorOffset is the offset in acme's cursor file, or -1 when there is none.
func cursorOffset(t *testing.T) int64 {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(paths.LobeDir("acme"), "cursors", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		return -1
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Offset int64 `json:"offset"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c.Offset
}

// giantLine is one JSON line of n bytes with no line break inside it, and its break.
func giantLine(n int) string {
	return `{"type":"user","pad":"` + strings.Repeat("x", n) + `"}` + "\n"
}

// TestLobeOversizedLine: a line longer than the read window is skipped a window at a time, its tail
// does not parse, and the rejected pair after it is logged once.
func TestLobeOversizedLine(t *testing.T) {
	t.Run("with a stored cursor", func(t *testing.T) {
		f := newHome(t)
		setLogin(t, f, "acme")
		T := acmeTranscript()
		appendText(t, T, rejectedEdit(t, "toolu_o0", "ZZZ"))
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		if n := len(lines(t, "acme")); n != 1 {
			t.Fatalf("first pair: %d lines, want 1", n)
		}
		appendText(t, T, giantLine(9<<20)+rejectedEdit(t, "toolu_o1", "QQQ"))
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		if n := len(lines(t, "acme")); n != 2 {
			t.Fatalf("after two runs: %d lines, want 2", n)
		}
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		if n := idCount(t, "acme", eventlog.ID("claude", "s1", "toolu_o1")); n != 1 {
			t.Errorf("the pair after the long line is logged %d times, want 1", n)
		}
	})

	t.Run("at the tail with no cursor, the window holds no line break", func(t *testing.T) {
		f := newHome(t)
		setLogin(t, f, "acme")
		T := acmeTranscript()
		appendText(t, T, rejectedEdit(t, "toolu_o0", "ZZZ"))
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		for _, c := range mustGlob(t, filepath.Join(paths.LobeDir("acme"), "cursors", "*.json")) {
			if err := os.Remove(c); err != nil {
				t.Fatal(err)
			}
		}
		// A line still being written: 17 MiB and no break yet.
		appendText(t, T, `{"type":"user","pad":"`+strings.Repeat("x", 17<<20))
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		if n := len(lines(t, "acme")); n != 1 {
			t.Fatalf("partial long line: %d lines, want 1", n)
		}
		if got, want := cursorOffset(t), fileSize(t, T); got != want {
			t.Fatalf("cursor at %d after the skipped window, want the end of the file %d", got, want)
		}
		appendText(t, T, "\"}\n"+rejectedEdit(t, "toolu_o2", "QQQ"))
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		runHook(t, "stop", stopJSON(t, f.app("acme"), T))
		if n := idCount(t, "acme", eventlog.ID("claude", "s1", "toolu_o2")); n != 1 {
			t.Errorf("the pair after the long line is logged %d times, want 1", n)
		}
	})

	t.Run("no store: the skip writes nothing", func(t *testing.T) {
		f := newHome(t)
		setLogin(t, f, "acme")
		appendText(t, acmeTranscript(), `{"type":"user","pad":"`+strings.Repeat("x", 17<<20))
		runHook(t, "stop", stopJSON(t, f.app("acme"), acmeTranscript()))
		if exists(paths.LobeDir("acme")) {
			t.Fatalf("a lobe folder was created by a run with nothing to record")
		}
	})
}

// TestLobeRejectedIDSet: a call is logged once even when another line of the day file holds its id
// inside its input; only the top-level id of a line counts.
func TestLobeRejectedIDSet(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_k0", "npm run buld", "Exit code 1\nnpm error Missing script: buld"))
	want := eventlog.ID("claude", "s1", "toolu_k1")
	decoy, err := json.Marshal(map[string]string{"id": "evt_decoy", "input": `{"id":"` + want + `"}`})
	if err != nil {
		t.Fatal(err)
	}
	appendText(t, todayFile(t), string(decoy)+"\n")
	appendText(t, T, rejectedEdit(t, "toolu_k1", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := idCount(t, "acme", want); n != 1 {
		t.Fatalf("the call is logged %d times, want 1", n)
	}
	runHook(t, "stop", stopJSON(t, app, T))
	if n := idCount(t, "acme", want); n != 1 {
		t.Fatalf("after a second stop: %d, want 1", n)
	}
}

// TestLobeRejectedIDSetCancelled: a day-file scan whose context is done reports not ok, so nothing is
// written.
func TestLobeRejectedIDSetCancelled(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	runHook(t, evPost, payloadJSON("s1", f.app("acme"), "Bash", "toolu_k2", "npm run buld", "Exit code 1\nnpm error Missing script: buld"))
	ro, err := store.Open("acme", store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := loggedIDs(ctx, ro, time.Now()); ok {
		t.Errorf("a scan with a done context reported ok")
	}
	if ids, ok := loggedIDs(context.Background(), ro, time.Now()); !ok || len(ids) != 1 {
		t.Errorf("a live scan gave ok %v and %d ids, want ok and 1", ok, len(ids))
	}
}

// TestLobeRejectedNoCallsReadsNoDayFile: a transcript with no rejected call does not read the day
// files. Today's day file is made a folder here, so a read of it would fail; the cursor still moves.
func TestLobeRejectedNoCallsReadsNoDayFile(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_n0", "npm run buld", "Exit code 1\nnpm error Missing script: buld"))
	day := todayFile(t)
	if err := os.Remove(day); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(day, 0o700); err != nil {
		t.Fatal(err)
	}
	appendText(t, T, useLine(t, "toolu_n1", "Bash", map[string]any{"command": "npm test"})+
		resultLine(t, "toolu_n1", "all tests passed", false))
	runHook(t, "stop", stopJSON(t, app, T))
	if got, want := cursorOffset(t), fileSize(t, T); got != want {
		t.Fatalf("cursor at %d, want the end of the transcript %d: the day file was read", got, want)
	}
}

// TestLobeRejectedSplitLookBack: a cursor that falls between a tool_use line and its rejected result
// still names the call's tool. A rejected call of the memory's own tool, split the same way, is skipped.
func TestLobeRejectedSplitLookBack(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	// The filler takes the file past the 4 KiB head, so the cursor's head stays the same later on.
	appendText(t, T, `{"type":"user","pad":"`+strings.Repeat("y", 5000)+`"}`+"\n"+rejectedEdit(t, "toolu_s0", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, T))

	// The tool_use is written and the run stops before its result.
	appendText(t, T, useLine(t, "toolu_s1", "Edit", map[string]any{
		"file_path": `C:\x\split.txt`, "old_string": "SPLIT", "new_string": "x",
	}))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("before the result: %d lines, want 1", n)
	}
	appendText(t, T, resultLine(t, "toolu_s1", "String to replace not found in file.\nString: SPLIT", true))
	runHook(t, "stop", stopJSON(t, app, T))
	ls := lines(t, "acme")
	if len(ls) != 2 {
		t.Fatalf("after the result: %d lines, want 2", len(ls))
	}
	var got struct {
		ID    string          `json:"id"`
		Tool  string          `json:"tool"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal([]byte(ls[1]), &got); err != nil {
		t.Fatal(err)
	}
	if want := eventlog.ID("claude", "s1", "toolu_s1"); got.ID != want {
		t.Errorf("id = %s, want %s", got.ID, want)
	}
	if got.Tool != "Edit" || !strings.Contains(string(got.Input), "split.txt") {
		t.Errorf("tool %q input %s, want Edit with the tool_use's input", got.Tool, got.Input)
	}

	// The memory's own command, split the same way: the result is rejected, and nothing is logged.
	appendText(t, T, useLine(t, "toolu_s2", "Bash", map[string]any{"command": "tq lobe status"}))
	runHook(t, "stop", stopJSON(t, app, T))
	appendText(t, T, resultLine(t, "toolu_s2", "Unknown failure.", true))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 2 {
		t.Fatalf("the memory's own command split across runs: %d lines, want 2", n)
	}
}

// TestLobeRejectedSplitAtFileStart: the transcript's first line is a tool_use, and the run stops
// before its result. The look-back reaches the file's first byte, and the call still gets its tool.
func TestLobeRejectedSplitAtFileStart(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	// The filler line takes the first run past the 4 KiB head, so the cursor's head stays the same.
	filler := `{"type":"user","pad":"` + strings.Repeat("y", 5000) + `"}` + "\n"
	appendText(t, T, useLine(t, "toolu_f1", "Edit", map[string]any{
		"file_path": `C:\x\first.txt`, "old_string": "FIRST", "new_string": "x",
	})+filler+rejectedEdit(t, "toolu_f0", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("first run: %d lines, want 1", n)
	}
	appendText(t, T, resultLine(t, "toolu_f1", "String to replace not found in file.\nString: FIRST", true))
	runHook(t, "stop", stopJSON(t, app, T))
	var got struct {
		Tool string `json:"tool"`
	}
	ls := lines(t, "acme")
	if len(ls) != 2 {
		t.Fatalf("second run: %d lines, want 2", len(ls))
	}
	if err := json.Unmarshal([]byte(ls[1]), &got); err != nil {
		t.Fatal(err)
	}
	if got.Tool != "Edit" {
		t.Errorf("tool = %q, want Edit: the first line of the file was cut from the look-back", got.Tool)
	}
}

// TestLobeRejectedCapTwoRuns: 250 rejected calls in one transcript give at most 200 lines on the first run,
// and all 250, once each, after the next runs. On a quiet machine that is 200 and then 50. On a loaded one
// the 1 s cutoff can end a run before its 200 are written; the cursor is then not moved, and the next run
// writes the rest without a duplicate. So the test allows up to six runs.
func TestLobeRejectedCapTwoRuns(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	var b strings.Builder
	for i := 0; i < 250; i++ {
		b.WriteString(rejectedEdit(t, fmt.Sprintf("toolu_c%03d", i), "ZZZ"))
	}
	appendText(t, T, b.String())
	for run := 1; run <= 6 && len(lines(t, "acme")) < 250; run++ {
		before := len(lines(t, "acme"))
		runHook(t, "stop", stopJSON(t, app, T))
		if added := len(lines(t, "acme")) - before; added > 200 {
			t.Fatalf("run %d added %d lines, more than the cap of 200", run, added)
		}
	}
	ls := decodeLogged(t, lines(t, "acme"))
	if len(ls) != 250 {
		t.Fatalf("after six runs: %d lines, want 250", len(ls))
	}
	// The cursor reached the end: one more run reads nothing and writes nothing.
	if got, want := cursorOffset(t), fileSize(t, T); got != want {
		t.Fatalf("cursor at %d after the last call, want the transcript's end %d", got, want)
	}
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 250 {
		t.Fatalf("a run after the end added lines: %d, want 250", n)
	}
	seen := map[string]bool{}
	for _, e := range ls {
		seen[e.ID] = true
	}
	if len(seen) != 250 {
		t.Errorf("%d distinct ids, want 250", len(seen))
	}
}

// TestLobeRejectedReplacedFile: a transcript replaced by a file of the same size, holding a new
// rejected call, is read from its start: the cursor's head does not match, so the call is logged.
func TestLobeRejectedReplacedFile(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	appendText(t, T, rejectedEdit(t, "toolu_r0", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("first file: %d lines, want 1", n)
	}
	replacement := rejectedEdit(t, "toolu_r1", "QQQ")
	if got, want := int64(len(replacement)), fileSize(t, T); got != want {
		t.Fatalf("setup: the replacement is %d bytes, the first file %d; they must match", got, want)
	}
	mustWrite(t, T, replacement)
	runHook(t, "stop", stopJSON(t, app, T))
	if n := idCount(t, "acme", eventlog.ID("claude", "s1", "toolu_r1")); n != 1 {
		t.Fatalf("the call in the replaced file is logged %d times, want 1", n)
	}
}

// TestLobeRejectedSmallFileKeepsCursor: a transcript under 4 KiB that grows keeps its cursor, so a run
// reads only the new lines. The day file is removed between the runs: a run that read the file from its
// start would log the first call again.
func TestLobeRejectedSmallFileKeepsCursor(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	appendText(t, T, rejectedEdit(t, "toolu_k0", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("first run: %d lines, want 1", n)
	}
	if fileSize(t, T) >= headBytes {
		t.Fatalf("setup: the transcript must stay under %d bytes", headBytes)
	}
	if err := os.Remove(todayFile(t)); err != nil {
		t.Fatal(err)
	}
	appendText(t, T, rejectedEdit(t, "toolu_k1", "QQQ"))
	runHook(t, "stop", stopJSON(t, app, T))
	ids := decodeLogged(t, lines(t, "acme"))
	if len(ids) != 1 || ids[0].ID != eventlog.ID("claude", "s1", "toolu_k1") {
		t.Fatalf("second run logged %+v, want only toolu_k1", ids)
	}
}

// TestLobeReadChunkSameFile: readChunk reads only the file the path check saw. Given the Lstat of
// another file, it reads nothing and returns an error; given the file's own Lstat, it reads.
func TestLobeReadChunkSameFile(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.jsonl")
	b := filepath.Join(tmp, "b.jsonl")
	mustWrite(t, a, "x\n")
	mustWrite(t, b, "y\n")
	fb, err := os.Lstat(b)
	if err != nil {
		t.Fatal(err)
	}
	if read, err := readChunk(a, fb, cursorState{}, false); err == nil || read.ok || len(read.chunk) != 0 {
		t.Fatalf("a file other than the checked one was read: %+v, err %v", read, err)
	}
	fa, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}
	if read, err := readChunk(a, fa, cursorState{}, false); err != nil || !read.ok || string(read.chunk) != "x\n" {
		t.Fatalf("the checked file: read %+v, err %v; want its line", read, err)
	}
}

// fileSize is the size of the file p.
func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// mustGlob is the files that match pattern.
func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	m, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
