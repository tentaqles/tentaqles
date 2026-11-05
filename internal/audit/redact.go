package audit

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tentaqles/tentaqles/internal/secrets"
)

// The denial log records the command a deny refused (PRD v2 §6.1 step 5,
// §7.3), redacted so the log never becomes the place a secret ends up
// (ADR-0006). The rule is structural, not a list of secret shapes: a word
// is kept only when its position says it cannot be a value.
//
// RedactCommand splits the line into words and operators (quote-aware, the
// same separators a shell uses: whitespace, ; | & ( ) < > ` $( and a
// newline) and keeps:
//
//   - operators;
//   - the command word of each segment (the first word after an operator,
//     after NAME=value assignments and wrappers such as sudo, env, xargs),
//     when it looks like a command name (letters, digits, . _ + - and a
//     path), is at most 40 characters and does not mix an upper-case letter
//     with a digit (a token, not a command);
//   - flag names: `--name` whole, `--name=` with its value masked, a
//     two-character `-x`; a longer single-dash word only when it is a known
//     option (-Recurse, -auto-approve, ...), otherwise its first two
//     characters (`-pS3cret` is `-p…`);
//   - the name of an assignment (`NAME=…`, `$env:NAME = …`);
//   - variable references ($NAME, ${NAME}, $env:NAME);
//   - a short list of subcommand words (push, s3, run-instances, ...) only
//     in the subcommand position: directly after the command word or
//     another kept subcommand, before any flag, and never for echo, printf
//     and the like, whose arguments are data;
//   - the words the caller names in keep (the path or token the deny
//     matched).
//
// Every other word is …, a quoted word is "…" (its quote kept), here-doc and
// here-string bodies are one placeholder line, every control character
// (C0 but the newline, DEL, C1) is dropped, and the result is cut to
// MaxCommandRunes. A secret can only survive as a command word without a
// digit-and-capital mix, or as one of the listed subcommand words in the
// subcommand position.
func RedactCommand(cmd string, keep ...string) string {
	keepSet := map[string]bool{}
	for _, k := range keep {
		for _, w := range strings.Fields(k) {
			keepSet[w] = true
		}
	}
	var out []string
	for _, line := range strings.Split(dropHeredocs(stripControl(cmd)), "\n") {
		if line == heredocPlaceholder || line == hereStringPlaceholder {
			out = append(out, line)
			continue
		}
		out = append(out, redactLine(line, keepSet))
	}
	return truncate(strings.TrimSpace(strings.Join(out, "\n")), MaxCommandRunes)
}

const (
	heredocPlaceholder    = "<heredoc body dropped>"
	hereStringPlaceholder = "<here-string body dropped>"
)

// RedactText masks secret-shaped spans, drops control characters and cuts
// the text; it is applied to a denial's reason and subject, which tq writes
// itself (rule text, a matched path, a manifest pattern).
func RedactText(s string) string {
	return truncate(secrets.Redact(stripControl(s)), MaxReasonRunes)
}

// Limits of what one denial line carries.
const (
	MaxCommandRunes = 240
	MaxReasonRunes  = 600
)

// stripControl drops every C0 control character but the newline and the
// tab (a tab becomes a space), DEL and every C1 control character, so a
// logged string can carry no terminal escape sequence.
func stripControl(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
			return -1
		}
		return r
	}, s)
}

// token is one lexical unit of a line: an operator or a word.
type token struct {
	text   string
	op     bool
	quoted rune // the first quote character of a word that contains one
}

// isQuote reports the quote characters a word may contain: ASCII and the
// typographic ones a pasted command carries.
func isQuote(r rune) bool {
	switch r {
	case '\'', '"', '‘', '’', '“', '”', '«', '»':
		return true
	}
	return false
}

// closingQuote is the rune that ends a quoted span opened by r.
func closingQuote(r rune) rune {
	switch r {
	case '‘':
		return '’'
	case '“':
		return '”'
	case '«':
		return '»'
	}
	return r
}

