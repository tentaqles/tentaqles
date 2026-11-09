// Package cmd wires the tq CLI. Each subcommand lives in its own file.
package cmd

import "github.com/spf13/cobra"

// Version is injected at build time via -ldflags "-X .../internal/cmd.Version=x".
var Version = "dev"

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "tentaqles",
		Short:         "Tentaqles: one terminal identity per client folder",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newReachCmd(), newAddCmd(), newRemoveCmd(), newDropCmd(), newEnvCmd(), newActivateCmd(), newListCmd(), newRunCmd(), newLoginCmd(),
		newOpenCmd(), newDoctorCmd(), newProvidersCmd(), newHooksCmd(), newClaudeHookCmd(), newCodexHookCmd(), newSettingsCmd(),
		newStatuslineCmd(), newVersionCmd())
	return root
}
