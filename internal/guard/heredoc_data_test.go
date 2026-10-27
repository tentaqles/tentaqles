package guard

import (
	"strings"
	"testing"
)

const guardFile = "~/.tentaqles/guard.yaml"

// BlankDataHeredocs blanks the bodies only when the whole command is one of
// the known data shapes and every segment is inert; anything else is
// scanned as it is.
func TestBlankDataHeredocs(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		blanked bool // the body text "BODY" is gone
	}{
		// the data shapes
		{"cat to file", "cat > notes.md <<'EOF'\nBODY\nEOF", true},
		{"cat marker first", "cat <<EOF > notes.md\nBODY\nEOF", true},
		{"cat to stdout", "cat <<EOF\nBODY\nEOF", true},
		{"tee", "tee notes.md <<EOF\nBODY\nEOF", true},
		{"tee -a to dev null", "tee -a notes.md >/dev/null <<EOF\nBODY\nEOF", true},
		{"git commit -F -", "git commit -F - <<'EOF'\nBODY\nEOF", true},
		{"git commit --file=-", "git commit --file=- <<'EOF'\nBODY\nEOF", true},
		{"git -C dir tag -F-", "git -C repo tag -a v1 -F- <<'EOF'\nBODY\nEOF", true},
		{"git notes -F -", "git notes add -F - <<EOF\nBODY\nEOF", true},
		{"git commit -m substitution", "git commit -m \"$(cat <<'EOF'\nBODY\nEOF\n)\"", true},
		{"git tag -m substitution", "git tag -a v1 -m \"$(cat <<'EOF'\nBODY\nEOF\n)\"", true},
		{"gh pr body substitution", "gh pr create --title t --body \"$(cat <<'EOF'\nBODY\nEOF\n)\"", true},
		{"gh issue comment body", "gh issue comment 3 --body \"$(cat <<'EOF'\nBODY\nEOF\n)\"", true},
		{"add then commit substitution", "git add a.go && git commit -m \"$(cat <<'EOF'\nBODY\nEOF\n)\" && git status", true},
		{"dash tabs", "cat <<-EOF > x\n\tBODY\n\tEOF", true},
		{"crlf", "cat > x <<EOF\r\nBODY\r\nEOF\r\n", true},
		{"after cd and mkdir", "cd docs && mkdir -p sub && cat > sub/x.md <<'EOF'\nBODY\nEOF", true},
		{"two data heredocs", "cat > a <<A\nBODY\nA\ncat > b <<B\nBODY\nB", true},
		{"comment line", "# write the note\ncat > a <<A\nBODY\nA", true},
		{"stderr redirect", "cat > a <<A 2>&1\nBODY\nA", true},

		// shells, interpreters, runners, wrappers
		{"bash", "bash <<'EOF'\nBODY\nEOF", false},
		{"sh -s", "sh -s <<EOF\nBODY\nEOF", false},
		{"python -", "python3 - <<EOF\nBODY\nEOF", false},
		{"source stdin", "source /dev/stdin <<EOF\nBODY\nEOF", false},
		{"dot stdin", ". /dev/stdin <<EOF\nBODY\nEOF", false},
		{"env -i bash", "env -i bash <<EOF\nBODY\nEOF", false},
		{"marker before program", "<<EOF bash\nBODY\nEOF", false},
		{"bare redirect", "> notes.md <<EOF\nBODY\nEOF", false},
		{"variable program", "$SHELL <<EOF\nBODY\nEOF", false},
		{"unknown program", "mytool <<EOF\nBODY\nEOF", false},
		{"sudo tee", "sudo tee /etc/x <<EOF\nBODY\nEOF", false},
		{"assignment prefix", "GIT_EDITOR=x git commit -F - <<EOF\nBODY\nEOF", false},

		// pipes: any pipe scans
		{"cat piped to bash", "cat <<EOF | bash\nBODY\nEOF", false},
		{"cat piped to busybox sh", "cat <<EOF | busybox sh\nBODY\nEOF", false},
		{"cat piped to ash", "cat <<EOF | ash\nBODY\nEOF", false},
		{"cat piped to at", "cat <<EOF | at now\nBODY\nEOF", false},
		{"cat piped to sudo -u root bash", "cat <<EOF | sudo -u root bash\nBODY\nEOF", false},
		{"cat piped to xargs", "cat <<EOF | xargs -0 -I{} sh -c {}\nBODY\nEOF", false},
		{"cat piped to tee", "cat <<EOF | tee x\nBODY\nEOF", false},

		// substitutions other than the "$(cat <<WORD)" argument shapes
		{"substitution piped to bash", "echo \"$(cat <<'EOF'\nBODY\nEOF\n)\" | bash", false},
		{"substitution in command position", "$(cat <<'EOF'\nBODY\nEOF\n)", false},
		{"quoted substitution in command position", "\"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"eval substitution", "eval \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"sh -c substitution", "sh -c \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"find -exec sh -c substitution", "find . -exec sh -c \"$(cat <<'EOF'\nBODY\nEOF\n)\" \\;", false},
		{"su -c substitution", "su -c \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"wsl sh -c substitution", "wsl sh -c \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"echo substitution", "echo \"$(cat <<'EOF'\nBODY\nEOF\n)\" > notes.md", false},
		{"assignment", "S=$(cat <<'EOF'\nBODY\nEOF\n)\neval \"$S\"", false},
		{"process substitution", "bash <(cat <<'EOF'\nBODY\nEOF\n)", false},
		{"backtick", "`cat <<EOF\nBODY\nEOF\n`", false},
		{"other substitution in the message", "git commit -m \"$(cat <<'EOF'\nBODY\nEOF\n)$(id)\"", false},
		{"substitution with more than cat", "git commit -m \"$(cat <<'EOF' | sh\nBODY\nEOF\n)\"", false},
		{"git notes -m substitution", "git notes add -m \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},
		{"gh view body", "gh pr view --body \"$(cat <<'EOF'\nBODY\nEOF\n)\"", false},

		// later commands that may run what was written
		{"written then run", "cat > x.sh <<'EOF'\nBODY\nEOF\nbash x.sh", false},
		{"written then sourced", "cat > x.sh <<'EOF'\nBODY\nEOF\n. ./x.sh", false},
		{"written then ./run", "cat > x.sh <<'EOF'\nBODY\nEOF\nchmod +x x.sh && ./x.sh", false},
		{"written then make", "cat > mk <<'EOF'\nBODY\nEOF\nmake -f mk", false},
		{"written then find -exec", "cat > x.txt <<'EOF'\nBODY\nEOF\nfind x.txt -exec sh {} \\;", false},
		{"tee then sh", "tee x <<EOF\nBODY\nEOF\nsh x", false},

		// git shapes that are not data
		{"git alias via -c", "git -c alias.c='!sh' c commit <<EOF\nBODY\nEOF", false},
		{"git commit without -F -", "git commit <<EOF\nBODY\nEOF", false},
		{"git commit -F file", "git commit -F msg.txt <<EOF\nBODY\nEOF", false},
		{"git apply", "git apply <<EOF\nBODY\nEOF", false},

		// heredocs tq does not read the way bash does
		{"unterminated", "cat > x <<EOF\nBODY", false},
		{"odd delimiter", "cat > n.md <<E\"O\"F\nBODY\nEOF", false},
		{"backslash delimiter", "cat > n.md <<\\EOF\nBODY\nEOF", false},
		{"continued cat line", "cat \\\n  > x <<EOF\nBODY\nEOF", true},
		{"continued bash line", "bash \\\n  -s <<EOF\nBODY\nEOF", false},
		{"continued pipe", "cat <<EOF \\\n | bash\nBODY\nEOF", false},
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
			}()
		}
	}
}
