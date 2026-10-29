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

// hubStoreBase matches a shell reference to $TQ_HOME/hub or $TQ_HOME/packs:
// the default home (~/.tentaqles, any prefix) or the TQ_HOME variable in
// sh, PowerShell or cmd form. A static rule can't know a custom TQ_HOME
// spelled as a literal path.
const hubStoreBase = `(?:\.tentaqles|\$TQ_HOME|\$\{TQ_HOME\}|\$env:TQ_HOME|%TQ_HOME%)[/\\]+(?:hub|packs)`

// hubStoreRef is hubStoreBase ending at a word boundary (`hubble` is not hub).
const hubStoreRef = hubStoreBase + `(?:[/\\"'\s;&|)]|$)`

// hubStoreArg is the part of one shell word before hubStoreRef; a quoted
// word may hold spaces.
const hubStoreArg = `(?:"[^"\n]*|'[^'\n]*|[^\s;&|"'<>]*)`

// hubStoreDest is a whole word naming the store that ends the arguments:
// only flags, redirects or a comment may follow it (builtin.go destTail).
const hubStoreDest = hubStoreArg + hubStoreBase + `(?:[/\\][^\s;&|"']*)?` + destTail

// cmdAt puts a verb at a word start, so `grep -ri` is not `ri`, and ends it
// at a word boundary (`cp` is not `cpi`); the space after it is left for
// the flag patterns that follow. A path prefix (/bin/cp, C:\x\rm.exe) counts.
func cmdAt(verbs string) string { return `(?:^|[\s;&|(/\\])(?:` + verbs + `)(?:\.exe)?\b` }

// hubStoreWriteCommand: sqlite3 on the store, a redirect into it, tee, the
// destination of cp/mv/Copy-Item/Move-Item/Expand-Archive (last word, -t,
// -Destination), dd of=, sed -i / perl -i, download and unpack targets, an
// interpreter one-liner or [IO.File]::Write* naming it, and the writers and
// removers that name it.
var hubStoreWriteCommand = `\bsqlite3(?:\.exe)?\b` + seg + hubStoreRef +
	`|>\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`tee`) + seg + hubStoreRef +
	`|` + cmdAt(`cp|mv|copy|move|rsync|install|Copy-Item|Move-Item|cpi|mi|Expand-Archive`) + seg + `\s` + hubStoreDest +
	`|` + cmdAt(`cp|mv|install`) + seg + `\s(?:-t\s*|--target-directory[=\s]\s*)` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`copy|move|Copy-Item|Move-Item|cpi|mi|Expand-Archive`) + seg + `\s-d[a-z]*[:\s]\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`dd`) + seg + `\sof=` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`sed|perl`) + seg + `\s(?:-[a-z]*i|--in-place)` + seg + hubStoreRef +
	`|` + cmdAt(`curl`) + seg + `\s(?:-[a-z]*o|--output(?:-dir)?)[=\s]\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`wget`) + seg + `\s(?:-[a-z]*[op]|--output-document|--output-file|--directory-prefix)[=\s]\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`Invoke-WebRequest|iwr|Invoke-RestMethod|irm`) + seg + `\s-o[a-z]*[:\s]\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`tar`) + seg + `\s(?:(?-i:-C)\s*|--directory[=\s]\s*)` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`unzip`) + seg + `\s-d\s*` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`7z|7za`) + seg + `\s-o` + hubStoreArg + hubStoreRef +
	`|` + cmdAt(`python[0-9.]*|pythonw|py|perl|ruby|node|deno|bun|php`) + `[^;&|\n"']*(?:"[^"]*` + hubStoreRef + `|'[^']*` + hubStoreRef + `)` +
	`|\[(?:System\.)?IO\.File\]::(?:Write|Append|Create|Delete|Move|Copy|Replace|OpenWrite|SetAttributes)\w*\s*\([^)]*` + hubStoreRef +
	`|` + cmdAt(`rm|rmdir|del|erase|rd|touch|truncate|ln|chmod|mkdir|md|Remove-Item|ri|Set-Content|sc|Add-Content|ac|Out-File|New-Item|ni|Clear-Content|clc|Rename-Item|rni|ren`) + seg + hubStoreRef

// hubStorePath is the file-tool form: `//` collapses, and any `..` under
// .tentaqles is denied, since the path is not cleaned before matching.
const hubStorePath = `(^|/)\.tentaqles/+(?:(?:hub|packs)(?:/|$)|(?:.*/)?\.\.(?:/|$))`

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
			Path:   hubStorePath,
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
