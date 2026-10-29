package policy

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestRegoBundleWithOPA compiles the shipped Rego bundle under Rego v1 and
// runs its opa test fixtures, when an opa binary is on PATH (the CI job
// "opa" installs a pinned OPA 1.x). Without opa it is skipped.
func TestRegoBundleWithOPA(t *testing.T) {
	opa, err := exec.LookPath("opa")
	if err != nil {
		t.Skip("opa is not on PATH; the CI job opa runs this bundle")
	}
	for _, args := range [][]string{
		{"check", "--strict", "rego"},
		{"test", "rego"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		out, err := exec.CommandContext(ctx, opa, args...).CombinedOutput()
		cancel()
		if err != nil {
			t.Errorf("opa %v: %v\n%s", args, err, out)
		}
	}
}
