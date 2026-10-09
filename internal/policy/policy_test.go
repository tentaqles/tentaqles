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

func TestBuiltinDecisions(t *testing.T) {
	s := builtinSet(t)
	cases := []struct {
		name string
		call ToolCall
		want Action
		rule string // expected first matched rule id ("" for allow)
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
		// git
		{"force push main", ToolCall{Tool: "Bash", Command: "git push --force origin main"}, Deny, "tq/force-push-main"},
		{"force push main flag after", ToolCall{Tool: "Bash", Command: "git push origin main -f"}, Deny, "tq/force-push-main"},
		{"plus refspec main", ToolCall{Tool: "PowerShell", Command: "git push origin +main"}, Deny, "tq/force-push-main"},
		{"lease push main", ToolCall{Tool: "Bash", Command: "git push --force-with-lease origin master"}, Deny, "tq/force-push-main"},
		{"force push feature asks", ToolCall{Tool: "Bash", Command: "git push --force origin feat/x"}, Ask, "tq/force-push"},
		{"lease push feature ok", ToolCall{Tool: "Bash", Command: "git push --force-with-lease origin feat/x"}, Allow, ""},
		{"normal push main ok", ToolCall{Tool: "Bash", Command: "git push origin main"}, Allow, ""},
		{"admin merge", ToolCall{Tool: "Bash", Command: "gh pr merge 12 --squash --admin"}, Ask, "tq/gh-admin-merge"},
		{"normal merge ok", ToolCall{Tool: "Bash", Command: "gh pr merge 12 --squash"}, Allow, ""},
		{"repo delete", ToolCall{Tool: "Bash", Command: "gh repo delete org/x --yes"}, Deny, "tq/gh-repo-delete"},
		// cloud
		{"az delete", ToolCall{Tool: "Bash", Command: "az postgres flexible-server delete -g rg-prod -n db"}, Ask, "tq/cloud-delete"},
		{"aws s3 rm", ToolCall{Tool: "PowerShell", Command: "aws s3 rm s3://bucket/x --recursive"}, Ask, "tq/cloud-delete"},
		{"az list ok", ToolCall{Tool: "Bash", Command: "az group list -o table"}, Allow, ""},
		{"cdk destroy", ToolCall{Tool: "Bash", Command: "npx cdk destroy MyStack"}, Ask, "tq/iac-destroy"},
		{"reg add", ToolCall{Tool: "PowerShell", Command: `reg add HKCU\Software\X /v Y /d 1`}, Ask, "tq/registry-write"},
		// databases
		{"psql drop", ToolCall{Tool: "Bash", Command: `psql "$DB" -c "DROP TABLE users"`}, Ask, "tq/sql-destructive-shell"},
		{"psql select ok", ToolCall{Tool: "Bash", Command: `psql "$DB" -c "select * from users where updated_at > now()"`}, Allow, ""},
		{"edit applied migration", ToolCall{Tool: "Edit", Path: `C:\r\app\supabase\migrations\0001_init.sql`, Exists: true}, Ask, "tq/migration-edit"},
		{"new migration ok", ToolCall{Tool: "Write", Path: `C:\r\app\supabase\migrations\0002_add.sql`}, Allow, ""},
		{"mcp postgres write", ToolCall{Tool: "mcp__postgres__query", Content: `{"sql":"DELETE FROM orders WHERE id=1"}`}, Ask, "tq/mcp-sql-write"},
		{"mcp postgres read ok", ToolCall{Tool: "mcp__postgres__query", Content: `{"sql":"select count(*) from orders"}`}, Allow, ""},
		{"supabase apply migration", ToolCall{Tool: "mcp__plugin_supabase_supabase__apply_migration", Content: `{}`}, Ask, "tq/mcp-apply-migration"},
		// automation
		{"n8n publish", ToolCall{Tool: "mcp__n8n__publish_workflow", Content: `{"id":"1"}`}, Ask, "tq/mcp-n8n-write"},
		{"n8n prefixed tool", ToolCall{Tool: "mcp__n8n-mcp__n8n_create_workflow", Content: `{}`}, Ask, "tq/mcp-n8n-write"},
		{"n8n plugin server", ToolCall{Tool: "mcp__plugin_n8n_n8n__n8n_update_partial_workflow", Content: `{}`}, Ask, "tq/mcp-n8n-write"},
		{"n8n read stays allowed", ToolCall{Tool: "mcp__n8n-mcp__n8n_get_workflow", Content: `{}`}, Allow, ""},
		{"n8n get ok", ToolCall{Tool: "mcp__n8n__get_workflow_details", Content: `{"id":"1"}`}, Allow, ""},
		{"github merge", ToolCall{Tool: "mcp__github__merge_pull_request", Content: `{}`}, Ask, "tq/mcp-github-write"},
		{"github read ok", ToolCall{Tool: "mcp__github__get_pull_request", Content: `{}`}, Allow, ""},
		{"hostinger execute", ToolCall{Tool: "mcp__hostinger-mcp__execute", Content: `{}`}, Ask, "tq/mcp-hosting-exec"},
		// CI
		{"gha unpinned", ToolCall{Tool: "Write", Path: `C:\r\.github\workflows\ci.yml`, Content: "steps:\n  - uses: foo/bar@main\n"}, Ask, "tq/gha-unpinned"},
		{"gha tag ok", ToolCall{Tool: "Write", Path: `C:\r\.github\workflows\ci.yml`, Content: "steps:\n  - uses: actions/checkout@v4\n"}, Allow, ""},
		// harmless
		{"portuguese edit ok", ToolCall{Tool: "Edit", Path: `C:\r\docs\leia-me.md`, Content: "Configuração não é necessária 🚀"}, Allow, ""},
		{"ls ok", ToolCall{Tool: "Bash", Command: "ls -la"}, Allow, ""},
	}
	for _, c := range cases {
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

func TestLayers(t *testing.T) {
	proj := []Rule{{ID: "p/no-console", Action: Ask, Tool: "Edit|Write", Content: `console\.log`, Reason: "no console.log"}}
	man := []Rule{{ID: "m/no-prod", Action: Deny, Command: `prod-db`, Reason: "prod"}}

	s, errs := Build(Layers{Builtin: Builtin(), Manifest: man, Project: proj, Disable: []string{"tq/mcp-n8n-write"}})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if d := s.Evaluate(ToolCall{Tool: "mcp__n8n__publish_workflow"}); d.Action != Allow {
		t.Errorf("disabled builtin still fires: %v", ids(d))
	}
	if d := s.Evaluate(ToolCall{Tool: "Bash", Command: "psql prod-db"}); d.Action != Deny {
		t.Errorf("manifest rule missing: %v", d)
	}
	if d := s.Evaluate(ToolCall{Tool: "Write", Path: "a.js", Content: "console.log(1)"}); d.Action != Ask || d.Matched[0].Source != "project" {
		t.Errorf("project rule: %+v", d)
	}
}

func TestBuildSkipsBadRuleButKeepsOthers(t *testing.T) {
	s, errs := Build(Layers{Builtin: Builtin(), Project: []Rule{
		{ID: "bad", Action: Deny, Command: "("},
		{ID: "noaction", Command: "x"},
		{ID: "empty", Action: Deny},
	}})
	if len(errs) != 3 {
		t.Fatalf("errs = %v", errs)
	}
	if d := s.Evaluate(ToolCall{Tool: "Bash", Command: "printenv"}); d.Action != Deny {
		t.Fatal("builtins lost after a bad project rule")
	}
}

func TestReasonAndMerge(t *testing.T) {
	s := builtinSet(t)
	d := s.Evaluate(ToolCall{Tool: "Bash", Command: "printenv"})
	if !strings.HasPrefix(d.Reason(), "BLOCKED [tq/env-dump]") {
		t.Errorf("reason = %q", d.Reason())
	}
	ask := s.Evaluate(ToolCall{Tool: "Bash", Command: "gh pr merge 1 --admin"})
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
