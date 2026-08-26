package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

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
	var bypass bool
	c := &cobra.Command{
		Use:   "allow <name>",
		Short: "Trust a workspace's current manifest so it can export env",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := findWorkspace(args[0])
			if err != nil {
				return err
			}
			if err := trust.Allow(ws.Hash); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Trusted %s (%s)\n", ws.Name, ws.Hash[:12])
			if bypass {
				if err := trust.AllowBypass(ws.Hash); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "Bypass permissions allowed for %s\n", ws.Name)
				if ws.Manifest.HasCloudIdentity() {
					fmt.Fprintln(c.ErrOrStderr(), "warning: bypass + cloud identity — Claude can run cloud CLIs unattended in this workspace")
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&bypass, "bypass", false, "also allow claude permission_mode: bypass")
	return c
}

func newDenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny <name>",
		Short: "Revoke trust for a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := findWorkspace(args[0])
			if err != nil {
				return err
			}
			if err := trust.Deny(ws.Hash); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Denied %s\n", ws.Name)
			return nil
		},
	}
}
