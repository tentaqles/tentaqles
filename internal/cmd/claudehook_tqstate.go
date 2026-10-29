package cmd

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// The hook-time halves of tq/hub-store-write, tq/human-answer and
// tq/pack-config-change (the static halves are in
// internal/policy/builtin_framework.go). They are built from the real state
// directory at hook time (paths.Home(), its final path and its 8.3 short
// form), so a custom TQ_HOME, a short name, a junction or a quoting trick
// does not slip past a fixed regex. All three ids are never-disable.

// normShell folds a command for mention matching: lower case, forward
// slashes, quotes, backticks and glob brackets removed, /./ and // collapsed.
func normShell(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, `\`, "/")
	s = strings.NewReplacer(`"`, "", `'`, "", "`", "", "[", "", "]", "").Replace(s)
	for {
		n := strings.ReplaceAll(strings.ReplaceAll(s, "/./", "/"), "//", "/")
		if n == s {
			break
		}
		s = n
	}
	return s
}

// normPathForm is a path in the normShell form.
func normPathForm(p string) string {
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(normShell(filepath.Clean(p)), "/")
}

// stateDirForms are the spellings of tq's state directory a command may use.
func stateDirForms() []string { return stateInfo().forms }

func computeStateDirForms() []string {
	home := paths.Home()
	set := map[string]bool{".tentaqles": true, "tentaq~": true, "$tq_home": true, "${tq_home}": true, "%tq_home%": true, "$env:tq_home": true}
	var dirs []string
	add := func(p string) {
		if p != "" {
			dirs = append(dirs, p)
		}
	}
	add(home)
	if abs, err := filepath.Abs(home); err == nil {
		add(abs)
		if real, err := packs.FinalPath(abs); err == nil {
			add(real)
		}
		add(shortPath(abs))
	}
	uh, _ := os.UserHomeDir()
	for _, d := range dirs {
		n := normPathForm(d)
		if n == "" {
			continue
		}
		set[n] = true
		// Cygwin and git-bash spellings of a drive path: /c/users/..., /mnt/c/...
		if len(n) > 2 && n[1] == ':' {
			set["/"+n[:1]+n[2:]] = true
			set["/mnt/"+n[:1]+n[2:]] = true
		}
		if uh != "" {
			if rel, err := filepath.Rel(uh, d); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
				r := normShell(filepath.ToSlash(rel))
				for _, h := range []string{"~/", "$home/", "${home}/", "$env:userprofile/", "%userprofile%/", "$env:home/"} {
					set[h+r] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

var (
	dotdotRe    = regexp.MustCompile(`(^|/)\.\.(/|$|\s)`)
	proseSplit  = regexp.MustCompile(`(;|&&|\|\||\n)`)
	shortRootRe = regexp.MustCompile(`/tentaq~[0-9]+/`)
)

// stateCache memoizes, once per process and per (TQ_HOME, environment),
// everything the hook-time check derives from the file system and the
// environment: the root spellings (FinalPath and 8.3 calls), the protected
// set (8.3 names of the protected children), the inside variables and their
// regexes. The hook is a one-shot process; tests that change TQ_HOME or the
// environment get a fresh entry.
type stateCache struct {
	key   string
	forms []string
	re    *rootMatcher
	prot  map[string]bool
	vars  map[string]string
	bare  map[string]*regexp.Regexp
}

var stateMemo struct {
	sync.Mutex
	c *stateCache
}

func stateInfo() *stateCache {
	h := fnv.New64a()
	h.Write([]byte(paths.Home()))
	for _, kv := range os.Environ() {
		h.Write([]byte{0})
		h.Write([]byte(kv))
	}
	key := fmt.Sprintf("%x", h.Sum64())
	stateMemo.Lock()
	defer stateMemo.Unlock()
	if c := stateMemo.c; c != nil && c.key == key {
		return c
	}
	c := &stateCache{key: key, prot: computeProtectedSet(), vars: computeInsideVars(), bare: map[string]*regexp.Regexp{}}
	c.forms = computeStateDirForms()
	c.re = computeRootRegex(c.forms)
	for k := range c.vars {
		c.bare[k] = regexp.MustCompile(regexp.QuoteMeta(k) + `($|[^a-z0-9_])`)
	}
	stateMemo.c = c
	return c
}

var bareTq = regexp.MustCompile(`^\s*(\S*/)?tq(\.exe)?\s`)

// bareTqCommand: one tq invocation and nothing else (no chain, pipe,
// redirect or substitution).
func bareTqCommand(n string) bool {
	return bareTq.MatchString(n) && !strings.ContainsAny(n, ";&|<>\n`") && !strings.Contains(n, "$(")
}

// tqSelfPath is the running tq binary; tqLookPath resolves a bare `tq` on
// PATH. Tests replace both.
var (
	tqSelfPath = func() string {
		p, err := os.Executable()
		if err != nil {
			return ""
		}
		return p
	}
	tqLookPath = exec.LookPath
)

// samePath compares two executables by their final paths.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	fa, fb := a, b
	if f, err := packs.FinalPath(a); err == nil {
		fa = f
	}
	if f, err := packs.FinalPath(b); err == nil {
		fb = f
	}
	return normPathForm(fa) == normPathForm(fb)
}

