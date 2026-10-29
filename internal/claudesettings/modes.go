package claudesettings

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

// ghRepoDeleteEntry is the baseline deny that a loosened git-history moves to
// ask (spec E §8.1): a settings deny blocks in every Claude mode, so it would
// beat relaxed's confirm for tq/gh-repo-delete.
const ghRepoDeleteEntry = "Bash(gh repo delete:*)"

const ghRepoDeleteID = "tq/gh-repo-delete"

// gitHistoryLoosened reports whether any layer (the global guard.yaml, the
// workspace or one of its projects) puts tq/gh-repo-delete below deny:
// relaxed, a custom table with git-history ask or skip, or a rules_override.
// The legacy guard.unattended block reads as relaxed. Whether the workspace
// has the grant does not matter here: without it the hook clamps the rule
// back to deny, and the hook runs before the settings. A global file that
// cannot be loaded applies nothing (the deny stays). The launch layer
// (--tq-mode, TQ_MODE) is per session and cannot be rendered: a relaxed
// launch in a workspace that is not relaxed keeps the settings deny.
func gitHistoryLoosened(g manifest.Guard) bool {
	if g.Unattended.Configured() {
		return true
	}
	if gf, err := guardcfg.Load(); err == nil {
		cats := map[string]string{}
		for k, v := range gf.Categories {
			cats[string(k)] = string(v)
		}
		ovr := map[string]string{}
		for k, v := range gf.RulesOverride {
			ovr[k] = string(v)
		}
		if layerLoosens(gf.Mode, gf.Base, cats, ovr) {
			return true
		}
	}
	if layerLoosens(g.Mode, g.Base, g.Categories, g.RulesOverride) {
		return true
	}
	for _, p := range g.Projects {
		if layerLoosens(p.Mode, p.Base, p.Categories, p.RulesOverride) {
			return true
		}
	}
	return false
}

func layerLoosens(mode, base string, cats, overrides map[string]string) bool {
	if v, ok := overrides[ghRepoDeleteID]; ok {
		return below(strings.TrimSpace(v))
	}
	v := guardcfg.ValueRule
	switch mode = strings.TrimSpace(mode); mode {
	case guardcfg.ModeCustom:
		if base == "" {
			base = guardcfg.ModeDefault
		}
		if t, ok := guardcfg.Preset(base); ok {
			v = t.Get(guardcfg.CatGitHistory)
		}
		if c, ok := cats[string(guardcfg.CatGitHistory)]; ok {
			v = guardcfg.Value(strings.TrimSpace(c))
		}
	default:
		if t, ok := guardcfg.Preset(mode); ok {
			v = t.Get(guardcfg.CatGitHistory)
		}
	}
	return below(string(v))
}

// below reports a value that takes tq/gh-repo-delete (a deny) below deny.
func below(v string) bool { return v == string(guardcfg.ValueAsk) || v == string(guardcfg.ValueSkip) }
