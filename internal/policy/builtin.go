package policy

// seg is "anything up to the next shell separator": rules written with it stay
// inside one command of a chain like `cd x && cat .env`.
const seg = `[^;&|\n]*`

// bt is a backtick (not writable inside a raw string).
const bt = "`"

// envExcept exempts the conventional committed templates of a .env file.
const envExcept = `\.env\.(example|sample|template|dist|defaults|schema)\b`

// readVerbs are commands (bash, cmd and PowerShell) that print a file.
const readVerbs = `\b(cat|type|more|less|head|tail|bat|nl|strings|xxd|od|grep|rg|findstr|gc|get-content|select-string|sls)\b`

// Builtin returns tq's secret rules, the only rules the hook applies besides
// the identity guard (docs/prd.md D-31). IDs are stable: the hook selects
// them by id and tests pin them. The slice is a fresh copy.
func Builtin() []Rule {
	return []Rule{
		// --- secrets: reading ------------------------------------------------
		{
			ID: "tq/secret-store-read", Category: CategorySecrets, Action: Deny,
			Tool:   "Read|Grep|NotebookRead",
			Path:   `(^|/)\.ssh/id_[a-z0-9_]+$|(^|/)\.aws/credentials$|(^|/)\.git-credentials$|(^|/)\.netrc$|(^|/)\.docker/config\.json$|(^|/)\.azure/(accesstokens\.json|msal_token_cache\.[a-z]+)$|(^|/)\.tentaqles/bundles/catalog\.yaml$|(^|/)\.config/gh/hosts\.yml$`,
			Reason: "credential stores (SSH keys, cloud/git credentials, the tq catalog) are never read into the conversation",
		},
		{
			ID: "tq/secret-store-shell", Category: CategorySecrets, Action: Deny,
			Command: readVerbs + seg + `(\.ssh[/\\]id_[a-z0-9_]+\b|\.aws[/\\]credentials\b|\.git-credentials\b|\.netrc\b|bundles[/\\]catalog\.yaml\b|gh[/\\]hosts\.yml\b)`,
			Except:  `\.ssh[/\\]id_[a-z0-9_]+\.pub\b`,
			Reason:  "credential stores (SSH keys, cloud/git credentials, the tq catalog) are never printed into the conversation",
		},
		{
			ID: "tq/env-file-read", Category: CategorySecrets, Action: Deny,
			Tool:   "Read|Grep|NotebookRead",
			Path:   `(^|/)\.env(\.[^/]*)?$`,
			Except: envExcept,
			Reason: "reading a .env file puts its secrets in the transcript. Use `tq dotenv run -- <command>` instead and reference the variable as $NAME (use `tq dotenv run -- bash -c '...'` when the command needs $NAME expansion); values are masked in its output.",
		},
		{
			ID: "tq/env-file-shell", Category: CategorySecrets, Action: Deny,
			Command: readVerbs + seg + `(^|[\s/\\'"=])\.env(\.[a-z0-9_-]+)?\b`,
			Except:  envExcept,
			Reason:  "printing a .env file puts its secrets in the transcript. Use `tq dotenv run -- <command>` instead and reference the variable as $NAME (use `tq dotenv run -- bash -c '...'` when the command needs $NAME expansion); values are masked in its output.",
		},
		{
			ID: "tq/env-dump", Category: CategorySecrets, Action: Deny,
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
	}
}