// isSelfTq reports whether a command word runs this tq: the running
// binary's own path, or a bare tq (tq.exe) that resolves on PATH to it.
// Any other path (./tq, a link elsewhere) is just a program.
func isSelfTq(word string) bool {
	w := strings.Trim(word, `"'`)
	self := tqSelfPath()
	switch strings.ToLower(w) {
	case "tq", "tq.exe":
		p, err := tqLookPath("tq")
		return err == nil && samePath(p, self)
	}
	if !filepath.IsAbs(filepath.FromSlash(w)) && !isAbsSlash(w) {
		return false
	}
	return samePath(w, self)
}

func isAbsSlash(p string) bool {
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.HasPrefix(p, "/") || (len(p) >= 3 && p[1] == ':' && p[2] == '/')
}

// tqSafeSubcommands never run a child command or write a path argument
// (tq run, tq dotenv run and tq redact run run one; tq redact, migrate,
// bundle, settings and the rest are checked like any program).
var tqSafeSubcommands = map[string]bool{"pack": true, "hub": true, "doctor": true, "guard": true, "list": true,
	"insights": true, "version": true, "decide": true, "providers": true, "secrets": true, "help": true}

// tqExempt reports a command the state-dir check leaves to tq itself: one
// call (bareTqCommand) of this tq (isSelfTq) with a safe subcommand as its
// first argument. A child command after -- (tq pack cloud plan -- terraform
// plan) is checked as its own command, so its mention is not exempt.
func tqExempt(cmd, mention string) bool {
	if !bareTqCommand(normShell(cmd)) {
		return false
	}
	w := strings.Fields(mention)
	if len(w) < 2 || !isSelfTq(w[0]) || !tqSafeSubcommands[strings.ToLower(w[1])] {
		return false
	}
	for i, x := range w {
		if x == "--" {
			return !protectedMention(strings.Join(w[i+1:], " "))
		}
	}
	// tq pack <x> plan runs its plan command without a -- too (cobra stops
	// at the first positional): every word after plan is checked as the
	// child. Its own flags (--env, --timeout, --json) name no path.
	if strings.EqualFold(w[1], "pack") {
		for i := 2; i < len(w); i++ {
			if strings.EqualFold(w[i], "plan") {
				return !protectedMention(strings.Join(w[i+1:], " "))
			}
		}
	}
	return true
}

// protectedSubs are the parts of tq's state directory whose integrity a
// gate relies on: the hub store and its serve info (hub/), the pack
// ledgers, claims, plans and state dirs (packs/), the build gate's records
// and HMAC key (build-gate/), the manifest trust store (trust/), the global
// guard settings (guard.yaml), the registry of bases (config.yaml: editing
// it can drop a session's workspace and with it the client's guard rules),
// and the guard and audit logs (guard/, audit.jsonl). The rest
// (identities/<id>/claude/..., bundles) is the agents' normal working area.
var protectedSubs = []string{"hub", "packs", "build-gate", "trust", "guard.yaml", "config.yaml", "guard", "audit.jsonl"}

