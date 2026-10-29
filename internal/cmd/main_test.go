package cmd

import (
	"os"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	dbgate "github.com/tentaqles/tentaqles/internal/packs/db/gate"
)

// The hook tests run the pack guards end to end with generous read and hash
// budgets: on a loaded CI runner hashing the fixture migrations can exceed
// the 30 ms production bound, which fails closed (CI on Windows:
// TestHookDBApprovedMigrationOnce) instead of testing the approval flow.
// internal/packs and internal/packs/db/gate test that going over a budget
// denies.
func TestMain(m *testing.M) {
	packs.GenerousBudgetsForTests()
	dbgate.HashBudget = 10 * time.Second
	os.Exit(m.Run())
}
