package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Company services (spec B §8): the chat/email checks (the MCP entry in the identity's
// .claude.json).

// mcpServiceChecks reports whether each enabled chat/email provider has its MCP entry in the
// workspace's Claude identity (<identity>/.claude.json mcpServers). Claude.ai connectors live in
// the account, not that file, so a missing entry is a warning, never an error.
func mcpServiceChecks(w *resolve.Workspace, add func(level, code, ws, msg, fix string)) {
	m := w.Manifest
	if !m.Chat.Enabled() && !m.Email.Enabled() {
		return
	}
	idDir := paths.IdentityDir(w.Name, "claude")
	file := filepath.Join(idDir, ".claude.json")
	projectMCP := filepath.Join(w.Root, ".mcp.json")
	servers := mcpServerNames(file, w.Root, filepath.Join(idDir, ".mcp.json"), projectMCP)
	cat := providers.MustLoad()
	for _, s := range []struct {
		kind string
		svc  *manifest.Service
	}{{"chat", m.Chat}, {"email", m.Email}} {
		if !s.svc.Enabled() {
			continue
		}
		want := s.svc.Provider
		if p, ok := cat.Get(want); ok {
			want = p.MCPName()
		}
		if name, ok := matchMCP(servers, want, s.svc.Provider); ok {
			add("ok", s.kind+"-ok", w.Name, fmt.Sprintf("%s %s: MCP server %q is configured", s.kind, s.svc.Provider, name), "")
			continue
		}
		add("warn", s.kind+"-mcp-missing", w.Name,
			fmt.Sprintf("%s %s: no MCP server for it in %s (global or this folder's project entry) or %s (ignore this if you connected it on claude.ai)", s.kind, s.svc.Provider, file, projectMCP),
			fmt.Sprintf("add %q to claude.bundle.mcp and the tq bundle catalog, run tq bundle sync %s, then /mcp in a Claude session", want, w.Name))
	}
}

// MCPServerNames is mcpServerNames for callers outside the package (tq status --json): keys
// only, so no value of any entry is read.
func MCPServerNames(claudeJSON, root string, mcpJSONs ...string) []string {
	return mcpServerNames(claudeJSON, root, mcpJSONs...)
}

// mcpServerNames is the union of MCP server names (keys only; values stay unparsed, so no env or
// header token is ever read) from the identity's .claude.json (top-level mcpServers and
// projects[<root or a folder under it>].mcpServers) and from each extra .mcp.json. A missing or
// unreadable file contributes nothing.
func mcpServerNames(claudeJSON, root string, mcpJSONs ...string) []string {
	set := map[string]bool{}
	add := func(m map[string]json.RawMessage) {
		for k := range m {
			set[k] = true
		}
	}
	type servers struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if raw, err := os.ReadFile(claudeJSON); err == nil {
		var cj struct {
			servers
			Projects map[string]servers `json:"projects"`
		}
		if json.Unmarshal(raw, &cj) == nil {
			add(cj.MCPServers)
			for p, s := range cj.Projects {
				if root != "" && withinDir(root, p) {
					add(s.MCPServers)
				}
			}
		}
	}
	for _, f := range mcpJSONs {
		if raw, err := os.ReadFile(f); err == nil {
			var s servers
			if json.Unmarshal(raw, &s) == nil {
				add(s.MCPServers)
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// withinDir reports whether p (a .claude.json project key, often with forward slashes) is root or
// a folder under it. filepath.Rel compares case-insensitively on Windows.
func withinDir(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(filepath.FromSlash(p)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// matchMCP finds the server for a provider: the catalog's mcp name exactly, else a key that has
// the provider id as a whole token (split on - _ . and spaces, case-insensitive), as in
// "slack-mcp" or "ms-Teams-mcp", never "steamship".
func matchMCP(servers []string, want, provider string) (string, bool) {
	for _, s := range servers {
		if strings.EqualFold(s, want) {
			return s, true
		}
	}
	split := func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' }
	for _, s := range servers {
		for _, tok := range strings.FieldsFunc(s, split) {
			if strings.EqualFold(tok, provider) {
				return s, true
			}
		}
	}
	return "", false
}

// finish adds "all checks passed" when nothing but ok findings were reported.
func finish(fs []Finding) []Finding {
	for _, f := range fs {
		if f.Level != "ok" {
			return fs
		}
	}
	return append(fs, Finding{"ok", "ok", "", "all checks passed", ""})
}
