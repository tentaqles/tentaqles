package health

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }

// clientWithLobe is a client named acme whose manifest holds the given lobe text (a whole YAML fragment).
func clientWithLobe(t *testing.T, lobe string) Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	raw := "schema: tentaqles-client-v2\nclient: acme\ngit:\n    email: dev@acme.test\nidentities:\n    claude: {}\n" + lobe
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return Client{Name: "acme", WS: &resolve.Workspace{Name: "acme", ManifestPath: path, Manifest: m}}
}

// A lobe value that is not a mapping makes all nine settings invalid, and the fix says to edit the lobe key
// of the named manifest by hand, not to run lobe set (which cannot write into it).
func TestSettingFinding_NotAMappingFixIsByHand(t *testing.T) {
	c := clientWithLobe(t, "lobe: 7\n")
	f, ok := settingFinding(c)
	if !ok {
		t.Fatal("no lobe-setting-invalid finding for lobe: 7")
	}
	if f.Code != CodeSettingInvalid {
		t.Fatalf("code = %s", f.Code)
	}
	if !strings.Contains(f.Fix, "by hand") || !strings.Contains(f.Fix, "lobe key") {
		t.Errorf("fix does not say to edit the lobe key by hand: %q", f.Fix)
	}
	if !strings.Contains(f.Fix, c.WS.ManifestPath) {
		t.Errorf("fix does not name the manifest %s: %q", c.WS.ManifestPath, f.Fix)
	}
	if strings.Contains(f.Fix, "tentaqles lobe set <key>") {
		t.Errorf("fix offers lobe set for a lobe value that is not a mapping: %q", f.Fix)
	}
}

// A mapping with a wrong value keeps the lobe set fix line.
func TestSettingFinding_MappingFixIsLobeSet(t *testing.T) {
	c := clientWithLobe(t, "lobe:\n    capture: maybe\n")
	f, ok := settingFinding(c)
	if !ok {
		t.Fatal("no lobe-setting-invalid finding for capture: maybe")
	}
	if f.Fix != "set each value with tentaqles lobe set <key> <value>; tentaqles lobe settings shows them" {
		t.Errorf("fix = %q", f.Fix)
	}
}

// openHookStore makes the client's store under the test home and opens it read-write, as a render does.
func openHookStore(t *testing.T, name string) *store.Store {
	t.Helper()
	if err := os.MkdirAll(paths.IdentityDir(name, ""), 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(name, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// writeHookErrors replaces stats/hook-errors.jsonl of the store with body.
func writeHookErrors(t *testing.T, st *store.Store, body string) {
	t.Helper()
	p := filepath.Join(st.Dir(), "stats", "hook-errors.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// hookLine is one error line stamped at, with pad bytes of text.
func hookLine(at time.Time, pad int) string {
	return fmt.Sprintf(`{"at":%q,"pad":%q}`+"\n", at.UTC().Format(time.RFC3339), strings.Repeat("x", pad))
}

// A line of 300 KB does not stop the count: the scanner's buffer holds it, and the lines after it are counted.
func TestRecentHookErrors_LongLineDoesNotStopCounting(t *testing.T) {
	st := openHookStore(t, "acme")
	now := time.Now()
	var b strings.Builder
	b.WriteString(hookLine(now, 300<<10))
	for i := 0; i < 5; i++ {
		b.WriteString(hookLine(now, 0))
	}
	writeHookErrors(t, st, b.String())
	if n := recentHookErrors(st, now); n != 6 {
		t.Fatalf("counted %d lines, want 6 (the long one and the five after it)", n)
	}
}

// Only the last 1 MiB of the file is read: recent lines that lie before a megabyte of other text are not
// counted, and recent lines at the end are.
func TestRecentHookErrors_ReadsOnlyTheLastMiB(t *testing.T) {
	st := openHookStore(t, "acme")
	now := time.Now()
	recent := strings.Repeat(hookLine(now, 0), 5)
	filler := strings.Repeat("not a json line\n", 80000) // about 1.2 MiB

	writeHookErrors(t, st, recent+filler)
	if n := recentHookErrors(st, now); n != 0 {
		t.Errorf("lines before the last MiB were counted: %d", n)
	}

	writeHookErrors(t, st, filler+recent)
	if n := recentHookErrors(st, now); n != 5 {
		t.Errorf("counted %d lines at the end of the file, want 5", n)
	}
}

// The Codex handler's state gives one lobe-hooks-untrusted warning unless it is "" (no codex in the scope) or
// "trusted". The fix line is tentaqles settings render <client>. Other findings of the client are not counted.
func TestCodexHooksFinding(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{{"", false}, {"trusted", false}, {"untrusted", true}}
	for _, c := range cases {
		t.Run("state "+c.state, func(t *testing.T) {
			var got []Finding
			for _, f := range Findings([]Client{{Name: "acme", CodexHooks: c.state}}, time.Now()) {
				if f.Code == CodeHooksUntrusted {
					got = append(got, f)
				}
			}
			if !c.want {
				if len(got) != 0 {
					t.Fatalf("state %q gives %+v, want no finding", c.state, got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("state %q gives %d findings, want 1: %+v", c.state, len(got), got)
			}
			f := got[0]
			if f.Level != "warn" || f.Client != "acme" {
				t.Errorf("level %q client %q, want warn acme", f.Level, f.Client)
			}
			if !strings.Contains(f.Msg, c.state) {
				t.Errorf("msg %q does not name the state %q", f.Msg, c.state)
			}
			if f.Fix != "tentaqles settings render acme" {
				t.Errorf("fix = %q", f.Fix)
			}
		})
	}
}
