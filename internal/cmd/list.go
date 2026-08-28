package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func newListCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List workspaces under all registered bases",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			all, errs := resolve.ListWorkspaces(cfg)
			type row struct {
				Name, Root, Email string
				Trusted           bool
				Identities        []string
			}
			rows := make([]row, 0, len(all))
			for _, w := range all {
				rows = append(rows, row{w.Name, w.Root, w.Manifest.Git.Email, trust.IsTrusted(w.Hash), w.Manifest.IdentityNames()})
			}
			if asJSON {
				return json.NewEncoder(c.OutOrStdout()).Encode(map[string]any{"bases": cfg.Bases, "workspaces": rows, "errors": fmt.Sprint(errs)})
			}
			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tTRUSTED\tEMAIL\tIDENTITIES\tROOT")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%v\t%s\t%s\t%s\n", r.Name, r.Trusted, r.Email, strings.Join(r.Identities, ","), r.Root)
			}
			tw.Flush()
			for _, e := range errs {
				fmt.Fprintln(c.ErrOrStderr(), "warning:", e)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}
