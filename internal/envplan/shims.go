package envplan

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// The browser shims: for a client with a browsers: list, three programs in
// $TQ_HOME/shims/<ws>/ that open an http or https URL in the client's browser profile:
//
//   - tq-open, the program BROWSER names;
//   - xdg-open and open, first on the identity PATH, which do the same for one http or https
//     URL and hand anything else to the real xdg-open or open.
//
// Each is a copy of the tq binary (the first) or a link to that copy, which answers as a shim
// when it runs from such a folder (ShimName). A program, not a batch file, on every OS: cmd.exe
// re-parses a batch file's arguments, and a URL with & in it would run the rest as a command.
// The folder holds only what tq writes.

// ShimOpen is the program BROWSER names, without a Windows suffix.
const ShimOpen = "tq-open"

// shimNames are the shim programs, without a Windows suffix; ShimOpen is the copy, the others
// are links to it.
var shimNames = []string{ShimOpen, "open", "xdg-open"}

func shimFile(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// ShimsDir is identity ws's folder of shims.
func ShimsDir(ws string) string { return paths.ShimsDir(ws) }

// HasBrowsers reports whether ws lists a browser: the only case the environment, the PATH and
// the shims change for (REQ-13).
func HasBrowsers(ws *resolve.Workspace) bool {
	return ws != nil && ws.Manifest != nil && len(ws.Manifest.Browsers) > 0
}

// BrowserVar is the value of BROWSER for ws: the path of ShimOpen, with forward slashes on
// Windows (tools split BROWSER like a shell line, which would eat backslashes) and in double
// quotes when the path has a space.
func BrowserVar(ws string) string {
	p := filepath.Join(ShimsDir(ws), shimFile(ShimOpen))
	if runtime.GOOS == "windows" {
		p = filepath.ToSlash(p)
	}
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

// ShimMode says what a program running from a shims folder is: ShimURL for tq-open (every
// argument is a URL) and ShimWrap for open and xdg-open.
type ShimMode int

const (
	ShimNone ShimMode = iota
	ShimURL
	ShimWrap
)

// ShimName reads the program at exe: the identity whose shims folder it is in, its name and
// what it does. ShimNone when exe is not a shim of $TQ_HOME-shaped layout
// (<anything>/shims/<ws>/<name>).
func ShimName(exe string) (ws, name string, mode ShimMode) {
	dir := filepath.Dir(exe)
	if !strings.EqualFold(filepath.Base(filepath.Dir(dir)), "shims") {
		return "", "", ShimNone
	}
	ws = filepath.Base(dir)
	name = strings.ToLower(filepath.Base(exe))
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, ".exe")
	}
	if !workspace.NameRe.MatchString(ws) {
		return "", "", ShimNone
	}
	switch name {
	case ShimOpen:
		return ws, name, ShimURL
	case "open", "xdg-open":
		return ws, name, ShimWrap
	}
	return "", "", ShimNone
}

// shimsEntry reports whether an OS-spelled PATH entry is identity ws's shims folder.
func shimsEntry(p, ws string) bool {
	return p != "" && samePath(filepath.Clean(strings.Trim(p, `"`)), filepath.Clean(ShimsDir(ws)))
}

// shimsEntryCurrent reports whether a PATH in syn's spelling already is what FilterPath would
// make of it as far as the shims folder goes: a client with browsers has it first, a client
// with none has no entry of it. Plan keeps a PATH equal to the one tq last wrote without
// reading a folder; a terminal that was in the client before its manifest gained (or lost) a
// browsers: list must leave that fast path once, or it never gets (or never drops) the folder.
func (l *Layer) shimsEntryCurrent(path string, syn PathSyntax) bool {
	entries := strings.Split(path, syn.Sep)
	isShims := func(e string) bool {
		return e != "" && shimsEntry(syn.ToOS(e), l.Identity)
	}
	if l.shims != "" {
		return len(entries) > 0 && isShims(entries[0])
	}
	for _, e := range entries {
		if isShims(e) {
			return false
		}
	}
	return true
}

