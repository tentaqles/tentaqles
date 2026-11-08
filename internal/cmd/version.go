package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Commit is the full git commit this tq was built from, injected by the release build
// (.goreleaser.yaml: -X .../internal/cmd.Commit={{.FullCommit}}); "" for a dev build.
var Commit = ""

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tentaqles version and the commit it was built from",
		RunE: func(c *cobra.Command, _ []string) error {
			commit := Commit
			if commit == "" {
				commit = "unknown (dev build)"
			}
			_, err := fmt.Fprintf(c.OutOrStdout(), "tentaqles %s (commit %s)\n", Version, commit)
			return err
		},
	}
}
