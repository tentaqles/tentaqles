// Package selftest proves that the lobe store works on this machine with the installed program. It runs
// the checks of the store's own tests from the program itself: writer processes that append under the
// lock, and a replace under a reader. Everything happens in a scratch folder under the store's tmp/
// folder, which is removed at the end.
package selftest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

const (
	writers      = 20
	perWriter    = 500
	replaceCount = 1000
	padBytes     = 1024
	lockWait     = 5 * time.Second

	replaceAttempts = 20
	replaceGap      = 10 * time.Millisecond
)

// Result is what the self test measured.
type Result struct {
	Writers, Lines, Torn, Lost int
	Replaces, Partial          int
	RepoKeyApp                 string
}

// Run runs the three checks in a scratch folder under the store's tmp/ and removes it. exe is the
// program to start as a writer child, args are the arguments that make it run WriterChild for a given
// file and count (the caller supplies a function that builds them).
func Run(st *store.Store, clientDir string, exe string, childArgs func(rel string, writer, count int) []string) (res Result, err error) {
	res.Writers, res.Replaces = writers, replaceCount
	name, err := newName()
	if err != nil {
		return res, err
	}
	scratch, rel, err := makeScratch(st, name)
	if err != nil {
		return res, err
	}
	defer func() {
		if rmErr := os.RemoveAll(scratch); rmErr != nil && err == nil {
			err = fmt.Errorf("remove the scratch folder: %w", rmErr)
		}
	}()

	lines := rel + "/writers.jsonl"
	if err = runWriters(exe, clientDir, childArgs, lines); err != nil {
		return res, err
	}
	if err = checkWriters(st, lines, &res); err != nil {
		return res, err
	}
	if err = checkReplaces(st, rel+"/state.json", &res); err != nil {
		return res, err
	}
	res.RepoKeyApp = store.RepoKey(clientDir, filepath.Join(clientDir, "app"))
	return res, nil
}

// WriterChild is the body of one writer process: it takes the store lock (waiting up to 5 seconds)
// around each of count appends of a line {"w":<writer>,"n":<n>} to the store-relative file rel.
func WriterChild(st *store.Store, rel string, writer, count int) error {
	for n := 0; n < count; n++ {
		line, err := scrub.Line(struct {
			W int `json:"w"`
			N int `json:"n"`
		}{writer, n})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), lockWait)
		unlock, err := st.Lock(ctx)
		cancel()
		if err != nil {
			return err
		}
		err = st.Append(rel, line)
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// newName is a random folder name of 16 hex characters.
func newName() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// makeScratch makes tmp/<name> inside the store. It returns the real path and the store-relative one.
func makeScratch(st *store.Store, name string) (full, rel string, err error) {
	tmp := filepath.Join(st.Dir(), "tmp")
	if err := os.Mkdir(tmp, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", "", err
	}
	full = filepath.Join(tmp, name)
	if err := os.Mkdir(full, 0700); err != nil {
		return "", "", err
	}
	return full, "tmp/" + name, nil
}

// runWriters starts the writer children, one per writer number, waits for all of them, and returns the
// first failure with the child's output.
func runWriters(exe, dir string, childArgs func(rel string, writer, count int) []string, rel string) error {
	type child struct {
		cmd *exec.Cmd
		out *bytes.Buffer
	}
	var started []child
	var firstErr error
	for w := 0; w < writers; w++ {
		cmd := exec.Command(exe, childArgs(rel, w, perWriter)...)
		cmd.Dir = dir
		out := &bytes.Buffer{}
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			firstErr = fmt.Errorf("start writer %d: %w", w, err)
			break
		}
		started = append(started, child{cmd, out})
	}
	for w, c := range started {
		if err := c.cmd.Wait(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("writer %d: %w: %s", w, err, strings.TrimSpace(c.out.String()))
		}
	}
	return firstErr
}

// checkWriters reads the file the writers appended to and counts its lines, the lines that do not
// parse, and the (writer, n) pairs that are missing.
func checkWriters(st *store.Store, rel string, res *Result) error {
	raw, err := st.Read(rel)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	}
	res.Lines = len(lines)

	type pair struct{ w, n int }
	seen := make(map[pair]bool, writers*perWriter)
	for _, l := range lines {
		var p struct {
			W *int `json:"w"`
			N *int `json:"n"`
		}
		if err := json.Unmarshal([]byte(l), &p); err != nil || p.W == nil || p.N == nil {
			res.Torn++
			continue
		}
		seen[pair{*p.W, *p.N}] = true
	}
	for w := 0; w < writers; w++ {
		for n := 0; n < perWriter; n++ {
			if !seen[pair{w, n}] {
				res.Lost++
			}
		}
	}
	return nil
}

// checkReplaces replaces one file replaceCount times in the scratch folder while another goroutine
// reads it. A read whose bytes are not a complete JSON object is partial. A missing file is not.
func checkReplaces(st *store.Store, rel string, res *Result) error {
	pad := strings.Repeat("x", padBytes)
	var stop atomic.Bool
	var partial atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			raw, err := st.Read(rel)
			if err != nil {
				continue
			}
			var v map[string]any
			if json.Unmarshal(raw, &v) != nil || v == nil {
				partial.Add(1)
			}
		}
	}()

	var err error
	for i := 0; i < replaceCount && err == nil; i++ {
		body, merr := json.Marshal(struct {
			Counter int    `json:"counter"`
			Pad     string `json:"pad"`
		}{i, pad})
		if merr != nil {
			err = merr
			break
		}
		err = replaceOnce(st, rel, scrub.Input(json.RawMessage(body)))
	}
	stop.Store(true)
	<-done
	res.Partial = int(partial.Load())
	if err != nil {
		return fmt.Errorf("replace %s: %w", rel, err)
	}
	return nil
}

// replaceOnce replaces rel with data. On Windows a reader that holds the file can make a rename fail for
// a few tens of milliseconds, longer than the store's own rename retries, so a failed replace is tried
// again, up to replaceAttempts times in all.
func replaceOnce(st *store.Store, rel string, data scrub.Clean) error {
	var err error
	for attempt := 0; attempt < replaceAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(replaceGap)
		}
		ctx, cancel := context.WithTimeout(context.Background(), lockWait)
		err = st.Replace(ctx, rel, data)
		cancel()
		if err == nil {
			return nil
		}
	}
	return err
}
