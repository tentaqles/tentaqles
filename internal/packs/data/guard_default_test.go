package data

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// Final review m4: each data guard declares the default tq guard list shows.
func TestDataGuardDefaults(t *testing.T) {
	want := map[string]string{"tq/dbt-full-refresh": "ask", "tq/pipeline-run": "ask", "tq/warehouse-query-unbounded": "ask",
		"tq/dbt-prod-target": "deny", "tq/bq-unbounded-query": "deny", "tq/warehouse-resize": "deny or ask"}
	n := 0
	for _, g := range packs.Guards() {
		if w, ok := want[g.ID]; ok {
			n++
			if g.Default != w {
				t.Errorf("%s: %q, want %q", g.ID, g.Default, w)
			}
		}
	}
	if n != len(want) {
		t.Fatalf("found %d of %d guards", n, len(want))
	}
}
