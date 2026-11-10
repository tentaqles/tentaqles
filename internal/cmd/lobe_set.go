package cmd

import (
	"bytes"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/lobe/cli"
	"github.com/tentaqles/tentaqles/internal/lobe/settings"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

// lobeDeps is what the lobe's command rules need from this process.
func lobeDeps() cli.Deps {
	return cli.Deps{InAgent: insideAgent, Getenv: os.Getenv, Getwd: os.Getwd}
}

// newLobeSettingsCmd is tentaqles lobe settings: the nine settings and where each value comes from.
func newLobeSettingsCmd() *cobra.Command {
	var client string
	var asJSON bool
	c := &cobra.Command{
		Use:   "settings",
		Short: "Print the client's nine lobe settings and where each value comes from",
		Long: "tentaqles lobe settings prints the nine settings of the client, one line each, in the order\n" +
			"capture, recall, lessons, graph, enforce, share, model, documents, retention_days:\n" +
			"enforce=advice (default). A value that is on but cannot act because a key it needs is off says so.\n\n" +
			"--json prints one compact object on one line with the nine keys and their effective values.\n" +
			"It works on the client of the current folder, or --client (a name or a folder). Inside a Claude Code\n" +
			"session it works only on that session's own client.",
		Example: "  tentaqles lobe settings\n  tentaqles lobe settings --json\n  tentaqles lobe settings --client acme",
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return cli.Settings(lobeDeps(), client, asJSON, c.OutOrStdout())
		},
	}
	c.Flags().StringVar(&client, "client", "", "the client to read: its name, or its folder (default: the current folder's client)")
	c.Flags().BoolVar(&asJSON, "json", false, "print one compact JSON object on one line")
	return c
}

// newLobeSetCmd is tentaqles lobe set <key> <value>: changes one lobe setting after a typed yes. It is
// for a person only and refuses inside a Claude Code session.
func newLobeSetCmd() *cobra.Command {
	var client string
	var yes bool
	c := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Change one lobe setting of a client (for a person, after a typed yes)",
		Long: "tentaqles lobe set changes one of the nine settings of the client, in its manifest, under the lobe key.\n" +
			"The value default removes the key, so the setting takes its default again. Only lobe.<key> changes.\n\n" +
			"It lists the change and waits for a typed yes (--yes skips that), then writes the manifest.\n" +
			"It refuses to run from inside a Claude Code session, and it changes nothing there.\n" +
			"It works on the client of the current folder, or --client (a name or a folder).\n\n" +
			"Values: capture, recall, lessons, graph, model and documents take on or off; enforce takes off,\n" +
			"advice or block; share takes private or shared; retention_days takes a whole number from 1 to 365.",
		Example: "  tentaqles lobe set capture off\n  tentaqles lobe set enforce advice --client acme --yes\n  tentaqles lobe set capture default",
		Args:    cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			return lobeSet(c, lobeDeps(), client, args[0], args[1], yes)
		},
	}
	c.Flags().StringVar(&client, "client", "", "the client to change: its name, or its folder (default: the current folder's client)")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for the typed yes")
	return c
}

// lobeSet is the body of tentaqles lobe set. Nothing is written unless the edit is valid, the change is
// confirmed, and the new manifest parses.
func lobeSet(c *cobra.Command, d cli.Deps, client, key, value string, yes bool) error {
	w := c.OutOrStdout()
	if err := cli.RefusePerson(d); err != nil {
		return fmt.Errorf("tentaqles lobe set: %w", err)
	}
	ws, err := cli.Target(d, client)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(ws.ManifestPath)
	if err != nil {
		return err
	}
	out, err := settings.Edit(raw, key, value)
	if err != nil {
		return err
	}
	if bytes.Equal(out, raw) {
		fmt.Fprintln(w, "Nothing changed.")
		return nil
	}
	oldM, err := manifest.Parse(raw, ws.ManifestPath)
	if err != nil {
		return err
	}
	newM, err := manifest.Parse(out, ws.ManifestPath)
	if err != nil {
		return fmt.Errorf("the edited manifest does not parse, nothing was written: %w", err)
	}
	before, _ := settings.FromManifest(oldM).Get(key)
	after, _ := settings.FromManifest(newM).Get(key)
	about := fmt.Sprintf("tentaqles lobe set: about to change the client %s (lobe.%s: %s -> %s) and rewrite its manifest %s",
		ws.Name, key, before.Value, after.Value, ws.ManifestPath)
	if yes {
		fmt.Fprintln(w, about)
	} else if err := confirm(c, about); err != nil {
		return err
	}
	// The manifest may have changed while the question waited for the typed yes. The change was computed
	// from the first read, so a manifest that differs now is not overwritten: nothing is written.
	now, err := os.ReadFile(ws.ManifestPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(now, raw) {
		return fmt.Errorf("tentaqles lobe set: the manifest %s changed meanwhile, so nothing was written; run the command again", ws.ManifestPath)
	}
	if err := writeFileAtomic(ws.ManifestPath, out, 0o600); err != nil {
		return err
	}
	for _, line := range settings.Reminders(settings.FromManifest(newM)) {
		fmt.Fprintln(w, line)
	}
	return nil
}
