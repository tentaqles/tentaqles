package shellwords

import (
	"encoding/json"
	"os"
	"testing"
)

func samePart(a, b []Part) bool {
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

func TestLobeShellwordsParts(t *testing.T) {
	cases := []struct {
		command string
		want    []Part
	}{
		{"npm test | tail -5 && echo done", []Part{{"", "npm test"}, {"|", "tail -5"}, {"&&", "echo done"}}},
		{`echo "a | tail"`, []Part{{"", `echo "a | tail"`}}},
		{"a;b\nc", []Part{{"", "a"}, {";", "b"}, {"\n", "c"}}},
		{"a\r\nb", []Part{{"", "a"}, {"\n", "b"}}},
		{"  | x", []Part{{"", "x"}}},
		{"a || b & c", []Part{{"", "a"}, {"||", "b"}, {"&", "c"}}},
		{"x $(y) z", []Part{{"", "x"}, {"$(", "y"}, {")", "z"}}},
		{"pytest || true\n", []Part{{"", "pytest"}, {"||", "true"}}},
		{"", nil},
	}
	for _, tc := range cases {
		if got := Parts(tc.command); !samePart(got, tc.want) {
			t.Errorf("Parts(%q) = %+v, want %+v", tc.command, got, tc.want)
		}
	}
}

// TestLobeShellwordsPartsMatchStatements checks that Parts splits where Statements does, on every
// case of the shared case file.
func TestLobeShellwordsPartsMatchStatements(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []shellCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		parts := Parts(tc.Command)
		var texts []string
		for _, p := range parts {
			texts = append(texts, p.Text)
		}
		if !sameStrings(texts, Statements(tc.Command)) {
			t.Errorf("Parts(%q) texts = %q, Statements = %q", tc.Command, texts, Statements(tc.Command))
		}
	}
}
