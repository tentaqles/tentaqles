package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/guardplugin"
	"github.com/tentaqles/tentaqles/internal/lobe/cli"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// lobeHooks finds the installed tentaqles plugin that the client's Claude Code loads, and reads its
// hooks/hooks.json. found is false when no plugin is installed for the client.
func lobeHooks(d cli.Deps, client string) (data []byte, found bool, err error) {
	ws, err := cli.Target(d, client)
	if err != nil {
		return nil, false, err
	}
	data, found = pluginHooks(paths.IdentityDir(ws.Name, "claude"))
	return data, found, nil
}

// lobeCodexHooks is the state of the lobe's Codex handler for the client (codexHooksState), "" when the
// client has no codex in its scope. client is resolved as lobeHooks resolves it.
func lobeCodexHooks(d cli.Deps, client string) (string, error) {
	ws, err := cli.Target(d, client)
	if err != nil {
		return "", err
	}
	return codexHooksState(ws), nil
}

// pluginHooks reads the hooks/hooks.json of the tentaqles plugin that the Claude config dir claudeDir
// loads. found is false when no plugin is installed; a plugin without a readable hooks file is found with
// nil data, which has no lobe entries.
func pluginHooks(claudeDir string) (data []byte, found bool) {
	root := guardplugin.PluginRoot(claudeDir)
	if root == "" {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(root, "hooks", "hooks.json"))
	if err != nil {
		return nil, true
	}
	return data, true
}

func newLobeStatusCmd() *cobra.Command {
	var client string
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show the client's memory: store, settings, event counts and hook wiring",
		Long: `Prints what the memory of the client holds: the client, the store's state, the nine settings, the number
of stored events in total and by kind, the day files and the broken lines skipped, and whether the plugin
this client's Claude Code loads has the memory's hooks.

It works on the client of the current folder, or on --client. Inside a Claude Code session --client
is accepted only for the client of the current folder. A refused store is printed as one line that names
store.json or the linked folder, and the command exits 1.

--json prints one compact line: client, store, settings, events and hooks.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			d := lobeDeps()
			data, found, err := lobeHooks(d, client)
			if err != nil {
				return err
			}
			codex, err := lobeCodexHooks(d, client)
			if err != nil {
				return err
			}
			return cli.Status(d, client, asJSON, data, found, codex, c.OutOrStdout())
		},
	}
	c.Flags().StringVar(&client, "client", "", "the client to read (default: the client of the current folder)")
	c.Flags().BoolVar(&asJSON, "json", false, "print one line of compact JSON")
	return c
}

func newLobeEventsCmd() *cobra.Command {
	var client string
	c := &cobra.Command{
		Use:   "events",
		Short: "Print the newest stored event lines of the client, scrubbed",
		Long: `Prints the newest stored event lines of the client as they are on disk, one JSON object per line, oldest
of the selection first. At most 20 lines are printed. An empty log prints nothing and exits 0.

It works on the client of the current folder, or on --client, under the same rules as lobe status.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return cli.Events(lobeDeps(), client, c.OutOrStdout())
		},
	}
	c.Flags().StringVar(&client, "client", "", "the client to read (default: the client of the current folder)")
	return c
}
