package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/setup"
)

// Owned by WS3 (spec E §10.1): `tq mode`, the guard-mode controls. Plain
// `tq mode` and `tq mode log` are read-only and open to agents; every other
// verb is human-only (no agent marker, an interactive terminal), and the
// policy rule tq/mode-change denies them in a tool call first.

func newModeCmd() *cobra.Command {
	var ws, agent string
	var asJSON bool
	c := &cobra.Command{
		Use:   "mode",
		Short: "Show or change the guard mode (default, relaxed, strict, custom)",
		Long: `The guard mode decides what tq does with each rule that matches a tool call:
every rule sits in one category, and a mode gives each category skip, ask,
deny or rule (the rule's own action).

  default   every rule at its own action (what applies when nothing is set)
  relaxed   skip every toggleable category; git-history and system ask
  strict    prod-changes, cloud-destructive and git-history deny; external-writes asks
  custom    a preset (--base) plus per-category values (tq mode toggle)

identity, secrets, self-protection and privacy are locked: enforced in every
mode tq is on in, and never toggled. A mode looser than default needs a
one-time grant (tq allow --mode NAME MODE); without it the session runs at
default. off is never stored: use claude-off, codex-off or tq off.

  tq mode [--ws NAME] [--json] [--agent codex]
  tq mode set default|relaxed|strict|custom [--ws NAME | --project DIR | --global] [--base P]
  tq mode toggle CATEGORY skip|ask|deny|rule [scope]
  tq mode override RULE-ID skip|ask|deny|rule|clear [scope]
  tq mode edit [scope]
  tq mode log [--ws NAME] [--since 24h]

set, toggle, override and edit are for you only: they refuse agent sessions
and need an interactive terminal.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return modeShow(c, ws, asJSON, agent)
		},
	}
	c.Flags().StringVar(&ws, "ws", "", "workspace (default: the current folder's)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the resolved mode as JSON")
	c.Flags().StringVar(&agent, "agent", hookAgentClaude, "claude, or codex to list the asks that become blocks in Codex")

	c.AddCommand(newModeSetCmd(), newModeToggleCmd(), newModeOverrideCmd(), newModeEditCmd(), newModeLogCmd())
	return c
}

// modeScopeFlags are the layer flags shared by the writers.
type modeScopeFlags struct {
	ws, project string
	global      bool
}

func (f *modeScopeFlags) add(c *cobra.Command) {
	c.Flags().StringVar(&f.ws, "ws", "", "workspace (default: the current folder's)")
	c.Flags().StringVar(&f.project, "project", "", "a folder inside the workspace (guard.projects)")
	c.Flags().BoolVar(&f.global, "global", false, "the global ~/.tentaqles/guard.yaml (every workspace and neutral folders)")
}

// target resolves the flags to the workspace (nil for --global) and scope.
func (f modeScopeFlags) target() (*resolve.Workspace, setup.GuardScope, error) {
	if f.global {
		if f.ws != "" || f.project != "" {
			return nil, setup.GuardScope{}, errors.New("use --global alone, without --ws or --project")
		}
		return nil, setup.GuardScope{Global: true}, nil
	}
	w, err := guardTarget(f.ws, false)
	if err != nil {
		return nil, setup.GuardScope{}, err
	}
	sc := setup.GuardScope{}
	if f.project != "" {
		if sc.Project, err = resolveModeProject(w.Root, f.project); err != nil {
			return nil, sc, err
		}
	}
	return w, sc, nil
}

func newModeSetCmd() *cobra.Command {
	var f modeScopeFlags
	var base string
	c := &cobra.Command{
		Use:   "set default|relaxed|strict|custom",
		Short: "Set the guard mode of a layer (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq mode set"); err != nil {
				return err
			}
			return modeWrite(c, f, setup.GuardOp{Kind: setup.GuardOpMode, Mode: args[0], Base: base}, "mode "+args[0])
		},
	}
	f.add(c)
	c.Flags().StringVar(&base, "base", "", "the preset a custom mode starts from: default|relaxed|strict")
	return c
}

func newModeToggleCmd() *cobra.Command {
	var f modeScopeFlags
	c := &cobra.Command{
		Use:   "toggle CATEGORY skip|ask|deny|rule",
		Short: "Set one category; a preset becomes custom with it as base (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq mode toggle"); err != nil {
				return err
			}
			return modeWrite(c, f, setup.GuardOp{Kind: setup.GuardOpCategory, Category: args[0], Value: args[1]}, args[0]+" "+args[1])
		},
	}
	f.add(c)
	return c
}

func newModeOverrideCmd() *cobra.Command {
	var f modeScopeFlags
	c := &cobra.Command{
		Use:   "override RULE-ID skip|ask|deny|rule|clear",
		Short: "Set or clear one rules_override entry (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq mode override"); err != nil {
				return err
			}
			what := args[0] + " " + args[1]
			if args[1] == setup.ClearOverride {
				what = args[0] + " override cleared"
			}
			return modeWrite(c, f, setup.GuardOp{Kind: setup.GuardOpOverride, RuleID: args[0], Value: args[1]}, what)
		},
	}
	f.add(c)
	return c
}

// modeWrite applies op to the flags' layer and reports it.
func modeWrite(c *cobra.Command, f modeScopeFlags, op setup.GuardOp, what string) error {
	w, sc, err := f.target()
	if err != nil {
		return err
	}
	res, err := setup.ApplyGuardOp(w, sc, op)
	if err != nil {
		return err
	}
	return reportModeWrite(c, w, res, what)
}

// reportModeWrite prints what a write did, offers the grant on the TTY when
// the layers now ask for more than the ceiling allows, and re-renders the
// Claude settings and the Codex files.
func reportModeWrite(c *cobra.Command, w *resolve.Workspace, res setup.GuardResult, what string) error {
	out := c.OutOrStdout()
	if w == nil {
		fmt.Fprintf(out, "%s globally; %s written\n", what, guardcfg.Path())
	} else {
		fmt.Fprintf(out, "%s for %s (%s); manifest re-trusted\n", what, w.Name, scopeLabel(res.Scope))
	}
	for _, msg := range res.Warnings {
		fmt.Fprintf(c.ErrOrStderr(), "warning: %s\n", msg)
	}
	if res.Carried {
		fmt.Fprintf(out, "%s grant carried\n", res.CarriedMode)
	}
	if res.Dropped != "" {
		fmt.Fprintf(out, "mode grant dropped (was %s): the change loosens past it\n", res.Dropped)
	}
	if w == nil {
		if res.Looser && res.Grant != nil {
			fmt.Fprintf(out, "looser than default: workspaces and neutral folders stay at default until you run: tq allow --mode --global %s\n", res.Grant.Mode)
		}
		return nil
	}
	fresh, err := guardTarget(w.Name, false)
	if err != nil {
		return err
	}
	if res.Looser && res.Grant != nil {
		mode := res.Grant.Mode
		fmt.Fprintf(out, "grant %s for %s? [y/N] ", mode, w.Name)
		line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
		fmt.Fprintln(out)
		if a := strings.ToLower(strings.TrimSpace(line)); a == "y" || a == "yes" {
			g, err := setup.GrantMode(fresh)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "granted %s for %s\n", g.Mode, w.Name)
		} else {
			fmt.Fprintf(out, "runs at default until you run: tq allow --mode %s %s\n", w.Name, mode)
		}
	}
	renderIdentityFiles(c, fresh)
	return nil
}

func scopeLabel(s string) string {
	if strings.HasPrefix(s, "project:") {
		return "project " + strings.TrimPrefix(s, "project:")
	}
	return s
}

// resolveModeProject resolves p (relative paths against root) to a slash
// path strictly inside root. The root itself, outside paths and missing
// folders are refused.
func resolveModeProject(root, p string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("--project %s: not a folder", abs)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return "", errors.New("--project is outside the workspace")
	}
	rel = filepath.ToSlash(rel)
	switch {
	case rel == ".":
		return "", errors.New("--project is the workspace root: leave --project out")
	case rel == ".." || strings.HasPrefix(rel, "../"):
		return "", fmt.Errorf("--project %s is outside the workspace %s", abs, absRoot)
	}
	return rel, nil
}

// ---- show -------------------------------------------------------------------

type modeJSON struct {
	setup.GuardView
	CodexAsks []string `json:"codex_asks,omitempty"`
}

// modeSubject resolves --ws (or the current folder) for the read-only verbs:
// a trusted workspace, or nil for a neutral folder.
func modeSubject(name string) (*resolve.Workspace, string, error) {
	if name != "" {
		w, err := guardTarget(name, false)
		if err != nil {
			return nil, "", err
		}
		return w, w.Root, nil
	}
	cwd, _ := os.Getwd()
	cfg, err := registry.Load()
	if err != nil {
		return nil, cwd, err
	}
	res := resolve.Resolve(cwd, cfg)
	if res.Workspace == nil && res.Untrusted != nil {
		return nil, cwd, fmt.Errorf("workspace %s is not trusted: review its manifest and run tq allow %s first", res.Untrusted.Name, res.Untrusted.Name)
	}
	return res.Workspace, cwd, nil
}

func modeShow(c *cobra.Command, name string, asJSON bool, agent string) error {
	if agent != hookAgentClaude && agent != hookAgentCodex {
		return fmt.Errorf("--agent must be claude or codex, got %q", agent)
	}
	w, cwd, err := modeSubject(name)
	if err != nil {
		return err
	}
	res := resolveMode(w, cwd, "")
	now := time.Now()
	var view setup.GuardView
	if w != nil {
		g, gerr := guardcfg.Load()
		if gerr != nil {
			g = guardcfg.File{}
		}
		view = setup.GuardViewOf(w.Name, res, setup.WorkspaceFacts(w, g, now))
	} else {
		view = setup.GuardViewOf("", res, setup.GuardFacts{})
	}
	var asks []string
	if agent == hookAgentCodex {
		if w != nil {
			asks = codexAskIDs(res, w.Manifest)
		} else {
			asks = codexAskIDs(res, nil)
		}
	}
	out := c.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(modeJSON{GuardView: view, CodexAsks: asks})
	}
	subject := "this folder (no workspace)"
	if w != nil {
		subject = w.Name
	}
	head := fmt.Sprintf("guard mode for %s: %s", subject, view.Label)
	var paren []string
	if view.Requested != "" && view.Requested != view.Mode {
		paren = append(paren, view.Requested+" requested, not granted")
	} else if s := modeSourceLabel(view.ModeSource, w); s != "" {
		paren = append(paren, "set by "+s)
	}
	if len(paren) > 0 {
		head += " (" + strings.Join(paren, "; ") + ")"
	}
	fmt.Fprintln(out, head)
	if view.Granted {
		at := ""
		if view.GrantedAt != "" {
			at = ", granted " + view.GrantedAt
		}
		fmt.Fprintf(out, "ceiling: %s grant%s\n", view.Ceiling, at)
	} else {
		fmt.Fprintln(out, "ceiling: default (no grant)")
	}
	if view.NeedsGrant && w != nil {
		p := view.Proposal
		if p == "" {
			p = view.Requested
		}
		fmt.Fprintf(out, "looser than the ceiling: the session runs clamped until you run: tq allow --mode %s %s\n", w.Name, p)
	}
	for _, cat := range view.Categories {
		src := cat.Source
		if cat.Locked {
			src = "locked"
		}
		fmt.Fprintf(out, "  %-18s %-5s %s\n", cat.Name, cat.Value, src)
	}
	if len(view.Overrides) == 0 {
		fmt.Fprintln(out, "rules_override: none")
	} else {
		fmt.Fprintln(out, "rules_override:")
		for _, o := range view.Overrides {
			src := ""
			if o.Source != "" {
				src = " (" + o.Source + ")"
			}
			fmt.Fprintf(out, "  %s: %s%s\n", o.ID, o.Value, src)
		}
	}
	if len(view.Clamped) > 0 {
		fmt.Fprintf(out, "clamped to the ceiling: %s\n", strings.Join(view.Clamped, ", "))
	}
	if len(view.Ignored) > 0 {
		fmt.Fprintf(out, "ignored (the repo file tried to loosen): %s\n", strings.Join(view.Ignored, ", "))
	}
	if w != nil {
		fmt.Fprintf(out, "skipped in 24h: %s and %s\n", plural(view.Confirms, "confirm"), plural(view.Blocks, "block"))
	}
	fmt.Fprintf(out, "locked: %s\n", strings.Join(view.Locked, ", "))
	if agent == hookAgentCodex {
		if len(asks) == 0 {
			fmt.Fprintln(out, "asks that become blocks in Codex: none")
		} else {
			fmt.Fprintf(out, "asks that become blocks in Codex (Codex hooks cannot ask; shell calls with a prompt prefix rule still prompt): %s\n", strings.Join(asks, ", "))
		}
	}
	return nil
}

// ---- edit -------------------------------------------------------------------

// modeEditState is what `tq mode edit` shows and returns: the mode, the base
// for custom, and a value for every category.
type modeEditState struct {
	Mode   string
	Base   string
	Values map[string]string
}

type modeEditContext struct {
	Title string // "acme (workspace)"
	In    io.Reader
	Out   io.Writer
}

func (s modeEditState) label() string {
	if s.Mode == guardcfg.ModeCustom {
		b := s.Base
		if b == "" {
			b = guardcfg.ModeDefault
		}
		return "custom (" + b + ")"
	}
	return s.Mode
}

// modeEditDiff lists what changed between two states.
func modeEditDiff(before, after modeEditState) string {
	var b strings.Builder
	if before.label() != after.label() {
		fmt.Fprintf(&b, "mode: %s -> %s\n", before.label(), after.label())
	}
	for _, c := range guardcfg.AllCategories() {
		if guardcfg.Locked(c) {
			continue
		}
		if x, y := before.Values[string(c)], after.Values[string(c)]; x != y {
			fmt.Fprintf(&b, "%s: %s -> %s\n", c, x, y)
		}
	}
	return b.String()
}

// modeEditOp turns an edited state into one table write: a preset when the
// values are the preset's, else custom with the differences from the base.
func modeEditOp(s modeEditState) setup.GuardOp {
	base := s.Mode
	if s.Mode == guardcfg.ModeCustom {
		base = s.Base
	}
	if !guardcfg.ValidBase(base) {
		base = guardcfg.ModeDefault
	}
	bt, _ := guardcfg.Preset(base)
	cats := map[string]string{}
	for _, c := range guardcfg.AllCategories() {
		if guardcfg.Locked(c) {
			continue
		}
		if v, ok := s.Values[string(c)]; ok && v != string(bt.Get(c)) {
			cats[string(c)] = v
		}
	}
	if s.Mode != guardcfg.ModeCustom && len(cats) == 0 {
		return setup.GuardOp{Kind: setup.GuardOpTable, Mode: s.Mode}
	}
	return setup.GuardOp{Kind: setup.GuardOpTable, Mode: guardcfg.ModeCustom, Base: base, Categories: cats}
}

// modeEditForm runs the interactive list (a seam for tests): the preset,
// then one four-way select per category (locked rows shown, not editable),
// then the diff and a confirm. ok false means cancelled.
var modeEditForm = func(c modeEditContext, s modeEditState) (modeEditState, bool, error) {
	mode, base := s.Mode, s.Base
	if base == "" {
		base = guardcfg.ModeDefault
	}
	var modes []huh.Option[string]
	for _, m := range guardcfg.StoredModes() {
		modes = append(modes, huh.NewOption(m, m))
	}
	if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Guard mode for " + c.Title).Options(modes...).Value(&mode))).Run(); err != nil {
		return s, false, err
	}
	if mode == guardcfg.ModeCustom {
		var bases []huh.Option[string]
		for _, p := range guardcfg.PresetNames() {
			bases = append(bases, huh.NewOption(p, p))
		}
		if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Start from").Options(bases...).Value(&base))).Run(); err != nil {
			return s, false, err
		}
	}
	start := s.Values
	switch {
	case mode != guardcfg.ModeCustom && mode != s.Mode:
		t, _ := guardcfg.Preset(mode)
		start = stateValues(t)
	case mode == guardcfg.ModeCustom && (s.Mode != guardcfg.ModeCustom || base != s.Base):
		t, _ := guardcfg.Preset(base)
		start = stateValues(t)
	}
	cats := guardcfg.AllCategories()
	vals := make([]string, len(cats))
	var fields []huh.Field
	var values []huh.Option[string]
	for _, v := range guardcfg.Values() {
		values = append(values, huh.NewOption(string(v), string(v)))
	}
	for i, cat := range cats {
		vals[i] = start[string(cat)]
		if guardcfg.Locked(cat) {
			fields = append(fields, huh.NewNote().Title(string(cat)+" (locked)").Description("enforced in every mode: rule"))
			continue
		}
		fields = append(fields, huh.NewSelect[string]().Title(string(cat)).Options(values...).Value(&vals[i]))
	}
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return s, false, err
	}
	out := modeEditState{Mode: mode, Values: map[string]string{}}
	if mode == guardcfg.ModeCustom {
		out.Base = base
	}
	for i, cat := range cats {
		out.Values[string(cat)] = vals[i]
	}
	diff := modeEditDiff(s, out)
	if diff == "" {
		return out, false, nil
	}
	fmt.Fprint(c.Out, diff)
	ok := false
	if err := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Write this?").Value(&ok))).Run(); err != nil {
		return s, false, err
	}
	return out, ok, nil
}

func stateValues(t guardcfg.Table) map[string]string {
	out := map[string]string{}
	for _, c := range guardcfg.AllCategories() {
		out[string(c)] = string(t.Get(c))
	}
	return out
}

func newModeEditCmd() *cobra.Command {
	var f modeScopeFlags
	c := &cobra.Command{
		Use:   "edit",
		Short: "Edit a layer's mode and categories interactively (human-only)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq mode edit"); err != nil {
				return err
			}
			w, sc, err := f.target()
			if err != nil {
				return err
			}
			st, err := setup.GuardLayer(w, sc)
			if err != nil {
				return err
			}
			before := modeEditState{Mode: st.Mode, Base: st.Base, Values: st.Values}
			title := "every workspace (global)"
			if w != nil {
				title = w.Name + " (" + scopeLabel(sc.String()) + ")"
			}
			after, ok, err := modeEditForm(modeEditContext{Title: title, In: c.InOrStdin(), Out: c.OutOrStdout()}, modeEditState{Mode: before.Mode, Base: before.Base, Values: cloneStrings(before.Values)})
			if err != nil {
				return err
			}
			// Locked rows never change, whatever the form returned.
			for _, cat := range guardcfg.LockedCategories() {
				after.Values[string(cat)] = before.Values[string(cat)]
			}
			if !ok || modeEditDiff(before, after) == "" {
				fmt.Fprintln(c.OutOrStdout(), "nothing changed")
				return nil
			}
			res, err := setup.ApplyGuardOp(w, sc, modeEditOp(after))
			if err != nil {
				return err
			}
			return reportModeWrite(c, w, res, "mode "+after.label())
		},
	}
	f.add(c)
	return c
}

func cloneStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---- log --------------------------------------------------------------------

func newModeLogCmd() *cobra.Command {
	var ws, since string
	c := &cobra.Command{
		Use:   "log",
		Short: "List the confirms and blocks the guard mode skipped or lowered",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return modeLog(c, ws, since)
		},
	}
	c.Flags().StringVar(&ws, "ws", "", "workspace (default: the current folder's)")
	c.Flags().StringVar(&since, "since", "24h", "how far back to list")
	return c
}

func modeLog(c *cobra.Command, ws, since string) error {
	d, err := time.ParseDuration(since)
	if err != nil || d <= 0 {
		return fmt.Errorf("--since must be a positive duration such as 24h, got %q", since)
	}
	w, err := guardTarget(ws, false)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tSCOPE\tMODE\tCLAUDE\tACTION\tRULES\tTOOL\tCOMMAND")
	for _, r := range guardlog.ReadAll(w.Name, time.Now().Add(-d)) {
		from := "ask"
		if r.IsBlock() {
			from = "deny"
		}
		to := r.To
		if to == "" {
			to = "allow"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s -> %s\t%s\t%s\t%s\n",
			r.Time.Format(time.RFC3339), dash(r.Scope), dash(r.Mode), dash(r.ClaudeMode), from, to, strings.Join(r.Rules, ","), dash(r.Tool), r.Command)
	}
	return tw.Flush()
}