// tokenize splits a line into words and operators, quote-aware.
func tokenize(line string) []token {
	var toks []token
	var cur strings.Builder
	var quoted rune
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, token{text: cur.String(), quoted: quoted})
			cur.Reset()
			quoted = 0
		}
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case isQuote(r):
			if quoted == 0 {
				quoted = r
			}
			end := closingQuote(r)
			cur.WriteRune(r)
			j := i + 1
			for ; j < len(rs); j++ {
				if r == '"' && rs[j] == '\\' && j+1 < len(rs) {
					cur.WriteRune(rs[j])
					j++
					cur.WriteRune(rs[j])
					continue
				}
				cur.WriteRune(rs[j])
				if rs[j] == end || (r == '\'' && rs[j] == '’') {
					break
				}
			}
			i = j
		case unicode.IsSpace(r):
			flush()
		case r == '$' && i+1 < len(rs) && rs[i+1] == '(':
			flush()
			toks = append(toks, token{text: "$(", op: true})
			i++
		case strings.ContainsRune(";|&()<>`", r):
			flush()
			op := string(r)
			for i+1 < len(rs) && strings.ContainsRune("|&<>", rs[i+1]) && len(op) < 3 {
				op += string(rs[i+1])
				i++
			}
			toks = append(toks, token{text: op, op: true})
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

var (
	// An assignment name is conventional: all capitals (AWS_ACCESS_KEY_ID)
	// or all lower case; a mixed-case word ending in = is a base64 value.
	assignRe   = regexp.MustCompile(`^([A-Z_][A-Z0-9_]*|[a-z_][a-z0-9_]*)=`)
	psVarRe    = regexp.MustCompile(`^\$(?:env:)?[A-Za-z_][A-Za-z0-9_]*$`)
	psAssignRe = regexp.MustCompile(`^(\$(?:env:)?[A-Za-z_][A-Za-z0-9_]*)=`)
	varRefRe   = regexp.MustCompile(`^\$(?:\{[A-Za-z_][A-Za-z0-9_]*\}|(?:env:)?[A-Za-z_][A-Za-z0-9_]*)$`)
	verbRe     = regexp.MustCompile(`^[A-Za-z0-9._+~/\\:-]+$`)
	longFlagRe = regexp.MustCompile(`^--[A-Za-z][A-Za-z0-9-]*$`)
	shortFlag  = regexp.MustCompile(`^-[A-Za-z0-9]$`)
	upperRe    = regexp.MustCompile(`[A-Z]`)
	digitRe    = regexp.MustCompile(`[0-9]`)
)

// redactLine applies the structural rule to one line.
func redactLine(line string, keep map[string]bool) string {
	toks := tokenize(line)
	var out []string
	expectVerb := true  // the next word is a command word
	afterRedir := false // the next word is a redirect target
	subPos := false     // the next positional is in the subcommand position
	psAssign := false   // the previous words were `$x =`
	for i, t := range toks {
		if t.op {
			out = append(out, t.text)
			switch {
			case strings.ContainsAny(t.text, "<>") && t.text != "<(" && t.text != ">(":
				afterRedir = true
			default:
				expectVerb, subPos, psAssign = true, false, false
			}
			continue
		}
		w := t.text
		switch {
		case keep[w]:
			out = append(out, w)
			afterRedir = false
			if expectVerb {
				expectVerb, subPos = false, true
			}
			continue
		case afterRedir:
			out = append(out, masked(t))
			afterRedir = false
			continue
		case t.quoted != 0:
			out = append(out, masked(t))
			expectVerb, subPos = false, false
			continue
		}
		if expectVerb {
			switch {
			case assignRe.MatchString(w):
				out = append(out, assignRe.FindStringSubmatch(w)[1]+"=…")
				continue
			case psAssignRe.MatchString(w):
				out = append(out, psAssignRe.FindStringSubmatch(w)[1]+"=…")
				continue
			case psVarRe.MatchString(w) && i+1 < len(toks) && toks[i+1].text == "=":
				out = append(out, w)
				psAssign = true
				continue
			case w == "=" && psAssign:
				out = append(out, w)
				continue
			case psAssign:
				// $x = <value>: a cmdlet (Verb-Noun) is a command word,
				// anything else is the value.
				psAssign = false
				if cmdletRe.MatchString(w) {
					out = append(out, w)
					expectVerb, subPos = false, true
				} else {
					out = append(out, "…")
					expectVerb = false
				}
				continue
			case isVerb(w):
				out = append(out, w)
				if wrappers[strings.ToLower(baseName(w))] {
					continue // sudo/env/xargs ...: the command word follows
				}
				expectVerb = false
				subPos = !dataVerbs[strings.ToLower(baseName(w))]
				continue
			}
			out = append(out, "…")
			expectVerb = false
			continue
		}
		if strings.HasPrefix(w, "-") && len(w) > 1 {
			subPos = false
			out = append(out, redactFlag(w))
			continue
		}
		if varRefRe.MatchString(w) {
			out = append(out, w)
			subPos = false
			continue
		}
		if subPos && subcommands[strings.ToLower(w)] {
			out = append(out, w)
			continue
		}
		subPos = false
		out = append(out, "…")
	}
	return strings.Join(out, " ")
}

