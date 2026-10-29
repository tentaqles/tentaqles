package policy

import (
	"os"
	"regexp"
	"strings"
)

// packRules are the static regex rules of the packs (spec C §2.10). Each
// sets Pack: the hook drops it while that pack is not enabled. One id may
// appear once per pack (tq/hub-store-write is listed for cloud and for db).
// Spec D's guard-hardening and iac-policy workstreams append their blocks
// here.
func packRules() []Rule {
	var rules []Rule
	rules = append(rules, hubStoreWriteRules("cloud")...) // D-GR
	rules = append(rules, hubStoreWriteRules("db")...)    // D-GR
	rules = append(rules, planRules()...)                 // D-CP
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

// --- spec D §16, D-CP: tq/plan-read, tq/plan-print, tq/plan-commit ----------

// planStore is the cloud pack's plan store, $TQ_HOME/packs/<ws>/cloud/plans/,
// in the spellings a command may use.
const planStore = `(\.tentaqles|\$\{?TQ_HOME\}?|\$env:TQ_HOME|%TQ_HOME%)[/\\]+packs[/\\]+[^/\\\s]+[/\\]+cloud[/\\]+plans([/\\]|\b)`

// planFile is a saved plan, a state file (and its backups), the plan store,
// or a CDK asset manifest.
const planFile = `\.tfplan\b|\.tfstate\b|` + planStore + `|cdk\.out[/\\]+(\S*[/\\])?[^/\\\s]*\.assets\.json\b`

// planReadVerbs print a file: readVerbs, plus jq and `terraform show <plan>`
// (terraform.exe and a quoted path to it too).
const planReadVerbs = `(` + readVerbs + `|\bjq\b|\b(terraform|tofu)(\.exe)?["']?` + iacFlags + `\s+show\b)`

// tqHomeStore is the plan store under a custom TQ_HOME, as a path regex
// (slash form, for the file tools) and a command regex (either separator).
// Both are "" when TQ_HOME is unset: the ~/.tentaqles and $TQ_HOME
// spellings of planStore cover the default home.
func tqHomeStore() (pathRe, cmdRe string) {
	home := strings.TrimRight(NormalizePath(os.Getenv("TQ_HOME")), "/")
	if home == "" {
		return "", ""
	}
	var segs []string
	for _, s := range strings.Split(home, "/") {
		if s != "" {
			segs = append(segs, regexp.QuoteMeta(s))
		}
	}
	if len(segs) == 0 {
		return "", ""
	}
	lead := ""
	if strings.HasPrefix(home, "/") {
		lead = "/"
	}
	pathRe = `(^|/)` + lead + strings.Join(segs, `/+`) + `/+packs/+[^/]+/+cloud/+plans(/|$)`
	cmdRe = strings.Join(segs, `[/\\]+`) + `[/\\]+packs[/\\]+[^/\\\s]+[/\\]+cloud[/\\]+plans([/\\]|\b)`
	return pathRe, cmdRe
}

// gitAddVerb is `git [flags] add|stage`.
const gitAddVerb = `\bgit(\.exe)?["']?` + iacFlags + `\s+(add|stage)\b`

// planCommitTarget is what must never be staged: plans, state, the
// .terraform/ working directory (not .terraform.lock.hcl), the pack store,
// and database dumps.
const planCommitTarget = `(\.tfplan\b|\.tfstate\b|(^|[\s"'/\\=])\.terraform([/\\]|["']|\s|$)|(\.tentaqles|\$\{?TQ_HOME\}?|\$env:TQ_HOME|%TQ_HOME%)[/\\]+packs\b|\.(dump|pgdump|bacpac)\b|\btq_drill_)`

func planRules() []Rule {
	readPath := `(^|/)[^/]*\.tfplan$|(^|/)[^/]*\.tfstate(\.[^/]*)?$|(^|/)\.tentaqles/+packs/+[^/]+/+cloud/+plans(/|$)|(^|/)cdk\.out/(.*/)?[^/]*\.assets\.json$`
	readCmd, commitTarget := planFile, planCommitTarget
	if p, c := tqHomeStore(); p != "" {
		readPath += `|` + p
		readCmd += `|` + c
		commitTarget = `(` + planCommitTarget + `|` + c + `)`
	}
	return []Rule{
		{
			ID: "tq/plan-read", Action: Deny, Pack: "cloud",
			Tool:   "Read|Grep|NotebookRead",
			Path:   readPath,
			Reason: "saved plans and state hold values in clear; they are never read into the conversation. Use `tq pack cloud show <plan-id>` for the redacted summary",
		},
		{
			ID: "tq/plan-read", Action: Deny, Pack: "cloud",
			Command:         planReadVerbs + seg + `(` + readCmd + `)`,
			SkipHeredocData: true,
			Reason:          "saved plans and state hold values in clear; they are never printed into the conversation. Use `tq pack cloud show <plan-id>` for the redacted summary",
		},
		{
			ID: "tq/plan-print", Action: Ask, Pack: "cloud",
			Command: `\b(terraform|tofu)(\.exe)?["']?` + iacFlags + `\s+(show|output|state\s+(pull|show))\b|\bpulumi(\.exe)?["']?` + seg + `\bstack\s+(output|export)\b` + seg + `--show-secrets\b`,
			Reason:  "this prints state or outputs, which can hold secrets, into the transcript; use `tq pack cloud show <plan-id>` for the redacted plan summary, or confirm",
		},
		{
			ID: "tq/plan-commit", Action: Deny, Pack: "cloud",
			Command: gitAddVerb + seg + commitTarget,
			Reason:  "plans, state, .terraform/, the pack store and database dumps never go into git (they hold values in clear); add them to .gitignore. `git add -A` is covered by .gitignore and the cloud/no-plan-files check",
		},
	}
}

// --- end D-CP ---
