package cmd

import (
	"encoding/base64"
	"strings"
	"testing"
)

// probeOut is what the PowerShell probe prints for a profile path and a policy: the path as base64 of its
// UTF-8 bytes on the first line, the policy on the last, and an optional line ending.
func probeOut(profile, policy, eol string) []byte {
	return []byte(base64.StdEncoding.EncodeToString([]byte(profile)) + eol + policy + eol)
}

// TestCheckPowerShellProbe is the table of the probe's output (L-11): the profile on the first line, the
// policy on the last, the policies that refuse and the ones that accept, a differing path, CRLF output,
// too few lines, and a non-ASCII path (L-10).
func TestCheckPowerShellProbe(t *testing.T) {
	const want = `C:\Users\renato\Documents\PowerShell\Microsoft.PowerShell_profile.ps1`
	const joao = `C:\Users\João\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1`
	cases := []struct {
		name    string
		out     []byte
		profile string
		refuse  string // a substring of the error, or "" when the probe must pass
	}{
		{"remote signed passes", probeOut(want, "RemoteSigned", "\n"), want, ""},
		{"unrestricted passes", probeOut(want, "Unrestricted", "\n"), want, ""},
		{"bypass passes", probeOut(want, "Bypass", "\n"), want, ""},
		{"undefined passes", probeOut(want, "Undefined", "\n"), want, ""},
		{"restricted refused", probeOut(want, "Restricted", "\n"), want, "Restricted"},
		{"allsigned refused", probeOut(want, "AllSigned", "\n"), want, "AllSigned"},
		{"a differing path refused", probeOut(`C:\Users\other\profile.ps1`, "RemoteSigned", "\n"), want, "reads its profile from"},
		{"crlf output passes", probeOut(want, "RemoteSigned", "\r\n"), want, ""},
		{"a blank line is skipped", []byte("\r\n" + base64.StdEncoding.EncodeToString([]byte(want)) + "\r\n\r\nRemoteSigned\r\n"), want, ""},
		{"one line refused", []byte(base64.StdEncoding.EncodeToString([]byte(want)) + "\n"), want, "did not say where"},
		{"nothing refused", nil, want, "did not say where"},
		{"a path that is not base64 refused", []byte("C:\\not-base64\nRemoteSigned\n"), want, "cannot read"},
		{"a non-ASCII path passes", probeOut(joao, "RemoteSigned", "\n"), joao, ""},
		{"a non-ASCII path that differs refused", probeOut(joao, "RemoteSigned", "\n"), want, "reads its profile from"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkPowerShellProbe("powershell", tc.out, tc.profile)
			switch {
			case tc.refuse == "" && err != nil:
				t.Fatalf("must pass: %v", err)
			case tc.refuse != "" && err == nil:
				t.Fatalf("must refuse with %q", tc.refuse)
			case tc.refuse != "" && !strings.Contains(err.Error(), tc.refuse):
				t.Fatalf("error %q, want it to contain %q", err, tc.refuse)
			}
		})
	}
}

// The probe's refusal names the path PowerShell reads, decoded, so a non-ASCII folder reads right (L-10).
func TestCheckPowerShellProbe_NamesANonASCIIPath(t *testing.T) {
	const joao = `C:\Users\João\Documents\PowerShell\Microsoft.PowerShell_profile.ps1`
	err := checkPowerShellProbe("pwsh", probeOut(joao, "RemoteSigned", "\n"), `C:\Users\Joao\Documents\PowerShell\Microsoft.PowerShell_profile.ps1`)
	if err == nil || !strings.Contains(err.Error(), joao) {
		t.Fatalf("the path does not read right: %v", err)
	}
}
