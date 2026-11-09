package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tentaqles/tentaqles"
)

// Commit is the full git commit this tq was built from, injected by the release build
// (.goreleaser.yaml: -X .../internal/cmd.Commit={{.FullCommit}}); "" for a dev build.
var Commit = ""

func newVersionCmd() *cobra.Command {
	var notices bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Print the tentaqles version and the commit it was built from",
		Long: "Print the tentaqles version and the commit it was built from.\n\n" +
			"With --notices, print the NOTICE file, the tentaqles LICENSE, and the license text of every\n" +
			"project tentaqles draws on. These files are built into the binary.",
		RunE: func(c *cobra.Command, _ []string) error {
			if notices {
				return printNotices(c.OutOrStdout())
			}
			commit := Commit
			if commit == "" {
				commit = "unknown (dev build)"
			}
			_, err := fmt.Fprintf(c.OutOrStdout(), "tentaqles %s (commit %s)\n", Version, commit)
			return err
		},
	}
	c.Flags().BoolVar(&notices, "notices", false,
		"print the NOTICE file, the tentaqles LICENSE, and the license texts of the projects tentaqles draws on")
	return c
}

// printNotices writes NOTICE, the tentaqles LICENSE, then each file under third_party/licenses in
// sorted path order, each under a `==== <path> ====` heading.
func printNotices(w io.Writer) error {
	if err := writeNoticeFile(w, "", "NOTICE"); err != nil {
		return err
	}
	if err := writeNoticeFile(w, "\n==== LICENSE (tentaqles) ====\n", "LICENSE"); err != nil {
		return err
	}
	var paths []string
	err := fs.WalkDir(tentaqles.Notices, "third_party/licenses", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := writeNoticeFile(w, fmt.Sprintf("\n==== %s ====\n", p), p); err != nil {
			return err
		}
	}
	return nil
}

// writeNoticeFile writes an optional heading (empty for none), then the embedded file's text, ending with a newline.
func writeNoticeFile(w io.Writer, heading, name string) error {
	if heading != "" {
		if _, err := io.WriteString(w, heading); err != nil {
			return err
		}
	}
	raw, err := tentaqles.Notices.ReadFile(name)
	if err != nil {
		return err
	}
	text := string(raw)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	_, err = io.WriteString(w, text)
	return err
}
