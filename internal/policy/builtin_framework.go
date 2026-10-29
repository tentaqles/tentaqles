package policy

// The pack framework's core rules (spec C §2.10, §5.3, and the base of spec
// D's tq/hub-store-write). They carry no Pack: they are active in every
// workspace, whether or not a pack is enabled, because enabling a pack is
// exactly what an agent's tq-packs.yaml write would try.

// tqStateDir matches tq's state folder in a command or a path: the default
// ~/.tentaqles, or TQ_HOME spelled as $TQ_HOME, ${TQ_HOME}, %TQ_HOME% or
// $env:TQ_HOME.
const tqStateDir = `(\.tentaqles|\$\{?TQ_HOME\}?|%TQ_HOME%|\$env:TQ_HOME)[/\\]+(hub|packs)([/\\]|\b)`

// shellWriteVerbs write, move, copy, delete or open a file for writing.
const shellWriteVerbs = `(>{1,2}|\b(tee|sqlite3|sed\s+-i|perl\s+-[a-z]*i|truncate|dd|cp|mv|rm|del|erase|rmdir|ln|mklink|install|rsync|copy|move|ren|set-content|add-content|out-file|copy-item|move-item|remove-item|new-item|clear-content|rename-item)\b)`

// tqCmd is a tq invocation (tq, tq.exe, a path to it, quoted or not).
const tqCmd = `(^|[\s;&|(` + bt + `"'=])(\S*[/\\])?tq(\.exe)?["']?\s+`

// httpClients post from the shell.
const httpClients = `\b(curl|wget|http|https|invoke-webrequest|iwr|invoke-restmethod|irm|python3?|py|node|deno|bun|ruby|perl|php|pwsh|powershell)\b`

func frameworkRules() []Rule {
	return []Rule{
		{
			ID: "tq/secret-store-shell", Action: Deny,
			// The pack ledger key (service tentaqles-packs) signs human answers;
			// reading it (cmdkey, CredentialManager/PasswordVault, secret-tool,
			// security find-generic-password, a keyring library) would let an
			// agent sign a forged answer.
			Command: `tentaqles-packs`,
			Reason:  "the pack ledger key in the OS keychain signs human answers; it is never read from the shell",
		},
		{
			ID: "tq/human-answer", Action: Deny,
			Command: tqCmd + `(hub\s+(answer|serve-key|key|token)\b|pack\s+\S+\s+(approve|promote\s+apply|apply)\b)`,
			Reason:  "answering an approval and applying an approved action are done by a human in their own terminal or browser, never by an agent",
		},
		{
			ID: "tq/human-answer", Action: Deny,
			Command: httpClients + gseg + `(/decisions/|/answer\b|/api/cards/)`,
			Reason:  "an HTTP call to the hub's decision or answer endpoints; decisions are answered by a human in the browser or the terminal",
		},
		{
			ID: "tq/human-answer", Action: Deny,
			Tool:   "Read|Grep|NotebookRead|Glob",
			Path:   `(^|/)\.tentaqles/hub/(serve-[^/]*\.json|[^/]*token[^/]*)$`,
			Reason: "the hub's serve info holds its local key; it is never read into the conversation",
		},
		{
			ID: "tq/human-answer", Action: Deny,
			Command: `\bserve-[0-9*?]*\.json\b|[/\\]hub[/\\][^\s;&|]*token`,
			Reason:  "the hub's serve info holds its local key; it is never read into the conversation",
		},
		{
			ID: "tq/pack-config-change", Action: Ask,
			Tool:   "Edit|Write|MultiEdit|NotebookEdit",
			Path:   `(^|/)tq-packs\.ya?ml$|(^|/)\.tq/[^/]*baseline[^/]*\.json$|(^|/)[^/]*baseline[^/]*\.json$`,
			Reason: "this changes the pack config (tq-packs.yaml) or a pack baseline, which changes what tq pack check requires; confirm the change",
		},
		{
			ID: "tq/pack-config-change", Action: Ask,
			Command: shellWriteVerbs + seg + `\btq-packs\.ya?ml\b|\bmkfifo\b` + seg + `\btq-packs\.ya?ml\b|` + shellWriteVerbs + seg + `[^\s;&|]*baseline[^\s;&|/\\]*\.json\b`,
			Reason:  "this writes the pack config (tq-packs.yaml) or a pack baseline from the shell; confirm the change",
		},
		{
			ID: "tq/pack-config-change", Action: Ask,
			Tool:    "Edit|Write|MultiEdit",
			Path:    `(^|/)\.tentaqles\.ya?ml$`,
			Content: `(^|\n)\s*packs\s*:|\btools\s*:|\btool_paths\s*:|\bbaseline\s*:`,
			Reason:  "this edits the manifest's packs: block; confirm it (the manifest is re-trusted with tq allow)",
		},
		{
			ID: "tq/hub-store-write", Action: Deny,
			Tool:   "Edit|Write|MultiEdit|NotebookEdit",
			Path:   `(^|/)\.tentaqles/(hub|packs)/`,
			Reason: "the hub store, the pack ledgers and approvals and the saved plans are written only by tq",
		},
		{
			ID: "tq/hub-store-write", Action: Deny,
			Command:         shellWriteVerbs + gseg + tqStateDir + `|` + tqStateDir + gseg + `\s(>{1,2})`,
			SkipHeredocData: true,
			Reason:          "the hub store, the pack ledgers and approvals and the saved plans are written only by tq",
		},
	}
}
