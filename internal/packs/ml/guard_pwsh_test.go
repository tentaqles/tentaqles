package ml

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func encPwsh(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		b[2*i], b[2*i+1] = byte(r), byte(r>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Integration hardening: tq/model-promote reads a pwsh -EncodedCommand and
// asks on the PowerShell forms it cannot read.
func TestModelPromoteGuardPowerShellForms(t *testing.T) {
	g := guardCtx(t, guardConfig)
	for _, c := range []struct {
		cmd  string
		want policy.Action
	}{
		{"pwsh -EncodedCommand " + encPwsh("tq pack ml promote apply d-1"), policy.Deny},
		{"pwsh -e " + encPwsh("databricks registered-models set-alias main.ml.churn champion 7"), policy.Deny},
		{"powershell -NoProfile -enc " + encPwsh("pwsh -ec "+encPwsh("uv run python scripts/promote.py churn 7 production")), policy.Deny},
		{"pwsh -ec " + encPwsh("Get-Date"), policy.Allow},
		{"pwsh -e ###", policy.Ask},
		{"pwsh -File promote.ps1", policy.Ask},
		{"pwsh promote.ps1", policy.Ask},
		{"pwsh -Command -", policy.Ask},
		{"bash -c 'pwsh -f x.ps1'", policy.Ask},
		{"pwsh -NoProfile -Command Get-Date", policy.Allow},
		{"echo use pwsh -File x.ps1 later", policy.Allow},
		{strings.Repeat("x ", scan.MaxPwshWords+10) + "pwsh -c ok", policy.Ask},
	} {
		a, reason, _ := checkModelPromote(policy.ToolCall{Tool: "Bash", Command: c.cmd}, g)
		if a == "" {
			a = policy.Allow
		}
		if a != c.want {
			t.Errorf("%.60q: %q (%s), want %q", c.cmd, a, reason, c.want)
		}
	}
}
