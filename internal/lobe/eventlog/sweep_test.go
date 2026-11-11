package eventlog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// sweepNow is the clock of the sweep tests: 2026-11-23 12:00 UTC. With retention 14 the day files of
// 2026-11-08 and earlier are expired, 2026-11-09 and later are kept.
func sweepNow(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-11-23T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// makeSweepFiles creates each name in the events folder of st, with the size given (0 for an empty file).
// Sizes are set with os.Truncate, so a 24 MiB file is not written out byte by byte.
func makeSweepFiles(t *testing.T, st *store.Store, sizes map[string]int64) {
	t.Helper()
	dir := filepath.Join(st.Dir(), "events")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, size := range sizes {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if err := os.Truncate(p, size); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// sweepNames lists the names in the events folder of st, sorted.
func sweepNames(t *testing.T, st *store.Store) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(st.Dir(), "events"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// wantNames fails the test unless got and want hold the same names.
func wantNames(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("folder holds %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("folder holds %v, want %v", got, want)
		}
	}
}

// TestLobeLogSweep: retention 14 and day files 13, 14 and 15 days old: only the one 15 days old is deleted;
// a file with another name stays.
func TestLobeLogSweep(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	makeSweepFiles(t, st, map[string]int64{
		"events-20261110.jsonl": 0, // 13 days old
		"events-20261109.jsonl": 0, // 14 days old
		"events-20261108.jsonl": 0, // 15 days old
		"notes.txt":             0,
	})
	n, err := Sweep(context.Background(), st, 14, sweepNow(t))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d, want 1", n)
	}
	wantNames(t, sweepNames(t, st), []string{"events-20261109.jsonl", "events-20261110.jsonl", "notes.txt"})
}

// TestLobeLogSweepBounds: the sweep's limits. The lock held elsewhere deletes nothing; 100 expired files
// take two runs (64, then 36); day files over 64 MiB lose the oldest until they are under it; a name that
// does not parse stays; a link is kept with its target; retention 3 and an invalid retention (read as 14)
// each delete what their rule says; a done context deletes nothing.
func TestLobeLogSweepBounds(t *testing.T) {
	t.Run("lock held elsewhere deletes nothing", func(t *testing.T) {
		setupHome(t)
		st := openAcme(t)
		makeSweepFiles(t, st, map[string]int64{"events-20260101.jsonl": 0})
		unlock, err := st.Lock(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		n, err := Sweep(context.Background(), st, 14, sweepNow(t))
		if err != nil || n != 0 {
			t.Fatalf("Sweep while locked = (%d, %v), want (0, nil)", n, err)
		}
		wantNames(t, sweepNames(t, st), []string{"events-20260101.jsonl"})
	})

	t.Run("100 expired files: 64 then 36, oldest first", func(t *testing.T) {
		setupHome(t)
		st := openAcme(t)
		first, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
		sizes := map[string]int64{}
		var all []string
		for i := 0; i < 100; i++ {
			name := "events-" + first.AddDate(0, 0, i).Format("20060102") + ".jsonl"
			sizes[name] = 0
			all = append(all, name)
		}
		makeSweepFiles(t, st, sizes)
		n, err := Sweep(context.Background(), st, 14, sweepNow(t))
		if err != nil || n != 64 {
			t.Fatalf("first Sweep = (%d, %v), want (64, nil)", n, err)
		}
		wantNames(t, sweepNames(t, st), all[64:])
		n, err = Sweep(context.Background(), st, 14, sweepNow(t))
		if err != nil || n != 36 {
			t.Fatalf("second Sweep = (%d, %v), want (36, nil)", n, err)
		}
		wantNames(t, sweepNames(t, st), nil)
	})

	t.Run("day files over 64 MiB lose the oldest until under", func(t *testing.T) {
		setupHome(t)
		st := openAcme(t)
		const mib = 1 << 20
		makeSweepFiles(t, st, map[string]int64{
			"events-20261112.jsonl": 24 * mib,
			"drops-20261115.jsonl":  24 * mib,
			"events-20261120.jsonl": 24 * mib, // 72 MiB together: the oldest goes, 48 MiB is under 64
		})
		n, err := Sweep(context.Background(), st, 14, sweepNow(t))
		if err != nil || n != 1 {
			t.Fatalf("Sweep = (%d, %v), want (1, nil)", n, err)
		}
		wantNames(t, sweepNames(t, st), []string{"drops-20261115.jsonl", "events-20261120.jsonl"})
	})

	t.Run("a name that does not parse stays", func(t *testing.T) {
		setupHome(t)
		st := openAcme(t)
		makeSweepFiles(t, st, map[string]int64{
			"events-2020.jsonl":        0,
			"events-20201301.jsonl":    0, // month 13
			"events-20200101.jsonl.bk": 0,
			"drops-20200101.txt":       0,
			"events-20200101.jsonl":    0, // a valid expired name: the one deleted
		})
		if err := os.Mkdir(filepath.Join(st.Dir(), "events", "events-20200102.jsonl"), 0700); err != nil {
			t.Fatal(err)
		}
		n, err := Sweep(context.Background(), st, 14, sweepNow(t))
		if err != nil || n != 1 {
			t.Fatalf("Sweep = (%d, %v), want (1, nil)", n, err)
		}
		wantNames(t, sweepNames(t, st), []string{
			"events-2020.jsonl", "events-20201301.jsonl", "events-20200101.jsonl.bk",
			"drops-20200101.txt", "events-20200102.jsonl",
		})
	})

	t.Run("a link is kept and its target is not touched", func(t *testing.T) {
		setupHome(t)
		st := openAcme(t)
		makeSweepFiles(t, st, nil)
		target := filepath.Join(t.TempDir(), "outside.jsonl")
		if err := os.WriteFile(target, []byte("x\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(st.Dir(), "events", "events-20200104.jsonl")); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
		n, err := Sweep(context.Background(), st, 14, sweepNow(t))
		if err != nil || n != 0 {
			t.Fatalf("Sweep = (%d, %v), want (0, nil)", n, err)
		}
		wantNames(t, sweepNames(t, st), []string{"events-20200104.jsonl"})
		if _, err := os.Stat(target); err != nil {
			t.Errorf("link target was touched: %v", err)
		}
	})

	t.Run("retention 3 deletes from four days back", func(t *testing.T) {
		setupHome(t)
		st := openAcme(t)
		makeSweepFiles(t, st, map[string]int64{
			"events-20261119.jsonl": 0, // 4 days old
			"events-20261120.jsonl": 0, // 3 days old
		})
		n, err := Sweep(context.Background(), st, 3, sweepNow(t))
		if err != nil || n != 1 {
			t.Fatalf("Sweep = (%d, %v), want (1, nil)", n, err)
		}
		wantNames(t, sweepNames(t, st), []string{"events-20261120.jsonl"})
	})

	for _, bad := range []int{0, -1, 366} {
		t.Run("retention outside 1..365 is read as 14", func(t *testing.T) {
			setupHome(t)
			st := openAcme(t)
			makeSweepFiles(t, st, map[string]int64{
				"events-20261108.jsonl": 0, // 15 days old: deleted at 14
				"events-20261109.jsonl": 0, // 14 days old: kept at 14
			})
			n, err := Sweep(context.Background(), st, bad, sweepNow(t))
			if err != nil || n != 1 {
				t.Fatalf("Sweep(retention %d) = (%d, %v), want (1, nil)", bad, n, err)
			}
			wantNames(t, sweepNames(t, st), []string{"events-20261109.jsonl"})
		})
	}

	t.Run("a done context deletes nothing", func(t *testing.T) {
		setupHome(t)
		st := openAcme(t)
		makeSweepFiles(t, st, map[string]int64{"events-20260101.jsonl": 0})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		n, err := Sweep(ctx, st, 14, sweepNow(t))
		if n != 0 || !errors.Is(err, context.Canceled) {
			t.Fatalf("Sweep with a done context = (%d, %v), want (0, context.Canceled)", n, err)
		}
		wantNames(t, sweepNames(t, st), []string{"events-20260101.jsonl"})
	})
}

// TestLobeLogSweepSkipsAFailedDelete: one expired file that cannot be deleted does not stop the sweep. The
// two expired files after it are deleted, the failed one stays, and Sweep returns the delete's error with
// the count of the files it did delete.
func TestLobeLogSweepSkipsAFailedDelete(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	makeSweepFiles(t, st, map[string]int64{
		"events-20260101.jsonl": 0, // oldest: the delete fails
		"events-20260102.jsonl": 0,
		"events-20260103.jsonl": 0,
		"events-20261120.jsonl": 0, // not expired at 14 days: kept
	})
	errBoom := errors.New("file is held open")
	old := sweepRemove
	t.Cleanup(func() { sweepRemove = old })
	sweepRemove = func(s *store.Store, rel string) error {
		if rel == "events/events-20260101.jsonl" {
			return errBoom
		}
		return old(s, rel)
	}
	n, err := Sweep(context.Background(), st, 14, sweepNow(t))
	if !errors.Is(err, errBoom) {
		t.Fatalf("Sweep error = %v, want %v", err, errBoom)
	}
	if n != 2 {
		t.Errorf("deleted %d, want 2", n)
	}
	wantNames(t, sweepNames(t, st), []string{"events-20260101.jsonl", "events-20261120.jsonl"})
}
