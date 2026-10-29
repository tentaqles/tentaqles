package policy

import (
	"regexp"
	"sort"
	"strings"
)

// Owned by WS2 (spec E §9, §17): the nested-agent rule.

// NestedAgentRuleID is the id of the rule below (guardcfg.NestedAgentID names
// it too; it is self-protection, a locked category).
const NestedAgentRuleID = "tq/nested-agent-loosened"

// Pieces of the nested-agent regex. Like every shell rule they are
// heuristics: sh -c, scripts written to disk, aliases and variable expansion
// evade them (spec E §9 "Limits"); the binaries' own agent-marker refusals are
// the second line.
const (
	// agCmdStart is the start of a command: the input start, a separator, a
	// subshell or a command substitution. Text inside a quoted argument (a
	// commit message) never starts a command.
	agCmdStart = `(?:^|[;&|\n(` + bt + `]|\$\()\s*`
	// agPrefix is what may come between a command start and the program:
	// env-style wrappers with their flags, and VAR=value prefixes.
	agPrefix = `(?:(?:env|nohup|time|command|exec|sudo|start)\s+(?:-{1,2}[\w-]+(?:=\S*)?\s+)*)*(?:[A-Za-z_]\w*=\S*\s+)*`
	// agPos is where an agent program starts: a command start (with its
	// prefix), or the argument after `--` (tq run <ws> -- claude).
	agPos = `(?:` + agCmdStart + agPrefix + `|\s--\s+)`
	// agProg opens a program token: an optional PowerShell call operator, an
	// optional quote and an optional directory.
	agProg = `(?:&\s*)?["']?(?:[^\s"';&|]*[/\\])?`
	// agEnd closes a program token.
	agEnd = `(?:\.exe|\.cmd|\.ps1)?["']?(?:\s|$)`
	// agVal opens a flag's value: `=` or whitespace, then an optional quote.
	agVal = `(?:=|\s+)["']?`

	agClaude = agPos + agProg + `claude` + agEnd
	agCodex  = agPos + agProg + `codex` + agEnd
	agTq     = agCmdStart + agPrefix + agProg + `tq` + agEnd
)

// agAssign matches an assignment of the variable vars (an alternation of
// names) followed by val, in the forms bash, zsh, fish, PowerShell and cmd
// use. It is anchored at a command start, so `git commit -m "TQ_MODE=off"`
// is data.
func agAssign(vars, val string) string {
	v := `(?:` + vars + `)`
	return `(?:` +
		// VAR=x cmd, export VAR=x, env [-i] VAR=x cmd, declare -x VAR=x
		agCmdStart + `(?:(?:export|declare|typeset|readonly|local)\s+(?:-\w+\s+)*|env\s+(?:-{1,2}[\w-]+(?:=\S*)?\s+)*)?(?:[A-Za-z_]\w*=\S*\s+)*` + v + `=` + val +
		// PowerShell: $env:VAR = x, ${env:VAR} = x
		`|` + agCmdStart + `\$\{?env:` + v + `\}?\s*=\s*` + val +
		// fish: set -x VAR x (not set -e)
		`|` + agCmdStart + `set\s+(?:-[a-df-zA-Z]+\s+|--(?:export|global|universal|local|path)\s+)*` + v + `\s+` + val +
		// cmd: set VAR=x, set "VAR=x" (unanchored: it is usually inside cmd /c "...")
		`|\bset\s+"?` + v + `=` + val +
		// PowerShell providers and .NET
		`|\b(?:set-item|si|new-item|ni)\s+(?:-path\s+)?["']?env:[\\/]?` + v + `\b` +
		`|\[(?:system\.)?environment\]::setenvironmentvariable\(\s*["']` + v + `["']` +
		`)`
}

// agNotWords matches a non-empty string over the characters allowed by the
// negated class body neg (the part after `[^`) that is not one of words. It is
// built from a trie of words, since RE2 has no lookahead: at each prefix of a
// word, either the string stops (when the prefix is not itself a word), or
// continues with the next character of a word, or leaves the trie with any
// other allowed character.
func agNotWords(words []string, neg string) string {
	set := map[string]bool{}
	for _, w := range words {
		set[w] = true
	}
	var node func(p string) string
	node = func(p string) string {
		var alts []string
		if p != "" && !set[p] {
			alts = append(alts, ``)
		}
		next := map[byte]bool{}
		for w := range set {
			if len(w) > len(p) && strings.HasPrefix(w, p) {
				next[w[len(p)]] = true
			}
		}
		var cs []string
		for c := range next {
			cs = append(cs, string(c))
		}
		sort.Strings(cs)
		other := neg
		for _, c := range cs {
			alts = append(alts, regexp.QuoteMeta(c)+node(p+c))
			other += regexp.QuoteMeta(c)
		}
		alts = append(alts, `[^`+other+`][^`+neg+`]*`)
		return `(?:` + strings.Join(alts, "|") + `)`
	}
	return node("")
}

