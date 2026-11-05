package policy

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/providers"
)

// The identity category's v2 rules (PRD v2 §6.3, §7.1; ADR-0006), on behind the manifest
// flag guard: v2: everything an identity's provider scope leaves out is denied in every
// tool. The match lists are the catalog's data (providers.Match); this file only reads
// them. Like the rest of the hook it is a text filter: it matches literal spellings in the
// tool call, never what a command builds at run time.

// The v2 identity rule ids. A deny reads BLOCKED [identity/<id>].
const (
	// OutOfScopeCLIID: an out-of-scope provider's CLI run by name or path, behind a
	// wrapper, inside a nested shell, as `python -m <module>`, through a package runner.
	OutOfScopeCLIID = "out-of-scope-cli"
	// OutOfScopeEnvID: a reference to an out-of-scope provider's environment variable.
	OutOfScopeEnvID = "out-of-scope-env"
	// OutOfScopePathID: an out-of-scope provider's credential or config path.
	OutOfScopePathID = "out-of-scope-path"
	// OutOfScopeHostID: an out-of-scope provider's API host in a network call.
	OutOfScopeHostID = "out-of-scope-host"
	// OutOfScopeMCPID: an MCP tool whose server is an out-of-scope provider's.
	OutOfScopeMCPID = "out-of-scope-mcp"
	// OtherIdentityID: a path under another identity's workspace root or its identity
	// folder under the tq home.
	OtherIdentityID = "other-identity"
)

// ScopeInputs are the facts a Scope is compiled from; the hook gathers them.
type ScopeInputs struct {
	Identity  string               // the workspace name
	Scope     []string             // manifest.Scope(): the catalog ids the identity may touch
	Providers []providers.Provider // the whole catalog (embedded plus the user files)
	Set       *protect.Set         // the hook run's protected path set: Normalize, Expand, Canon
	UserHome  string               // what ~ is
	Env       func(string) string  // APPDATA, LOCALAPPDATA
	// OtherRoots and OtherDirs map another identity's workspace root, and its folder under
	// the tq home (~/.tentaqles/identities/<name>), to its name.
	OtherRoots map[string]string
	OtherDirs  map[string]string
}

type scopedProvider struct {
	id, name string
	match    providers.Match
	paths    []string // canonical
}

type otherPath struct{ path, name string }

type envName struct {
	name   string // without the trailing *
	prefix bool
	p      *scopedProvider
}

type mention struct {
	needle string // slash form, folded as the set folds
	path   string // canonical
	p      *scopedProvider
	other  string // the other identity's name, for an other-identity root
}

// Scope is an identity's compiled scope: the in-scope and out-of-scope providers' match
// lists, ready to judge tool calls. A nil Scope allows everything (the v1 rules).
type Scope struct {
	identity string
	scope    []string
	in, out  []*scopedProvider
	set      *protect.Set
	ci       bool
	env      func(string) string

	outExe, outModule map[string]*scopedProvider
	inMCP             map[string]bool
	outMCP            map[string]*scopedProvider
	outEnv            []envName
	inEnv             []envName
	others            []otherPath
	mentions          []mention
	needles           []string
	envRes            map[string]*regexp.Regexp
}

