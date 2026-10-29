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
	"time"

	"github.com/tentaqles/tentaqles/internal/commitmsg"
	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
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
//
// deadline ends every scan; the hook computes it at entry (start plus the
// scan budget), so the identity lookups that ran before count against it.
func policyDecision(cwd string, ws *resolve.Workspace, cfg *registry.Config, call policy.ToolCall, deadline time.Time) policy.Decision {
	eff := effectiveGuard(ws)
	// Pack guards (spec C §2.10) run first so a guard that handles the call
	// can supersede core matches; their verdicts merge in below.
	penv := hookPackEnv(cwd, ws, deadline)
	outcomes := packs.EvaluateGuards(call, penv, eff.Off, eff.ActionFor)
	superseded := map[string]bool{}
	for _, o := range outcomes {
		for _, id := range o.Supersedes {
			superseded[id] = true
		}
	}
	var builtin []policy.Rule
	for _, r := range policy.ForPacks(policy.Builtin(), penv.Config.IsEnabled) {
		if !superseded[r.ID] {
			builtin = append(builtin, r)
		}
	}
	layers := policy.Layers{Builtin: builtin, Global: eff.Global, Disable: eff.Disable, Actions: eff.Actions}
	if ws != nil {
		layers.Manifest = ws.Manifest.Guard.Rules
		for i := range layers.Manifest {
			layers.Manifest[i].Source = ws.ManifestPath
		}
	}
	layers.Project, _ = loadProjectRules(cwd, ws)
	set, _ := policy.Build(layers)
	d := set.Evaluate(call)

	// One budget for every scan in this invocation (commit and push rules).
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var commitRun commitscan.Runner
	if call.IsShell() && commitscan.PlanFor(call.Command).Commits {
		commitRun = commitRunner(ctx)
	}
	if commitRun != nil && !eff.Off("tq/commit-secret") {
		hits, err := commitscan.ScanCtx(ctx, cwd, call.Command, commitRun)
		switch {
		case len(hits) > 0:
			d = policy.Merge(d, ruleDecision(eff.ActionFor("tq/commit-secret", policy.Deny), "tq/commit-secret", commitReason(hits)))
		case err != nil:
			d = policy.Merge(d, ruleDecision(policy.Ask, "tq/commit-secret", "the secret "+commitScanAsk(ctx, err)))
		}
	}
	for _, r := range []struct {
		id string
		fn func(policy.Action) (policy.Decision, bool)
	}{
		{"tq/commit-pii", func(a policy.Action) (policy.Decision, bool) {
			return commitPIIDecision(ctx, ws, call, cwd, commitRun, a)
		}},
		{"tq/push-pii", func(a policy.Action) (policy.Decision, bool) { return pushPIIDecision(ctx, ws, call, cwd, a) }},
		{"tq/text-pii", func(a policy.Action) (policy.Decision, bool) { return textPIIDecision(ws, call, cwd, a) }},
		{"tq/pii-fixture-dump", func(a policy.Action) (policy.Decision, bool) { return piiFixtureDumpDecision(ws, call, cwd, a) }},
	} {
		if eff.Off(r.id) || superseded[r.id] {
			continue
		}
		if pd, ok := r.fn(eff.ActionFor(r.id, policy.Deny)); ok {
			d = policy.Merge(d, pd)
		}
	}
	if call.IsShell() && !eff.Off("tq/commit-message") {
		if r := commitmsg.Check(call.Command, cwd); r.Checked && !r.OK {
			d = policy.Merge(d, ruleDecision(eff.ActionFor("tq/commit-message", policy.Ask), "tq/commit-message", "commit message: "+r.Why))
		}
	}
	if call.IsMCP() && !eff.Off("tq/cross-client-mcp") {
		if other := mcpOwner(ws, cfg, call.MCPServer()); other != "" {
			d = policy.Merge(d, ruleDecision(eff.ActionFor("tq/cross-client-mcp", policy.Deny), "tq/cross-client-mcp",
				fmt.Sprintf("MCP server %q belongs to client %q, not to this workspace", call.MCPServer(), other)))
		}
	}
	// The state directory, the hub answer and the pack config, checked at hook
	// time against the real paths (never-disable).
	// A guard that handled and allowed the call may name the StateDir files it
	// verified and the command reads (a stored plan): only those exact paths,
	// for this call only, are not counted by tq/hub-store-write.
	var readArgs []string
	for _, o := range outcomes {
		if o.Handled && o.Action == policy.Allow && !o.Failed {
			readArgs = append(readArgs, o.ReadArgs...)
		}
	}
	d = policy.Merge(d, stateDirDecision(call, cwd, readArgs...))
	return applyPackOutcomes(d, ws, outcomes)
}

