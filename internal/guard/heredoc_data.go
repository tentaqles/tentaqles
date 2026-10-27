package guard

import (
	"regexp"
	"strings"
)

// BlankDataHeredocs replaces every heredoc body of a Bash command with
// spaces (keeping the length and line breaks) when, and only when, the
// whole command is a single command (no `;`, `&&`, `||`, `|`, `&` or second
// line outside the bodies) of one of these shapes:
//
//   - `cat` or `tee` reading a heredoc (`cat > f <<EOF`, `cat <<EOF > f`,
//     `tee f <<EOF`), where no word names a `.git` directory, a git config
//     or a git hook;
//   - `git commit|tag|notes … -F -` (or `-F-`, `--file=-`, `--file -`)
//     reading a heredoc, the subcommand being git's first non-option word;
//   - `git commit|tag … -m "$(cat <<'EOF' … EOF)"` or
//     `gh pr|issue create|edit|comment … --body|--title "$(cat <<EOF … )"`,
//     where the `$( )` holds only `cat <<WORD`.
//
// Anything else (a chain, a pipe, another substitution, a `$` expansion,
// an assignment, any other program, an unterminated heredoc or one with an
// odd delimiter): nothing is blanked and the command is returned unchanged.
// When in doubt, scan.
func BlankDataHeredocs(c string) string {
	lines := strings.Split(c, "\n")
	hs := scanHeredocs(lines)
	if len(hs) == 0 {
		return c
	}
	for _, h := range hs {
		if !h.closed || h.odd {
			return c
		}
	}
	lineStart := make([]int, len(lines)+1)
	for i, l := range lines {
		lineStart[i+1] = lineStart[i] + len(l) + 1
	}
	skelLines := append([]string(nil), lines...)
	for _, h := range hs {
		blankLines(skelLines, h.from, h.to+1) // the closing line too
	}
	skel := []byte(strings.Join(skelLines, "\n"))

	// Replace each `"$(cat <<WORD … )"` argument with a placeholder word, so
	// the command it belongs to reads as one line: `git commit -m "X   "`.
	var subst []int // placeholder offsets
	var marks []int // offsets of heredoc markers not inside a placeholder
	for _, h := range hs {
		line := lines[h.line]
		pre := line[:h.start]
		m := substCatRe.FindStringIndex(pre)
		if m == nil || strings.TrimSpace(line[h.end:]) != "" || h.to+1 >= len(lines) {
			marks = append(marks, lineStart[h.line]+h.start)
			continue
		}
		closeLine := lines[h.to+1]
		p := strings.IndexByte(closeLine, ')')
		if p < 0 || strings.TrimSpace(closeLine[:p]) != "" || !substArgRe.MatchString(pre[:m[0]]) {
			marks = append(marks, lineStart[h.line]+h.start)
			continue
		}
		from, to := lineStart[h.line]+m[0], lineStart[h.to+1]+p
		for k := from; k <= to; k++ {
			skel[k] = ' '
		}
		skel[from] = 'X'
		subst = append(subst, from)
	}

	segs, ok := inertSegments(string(skel))
	if !ok {
		return c
	}
	var one []span
	for _, sp := range segs {
		if strings.TrimSpace(string(skel[sp.start:sp.end])) != "" {
			one = append(one, sp)
		}
	}
	if len(one) != 1 || !dataSegment(string(skel[one[0].start:one[0].end]), len(marks) > 0, len(subst) > 0) {
		return c
	}
	out := append([]string(nil), lines...)
	for _, h := range hs {
		blankLines(out, h.from, h.to)
	}
	return strings.Join(out, "\n")
}

// substCatRe ends a line prefix with `$(cat ` right before the marker.
var substCatRe = regexp.MustCompile(`\$\(\s*cat\s+$`)

// substArgRe ends the text before `$(` with a message/body/title flag and
// an opening double quote.
var substArgRe = regexp.MustCompile(`(^|\s)(-m|--message|-b|--body|-t|--title)(=|\s+)"$`)

