package install

import (
	"regexp"
	"strings"
	"unicode"
)

// ansiRe matches the terminal escape sequences a package manager prints with its progress: CSI
// (colour, cursor) and OSC (title) sequences.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)|\x1b[@-Z\\\\-_]")

// warnRe matches a warning or notice line: npm warn, Warning:, WARN, apt's W: and pip's WARNING.
var warnRe = regexp.MustCompile(`(?i)^(npm\s+(warn|notice)\b|warn(ing)?\b|notice\b|w:|\[warn)`)

// errRe matches the words a package manager's error line holds: npm error, brew's Error:, pip's
// ERROR, apt's E:, winget's "Installer failed" and the OS's own refusals.
var errRe = regexp.MustCompile(`(?i)\b(err|error|errors|fatal|failed|failure|denied|unable|cannot|couldn't|could not|not found|no such|no matching|not permitted|permission)\b|^e:|err!`)

// sizeRe matches a size, a speed or a percentage: what a download progress line is made of.
var sizeRe = regexp.MustCompile(`(?i)\d+(\.\d+)?\s*(%|[kmgt]?i?b(/s)?)`)

// ErrorLine is the line that says why a package manager failed. It looks in stderr, then in
// stdout, for the first line that reads as an error, skipping what a package manager prints
// before it: a progress line or bar, a spinner frame, a warning. Without such a line it takes
// the first plain line of stderr, then the last plain line of stdout, and then the error's own
// text (the exit status). Escape and control characters are removed and the line is cut to
// 200 runes.
func ErrorLine(stderr, stdout string, err error) string {
	errLines, outLines := lines(stderr), lines(stdout)
	for _, set := range [][]string{errLines, outLines} {
		for _, l := range set {
			if !isNoise(l) && !warnRe.MatchString(l) && errRe.MatchString(l) {
				return clip(l)
			}
		}
	}
	for _, l := range errLines {
		if !isNoise(l) && !warnRe.MatchString(l) {
			return clip(l)
		}
	}
	// winget says "No applicable installer found" or "No package found matching input
	// criteria" on stdout, with no error word: the last plain line is its verdict.
	for i := len(outLines) - 1; i >= 0; i-- {
		if l := outLines[i]; !isNoise(l) && !warnRe.MatchString(l) {
			return clip(l)
		}
	}
	if err != nil {
		return clip(clean(err.Error()))
	}
	return "it failed with no message"
}

// lines splits a stream into trimmed lines with escape and control characters removed. A
// carriage return starts a new line: a progress bar redraws itself that way.
func lines(s string) []string {
	s = ansiRe.ReplaceAllString(s, "")
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", "\n"), "\n") {
		if l = clean(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// isNoise reports a spinner frame (no letter or digit in it), a bar, or a download progress
// line (only sizes, speeds, percentages and punctuation around the text).
func isNoise(l string) bool {
	hasWord := false
	for _, r := range l {
		if strings.ContainsRune("█▒░▓■▪▌▐", r) {
			return true
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasWord = true
		}
	}
	if !hasWord {
		return true
	}
	rest := sizeRe.ReplaceAllString(l, "")
	for _, r := range rest {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func firstLine(s string) string {
	if ls := lines(s); len(ls) > 0 {
		return ls[0]
	}
	return ""
}

// clean drops control characters and trims the text.
func clean(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// clip cuts the text to 200 runes.
func clip(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "..."
	}
	return s
}
