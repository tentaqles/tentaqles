package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/paths"
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
		Long: `tq allow <name> trusts the workspace as it is now: it renders the identity's
settings, then signs the integrity record over the manifest and the root
marker, the enforcing keys of the identity's Claude settings.json, the
installed tentaqles plugin's hooks, scripts and manifest, the Codex hook files
and the shims (tq hooks verify lists them). An edit to any of them is
untrusted until a human signs again (tq allow, or tq hooks verify --accept).

Human-only: an agent session (Claude Code, Codex, a plugin hook) is refused,
so a session can never trust a folder itself; you confirm before the trust
is written.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := refuseAgent("tq allow"); err != nil {
				return err
			}
			_, err := trustWorkspace(c, args[0], "tq allow")
			return err
		},
	}
}

// trustWorkspace signs name's current state as trusted, once the human
// confirms (what names the command in the gate: tq allow, or tq hooks
// verify --accept). The identity files are rendered first, for a workspace
// that is not trusted yet too, because the record covers them
// (settings.json#tq, the Codex hook files); the record is then signed once,
// over the files as rendered; the git includes, which list only trusted
// workspaces, are synced last.
func trustWorkspace(c *cobra.Command, name, what string) (*resolve.Workspace, error) {
	ws, err := findWorkspace(name)
	if err != nil {
		return nil, err
	}
	about := fmt.Sprintf("%s: about to render %s's identity settings, sign its integrity record over the manifest (%s) and the hook files as they are now in %s, and rewrite the git include list",
		what, ws.Name, ws.Hash[:12], trust.SignedRecordPath(ws.Name))
	if err := humanGate(c, what, about); err != nil {
		return nil, err
	}
	return ws, signWorkspace(c, ws)
}

// signWorkspace is trustWorkspace past the gate: render, sign, sync the git
// includes. tq hooks verify --accept --all gates once and signs several.
func signWorkspace(c *cobra.Command, ws *resolve.Workspace) error {
	renderIdentityFilesFor(c, ws, true)
	if err := trust.Allow(ws.Name, ws.Hash); err != nil {
		return err
	}
	if err := syncGitIncludes(); err != nil {
		return err
	}
	fmt.Fprintf(c.OutOrStdout(), "Trusted %s (%s)\n", ws.Name, ws.Hash[:12])
	return nil
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
			about := fmt.Sprintf("tq deny: about to remove %s's trust record from %s and rewrite the git include list", ws.Name, paths.TrustDir())
			if err := humanGate(c, "tq deny", about); err != nil {
				return err
			}
			if err := trust.Deny(ws.Name, ws.Hash); err != nil {
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
