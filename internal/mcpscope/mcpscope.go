// Package mcpscope is the harness config layer of PRD v2 §6.2 (layer 3 of the §6.1 scenario;
// ADR-0007): for an identity whose manifest sets guard: v2, the MCP servers registered in its
// Claude config home (.claude.json mcpServers, top level and per project) and its Codex home
// (config.toml [mcp_servers.<name>]) hold no server tq can attribute to a provider outside the
// scope, so no out-of-scope MCP tool reaches the model's tool list.
//
// A server is attributed by its name and its config entry (policy.OutOfScopeMCPEntry): a name
// token, the package or image it runs (mcp_packages), its command, a variable it sets, or the
// host it calls. A server that matches no provider (a user's own tool, a database, a docs
// server) is never touched. A removed entry is kept, as it was, in a file beside the config
// (RemovedFile), so a human can put it back after adding the provider to the scope. The same
// entries are what the hook judges an mcp__<server>__ call by (Entries).
package mcpscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// RemovedFile is the file, in the Claude or Codex identity dir, that keeps every MCP entry
// tq removed as out of scope, newest last.
const RemovedFile = "mcp-out-of-scope.json"

// Judge is an identity's MCP scope.
type Judge struct {
	ws string
	s  *policy.Scope
}

// For is ws's judge, or nil when the manifest does not set guard: v2 (nothing is filtered).
func For(ws string, m *manifest.Manifest) *Judge {
	if m == nil || !m.GuardV2() {
		return nil
	}
	return &Judge{ws: ws, s: policy.ScopeFor(ws, m.Scope(), m.BundleMCP(), m.MCPAllow())}
}

// Out reports whether the MCP server name with its config entry (the JSON object of
// mcpServers, or a Codex table read as one: command, args, env, url) is out of scope, and why.
func (j *Judge) Out(name string, entry map[string]any) (string, bool) {
	if j == nil {
		return "", false
	}
	return j.s.JudgeMCP(name, entry)
}

// Removed is one MCP entry render took out.
type Removed struct {
	Where string `json:"where"` // the config file, and the project for a per-project entry
	Name  string `json:"name"`
	Why   string `json:"why"`
	// Entry is the entry as it was: a JSON object for Claude, the TOML text for Codex.
	Entry any    `json:"entry"`
	At    string `json:"at"`
}

// readClaude parses dir/.claude.json, numbers as written. A missing or empty file is nil.
func readClaude(p string) ([]byte, map[string]any, error) {
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) || (err == nil && len(bytes.TrimSpace(raw)) == 0) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return raw, root, nil
}

// FilterClaude removes the out-of-scope servers from dir/.claude.json: the top-level
// mcpServers (user scope) and every projects[<path>].mcpServers (local scope). The file is
// edited in place, member by member: every other byte (indentation, key order, numbers,
// escapes) stays as it was. It writes nothing on a dry run or when nothing is out of scope.
func FilterClaude(dir string, j *Judge, dryRun bool) ([]Removed, error) {
	if j == nil {
		return nil, nil
	}
	p := filepath.Join(dir, ".claude.json")
	raw, root, err := readClaude(p)
	if err != nil || root == nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var removed []Removed
	var cuts [][]string // JSON paths of the members to delete
	sweep := func(servers map[string]any, where string, path []string) {
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			e, _ := servers[n].(map[string]any)
			if why, out := j.Out(n, e); out {
				removed = append(removed, Removed{Where: where, Name: n, Why: why, Entry: servers[n], At: now})
				cuts = append(cuts, append(append([]string{}, path...), n))
			}
		}
	}
	if s, ok := root["mcpServers"].(map[string]any); ok {
		sweep(s, p, []string{"mcpServers"})
	}
	if projs, ok := root["projects"].(map[string]any); ok {
		keys := make([]string, 0, len(projs))
		for k := range projs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if pr, ok := projs[k].(map[string]any); ok {
				if s, ok := pr["mcpServers"].(map[string]any); ok {
					sweep(s, p+" (project "+k+")", []string{"projects", k, "mcpServers"})
				}
			}
		}
	}
	if len(removed) == 0 || dryRun {
		return removed, nil
	}
	out := raw
	for _, c := range cuts {
		next, err := deleteMember(out, c)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = next
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("%s: the edited file is not valid JSON; nothing written", p)
	}
	if err := keepRemoved(dir, removed); err != nil {
		return nil, err
	}
	return removed, writeAtomic(p, out)
}

