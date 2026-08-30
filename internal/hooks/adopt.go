package hooks

import (
	"errors"
	"os"
	"regexp"
	"strings"
)

// Adoption of hand-installed hooks.
//
// Before tq shipped `tq hooks install`, the block was pasted into profiles by
// hand. Its shape is:
//
//	# --- tq (managed by Tentaqles; ...) ---
//	if ($env:TQ_ENABLED -eq '0') {      # PowerShell: the *legacy* launcher
//	    ...
//	} else {                            #             the tq activation
//	    ...
//	    function claude { ... --dangerously-skip-permissions @args }
//	}
//
//	# --- tq (managed by Tentaqles; ...) ---
//	if [ "${TQ_ENABLED:-1}" != "0" ]; then   # bash/zsh: no legacy branch
//	    ...
//	fi
//
// Adopt replaces that block with tq's marker-delimited managed block, keeping
// the legacy branch verbatim inside a `# >>> tq-legacy >>>` wrapper and
// carrying over the user's `claude` wrapper function. Every byte outside the
// located block — BOM, line endings and all — is preserved.

const (
	// LegacyHeaderPrefix is the first line of a hand-installed block.
	LegacyHeaderPrefix = "# --- tq (managed by Tentaqles"

	// LegacyStartMarker and LegacyEndMarker delimit the preserved legacy
	// branch inside an adopted profile. Nothing removes it automatically:
	// it is the user's pre-tq fallback, and it is theirs to delete once
	// they are confident they no longer need TQ_ENABLED=0.
	LegacyStartMarker = "# >>> tq-legacy >>>"
	LegacyEndMarker   = "# <<< tq-legacy <<<"

	// legacyNote must not promise a command that does not exist: it is
	// written into the user's real profile and outlives this release.
	legacyNote = "  (your pre-tq setup, kept verbatim by tq migrate; delete this block by hand when you no longer need TQ_ENABLED=0)"

	// CarryComment introduces a shell function carried over verbatim from the
	// hand-installed block.
	CarryComment = "# carried over by tq migrate; permission_mode will move into the manifest"

	// carriedGuard reproduces the `else` branch the carried wrapper used to
	// live in, so setting TQ_ENABLED=0 still hands control to the legacy
	// launcher's own definitions.
	carriedGuard = "if ($env:TQ_ENABLED -ne '0') {"
)

// Reasons Adopt reports when it declines to change a profile. They are
// human-readable; callers should branch on ChangeSet.Found / .Changed, not on
// the text.
const (
	ReasonNoProfilePath = "no profile path for this shell"
	ReasonNoFile        = "profile does not exist"
	ReasonManaged       = "profile already contains a tq-managed block"
	ReasonUnrecognised  = "no recognisable hand-installed tq block"
)

// ChangeSet is the result of planning an adoption for one profile. It is
// inert until Apply is called.
type ChangeSet struct {
	Shell   Shell
	Profile string

	// Old and New are the decoded (UTF-8, BOM-stripped) file contents before
	// and after. New == Old when Changed is false.
	Old string
	New string

	// Start and End are byte offsets into Old delimiting the hand-installed
	// block that New replaces. Old[:Start] and Old[End:] are carried over
	// unchanged. End includes the closing line's terminator.
	Start int
	End   int

	// Found reports whether a hand-installed block was located at all. A
	// profile that is unmanaged but unrecognised has Found == false and should
	// be reported to the user for manual cleanup rather than rewritten.
	Found bool

	// Changed reports whether Apply would write anything.
	Changed bool

	// Reason explains why Changed is false.
	Reason string

	// Legacy is the verbatim body of the TQ_ENABLED=0 branch, ending with its
	// own line terminator. Empty when the shell has no legacy branch.
	Legacy string

	// Wrapper is the carried-over `function claude { ... }` definition,
	// dedented to column 0 and without a trailing newline. Empty when there is
	// none.
	Wrapper string

	// Dropped lists non-scaffolding lines from the replaced tq-activation
	// branch that are not carried over — in practice the hand-rolled PATH
	// munging. Callers should surface these as warnings.
	Dropped []string

	enc    encoding
	mode   os.FileMode
	exists bool
}

