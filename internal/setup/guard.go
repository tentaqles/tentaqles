package setup

// Guard modes (spec E §6.3, §10): the view and the writers that `tq mode` and
// the desktop Guard tab share. Every writer edits one layer (the global
// guard.yaml, the workspace manifest's guard block, or one guard.projects
// entry), re-trusts a manifest it rewrote, and carries the workspace's mode
// grant to the new hash only when the change does not loosen past it. No
// writer ever grants: GrantMode does, and only the human-only callers
// (`tq mode set` after a TTY confirm, `tq allow --mode`, the desktop app)
// call it.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
	"gopkg.in/yaml.v3"
)

// ErrLockedCategory is wrapped by every refusal to change a locked group
// (identity, secrets, self-protection, privacy) or one of its rule ids.
var ErrLockedCategory = errors.New("locked: enforced in every mode")

// GuardScope is the layer a guard-mode change writes: the global guard.yaml,
// the workspace manifest (Project ""), or one guard.projects entry.
type GuardScope struct {
	Global  bool
	Project string // relative to the workspace root, slash-separated
}

// String names the scope: "global", "workspace" or "project:<dir>".
func (s GuardScope) String() string {
	switch {
	case s.Global:
		return "global"
	case s.Project != "":
		return "project:" + s.Project
	}
	return "workspace"
}

// ParseGuardScope reads String's form ("" is the workspace).
func ParseGuardScope(s string) (GuardScope, error) {
	switch {
	case s == "" || s == "workspace":
		return GuardScope{}, nil
	case s == "global":
		return GuardScope{Global: true}, nil
	case strings.HasPrefix(s, "project:") && strings.TrimSpace(strings.TrimPrefix(s, "project:")) != "":
		return GuardScope{Project: strings.TrimSpace(strings.TrimPrefix(s, "project:"))}, nil
	}
	return GuardScope{}, fmt.Errorf("scope must be workspace, global or project:<dir>, got %q", s)
}

// Guard op kinds.
const (
	GuardOpMode     = "mode"     // set Mode (and Base for custom)
	GuardOpCategory = "category" // set one category; a preset becomes custom with it as base
	GuardOpOverride = "override" // set or ("clear") remove one rules_override entry
	GuardOpTable    = "table"    // set Mode, Base and the whole Categories table at once (tq mode edit)
	// GuardOpCodexSandbox sets guard.codex.sandbox (workspace only).
	GuardOpCodexSandbox = "codex-sandbox"
	// GuardOpUnattendedOff is the deprecated `tq unattended off`: the scope
	// goes back to default; at the workspace, every project mode looser than
	// default goes too.
	GuardOpUnattendedOff = "unattended-off"
)

// ClearOverride is the GuardOpOverride value that removes the entry.
const ClearOverride = "clear"

// GuardOp is one change to a layer's guard-mode keys.
type GuardOp struct {
	Kind       string
	Mode       string
	Base       string
	Category   string
	RuleID     string
	Value      string
	Categories map[string]string // GuardOpTable
	Sandbox    string            // GuardOpCodexSandbox
}

// GuardResult says what a change did to the workspace's trust and grant.
type GuardResult struct {
	Workspace string
	Scope     string
	Hash      string // the re-trusted manifest hash ("" for global)
	// Looser is set when the layers now ask for more than the ceiling (the
	// carried grant, or default without one) allows: the session runs
	// clamped until a human grants Grant.
	Looser bool
	// Grant is what a grant covering the layers would hold (when Looser).
	Grant *trust.Grant
	// Carried: the existing grant (of mode CarriedMode) moved to the new hash.
	Carried     bool
	CarriedMode string
	// Dropped is the mode of a grant that was not carried because the
	// change loosened past it.
	Dropped  string
	Warnings []string
}

// modeKeys are one layer's guard-mode keys.
type modeKeys struct {
	Mode          string
	Base          string
	Categories    map[string]string
	RulesOverride map[string]string
}

func (k modeKeys) empty() bool {
	return k.Mode == "" && k.Base == "" && len(k.Categories) == 0 && len(k.RulesOverride) == 0
}

// lockedID reports whether a rule id sits in a locked group.
func lockedID(id string) bool {
	if strings.HasPrefix(id, guardcfg.GuardIDPrefix) || id == guardcfg.PluginIdentityGuardID || id == guardcfg.PluginHeadlessManifestGuardID {
		return true
	}
	c, ok := guardcfg.Categories[id]
	return ok && guardcfg.Locked(c)
}

// lockedDeclared reports whether a custom rule in m or g declares id with a
// locked category (`category: secrets` and so on): such a rule is enforced in
// every mode, so it takes no override either.
func lockedDeclared(m *manifest.Manifest, g guardcfg.File, id string) bool {
	rules := append([]policy.Rule{}, g.Rules...)
	if m != nil {
		rules = append(rules, m.Guard.Rules...)
	}
	for i := range rules {
		if rules[i].ID == id && guardcfg.Locked(guardcfg.CategoryOf(&rules[i])) {
			return true
		}
	}
	return false
}

// checkOverrideLocked refuses an override of a custom rule that declares a
// locked category (lockedID covers the built-in ids).
func checkOverrideLocked(m *manifest.Manifest, g guardcfg.File, op GuardOp) error {
	id := strings.TrimSpace(op.RuleID)
	if op.Kind != GuardOpOverride || op.Value == ClearOverride || !lockedDeclared(m, g, id) {
		return nil
	}
	return fmt.Errorf("rule %s: %w (it declares a category among %s)", id, ErrLockedCategory, lockedNames())
}

func lockedNames() string {
	var s []string
	for _, c := range guardcfg.LockedCategories() {
		s = append(s, string(c))
	}
	return strings.Join(s, ", ")
}

func validValueNames() string {
	var s []string
	for _, v := range guardcfg.Values() {
		s = append(s, string(v))
	}
	return strings.Join(s, "|")
}

