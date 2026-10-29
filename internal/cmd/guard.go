package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
	"gopkg.in/yaml.v3"
)

func newGuardCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "guard",
		Short: "Turn guard rules on or off, or change ask <-> deny, per workspace or for all",
		Long: `See and change which guard rules apply.

  tq guard list [--ws NAME]                     every rule, its action, on/off, and why
  tq guard off RULE... [--ws NAME | --all]      switch rules off
  tq guard on  RULE... [--ws NAME | --all]      switch them back on
  tq guard set RULE ask|deny|default [--ws NAME | --all]

--all writes ~/.tentaqles/guard.yaml, which applies to every workspace.
--ws NAME (the default is the workspace of the current folder) edits that
workspace's .tentaqles.yaml guard block and re-trusts it; a manifest wins
over the global file. A repo's .claude/tq-rules.yaml can only add rules, so
nothing in a cloned repo can switch a rule off.

Inside Claude Code these commands ask you first (rule tq/guard-change), so
an agent cannot quietly turn a check off.`,
	}
	var ws string
	var all bool
	scope := func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&ws, "ws", "", "workspace to change (default: the current folder's)")
		cmd.Flags().BoolVar(&all, "all", false, "change the global file for every workspace")
	}
	var asJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List every rule with its effective state",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			w, err := guardTarget(ws, false)
			if err != nil {
				return err
			}
			return guardList(c, w, asJSON)
		},
	}
	list.Flags().StringVar(&ws, "ws", "", "workspace (default: the current folder's)")
	list.Flags().BoolVar(&asJSON, "json", false, "print JSON")

	off := &cobra.Command{
		Use: "off RULE...", Short: "Switch rules off", Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error { return guardChange(c, ws, all, args, "off", "") },
	}
	on := &cobra.Command{
		Use: "on RULE...", Short: "Switch rules back on", Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error { return guardChange(c, ws, all, args, "on", "") },
	}
	set := &cobra.Command{
		Use: "set RULE ask|deny|default", Short: "Change a rule's action (default removes the override)", Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			a := strings.ToLower(args[1])
			if a != "ask" && a != "deny" && a != "default" {
				return fmt.Errorf("action must be ask, deny or default, got %q", args[1])
			}
			return guardChange(c, ws, all, args[:1], "set", a)
		},
	}
	for _, s := range []*cobra.Command{off, on, set} {
		scope(s)
	}
	c.AddCommand(list, off, on, set)
	return c
}

