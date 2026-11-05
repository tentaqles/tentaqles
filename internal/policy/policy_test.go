package policy

import (
	"strings"
	"testing"
)

func builtinSet(t *testing.T) *Set {
	t.Helper()
	s, errs := Build(Layers{Builtin: Builtin()})
	if len(errs) > 0 {
		t.Fatalf("builtin rules do not compile: %v", errs)
	}
	return s
}

// builtinDecisionCases is the decision corpus of the five secret rules.
var builtinDecisionCases = []struct {
	name string
	call ToolCall
	want Action
	rule string // expected matched rule id ("" for allow)
}{
	// secret stores
	{"read ssh key windows path", ToolCall{Tool: "Read", Path: `C:\Users\renato\.ssh\id_ed25519`}, Deny, "tq/secret-store-read"},
	{"read ssh pub ok", ToolCall{Tool: "Read", Path: `C:\Users\renato\.ssh\id_ed25519.pub`}, Allow, ""},
	{"read tq catalog", ToolCall{Tool: "Read", Path: `C:/Users/renato/.tentaqles/bundles/catalog.yaml`}, Deny, "tq/secret-store-read"},
	{"cat aws creds", ToolCall{Tool: "Bash", Command: "cat ~/.aws/credentials"}, Deny, "tq/secret-store-shell"},
	{"powershell gc ssh key", ToolCall{Tool: "PowerShell", Command: `Get-Content $HOME\.ssh\id_rsa`}, Deny, "tq/secret-store-shell"},
	// .env
	{"read .env windows", ToolCall{Tool: "Read", Path: `C:\repos\x\.env`}, Deny, "tq/env-file-read"},
	{"read .env.local", ToolCall{Tool: "Read", Path: "/r/x/.env.local"}, Deny, "tq/env-file-read"},
	{"read .env.example ok", ToolCall{Tool: "Read", Path: `C:\repos\x\.env.example`}, Allow, ""},
	{"read envrc ok", ToolCall{Tool: "Read", Path: "/r/x/.envrc"}, Allow, ""},
	{"cat .env", ToolCall{Tool: "Bash", Command: "cd app && cat .env"}, Deny, "tq/env-file-shell"},
	{"cat .env.example ok", ToolCall{Tool: "Bash", Command: "cat .env.example"}, Allow, ""},
	{"cat example then real", ToolCall{Tool: "Bash", Command: "cat .env.example .env"}, Deny, "tq/env-file-shell"},
	{"grep process.env ok", ToolCall{Tool: "Bash", Command: `grep -rn "process.env" src`}, Allow, ""},
	{"cp example to env ok", ToolCall{Tool: "Bash", Command: "cp .env.example .env"}, Allow, ""},
	{"powershell gc .env", ToolCall{Tool: "PowerShell", Command: `Get-Content .\.env`}, Deny, "tq/env-file-shell"},
	// env dumps
	{"printenv", ToolCall{Tool: "Bash", Command: "printenv"}, Deny, "tq/env-dump"},
	{"env piped", ToolCall{Tool: "Bash", Command: "env | sort"}, Deny, "tq/env-dump"},
	{"env with command ok", ToolCall{Tool: "Bash", Command: "env FOO=1 node x.js"}, Allow, ""},
	{"printenv one var ok", ToolCall{Tool: "Bash", Command: "printenv PATH"}, Allow, ""},
	{"ps env drive", ToolCall{Tool: "PowerShell", Command: "Get-ChildItem Env:"}, Deny, "tq/env-dump"},
	// what the secret rules do not touch: git history, cloud, infra, settings, MCP
	{"force push main ok", ToolCall{Tool: "Bash", Command: "git push --force origin main"}, Allow, ""},
	{"repo delete ok", ToolCall{Tool: "Bash", Command: "gh repo delete org/x --yes"}, Allow, ""},
	{"az delete ok", ToolCall{Tool: "Bash", Command: "az postgres flexible-server delete -g rg-prod -n db"}, Allow, ""},
	{"terraform apply ok", ToolCall{Tool: "Bash", Command: "terraform apply -auto-approve"}, Allow, ""},
	{"settings bypass ok", ToolCall{Tool: "Write", Path: `C:\r\x\.claude\settings.local.json`, Content: `{"permissions": {"defaultMode": "bypassPermissions"}}`}, Allow, ""},
	{"sudo ok", ToolCall{Tool: "Bash", Command: "sudo apt install x"}, Allow, ""},
	{"mcp postgres write ok", ToolCall{Tool: "mcp__postgres__query", Content: `{"sql":"DELETE FROM orders WHERE id=1"}`}, Allow, ""},
	// harmless
	{"portuguese edit ok", ToolCall{Tool: "Edit", Path: `C:\r\docs\leia-me.md`, Content: "Configuração não é necessária 🚀"}, Allow, ""},
	{"ls ok", ToolCall{Tool: "Bash", Command: "ls -la"}, Allow, ""},
}