func validCategory(name, value string) error {
	c, ok := guardcfg.ParseCategory(name)
	if !ok {
		return fmt.Errorf("unknown category %q", name)
	}
	if guardcfg.Locked(c) {
		return fmt.Errorf("category %s: %w (%s cannot be toggled)", name, ErrLockedCategory, lockedNames())
	}
	if _, ok := guardcfg.ParseValue(value); !ok {
		return fmt.Errorf("category %s: value must be %s, got %q", name, validValueNames(), value)
	}
	return nil
}

func (op GuardOp) validate(scope GuardScope) error {
	switch op.Kind {
	case GuardOpMode, GuardOpTable:
		if op.Mode == guardcfg.ModeOff {
			return errors.New("off is never stored: use claude-off, codex-off or tq off for one launch or window")
		}
		if !guardcfg.ValidStoredMode(op.Mode) {
			return fmt.Errorf("mode must be %s, got %q", strings.Join(guardcfg.StoredModes(), "|"), op.Mode)
		}
		if op.Base != "" && op.Mode != guardcfg.ModeCustom {
			return fmt.Errorf("--base is only valid with mode custom")
		}
		if op.Base != "" && !guardcfg.ValidBase(op.Base) {
			return fmt.Errorf("base must be %s, got %q", strings.Join(guardcfg.PresetNames(), "|"), op.Base)
		}
		if op.Kind == GuardOpTable {
			if len(op.Categories) > 0 && op.Mode != guardcfg.ModeCustom {
				return errors.New("categories are only valid with mode custom")
			}
			for c, v := range op.Categories {
				if err := validCategory(c, v); err != nil {
					return err
				}
			}
		}
	case GuardOpCategory:
		return validCategory(op.Category, op.Value)
	case GuardOpOverride:
		id := strings.TrimSpace(op.RuleID)
		if id == "" || strings.ContainsAny(id, " \t\r\n") {
			return fmt.Errorf("%q is not a rule id", op.RuleID)
		}
		if lockedID(id) {
			return fmt.Errorf("rule %s: %w (its group is one of %s)", id, ErrLockedCategory, lockedNames())
		}
		if _, ok := guardcfg.ParseValue(op.Value); !ok && op.Value != ClearOverride {
			return fmt.Errorf("rule %s: value must be %s|%s, got %q", id, validValueNames(), ClearOverride, op.Value)
		}
	case GuardOpCodexSandbox:
		if scope.Global || scope.Project != "" {
			return errors.New("the Codex sandbox is set for the whole workspace")
		}
		ok := false
		for _, s := range manifest.CodexSandboxes {
			ok = ok || s == op.Sandbox
		}
		if !ok {
			return fmt.Errorf("codex sandbox must be one of %s, got %q", strings.Join(manifest.CodexSandboxes, "|"), op.Sandbox)
		}
	case GuardOpUnattendedOff:
	default:
		return fmt.Errorf("unknown guard op %q", op.Kind)
	}
	return nil
}

