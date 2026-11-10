package eventlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// Readers take no lock. A line is whole when a line break ends it. Of the whole lines, a blank line
// (only white space) is skipped and not counted; a line that is not a JSON object is skipped and
// counted; a last line with no line break is skipped and counted, unless it is blank. A carriage
// return before the line break is not part of the line.

const (
	// tailWindow is the first read of Tail: the last 1 MiB of the file.
	tailWindow = 1 << 20
	// tailMaxWindow is the most Tail reads. It reads the last 1 MiB, and when that holds fewer than
	// max whole valid lines and the file is larger, it reads the last 2 MiB, then 4, 8 and 16 MiB. A
	// file whose last 16 MiB hold fewer lines gives back those lines.
	tailMaxWindow = 16 << 20
)

// Scan calls fn with each whole valid line of the client's day files, oldest day first and in file
// order within a day. The day files are the events/events-YYYYMMDD.jsonl files of the store; other
// files are ignored. The line passed to fn has no line break and no trailing carriage return; it is
// only valid during the call. When fn returns false, Scan stops and reads no further file.
//
// Scan returns the number of day files it read, the number of broken lines it skipped (see the rules
// above) and the first error that is not "does not exist". A missing events folder is no error and
// gives zero files.
//
// Scan reads each day file whole with Store.Read, with no size limit of its own. That is bounded in
// practice: the hook appends one line per event, and a day file is removed when retention expires it.
// A reader that needs only the newest lines of a long file uses Tail, which reads a window.
func Scan(s *store.Store, fn func(line []byte) bool) (files, skipped int, err error) {
	dir := filepath.Join(s.Dir(), "events")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var names []string
	for _, e := range entries {
		// A link or any other non-regular entry is not a day file.
		if e.Type().IsRegular() && isDayFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	// YYYYMMDD sorts by date, so a name sort puts the oldest day first.
	sort.Strings(names)
	for _, name := range names {
		data, err := s.Read("events/" + name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return files, skipped, err
		}
		files++
		whole, last := splitWhole(data)
		for _, line := range whole {
			if !isObjectLine(line) {
				skipped++
				continue
			}
			if !fn(line) {
				return files, skipped, nil
			}
		}
		if !isBlank(last) {
			skipped++
		}
	}
	return files, skipped, nil
}

// Tail returns the last max whole valid lines of the file at path, oldest first, and the number of
// broken lines it skipped while it looked at those lines (the span from the oldest returned line to
// the end of the file). A missing file is no error and gives no lines. max below 1 gives none.
//
// Tail reads at most the last 16 MiB of the file: the last 1 MiB first, and a larger window only when
// the lines found so far are fewer than max and the file is larger than the window read. A line
// longer than the window is not returned.
func Tail(path string, max int) (lines [][]byte, skipped int, err error) {
	if max <= 0 {
		return nil, 0, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := fi.Size()
	window := int64(tailWindow)
	for {
		got, broken, whole, err := tailWindowOnce(f, size, window, max)
		if err != nil {
			return nil, 0, err
		}
		if len(got) == max || whole || window >= tailMaxWindow {
			return got, broken, nil
		}
		window *= 2
		if window > tailMaxWindow {
			window = tailMaxWindow
		}
	}
}

// tailWindowOnce reads the last window bytes of f (size is its size) and walks the lines back from the
// end until it has max valid lines. whole reports that the read started at the file's first byte, so
// no line before it exists.
func tailWindowOnce(f *os.File, size, window int64, max int) (lines [][]byte, skipped int, whole bool, err error) {
	off := size - window
	if off < 0 {
		off = 0
	}
	partial := off > 0
	if partial {
		// One byte before the window tells whether the window starts a line.
		off--
	}
	buf := make([]byte, size-off)
	n, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, false, err
	}
	buf = buf[:n]
	if partial {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			buf = nil
		} else {
			buf = buf[i+1:]
		}
	}

	end := len(buf)
	if end > 0 && buf[end-1] != '\n' {
		// The last line has no line break: it is not whole.
		start := bytes.LastIndexByte(buf, '\n') + 1
		if !isBlank(buf[start:]) {
			skipped++
		}
		end = start
	}
	for end > 0 && len(lines) < max {
		ls := bytes.LastIndexByte(buf[:end-1], '\n') + 1
		line := trimCR(buf[ls : end-1])
		end = ls
		switch {
		case isBlank(line):
		case isObjectLine(line):
			lines = append(lines, line)
		default:
			skipped++
		}
	}
	// The walk ran from the newest line back, so reverse to oldest first.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines, skipped, !partial, nil
}

// splitWhole splits data, the bytes of a whole day file, into its whole lines (each without its line
// break and carriage return), in order, and returns the last line with no line break as last. Blank
// lines are dropped. Lines that are not JSON objects are returned as they are: the caller decides and
// counts them, and the caller counts last unless it is blank.
func splitWhole(data []byte) (whole [][]byte, last []byte) {
	parts := bytes.Split(data, []byte{'\n'})
	last = parts[len(parts)-1]
	for _, p := range parts[:len(parts)-1] {
		line := trimCR(p)
		if !isBlank(line) {
			whole = append(whole, line)
		}
	}
	return whole, last
}

// isDayFile reports whether name is events-YYYYMMDD.jsonl with eight digits.
func isDayFile(name string) bool {
	const pre, suf = "events-", ".jsonl"
	if len(name) != len(pre)+8+len(suf) || name[:len(pre)] != pre || name[len(name)-len(suf):] != suf {
		return false
	}
	for _, c := range name[len(pre) : len(pre)+8] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isObjectLine reports whether line holds one JSON object, with white space around it.
func isObjectLine(line []byte) bool {
	t := bytes.TrimSpace(line)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

func isBlank(b []byte) bool {
	return len(bytes.TrimSpace(b)) == 0
}

func trimCR(b []byte) []byte {
	return bytes.TrimSuffix(b, []byte{'\r'})
}
