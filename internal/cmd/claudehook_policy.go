package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/protect"
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
		Pattern      string `json:"pattern"`
		Content      string `json:"content"`
		NewString    string `json:"new_string"`
		NewSource    string `json:"new_source"`
		URL          string `json:"url"`
		Edits        []struct {
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &in)
	}
	call.Command = strings.TrimSpace(in.Command)
	call.URL = strings.TrimSpace(in.URL)
	switch {
	case in.FilePath != "":
		call.Path = in.FilePath
	case in.NotebookPath != "":
		call.Path = in.NotebookPath
	case in.Path != "":
		call.Path = in.Path
	case p.ToolName == "Glob" && in.Pattern != "":
		// The Glob tool locates files by its pattern; it is checked like
		// Grep (a read of what it would list).
		call.Path = in.Pattern
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

// knownTools are the tool names the hook judges by their fields. A tool
// outside this list and outside mcp__* is judged by what it carries: one
// with a command or path field is denied (tq cannot tell what it runs or
// touches: a renamed or lower-case "bash" is not a pass), one without is
// allowed (WebFetch-like tools tq has no rule for).
var knownTools = map[string]bool{
	"Bash": true, "PowerShell": true, "Read": true, "Write": true, "Edit": true, "MultiEdit": true,
	"NotebookEdit": true, "NotebookRead": true, "Grep": true, "Glob": true, "LS": true, "Agent": true,
	"Task": true, "apply_patch": true, "WebFetch": true, "WebSearch": true, "TodoWrite": true,
	"BashOutput": true, "KillShell": true, "ExitPlanMode": true, "Skill": true, "SlashCommand": true,
}

// commandOrPathFields are the tool_input keys that name what a call runs or
// touches.
var commandOrPathFields = []string{"command", "file_path", "notebook_path", "path", "pattern"}

// payloadFieldError is why the hook cannot judge p from its fields, or ""
// (PRD v2 §6.3, fail closed; ADR-0006): a shell tool whose command is
// missing, empty or not a string; a field that names a path or a command
// but is not a string; a Read with no path; an unknown tool that carries a
// command or a path.
func payloadFieldError(p hookPayload) string {
	if strings.HasPrefix(p.ToolName, "mcp__") {
		return ""
	}
	raw := unwrapToolInput(p.ToolInput)
	var in map[string]json.RawMessage
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "tool_input is not an object"
		}
	}
	str := func(k string) (string, bool, bool) { // value, present, isString
		v, ok := in[k]
		if !ok {
			return "", false, false
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "", true, false
		}
		return s, true, true
	}
	if !knownTools[p.ToolName] {
		for _, k := range commandOrPathFields {
			if _, present, _ := str(k); present {
				return fmt.Sprintf("the tool %q is not one tq knows and its input carries %q", p.ToolName, k)
			}
		}
		return ""
	}
	for _, k := range commandOrPathFields {
		if _, present, isStr := str(k); present && !isStr {
			return fmt.Sprintf("tool_input.%s of %s is not a string", k, p.ToolName)
		}
	}
	switch p.ToolName {
	case "Bash", "PowerShell":
		if c, _, _ := str("command"); strings.TrimSpace(c) == "" {
			return p.ToolName + " has no command"
		}
	case "Read", "NotebookRead":
		f, _, _ := str("file_path")
		n, _, _ := str("notebook_path")
		if strings.TrimSpace(f) == "" && strings.TrimSpace(n) == "" {
			return p.ToolName + " names no file"
		}
	}
	return ""
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

// resolveCwd maps cwd to its workspace (Result.Workspace is the trusted one,
// nil for a neutral or untrusted cwd: the built-in rules still apply). A
// registry tq cannot read is the error, and the hook denies on it.
func resolveCwd(cwd string) (resolve.Result, *registry.Config, error) {
	cfg, err := registry.Load()
	if err != nil {
		return resolve.Result{}, nil, err
	}
	return resolve.Resolve(cwd, cfg), cfg, nil
}