// NewScope compiles in. It never fails: a path whose root is unknown (no %APPDATA% on this
// system) is skipped.
func NewScope(in ScopeInputs) *Scope {
	s := &Scope{identity: in.Identity, scope: append([]string{}, in.Scope...), set: in.Set, env: in.Env,
		outExe: map[string]*scopedProvider{}, outModule: map[string]*scopedProvider{},
		inMCP: map[string]bool{}, outMCP: map[string]*scopedProvider{}, envRes: map[string]*regexp.Regexp{}}
	if s.env == nil {
		s.env = func(string) string { return "" }
	}
	s.ci = in.Set != nil && in.Set.CaseInsensitive()
	sort.Strings(s.scope)
	inScope := map[string]bool{}
	for _, id := range in.Scope {
		inScope[id] = true
	}
	roots := map[string]string{} // ~, %APPDATA%, %LOCALAPPDATA% -> canonical
	rootOf := func(spec string) (string, string) {
		for _, r := range providers.PathRoots {
			if !strings.HasPrefix(spec, r) {
				continue
			}
			c, ok := roots[r]
			if !ok {
				raw := ""
				switch r {
				case "~/":
					raw = in.UserHome
				case "%APPDATA%/":
					raw = s.env("APPDATA")
				case "%LOCALAPPDATA%/":
					raw = s.env("LOCALAPPDATA")
				}
				if strings.TrimSpace(raw) != "" && in.Set != nil {
					c = in.Set.Canon(raw)
				}
				roots[r] = c
			}
			return c, spec[len(r):]
		}
		return "", ""
	}
	for i := range in.Providers {
		pv := in.Providers[i]
		if pv.Match.Empty() {
			continue
		}
		sp := &scopedProvider{id: pv.ID, name: pv.Name, match: pv.Match}
		for _, spec := range pv.Match.Paths {
			root, rest := rootOf(spec)
			if root == "" {
				continue
			}
			p := filepath.Join(root, filepath.FromSlash(rest))
			if s.ci {
				p = strings.ToLower(p)
			}
			sp.paths = append(sp.paths, p)
		}
		if inScope[pv.ID] {
			s.in = append(s.in, sp)
		} else {
			s.out = append(s.out, sp)
		}
	}
	inExe, inModule := map[string]bool{}, map[string]bool{}
	for _, sp := range s.in {
		for _, e := range sp.match.Executables {
			inExe[exeName(e)] = true
		}
		for _, m := range sp.match.Modules {
			inModule[strings.ToLower(m)] = true
		}
		for _, t := range sp.match.MCP {
			s.inMCP[t] = true
		}
		for _, e := range sp.match.Env {
			s.inEnv = append(s.inEnv, newEnvName(e, sp))
		}
	}
	for _, sp := range s.out {
		for _, e := range sp.match.Executables {
			if n := exeName(e); !inExe[n] && s.outExe[n] == nil {
				s.outExe[n] = sp
			}
		}
		for _, m := range sp.match.Modules {
			if l := strings.ToLower(m); !inModule[l] && s.outModule[l] == nil {
				s.outModule[l] = sp
			}
		}
		for _, t := range sp.match.MCP {
			if s.outMCP[t] == nil {
				s.outMCP[t] = sp
			}
		}
		for _, e := range sp.match.Env {
			en := newEnvName(e, sp)
			if !en.prefix && s.inClaimsEnv(en.name) {
				continue
			}
			s.outEnv = append(s.outEnv, en)
		}
		for _, p := range sp.paths {
			s.addMention(p, sp, "")
			s.needles = append(s.needles, strings.ToLower(filepath.Base(p)))
		}
	}
	// Only the parent folders (the bases, the identities folder) are canonicalised, once
	// each: the hook stays cheap with many identities.
	parents := map[string]string{}
	addOthers := func(m map[string]string) {
		for raw, name := range m {
			if strings.TrimSpace(raw) == "" || in.Set == nil {
				continue
			}
			dir := filepath.Dir(filepath.Clean(raw))
			pc, ok := parents[dir]
			if !ok {
				pc = in.Set.Canon(dir)
				parents[dir] = pc
			}
			leaf := filepath.Base(raw)
			if s.ci {
				leaf = strings.ToLower(leaf)
			}
			c := filepath.Join(pc, leaf)
			s.others = append(s.others, otherPath{c, name})
			s.addMention(c, nil, name)
			s.needles = append(s.needles, strings.ToLower(name))
		}
	}
	addOthers(in.OtherRoots)
	addOthers(in.OtherDirs)
	sort.Slice(s.others, func(i, j int) bool { return s.others[i].path < s.others[j].path })
	return s
}

func newEnvName(e string, sp *scopedProvider) envName {
	n, prefix := strings.CutSuffix(strings.ToUpper(e), "*")
	return envName{name: n, prefix: prefix, p: sp}
}

func (s *Scope) addMention(p string, sp *scopedProvider, other string) {
	add := func(n string) {
		if s.ci {
			n = strings.ToLower(n)
		}
		s.mentions = append(s.mentions, mention{needle: n, path: p, p: sp, other: other})
	}
	slash := filepath.ToSlash(p)
	add(slash)
	if runtime.GOOS == "windows" && len(slash) > 2 && slash[1] == ':' {
		add("/" + strings.ToLower(slash[:1]) + slash[2:]) // the MSYS spelling, /c/...
	}
}

// exeName is the executable a command word names: its last path element, quotes removed,
// lower-cased, without a Windows suffix (.exe .cmd .bat .ps1 .com).
func exeName(w string) string {
	n := guard.BaseName(w)
	for _, suf := range []string{".cmd", ".bat", ".ps1", ".com"} {
		n = strings.TrimSuffix(n, suf)
	}
	return n
}

