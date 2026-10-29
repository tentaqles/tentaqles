package policy

import (
	"os"
	"regexp"
	"strings"
)

// packRules are the static regex rules of the packs (spec C §2.10). Each
// sets Pack: the hook drops it while that pack is not enabled. One id may
// appear once per pack. Spec D's workstreams append their blocks here.
//
// D-GR's tq/hub-store-write entries (one file and one shell entry for cloud
// and for db) were dropped in integration round 3: the core
// tq/hub-store-write (builtin_framework.go, no Pack) and the hook-time
// state-dir check deny every write they denied, under every spelling of
// $TQ_HOME, whether or not a pack is on (cmd's
// TestHubStoreWriteCoreCoversDroppedDGRForms).
func packRules() []Rule {
	var rules []Rule
	rules = append(rules, planRules()...) // D-CP
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
