package guard

import (
	"regexp"
	"strings"
)

// codeWord is a program that runs the text it is given: shells,
// interpreters and eval-like builtins.
var codeWord = regexp.MustCompile(`^(bash|sh|zsh|dash|ksh|mksh|fish|csh|tcsh|pwsh|powershell|python[0-9.]*|py|pypy[0-9.]*|node|nodejs|deno|bun|perl|ruby|php|lua|cmd|osascript|tclsh|wish|eval|source|\.|iex|invoke-expression|exec)$`)

// wrapperWord runs the command that follows it.
var wrapperWord = map[string]bool{"sudo": true, "doas": true, "env": true, "command": true, "builtin": true, "nohup": true, "time": true, "nice": true, "timeout": true, "xargs": true, "stdbuf": true}

var assignWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// progName normalizes a command word: no quotes, no directory, lower case,
// no .exe.
func progName(w string) string {
	w = strings.Trim(w, `"'`)
	if i := strings.LastIndexAny(w, `/\`); i >= 0 && w != "." {
		w = w[i+1:]
	}
	return strings.TrimSuffix(strings.ToLower(w), ".exe")
}

// runsCode reports whether any command in skel (a command whose heredoc
// bodies are blanked) runs a shell, an interpreter, eval/source or a
// command named by a variable. Such a command can execute text a heredoc
// wrote or printed, so no body is treated as data.
func runsCode(skel string) bool {
	for _, sp := range segmentSpans(skel) {
		for _, w := range strings.Fields(skel[sp.start:sp.end]) {
			if strings.HasPrefix(w, "$") || strings.HasPrefix(w, `"$`) {
				return true
			}
			n := progName(w)
			if n == "" || assignWord.MatchString(w) || wrapperWord[n] || strings.HasPrefix(w, "-") || isNumber(w) {
				continue
			}
			if codeWord.MatchString(n) || scriptPath(w) {
				return true
			}
			break // the command word is something else
		}
	}
	return false
}

// scriptPath reports whether a command word runs a local script file.
func scriptPath(w string) bool {
	w = strings.ToLower(strings.Trim(w, `"'`))
	return strings.HasPrefix(w, "./") || strings.HasPrefix(w, `.\`) || strings.HasPrefix(w, "../") ||
		scriptExt.MatchString(w)
}

var scriptExt = regexp.MustCompile(`\.(sh|bash|zsh|py|js|mjs|cjs|ts|ps1|pl|rb|php|cmd|bat)$`)

func isNumber(w string) bool {
	if w == "" || w[0] < '0' || w[0] > '9' {
		return false
	}
	for i := 0; i < len(w); i++ {
		if (w[i] < '0' || w[i] > '9') && w[i] != '.' && w[i] != 's' && w[i] != 'm' {
			return false
		}
	}
	return true
}

func isSegSep(ch byte) bool {
	switch ch {
	case ';', '|', '&', '(', ')', '`', '{', '}':
		return true
	}
	return false
}

// segBounds returns the command segment of line around [start, end): from
// after the previous separator to the next one, and the separator before it
// (0 at the start of the line).
func segBounds(line string, start, end int) (from, to int, sep byte) {
	from = start
	for from > 0 && !isSegSep(line[from-1]) {
		from--
	}
	if from > 0 {
		sep = line[from-1]
	}
	to = end
	for to < len(line) && !isSegSep(line[to]) {
		to++
	}
	return from, to, sep
}

var redirWord = regexp.MustCompile(`^(\d*>>?|\d*>&|&>>?|<)`)

