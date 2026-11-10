package shellwords

import (
	"regexp"
	"strings"
)

// Part is one piece of a command line, split at its top-level separators, with the separator that
// precedes it. Sep is "" for the first part; otherwise it is ";", "&&", "||", "|", "&" or "\n" (a
// carriage return counts as "\n"). The rarer separators that Statements also splits at (`$(`, a
// backtick, `(`, `)`, `{`, `}`) are reported by their own text. Text is trimmed of surrounding white
// space. A part with no text is dropped; the separator before the next part is then the one that
// followed the dropped part.
type Part struct {
	Sep  string
	Text string
}

// Parts splits command at the separators Statements splits at, and keeps the separator before each
// part. Separators inside quotes do not split, as in Statements, and the text is taken from command
// itself. Parts(command) and Statements(command) give the same texts in the same order.
func Parts(command string) []Part {
	sepOnce.Do(func() { sepRE = regexp.MustCompile(sepPattern) })
	masked := maskQuoted(command)
	var out []Part
	prev, sep := 0, ""
	cut := func(end int) {
		text := strings.TrimSpace(command[prev:end])
		if text == "" {
			return
		}
		if len(out) == 0 {
			sep = ""
		}
		out = append(out, Part{Sep: sep, Text: text})
	}
	for _, loc := range sepRE.FindAllStringIndex(masked, -1) {
		cut(loc[0])
		prev = loc[1]
		sep = separatorName(command[loc[0]:loc[1]])
	}
	cut(len(command))
	return out
}

// separatorName is the name Parts reports for a matched separator: a carriage return is a line break.
func separatorName(s string) string {
	if s == "\r" {
		return "\n"
	}
	return s
}
