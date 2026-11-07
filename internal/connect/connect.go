// Package connect registers an MCP server in an identity's .claude.json for
// `tentaqles connect <provider>` (ADR-0017).
//
// It is the one place an agent-run command writes the identity's Claude config, so it is
// narrow: it adds one member to the top-level mcpServers object and changes nothing else. The
// file is edited in place, as bytes (every other byte, key order and number spelling stays as
// Claude Code wrote it), under a lock file, and replaced by a rename, so a concurrent run or
// Claude Code itself never reads a half-written file. A file that is not valid JSON, or whose
// mcpServers is not an object, is never touched.
package connect

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Add puts the server name with entry into the top-level mcpServers of the .claude.json at
// path, creating the file when it does not exist. It reports added false, and writes nothing,
// when mcpServers already holds a server of that name (whatever its entry).
//
// before, when not nil, runs once the edit is ready and before the file is replaced: an error
// from it stops the write, so a change is never made without its log line (Log).
func Add(path, name string, entry map[string]any, before func() error) (added bool, err error) {
	if name == "" {
		return false, errors.New("connect: no server name")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	unlock, err := lock(path + ".tq-connect.lock")
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	defer unlock()

	val, err := encode(entry)
	if err != nil {
		return false, err
	}
	// Claude Code writes this file too, and does not honour our lock. The edit is made from
	// what was read, so the file is read again just before the rename: if it changed, the edit
	// is made again from the new bytes. That shrinks the window to the rename itself; it cannot
	// close it, which is why the command warns when a session looks open.
	logged := false
	for try := 0; try < editTries; try++ {
		raw, mode, err := readConfig(path)
		if err != nil {
			return false, err
		}
		out, added, err := insert(raw, name, val)
		if err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
		if !added {
			return false, nil
		}
		if !json.Valid(out) {
			return false, fmt.Errorf("%s: the edited file is not valid JSON; nothing written", path)
		}
		if before != nil && !logged {
			if err := before(); err != nil {
				return false, err
			}
			logged = true
		}
		if betweenReadAndRename != nil {
			betweenReadAndRename()
		}
		cur, _, err := readConfig(path)
		if err != nil {
			return false, err
		}
		if bytes.Equal(cur, raw) {
			return true, writeAtomic(path, out, mode)
		}
	}
	return false, fmt.Errorf("%s kept changing while connect edited it (a running Claude session?); nothing written, run connect again once it is idle", path)
}

// editTries is how many times Add makes its edit again from a file that changed under it.
const editTries = 5

// betweenReadAndRename is a test hook: it runs after the edit is made and before the file is
// read again. Nil in tq.
var betweenReadAndRename func()

// readConfig reads the file (nil for one that does not exist) and the mode a replacement keeps.
func readConfig(path string) ([]byte, os.FileMode, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, 0o600, nil
	}
	if err != nil {
		return nil, 0, err
	}
	mode := os.FileMode(0o600)
	if fi, serr := os.Stat(path); serr == nil {
		mode = fi.Mode().Perm()
	}
	return raw, mode, nil
}

// encode is the compact JSON of v with <, > and & as written.
func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

func keyJSON(k string) []byte {
	b, _ := encode(k)
	return b
}

// insert returns raw with the member name: val added to its top-level mcpServers object.
// raw empty (or only space) is a file that does not exist yet.
func insert(raw []byte, name string, val []byte) (out []byte, added bool, err error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		var b bytes.Buffer
		b.WriteString("{\n  \"mcpServers\": {\n    ")
		b.Write(keyJSON(name))
		b.WriteString(": ")
		b.Write(val)
		b.WriteString("\n  }\n}\n")
		return b.Bytes(), true, nil
	}
	if !json.Valid(raw) {
		return nil, false, errors.New("not valid JSON; left as it is")
	}
	root := skipWS(raw, 0)
	if raw[root] != '{' {
		return nil, false, errors.New("the top level is not a JSON object; left as it is")
	}
	ms, rootClose, err := members(raw, root)
	if err != nil {
		return nil, false, fmt.Errorf("%w; left as it is", err)
	}
	idx := -1
	for i, m := range ms {
		if m.key == "mcpServers" {
			idx = i // the last one wins, as the JSON decoders do
		}
	}
	member := func(k string, v []byte) []byte { return append(append(keyJSON(k), ": "...), v...) }
	if idx < 0 {
		obj := append(append([]byte("{"), member(name, val)...), '}')
		return splice(raw, ms, root, rootClose, member("mcpServers", obj)), true, nil
	}
	vs := ms[idx].valueStart
	if raw[vs] != '{' {
		return nil, false, errors.New("mcpServers is not an object; left as it is")
	}
	sub, subClose, err := members(raw, vs)
	if err != nil {
		return nil, false, fmt.Errorf("%w; left as it is", err)
	}
	for _, m := range sub {
		if m.key == name {
			return raw, false, nil
		}
	}
	return splice(raw, sub, vs, subClose, member(name, val)), true, nil
}

// splice adds the member text to the object whose members are ms, opened at open and closed
// at closeAt, using the indentation of its last member (or the compact form when it is empty).
func splice(raw []byte, ms []member, open, closeAt int, text []byte) []byte {
	var out []byte
	if len(ms) == 0 {
		out = append(out, raw[:open+1]...)
		out = append(out, text...)
		return append(out, raw[closeAt:]...)
	}
	last := ms[len(ms)-1]
	// The whitespace that precedes the last key is the indentation to repeat.
	i := last.keyStart
	for i > 0 && (raw[i-1] == ' ' || raw[i-1] == '\t' || raw[i-1] == '\n' || raw[i-1] == '\r') {
		i--
	}
	sep := raw[i:last.keyStart]
	out = append(out, raw[:last.valueEnd]...)
	out = append(out, ',')
	out = append(out, sep...)
	out = append(out, text...)
	return append(out, raw[last.valueEnd:]...)
}

var tmpSeq atomic.Uint64

// writeAtomic writes b beside p and renames it over p. A rename that fails because the file
// is open elsewhere (Windows) is retried for a moment.
func writeAtomic(p string, b []byte, mode os.FileMode) error {
	tmp := fmt.Sprintf("%s.tq-%d-%d.tmp", p, os.Getpid(), tmpSeq.Add(1))
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(tmp, p); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = os.Remove(tmp)
	return err
}

// lockWait bounds how long Add waits for another writer; a lock older than lockStale is a
// crashed writer's and is taken over.
const (
	lockWait  = 15 * time.Second
	lockStale = 30 * time.Second
)

// lock takes an exclusive lock file and returns its release.
func lock(p string) (func(), error) {
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if fi, serr := os.Stat(p); serr == nil && time.Since(fi.ModTime()) > lockStale {
			_ = os.Remove(p)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another writer holds %s", p)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
