package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/onboard"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/termopen"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// Seams for tests: the git global setup tq add also runs, the terminal launcher and the wizard.
var (
	onboardRunGit       = gitcfg.RunGit
	onboardOpenTerminal = termopen.Open
	onboardWizard       = runOnboardWizard
)

func newOnboardCmd() *cobra.Command {
	var from, base string
	var openTerm bool
	c := &cobra.Command{
		Use:   "onboard [<name>]",
		Short: "Set up a client workspace and its company tools: identity, git, cloud, data, agents, tracker, chat, email",
		Long: "tq onboard extends tq add with a wizard (or --from <yaml>) that also records the company's tracker,\n" +
			"chat, email, data platform and agent CLIs in the manifest. Every question can be skipped. The manifest\n" +
			"holds names only: tq onboard never asks for a token. It leaves the workspace untrusted and prints the\n" +
			"tq allow and login lines for you to run, then offers to open a new terminal in the folder.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if base == "" {
				cfg, err := registry.Load()
				if err != nil {
					return err
				}
				if len(cfg.Bases) == 1 {
					base = cfg.Bases[0]
				}
			}
			var a *onboard.Answers
			interactive := from == ""
			if interactive {
				// A wizard waits on a keyboard; inside an agent session nobody is there to answer.
				if hubAgent() {
					return fmt.Errorf("tq onboard is interactive and this is an agent session; run it in your own terminal, or pass --from <answers.yaml>")
				}
				name := ""
				if len(args) == 1 {
					name = args[0]
				}
				w, err := onboardWizard(name, base)
				if err != nil {
					return err
				}
				a = w.Answers
				openTerm = w.OpenTerminal
			} else {
				loaded, err := onboard.Load(from)
				if err != nil {
					return err
				}
				a = loaded
				if len(args) == 1 {
					a.Name = args[0]
				}
			}
			if a.Base == "" {
				a.Base = base
			}
			if a.Base == "" {
				return fmt.Errorf("--base required (no single registered base; tq init <folder> registers one)")
			}
			o, err := a.AddOptions()
			if err != nil {
				return err
			}
			o.RunGit = onboardRunGit
			ws, err := workspace.Add(o)
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "Created %s (untrusted)\n", ws.Root)
			fmt.Fprintf(w, "Wrote %s (names only, no secrets)\n\nNext, in your own terminal:\n", ws.ManifestPath)
			for _, line := range onboard.NextSteps(ws.Name, ws.Manifest) {
				fmt.Fprintln(w, "  "+line)
			}
			if openTerm {
				openOnboardTerminal(w, ws.Root)
			} else {
				fmt.Fprintf(w, "\nOpen a new terminal in %s to start working there.\n", ws.Root)
			}
			return nil
		},
	}
	c.Flags().StringVar(&from, "from", "", "read the answers from a YAML file instead of asking (non-interactive)")
	c.Flags().StringVar(&base, "base", "", "base folder (default: the only registered base)")
	c.Flags().BoolVar(&openTerm, "open-terminal", false, "with --from: open a new terminal in the folder afterwards")
	return c
}

// openOnboardTerminal opens a new terminal in root. It never closes the current one: that is the
// user's call, so it says so.
func openOnboardTerminal(w io.Writer, root string) {
	if hubAgent() {
		fmt.Fprintf(w, "\nNot opening a terminal from an agent session; open one in %s yourself.\n", root)
		return
	}
	l, err := onboardOpenTerminal(root)
	if err != nil {
		fmt.Fprintf(w, "\nCould not open a terminal (%v); open one in %s yourself.\n", err, root)
		return
	}
	fmt.Fprintf(w, "\nOpened a new terminal in %s (%s).\nClose this one when you are ready; tq never closes a terminal for you.\n",
		root, strings.TrimSpace(l.Name))
}
