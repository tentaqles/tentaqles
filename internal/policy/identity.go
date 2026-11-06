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

// The identity category's v2 rules (PRD v2 §6.3, §7.1; ADR-0007), on behind the manifest
// flag guard: v2: everything an identity's provider scope leaves out is denied in every
// tool. The claims are the catalog's data (providers.Match); this file reads them.
//
// Precision (ADR-0007 round 2): a provider's CLAIMS (what being in scope allows) come from
// the EMBEDDED catalog only; a provider's DENIES (what being out of scope blocks) come from
// the merged catalog (embedded + user files). So a user override can only widen denial, never
// make something allowed. A shared CLI (az) is owned wholesale by one provider and claimed
// per-subcommand by others (azure-devops owns `az devops|repos|...`); the most precise
// in-scope claim wins. Like the rest of the hook it is a text filter: literal spellings only.

// The v2 identity rule ids. A deny reads BLOCKED [identity/<id>].
const (
	OutOfScopeCLIID  = "out-of-scope-cli"
	OutOfScopeEnvID  = "out-of-scope-env"
	OutOfScopePathID = "out-of-scope-path"
	OutOfScopeHostID = "out-of-scope-host"
	OutOfScopeMCPID  = "out-of-scope-mcp"
	OtherIdentityID  = "other-identity"
	// GuardErrorID is the fail-closed deny: an unreadable manifest or provider file.
	GuardErrorID = "tq/guard-error"
)

// ScopeInputs are the facts a Scope is compiled from; the hook gathers them.
type ScopeInputs struct {
	Identity  string               // the workspace name
	Scope     []string             // manifest.Scope(): the catalog ids the identity may touch
	Providers []providers.Provider // the whole catalog, merged (embedded + user files)
	// Embedded is the embedded-only match per id, used for an in-scope provider's claims so a
	// user file can never widen a claim. Missing id falls back to the merged match.
	Embedded  map[string]providers.Match
	BundleMCP []string            // the identity's own declared MCP servers (claude.bundle.mcp)
	Set       *protect.Set        // the hook run's protected path set: Normalize, Expand, Canon
	UserHome  string              // what ~ is
	Env       func(string) string // APPDATA, LOCALAPPDATA
	// OtherRoots and OtherDirs map another identity's workspace root, and its folder under
	// the tq home (~/.tentaqles/identities/<name>), to its name.
	OtherRoots map[string]string
	OtherDirs  map[string]string
	// MCPEntries loads the MCP servers of the identity's harness config, by server name
	// (ADR-0007): an mcp__<server>__ call is judged by its entry too. Called once, on the
	// first MCP call; nil when the caller has none.
	MCPEntries func() map[string][]map[string]any
	// MCPAllow is the manifest's mcp.allow: exact MCP server names kept though no in-scope
	// provider claims them and they are not general-purpose (the hybrid list, ADR-0007).
	MCPAllow []string
}

type scopedProvider struct {
	id, name string
	inScope  bool
	generic  bool     // providers.Provider.GenericTool (ADR-0007)
	packages []string // match.mcp_packages (ADR-0007)
	mcpHosts []string // match.mcp_hosts (ADR-0007)
}

// exeClaim is one provider's claim on an executable: wholesale, or a set of first subcommands.
type exeClaim struct {
	p        *scopedProvider
	wildcard bool
	subs     map[string]bool
}

type envName struct {
	name   string
	prefix bool
	p      *scopedProvider
}

type pathEntry struct {
	path string // canonical
	p    *scopedProvider
}

type mention struct {
	needle string
	path   string
	p      *scopedProvider
	other  string
}

// Scope is an identity's compiled scope. A nil Scope allows everything (the v1 rules).
type Scope struct {
	identity string
	scope    []string
	set      *protect.Set
	ci       bool
	env      func(string) string

	provs []*scopedProvider

	exeClaims map[string][]exeClaim // exe name -> claims (in and out)
	modClaims map[string][]exeClaim // module -> claims (subs unused)
	imgClaims map[string][]exeClaim // image basename and full -> claims
	psClaims  map[string][]exeClaim // PowerShell module (lower-case) -> claims
	cmdlets   []cmdletClaim

	outEnv  []envName
	inEnv   []envName
	outPath []pathEntry
	inPath  []pathEntry
	others  []pathEntry

	inHosts  [][]string
	outHosts []*hostSet
	inGit    []string
	outGit   []*hostSet

	bundleMCP map[string]bool
	inMCP     map[string]bool
	outMCP    map[string]*scopedProvider

	mentions []mention
	needles  []string
	envRes   map[string]*regexp.Regexp

	mcpLoad    func() map[string][]map[string]any
	mcpEntries map[string][]map[string]any // by the tool-name spelling of the server name; every source's entry
	mcpLoaded  bool
	mcpAllow   map[string]bool
	general    []providers.GeneralMCP
}

// cmdletClaim is one provider's cmdlets entry (exact or *-NounPrefix*).
type cmdletClaim struct {
	entry string
	p     *scopedProvider
}

type hostSet struct {
	p     *scopedProvider
	hosts []string
	m     providers.Match // Hosts and Public, for an out-of-scope provider's API hosts
}

