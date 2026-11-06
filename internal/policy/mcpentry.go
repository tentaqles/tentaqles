package policy

import (
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// An MCP server judged by its config entry (ADR-0008): the harness config layer removes it at
// render and the hook denies its mcp__<server>__ tools, by the same rule.

// OutOfScopeMCPEntry judges an MCP server by its name and its config entry (the JSON object of
// mcpServers, or a Codex table read as one). It is out of scope when its name is
// (OutOfScopeMCPServer), or when its entry
//   - runs an out-of-scope CLI (its command, or the first word of a shell-wrapped string);
//   - runs a package or image of an out-of-scope provider: a word of the command or args
//     (a --flag=value split, a shell-wrapped string split into words, a git or https URL
//     without its scheme, a file path by its file name too) starting with one of its
//     mcp_packages, compared PyPI-style (- _ . alike, case ignored); a docker or podman image
//     of its images list;
//   - selects an out-of-scope provider's toolset (--prebuilt bigquery: a token of its mcp list);
//   - sets an out-of-scope provider's variable (its env keys, or docker -e/--env NAME[=...]);
//   - calls an out-of-scope provider's API host or remote MCP host (url, serverUrl, or a URL
//     among the words: mcp-remote <url>).
//
// An in-scope provider's claim on the same fact wins. A server whose name and entry match no
// provider is in scope. what names the provider and the fact.
func (s *Scope) OutOfScopeMCPEntry(name string, entry map[string]any) (what string, ok bool) {
	if what, ok := s.OutOfScopeMCPServer(name); ok {
		return what, true
	}
	if entry == nil {
		return "", false
	}
	cmd, _ := entry["command"].(string)
	var args []string
	if as, ok := entry["args"].([]any); ok {
		for _, a := range as {
			if str, ok := a.(string); ok {
				args = append(args, str)
			}
		}
	}
	verbs := []string{}
	if cmd != "" {
		verbs = append(verbs, cmd)
	}
	words := []string{cmd}
	for _, a := range args {
		if f := strings.Fields(a); len(f) > 1 {
			// a shell-wrapped command: powershell -c "uvx awslabs...", bash -c "...".
			words = append(words, f...)
			verbs = append(verbs, strings.Trim(f[0], `"'`))
			continue
		}
		words = append(words, a)
	}
	for _, v := range verbs {
		if id, ok := s.OutOfScopeExecutable(v); ok && !providers.AmbiguousExecutable(baseFile(v)) {
			return "MCP server " + name + " runs the CLI of " + id, true
		}
	}
	docker := false
	if b := exeName(cmd); b == "docker" || b == "podman" {
		docker = true
	}
	var envKeys []string
	// A vendor's credential variable referenced anywhere in the entry (${VERCEL_TOKEN} in a
	// header, an env value, an arg) is that vendor's.
	walkStrings(entry, func(str string) bool {
		envKeys = append(envKeys, varRefs(str)...)
		return true
	})
	if hs, ok := entry["headers"].(map[string]any); ok {
		for k := range hs {
			if sp := s.tokenOwner(k); sp != nil { // X-Vercel-Token
				return "MCP server " + name + " sends the " + k + " header of " + s.desc(sp), true
			}
		}
	}
	for i := 0; i < len(words); i++ {
		w := strings.Trim(words[i], `"'`)
		if w == "" {
			continue
		}
		if k, v, ok := strings.Cut(w, "="); ok && strings.HasPrefix(k, "-") {
			if docker && (k == "-e" || k == "--env") {
				envKeys = append(envKeys, strings.SplitN(v, "=", 2)[0])
			}
			w = v // --package=@aws/x, --with=awscli
		}
		if docker && (w == "-e" || w == "--env") && i+1 < len(words) {
			envKeys = append(envKeys, strings.SplitN(strings.Trim(words[i+1], `"'`), "=", 2)[0])
			i++
			continue
		}
		if docker && strings.HasPrefix(w, "-e") && len(w) > 2 && w[2] != '-' { // -eNAME
			envKeys = append(envKeys, strings.SplitN(w[2:], "=", 2)[0])
			continue
		}
		if w == "--env-file" && i+1 < len(words) {
			f := strings.TrimSuffix(strings.ToLower(baseFile(strings.Trim(words[i+1], `"'`))), ".env")
			if sp := s.tokenOwner(f); sp != nil {
				return "MCP server " + name + " reads the env file " + words[i+1] + " of " + s.desc(sp), true
			}
		}
		// A package runner's argument (npx -y vercel@latest, uvx --with awscli, -p wrangler,
		// python -m awscli): an out-of-scope CLI or Python module, version stripped.
		if bare := stripVersion(w); !strings.HasPrefix(bare, "-") {
			if id, ok := s.OutOfScopeExecutable(bare); ok && !providers.AmbiguousExecutable(baseFile(bare)) {
				return "MCP server " + name + " runs " + w + ", the CLI of " + id, true
			}
			if h, ok := s.moduleHit(bare); ok {
				return "MCP server " + name + " runs " + h.what, true
			}
		}
		if w == "--prebuilt" && i+1 < len(words) {
			if sp := s.tokenOwner(words[i+1]); sp != nil {
				return "MCP server " + name + " runs the " + words[i+1] + " toolset of " + s.desc(sp), true
			}
		}
		if docker {
			if h, ok := s.imageHit(w); ok {
				return "MCP server " + name + " runs " + h.what, true
			}
		}
		for _, cand := range []string{w, baseFile(w)} {
			if sp := s.packageOwner(cand); sp != nil {
				return "MCP server " + name + " runs " + w + ", a package of " + s.desc(sp), true
			}
		}
		if h, path := urlParts(w); h != "" {
			if what, ok := s.mcpHostOut(h, path); ok {
				return "MCP server " + name + " calls " + what, true
			}
		}
	}
	for _, k := range []string{"url", "serverUrl"} {
		if u, _ := entry[k].(string); u != "" {
			h, path := urlParts(u)
			if what, ok := s.mcpHostOut(h, path); ok {
				return "MCP server " + name + " calls " + what, true
			}
		}
	}
	if env, ok := entry["env"].(map[string]any); ok {
		for k := range env {
			envKeys = append(envKeys, k)
		}
	}
	if out := s.OutOfScopeEnv(envKeys); len(out) > 0 {
		return "MCP server " + name + " sets " + out[0] + ", a variable of a provider outside the scope", true
	}
	return "", false
}

// stripVersion drops a package's version or tag: vercel@latest -> vercel, @aws/x@1 -> @aws/x.
func stripVersion(w string) string {
	if i := strings.LastIndex(w, "@"); i > 0 && !strings.Contains(w[i:], "/") {
		return w[:i]
	}
	return w
}

var varRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)|%([A-Za-z_][A-Za-z0-9_]*)%`)

// varRefs are the variable names a string references: ${NAME}, $NAME, %NAME%.
func varRefs(s string) []string {
	var out []string
	for _, m := range varRefRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1]+m[2]+m[3])
	}
	return out
}

// baseFile is a path's file name without a Windows program suffix:
// C:\bin\awslabs.aws-api-mcp-server.exe -> awslabs.aws-api-mcp-server.
func baseFile(w string) string {
	w = strings.ReplaceAll(w, `\`, "/")
	if i := strings.LastIndex(w, "/"); i >= 0 {
		w = w[i+1:]
	}
	l := strings.ToLower(w)
	for _, suf := range []string{".exe", ".cmd", ".bat", ".ps1", ".com"} {
		if strings.HasSuffix(l, suf) {
			return w[:len(w)-len(suf)]
		}
	}
	return w
}

func pkgNorm(w string) string { return providers.PkgNorm(w) }

// packageOwner is the out-of-scope provider whose mcp_packages w starts with, unless an
// in-scope provider lists one too.
func (s *Scope) packageOwner(w string) *scopedProvider {
	l := pkgNorm(w)
	if l == "" {
		return nil
	}
	has := func(sp *scopedProvider) bool {
		for _, p := range sp.packages {
			if strings.HasPrefix(l, pkgNorm(p)) {
				return true
			}
		}
		return false
	}
	for _, sp := range s.provs {
		if sp.inScope && has(sp) {
			return nil
		}
	}
	for _, sp := range s.provs {
		if !sp.inScope && has(sp) {
			return sp
		}
	}
	return nil
}

// tokenOwner is the out-of-scope provider whose mcp list holds a token of w (bigquery).
func (s *Scope) tokenOwner(w string) *scopedProvider {
	for _, t := range mcpTokens(strings.Trim(w, `"'`)) {
		if s.inMCP[t] {
			return nil
		}
	}
	for _, t := range mcpTokens(strings.Trim(w, `"'`)) {
		if sp := s.outMCP[t]; sp != nil {
			return sp
		}
	}
	return nil
}