// Bytes returns the new file contents, re-encoded in the profile's original
// encoding (BOM and endianness included).
func (c ChangeSet) Bytes() []byte { return encodeProfile(c.New, c.enc) }

// Detail returns a one-line human summary suitable for a migration plan.
func (c ChangeSet) Detail() string {
	if !c.Changed {
		return c.Reason
	}
	parts := []string{"replace hand-installed block with the managed one"}
	if c.Legacy != "" {
		parts = append(parts, "keep legacy launcher verbatim")
	}
	if c.Wrapper != "" {
		parts = append(parts, "carry over the claude wrapper")
	}
	return strings.Join(parts, "; ")
}

// Apply backs the profile up to "<profile>.tq-backup" (once, ever) and writes
// the new contents atomically. It is a no-op when Changed is false.
func (c ChangeSet) Apply() error {
	if !c.Changed {
		return nil
	}
	if c.Profile == "" {
		return errors.New("hooks: ChangeSet has no profile path")
	}
	if err := backupProfile(c.Profile); err != nil {
		return err
	}
	return writeProfile(c.Profile, c.Bytes(), c.mode)
}

// Adopt plans the adoption of sh's profile in p. It never writes; call
// ChangeSet.Apply to commit.
func Adopt(sh Shell, p Profiles) (ChangeSet, error) {
	profile, ok := p[sh]
	if !ok || profile == "" {
		return ChangeSet{Shell: sh, Reason: ReasonNoProfilePath, enc: encUTF8, mode: 0o644}, nil
	}
	return AdoptFile(profile, sh)
}

// AdoptFile is Adopt against an explicit path.
func AdoptFile(path string, sh Shell) (ChangeSet, error) {
	content, enc, mode, exists, err := readProfile(path)
	cs := ChangeSet{Shell: sh, Profile: path, Old: content, New: content, enc: enc, mode: mode, exists: exists}
	if err != nil {
		return cs, err
	}
	if !exists {
		cs.Reason = ReasonNoFile
		return cs, nil
	}
	if strings.Contains(content, startMarker) || strings.Contains(content, endMarker) {
		cs.Reason = ReasonManaged
		return cs, nil
	}

	lp, ok := parseLegacy(content, sh)
	if !ok {
		cs.Reason = ReasonUnrecognised
		return cs, nil
	}
	cs.Found = true
	cs.Start, cs.End = lp.blockStart, lp.blockEnd

	block := content[lp.blockStart:lp.blockEnd]
	eol := "\n"
	if strings.Contains(block, "\r\n") {
		eol = "\r\n"
	} else if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}

	cs.Legacy = lp.legacyBody(content)
	cs.Wrapper = lp.carriedWrapper(content)
	cs.Dropped = lp.droppedLines(content, cs.Wrapper)

	var b strings.Builder
	if cs.Legacy != "" {
		b.WriteString(LegacyStartMarker + legacyNote + eol)
		b.WriteString(lp.legacyIfLine(content) + eol)
		b.WriteString(normalizeEOL(cs.Legacy, eol))
		b.WriteString("}" + eol)
		b.WriteString(LegacyEndMarker + eol)
		b.WriteString(eol)
	}
	b.WriteString(normalizeEOL(Block(sh), eol))
	if cs.Wrapper != "" {
		b.WriteString(eol)
		b.WriteString(CarryComment + eol)
		// In the hand-installed block this function lived in the `else` branch,
		// so it only took effect when the legacy launcher did not run. Keep
		// that guard: without it, TQ_ENABLED=0 would fall back to the legacy
		// launcher and then have its own `claude` router clobbered by this
		// wrapper.
		guarded := cs.Legacy != ""
		if guarded {
			b.WriteString(carriedGuard + eol)
		}
		b.WriteString(normalizeEOL(cs.Wrapper, eol))
		if !strings.HasSuffix(cs.Wrapper, "\n") {
			b.WriteString(eol)
		}
		if guarded {
			b.WriteString("}" + eol)
		}
	}

	cs.New = content[:lp.blockStart] + b.String() + content[lp.blockEnd:]
	cs.Changed = cs.New != content
	if !cs.Changed {
		cs.Reason = "nothing to do"
	}
	return cs, nil
}

