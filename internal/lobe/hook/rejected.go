package hook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/pathcmp"
	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/sense"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// evStop is the event that reads the session's transcript for calls Claude Code rejected.
const evStop = "stop"

// maxTranscriptRead is the most transcript bytes one run reads, and the window in which the first line
// start is searched for when a run starts at the tail of a long file. A line longer than the window is
// skipped a window at a time: its tail is a partial line, which does not parse.
const maxTranscriptRead = 8 << 20

// maxLookBack is the most bytes before a chunk that are read, to find the tool_use of a result in the
// chunk. Only tool_use blocks are taken from them.
const maxLookBack = 1 << 20

// headBytes is the length of the file's head that the cursor fingerprints.
const headBytes = 4096

// searchChunk is the read size of the search for a line start.
const searchChunk = 64 << 10

// cursorVersion is the version of the cursor file.
const cursorVersion = 1

// cursorLine is the cursor file's record: the byte offset after the last whole line read, and the
// fingerprint of the file's head (hex SHA-256 of its first min(4096, offset) bytes) when the cursor was
// written. A cursor whose head does not match the file's is no cursor: the file was replaced.
type cursorLine struct {
	V      int    `json:"v"`
	Offset int64  `json:"offset"`
	Head   string `json:"head"`
}

// cursorState is a cursor as read back: its offset and its head fingerprint.
type cursorState struct {
	offset int64
	head   string
}

// transcriptAllowed checks the path of a transcript and returns the Lstat of the file when the path
// may be read. sub is the folder under login that holds the transcripts: "projects" for a Claude
// transcript, "sessions" for a Codex session file. p must lie strictly inside login's sub folder (made
// absolute and clean; the comparison is case-insensitive on Windows, as loginMatches compares). Every
// component from the login folder down is checked with Lstat: the login folder and the sub folder and
// each folder below them must be a directory that is neither a symlink nor ModeIrregular (Go reports a
// Windows junction as irregular), and the last component must be a regular file. No link is followed
// here, and readChunk (or codexExit) checks that the file it opens is this one.
func transcriptAllowed(login, sub, p string) (os.FileInfo, bool) {
	if login == "" || sub == "" || p == "" {
		return nil, false
	}
	root := pathcmp.AbsPath(login)
	subDir := filepath.Join(root, sub)
	t := pathcmp.AbsPath(p)
	if !within(subDir, t) {
		return nil, false
	}
	rel, err := filepath.Rel(subDir, t)
	if err != nil {
		return nil, false
	}
	chain := []string{root, subDir}
	cur := subDir
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		chain = append(chain, cur)
	}
	var file os.FileInfo
	for i, c := range chain {
		fi, err := os.Lstat(c)
		if err != nil {
			return nil, false
		}
		if i < len(chain)-1 {
			if !fi.IsDir() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return nil, false
			}
			continue
		}
		if !fi.Mode().IsRegular() {
			return nil, false
		}
		file = fi
	}
	return file, file != nil
}

// within reports whether p lies strictly inside dir: dir followed by a separator is a prefix of p.
// A sibling whose name starts the same way (projectsX) does not match. Case is ignored on Windows.
func within(dir, p string) bool {
	prefix := dir + string(filepath.Separator)
	if len(p) <= len(prefix) {
		return false
	}
	head := p[:len(prefix)]
	if runtime.GOOS == "windows" {
		return strings.EqualFold(head, prefix)
	}
	return head == prefix
}

// parseCursor reads the bytes of a cursor file. It gives the offset and the head when the bytes are a
// JSON object with v 1 and a non-negative offset; any other shape gives false. A cursor with no head
// gives an empty head, which never matches a file's.
func parseCursor(b []byte) (cursorState, bool) {
	var c struct {
		V      *int   `json:"v"`
		Offset *int64 `json:"offset"`
		Head   string `json:"head"`
	}
	if json.Unmarshal(b, &c) != nil || c.V == nil || c.Offset == nil {
		return cursorState{}, false
	}
	if *c.V != cursorVersion || *c.Offset < 0 {
		return cursorState{}, false
	}
	return cursorState{offset: *c.Offset, head: c.Head}, true
}

// chunkRead is what one run reads from a transcript. start is the offset of chunk, prev the bytes
// before it (up to maxLookBack), chunk the whole lines read, ending in a line break, and next the
// offset the cursor takes. ok is false when there is nothing to do. ok with an empty chunk is a skip:
// the cursor moves past bytes that hold no whole line to read.
type chunkRead struct {
	start int64
	prev  []byte
	chunk []byte
	next  int64
	head  string // the file's head fingerprint, written with the cursor
	ok    bool
}

// fileHead is the hex SHA-256 of the first min(headBytes, upTo) bytes of f. The cursor fingerprints only
// bytes it has already passed, so a file that grows keeps its fingerprint and a replaced one loses it.
func fileHead(f *os.File, upTo int64) (string, error) {
	n := min(upTo, headBytes)
	buf := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, 0, n), buf); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// readChunk reads the whole lines of the transcript at path that a run has not read yet. The file it
