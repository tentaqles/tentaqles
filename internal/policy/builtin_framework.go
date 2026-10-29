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

func frameworkRules() []Rule {
	return []Rule{
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
