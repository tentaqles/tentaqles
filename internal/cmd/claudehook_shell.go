package cmd

import (
	"path/filepath"
	"regexp"
	"strings"
)

// A small, quote-aware shell reader for the PII exit rules. It is not a full
// shell parser: it splits a command into segments on && || ; | & and newlines
// outside quotes, and a segment into words with quotes removed. Anything it
// cannot read safely (an unbalanced quote) is reported so the caller can ask
// instead of guessing.

type shSeg struct {
	Text       string
	Unbalanced bool // the segment ends inside an open quote
}

func splitSegments(cmd string) []shSeg {
	var segs []shSeg
	var cur strings.Builder
	var q rune
	flush := func(unbalanced bool) {
		if t := strings.TrimSpace(cur.String()); t != "" {
			segs = append(segs, shSeg{Text: t, Unbalanced: unbalanced})
		}
		cur.Reset()
	}
	rs := []rune(cmd)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if q != 0 {
			cur.WriteRune(c)
			if c == '\\' && q == '"' && i+1 < len(rs) {
				i++
				cur.WriteRune(rs[i])
				continue
			}
			if c == q {
				q = 0
			}
			continue
		}
		switch {
		case c == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\'' || rs[i+1] == ' '):
			cur.WriteRune(c)
			i++
			cur.WriteRune(rs[i])
		case c == '\'' || c == '"':
			q = c
			cur.WriteRune(c)
		case c == ';' || c == '\n':
			flush(false)
		case c == '|':
			flush(false)
			if i+1 < len(rs) && rs[i+1] == '|' {
				i++
			}
		case c == '&':
			prevRedir := i > 0 && (rs[i-1] == '>' || rs[i-1] == '<')
			nextRedir := i+1 < len(rs) && rs[i+1] == '>'
			if i+1 < len(rs) && rs[i+1] == '&' {
				flush(false)
				i++
			} else if prevRedir || nextRedir {
				cur.WriteRune(c)
			} else {
				flush(false)
			}
		default:
			cur.WriteRune(c)
		}
	}
	flush(q != 0)
	return segs
}

// tokenize splits a segment into words with quotes removed. ok is false when
// a quote is left open.
func tokenize(seg string) (toks []string, ok bool) {
	var cur strings.Builder
	has := false
	var q rune
	rs := []rune(seg)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if q != 0 {
			if c == q {
				q = 0
				continue
			}
			if q == '"' && c == '\\' && i+1 < len(rs) && rs[i+1] == '"' {
				i++
				cur.WriteRune('"')
				continue
			}
			cur.WriteRune(c)
			continue
		}
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			if has {
				toks = append(toks, cur.String())
				cur.Reset()
				has = false
			}
		case c == '\'' || c == '"':
			q = c
			has = true
		case c == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\'' || rs[i+1] == ' '):
			i++
			cur.WriteRune(rs[i])
			has = true
		default:
			cur.WriteRune(c)
			has = true
		}
	}
	if has {
		toks = append(toks, cur.String())
	}
	return toks, q == 0
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

var prefixWords = map[string]bool{"sudo": true, "doas": true, "env": true, "nohup": true, "time": true, "command": true, "exec": true, "nice": true}

// verbIndex returns the index of the command word, skipping assignments,
// wrappers such as sudo and the options that follow them. -1 when none.
func verbIndex(toks []string) int {
	afterWrapper := false
	for i, t := range toks {
		switch {
		case assignRe.MatchString(t):
		case prefixWords[strings.ToLower(t)]:
			afterWrapper = true
		case afterWrapper && strings.HasPrefix(t, "-"):
		default:
			return i
		}
	}
	return -1
}

func baseName(t string) string {
	t = strings.ReplaceAll(t, `\`, "/")
	t = t[strings.LastIndex(t, "/")+1:]
	t = strings.TrimSuffix(strings.ToLower(t), ".exe")
	return filepath.ToSlash(t)
}