// NewScope compiles in. It never fails.
func NewScope(in ScopeInputs) *Scope {
	s := &Scope{identity: in.Identity, scope: append([]string{}, in.Scope...), set: in.Set, env: in.Env,
		exeClaims: map[string][]exeClaim{}, modClaims: map[string][]exeClaim{},
		imgClaims: map[string][]exeClaim{}, psClaims: map[string][]exeClaim{}, bundleMCP: map[string]bool{}, inMCP: map[string]bool{},
		outMCP: map[string]*scopedProvider{}, envRes: map[string]*regexp.Regexp{}, mcpLoad: in.MCPEntries, mcpAllow: map[string]bool{}}
	for _, n := range in.MCPAllow {
		s.mcpAllow[MCPToolName(n)] = true
	}
	s.general, _ = providers.MCPGeneral()
	if s.env == nil {
		s.env = func(string) string { return "" }
	}
	s.ci = in.Set != nil && in.Set.CaseInsensitive()
	sort.Strings(s.scope)
	inScope := map[string]bool{}
	for _, id := range in.Scope {
		inScope[id] = true
	}
	for _, srv := range in.BundleMCP {
		s.bundleMCP[strings.ToLower(srv)] = true
	}

	roots := map[string]string{}
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
	canonPath := func(spec string) string {
		root, rest := rootOf(spec)
		if root == "" {
			return ""
		}
		p := filepath.Join(root, filepath.FromSlash(rest))
		if s.ci {
			p = strings.ToLower(p)
		}
		return p
	}

	for i := range in.Providers {
		pv := in.Providers[i]
		sp := &scopedProvider{id: pv.ID, name: pv.Name, inScope: inScope[pv.ID]}
		// claims from embedded (in-scope) or merged (out-of-scope)
		claim := pv.Match
		if sp.inScope {
			if em, ok := in.Embedded[pv.ID]; ok {
				claim = em
			}
		}
		if claim.Empty() {
			continue
		}
		sp.generic, sp.packages, sp.mcpHosts = pv.GenericTool(), claim.MCPPackages, claim.MCPHosts
		s.provs = append(s.provs, sp)
		for _, e := range claim.Executables {
			n := exeName(e)
			s.exeClaims[n] = append(s.exeClaims[n], exeClaim{p: sp, wildcard: true})
		}
		for exe, subs := range claim.Subcommands {
			set := map[string]bool{}
			for _, sub := range subs {
				set[strings.ToLower(sub)] = true
			}
			s.exeClaims[exeName(exe)] = append(s.exeClaims[exeName(exe)], exeClaim{p: sp, subs: set})
		}
		for _, m := range claim.Modules {
			l := strings.ToLower(m)
			s.modClaims[l] = append(s.modClaims[l], exeClaim{p: sp, wildcard: true})
		}
		for _, img := range claim.Images {
			l := strings.ToLower(img)
			s.imgClaims[l] = append(s.imgClaims[l], exeClaim{p: sp, wildcard: true})
			s.imgClaims[imageBase(l)] = append(s.imgClaims[imageBase(l)], exeClaim{p: sp, wildcard: true})
		}
		for _, m := range claim.PSModules {
			l := strings.ToLower(m)
			s.psClaims[l] = append(s.psClaims[l], exeClaim{p: sp, wildcard: true})
		}
		for _, c := range claim.Cmdlets {
			s.cmdlets = append(s.cmdlets, cmdletClaim{entry: c, p: sp})
		}
		for _, e := range claim.Env {
			en := newEnvName(e, sp)
			if sp.inScope {
				s.inEnv = append(s.inEnv, en)
			} else {
				s.outEnv = append(s.outEnv, en)
			}
		}
		for _, spec := range claim.Paths {
			c := canonPath(spec)
			if c == "" {
				continue
			}
			if sp.inScope {
				s.inPath = append(s.inPath, pathEntry{c, sp})
			} else {
				s.outPath = append(s.outPath, pathEntry{c, sp})
				s.addMention(c, sp, "")
				s.needles = append(s.needles, strings.ToLower(filepath.Base(c)))
			}
		}
		if len(claim.Hosts) > 0 {
			if sp.inScope {
				s.inHosts = append(s.inHosts, claim.Hosts)
			} else {
				s.outHosts = append(s.outHosts, &hostSet{p: sp, hosts: claim.Hosts, m: providers.Match{Hosts: claim.Hosts, Public: claim.Public}})
			}
		}
		for _, g := range claim.GitHosts {
			if sp.inScope {
				s.inGit = append(s.inGit, g)
			}
		}
		if len(claim.GitHosts) > 0 && !sp.inScope {
			s.outGit = append(s.outGit, &hostSet{p: sp, hosts: claim.GitHosts})
		}
		for _, t := range claim.MCP {
			if sp.inScope {
				s.inMCP[t] = true
			} else if s.outMCP[t] == nil {
				s.outMCP[t] = sp
			}
		}
	}

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
			s.others = append(s.others, pathEntry{c, &scopedProvider{id: name, name: name}})
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
		add("/" + strings.ToLower(slash[:1]) + slash[2:])
	}
}

