package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/bundle"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func newBundleCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "bundle",
		Short: "Manage Claude bundles (plugins, skills, MCP servers) from a shared catalog",
	}
	c.AddCommand(newBundleSyncCmd(), newBundleDiffCmd(), newBundleCaptureCmd(), newBundleCatalogCmd())
	return c
}

func newBundleSyncCmd() *cobra.Command {
	var force, asJSON bool
	c := &cobra.Command{
		Use:   "sync <workspace>",
		Short: "Materialize a workspace's claude.bundle into its Claude identity dir",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := findWorkspace(args[0])
			if err != nil {
				return err
			}
			if !trust.IsTrusted(ws.Hash) {
				return fmt.Errorf("workspace %s is untrusted; run: tq allow %s", ws.Name, ws.Name)
			}
			cat, err := bundle.LoadCatalog()
			if err != nil {
				return err
			}
			rep, err := bundle.Sync(ws, cat, bundle.Options{Force: force})
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(c.OutOrStdout()).Encode(rep)
			}
			fmt.Fprintf(c.OutOrStdout(), "settings changed: %v\n", rep.Settings)
			fmt.Fprintf(c.OutOrStdout(), "skills synced: %s\n", strings.Join(rep.Skills, ", "))
			fmt.Fprintf(c.OutOrStdout(), "mcp servers synced: %s\n", strings.Join(rep.MCP, ", "))
			for _, w := range rep.Warnings {
				fmt.Fprintln(c.ErrOrStderr(), "warning:", w)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "sync even if Claude appears to be running with this config dir")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newBundleDiffCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "diff <workspace>",
		Short: "Show drift between a workspace's claude.bundle and its Claude identity dir",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := findWorkspace(args[0])
			if err != nil {
				return err
			}
			cat, err := bundle.LoadCatalog()
			if err != nil {
				return err
			}
			drifts, err := bundle.Diff(ws, cat)
			if err != nil {
				return err
			}
			if asJSON {
				if err := json.NewEncoder(c.OutOrStdout()).Encode(drifts); err != nil {
					return err
				}
			} else {
				for _, d := range drifts {
					line := fmt.Sprintf("%s %s", d.Kind, d.Name)
					if d.Detail != "" {
						line += " — " + d.Detail
					}
					fmt.Fprintln(c.OutOrStdout(), line)
				}
			}
			if len(drifts) > 0 {
				exitFunc(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newBundleCaptureCmd() *cobra.Command {
	var dir string
	var writeCatalog bool
	c := &cobra.Command{
		Use:   "capture [workspace]",
		Short: "Reconstruct a claude.bundle manifest fragment and catalog entries from an existing Claude identity dir",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			d := dir
			if d == "" {
				if len(args) == 0 {
					return fmt.Errorf("specify a workspace name or --dir <path>")
				}
				ws, err := findWorkspace(args[0])
				if err != nil {
					return err
				}
				d = paths.IdentityDir(ws.Name, "claude")
			}
			captured, err := bundle.Capture(d)
			if err != nil {
				return err
			}
			fmt.Fprint(c.OutOrStdout(), captured.BundleYAML())
			for _, w := range captured.Warnings {
				fmt.Fprintln(c.ErrOrStderr(), "warning:", w)
			}
			if writeCatalog {
				cat, err := bundle.LoadCatalog()
				if err != nil {
					return err
				}
				for name, mp := range captured.Catalog.Marketplaces {
					if _, exists := cat.Marketplaces[name]; !exists {
						cat.Marketplaces[name] = mp
					}
				}
				for name, sk := range captured.Catalog.Skills {
					if _, exists := cat.Skills[name]; !exists {
						cat.Skills[name] = sk
					}
				}
				for name, srv := range captured.Catalog.MCP {
					if _, exists := cat.MCP[name]; !exists {
						cat.MCP[name] = srv
					}
				}
				if err := cat.Save(); err != nil {
					return err
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "Claude identity dir to capture from (default: the workspace's own)")
	c.Flags().BoolVar(&writeCatalog, "write-catalog", false, "upsert captured entries into the catalog (never overwrites existing names)")
	return c
}

func newBundleCatalogCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "catalog",
		Short: "Show the bundle catalog's path, contents and validation warnings",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cat, err := bundle.LoadCatalog()
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "%s\n", paths.Catalog())
			fmt.Fprintf(c.OutOrStdout(), "marketplaces: %d\n", len(cat.Marketplaces))
			fmt.Fprintf(c.OutOrStdout(), "skills: %d\n", len(cat.Skills))
			fmt.Fprintf(c.OutOrStdout(), "mcp: %d\n", len(cat.MCP))
			for _, w := range cat.Validate() {
				fmt.Fprintln(c.ErrOrStderr(), "warning:", w)
			}
			return nil
		},
	}
	return c
}