// applyOp changes one layer's keys. parent is the layer it inherits from
// (the workspace for a project, the global file for the workspace).
func applyOp(k *modeKeys, op GuardOp, parent modeKeys) {
	// startCustom turns k into custom, keeping a preset or the parent's mode
	// as the base.
	startCustom := func() {
		switch {
		case k.Mode == guardcfg.ModeCustom:
			if k.Base == "" {
				k.Base = guardcfg.ModeDefault
			}
			return
		case k.Mode != "":
			k.Base = k.Mode
			k.Categories = nil
		case parent.Mode == guardcfg.ModeCustom:
			k.Base = parent.Base
			k.Categories = copyMap(parent.Categories)
		case parent.Mode != "":
			k.Base = parent.Mode
		default:
			k.Base = guardcfg.ModeDefault
		}
		if k.Base == "" {
			k.Base = guardcfg.ModeDefault
		}
		k.Mode = guardcfg.ModeCustom
	}
	switch op.Kind {
	case GuardOpMode:
		if op.Mode == guardcfg.ModeCustom {
			startCustom()
			if op.Base != "" {
				k.Base = op.Base
			}
			return
		}
		k.Mode, k.Base, k.Categories = op.Mode, "", nil
	case GuardOpTable:
		k.Mode, k.Base, k.Categories = op.Mode, op.Base, copyMap(op.Categories)
		if k.Mode == guardcfg.ModeCustom && k.Base == "" {
			k.Base = guardcfg.ModeDefault
		}
	case GuardOpCategory:
		startCustom()
		if k.Categories == nil {
			k.Categories = map[string]string{}
		}
		k.Categories[op.Category] = op.Value
	case GuardOpOverride:
		id := strings.TrimSpace(op.RuleID)
		if op.Value == ClearOverride {
			delete(k.RulesOverride, id)
			return
		}
		if k.RulesOverride == nil {
			k.RulesOverride = map[string]string{}
		}
		k.RulesOverride[id] = op.Value
	case GuardOpUnattendedOff:
		k.Mode, k.Base, k.Categories = guardcfg.ModeDefault, "", nil
	}
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---- strictness and layers ------------------------------------------------

// rank orders values from loosest to strictest. rule (a rule's own action,
// ask or deny) sits between ask and deny, so comparisons are conservative:
// ask below rule counts as looser even for a rule that asks anyway.
func rank(v guardcfg.Value) int {
	switch v {
	case guardcfg.ValueSkip:
		return 0
	case guardcfg.ValueAsk:
		return 1
	case guardcfg.ValueDeny:
		return 3
	}
	return 2
}

func loosest(a, b guardcfg.Value) guardcfg.Value {
	if rank(b) < rank(a) {
		return b
	}
	return a
}

// layer is one effective layer: a category table plus the overrides it sees.
type layer struct {
	table     guardcfg.Table
	overrides map[string]guardcfg.Value
}

func tableFor(mode, base string, cats map[string]string, parent guardcfg.Table) guardcfg.Table {
	switch mode {
	case "":
		return parent.Clone()
	case guardcfg.ModeCustom:
		if !guardcfg.ValidBase(base) {
			base = guardcfg.ModeDefault
		}
		t, _ := guardcfg.Preset(base)
		for name, val := range cats {
			c, ok := guardcfg.ParseCategory(name)
			v, vok := guardcfg.ParseValue(val)
			if ok && vok && !guardcfg.Locked(c) {
				t[c] = v
			}
		}
		return t
	}
	t, ok := guardcfg.Preset(mode)
	if !ok {
		t, _ = guardcfg.Preset(guardcfg.ModeDefault)
	}
	return t
}

func mergeOverrides(dst map[string]guardcfg.Value, src map[string]string) map[string]guardcfg.Value {
	out := map[string]guardcfg.Value{}
	for k, v := range dst {
		out[k] = v
	}
	for id, val := range src {
		if v, ok := guardcfg.ParseValue(val); ok && !lockedID(id) {
			out[id] = v
		}
	}
	return out
}

func globalKeys(g guardcfg.File) modeKeys {
	k := modeKeys{Mode: g.Mode, Base: g.Base}
	if len(g.Categories) > 0 {
		k.Categories = map[string]string{}
		for c, v := range g.Categories {
			k.Categories[string(c)] = string(v)
		}
	}
	if len(g.RulesOverride) > 0 {
		k.RulesOverride = map[string]string{}
		for id, v := range g.RulesOverride {
			k.RulesOverride[id] = string(v)
		}
	}
	return k
}

func setGlobalKeys(g *guardcfg.File, k modeKeys) {
	g.Mode, g.Base = k.Mode, k.Base
	g.Categories, g.RulesOverride = nil, nil
	for c, v := range k.Categories {
		if g.Categories == nil {
			g.Categories = map[guardcfg.Category]guardcfg.Value{}
		}
		g.Categories[guardcfg.Category(c)] = guardcfg.Value(v)
	}
	for id, v := range k.RulesOverride {
		if g.RulesOverride == nil {
			g.RulesOverride = map[string]guardcfg.Value{}
		}
		g.RulesOverride[id] = guardcfg.Value(v)
	}
}

// legacyKeys reads a guard.unattended block as guard-mode keys (spec E
// §11.1): workspace: true is relaxed, each project is a relaxed project,
// keep_asking ids become rule overrides (an absent list keeps the old
// defaults; a locked id, such as a PII rule, is dropped as redundant).
func legacyKeys(u *manifest.Unattended) (ws modeKeys, projects map[string]modeKeys, sandbox string) {
	if u == nil {
		return ws, nil, ""
	}
	if u.Workspace {
		ws.Mode = guardcfg.ModeRelaxed
	}
	for _, p := range u.Projects {
		if projects == nil {
			projects = map[string]modeKeys{}
		}
		projects[manifest.CleanProject(p)] = modeKeys{Mode: guardcfg.ModeRelaxed}
	}
	var ids []string
	switch {
	case u.KeepAsking != nil:
		ids = *u.KeepAsking
	case u.Configured():
		ids = manifest.DefaultKeepAsking
	}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id == "" || lockedID(id) {
			continue
		}
		if ws.RulesOverride == nil {
			ws.RulesOverride = map[string]string{}
		}
		ws.RulesOverride[id] = string(guardcfg.ValueRule)
	}
	return ws, projects, u.CodexSandbox
}

func wsKeys(g manifest.Guard) modeKeys {
	return modeKeys{Mode: g.Mode, Base: g.Base, Categories: g.Categories, RulesOverride: g.RulesOverride}
}

// manifestKeys returns the workspace and project keys of m, reading a legacy
// guard.unattended block when the manifest sets no mode key.
func manifestKeys(m *manifest.Manifest) (modeKeys, map[string]modeKeys) {
	if m == nil {
		return modeKeys{}, nil
	}
	if m.Guard.Unattended != nil && !m.Guard.HasMode() {
		ws, ps, _ := legacyKeys(m.Guard.Unattended)
		return ws, ps
	}
	ps := map[string]modeKeys{}
	for dir, p := range m.Guard.Projects {
		ps[manifest.CleanProject(dir)] = modeKeys{Mode: p.Mode, Base: p.Base, Categories: p.Categories, RulesOverride: p.RulesOverride}
	}
	return wsKeys(m.Guard), ps
}

