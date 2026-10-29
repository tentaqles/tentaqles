package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
	"gopkg.in/yaml.v3"
)

// The guard mode of a tool call (spec E §6, §11.2): the mode gate maps each
// matching rule to its effective action (guardcfg.Resolved.Effective) and
// keeps the rules it skipped or lowered for the audit. It replaces the
// unattended gate.

// launchMode returns the per-launch override (TQ_MODE in tq's own env, never
// from a tool call), "" when unset.
func launchMode() string { return os.Getenv(guardcfg.EnvMode) }

// resolveMode resolves the guard mode for ws at cwd; target is a file tool's
// absolute path ("" otherwise). ws nil is a neutral folder.
func resolveMode(ws *resolve.Workspace, cwd, target string) guardcfg.Resolved {
	return resolveModeFor(ws, cwd, target, launchMode())
}

// resolveModeFor gathers the layers (the global guard.yaml, the trusted
// manifest, the repo files, the launch override) and the ceiling (the
// workspace's grant, in a neutral folder the global grant, in an untrusted
// workspace none) and resolves
// them. An unreadable guard.yaml or grant counts as absent, which never
// loosens: the default ceiling still applies.
func resolveModeFor(ws *resolve.Workspace, cwd, target, launch string) guardcfg.Resolved {
	g, err := guardcfg.Load()
	if err != nil {
		g = guardcfg.File{}
	}
	var m *manifest.Manifest
	var grant *guardcfg.Grant
	name, root := "", ""
	switch {
	case ws != nil && ws.Manifest != nil && trust.IsTrusted(ws.Hash):
		m, name, root = ws.Manifest, ws.Name, ws.Root
		grant = guardcfg.WorkspaceGrant(ws.Hash, ws.Name, ws.Root)
		if grant != nil && grant.FromBypass {
			// Name the legacy grant once so a re-trust after an edit can
			// report it dropped (tq allow). Best effort: it reads the same.
			_ = trust.AdoptBypass(ws.Hash, ws.Name, ws.Root)
		}
	case ws != nil && ws.Manifest != nil:
		// An untrusted workspace (an edited manifest, a new client folder)
		// has the default ceiling: the global grant is only for neutral
		// folders (spec E §4.2, §6.3), so editing the manifest never
		// reaches a global relaxed. The global layer can still tighten.
		ws = nil
	default:
		ws = nil
		// The global grant is for folders outside every workspace only. A
		// folder under a base whose manifest is missing, invalid or
		// untrusted has the default ceiling, so breaking or deleting the
		// manifest never reaches a global relaxed (final review M1).
		if neutralFolder(cwd) && (target == "" || neutralFolder(filepath.Dir(target))) {
			grant = guardcfg.GlobalGrant(g)
		}
	}
	if launch == "" && os.Getenv("TENTAQLES_HEADLESS") == "1" {
		// A headless run with no explicit mode gets the workspace's
		// headless_mode (default when absent), never its interactive mode.
		launch = guardcfg.ModeDefault
		if m != nil && m.Guard.HeadlessMode != "" {
			launch = m.Guard.HeadlessMode
		}
	}
	return guardcfg.Resolve(m, name, root, cwd, target, g, loadRepoMode(cwd, ws), launch, grant)
}

// neutralFolder reports whether dir is outside every workspace: outside
// every registered base, or a base folder itself. An unreadable registry is
// not neutral (fail closed: the default ceiling).
func neutralFolder(dir string) bool {
	if dir == "" {
		return false
	}
	cfg, err := registry.Load()
	if err != nil {
		return false
	}
	switch resolve.Resolve(dir, cfg).Reason {
	case "outside any base", "at base root":
		return true
	}
	return false
}