// guardTarget resolves --ws (or the current folder) to a trusted workspace.
// With all set, no workspace is needed.
func guardTarget(name string, all bool) (*resolve.Workspace, error) {
	if all && name != "" {
		return nil, errors.New("use either --ws or --all, not both")
	}
	if all {
		return nil, nil
	}
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	if name == "" {
		cwd, _ := os.Getwd()
		res := resolve.Resolve(cwd, cfg)
		if res.Workspace == nil {
			if res.Untrusted != nil {
				return nil, fmt.Errorf("workspace %s is not trusted: review its manifest and run tq allow %s first", res.Untrusted.Name, res.Untrusted.Name)
			}
			return nil, errors.New("not in a workspace: pass --ws NAME, or --all for every workspace")
		}
		return res.Workspace, nil
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

type guardRow struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Default string `json:"default"`
	Now     string `json:"now"`             // ask | deny | off
	Where   string `json:"where,omitempty"` // global | <workspace>
	Summary string `json:"summary,omitempty"`
}

func guardRows(w *resolve.Workspace) ([]guardRow, guardcfg.Effective, error) {
	g, gerr := guardcfg.Load()
	var m *manifest.Manifest
	name := ""
	if w != nil {
		m, name = w.Manifest, w.Name
	}
	eff := guardcfg.Merge(g, gerr, m, name)
	var rows []guardRow
	for _, r := range guardcfg.Known(m, g) {
		row := guardRow{ID: r.ID, Kind: r.Kind, Default: string(r.Action), Summary: r.Summary}
		switch {
		case eff.Off(r.ID):
			row.Now, row.Where = "off", eff.DisableOrigin[r.ID]
		default:
			row.Now = string(eff.ActionFor(r.ID, r.Action))
			row.Where = eff.ActionOrigin[r.ID]
		}
		rows = append(rows, row)
	}
	return rows, eff, gerr
}

func guardList(c *cobra.Command, w *resolve.Workspace, asJSON bool) error {
	rows, _, gerr := guardRows(w)
	out := c.OutOrStdout()
	if asJSON {
		return json.NewEncoder(out).Encode(rows)
	}
	scope := "no workspace (global file only)"
	if w != nil {
		scope = "workspace " + w.Name
	}
	fmt.Fprintf(out, "guard rules for %s\n\n", scope)
	fmt.Fprintf(out, "%-34s %-9s %-8s %-6s %s\n", "RULE", "KIND", "DEFAULT", "NOW", "CHANGED BY")
	for _, r := range rows {
		fmt.Fprintf(out, "%-34s %-9s %-8s %-6s %s\n", r.ID, r.Kind, r.Default, r.Now, r.Where)
	}
	if gerr != nil {
		fmt.Fprintf(out, "\nwarning: %v\nThe global file is ignored until it parses, so every rule it changed is back to its default.\n", gerr)
	}
	return nil
}

// guardChange applies off/on/set to the global file or a workspace manifest.
func guardChange(c *cobra.Command, wsName string, all bool, ids []string, op, action string) error {
	w, err := guardTarget(wsName, all)
	if err != nil {
		return err
	}
	g, gerr := guardcfg.Load()
	if all && gerr != nil {
		return fmt.Errorf("%v (fix or remove the file first)", gerr)
	}
	var m *manifest.Manifest
	if w != nil {
		m = w.Manifest
	}
	known := guardcfg.Known(m, g)
	for _, id := range ids {
		if _, ok := guardcfg.Lookup(id, known); !ok {
			return fmt.Errorf("unknown rule %q: tq guard list shows the rule ids", id)
		}
	}
	before, _, _ := guardRows(w)
	if all {
		applyGlobal(&g, ids, op, action)
		if err := guardcfg.Save(g); err != nil {
			return err
		}
	} else if dropped, err := editManifestGuardCarry(w, ids, op, action); err != nil {
		return err
	} else if dropped != nil {
		fmt.Fprintf(c.ErrOrStderr(), "mode grant dropped (was %s): run tq allow --mode %s %s\n", dropped.Mode, w.Name, dropped.Mode)
	}
	// Re-resolve so the report shows the real effective state.
	if w != nil {
		if w, err = guardTarget(w.Name, false); err != nil {
			return err
		}
	}
	after, _, _ := guardRows(w)
	reportGuardDiff(c, before, after, ids, all, w)
	return nil
}

func applyGlobal(g *guardcfg.File, ids []string, op, action string) {
	for _, id := range ids {
		switch op {
		case "off":
			g.Disable = append(g.Disable, id)
		case "on":
			g.Disable = without(g.Disable, id)
		case "set":
			if g.Actions == nil {
				g.Actions = map[string]policy.Action{}
			}
			if action == "default" {
				delete(g.Actions, id)
			} else {
				g.Actions[id] = policy.Action(action)
			}
		}
	}
	if len(g.Actions) == 0 {
		g.Actions = nil
	}
}

// guardBlock mirrors manifest.Guard for editing; rules stay a raw node so
// they are written back exactly as they were.
type guardBlock struct {
	Disable []string          `yaml:"disable,omitempty"`
	Enable  []string          `yaml:"enable,omitempty"`
	Actions map[string]string `yaml:"actions,omitempty"`
	Rules   yaml.Node         `yaml:"rules,omitempty"`

	Unattended *manifest.Unattended `yaml:"unattended,omitempty"`

	// Guard-mode keys (spec E §7), carried through every rewrite.
	Mode          string                           `yaml:"mode,omitempty"`
	Base          string                           `yaml:"base,omitempty"`
	Categories    map[string]string                `yaml:"categories,omitempty"`
	RulesOverride map[string]string                `yaml:"rules_override,omitempty"`
	Projects      map[string]manifest.GuardProject `yaml:"projects,omitempty"`
	HeadlessMode  string                           `yaml:"headless_mode,omitempty"`
	Codex         *manifest.GuardCodex             `yaml:"codex,omitempty"`
}

// editManifestGuard applies off/on/set for ids to w's guard block through
// rewriteManifestGuard.
func editManifestGuard(w *resolve.Workspace, ids []string, op, action string) error {
	_, err := editManifestGuardCarry(w, ids, op, action)
	return err
}

// editManifestGuardCarry is editManifestGuard that also returns the mode
// grant the rewrite dropped (see rewriteManifestGuardCarry).
func editManifestGuardCarry(w *resolve.Workspace, ids []string, op, action string) (*trust.Grant, error) {
	return rewriteManifestGuardCarry(w, func(gb *guardBlock) error {
		g, gerr := guardcfg.Load()
		for _, id := range ids {
			switch op {
			case "off":
				gb.Disable = appendUniq(gb.Disable, id)
				gb.Enable = without(gb.Enable, id)
			case "on":
				gb.Disable = without(gb.Disable, id)
				// Still off from the global file or decision.disable? Enable it
				// here, which wins for this workspace.
				mm := *w.Manifest
				mm.Guard.Disable = gb.Disable
				mm.Guard.Enable = gb.Enable
				if guardcfg.Merge(g, gerr, &mm, w.Name).Off(id) {
					gb.Enable = appendUniq(gb.Enable, id)
				}
			case "set":
				if action == "default" {
					delete(gb.Actions, id)
				} else {
					if gb.Actions == nil {
						gb.Actions = map[string]string{}
					}
					gb.Actions[id] = action
				}
			}
		}
		return nil
	})
}

// rewriteManifestGuard reads w's guard block, lets mutate change it, writes
// the manifest back (dropping an empty block), validates the result loads,
// and re-trusts it, carrying the mode grant from the old hash to the new
// when the new configuration is no looser (spec E §6.3). It refuses a
// manifest that is not currently trusted, so re-trusting can never approve
// edits the user has not reviewed.
func rewriteManifestGuard(w *resolve.Workspace, mutate func(gb *guardBlock) error) error {
	_, err := rewriteManifestGuardCarry(w, mutate)
	return err
}

// rewriteManifestGuardCarry is rewriteManifestGuard that also returns the
// grant the rewrite dropped (non-nil when the old hash had one and the new
// configuration is looser than it), so callers can tell the human.
func rewriteManifestGuardCarry(w *resolve.Workspace, mutate func(gb *guardBlock) error) (*trust.Grant, error) {
	oldHash, err := rewriteManifestGuardInner(w, mutate)
	if err != nil {
		return nil, err
	}
	return carryRewriteGrant(w, oldHash)
}

// carryRewriteGrant carries the grant of oldHash (the manifest before the
// rewrite) to the manifest now on disk.
func carryRewriteGrant(w *resolve.Workspace, oldHash string) (*trust.Grant, error) {
	newHash, err := trust.HashFile(w.ManifestPath)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(w.ManifestPath)
	if err != nil {
		return nil, err
	}
	g, gerr := guardcfg.Load()
	if gerr != nil {
		g = guardcfg.File{}
	}
	old, carried, err := guardcfg.CarryGrant(oldHash, newHash, m, w.Name, w.Root, g)
	if err != nil || carried {
		return nil, err
	}
	return old, nil
}

func rewriteManifestGuardInner(w *resolve.Workspace, mutate func(gb *guardBlock) error) (string, error) {
	path := w.ManifestPath
	oldHash, err := trust.HashFile(path)
	if err != nil {
		return "", err
	}
	if !trust.IsTrusted(oldHash) {
		return "", fmt.Errorf("%s changed since it was trusted: review it and run tq allow %s first", path, w.Name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", fmt.Errorf("%s: not a YAML mapping", path)
	}
	root := doc.Content[0]
	var gb guardBlock
	gi := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "guard" {
			gi = i
			if err := root.Content[i+1].Decode(&gb); err != nil {
				return "", fmt.Errorf("%s: guard block: %w", path, err)
			}
		}
	}
	if err := mutate(&gb); err != nil {
		return "", err
	}
	var gnode yaml.Node
	if err := gnode.Encode(gb); err != nil {
		return "", err
	}
	empty := len(gb.Disable) == 0 && len(gb.Enable) == 0 && len(gb.Actions) == 0 && gb.Rules.IsZero() && gb.Unattended == nil &&
		gb.Mode == "" && gb.Base == "" && len(gb.Categories) == 0 && len(gb.RulesOverride) == 0 && len(gb.Projects) == 0 && gb.HeadlessMode == "" && gb.Codex == nil
	switch {
	case gi >= 0 && empty:
		root.Content = append(root.Content[:gi], root.Content[gi+2:]...)
	case gi >= 0:
		root.Content[gi+1] = &gnode
	case !empty:
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "guard"}, &gnode)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	_ = enc.Close()
	tmp := path + ".tq-guard.tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return "", err
	}
	if _, err := manifest.Load(tmp); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("the edited manifest would not load, nothing changed: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	newHash, err := trust.HashFile(path)
	if err != nil {
		return "", err
	}
	if err := trust.Allow(newHash); err != nil {
		return "", err
	}
	return oldHash, nil
}

