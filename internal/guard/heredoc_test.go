package guard

import (
	"strings"
	"testing"
)

// heredocCases are the shapes that used to break the heredoc scanner: two
// heredocs on one line, <<- with tabs, quoted markers, an unterminated body,
// a body that mentions another marker, a body inside $( ), CRLF endings.
var heredocCases = []struct {
	name string
	in   string
	// kept must survive blanking; gone must not.
	kept, gone []string
}{
	{"two on one line", "cat <<A <<B\nalpha\nA\nbeta\nB\necho after", []string{"cat <<A <<B", "echo after"}, []string{"alpha", "beta"}},
	{"two on one line, long first line", "cat <<A <<B >/tmp/some/long/path/x.txt\na\nA\nb\nB", []string{"cat <<A <<B"}, []string{"\na\n", "\nb\n"}},
	{"two on one line, short closing", "x <<LONGWORDONE <<LONGWORDTWO\nLONGWORDONE\nLONGWORDTWO", []string{"LONGWORDTWO"}, nil},
	{"dash with tabs", "cat <<-EOF\n\tbody line\n\tEOF\necho after", []string{"echo after"}, []string{"body line"}},
	{"single-quoted marker", "cat <<'EOF'\n$(secret)\nEOF", nil, []string{"$(secret)"}},
	{"double-quoted marker", "cat <<\"EOF\"\nbody\nEOF", nil, []string{"body"}},
	{"unterminated", "cat <<EOF\nnever closed\nstill open", []string{"never closed", "still open"}, nil},
	{"unterminated second", "cat <<A <<B\na\nA\nb", []string{"\nb"}, []string{"\na\n"}},
	{"body mentions marker", "cat <<OUT\nuse <<EOF here\nand <<A <<B too\nOUT\necho after", []string{"echo after"}, []string{"use <<EOF", "and <<A"}},
	{"nested substitution", "git commit -m \"$(cat <<'EOF'\nfix: thing\nEOF\n)\"\necho after", []string{"git commit -m", "echo after"}, []string{"fix: thing"}},
	{"crlf", "cat <<EOF\r\nbody\r\nEOF\r\necho after\r\n", []string{"echo after"}, []string{"body"}},
	{"here-string is not a heredoc", "cat <<<word\nkeep me\nword", []string{"keep me"}, nil},
	{"marker at end of input", "cat <<", []string{"cat <<"}, nil},
	{"empty input", "", nil, nil},
	// a `<<` inside quotes or a comment opens nothing: the next lines run
	{"single-quoted <<", "git commit -m 'note <<X'\nprintenv\nX", []string{"printenv"}, nil},
	{"double-quoted <<", "echo \"a <<X\"\nprintenv\nX", []string{"printenv"}, nil},
	{"quoted << as an argument", "cat '<<X' > n.md\nprintenv\nX", []string{"printenv"}, nil},
	{"commented <<", "ls # <<X\nprintenv\nX", []string{"printenv"}, nil},
	{"multi-line quote holds <<", "git commit -m \"a\nb <<X\"\nprintenv\nX", []string{"printenv"}, nil},
	{"apostrophe in a comment ends with the line", "ls # it's\ncat <<EOF\nbody\nEOF", []string{"ls # it's"}, []string{"body"}},
	{"$# is not a comment", "echo $# <<EOF\nbody\nEOF", nil, []string{"body"}},
	// the delimiter is the word with its quotes removed, as in bash
	{"quote inside the delimiter", "cat > n.md <<E\"O\"F\nbody\nE\nprintenv\nEOF\necho after", []string{"echo after"}, []string{"body", "printenv"}},
	{"backslash delimiter", "cat <<\\EOF\nbody\nEOF", nil, []string{"body"}},
	// the body starts after the logical line
	{"continued opener line", "cat <<EOF \\\n | bash\nbody\nEOF", []string{"| bash"}, []string{"body"}},
	{"continuation at end", "cat <<EOF \\", []string{"cat <<EOF"}, nil},
}

// scanHeredocs flags a delimiter that is not a plain word or one quote pair
// around it.
func TestScanHeredocsOddDelimiter(t *testing.T) {
	for in, odd := range map[string]bool{
		"cat <<EOF\nx\nEOF":       false,
		"cat <<'EOF'\nx\nEOF":     false,
		"cat <<\"EOF\"\nx\nEOF":   false,
		"cat <<-EOF\nx\nEOF":      false,
		"cat <<E\"O\"F\nx\nEOF":   true,
		"cat <<\\EOF\nx\nEOF":     true,
		"cat <<'E'OF\nx\nEOF":     true,
		"cat <<\"E'OF\"\nx\nE'OF": true,
	} {
		hs := scanHeredocs(strings.Split(in, "\n"))
		if len(hs) != 1 || !hs[0].closed || hs[0].odd != odd {
			t.Errorf("%q: %+v, want one closed heredoc with odd=%v", in, hs, odd)
		}
	}
}

func TestBlankHeredocBodiesNeverPanics(t *testing.T) {
	for _, tc := range heredocCases {
		t.Run(tc.name, func(t *testing.T) {
			var out string
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic: %v", r)
					}
				}()
				out = blankHeredocBodies(tc.in)
			}()
			if len(out) != len(tc.in) {
				t.Errorf("length changed: %d -> %d", len(tc.in), len(out))
			}
			if strings.Count(out, "\n") != strings.Count(tc.in, "\n") {
				t.Errorf("line breaks changed: %q", out)
			}
			for _, k := range tc.kept {
				if !strings.Contains(out, k) {
					t.Errorf("%q was blanked: %q", k, out)
				}
			}
			for _, g := range tc.gone {
				if strings.Contains(out, g) {
					t.Errorf("%q survived: %q", g, out)
				}
			}
		})
	}
}

// Every prefix of every case: a scanner that indexes the wrong line panics on
// some truncation of these inputs.
func TestBlankHeredocBodiesPrefixesNeverPanic(t *testing.T) {
	for _, tc := range heredocCases {
		for i := 0; i <= len(tc.in); i++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s[:%d]: panic: %v", tc.name, i, r)
					}
				}()
				_ = blankHeredocBodies(tc.in[:i])
				_ = StripHeredocs(tc.in[:i])
			}()
		}
	}
}
