package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// syncGitIncludes rewrites the managed git include file so it lists exactly the
// currently-trusted workspaces. Called after every trust change.
func syncGitIncludes() error {
	cfg, err := registry.Load()
	if err != nil {
		return err
	}
	return workspace.SyncGit(cfg)
}

func findWorkspace(name string) (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	all, errs := resolve.ListWorkspaces(cfg)
	for _, w := range all {
		if w.Name == name {
			w := w
			return &w, nil
		}
	}
	return nil, fmt.Errorf("no workspace %q (errors: %v)", name, errs)
}

func newAllowCmd() *cobra.Command {
	var bypass, mode, global bool
	c := &cobra.Command{
		Use:   "allow <name> | allow --mode <name> <relaxed|custom|default> | allow --mode --global <mode>",
		Short: "Trust a workspace's current manifest so it can export env; --mode grants a guard mode looser than default",
		Long: `tq allow <name> trusts the workspace manifest as it is now (a hash). A mode
grant is tied to that hash too: trusting an edited manifest drops it, and tq
says so.

tq allow --mode <name> relaxed|custom grants the workspace a guard mode looser
than default (the ceiling, spec E): relaxed is the relaxed preset; custom is
the workspace's custom table as tq mode shows it now (root and projects).
tq allow --mode <name> default removes the grant. --global grants (or
removes) the mode of ~/.tentaqles/guard.yaml for neutral folders.

--mode is human-only: it refuses agent sessions and needs a terminal.
--bypass <name> is a deprecated alias of --mode <name> relaxed.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			switch {
			case bypass && (mode || global):
				return errors.New("--bypass is an alias of --mode <name> relaxed: use one")
			case bypass:
				if len(args) != 1 {
					return errors.New("usage: tq allow --bypass <name>")
				}
				fmt.Fprintf(c.ErrOrStderr(), "deprecated: use tq allow --mode %s relaxed\n", args[0])
				return allowMode(c, args[0], guardcfg.ModeRelaxed)
			case global && !mode:
				return errors.New("--global goes with --mode: tq allow --mode --global <mode>")
			case mode && global:
				if len(args) != 1 {
					return errors.New("usage: tq allow --mode --global <relaxed|custom|default>")
				}
				return allowGlobalMode(c, args[0])
			case mode:
				if len(args) != 2 {
					return errors.New("usage: tq allow --mode <name> <relaxed|custom|default>")
				}
				return allowMode(c, args[0], args[1])
			}
			if len(args) != 1 {
				return errors.New("usage: tq allow <name>")
			}
			ws, err := trustWorkspace(c, args[0])
			if err != nil {
				return err
			}
			reportDroppedGrants(c, ws)
			// Always re-render: trusting a new manifest drops the grant for
			// it, so the Codex block and profile must go.
			renderCodex(c, ws, false)
			return nil
		},
	}
	c.Flags().BoolVar(&mode, "mode", false, "grant a guard mode looser than default: tq allow --mode <name> relaxed|custom|default (human-only)")
	c.Flags().BoolVar(&global, "global", false, "with --mode: grant the mode of ~/.tentaqles/guard.yaml (neutral folders)")
	c.Flags().BoolVar(&bypass, "bypass", false, "deprecated: tq allow --mode <name> relaxed")
	return c
}

// trustWorkspace trusts name's current manifest and syncs the git includes.
func trustWorkspace(c *cobra.Command, name string) (*resolve.Workspace, error) {
	ws, err := findWorkspace(name)
	if err != nil {
		return nil, err
	}
	if err := trust.Allow(ws.Hash); err != nil {
		return nil, err
	}
	if err := syncGitIncludes(); err != nil {
		return nil, err
	}
	fmt.Fprintf(c.OutOrStdout(), "Trusted %s (%s)\n", ws.Name, ws.Hash[:12])
	return ws, nil
}

// reportDroppedGrants tells the human that re-trusting an edited manifest
// dropped its mode grant (grants are keyed by the manifest hash), and removes
// the stale grant files so the message is said once.
func reportDroppedGrants(c *cobra.Command, ws *resolve.Workspace) {
	if guardcfg.WorkspaceGrant(ws.Hash, ws.Name, ws.Root) != nil {
		return
	}
	for _, kg := range trust.GrantsFor(ws.Name) {
		if kg.Key == ws.Hash {
			continue
		}
		fmt.Fprintf(c.ErrOrStderr(), "mode grant dropped (was %s): run tq allow --mode %s %s\n", kg.Grant.Mode, ws.Name, kg.Grant.Mode)
		_ = trust.DeleteGrant(kg.Key)
	}
}

func validGrantMode(m string) error {
	switch m {
	case guardcfg.ModeRelaxed, guardcfg.ModeCustom, guardcfg.ModeDefault:
		return nil
	case guardcfg.ModeStrict:
		return errors.New("strict is never looser than default and needs no grant")
	case guardcfg.ModeOff:
		return errors.New("off is never granted: use claude-off, tq run --off or tq off")
	}
	return fmt.Errorf("mode must be relaxed, custom or default (removes the grant), got %q", m)
}

// allowMode trusts the workspace's manifest and grants (or, for default,
// removes) its mode grant.
func allowMode(c *cobra.Command, name, m string) error {
	if err := validGrantMode(m); err != nil {
		return err
	}
	if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq allow --mode"); err != nil {
		return err
	}
	// A grant is for a manifest the human already reviewed: tq allow --mode
	// never trusts an edited one on the way (security review m5), the same
	// rule tq mode set follows.
	ws, err := findWorkspace(name)
	if err != nil {
		return err
	}
	if !trust.IsTrusted(ws.Hash) {
		return fmt.Errorf("%s changed since it was trusted (or was never trusted): review it and run tq allow %s first, then tq allow --mode %s %s", ws.ManifestPath, ws.Name, ws.Name, m)
	}
	if m == guardcfg.ModeDefault {
		if err := trust.DeleteGrant(ws.Hash); err != nil {
			return err
		}
		fmt.Fprintf(c.OutOrStdout(), "Mode grant removed for %s: it runs at default or stricter\n", ws.Name)
		renderCodex(c, ws, false)
		return nil
	}
	g, gerr := guardcfg.Load()
	if gerr != nil {
		g = guardcfg.File{}
	}
	gr, ok := guardcfg.GrantFor(ws.Manifest, ws.Name, ws.Root, g, m)
	if !ok {
		return fmt.Errorf("cannot grant %q", m)
	}
	if err := trust.SaveGrant(ws.Hash, gr); err != nil {
		return err
	}
	fmt.Fprintf(c.OutOrStdout(), "Granted %s for %s: it may run up to that loose\n", m, ws.Name)
	if ws.Manifest.HasCloudIdentity() {
		fmt.Fprintln(c.ErrOrStderr(), "warning: "+m+" + cloud identity — Claude can run cloud CLIs without a confirm in this workspace")
	}
	renderCodex(c, ws, false)
	return nil
}

// allowGlobalMode grants (or removes) the mode of the global guard.yaml,
// pinned to its current mode keys.
func allowGlobalMode(c *cobra.Command, m string) error {
	if err := validGrantMode(m); err != nil {
		return err
	}
	if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq allow --mode --global"); err != nil {
		return err
	}
	if m == guardcfg.ModeDefault {
		if err := trust.DeleteGrant(trust.GlobalGrantKey); err != nil {
			return err
		}
		fmt.Fprintln(c.OutOrStdout(), "Global mode grant removed")
		return nil
	}
	g, err := guardcfg.Load()
	if err != nil {
		return err
	}
	gr, ok := guardcfg.GlobalGrantFor(g, m)
	if !ok {
		return fmt.Errorf("cannot grant %q", m)
	}
	if err := trust.SaveGrant(trust.GlobalGrantKey, gr); err != nil {
		return err
	}
	fmt.Fprintf(c.OutOrStdout(), "Granted %s for %s (neutral folders); editing its mode keys drops the grant\n", m, guardcfg.Path())
	return nil
}

func newDenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny <name>",
		Short: "Revoke trust for a workspace (and its mode grant)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := findWorkspace(args[0])
			if err != nil {
				return err
			}
			if err := trust.Deny(ws.Hash); err != nil {
				return err
			}
			if err := syncGitIncludes(); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Denied %s\n", ws.Name)
			// The grant is gone: remove the Codex unattended settings.
			renderCodex(c, ws, false)
			return nil
		},
	}
}