// isAnyShims reports whether an OS path is some identity's shims folder.
func isAnyShims(p string) bool {
	p = filepath.Clean(strings.Trim(p, `"`))
	return samePath(filepath.Dir(p), filepath.Clean(paths.ShimsRoot())) && workspace.NameRe.MatchString(filepath.Base(p))
}

// ShimsReady reports whether the shims folder holds exactly what tq writes: the three programs
// at the size of the tq binary, and nothing else.
func ShimsReady(ws string) bool {
	if !workspace.NameRe.MatchString(ws) {
		return false
	}
	src, err := StubSource()
	if err != nil {
		return false
	}
	sfi, err := os.Stat(src)
	if err != nil {
		return false
	}
	es, err := os.ReadDir(ShimsDir(ws))
	if err != nil || len(es) != len(shimNames) {
		return false
	}
	for _, e := range es {
		if !e.Type().IsRegular() {
			return false
		}
		fi, err := e.Info()
		if err != nil || fi.Size() != sfi.Size() {
			return false
		}
	}
	for _, n := range shimNames {
		if _, err := os.Stat(filepath.Join(ShimsDir(ws), shimFile(n))); err != nil {
			return false
		}
	}
	return true
}

// WriteShims brings identity ws's shims folder in line with the tq binary: the copy written or
// replaced, the links made, anything else removed. It reports whether anything changed.
// tq settings render and tentaqles reach call it.
func WriteShims(ws string) (bool, error) {
	if !workspace.NameRe.MatchString(ws) {
		return false, fmt.Errorf("invalid workspace name %q", ws)
	}
	src, err := StubSource()
	if err != nil {
		return false, fmt.Errorf("the shim program: %w", err)
	}
	dir := ShimsDir(ws)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	keep := map[string]bool{}
	for _, n := range shimNames {
		keep[shimFile(n)] = true
	}
	changed := false
	es, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range es {
		if !keep[e.Name()] {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	main := filepath.Join(dir, shimFile(ShimOpen))
	if !sameBytes(src, main) {
		_ = os.Remove(main)
		if err := copyFile(src, main); err != nil {
			return changed, fmt.Errorf("the shim program: %w", err)
		}
		changed = true
	}
	mfi, err := os.Stat(main)
	if err != nil {
		return changed, err
	}
	for _, n := range shimNames {
		if n == ShimOpen {
			continue
		}
		p := filepath.Join(dir, shimFile(n))
		if cur, err := os.Stat(p); err == nil && (os.SameFile(cur, mfi) || sameBytes(main, p)) {
			continue
		}
		_ = os.Remove(p)
		if os.Link(main, p) != nil {
			if err := copyFile(main, p); err != nil {
				return changed, fmt.Errorf("shim %s: %w", n, err)
			}
		}
		changed = true
	}
	return changed, nil
}

// RemoveShims removes identity ws's shims folder (the client lists no browser any more). It
// reports whether there was one.
func RemoveShims(ws string) (bool, error) {
	if !workspace.NameRe.MatchString(ws) {
		return false, fmt.Errorf("invalid workspace name %q", ws)
	}
	dir := ShimsDir(ws)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return true, os.RemoveAll(dir)
}

// sameBytes reports whether two files hold the same bytes; false when either is unreadable.
func sameBytes(a, b string) bool {
	fa, err := os.Open(a)
	if err != nil {
		return false
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false
	}
	defer fb.Close()
	ia, err := fa.Stat()
	if err != nil {
		return false
	}
	ib, err := fb.Stat()
	if err != nil || ia.Size() != ib.Size() {
		return false
	}
	if os.SameFile(ia, ib) {
		return true
	}
	const n = 64 << 10
	ba, bb := make([]byte, n), make([]byte, n)
	for {
		na, ea := io.ReadFull(fa, ba)
		nb, eb := io.ReadFull(fb, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			return false
		}
		if ea != nil || eb != nil {
			return (ea == io.EOF || ea == io.ErrUnexpectedEOF) && (eb == io.EOF || eb == io.ErrUnexpectedEOF)
		}
	}
}
