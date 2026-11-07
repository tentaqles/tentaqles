package termopen

import (
	"regexp"
	"strings"
)

// splitWords splits a command line into words the way a POSIX shell's quoting reads them: spaces separate
// words, single and double quotes group them, and a backslash outside single quotes escapes the next
// character. An unterminated quote runs to the end of the line. Windows command lines are split by
// splitCommandLine instead (cmdline_windows.go).
func splitWords(s string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			// Inside double quotes a backslash escapes only a quote or another backslash; any other
			// backslash is kept, so a Windows path in quotes keeps its separators.
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
				i++
				cur.WriteRune(runes[i])
			default:
				cur.WriteRune(r)
			}
		case r == '\\' && i+1 < len(runes):
			i++
			cur.WriteRune(runes[i])
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// reasonRe matches the stderr line that gives a launcher's reason: the same words install.ErrorLine looks for.
var reasonRe = regexp.MustCompile(`(?i)error|fatal|cannot|can't|failed|denied|not found|unable|no such`)

// reasonLine is the line of a launcher's stderr that gives the reason: the first line that reads as an error,
// else the last line (the rule install.ErrorLine follows). Empty when stderr says nothing.
func reasonLine(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	for _, l := range lines {
		if reasonRe.MatchString(l) {
			return l
		}
	}
	if len(lines) > 0 {
		return lines[len(lines)-1]
	}
	return ""
}