// layersOf returns the effective workspace layer and each project layer of
// m on top of the global file g.
func layersOf(m *manifest.Manifest, g guardcfg.File) []layer {
	def, _ := guardcfg.Preset(guardcfg.ModeDefault)
	gk := globalKeys(g)
	gl := layer{table: tableFor(gk.Mode, gk.Base, gk.Categories, def), overrides: mergeOverrides(nil, gk.RulesOverride)}
	if m == nil {
		return []layer{gl}
	}
	wk, ps := manifestKeys(m)
	wl := layer{table: tableFor(wk.Mode, wk.Base, wk.Categories, gl.table), overrides: mergeOverrides(gl.overrides, wk.RulesOverride)}
	out := []layer{wl}
	for _, dir := range sortedKeys(ps) {
		p := ps[dir]
		out = append(out, layer{table: tableFor(p.Mode, p.Base, p.Categories, wl.table), overrides: mergeOverrides(wl.overrides, p.RulesOverride)})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func relaxedGrant() *trust.Grant {
	t, _ := guardcfg.Preset(guardcfg.ModeRelaxed)
	return &trust.Grant{Mode: guardcfg.ModeRelaxed, Table: tableStrings(t)}
}

func tableStrings(t guardcfg.Table) map[string]string {
	out := map[string]string{}
	for _, c := range guardcfg.AllCategories() {
		out[string(c)] = string(t.Get(c))
	}
	return out
}

// ceilingOf turns a grant into the loosest layer it allows; nil is default.
func ceilingOf(g *trust.Grant) layer {
	def, _ := guardcfg.Preset(guardcfg.ModeDefault)
	if g == nil {
		return layer{table: def}
	}
	if len(g.Table) == 0 && g.Mode == guardcfg.ModeRelaxed {
		g = relaxedGrant()
	}
	t := guardcfg.Table{}
	for _, c := range guardcfg.AllCategories() {
		v, ok := guardcfg.ParseValue(g.Table[string(c)])
		if !ok || guardcfg.Locked(c) {
			v = guardcfg.ValueRule
		}
		t[c] = v
	}
	l := layer{table: t, overrides: map[string]guardcfg.Value{}}
	for id, val := range g.Overrides {
		if v, ok := guardcfg.ParseValue(val); ok {
			l.overrides[id] = v
		}
	}
	return l
}

// idValue is the value a layer gives rule id: its override, else its
// category's value (rule for an unclassified id).
func (l layer) idValue(id string) guardcfg.Value {
	if v, ok := l.overrides[id]; ok {
		return v
	}
	if c, ok := guardcfg.Categories[id]; ok {
		return l.table.Get(c)
	}
	return guardcfg.ValueRule
}

// ownActions returns the own actions of the rules in each category and of
// each known id (computed once). A category with no known rule may hold
// either.
var ownActions = sync.OnceValues(func() (map[guardcfg.Category][]policy.Action, map[string][]policy.Action) {
	byCat := map[guardcfg.Category]map[policy.Action]bool{}
	byID := map[string][]policy.Action{}
	for _, r := range guardcfg.Known(nil, guardcfg.File{}) {
		if r.Action != policy.Ask && r.Action != policy.Deny {
			continue
		}
		byID[r.ID] = append(byID[r.ID], r.Action)
		if c, ok := guardcfg.Categories[r.ID]; ok {
			if byCat[c] == nil {
				byCat[c] = map[policy.Action]bool{}
			}
			byCat[c][r.Action] = true
		}
	}
	cats := map[guardcfg.Category][]policy.Action{}
	for c, set := range byCat {
		for _, a := range []policy.Action{policy.Ask, policy.Deny} {
			if set[a] {
				cats[c] = append(cats[c], a)
			}
		}
	}
	return cats, byID
})

var bothActions = []policy.Action{policy.Ask, policy.Deny}

func catOwns(c guardcfg.Category) []policy.Action {
	cats, _ := ownActions()
	if a := cats[c]; len(a) > 0 {
		return a
	}
	return bothActions
}

func idOwns(id string) []policy.Action {
	_, ids := ownActions()
	if a := ids[id]; len(a) > 0 {
		return a
	}
	return bothActions
}

// strictness is how strict v makes a rule whose own action is own.
func strictness(v guardcfg.Value, own policy.Action) int {
	switch v.Apply(own) {
	case policy.Allow:
		return 0
	case policy.Ask:
		return 1
	}
	return 2
}

// looser reports whether any layer gives some rule (by category or by id) an
// action looser than the ceiling allows.
func looser(ls []layer, ceil layer) bool {
	idLooser := func(l layer, id string) bool {
		for _, own := range idOwns(id) {
			if strictness(l.idValue(id), own) < strictness(ceil.idValue(id), own) {
				return true
			}
		}
		return false
	}
	for _, l := range ls {
		for _, c := range guardcfg.AllCategories() {
			if guardcfg.Locked(c) {
				continue
			}
			for _, own := range catOwns(c) {
				if strictness(l.table.Get(c), own) < strictness(ceil.table.Get(c), own) {
					return true
				}
			}
		}
		for id := range l.overrides {
			if idLooser(l, id) {
				return true
			}
		}
		for id := range ceil.overrides {
			if idLooser(l, id) {
				return true
			}
		}
	}
	return false
}

// proposal is the grant that covers every layer: a custom table of the
// loosest value per category and id, so the human grants exactly what they
// see (spec E §6.3). It is relaxed only when that table is the relaxed preset
// itself (overrides may only tighten it). nil when no layer is looser than
// default.
func proposal(ls []layer) *trust.Grant {
	if !looser(ls, ceilingOf(nil)) {
		return nil
	}
	merged := guardcfg.Table{}
	for _, c := range guardcfg.AllCategories() {
		v := guardcfg.ValueDeny
		if guardcfg.Locked(c) {
			v = guardcfg.ValueRule
		} else {
			for _, l := range ls {
				v = loosest(v, l.table.Get(c))
			}
		}
		merged[c] = v
	}
	if isRelaxedTable(merged) && !looser(ls, ceilingOf(relaxedGrant())) {
		return relaxedGrant()
	}
	ids := map[string]bool{}
	for _, l := range ls {
		for id := range l.overrides {
			ids[id] = true
		}
	}
	g := &trust.Grant{Mode: guardcfg.ModeCustom, Table: tableStrings(merged)}
	for _, id := range sortedKeys(ids) {
		v := guardcfg.ValueDeny
		for _, l := range ls {
			v = loosest(v, l.idValue(id))
		}
		if v != (layer{table: merged}).idValue(id) {
			if g.Overrides == nil {
				g.Overrides = map[string]string{}
			}
			g.Overrides[id] = string(v)
		}
	}
	return g
}

// isRelaxedTable reports whether t gives every unlocked category the relaxed
// preset's value.
func isRelaxedTable(t guardcfg.Table) bool {
	rt, _ := guardcfg.Preset(guardcfg.ModeRelaxed)
	for _, c := range guardcfg.AllCategories() {
		if !guardcfg.Locked(c) && t.Get(c) != rt.Get(c) {
			return false
		}
	}
	return true
}

// currentGrant is hash's grant: the .mode file, else a legacy bypass trust
// read as relaxed (spec E §11.1). An unreadable grant is none.
func currentGrant(hash string) *trust.Grant {
	if g, err := trust.LoadGrant(hash); err == nil && g != nil {
		return g
	}
	if trust.IsBypassAllowed(hash) {
		return relaxedGrant()
	}
	return nil
}

// ---- writers ----------------------------------------------------------------

// ApplyGuardOp writes op to scope's layer. w is the trusted workspace (nil
// for the global scope). It never grants.
func ApplyGuardOp(w *resolve.Workspace, scope GuardScope, op GuardOp) (GuardResult, error) {
	if err := op.validate(scope); err != nil {
		return GuardResult{}, err
	}
	if scope.Global {
		return applyGlobal(op)
	}
	if w == nil {
		return GuardResult{}, errors.New("no workspace: pass one, or use the global scope")
	}
	return applyWorkspace(w, scope, op)
}

func unknownIDWarning(m *manifest.Manifest, g guardcfg.File, op GuardOp) []string {
	if op.Kind != GuardOpOverride || op.Value == ClearOverride {
		return nil
	}
	id := strings.TrimSpace(op.RuleID)
	if _, ok := guardcfg.Categories[id]; ok {
		return nil
	}
	if _, ok := guardcfg.Lookup(id, guardcfg.Known(m, g)); ok {
		return nil
	}
	return []string{fmt.Sprintf("%s is not a known rule id (tq guard list shows them); the override does nothing until a rule has it", id)}
}

func applyGlobal(op GuardOp) (GuardResult, error) {
	g, err := guardcfg.Load()
	if err != nil {
		return GuardResult{}, fmt.Errorf("the global guard file cannot be read, nothing changed: %w", err)
	}
	if err := checkOverrideLocked(nil, g, op); err != nil {
		return GuardResult{}, err
	}
	k := globalKeys(g)
	applyOp(&k, op, modeKeys{})
	if op.Kind == GuardOpUnattendedOff {
		k = modeKeys{RulesOverride: k.RulesOverride}
	}
	setGlobalKeys(&g, k)
	if err := guardcfg.Save(g); err != nil {
		return GuardResult{}, err
	}
	res := GuardResult{Scope: GuardScope{Global: true}.String(), Warnings: unknownIDWarning(nil, g, op)}
	ls := layersOf(nil, g)
	var ceil *trust.Grant
	if gg, err := trust.LoadGrant(trust.GlobalGrantKey); err == nil {
		ceil = gg
	}
	if looser(ls, ceilingOf(ceil)) {
		res.Looser, res.Grant = true, proposal(ls)
	}
	return res, nil
}

// checkTrusted returns the manifest's current hash, refusing one that changed
// since it was trusted: re-trusting must never approve an edit the user has
// not reviewed.
func checkTrusted(w *resolve.Workspace) (string, error) {
	h, err := trust.HashFile(w.ManifestPath)
	if err != nil {
		return "", err
	}
	if !trust.IsTrusted(h) {
		return "", fmt.Errorf("%s changed since it was trusted: review it and run tq allow %s first", w.ManifestPath, w.Name)
	}
	return h, nil
}

// checkProject validates a project folder: relative, strictly inside root,
// an existing folder. It returns the cleaned slash form.
func checkProject(root, p string) (string, error) {
	raw := strings.TrimSpace(p)
	c := manifest.CleanProject(raw)
	switch {
	case raw == "":
		return "", errors.New("project: empty")
	case filepath.IsAbs(raw) || strings.HasPrefix(c, "/") || filepath.VolumeName(raw) != "" || (len(raw) > 1 && raw[1] == ':'):
		return "", fmt.Errorf("project %q must be relative to the workspace root", p)
	case c == ".":
		return "", fmt.Errorf("project %q is the workspace root: use the workspace scope", p)
	case c == ".." || strings.HasPrefix(c, "../"):
		return "", fmt.Errorf("project %q leaves the workspace root", p)
	}
	if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(c))); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("project %s: not a folder in %s", c, root)
	}
	return c, nil
}