// applyPackOutcomes merges the pack guards' verdicts (strongest wins), then,
// unless the call is denied, appends the `consumed` ledger line of every
// single-use approval a guard allowed the call with. A failed append denies.
func applyPackOutcomes(d policy.Decision, ws *resolve.Workspace, outcomes []packs.GuardOutcome) policy.Decision {
	for _, o := range outcomes {
		if o.Action != policy.Allow {
			d = policy.Merge(d, policy.Decision{Action: o.Action, Matched: []*policy.Rule{{ID: o.ID, Action: o.Action, Reason: o.Reason, Source: "pack " + o.Pack}}})
		}
	}
	if d.Action != policy.Deny {
		for _, o := range outcomes {
			for _, id := range o.Consume {
				err := errors.New("no trusted workspace")
				if ws != nil {
					// Claim atomically first: exactly one concurrent hook wins the
					// approval; the others are denied.
					err = ledger.Claim(ws.Name, o.Pack, id)
					if err == nil {
						err = ledger.Append(ws.Name, ledger.Entry{Event: ledger.Consumed, Decision: id, Pack: o.Pack})
					}
					if err == nil {
						// The keychain high-water mark: deleting the claim file or
						// re-appending compacted lines cannot reopen this approval.
						err = ledger.SetHighWater(ws.Name, o.Pack, id)
					}
					if err == nil && d.Action == policy.Ask {
						// The hook cannot know whether an asked call runs, so the
						// approval is consumed now (conservative); the prompt says so.
						d = policy.Merge(d, policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: o.ID, Action: policy.Ask, Source: "pack " + o.Pack,
							Reason: "this call uses the single-use approval " + id + "; it is consumed now, even if you decline"}}})
					}
				}
				if err != nil {
					d = policy.Merge(d, policy.Decision{Action: policy.Deny, Matched: []*policy.Rule{{ID: o.ID, Action: policy.Deny, Source: "pack " + o.Pack,
						Reason: "the approval " + id + " could not be marked used (" + err.Error() + "); denied so it cannot be used twice"}}})
				}
			}
		}
	}
	if ws != nil {
		packs.SetEventWorkspace(ws.Name)
	}
	for _, o := range outcomes {
		for _, c := range o.Counters {
			packs.Count(c.Pack, c.Name, c.N)
		}
	}
	return d
}

// hookPackEnv is what pack guards see in the hook. With no trusted workspace
// there is no pack config at all (the repo layer is not read, no pack guard
// runs). Otherwise the manifest plus the repo's tq-packs.yaml, with the repo
// root found by walking up to a .git entry (no git process); the repo file
// is read under the hook deadline, and an invalid or slow one leaves the
// manifest layer alone.
func hookPackEnv(cwd string, ws *resolve.Workspace, deadline time.Time) packs.GuardEnv {
	env := packs.GuardEnv{Config: packs.Merge(nil, nil), Cwd: cwd}
	if ws == nil || ws.Manifest == nil {
		return env
	}
	env.Manifest, env.Workspace, env.WorkspaceRoot = ws.Manifest, ws.Name, ws.Root
	stop := filepath.Clean(ws.Root)
	dir := filepath.Clean(cwd)
	for i := 0; i < 32 && dir != ""; i++ {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			env.RepoRoot = dir
			break
		}
		if strings.EqualFold(dir, stop) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	root := env.RepoRoot
	if root == "" {
		root = env.WorkspaceRoot
	}
	manifestOnly := packs.Merge(ws.Manifest.Packs, nil)
	type res struct {
		cfg packs.Config
		err error
	}
	done := make(chan res, 1)
	go func() {
		cfg, err := packs.LoadConfig(ws.Manifest, root)
		done <- res{cfg, err}
	}()
	wait := time.Until(deadline)
	if wait < 50*time.Millisecond {
		wait = 50 * time.Millisecond
	}
	select {
	case r := <-done:
		if r.err != nil || r.cfg.Err() != nil {
			env.Config = manifestOnly
		} else {
			env.Config = r.cfg
		}
	case <-time.After(wait):
		env.Config = manifestOnly
	}
	return env
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
