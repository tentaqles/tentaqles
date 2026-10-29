package policy

// packRules are the static regex rules of the packs (spec C §2.10). Each
// sets Pack: the hook drops it while that pack is not enabled. One id may
// appear once per pack (tq/hub-store-write is listed for cloud and for db).
// Spec D's guard-hardening and iac-policy workstreams append their blocks
// here.
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

// planReadVerbs print a file: readVerbs, plus jq and `terraform show <plan>`.
const planReadVerbs = `(` + readVerbs + `|\bjq\b|\b(terraform|tofu)\b` + iacFlags + `\s+show\b)`

// gitAddVerb is `git [flags] add|stage`.
const gitAddVerb = `\bgit(\.exe)?["']?` + iacFlags + `\s+(add|stage)\b`

// planCommitTarget is what must never be staged: plans, state, the
// .terraform/ working directory (not .terraform.lock.hcl), the pack store,
// and database dumps.
const planCommitTarget = `(\.tfplan\b|\.tfstate\b|(^|[\s"'/\\=])\.terraform([/\\]|["']|\s|$)|(\.tentaqles|\$\{?TQ_HOME\}?|\$env:TQ_HOME|%TQ_HOME%)[/\\]+packs\b|\.(dump|pgdump|bacpac)\b|\btq_drill_)`

func planRules() []Rule {
	return []Rule{
		{
			ID: "tq/plan-read", Action: Deny, Pack: "cloud",
			Tool:   "Read|Grep|NotebookRead",
			Path:   `(^|/)[^/]*\.tfplan$|(^|/)[^/]*\.tfstate(\.[^/]*)?$|(^|/)\.tentaqles/+packs/+[^/]+/+cloud/+plans(/|$)|(^|/)cdk\.out/(.*/)?[^/]*\.assets\.json$`,
			Reason: "saved plans and state hold values in clear; they are never read into the conversation. Use `tq pack cloud show <plan-id>` for the redacted summary",
		},
		{
			ID: "tq/plan-read", Action: Deny, Pack: "cloud",
			Command:         planReadVerbs + seg + `(` + planFile + `)`,
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
			Command: gitAddVerb + seg + planCommitTarget,
			Reason:  "plans, state, .terraform/, the pack store and database dumps never go into git (they hold values in clear); add them to .gitignore. `git add -A` is covered by .gitignore and the cloud/no-plan-files check",
		},
	}
}

// --- end D-CP ---