// scopeHit is what an identity rule found.
type scopeHit struct {
	id, subject, what string
	other             string // other-identity: the identity's name
}

// Evaluate judges call against the scope; cwd is the directory it runs in. It returns the
// deny, or the zero Decision.
func (s *Scope) Evaluate(cwd string, call ToolCall) Decision {
	if s == nil {
		return Decision{}
	}
	h, ok := s.judge(cwd, call)
	if !ok {
		return Decision{}
	}
	var reason string
	if h.id == OtherIdentityID {
		reason = h.subject + " belongs to identity " + h.other + ", not to " + s.identity +
			": a session reads and writes only its own identity's files. tq denies it in every tool; do not retry another way: tell the developer, who works on it from that identity"
	} else {
		sc := strings.Join(s.scope, ", ")
		if sc == "" {
			sc = "none"
		}
		reason = h.what + ", outside the scope of identity " + s.identity + " (scope: " + sc + "). " +
			"tq denies it in every tool; do not retry another way: tell the developer, who does it from the identity that owns it or adds the provider to this identity's scope"
	}
	return Decision{Action: Deny, Matched: []*Rule{{ID: h.id, Category: CategoryIdentity, Action: Deny, Reason: reason, Source: "builtin", Subject: h.subject}}}
}

func (s *Scope) judge(cwd string, call ToolCall) (scopeHit, bool) {
	switch {
	case call.IsShell():
		text := call.Command
		if call.Tool == "Bash" {
			text = guard.BlankDataHeredocs(text)
		}
		return s.shell(cwd, call.Command, text, 0)
	case call.IsMCP():
		if h, ok := s.mcpServer(call.MCPServer()); ok {
			return h, true
		}
		return s.mcpInput(cwd, call.Content)
	case call.Tool == "WebFetch":
		if host := urlHost(call.URL); host != "" {
			return s.hostHit(host)
		}
		return scopeHit{}, false
	}
	if _, ok := fileToolAccess(call.Tool); !ok {
		return scopeHit{}, false
	}
	p := call.Path
	if strings.TrimSpace(p) == "" {
		if call.Tool != "Grep" && call.Tool != "Glob" {
			return scopeHit{}, false
		}
		p = cwd
	}
	return s.pathHit(s.set.Normalize(p, cwd))
}

func (s *Scope) desc(sp *scopedProvider) string { return sp.name + " (" + sp.id + ")" }

// --- paths ---

func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// pathHit judges a canonical path.
func (s *Scope) pathHit(p string) (scopeHit, bool) {
	if p == "" {
		return scopeHit{}, false
	}
	for _, o := range s.others {
		if within(o.path, p) {
			return scopeHit{id: OtherIdentityID, subject: p, other: o.name}, true
		}
	}
	for _, sp := range s.out {
		for _, c := range sp.paths {
			if within(c, p) && !s.inClaimsPath(p) {
				return scopeHit{id: OutOfScopePathID, subject: p, what: p + " is a credential path of " + s.desc(sp)}, true
			}
		}
	}
	return scopeHit{}, false
}

func (s *Scope) inClaimsPath(p string) bool {
	for _, sp := range s.in {
		for _, c := range sp.paths {
			if within(c, p) {
				return true
			}
		}
	}
	return false
}

// mayName is the cheap filter before a word is normalized: only a word that spells a
// credential folder's name or another identity's name can resolve under one.
func (s *Scope) mayName(w string) bool {
	if w == "" {
		return false
	}
	l := strings.ToLower(w)
	for _, n := range s.needles {
		if strings.Contains(l, n) {
			return true
		}
	}
	return false
}

var appDataRe = regexp.MustCompile(`(?i)%(LOCALAPPDATA|APPDATA)%|\$env:(LOCALAPPDATA|APPDATA)\b|\$\{(LOCALAPPDATA|APPDATA)\}|\$(LOCALAPPDATA|APPDATA)\b`)

