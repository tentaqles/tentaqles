package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// addRunGit is the seam tests use to fake git for `tq add`: the real runner
// writes the global git config, which a test must never touch.
var addRunGit = gitcfg.RunGit

func newAddCmd() *cobra.Command {
	var o workspace.AddOptions
	var ids string
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a workspace folder with its manifest, identity dirs and git identity (human only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			// Human-only: a session never scaffolds a folder, so it can never
			// get one trusted; the human who confirms gets it trusted.
			if err := refuseAgent("tq add"); err != nil {
				return err
			}
			o.Name = args[0]
			for _, id := range strings.Split(strings.ReplaceAll(ids, " ", ""), ",") {
				if id != "" {
					o.Identities = append(o.Identities, id)
				}
			}
			o.RunGit = addRunGit
			o.Trust = true
			if o.Base == "" {
				cfg, err := registry.Load()
				if err != nil {
					return err
				}
				if len(cfg.Bases) != 1 {
					return fmt.Errorf("--base required (registered bases: %v)", cfg.Bases)
				}
				o.Base = cfg.Bases[0]
			}
			about := fmt.Sprintf("tq add: about to create %s (marker, git identity, trusted) and %s (manifest, identity dirs)", filepath.Join(o.Base, o.Name), filepath.Join(paths.IdentitiesRoot(), o.Name))
			if err := humanGate(c, "tq add", about); err != nil {
				return err
			}
			// The manifest goes under $TQ_HOME (ADR-0002): create it private
			// to the user first (on Windows: the one icacls run).
			if err := ensureProtectedHome(); err != nil {
				return err
			}
			ws, err := workspace.Add(o)
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "Created %s (trusted)\n", ws.Root)
			for _, id := range ws.Manifest.IdentityNames() {
				fmt.Fprintf(w, "  login: tq login %s %s\n", ws.Name, id)
			}
			return nil
		},
	}
	c.Flags().StringVar(&o.Base, "base", "", "base folder (default: the only registered base)")
	c.Flags().StringVar(&o.GitEmail, "git-email", "", "git user.email for this workspace (required)")
	c.Flags().StringVar(&o.GitName, "git-name", "", "git user.name")
	c.Flags().StringVar(&o.DisplayName, "display-name", "", "human name")
	c.Flags().StringVar(&o.Color, "color", "", "tab color hint, e.g. #e0432f")
	c.Flags().StringVar(&o.GitProvider, "git-provider", "", "git host: github|gitlab|azure-devops|bitbucket (decides which CLI an expected_user is checked against)")
	c.Flags().StringVar(&ids, "identities", "claude,gh", "comma list: claude,codex,gemini,cursor,gh,az,aws,gcloud,kube,npm")
	return c
}