func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func applyWorkspace(w *resolve.Workspace, scope GuardScope, op GuardOp) (GuardResult, error) {
	oldHash, err := checkTrusted(w)
	if err != nil {
		return GuardResult{}, err
	}
	project := ""
	if scope.Project != "" {
		if project, err = checkProject(w.Root, scope.Project); err != nil {
			return GuardResult{}, err
		}
	}
	g, gerr := guardcfg.Load()
	if gerr != nil {
		g = guardcfg.File{}
	}
	if err := checkOverrideLocked(w.Manifest, g, op); err != nil {
		return GuardResult{}, err
	}
	raw, err := os.ReadFile(w.ManifestPath)
	if err != nil {
		return GuardResult{}, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return GuardResult{}, fmt.Errorf("%s: not a YAML mapping", w.ManifestPath)
	}
	root := doc.Content[0]
	gm := mapGet(root, "guard")
	if gm == nil || gm.Kind != yaml.MappingNode {
		gm = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapSet(root, "guard", gm)
	}
	if err := migrateLegacy(gm); err != nil {
		return GuardResult{}, fmt.Errorf("%s: guard.unattended: %w", w.ManifestPath, err)
	}

	ws := readKeys(gm)
	switch {
	case op.Kind == GuardOpCodexSandbox:
		cx := mapGet(gm, "codex")
		if cx == nil || cx.Kind != yaml.MappingNode {
			cx = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			mapSet(gm, "codex", cx)
		}
		mapSet(cx, "sandbox", scalar(op.Sandbox))
	case project == "":
		applyOp(&ws, op, globalKeys(g))
		writeKeys(gm, ws)
		if op.Kind == GuardOpUnattendedOff {
			clearLooseProjects(gm, ws)
		}
	default:
		pm := projectNode(gm, project, true)
		pk := readKeys(pm)
		if op.Kind == GuardOpUnattendedOff {
			pk.Mode, pk.Base, pk.Categories = "", "", nil
		} else {
			applyOp(&pk, op, ws)
		}
		writeKeys(pm, pk)
	}
	pruneProjects(gm)
	if len(gm.Content) == 0 {
		mapDel(root, "guard")
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return GuardResult{}, err
	}
	_ = enc.Close()
	tmp := w.ManifestPath + ".tq-mode.tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return GuardResult{}, err
	}
	m, err := manifest.Load(tmp)
	if err != nil {
		os.Remove(tmp)
		return GuardResult{}, fmt.Errorf("the edited manifest would not load, nothing changed: %w", err)
	}
	if err := os.Rename(tmp, w.ManifestPath); err != nil {
		os.Remove(tmp)
		return GuardResult{}, err
	}
	newHash, err := trust.HashFile(w.ManifestPath)
	if err != nil {
		return GuardResult{}, err
	}
	if err := trust.Allow(newHash); err != nil {
		return GuardResult{}, err
	}

	res := GuardResult{Workspace: w.Name, Scope: GuardScope{Project: project}.String(), Hash: newHash, Warnings: unknownIDWarning(m, g, op)}
	ls := layersOf(m, g)
	var ceil *trust.Grant
	if old := currentGrant(oldHash); old != nil {
		if looser(ls, ceilingOf(old)) {
			res.Dropped = old.Mode
		} else {
			carried := *old
			if err := trust.SaveGrant(newHash, carried); err != nil {
				return res, fmt.Errorf("manifest re-trusted, but the %s grant could not be carried: %w", old.Mode, err)
			}
			res.Carried, res.CarriedMode, ceil = true, old.Mode, &carried
		}
	}
	if looser(ls, ceilingOf(ceil)) {
		res.Looser, res.Grant = true, proposal(ls)
	}
	return res, nil
}