// inertSegments splits skel at top-level `;`, `&&`, `||`, `&` and line
// breaks, with comments blanked. ok is false when skel has a pipe or any
// command substitution, backtick or process substitution left.
func inertSegments(skel string) ([]span, bool) {
	var out []span
	lx := newShLexer()
	start := 0
	for i := 0; i < len(skel); i++ {
		ch := skel[i]
		var nb byte
		if i+1 < len(skel) {
			nb = skel[i+1]
		}
		if !lx.comment && !lx.esc && lx.top().kind != 's' && lx.top().kind != 'a' && (ch == '`' || (ch == '$' && nb == '(') || ((ch == '<' || ch == '>') && nb == '(')) {
			return nil, false // a substitution tq does not read
		}
		if lx.live() {
			switch {
			case ch == '#' && wordStart(lx.prev):
				out = append(out, span{start, i})
				start = len(skel) // the rest of the line is a comment
			}
		}
		if lx.root() && start <= i {
			switch {
			case ch == '|' && nb == '|':
				out = append(out, span{start, i})
				lx.step(ch, nb)
				lx.step(nb, 0)
				i++
				start = i + 1
				continue
			case ch == '|':
				return nil, false
			case ch == '&' && (nb == '>' || (i > 0 && (skel[i-1] == '>' || skel[i-1] == '<'))):
				// a redirect (`2>&1`, `&>f`), not a separator
			case ch == ';' || ch == '&' || ch == '\n':
				out = append(out, span{start, i})
				start = i + 1
			}
		}
		if ch == '\n' && start > i {
			start = i + 1 // a comment ends with its line
		}
		i += lx.step(ch, nb)
	}
	if start < len(skel) {
		out = append(out, span{start, len(skel)})
	}
	return out, true
}

// dataSegment judges the one command of a data heredoc: hasHeredoc / hasSubst
// say it reads a heredoc directly or holds a `"$(cat <<…)"` placeholder.
func dataSegment(text string, hasHeredoc, hasSubst bool) bool {
	if strings.ContainsAny(text, "$`") || hasHeredoc == hasSubst {
		return false
	}
	raw := strings.Fields(text)
	if len(raw) == 0 || assignWord.MatchString(raw[0]) {
		return false
	}
	words := commandWords(text)
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "cat", "tee":
		if hasSubst {
			return false
		}
		for _, w := range raw {
			if gitPath(w) {
				return false
			}
		}
		return true
	case "git":
		return dataGit(words[1:], hasSubst)
	case "gh":
		if !hasSubst || len(words) < 3 || (words[1] != "pr" && words[1] != "issue") {
			return false
		}
		switch words[2] {
		case "create", "edit", "comment":
			return true
		}
	}
	return false
}

// gitPathRe matches a path into a .git directory, a git config or a git
// hook: text written there is run by later git commands.
var gitPathRe = regexp.MustCompile(`(?i)(^|[/\\])\.git([/\\]|$)|(^|[/\\])hooks[/\\]|(^|[/\\])(\.?gitconfig|\.gitmodules|config|applypatch-msg|pre-applypatch|post-applypatch|pre-commit|pre-merge-commit|prepare-commit-msg|commit-msg|post-commit|pre-rebase|post-checkout|post-merge|pre-push|pre-receive|update|post-receive|post-update|push-to-checkout|pre-auto-gc|post-rewrite|sendemail-validate|fsmonitor-watchman|reference-transaction|post-index-change|proc-receive)$`)

var redirPrefix = regexp.MustCompile(`^(\d*>>?|&>>?|\d*>&)`)

func gitPath(w string) bool {
	w = strings.Trim(redirPrefix.ReplaceAllString(w, ""), `"'`)
	return w != "" && gitPathRe.MatchString(w)
}

// dataGit judges a git call (args after `git`): only -C and --no-pager
// before the subcommand; commit, tag or notes reading a heredoc with -F -,
// or commit or tag with a "$(cat …)" message.
func dataGit(args []string, hasSubst bool) bool {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-C":
			i += 2
		case "--no-pager", "-P":
			i++
		default:
			return false // -c, --exec-path, --git-dir, …
		}
	}
	if i >= len(args) {
		return false
	}
	sub, rest := args[i], args[i+1:]
	switch {
	case hasSubst:
		return sub == "commit" || sub == "tag"
	case sub == "commit" || sub == "tag" || sub == "notes":
		return readsStdinFile(rest)
	}
	return false
}

func readsStdinFile(args []string) bool {
	for j, a := range args {
		switch {
		case a == "-F-" || a == "--file=-":
			return true
		case (a == "-F" || a == "--file") && j+1 < len(args) && args[j+1] == "-":
			return true
		}
	}
	return false
}

var assignWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

var redirWord = regexp.MustCompile(`^(\d*>>?|\d*>&|&>>?|<)`)

// commandWords is the words of a segment without heredoc markers and
// redirects (with their targets).
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
		out = append(out, w)
	}
	return out
}
