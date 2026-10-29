package data

import (
	"strings"
)

// costSegment is one simple command of a shell line: its words with quotes
// removed. The tokenizer is deliberately small: it understands single and
// double quotes, backslash and backtick escapes, and the separators ; & | and
// newlines (so && || and pipes split too). It never runs anything.
type costSegment struct {
	Words []string
}

// costMaxWords bounds the tokenizer on a hostile command (guards < 50 ms).
const costMaxWords = 50000

// costSegments splits a Bash or PowerShell command line into segments.
func costSegments(cmd string) []costSegment {
	var segs []costSegment
	var words []string
	var cur strings.Builder
	inWord := false
	total := 0
	flushWord := func() {
		if inWord {
			words = append(words, cur.String())
			total++
		}
		cur.Reset()
		inWord = false
	}
	flushSeg := func() {
		flushWord()
		if len(words) > 0 {
			segs = append(segs, costSegment{Words: words})
		}
		words = nil
	}
	var quote byte
	for i := 0; i < len(cmd) && total < costMaxWords; i++ {
		c := cmd[i]
		if quote != 0 {
			switch {
			case c == quote:
				quote = 0
			case quote == '"' && (c == '\\' || c == '`') && i+1 < len(cmd) && (cmd[i+1] == '"' || cmd[i+1] == '\\' || cmd[i+1] == '`' || cmd[i+1] == '$'):
				i++
				cur.WriteByte(cmd[i])
			default:
				cur.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			inWord = true
		case '\\', '`':
			if i+1 < len(cmd) {
				if cmd[i+1] == '\n' || cmd[i+1] == '\r' {
					// line continuation
					i++
					if i+1 < len(cmd) && cmd[i] == '\r' && cmd[i+1] == '\n' {
						i++
					}
					flushWord()
					continue
				}
				if c == '\\' && costWinPathChar(cmd, i) {
					cur.WriteByte(c)
					inWord = true
					continue
				}
				i++
				cur.WriteByte(cmd[i])
				inWord = true
				continue
			}
			cur.WriteByte(c)
			inWord = true
		case ' ', '\t':
			flushWord()
		case ';', '&', '|', '\n', '\r':
			flushSeg()
		case '<', '>':
			flushWord()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flushSeg()
	return segs
}

// costWinPathChar reports whether the backslash at i sits inside a Windows
// path (C:\x, .\x, dir\x), where it is a separator rather than an escape.
func costWinPathChar(s string, i int) bool {
	if i+1 >= len(s) {
		return false
	}
	next := s[i+1]
	return next != ' ' && next != '"' && next != '\'' && next != '$' && next != '\\' && next != '`'
}

// costToolName normalises a word to a command name: the base name, lower case,
// without .exe/.cmd/.bat/.ps1 and without a leading $( or (.
func costToolName(w string) string {
	w = strings.TrimLeft(w, "$(`")
	w = strings.TrimRight(w, ")")
	if i := strings.LastIndexAny(w, `/\`); i >= 0 {
		w = w[i+1:]
	}
	w = strings.ToLower(w)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		w = strings.TrimSuffix(w, ext)
	}
	return w
}

// find returns the index of the first word that names tool, or -1.
func (s costSegment) find(tool string) int {
	for i, w := range s.Words {
		if costToolName(w) == tool {
			return i
		}
	}
	return -1
}

// after returns the words after the tool at index i.
func (s costSegment) after(i int) []string {
	if i < 0 || i+1 > len(s.Words) {
		return nil
	}
	return s.Words[i+1:]
}

// costPositional returns the non-flag words of args (a word following a flag
// without '=' is not skipped: callers that need values use costFlagValues).
func costPositional(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// costFlagValues returns every value of a flag among args, in the forms
// `--name value`, `--name=value`, and for a one-letter short flag `-qvalue`.
// names are given with their dashes ("--num-workers", "-q").
func costFlagValues(args []string, names ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		for _, n := range names {
			switch {
			case a == n:
				if i+1 < len(args) {
					out = append(out, args[i+1])
				} else {
					out = append(out, "")
				}
			case strings.HasPrefix(a, n+"="):
				out = append(out, a[len(n)+1:])
			case len(n) == 2 && n[0] == '-' && n[1] != '-' && strings.HasPrefix(a, n) && len(a) > 2 && a[2] != '-':
				out = append(out, a[2:])
			}
		}
	}
	return out
}

// costHasFlag reports whether a bare flag is present (`--x` or `--x=…`).
func costHasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}
