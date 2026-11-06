package guard

import "strings"

// PowerShell's quoting for the tokenizer (ADR-0007 round 4). In PowerShell a backslash is an
// ordinary character, a backtick escapes the next character (inside double quotes and outside
// quotes), a quote is doubled inside a string of its kind ('it''s', "say ""hi"""), and a
// here-string (@'...'@, @"..."@) opens at the end of a line and closes with '@ or "@ at the
// start of one. Reading a PowerShell line with bash's rules lets a string that ends in a
// backslash ("C:\temp\") run on to the end of the line and hide every command after it.

// CommandsIn is Commands for a line of the given shell: ps for a PowerShell line (the
// PowerShell tool, a decoded -EncodedCommand, the string of pwsh -Command). A PowerShell line
// is split with PowerShell's rules, and also with bash's, so no command the bash reading found
// is lost; a nested shell's string is split with that shell's rules (pwsh, powershell, iex:
// PowerShell; bash, sh, zsh: bash). A here-string that is run (iex, Invoke-Expression,
// [scriptblock]::Create, pwsh -Command, piped to iex, assigned and later run) is split as
// commands too.
func CommandsIn(c string, ps bool) []Command {
	if !ps {
		return Commands(c)
	}
	out := commandsDepthPS(c, "", false, 0)
	return append(out, Commands(c)...)
}

// SeparatorSpansIn is SeparatorSpans for a line of the given shell: for a PowerShell line, the
// separators of both readings.
func SeparatorSpansIn(c string, ps bool) [][2]int {
	out := SeparatorSpans(c)
	if ps {
		for _, loc := range sepRegexp.FindAllStringIndex(maskQuotedPS(c), -1) {
			out = append(out, [2]int{loc[0], loc[1]})
		}
	}
	return out
}

// nestedPS reports whether the string a nested shell runs is PowerShell, given the verb that
// runs it and the outer line's shell.
func nestedPS(verbWord string, outerPS bool) bool {
	switch strings.TrimSuffix(strings.ToLower(baseWord(verbWord)), ".exe") {
	case "pwsh", "powershell", "iex", "invoke-expression":
		return true
	case "bash", "sh", "zsh", "dash", "ksh", "fish":
		return false
	}
	return outerPS
}

// commandsDepthPS is commandsDepth with PowerShell's rules.
func commandsDepthPS(c, base string, baseUnknown bool, depth int) []Command {
	var out []Command
	masked := maskQuotedPS(c)
	spans := segmentSpans(masked)
	segs := make([][]psWord, len(spans))
	for k, sp := range spans {
		segs[k] = splitWordsPS(c[sp.start:sp.end])
	}
	sepAfter := func(k int) string {
		if k+1 >= len(spans) {
			return ""
		}
		return strings.TrimSpace(c[spans[k].end:spans[k+1].start])
	}
	for k, ws := range segs {
		words := psTexts(ws)
		if len(words) == 0 {
			continue
		}
		out = append(out, Command{Words: words, Base: base, BaseUnknown: baseUnknown, Nested: depth > 0})
		if dir, ok := cdTarget(words); ok {
			if dir == "" {
				baseUnknown = true
			} else {
				base = joinBase(base, dir)
			}
			continue
		}
		if depth >= 3 {
			continue
		}
		if inner, ok := nestedCommand(words); ok {
			out = append(out, nestedSplit(inner, nestedPS(words[0], true), base, baseUnknown, depth+1)...)
		}
		for _, w := range ws {
			if w.here && hereRuns(segs, spans, c, k, sepAfter) {
				out = append(out, nestedSplit(w.text, true, base, baseUnknown, depth+1)...)
			}
		}
	}
	return out
}

func nestedSplit(inner string, ps bool, base string, baseUnknown bool, depth int) []Command {
	if !ps {
		return commandsDepth(inner, base, baseUnknown, depth)
	}
	return append(commandsDepthPS(inner, base, baseUnknown, depth), commandsDepth(inner, base, baseUnknown, depth)...)
}

// psCodeVerbs run a string they are given as PowerShell code.
var psCodeVerbs = map[string]bool{"iex": true, "invoke-expression": true, "invoke-command": true, "icm": true,
	"start-job": true, "sajb": true, "pwsh": true, "powershell": true, "&": true, ".": true}

func psVerb(ws []psWord) string {
	if len(ws) == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(baseWord(ws[0].text)), ".exe")
}

// hereRuns reports whether the here-string in segment k is run as code: its command runs a
// string, it is the argument of a call that does (iex(...), [scriptblock]::Create(...)), it is
// piped to one, or it is assigned to a variable a later command runs.
func hereRuns(segs [][]psWord, spans []span, c string, k int, sepAfter func(int) string) bool {
	ws := segs[k]
	if psCodeVerbs[psVerb(ws)] {
		return true
	}
	if k > 0 && strings.HasSuffix(c[spans[k-1].end:spans[k].start], "(") {
		prev := strings.ToLower(strings.TrimSpace(c[spans[k-1].start:spans[k-1].end]))
		if strings.HasSuffix(prev, "::create") || psCodeVerbs[psVerb(segs[k-1])] {
			return true
		}
	}
	if sepAfter(k) == "|" && k+1 < len(segs) && psCodeVerbs[psVerb(segs[k+1])] {
		return true
	}
	if len(ws) >= 2 && strings.HasPrefix(ws[0].text, "$") && ws[1].text == "=" {
		v := strings.ToLower(ws[0].text)
		for j := k + 1; j < len(segs); j++ {
			seg := strings.ToLower(c[spans[j].start:spans[j].end])
			if !strings.Contains(seg, v) {
				continue
			}
			if psCodeVerbs[psVerb(segs[j])] {
				return true
			}
			if j > 0 && strings.HasSuffix(strings.ToLower(strings.TrimSpace(c[spans[j-1].start:spans[j-1].end])), "::create") {
				return true
			}
		}
	}
	return false
}

