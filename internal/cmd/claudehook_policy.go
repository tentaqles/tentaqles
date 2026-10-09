package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"gopkg.in/yaml.v3"
)

// ProjectRulesFile is the per-repo rule file. It is repo content, so it can
// only add rules (see policy.Layers).
const ProjectRulesFile = ".claude/tq-rules.yaml"

// shellTools are the tools whose tool_input.command is a shell command line.
func isShellTool(name string) bool { return name == "Bash" || name == "PowerShell" }

// toolCallFrom normalizes a hook payload into the policy's view of the call.
// Unknown tools yield a ToolCall with only Tool set, which no path/content
// rule can match.
func toolCallFrom(p hookPayload, cwd string) policy.ToolCall {
	call := policy.ToolCall{Tool: p.ToolName}
	raw := unwrapToolInput(p.ToolInput)
	if strings.HasPrefix(p.ToolName, "mcp__") {
		call.Content = string(raw)
		return call
	}
	if p.ToolName == "Agent" {
		// The task prompt is what content rules can judge on a launch.
		var in struct {
			Prompt      string `json:"prompt"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal(raw, &in)
		call.Content = strings.Join(nonEmpty([]string{in.Description, in.Prompt}), "\n")
		return call
	}
	var in struct {
		Command      string `json:"command"`
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Path         string `json:"path"`
		Content      string `json:"content"`
		NewString    string `json:"new_string"`
		NewSource    string `json:"new_source"`
		Edits        []struct {
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &in)
	}
	call.Command = strings.TrimSpace(in.Command)
	switch {
	case in.FilePath != "":
		call.Path = in.FilePath
	case in.NotebookPath != "":
		call.Path = in.NotebookPath
	case in.Path != "":
		call.Path = in.Path
	}
	parts := []string{in.Content, in.NewString, in.NewSource}
	for _, e := range in.Edits {
		parts = append(parts, e.NewString)
	}
	call.Content = strings.Join(nonEmpty(parts), "\n")
	if call.Path != "" {
		abs := call.Path
		if !filepath.IsAbs(abs) && cwd != "" {
			abs = filepath.Join(cwd, abs)
		}
		if _, err := os.Stat(abs); err == nil {
			call.Exists = true
		}
	}
	return call
}

// unwrapToolInput returns tool_input as a JSON object, accepting both the
// object form and the JSON-encoded-string form Claude Code may send.
func unwrapToolInput(raw json.RawMessage) json.RawMessage {
	var s string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return json.RawMessage(s)
	}
	return raw
}

func nonEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// resolveTrusted maps cwd to its trusted workspace, or nil. Errors mean nil:
// the built-in rules still apply without a workspace.
func resolveTrusted(cwd string) (*resolve.Workspace, *registry.Config) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, nil
	}
	return resolve.Resolve(cwd, cfg).Workspace, cfg
}

// loadProjectRules collects every .claude/tq-rules.yaml from cwd up to the
// workspace root (or at most 8 levels when there is no workspace).
func loadProjectRules(cwd string, ws *resolve.Workspace) ([]policy.Rule, []error) {
	var rules []policy.Rule
	var errs []error
	stop := ""
	if ws != nil {
		stop = strings.ToLower(filepath.Clean(ws.Root))
	}
	dir := filepath.Clean(cwd)
	for i := 0; i < 8 && dir != ""; i++ {
		p := filepath.Join(dir, filepath.FromSlash(ProjectRulesFile))
		if raw, err := os.ReadFile(p); err == nil {
			var f struct {
				Rules []policy.Rule `yaml:"rules"`
			}
			if err := yaml.Unmarshal(raw, &f); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p, err))
			} else {
				for j := range f.Rules {
					f.Rules[j].Source = p
				}
				rules = append(rules, f.Rules...)
			}
		}
		if strings.ToLower(dir) == stop {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return rules, errs
}

// policyDecision evaluates every layer of rules plus the two checks that need
// I/O: the commit secret scan and the cross-client MCP check.
func policyDecision(cwd string, ws *resolve.Workspace, cfg *registry.Config, call policy.ToolCall) policy.Decision {
	eff := effectiveGuard(ws)
	layers := policy.Layers{Builtin: policy.Builtin(), Global: eff.Global, Disable: eff.Disable, Actions: eff.Actions}
	if ws != nil {
		layers.Manifest = ws.Manifest.Guard.Rules
		for i := range layers.Manifest {
			layers.Manifest[i].Source = ws.ManifestPath
		}
	}
	layers.Project, _ = loadProjectRules(cwd, ws)
	set, _ := policy.Build(layers)
	d := set.Evaluate(call)

	if call.IsShell() && !eff.Off("tq/commit-secret") {
		if hits := commitscan.Scan(cwd, call.Command, gitcfg.RunGitIn); len(hits) > 0 {
			d = policy.Merge(d, ruleDecision(eff.ActionFor("tq/commit-secret", policy.Deny), "tq/commit-secret", commitReason(hits)))
		}
	}
	if call.IsMCP() && !eff.Off("tq/cross-client-mcp") {
		if other := mcpOwner(ws, cfg, call.MCPServer()); other != "" {
			d = policy.Merge(d, ruleDecision(eff.ActionFor("tq/cross-client-mcp", policy.Deny), "tq/cross-client-mcp",
				fmt.Sprintf("MCP server %q belongs to client %q, not to this workspace", call.MCPServer(), other)))
		}
	}
	return d
}

func ruleDecision(a policy.Action, id, reason string) policy.Decision {
	return policy.Decision{Action: a, Matched: []*policy.Rule{{ID: id, Action: a, Reason: reason, Source: "builtin"}}}
}

func commitReason(hits []commitscan.Hit) string {
	var names []string
	for i, h := range hits {
		if i == 10 {
			names = append(names, fmt.Sprintf("… and %d more", len(hits)-10))
			break
		}
		names = append(names, h.String())
	}
	return "this commit would record secrets: " + strings.Join(names, ", ") +
		". Remove them (use env vars / a secret store), or mark a known-fake fixture line with `tq:allow-secret`"
}

// mcpOwner returns the client whose bundle declares server when the current
// workspace's bundle does not, or "". Workspaces that don't manage MCP
// through a bundle are never judged: there is nothing to compare against.
func mcpOwner(ws *resolve.Workspace, cfg *registry.Config, server string) string {
	if ws == nil || cfg == nil || server == "" || ws.Manifest.Claude.Bundle == nil {
		return ""
	}
	for _, s := range ws.Manifest.Claude.Bundle.MCP {
		if s == server {
			return ""
		}
	}
	all, _ := resolve.ListWorkspaces(cfg)
	for _, other := range all {
		if other.Manifest == nil || other.Manifest.Client == ws.Manifest.Client || other.Manifest.Claude.Bundle == nil {
			continue
		}
		for _, s := range other.Manifest.Claude.Bundle.MCP {
			if s == server {
				return other.Manifest.Client
			}
		}
	}
	return ""
}

// writeAsk emits the PreToolUse JSON that makes Claude Code ask the user.
func writeAsk(w io.Writer, reason string) error {
	return json.NewEncoder(w).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "ask",
			"permissionDecisionReason": reason,
		},
	})
}

// effectiveGuard merges the user's global guard file with the workspace
// manifest (nil ws: the global file alone).
func effectiveGuard(ws *resolve.Workspace) guardcfg.Effective {
	if ws == nil || ws.Manifest == nil {
		return guardcfg.Resolve(nil, "")
	}
	return guardcfg.Resolve(ws.Manifest, ws.Name)
}
