package ml

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// toolrun passes a filtered environment: every external ml check names the
// variables its tool needs.
func TestExternalChecksDeclareEnv(t *testing.T) {
	for _, c := range packs.Checks("ml") {
		if c.Kind == packs.External && len(c.Env) == 0 {
			t.Errorf("%s runs %s with no Check.Env", c.ID, c.Tool)
		}
	}
}
