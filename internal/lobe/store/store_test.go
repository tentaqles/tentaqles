package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// setupHome isolates TQ_HOME for one test and creates the client folder acme by hand.
func setupHome(t *testing.T) string {
	t.Helper()
	t.Setenv("TQ_HOME", t.TempDir())
	if err := os.MkdirAll(paths.IdentitiesRoot(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(paths.IdentitiesRoot(), "acme"), 0700); err != nil {
		t.Fatal(err)
	}
	return paths.IdentitiesRoot()
}

func openAcme(t *testing.T) *Store {
	t.Helper()
	st, err := Open("acme", ReadWrite)
	if err != nil {
		t.Fatalf("Open acme: %v", err)
	}
	return st
}

func listNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func sameNames(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

func TestLobeStoreOpenCreates(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	if st.Client() != "acme" {
		t.Errorf("Client() = %q", st.Client())
	}
	if st.Dir() != paths.LobeDir("acme") {
		t.Errorf("Dir() = %q", st.Dir())
	}
	raw, err := os.ReadFile(filepath.Join(st.Dir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		V      int    `json:"v"`
		Client string `json:"client"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("store.json does not parse: %v", err)
	}
	if m.V != 1 || m.Client != "acme" {
		t.Errorf("store.json = %s", raw)
	}
	if names := listNames(t, st.Dir()); !sameNames(names, "store.json") {
		t.Errorf("store folder holds %v, want only store.json", names)
	}
	if _, err := Open("acme", ReadOnly); err != nil {
		t.Errorf("Open ReadOnly: %v", err)
	}
	if _, err := Open("ghost", ReadOnly); err != ErrNoStore {
		t.Errorf("Open ghost ReadOnly = %v, want ErrNoStore", err)
	}
	if _, err := Open("ghost", ReadWrite); err == nil {
		t.Error("Open ghost ReadWrite succeeded without a client folder")
	}
	if _, err := os.Stat(filepath.Join(paths.IdentitiesRoot(), "ghost")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ghost client folder was created: %v", err)
	}
}

func TestLobeStoreRefusesLinkAndForeign(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		root := setupHome(t)
		target := t.TempDir()
		link := filepath.Join(root, "acme", "lobe")
		if runtime.GOOS == "windows" {
			out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
			if err != nil {
				t.Skipf("cannot create a junction (%v: %s); the link case is not run here", err, out)
			}
			t.Cleanup(func() { os.Remove(link) })
		} else if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot create a symlink (%v); the link case is not run here", err)
		}
		if _, err := Open("acme", ReadWrite); err != ErrLinked {
			t.Errorf("Open through a link = %v, want ErrLinked", err)
		}
		if _, err := Open("acme", ReadOnly); err != ErrLinked {
			t.Errorf("Open ReadOnly through a link = %v, want ErrLinked", err)
		}
	})

	t.Run("foreign", func(t *testing.T) {
		root := setupHome(t)
		if err := os.Mkdir(filepath.Join(root, "other"), 0700); err != nil {
			t.Fatal(err)
		}
		other, err := Open("other", ReadWrite)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(other.Dir(), "store.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(paths.LobeDir("acme"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.LobeDir("acme"), "store.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open("acme", ReadWrite); err != ErrForeign {
			t.Errorf("Open with another client's store.json = %v, want ErrForeign", err)
		}
	})

	t.Run("newer", func(t *testing.T) {
		setupHome(t)
		writeStoreJSON(t, `{"v":2,"client":"acme","created":"2026-01-01T00:00:00Z"}`)
		if _, err := Open("acme", ReadWrite); err != ErrNewer {
			t.Errorf("Open with v 2 = %v, want ErrNewer", err)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		setupHome(t)
		writeStoreJSON(t, `x`)
		if _, err := Open("acme", ReadWrite); err != ErrUnreadable {
			t.Errorf("Open with store.json x = %v, want ErrUnreadable", err)
		}
	})
}

// writeStoreJSON makes the store folder and writes its store.json by hand.
func writeStoreJSON(t *testing.T, text string) {
	t.Helper()
	if err := os.Mkdir(paths.LobeDir("acme"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.LobeDir("acme"), "store.json"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLobeStorePathCheck(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	bad := []string{"../x", "a/../../x", "/abs", "C:/x", `a\b`, "", "a:b", "events/x.jsonl:hidden", "events/x\x00.jsonl", "events/x\n.jsonl", "events/\tx.jsonl"}
	for _, rel := range bad {
		if err := st.Append(rel, cleanJSON(t, `{"a":1}`)); err != ErrPath {
			t.Errorf("Append(%q) = %v, want ErrPath", rel, err)
		}
		if _, err := st.Read(rel); err != ErrPath {
			t.Errorf("Read(%q) = %v, want ErrPath", rel, err)
		}
	}
	if names := listNames(t, filepath.Dir(st.Dir())); !sameNames(names, "lobe") {
		t.Errorf("client folder holds %v after refused paths, want only lobe", names)
	}
	if names := listNames(t, st.Dir()); !sameNames(names, "store.json") {
		t.Errorf("store folder holds %v after refused paths", names)
	}
}

func TestLobeStoreAppend(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	if err := st.Append("events/t.jsonl", cleanJSON(t, `{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.Append("events/t.jsonl", cleanJSON(t, `{"a":2}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(st.Dir(), "events", "t.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Errorf("file does not end in a newline: %q", raw)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 2 || lines[0] != `{"a":1}` || lines[1] != `{"a":2}` {
		t.Errorf("lines = %q, want two lines", lines)
	}
	if fi, err := os.Stat(filepath.Join(st.Dir(), "events")); err != nil || !fi.IsDir() {
		t.Errorf("events folder missing: %v", err)
	}

	if err := st.Append("events/t.jsonl", scrub.Text("one\ntwo")); err == nil {
		t.Error("a line with a line break was accepted")
	}
	raw2, _ := os.ReadFile(filepath.Join(st.Dir(), "events", "t.jsonl"))
	if !bytes.Equal(raw, raw2) {
		t.Errorf("a refused line changed the file: %q", raw2)
	}

	if err := os.RemoveAll(st.Dir()); err != nil {
		t.Fatal(err)
	}
	if err := st.Append("events/t.jsonl", cleanJSON(t, `{"a":3}`)); err == nil {
		t.Error("Append after the store was deleted succeeded")
	}
	if _, err := os.Stat(st.Dir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Append recreated the store folder: %v", err)
	}
}

// TestLobeStoreAppendTornTail: Append never reads the file, so it adds no line break after a torn tail.
// AppendRepaired, which the caller runs under the lock, adds one; on a whole tail it adds none.
func TestLobeStoreAppendTornTail(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	read := func(name string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(st.Dir(), "events", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	torn := func(name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(st.Dir(), "events"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(st.Dir(), "events", name), []byte(`{"v":`), 0600); err != nil {
			t.Fatal(err)
		}
	}

	torn("plain.jsonl")
	if err := st.Append("events/plain.jsonl", cleanJSON(t, `{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if got, want := read("plain.jsonl"), "{\"v\":{\"a\":1}\n"; got != want {
		t.Errorf("Append after a torn tail wrote %q, want %q (no leading line break)", got, want)
	}

	torn("repaired.jsonl")
	if err := st.AppendRepaired("events/repaired.jsonl", cleanJSON(t, `{"a":2}`)); err != nil {
		t.Fatal(err)
	}
	if got, want := read("repaired.jsonl"), "{\"v\":\n{\"a\":2}\n"; got != want {
		t.Errorf("AppendRepaired after a torn tail wrote %q, want %q", got, want)
	}
	if err := st.AppendRepaired("events/repaired.jsonl", cleanJSON(t, `{"a":3}`)); err != nil {
		t.Fatal(err)
	}
	if got, want := read("repaired.jsonl"), "{\"v\":\n{\"a\":2}\n{\"a\":3}\n"; got != want {
		t.Errorf("AppendRepaired on a whole tail wrote %q, want %q", got, want)
	}
}

func TestLobeStoreTwentyWriters(t *testing.T) {
	setupHome(t)
	openAcme(t)
	const writers, lines = 20, 500
	var wg sync.WaitGroup
	errs := make([]error, writers)
	outs := make([]string, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			cmd := helperCmd("append", "acme", fmt.Sprintf("LOBE_WRITER=%d", w))
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			errs[w] = cmd.Run()
			outs[w] = out.String()
		}(w)
	}
	wg.Wait()
	for w := range errs {
		if errs[w] != nil {
			t.Fatalf("writer %d: %v\n%s", w, errs[w], outs[w])
		}
	}

	st, _ := Open("acme", ReadOnly)
	raw, err := st.Read("events/t.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSuffix(string(raw), "\n")
	all := strings.Split(text, "\n")
	if len(all) != writers*lines {
		t.Fatalf("file has %d lines, want %d", len(all), writers*lines)
	}
	type pair struct{ w, n int }
	seen := make(map[pair]int)
	torn := 0
	for _, l := range all {
		var p struct {
			W *int `json:"w"`
			N *int `json:"n"`
		}
		if err := json.Unmarshal([]byte(l), &p); err != nil || p.W == nil || p.N == nil {
			torn++
			continue
		}
		seen[pair{*p.W, *p.N}]++
	}
	lost := 0
	dup := 0
	for w := 0; w < writers; w++ {
		for n := 0; n < lines; n++ {
			switch c := seen[pair{w, n}]; {
			case c == 0:
				lost++
			case c > 1:
				dup += c - 1
			}
		}
	}
	t.Logf("lines=%d torn=%d lost=%d duplicated=%d", len(all), torn, lost, dup)
	if torn != 0 || lost != 0 || dup != 0 {
		t.Errorf("torn=%d lost=%d duplicated=%d, want all 0", torn, lost, dup)
	}
}

func TestLobeLockDiesWithItsHolder(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	child, _ := startHolder(t, "acme")
	defer child.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	start := time.Now()
	_, err := st.Lock(ctx)
	cancel()
	t.Logf("Lock while the child holds it: %v after %v", err, time.Since(start))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Lock while held = %v, want ErrBusy", err)
	}

	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	killed := time.Now()
	_ = child.Wait()

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlock, err := st.Lock(ctx)
	took := time.Since(killed)
	if err != nil {
		t.Fatalf("Lock after the holder died = %v", err)
	}
	unlock()
	t.Logf("Lock succeeded %v after the kill", took)
	if took > 500*time.Millisecond {
		t.Fatalf("Lock took %v after the kill, limit 500ms", took)
	}
	if took > 100*time.Millisecond {
		t.Logf("note: over the 100ms target on this machine")
	}
}

func TestLobeBusyReplaceWritesNothing(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	known := `{"state":"known"}`
	if err := st.Replace(context.Background(), "state.json", cleanJSON(t, known)); err != nil {
		t.Fatal(err)
	}
	child, _ := startHolder(t, "acme")
	defer child.Wait()
	defer child.Process.Kill()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	start := time.Now()
	err := st.Replace(ctx, "state.json", cleanJSON(t, `{"state":"new"}`))
	elapsed := time.Since(start)
	cancel()
	t.Logf("Replace while busy: %v after %v", err, elapsed)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Replace while busy = %v, want ErrBusy", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("Replace took %v to give up, limit 100ms", elapsed)
	}
	raw, _ := os.ReadFile(filepath.Join(st.Dir(), "state.json"))
	if string(raw) != known {
		t.Errorf("file changed to %q", raw)
	}
	if names := listNames(t, st.Dir()); !sameNames(names, "store.json", "lock", "state.json") {
		t.Errorf("store folder holds %v, want no temporary file", names)
	}
}

// TestLobeReplaceUnderReader checks the store's reader, Store.Read, which is what the memory reads with,
// while Replace renames a new version over the file many times. Store.Read retries a missing file, so a
// rename in progress does not show as a missing file to the memory.
func TestLobeReplaceUnderReader(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	const rel = "state.json"
	full := filepath.Join(st.Dir(), rel)
	pad := strings.Repeat("lorem ipsum dolor sit amet ", 80)

	var seen atomic.Bool
	var stop atomic.Bool
	var reads, partial, missingLate, readErrs atomic.Int64
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for !stop.Load() {
			wasSeen := seen.Load()
			b, err := st.Read(rel)
			reads.Add(1)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					if wasSeen {
						missingLate.Add(1)
					}
				} else {
					readErrs.Add(1)
				}
				continue
			}
			var v map[string]any
			if json.Unmarshal(b, &v) != nil {
				partial.Add(1)
			}
		}
	}()
	const n = 1000
	failed := 0
	for i := 0; i < n; i++ {
		body := fmt.Sprintf(`{"counter":%d,"pad":%q}`, i, pad)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := st.Replace(ctx, rel, cleanJSON(t, body))
		cancel()
		if err != nil {
			failed++
			continue
		}
		seen.Store(true)
	}
	stop.Store(true)
	<-readerDone

	t.Logf("replaces=%d failed=%d (%.1f%%) reads=%d partial=%d missing-after-first=%d reader-errors=%d",
		n, failed, 100*float64(failed)/n, reads.Load(), partial.Load(), missingLate.Load(), readErrs.Load())
	if failed*100 > n*5 {
		t.Errorf("%d of %d replaces failed, more than 5 percent", failed, n)
	}
	if partial.Load() != 0 {
		t.Errorf("%d partial reads", partial.Load())
	}
	if missingLate.Load() != 0 {
		t.Errorf("%d reads found the file missing after the first replace", missingLate.Load())
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Errorf("final file is not a complete object: %v", err)
	}
	if names := listNames(t, st.Dir()); !sameNames(names, "store.json", "lock", rel) {
		t.Errorf("store folder holds %v, want no temporary file", names)
	}
}

func TestLobeStoreReadDuringReplace(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	const rel = "sessions/r.json"
	const n = 300
	pad := strings.Repeat("x", 1000)
	bodies := make([]scrub.Clean, n)
	for i := range bodies {
		bodies[i] = cleanJSON(t, fmt.Sprintf(`{"counter":%d,"pad":%q}`, i, pad))
	}

	var reads, failed, partial, writeErrs atomic.Int64
	var writerDone atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer writerDone.Store(true)
		for _, b := range bodies {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := st.Replace(ctx, rel, b)
			cancel()
			if err != nil {
				writeErrs.Add(1)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for !writerDone.Load() {
			raw, err := st.Read(rel)
			reads.Add(1)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					failed.Add(1)
				}
				continue
			}
			var v map[string]any
			if json.Unmarshal(raw, &v) != nil {
				partial.Add(1)
			}
		}
	}()
	wg.Wait()

	t.Logf("reads=%d failed=%d (%.2f%%) partial=%d", reads.Load(), failed.Load(),
		100*float64(failed.Load())/float64(max(reads.Load(), 1)), partial.Load())
	t.Logf("replaces=%d failed=%d (writer side; not a read failure)", n, writeErrs.Load())
	if partial.Load() != 0 {
		t.Errorf("%d reads returned bytes that do not parse as a complete JSON object", partial.Load())
	}
	if failed.Load()*100 > reads.Load() {
		t.Errorf("%d of %d reads failed, more than 1 percent", failed.Load(), reads.Load())
	}
}

// TestLobeReadMissingFileStillMissing checks that Store.Read retries a missing file but still reports it
// as missing, after the retry delay (about 15 milliseconds).
func TestLobeReadMissingFileStillMissing(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	start := time.Now()
	_, err := st.Read("never-written.json")
	elapsed := time.Since(start)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read of a missing file = %v, want fs.ErrNotExist", err)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("Read of a missing file took %v, want the retries (at least 10ms)", elapsed)
	}
	if elapsed > time.Second {
		t.Errorf("Read of a missing file took %v, limit 1s", elapsed)
	}
}

func TestLobeUpdateKeepsOtherKeys(t *testing.T) {
	setupHome(t)
	openAcme(t)
	const writers = 20
	var wg sync.WaitGroup
	errs := make([]error, writers)
	outs := make([]string, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			cmd := helperCmd("update", "acme", fmt.Sprintf("LOBE_WRITER=%d", w))
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			errs[w] = cmd.Run()
			outs[w] = out.String()
		}(w)
	}
	wg.Wait()
	for w := range errs {
		if errs[w] != nil {
			t.Fatalf("writer %d: %v\n%s", w, errs[w], outs[w])
		}
	}
	st, _ := Open("acme", ReadOnly)
	raw, err := st.Read("sessions/s1.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("file does not parse: %v", err)
	}
	for w := 0; w < writers; w++ {
		key := fmt.Sprintf("k%d", w)
		if _, ok := m[key]; !ok {
			t.Errorf("key %s missing; file is %s", key, raw)
		}
	}
	if len(m) != writers {
		t.Errorf("file holds %d keys, want %d", len(m), writers)
	}
}

func TestLobeSessionFileName(t *testing.T) {
	if got := SessionFile("abc-123_X"); got != "sessions/abc-123_X.json" {
		t.Errorf("SessionFile(abc-123_X) = %q", got)
	}
	bad := []string{
		"../escape",
		"a/b",
		`a\b`,
		"C:x",
		"a\x00b",
		strings.Repeat("a", 300),
	}
	re := regexp.MustCompile(`^sessions/s_[0-9a-f]{16}\.json$`)
	names := map[string]string{}
	for _, id := range bad {
		got := SessionFile(id)
		if !re.MatchString(got) {
			t.Errorf("SessionFile(%q) = %q, want sessions/s_<16 hex>.json", id, got)
			continue
		}
		if strings.Count(got, "/") != 1 {
			t.Errorf("SessionFile(%q) = %q is not directly inside sessions/", id, got)
		}
		if other, dup := names[got]; dup {
			t.Errorf("SessionFile(%q) and SessionFile(%q) both give %q", id, other, got)
		}
		names[got] = id
	}
}

func TestLobeCursorFileName(t *testing.T) {
	if got := CursorFile("abc-123_X"); got != "cursors/abc-123_X.json" {
		t.Errorf("CursorFile(abc-123_X) = %q", got)
	}
	// The name is the one SessionFile gives the same id, in the other folder.
	for _, id := range []string{"abc-123_X", "../escape", "a/b", "", strings.Repeat("a", 300)} {
		s, c := SessionFile(id), CursorFile(id)
		if strings.TrimPrefix(s, "sessions/") != strings.TrimPrefix(c, "cursors/") {
			t.Errorf("CursorFile(%q) = %q does not share the name of SessionFile %q", id, c, s)
		}
		if err := checkRel(c); err != nil {
			t.Errorf("CursorFile(%q) = %q is not a store path: %v", id, c, err)
		}
	}
}

func TestLobeRepoKey(t *testing.T) {
	c := t.TempDir()
	if got := RepoKey(c, c+"/app"); got != "r_a172cedc" {
		t.Errorf("RepoKey(c, c/app) = %q, want r_a172cedc", got)
	}
	if got := RepoKey(c, c); got != "r_cdb4ee2a" {
		t.Errorf("RepoKey(c, c) = %q, want r_cdb4ee2a", got)
	}
	k1 := RepoKey(c, filepath.Join(c, "etl", "load"))
	k2 := RepoKey(c, c+"/etl/load")
	if k1 != "r_223b4317" || k2 != "r_223b4317" {
		t.Errorf("etl/load keys = %q and %q, want r_223b4317 for both", k1, k2)
	}
	if runtime.GOOS == "windows" {
		if RepoKey(c, c+"/App") != RepoKey(c, c+"/app") {
			t.Error("App and app give different keys on Windows")
		}
	}

	// A linked worktree takes the key of its main repository (c/app); a submodule and a worktree whose
	// main repository lies outside the client folder keep their own keys (the keys of "sub" and "wt2").
	writeWorktreeShape(t, c)
	if got := RepoKey(c, filepath.Join(c, "wt", "fix")); got != "r_a172cedc" {
		t.Errorf("worktree RepoKey = %q, want the main repository's r_a172cedc", got)
	}
	writeGitTestFile(t, filepath.Join(c, "sub", ".git"), "gitdir: "+filepath.Join(c, "subgit")+"\n")
	writeGitTestFile(t, filepath.Join(c, "subgit", "placeholder"), "")
	if got := RepoKey(c, filepath.Join(c, "sub")); got != "r_ddc6e2b2" {
		t.Errorf("submodule RepoKey = %q, want the key of sub, r_ddc6e2b2", got)
	}
	outside := t.TempDir()
	writeGitTestFile(t, filepath.Join(outside, "app", ".git", "worktrees", "x", "commondir"), "../..\n")
	writeGitTestFile(t, filepath.Join(c, "wt2", ".git"), "gitdir: "+filepath.Join(outside, "app", ".git", "worktrees", "x")+"\n")
	if got := RepoKey(c, filepath.Join(c, "wt2")); got != "r_745b01a8" {
		t.Errorf("worktree outside the client RepoKey = %q, want the key of wt2, r_745b01a8", got)
	}

	// A worktree of a bare repository (its common folder is bare.git, not .git) keeps its own key: the
	// key of its relative path, not the key of bare.git's parent (the client folder, r_cdb4ee2a).
	bareClient := t.TempDir()
	writeGitTestFile(t, filepath.Join(bareClient, "bare.git", "worktrees", "fix", "commondir"), "../..\n")
	writeGitTestFile(t, filepath.Join(bareClient, "wt", "fix", ".git"), "gitdir: "+filepath.Join(bareClient, "bare.git", "worktrees", "fix")+"\n")
	plain := t.TempDir()
	got := RepoKey(bareClient, filepath.Join(bareClient, "wt", "fix"))
	if want := RepoKey(plain, filepath.Join(plain, "wt", "fix")); got != want || got == "r_cdb4ee2a" {
		t.Errorf("worktree of bare.git RepoKey = %q, want its own relative key %q (not r_cdb4ee2a)", got, want)
	}
}

// writeWorktreeShape builds, in client folder c, the main repository c/app (its .git folder) and the
// linked worktree c/wt/fix (a .git file naming c/app/.git/worktrees/fix, whose commondir is ../..).
func writeWorktreeShape(t *testing.T, c string) {
	t.Helper()
	writeGitTestFile(t, filepath.Join(c, "app", ".git", "worktrees", "fix", "commondir"), "../..\n")
	writeGitTestFile(t, filepath.Join(c, "wt", "fix", ".git"), "gitdir: "+filepath.Join(c, "app", ".git", "worktrees", "fix")+"\n")
}

// writeGitTestFile writes content to path, creating its parent folders.
func writeGitTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLobeRepoKeyStartsNoProcess(t *testing.T) {
	c := t.TempDir()
	writeWorktreeShape(t, c)
	t.Setenv("PATH", "")
	if got := RepoKey(c, filepath.Join(c, "wt", "fix")); got != "r_a172cedc" {
		t.Errorf("worktree RepoKey with an empty PATH = %q, want r_a172cedc", got)
	}
}

func TestLobeStoreNoInit(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(src, []byte("func init(")) {
			t.Errorf("%s contains func init(", name)
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
				for _, val := range vs.Values {
					ast.Inspect(val, func(n ast.Node) bool {
						if _, isCall := n.(*ast.CallExpr); isCall {
							t.Errorf("%s: package-level var %s has a call in its initializer", name, vs.Names[0].Name)
						}
						return true
					})
				}
			}
		}
	}
}
