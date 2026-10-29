package ledger

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestMain doubles as the helper process of the cross-process test.
func TestMain(m *testing.M) {
	switch os.Getenv("TQ_LEDGER_HELPER") {
	case "append":
		n, _ := strconv.Atoi(os.Getenv("TQ_LEDGER_N"))
		exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		for i := 0; i < n; i++ {
			if err := Append("acme", Entry{Event: Asked, Decision: fmt.Sprintf("live-%d", i), Pack: "ml", Grant: "approve", ExpiresAt: exp}); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		os.Exit(0)
	case "compact":
		stop := time.Now().Add(3 * time.Second)
		for time.Now().Before(stop) {
			_ = Compact("acme", "ml", time.Now())
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(t *testing.T, mode string, n int) *exec.Cmd {
	t.Helper()
	self, _ := os.Executable()
	c := exec.Command(self, "-test.run=^$")
	c.Env = append(os.Environ(), "TQ_LEDGER_HELPER="+mode, "TQ_LEDGER_N="+strconv.Itoa(n))
	c.Stderr = os.Stderr
	return c
}

// m-r1-2: a compactor in one process never loses the lines another
// process appends.
func TestCompactDoesNotLoseConcurrentAppends(t *testing.T) {
	isolate(t)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	for i := 0; i < 200; i++ { // finished entries, so the compactor has work
		if err := Append("acme", Entry{Event: Asked, Decision: fmt.Sprintf("old-%d", i), Pack: "ml", Grant: "approve", ExpiresAt: past}); err != nil {
			t.Fatal(err)
		}
	}
	const n = 150
	comp := helper(t, "compact", 0)
	if err := comp.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a := helper(t, "append", n)
		if err := a.Run(); err != nil {
			t.Errorf("appender: %v", err)
		}
	}()
	wg.Wait()
	_ = comp.Wait()
	es, err := Read("acme", "ml")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range es {
		got[e.Decision] = true
	}
	for i := 0; i < n; i++ {
		if !got[fmt.Sprintf("live-%d", i)] {
			t.Fatalf("lost live-%d (have %d entries)", i, len(es))
		}
	}
	p, _ := Path("acme", "ml")
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "*.compact")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
