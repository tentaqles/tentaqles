package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

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

// hookRuleIDs are the rules the hook applies, and the only ids it reports
// besides the identity guard's: the built-in secret rules, the commit secret
// scan and the cross-client MCP check. No other built-in, no manifest, repo
// or global rule file, no mode and no pack guard is read here.
var hookRuleIDs = map[string]bool{
	"tq/secret-store-read":  true,
	"tq/secret-store-shell": true,
	"tq/env-file-read":      true,
	"tq/env-file-shell":     true,
	"tq/env-dump":           true,
	"tq/commit-secret":      true,
	"tq/cross-client-mcp":   true,
}

var (
	hookSetOnce sync.Once
	hookSet     *policy.Set
)

// buildPolicy compiles the hook's rule set once: the built-in rules in
// hookRuleIDs, one rule per id (the first rule of an id wins).
func buildPolicy() *policy.Set {
	hookSetOnce.Do(func() {
		seen := map[string]bool{}
		var rules []policy.Rule
		for _, r := range policy.Builtin() {
			if !hookRuleIDs[r.ID] || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			rules = append(rules, r)
		}
		hookSet, _ = policy.Build(policy.Layers{Builtin: rules})
	})
	return hookSet
}

// policyDecision evaluates the hook's rule set plus the two checks that need
// I/O: the commit secret scan and the cross-client MCP check.
//
// deadline ends the scan; the hook computes it at entry (start plus the
// scan budget), so the identity lookups that ran before count against it.
func policyDecision(cwd string, ws *resolve.Workspace, cfg *registry.Config, call policy.ToolCall, deadline time.Time) policy.Decision {
	return policyDecisionWith(buildPolicy(), cwd, ws, cfg, call, deadline)
}

// policyDecisionWith is policyDecision with the rule set already built.
func policyDecisionWith(set *policy.Set, cwd string, ws *resolve.Workspace, cfg *registry.Config, call policy.ToolCall, deadline time.Time) policy.Decision {
	d := set.Evaluate(call)
	if call.IsShell() && commitscan.PlanFor(call.Command).Commits {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		hits, err := commitscan.ScanCtx(ctx, cwd, call.Command, commitRunner(ctx))
		switch {
		case len(hits) > 0:
			d = policy.Merge(d, ruleDecision(policy.Deny, "tq/commit-secret", commitReason(hits)))
		case err != nil:
			// The one ask left: a scan that could not vouch for the commit.
			d = policy.Merge(d, ruleDecision(policy.Ask, "tq/commit-secret", "the secret "+commitScanAsk(ctx, err)))
		}
	}
	if call.IsMCP() {
		if other := mcpOwner(ws, cfg, call.MCPServer()); other != "" {
			d = policy.Merge(d, ruleDecision(policy.Deny, "tq/cross-client-mcp",
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

// maxScanBytes bounds the text the commit scan reads.
const maxScanBytes = commitscan.MaxTotalBytes

var errScanTooLarge = errors.New("too large to scan")

// commitScanAsk words the ask for a commit scan that could not finish; it
// completes "the secret ...".
func commitScanAsk(ctx context.Context, err error) string {
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return scanTimedOut + "; retry, or commit fewer files"
	case commitscan.IsAsk(err):
		return "scan cannot vouch for this commit: " + err.Error()
	case errors.Is(err, errScanTooLarge):
		return fmt.Sprintf("scan stopped: what this commit records is larger than the %d MiB scan limit; commit fewer files", maxScanBytes>>20)
	}
	return "scan could not read what this commit records; check the repository state or confirm"
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
