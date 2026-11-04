package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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
	return &cobra.Command{
		Use:   "allow <name>",
		Short: "Trust a workspace's current manifest so it can export env (human only)",
		Long: `tq allow <name> trusts the workspace manifest as it is now (a hash). An
edited manifest is untrusted until tq allow runs again.

Human-only: an agent session (Claude Code, Codex, a plugin hook) is refused,
so a session can never trust a folder itself.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := refuseAgent("tq allow"); err != nil {
				return err
			}
			ws, err := trustWorkspace(c, args[0])
			if err != nil {
				return err
			}
			// Re-render the identity files for the manifest as trusted now.
			renderIdentityFiles(c, ws)
			return nil
		},
	}
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

func newDenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny <name>",
		Short: "Revoke trust for a workspace (human only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := refuseAgent("tq deny"); err != nil {
				return err
			}
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
			renderIdentityFiles(c, ws)
			return nil
		},
	}
}