// exeName is the executable a command word names: its last path element, quotes removed,
// lower-cased, without a Windows suffix. Shell backslash joins (a\ws) are collapsed.
func exeName(w string) string {
	n := guard.BaseName(strings.ReplaceAll(strings.Trim(w, `"'`), `\`, "/"))
	n = n[strings.LastIndex(n, "/")+1:]
	for _, suf := range []string{".exe", ".cmd", ".bat", ".ps1", ".com"} {
		n = strings.TrimSuffix(n, suf)
	}
	return n
}

// imageBase is a docker image's basename without tag/registry: amazon/aws-cli -> aws-cli.
func imageBase(img string) string {
	img = strings.SplitN(img, ":", 2)[0]
	img = strings.SplitN(img, "@", 2)[0]
	return img[strings.LastIndex(img, "/")+1:]
}

type scopeHit struct {
	id, subject, what string
	other             string
}

// Evaluate judges call against the scope; cwd is the directory it runs in.
func (s *Scope) Evaluate(cwd string, call ToolCall) Decision {
	if s == nil {
		return Decision{}
	}
	h, ok := s.judge(cwd, call)
	if !ok {
		return Decision{}
	}
	// The scope line itself is added by the hook (cmd.scopeNote, from Manifest.Scope()), the
	// one place a deny message prints it.
	var reason string
	if h.id == OtherIdentityID {
		reason = h.subject + " belongs to identity " + h.other + ", not to " + s.identity +
			": a session reads and writes only its own identity's files. tq denies it in every tool; do not retry another way: tell the developer, who works on it from that identity"
	} else {
		reason = h.what + ", outside the scope of identity " + s.identity + ". " +
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
		return s.shell(cwd, call.Command, text, 0, call.Tool == "PowerShell")
	case call.IsMCP():
		if h, ok := s.mcpServer(call.MCPServer()); ok {
			return h, true
		}
		if h, ok := s.mcpByEntry(call.MCPServer()); ok {
			return h, true
		}
		return s.mcpInput(cwd, call.Content)
	case call.Tool == "WebFetch":
		if host, path := urlParts(call.URL); host != "" {
			return s.hostHit(host, path)
		}
		return scopeHit{}, false
	case call.Tool == "Glob" || call.Tool == "Grep":
		return s.searchHit(cwd, call)
	}
	// LS lists a folder: judged as a read of its path.
	if _, ok := fileToolAccess(call.Tool); !ok && call.Tool != "LS" {
		return scopeHit{}, false
	}
	if strings.TrimSpace(call.Path) == "" {
		return scopeHit{}, false
	}
	return s.pathHit(s.norm(call.Path, cwd))
}

func (s *Scope) desc(sp *scopedProvider) string { return sp.name + " (" + sp.id + ")" }

// norm expands %APPDATA%/%LOCALAPPDATA% (which protect.Set does not) then normalizes.
func (s *Scope) norm(p, base string) string {
	return s.set.Normalize(s.expandAppData(p), base)
}

var appDataRe = regexp.MustCompile(`(?i)%(LOCALAPPDATA|APPDATA)%|\$env:(LOCALAPPDATA|APPDATA)\b|\$\{(LOCALAPPDATA|APPDATA)\}|\$(LOCALAPPDATA|APPDATA)\b`)

func (s *Scope) expandAppData(text string) string {
	return appDataRe.ReplaceAllStringFunc(text, func(m string) string {
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
}

func within(root, p string) bool {
	return p != "" && root != "" && (p == root || strings.HasPrefix(p, root+string(filepath.Separator)))
}

func (s *Scope) pathHit(p string) (scopeHit, bool) {
	if p == "" {
		return scopeHit{}, false
	}
	for _, o := range s.others {
		if within(o.path, p) {
			return scopeHit{id: OtherIdentityID, subject: p, other: o.p.id}, true
		}
	}
	if s.inClaimsPath(p) {
		return scopeHit{}, false
	}
	for _, e := range s.outPath {
		if within(e.path, p) {
			return scopeHit{id: OutOfScopePathID, subject: p, what: p + " is a credential path of " + s.desc(e.p)}, true
		}
	}
	return scopeHit{}, false
}

func (s *Scope) inClaimsPath(p string) bool {
	for _, e := range s.inPath {
		if within(e.path, p) {
			return true
		}
	}
	return false
}

// searchHit judges a Glob/Grep: the search root (path), the literal prefixes of the pattern
// and glob joined onto it, and — for a recursive search — a root that is an ancestor of an
// out-of-scope tree. For a Glob with no path, toolCallFrom put the pattern in Path, so Path
// then equals Pattern and is treated as the pattern, resolved against cwd.
func (s *Scope) searchHit(cwd string, call ToolCall) (scopeHit, bool) {
	pathRoot := ""
	if strings.TrimSpace(call.Path) != "" && call.Path != call.Pattern {
		pathRoot = s.norm(call.Path, cwd)
	}
	root := pathRoot
	if root == "" {
		root = cwd
	}
	// the literal prefix of each pattern, resolved against the path root (or cwd)
	for _, sub := range []string{call.Pattern, call.Glob} {
		if lp := literalPrefix(sub); lp != "" {
			if h, ok := s.pathHit(s.norm(joinRel(pathRoot, lp), cwd)); ok {
				return h, true
			}
		}
	}
	if pathRoot != "" {
		if h, ok := s.pathHit(pathRoot); ok {
			return h, true
		}
	}
	// a recursive search rooted at an ancestor of an out-of-scope tree reads it
	rec := call.Tool == "Grep" || strings.Contains(call.Pattern, "**") || strings.Contains(call.Glob, "**")
	if rec && root != "" {
		for _, e := range s.outPath {
			if within(root, e.path) && !s.inClaimsPath(e.path) {
				return scopeHit{id: OutOfScopePathID, subject: e.path, what: e.path + " is a credential path of " + s.desc(e.p)}, true
			}
		}
		for _, o := range s.others {
			if within(root, o.path) {
				return scopeHit{id: OtherIdentityID, subject: o.path, other: o.p.id}, true
			}
		}
	}
	return scopeHit{}, false
}

func joinRel(base, rel string) string {
	if base == "" {
		return rel
	}
	return base + "/" + rel
}

// literalPrefix is the path up to the first glob metacharacter.
func literalPrefix(p string) string {
	p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"'`))
	if p == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	var keep []string
	for _, c := range parts {
		if strings.ContainsAny(c, "*?[") {
			break
		}
		keep = append(keep, c)
	}
	return strings.Join(keep, "/")
}

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

func (s *Scope) mentionHit(text string) (scopeHit, bool) {
	if s.set == nil || len(s.mentions) == 0 {
		return scopeHit{}, false
	}
	t := filepath.ToSlash(s.set.Expand(s.expandAppData(text)))
	if s.ci {
		t = strings.ToLower(t)
	}
	if h, ok := s.mentionIn(t); ok {
		return h, true
	}
	// A quoted path segment ($env:APPDATA\'GitHub CLI', ~/".aws"/credentials) is the same
	// path once the shell removes the quotes.
	if u := strings.NewReplacer(`'`, "", `"`, "").Replace(t); u != t {
		return s.mentionIn(u)
	}
	return scopeHit{}, false
}

