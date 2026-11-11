package eventlog

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/settings"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

const (
	// sweepMaxFiles is the most files one sweep deletes (proposed in 011). The rest wait for the next run.
	sweepMaxFiles = 64
	// sweepMaxBytes is the total the day files may hold before the oldest go (proposed in 011): 64 MiB.
	sweepMaxBytes = 64 << 20
	// defaultRetention is read for a retention_days outside settings.RetentionMin..RetentionMax (the
	// default of settings, 14). settings keeps its default private, so the value is repeated here.
	defaultRetention = 14
	// eventsFolder is the store-relative folder of the day files.
	eventsFolder = "events"
	// dayLayout is the date part of a day file name: YYYYMMDD, UTC.
	dayLayout = "20060102"
)

// dayFile is one day file of the events folder: its name, the date in its name and its size.
type dayFile struct {
	name string
	date time.Time
	size int64
}

// Sweep deletes the expired day files of the store's events folder and returns how many it deleted.
//
// A day file is events-YYYYMMDD.jsonl or drops-YYYYMMDD.jsonl whose date is more than retentionDays days
// before the UTC date of now. A file with another name, or a date that does not parse, is kept. When the
// valid day files together hold more than 64 MiB, the oldest go too until they do not. At most 64 files
// are deleted per run, oldest first. A retentionDays outside settings.RetentionMin..RetentionMax is read
// as 14.
//
// The function takes the store lock and waits up to LockWait for it, as Append does. A ctx that is already
// done on entry returns its error. A ctx that ends while waiting for the lock, or a lock held by another
// process, deletes nothing and returns (0, nil). It takes the store, not a folder path, so it can only
// reach the events folder of one client; it never follows a link or touches a sub-folder. A file that
// cannot be deleted is skipped: the rest of the expired files are still deleted, and the first such error
// is returned with the count of the files that were deleted. Once ctx is done mid-run, it returns the ctx
// error at the next check, with the count of the files already deleted. The signature takes ctx, the store, retentionDays and now (not a folder and a clock
// value read inside) so a test can fix the date, and so the hook's sweep step and the Stop-time pass of 018
// call the same code.
func Sweep(ctx context.Context, s *store.Store, retentionDays int, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if retentionDays < settings.RetentionMin || retentionDays > settings.RetentionMax {
		retentionDays = defaultRetention
	}
	lctx, cancel := context.WithTimeout(ctx, LockWait)
	unlock, err := s.Lock(lctx)
	cancel()
	switch {
	case err == nil:
	case errors.Is(err, store.ErrBusy):
		return 0, nil
	default:
		return 0, err
	}
	defer unlock()
	return sweepLocked(ctx, s, retentionDays, now)
}

// sweepRemove deletes one store-relative file. It is a variable so a test can make one delete fail; the
// real one is the store's Remove.
var sweepRemove = (*store.Store).Remove

// sweepLocked is Sweep under the store lock, with retentionDays already read.
func sweepLocked(ctx context.Context, s *store.Store, retentionDays int, now time.Time) (int, error) {
	dir := filepath.Join(s.Dir(), eventsFolder)
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !fi.IsDir() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return 0, store.ErrLinked
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var files []dayFile
	var total int64
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		date, ok := dayFileDate(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, dayFile{name: e.Name(), date: date, size: info.Size()})
		total += info.Size()
	}
	// Oldest first: by date, then by name. Every expired file sorts before every kept one.
	sort.Slice(files, func(i, j int) bool {
		if !files[i].date.Equal(files[j].date) {
			return files[i].date.Before(files[j].date)
		}
		return files[i].name < files[j].name
	})

	y, m, d := now.UTC().Date()
	cutoff := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -retentionDays)

	deleted := 0
	var firstErr error
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		if deleted >= sweepMaxFiles {
			break
		}
		expired := f.date.Before(cutoff)
		if !expired && total <= sweepMaxBytes {
			continue
		}
		err := sweepRemove(s, eventsFolder+"/"+f.name)
		if errors.Is(err, store.ErrLinked) {
			// Replaced by a link since the listing: not ours to delete, and not counted.
			continue
		}
		if err != nil {
			// One file that cannot go (held open, permission denied) does not stop the rest.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		total -= f.size
		deleted++
	}
	return deleted, firstErr
}

// dayFileDate reads the UTC date of a day file name, events-YYYYMMDD.jsonl or drops-YYYYMMDD.jsonl. It
// reports false for any other name and for a date that does not parse back to the same eight digits.
func dayFileDate(name string) (time.Time, bool) {
	var rest string
	switch {
	case strings.HasPrefix(name, "events-"):
		rest = strings.TrimPrefix(name, "events-")
	case strings.HasPrefix(name, "drops-"):
		rest = strings.TrimPrefix(name, "drops-")
	default:
		return time.Time{}, false
	}
	if !strings.HasSuffix(rest, ".jsonl") {
		return time.Time{}, false
	}
	ds := strings.TrimSuffix(rest, ".jsonl")
	if len(ds) != len(dayLayout) {
		return time.Time{}, false
	}
	t, err := time.Parse(dayLayout, ds)
	if err != nil || t.Format(dayLayout) != ds {
		return time.Time{}, false
	}
	return t, true
}
