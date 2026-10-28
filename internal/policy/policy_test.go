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
		// unattended mode changes
		{"unattended on asks", ToolCall{Tool: "Bash", Command: "tq unattended on"}, Ask, "tq/guard-change"},
		{"unattended off project asks", ToolCall{Tool: "Bash", Command: "tq unattended off --project demo-todo"}, Ask, "tq/guard-change"},
		{"unattended on exe asks", ToolCall{Tool: "PowerShell", Command: "tq.exe unattended on"}, Ask, "tq/guard-change"},
		{"unattended status ok", ToolCall{Tool: "Bash", Command: "tq unattended status"}, Allow, ""},
		{"unattended log ok", ToolCall{Tool: "Bash", Command: "tq unattended log"}, Allow, ""},
		{"unattended ws flag first asks", ToolCall{Tool: "Bash", Command: "tq unattended --ws acme on"}, Ask, "tq/guard-change"},
		{"unattended sandbox flag first asks", ToolCall{Tool: "Bash", Command: "tq unattended --codex-sandbox danger-full-access on"}, Ask, "tq/guard-change"},
		{"unattended help flag first asks", ToolCall{Tool: "Bash", Command: "tq unattended -h on"}, Ask, "tq/guard-change"},
		{"unattended ws equals flag first asks", ToolCall{Tool: "PowerShell", Command: "tq.exe unattended --ws=acme off"}, Ask, "tq/guard-change"},
		{"guard ws flag first asks", ToolCall{Tool: "Bash", Command: "tq guard --ws acme off tq/x"}, Ask, "tq/guard-change"},
		{"guard all flag first set asks", ToolCall{Tool: "Bash", Command: "tq guard --all set tq/x ask"}, Ask, "tq/guard-change"},
		{"unattended status ws ok", ToolCall{Tool: "Bash", Command: "tq unattended status --ws acme"}, Allow, ""},
		{"unattended ws status ok", ToolCall{Tool: "Bash", Command: "tq unattended --ws acme status"}, Allow, ""},
		{"unattended log ws ok", ToolCall{Tool: "Bash", Command: "tq unattended log --ws acme"}, Allow, ""},
		{"guard list ws ok", ToolCall{Tool: "Bash", Command: "tq guard list --ws acme"}, Allow, ""},
		{"guard ws list ok", ToolCall{Tool: "Bash", Command: "tq guard --ws acme list"}, Allow, ""},
		{"guard-change quoted project dq", ToolCall{Tool: "Bash", Command: `tq unattended --project "my dir" on`}, Ask, "tq/guard-change"},
		{"guard-change quoted project sq", ToolCall{Tool: "Bash", Command: `tq unattended --project 'my dir' on`}, Ask, "tq/guard-change"},
		{"guard-change escaped space value", ToolCall{Tool: "Bash", Command: `tq unattended --project=my\ dir on`}, Ask, "tq/guard-change"},
		{"guard-change subshell value", ToolCall{Tool: "Bash", Command: `tq unattended --ws "$(echo acme)" on`}, Ask, "tq/guard-change"},
		{"guard-change guard quoted value set", ToolCall{Tool: "Bash", Command: `tq guard --ws "a b" set tq/x ask`}, Ask, "tq/guard-change"},
		{"guard-change quoted verb dq", ToolCall{Tool: "Bash", Command: `tq unattended "on"`}, Ask, "tq/guard-change"},
		{"guard-change quoted verb sq after flag", ToolCall{Tool: "Bash", Command: `tq unattended --ws acme 'on'`}, Ask, "tq/guard-change"},
		{"guard-change quoted guard verb", ToolCall{Tool: "Bash", Command: `tq guard 'off' tq/x`}, Ask, "tq/guard-change"},
		{"guard-change quoted exe path", ToolCall{Tool: "PowerShell", Command: `& "C:\tools\tq.exe" unattended --ws acme on`}, Ask, "tq/guard-change"},
		{"guard-change quoted exe allow", ToolCall{Tool: "Bash", Command: `"/opt/bin/tq" allow acme`}, Ask, "tq/guard-change"},
		{"guard-change quoted ws status ok", ToolCall{Tool: "Bash", Command: `tq unattended status --ws "my ws"`}, Allow, ""},
		{"guard-change log since ok", ToolCall{Tool: "Bash", Command: `tq unattended log --since 24h`}, Allow, ""},
		{"guard-change guard list ws ok", ToolCall{Tool: "Bash", Command: `tq guard list --ws acme`}, Allow, ""},
		{"guard-change round2 0", ToolCall{Tool: "Bash", Command: `tq unattended --project .\ on`}, Ask, "tq/guard-change"},
		{"guard-change round2 1", ToolCall{Tool: "Bash", Command: `tq unattended --project C:\repos\x\ on`}, Ask, "tq/guard-change"},
		{"guard-change round2 2", ToolCall{Tool: "Bash", Command: `tq unattended --project \\nas\proj on`}, Ask, "tq/guard-change"},
		{"guard-change round2 3", ToolCall{Tool: "Bash", Command: `tq unattended --project \\nas\proj\ on`}, Ask, "tq/guard-change"},
		{"guard-change round2 4", ToolCall{Tool: "Bash", Command: `tq unattended --project \tmp\x on`}, Ask, "tq/guard-change"},
		{"guard-change round2 5", ToolCall{Tool: "Bash", Command: `tq unattended --ws 'a'"b" on`}, Ask, "tq/guard-change"},
		{"guard-change round2 6", ToolCall{Tool: "PowerShell", Command: "tq unattended --ws \"a`\"b\" on"}, Ask, "tq/guard-change"},
		{"guard-change round2 7", ToolCall{Tool: "Bash", Command: `tq unattended --project=C:\x\ on`}, Ask, "tq/guard-change"},
		{"guard-change round2 8", ToolCall{Tool: "PowerShell", Command: `& "tq" unattended on`}, Ask, "tq/guard-change"},
		{"guard-change round2 9", ToolCall{Tool: "PowerShell", Command: `& 'tq.exe' unattended on`}, Ask, "tq/guard-change"},
		{"guard-change round2 10", ToolCall{Tool: "Bash", Command: `"tq" allow acme`}, Ask, "tq/guard-change"},
		{"guard-change round2 11", ToolCall{Tool: "Bash", Command: `tq unattended --ws "$(echo "acme")" on`}, Ask, "tq/guard-change"},
		{"guard-change round2 12", ToolCall{Tool: "Bash", Command: `tq unattended --ws a"b c" on`}, Ask, "tq/guard-change"},
		{"guard-change round3 subshell", ToolCall{Tool: "Bash", Command: `$(tq unattended on)`}, Ask, "tq/guard-change"},
		{"guard-change round3 assigned subshell", ToolCall{Tool: "Bash", Command: `x=$(tq unattended on)`}, Ask, "tq/guard-change"},
		{"guard-change round3 paren group", ToolCall{Tool: "Bash", Command: `(tq unattended on)`}, Ask, "tq/guard-change"},
		{"guard-change round3 redirect file", ToolCall{Tool: "Bash", Command: `tq unattended on>out.txt`}, Ask, "tq/guard-change"},
		{"guard-change round3 redirect null", ToolCall{Tool: "PowerShell", Command: `tq unattended on>$null`}, Ask, "tq/guard-change"},
		{"guard-change round3 redirect in", ToolCall{Tool: "Bash", Command: `tq guard set<in.txt`}, Ask, "tq/guard-change"},
		{"guard-change round3 guard subshell", ToolCall{Tool: "Bash", Command: `$(tq guard off tq/x)`}, Ask, "tq/guard-change"},
		{"guard-change round3 backtick subst", ToolCall{Tool: "Bash", Command: "`tq unattended on`"}, Ask, "tq/guard-change"},
		{"guard-change round3 bash continuation", ToolCall{Tool: "Bash", Command: "tq unattended --ws acme \\\n on"}, Ask, "tq/guard-change"},
		{"guard-change round3 bash continuation crlf", ToolCall{Tool: "Bash", Command: "tq unattended --ws acme \\\r\n on"}, Ask, "tq/guard-change"},
		{"guard-change round3 ps continuation", ToolCall{Tool: "PowerShell", Command: "tq unattended `\n on"}, Ask, "tq/guard-change"},
		{"guard-change round3 status subshell ok", ToolCall{Tool: "Bash", Command: `$(tq unattended status)`}, Allow, ""},
		{"guard-change guard list bare ok", ToolCall{Tool: "Bash", Command: "tq guard list"}, Allow, ""},
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
		{"gha unpinned", ToolCall{Tool: "Write", Path: `C:\r\.github\workflows\ci.yml`, Content: "steps:\n  - uses: foo/bar@main\n"}, Deny, "tq/gha-branch-ref"},
		{"gha tag ok", ToolCall{Tool: "Write", Path: `C:\r\.github\workflows\ci.yml`, Content: "steps:\n  - uses: actions/checkout@v4\n"}, Allow, ""},
		// IaC apply and remote migrations
		{"terraform apply asks", ToolCall{Tool: "Bash", Command: "terraform apply -auto-approve"}, Ask, "tq/iac-apply"},
		{"pulumi up asks", ToolCall{Tool: "Bash", Command: "pulumi up --yes --stack prod"}, Ask, "tq/iac-apply"},
		{"az deployment asks", ToolCall{Tool: "Bash", Command: "az deployment group create -g rg -f main.bicep"}, Ask, "tq/iac-apply"},
		{"cfn deploy asks", ToolCall{Tool: "Bash", Command: "aws cloudformation deploy --stack-name x --template-file t.yml"}, Ask, "tq/iac-apply"},
		{"terraform plan ok", ToolCall{Tool: "Bash", Command: "terraform plan"}, Allow, ""},
		{"prisma migrate deploy asks", ToolCall{Tool: "Bash", Command: "npx prisma migrate deploy"}, Ask, "tq/remote-migration"},
		{"supabase db push linked asks", ToolCall{Tool: "Bash", Command: "supabase db push --linked"}, Ask, "tq/remote-migration"},
		{"alembic upgrade asks", ToolCall{Tool: "Bash", Command: "alembic upgrade head"}, Ask, "tq/remote-migration"},
		{"alembic upgrade localhost ok", ToolCall{Tool: "Bash", Command: "DATABASE_URL=postgres://u:p@localhost/db alembic upgrade head"}, Allow, ""}, // tq:allow-secret
		{"supabase db reset local ok", ToolCall{Tool: "Bash", Command: "supabase db reset --local"}, Allow, ""},
		// settings widening
		{"settings bash star denied", ToolCall{Tool: "Edit", Path: "/r/x/.claude/settings.json", Content: `"allow": ["Bash(*)"]`}, Deny, "tq/settings-widen"},
		{"settings bypass denied", ToolCall{Tool: "Write", Path: `C:\r\x\.claude\settings.local.json`, Content: `{"permissions": {"defaultMode": "bypassPermissions"}}`}, Deny, "tq/settings-widen"},
		{"settings disable hooks denied", ToolCall{Tool: "Edit", Path: "/r/x/.claude/settings.json", Content: `"disableAllHooks": true`}, Deny, "tq/settings-widen"},
		{"settings harmless edit ok", ToolCall{Tool: "Edit", Path: "/r/x/.claude/settings.json", Content: `"theme": "dark"`}, Allow, ""},
		{"settings shell redirect denied", ToolCall{Tool: "Bash", Command: `echo '{}' > .claude/settings.local.json`}, Deny, "tq/settings-shell-write"},
		{"settings powershell write denied", ToolCall{Tool: "PowerShell", Command: `Set-Content .claude\settings.json '{}'`}, Deny, "tq/settings-shell-write"},
		{"settings cat ok", ToolCall{Tool: "Bash", Command: "cat .claude/settings.json"}, Allow, ""},
		// actions pinned to branches
		{"uses main denied", ToolCall{Tool: "Edit", Path: "/r/.github/workflows/ci.yml", Content: "uses: actions/checkout@main"}, Deny, "tq/gha-branch-ref"},
		{"uses master denied", ToolCall{Tool: "Write", Path: "/r/.github/workflows/ci.yml", Content: "- uses: foo/bar@master"}, Deny, "tq/gha-branch-ref"},
		{"uses latest asks", ToolCall{Tool: "Edit", Path: "/r/.github/workflows/ci.yml", Content: "uses: foo/bar@latest"}, Ask, "tq/gha-unpinned"},
		{"uses sha ok", ToolCall{Tool: "Edit", Path: "/r/.github/workflows/ci.yml", Content: "uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2"}, Allow, ""},
		{"cp settings as source ok", ToolCall{Tool: "Bash", Command: "cp .claude/settings.json /tmp/backup.json"}, Allow, ""},
		{"cp onto settings denied", ToolCall{Tool: "Bash", Command: "cp /tmp/x.json .claude/settings.json"}, Deny, "tq/settings-shell-write"},
		{"copy-item settings as source ok", ToolCall{Tool: "PowerShell", Command: `Copy-Item .claude\settings.json backup.json`}, Allow, ""},
		{"terraform chdir apply asks", ToolCall{Tool: "Bash", Command: "terraform -chdir=infra apply"}, Ask, "tq/iac-apply"},
		{"kubectl namespace apply asks", ToolCall{Tool: "Bash", Command: "kubectl -n prod apply -f x.yaml"}, Ask, "tq/iac-apply"},
		{"kubectl context apply asks", ToolCall{Tool: "Bash", Command: "kubectl --context prod apply -f x.yaml"}, Ask, "tq/iac-apply"},
		{"helm namespace upgrade asks", ToolCall{Tool: "Bash", Command: "helm -n x upgrade rel chart"}, Ask, "tq/iac-apply"},
		{"pulumi stack up asks", ToolCall{Tool: "Bash", Command: "pulumi --stack prod up"}, Ask, "tq/iac-apply"},
		{"kubectl dry-run ok", ToolCall{Tool: "Bash", Command: "kubectl apply --dry-run=client -f x.yaml"}, Allow, ""},
		{"iac dry-run in comment still asks", ToolCall{Tool: "Bash", Command: "terraform apply -auto-approve # --dry-run"}, Ask, "tq/iac-apply"},
		{"iac dry-run flag with comment ok", ToolCall{Tool: "Bash", Command: "kubectl apply -f x.yaml --dry-run=client # note"}, Allow, ""},
		{"migration localhost in comment still asks", ToolCall{Tool: "Bash", Command: "alembic upgrade head # localhost"}, Ask, "tq/remote-migration"},
		{"copy-item onto settings with -Force denied", ToolCall{Tool: "PowerShell", Command: `Copy-Item x .claude\settings.json -Force`}, Deny, "tq/settings-shell-write"},
		{"cp onto settings with redirect denied", ToolCall{Tool: "Bash", Command: "cp x .claude/settings.json 2>&1"}, Deny, "tq/settings-shell-write"},
		{"cp onto settings with comment denied", ToolCall{Tool: "Bash", Command: "cp x .claude/settings.json # c"}, Deny, "tq/settings-shell-write"},
		{"cp settings as source with flag ok", ToolCall{Tool: "Bash", Command: "cp .claude/settings.json backup.json -f"}, Allow, ""},
		{"cp settings as source trailing -v ok", ToolCall{Tool: "Bash", Command: "cp .claude/settings.json /tmp/backup.json -v"}, Allow, ""},
		// tracker CLI writes outside the sync engine (spec B §12)
		{"az boards update asks", ToolCall{Tool: "Bash", Command: "az boards work-item update --id 123 --state Active"}, Ask, "tq/tracker-cli-write"},
		{"az boards create asks", ToolCall{Tool: "PowerShell", Command: `az boards work-item create --title "Export" --type Task --org https://dev.azure.com/acme`}, Ask, "tq/tracker-cli-write"},
		{"az boards delete asks", ToolCall{Tool: "Bash", Command: "az boards work-item delete --id 123 --yes"}, Ask, "tq/tracker-cli-write"},
		{"az flags before boards asks", ToolCall{Tool: "Bash", Command: "az --only-show-errors boards work-item update --id 1 --state Done"}, Ask, "tq/tracker-cli-write"},
		{"az boards in a chain asks", ToolCall{Tool: "Bash", Command: "cd x && az boards work-item update --id 1 --discussion done"}, Ask, "tq/tracker-cli-write"},
		{"gh issue edit asks", ToolCall{Tool: "Bash", Command: "gh issue edit 12 --add-label agent-ready"}, Ask, "tq/tracker-cli-write"},
		{"gh issue close asks", ToolCall{Tool: "Bash", Command: "gh issue close 12 --comment done"}, Ask, "tq/tracker-cli-write"},
		{"gh issue create asks", ToolCall{Tool: "PowerShell", Command: `gh issue create --title "Export" --body "CSV"`}, Ask, "tq/tracker-cli-write"},
		{"gh issue comment asks", ToolCall{Tool: "Bash", Command: "gh issue comment 12 --body 'approved'"}, Ask, "tq/tracker-cli-write"},
		{"gh repo flag issue comment asks", ToolCall{Tool: "Bash", Command: "gh -R acme/app issue comment 12 -b x"}, Ask, "tq/tracker-cli-write"},
		{"gh project item-edit asks", ToolCall{Tool: "Bash", Command: "gh project item-edit --id PVTI_1 --field-id F --single-select-option-id O"}, Ask, "tq/tracker-cli-write"},
		{"acli workitem edit asks", ToolCall{Tool: "Bash", Command: "acli jira workitem edit --key PLAT-1 --summary x"}, Ask, "tq/tracker-cli-write"},
		{"acli workitem transition asks", ToolCall{Tool: "PowerShell", Command: `acli jira workitem transition --key PLAT-1 --status "In Review"`}, Ask, "tq/tracker-cli-write"},
		{"acli workitem create asks", ToolCall{Tool: "Bash", Command: "acli jira workitem create --project PLAT --type Task --summary x"}, Ask, "tq/tracker-cli-write"},
		{"acli workitem comment asks", ToolCall{Tool: "Bash", Command: "acli jira workitem comment create --key PLAT-1 --body x"}, Ask, "tq/tracker-cli-write"},
		{"az boards show ok", ToolCall{Tool: "Bash", Command: "az boards work-item show --id 123 -o json"}, Allow, ""},
		{"az boards query ok", ToolCall{Tool: "Bash", Command: "az boards query --wiql 'select [System.Id] from WorkItems'"}, Allow, ""},
		{"gh issue view ok", ToolCall{Tool: "Bash", Command: "gh issue view 12 --json title,body"}, Allow, ""},
		{"gh issue list ok", ToolCall{Tool: "Bash", Command: "gh issue list --assignee @me"}, Allow, ""},
		{"gh pr create naming issue create ok", ToolCall{Tool: "Bash", Command: `gh pr create --title "fix issue create flow" --body x`}, Allow, ""},
		{"gh project item-list ok", ToolCall{Tool: "Bash", Command: "gh project item-list 3 --owner acme"}, Allow, ""},
		{"acli workitem view ok", ToolCall{Tool: "Bash", Command: "acli jira workitem view PLAT-1"}, Allow, ""},
		{"tq hub sync now ok", ToolCall{Tool: "Bash", Command: "tq hub sync now --json"}, Allow, ""},
		// equivalent write paths: gh api, gh issue extras, work-item relations, az rest/devops invoke, acli extras
		{"gh api patch issue asks", ToolCall{Tool: "Bash", Command: "gh api -X PATCH repos/acme/app/issues/12 -f state=closed"}, Ask, "tq/tracker-cli-write"},
		{"gh api method post comment asks", ToolCall{Tool: "Bash", Command: "gh api --method POST repos/acme/app/issues/12/comments -f body=x"}, Ask, "tq/tracker-cli-write"},
		{"gh api field implies post asks", ToolCall{Tool: "PowerShell", Command: `gh api repos/acme/app/issues -f title="Export" -f body=x`}, Ask, "tq/tracker-cli-write"},
		{"gh api delete label asks", ToolCall{Tool: "Bash", Command: "gh api repos/acme/app/issues/12/labels/bug --method=DELETE"}, Ask, "tq/tracker-cli-write"},
		{"gh api graphql mutation asks", ToolCall{Tool: "Bash", Command: `gh api graphql -f query='mutation { updateProjectV2ItemFieldValue(input: {}) { clientMutationId } }'`}, Ask, "tq/tracker-cli-write"},
		{"gh api projects put asks", ToolCall{Tool: "Bash", Command: "gh api -X PUT orgs/acme/projects/3 --input p.json"}, Ask, "tq/tracker-cli-write"},
		{"gh issue transfer asks", ToolCall{Tool: "Bash", Command: "gh issue transfer 12 acme/other"}, Ask, "tq/tracker-cli-write"},
		{"gh issue lock asks", ToolCall{Tool: "Bash", Command: "gh issue lock 12"}, Ask, "tq/tracker-cli-write"},
		{"gh issue pin asks", ToolCall{Tool: "Bash", Command: "gh issue pin 12"}, Ask, "tq/tracker-cli-write"},
		{"gh issue develop asks", ToolCall{Tool: "Bash", Command: "gh issue develop 12 --checkout"}, Ask, "tq/tracker-cli-write"},
		{"az work-item relation add asks", ToolCall{Tool: "Bash", Command: "az boards work-item relation add --id 1 --relation-type parent --target-id 2"}, Ask, "tq/tracker-cli-write"},
		{"az work-item relation remove asks", ToolCall{Tool: "Bash", Command: "az boards work-item relation remove --id 1 --relation-type parent --target-id 2 --yes"}, Ask, "tq/tracker-cli-write"},
		{"az rest patch devops asks", ToolCall{Tool: "PowerShell", Command: `az rest --method patch --url "https://dev.azure.com/acme/app/_apis/wit/workitems/1?api-version=7.1" --body @p.json`}, Ask, "tq/tracker-cli-write"},
		{"az devops invoke patch asks", ToolCall{Tool: "Bash", Command: "az devops invoke --area wit --resource workitems --route-parameters id=1 --http-method PATCH --in-file p.json"}, Ask, "tq/tracker-cli-write"},
		{"acli workitem clone asks", ToolCall{Tool: "Bash", Command: "acli jira workitem clone --key PLAT-1"}, Ask, "tq/tracker-cli-write"},
		{"acli workitem link asks", ToolCall{Tool: "Bash", Command: "acli jira workitem link create --out PLAT-1 --in PLAT-2 --type Blocks"}, Ask, "tq/tracker-cli-write"},
		{"acli workitem archive asks", ToolCall{Tool: "Bash", Command: "acli jira workitem archive --key PLAT-1"}, Ask, "tq/tracker-cli-write"},
		{"gh api issues read ok", ToolCall{Tool: "Bash", Command: "gh api repos/acme/app/issues"}, Allow, ""},
		{"gh api graphql query ok", ToolCall{Tool: "Bash", Command: `gh api graphql -f query='query { viewer { login } }'`}, Allow, ""},
		{"gh api pr comments post ok", ToolCall{Tool: "Bash", Command: "gh api -X POST repos/acme/app/pulls/3/comments -f body=x"}, Allow, ""},
		{"az rest get devops ok", ToolCall{Tool: "Bash", Command: `az rest --method get --url "https://dev.azure.com/acme/app/_apis/wit/workitems/1"`}, Allow, ""},
		{"az devops invoke get ok", ToolCall{Tool: "Bash", Command: "az devops invoke --area wit --resource workitems --route-parameters id=1"}, Allow, ""},
		{"az work-item relation show ok", ToolCall{Tool: "Bash", Command: "az boards work-item relation show --id 1"}, Allow, ""},
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
