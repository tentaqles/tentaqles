package gate

import (
	"encoding/base64"
	"strings"
	"unicode/utf16"
)

// Word is one shell word with its quotes removed. Quoted is true when any
// part of it was quoted; Subst holds the text of command substitutions
// found inside double quotes ($(...) or `...`), which run too.
type Word struct {
	Text   string
	Quoted bool
}

// Segment is one simple command of a shell line: the text between two
// separators (&&, ||, ;, |, &, newline, (, ), {, }, $( and a backtick, the
// separators of the guard's sepPattern), outside quotes.
type Segment struct {
	Raw   string
	Words []Word
}

const maxDepth = 4

// Split cuts a shell command (bash or PowerShell) into segments. Quotes are
// honoured; a backslash-newline or backtick-newline continues the line; a
// comment runs to the end of the line; a redirection and its target are
// dropped; the command substitutions inside double quotes become segments
// of their own.
func Split(cmd string) []Segment { return split(cmd, 0) }

func split(cmd string, depth int) []Segment { return splitShell(cmd, depth, false) }

func splitShell(cmd string, depth int, powershell bool) []Segment {
	var segs []Segment
	var cur Segment
	var raw strings.Builder
	var word strings.Builder
	inWord, quoted := false, false
	redirect := false // the next word is a redirection target
	endWord := func() {
		if inWord {
			if redirect {
				redirect = false
			} else {
				cur.Words = append(cur.Words, Word{Text: word.String(), Quoted: quoted})
			}
		}
		word.Reset()
		inWord, quoted = false, false
	}
	endSeg := func() {
		endWord()
		redirect = false
		cur.Raw = strings.TrimSpace(raw.String())
		if len(cur.Words) > 0 {
			segs = append(segs, cur)
		}
		cur = Segment{}
		raw.Reset()
	}
	var nested []string
	r := []rune(cmd)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case (c == '\\' || c == '`') && i+1 < len(r) && (r[i+1] == '\n' || (r[i+1] == '\r' && i+2 < len(r) && r[i+2] == '\n')):
			// a line continuation
			if r[i+1] == '\r' {
				i++
			}
			i++
			endWord()
			raw.WriteRune(' ')
		case c == '\'':
			// literal to the next quote
			j := i + 1
			for j < len(r) && r[j] != '\'' {
				j++
			}
			word.WriteString(string(r[i+1 : min(j, len(r))]))
			raw.WriteString(string(r[i:min(j+1, len(r))]))
			inWord, quoted = true, true
			i = j
		case c == '"':
			j := i + 1
			for j < len(r) && r[j] != '"' {
				if (r[j] == '\\' || r[j] == '`') && j+1 < len(r) && (r[j+1] == '"' || r[j+1] == '\\' || r[j+1] == '$' || r[j+1] == '`') {
					word.WriteRune(r[j+1])
					j += 2
					continue
				}
				if r[j] == '$' && j+1 < len(r) && r[j+1] == '(' {
					// $( ... ) inside double quotes: the matching paren
					k, d := j+2, 1
					for k < len(r) && d > 0 {
						if r[k] == '(' {
							d++
						} else if r[k] == ')' {
							d--
						}
						k++
					}
					nested = append(nested, string(r[j+2:max(j+2, k-1)]))
					word.WriteString(string(r[j:k]))
					j = k
					continue
				}
				if r[j] == '`' {
					// a bash `...` substitution, when it closes inside the quotes
					k := j + 1
					for k < len(r) && r[k] != '`' && r[k] != '"' {
						k++
					}
					if k < len(r) && r[k] == '`' {
						nested = append(nested, string(r[j+1:k]))
						word.WriteString(string(r[j : k+1]))
						j = k + 1
						continue
					}
				}
				word.WriteRune(r[j])
				j++
			}
			raw.WriteString(string(r[i:min(j+1, len(r))]))
			inWord, quoted = true, true
			i = j
		case c == '\\' && i+1 < len(r) && !powershell:
			word.WriteRune(r[i+1])
			raw.WriteRune(c)
			raw.WriteRune(r[i+1])
			inWord = true
			i++
		case c == '#' && !inWord:
			for i < len(r) && r[i] != '\n' {
				i++
			}
			endSeg()
		case c == '>' || c == '<':
			// a redirection: "2>" "&>" ">>" ">&1" "<"; its target is dropped
			if inWord && isDigits(word.String()) {
				word.Reset()
				inWord = false
			}
			endWord()
			raw.WriteRune(c)
			for i+1 < len(r) && (r[i+1] == '>' || r[i+1] == '&' || r[i+1] == '|') {
				i++
				raw.WriteRune(r[i])
			}
			// ">&1": the descriptor is the target
			redirect = true
		case c == '$' && i+1 < len(r) && r[i+1] == '(':
			endSeg()
			i++
		case strings.ContainsRune("&|;\n\r(){}`", c):
			endSeg()
		case c == ' ' || c == '\t':
			endWord()
			raw.WriteRune(c)
		default:
			word.WriteRune(c)
			raw.WriteRune(c)
			inWord = true
		}
	}
	endSeg()
	if depth < maxDepth {
		for _, n := range nested {
			segs = append(segs, splitShell(n, depth+1, powershell)...)
		}
	}
	return segs
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// FromArgv makes a segment of an argv (tq pack cloud plan's command).
func FromArgv(argv []string) Segment {
	s := Segment{Raw: strings.Join(argv, " ")}
	for _, a := range argv {
		s.Words = append(s.Words, Word{Text: a})
	}
	return s
}

// decodePowerShell decodes a -EncodedCommand argument (base64 of UTF-16LE).
func decodePowerShell(b64 string) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw)%2 != 0 {
		return "", false
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(u)), true
}
