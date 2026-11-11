package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/lobe/cli"
)

// newLobeEventsPurgeCmd is tentaqles lobe events purge: deletes the client's event and drops day files. It
// is for a person only and refuses inside a Claude Code session.
func newLobeEventsPurgeCmd() *cobra.Command {
	var client string
	var yes bool
	c := &cobra.Command{
		Use:   "purge",
		Short: "Delete the client's event and drops day files (for a person, after a typed yes)",
		Long: "tentaqles lobe events purge deletes every events-YYYYMMDD.jsonl and drops-YYYYMMDD.jsonl file of the\n" +
			"client's event log, under the store lock. Nothing else is deleted: not keep.txt, nothing outside the\n" +
			"events folder, nothing of another client.\n\n" +
			"It lists how many files go and waits for a typed yes (--yes skips that), then deletes them.\n" +
			"It refuses to run from inside a Claude Code session, and it deletes nothing there.\n" +
			"It works on the client of the current folder, or --client (a name or a folder).",
		Example: "  tentaqles lobe events purge\n  tentaqles lobe events purge --client acme --yes",
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return lobeEventsPurge(c, lobeDeps(), client, yes)
		},
	}
	c.Flags().StringVar(&client, "client", "", "the client to purge: its name, or its folder (default: the current folder's client)")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for the typed yes")
	return c
}

// lobeEventsPurge is the body of tentaqles lobe events purge. The person check comes first, before the
// client is read; cli.Purge then asks, and deletes only after a yes.
func lobeEventsPurge(c *cobra.Command, d cli.Deps, client string, yes bool) error {
	if err := cli.RefusePerson(d); err != nil {
		return fmt.Errorf("tentaqles lobe events purge: %w", err)
	}
	w := c.OutOrStdout()
	ask := func(about string) error {
		if yes {
			fmt.Fprintln(w, about)
			return nil
		}
		return confirm(c, about)
	}
	return cli.Purge(d, client, ask, w)
}