// mentionHit scans text (a shell line, inline code) for a credential folder or another
// identity's root spelled in it, after expanding ~, $HOME, %USERPROFILE%, $env:USERPROFILE,
// %APPDATA% and the like.
func (s *Scope) mentionHit(text string) (scopeHit, bool) {
	if s.set == nil || len(s.mentions) == 0 {
		return scopeHit{}, false
	}
	t := appDataRe.ReplaceAllStringFunc(text, func(m string) string {
		g := appDataRe.FindStringSubmatch(m)
		for _, n := range g[1:] {
			if n != "" {
				if v := s.env(strings.ToUpper(n)); v != "" {
					return v
				}
			}
		}
		return m
	})
	t = filepath.ToSlash(s.set.Expand(t))
	if s.ci {
		t = strings.ToLower(t)
	}
	for _, m := range s.mentions {
		for i := 0; ; {
			j := strings.Index(t[i:], m.needle)
			if j < 0 {
				break
			}
			at, end := i+j, i+j+len(m.needle)
			i = end
			if (at > 0 && isPathByte(t[at-1])) || (end < len(t) && isPathByte(t[end])) {
				continue // part of a longer name
			}
			if m.other != "" {
				return scopeHit{id: OtherIdentityID, subject: m.path, other: m.other}, true
			}
			if s.inClaimsPath(m.path) {
				continue
			}
			return scopeHit{id: OutOfScopePathID, subject: m.path, what: m.path + " is a credential path of " + s.desc(m.p)}, true
		}
	}
	return scopeHit{}, false
}

// isPathByte reports a byte that continues a path element's name.
func isPathByte(b byte) bool {
	return b == '.' || b == '_' || b == '-' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// --- MCP ---

func mcpTokens(server string) []string {
	return strings.FieldsFunc(strings.ToLower(server), func(r rune) bool { return r == '_' || r == '-' || r == '.' })
}

// mcpServer judges an MCP server name: in scope when an in-scope provider claims one of its
// tokens, denied when an out-of-scope provider claims one.
func (s *Scope) mcpServer(server string) (scopeHit, bool) {
	toks := mcpTokens(server)
	for _, t := range toks {
		if s.inMCP[t] {
			return scopeHit{}, false
		}
	}
	for _, t := range toks {
		if sp := s.outMCP[t]; sp != nil {
			return scopeHit{id: OutOfScopeMCPID, subject: "mcp__" + server, what: "MCP server " + server + " is " + s.desc(sp)}, true
		}
	}
	return scopeHit{}, false
}

// mcpInput reads every string of an MCP tool input that looks like a path.
func (s *Scope) mcpInput(cwd, raw string) (scopeHit, bool) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return scopeHit{}, false
	}
	var found scopeHit
	ok := false
	walkStrings(v, func(str string) bool {
		if len(str) > 1024 || strings.ContainsAny(str, "\n\r") || !strings.ContainsAny(str, `/\~$%`) {
			return true
		}
		if h, hit := s.pathHit(s.set.Normalize(str, cwd)); hit {
			found, ok = h, true
			return false
		}
		return true
	})
	return found, ok
}

// --- hosts ---

var urlRe = regexp.MustCompile(`(?i)\b(?:https?|wss?|ftps?)://(?:[^\s/@'"` + "`" + `]*@)?([a-z0-9.-]+)`)

func urlHost(u string) string {
	if m := urlRe.FindStringSubmatch(u); m != nil {
		return strings.Trim(strings.ToLower(m[1]), ".")
	}
	return ""
}

// netVerbs take a bare host (no scheme) as an argument.
var netVerbs = wordSet("curl", "wget", "invoke-webrequest", "iwr", "invoke-restmethod", "irm", "http", "https", "xh", "httpie",
	"aria2c", "start-bitstransfer", "ssh", "scp", "sftp", "nc", "ncat", "telnet")

// bareHost reads a host out of an argument without a scheme: [user@]host[:port][/path].
func bareHost(a string) string {
	a = strings.Trim(a, `"'`)
	if strings.HasPrefix(a, "-") || strings.Contains(a, "://") {
		return ""
	}
	if i := strings.LastIndex(a, "@"); i >= 0 {
		a = a[i+1:]
	}
	if i := strings.IndexAny(a, "/:?"); i >= 0 {
		a = a[:i]
	}
	a = strings.Trim(strings.ToLower(a), ".")
	if !strings.Contains(a, ".") {
		return ""
	}
	return a
}

func hostIn(h string, list []string) bool {
	for _, pat := range list {
		if h == pat || strings.HasSuffix(h, "."+pat) {
			return true
		}
	}
	return false
}

func (s *Scope) hostHit(h string) (scopeHit, bool) {
	for _, sp := range s.in {
		if hostIn(h, sp.match.Hosts) {
			return scopeHit{}, false
		}
	}
	for _, sp := range s.out {
		if hostIn(h, sp.match.Hosts) {
			return scopeHit{id: OutOfScopeHostID, subject: h, what: h + " is an API host of " + s.desc(sp)}, true
		}
	}
	return scopeHit{}, false
}