// FindLegacyBlock locates the hand-installed tq block in content and returns
// its byte offsets. content[start:end] is the whole block, including the
// header comment line and the terminator of its closing line, so
// content[:start] + replacement + content[end:] rebuilds the file.
func FindLegacyBlock(content string, sh Shell) (start, end int, ok bool) {
	lp, ok := parseLegacy(content, sh)
	if !ok {
		return 0, 0, false
	}
	return lp.blockStart, lp.blockEnd, true
}

// SplitLegacy returns the verbatim body of the TQ_ENABLED=0 branch of a block
// previously returned by FindLegacyBlock. The body keeps its original
// indentation and ends with its own line terminator. Only PowerShell profiles
// have such a branch; bash, zsh and fish return ok == false.
func SplitLegacy(block string, sh Shell) (legacyBranch string, ok bool) {
	lp, found := parseLegacy(block, sh)
	if !found {
		return "", false
	}
	body := lp.legacyBody(block)
	if body == "" {
		return "", false
	}
	return body, true
}

// ---------------------------------------------------------------------------
// parsing
// ---------------------------------------------------------------------------

type srcLine struct {
	start int // byte offset of the first byte of the line
	end   int // byte offset just past the line terminator
	text  string
}

func splitSrcLines(s string) []srcLine {
	var out []srcLine
	i := 0
	for i < len(s) {
		j := strings.IndexByte(s[i:], '\n')
		if j < 0 {
			out = append(out, srcLine{i, len(s), strings.TrimSuffix(s[i:], "\r")})
			break
		}
		out = append(out, srcLine{i, i + j + 1, strings.TrimSuffix(s[i:i+j], "\r")})
		i = i + j + 1
	}
	return out
}

// legacyParse records where the interesting pieces of a hand-installed block
// live, as byte offsets into the string it was parsed from.
type legacyParse struct {
	shell      Shell
	blockStart int
	blockEnd   int

	// PowerShell: braces of the TQ_ENABLED=0 branch and of the else branch.
	ifLineStart int
	ifLineEnd   int
	open        int
	close       int
	elseOpen    int // -1 when there is no else branch
	elseClose   int

	// bash/zsh: the body of the single `if … then … fi`.
	bodyStart int
	bodyEnd   int
}

