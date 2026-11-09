package shellwords

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

type shellCase struct {
	Command    string   `json:"command"`
	Words      []string `json:"words"`
	Statements []string `json:"statements"`
}

func sameStrings(a, b []string) bool {
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

func TestLobeShellwordsCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []shellCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 40 {
		t.Fatalf("got %d cases, want at least 40", len(cases))
	}
	for _, tc := range cases {
		if got := Split(tc.Command); !sameStrings(got, tc.Words) {
			t.Errorf("Split(%q) = %q, want %q", tc.Command, got, tc.Words)
		}
		if got := Statements(tc.Command); !sameStrings(got, tc.Statements) {
			t.Errorf("Statements(%q) = %q, want %q", tc.Command, got, tc.Statements)
		}
	}
}

func TestLobeShellwordsProgram(t *testing.T) {
	cases := []struct{ command, want string }{
		{"FOO=1 /usr/bin/tentaqles lobe status", "tentaqles"},
		{`C:\tools\TQ.EXE lobe`, "tq"},
		{"", ""},
		{"A=1", ""},
		{"  npm test", "npm"},
		{"./node_modules/.bin/jest", "jest"},
		{"tq.exe", "tq"},
		{"1abc=2 prog", "1abc=2"},
	}
	for _, tc := range cases {
		if got := Program(tc.command); got != tc.want {
			t.Errorf("Program(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestLobeShellwordsStripRedirects(t *testing.T) {
	cases := []struct{ command, want string }{
		{"tq lobe status 2>&1", "tq lobe status "},
		{"cd app && tq lobe status 2>&1", "cd app && tq lobe status "},
		{"tentaqles lobe graph find x > out.txt 2>/dev/null", "tentaqles lobe graph find x  "},
		{"tq lobe status &> all.log", "tq lobe status "},
		{"tq lobe status 2>&1 | tee log", "tq lobe status  | tee log"},
		{"echo 1 2>&1", "echo 1 "},
	}
	for _, tc := range cases {
		if got := StripRedirects(tc.command); got != tc.want {
			t.Errorf("StripRedirects(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestLobeShellwordsNoInit(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "init" {
					t.Errorf("%s: func init is not allowed", name)
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, s := range d.Specs {
					vs := s.(*ast.ValueSpec)
					for _, v := range vs.Values {
						ast.Inspect(v, func(n ast.Node) bool {
							if _, ok := n.(*ast.CallExpr); ok {
								t.Errorf("%s: package var %s is initialized by a call", name, vs.Names[0].Name)
							}
							return true
						})
					}
				}
			}
		}
	}
}