// keepRemoved appends removed to dir/RemovedFile, before the config loses them. A file that
// is not a list tq wrote (hand-edited, truncated) is never overwritten: it is moved aside to
// RemovedFile.corrupt-<time> and a new list starts.
func keepRemoved(dir string, removed []Removed) error {
	p := filepath.Join(dir, RemovedFile)
	var all []Removed
	if b, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &all); err != nil {
			aside := p + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
			if err := os.Rename(p, aside); err != nil {
				return fmt.Errorf("%s is not a list tq can read and could not be moved aside: %w", p, err)
			}
			all = nil
		}
	}
	all = append(all, removed...)
	out, err := marshal(all)
	if err != nil {
		return err
	}
	return writeAtomic(p, out)
}

// marshal is indented JSON with <, > and & as written.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tq-%d.tmp", p, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Entries are the MCP servers the identity's harness loads that tq can read, by server name,
// every source's entry kept (ADR-0007): .claude.json's top-level servers (user scope) and
// those of every project whose path holds cwd (local scope, in path order), the .mcp.json
// files from cwd up to root (project scope; root is the workspace root, "" for cwd only), the
// identity's installed plugins' servers (named plugin_<plugin>_<server>, as Claude Code
// names their tools), and Codex's config.toml servers. The hook denies a name when any of
// its entries is out of scope, whatever precedence the harness gives them. A read or parse
// error leaves that source out: the hook still judges by name.
func Entries(claudeDir, codexDir, cwd, root string) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	add := func(m map[string]any) {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if em, ok := m[n].(map[string]any); ok {
				out[n] = append(out[n], em)
			}
		}
	}
	if claudeDir != "" {
		if _, root, err := readClaude(filepath.Join(claudeDir, ".claude.json")); err == nil && root != nil {
			if s, ok := root["mcpServers"].(map[string]any); ok {
				add(s)
			}
			if projs, ok := root["projects"].(map[string]any); ok {
				keys := make([]string, 0, len(projs))
				for k := range projs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if pr, ok := projs[k].(map[string]any); ok && within(k, cwd) {
						if s, ok := pr["mcpServers"].(map[string]any); ok {
							add(s)
						}
					}
				}
			}
		}
		for _, ps := range PluginServers(claudeDir) {
			out[ps.ToolName] = append(out[ps.ToolName], ps.Entry)
		}
	}
	// The entries render removed: a session that started before the render still has them
	// loaded (and Claude Code may write them back), so their tools stay denied.
	for _, dir := range []string{claudeDir, codexDir} {
		if dir == "" {
			continue
		}
		for _, r := range readRemoved(dir) {
			switch e := r.Entry.(type) {
			case map[string]any:
				out[r.Name] = append(out[r.Name], e)
			case string:
				_, facts := scanCodex(e)
				for n, f := range facts {
					out[n] = append(out[n], f)
				}
			}
		}
	}
	for d := filepath.Clean(cwd); d != "" && cwd != ""; d = filepath.Dir(d) {
		if _, r, err := readClaude(filepath.Join(d, ".mcp.json")); err == nil && r != nil {
			if s, ok := r["mcpServers"].(map[string]any); ok {
				add(s)
			}
		}
		if root == "" || samePath(d, filepath.Clean(root)) || filepath.Dir(d) == d {
			break
		}
	}
	if codexDir != "" {
		if b, err := os.ReadFile(filepath.Join(codexDir, "config.toml")); err == nil {
			_, facts := scanCodex(string(b))
			names := make([]string, 0, len(facts))
			for n := range facts {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				out[n] = append(out[n], facts[n])
			}
		}
	}
	return out
}

