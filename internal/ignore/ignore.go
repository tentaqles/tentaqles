// Package ignore matches paths against gitignore-syntax rules without
// running git. It is built to err toward exclusion, because what it lets
// through may be sent off the machine (tq decide explore):
//
//   - negation lines ("!pattern") are dropped: they can only un-ignore, so
//     dropping them excludes more, never less;
//   - matching is case-insensitive (git on Windows/macOS defaults to
//     core.ignorecase; elsewhere this only excludes more);
//   - a line the parser is not sure about is an error, and callers treat a
//     file with any error as "ignore everything this file governs".
//
// Supported syntax: blank lines and "#" comments, "\#" and "\!" escapes,
// trailing spaces (stripped unless escaped), leading "/" and middle "/"
// anchoring, trailing "/" for directories only, "*", "?", "[...]" classes
// with ranges and "!"/"^" negation, and "**" in leading ("**/x"), middle
// ("a/**/b") and trailing ("a/**") position.
package ignore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Pattern is one compiled rule.
type Pattern struct {
	Source  string
	DirOnly bool
	re      *regexp.Regexp
}

// Rules is the compiled content of one ignore file. Paths given to Match are
// slash-separated and relative to the directory the file governs.
type Rules struct {
	Patterns []Pattern
}

// ErrUnsupported marks a line the parser will not guess at.
var ErrUnsupported = errors.New("unsupported ignore pattern")

// Parse compiles an ignore file. It returns an error naming the first line
// it cannot compile with confidence; callers must then fail closed.
func Parse(text string) (Rules, error) {
	var r Rules
	for i, line := range strings.Split(text, "\n") {
		p, ok, err := ParseLine(line)
		if err != nil {
			return Rules{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		if ok {
			r.Patterns = append(r.Patterns, p)
		}
	}
	return r, nil
}

// ParseLine compiles one line. ok is false for blank lines, comments and
// negations (which are deliberately dropped).
func ParseLine(line string) (p Pattern, ok bool, err error) {
	line = strings.TrimSuffix(line, "\r")
	// Trailing spaces are ignored unless escaped with a backslash.
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, `\ `) {
		line = line[:len(line)-1]
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return Pattern{}, false, nil
	}
	if strings.HasPrefix(line, "!") {
		return Pattern{}, false, nil // negation: only un-ignores; dropped
	}
	p.Source = line
	pat := line
	if strings.HasSuffix(pat, "/") && !strings.HasSuffix(pat, `\/`) {
		p.DirOnly = true
		pat = strings.TrimSuffix(pat, "/")
	}
	if pat == "" || strings.HasSuffix(pat, "/") {
		return Pattern{}, false, fmt.Errorf("%w: %q", ErrUnsupported, line)
	}
	// A slash at the start or in the middle anchors the pattern to the
	// directory of the ignore file; otherwise it matches at any depth.
	anchored := strings.Contains(pat, "/")
	pat = strings.TrimPrefix(pat, "/")
	body, err := globToRegex(pat)
	if err != nil {
		return Pattern{}, false, fmt.Errorf("%w: %q: %v", ErrUnsupported, line, err)
	}
	expr := "(?i)^"
	if !anchored {
		expr += "(?:.*/)?"
	}
	expr += body + "$"
	re, err := regexp.Compile(expr)
	if err != nil {
		return Pattern{}, false, fmt.Errorf("%w: %q: %v", ErrUnsupported, line, err)
	}
	p.re = re
	return p, true, nil
}

// Match reports whether rel (relative to the rules' directory) is ignored
// by a rule. A directory that matches excludes everything below it; the
// caller is expected to prune it.
func (r Rules) Match(rel string, isDir bool) bool {
	for _, p := range r.Patterns {
		if p.DirOnly && !isDir {
			continue
		}
		if p.re.MatchString(rel) {
			return true
		}
	}
	return false
}

func globToRegex(pat string) (string, error) {
	rs := []rune(pat)
	n := len(rs)
	var b strings.Builder
	for i := 0; i < n; i++ {
		c := rs[i]
		switch c {
		case '\\':
			if i+1 >= n {
				return "", errors.New("trailing backslash")
			}
			i++
			b.WriteString(regexp.QuoteMeta(string(rs[i])))
		case '*':
			j := i
			for j < n && rs[j] == '*' {
				j++
			}
			if j-i >= 2 && (i == 0 || rs[i-1] == '/') && (j == n || rs[j] == '/') {
				if j == n {
					b.WriteString(".*") // trailing "/**" or a lone "**"
				} else {
					b.WriteString("(?:.*/)?") // "**/" leading or middle
					j++                       // the slash is part of the match
				}
			} else {
				b.WriteString("[^/]*") // any other run of stars is one "*"
			}
			i = j - 1
		case '?':
			b.WriteString("[^/]")
		case '[':
			cls, used, err := classToRegex(rs[i:])
			if err != nil {
				return "", err
			}
			b.WriteString(cls)
			i += used - 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String(), nil
}

// classToRegex converts a bracket expression starting at rs[0] == '['. It
// returns the regex and how many runes it consumed. POSIX classes
// ("[[:alpha:]]") and anything malformed are errors (fail closed).
func classToRegex(rs []rune) (string, int, error) {
	n := len(rs)
	j := 1
	neg := false
	if j < n && (rs[j] == '!' || rs[j] == '^') {
		neg = true
		j++
	}
	var items strings.Builder
	first := true
	lit := func(r rune) string { return fmt.Sprintf(`\x{%x}`, r) }
	for j < n {
		c := rs[j]
		if c == ']' && !first {
			if items.Len() == 0 && !neg {
				return "", 0, errors.New("empty class")
			}
			if neg {
				return "[^/" + items.String() + "]", j + 1, nil
			}
			return "[" + items.String() + "]", j + 1, nil
		}
		first = false
		if c == '[' && j+1 < n && (rs[j+1] == ':' || rs[j+1] == '=' || rs[j+1] == '.') {
			return "", 0, errors.New("POSIX character classes are not supported")
		}
		if c == '\\' {
			j++
			if j >= n {
				return "", 0, errors.New("unterminated class")
			}
			c = rs[j]
		}
		if j+2 < n && rs[j+1] == '-' && rs[j+2] != ']' {
			hi := rs[j+2]
			if hi == '\\' || hi == '[' || hi < c {
				return "", 0, errors.New("unsupported range")
			}
			if c != '/' {
				items.WriteString(lit(c) + "-" + lit(hi))
			}
			j += 3
			continue
		}
		if c != '/' { // a class never matches a slash
			items.WriteString(lit(c))
		}
		j++
	}
	return "", 0, errors.New("unterminated class")
}