var cmdletRe = regexp.MustCompile(`^[A-Z][A-Za-z]+-[A-Z][A-Za-z]+$`)

func masked(t token) string {
	if t.quoted != 0 && strings.IndexRune(t.text, t.quoted) == 0 {
		q := string(t.quoted)
		return q + "…" + string(closingQuote(t.quoted))
	}
	return "…"
}

// isVerb reports whether w can be kept as a command word.
func isVerb(w string) bool {
	if len(w) > 40 || !verbRe.MatchString(w) || strings.Contains(w, "=") {
		return false
	}
	b := baseName(w)
	if b == "" || (upperRe.MatchString(b) && digitRe.MatchString(b)) {
		return false
	}
	return !secrets.Contains(w)
}

func baseName(w string) string {
	if i := strings.LastIndexAny(w, `/\`); i >= 0 {
		w = w[i+1:]
	}
	return w
}

// redactFlag keeps a flag's name and masks any value glued to it.
func redactFlag(w string) string {
	if name, _, ok := strings.Cut(w, "="); ok {
		if longFlagRe.MatchString(name) && !(upperRe.MatchString(name) && digitRe.MatchString(name)) {
			return name + "=…"
		}
		if knownOptions[strings.ToLower(name)] {
			return name + "=…"
		}
		return "…"
	}
	switch {
	case shortFlag.MatchString(w):
		return w
	case longFlagRe.MatchString(w) && !(upperRe.MatchString(w) && digitRe.MatchString(w)):
		return w
	case knownOptions[strings.ToLower(w)]:
		return w
	}
	if r := []rune(w); len(r) >= 2 && (unicode.IsLetter(r[1]) || unicode.IsDigit(r[1])) {
		return string(r[:2]) + "…"
	}
	return "…"
}

// wrappers run the command that follows them.
var wrappers = setOf("sudo", "env", "xargs", "nohup", "time", "exec", "command", "builtin", "nice", "doas", "timeout", "stdbuf", "watch")

// dataVerbs take data, not subcommands: their arguments are never kept.
var dataVerbs = setOf("echo", "printf", "print", "write-output", "write-host", "set", "export", "cat", "type", "base64", "tee",
	"setx", "set-content", "add-content", "out-file", "convertto-securestring", "sshpass", "htpasswd")

// knownOptions are single-dash options that are words, not a flag with a
// glued value (PowerShell parameters, terraform, find, ...).
var knownOptions = setOf(
	"-recurse", "-force", "-path", "-literalpath", "-filter", "-include", "-exclude", "-name", "-value", "-file",
	"-command", "-noprofile", "-nologo", "-noninteractive", "-executionpolicy", "-asplaintext", "-credential",
	"-headers", "-method", "-uri", "-body", "-outfile", "-erroraction", "-confirm", "-whatif", "-encoding",
	"-auto-approve", "-var", "-var-file", "-chdir", "-input", "-lock", "-target", "-out",
	"-type", "-maxdepth", "-mindepth", "-exec", "-delete", "-print", "-iname", "-newer", "-mtime",
	"-string", "-securestring", "-key", "-in", "-out", "-pass", "-aes256", "-salt",
)

// subcommands are the words kept in the subcommand position: what git, gh,
// the cloud CLIs, package managers and tq call their verbs.
var subcommands = setOf(
	// git
	"add", "am", "apply", "archive", "bisect", "blame", "branch", "checkout", "cherry-pick", "clean", "clone",
	"commit", "config", "describe", "diff", "fetch", "grep", "init", "log", "merge", "mv", "pull", "push",
	"rebase", "reflog", "remote", "reset", "restore", "revert", "rm", "show", "stash", "status", "submodule",
	"switch", "tag", "worktree", "origin", "upstream", "main", "master", "head",
	// gh
	"auth", "login", "logout", "api", "pr", "issue", "repo", "run", "workflow", "release", "secret", "gist",
	"create", "list", "view", "edit", "close", "reopen", "delete", "checks", "ready", "token",
	// clouds and infra
	"s3", "s3api", "ec2", "sts", "iam", "lambda", "ecr", "eks", "rds", "ssm", "secretsmanager", "cloudformation",
	"ls", "cp", "sync", "mb", "rb", "describe-instances", "run-instances", "terminate-instances",
	"get-caller-identity", "configure", "get", "set", "account", "group", "vm", "storage", "keyvault", "webapp",
	"functionapp", "aks", "acr", "deployment", "ad", "sp", "compute", "instances", "projects", "container",
	"clusters", "get-credentials", "apply", "plan", "destroy", "import", "output", "state", "validate", "fmt",
	"pods", "deployments", "services", "nodes", "namespaces", "logs", "exec", "describe", "rollout", "scale",
	// package managers and builds
	"install", "uninstall", "update", "upgrade", "publish", "build", "test", "start", "dev", "lint", "ci",
	"version", "audit", "pack", "link", "outdated", "tidy", "vet", "mod", "download",
	// tq
	"allow", "deny", "doctor", "hooks", "verify", "settings", "render", "env", "activate", "migrate",
	"onboard", "setup", "bundle", "providers", "statusline", "worktrees", "dotenv", "claude-hook",
	"pre-tool-use", "session-start", "off", "trust", "--version", "help",
)

func setOf(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

var heredocStartRe = regexp.MustCompile(`<<(-?)\s*(?:'([^'\n]+)'|"([^"\n]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

// dropHeredocs replaces the body of every here-doc and PowerShell
// here-string with one placeholder line, keeping the line that opens it.
func dropHeredocs(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		// PowerShell here-string: a line ending in @' or @" opens it, a line
		// starting with '@ or "@ closes it.
		if strings.HasSuffix(trimmed, "@'") || strings.HasSuffix(trimmed, `@"`) {
			closer := "'@"
			if strings.HasSuffix(trimmed, `@"`) {
				closer = `"@`
			}
			out = append(out, strings.TrimSuffix(strings.TrimSuffix(trimmed, "@'"), `@"`), hereStringPlaceholder)
			j := i + 1
			for ; j < len(lines); j++ {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), closer) {
					break
				}
			}
			i = j
			continue
		}
		// A bash here-string (<<<) is a word, not a here-doc.
		ms := heredocStartRe.FindAllStringSubmatch(strings.ReplaceAll(line, "<<<", "   "), -1)
		out = append(out, line)
		if len(ms) == 0 {
			continue
		}
		// Bodies follow in order, one per here-doc opened on the line.
		j := i + 1
		for _, m := range ms {
			delim := m[2] + m[3] + m[4]
			strip := m[1] == "-"
			for ; j < len(lines); j++ {
				l := lines[j]
				if strip {
					l = strings.TrimLeft(l, "\t")
				}
				if l == delim {
					j++
					break
				}
			}
		}
		if j > i+1 {
			out = append(out, heredocPlaceholder)
		}
		i = j - 1
	}
	return strings.Join(out, "\n")
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
