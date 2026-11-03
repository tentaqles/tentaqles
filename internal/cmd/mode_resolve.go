package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/statusline"
	"github.com/tentaqles/tentaqles/internal/trust"
	"gopkg.in/yaml.v3"
)

// Guard-mode resolution for the commands that still read modes (tq mode,
// tq run, tq env, tq settings). The hook does not read modes any more: it
// applies the identity guard and the secret rules whatever the mode. This
// file goes with the modes.

// ProjectRulesFile is the per-repo rule file. Only its mode keys are read,
// by the commands above; the hook never reads it.
const ProjectRulesFile = ".claude/tq-rules.yaml"

// envOff reports whether tq is off for this process: TQ_MODE is exactly off
// (guardcfg.IsOff, the one matcher every tq part uses). TQ_ENABLED=0 is not
// off here: it only skips shell activation.
func envOff() bool { return guardcfg.IsOff(os.Getenv(guardcfg.EnvMode)) }

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
// up to the workspace root and folds them into one tighten-only file.
// Unparsable files are skipped (they can only tighten).
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

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// modeSourceLabel names the layer that set the mode for a person; "" for the
// built-in default.
func modeSourceLabel(src string, ws *resolve.Workspace) string {
	switch {
	case src == "" || src == "builtin":
		return ""
	case src == "global":
		return "global"
	case src == "launch":
		return "launch override"
	case src == "repo":
		return "repo file"
	case strings.HasPrefix(src, "project:"):
		return "project " + strings.TrimPrefix(src, "project:")
	case ws != nil && src == ws.Name:
		return "workspace"
	}
	return src
}

// codexAskIDs lists the known rule ids whose effective action under res is
// ask. Codex hooks cannot ask, so the Codex adapter turns these into blocks.
func codexAskIDs(res guardcfg.Resolved, m *manifest.Manifest) []string {
	seen := map[string]bool{}
	var out []string
	for _, ri := range guardcfg.Known(m, guardcfg.File{}) {
		r := policy.Rule{ID: ri.ID, Action: ri.Action}
		if !seen[ri.ID] && res.Effective(&r) == policy.Ask {
			seen[ri.ID] = true
			out = append(out, ri.ID)
		}
	}
	sort.Strings(out)
	return out
}

// statuslineModeFacts fills the status line's guard segment: "off" under
// TQ_MODE=off, else the resolved mode and the 24h skip count. It reads local
// files only, like the rest of the status line.
func statuslineModeFacts(f *statusline.Facts, ws *resolve.Workspace, cwd string) {
	if guardcfg.IsOff(launchMode()) {
		f.Mode = statusline.ModeOff
		return
	}
	res := resolveMode(ws, cwd, "")
	f.Mode = res.Mode
	if f.Mode == "" {
		f.Mode = guardcfg.ModeDefault
	}
	f.Looser = res.LooserThanDefault()
	if ws != nil {
		f.Skipped = statusline.Skipped(ws.Name, time.Now())
	}
}