type psWord struct {
	text string
	here bool // a here-string: text is its body
}

func psTexts(ws []psWord) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.text
	}
	return out
}

// psHereString reads a here-string opening at s[i] ('@' then a quote): ok with its end (past
// the closer) and its body. It is one only when the rest of the opening line is blank; an
// unclosed one runs to the end of s.
func psHereString(s string, i int) (end int, body string, ok bool) {
	if i+1 >= len(s) || (s[i+1] != '\'' && s[i+1] != '"') {
		return 0, "", false
	}
	q := s[i+1]
	j := i + 2
	for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r') {
		j++
	}
	if j >= len(s) || s[j] != '\n' {
		return 0, "", false
	}
	closer := "\n" + string(q) + "@"
	k := strings.Index(s[j:], closer)
	if k < 0 {
		return len(s), s[j+1:], true
	}
	body = "" // an empty here-string: the closer follows the opening line
	if k > 0 {
		body = s[j+1 : j+k]
	}
	return j + k + len(closer), body, true
}

// maskQuotedPS is maskQuoted with PowerShell's quoting: separators inside a string or a
// here-string are masked; a $( ... ) subexpression in a double-quoted string runs and is kept;
// a backtick escapes the next character, which is masked when it is a separator.
func maskQuotedPS(c string) string {
	b := []byte(c)
	var q byte
	for i := 0; i < len(b); i++ {
		ch := b[i]
		if (q == 0 || q == '"') && ch == '$' && i+1 < len(b) && b[i+1] == '{' {
			maskBraces(b, i+1)
		}
		switch q {
		case 0:
			switch ch {
			case '`':
				b[i] = 0x01
				if i+1 < len(b) {
					i++
					if isSepByte(b[i]) {
						b[i] = 0x01
					}
				}
			case '@':
				if end, _, ok := psHereString(c, i); ok {
					for j := i + 2; j < end; j++ {
						if isSepByte(b[j]) && !(c[i+1] == '"' && b[j] == '$' && j+1 < end && c[j+1] == '(') {
							b[j] = 0x01
						}
					}
					i = end - 1
				}
			case '\'', '"':
				q = ch
			}
		case '\'':
			switch {
			case ch == '\'' && i+1 < len(b) && b[i+1] == '\'':
				i++
			case ch == '\'':
				q = 0
			case isSepByte(ch):
				b[i] = 0x01
			}
		case '"':
			switch {
			case ch == '`':
				b[i] = 0x01
				if i+1 < len(b) {
					i++
					if isSepByte(b[i]) || b[i] == '"' {
						b[i] = 0x01
					}
				}
			case ch == '"' && i+1 < len(b) && b[i+1] == '"':
				i++
			case ch == '"':
				q = 0
			case ch == '$' && i+1 < len(b) && b[i+1] == '(':
				i++ // a subexpression runs: keep `$(`
			case isSepByte(ch):
				b[i] = 0x01
			}
		}
	}
	return string(b)
}

// splitWordsPS splits a simple PowerShell command into words, quotes removed: a doubled quote is a
// quote inside a string of their kind, a backtick escapes the next character, a backslash is
// kept, and a here-string is one word holding its body.
func splitWordsPS(s string) []psWord {
	var out []psWord
	var cur strings.Builder
	in := false
	var q byte
	flush := func() {
		if in {
			out = append(out, psWord{text: cur.String()})
			cur.Reset()
			in = false
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case q == '\'':
			if ch == '\'' && i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteByte('\'')
				i++
			} else if ch == '\'' {
				q = 0
			} else {
				cur.WriteByte(ch)
			}
		case q == '"':
			switch {
			case ch == '`' && i+1 < len(s):
				i++
				cur.WriteByte(s[i])
			case ch == '"' && i+1 < len(s) && s[i+1] == '"':
				cur.WriteByte('"')
				i++
			case ch == '"':
				q = 0
			default:
				cur.WriteByte(ch)
			}
		case ch == '@' && !in:
			if end, body, ok := psHereString(s, i); ok {
				out = append(out, psWord{text: body, here: true})
				i = end - 1
				continue
			}
			cur.WriteByte(ch)
			in = true
		case ch == '\'' || ch == '"':
			q, in = ch, true
		case ch == '`' && i+1 < len(s):
			i++
			if s[i] != '\n' {
				cur.WriteByte(s[i])
				in = true
			}
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == 0x01:
			flush()
		default:
			cur.WriteByte(ch)
			in = true
		}
	}
	flush()
	return out
}