func parseLegacy(content string, sh Shell) (legacyParse, bool) {
	lines := splitSrcLines(content)
	hdr := -1
	for k, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l.text), LegacyHeaderPrefix) {
			hdr = k
			break
		}
	}
	if hdr < 0 {
		return legacyParse{}, false
	}
	// First meaningful line after the header must open the guard.
	ifIdx := -1
	for k := hdr + 1; k < len(lines); k++ {
		t := strings.TrimSpace(lines[k].text)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		ifIdx = k
		break
	}
	if ifIdx < 0 {
		return legacyParse{}, false
	}
	guard := strings.TrimSpace(lines[ifIdx].text)
	if !strings.HasPrefix(guard, "if") || !strings.Contains(guard, "TQ_ENABLED") {
		return legacyParse{}, false
	}

	lp := legacyParse{
		shell:       sh,
		blockStart:  lines[hdr].start,
		ifLineStart: lines[ifIdx].start,
		ifLineEnd:   lines[ifIdx].end,
		elseOpen:    -1,
		elseClose:   -1,
	}

	switch sh {
	case "powershell", "pwsh":
		ob := strings.IndexByte(content[lines[ifIdx].start:lines[ifIdx].end], '{')
		if ob < 0 {
			return legacyParse{}, false
		}
		lp.open = lines[ifIdx].start + ob
		cl, ok := psMatchBrace(content, lp.open)
		if !ok {
			return legacyParse{}, false
		}
		lp.close = cl
		last := cl
		// Consume `} else {` / `} elseif (…) {` chains on the same line.
		for {
			rest := content[last+1 : lineEndOf(content, last)]
			t := strings.TrimSpace(strings.TrimSuffix(rest, "\r"))
			if !strings.HasPrefix(t, "else") {
				break
			}
			ob := strings.IndexByte(rest, '{')
			if ob < 0 {
				break
			}
			open := last + 1 + ob
			cl, ok := psMatchBrace(content, open)
			if !ok {
				return legacyParse{}, false
			}
			if lp.elseOpen < 0 {
				lp.elseOpen, lp.elseClose = open, cl
			} else {
				lp.elseClose = cl
			}
			last = cl
		}
		lp.blockEnd = lineEndOf(content, last)
		if lp.blockEnd < len(content) && content[lp.blockEnd] == '\n' {
			lp.blockEnd++
		}
		return lp, true

	case "bash", "zsh":
		if !strings.HasSuffix(guard, "then") {
			return legacyParse{}, false
		}
		depth := 0
		for k := ifIdx; k < len(lines); k++ {
			depth += shIfDelta(lines[k].text)
			if depth <= 0 {
				lp.bodyStart = lines[ifIdx].end
				lp.bodyEnd = lines[k].start
				lp.blockEnd = lines[k].end
				return lp, true
			}
		}
		return legacyParse{}, false
	}
	return legacyParse{}, false
}

// legacyBody returns the verbatim TQ_ENABLED=0 branch body (PowerShell only).
func (lp legacyParse) legacyBody(content string) string {
	if lp.shell != "powershell" && lp.shell != "pwsh" {
		return ""
	}
	start := lp.ifLineEnd
	end := lineStartOf(content, lp.close)
	// A closing brace that shares its line with code (`Set-IdentityEnv }`)
	// must not swallow that code.
	if strings.TrimSpace(content[end:lp.close]) != "" {
		end = lp.close
	}
	if end <= start {
		return ""
	}
	return content[start:end]
}

// legacyIfLine returns the original guard line, dedented and without its
// terminator, so the preserved branch keeps the exact condition the user wrote.
func (lp legacyParse) legacyIfLine(content string) string {
	line := content[lp.ifLineStart:lp.ifLineEnd]
	line = strings.TrimRight(line, "\r\n")
	return strings.TrimLeft(line, " \t")
}

var psClaudeWrapperRe = regexp.MustCompile(`(?m)^([ \t]*)function[ \t]+claude[ \t]*\{`)

// carriedWrapper returns the `function claude { … }` definition from the
// tq-activation (else) branch, dedented to column 0.
func (lp legacyParse) carriedWrapper(content string) string {
	if lp.shell != "powershell" && lp.shell != "pwsh" {
		return ""
	}
	if lp.elseOpen < 0 || lp.elseClose <= lp.elseOpen {
		return ""
	}
	region := content[lp.elseOpen+1 : lp.elseClose]
	m := psClaudeWrapperRe.FindStringSubmatchIndex(region)
	if m == nil {
		return ""
	}
	open := strings.IndexByte(region[m[0]:m[1]], '{') + m[0]
	cl, ok := psMatchBrace(region, open)
	if !ok {
		return ""
	}
	fn := region[m[0] : cl+1]
	if !strings.Contains(fn, "--dangerously-skip-permissions") {
		return ""
	}
	indent := region[m[2]:m[3]]
	return dedent(fn, indent)
}

