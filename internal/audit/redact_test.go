package audit

import (
	"strings"
	"testing"
)

// The denial log records a redacted command (ADR-0006): the shape stays,
// the values that can be secrets do not.
func TestRedactCommand(t *testing.T) {
	cases := []struct {
		name, in   string
		keep, drop []string
	}{
		{"github token", "aws s3 ls --profile x ghp_abcdefghijklmnopqrstuvwxyz0123456789", // tq:allow-secret (fake fixture)
			[]string{"aws s3 ls", "--profile"}, []string{"ghp_abcdefghijklmnopqrstuvwxyz0123456789"}}, // tq:allow-secret (fake fixture)
		{"bearer header quoted", `curl -H "Authorization: Bearer abcDEF1234567890xyzXYZ" https://api.example.com/v1`, // tq:allow-secret (fake fixture)
			[]string{"curl", "-H", "https://api.example.com/v1"}, []string{"abcDEF1234567890xyzXYZ", "Authorization"}},
		{"bearer header unquoted value", `curl --header Authorization:abc123secretvalue https://x.test`,
			[]string{"curl", "--header"}, []string{"abc123secretvalue"}},
		{"assignment", "GH_TOKEN=s3cr3tvalue gh pr create",
			[]string{"GH_TOKEN=", "gh pr create"}, []string{"s3cr3tvalue"}},
		{"env wrapper assignment", "env AWS_SECRET_ACCESS_KEY=abc/def+ghi aws s3 ls",
			[]string{"env AWS_SECRET_ACCESS_KEY=", "aws s3 ls"}, []string{"abc/def+ghi"}},
		{"powershell assignment", `$env:API_KEY = hunter2hunter; az login`,
			[]string{"$env:API_KEY", "az login"}, []string{"hunter2hunter"}},
		{"password flag", "mysql --password=Tr0ub4dor -h db",
			[]string{"mysql", "--password=", "-h db"}, []string{"Tr0ub4dor"}},
		{"password flag spaced", "psql --pass Tr0ub4dor -h db",
			[]string{"psql", "--pass"}, []string{"Tr0ub4dor"}},
		{"curl user password", "curl -u admin:hunter2 https://x.test",
			[]string{"curl -u admin:"}, []string{"hunter2"}},
		{"git -u is not a credential", "git push -u origin main",
			[]string{"git push -u origin main"}, nil},
		{"connection string", "psql postgres://admin:hunter2@db.internal/app", // tq:allow-secret (fake fixture)
			[]string{"psql"}, []string{"hunter2"}},
		{"heredoc body", "cat > notes.txt <<'EOF'\nthe password is hunter2\nEOF\ngit push",
			[]string{"cat > notes.txt <<", "<heredoc body dropped>", "git push"}, []string{"hunter2"}},
		{"heredoc dash", "cat <<-END\n\tsecret line\n\tEND\necho done",
			[]string{"<heredoc body dropped>", "echo done"}, []string{"secret line"}},
		{"two heredocs", "cat <<A <<B\nsecret-a\nA\nsecret-b\nB\nls",
			[]string{"<heredoc body dropped>", "ls"}, []string{"secret-a", "secret-b"}},
		{"here-string", "cat <<< 'inline secret'\nls",
			[]string{"cat <<<", "ls"}, []string{"inline secret"}},
		{"powershell here-string", "$x = @'\nmy secret text\n'@\nSet-Content a.txt $x",
			[]string{"Set-Content a.txt"}, []string{"my secret text"}},
		{"quoted bodies", `git commit -m "token is abc" && echo 'and xyz'`,
			[]string{`git commit -m "…"`, `echo '…'`}, []string{"token is abc", "and xyz"}},
		{"long unknown token", "deploy --key-id=1 zq9X8w7V6u5T4s3R2q1P0o9N8m7L",
			[]string{"deploy"}, []string{"zq9X8w7V6u5T4s3R2q1P0o9N8m7L"}},
		{"plain path kept", "rm -rf ~/.tentaqles/config.yaml",
			[]string{"rm -rf ~/.tentaqles/config.yaml"}, nil},
	}
	for _, c := range cases {
		got := RedactCommand(c.in)
		for _, k := range c.keep {
			if !strings.Contains(got, k) {
				t.Errorf("%s: %q lost %q", c.name, got, k)
			}
		}
		for _, d := range c.drop {
			if strings.Contains(got, d) {
				t.Errorf("%s: %q still carries %q", c.name, got, d)
			}
		}
	}
}

func TestRedactCommandTruncates(t *testing.T) {
	got := RedactCommand(strings.Repeat("ls a ", 200))
	if n := len([]rune(got)); n > MaxCommandRunes+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("len %d, %q", n, got)
	}
}

// A denial written with a secret in any text field never lands with it.
func TestAppendDenialMasksEveryField(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	tok := "ghp_abcdefghijklmnopqrstuvwxyz0123456789" // tq:allow-secret (fake fixture)
	if err := AppendDenial(Denial{Category: "identity", Layer: LayerHook, Command: "x " + tok, Subject: tok, Reason: "because " + tok}); err != nil {
		t.Fatal(err)
	}
	ds, err := ReadDenials()
	if err != nil || len(ds) != 1 {
		t.Fatalf("%v %v", ds, err)
	}
	d := ds[0]
	for _, f := range []string{d.Command, d.Subject, d.Reason} {
		if strings.Contains(f, tok) {
			t.Errorf("field kept the token: %q", f)
		}
	}
}

// Lines of the Phase 1 schema (kind, workspace) still read.
func TestReadDenialsOldSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	if err := appendLine(DenialsPath(), map[string]any{"kind": "human-only", "command": "tq allow", "reason": "r", "workspace": "acme"}); err != nil {
		t.Fatal(err)
	}
	if err := appendLine(DenialsPath(), map[string]any{"kind": "secrets", "layer": "hook", "command": "printenv", "reason": "r", "workspace": "acme"}); err != nil {
		t.Fatal(err)
	}
	ds, err := ReadDenials()
	if err != nil || len(ds) != 2 {
		t.Fatalf("%v %v", ds, err)
	}
	if ds[0].Category != "self-protection" || ds[0].Layer != LayerHumanOnly || ds[0].Identity != "acme" {
		t.Errorf("old human-only line: %+v", ds[0])
	}
	if ds[1].Category != "secrets" || ds[1].Identity != "acme" {
		t.Errorf("old hook line: %+v", ds[1])
	}
}
