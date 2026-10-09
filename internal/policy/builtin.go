package policy

// seg is "anything up to the next shell separator": rules written with it stay
// inside one command of a chain like `cd x && git push`.
const seg = `[^;&|\n]*`

// envExcept exempts the conventional committed templates of a .env file.
const envExcept = `\.env\.(example|sample|template|dist|defaults|schema)\b`

// readVerbs are commands (bash, cmd and PowerShell) that print a file.
const readVerbs = `\b(cat|type|more|less|head|tail|bat|nl|strings|xxd|od|grep|rg|findstr|gc|get-content|select-string|sls)\b`

// Builtin returns tq's default rules. IDs are stable: manifests disable them
// by id (guard.disable) and tests pin them.
func Builtin() []Rule {
	return []Rule{
		// --- secrets: reading ------------------------------------------------
		{
			ID: "tq/secret-store-read", Action: Deny,
			Tool:   "Read|Grep|NotebookRead",
			Path:   `(^|/)\.ssh/id_[a-z0-9_]+$|(^|/)\.aws/credentials$|(^|/)\.git-credentials$|(^|/)\.netrc$|(^|/)\.docker/config\.json$|(^|/)\.azure/(accesstokens\.json|msal_token_cache\.[a-z]+)$|(^|/)\.tentaqles/bundles/catalog\.yaml$|(^|/)\.config/gh/hosts\.yml$`,
			Reason: "credential stores (SSH keys, cloud/git credentials, the tq catalog) are never read into the conversation",
		},
		{
			ID: "tq/secret-store-shell", Action: Deny,
			Command: readVerbs + seg + `(\.ssh[/\\]id_[a-z0-9_]+\b|\.aws[/\\]credentials\b|\.git-credentials\b|\.netrc\b|bundles[/\\]catalog\.yaml\b|gh[/\\]hosts\.yml\b)`,
			Except:  `\.ssh[/\\]id_[a-z0-9_]+\.pub\b`,
			Reason:  "credential stores (SSH keys, cloud/git credentials, the tq catalog) are never printed into the conversation",
		},
		{
			ID: "tq/env-file-read", Action: Deny,
			Tool:   "Read|Grep|NotebookRead",
			Path:   `(^|/)\.env(\.[^/]*)?$`,
			Except: envExcept,
			Reason: "reading a .env file puts its secrets in the transcript. Use `tq dotenv run -- <command>` instead and reference the variable as $NAME (use `tq dotenv run -- bash -c '...'` when the command needs $NAME expansion); values are masked in its output.",
		},
		{
			ID: "tq/env-file-shell", Action: Deny,
			Command: readVerbs + seg + `(^|[\s/\\'"=])\.env(\.[a-z0-9_-]+)?\b`,
			Except:  envExcept,
			Reason:  "printing a .env file puts its secrets in the transcript. Use `tq dotenv run -- <command>` instead and reference the variable as $NAME (use `tq dotenv run -- bash -c '...'` when the command needs $NAME expansion); values are masked in its output.",
		},
		{
			ID: "tq/env-dump", Action: Deny,
			Command: `(^|[;&|\n(]\s*)(printenv|env|set|export\s+-p|(get-childitem|gci|dir|ls)\s+env:\\?)\s*($|[;&|\n)])`,
			Reason:  "dumping the whole environment prints every token in it. Check one variable without printing it (e.g. `test -n \"$NAME\"`), or run the command with `tq dotenv run -- <command>`",
		},
		// --- the guard's own settings -------------------------------------------
		// The agent must not be able to switch the guard off by itself: changes
		// go through tq (and the user confirms them), never by editing files.
		{
			ID: "tq/guard-change", Action: Ask,
			Command: `(^|[;&|\n(]\s*|\s)(\S*[/\\])?tq(\.exe)?\s+(guard\s+(off|on|set)|allow|deny)\b`,
			Reason:  "this changes what tq's guard checks or trusts; confirm it is what you want",
		},
		{
			ID: "tq/guard-file-write", Action: Deny,
			Tool:   "Edit|Write|MultiEdit|NotebookEdit",
			Path:   `(^|/)\.tentaqles/guard\.yaml$`,
			Reason: "the global guard file is changed with `tq guard off|on|set --all`, not edited directly",
		},
		{
			ID: "tq/guard-file-write", Action: Deny,
			Command: `\.tentaqles[/\\]+guard\.yaml`,
			Except:  `(\S*[/\\])?tq(\.exe)?\s+guard\b[^;&|\n]*`,
			Reason:  "the global guard file is changed with `tq guard off|on|set --all`, not from the shell",
		},
		// --- git ---------------------------------------------------------------
		{
			ID: "tq/force-push-main", Action: Deny,
			Command: `\bgit\b` + seg + `\bpush\b` + seg + `((\s(--force|--force-with-lease|-f)\b` + seg + `\b(main|master)\b)|(\b(main|master)\b` + seg + `\s(--force|--force-with-lease|-f)\b)|\s\+(refs/heads/)?(main|master)\b)`,
			Reason:  "force-pushing main/master is never allowed",
		},
		{
			ID: "tq/force-push", Action: Ask,
			Command: `\bgit\b` + seg + `\bpush\b` + seg + `\s(--force|-f)(\s|$)`,
			Reason:  "plain --force can overwrite others' work; prefer --force-with-lease, or confirm",
		},
		{
			ID: "tq/gh-admin-merge", Action: Ask,
			Command: `\bgh\s+pr\s+merge\b` + seg + `--admin\b`,
			Reason:  "--admin bypasses branch protection",
		},
		{
			ID: "tq/gh-repo-delete", Action: Deny,
			Command: `\bgh\s+repo\s+delete\b`,
			Reason:  "deleting a repository is done by hand, not by an agent",
		},
		// --- cloud / infra -----------------------------------------------------
		{
			ID: "tq/cloud-delete", Action: Ask,
			Command: `\b(az|aws|gcloud|gsutil|doctl|databricks|hostinger)\b` + seg + `\s(delete|remove|rm|purge|destroy|terminate-instances|delete-[a-z0-9-]+|remove-[a-z0-9-]+)(\s|$)`,
			Reason:  "deleting cloud resources",
		},
		{
			ID: "tq/iac-destroy", Action: Ask,
			Command: `\b(terraform|tofu|cdk|pulumi|sst)\b` + seg + `\bdestroy\b`,
			Reason:  "destroying infrastructure",
		},
		{
			ID: "tq/registry-write", Action: Ask,
			Command: `\breg(\.exe)?\s+(add|delete|import)\b|\b(set-itemproperty|new-itemproperty|remove-itemproperty|remove-item)\b` + seg + `\bhk(lm|cu|cr|u|cc):`,
			Reason:  "writing the Windows registry",
		},
		// --- databases ---------------------------------------------------------
		{
			ID: "tq/sql-destructive-shell", Action: Ask,
			Command: `\b(psql|sqlcmd|mysql|sqlite3|snowsql|snow\s+sql|supabase\s+db\s+(execute|query)|bq\s+query|databricks\s+sql)\b[^\n]*\b(drop\s+(table|schema|database|view|function|index|column|policy|role)|truncate\b|alter\s+(table|role|policy)|delete\s+from|update\s+\S+\s+set|grant\b|revoke\b)`,
			Reason:  "destructive or schema-changing SQL run directly against a database; schema changes belong in a migration",
		},
		{
			ID: "tq/migration-edit", Action: Ask,
			Tool:         "Edit|MultiEdit|Write",
			Path:         `(^|/)(migrations|migrate|alembic/versions|supabase/migrations|prisma/migrations|db/migrate|flyway|sql/migrations)/[^/]+$`,
			ExistingOnly: true,
			Reason:       "editing an existing migration; if it was already applied anywhere, write a new migration instead",
		},
		{
			ID: "tq/mcp-sql-write", Action: Ask,
			Tool:    `mcp__.*(postgres|supabase|sql|database|db|snowflake|databricks|mysql|mongo).*__.*`,
			Content: `\b(insert\s+into|update\s+[\w."]+\s+set|delete\s+from|drop\s+\w|truncate\s|alter\s+\w|create\s+(or\s+replace\s+)?(table|index|function|policy|role|schema|extension|trigger|view|type)|grant\s|revoke\s)`,
			Reason:  "a write/DDL statement through a database MCP server",
		},
		{
			ID: "tq/mcp-apply-migration", Action: Ask,
			Tool:   `mcp__.*__(apply_migration|execute_migration|run_migration|deploy_.*|merge_branch|reset_branch|delete_branch|pause_project|restore_project)`,
			Reason: "a migration/deploy/branch operation through an MCP server",
		},
		// --- automation / outward-facing MCP -----------------------------------
		{
			ID: "tq/mcp-n8n-write", Action: Ask,
			Tool:   `mcp__.*n8n.*__(?:\w*?_)?(create|update|publish|unpublish|archive|delete|execute|test|restore|mutate|revert|rename|add|move|call)(?:_.*)?`,
			Reason: "changing or running an n8n workflow/agent (it may be production)",
		},
		{
			ID: "tq/mcp-github-write", Action: Ask,
			Tool:   `mcp__.*github.*__(create|update|merge|push|fork|delete|add)_.*`,
			Reason: "writing to GitHub through MCP (outward-facing)",
		},
		{
			ID: "tq/mcp-hosting-exec", Action: Ask,
			Tool:   `mcp__.*(hostinger|vercel|netlify|render|railway|fly).*__(execute|multi-execute|deploy.*|delete.*|create.*|update.*)`,
			Reason: "a hosting provider operation through MCP",
		},
		// --- CI ----------------------------------------------------------------
		{
			ID: "tq/gha-unpinned", Action: Ask,
			Tool:    "Edit|MultiEdit|Write",
			Path:    `(^|/)\.github/workflows/[^/]+\.ya?ml$`,
			Content: `uses:\s*[^\s@#]+@(main|master|latest|head)\b`,
			Reason:  "a GitHub Action pinned to a moving branch; pin a release tag or commit SHA",
		},
		{
			ID: "tq/gha-pr-target", Action: Ask,
			Tool:    "Edit|MultiEdit|Write",
			Path:    `(^|/)\.github/workflows/[^/]+\.ya?ml$`,
			Content: `\bpull_request_target\b`,
			Reason:  "pull_request_target runs untrusted PR code with repository secrets",
		},
	}
}
