package ml

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration round 3: global flags of tq (and of the pack subcommands)
// before the verb do not hide `tq pack ml promote apply`.
func TestModelPromoteApplyWithFlagsBetween(t *testing.T) {
	g := guardCtx(t, guardConfig)
	for _, c := range []struct {
		cmd  string
		want policy.Action
	}{
		{"tq pack ml promote apply d-1", policy.Deny},
		{"tq --workspace acme pack ml promote apply d-1", policy.Deny},
		{"tq pack ml --workspace acme promote apply d-1", policy.Deny},
		{"tq pack --json ml promote --yes apply d-1", policy.Deny},
		{"tq pack ml promote request churn@7", policy.Allow},
		{"tq pack ml promote status; echo apply", policy.Allow},
	} {
		if a, reason, _ := checkModelPromote(policy.ToolCall{Tool: "Bash", Command: c.cmd}, g); a != c.want {
			t.Errorf("%q: %q (%s), want %q", c.cmd, a, reason, c.want)
		}
	}
}
