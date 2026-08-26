package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/workspace"
)

func newAddCmd() *cobra.Command {
	var o workspace.AddOptions
	var ids string
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a workspace folder with its manifest, identity dirs and git identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			o.Name = args[0]
			o.Identities = strings.Split(strings.ReplaceAll(ids, " ", ""), ",")
			o.RunGit = gitcfg.RunGit
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
	c.Flags().StringVar(&ids, "identities", "claude,gh", "comma list: claude,codex,gemini,cursor,gh,az,aws,gcloud,kube,npm")
	c.Flags().StringVar(&o.PermissionMode, "permission-mode", "", "claude permission_mode: default|acceptEdits|plan|bypass")
	return c
}