// mcpHostOut reports an out-of-scope provider's API host (hosts) or remote MCP host
// (mcp_hosts) at URL path path, in-scope claims first.
func (s *Scope) mcpHostOut(h, path string) (string, bool) {
	h = strings.Trim(strings.ToLower(h), ".")
	if h == "" {
		return "", false
	}
	path = providers.NormalizeURLPath(path)
	for _, sp := range s.provs {
		if sp.inScope && hostIn(h, path, sp.mcpHosts) {
			return "", false
		}
	}
	if hit, ok := s.hostHit(h, path); ok {
		return hit.what, true
	}
	for _, sp := range s.provs {
		if !sp.inScope && hostIn(h, path, sp.mcpHosts) {
			return h + " is a remote MCP endpoint of " + s.desc(sp), true
		}
	}
	return "", false
}

// MCPToolName is how Claude Code spells an MCP server name in a tool name
// (mcp__<server>__<tool>): every character but a letter, a digit, _ and - becomes _.
func MCPToolName(server string) string {
	b := []byte(server)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			b[i] = '_'
		}
	}
	return string(b)
}

// mcpByEntry judges an MCP call by every config entry of its server name, loaded once, with
// the hybrid rule (JudgeMCP): a server defined in several sources (user, project, .mcp.json,
// a plugin, Codex, a render's backup) is denied unless every entry is kept, whichever one the
// harness picks. A claude.ai connector has no entry: it is judged by its name. Any other name
// found in no source keeps the name rule alone.
func (s *Scope) mcpByEntry(server string) (scopeHit, bool) {
	if s.mcpLoad != nil && !s.mcpLoaded {
		s.mcpLoaded = true
		s.mcpEntries = map[string][]map[string]any{}
		for n, es := range s.mcpLoad() {
			k := MCPToolName(n)
			s.mcpEntries[k] = append(s.mcpEntries[k], es...)
		}
	}
	es := s.mcpEntries[server]
	if len(es) == 0 {
		if strings.HasPrefix(server, "claude_ai_") {
			if what, deny := s.JudgeMCP(server, nil); deny {
				return scopeHit{id: OutOfScopeMCPID, subject: "mcp__" + server, what: what}, true
			}
		}
		return scopeHit{}, false
	}
	for _, e := range es {
		if what, deny := s.JudgeMCP(server, e); deny {
			return scopeHit{id: OutOfScopeMCPID, subject: "mcp__" + server, what: what}, true
		}
	}
	return scopeHit{}, false
}

