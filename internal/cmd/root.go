// Package cmd wires the tq CLI. Each subcommand lives in its own file.
package cmd

import "github.com/spf13/cobra"

// Version is injected at build time via -ldflags "-X .../internal/cmd.Version=x".
var Version = "dev"

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "tentaqles",
		Short:         "Tentaqles: one terminal identity per workspace folder",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd(), newEnvCmd(), newActivateCmd(), newAllowCmd(), newDenyCmd(), newListCmd(), newRunCmd(), newLoginCmd(), newDoctorCmd(), newStatusCmd(), newBundleCmd(), newProvidersCmd(), newHooksCmd(), newClaudeHookCmd(), newSecretsCmd(), newSecretCmd(), newSettingsCmd(), newStatuslineCmd(), newDotenvCmd(), newMigrateCmd(), newUninstallCmd())
	root.AddCommand(newReachCmd(), newOpenCmd(), newInstallCmd(), newConnectCmd())
	return root
}