// unreadableReason words the fail-closed deny of a cwd that names a
// workspace whose manifest tq cannot read (PRD v2 §6.3, "missing manifest").
func unreadableReason(res resolve.Result) string {
	name := res.Name
	if name == "" {
		name = "this folder"
	}
	return fmt.Sprintf("the manifest of identity %s cannot be read (%s): tq cannot tell what this identity may touch, so it denies every call here until a human fixes the manifest", name, res.Reason)
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

// policyDecision evaluates, in the order of PRD v2 §6.3 after the identity
// guard: the secret rules plus the two checks that need I/O (the commit
// secret scan and the cross-client MCP check), then, when nothing denied,
// the self-protection rules (policy.SelfProtection) on the protected path
// set of this run.
//
// deadline ends the scan; the hook computes it at entry (start plus the
// scan budget), so the identity lookups that ran before count against it.
func policyDecision(cwd string, ws *resolve.Workspace, cfg *registry.Config, call policy.ToolCall, deadline time.Time) policy.Decision {
	return policyDecisionWith(buildPolicy(), protectedSet(ws, cfg), cwd, ws, cfg, call, deadline)
}

// policyDecisionWith is policyDecision with the rule set and the protected
// path set already built (a patch judges many files with one of each).
func policyDecisionWith(set *policy.Set, prot *protect.Set, cwd string, ws *resolve.Workspace, cfg *registry.Config, call policy.ToolCall, deadline time.Time) policy.Decision {
	// The identity category's v2 rules (guard: v2, ADR-0006) come before secrets (§6.3);
	// the manifest's blocked commands and the v1 identity rules ran in guard.Decide.
	if d := hookScope(ws, cfg, prot).Evaluate(cwd, call); d.Action == policy.Deny {
		return d
	}
	d := policyRules(set, cwd, ws, cfg, call, deadline)
	if d.Action != policy.Deny {
		// Self-protection comes last (§6.3); the first category that
		// denies is the one reported.
		d = policy.Merge(d, policy.SelfProtection(prot, cwd, call))
	}
	return d
}

// policyRules is the secrets category plus the cross-client MCP check.
func policyRules(set *policy.Set, cwd string, ws *resolve.Workspace, cfg *registry.Config, call policy.ToolCall, deadline time.Time) policy.Decision {
	d := set.Evaluate(call)
	if call.IsShell() && commitscan.PlanFor(call.Command).Commits {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		hits, err := commitscan.ScanCtx(ctx, cwd, call.Command, commitRunner(ctx))
		switch {
		case len(hits) > 0:
			d = policy.Merge(d, ruleDecision(policy.Deny, "tq/commit-secret", commitReason(hits)))
		case err != nil:
			// A scan that could not vouch for the commit denies it (PRD v2
			// §6.3: no ask; the owner's decision of 2026-11-05).
			d = policy.Merge(d, ruleDecision(policy.Deny, "tq/commit-secret", "the secret "+commitScanIncomplete(ctx, err)))
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

// ruleCategories are the categories of the hook's own rules (PRD v2 §7.1):
// the commit scan is secrets, the cross-client MCP check is identity (a
// server outside the identity's scope); tq/guard-error has none.
var ruleCategories = map[string]string{
	"tq/commit-secret":    policy.CategorySecrets,
	"tq/cross-client-mcp": policy.CategoryIdentity,
}

func ruleDecision(a policy.Action, id, reason string) policy.Decision {
	return policy.Decision{Action: a, Matched: []*policy.Rule{{ID: id, Category: ruleCategories[id], Action: a, Reason: reason, Source: "builtin"}}}
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

// commitScanIncomplete words the deny of a commit whose secret scan could
// not finish; it completes "the secret ...". tq denies what it could not
// scan: the agent narrows the commit, or the developer commits it from
// their own terminal.
func commitScanIncomplete(ctx context.Context, err error) string {
	const tail = "; tq denies a commit it could not scan (a developer can commit it from their own terminal)"
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return scanTimedOut + "; retry, or commit fewer files" + tail
	case commitscan.IsIncomplete(err):
		return "scan cannot vouch for this commit: " + err.Error() + tail
	case errors.Is(err, errScanTooLarge):
		return fmt.Sprintf("scan stopped: what this commit records is larger than the %d MiB scan limit; commit fewer files", maxScanBytes>>20) + tail
	}
	return "scan could not read what this commit records; check the repository state" + tail
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
