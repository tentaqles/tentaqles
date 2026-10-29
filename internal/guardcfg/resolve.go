package guardcfg

import (
	"github.com/tentaqles/tentaqles/internal/paths"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// Source names used in Resolved.Sources and ModeSource.
const (
	SourceBuiltin = "builtin"
	SourceGlobal  = "global"
	SourceRepo    = "repo"
	SourceLaunch  = "launch"
	// SourceProjectPrefix prefixes a guard.projects layer ("project:app").
	SourceProjectPrefix = "project:"
)

// modeState is the mode while the layers are applied.
type modeState struct {
	mode, base string
	table      Table
	overrides  map[string]Value
	sources    map[string]string
	modeSource string
}

// modeLayer is one trusted layer's mode keys.
type modeLayer struct {
	name       string
	mode, base string
	cats       map[Category]Value
	overrides  map[string]Value
}

func defaultState() modeState {
	t, _ := Preset(ModeDefault)
	return modeState{mode: ModeDefault, base: ModeDefault, table: t, overrides: map[string]Value{}, sources: map[string]string{}, modeSource: SourceBuiltin}
}

// apply lays l over s. A valid stored mode replaces the table (a preset, or
// for custom its base plus l's categories, never a locked one). l's rule
// overrides are merged per id; locked ids and unknown values are dropped (the
// loaders reject them; this never widens without them).
func (s *modeState) apply(l modeLayer) {
	if ValidStoredMode(l.mode) {
		base := l.mode
		if l.mode == ModeCustom {
			base = l.base
			if !ValidBase(base) {
				base = ModeDefault
			}
		}
		t, _ := Preset(base)
		if l.mode == ModeCustom {
			for c, v := range l.cats {
				if _, ok := ParseCategory(string(c)); !ok || Locked(c) {
					continue
				}
				if _, ok := ParseValue(string(v)); ok {
					t[c] = v
				}
			}
		}
		s.mode, s.base, s.table, s.modeSource = l.mode, base, t, l.name
		for _, c := range allCategories {
			s.sources[string(c)] = l.name
		}
	}
	for id, v := range l.overrides {
		if _, ok := ParseValue(string(v)); !ok || Locked(Categories[id]) || strings.TrimSpace(id) == "" {
			continue
		}
		s.overrides[id] = v
		s.sources[id] = l.name
	}
}

func catsOf(m map[string]string) map[Category]Value {
	out := map[Category]Value{}
	for k, v := range m {
		out[Category(k)] = Value(v)
	}
	return out
}

func valsOf(m map[string]string) map[string]Value {
	out := map[string]Value{}
	for k, v := range m {
		out[k] = Value(v)
	}
	return out
}

// valueFor is the value s gives rule id: its override, else its category's
// value (an unknown id: rule).
func valueFor(t Table, ov map[string]Value, id string) Value {
	if v, ok := ov[id]; ok {
		return v.norm()
	}
	if c, ok := Categories[id]; ok {
		return t.Get(c).norm()
	}
	return ValueRule
}

// layersFor returns the trusted layers 2-4 for a call: the global file, the
// workspace manifest (read through its legacy view) when cwd is inside root,
// and the deepest guard.projects entry covering cwd and the target's folder.
func layersFor(m *manifest.Manifest, ws, root, cwd, target string, g File) []modeLayer {
	ls := []modeLayer{{name: SourceGlobal, mode: g.Mode, base: g.Base, cats: g.Categories, overrides: g.RulesOverride}}
	if m == nil {
		return ls
	}
	if root != "" && !inside(cwd, root) {
		return ls // a cwd outside the workspace is a neutral folder
	}
	// A file target outside the workspace keeps the workspace layer (its
	// grant is the ceiling anyway); deepestProject drops the project layer
	// for it, as for a target outside the project.
	gv := m.Guard.ModeView()
	if ws == "" {
		ws = m.Client
	}
	ls = append(ls, modeLayer{name: ws, mode: gv.Mode, base: gv.Base, cats: catsOf(gv.Categories), overrides: valsOf(gv.RulesOverride)})
	if dir, p, ok := deepestProject(gv.Projects, root, cwd, target); ok {
		ls = append(ls, modeLayer{name: SourceProjectPrefix + dir, mode: p.Mode, base: p.Base, cats: catsOf(p.Categories), overrides: valsOf(p.RulesOverride)})
	}
	return ls
}

// deepestProject returns the guard.projects entry with the longest folder
// covering cwd and (for a file tool) the target's folder.
func deepestProject(ps map[string]manifest.GuardProject, root, cwd, target string) (string, manifest.GuardProject, bool) {
	if root == "" || cwd == "" {
		return "", manifest.GuardProject{}, false
	}
	best, bestLen := "", -1
	for raw := range ps {
		cp := manifest.CleanProject(raw)
		if cp == "" || cp == "." || cp == ".." || strings.HasPrefix(cp, "../") || path.IsAbs(cp) || filepath.IsAbs(cp) || filepath.VolumeName(cp) != "" {
			continue // defense in depth: Validate rejects these
		}
		dir := filepath.Join(root, filepath.FromSlash(cp))
		if !inside(cwd, dir) || (target != "" && !inside(targetDir(target, cwd), dir)) {
			continue
		}
		if len(cp) > bestLen || (len(cp) == bestLen && raw < best) {
			best, bestLen = raw, len(cp)
		}
	}
	if bestLen < 0 {
		return "", manifest.GuardProject{}, false
	}
	return manifest.CleanProject(best), ps[best], true
}

func targetDir(target, cwd string) string {
	abs := target
	if !filepath.IsAbs(abs) && cwd != "" {
		abs = filepath.Join(cwd, abs)
	}
	return filepath.Dir(abs)
}

func inside(p, dir string) bool {
	if p == "" || dir == "" {
		return false
	}
	return within(normDir(p), normDir(dir))
}

// requested applies layers 1-4 and the launch override (layer 6), with no
// repo file and no clamp: what the trusted configuration asks for.
func requested(m *manifest.Manifest, ws, root, cwd, target string, g File, launch string) modeState {
	s := defaultState()
	for _, l := range layersFor(m, ws, root, cwd, target, g) {
		s.apply(l)
	}
	switch {
	case launch == ModeCustom:
		// custom at launch keeps the layers' custom table; any other mode
		// becomes custom with nothing changed (= its own table).
		if s.mode != ModeCustom {
			s.base, s.mode = s.mode, ModeCustom
		}
		s.modeSource = SourceLaunch
	case ValidStoredMode(launch):
		t, _ := Preset(launch)
		s.mode, s.base, s.table, s.modeSource = launch, launch, t, SourceLaunch
		for _, c := range allCategories {
			s.sources[string(c)] = SourceLaunch
		}
	}
	return s
}

// MergeRepo folds several repo files (cwd up to the workspace root) into one
// tighten-only file: per category and per rule id the strictest value any of
// them sets. A file's mode is expanded into its table.
func MergeRepo(files ...RepoFile) RepoFile {
	out := RepoFile{Categories: map[Category]Value{}, RulesOverride: map[string]Value{}}
	for _, f := range files {
		for c, v := range repoTable(f) {
			if cur, ok := out.Categories[c]; !ok || v.rank() > cur.rank() {
				out.Categories[c] = v
			}
		}
		for id, v := range f.RulesOverride {
			if _, ok := ParseValue(string(v)); !ok {
				continue
			}
			if cur, ok := out.RulesOverride[id]; !ok || v.rank() > cur.rank() {
				out.RulesOverride[id] = v
			}
		}
	}
	return out
}

// repoTable expands a repo file's mode and categories into the values it
// asks for (unknown names and values dropped).
func repoTable(f RepoFile) map[Category]Value {
	out := map[Category]Value{}
	switch {
	case ValidBase(f.Mode):
		t, _ := Preset(f.Mode)
		for c, v := range t {
			out[c] = v
		}
	case f.Mode == ModeCustom:
		t, _ := Preset(ModeDefault)
		for c, v := range t {
			out[c] = v
		}
	}
	for c, v := range f.Categories {
		if _, ok := ParseCategory(string(c)); !ok {
			continue
		}
		if _, ok := ParseValue(string(v)); ok {
			out[c] = v
		}
	}
	return out
}

// Resolve computes the mode for a call (spec E §6.1): built-in default, the
// global file g, the workspace manifest m (named ws, rooted at root), the
// deepest guard.projects entry covering cwd (and target, for a file tool;
// "" otherwise), the per-launch override launch (TQ_MODE, "" when unset;
// "off" is handled before this), then the tighten-only repo file, then the
// ceiling grant (nil: the default ceiling). m may be nil (neutral folder).
//
// The repo file is applied after the launch override so it tightens every
// mode. Invalid values in any layer are ignored, which never loosens.
func Resolve(m *manifest.Manifest, ws, root, cwd, target string, g File, repo RepoFile, launch string, grant *Grant) Resolved {
	s := requested(m, ws, root, cwd, target, g, launch)
	req, reqBase, reqSource := s.mode, s.base, s.modeSource
	res := Resolved{Requested: req, ModeSource: reqSource, Ceiling: grant}
	if req == ModeStrict {
		// Strict pins its categories against a looser rules_override
		// (spec E §4.4); Effective does the same for custom rules.
		pin, _ := Preset(ModeStrict)
		for id, v := range s.overrides {
			if c, ok := Categories[id]; ok && pin.Get(c) != ValueRule {
				s.overrides[id] = stricterValue(v, pin.Get(c))
			}
		}
	}

	// Layer 5: the repo file only tightens.
	for c, v := range repoTable(repo) {
		cur := s.table.Get(c).norm()
		switch {
		case Locked(c):
			if v.norm() != ValueRule {
				res.Ignored = append(res.Ignored, string(c))
			}
		case v.rank() > cur.rank():
			s.table[c] = v
			s.sources[string(c)] = SourceRepo
		case v.rank() < cur.rank():
			res.Ignored = append(res.Ignored, string(c))
		}
	}
	for id, v := range repo.RulesOverride {
		if _, ok := ParseValue(string(v)); !ok {
			continue
		}
		cur := valueFor(s.table, s.overrides, id)
		switch {
		case Locked(Categories[id]):
			res.Ignored = append(res.Ignored, id)
		case v.rank() > cur.rank():
			s.overrides[id] = v
			s.sources[id] = SourceRepo
		case v.rank() < cur.rank():
			res.Ignored = append(res.Ignored, id)
		}
	}

	// The ceiling clamp, at table level for the views (Effective clamps
	// each rule exactly).
	ct, cov := grantTable(grant)
	for _, c := range allCategories {
		if Locked(c) {
			s.table[c] = ValueRule
			continue
		}
		v, w := s.table.Get(c).norm(), ct.Get(c).norm()
		if lowersCat(c, v, w) {
			s.table[c] = stricterValue(v, w)
			res.Clamped = append(res.Clamped, string(c))
		} else {
			s.table[c] = v
		}
	}
	for id, v := range s.overrides {
		c, known := Categories[id]
		if !known {
			continue // a custom rule's category is only known per rule: Effective clamps it
		}
		w := ct.Get(c).norm()
		if gv, ok := cov[id]; ok {
			w = gv
		}
		if lowersID(id, v, w) {
			s.overrides[id] = stricterValue(v, w)
			res.Clamped = append(res.Clamped, id)
		}
	}
	sort.Strings(res.Clamped)
	sort.Strings(res.Ignored)

	res.Table, res.Overrides, res.Sources = s.table, s.overrides, s.sources
	res.Mode, res.Base = nameMode(req, reqBase, s.table)
	return res
}

// nameMode names the table the clamp left: the requested preset when it is
// unchanged, else the preset it equals (default first), else custom on the
// requested base.
func nameMode(req, base string, t Table) (string, string) {
	if ValidBase(req) && equalTables(t, req) {
		return req, req
	}
	if req == ModeCustom && !equalTables(t, ModeDefault) {
		return ModeCustom, base
	}
	for _, p := range []string{ModeDefault, ModeStrict, ModeRelaxed} {
		if equalTables(t, p) {
			return p, p
		}
	}
	if !ValidBase(base) {
		base = ModeDefault
	}
	return ModeCustom, base
}

func equalTables(t Table, preset string) bool {
	p, ok := Preset(preset)
	if !ok {
		return false
	}
	for _, c := range allCategories {
		if t.Get(c).norm() != p.Get(c) {
			return false
		}
	}
	return true
}

func normDir(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	p = evalExisting(filepath.Clean(p))
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return strings.TrimSuffix(p, "/")
}

func within(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// evalExisting resolves symlinks in p's longest existing prefix and keeps
// the missing tail (paths.Resolve, shared with the packs).
func evalExisting(p string) string { return paths.Resolve(p) }
