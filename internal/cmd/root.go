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
	root.AddCommand(newVersionCmd(), newEnvCmd(), newActivateCmd(), newInitCmd(), newAddCmd(), newAllowCmd(), newDenyCmd(), newListCmd(), newRunCmd(), newLoginCmd(), newDoctorCmd(), newBundleCmd(), newProvidersCmd(), newHooksCmd(), newSetupCmd(), newClaudeHookCmd(), newSecretsCmd(), newSecretCmd(), newSettingsCmd(), newStatuslineCmd(), newWorktreesCmd(), newInsightsCmd(), newDecideCmd(), newDotenvCmd(), newRedactCmd(), newGuardCmd(), newMigrateCmd(), newUninstallCmd(), newHubCmd(), newUnattendedCmd())
	root.AddCommand(newOnboardCmd()) // spec B §8; own line so other workstreams' additions merge cleanly
	root.AddCommand(newModeCmd())    // spec E §10.1 (WS3)
	return root
}