// JudgeMCP is the owner's hybrid MCP list (ADR-0008, 2026-11-05): an MCP server with its
// config entry (nil for a claude.ai connector, judged by its name) is kept when
//
//	(a) the catalog attributes it to an in-scope provider, or the manifest declares it
//	    (claude.bundle.mcp), or
//	(b) it is a general-purpose, provider-free server (providers.MCPGeneral: by what it runs,
//	    a connector by its name), or
//	(c) its exact name is in the manifest's mcp.allow;
//
// and denied (deny true, with why) otherwise. An out-of-scope attribution
// (OutOfScopeMCPEntry) denies first, whatever (a) to (c) say.
func (s *Scope) JudgeMCP(name string, entry map[string]any) (why string, deny bool) {
	if what, out := s.OutOfScopeMCPEntry(name, entry); out {
		return what, true
	}
	if s.mcpAllow[MCPToolName(name)] || s.bundleMCP[strings.ToLower(name)] || s.inScopeMCP(name, entry) || s.generalMCP(name, entry) {
		return "", false
	}
	return "MCP server " + name + " belongs to no provider of the scope, is not a general-purpose server and is not in the manifest's mcp.allow", true
}

// entryURL is the host and normalised path of a URL an entry calls.
type entryURL struct{ host, path string }