// clearLooseProjects removes the mode keys of every project whose table is
// looser than default (the old `unattended off` cleared every project).
func clearLooseProjects(gm *yaml.Node, ws modeKeys) {
	ps := mapGet(gm, "projects")
	if ps == nil || ps.Kind != yaml.MappingNode {
		return
	}
	def, _ := guardcfg.Preset(guardcfg.ModeDefault)
	wsTable := tableFor(ws.Mode, ws.Base, ws.Categories, def)
	for i := 0; i+1 < len(ps.Content); i += 2 {
		pm := ps.Content[i+1]
		if pm.Kind != yaml.MappingNode {
			continue
		}
		pk := readKeys(pm)
		if pk.Mode == "" {
			continue
		}
		if looser([]layer{{table: tableFor(pk.Mode, pk.Base, pk.Categories, wsTable)}}, ceilingOf(nil)) {
			pk.Mode, pk.Base, pk.Categories = "", "", nil
			writeKeys(pm, pk)
		}
	}
}

// migrateLegacy rewrites a guard.unattended block into the guard-mode keys
// (spec E §11.1), in place.
func migrateLegacy(gm *yaml.Node) error {
	un := mapGet(gm, "unattended")
	if un == nil {
		return nil
	}
	var u manifest.Unattended
	if err := un.Decode(&u); err != nil {
		return err
	}
	mapDel(gm, "unattended")
	ws, ps, sandbox := legacyKeys(&u)
	cur := readKeys(gm)
	if cur.Mode == "" {
		cur.Mode = ws.Mode
	}
	for id, v := range ws.RulesOverride {
		if _, ok := cur.RulesOverride[id]; !ok {
			if cur.RulesOverride == nil {
				cur.RulesOverride = map[string]string{}
			}
			cur.RulesOverride[id] = v
		}
	}
	writeKeys(gm, cur)
	for _, dir := range sortedKeys(ps) {
		pm := projectNode(gm, dir, true)
		pk := readKeys(pm)
		if pk.Mode == "" {
			pk.Mode = ps[dir].Mode
		}
		writeKeys(pm, pk)
	}
	if sandbox != "" {
		cx := mapGet(gm, "codex")
		if cx == nil || cx.Kind != yaml.MappingNode {
			cx = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			mapSet(gm, "codex", cx)
		}
		if mapGet(cx, "sandbox") == nil {
			mapSet(cx, "sandbox", scalar(sandbox))
		}
	}
	return nil
}

// ---- grants -----------------------------------------------------------------

// GrantMode grants w's current layers: the one-time human permission to run
// looser than default (spec E §6.3). Only human-only callers may use it. It
// refuses a manifest that changed since it was trusted and one that is no
// looser than default.
func GrantMode(w *resolve.Workspace) (trust.Grant, error) {
	if w == nil {
		return trust.Grant{}, errors.New("no workspace")
	}
	h, err := checkTrusted(w)
	if err != nil {
		return trust.Grant{}, err
	}
	m, err := manifest.Load(w.ManifestPath)
	if err != nil {
		return trust.Grant{}, err
	}
	g, gerr := guardcfg.Load()
	if gerr != nil {
		g = guardcfg.File{}
	}
	p := proposal(layersOf(m, g))
	if p == nil {
		return trust.Grant{}, fmt.Errorf("%s runs no looser than default: nothing to grant", w.Name)
	}
	p.GrantedAt = time.Now().UTC()
	if err := trust.SaveGrant(h, *p); err != nil {
		return trust.Grant{}, err
	}
	return *p, nil
}

// RevokeGrant removes w's grant (and a legacy bypass trust, which reads as a
// relaxed grant). The manifest stays trusted.
func RevokeGrant(w *resolve.Workspace) error {
	if w == nil {
		return errors.New("no workspace")
	}
	h, err := trust.HashFile(w.ManifestPath)
	if err != nil {
		return err
	}
	if err := trust.DeleteGrant(h); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(paths.TrustDir(), h+".bypass")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ---- view -------------------------------------------------------------------

// GuardCategory is one row of the category table.
type GuardCategory struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Source string `json:"source"`
	Locked bool   `json:"locked"`
}

// GuardOverride is one rules_override entry.
type GuardOverride struct {
	ID     string `json:"id"`
	Value  string `json:"value"`
	Source string `json:"source,omitempty"`
}