// protectedSet is protectedSubs plus their 8.3 short names at hook time.
func protectedSet() map[string]bool { return stateInfo().prot }

func computeProtectedSet() map[string]bool {
	set := map[string]bool{}
	for _, p := range protectedSubs {
		set[p] = true
		if s := shortPath(filepath.Join(paths.Home(), p)); s != "" {
			set[strings.ToLower(filepath.Base(s))] = true
		}
	}
	return set
}

func isProtectedSeg(seg string) bool { return protectedSet()[seg] }

func (c *stateCache) isProt(seg string) bool { return c.prot[seg] }

// insideVars are the environment variables whose value points inside tq's
// state directory (tq exports CLAUDE_CONFIG_DIR, GH_CONFIG_DIR and others
// into identities/<id>/): name (lower case) -> the folded value.
func insideVars() map[string]string { return stateInfo().vars }

func computeInsideVars() map[string]string {
	out := map[string]string{}
	home := normPathForm(paths.Home())
	var real string
	if r, err := packs.FinalPath(paths.Home()); err == nil {
		real = normPathForm(r)
	}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" || strings.EqualFold(k, "TQ_HOME") {
			continue
		}
		nv := normPathForm(v)
		if (home != "" && (nv == home || strings.HasPrefix(nv, home+"/"))) || (real != "" && (nv == real || strings.HasPrefix(nv, real+"/"))) {
			out[strings.ToLower(k)] = nv
		}
	}
	return out
}

// expandInside replaces every spelling of an inside variable ($X, ${X},
// %X%, $env:X, and the bare name as in os.environ[X] or process.env.X) with
// its folded value, so `$CLAUDE_CONFIG_DIR/../../../packs` reads as a path.
func expandInside(n string) string {
	si := stateInfo()
	vars := si.vars
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, k := range names {
		v := vars[k]
		if !strings.Contains(n, k) {
			continue // every spelling contains the name
		}
		for _, sp := range []string{"${" + k + "}", "$env:" + k, "%" + k + "%", "$" + k} {
			n = strings.ReplaceAll(n, sp, v)
		}
		// The bare name (os.environ[X], process.env.X): quotes and brackets are
		// already folded away, so no left boundary is required.
		if strings.Contains(n, k) {
			n = si.bare[k].ReplaceAllString(n, v+"${1}")
		}
	}
	return n
}

// rootRegex matches any spelling of the state directory root.
func rootRegex() *rootMatcher { return stateInfo().re }

// rootMatcher finds the state-directory root spellings: `tentaq~<digits>`
// and the literal forms, leftmost first and, at one position, the 8.3 form
// then the longest literal (the order of the alternation it replaces, which
// was a large regexp and too slow on a 64 KB command). Literal search uses
// strings.Index.
type rootMatcher struct {
	forms []string // longest first
}

func computeRootRegex(forms []string) *rootMatcher {
	var fs []string
	for _, f := range forms {
		if f != "" && f != "tentaq~" {
			fs = append(fs, f)
		}
	}
	sort.SliceStable(fs, func(i, j int) bool { return len(fs[i]) > len(fs[j]) })
	return &rootMatcher{forms: fs}
}

// shortAt returns the end of `tentaq~<digits>` at i, or -1.
func shortAt(s string, i int) int {
	const p = "tentaq~"
	if !strings.HasPrefix(s[i:], p) {
		return -1
	}
	j := i + len(p)
	k := j
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	if k == j {
		return -1
	}
	return k
}

// all returns every match, leftmost first and non-overlapping, in one pass
// per form: the candidate starts of each form are collected, and at each
// start the 8.3 form wins, then the longest literal.
func (m *rootMatcher) all(s string, firstOnly bool) [][]int {
	end := map[int]int{}
	var starts []int
	add := func(i, e int, short bool) {
		if old, ok := end[i]; ok {
			if !short && old >= e {
				return
			}
			if !short && old != e && shortAt(s, i) == old {
				return // the 8.3 form wins at its start
			}
		} else {
			starts = append(starts, i)
		}
		end[i] = e
	}
	for i := 0; ; {
		k := strings.Index(s[i:], "tentaq~")
		if k < 0 {
			break
		}
		if e := shortAt(s, i+k); e >= 0 {
			add(i+k, e, true)
		}
		i += k + 1
	}
	for _, f := range m.forms {
		for i := 0; ; {
			k := strings.Index(s[i:], f)
			if k < 0 {
				break
			}
			add(i+k, i+k+len(f), false)
			i += k + 1
		}
	}
	sort.Ints(starts)
	var out [][]int
	from := 0
	for _, st := range starts {
		if st < from {
			continue
		}
		out = append(out, []int{st, end[st]})
		if firstOnly {
			break
		}
		from = end[st]
	}
	return out
}

