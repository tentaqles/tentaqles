package guardcfg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// grantTable reads a grant as a ceiling: its table (relaxed with no table:
// the relaxed preset; an unknown mode: default) and its rule overrides.
// Unknown values read as rule and locked entries are dropped, so a damaged
// grant only tightens. nil is the default ceiling.
func grantTable(g *Grant) (Table, map[string]Value) {
	t, _ := Preset(ModeDefault)
	ov := map[string]Value{}
	if g == nil {
		return t, ov
	}
	switch {
	case g.Mode == ModeRelaxed && len(g.Table) == 0:
		t, _ = Preset(ModeRelaxed)
	case g.Mode == ModeRelaxed || g.Mode == ModeCustom:
		for k, v := range g.Table {
			c, ok := ParseCategory(k)
			if !ok || Locked(c) {
				continue
			}
			t[c] = Value(v).norm()
		}
	}
	if g.Mode == ModeRelaxed || g.Mode == ModeCustom {
		for id, v := range g.Overrides {
			if Locked(Categories[id]) {
				continue
			}
			ov[id] = Value(v).norm()
		}
	}
	return t, ov
}

// scopeStates returns what the trusted layers ask for in each part of the
// workspace: the root and every guard.projects folder (no launch, no repo).
func scopeStates(m *manifest.Manifest, ws, root string, g File) []modeState {
	out := []modeState{requested(m, ws, root, root, "", g, "")}
	if m == nil {
		return out
	}
	gv := m.Guard.ModeView()
	dirs := make([]string, 0, len(gv.Projects))
	for d := range gv.Projects {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		dir := filepath.Join(root, filepath.FromSlash(manifest.CleanProject(d)))
		out = append(out, requested(m, ws, root, dir, "", g, ""))
	}
	return out
}

// GrantFor builds the grant `tq allow --mode <ws> <mode>` stores: for
// relaxed the relaxed preset; for custom the loosest of what the workspace
// root and each guard.projects folder resolve to, so the human grants
// exactly the tables tq mode shows. ok is false for any other mode.
func GrantFor(m *manifest.Manifest, ws, root string, g File, mode string) (Grant, bool) {
	switch mode {
	case ModeRelaxed:
		t, _ := Preset(ModeRelaxed)
		return Grant{Mode: ModeRelaxed, Table: tableStrings(t), Workspace: ws}, true
	case ModeCustom:
	default:
		return Grant{}, false
	}
	states := scopeStates(m, ws, root, g)
	t := Table{}
	for _, c := range allCategories {
		if Locked(c) {
			continue
		}
		v := ValueDeny
		for _, s := range states {
			if x := s.table.Get(c).norm(); x.Looser(v) {
				v = x
			}
		}
		t[c] = v
	}
	ids := map[string]bool{}
	for _, s := range states {
		for id := range s.overrides {
			ids[id] = true
		}
	}
	ov := map[string]string{}
	for id := range ids {
		v := ValueDeny
		for _, s := range states {
			if x := valueFor(s.table, s.overrides, id); x.Looser(v) {
				v = x
			}
		}
		ov[id] = string(v)
	}
	return Grant{Mode: ModeCustom, Table: tableStrings(t), Overrides: ov, Workspace: ws}, true
}

func tableStrings(t Table) map[string]string {
	out := map[string]string{}
	for c, v := range t {
		if !Locked(c) {
			out[string(c)] = string(v.norm())
		}
	}
	return out
}

// WithinGrant reports whether everything m asks for (the root and every
// project folder, no launch) is no looser than grant. A nil grant is the
// default ceiling.
func WithinGrant(m *manifest.Manifest, ws, root string, g File, grant *Grant) bool {
	ct, cov := grantTable(grant)
	for _, s := range scopeStates(m, ws, root, g) {
		for _, c := range allCategories {
			if !Locked(c) && lowersCat(c, s.table.Get(c), ct.Get(c)) {
				return false
			}
		}
		ids := map[string]bool{}
		for id := range s.overrides {
			ids[id] = true
		}
		for id := range cov {
			ids[id] = true
		}
		for id := range ids {
			if Locked(Categories[id]) {
				continue
			}
			if lowersID(id, valueFor(s.table, s.overrides, id), valueFor(ct, cov, id)) {
				return false
			}
		}
	}
	return true
}

// WorkspaceGrant loads the grant for a manifest hash (a legacy .bypass file
// reads as relaxed). Errors are no grant (fail closed).
func WorkspaceGrant(hash string) *Grant {
	if hash == "" {
		return nil
	}
	g, err := trust.LoadGrant(hash)
	if err != nil {
		return nil
	}
	return g
}

// CarryGrant moves the mode grant of oldHash to newHash after one of tq's
// own manifest writers changed the manifest (now m), but only when the new
// configuration is no looser than the grant (spec E §6.3). It returns the
// old grant and whether it was carried; (nil, false) when there was none.
func CarryGrant(oldHash, newHash string, m *manifest.Manifest, ws, root string, g File) (*Grant, bool, error) {
	old := WorkspaceGrant(oldHash)
	if old == nil {
		return nil, false, nil
	}
	if oldHash == newHash {
		return old, true, nil
	}
	if !WithinGrant(m, ws, root, g, old) {
		return old, false, nil
	}
	carried := *old
	carried.FromBypass = false
	if carried.Workspace == "" {
		carried.Workspace = ws
	}
	if old.FromBypass {
		t, _ := Preset(ModeRelaxed)
		carried.Table = tableStrings(t)
	}
	if err := trust.SaveGrant(newHash, carried); err != nil {
		return old, false, err
	}
	return old, true, nil
}

// GlobalDigest is the SHA-256 of the global file's mode keys (mode, base,
// categories, rules_override), which the global grant pins.
func GlobalDigest(f File) string {
	b, _ := json.Marshal(struct {
		Mode          string             `json:"mode"`
		Base          string             `json:"base"`
		Categories    map[Category]Value `json:"categories"`
		RulesOverride map[string]Value   `json:"rules_override"`
	}{f.Mode, f.Base, f.Categories, f.RulesOverride})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// GlobalGrant returns the global grant ($TQ_HOME/trust/global.mode) when its
// digest matches f's mode keys, else nil: an edit of guard.yaml after the
// grant drops it.
func GlobalGrant(f File) *Grant {
	g, err := trust.LoadGrant(trust.GlobalGrantKey)
	if err != nil || g == nil || g.Digest != GlobalDigest(f) {
		return nil
	}
	return g
}

// GlobalGrantFor builds the grant `tq allow --mode --global <mode>` stores.
func GlobalGrantFor(f File, mode string) (Grant, bool) {
	gr, ok := GrantFor(nil, "", "", f, mode)
	if !ok {
		return Grant{}, false
	}
	gr.Workspace = ""
	gr.Digest = GlobalDigest(f)
	return gr, true
}
