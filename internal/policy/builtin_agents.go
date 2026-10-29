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
// names) whose value matches val, in the forms bash, zsh, fish, PowerShell and
// cmd use. It is anchored at a command start, so `git commit -m "TQ_MODE=off"`
// is data. val takes the form, because what ends a value differs between
// shells (agModeLoose uses that); agAny wraps a value regex that does not care.
func agAssign(vars string, val func(form int) string) string {
	v := `(?:` + vars + `)`
	return `(?:` +
		// VAR=x cmd, export VAR=x, env [-i] VAR=x cmd, declare -x VAR=x
		agCmdStart + `(?:(?:export|declare|typeset|readonly|local)\s+(?:-\w+\s+)*|env\s+(?:-{1,2}[\w-]+(?:=\S*)?\s+)*)?(?:[A-Za-z_]\w*=\S*\s+)*` + v + `=` + val(formSh) +
		// PowerShell: $env:VAR = x, ${env:VAR} = x
		`|` + agCmdStart + `\$\{?env:` + v + `\}?\s*=\s*` + val(formPwsh) +
		// fish: set -x VAR x (not set -e)
		`|` + agCmdStart + `set\s+(?:-[a-df-zA-Z]+\s+|--(?:export|global|universal|local|path)\s+)*` + v + `\s+` + val(formFish) +
		// cmd: set VAR=x, set "VAR=x" (unanchored: it is usually inside cmd /c "...")
		`|\bset\s+"?` + v + `=` + val(formCmd) +
		// PowerShell providers and .NET
		`|\b(?:set-item|si|new-item|ni)\s+(?:-path\s+)?["']?env:[\\/]?` + v + `\b` +
		`|\[(?:system\.)?environment\]::setenvironmentvariable\(\s*["']` + v + `["']` +
		`)`
}

// The assignment forms agAssign passes to its value function.
const (
	formSh = iota
	formPwsh
	formFish
	formCmd
)

// agAny is a value regex that is the same in every form.
func agAny(val string) func(int) string { return func(int) string { return val } }

// agNotSafe matches the text after an assignment unless that text starts with
// one of words followed by a safe end. It is built from a trie of words, since
// RE2 has no lookahead. At each proper prefix of a word the text either stops
// (an empty or partial value), continues with the next character of a word,
// or leaves the trie with any other character. After a whole word, pad and
// then a character outside term (and outside the word's own continuations)
// leaves it too: default'x', 'strict'.Replace(..), an empty quote pair before off.
// rootSkip lists characters that may come before the value (whitespace the
// form already allows there), so they never count as leaving at the root.
// Matching is case-sensitive: Default is not a safe value.
func agNotSafe(words []string, rootSkip, pad, term string) string {
	set := map[string]bool{}
	for _, w := range words {
		set[w] = true
	}
	var node func(p string) string
	node = func(p string) string {
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
		var alts []string
		children := ""
		for _, c := range cs {
			alts = append(alts, regexp.QuoteMeta(c)+node(p+c))
			children += regexp.QuoteMeta(c)
		}
		if set[p] {
			alts = append(alts, pad+`[^`+term+children+`]`)
		} else {
			skip := ""
			if p == "" {
				skip = rootSkip
			}
			alts = append(alts, `$`, `[^`+children+skip+`]`)
		}
		return `(?:` + strings.Join(alts, "|") + `)`
	}
	return `(?-i:` + node("") + `)`
}

// agModeLoose is the value of a TQ_MODE assignment unless it is exactly
// default or strict (bare, '...' or "...") followed by the end of the word or
// statement. Everything else is denied (fail closed): the empty value, an
// empty quoted token joined to a word (to the shell, empty quotes before off give off), a
// PowerShell expression ('strict' -replace .., ('off'), "$x", 'a'+'b'), $null,
// a variable. Nothing is ever cut from the command, so a safe assignment can
// not hide what follows it (round 0's Except did).
func agModeLoose(form int) string {
	safe := []string{"default", "strict", "'default'", "'strict'", `"default"`, `"strict"`}
	switch form {
	case formPwsh:
		// A statement ends at ; | ) } & # or a line end. Anything else after
		// the literal (an operator, a method call) makes it an expression.
		return agNotSafe(safe, `\s`, `[ \t]*`, ` \t\r\n;|)}&#`)
	case formFish:
		return agNotSafe(safe, `\s`, ``, `\s;&|)`+bt)
	case formCmd:
		// set "TQ_MODE=strict": the closing quote ends the value.
		safe = append(safe, `default"`, `strict"`)
		return agNotSafe(safe, ``, ``, `\s;&|)`)
	default:
		return agNotSafe(safe, ``, ``, `\s;&|)`+bt)
	}
}

// agUnset matches clearing one of vars: env -u, unset, fish set -e,
// Remove-Item Env:.
func agUnset(vars string) string {
	v := `(?:` + vars + `)`
	return `(?:` +
		`\benv\s+(?:-{1,2}[\w-]+(?:=\S*)?\s+)*?(?:-u\s*|--unset[=\s])["']?` + v + `\b` +
		`|` + agCmdStart + `unset\s+(?:-\w+\s+)*(?:[A-Za-z_]\w*\s+)*` + v + `\b` +
		`|` + agCmdStart + `set\s+(?:-\w+\s+)*(?:-\w*e\w*|--erase)\s+(?:-\w+\s+)*` + v + `\b` +
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
		// TQ_MODE to anything but default/strict, or cleared (that drops a
		// strict launch layer), TQ_ENABLED=0, Claude's simple/safe modes.
		agAssign(`TQ_MODE`, agModeLoose),
		agUnset(`TQ_MODE`),
		agAssign(`TQ_ENABLED`, agAny(`["']?0\b`)),
		agAssign(`CLAUDE_CODE_SIMPLE|CLAUDE_CODE_SAFE_MODE`, agAny(`["']?(?:1|true)\b`)),
		// The agent config homes: any reassignment or clearing.
		agAssign(`CLAUDE_CONFIG_DIR|CODEX_HOME`, agAny(``)),
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
