package cli

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
)

// EventsLimit is how many stored lines `tentaqles lobe events` prints (the number is proposed in 005).
const EventsLimit = 20

// Events prints the newest stored event lines of the client, as they are on disk, one per line, oldest of
// the selection first. At most EventsLimit lines are printed, taken from the newest day file first and
// continuing into older day files. An empty log prints nothing.
func Events(d Deps, clientFlag string, out io.Writer) error {
	ws, err := Target(d, clientFlag)
	if err != nil {
		return err
	}
	if err := CheckLogin(d, ws); err != nil {
		return err
	}
	st, err := openStore(ws.Name)
	if err != nil {
		return err
	}
	lines, err := newestLines(filepath.Join(st.Dir(), "events"), EventsLimit)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	for _, l := range lines {
		if _, err := w.Write(l); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}

// newestLines returns the last max whole valid lines of the day files in dir, oldest first. It reads the
// newest day file first with eventlog.Tail, then older ones, until it has max lines or runs out of files.
// A missing folder gives no lines.
func newestLines(dir string, max int) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && isDayFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	// YYYYMMDD sorts by date: a descending name sort puts the newest day first.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	// chunks holds each file's lines, newest file first; each chunk is oldest first.
	var chunks [][][]byte
	need := max
	for _, name := range names {
		if need <= 0 {
			break
		}
		lines, _, err := eventlog.Tail(filepath.Join(dir, name), need)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, lines)
		need -= len(lines)
	}
	var out [][]byte
	for i := len(chunks) - 1; i >= 0; i-- {
		out = append(out, chunks[i]...)
	}
	return out, nil
}
