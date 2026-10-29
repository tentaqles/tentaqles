package gate

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// The functional tests run with a generous hash budget: a loaded CI runner
// can take longer than the guard's 30 ms to stat and read the fixtures,
// and that fails closed instead of testing the binding.
// TestHashBudgetDenies keeps the real behaviour: a hash over the budget
// denies.
func TestMain(m *testing.M) {
	HashBudget = 10 * time.Second
	os.Exit(m.Run())
}

func TestHashBudgetDenies(t *testing.T) {
	prev := HashBudget
	HashBudget = time.Nanosecond
	t.Cleanup(func() { HashBudget = prev })
	f := approvedProd(t)
	a, r, _ := f.migrate("Bash", "supabase db push")
	if a != policy.Deny || !strings.Contains(r, "time budget") {
		t.Fatalf("over the budget: got %s (%s), want a deny naming the budget", a, r)
	}
}