func (m *rootMatcher) FindStringIndex(s string) []int {
	if l := m.all(s, true); len(l) > 0 {
		return l[0]
	}
	return nil
}

func (m *rootMatcher) MatchString(s string) bool { return m.FindStringIndex(s) != nil }

func (m *rootMatcher) FindAllStringIndex(s string, _ int) [][]int { return m.all(s, false) }

var (
	cdRe     = regexp.MustCompile(`^\s*(cd|pushd|chdir|set-location|sl)\s+(\S+)\s*$`)
	linkVerb = regexp.MustCompile(`\b(ln|mklink|new-item\b.*-itemtype\s+(junction|symboliclink|hardlink)|cmd\s+/c\s+mklink)\b`)
	msgArg   = regexp.MustCompile(`(?i)\s(-m|-am|--message)(=|\s+)("([^"\\]|\\.)*"|'[^']*'|\S+)`)
	echoSeg  = regexp.MustCompile(`(?i)^\s*(echo|write-host|write-output|printf)\b`)
)

// Hand-written splitters for the hot loops (the regexps they replace were
// the cost on a 64 KB command). isTokenSep separates tokens (whitespace and
// ;&|()<>,=); isSegSep separates segments (; & | and newline; a run yields
// no empty segment, which the loops skipped anyway); isWordSep separates
// words (whitespace and ()<>=,).
func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' // Go's \s
}
func isTokenSep(r rune) bool { return isSpace(r) || strings.ContainsRune(";&|()<>,=", r) }
func isSegSep(r rune) bool   { return r == ';' || r == '&' || r == '|' || r == '\n' }
func isWordSep(r rune) bool  { return isSpace(r) || strings.ContainsRune("()<>=,", r) }