// droppedLines lists lines of the replaced tq-activation branch that are not
// part of its standard scaffolding and are not carried over — the hand-rolled
// PATH munging, in practice.
func (lp legacyParse) droppedLines(content, wrapper string) []string {
	var region string
	switch lp.shell {
	case "powershell", "pwsh":
		if lp.elseOpen < 0 || lp.elseClose <= lp.elseOpen {
			return nil
		}
		region = content[lp.elseOpen+1 : lp.elseClose]
	case "bash", "zsh":
		if lp.bodyEnd <= lp.bodyStart {
			return nil
		}
		region = content[lp.bodyStart:lp.bodyEnd]
	default:
		return nil
	}
	carried := map[string]bool{}
	for _, l := range strings.Split(strings.ReplaceAll(wrapper, "\r\n", "\n"), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			carried[t] = true
		}
	}
	var out []string
	for _, l := range splitSrcLines(region) {
		t := strings.TrimSpace(l.text)
		if t == "" || strings.HasPrefix(t, "#") || carried[t] {
			continue
		}
		if isScaffolding(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// isScaffolding reports whether a line is part of the boilerplate the managed
// block replaces (the guard, the activation call, the not-found warning and
// bare block delimiters).
func isScaffolding(t string) bool {
	for _, s := range []string{
		"TQ_ENABLED",
		"tq activate",
		"Get-Command tq",
		"command -v tq",
		"Write-Warning",
	} {
		if strings.Contains(t, s) {
			return true
		}
	}
	switch t {
	case "{", "}", "} else {", "else {", "else", "fi", "then", "esac", "};":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// tiny lexers
// ---------------------------------------------------------------------------

// psMatchBrace returns the index of the `}` matching the `{` at open,
// skipping braces inside single- and double-quoted strings, line comments and
// <# … #> block comments. Here-strings (@" … "@) are not handled; a profile
// using one inside the tq block is reported as unrecognised rather than
// mis-parsed, because the resulting offsets would fail the else/`}` checks.
func psMatchBrace(s string, open int) (int, bool) {
	if open < 0 || open >= len(s) || s[open] != '{' {
		return 0, false
	}
	depth := 0
	inSingle, inDouble, inLine, inBlock := false, false, false, false
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
			}
		case inBlock:
			if c == '#' && i+1 < len(s) && s[i+1] == '>' {
				inBlock = false
				i++
			}
		case inSingle:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			switch c {
			case '`':
				i++
			case '"':
				inDouble = false
			}
		default:
			switch c {
			case '<':
				if i+1 < len(s) && s[i+1] == '#' {
					inBlock = true
					i++
				}
			case '#':
				inLine = true
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case '`':
				i++
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return i, true
				}
			}
		}
	}
	return 0, false
}

// shIfDelta returns the net if/fi nesting change contributed by one shell line.
func shIfDelta(line string) int {
	// Strip a trailing comment that starts at a word boundary and outside quotes.
	inSingle, inDouble := false, false
	cut := len(line)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			if c == '\\' {
				i++
			} else if c == '"' {
				inDouble = false
			}
		default:
			switch c {
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case '#':
				if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' || line[i-1] == ';' {
					cut = i
				}
			}
		}
		if cut != len(line) {
			break
		}
	}
	delta := 0
	for _, tok := range strings.FieldsFunc(line[:cut], func(r rune) bool {
		switch r {
		case ' ', '\t', ';', '&', '|', '(', ')':
			return true
		}
		return false
	}) {
		switch tok {
		case "if":
			delta++
		case "fi":
			delta--
		}
	}
	return delta
}

// ---------------------------------------------------------------------------
// small string helpers
// ---------------------------------------------------------------------------

func lineStartOf(s string, i int) int {
	if j := strings.LastIndexByte(s[:i], '\n'); j >= 0 {
		return j + 1
	}
	return 0
}

// lineEndOf returns the index of the '\n' terminating i's line, or len(s).
func lineEndOf(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(s)
}

// normalizeEOL rewrites every line ending in s to eol.
func normalizeEOL(s, eol string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if eol == "\n" {
		return s
	}
	return strings.ReplaceAll(s, "\n", eol)
}

// dedent removes indent from the start of every line of s that has it.
func dedent(s, indent string) string {
	if indent == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, indent)
	}
	return strings.Join(lines, "\n")
}
