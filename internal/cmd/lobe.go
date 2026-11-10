package cmd

import (
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/lobe/hook"
)

// newLobeCmd is the tentaqles lobe group: the commands of the per-client memory.
func newLobeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "lobe",
		Short: "The per-client memory: test setup, and later its commands",
	}
	c.AddCommand(newLobeSandboxCmd(), newLobeSelftestCmd(), newLobeSettingsCmd(), newLobeSetCmd(), newLobeStatusCmd(), newLobeEventsCmd(), newLobeFingerprintCmd())
	return c
}

// newLobeHookCmd is tentaqles lobe-hook: the entry point of the memory's hooks. The program answers a
// lobe-hook run before cobra runs, so this command exists for --help and the reference, and as a
// second way in.
func newLobeHookCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "lobe-hook",
		Short: "The entry point of the memory's hooks (started by the Claude Code plugin)",
	}
	c.AddCommand(&cobra.Command{
		Use:   "claude <event>",
		Short: "Record a Claude Code hook event in the client's memory",
		Long: `The Claude Code plugin starts tentaqles lobe-hook claude <event> with the hook's JSON on stdin.
The command records what the memory needs about the call and prints nothing or a little context.
On any problem it prints nothing and exits 0, so it never blocks a tool call.

The seven event names are session-start, user-prompt-submit, pre-tool-use, post-tool-use,
post-tool-use-failure, stop and session-end.

TQ_LOBE_NO_HOOKS=1 switches the hooks off. A person never needs to run this command.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			_, _ = hook.Main([]string{"lobe-hook", "claude", args[0]}, c.InOrStdin(), c.OutOrStdout())
			return nil
		},
	})
	return c
}
