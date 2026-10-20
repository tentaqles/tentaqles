package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// A transcript for the workspace root (or a project under it) that is much
// newer than memory.db means the plugin hooks stopped capturing.
func TestMemoryStale(t *testing.T) {
	isolatedHome(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(testutil.TempDir(t), "acme")
	db := filepath.Join(root, ".claude", "memory.db")
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(db, now.Add(-5*24*time.Hour), now.Add(-5*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	transcript := func(slug string, age time.Duration) {
		dir := filepath.Join(paths.IdentityDir("acme", "claude"), "projects", slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(dir, "s.jsonl")
		if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(f, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	collect := func() []Finding {
		var got []Finding
		memoryStale("acme", root, now, func(level, code, ws, msg, fix string) {
			got = append(got, Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix})
		})
		return got
	}

	// No transcripts at all: nothing to compare against.
	if got := collect(); len(got) != 0 {
		t.Fatalf("no transcripts, got %+v", got)
	}
	// A transcript from another workspace is ignored even when it is recent.
	transcript("c--repos-other", time.Hour)
	if got := collect(); len(got) != 0 {
		t.Fatalf("foreign transcript, got %+v", got)
	}
	// A transcript of a project under the root, newer than the db by well
	// over the grace period: stale.
	transcript(strings.ToLower(transcriptSlug(root))+"-sub", 2*time.Hour)
	got := collect()
	if len(got) != 1 || got[0].Code != "memory-stale" || got[0].Level != "warn" || got[0].Workspace != "acme" || got[0].Fix == "" {
		t.Fatalf("stale, got %+v", got)
	}
	if !strings.Contains(got[0].Msg, "memory.db") {
		t.Fatalf("message must name the db: %q", got[0].Msg)
	}
	// Once the db is touched again the finding goes away.
	if err := os.Chtimes(db, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := collect(); len(got) != 0 {
		t.Fatalf("fresh db, got %+v", got)
	}
}

func TestMemoryStale_NoDBIsNotAFinding(t *testing.T) {
	isolatedHome(t)
	root := filepath.Join(testutil.TempDir(t), "acme")
	var got []Finding
	memoryStale("acme", root, time.Now(), func(level, code, ws, msg, fix string) {
		got = append(got, Finding{Code: code})
	})
	if len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestTranscriptSlug(t *testing.T) {
	cases := map[string]string{
		`C:\repos\tentaqles\tentaqles`: "C--repos-tentaqles-tentaqles",
		"/home/u/work/acme":            "-home-u-work-acme",
		"C:/repos/dirty bird":          "C--repos-dirty-bird",
	}
	for in, want := range cases {
		if got := transcriptSlug(in); got != want {
			t.Errorf("transcriptSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
