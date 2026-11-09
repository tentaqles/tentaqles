package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/lobe/selftest"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The values a passing self test measures.
const (
	selftestLines   = 10000
	selftestRepoKey = "r_a172cedc"
)

var errSelftestNoClient = errors.New("tentaqles lobe selftest: run it inside a client's folder")

// newLobeSelftestCmd is tentaqles lobe selftest. The three hidden flags make the command a writer child
// of a self test run: it appends to the file --writer under the lock, for the client of the current
// folder, and exits.
func newLobeSelftestCmd() *cobra.Command {
	var writer string
	var writerID, count int
	c := &cobra.Command{
		Use:   "selftest",
		Short: "Prove the memory's store works on this machine, with the installed program",
		Long: `tentaqles lobe selftest checks that the memory's store works on this machine, with the installed program,
for the client folder you run it in. It starts twenty writer processes that each append five hundred lines
to one file under the store lock, then checks that all ten thousand lines are whole and present. It replaces
one file a thousand times while a reader reads it, and checks that no read returns part of a file. It prints
the key the store gives this folder's app subfolder.

All of it runs in a scratch folder under the client's memory folder, which is removed at the end. The command
prints three lines and exits 0 only when every check passes:

  writers=20 lines=10000 torn=0 lost=0
  replaces=1000 partial=0
  repo-key app=r_a172cedc

Testers may run it. A person does not need to.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := lobeSelftestWorkspace()
			if err != nil {
				return err
			}
			if ws == nil {
				return errSelftestNoClient
			}
			st, err := store.Open(ws.Name, store.ReadWrite)
			if err != nil {
				return err
			}
			if writer != "" {
				return selftest.WriterChild(st, writer, writerID, count)
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			r, err := selftest.Run(st, ws.Root, exe, func(rel string, w, n int) []string {
				return []string{"lobe", "selftest", "--writer", rel, "--writer-id", strconv.Itoa(w), "--count", strconv.Itoa(n)}
			})
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			fmt.Fprintf(out, "writers=%d lines=%d torn=%d lost=%d\n", r.Writers, r.Lines, r.Torn, r.Lost)
			fmt.Fprintf(out, "replaces=%d partial=%d\n", r.Replaces, r.Partial)
			fmt.Fprintf(out, "repo-key app=%s\n", r.RepoKeyApp)
			if r.Lines != selftestLines || r.Torn != 0 || r.Lost != 0 || r.Partial != 0 || r.RepoKeyApp != selftestRepoKey {
				return errors.New("tentaqles lobe selftest: a check failed")
			}
			return nil
		},
	}
	c.Flags().StringVar(&writer, "writer", "", "")
	c.Flags().IntVar(&writerID, "writer-id", 0, "")
	c.Flags().IntVar(&count, "count", 0, "")
	for _, f := range []string{"writer", "writer-id", "count"} {
		_ = c.Flags().MarkHidden(f)
	}
	return c
}

// lobeSelftestWorkspace is the client of the current folder, or nil when the folder is in no client.
func lobeSelftestWorkspace() (*resolve.Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	return resolve.Resolve(cwd, cfg).Workspace, nil
}
