package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
)

// EventsLimit is how many stored lines `tentaqles lobe events` selects (the number is proposed in 005).
const EventsLimit = 20

// searchLines asks eventlog.Tail for every whole line it can reach: a first line is looked for in the whole
// day file, within Tail's own 16 MiB bound.
const searchLines = math.MaxInt32

// readTail is the reader the events verb uses for day files. A test replaces it to see which files are read.
var readTail = eventlog.Tail

// Events prints the newest stored event lines of the client, as they are on disk, one per line, oldest of
// the selection first. At most EventsLimit lines are selected, from the newest day file first and continuing
// into older day files. The first-line rule applies to the printed lines: of two lines with the same session
// and id only the first is printed, so fewer than EventsLimit lines can print when copies are dropped. When
// the selection holds a reference line (one with "dup") whose first line is not in the selection, that first
// line is printed in front, once, when it is found in the reference's own day file or the day before (see
// withFirstLines). An expired first line prints nothing extra. An empty log prints nothing.
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
	dir := filepath.Join(st.Dir(), "events")
	sel, err := newestLines(dir, EventsLimit)
	if err != nil {
		return err
	}
	lines, err := withFirstLines(dir, sel)
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

// pointer is the S-06 pointer to an event: its session and its id. A bare id is never a pointer.
type pointer struct{ session, id string }

// keyOf is what the readers here need of a stored line. A line that is not a JSON object gives ok false.
type keyOf struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	Dup     string `json:"dup"`
}

func parseKey(line []byte) (k keyOf, ok bool) {
	if err := json.Unmarshal(line, &k); err != nil {
		return keyOf{}, false
	}
	return k, true
}

// stored is a selected line and the name of the day file that holds it.
type stored struct {
	day  string
	line []byte
}

// dayFiles returns the names of the day files in dir, newest first. A missing folder gives none.
func dayFiles(dir string) ([]string, error) {
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
	return names, nil
}

// newestLines returns the last max whole valid lines of the day files in dir, oldest first, each with the
// name of its day file. It reads the newest day file first with eventlog.Tail, then older ones, until it has
// max lines or runs out of files. A missing folder gives no lines.
func newestLines(dir string, max int) ([]stored, error) {
	names, err := dayFiles(dir)
	if err != nil {
		return nil, err
	}
	// chunks holds each file's lines, newest file first; each chunk is oldest first.
	var chunks [][]stored
	need := max
	for _, name := range names {
		if need <= 0 {
			break
		}
		lines, _, err := readTail(filepath.Join(dir, name), need)
		if err != nil {
			return nil, err
		}
		var chunk []stored
		for _, l := range lines {
			chunk = append(chunk, stored{day: name, line: l})
		}
		chunks = append(chunks, chunk)
		need -= len(lines)
	}
	var out []stored
	for i := len(chunks) - 1; i >= 0; i-- {
		out = append(out, chunks[i]...)
	}
	return out, nil
}

// firstDayFiles returns the names of the day files a reference's first line can be in, older first: the day
// before the reference's own day file and the reference's own day file. The reference's day is the name of
// the file that holds it. A first line is at most 300 seconds older than its reference (S-06 step 4), so it is
// in one of these two. ok is false when the name does not carry a date.
func firstDayFiles(day string) (names [2]string, ok bool) {
	const pre = "events-"
	if len(day) != len(pre)+8+len(".jsonl") {
		return names, false
	}
	d, err := time.Parse("20060102", day[len(pre):len(pre)+8])
	if err != nil {
		return names, false
	}
	return [2]string{"events-" + d.AddDate(0, 0, -1).Format("20060102") + ".jsonl", day}, true
}

// firstLine is a copy of a first line found in a day file: the file's name, its position in the file and
// the line.
type firstLine struct {
	day  string
	pos  int
	line []byte
}

// firstCopy returns the first line of pointer p in the day files names (older first), so the older copy
// wins as it does in eventlog.Scan. Each file is read at most once, through cache. ok is false when neither
// file holds p.
func firstCopy(dir string, p pointer, names [2]string, cache map[string][][]byte) (firstLine, bool, error) {
	for _, name := range names {
		lines, ok := cache[name]
		if !ok {
			var err error
			lines, _, err = readTail(filepath.Join(dir, name), searchLines)
			if err != nil {
				return firstLine{}, false, err
			}
			cache[name] = lines
		}
		for pos, l := range lines {
			k, ok := parseKey(l)
			if ok && (pointer{k.Session, k.ID}) == p {
				return firstLine{day: name, pos: pos, line: l}, true, nil
			}
		}
	}
	return firstLine{}, false, nil
}

