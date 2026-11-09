package eventlog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

const (
	testAt      = "2026-11-09T14:03:11Z"
	testDayFile = "events/events-20261109.jsonl"
)

var idPattern = regexp.MustCompile(`^evt_[0-9a-f]{8}$`)

// setupHome isolates TQ_HOME for one test and creates the client folder acme by hand.
func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "identities", "acme"), 0700); err != nil {
		t.Fatal(err)
	}
	return home
}

func openAcme(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open("acme", store.ReadWrite)
	if err != nil {
		t.Fatalf("Open acme: %v", err)
	}
	return st
}

// storeDir is the lobe folder of client acme under home.
func storeDir(home string) string {
	return filepath.Join(home, "identities", "acme", "lobe")
}

// readLines returns the lines of a file, each one ended by a newline.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatalf("%s does not end in a newline: %q", path, text)
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func TestLobeEventIDVector(t *testing.T) {
	want := map[string]string{
		"toolu_01": "evt_9bde2e09",
		"toolu_02": "evt_8618533e",
		"toolu_03": "evt_7fe44cf7",
		"toolu_04": "evt_20ed2862",
	}
	for call, id := range want {
		if got := ID("claude", "s1", call); got != id {
			t.Errorf("ID(claude, s1, %s) = %s, want %s", call, got, id)
		}
	}

	a := IDWithoutCall("claude", "s1", "Bash", "fail", `{"command":"x"}`, 1)
	b := IDWithoutCall("claude", "s1", "Bash", "fail", `{"command":"x"}`, 2)
	if a == b {
		t.Errorf("IDWithoutCall gave the same id %s for two clocks", a)
	}
	for _, id := range []string{a, b} {
		if !idPattern.MatchString(id) {
			t.Errorf("IDWithoutCall = %q, want evt_ plus 8 hex characters", id)
		}
	}
	if again := IDWithoutCall("claude", "s1", "Bash", "fail", `{"command":"x"}`, 1); again != a {
		t.Errorf("IDWithoutCall with the same arguments gave %s, want %s", again, a)
	}
}

func TestLobeEventLine(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	in := scrub.Input(json.RawMessage(`{"command":"npm run buld && echo <done>"}`))
	exit := 1
	e := Event{
		V:       1,
		ID:      "evt_9bde2e09",
		At:      testAt,
		Session: "s1",
		Host:    "claude",
		Tool:    "Bash",
		Kind:    KindFail,
		Repo:    "r_a172cedc",
		Cwd:     ".",
		Input:   in,
		Error:   scrub.Text("npm error Missing script: buld"),
		Exit:    &exit,
	}
	if err := Append(context.Background(), st, e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	lines := readLines(t, filepath.Join(storeDir(home), filepath.FromSlash(testDayFile)))
	if len(lines) != 1 {
		t.Fatalf("file holds %d lines, want 1", len(lines))
	}
	line := lines[0]
	const prefix = `{"v":1,"id":"evt_9bde2e09","at":"2026-11-09T14:03:11Z","session":"s1","host":"claude","tool":"Bash","kind":"fail","repo":"r_a172cedc","cwd":".","input":`
	if !strings.HasPrefix(line, prefix) {
		t.Errorf("line does not start with the expected prefix:\n%s", line)
	}
	for _, s := range []string{"&&", "<", ">"} {
		if !strings.Contains(line, s) {
			t.Errorf("line does not hold %q as itself: %s", s, line)
		}
	}
	var got struct {
		Input string `json:"input"`
		Error string `json:"error"`
		Exit  *int   `json:"exit"`
	}
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("line does not parse: %v", err)
	}
	if want := `{"command":"npm run buld && echo <done>"}`; got.Input != want {
		t.Errorf("input = %q, want %q", got.Input, want)
	}
	if got.Error != "npm error Missing script: buld" {
		t.Errorf("error = %q", got.Error)
	}
	if got.Exit == nil || *got.Exit != 1 {
		t.Errorf("exit = %v, want 1", got.Exit)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"fp", "dup"} {
		if _, ok := keys[k]; ok {
			t.Errorf("line has key %q, want none", k)
		}
	}

	// A second event: an empty session, no repo, an empty error and no exit.
	e2 := Event{V: 1, ID: "evt_00000002", At: testAt, Session: "", Host: "claude", Tool: "Bash", Kind: KindOK, Cwd: "."}
	if err := Append(context.Background(), st, e2); err != nil {
		t.Fatal(err)
	}
	// A third event with an exit of 0, which must be written.
	zero := 0
	e3 := Event{V: 1, ID: "evt_00000003", At: testAt, Session: "s3", Host: "claude", Tool: "Bash", Kind: KindFail, Cwd: ".", Exit: &zero}
	if err := Append(context.Background(), st, e3); err != nil {
		t.Fatal(err)
	}
	lines = readLines(t, filepath.Join(storeDir(home), filepath.FromSlash(testDayFile)))
	if len(lines) != 3 {
		t.Fatalf("file holds %d lines, want 3", len(lines))
	}
	var keys2 map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[1]), &keys2); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines[1], `"session":""`) {
		t.Errorf("second line lacks an empty session: %s", lines[1])
	}
	for _, k := range []string{"repo", "error", "exit"} {
		if _, ok := keys2[k]; ok {
			t.Errorf("second line has key %q, want none: %s", k, lines[1])
		}
	}
	if !strings.Contains(lines[2], `"exit":0`) {
		t.Errorf("third line lacks \"exit\":0: %s", lines[2])
	}
}

