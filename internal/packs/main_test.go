package packs

import (
	"os"
	"testing"
)

// The tests run with generous read, hash and guard budgets: on a loaded CI
// runner reading a small fixture can take longer than the 40 ms read
// budget, which fails closed instead of testing what ReadLocal or a guard
// decides (CI on Windows: TestReadLocalRefusals,
// TestReadLocalResolvesTheFinalPath). The tests of what going over a
// budget does set the bound they test: TestGuardBudgetMeansDeny (the real
// 50 ms against a 300 ms guard), TestReadStateBounds, TestHashStateBounds
// and TestTotalGuardBudget (an already spent budget).
func TestMain(m *testing.M) {
	GenerousBudgetsForTests()
	os.Exit(m.Run())
}

// realGuardBudget puts the production per-guard budget back for one test.
func realGuardBudget(t testing.TB) {
	prev := guardBudget
	guardBudget = GuardBudget
	t.Cleanup(func() { guardBudget = prev })
}