// GuardView is a workspace's resolved guard mode for the CLI, the desktop
// Guard tab and the hub page.
type GuardView struct {
	Workspace  string          `json:"workspace,omitempty"`
	Root       string          `json:"root,omitempty"`
	Mode       string          `json:"mode"`
	Label      string          `json:"label"`
	Base       string          `json:"base,omitempty"`
	Requested  string          `json:"requested,omitempty"`
	ModeSource string          `json:"mode_source,omitempty"`
	Categories []GuardCategory `json:"categories"`
	Overrides  []GuardOverride `json:"overrides"`
	Ceiling    string          `json:"ceiling"`
	Granted    bool            `json:"granted"`
	GrantedAt  string          `json:"granted_at,omitempty"`
	Looser     bool            `json:"looser_than_default"`
	// NeedsGrant: the layers ask for more than the ceiling allows, so the
	// session runs clamped; Proposal names the grant that would cover them.
	NeedsGrant bool     `json:"needs_grant"`
	Proposal   string   `json:"proposal,omitempty"`
	Clamped    []string `json:"clamped"`
	Ignored    []string `json:"ignored"`
	Confirms   int      `json:"skipped_confirms_24h"`
	Blocks     int      `json:"skipped_blocks_24h"`
	Locked     []string `json:"locked"`
	Projects   []string `json:"projects"`
}

// GuardFacts are what GuardViewOf needs beyond the resolved mode.
type GuardFacts struct {
	Root             string
	Confirms, Blocks int
	Projects         []string
	NeedsGrant       bool
	Proposal         string
}

// ModeLabel names a resolved mode: "relaxed", or "custom (relaxed + 2)" with
// the number of categories that differ from the base.
func ModeLabel(res guardcfg.Resolved) string {
	if res.Mode != guardcfg.ModeCustom {
		if res.Mode == "" {
			return guardcfg.ModeDefault
		}
		return res.Mode
	}
	base := res.Base
	if !guardcfg.ValidBase(base) {
		base = guardcfg.ModeDefault
	}
	n := len(CustomDiff(res))
	if n == 0 {
		return "custom (" + base + ")"
	}
	return fmt.Sprintf("custom (%s + %d)", base, n)
}

// CustomDiff lists the categories where a custom table differs from its
// base, as "category value", in display order.
func CustomDiff(res guardcfg.Resolved) []string {
	base := res.Base
	if !guardcfg.ValidBase(base) {
		base = guardcfg.ModeDefault
	}
	bt, _ := guardcfg.Preset(base)
	var out []string
	for _, c := range guardcfg.AllCategories() {
		if guardcfg.Locked(c) {
			continue
		}
		if v := res.Table.Get(c); v != bt.Get(c) {
			out = append(out, string(c)+" "+string(v))
		}
	}
	return out
}

// GuardViewOf renders res for workspace name ("" for a neutral folder).
func GuardViewOf(name string, res guardcfg.Resolved, f GuardFacts) GuardView {
	v := GuardView{
		Workspace: name, Root: f.Root, Mode: res.Mode, Label: ModeLabel(res), Base: res.Base,
		Requested: res.Requested, ModeSource: res.ModeSource,
		Categories: []GuardCategory{}, Overrides: []GuardOverride{},
		Ceiling: guardcfg.ModeDefault, Looser: res.LooserThanDefault(),
		Clamped: append([]string{}, res.Clamped...), Ignored: append([]string{}, res.Ignored...),
		Confirms: f.Confirms, Blocks: f.Blocks, Projects: append([]string{}, f.Projects...),
		NeedsGrant: f.NeedsGrant, Proposal: f.Proposal,
	}
	if v.Mode == "" {
		v.Mode = guardcfg.ModeDefault
	}
	if v.Mode != guardcfg.ModeCustom {
		v.Base = ""
	}
	if res.Ceiling != nil {
		v.Ceiling, v.Granted = res.Ceiling.Mode, true
		if !res.Ceiling.GrantedAt.IsZero() {
			v.GrantedAt = res.Ceiling.GrantedAt.UTC().Format(time.RFC3339)
		}
	}
	if res.Requested != "" && res.Requested != v.Mode || len(res.Clamped) > 0 {
		v.NeedsGrant = true
		if v.Proposal == "" && res.Requested != "" && res.Requested != v.Mode {
			v.Proposal = res.Requested
		}
	}
	src := func(key string) string {
		if s := res.Sources[key]; s != "" {
			return s
		}
		return "builtin"
	}
	for _, c := range guardcfg.AllCategories() {
		row := GuardCategory{Name: string(c), Value: string(res.Table.Get(c)), Source: src(string(c)), Locked: guardcfg.Locked(c)}
		if row.Locked {
			row.Value = string(guardcfg.ValueRule)
		}
		v.Categories = append(v.Categories, row)
	}
	for _, id := range sortedKeys(res.Overrides) {
		v.Overrides = append(v.Overrides, GuardOverride{ID: id, Value: string(res.Overrides[id]), Source: res.Sources[id]})
	}
	for _, c := range guardcfg.LockedCategories() {
		v.Locked = append(v.Locked, string(c))
	}
	return v
}

// SkipCounts returns the confirms and blocks skipped or lowered for ws in the
// 24 hours before now. It reads only the log's tail.
func SkipCounts(ws string, now time.Time) (confirms, blocks int) {
	for _, r := range guardlog.Read(ws, now.Add(-24*time.Hour), 1<<20) {
		if r.IsBlock() {
			blocks++
		} else {
			confirms++
		}
	}
	return confirms, blocks
}

// ResolveWorkspace resolves w's mode at its root, as the hook would for a
// session there with no launch override and no repo file.
func ResolveWorkspace(w *resolve.Workspace, g guardcfg.File) guardcfg.Resolved {
	return guardcfg.Resolve(w.Manifest, w.Name, w.Root, w.Root, "", g, guardcfg.RepoFile{}, "", currentGrant(w.Hash))
}

// WorkspaceFacts gathers the view facts for w: counts, projects, and whether
// its layers need a grant they do not have.
func WorkspaceFacts(w *resolve.Workspace, g guardcfg.File, now time.Time) GuardFacts {
	f := GuardFacts{Root: w.Root}
	f.Confirms, f.Blocks = SkipCounts(w.Name, now)
	_, ps := manifestKeys(w.Manifest)
	f.Projects = sortedKeys(ps)
	ls := layersOf(w.Manifest, g)
	if looser(ls, ceilingOf(currentGrant(w.Hash))) {
		f.NeedsGrant = true
		if p := proposal(ls); p != nil {
			f.Proposal = p.Mode
		}
	}
	return f
}

