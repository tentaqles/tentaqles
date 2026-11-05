package guard

import (
	"strings"
	"testing"
)

func joinCommands(cs []Command) string {
	var parts []string
	for _, c := range cs {
		s := strings.Join(c.Words, " ")
		if c.Base != "" {
			s += " @" + c.Base
		}
		if c.BaseUnknown {
			s += " @?"
		}
		if c.Nested {
			s += " (nested)"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " | ")
}

// Commands is the hook's one tokenizer, exported for the self-protection
// rules: quote-aware segments, words without quotes, cd tracked, nested
// shells followed, data heredocs not read.
func TestCommands(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ls -la", "ls -la"},
		{`echo "a;b" && rm -rf x`, "echo a;b | rm -rf x"},
		{"cd ~/.tentaqles && rm config.yaml", "cd ~/.tentaqles | rm config.yaml @~/.tentaqles"},
		{"cd a; cd b; cat x", "cd a | cd b @a | cat x @a/b"},
		{"cd /abs; cd ../y; cat x", "cd /abs | cd ../y @/abs | cat x @/abs/../y"},
		{"cd - && cat x", "cd - | cat x @?"},
		{`bash -c "rm -rf ~/.tentaqles"`, "bash -c rm -rf ~/.tentaqles | rm -rf ~/.tentaqles (nested)"},
		{"cmd /c tq allow acme", "cmd /c tq allow acme | tq allow acme (nested)"},
		{"echo x > f.txt", "echo x > f.txt"},
		{"echo x >f.txt", "echo x >f.txt"},
		{"a | b; c", "a | b | c"},
		// a data heredoc is not read; one fed to a shell is (the closing
		// word is a harmless segment of its own)
		{"cat > notes.md <<'EOF'\nrm -rf ~/.tentaqles\nEOF", "cat > notes.md <<EOF | EOF"},
		{"bash <<'EOF'\nrm -rf x\nEOF", "bash <<EOF | rm -rf x | EOF"},
	}
	for _, c := range cases {
		if got := joinCommands(Commands(c.in)); got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestVerbAndBaseName(t *testing.T) {
	cases := []struct {
		words      []string
		verb, args string
		ok         bool
	}{
		{[]string{"rm", "-rf", "x"}, "rm", "-rf x", true},
		{[]string{"A=1", "sudo", "-u", "root", "tq", "allow", "x"}, "tq", "allow x", true},
		{[]string{"env", "X=1", "tq", "allow", "x"}, "tq", "allow x", true},
		{[]string{"nohup", "timeout", "5", "tq", "init"}, "tq", "init", true},
		{[]string{"xargs", "-n", "1", "rm"}, "rm", "", true},
		{[]string{`"C:\Program Files\tq\tq.exe"`, "allow", "x"}, `C:\Program Files\tq\tq.exe`, "allow x", true},
		{[]string{"A=1"}, "", "", false},
		{nil, "", "", false},
	}
	for _, c := range cases {
		verb, args, ok := Verb(c.words)
		if ok != c.ok || verb != c.verb || strings.Join(args, " ") != c.args {
			t.Errorf("%v: got %q %q %v, want %q %q %v", c.words, verb, args, ok, c.verb, c.args, c.ok)
		}
	}
	for w, want := range map[string]string{
		"tq": "tq", "tq.exe": "tq", `C:\tools\tq.exe`: "tq", "/usr/local/bin/tq": "tq", "./tq": "tq",
		`"tq"`: "tq", "Remove-Item": "remove-item", "python3": "python3",
	} {
		if got := BaseName(w); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", w, got, want)
		}
	}
}