func TestLobeLogDayFile(t *testing.T) {
	home := setupHome(t)
	openAcme(t)
	for at, want := range map[string]string{
		"2026-11-09T23:59:59Z":      "events/events-20261109.jsonl",
		"2026-11-10T01:30:00+03:00": "events/events-20261109.jsonl",
	} {
		got, err := FileFor(at)
		if err != nil || got != want {
			t.Errorf("FileFor(%q) = %q, %v; want %q", at, got, err, want)
		}
	}
	if _, err := FileFor("yesterday"); err == nil {
		t.Error("FileFor(yesterday) gave no error")
	}

	st, _ := store.Open("acme", store.ReadWrite)
	err := Append(context.Background(), st, Event{V: 1, ID: "evt_00000001", At: "yesterday", Session: "s1", Host: "claude", Tool: "Bash", Kind: KindFail, Cwd: "."})
	if err == nil {
		t.Error("Append with At yesterday gave no error")
	}
	if _, err := os.Stat(filepath.Join(storeDir(home), "events")); !os.IsNotExist(err) {
		t.Errorf("events folder exists after a refused event: %v", err)
	}
}

func TestLobeLogSecretInMetadataIsRefused(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	e := Event{V: 1, ID: "evt_00000001", At: testAt, Session: "s1", Host: "claude",
		Tool: "gh" + "p_" + strings.Repeat("A", 36), Kind: KindFail, Cwd: "."}
	err := Append(context.Background(), st, e)
	if !errors.Is(err, scrub.ErrUnsafe) {
		t.Errorf("Append with a token in Tool = %v, want scrub.ErrUnsafe", err)
	}
	if _, err := os.Stat(filepath.Join(storeDir(home), "events")); !os.IsNotExist(err) {
		t.Errorf("events folder exists after a refused event: %v", err)
	}
}

// tally is what a parent counts over the lines a set of writers left behind.
type tally struct {
	lines, torn, lost, dup int
}

// countLines counts the lines of a file: writer and call number come from key, which reports false
// for a line that does not parse. Each (writer, n) pair must appear exactly once.
func countLines(lines []string, key func(line string) (int, int, bool)) tally {
	type pair struct{ w, n int }
	seen := map[pair]int{}
	var tl tally
	tl.lines = len(lines)
	for _, l := range lines {
		w, n, ok := key(l)
		if !ok {
			tl.torn++
			continue
		}
		seen[pair{w, n}]++
	}
	for w := 0; w < writers; w++ {
		for n := 0; n < perWriter; n++ {
			switch c := seen[pair{w, n}]; {
			case c == 0:
				tl.lost++
			case c > 1:
				tl.dup += c - 1
			}
		}
	}
	return tl
}

func nolockKey(line string) (int, int, bool) {
	var p struct {
		W *int `json:"w"`
		N *int `json:"n"`
	}
	if err := json.Unmarshal([]byte(line), &p); err != nil || p.W == nil || p.N == nil {
		return 0, 0, false
	}
	return *p.W, *p.N, true
}

func eventKey(line string) (int, int, bool) {
	var p struct {
		Session string `json:"session"`
		Input   string `json:"input"`
	}
	if err := json.Unmarshal([]byte(line), &p); err != nil || !strings.HasPrefix(p.Session, "p") {
		return 0, 0, false
	}
	w, err := strconv.Atoi(strings.TrimPrefix(p.Session, "p"))
	if err != nil {
		return 0, 0, false
	}
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(p.Input), &in); err != nil {
		return 0, 0, false
	}
	var n int
	if c, err := fmt.Sscanf(in.Command, "false %d", &n); err != nil || c != 1 {
		return 0, 0, false
	}
	return w, n, true
}