// GuardViewFor is the view of one trusted workspace.
func GuardViewFor(w *resolve.Workspace, now time.Time) GuardView {
	g, err := guardcfg.Load()
	if err != nil {
		g = guardcfg.File{}
	}
	return GuardViewOf(w.Name, ResolveWorkspace(w, g), WorkspaceFacts(w, g, now))
}

// GuardWorkspaces lists the view of every trusted workspace, by name.
func GuardWorkspaces() ([]GuardView, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	wss, _ := resolve.ListWorkspaces(cfg)
	sort.Slice(wss, func(i, j int) bool { return wss[i].Name < wss[j].Name })
	now := time.Now()
	out := []GuardView{}
	for i := range wss {
		w := &wss[i]
		if w.Manifest == nil || !trust.IsTrusted(w.Hash) {
			continue
		}
		out = append(out, GuardViewFor(w, now))
	}
	return out, nil
}

// FindWorkspace returns the trusted workspace called name.
func FindWorkspace(name string) (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	wss, _ := resolve.ListWorkspaces(cfg)
	for i := range wss {
		if wss[i].Name == name {
			w := wss[i]
			if w.Manifest == nil || !trust.IsTrusted(w.Hash) {
				return nil, fmt.Errorf("workspace %s is not trusted: review its manifest and run tq allow %s first", name, name)
			}
			return &w, nil
		}
	}
	return nil, fmt.Errorf("no workspace %q (tq list shows them)", name)
}

// ---- yaml.Node helpers ----------------------------------------------------

func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapSet(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

func mapDel(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

func strMapNode(m map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range sortedKeys(m) {
		n.Content = append(n.Content, scalar(k), scalar(m[k]))
	}
	return n
}

func readKeys(m *yaml.Node) modeKeys {
	var k modeKeys
	if n := mapGet(m, "mode"); n != nil {
		k.Mode = n.Value
	}
	if n := mapGet(m, "base"); n != nil {
		k.Base = n.Value
	}
	if n := mapGet(m, "categories"); n != nil {
		_ = n.Decode(&k.Categories)
	}
	if n := mapGet(m, "rules_override"); n != nil {
		_ = n.Decode(&k.RulesOverride)
	}
	return k
}

func writeKeys(m *yaml.Node, k modeKeys) {
	for key, val := range map[string]string{"mode": k.Mode, "base": k.Base} {
		if val == "" {
			mapDel(m, key)
		} else {
			mapSet(m, key, scalar(val))
		}
	}
	for key, val := range map[string]map[string]string{"categories": k.Categories, "rules_override": k.RulesOverride} {
		if len(val) == 0 {
			mapDel(m, key)
		} else {
			mapSet(m, key, strMapNode(val))
		}
	}
}

// projectNode returns guard.projects.<dir> (matching case-insensitively on
// Windows), creating it when create is set.
func projectNode(gm *yaml.Node, dir string, create bool) *yaml.Node {
	ps := mapGet(gm, "projects")
	if ps == nil || ps.Kind != yaml.MappingNode {
		if !create {
			return nil
		}
		ps = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapSet(gm, "projects", ps)
	}
	for i := 0; i+1 < len(ps.Content); i += 2 {
		if sameDir(manifest.CleanProject(ps.Content[i].Value), dir) {
			if ps.Content[i+1].Kind != yaml.MappingNode {
				ps.Content[i+1] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
			}
			return ps.Content[i+1]
		}
	}
	if !create {
		return nil
	}
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	ps.Content = append(ps.Content, scalar(dir), n)
	return n
}

// pruneProjects drops empty project entries and an empty projects map.
func pruneProjects(gm *yaml.Node) {
	ps := mapGet(gm, "projects")
	if ps == nil {
		return
	}
	if ps.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(ps.Content); {
			if v := ps.Content[i+1]; v.Kind == yaml.MappingNode && len(v.Content) == 0 {
				ps.Content = append(ps.Content[:i], ps.Content[i+2:]...)
				continue
			}
			i += 2
		}
	}
	if len(ps.Content) == 0 {
		mapDel(gm, "projects")
	}
}

// GuardLayerState is one layer's mode and its effective category table, the
// starting point of `tq mode edit`.
type GuardLayerState struct {
	Mode   string            // the layer's mode, or the one it inherits
	Base   string            // for custom
	Values map[string]string // category -> value, every category
}

// GuardLayer returns scope's layer state (w nil for the global scope).
func GuardLayer(w *resolve.Workspace, scope GuardScope) (GuardLayerState, error) {
	g, err := guardcfg.Load()
	if err != nil {
		if scope.Global {
			return GuardLayerState{}, err
		}
		g = guardcfg.File{}
	}
	def, _ := guardcfg.Preset(guardcfg.ModeDefault)
	inherit := func(k, parent modeKeys) modeKeys {
		if k.Mode == "" {
			return modeKeys{Mode: parent.Mode, Base: parent.Base, Categories: parent.Categories}
		}
		return k
	}
	gk := inherit(globalKeys(g), modeKeys{Mode: guardcfg.ModeDefault})
	k := gk
	if !scope.Global {
		if w == nil {
			return GuardLayerState{}, errors.New("no workspace")
		}
		wk, ps := manifestKeys(w.Manifest)
		k = inherit(wk, gk)
		if scope.Project != "" {
			dir, err := checkProject(w.Root, scope.Project)
			if err != nil {
				return GuardLayerState{}, err
			}
			for d, pk := range ps {
				if sameDir(d, dir) {
					k = inherit(pk, k)
				}
			}
		}
	}
	st := GuardLayerState{Mode: k.Mode, Values: tableStrings(tableFor(k.Mode, k.Base, k.Categories, def))}
	if k.Mode == guardcfg.ModeCustom {
		st.Base = k.Base
		if st.Base == "" {
			st.Base = guardcfg.ModeDefault
		}
	}
	return st, nil
}
