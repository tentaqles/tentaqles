package cmd

import "github.com/spf13/cobra"

// newLobeCmd is the tentaqles lobe group: the commands of the per-client memory.
func newLobeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "lobe",
		Short: "The per-client memory: test setup, and later its commands",
	}
	c.AddCommand(newLobeSandboxCmd())
	return c
}