// commandWords is the words of a segment without heredoc markers, redirects
// (and their targets) and leading variable assignments.
func commandWords(seg string) []string {
	seg = heredocStart.ReplaceAllString(seg, " ")
	var out []string
	f := strings.Fields(seg)
	for i := 0; i < len(f); i++ {
		w := f[i]
		if m := redirWord.FindString(w); m != "" {
			if m == w {
				i++ // the target is the next word
			}
			continue
		}
		if len(out) == 0 && assignWord.MatchString(w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// isDataHeredoc reports whether h's body is data: it is fed to cat, tee or
// git commit/tag/notes (-F -), or to a bare redirect, at a place where the
// result is not run. Anything else, or anything tq cannot place, is code.
func isDataHeredoc(lines []string, skel string, lineStart []int, h heredoc) bool {
	if !h.closed {
		return false
	}
	if h.line > 0 && strings.HasSuffix(strings.TrimRight(lines[h.line-1], " \t\r"), `\`) {
		return false // a continued command: its program is on another line
	}
	if quotedAt(skel, lineStart[h.line]) {
		return false // the line starts inside an open quote
	}
	line := lines[h.line]
	from, to, sep := segBounds(line, h.start, h.end)
	words := commandWords(line[from:to])
	if len(words) > 0 {
		switch progName(words[0]) {
		case "cat", "tee":
		case "git":
			ok := false
			for _, w := range words[1:] {
				if w == "commit" || w == "tag" || w == "notes" {
					ok = true
				}
			}
			if !ok {
				return false
			}
		default:
			return false
		}
	}
	switch sep {
	case '`', ')', '}':
		return false
	case '(':
		// $( ... ) as an argument of a plain command is data; a subshell,
		// process substitution, command position or nesting is not.
		if from < 2 || line[from-2] != '$' {
			return false
		}
		efrom, _, esep := segBounds(line, from-2, from-2)
		if esep == '(' || esep == '`' {
			return false
		}
		var enc []string
		for _, w := range strings.Fields(line[efrom : from-2]) {
			if w = strings.Trim(w, `"'`); w != "" {
				enc = append(enc, w)
			}
		}
		for len(enc) > 0 && assignWord.MatchString(enc[0]) && !strings.HasSuffix(enc[0], "=") {
			enc = enc[1:]
		}
		if len(enc) == 0 || assignWord.MatchString(enc[0]) {
			return false // command position, or stored in a variable
		}
		n := progName(enc[0])
		if codeWord.MatchString(n) || wrapperWord[n] {
			return false
		}
	}
	return true
}

// BlankDataHeredocs replaces with spaces the bodies of heredocs that are
// data (see isDataHeredoc), keeping the length and line breaks. The command
// lines themselves, redirect targets included, are kept. When any command
// runs a shell or an interpreter, every body is kept: it could run what a
// heredoc wrote. Unterminated heredocs are kept. When in doubt, keep.
func BlankDataHeredocs(c string) string {
	out, _ := dataView(c)
	return out
}

// dataView returns BlankDataHeredocs(c) and, when c has heredocs, the
// skeleton (every closed body blanked) and the closed bodies that are code.
func dataView(c string) (string, *dataInfo) {
	lines := strings.Split(c, "\n")
	hs := scanHeredocs(lines)
	if len(hs) == 0 {
		return c, nil
	}
	skelLines := append([]string(nil), lines...)
	for _, h := range hs {
		if h.closed {
			blankLines(skelLines, h.from, h.to)
		}
	}
	skel := strings.Join(skelLines, "\n")
	lineStart := make([]int, len(lines)+1)
	for i, l := range lines {
		lineStart[i+1] = lineStart[i] + len(l) + 1
	}
	info := &dataInfo{skel: skel, lineStart: lineStart}
	code := runsCode(skel)
	out := append([]string(nil), lines...)
	for _, h := range hs {
		switch {
		case !code && isDataHeredoc(lines, skel, lineStart, h):
			blankLines(out, h.from, h.to)
		case h.closed:
			info.code = append(info.code, h)
		}
	}
	return strings.Join(out, "\n"), info
}

type dataInfo struct {
	skel      string
	lineStart []int
	code      []heredoc
}

// MaskQuotedArgs is BlankDataHeredocs plus: separators inside a quoted
// argument (`"a; set"`, a multi-line -m message) are replaced with spaces,
// so a rule anchored on a command word does not see prose as a command.
// Code stays visible: a `$( )` or backtick inside double quotes, a string
// passed to -c / -e / -Command / eval / iex, and code heredoc bodies.
func MaskQuotedArgs(c string) string {
	view, info := dataView(c)
	if info == nil {
		return maskQuotes(view)
	}
	// Mask the skeleton (no body can open a quote there), then put the code
	// bodies back verbatim at the same offsets.
	b := []byte(maskQuotes(info.skel))
	for _, h := range info.code {
		lo := info.lineStart[h.from] - 1
		hi := info.lineStart[h.to]
		if lo < 0 || hi > len(c) || hi > len(b) {
			continue
		}
		copy(b[lo:hi], c[lo:hi])
	}
	return string(b)
}

func maskByte(ch byte) bool {
	switch ch {
	case '&', '|', ';', '\n', '\r', '(', ')', '{', '}':
		return true
	}
	return false
}

// codeArg reports whether the quote at s[i] is the argument of a flag or
// builtin that runs it as code.
func codeArg(s string, i int) bool {
	if i > 0 && s[i-1] == '$' {
		i-- // $'...'
	}
	j := i
	for j > 0 && s[j-1] != ' ' && s[j-1] != '\t' && s[j-1] != '\n' {
		j--
	}
	tok := strings.ToLower(s[j:i])
	if tok == "" {
		k := j
		for k > 0 && (s[k-1] == ' ' || s[k-1] == '\t') {
			k--
		}
		j = k
		for j > 0 && s[j-1] != ' ' && s[j-1] != '\t' && s[j-1] != '\n' {
			j--
		}
		tok = strings.ToLower(s[j:k])
	}
	return codeFlag.MatchString(tok)
}

var codeFlag = regexp.MustCompile(`^(-[a-z]*[ce]|--?command|--eval|-encodedcommand|eval|iex|invoke-expression)$`)

// matchParen returns the index of the ')' closing the '(' at s[k], or len(s).
func matchParen(s string, k int) int {
	depth := 0
	for ; k < len(s); k++ {
		switch s[k] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(s)
}

// maskQuotes blanks separators inside quoted arguments (see MaskQuotedArgs).
func maskQuotes(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		ch := s[i]
		if ch == '\\' {
			i++
			continue
		}
		if ch != '\'' && ch != '"' {
			continue
		}
		mask := !codeArg(s, i)
		k := i + 1
		for ; k < len(s); k++ {
			c := s[k]
			if c == ch {
				break
			}
			if ch == '"' {
				switch {
				case c == '\\':
					k++
					continue
				case c == '$' && k+1 < len(s) && s[k+1] == '(':
					k = matchParen(s, k+1)
					continue
				case c == '`':
					if e := strings.IndexByte(s[k+1:], '`'); e >= 0 {
						k += e + 1
					} else {
						k = len(s)
					}
					continue
				}
			}
			if mask && maskByte(c) {
				b[k] = ' '
			}
		}
		i = k
	}
	return string(b)
}
