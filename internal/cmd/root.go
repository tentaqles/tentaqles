// Package cmd wires the tq CLI. Each subcommand lives in its own file.
package cmd

import "github.com/spf13/cobra"

// Version is injected at build time via -ldflags "-X .../internal/cmd.Version=x".
var Version = "dev"

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "tq",
		Short:         "Tentaqles: one terminal identity per workspace folder",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	return root
}
