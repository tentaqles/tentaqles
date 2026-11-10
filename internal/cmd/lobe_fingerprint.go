package cmd

import (
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/lobe/cli"
)

// newLobeFingerprintCmd is tentaqles lobe fingerprint --tool <name>: the fingerprint and the template of
// an error text read from stdin. It needs no client, no store and no login, so it runs everywhere.
func newLobeFingerprintCmd() *cobra.Command {
	var tool string
	c := &cobra.Command{
		Use:   "fingerprint",
		Short: "Print the fingerprint and the template of an error text read from stdin",
		Long: `Reads an error text from stdin, scrubs it as the memory's hook does, and prints two lines: the fingerprint
(fp_ and 16 hex digits) and the template the fingerprint is made from. Two errors that differ only in paths,
numbers, ids or quoted values print the same fingerprint.

--tool names the tool that failed (Bash, Edit, mcp__server__tool and so on). The tool class is part of the
fingerprint, so the same text from another tool gives another one.

The verb reads no store and resolves no client, so it works in any folder and inside a Claude Code session.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return cli.Fingerprint(tool, c.InOrStdin(), c.OutOrStdout())
		},
	}
	c.Flags().StringVar(&tool, "tool", "", "the name of the tool that failed, for example Bash or mcp__server__tool (required)")
	return c
}