// --- environment variables ---

func (s *Scope) inClaimsEnv(name string) bool {
	for _, e := range s.inEnv {
		if e.name == name || (e.prefix && strings.HasPrefix(name, e.name)) {
			return true
		}
	}
	return false
}

// envRefPre is what makes a variable name a reference: an expansion ($NAME, ${NAME,
// $env:NAME, %NAME%, env:NAME, process.env.NAME), a lookup in code (os.environ['NAME'],
// getenv("NAME"), [Environment]::GetEnvironmentVariable('NAME')), or a shell builtin that
// sets, clears or prints it.
const envRefPre = `(?:\$\{?|\$env:|%|\benv:\\?|process\.env\.|(?:environ\s*\[|environ\.get\s*\(|getenv\s*\(|getenvironmentvariable\s*\(|env::var\s*\(|process\.env\[)\s*['"]?|(?:^|[\s;&|(` + "`" + `"'])(?:export|unset|set|setx|printenv|readonly)\s+)`

// envAssignPre is a NAME= assignment at the start of a word.
const envAssignPre = `(?:^|[\s;&|(` + "`" + `"'])`

func (s *Scope) envRe(e envName) *regexp.Regexp {
	key := e.name
	if e.prefix {
		key += "*"
	}
	if re, ok := s.envRes[key]; ok {
		return re
	}
	pat := regexp.QuoteMeta(e.name)
	if e.prefix {
		pat += `\w+`
	}
	re := regexp.MustCompile(`(?i)` + envRefPre + `(` + pat + `)\b|` + envAssignPre + `(` + pat + `)=`)
	s.envRes[key] = re
	return re
}

func (s *Scope) envHit(text string) (scopeHit, bool) {
	upper := strings.ToUpper(text)
	for _, e := range s.outEnv {
		if !strings.Contains(upper, e.name) {
			continue
		}
		for _, m := range s.envRe(e).FindAllStringSubmatch(text, -1) {
			name := strings.ToUpper(m[1] + m[2])
			if e.prefix && s.inClaimsEnv(name) {
				continue
			}
			return scopeHit{id: OutOfScopeEnvID, subject: name, what: name + " is a variable of " + s.desc(e.p)}, true
		}
	}
	return scopeHit{}, false
}

// --- the shell ---

var pythonRe = regexp.MustCompile(`^(python(\d+(\.\d+)?)?w?|py|pyw)$`)

// runners run a package by name: the word after them (after a sub-command for some).
var runners = map[string][]string{
	"npx": nil, "bunx": nil, "pnpx": nil, "uvx": nil,
	"pnpm": {"dlx", "exec"}, "yarn": {"dlx", "exec"}, "npm": {"exec", "x"}, "bun": {"x"},
	"pipx": {"run"}, "uv": {"run", "tool"},
}

func (s *Scope) exeHit(word string) (scopeHit, bool) {
	n := exeName(word)
	if sp := s.outExe[n]; sp != nil {
		return scopeHit{id: OutOfScopeCLIID, subject: n, what: n + " is the CLI of " + s.desc(sp)}, true
	}
	return scopeHit{}, false
}

func (s *Scope) moduleHit(mod string) (scopeHit, bool) {
	l := strings.ToLower(strings.Trim(mod, `"'`))
	for m, sp := range s.outModule {
		if l == m || strings.HasPrefix(l, m+".") {
			return scopeHit{id: OutOfScopeCLIID, subject: "python -m " + l, what: "python -m " + l + " runs the CLI of " + s.desc(sp)}, true
		}
	}
	return scopeHit{}, false
}

// packageName reads a runner's package argument: @scope/name@version -> name.
func packageName(a string) string {
	a = strings.Trim(a, `"'`)
	if strings.HasPrefix(a, "@") {
		if i := strings.Index(a, "/"); i >= 0 {
			a = a[i+1:]
		}
	}
	if i := strings.IndexAny(a, "@=="); i > 0 {
		a = a[:i]
	}
	return a
}

