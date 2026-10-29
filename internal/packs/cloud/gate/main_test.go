package gate

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// The guard tests run with generous read and hash budgets: on a loaded CI
// runner a fixture read can exceed the 40 ms production bound, which fails
// closed (CI: TestPulumiWeakTreeBinding) instead of testing the binding.
// The packs package tests that going over a budget denies.
func TestMain(m *testing.M) {
	packs.GenerousBudgetsForTests()
	os.Exit(m.Run())
}