// opens must be the one want describes (os.SameFile), or nothing is read. It starts at cur when the
// cursor's head matches the file's head and its offset is not past the end of the file; otherwise at
// the first line start in the last 8 MiB. At most 8 MiB is read. The look-back before the chunk is up
// to 1 MiB.
func readChunk(path string, want os.FileInfo, cur cursorState, hasCursor bool) (chunkRead, error) {
	f, err := os.Open(path)
	if err != nil {
		return chunkRead{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return chunkRead{}, err
	}
	if !os.SameFile(want, fi) {
		// The file opened is not the one the path check saw: the transcript changed between the two.
		return chunkRead{}, errors.New("the transcript changed between the check and the read")
	}
	size := fi.Size()
	valid := false
	if hasCursor && cur.offset <= size {
		h, err := fileHead(f, cur.offset)
		if err != nil {
			return chunkRead{}, err
		}
		valid = h == cur.head
	}
	var start int64
	if valid {
		start = cur.offset
	} else {
		start = max(0, size-maxTranscriptRead)
		if start > 0 {
			s, found, err := lineStart(f, start, size)
			if err != nil {
				return chunkRead{}, err
			}
			if !found {
				// No line break in the window: it lies inside one long line. The window is skipped; its
				// tail is a partial line that does not parse.
				next := min(size, start+maxTranscriptRead)
				return withHead(f, chunkRead{start: next, next: next, ok: true})
			}
			start = s
		}
	}
	n := min(size-start, maxTranscriptRead)
	if n <= 0 {
		return chunkRead{}, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, start, n), buf); err != nil {
		return chunkRead{}, err
	}
	last := bytes.LastIndexByte(buf, '\n')
	if last < 0 {
		// The window is one long line, and more of the file follows it: skip the window so the cursor
		// moves on. Otherwise the line is still being written, and the next run looks again.
		if n == maxTranscriptRead && size > start+n {
			return withHead(f, chunkRead{start: start, next: start + n, ok: true})
		}
		return chunkRead{}, nil
	}
	prev, err := readPrev(f, start)
	if err != nil {
		return chunkRead{}, err
	}
	if len(prev) > 0 && int64(len(prev)) == start {
		// The look-back starts at the file's first byte, a line start. RejectedWithContext drops the
		// first line of prev as a partial line, so a line break is put in front of it.
		prev = append([]byte{'\n'}, prev...)
	}
	return withHead(f, chunkRead{
		start: start,
		prev:  prev,
		chunk: buf[:last+1],
		next:  start + int64(last+1),
		ok:    true,
	})
}

// withHead sets the fingerprint the cursor is written with: that of the bytes before its new offset.
func withHead(f *os.File, c chunkRead) (chunkRead, error) {
	h, err := fileHead(f, c.next)
	if err != nil {
		return chunkRead{}, err
	}
	c.head = h
	return c, nil
}

// skipTail is the most a capture-off stop reads from the end of a transcript to find its last line end.
const skipTail = 1 << 20

// skipToEnd runs on a stop while capture is off. It records nothing and parses nothing: when the client's
// store already exists, it sets the session's cursor to the end of the transcript's last whole line, so
// calls rejected while capture was off are never read later. It reads at most the last 1 MiB of the file
// (to find that line end) and the head the cursor fingerprints. With no store, an unallowed path, or any
// error, it does nothing; it never creates a store.
func (r *run) skipToEnd() {
	login := os.Getenv("CLAUDE_CONFIG_DIR")
	want, ok := transcriptAllowed(login, "projects", r.pl.transcript)
	if !ok {
		return
	}
	if _, err := store.Open(r.ws.Name, store.ReadOnly); err != nil {
		return
	}
	f, err := os.Open(pathcmp.AbsPath(r.pl.transcript))
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !os.SameFile(want, fi) {
		return
	}
	size := fi.Size()
	n := min(size, skipTail)
	buf := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, size-n, n), buf); err != nil {
		return
	}
	last := bytes.LastIndexByte(buf, '\n')
	if last < 0 {
		return
	}
	end := size - n + int64(last+1)
	head, err := fileHead(f, end)
	if err != nil {
		return
	}
	st, err := store.Open(r.ws.Name, store.ReadWrite)
	if err != nil {
		return
	}
	line, err := scrub.Line(cursorLine{V: cursorVersion, Offset: end, Head: head})
	if err != nil {
		return
	}
	_ = st.Replace(r.ctx, store.CursorFile(r.pl.session), line)
}

