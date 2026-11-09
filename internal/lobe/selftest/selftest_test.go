package selftest

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// TestHelperProcess is not a test: the test binary re-runs itself with -test.run=^TestHelperProcess$,
// SELFTEST_HELPER=1 and SELFTEST_CLIENT in its environment, and the writer's rel, writer and count after
// "--". Without SELFTEST_HELPER it returns at once.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("SELFTEST_HELPER") != "1" {
		return
	}
	args := flag.Args()
	if len(args) != 3 {
		t.Fatalf("helper wants rel, writer and count, got %v", args)
	}
	writer, err := strconv.Atoi(args[1])
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	count, err := strconv.Atoi(args[2])
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	st, err := store.Open(os.Getenv("SELFTEST_CLIENT"), store.ReadWrite)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := WriterChild(st, args[0], writer, count); err != nil {
		t.Fatalf("writer %d: %v", writer, err)
	}
}

func TestLobeSelftestRun(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	clientDir := filepath.Join(paths.IdentitiesRoot(), "acme")
	if err := os.MkdirAll(clientDir, 0700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open("acme", store.ReadWrite)
	if err != nil {
		t.Fatalf("Open acme: %v", err)
	}
	t.Setenv("SELFTEST_HELPER", "1")
	t.Setenv("SELFTEST_CLIENT", "acme")

	childArgs := func(rel string, writer, count int) []string {
		return []string{"-test.run=^TestHelperProcess$", "--", rel, strconv.Itoa(writer), strconv.Itoa(count)}
	}
	res, err := Run(st, clientDir, os.Args[0], childArgs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("result: %+v", res)
	if res.Writers != 20 || res.Lines != 10000 || res.Torn != 0 || res.Lost != 0 {
		t.Errorf("writers: %+v, want 20 writers, 10000 whole lines, none lost", res)
	}
	if res.Replaces != 1000 || res.Partial != 0 {
		t.Errorf("replaces: %+v, want 1000 replaces and no partial read", res)
	}
	if res.RepoKeyApp != "r_a172cedc" {
		t.Errorf("RepoKeyApp = %q, want r_a172cedc", res.RepoKeyApp)
	}
	ents, err := os.ReadDir(filepath.Join(st.Dir(), "tmp"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("the scratch folder was not removed: %v", ents)
	}
}