// agModeLoose is a TQ_MODE value other than default or strict, taken whole:
// quoted ("..." or '...', up to the closing quote) or a bare word up to the
// next separator. Because it matches the whole value, nothing is ever cut
// from the command (round 0 used an Except that ate the separator after
// TQ_MODE=default and so hid the loosened command behind it).
func agModeLoose() string {
	safe := []string{"default", "strict"}
	bare := `\s"';&|()<>` + bt
	return `(?:` +
		`"` + agNotWords(safe, `"`) + `(?:"|$)` +
		`|'` + agNotWords(safe, `'`) + `(?:'|$)` +
		`|` + agNotWords(safe, bare) + `(?:[` + bare + `]|$)` +
		// default"x" is the word defaultx to a shell
		`|(?:default|strict)["'][^\s;&|()<>` + bt + `]` +
		`)`
}

// agUnset matches clearing one of vars: env -u, unset, fish set -e,
// Remove-Item Env:.
func agUnset(vars string) string {
	v := `(?:` + vars + `)`
	return `(?:` +
		`\benv\s+(?:-{1,2}[\w-]+(?:=\S*)?\s+)*?(?:-u\s*|--unset[=\s])["']?` + v + `\b` +
		`|` + agCmdStart + `unset\s+(?:-\w+\s+)*(?:[A-Za-z_]\w*\s+)*` + v + `\b` +
		`|` + agCmdStart + `set\s+(?:-\w+\s+)*-\w*e\w*\s+(?:-\w+\s+)*` + v + `\b` +
		`|\b(?:remove-item|ri|rm|del|erase|clear-item|cli)\s+(?:-\w+\s+)*["']?env:[\\/]?` + v + `\b` +
		`)`
}

// agFlag matches flag anywhere in the arguments of prog (which ends on the
// whitespace after the program name).
func agFlag(prog, flag string) string { return prog + `(?:` + seg + `\s)?` + flag }

// agLoosenedMode is a --tq-mode value looser than default (or off).
const agLoosenedMode = `--tq-mode` + agVal + `(?:off|relaxed|custom)\b`

// agSettingsLoose is a claude --settings value that switches tq off: inline
// JSON that disables the tentaqles plugin, all hooks, or empties the hooks,
// or a settings file (its content cannot be read by a regex, so a file fails
// closed: pass the settings inline, or run the command yourself).
const agSettingsLoose = `--settings` + agVal + `(?:` +
	`[^\n]*disableAllHooks` +
	`|[^\n]*["']?hooks["']?\s*:\s*\{\s*\}` +
	`|[^\n]*enabledPlugins[^\n]*tentaqles[^\n]*\bfalse\b` +
	`|[^\s{'"])`

func nestedAgentCommand() string {
	alts := []string{
		// TQ_MODE to anything but default/strict, TQ_ENABLED=0, Claude's
		// simple/safe modes.
		agAssign(`TQ_MODE`, agModeLoose()),
		agAssign(`TQ_ENABLED`, `["']?0\b`),
		agAssign(`CLAUDE_CODE_SIMPLE|CLAUDE_CODE_SAFE_MODE`, `["']?(?:1|true)\b`),
		// The agent config homes: any reassignment or clearing.
		agAssign(`CLAUDE_CONFIG_DIR|CODEX_HOME`, ``),
		agUnset(`CLAUDE_CONFIG_DIR|CODEX_HOME`),
		// Off launchers and tq verbs.
		agPos + agProg + `(?:claude|codex)-off` + agEnd,
		agTq + `\s*off\b`,
		agFlag(agTq+`\s*(?:run|env)\b`, `--off\b`),
		agFlag(agTq+`\s*run\b`, agLoosenedMode),
		// claude with loosened flags
		agFlag(agClaude, agLoosenedMode),
		agFlag(agClaude, `--(?:allow-)?dangerously-skip-permissions\b`),
		agFlag(agClaude, `--permission-mode`+agVal+`(?:bypassPermissions|auto|dontAsk)\b`),
		agFlag(agClaude, `--(?:bare|safe-mode)\b`),
		agFlag(agClaude, `--setting-sources`+agVal+`(?:project|local)(?:,(?:project|local))*["']?(?:\s|$|[;&|])`),
		agFlag(agClaude, agSettingsLoose),
		// codex with loosened flags
		agFlag(agCodex, `(?:--dangerously-bypass-approvals-and-sandbox|--yolo|--ignore-rules|--dangerously-bypass-hook-trust)\b`),
		agFlag(agCodex, `(?:-a|--ask-for-approval)`+agVal+`never\b`),
		agFlag(agCodex, `(?:-s|--sandbox)`+agVal+`danger-full-access\b`),
		agFlag(agCodex, `--disable`+agVal+`hooks\b`),
		agFlag(agCodex, `(?:-c|--config)`+agVal+`features\.hooks\s*=\s*["']?false\b`),
	}
	return strings.Join(alts, "|")
}

func init() {
	RegisterBuiltin(Rule{
		ID: NestedAgentRuleID, Action: Deny,
		// Setting TQ_MODE to default or strict never loosens: the TQ_MODE
		// alternative matches only other values. There is no Except: cutting
		// text out of the command can join or hide the command after it.
		Command:         nestedAgentCommand(),
		SkipHeredocData: true,
		Reason:          "an agent may not start another agent in off mode or with loosened settings (TQ_MODE, claude-off, --dangerously-skip-permissions, a cleared config home, ...); run it yourself in your own terminal",
	})
}
