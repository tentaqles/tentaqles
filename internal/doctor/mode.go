package doctor

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// The guard-mode checks (spec E §11.1), replacing the unattended ones:
//
//	mode-not-granted       a mode looser than its ceiling (no grant, or a
//	                       grant tighter than what the file asks for)
//	mode-unknown-rule      rules_override (or a legacy keep_asking) names no rule
//	mode-cloud             a granted mode that skips prod-changes or
//	                       cloud-destructive in a workspace with a cloud identity
//	guard-locked-override  guard.disable or a looser guard.actions on a locked rule
func init() {
	Register(Check{Name: "mode", Workspace: modeChecks, Global: globalModeChecks})
}

func loadGuardFile() guardcfg.File {
	g, err := guardcfg.Load()
	if err != nil {
		return guardcfg.File{}
	}
	return g
}

// modeChecks reports the workspace's guard-mode problems. It reads files
// only (guard.yaml, the grant).
func modeChecks(w *resolve.Workspace, _ Deps, add AddFunc) {
	if w == nil || w.Manifest == nil {
		return
	}
	m := w.Manifest
	g := loadGuardFile()
	grant := guardcfg.WorkspaceGrant(w.Hash, w.Name, w.Root)
	if grant == nil && trust.IsBypassAllowed(w.Hash) {
		// A legacy .bypass for a manifest hash more than one workspace has
		// (a byte copy) is ignored for all of them (security re-review r2).
		if owners := guardcfg.BypassOwners(w.Hash); len(owners) > 1 {
			add("warn", "mode-bypass-shared", w.Name,
				fmt.Sprintf("the legacy bypass grant of %s is ignored: %d workspaces have the same manifest (%s)", w.Name, len(owners), strings.Join(owners, ", ")),
				fmt.Sprintf("make the copies' manifests differ (or remove the copy), then grant the one that needs it: tq allow --mode %s relaxed", w.Name))
		}
	}

	scopes := []string{w.Root}
	gv := m.Guard.ModeView()
	dirs := make([]string, 0, len(gv.Projects))
	for d := range gv.Projects {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		scopes = append(scopes, filepath.Join(w.Root, filepath.FromSlash(manifest.CleanProject(d))))
	}
	notGranted := map[string]bool{}
	var clamped []string
	cloud := false
	for _, dir := range scopes {
		res := guardcfg.Resolve(m, w.Name, w.Root, dir, "", g, guardcfg.RepoFile{}, "", grant)
		if len(res.Clamped) > 0 {
			want := res.Requested
			if want != guardcfg.ModeRelaxed {
				want = guardcfg.ModeCustom
			}
			notGranted[want] = true
			clamped = append(clamped, res.Clamped...)
		}
		for _, c := range []guardcfg.Category{guardcfg.CatProdChanges, guardcfg.CatCloudDestructive} {
			if res.Table.Get(c) == guardcfg.ValueSkip {
				cloud = true
			}
		}
	}
	if len(notGranted) > 0 {
		want := guardcfg.ModeRelaxed
		if notGranted[guardcfg.ModeCustom] {
			want = guardcfg.ModeCustom
		}
		ceiling := "no grant"
		if grant != nil {
			ceiling = "the grant is " + grant.Mode
		}
		add("warn", "mode-not-granted", w.Name, "guard mode looser than its ceiling ("+ceiling+"): "+strings.Join(uniqSortedStrings(clamped), ", ")+" run at the ceiling", "tq allow --mode "+w.Name+" "+want)
	}
	var unknown []string
	for _, id := range guardcfg.UnknownOverrides(m, g) {
		if _, global := g.RulesOverride[id]; !global {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		add("warn", "mode-unknown-rule", w.Name, "rules_override (or guard.unattended.keep_asking) names unknown rules: "+strings.Join(unknown, ", "), "fix the ids in .tentaqles.yaml (see tq guard list)")
	}
	if cloud && m.HasCloudIdentity() {
		add("warn", "mode-cloud", w.Name, "guard mode skips prod-changes or cloud-destructive with a cloud identity: Claude can change or delete cloud resources without asking in this workspace", "")
	}
	var locked []string
	for _, e := range guardcfg.Merge(g, nil, m, w.Name).LockedIgnored {
		if strings.HasSuffix(e, "@"+w.Name) {
			locked = append(locked, strings.TrimSuffix(e, "@"+w.Name))
		}
	}
	if len(locked) > 0 {
		add("warn", "guard-locked-override", w.Name, "ignored: these entries name a locked rule (identity, secrets, self-protection, privacy), which is always enforced: "+strings.Join(locked, ", "), "remove them from guard.disable / guard.actions (or decision.disable) in .tentaqles.yaml")
	}
}

// globalModeChecks reports the global guard.yaml's guard-mode problems once.
func globalModeChecks(_ Deps, add AddFunc) {
	g, err := guardcfg.Load()
	if err != nil {
		return // reported by the guard checks; nothing from it applies
	}
	if g.Mode != "" {
		res := guardcfg.Resolve(nil, "", "", "", "", g, guardcfg.RepoFile{}, "", guardcfg.GlobalGrant(g))
		if len(res.Clamped) > 0 {
			want := guardcfg.ModeRelaxed
			if res.Requested != guardcfg.ModeRelaxed {
				want = guardcfg.ModeCustom
			}
			msg := "guard.yaml mode is looser than its grant: " + strings.Join(res.Clamped, ", ") + " run at the ceiling in neutral folders"
			if gr, _ := trust.LoadGrant(trust.GlobalGrantKey); gr != nil && gr.Digest != guardcfg.GlobalDigest(g) {
				msg += " (guard.yaml changed since the grant)"
			}
			add("warn", "mode-not-granted", "", msg, "tq allow --mode --global "+want)
		}
	}
	var unknown []string
	for _, id := range guardcfg.UnknownOverrides(nil, g) {
		unknown = append(unknown, id)
	}
	if len(unknown) > 0 {
		add("warn", "mode-unknown-rule", "", "guard.yaml rules_override names unknown rules: "+strings.Join(unknown, ", "), "fix the ids in "+guardcfg.Path())
	}
	var locked []string
	for _, e := range guardcfg.Merge(g, nil, nil, "").LockedIgnored {
		locked = append(locked, strings.TrimSuffix(e, "@global"))
	}
	if len(locked) > 0 {
		add("warn", "guard-locked-override", "", "ignored: guard.yaml entries name a locked rule, which is always enforced: "+strings.Join(locked, ", "), "remove them from "+guardcfg.Path())
	}
}

func uniqSortedStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