func reportGuardDiff(c *cobra.Command, before, after []guardRow, ids []string, all bool, w *resolve.Workspace) {
	out := c.OutOrStdout()
	where := "every workspace (" + guardcfg.Path() + ")"
	if !all && w != nil {
		where = w.Name + " (" + w.ManifestPath + ", re-trusted)"
	}
	b := map[string]guardRow{}
	for _, r := range before {
		b[r.ID] = r
	}
	sort.Strings(ids)
	for _, a := range after {
		for _, id := range ids {
			if a.ID == id {
				fmt.Fprintf(out, "%s: %s -> %s\n", id, b[id].Now, a.Now)
			}
		}
	}
	fmt.Fprintf(out, "applies to %s; open Claude Code sessions pick it up on their next tool call\n", where)
	for _, id := range ids {
		if strings.HasPrefix(id, "tq/guard-") {
			fmt.Fprintf(out, "warning: %s protects the guard's own settings; with it off, an agent can change them without asking you\n", id)
		}
	}
}

func without(xs []string, x string) []string {
	var out []string
	for _, v := range xs {
		if strings.TrimSpace(v) != x {
			out = append(out, v)
		}
	}
	return out
}

func appendUniq(xs []string, x string) []string {
	for _, v := range xs {
		if strings.TrimSpace(v) == x {
			return xs
		}
	}
	return append(xs, x)
}
