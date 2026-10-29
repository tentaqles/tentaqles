package policy

// seg is "anything up to the next shell separator": rules written with it stay
// inside one command of a chain like `cd x && git push`.
const seg = `[^;&|\n]*`

// bt is a backtick (not writable inside a raw string).
const bt = "`"

// gseg is seg that may also cross a bash (backslash-newline) or PowerShell
// (backtick-newline) line continuation.
const gseg = `(?:[^;&|\n]|\\\r?\n|` + bt + `\r?\n)*`

// verbEnd closes a verb token: a quote, whitespace, end of input, a command
// separator, a subshell paren, a redirect or a backtick.
const verbEnd = `(["']|\s|$|[;&|\n)<>` + bt + `])`

// destTail follows a path that must be the destination of cp/mv: only flag
// words, redirections and a comment may come after it before the segment ends.
const destTail = `['"]?(?:\s+(?:-\S+|\d*>\S*|#[^;&|\n]*))*\s*(?:$|[;&|\n])`

// iacFlags is any number of flag words (with their values) between a tool and
// its verb: `kubectl -n prod apply`, `terraform -chdir=infra apply`.
const iacFlags = `(?:\s+-{1,2}[\w.-]+(?:=\S+|\s+[^-\s]\S*)?)*`

// Pieces of tq/tracker-cli-write's raw-API alternatives. trackerWriteMethod is an HTTP write
// method flag (gh -X/--method, also -XPATCH and -X=POST; az rest --method or -m; az devops invoke
// --http-method); trackerGHWrite adds gh api's field flags, which make it POST; trackerGHPath is an
// issues or projects path. trackerGraphQLWrite is a GraphQL mutation, or a query the regex cannot
// read (from a file: -F query=@m.graphql, or --input), which is asked about as if it wrote.
const trackerWriteMethod = `\s(-X\s*=?\s*|-m(\s+|=)|(--method|--http-method)(\s+|=))["']?(post|patch|put|delete)\b`
const trackerGraphQLWrite = `(\bmutation\b|\s(-F|--field|-f|--raw-field)(\s+|=)?["']?query=@|\s--input\b)`
const trackerGHWrite = `(` + trackerWriteMethod + `|\s(-f|--field|--raw-field|--input)\b)`
const trackerGHPath = `(/(issues|projects(v2)?)\b|[\s"'](issues|projects(v2)?)/)`
const trackerAzureHost = `\b(dev\.azure\.com|visualstudio\.com)\b`

// envExcept exempts the conventional committed templates of a .env file.
const envExcept = `\.env\.(example|sample|template|dist|defaults|schema)\b`

// tqHomeVar is the TQ_HOME variable as bash, PowerShell or cmd spell it.
const tqHomeVar = `(\$\{?(env:)?tq_home\}?|%tq_home%)`

// tqStore is tq's trust store or registry under ~/.tentaqles or $TQ_HOME,
// or a file tq renders into an identity (tqIdentityFile), named through the
// identity's CLAUDE_CONFIG_DIR or CODEX_HOME too (agentDirVar), or a Codex
// config layer: a repo's .codex/ or ~/.codex.
const tqStore = `((\.tentaqles|` + tqHomeVar + `)[/\\]+(trust\b|config\.yaml\b|` + tqIdentityFile + `)` +
	`|` + agentDirVar + `[/\\]+(settings(\.local)?\.json\b|config\.toml\b|hooks\.json\b|rules\b)` +
	`|\.codex[/\\]+(config\.toml\b|hooks\.json\b|rules\b))`

// agentDirVar is CLAUDE_CONFIG_DIR or CODEX_HOME as bash, PowerShell or cmd
// spell it: in a tq session they are the identity dirs.
const agentDirVar = `(\$\{?(env:)?(claude_config_dir|codex_home)\}?|%(claude_config_dir|codex_home)%)`

// tqIdentityFile is, under identities/<ws>/, the codex identity's rules/,
// hooks.json and config.toml, or the Claude identity's settings files: tq
// renders them, and an agent that edits them widens its own session.
const tqIdentityFile = `identities[/\\]+[^/\\\s'"]+[/\\]+(codex[/\\]+(rules\b|hooks\.json\b|config\.toml\b)|claude[/\\]+settings(\.local)?\.json\b)`

// scriptWrite is a call in a python/node one-liner that writes or removes a
// file, or an open() in a write mode.
const scriptWrite = `(\bwrite|\bunlink|\bremove|\brename|\brmtree|\breplace\(|\bmove\(|\bcopy(file)?\(|\brmSync|\bappendFile|\bmkdir|\bchmod|\btouch\(|,\s*["'][wax]b?\+?["'])`

// readVerbs are commands (bash, cmd and PowerShell) that print a file.
const readVerbs = `\b(cat|type|more|less|head|tail|bat|nl|strings|xxd|od|grep|rg|findstr|gc|get-content|select-string|sls)\b`