// mapTokens replaces every maximal run of non-separator characters (a
// token) with f(run).
func mapTokens(s string, f func(string) string) string {
	var b strings.Builder
	b.Grow(len(s))
	start := -1
	for i, r := range s {
		if isTokenSep(r) {
			if start >= 0 {
				b.WriteString(f(s[start:i]))
				start = -1
			}
			b.WriteRune(r)
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		b.WriteString(f(s[start:]))
	}
	return b.String()
}

func firstSeg(s string) string {
	if i := strings.IndexAny(s, "/ \t;&|)<>,"); i >= 0 {
		return s[:i]
	}
	return s
}

// proseFree blanks text that is never a path: a commit message (-m,
// --message) and an echo segment that redirects nowhere.
func proseFree(raw string) string {
	if l := strings.ToLower(raw); strings.Contains(l, "-m") || strings.Contains(l, "-am") {
		raw = msgArg.ReplaceAllString(raw, " ")
	}
	var out []string
	for _, seg := range proseSplit.Split(raw, -1) {
		if echoSeg.MatchString(seg) && !strings.ContainsAny(seg, ">|") {
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, " ; ")
}

// foldPaths folds a command for path matching: inside variables expanded,
// string concatenation (+) dropped, and `..` resolved lexically in every
// path-like token. It also reports a root mention followed by `..`.
func foldPaths(n string, re *rootMatcher) (string, bool) {
	n = strings.ReplaceAll(expandInside(n), "+", "")
	dotdot := false
	n = mapTokens(n, func(tok string) string {
		if !strings.Contains(tok, "/") || !strings.Contains(tok, "..") {
			return tok
		}
		if loc := re.FindStringIndex(tok); loc != nil && strings.Contains(tok[loc[1]:], "/..") {
			dotdot = true
		}
		if strings.Contains(tok, "..") {
			lead := ""
			if strings.HasPrefix(tok, "~") {
				lead, tok = "~", tok[1:]
			}
			c := path.Clean(tok)
			return lead + c
		}
		return tok
	})
	return n, dotdot
}

// rawDots is the command with only quotes, backticks and + concatenation
// folded, for the .. check (the folded form has already resolved them).
func rawDots(raw string) string { return strings.ReplaceAll(normShell(raw), "+", "") }

// protectedMention reports whether a folded command can reach a protected
// part of the state directory:
//   - a root spelling followed by a protected or a glob segment (after `..`
//     is resolved and inside variables are expanded);
//   - a root spelling followed by `..` anywhere in its token (fail closed);
//   - the bare root and a protected name inside another path-like token
//     (D=~/.tentaqles; … $D/packs);
//   - a link created at or under the root;
//   - a cd under the root (tracked cumulatively, relative cds included)
//     followed by a relative path into a protected part.
func protectedMention(raw string) bool {
	si := stateInfo()
	re := si.re
	n, dotdot := foldPaths(normShell(proseFree(raw)), re)
	// Fail closed: a root spelling and a `..` path in one command (a
	// variable set to a path under the root, then `/../packs`).
	// R5-1: the prefilter reads the folded text (a quote between the dots).
	if rd := rawDots(raw); dotdot || (strings.Contains(rd, "..") && dotdotRe.MatchString(rd) && re.MatchString(n)) {
		return true
	}
	bare := false
	mentioned := false
	for _, l := range re.FindAllStringIndex(n, -1) {
		mentioned = true
		rest := n[l[1]:]
		if strings.HasPrefix(rest, "/") {
			seg := firstSeg(rest[1:])
			if seg == "" {
				bare = true
				continue
			}
			if si.isProt(seg) || strings.ContainsAny(seg, "*?{") {
				return true
			}
			continue
		}
		// An argv or join list: joinpath(.tentaqles, packs, ...).
		if strings.HasPrefix(rest, ",") && si.isProt(firstSeg(strings.TrimLeft(rest, ", "))) {
			return true
		}
		bare = true
	}
	if mentioned && (strings.Contains(n, "ln") || strings.Contains(n, "mklink") || strings.Contains(n, "new-item")) && linkVerb.MatchString(n) {
		return true
	}
	if bare {
		for _, tok := range strings.FieldsFunc(n, isTokenSep) {
			if !strings.Contains(tok, "/") {
				continue
			}
			for _, s := range strings.Split(tok, "/") {
				if si.isProt(s) {
					return true
				}
			}
		}
	}
	// Cumulative cd: rel is the virtual cwd under the root, known while the
	// cds stay under it.
	known, rel := false, ""
	for _, seg := range strings.FieldsFunc(n, isSegSep) {
		if m := cdRe.FindStringSubmatch(seg); m != nil {
			t := strings.TrimSuffix(m[2], "/")
			if loc := re.FindStringIndex(t); loc != nil && loc[0] == 0 {
				known, rel = true, strings.Trim(path.Clean("/"+t[loc[1]:]), "/")
			} else if known && !path.IsAbs(t) && !strings.HasPrefix(t, "~") && !strings.HasPrefix(t, "$") {
				c := path.Clean(rel + "/" + t)
				if c == ".." || strings.HasPrefix(c, "../") {
					known = false
				} else {
					rel = strings.Trim(c, "./")
					if c == "." {
						rel = ""
					}
				}
			} else {
				known = false
			}
			if known && si.isProt(firstSeg(rel)) {
				return true
			}
			continue
		}
		if !known {
			continue
		}
		for _, w := range strings.FieldsFunc(seg, isWordSep) {
			w = strings.TrimPrefix(w, "./")
			if w == "" || strings.HasPrefix(w, "-") || strings.Contains(w, "://") || path.IsAbs(w) || strings.HasPrefix(w, "~") || strings.HasPrefix(w, "$") {
				continue
			}
			full := strings.TrimPrefix(path.Clean(rel+"/"+w), "/")
			if si.isProt(firstSeg(full)) {
				return true
			}
		}
	}
	return false
}

var (
	humanAnswerRe = regexp.MustCompile(`\bhub[\s,]+(-{1,2}[a-z0-9-]+(=\S+)?[\s,]+(\S+[\s,]+)?)*answer\b|\b` + policy.ApplyWord)
	httpClientRe  = regexp.MustCompile(`\b(curl|wget|http|https|invoke-webrequest|iwr|invoke-restmethod|irm|python3?|py|node|deno|bun|ruby|perl|php|pwsh|powershell|urlopen|requests|fetch)\b`)
	pctRe         = regexp.MustCompile(`%([0-9a-f]{2})`)
)

// decodePct decodes %xx escapes (a URL path spelled to dodge the rule).
func decodePct(s string) string {
	return pctRe.ReplaceAllStringFunc(s, func(m string) string {
		var b byte
		for _, c := range m[1:] {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= byte(c - '0')
			default:
				b |= byte(c-'a') + 10
			}
		}
		return string(b)
	})
}

// foldContinuations joins continued lines back into one command line: bash
// backslash-newline, PowerShell backtick-newline and cmd caret-newline
// (R6-2), so a match that stops at a newline cannot be split by one.
func foldContinuations(s string) string {
	return strings.NewReplacer("\\\r\n", " ", "\\\n", " ", "`\r\n", " ", "`\n", " ", "^\r\n", " ", "^\n", " ").Replace(s)
}

// humanMsgArg is a message or note flag value; humanProseSplit and
// echoSeg find echo segments (R6-5).
var humanMsgArg = regexp.MustCompile(`(?i)\s(-m|-am|--message|--note|--reason|--comment)(=|\s+)("([^"\\]|\\.)*"|'[^']*'|\S+)`)

// executes reports text a shell runs even inside a message or an echo:
// command substitution, a backtick, process substitution.
func executes(s string) bool {
	return strings.Contains(s, "$(") || strings.Contains(s, "`") || strings.Contains(s, "<(") || strings.Contains(s, ">(")
}

// humanProseFree blanks prose that only mentions a command: a commit message
// or a --note/--reason/--comment value, and an echo segment that redirects
// nowhere, unless the text runs something ($(...), a backtick).
func humanProseFree(raw string) string {
	raw = humanMsgArg.ReplaceAllStringFunc(raw, func(m string) string {
		if executes(m) {
			return m
		}
		return " "
	})
	var out []string
	for _, seg := range proseSplit.Split(raw, -1) {
		if echoSeg.MatchString(seg) && !strings.ContainsAny(seg, ">|") && !executes(seg) {
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, " ; ")
}

// humanAnswerCommand is the hook's tq/human-answer check of a shell command:
// continuations folded, prose blanked, then humanAnswerShell.
func humanAnswerCommand(cmd string) bool {
	return humanAnswerShell(normShell(humanProseFree(foldContinuations(cmd))))
}

// humanAnswerShell is the folded half of tq/human-answer: a tq hub answer
// or a pack apply subcommand (promote apply, cloud apply) in any spelling (global flags before the
// verb, t”q, a variable or call operator, a subprocess argv list), and an
// HTTP client reaching the decision endpoints with a decoded, quote- and
// concatenation-folded URL.
func humanAnswerShell(n string) bool {
	f := strings.ReplaceAll(n, "+", "")
	if (strings.Contains(f, "answer") || strings.Contains(f, "apply")) && humanAnswerRe.MatchString(f) {
		return true
	}
	d := decodePct(f)
	return (strings.Contains(d, "/decisions/") || strings.Contains(d, "/answer")) && httpClientRe.MatchString(d)
}

// canonicalPath resolves a written path: the nearest existing parent through
// its open handle (packs.FinalPath: junctions, symlinks and 8.3 short names),
// plus the rest, in the normShell form.
func canonicalPath(p, cwd string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	rest := []string{}
	cur := p
	for i := 0; i < 64; i++ {
		if real, err := packs.FinalPath(cur); err == nil {
			return normPathForm(filepath.Join(append([]string{real}, rest...)...))
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
	return normPathForm(p)
}

// underProtected reports whether a canonical path is inside a protected part
// of the state directory (any absolute spelling of the root).
func underProtected(canon string) bool {
	if canon == "" {
		return false
	}
	var roots []string
	si := stateInfo()
	for _, f := range si.forms {
		if strings.Contains(f, "/") && !strings.HasPrefix(f, "$") && !strings.HasPrefix(f, "%") && !strings.HasPrefix(f, "~") {
			roots = append(roots, f)
		}
	}
	for _, r := range roots {
		if strings.HasPrefix(canon, r+"/") && si.isProt(firstSeg(canon[len(r)+1:])) {
			return true
		}
	}
	for _, marker := range []string{"/.tentaqles/"} {
		if i := strings.Index(canon, marker); i >= 0 && si.isProt(firstSeg(canon[i+len(marker):])) {
			return true
		}
	}
	if m := shortRootRe.FindStringIndex(canon); m != nil && si.isProt(firstSeg(canon[m[1]:])) {
		return true
	}
	return false
}

var readOnlyVerb = regexp.MustCompile(`^\s*(cat|type|more|less|head|tail|bat|nl|grep|rg|findstr|gc|get-content|select-string|sls|git\s+(diff|log|show|status|blame))\b`)

// stateDirDecision is the hook-time check of the three never-disable ids.
// readArgs are the StateDir files a pack guard verified for this call
// (packs.GuardCtx.ReadArgs); see withoutReadArgs.
func stateDirDecision(call policy.ToolCall, cwd string, readArgs ...packs.ReadArg) policy.Decision {
	var d policy.Decision
	deny := func(id, reason string) { d = policy.Merge(d, ruleDecision(policy.Deny, id, reason)) }
	ask := func(id, reason string) { d = policy.Merge(d, ruleDecision(policy.Ask, id, reason)) }
	if call.IsShell() {
		cmd := call.Command
		if call.Tool == "Bash" {
			cmd = guard.BlankDataHeredocs(cmd)
		}
		n := normShell(cmd)
		if humanAnswerCommand(cmd) {
			deny("tq/human-answer", "answering an approval and applying an approved action are done by a human in their own terminal, never by an agent")
		}
		if strings.Contains(strings.ReplaceAll(n, "+", ""), "tentaqles-packs") {
			deny("tq/secret-store-shell", "the pack ledger key in the OS keychain signs human answers; it is never read from the shell")
		}
		mention := cmd
		if len(readArgs) > 0 {
			if m, ok := withoutReadArgs(cmd, readArgs); ok {
				mention = m
			}
		}
		if protectedMention(mention) && !tqExempt(cmd, mention) {
			deny("tq/hub-store-write", "this command reaches a protected part of tq's state directory ("+paths.Home()+": hub, packs, build-gate, trust, guard, guard.yaml, config.yaml or audit.jsonl), which holds the hub store, the pack ledgers, approvals, plans and keys; only tq itself touches it")
		}
		if (strings.Contains(n, "tq-packs.y") || strings.Contains(n, "tq-pac~")) && !(readOnlyVerb.MatchString(n) && !strings.ContainsAny(n, ">;&|")) {
			ask("tq/pack-config-change", "this command may change the pack config (tq-packs.yaml); confirm it")
		}
		return d
	}
	if call.Path == "" {
		return d
	}
	canon := canonicalPath(call.Path, cwd)
	switch call.Tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		if underProtected(canon) {
			deny("tq/hub-store-write", "this file is in a protected part of tq's state directory ("+paths.Home()+": hub, packs, build-gate, trust, guard, guard.yaml, config.yaml or audit.jsonl); only tq writes there")
		}
		base := filepath.Base(canon)
		if base == "tq-packs.yaml" || base == "tq-packs.yml" {
			ask("tq/pack-config-change", "this changes the pack config (tq-packs.yaml); confirm it")
		}
	case "Read", "Grep", "NotebookRead", "Glob":
		if underProtected(canon) {
			rel := canon[strings.LastIndex(canon, "/hub/")+1:]
			if strings.Contains(canon, "/hub/") && (strings.HasPrefix(filepath.Base(rel), "serve-") || strings.Contains(filepath.Base(rel), "token")) {
				deny("tq/human-answer", "the hub's serve info holds its local key; it is never read into the conversation")
			}
		}
	}
	return d
}
