package policy

// packRules are the static regex rules of the packs (spec C §2.10). Each
// sets Pack: the hook drops it while that pack is not enabled. One id may
// appear once per pack (tq/hub-store-write is listed for cloud and for db).
// Spec D's guard-hardening and iac-policy workstreams append their blocks
// here.
func packRules() []Rule {
	var rules []Rule
	rules = append(rules, hubStoreWriteRules("cloud")...) // D-GR
	rules = append(rules, hubStoreWriteRules("db")...)    // D-GR
	return rules
}

// PackRules returns the static pack rules (for listing).
func PackRules() []Rule { return packRules() }

// ForPacks returns rules without the pack rules whose pack is not enabled.
func ForPacks(rules []Rule, enabled func(pack string) bool) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.Pack != "" && (enabled == nil || !enabled(r.Pack)) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// --- spec D §16, D-GR: tq/hub-store-write --------------------------------

// hubStoreRef matches a shell reference to $TQ_HOME/hub or $TQ_HOME/packs:
// the default home (~/.tentaqles, any prefix) or the TQ_HOME variable in
// sh, PowerShell or cmd form. A static rule can't know a custom TQ_HOME
// spelled as a literal path.
const hubStoreRef = `(?:\.tentaqles|\$TQ_HOME|\$\{TQ_HOME\}|\$env:TQ_HOME|%TQ_HOME%)[/\\]+(?:hub|packs)(?:[/\\"'\s;&|)]|$)`

// hubStoreArg is the part of one shell word before hubStoreRef.
const hubStoreArg = `["']?[^\s;&|"'<>]*`

// cmdAt puts a verb at a word start, so `grep -ri` is not `ri`, and ends it
// at a word boundary (`cp` is not `cpi`); the space after it is left for
// the flag patterns that follow.
func cmdAt(verbs string) string { return `(?:^|[\s;&|(])(?:` + verbs + `)(?:\.exe)?\b` }

// hubStoreWriteCommand: sqlite3 on the store, a redirect into it, tee, the
// destination of cp/mv/Copy-Item/Move-Item (last word, -t, -Destination),
// dd of=, sed -i, and the writers and removers that name it.
var hubStoreWriteCommand = `\bsqlite3(?:\.exe)?\b` + seg + hubStoreRef +
	`|>\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`tee`) + seg + hubStoreRef +
	`|` + cmdAt(`cp|mv|copy|move|rsync|install|Copy-Item|Move-Item|cpi|mi`) + seg + `\s` + hubStoreArg + hubStoreRef + `[^\s;&|]*\s*(?:$|[;&|\n])` +
	`|` + cmdAt(`cp|mv|install`) + seg + `\s(?:-t\s*|--target-directory[=\s]\s*)` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`copy|move|Copy-Item|Move-Item|cpi|mi`) + seg + `\s-d[a-z]*[:\s]\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`dd`) + seg + `\sof=` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`sed`) + seg + `\s(?:-[a-z]*i|--in-place)` + seg + hubStoreRef +
	`|` + cmdAt(`rm|rmdir|del|erase|rd|touch|truncate|ln|chmod|mkdir|md|Remove-Item|ri|Set-Content|sc|Add-Content|ac|Out-File|New-Item|ni|Clear-Content|clc|Rename-Item|rni|ren`) + seg + hubStoreRef

const hubStoreWriteReason = "the hub store, the pack ledgers and the saved plans under $TQ_HOME (hub/, packs/) are written only by tq (`tq hub`, `tq pack ...`); this rule can't be disabled"

// hubStoreWriteRules is tq/hub-store-write for one pack: one id, listed for
// cloud and for db so it is active when either is on (spec D §16). Each
// pack carries a file-tool entry and a shell entry, as tq/guard-file-write
// does. guardcfg's never-disable list keeps guard.disable off it.
func hubStoreWriteRules(pack string) []Rule {
	return []Rule{
		{
			ID: "tq/hub-store-write", Action: Deny, Pack: pack,
			Tool:   "Edit|Write|MultiEdit|NotebookEdit",
			Path:   `(^|/)\.tentaqles/(hub|packs)(/|$)`,
			Reason: hubStoreWriteReason,
		},
		{
			ID: "tq/hub-store-write", Action: Deny, Pack: pack,
			Command:         hubStoreWriteCommand,
			SkipHeredocData: true,
			Reason:          hubStoreWriteReason,
		},
	}
}

// --- end D-GR ------------------------------------------------------------
