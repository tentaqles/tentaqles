package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

func newBindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bind",
		Short: "Bind the client in this folder to this folder",
		Long: "tentaqles records the folder each client was created in, and only that folder is the client. A copy\n" +
			"of the client's folder somewhere else is refused.\n\n" +
			"Run tentaqles bind inside a client's folder after you moved it to another clients folder, or for a\n" +
			"client created before folders were recorded. It shows the recorded folder and this folder and waits\n" +
			"for yes typed back. It refuses to run from inside a Claude Code session.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if insideAgent() {
				return errors.New("tentaqles bind: run it in your own terminal, not from a Claude Code session")
			}
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			name, root, err := resolve.Claim(cwd, cfg)
			if err != nil {
				return fmt.Errorf("tentaqles bind: this folder is not a client's folder: %v", err)
			}
			w := c.OutOrStdout()
			rec, ok := cfg.Folder(name)
			if ok && registry.SamePath(rec, root) {
				fmt.Fprintf(w, "tentaqles bind: client %s is already bound to %s; nothing to do.\n", name, root)
				return nil
			}
			recorded := rec
			if !ok {
				recorded = "none (created before folders were recorded)"
			}
			fmt.Fprintf(w, "tentaqles bind: client %s\n", name)
			fmt.Fprintf(w, "  recorded folder: %s\n", recorded)
			fmt.Fprintf(w, "  this folder:     %s\n", root)
			fmt.Fprintf(w, "After this, only this folder is the client %s. Any other folder that holds its marker is refused.\n", name)
			fmt.Fprint(w, "Type yes to continue: ")
			line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
			if strings.TrimSpace(line) != "yes" {
				fmt.Fprintln(w)
				return errAborted
			}
			if err := cfg.Bind(name, root); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			if err := workspace.SyncGit(cfg); err != nil {
				return err
			}
			fmt.Fprintf(w, "tentaqles bind: %s is bound to %s.\n", name, root)
			return nil
		},
	}
}