func TestBuiltinDecisions(t *testing.T) {
	s := builtinSet(t)
	for _, c := range builtinDecisionCases {
		d := s.Evaluate(c.call)
		if d.Action != c.want {
			t.Errorf("%s: action = %q, want %q (matched %v)", c.name, d.Action, c.want, ids(d))
			continue
		}
		if c.rule != "" && !contains(ids(d), c.rule) {
			t.Errorf("%s: matched %v, want %s among them", c.name, ids(d), c.rule)
		}
	}
}

// Builtin holds exactly the five secret rules, every one a deny.
func TestBuiltinIsTheFiveSecretRules(t *testing.T) {
	want := []string{"tq/secret-store-read", "tq/secret-store-shell", "tq/env-file-read", "tq/env-file-shell", "tq/env-dump"}
	var got []string
	for _, r := range Builtin() {
		got = append(got, r.ID)
		if r.Action != Deny {
			t.Errorf("%s: action %q, want deny", r.ID, r.Action)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("builtin ids = %v, want %v", got, want)
	}
}

func TestBuildSkipsBadRuleButKeepsOthers(t *testing.T) {
	rules := append(Builtin(),
		Rule{ID: "bad", Action: Deny, Command: "("},
		Rule{ID: "noaction", Command: "x"},
		Rule{ID: "empty", Action: Deny},
	)
	s, errs := Build(Layers{Builtin: rules})
	if len(errs) != 3 {
		t.Fatalf("errs = %v", errs)
	}
	if d := s.Evaluate(ToolCall{Tool: "Bash", Command: "printenv"}); d.Action != Deny {
		t.Fatal("builtins lost after a bad rule")
	}
}

func TestReasonAndMerge(t *testing.T) {
	s := builtinSet(t)
	d := s.Evaluate(ToolCall{Tool: "Bash", Command: "printenv"})
	if !strings.HasPrefix(d.Reason(), "BLOCKED [secrets/tq/env-dump]") {
		t.Errorf("reason = %q", d.Reason())
	}
	ask := Decision{Action: Ask, Matched: []*Rule{{ID: "tq/commit-secret", Action: Ask, Reason: "scan", Source: "builtin"}}}
	if !strings.HasPrefix(ask.Reason(), "CONFIRM [tq/commit-secret]") {
		t.Errorf("ask reason = %q", ask.Reason())
	}
	if m := Merge(ask, d); m.Action != Deny {
		t.Errorf("merge kept %q", m.Action)
	}
	if m := Merge(Decision{}, ask); m.Action != Ask {
		t.Errorf("merge lost ask")
	}
}

func TestMCPServer(t *testing.T) {
	if got := (ToolCall{Tool: "mcp__hostinger-mcp__execute"}).MCPServer(); got != "hostinger-mcp" {
		t.Errorf("got %q", got)
	}
	if got := (ToolCall{Tool: "Read"}).MCPServer(); got != "" {
		t.Errorf("got %q", got)
	}
}

func ids(d Decision) []string {
	var out []string
	for _, r := range d.Matched {
		out = append(out, r.ID)
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