// readRemoved is dir/RemovedFile's entries; a missing or unreadable file is none.
func readRemoved(dir string) []Removed {
	b, err := os.ReadFile(filepath.Join(dir, RemovedFile))
	if err != nil {
		return nil
	}
	var all []Removed
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&all) != nil {
		return nil
	}
	return all
}

// PluginServer is one MCP server an installed Claude Code plugin brings.
type PluginServer struct {
	Plugin, Server string
	// ToolName is the server name in its tools: plugin_<plugin>_<server>.
	ToolName string
	Entry    map[string]any
	File     string
}

// PluginServers reads the MCP servers of the plugins installed in the Claude config home
// claudeDir: plugins/installed_plugins.json names each plugin's installPath, whose .mcp.json
// ({"mcpServers": {...}} or the servers at the top level) and .claude-plugin/plugin.json
// ("mcpServers") declare them. A plugin's files are its own: render reports an out-of-scope
// one and never edits it; the hook denies its tools.
func PluginServers(claudeDir string) []PluginServer {
	raw, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	keys := make([]string, 0, len(doc.Plugins))
	for k := range doc.Plugins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []PluginServer
	for _, k := range keys {
		type inst struct {
			InstallPath string `json:"installPath"`
		}
		var many []inst
		if json.Unmarshal(doc.Plugins[k], &many) != nil {
			var one inst
			if json.Unmarshal(doc.Plugins[k], &one) != nil {
				continue
			}
			many = []inst{one}
		}
		plugin := strings.SplitN(k, "@", 2)[0]
		for _, in := range many {
			if in.InstallPath == "" {
				continue
			}
			for _, src := range pluginSources(in.InstallPath) {
				names := make([]string, 0, len(src.servers))
				for n := range src.servers {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					if e, ok := src.servers[n].(map[string]any); ok {
						out = append(out, PluginServer{Plugin: plugin, Server: n, ToolName: "plugin_" + plugin + "_" + n, Entry: e, File: src.file})
					}
				}
			}
		}
	}
	return out
}

type pluginSource struct {
	file    string
	servers map[string]any
}

// pluginSources are the MCP server maps of the plugin at root, in every shape Claude Code's
// plugin manifest accepts: root/.mcp.json (servers under mcpServers or at the top level), and
// .claude-plugin/plugin.json's mcpServers as an object of servers, a path (relative to root,
// ${CLAUDE_PLUGIN_ROOT} allowed) to such a file, or an array of paths and objects.
func pluginSources(root string) []pluginSource {
	var out []pluginSource
	readFile := func(f string) {
		if _, doc, err := readClaude(f); err == nil && doc != nil {
			if s, ok := doc["mcpServers"].(map[string]any); ok {
				out = append(out, pluginSource{f, s})
			} else {
				out = append(out, pluginSource{f, doc})
			}
		}
	}
	resolve := func(p string) string {
		p = strings.ReplaceAll(p, "${CLAUDE_PLUGIN_ROOT}", root)
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, filepath.FromSlash(p))
		}
		return filepath.Clean(p)
	}
	readFile(filepath.Join(root, ".mcp.json"))
	manifest := filepath.Join(root, ".claude-plugin", "plugin.json")
	_, doc, err := readClaude(manifest)
	if err != nil || doc == nil {
		return out
	}
	var one func(v any)
	one = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			out = append(out, pluginSource{manifest, x})
		case string:
			readFile(resolve(x))
		case []any:
			for _, e := range x {
				one(e)
			}
		}
	}
	one(doc["mcpServers"])
	return out
}

// within reports whether p (a project key of .claude.json, forward slashes) is cwd or holds it.
func within(p, cwd string) bool {
	a := strings.TrimRight(filepath.ToSlash(p), "/")
	b := filepath.ToSlash(cwd)
	if os.PathSeparator == '\\' {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	return a != "" && (a == b || strings.HasPrefix(b, a+"/"))
}

func samePath(a, b string) bool {
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
