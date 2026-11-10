package sense

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLobeSenseOkProjection is the projection of step 8 of the successes feature: one call per name group
// of the table (nine tools, the last one an MCP tool), plus the edge cases.
func TestLobeSenseOkProjection(t *testing.T) {
	long := strings.Repeat("é", 250)
	cases := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{"Bash", "Bash",
			`{"command":"go test ./...","description":"run the tests","timeout":120000}`,
			`{"command":"go test ./..."}`},
		{"PowerShell", "PowerShell",
			`{"command":"Get-ChildItem -Recurse","timeout":5000}`,
			`{"command":"Get-ChildItem -Recurse"}`},
		{"Edit", "Edit",
			`{"file_path":"/repo/a.go","old_string":"x","new_string":"y","replace_all":false}`,
			`{"file_path":"/repo/a.go"}`},
		{"Write", "Write",
			`{"file_path":"/repo/b.txt","content":"hello"}`,
			`{"file_path":"/repo/b.txt"}`},
		{"NotebookEdit", "NotebookEdit",
			`{"notebook_path":"/repo/n.ipynb","new_source":"print(1)","cell_id":"c1"}`,
			`{"notebook_path":"/repo/n.ipynb"}`},
		{"Read", "Read",
			`{"file_path":"/repo/r.go","offset":10,"limit":20}`,
			`{"file_path":"/repo/r.go"}`},
		{"Grep", "Grep",
			`{"pattern":"TODO","path":"/repo","glob":"*.go","output_mode":"content"}`,
			`{"path":"/repo","pattern":"TODO"}`},
		{"WebFetch", "WebFetch",
			`{"url":"https://example.com/a","prompt":"summarize"}`,
			`{"url":"https://example.com/a"}`},
		{"MCP", "mcp__db__query",
			`{"sql":"` + long + `","meta":{"a":1},"list":[1,2],"n":3,"ok":true,"nothing":null}`,
			`{"list":[],"meta":{},"n":3,"nothing":null,"ok":true,"sql":"` + strings.Repeat("é", 200) + `"}`},
		{"known tool without its fields", "Bash",
			`{"description":"x","timeout":5}`,
			`{"description":"x","timeout":5}`},
		{"JSON string input", "Bash",
			`"{\"command\":\"ls -la\"}"`,
			`{"command":"ls -la"}`},
		{"JSON string input, MCP tool", "mcp__db__query",
			`"{\"sql\":\"select 1\",\"limit\":5}"`,
			`{"limit":5,"sql":"select 1"}`},
		{"array input", "Bash", `[1,2]`, `{}`},
		{"string input", "Bash", `"not json"`, `{}`},
		{"number input", "Read", `42`, `{}`},
		{"null input", "Read", `null`, `{}`},
		{"empty input", "Read", ``, `{}`},
	}
	ok := 0
	for _, c := range cases {
		got := string(Project(c.tool, json.RawMessage(c.input)))
		if got != c.want {
			t.Errorf("%s: Project(%q) = %s, want %s", c.name, c.tool, got, c.want)
			continue
		}
		ok++
	}
	if ok != len(cases) {
		t.Fatalf("%d of %d projections matched", ok, len(cases))
	}
}
