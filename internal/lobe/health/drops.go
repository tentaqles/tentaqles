package health

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// CodeEventsDropped is lobe-events-dropped (feature 011, F-07 step 5): the client's drops files hold 5 or
// more lines from the last 24 hours.
const CodeEventsDropped = "lobe-events-dropped"

// EventsDroppedWindow and EventsDroppedMin are the thresholds of lobe-events-dropped: 5 or more lines of
// the drops files from the last 24 hours (feature 011, proposed there).
const (
	EventsDroppedWindow = 24 * time.Hour
	EventsDroppedMin    = 5
)

// dropsReadLimit is the most of a drops file that is read: 64 KiB, the size at which the file stops
// growing (eventlog.AppendDrop). A file longer than that is read only up to it.
const dropsReadLimit = 64 << 10

// droppedFinding is lobe-events-dropped for the client when its drops files hold EventsDroppedMin lines or
// more from the window that ends at now. The message counts the lines per reason, in name order.
func droppedFinding(client string, st *store.Store, now time.Time) (Finding, bool) {
	counts, total := recentDrops(st, now)
	if total < EventsDroppedMin {
		return Finding{}, false
	}
	reasons := make([]string, 0, len(counts))
	for why := range counts {
		reasons = append(reasons, why)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, why := range reasons {
		parts = append(parts, fmt.Sprintf("%s %d", why, counts[why]))
	}
	return Finding{
		Level:  "warn",
		Code:   CodeEventsDropped,
		Client: client,
		Msg: fmt.Sprintf("the client's event log refused %d events in the last 24 hours (%s)",
			total, strings.Join(parts, ", ")),
		Fix: "check the free disk space on the drive of this client's lobe folder and the size of today's events file in its events folder; to start the event log over, run tentaqles lobe events purge in your own terminal (it deletes every event and drops file of this client)",
	}, true
}

// recentDrops counts, per reason, the lines of the drops files of today and yesterday (UTC) whose "at" time
// is within the window that ends at now. Each file is read at most dropsReadLimit bytes. A line that does
// not parse is not counted.
func recentDrops(st *store.Store, now time.Time) (map[string]int, int) {
	counts := make(map[string]int)
	total := 0
	since := now.Add(-EventsDroppedWindow)
	for _, day := range []time.Time{now.UTC(), now.UTC().AddDate(0, 0, -1)} {
		data := readDropsFile(filepath.Join(st.Dir(), filepath.FromSlash(eventlog.DropsFileFor(day))))
		for _, line := range bytes.Split(data, []byte("\n")) {
			var e struct {
				At  string `json:"at"`
				Why string `json:"why"`
			}
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			at, err := time.Parse(time.RFC3339, e.At)
			if err != nil || at.Before(since) {
				continue
			}
			counts[e.Why]++
			total++
		}
	}
	return counts, total
}

// readDropsFile returns the first dropsReadLimit bytes of the regular file at path. A missing file, a
// link or a file that cannot be read gives nil.
func readDropsFile(path string) []byte {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, dropsReadLimit))
	return data
}
