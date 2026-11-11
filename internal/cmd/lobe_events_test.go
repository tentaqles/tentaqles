package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// treeSnapshot maps every regular file under dir (slash path, relative to dir) to its bytes.
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLobeEventsPurge: after a yes, every events and drops day file of acme is gone; keep.txt, every file
// outside events/ and the store.json stay byte for byte; globex's lobe folder is byte-identical. A declined
// question deletes nothing.
func TestLobeEventsPurge(t *testing.T) {
	base := newBase(t)
	acme := writeClient(t, base, "acme", lobeManifestBody)
	writeClient(t, base, "globex", healthBody("globex"))
	chdir(t, acme)
	for _, name := range []string{"acme", "globex"} {
		if _, err := store.Open(name, store.ReadWrite); err != nil {
			t.Fatal(err)
		}
	}

	today := time.Now().UTC().Format("20060102")
	acmeLobe := paths.LobeDir("acme")
	events := filepath.Join(acmeLobe, "events")
	doomed := []string{
		filepath.Join(events, "events-20200101.jsonl"),
		filepath.Join(events, "events-"+today+".jsonl"),
		filepath.Join(events, "drops-20200101.jsonl"),
		filepath.Join(events, "drops-"+today+".jsonl"),
	}
	kept := []string{
		filepath.Join(events, "keep.txt"),
		filepath.Join(events, "events-2020.jsonl"),
		filepath.Join(acmeLobe, "stats", "hook-errors.jsonl"),
		filepath.Join(acmeLobe, "lessons", "note.md"),
		filepath.Join(acmeLobe, "sessions", "s1.json"),
	}
	for _, p := range append(append([]string{}, doomed...), kept...) {
		writeTB(t, p, "line for "+filepath.Base(p)+"\n")
	}
	globexLobe := paths.LobeDir("globex")
	writeTB(t, filepath.Join(globexLobe, "events", "events-20200101.jsonl"), "globex event\n")
	writeTB(t, filepath.Join(globexLobe, "events", "drops-20200101.jsonl"), "globex drop\n")
	writeTB(t, filepath.Join(globexLobe, "stats", "hook-errors.jsonl"), "globex error\n")
	globexBefore := treeSnapshot(t, globexLobe)
	storeBefore := treeSnapshot(t, acmeLobe)

	// A declined question deletes nothing.
	if out, err := runLobe(t, "no\n", "lobe", "events", "purge"); err == nil {
		t.Fatalf("a declined purge exited 0:\n%s", out)
	}
	for _, p := range doomed {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("a declined purge deleted %s: %v", p, err)
		}
	}

	// A yes deletes the event and drops day files of acme, and nothing else.
	if out, err := runLobe(t, "yes\n", "lobe", "events", "purge"); err != nil {
		t.Fatalf("purge with yes: %v\n%s", err, out)
	}
	for _, p := range doomed {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the purge (err=%v)", p, err)
		}
	}
	after := treeSnapshot(t, acmeLobe)
	gone := map[string]bool{}
	for _, p := range doomed {
		rel, _ := filepath.Rel(acmeLobe, p)
		gone[filepath.ToSlash(rel)] = true
	}
	for rel, body := range storeBefore {
		if gone[rel] {
			if _, ok := after[rel]; ok {
				t.Errorf("%s is still there after the purge", rel)
			}
			continue
		}
		if got, ok := after[rel]; !ok {
			t.Errorf("%s was deleted by the purge", rel)
		} else if got != body {
			t.Errorf("%s changed under the purge", rel)
		}
	}
	for rel := range after {
		// The store's own lock file (store.Lock) is not an event file; every other new name is a leftover.
		if _, ok := storeBefore[rel]; !ok && rel != "lock" {
			t.Errorf("the purge left a new file %s", rel)
		}
	}
	if got := treeSnapshot(t, globexLobe); len(got) != len(globexBefore) {
		t.Errorf("globex's lobe folder changed: %d files, want %d", len(got), len(globexBefore))
	} else {
		for rel, body := range globexBefore {
			if got[rel] != body {
				t.Errorf("globex's %s changed under acme's purge", rel)
			}
		}
	}
}

// TestLobeEventsPurgeRefusesALink: a link among the day files stops the purge before the first delete. The
// regular day files before it in name order stay, the link stays, and the link's target is untouched; the
// command exits non-zero. It skips when this system cannot make a file symbolic link.
func TestLobeEventsPurgeRefusesALink(t *testing.T) {
	base := newBase(t)
	acme := writeClient(t, base, "acme", lobeManifestBody)
	chdir(t, acme)
	if _, err := store.Open("acme", store.ReadWrite); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(paths.LobeDir("acme"), "events")
	regular := []string{
		filepath.Join(events, "drops-20200101.jsonl"),
		filepath.Join(events, "events-20200101.jsonl"),
	}
	for _, p := range regular {
		writeTB(t, p, "line\n")
	}
	target := filepath.Join(t.TempDir(), "outside.jsonl")
	writeTB(t, target, "outside\n")
	link := filepath.Join(events, "events-20200102.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot make a symbolic link: %v", err)
	}

	if out, err := runLobe(t, "yes\n", "lobe", "events", "purge"); err == nil {
		t.Fatalf("a purge over a link exited 0:\n%s", out)
	}
	for _, p := range regular {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a refused purge deleted %s: %v", p, err)
		}
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("a refused purge removed the link: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("the link's target was touched: %v", err)
	}
}