// loadRepoMode reads the mode keys of every .claude/tq-rules.yaml from cwd
// up to the workspace root (as loadProjectRules does) and folds them into one
// tighten-only file. Unparsable files are skipped (they can only tighten).
func loadRepoMode(cwd string, ws *resolve.Workspace) guardcfg.RepoFile {
	var files []guardcfg.RepoFile
	stop := ""
	if ws != nil {
		stop = strings.ToLower(filepath.Clean(ws.Root))
	}
	dir := filepath.Clean(cwd)
	for i := 0; i < 8 && dir != "" && dir != "."; i++ {
		p := filepath.Join(dir, filepath.FromSlash(ProjectRulesFile))
		if raw, err := os.ReadFile(p); err == nil {
			var f guardcfg.RepoFile
			if yaml.Unmarshal(raw, &f) == nil {
				files = append(files, f)
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
	return guardcfg.MergeRepo(files...)
}

// modeGate applies the resolved mode to one tool call and keeps the rules it
// changed for the audit. A nil gate changes nothing.
type modeGate struct {
	ws      *resolve.Workspace
	res     guardcfg.Resolved
	changed []policy.Mapped
}

// modeGateFor returns the gate for call in ws at cwd under the launch
// override launch; nil when everything resolves to default with no
// overrides (the fast path). A file tool's target decides the project layer
// with the cwd: an edit outside the project falls back to the workspace.
func modeGateFor(ws *resolve.Workspace, cwd string, call policy.ToolCall, launch string) *modeGate {
	target := ""
	if call.Path != "" {
		target = call.Path
		if !filepath.IsAbs(target) && cwd != "" {
			target = filepath.Join(cwd, target)
		}
	}
	res := resolveModeFor(ws, cwd, target, launch)
	if res.IsDefault() {
		return nil
	}
	if ws != nil && (ws.Manifest == nil || !trust.IsTrusted(ws.Hash)) {
		ws = nil
	}
	return &modeGate{ws: ws, res: res}
}

// fresh returns a gate with g's resolved mode and no recorded changes, so
// one resolution serves several calls with their own audit (nil stays nil).
func (g *modeGate) fresh() *modeGate {
	if g == nil {
		return nil
	}
	return &modeGate{ws: g.ws, res: g.res}
}

// resolved returns the gate's mode, DefaultResolved for a nil gate.
func (g *modeGate) resolved() guardcfg.Resolved {
	if g == nil {
		return guardcfg.DefaultResolved()
	}
	return g.res
}

// mapFunc is the mapper for policy.Set.EvaluateMapped and policy.MapDecision:
// Resolved.Effective. nil for a nil gate.
func (g *modeGate) mapFunc() func(*policy.Rule) policy.Action {
	if g == nil {
		return nil
	}
	return g.res.Effective
}

// note keeps changed rules for the audit.
func (g *modeGate) note(ms []policy.Mapped) {
	if g != nil {
		g.changed = append(g.changed, ms...)
	}
}

// apply maps a one-source decision (commit scan, PII, commit message,
// cross-client MCP, Jev, a pack guard) and notes what changed. A nil gate
// returns d.
func (g *modeGate) apply(d policy.Decision) policy.Decision {
	if g == nil {
		return d
	}
	out, changed := policy.MapDecision(d, g.mapFunc())
	g.note(changed)
	return out
}

// audit records the skipped or lowered rules that changed the outcome (their
// own action is stricter than final, the decision the session gets) and
// returns them, denies first, then by id. A rule the mode made stricter, or
// one no stricter than final, changed nothing and is not recorded.
func (g *modeGate) audit(claudeMode string, call policy.ToolCall, final policy.Decision) []policy.Mapped {
	if g == nil {
		return nil
	}
	var out []policy.Mapped
	seen := map[string]bool{}
	action := policy.Allow
	for _, m := range g.changed {
		own := m.Rule.Action
		key := m.Rule.ID + "\x00" + string(own)
		if !own.StricterThan(final.Action) || !own.StricterThan(m.To) || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
		if own.StricterThan(action) {
			action = own
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rule.Action != out[j].Rule.Action {
			return out[i].Rule.Action.StricterThan(out[j].Rule.Action)
		}
		return out[i].Rule.ID < out[j].Rule.ID
	})
	ids := make([]string, 0, len(out))
	named := map[string]bool{}
	for _, m := range out {
		if !named[m.Rule.ID] {
			named[m.Rule.ID] = true
			ids = append(ids, m.Rule.ID)
		}
	}
	subject := call.Command
	if subject == "" {
		subject = call.Path
	}
	if subject == "" {
		subject = call.Content // MCP/Agent input: masked and truncated by Append
	}
	wsName, scope := "", g.res.ModeSource
	var classes []string
	if g.ws != nil {
		wsName = g.ws.Name
		classes = g.ws.Manifest.Data.Classes
		if scope == g.ws.Name {
			scope = "workspace"
		}
	}
	to := string(final.Action)
	if to == "" {
		to = "allow"
	}
	guardlog.Append(guardlog.Record{WS: wsName, Scope: scope, Mode: g.res.Mode, ClaudeMode: claudeMode, To: to,
		Action: string(action), Rules: ids, Tool: call.Tool, Command: subject}, classes)
	return out
}

// skippedReason renders the rules a mode skipped or lowered: SKIPPED lines
// for the ones it let through, LOWERED lines for denies it turned into a
// confirm, each naming what the rule would have done.
func skippedReason(ms []policy.Mapped) string {
	var lines []string
	for _, m := range ms {
		was := "a confirm"
		if m.Rule.Action == policy.Deny {
			was = "a block"
		}
		verb, how := "SKIPPED", "was "+was
		if m.To != policy.Allow {
			verb, how = "LOWERED", "was "+was+", now a confirm"
		}
		lines = append(lines, fmt.Sprintf("%s [%s]: %s (%s; source: %s)", verb, m.Rule.ID, m.Rule.Reason, how, m.Rule.Source))
	}
	return strings.Join(lines, "\n")
}

// skippedCounts renders the confirms and blocks a mode skipped or lowered
// for ws in the 24 hours before now ("2 confirms and 1 block"). It reads
// only the logs' tails, so the hook path stays bounded.
func skippedCounts(ws string, now time.Time) string {
	confirms, blocks := 0, 0
	for _, r := range guardlog.Read(ws, now.Add(-24*time.Hour), 1<<20) {
		if r.IsBlock() {
			blocks++
		} else {
			confirms++
		}
	}
	return plural(confirms, "confirm") + " and " + plural(blocks, "block")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
