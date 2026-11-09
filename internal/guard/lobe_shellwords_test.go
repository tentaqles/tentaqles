package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type lobeShellCase struct {
	Command    string   `json:"command"`
	Words      []string `json:"words"`
	Statements []string `json:"statements"`
}

func lobeSameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLobeShellwordsMatchTheGuard checks the lobe's copy of the tokenizer
// (internal/lobe/shellwords) against the guard's own splitWords and
// segmentSpans, on the shared cases file.
func TestLobeShellwordsMatchTheGuard(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "lobe", "shellwords", "testdata", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []lobeShellCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 40 {
		t.Fatalf("got %d cases, want at least 40", len(cases))
	}
	for _, tc := range cases {
		if got := splitWords(tc.Command); !lobeSameStrings(got, tc.Words) {
			t.Errorf("guard splitWords(%q) = %q, want %q", tc.Command, got, tc.Words)
		}
		var stmts []string
		for _, sp := range segmentSpans(maskQuoted(tc.Command)) {
			if s := strings.TrimSpace(tc.Command[sp.start:sp.end]); s != "" {
				stmts = append(stmts, s)
			}
		}
		if !lobeSameStrings(stmts, tc.Statements) {
			t.Errorf("guard statements(%q) = %q, want %q", tc.Command, stmts, tc.Statements)
		}
	}
}