// exeSuffixHit finds an out-of-scope executable spelled with a Windows suffix anywhere in the
// line (`C:\Program Files\Amazon\AWSCLIV2\aws.exe` unquoted, which the tokenizer splits at
// the space).
func (s *Scope) exeSuffixHit(text string) (scopeHit, bool) {
	l := strings.ToLower(filepath.ToSlash(text))
	// Not .com: in a line it is far more often a domain (render.com, vercel.com).
	for _, suf := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		for i := 0; ; {
			j := strings.Index(l[i:], suf)
			if j < 0 {
				break
			}
			end := i + j
			i = end + len(suf)
			if i < len(l) && isPathByte(l[i]) {
				continue
			}
			start := end
			for start > 0 && !strings.ContainsRune(" \t\r\n\"'`/&|;(=,", rune(l[start-1])) {
				start--
			}
			if h, ok := s.exeHit(l[start:end]); ok {
				return h, true
			}
		}
	}
	return scopeHit{}, false
}

// shell judges a shell command line: raw is what the tokenizer reads, text is the line the
// text scans read (a Bash line with its data here-doc bodies blanked).
func (s *Scope) shell(cwd, raw, text string, depth int) (scopeHit, bool) {
	for _, c := range guard.Commands(raw) {
		base := commandBase(s.set, cwd, c)
		if c.Base != "" && base != "" {
			if h, ok := s.pathHit(base); ok {
				return h, true
			}
		}
		writes, reads := redirects(c.Words)
		words := append(append(append([]string{}, c.Words...), writes...), reads...)
		for _, w := range words {
			for _, p := range argPaths(w) {
				if s.mayName(p) {
					if h, ok := s.pathHit(s.set.Normalize(p, base)); ok {
						return h, true
					}
				}
			}
		}
		verb, args, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		if h, ok := s.exeHit(verb); ok {
			return h, true
		}
		name := exeName(verb)
		switch {
		case pythonRe.MatchString(name):
			for i := 0; i < len(args); i++ {
				a := args[i]
				mod := ""
				if a == "-m" && i+1 < len(args) {
					mod = args[i+1]
				} else if strings.HasPrefix(a, "-m") && len(a) > 2 {
					mod = a[2:]
				}
				if mod != "" {
					if h, ok := s.moduleHit(mod); ok {
						return h, true
					}
					break
				}
				if a == "-c" || !strings.HasPrefix(a, "-") {
					break // inline code or a script: the text scans read it
				}
			}
		case runners[name] != nil || name == "npx" || name == "bunx" || name == "pnpx" || name == "uvx":
			subs := runners[name]
			i := 0
			if subs != nil {
				for i < len(args) && strings.HasPrefix(args[i], "-") {
					i++
				}
				if i >= len(args) || !hasString(subs, strings.ToLower(args[i])) {
					break
				}
				i++
				if name == "uv" && i < len(args) && strings.EqualFold(args[i-1], "tool") {
					if !strings.EqualFold(args[i], "run") {
						break
					}
					i++
				}
			}
			for i < len(args) && strings.HasPrefix(args[i], "-") {
				i++
			}
			if i < len(args) {
				pkg := packageName(args[i])
				if h, ok := s.exeHit(pkg); ok {
					return h, true
				}
				if h, ok := s.moduleHit(pkg); ok {
					return h, true
				}
			}
		case name == "start-process" || name == "saps" || name == "start":
			for i := 0; i < len(args); i++ {
				a := args[i]
				target := ""
				if strings.EqualFold(a, "-FilePath") && i+1 < len(args) {
					target = args[i+1]
				} else if !strings.HasPrefix(a, "-") && strings.Trim(a, `"'`) != "" {
					target = a
				}
				if target != "" {
					if h, ok := s.exeHit(target); ok {
						return h, true
					}
					break
				}
			}
		case name == "powershell" || name == "pwsh":
			if dec, present, decoded := encodedPowerShell(args); present && decoded && depth < 3 {
				if h, ok := s.shell(base, dec, dec, depth+1); ok {
					return h, true
				}
			}
		case name == "eval" && depth < 3:
			for _, a := range args {
				if h, ok := s.shell(base, a, a, depth+1); ok {
					return h, true
				}
			}
		case netVerbs[name]:
			for _, a := range args {
				if host := bareHost(a); host != "" {
					if h, ok := s.hostHit(host); ok {
						return h, true
					}
				}
			}
		}
	}
	if h, ok := s.exeSuffixHit(text); ok {
		return h, true
	}
	for _, m := range urlRe.FindAllStringSubmatch(text, -1) {
		if h, ok := s.hostHit(strings.Trim(strings.ToLower(m[1]), ".")); ok {
			return h, true
		}
	}
	if h, ok := s.envHit(text); ok {
		return h, true
	}
	return s.mentionHit(text)
}

func hasString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