func (s *Scope) mentionIn(t string) (scopeHit, bool) {
	for _, m := range s.mentions {
		for i := 0; ; {
			j := strings.Index(t[i:], m.needle)
			if j < 0 {
				break
			}
			at, end := i+j, i+j+len(m.needle)
			i = end
			if (at > 0 && isPathByte(t[at-1])) || (end < len(t) && isPathByte(t[end])) {
				continue
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

func isPathByte(b byte) bool {
	return b == '.' || b == '_' || b == '-' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// --- MCP ---

func mcpTokens(server string) []string {
	parts := strings.FieldsFunc(strings.ToLower(server), func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	out := append([]string{}, parts...)
	for i := 0; i+1 < len(parts); i++ {
		out = append(out, parts[i]+parts[i+1]) // adjacent pair: google+cloud -> googlecloud
	}
	return out
}

// mcpServer: allowed if the exact server name is in the identity's own MCP list; otherwise an
// out-of-scope token denies (ADR-0007 round 2, covers mcp__aws_on_azure__*).
func (s *Scope) mcpServer(server string) (scopeHit, bool) {
	if s.bundleMCP[strings.ToLower(server)] {
		return scopeHit{}, false
	}
	toks := mcpTokens(server)
	var out *scopedProvider
	for _, t := range toks {
		if p := s.outMCP[t]; p != nil {
			out = p
			break
		}
	}
	if out == nil {
		return scopeHit{}, false
	}
	return scopeHit{id: OutOfScopeMCPID, subject: "mcp__" + server, what: "MCP server " + server + " is " + s.desc(out)}, true
}

func (s *Scope) mcpInput(cwd, raw string) (scopeHit, bool) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return scopeHit{}, false
	}
	var found scopeHit
	ok := false
	// a URL in a URL-shaped field (url, uri, endpoint, href, link, baseUrl, ...) or a value
	// that is one URL is the MCP tool's network call (a fetch or browser server): its host is
	// judged like a WebFetch's. A URL inside free text (an issue body, a memory note) is text
	// (QA round 3, item 5).
	walkKeyed(v, "", func(key, str string) bool {
		if !urlField(key) && !oneURLRe.MatchString(strings.TrimSpace(str)) {
			return true
		}
		for _, m := range urlRe.FindAllStringSubmatch(str, -1) {
			if h, hit := s.hostHit(cleanHost(m[1]), m[2]); hit {
				found, ok = h, true
				return false
			}
		}
		return true
	})
	if ok {
		return found, true
	}
	walkStrings(v, func(str string) bool {
		if len(str) > 1024 || strings.ContainsAny(str, "\n\r") || !strings.ContainsAny(str, `/\~$%`) {
			return true
		}
		if h, hit := s.pathHit(s.norm(str, cwd)); hit {
			found, ok = h, true
			return false
		}
		return true
	})
	return found, ok
}

// oneURLRe is a string value that is a single URL.
var oneURLRe = regexp.MustCompile(`(?i)^(?:https?|wss?|ftps?)://\S+$`)

// urlField reports whether a JSON key names a URL: url, uri, endpoint, href, link, or a key
// ending in url or uri (baseUrl, target_uri).
func urlField(key string) bool {
	k := strings.ToLower(key)
	switch k {
	case "url", "uri", "endpoint", "href", "link", "urls", "links":
		return true
	}
	return strings.HasSuffix(k, "url") || strings.HasSuffix(k, "uri") || strings.HasSuffix(k, "_endpoint")
}

// walkKeyed visits every string of a decoded JSON value with the key it sits under (an array
// element inherits its array's key); it stops when visit returns false.
func walkKeyed(v any, key string, visit func(key, str string) bool) bool {
	switch x := v.(type) {
	case string:
		return visit(key, x)
	case []any:
		for _, e := range x {
			if !walkKeyed(e, key, visit) {
				return false
			}
		}
	case map[string]any:
		for k, e := range x {
			if !walkKeyed(e, k, visit) {
				return false
			}
		}
	}
	return true
}

// --- hosts ---

// urlRe finds a URL: group 1 is its host, group 2 its path (up to a query or fragment).
var urlRe = regexp.MustCompile(`(?i)\b(?:https?|wss?|ftps?)://(?:[^\s/@'"` + "`" + `]*@)?([a-z0-9.-]+)(?::\d+)?(/[^\s?#'"` + "`" + `]*)?`)

func cleanHost(h string) string { return strings.Trim(strings.ToLower(h), ".") }

// urlParts is the host and lower-cased path of the first URL in u, or "".
func urlParts(u string) (host, path string) {
	if m := urlRe.FindStringSubmatch(u); m != nil {
		return cleanHost(m[1]), strings.ToLower(m[2])
	}
	return "", ""
}

var netVerbs = wordSet("curl", "wget", "invoke-webrequest", "iwr", "invoke-restmethod", "irm", "http", "https", "xh", "httpie",
	"aria2c", "start-bitstransfer", "ssh", "scp", "sftp", "nc", "ncat", "telnet")

// bareHost reads a network verb's argument written without a scheme (api.vercel.com/v9/x,
// user@host): its host and lower-cased path, or "" when it is not a host.
func bareHost(a string) (host, path string) {
	a = strings.Trim(a, `"'`)
	if strings.HasPrefix(a, "-") || strings.Contains(a, "://") {
		return "", ""
	}
	if i := strings.LastIndex(a, "@"); i >= 0 {
		a = a[i+1:]
	}
	if i := strings.IndexAny(a, "?#"); i >= 0 {
		a = a[:i]
	}
	if i := strings.IndexByte(a, '/'); i >= 0 {
		a, path = a[:i], strings.ToLower(a[i:])
	}
	if i := strings.IndexByte(a, ':'); i >= 0 {
		a = a[:i]
	}
	a = cleanHost(a)
	if !strings.Contains(a, ".") {
		return "", ""
	}
	return a, path
}

// sshVerbs open an ssh connection to their host argument.
var sshVerbs = wordSet("ssh", "scp", "sftp")

// gitNetSubs are the git subcommands that reach a remote, or record one to reach later.
var gitNetSubs = wordSet("push", "fetch", "pull", "ls-remote", "clone", "remote", "submodule", "archive")

// gitURL reads a git remote URL: its host and whether it uses the ssh transport
// (git@host:path, ssh://host/path). "" when a is not a remote URL.
func gitURL(a string) (host string, ssh bool) {
	a = strings.Trim(a, `"'`)
	if i := strings.Index(a, "://"); i > 0 {
		scheme := strings.ToLower(a[:i])
		rest := a[i+3:]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		if j := strings.LastIndex(rest, "@"); j >= 0 {
			rest = rest[j+1:]
		}
		if j := strings.IndexByte(rest, ':'); j >= 0 {
			rest = rest[:j]
		}
		return cleanHost(rest), scheme == "ssh" || scheme == "git+ssh" || scheme == "ssh+git"
	}
	// the scp-like form [user@]host:path: no slash before the colon, a dotted host
	if i := strings.IndexByte(a, ':'); i > 0 && !strings.ContainsAny(a[:i], `/\`) {
		h := a[:i]
		if j := strings.LastIndex(h, "@"); j >= 0 {
			h = h[j+1:]
		}
		if strings.Contains(h, ".") {
			return cleanHost(h), true
		}
	}
	return "", false
}

// gitTransport judges a git command that reaches a remote by URL (QA round 3, item 7): the
// ssh transport to an out-of-scope provider's git host logs in with that provider's key, and
// a push over https sends its credentials; an anonymous https clone, fetch, pull or ls-remote
// carries none and is allowed.
func (s *Scope) gitTransport(args []string) (scopeHit, bool) {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path":
			i++
		}
		i++
	}
	if i >= len(args) || !gitNetSubs[strings.ToLower(args[i])] {
		return scopeHit{}, false
	}
	sub := strings.ToLower(args[i])
	for _, a := range args[i+1:] {
		host, ssh := gitURL(a)
		if host == "" || !(ssh || sub == "push") {
			continue
		}
		if h, ok := s.gitHostHit(host); ok {
			return h, true
		}
	}
	return scopeHit{}, false
}

// gitHostHit reports host when it is an out-of-scope provider's git host (or a subdomain of
// one) and no in-scope provider lists it.
func (s *Scope) gitHostHit(host string) (scopeHit, bool) {
	under := func(g string) bool { return host == g || strings.HasSuffix(host, "."+g) }
	for _, g := range s.inGit {
		if under(g) {
			return scopeHit{}, false
		}
	}
	for _, hs := range s.outGit {
		for _, g := range hs.hosts {
			if under(g) {
				return scopeHit{id: OutOfScopeHostID, subject: host, what: host + " is a git host of " + s.desc(hs.p) + " (its ssh key or token)"}, true
			}
		}
	}
	return scopeHit{}, false
}

func hostIn(h, path string, list []string) bool {
	for _, entry := range list {
		if providers.HostMatches(entry, h, path) {
			return true
		}
	}
	return false
}

// hostHit judges a request to host h at path ("" for none). The path is normalised first
// (dot segments, repeated slashes, percent-encoded unreserved characters), then a hosts entry
// matches the host and its subdomains, under its path prefix when it has one, unless one of
// the provider's public entries matches it as precisely (ADR-0007 rounds 3 and 4: API hosts,
// not whole registrable domains).
func (s *Scope) hostHit(h, path string) (scopeHit, bool) {
	path = providers.NormalizeURLPath(path)
	for _, list := range s.inHosts {
		if hostIn(h, path, list) {
			return scopeHit{}, false
		}
	}
	for _, hs := range s.outHosts {
		if hs.m.APIHost(h, path) {
			return scopeHit{id: OutOfScopeHostID, subject: h, what: h + " is an API host of " + s.desc(hs.p)}, true
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

// envDeny reports the out-of-scope provider that owns env name, honouring in-scope claims.
func (s *Scope) envDeny(name string) *scopedProvider {
	name = strings.ToUpper(name)
	if s.inClaimsEnv(name) {
		return nil
	}
	for _, e := range s.outEnv {
		if e.name == name || (e.prefix && strings.HasPrefix(name, e.name)) {
			return e.p
		}
	}
	return nil
}

func quoteMeta(s string) string { return regexp.QuoteMeta(s) }

// explicitRe matches an explicit reference to env name N in code (any quotes).
func (s *Scope) explicitRe(e envName) *regexp.Regexp {
	key := "X:" + e.name
	if e.prefix {
		key += "*"
	}
	if re, ok := s.envRes[key]; ok {
		return re
	}
	q := quoteMeta(e.name)
	if e.prefix {
		q += `[A-Za-z0-9_]*`
	}
	alts := []string{
		`\$env:` + q,
		`%` + q + `%`,
		`\benv:\\?` + q,
		`\[(?:system\.)?environment\]::(?:get|set|expand)environmentvariable\s*\(\s*["']?` + q,
		`environ(?:\.copy\(\))?\s*\[\s*["']` + q,
		`environ\.get\s*\(\s*["']` + q,
		`getenv\s*\(\s*["']?` + q,
		`process\.env\.` + q + `\b`,
		`process\.env\[\s*["']` + q,
		`\$\{#` + q + `\}`,
		// Deno.env.get('X'), Bun.env.X / Bun.env['X'], Ruby ENV['X'] / ENV.fetch('X'),
		// Perl $ENV{X} / $ENV{'X'}
		`deno\.env\.(?:get|set|delete|has)\s*\(\s*["']` + q,
		`bun\.env\.` + q + `\b`,
		`bun\.env\[\s*["']` + q,
		`\bENV\s*\[\s*["']` + q,
		`\bENV\.(?:fetch|key\?|delete|store)\s*\(?\s*["']` + q,
		`\$ENV\s*\{\s*["']?` + q,
		`\{[^{}]*\b` + q + `\b[^{}]*\}\s*=\s*[^;\n]*process\.env`,
	}
	re := regexp.MustCompile(`(?i)(` + strings.Join(alts, "|") + `)`)
	s.envRes[key] = re
	return re
}

// expandRe matches a bash expansion or assignment of env name N (quote-aware caller).
func (s *Scope) expandRe(e envName) *regexp.Regexp {
	key := "E:" + e.name
	if e.prefix {
		key += "*"
	}
	if re, ok := s.envRes[key]; ok {
		return re
	}
	q := quoteMeta(e.name)
	if e.prefix {
		q += `[A-Za-z0-9_]*`
	}
	re := regexp.MustCompile(`(?i)(\$\{?#?` + q + `\b|(?:^|[\s;&|(` + "`" + `])` + q + `=)`)
	s.envRes[key] = re
	return re
}

func (s *Scope) envHit(code, expand string) (scopeHit, bool) {
	upperCode, upperExpand := strings.ToUpper(code), strings.ToUpper(expand)
	for _, e := range s.outEnv {
		stem := e.name
		if e.prefix {
			if !strings.Contains(upperCode, stem) && !strings.Contains(upperExpand, stem) {
				continue
			}
		} else if !strings.Contains(upperCode, e.name) && !strings.Contains(upperExpand, e.name) {
			continue
		}
		if m := s.explicitRe(e).FindString(code); m != "" {
			if name := pickName(m, e); s.envDeny(name) != nil {
				return scopeHit{id: OutOfScopeEnvID, subject: name, what: name + " is a variable of " + s.desc(e.p)}, true
			}
		}
		if m := s.expandRe(e).FindString(expand); m != "" {
			if name := pickName(m, e); s.envDeny(name) != nil {
				return scopeHit{id: OutOfScopeEnvID, subject: name, what: name + " is a variable of " + s.desc(e.p)}, true
			}
		}
	}
	// Env: drive / $env: / %..% wildcard enumeration: Env:AWS_*, $env:AWS_*
	if m := envWildcardRe.FindAllStringSubmatch(code, -1); m != nil {
		for _, g := range m {
			stem := strings.ToUpper(firstNonEmpty(g[1:]))
			if p := s.envPrefixOwner(stem); p != nil {
				return scopeHit{id: OutOfScopeEnvID, subject: stem + "*", what: stem + "* enumerates variables of " + s.desc(p)}, true
			}
		}
	}
	return scopeHit{}, false
}

var envWildcardRe = regexp.MustCompile(`(?i)(?:\$env:|%|\benv:\\?|\$\{?)([A-Za-z_][A-Za-z0-9_]*)\*`)

func (s *Scope) envPrefixOwner(stem string) *scopedProvider {
	if stem == "" || s.inClaimsEnv(stem) {
		return nil
	}
	for _, e := range s.outEnv {
		if strings.HasPrefix(e.name, stem) || (e.prefix && strings.HasPrefix(stem, e.name)) {
			if !s.inClaimsEnv(e.name) {
				return e.p
			}
		}
	}
	return nil
}

func firstNonEmpty(xs []string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// pickName extracts the var name a match named (for the log and in-claim check).
func pickName(m string, e envName) string {
	if !e.prefix {
		return e.name
	}
	up := strings.ToUpper(m)
	i := strings.Index(up, e.name)
	if i < 0 {
		return e.name
	}
	j := i + len(e.name)
	for j < len(up) && (up[j] == '_' || (up[j] >= 'A' && up[j] <= 'Z') || (up[j] >= '0' && up[j] <= '9')) {
		j++
	}
	return up[i:j]
}

// envBuiltins read or set a named variable from their arguments.
var envBuiltins = wordSet("export", "unset", "set", "setx", "printenv", "readonly", "declare", "typeset", "local")

func (s *Scope) envWord(name string) (scopeHit, bool) {
	up := strings.ToUpper(strings.Trim(name, `"'`))
	if i := strings.IndexAny(up, "="); i > 0 {
		up = up[:i]
	}
	if p := s.envDeny(up); p != nil {
		return scopeHit{id: OutOfScopeEnvID, subject: up, what: up + " is a variable of " + s.desc(p)}, true
	}
	return scopeHit{}, false
}

// --- executables ---

// bareInvocation is the subcommand of a CLI run with options only (az --version, az -h): a
// provider that claims any subcommand of the CLI claims it too (ADR-0007 round 3).
const bareInvocation = "<bare>"

// cliSubcommand is the first subcommand of a CLI's arguments: the first word that is not an
// option, past the values of the options every CLI with subcommand claims takes before it
// (az's -o/--output, --query, --subscription). With options only it is bareInvocation.
func cliSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := strings.Trim(args[i], `"'`)
		if a == "" {
			continue
		}
		if !strings.HasPrefix(a, "-") {
			return a
		}
		switch strings.ToLower(a) {
		case "-o", "--output", "--query", "--subscription":
			i++
		}
	}
	return bareInvocation
}

func (s *Scope) resolveClaims(claims []exeClaim, sub string) (scopeHit, bool, *scopedProvider) {
	inHit := false
	var out *scopedProvider
	for _, c := range claims {
		covers := c.wildcard || c.subs[strings.ToLower(sub)] || (sub == bareInvocation && len(c.subs) > 0)
		if !covers {
			continue
		}
		if c.p.inScope {
			inHit = true
		} else if out == nil {
			out = c.p
		}
	}
	return scopeHit{}, inHit, out
}

func (s *Scope) exeHit(word, sub string) (scopeHit, bool) {
	for _, n := range exeCandidates(word) {
		if claims, ok := s.exeClaims[n]; ok {
			if _, in, out := s.resolveClaims(claims, sub); in {
				return scopeHit{}, false
			} else if out != nil {
				return scopeHit{id: OutOfScopeCLIID, subject: n, what: n + " is the CLI of " + s.desc(out)}, true
			}
		}
	}
	return scopeHit{}, false
}

// exeCandidates are the names a command word may resolve to: the basename, and the
// backslash-collapsed form (a\ws -> aws).
func exeCandidates(word string) []string {
	out := []string{exeName(word)}
	if strings.Contains(word, `\`) {
		if c := exeName(strings.ReplaceAll(word, `\`, "")); c != out[0] {
			out = append(out, c)
		}
	}
	return out
}

// inlineModuleRe finds a module named in inline python code: import X, from X, and
// run_module('X') / import_module('X'). Literal spellings only (the guard is a text filter).
var inlineModuleRe = regexp.MustCompile(`(?:\bimport\s+|\bfrom\s+|(?:run_module|import_module)\s*\(\s*['"])([a-zA-Z0-9_.]+)`)

func (s *Scope) inlineModuleHit(code string) (scopeHit, bool) {
	for _, m := range inlineModuleRe.FindAllStringSubmatch(code, -1) {
		if h, ok := s.moduleHit(m[1]); ok {
			return h, true
		}
	}
	return scopeHit{}, false
}

func (s *Scope) moduleHit(mod string) (scopeHit, bool) {
	l := strings.ToLower(strings.Trim(mod, `"'`))
	for m, claims := range s.modClaims {
		if l == m || strings.HasPrefix(l, m+".") {
			if _, in, out := s.resolveClaims(claims, ""); in {
				return scopeHit{}, false
			} else if out != nil {
				return scopeHit{id: OutOfScopeCLIID, subject: "python -m " + l, what: "python -m " + l + " runs the CLI of " + s.desc(out)}, true
			}
		}
	}
	return scopeHit{}, false
}

func (s *Scope) imageHit(img string) (scopeHit, bool) {
	l := strings.ToLower(strings.Trim(img, `"'`))
	for _, cand := range []string{l, imageBase(l)} {
		if claims, ok := s.imgClaims[cand]; ok {
			if _, in, out := s.resolveClaims(claims, ""); in {
				return scopeHit{}, false
			} else if out != nil {
				return scopeHit{id: OutOfScopeCLIID, subject: cand, what: "the container image " + cand + " is the CLI of " + s.desc(out)}, true
			}
		}
	}
	return scopeHit{}, false
}

// psModuleVerbs take PowerShell module names as their arguments.
var psModuleVerbs = wordSet("import-module", "ipmo", "install-module", "update-module", "save-module", "find-module",
	"get-module", "gmo", "remove-module", "rmo", "uninstall-module", "install-psresource", "save-psresource",
	"find-psresource", "update-psresource", "get-installedmodule", "get-installedpsresource")

// psModuleArgs checks every non-option argument of a module verb (a comma list too) against
// the claimed PowerShell modules; option values (-Scope CurrentUser) are checked too, which
// only matters if one is spelled like a provider's module.
func (s *Scope) psModuleArgs(args []string) (scopeHit, bool) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		for _, m := range strings.Split(a, ",") {
			if h, ok := s.psModuleHit(m); ok {
				return h, true
			}
		}
	}
	return scopeHit{}, false
}

func (s *Scope) psModuleHit(mod string) (scopeHit, bool) {
	l := strings.ToLower(strings.TrimSpace(strings.Trim(mod, `"' `)))
	if l == "" {
		return scopeHit{}, false
	}
	var claims []exeClaim
	for m, cs := range s.psClaims {
		if providers.DottedCovers(m, l) {
			claims = append(claims, cs...)
		}
	}
	if _, in, out := s.resolveClaims(claims, ""); in {
		return scopeHit{}, false
	} else if out != nil {
		return scopeHit{id: OutOfScopeCLIID, subject: l, what: "the PowerShell module " + l + " is " + s.desc(out)}, true
	}
	return scopeHit{}, false
}

// cmdletHit judges a command word as a PowerShell cmdlet of a provider's module.
func (s *Scope) cmdletHit(word string) (scopeHit, bool) {
	name := exeName(word)
	if !strings.Contains(name, "-") || len(s.cmdlets) == 0 {
		return scopeHit{}, false
	}
	var out *scopedProvider
	for _, c := range s.cmdlets {
		if !providers.CmdletMatches(c.entry, name) {
			continue
		}
		if c.p.inScope {
			return scopeHit{}, false
		}
		if out == nil {
			out = c.p
		}
	}
	if out == nil {
		return scopeHit{}, false
	}
	return scopeHit{id: OutOfScopeCLIID, subject: name, what: name + " is a cmdlet of " + s.desc(out)}, true
}

func packageName(a string) string {
	a = strings.Trim(a, `"'`)
	if strings.HasPrefix(a, "@") {
		if i := strings.Index(a, "/"); i >= 0 {
			a = a[i+1:]
		}
	}
	if i := strings.IndexAny(a, "@="); i > 0 {
		a = a[:i]
	}
	return a
}

func (s *Scope) exeSuffixHit(text string) (scopeHit, bool) {
	l := strings.ToLower(filepath.ToSlash(text))
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
			if h, ok := s.exeHit(l[start:end], ""); ok {
				return h, true
			}
		}
	}
	return scopeHit{}, false
}

// pyReadsStdin reports whether python's arguments name no script, module or -c code: it
// reads its program from stdin (a lone `-`, options, redirections only).
func pyReadsStdin(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-":
			return true
		case a == "-c" || a == "-m" || (strings.HasPrefix(a, "-m") && len(a) > 2):
			return false
		case pyValueOpt[a]:
			i++
		case strings.HasPrefix(a, "<") || strings.HasPrefix(a, "-"):
		default:
			return false // a script
		}
	}
	return true
}

var pythonRe = regexp.MustCompile(`^(python(\d+(\.\d+)?)?w?|py|pyw)$`)

// pyValueOpt: python options that take a value, so -m is not mistaken for the script.
var pyValueOpt = wordSet("-X", "-W", "--check-hash-based-pycs")

var runnerSub = map[string][]string{
	"pnpm": {"dlx", "exec"}, "yarn": {"dlx", "exec"}, "npm": {"exec", "x"}, "bun": {"x"},
	"pipx": {"run"}, "uv": {"run", "tool"},
}

func isRunner(name string) bool {
	switch name {
	case "npx", "bunx", "pnpx", "uvx":
		return true
	}
	_, ok := runnerSub[name]
	return ok
}

// shell judges a shell command line; ps reads its text with PowerShell's quoting rules (the
// PowerShell tool, a decoded -EncodedCommand), otherwise bash's.
func (s *Scope) shell(cwd, raw, text string, depth int, ps bool) (scopeHit, bool) {
	for _, c := range guard.CommandsIn(raw, ps) {
		base := commandBase(s.set, cwd, c)
		if c.Base != "" && base != "" {
			if h, ok := s.pathHit(s.norm(c.Base, cwd)); ok {
				return h, true
			}
		}
		verb, args, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		name := exeName(verb)
		// path arguments (resolve relative words against a -C/--directory for tar)
		pbase := base
		if name == "tar" {
			if d := dirArg(args); d != "" {
				pbase = s.norm(d, base) // -C/--directory: later relative words resolve against it
			}
		}
		writes, reads := redirects(c.Words)
		for _, w := range append(append(append([]string{}, args...), writes...), reads...) {
			if s.mayName(w) {
				for _, p := range argPaths(w) {
					if h, ok := s.pathHit(s.norm(p, pbase)); ok {
						return h, true
					}
				}
			}
		}
		// executable by verb, and a PowerShell cmdlet of a provider's module
		if h, ok := s.exeHit(verb, cliSubcommand(args)); ok {
			return h, true
		}
		if h, ok := s.cmdletHit(verb); ok {
			return h, true
		}
		// env builtins
		if envBuiltins[name] {
			for _, a := range args {
				if a == "-p" || strings.HasPrefix(a, "-") {
					continue
				}
				if h, ok := s.envWord(a); ok {
					return h, true
				}
			}
		}
		switch {
		case pythonRe.MatchString(name):
			// python reading its program from stdin (`python - <<EOF`, `python3 <<EOF`,
			// `... | python`): the heredoc or piped body is the code, so the modules it
			// imports are read from the whole line
			if pyReadsStdin(args) && (strings.Contains(raw, "<<") || strings.Contains(raw, "|")) {
				if h, ok := s.inlineModuleHit(raw); ok {
					return h, true
				}
			}
			for i := 0; i < len(args); i++ {
				a := args[i]
				if pyValueOpt[a] {
					i++
					continue
				}
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
				if a == "-c" && i+1 < len(args) {
					if h, ok := s.inlineModuleHit(args[i+1]); ok {
						return h, true
					}
					break
				}
				if !strings.HasPrefix(a, "-") {
					break
				}
			}
		case isRunner(name):
			if h, ok := s.runnerHit(name, args); ok {
				return h, true
			}
		case name == "docker" || name == "podman" || name == "nerdctl":
			if h, ok := s.dockerRun(args); ok {
				return h, true
			}
		case name == "wsl" || name == "wsl.exe":
			if inner := afterWSL(args); inner != "" {
				if h, ok := s.exeHit(inner, ""); ok {
					return h, true
				}
			}
		case name == "find":
			if h, ok := s.findExec(args); ok {
				return h, true
			}
		case name == "get-command" || name == "gcm":
			for _, a := range args {
				if !strings.HasPrefix(a, "-") {
					if h, ok := s.exeHit(a, ""); ok {
						return h, true
					}
					if h, ok := s.cmdletHit(a); ok {
						return h, true
					}
					break
				}
			}
		case psModuleVerbs[name] || (name == "using" && len(args) > 1 && strings.EqualFold(args[0], "module")):
			if h, ok := s.psModuleArgs(args); ok {
				return h, true
			}
		case name == "start-process" || name == "saps":
			if h, ok := s.startProcess(args); ok {
				return h, true
			}
		case name == "start": // cmd `start [/switches] ["title"] exe [args]`
			for _, t := range cmdStartTargets(args) {
				if h, ok := s.exeHit(t, ""); ok {
					return h, true
				}
			}
		case (name == "powershell" || name == "pwsh") && depth < 3:
			if dec, present, decoded := encodedPowerShell(args); present && decoded {
				if h, ok := s.shell(base, dec, dec, depth+1, true); ok {
					return h, true
				}
			}
		case name == "eval" && depth < 3:
			for _, a := range args {
				if h, ok := s.shell(base, a, a, depth+1, ps); ok {
					return h, true
				}
			}
		case name == "git":
			if h, ok := s.gitTransport(args); ok {
				return h, true
			}
		case netVerbs[name]:
			for _, a := range args {
				if host, path := bareHost(a); host != "" {
					if h, ok := s.hostHit(host, path); ok {
						return h, true
					}
					// ssh, scp, sftp to a git host log in with that provider's key
					if sshVerbs[name] {
						if h, ok := s.gitHostHit(host); ok {
							return h, true
						}
					}
				}
			}
		}
	}
	// text scans: code (quotes kept, grep/commit/here-strings blanked) and expand (single
	// quotes also blanked, for bash expansion/assignment).
	code := maskTextArgs(text, ps)
	expand := blankSingleQuoted(code, ps)
	if h, ok := s.exeSuffixHit(code); ok {
		return h, true
	}
	for _, m := range urlRe.FindAllStringSubmatch(code, -1) {
		if h, ok := s.hostHit(cleanHost(m[1]), strings.ToLower(m[2])); ok {
			return h, true
		}
	}
	if h, ok := s.envHit(code, expand); ok {
		return h, true
	}
	return s.mentionHit(code)
}

func (s *Scope) runnerHit(name string, args []string) (scopeHit, bool) {
	subs := runnerSub[name]
	i := 0
	if subs != nil {
		for i < len(args) && strings.HasPrefix(args[i], "-") {
			i++
		}
		if i >= len(args) || !hasString(subs, strings.ToLower(args[i])) {
			return scopeHit{}, false
		}
		was := strings.ToLower(args[i])
		i++
		if name == "uv" && was == "tool" {
			if i >= len(args) || !strings.EqualFold(args[i], "run") {
				return scopeHit{}, false
			}
			i++
		}
	}
	// skip runner flags and their values (--from X, --spec X, --with X, -p X)
	for i < len(args) {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			break
		}
		if runnerValueOpt[strings.ToLower(a)] {
			i++
		}
		i++
	}
	if i >= len(args) {
		return scopeHit{}, false
	}
	pkg := packageName(args[i])
	if h, ok := s.exeHit(pkg, ""); ok {
		return h, true
	}
	return s.moduleHit(pkg)
}

var runnerValueOpt = wordSet("--from", "--spec", "--with", "-p", "--package", "--index-url")

func (s *Scope) dockerRun(args []string) (scopeHit, bool) {
	// Find `run` (also `container run`) among the leading words, skipping global flags and
	// their values loosely: any word may be checked, so no option list can shift the parse.
	i := 0
	for i < len(args) && strings.ToLower(args[i]) != "run" {
		i++
	}
	if i >= len(args) {
		return scopeHit{}, false
	}
	// Every non-dash word after run is checked against the claimed images, so an unlisted
	// value option (--platform x, --pull always, -u 0) cannot hide the image; an
	// --entrypoint value is checked against the executables.
	rest := args[i+1:]
	for k := 0; k < len(rest); k++ {
		a := rest[k]
		if v, ok := strings.CutPrefix(strings.ToLower(a), "--entrypoint="); ok {
			if h, hit := s.exeHit(v, ""); hit {
				return h, true
			}
			continue
		}
		if strings.EqualFold(a, "--entrypoint") && k+1 < len(rest) {
			if h, hit := s.exeHit(rest[k+1], ""); hit {
				return h, true
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if h, hit := s.imageHit(a); hit {
			return h, true
		}
	}
	return scopeHit{}, false
}

func afterWSL(args []string) string {
	skip := wordSet("-d", "--distribution", "-u", "--user", "--cd")
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-e" || a == "--exec" || a == "--" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if skip[a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

func (s *Scope) findExec(args []string) (scopeHit, bool) {
	for i, a := range args {
		if (a == "-exec" || a == "-execdir" || a == "-ok" || a == "-okdir") && i+1 < len(args) {
			return s.exeHit(args[i+1], "")
		}
	}
	return scopeHit{}, false
}

// startProcessOpts are Start-Process's value options, by kind: the executable, its
// argument list, and values to skip. PowerShell accepts any unambiguous prefix; a dash
// word of four or more letters that prefixes a name is read as that option.
var startProcessOpts = []struct{ name, kind string }{
	{"-filepath", "exe"}, {"-pspath", "exe"}, {"-literalpath", "exe"},
	{"-argumentlist", "args"}, {"-args", "args"},
	{"-windowstyle", "skip"}, {"-workingdirectory", "skip"}, {"-verb", "skip"},
	{"-redirectstandardoutput", "skip"}, {"-redirectstandarderror", "skip"},
	{"-redirectstandardinput", "skip"}, {"-credential", "skip"},
}

func startProcessOpt(a string) string {
	l := strings.ToLower(a)
	if !strings.HasPrefix(l, "-") || len(l) < 4 {
		return ""
	}
	for _, o := range startProcessOpts {
		if l == o.name || strings.HasPrefix(o.name, l) {
			return o.kind
		}
	}
	return ""
}

// startProcess checks -FilePath, every positional word and the -ArgumentList contents,
// skipping the values of the other value options.
func (s *Scope) startProcess(args []string) (scopeHit, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			// PowerShell binds -Name:value as the option and its value: split at the first
			// ':' (a drive letter in the value, -FilePath:C:\x\aws.exe, stays in the value).
			opt, val, colon := strings.Cut(a, ":")
			kind := startProcessOpt(opt)
			if kind == "" {
				continue // a switch (-Wait, -NoNewWindow, -PassThru)
			}
			if !colon {
				if i+1 >= len(args) {
					continue
				}
				i++
				val = args[i]
			}
			switch kind {
			case "exe":
				if h, ok := s.exeHit(val, ""); ok {
					return h, true
				}
			case "args":
				for _, w := range strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\'' || r == '"' }) {
					if h, ok := s.exeHit(w, ""); ok {
						return h, true
					}
				}
			}
			continue
		}
		if strings.Trim(a, `"'`) == "" {
			continue
		}
		if h, ok := s.exeHit(a, ""); ok {
			return h, true
		}
	}
	return scopeHit{}, false
}

// cmdStartValueSwitches are the cmd `start` switches that take the next word as a value.
var cmdStartValueSwitches = wordSet("/d", "/affinity", "/node")

// cmdStartTargets reads the candidate exe words of a cmd `start [/switches] ["title"] exe
// [args]` line. The tokenizer drops the quotes, so a title cannot be told from the exe, and
// the exe may itself be a wrapper (`start "" cmd /k aws`): every non-switch word is checked,
// except the values of /d, /affinity and /node (ADR-0007 round 3).
func cmdStartTargets(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "/") {
			if cmdStartValueSwitches[strings.ToLower(a)] {
				i++
			}
			continue
		}
		if strings.Trim(a, `"'`) == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// dirArg reads a -C/--directory value from tar-style args.
func dirArg(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-C" || a == "--directory" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		if v, ok := strings.CutPrefix(a, "--directory="); ok {
			return v
		}
		if v, ok := strings.CutPrefix(a, "-C"); ok && v != "" {
			return v
		}
	}
	return ""
}

func hasString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
