package guard

import (
	"strings"
	"testing"
)

const guardFile = "~/.tentaqles/guard.yaml"

// BlankDataHeredocs blanks a body only when it is data: fed to cat, tee or
// git commit -F, or a bare redirect. A body any program could run stays.
func TestBlankDataHeredocs(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		blanked bool // the body text "BODY" is gone
	}{
		{"cat to file", "cat > notes.md <<'EOF'\nBODY\nEOF", true},
		{"cat marker first", "cat <<EOF > notes.md\nBODY\nEOF", true},
		{"tee", "tee notes.md <<EOF\nBODY\nEOF", true},
		{"tee -a", "tee -a notes.md >/dev/null <<EOF\nBODY\nEOF", true},
		{"git commit -F -", "git commit -F - <<'EOF'\nBODY\nEOF", true},
		{"git commit -m substitution", "git commit -m \"$(cat <<'EOF'\nBODY\nEOF\n)\"", true},
		{"gh body substitution", "gh pr create --title t --body \"$(cat <<'EOF'\nBODY\nEOF\n)\"", true},
		{"bare redirect", "> notes.md <<EOF\nBODY\nEOF", true},
		{"dash tabs", "cat <<-EOF > x\n\tBODY\n\tEOF", true},
		{"crlf", "cat > x <<EOF\r\nBODY\r\nEOF\r\n", true},
		{"after cd", "cd docs && cat > x.md <<'EOF'\nBODY\nEOF", true},
		{"two data heredocs", "cat > a <<A\nBODY\nA\ncat > b <<B\nBODY\nB", true},

		{"bash", "bash <<'EOF'\nBODY\nEOF", false},
		{"sh -s", "sh -s <<EOF\nBODY\nEOF", false},
		{"python -", "python3 - <<EOF\nBODY\nEOF", false},
		{"node", "node <<EOF\nBODY\nEOF", false},
		{"pwsh", "pwsh -NoProfile -Command - <<EOF\nBODY\nEOF", false},
		{"perl", "perl <<EOF\nBODY\nEOF", false},
		{"ruby", "ruby <<EOF\nBODY\nEOF", false},
		{"marker before program", "<<EOF bash\nBODY\nEOF", false},
		{"cat piped to bash", "cat <<EOF | bash\nBODY\nEOF", false},
		{"cat piped to sudo sh", "cat <<EOF | sudo -E sh\nBODY\nEOF", false},
		{"substitution piped to bash", "echo \"$(cat <<'EOF'\nBODY\nEOF\n)\" | bash", false},
		{"substitution in command position", "$(cat <<'EOF'\nBODY\nEOF\n)", false},
		{"quoted substitution in command position", "\"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"eval substitution", "eval \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"bash -c substitution", "bash -c \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"assignment", "S=$(cat <<'EOF'\nBODY\nEOF\n)\neval \"$S\"", false},
		{"process substitution", "bash <(cat <<'EOF'\nBODY\nEOF\n)", false},
		{"backtick", "`cat <<EOF\nBODY\nEOF\n`", false},
		{"written then run", "cat > x.sh <<'EOF'\nBODY\nEOF\nbash x.sh", false},
		{"written then sourced", "cat > x.sh <<'EOF'\nBODY\nEOF\n. ./x.sh", false},
		{"variable program", "$SHELL <<EOF\nBODY\nEOF", false},
		{"unknown program", "mytool <<EOF\nBODY\nEOF", false},
		{"sudo tee", "sudo tee /etc/x <<EOF\nBODY\nEOF", false},
		{"unterminated", "cat > x <<EOF\nBODY", false},
		{"continued line", "cat \\\n  > x <<EOF\nBODY\nEOF", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := BlankDataHeredocs(tc.in)
			if len(out) != len(tc.in) {
				t.Fatalf("length changed: %q", out)
			}
			if got := !strings.Contains(out, "BODY"); got != tc.blanked {
				t.Errorf("blanked=%v, want %v: %q", got, tc.blanked, out)
			}
		})
	}
	// The command line itself is never blanked: the redirect target stays.
	in := "cat > " + guardFile + " <<'EOF'\ndisable: [x]\nEOF"
	if out := BlankDataHeredocs(in); !strings.Contains(out, guardFile) {
		t.Errorf("redirect target blanked: %q", out)
	}
}

// MaskQuotedArgs hides separators inside quoted arguments, so `set` in a
// message is not a segment, but keeps code an interpreter runs.
func TestMaskQuotedArgs(t *testing.T) {
	cases := []struct {
		in, want string // want: a substring that must survive
		gone     string // a substring that must not survive
	}{
		{`git commit -m "fix (set) thing"`, "", "(set)"},
		{"git commit -m 'a; set'", "", "; set"},
		{"git commit -m \"line\nset\nmore\"", "", "\nset\n"},
		{`echo "$(printenv)"`, "$(printenv)", ""},
		{`bash -c "ls; set"`, "ls; set", ""},
		{`pwsh -Command "dir; Get-ChildItem env:"`, "dir; Get-ChildItem env:", ""},
		{`eval 'a; set'`, "a; set", ""},
		{`echo "unterminated; set`, "", "; set"},
		{`echo a\"; set`, "; set", ""},
	}
	for _, tc := range cases {
		out := MaskQuotedArgs(tc.in)
		if len(out) != len(tc.in) {
			t.Errorf("%q: length changed: %q", tc.in, out)
		}
		if tc.want != "" && !strings.Contains(out, tc.want) {
			t.Errorf("%q: %q was masked: %q", tc.in, tc.want, out)
		}
		if tc.gone != "" && strings.Contains(out, tc.gone) {
			t.Errorf("%q: %q survived: %q", tc.in, tc.gone, out)
		}
	}
}

func TestDataViewPrefixesNeverPanic(t *testing.T) {
	for _, tc := range heredocCases {
		for i := 0; i <= len(tc.in); i++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s[:%d]: panic: %v", tc.name, i, r)
					}
				}()
				_ = BlankDataHeredocs(tc.in[:i])
				_ = MaskQuotedArgs(tc.in[:i])
			}()
		}
	}
}