// readPrev reads up to maxLookBack bytes of f before offset start.
func readPrev(f *os.File, start int64) ([]byte, error) {
	n := min(int64(maxLookBack), start)
	if n <= 0 {
		return nil, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, start-n, n), buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// lineStart is the offset of the first line that starts at or after from: from itself when the byte
// before it is a line break, otherwise the offset after the next line break. The search stops 8 MiB
// after from or at size, whichever comes first; found is false when no break is in that window.
func lineStart(f *os.File, from, size int64) (start int64, found bool, err error) {
	one := make([]byte, 1)
	if _, err := f.ReadAt(one, from-1); err == nil && one[0] == '\n' {
		return from, true, nil
	}
	limit := min(size, from+maxTranscriptRead)
	buf := make([]byte, searchChunk)
	for pos := from; pos < limit; {
		want := min(int64(len(buf)), limit-pos)
		n, err := f.ReadAt(buf[:want], pos)
		if i := bytes.IndexByte(buf[:n], '\n'); i >= 0 {
			return pos + int64(i) + 1, true, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, false, err
		}
		if n == 0 {
			break
		}
		pos += int64(n)
	}
	return 0, false, nil
}

// readRejected is the read half of the stop step. It checks the transcript path, opens the store
// read-only, reads the cursor, reads the bounded chunk and finds the rejected calls in it. Only when
// there are calls left after the skip rules does it read the day files, to see which are logged
// already. It keeps the calls and the new offset on the run. It returns the read-only store, nil when
// there is no store, and ok false when the run must stop with nothing written.
func (r *run) readRejected() (ro *store.Store, ok bool) {
	login := os.Getenv("CLAUDE_CONFIG_DIR")
	want, allowed := transcriptAllowed(login, "projects", r.pl.transcript)
	if !allowed {
		return nil, false
	}
	opened, err := store.Open(r.ws.Name, store.ReadOnly)
	var cur cursorState
	hasCursor := false
	switch {
	case errors.Is(err, store.ErrNoStore):
		opened = nil
	case err != nil:
		return nil, false
	default:
		if b, err := opened.Read(store.CursorFile(r.pl.session)); err == nil {
			cur, hasCursor = parseCursor(b)
		}
	}
	read, err := readChunk(pathcmp.AbsPath(r.pl.transcript), want, cur, hasCursor)
	if err != nil {
		return nil, false
	}
	if !read.ok {
		return opened, true
	}
	calls, resume := sense.RejectedWithContext(read.prev, read.chunk)
	r.newOffset, r.newHead, r.hasOffset = read.next, read.head, true
	if resume >= 0 {
		r.newOffset = read.start + int64(resume)
	}

	var todo []sense.Call
	for _, c := range calls {
		c.Session = r.pl.session
		c.Cwd = r.cwd
		if _, skip := sense.Skip(c); skip {
			continue
		}
		todo = append(todo, c)
	}
	if len(todo) == 0 {
		return opened, true
	}
	logged, ok := loggedIDs(r.ctx, opened, r.now)
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	for _, c := range todo {
		id := eventlog.ID("claude", c.Session, c.CallID)
		if seen[id] || logged[id] {
			continue
		}
		seen[id] = true
		r.rejected = append(r.rejected, c)
	}
	return opened, true
}

// loggedIDs is the set of event ids that today's and yesterday's day files (UTC) of the store hold,
// each read from the line's top-level "id" only. A missing file adds nothing. ok is false when a file
// cannot be read, or ctx is done before the scan ends. A nil store has nothing logged.
func loggedIDs(ctx context.Context, ro *store.Store, now time.Time) (map[string]bool, bool) {
	ids := map[string]bool{}
	if ro == nil {
		return ids, true
	}
	today := now.UTC()
	for _, at := range []time.Time{today, today.AddDate(0, 0, -1)} {
		rel, err := eventlog.FileFor(at.Format(time.RFC3339))
		if err != nil {
			return nil, false
		}
		b, err := ro.Read(rel)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false
		}
		for _, line := range bytes.Split(b, []byte("\n")) {
			if ctx.Err() != nil {
				return nil, false
			}
			var e struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(line, &e) == nil && e.ID != "" {
				ids[e.ID] = true
			}
		}
	}
	return ids, true
}

// recordRejected (S-01R) records the rejected calls of a stop, then writes the cursor. The cursor is
// written last, so a call that failed to record is looked at again by the next run, and the duplicate
// check keeps it from being written twice. Nothing is written when there is nothing to record. The
// cursor is written only when the store exists: the stop step returns before this for a run with no
// store and nothing to record.
func recordRejected(ctx context.Context, r *run) error {
	if len(r.rejected) > 0 {
		place := sense.Locate(r.ws.Root, r.cwd)
		for _, c := range r.rejected {
			// A rejected call is a failure, never a grep that found nothing, so the tail (read only for
			// that rule) is not needed.
			ev := sense.FromClaudeFailure(c, place, r.now, nil, false)
			if _, err := sense.Record(ctx, r.st, ev, c); err != nil {
				return err
			}
		}
	}
	if !r.hasOffset {
		return nil
	}
	line, err := scrub.Line(cursorLine{V: cursorVersion, Offset: r.newOffset, Head: r.newHead})
	if err != nil {
		return err
	}
	return r.st.Replace(ctx, store.CursorFile(r.pl.session), line)
}
