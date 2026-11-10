package fingerprint

import "testing"

// TestClassRows covers every row of S-08 step 1 and the edge names of the mcp rule.
func TestClassRows(t *testing.T) {
	cases := []struct {
		tool string
		want string
	}{
		{"Bash", "shell"},
		{"PowerShell", "shell"},
		{"Edit", "edit"},
		{"Write", "edit"},
		{"MultiEdit", "edit"},
		{"NotebookEdit", "edit"},
		{"apply_patch", "edit"},
		{"Read", "read"},
		{"Grep", "read"},
		{"Glob", "read"},
		{"WebFetch", "web"},
		{"WebSearch", "web"},
		{"mcp__warehouse__query", "mcp:warehouse"},
		{"mcp__tracker__create_issue", "mcp:tracker"},
		{"mcp__a__b__c", "mcp:a"},
		{"mcp__server", "other"},
		{"mcp__", "other"},
		{"mcp____tool", "other"},
		{"unknown", "other"},
		{"", "other"},
		{"bash", "other"},
		{"Task", "other"},
	}
	for _, c := range cases {
		if got := Class(c.tool); got != c.want {
			t.Errorf("Class(%q) = %q, want %q", c.tool, got, c.want)
		}
	}
}
