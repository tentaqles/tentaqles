package toolrun

import (
	"context"
	"testing"
)

// Integration round 3: the version probe runs with the tool's telemetry
// switches too (tflint --version checks GitHub for a newer release unless
// TFLINT_DISABLE_VERSION_CHECK=1).
func TestVersionProbeGetsTelemetrySwitches(t *testing.T) {
	isolate(t)
	t.Setenv("TQ_FAKE_TOOL", "versiontelemetry")
	p := fakeBin(t, t.TempDir(), "tflint")
	if v := Version(context.Background(), p); v != "0.53.0" {
		t.Fatalf("the probe ran without TFLINT_DISABLE_VERSION_CHECK=1: version %q", v)
	}
}

// The opt-outs confirmed from each tool's docs.
func TestTelemetrySwitches(t *testing.T) {
	for tool, want := range map[string]string{
		"dbt":     "DBT_SEND_ANONYMOUS_USAGE_STATS=false",
		"tflint":  "TFLINT_DISABLE_VERSION_CHECK=1",
		"trivy":   "TRIVY_DISABLE_TELEMETRY=true",
		"dvc":     "DVC_NO_ANALYTICS=1",
		"dagster": "DAGSTER_DISABLE_TELEMETRY=1",
	} {
		found := false
		for _, kv := range Telemetry[tool] {
			if kv == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: %s missing from %v", tool, want, Telemetry[tool])
		}
	}
	found := false
	for _, kv := range Telemetry["trivy"] {
		if kv == "TRIVY_SKIP_VERSION_CHECK=true" {
			found = true
		}
	}
	if !found {
		t.Error("trivy: TRIVY_SKIP_VERSION_CHECK=true missing")
	}
}