func TestLobeLogAppendParallel(t *testing.T) {
	for _, mode := range []string{"nolock", "event"} {
		t.Run(mode, func(t *testing.T) {
			home := setupHome(t)
			openAcme(t)
			side := t.TempDir()

			cmds := make([]*exec.Cmd, writers)
			outs := make([]*bytes.Buffer, writers)
			for w := 0; w < writers; w++ {
				cmds[w] = helperCmd(mode, w, side)
				outs[w] = &bytes.Buffer{}
				cmds[w].Stdout = outs[w]
				cmds[w].Stderr = outs[w]
				if err := cmds[w].Start(); err != nil {
					t.Fatalf("start writer %d: %v", w, err)
				}
			}
			for w := 0; w < writers; w++ {
				if err := cmds[w].Wait(); err != nil {
					t.Fatalf("writer %d: %v\n%s", w, err, outs[w].String())
				}
			}

			var path string
			key := nolockKey
			if mode == "nolock" {
				path = filepath.Join(storeDir(home), "events", "nolock.jsonl")
			} else {
				path = filepath.Join(storeDir(home), filepath.FromSlash(testDayFile))
				key = eventKey
			}
			tl := countLines(readLines(t, path), key)
			t.Logf("phase0-q3 mode=%s os=%s lines=%d torn=%d lost=%d dup=%d",
				mode, runtime.GOOS, tl.lines, tl.torn, tl.lost, tl.dup)
			if tl.lines != writers*perWriter || tl.torn != 0 || tl.lost != 0 || tl.dup != 0 {
				t.Errorf("mode %s: lines=%d torn=%d lost=%d dup=%d, want %d 0 0 0",
					mode, tl.lines, tl.torn, tl.lost, tl.dup, writers*perWriter)
			}

			if mode == "event" {
				logAppendTimes(t, side)
			}
		})
	}
}

// logAppendTimes reads the per-call durations the writers left in side and logs their percentiles.
func logAppendTimes(t *testing.T, side string) {
	t.Helper()
	var us []int64
	for w := 0; w < writers; w++ {
		raw, err := os.ReadFile(filepath.Join(side, fmt.Sprintf("dur-%d.txt", w)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range strings.Fields(string(raw)) {
			v, err := strconv.ParseInt(f, 10, 64)
			if err != nil {
				t.Fatalf("bad duration %q: %v", f, err)
			}
			us = append(us, v)
		}
	}
	if len(us) != writers*perWriter {
		t.Fatalf("got %d durations, want %d", len(us), writers*perWriter)
	}
	sort.Slice(us, func(i, j int) bool { return us[i] < us[j] })
	pct := func(p float64) float64 {
		idx := int(p*float64(len(us))+0.999999) - 1
		return float64(us[idx]) / 1000
	}
	t.Logf("phase0-q3 append-ms p50=%.3f p95=%.3f max=%.3f", pct(0.50), pct(0.95), float64(us[len(us)-1])/1000)
}

func TestLobeLogLockHeld(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)

	cmd := helperCmd("hold", 0, "")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	sc := bufio.NewScanner(stdout)
	if !sc.Scan() || sc.Text() != "HELD" {
		cmd.Wait()
		t.Fatalf("holder did not report the lock; stderr: %s", stderr.String())
	}

	e := Event{V: 1, ID: "evt_00000001", At: testAt, Session: "s1", Host: "claude", Tool: "Bash", Kind: KindFail, Cwd: "."}
	start := time.Now()
	err = Append(context.Background(), st, e)
	elapsed := time.Since(start)
	t.Logf("Append while the child holds the lock: err=%v after %v", err, elapsed)
	if err != nil {
		t.Errorf("Append while held = %v, want nil", err)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("Append took %v while the lock was held, limit 300ms", elapsed)
	} else if elapsed > 100*time.Millisecond {
		t.Logf("note: Append took %v, over the 100ms target on this machine", elapsed)
	}

	lines := readLines(t, filepath.Join(storeDir(home), filepath.FromSlash(testDayFile)))
	if len(lines) != 1 {
		t.Errorf("file holds %d lines while the child still holds the lock, want 1", len(lines))
	}
	if err := cmd.Wait(); err != nil {
		t.Errorf("holder: %v; stderr: %s", err, stderr.String())
	}
}

func TestLobeLogWriteError(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	events := filepath.Join(storeDir(home), "events")
	if err := os.WriteFile(events, nil, 0600); err != nil {
		t.Fatal(err)
	}

	// Capture stdout: Append must print nothing.
	out, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	saved := os.Stdout
	os.Stdout = out
	appendErr := Append(context.Background(), st, Event{V: 1, ID: "evt_00000001", At: testAt, Session: "s1", Host: "claude", Tool: "Bash", Kind: KindFail, Cwd: "."})
	os.Stdout = saved

	if appendErr == nil {
		t.Error("Append with events as a file gave no error")
	}
	if fi, _ := out.Stat(); fi.Size() != 0 {
		t.Errorf("Append printed %d bytes to stdout", fi.Size())
	}
	fi, err := os.Stat(events)
	if err != nil {
		t.Fatalf("events file gone: %v", err)
	}
	if fi.IsDir() || fi.Size() != 0 {
		t.Errorf("events file changed: dir=%v size=%d", fi.IsDir(), fi.Size())
	}
}

func TestLobeEventlogNoInit(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "func init(") {
			t.Errorf("%s declares func init", name)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, v := range vs.Values {
					ast.Inspect(v, func(n ast.Node) bool {
						if _, isCall := n.(*ast.CallExpr); isCall {
							t.Errorf("%s: package variable %v is initialized by a call", name, vs.Names)
						}
						return true
					})
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test source file found")
	}
}