// entryFacts are what an entry runs and calls: its words (command, args, a --flag=value's
// value, a shell-wrapped string's words), the hosts and paths of its url, serverUrl and URL
// words, and its variables (env keys, docker -e names, ${NAME} references).
func entryFacts(entry map[string]any) (words []string, urls []entryURL, env []string, docker bool) {
	addURL := func(u string) {
		if h, path := urlParts(u); h != "" {
			urls = append(urls, entryURL{h, providers.NormalizeURLPath(path)})
		}
	}
	cmd, _ := entry["command"].(string)
	if cmd != "" {
		words = append(words, cmd)
	}
	if b := exeName(cmd); b == "docker" || b == "podman" {
		docker = true
	}
	if as, ok := entry["args"].([]any); ok {
		for _, a := range as {
			str, ok := a.(string)
			if !ok {
				continue
			}
			for _, w := range strings.Fields(str) {
				w = strings.Trim(w, `"'`)
				if k, v, ok := strings.Cut(w, "="); ok && strings.HasPrefix(k, "-") {
					w = v
				}
				words = append(words, w)
			}
		}
	}
	for _, w := range words {
		addURL(w)
	}
	for _, k := range []string{"url", "serverUrl"} {
		if u, _ := entry[k].(string); u != "" {
			addURL(u)
		}
	}
	if m, ok := entry["env"].(map[string]any); ok {
		for k := range m {
			env = append(env, k)
		}
	}
	return words, urls, env, docker
}

// inScopeMCP reports whether the catalog attributes the server to an in-scope provider: a
// token of its name, a package, CLI, image or host of its entry, or one of its variables.
func (s *Scope) inScopeMCP(name string, entry map[string]any) bool {
	for _, t := range mcpTokens(name) {
		if s.inMCP[t] {
			return true
		}
	}
	if entry == nil {
		return false
	}
	words, urls, env, docker := entryFacts(entry)
	for _, w := range words {
		bare := stripVersion(w)
		for _, n := range exeCandidates(bare) {
			for _, c := range s.exeClaims[n] {
				if c.p.inScope {
					return true
				}
			}
		}
		l := pkgNorm(bare)
		for _, sp := range s.provs {
			if !sp.inScope {
				continue
			}
			for _, p := range sp.packages {
				if strings.HasPrefix(l, pkgNorm(p)) {
					return true
				}
			}
		}
		if docker {
			for _, cand := range []string{strings.ToLower(w), imageBase(strings.ToLower(w))} {
				for _, c := range s.imgClaims[cand] {
					if c.p.inScope {
						return true
					}
				}
			}
		}
	}
	for _, u := range urls {
		for _, list := range s.inHosts {
			if hostIn(u.host, u.path, list) {
				return true
			}
		}
		for _, sp := range s.provs {
			if sp.inScope && hostIn(u.host, u.path, sp.mcpHosts) {
				return true
			}
		}
	}
	for _, k := range env {
		if s.inClaimsEnv(strings.ToUpper(k)) {
			return true
		}
	}
	return false
}

// generalMCP reports whether the server is a general-purpose, provider-free one: what its
// entry runs or calls matches one (a package or image up to a version, path or tag; a host);
// a connector (no entry) by a token of its name.
func (s *Scope) generalMCP(name string, entry map[string]any) bool {
	if entry == nil {
		for _, g := range s.general {
			for _, t := range mcpTokens(name) {
				for _, gt := range g.Match.MCP {
					if t == gt {
						return true
					}
				}
			}
		}
		return false
	}
	words, urls, _, _ := entryFacts(entry)
	for _, g := range s.general {
		for _, w := range words {
			for _, cand := range []string{w, baseFile(w)} {
				l := pkgNorm(stripVersion(cand))
				for _, p := range append(append([]string{}, g.Match.MCPPackages...), g.Match.Images...) {
					if pkgBoundary(l, pkgNorm(p)) {
						return true
					}
				}
			}
		}
		for _, u := range urls {
			if hostIn(u.host, u.path, g.Match.MCPHosts) {
				return true
			}
		}
	}
	return false
}

// pkgBoundary: l is the package p, or p up to a version, path or tag (@ / :).
func pkgBoundary(l, p string) bool {
	if p == "" || !strings.HasPrefix(l, p) {
		return false
	}
	return len(l) == len(p) || strings.HasSuffix(p, "/") || strings.ContainsRune("@/:", rune(l[len(p)]))
}