// withFirstLines applies the first-line rule to sel (oldest first) and adds the first lines its reference
// lines point at. The first line of a reference is the line with the reference's session and its dup id. It
// is searched for in the day file that holds the reference and the day before, and only there. The older copy
// wins when both hold one, as in eventlog.Scan. A found line is printed in front of sel, oldest day first,
// once. A copy of its pair in sel is dropped, so the first-line rule holds over the printed lines; a copy that
// is already sel's own first line is not printed twice.
func withFirstLines(dir string, sel []stored) ([][]byte, error) {
	// The first-line rule within sel: of two lines with one session and id, the first stays.
	seen := make(map[pointer]bool)
	kept := make([]stored, 0, len(sel))
	for _, s := range sel {
		k, ok := parseKey(s.line)
		if ok && k.ID != "" {
			p := pointer{k.Session, k.ID}
			if seen[p] {
				continue
			}
			seen[p] = true
		}
		kept = append(kept, s)
	}
	// The first-line pointers of the reference lines in kept, each once, with the day files to search.
	var want []pointer
	days := make(map[pointer][2]string)
	for _, s := range kept {
		k, ok := parseKey(s.line)
		if !ok || k.Dup == "" {
			continue
		}
		p := pointer{k.Session, k.Dup}
		if _, done := days[p]; done {
			continue
		}
		names, ok := firstDayFiles(s.day)
		if !ok {
			continue
		}
		days[p] = names
		want = append(want, p)
	}
	if len(want) == 0 {
		return keptLines(kept), nil
	}

	// The first line already in sel for each pair: the first kept line of that pair.
	firstIn := make(map[pointer][]byte)
	for _, s := range kept {
		if k, ok := parseKey(s.line); ok && k.ID != "" {
			p := pointer{k.Session, k.ID}
			if _, has := firstIn[p]; !has {
				firstIn[p] = s.line
			}
		}
	}
	cache := make(map[string][][]byte)
	var extra []firstLine
	extraPairs := make(map[pointer]bool)
	for _, p := range want {
		f, ok, err := firstCopy(dir, p, days[p], cache)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if in, has := firstIn[p]; has && bytes.Equal(in, f.line) {
			continue
		}
		extra = append(extra, f)
		extraPairs[p] = true
	}
	// Oldest day first, then position in the file: the order the first lines stand in the log.
	sort.SliceStable(extra, func(i, j int) bool {
		if extra[i].day != extra[j].day {
			return extra[i].day < extra[j].day
		}
		return extra[i].pos < extra[j].pos
	})
	out := make([][]byte, 0, len(extra)+len(kept))
	for _, f := range extra {
		out = append(out, f.line)
	}
	for _, s := range kept {
		if k, ok := parseKey(s.line); ok && extraPairs[pointer{k.Session, k.ID}] {
			continue
		}
		out = append(out, s.line)
	}
	return out, nil
}

// keptLines drops the day names of kept, for the output.
func keptLines(kept []stored) [][]byte {
	out := make([][]byte, 0, len(kept))
	for _, s := range kept {
		out = append(out, s.line)
	}
	return out
}

// purgeLockWait is how long purge waits for the store lock before it gives up. A person's command may wait
// longer than a hook, which waits LockWait and then appends without the lock.
const purgeLockWait = 2 * time.Second

// isDropsFile reports whether name is a drops file of the event log: drops-YYYYMMDD.jsonl (feature 011).
func isDropsFile(name string) bool {
	const pre, suf = "drops-", ".jsonl"
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

// purgeNames returns the names of the files purge deletes in dir: every events-YYYYMMDD.jsonl and
// drops-YYYYMMDD.jsonl, sorted. Directories and other names are not listed. A link with such a name is
// listed, so that store.Remove refuses it. A missing folder gives none.
func purgeNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(isDayFile(n) || isDropsFile(n)) {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// Purge deletes every events-*.jsonl and drops-*.jsonl day file of the client's event log, and nothing
// else: not keep.txt, not a file outside events/, not another client's store. It is for a person only:
// inside a Claude Code session it refuses before it reads anything (RefusePerson). Then it applies the
// rules every lobe verb follows: the client (Target), the login (CheckLogin), and the store (openStore).
//
// ask is called with one line that says how many files go and which client they are of. It returns an
// error to keep the files; the cobra command asks for a typed yes, or prints the line and returns nil under
// --yes. Nothing is deleted before ask returns nil. Deletion runs under the store lock, which Purge waits
// up to purgeLockWait for; the listing is read again under that lock, and each file goes through
// store.Remove, which refuses a link. With no matching file it prints a line, asks nothing and exits 0.
func Purge(d Deps, clientFlag string, ask func(about string) error, out io.Writer) error {
	if err := RefusePerson(d); err != nil {
		return err
	}
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
	dir := filepath.Join(st.Dir(), "events")
	names, err := purgeNames(dir)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintf(out, "No event or drops files to delete for client %s.\n", ws.Name)
		return nil
	}
	about := fmt.Sprintf("tentaqles lobe events purge: about to delete %d event and drops files of the client %s (%s)",
		len(names), ws.Name, dir)
	if err := ask(about); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), purgeLockWait)
	defer cancel()
	unlock, err := st.Lock(ctx)
	if err != nil {
		return fmt.Errorf("the event log of %s is busy, so nothing was deleted; run the command again: %w", ws.Name, err)
	}
	defer unlock()
	names, err = purgeNames(dir)
	if err != nil {
		return err
	}
	// Every name is checked before the first delete, so a link among them stops the purge with nothing
	// deleted, rather than leaving the files half gone.
	for _, n := range names {
		fi, err := os.Lstat(filepath.Join(dir, n))
		if err != nil {
			return fmt.Errorf("checking events/%s, so nothing was deleted: %w", n, err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("events/%s is a link or not a regular file, so nothing was deleted; replace it with a file or remove it, then run the command again", n)
		}
	}
	for _, n := range names {
		if err := st.Remove(filepath.ToSlash(filepath.Join("events", n))); err != nil {
			return fmt.Errorf("deleting events/%s: %w", n, err)
		}
	}
	fmt.Fprintf(out, "Deleted %d event and drops files of client %s.\n", len(names), ws.Name)
	return nil
}
