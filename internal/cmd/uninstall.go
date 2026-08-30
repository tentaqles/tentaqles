package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/migrate"
)

// uninstallRunner supplies the external commands Restore needs (git, reg.exe).
// Tests replace it with fakes.
var uninstallRunner = migrate.DefaultRunner

func newUninstallCmd() *cobra.Command {
	var (
		restore string
		yes     bool
	)
	c := &cobra.Command{
		Use:   "uninstall",
		Short: "Undo a tq migration by replaying its journal backwards",
		Long: "tq uninstall --restore [<ts>|latest] replays a migration journal in reverse:\n" +
			"identity directories move back, shell profiles and ~/.gitconfig are restored\n" +
			"from their byte-exact backups, and registry values go back with their original\n" +
			"types. It lists what it would undo and does nothing until you add --yes.\n\n" +
			"Removing tq's own hooks and global include is a separate job that this version\n" +
			"does not do.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			out := c.OutOrStdout()
			if !c.Flags().Changed("restore") {
				return errors.New("only --restore is implemented in this version")
			}
			// --restore's NoOptDefVal means `--restore <ts>` parses as the
			// bare flag plus a positional; accept both spellings.
			ts := strings.TrimSpace(restore)
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				ts = strings.TrimSpace(args[0])
			}
			if ts == "" {
				ts = "latest"
			}

			j, err := migrate.Load(ts)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "journal: %s\n", j.Dir)

			// Newest first, which is the order Restore reverses them in.
			ents := append([]migrate.Entry(nil), j.Entries...)
			sort.SliceStable(ents, func(a, b int) bool { return ents[a].Seq > ents[b].Seq })
			fmt.Fprintf(out, "restoring %s from %s, newest first:\n", entryCount(len(ents)), j.TS)
			for _, e := range ents {
				fmt.Fprintf(out, "  < %-*s  %s\n", kindWidth, string(e.Op), entrySummary(e))
			}

			if !yes {
				fmt.Fprintln(out, "dry run — nothing changed. Re-run with --yes.")
				return nil
			}

			fmt.Fprintln(out, "undoing:")
			lines, rerr := j.Restore(uninstallRunner())
			for _, l := range lines {
				fmt.Fprintf(out, "  %s\n", l)
			}
			if rerr != nil {
				fmt.Fprintf(c.ErrOrStderr(), "restore stopped after %d of %d entries: %v\n", len(lines), len(ents), rerr)
				exitFunc(1)
				return nil
			}
			fmt.Fprintf(out, "restored %d entries\n", len(lines))
			return nil
		},
	}
	c.Flags().StringVar(&restore, "restore", "", `undo the migration journal for <ts> (or "latest")`)
	c.Flags().Lookup("restore").NoOptDefVal = "latest"
	c.Flags().BoolVar(&yes, "yes", false, "actually replay the journal (without this it only lists what it would undo)")
	return c
}

func entryCount(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

// entrySummary says, in the user's terms, what reversing one entry does. It
// reads the journal's own argument names rather than any display string, so it
// stays true to what Restore will actually do.
func entrySummary(e migrate.Entry) string {
	a := e.Args
	switch e.Op {
	case migrate.OpMoveDir:
		return a["To"] + " -> move back to " + a["From"]
	case migrate.OpMakeLink:
		return a["Path"] + " -> remove the link to " + a["Target"]
	case migrate.OpRemoveLink:
		return a["Path"] + " -> recreate the link to " + a["Target"]
	case migrate.OpWriteFile, migrate.OpDeleteFile:
		if a["Backup"] == "" {
			return a["Path"] + " -> delete (it did not exist before)"
		}
		return fmt.Sprintf("%s -> restore %s bytes from %s", a["Path"], a["Bytes"], a["Backup"])
	case migrate.OpGitGlobalSet:
		if a["Present"] == "true" {
			return a["Key"] + " -> " + strconv.Quote(a["Old"])
		}
		return a["Key"] + " -> (unset)"
	case migrate.OpRegSet:
		k := a["Key"] + `\` + a["Name"]
		if a["Present"] == "true" {
			return fmt.Sprintf("%s -> %s (%s)", k, strconv.Quote(a["Old"]), a["Type"])
		}
		return k + " -> (delete)"
	}
	return fmt.Sprintf("%v", a)
}
