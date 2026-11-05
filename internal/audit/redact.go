package audit

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tentaqles/tentaqles/internal/secrets"
)

// The denial log records the command a deny refused (PRD v2 §6.1 step 5,
// §7.3), redacted so the log never becomes the place a secret ends up
// (ADR-0006). RedactCommand keeps the shape of the command (the verbs, the
// flags, the paths, the operators) and drops what can carry a value:
//
//  1. here-doc bodies (bash `<<EOF` ... `EOF`, `<<-`, quoted delimiters) and
//     PowerShell here-strings (`@'` ... `'@`, `@"` ... `"@`) become one
//     placeholder line;
//  2. the body of every quoted string ('...' and "...") becomes …;
//  3. the value of a NAME=value assignment, a `$env:NAME = value` /
//     `$x = value` PowerShell assignment, and of a flag whose name says it
//     holds a credential (--password, --token, -H/--header, --auth, ...) and
//     the password of `-u user:password` is …;
//  4. every secret-shaped span internal/secrets knows (token prefixes, JWTs,
//     Bearer values, connection strings with a password, private keys,
//     key=value credentials) is [REDACTED:<pattern>];
//  5. every remaining long, mixed letters-and-digits word (24 characters or
//     more: an unknown token format, a hash) is …;
//  6. the result is cut to MaxCommandRunes.
//
// It is a text filter, not a proof: a short secret typed as a bare argument
// (`mysql -pS3cret`) survives step 5. The log is a protected path (§8.2) and
// private to the user, and the hook denies a session's read of it.
func RedactCommand(cmd string) string {
	s := dropHeredocs(cmd)
	s = dropQuoted(s)
	s = assignRe.ReplaceAllString(s, "${1}…")
	s = psAssignRe.ReplaceAllString(s, "${1}…")
	s = secretFlagRe.ReplaceAllString(s, "${1}…")
	s = userPassRe.ReplaceAllString(s, "${1}…")
	s = secrets.Redact(s)
	s = longTokenRe.ReplaceAllStringFunc(s, func(w string) string {
		if strings.HasPrefix(w, "[REDACTED") {
			return w
		}
		hasLetter, hasDigit := false, false
		for _, r := range w {
			switch {
			case r >= '0' && r <= '9':
				hasDigit = true
			case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
				hasLetter = true
			}
		}
		if hasLetter && hasDigit {
			return "…"
		}
		return w
	})
	return truncate(strings.TrimSpace(s), MaxCommandRunes)
}

// RedactText masks secret-shaped spans and cuts the text; it is applied to
// a denial's reason, which names paths and rules but may quote a word of
// the command.
func RedactText(s string) string {
	return truncate(secrets.Redact(s), MaxReasonRunes)
}

// Limits of what one denial line carries.
const (
	MaxCommandRunes = 240
	MaxReasonRunes  = 600
)

var (
	heredocStartRe = regexp.MustCompile(`<<(-?)\s*(?:'([^'\n]+)'|"([^"\n]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)
	// NAME=value at the start of a word (a bash assignment, env NAME=value);
	// a --flag=value is handled by secretFlagRe, only when the flag names a
	// credential, so a plain --output=file keeps its value.
	assignRe = regexp.MustCompile(`((?:^|[\s;&|(])[A-Za-z_][A-Za-z0-9_]*=)[^\s;&|)]+`)
	// PowerShell: $env:NAME = value / $name = value, to the end of the
	// statement.
	psAssignRe = regexp.MustCompile(`(\$(?:env:)?[A-Za-z_][A-Za-z0-9_]*\s*=\s*)[^\s;|]+`)
	// A flag that holds a credential, with its value after = or a space.
	secretFlagRe = regexp.MustCompile(`(?i)((?:^|\s)-{1,2}(?:[a-z0-9-]*(?:pass(?:word)?|passwd|pwd|token|secret|api-?key|auth(?:orization)?|cred(?:ential)?s?|cookie|private-?key|client-?secret)[a-z0-9-]*|(?-i:H)|header|user)(?:=|\s+))[^\s;&|]+`)
	// curl -u user:password.
	userPassRe  = regexp.MustCompile(`((?:^|\s)-u\s+[^\s:]+:)[^\s;&|]+`)
	longTokenRe = regexp.MustCompile(`[A-Za-z0-9+/=_\-.]{24,}`)
)

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
			out = append(out, line, "<here-string body dropped>")
			j := i + 1
			for ; j < len(lines); j++ {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), closer) {
					out = append(out, lines[j])
					break
				}
			}
			i = j
			continue
		}
		// A bash here-string (<<<) is a quoted word, not a here-doc.
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
			out = append(out, "<heredoc body dropped>")
		}
		i = j - 1
	}
	return strings.Join(out, "\n")
}

// dropQuoted replaces the body of every quoted string with …, keeping the
// quotes. A backslash escapes the next character inside double quotes; an
// unterminated quote runs to the end.
func dropQuoted(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\'' && c != '"' {
			b.WriteByte(c)
			continue
		}
		j := i + 1
		for ; j < len(s); j++ {
			if c == '"' && s[j] == '\\' {
				j++
				continue
			}
			if s[j] == c {
				break
			}
		}
		b.WriteByte(c)
		if j > i+1 {
			b.WriteString("…")
		}
		if j < len(s) {
			b.WriteByte(c)
		}
		i = j
	}
	return b.String()
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