// coreBuiltin returns the rules defined in this file. IDs are stable: manifests disable them
// by id (guard.disable) and tests pin them.
func coreBuiltin() []Rule {
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
			// The dump must be the command word of a segment (start, or after
			// ; && || | ( a backtick or a newline, optionally behind `cmd /c`)
			// with no arguments: `set -e`, `echo set`, `Set-Content` and
			// `git commit -m "set the flag"` are not dumps. A closing quote
			// also ends the dump (`bash -c "ls; set"`), and a quote right
			// after -c (`bash -c 'set'`, `sh -lc "printenv"`) starts one. A
			// Bash heredoc body that is data (guard.BlankDataHeredocs) is not
			// read.
			Command: `(^|[;&|\n(` + bt + `]\s*|\s-[a-z]*c\s+["']\s*)(cmd(\.exe)?\s+/[ck]\s+)?(printenv|env|set|export\s+-p|declare\s+-p|typeset\s+-p|(get-childitem|gci|dir|ls)\s+env:\\?)\s*($|[;&|\n)"'` + bt + `])` +
				`|\[(system\.)?environment\]::getenvironmentvariables\s*\(`,
			SkipHeredocData: true,
			Reason:          "dumping the whole environment prints every token in it. Check one variable without printing it (e.g. `test -n \"$NAME\"`), or run the command with `tq dotenv run -- <command>`",
		},
		// --- the guard's own settings -------------------------------------------
		// The agent must not be able to switch the guard off by itself: changes
		// go through tq (and the user confirms them), never by editing files.
		{
			ID: "tq/guard-change", Action: Ask,
			// Not a flag grammar: after the tq executable (bare, path-prefixed
			// or quoted) and the word guard/unattended, the verb is any
			// whitespace/quote/=-delimited on|off|set token later in the same
			// command segment. A literal on/off argument before a read-only
			// verb therefore asks too (fails safe).
			// The segment may cross a bash (\<newline>) or PowerShell
			// (`<newline>) line continuation; the verb may be closed by a
			// subshell paren, a redirect or a backtick; a backtick or paren
			// may open the command (command substitution).
			Command: `(^|[;&|\n(` + bt + `]\s*|\s)["']?(\S*[/\\])?tq(\.exe)?["']?\s+(["']?guard\b["']?` + gseg + `[\s"'=](off|on|set)` + verbEnd + `|["']?unattended\b["']?` + gseg + `[\s"'=](off|on)` + verbEnd + `|["']?(allow|deny)(["']|\b))`,
			Reason:  "this changes what tq's guard checks or trusts; confirm it is what you want",
		},
		{
			ID: "tq/mode-change", Action: Deny,
			// tq's guard mode and its grants are human-only (spec E §10.1):
			// the binary refuses agent sessions and needs a terminal, and
			// this rule gives the agent a clear block first. Only a command
			// word counts (after a separator, sudo/env/..., or the quote of
			// `sh -c "…"`), so a commit message or an echo that mentions the
			// verbs does not. `tq mode`, `tq mode log` and `tq allow <ws>`
			// stay allowed (the last asks through tq/guard-change).
			Command: cmdStart + `["']?(\S*[/\\])?tq(\.exe)?["']?\s+(` +
				`["']?mode\b["']?` + gseg + `[\s"'=](set|toggle|override|edit)` + verbEnd +
				`|["']?allow\b` + gseg + `\s--?(mode|bypass)\b` +
				`|["']?unattended\b["']?` + gseg + `[\s"'=](on|off)` + verbEnd + `)`,
			SkipHeredocData: true,
			Reason:          "tq's guard mode and mode grants are changed by a human in a terminal (tq mode set|toggle|override|edit, tq allow --mode), never from an agent session",
		},
		{
			ID: "tq/guard-file-write", Action: Deny,
			Tool:   "Edit|Write|MultiEdit|NotebookEdit",
			Path:   `(^|/)\.tentaqles/guard\.yaml$`,
			Reason: "the global guard file is changed with `tq guard off|on|set --all`, not edited directly",
		},
		{
			ID: "tq/guard-file-write", Action: Deny,
			// No exception for tq itself: `tq guard` never names the file, and
			// an exception would also cover a redirect in the same command
			// (`tq guard list > ~/.tentaqles/guard.yaml`).
			// A heredoc body that is data (a note or fixture fed to cat/tee or
			// a commit message) may mention the file; the command line, the
			// redirect target included, and code bodies are still matched.
			Command:         `\.tentaqles[/\\]+guard\.yaml`,
			SkipHeredocData: true,
			Reason:          "the global guard file is changed with `tq guard off|on|set --all`, not from the shell",
		},
		// --- tq's trust store and registry ----------------------------------------
		// $TQ_HOME/trust holds the trust and bypass markers, $TQ_HOME/config.yaml
		// the registry: only tq (tq allow, tq init, ...) writes them. Reads stay
		// allowed. A custom TQ_HOME is matched by its variable spelling only.
		{
			ID: "tq/trust-store-write", Action: Deny,
			Tool: "Edit|Write|MultiEdit|NotebookEdit",
			Path: `(^|/)(\.tentaqles|` + tqHomeVar + `)/(trust(/|$)|config\.yaml$|identities/[^/]+/(codex/(rules(/|$)|hooks\.json$|config\.toml$)|claude/settings(\.local)?\.json$))` +
				`|(^|/)\.codex/(config\.toml$|hooks\.json$|rules(/|$))`,
			Reason: "tq's trust store, registry and rendered identity settings are changed with tq (tq allow, tq deny, tq init, tq settings render), not edited directly",
		},
		{
			ID: "tq/trust-store-write", Action: Deny,
			// Redirects and in-place writers deny on any mention of the target;
			// mv, rm and their PowerShell forms on any mention (moving the store
			// away is a write too); cp only when the target is the destination,
			// so `cp ~/.tentaqles/config.yaml backup` is a read. A python/node
			// one-liner denies when it names the target and a write call.
			Command: `(>{1,2}\s*|\b(tee|sed\s+-i|set-content|add-content|out-file|sc|ac|new-item|ni|rm|rmdir|del|erase|rd|remove-item|ri|mv|move|move-item|mi|rename-item|ren)\b` + seg + `)\S*` + tqStore +
				`|\b(cp|copy|copy-item|cpi)\b` + seg + `\S*` + tqStore + `\S*` + destTail +
				`|\b(python[0-9.]*|py|node|deno|bun)(\.exe)?\b[^\n]*\s-(c|e|p|-eval)\b[^\n]*(` + tqStore + `[^\n]*` + scriptWrite + `|` + scriptWrite + `[^\n]*` + tqStore + `)`,
			SkipHeredocData: true,
			Reason:          "tq's trust store, registry and rendered identity settings are changed with tq (tq allow, tq deny, tq init, tq settings render), not from the shell",
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
		// --- company trackers ------------------------------------------------------
		// Writes to the company board go through the hub's sync engine (the outbox, under
		// the sync.policy.* settings); the engine calls the CLIs itself, outside any tool
		// call. An agent running the CLI directly bypasses that policy and the redaction.
		// The tool is a word of its own (bare, path-prefixed or quoted) and flags may sit
		// between it and the subcommand (`gh -R acme/app issue edit`), so a PR title that
		// only mentions "issue create" is not a write.
		// The raw API paths count too: `gh api` with a write (an -X/--method write, or a field,
		// which makes gh POST) on an issues or projects path, or a GraphQL mutation; `az rest` with
		// a write method on dev.azure.com, and `az devops invoke` with a write --http-method. A GraphQL
		// query read from a file counts as a write: the rule cannot see whether it is a mutation.
		{
			ID: "tq/tracker-cli-write", Action: Ask,
			Command: `(^|[;&|\n(` + bt + `]|\s)["']?(\S*[/\\])?(` +
				`az(\.cmd|\.exe)?["']?` + iacFlags + `\s+(` +
				`boards\s+work-item\s+(create|update|delete|relation\s+(add|remove))` +
				`|rest\b(` + seg + trackerAzureHost + seg + trackerWriteMethod + `|` + seg + trackerWriteMethod + seg + trackerAzureHost + `)` +
				`|devops\s+invoke\b` + seg + trackerWriteMethod + `)` +
				`|gh(\.exe)?["']?` + iacFlags + `\s+(` +
				`issue\s+(edit|close|create|comment|reopen|delete|transfer|lock|unlock|pin|unpin|develop)` +
				`|project\s+item-(edit|add|create|delete|archive)` +
				`|api\b(` + seg + trackerGHPath + seg + trackerGHWrite + `|` + seg + trackerGHWrite + seg + trackerGHPath +
				`|` + seg + `\bgraphql\b` + seg + trackerGraphQLWrite + `))` +
				`|acli(\.exe)?["']?` + iacFlags + `\s+jira\s+workitem\s+(edit|transition|create|comment|delete|assign|clone|link|archive|unarchive)` +
				`)\b`,
			Reason: "this writes to the company tracker outside the hub's sync engine, so the sync.policy settings and the outbound redaction do not apply; prefer `tq hub sync` (or the /tentaqles:sync skill), or confirm this write",
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
		// --- infrastructure and remote databases ----------------------------------
		{
			ID: "tq/iac-apply", Action: Ask,
			Command: `\b(?:(?:terraform|tofu)` + iacFlags + `\s+apply|pulumi` + iacFlags + `\s+up|cdk` + iacFlags + `\s+deploy|cdktf` + iacFlags + `\s+deploy|az` + iacFlags + `\s+deployment\s+\S+\s+create|aws` + iacFlags + `\s+cloudformation\s+deploy|gcloud` + iacFlags + `\s+deployment-manager\s+deployments\s+(?:create|update)|sam` + iacFlags + `\s+deploy|serverless` + iacFlags + `\s+deploy|sls` + iacFlags + `\s+deploy|kubectl` + iacFlags + `\s+apply|helm` + iacFlags + `\s+(?:install|upgrade))\b`,
			Except:  `[^;&|\n#]*--dry-run[^;&|\n#]*`, // a dry run changes nothing: cut the command, but a comment cannot do it
			Reason:  "applies infrastructure changes to a live environment; confirm the target environment is the one named in the command (workspace, stack, resource group or kube context) and that a plan was reviewed",
		},
		{
			ID: "tq/remote-migration", Action: Ask,
			Command: `\b(prisma\s+migrate\s+deploy|alembic\s+upgrade|flyway\s+migrate|liquibase\s+update|dbmate\s+up|goose\s+up|knex\s+migrate:latest|sqitch\s+deploy|dotnet\s+ef\s+database\s+update|supabase\s+db\s+push|supabase\s+migration\s+up|drizzle-kit\s+(push|migrate)|atlas\s+migrate\s+apply|rails\s+db:migrate|php\s+artisan\s+migrate|manage\.py\s+migrate)\b`,
			Except:  `[^;&|\n#]*(\blocalhost\b|\b127\.0\.0\.1\b|\s--local\b)[^;&|\n#]*`, // cuts the command that targets a local database; a comment cannot
			Reason:  "runs database migrations against a connection that is not local; confirm the target database and that a backup or rollback path exists (see db-migration)",
		},
		// --- Claude Code settings ---------------------------------------------------
		{
			ID: "tq/settings-widen", Action: Deny,
			Tool: "Edit|MultiEdit|Write",
			Path: `(^|/)(\.claude/settings(\.local)?\.json|\.claude\.json|settings(\.local)?\.json)$`,
			// Also what switches tq off for the next or a nested session: the
			// tentaqles plugin disabled, an env block naming tq's or Claude's
			// mode variables, a looser defaultMode.
			Content: `Bash\(\*\)|bypassPermissions|"disableAllHooks"\s*:\s*true|"hooks"\s*:\s*\{\s*\}|"skipDangerousModePermissionPrompt"\s*:\s*true` +
				`|"enabledPlugins"\s*:\s*\{[^}]*"tentaqles[^"]*"\s*:\s*false` +
				`|"env"\s*:\s*\{[^}]*"(` + agLoosenEnv + `)"` +
				`|"defaultMode"\s*:\s*"(auto|dontAsk)"`,
			Reason: "this would widen permissions or switch hooks off from inside the session; permission changes go through `tq settings render` and the manifest",
		},
		{
			ID: "tq/settings-shell-write", Action: Deny,
			// Redirects and in-place writers deny on any mention of the target; the
			// copy/move verbs deny only when the target is the last word of the
			// segment (the destination), so `cp .claude/settings.json backup` is a read.
			Command: `(>{1,2}\s*|\b(tee|sed\s+-i|set-content|add-content|out-file)\b` + seg + `)\S*\.claude[/\\](settings(\.local)?\.json|tq-rules\.yaml)\b` +
				`|\b(cp|mv|copy-item|move-item)\b` + seg + `\S*\.claude[/\\](settings(\.local)?\.json|tq-rules\.yaml)` + destTail +
				`|(>{1,2}\s*|\b(tee|set-content|out-file)\b` + seg + `)\S*[/\\]\.claude\.json\b` +
				`|\b(cp|mv|copy-item|move-item)\b` + seg + `\S*[/\\]\.claude\.json` + destTail,
			Reason: "Claude Code settings are not written from the shell; use `tq settings render` (permissions) or the Edit tool, which the guard checks",
		},
		// --- CI ----------------------------------------------------------------
		{
			ID: "tq/gha-unpinned", Action: Ask,
			Tool:    "Edit|MultiEdit|Write",
			Path:    `(^|/)\.github/workflows/[^/]+\.ya?ml$`,
			Content: `uses:\s*[^\s@#]+@(latest|head)\b`,
			Reason:  "a GitHub Action pinned to a moving ref; pin a release tag or commit SHA",
		},
		{
			ID: "tq/gha-branch-ref", Action: Deny,
			Tool:    "Edit|MultiEdit|Write",
			Path:    `(^|/)\.github/workflows/[^/]+\.ya?ml$`,
			Content: `uses:\s*[^\s@#]+@(main|master)\b`,
			Reason:  "a GitHub Action pinned to a moving branch runs whatever is pushed there; pin a full commit SHA with the version in a comment",
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
