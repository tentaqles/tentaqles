package termopen

import (
	"regexp"
	"strings"
)

// doubtful reports a command line that may not have been split the way its shell read it: a quote of one of
// the kinds in quotes that is not closed (an odd count), or a trailing backslash. Interactive keeps such a
// shell open, since every doubt does.
func doubtful(s, quotes string) bool {
	for _, q := range quotes {
		if strings.Count(s, string(q))%2 == 1 {
			return true
		}
	}
	return strings.HasSuffix(s, `\`)
}

// splitPlain splits a command line joined from argv with plain spaces, as Linux and macOS hand it over
// (internal/humanonly): no shell quoting is re-applied, so a quote is a literal character and a value with a
// space becomes extra words. A doubt (an unclosed quote, a trailing backslash) makes it unsplittable. It is a
// plain function on every OS, so the POSIX rules are tested on any host.
func splitPlain(s string) ([]string, bool) {
	if doubtful(s, `'"`) {
		return nil, false
	}
	return strings.Fields(s), true
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
