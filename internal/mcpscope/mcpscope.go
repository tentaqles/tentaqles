// Package mcpscope is the harness config layer of PRD v2 §6.2 (layer 3 of the §6.1 scenario;
// ADR-0007): for an identity whose manifest sets guard: v2, the MCP servers registered in its
// Claude config home (.claude.json mcpServers, top level and per project) and its Codex home
// (config.toml [mcp_servers.<name>]) hold no server tq can tell belongs to a provider outside
// the scope, so no out-of-scope MCP tool reaches the model's tool list.
//
// "Can tell" is the hook's own rule (policy out-of-scope-mcp), plus two facts of the entry: a
// server is out of scope when a token of its name (split on _ - .) is an out-of-scope
// provider's and none is an in-scope provider's, when its url's host is an out-of-scope API
// host, or when its command is an out-of-scope CLI. A server that matches no provider (a
// user's own tool, a database, a docs server) is never touched. A removed entry is kept, as it
// was, in a file beside the config (RemovedFile), so a human can put it back after adding the
// provider to the scope.
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
	"github.com/tentaqles/tentaqles/internal/providers"
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
	return &Judge{ws: ws, s: policy.ScopeFor(ws, m.Scope(), providers.MustLoad().All())}
}

// Out reports whether the MCP server name with its config entry (the JSON object of
// mcpServers, or the TOML table read as one: command, args, url) is out of scope, and why.
func (j *Judge) Out(name string, entry map[string]any) (string, bool) {
	if j == nil {
		return "", false
	}
	if what, ok := j.s.OutOfScopeMCPServer(name); ok {
		return what, true
	}
	if u, _ := entry["url"].(string); u != "" {
		if what, ok := j.s.OutOfScopeHost(policy.URLHost(u)); ok {
			return "MCP server " + name + " calls " + what, true
		}
	}
	if c, _ := entry["command"].(string); c != "" {
		if id, ok := j.s.OutOfScopeExecutable(c); ok {
			return "MCP server " + name + " runs the CLI of " + id, true
		}
	}
	return "", false
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

// FilterClaude removes the out-of-scope servers from dir/.claude.json: the top-level
// mcpServers (user scope) and every projects[<path>].mcpServers (local scope). It writes
// nothing on a dry run or when nothing is out of scope; every other key is left as it was.
func FilterClaude(dir string, j *Judge, dryRun bool) ([]Removed, error) {
	if j == nil {
		return nil, nil
	}
	p := filepath.Join(dir, ".claude.json")
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	// Numbers stay as written (UseNumber): Claude Code keeps ids and timestamps there.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var removed []Removed
	sweep := func(servers map[string]any, where string) {
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			e, _ := servers[n].(map[string]any)
			if why, out := j.Out(n, e); out {
				removed = append(removed, Removed{Where: where, Name: n, Why: why, Entry: servers[n], At: now})
				delete(servers, n)
			}
		}
	}
	if s, ok := root["mcpServers"].(map[string]any); ok {
		sweep(s, p)
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
					sweep(s, p+" (project "+k+")")
				}
			}
		}
	}
	if len(removed) == 0 || dryRun {
		return removed, nil
	}
	if err := keepRemoved(dir, removed); err != nil {
		return nil, err
	}
	out, err := marshal(root)
	if err != nil {
		return nil, err
	}
	return removed, writeAtomic(p, out)
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

// keepRemoved appends removed to dir/RemovedFile, before the config loses them.
func keepRemoved(dir string, removed []Removed) error {
	p := filepath.Join(dir, RemovedFile)
	var all []Removed
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	all = append(all, removed...)
	out, err := marshal(all)
	if err != nil {
		return err
	}
	return writeAtomic(p, out)
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
