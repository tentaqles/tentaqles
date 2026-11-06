package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// Commit is the full git commit this tq was built from, injected by the
// release build (.goreleaser.yaml: -X .../internal/cmd.Commit={{.FullCommit}});
// "" for a dev build. It is the published release tq hooks verify --accept
// --all compares a new plugin folder with (trust.ReleaseCommit): an anchor
// inside the signed binary, not in any file a session could write.
var Commit = ""

// The link-time value is in place before package init runs.
func init() { trust.ReleaseCommit = Commit }

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tq version and the commit it was built from",
		RunE: func(c *cobra.Command, _ []string) error {
			commit := Commit
			if commit == "" {
				commit = "unknown (dev build)"
			}
			_, err := fmt.Fprintf(c.OutOrStdout(), "tq %s (commit %s)\n", Version, commit)
			return err
		},
	}
}
