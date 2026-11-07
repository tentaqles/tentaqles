package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/banner"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// Seams for tests: the interactive form and the git global setup.
var (
	reachForm   = runReachForm
	reachRunGit = gitcfg.RunGit
)

func newReachCmd() *cobra.Command {
	var noBanner, noInstall bool
	c := &cobra.Command{
		Use:   "reach [<path>]",
		Short: "Make a folder a client identity, or show the one it already is (human only)",
		Long: "tentaqles reach makes the current folder, or <path>, an isolated client identity. The folder's\n" +
			"name is the identity's name, and its parent folder is where your clients live. In a folder that\n" +
			"already is a client, it shows that client and changes nothing. Once the client is trusted it\n" +
			"installs the CLI of each provider the client can use (--no-install skips this).\n\n" +
			"Human-only: a session is refused, and you confirm before anything is written.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := refuseAgent("tentaqles reach"); err != nil {
				return err
			}
			dir, err := reachDir(args)
			if err != nil {
				return err
			}
			name := filepath.Base(dir)
			show := func() {
				if !banner.Silent(noBanner, os.Getenv) {
					banner.Print(c.OutOrStdout(), banner.Status{Version: Version, Identity: name, Dir: dir},
						banner.Env{Getenv: os.Getenv, GOOS: runtime.GOOS})
				}
			}
			if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
				show()
				return reachShowExisting(c, dir, name)
			}
			if !workspace.NameRe.MatchString(name) {
				return fmt.Errorf("the folder name %q can't name a client: use lowercase letters, digits, dots, dashes and underscores, starting with a letter or digit", name)
			}
			if _, err := os.Stat(paths.ManifestPath(name)); err == nil {
				return fmt.Errorf("a client named %s already exists elsewhere: pick another folder name", name)
			}
			show()
			a, err := reachForm()
			if err != nil {
				return err
			}
			global, _ := reachRunGit("config", "--global", "user.name")
			o, err := a.AddOptions(filepath.Dir(dir), name, reach.GitNameFor(global, a.Email))
			if err != nil {
				return err
			}
			o.RunGit = reachRunGit
			about := fmt.Sprintf("tentaqles reach: about to make %s the client %s (%s), and trust it", dir, name, strings.Join(o.Identities, ", "))
			if err := humanGate(c, "tentaqles reach", about); err != nil {
				return err
			}
			if err := ensureProtectedHome(); err != nil {
				return err
			}
			ws, err := workspace.Add(o)
			if err != nil {
				return err
			}
			if err := signWorkspace(c, ws); err != nil {
				return fmt.Errorf("%s was created but not trusted: %w. Run tq allow %s to finish", dir, err, name)
			}
			w := c.OutOrStdout()
			if !noInstall {
				reachInstall(c, ws)
			}
			fmt.Fprintf(w, "\n%s is ready. It can use: %s\n", name, strings.Join(ws.Manifest.Scope(), ", "))
			reachWarnOther(c, ws.Manifest)
			fmt.Fprintf(w, "Open a new terminal in %s, then start claude and log in with this client's account.\n", dir)
			return nil
		},
	}
	c.Flags().BoolVar(&noBanner, "no-banner", false, "print no banner (TQ_NO_BANNER=1 does the same)")
	c.Flags().BoolVar(&noInstall, "no-install", false, "do not install the CLIs of the client's providers")
	return c
}

// reachInstall installs the CLI of each provider in the new client's scope, one line each (the
// lines of tentaqles install), and goes on past a failure: the client is trusted either way,
// and tentaqles install <provider> retries one. A seam for tests.
var reachInstall = installScope

func installScope(c *cobra.Command, ws *resolve.Workspace) {
	w := c.OutOrStdout()
	cat, err := providers.Embedded()
	if err != nil {
		fmt.Fprintf(w, "No CLI was installed: %v\n", err)
		return
	}
	first := true
	for _, id := range ws.Manifest.Scope() {
		p, ok := cat.Get(id)
		if !ok || p.CLI == nil {
			continue
		}
		if first {
			fmt.Fprintf(w, "\nInstalling the CLIs this client can use (tentaqles install <provider> retries one):\n")
			first = false
		}
		res, err := installProvider(ws, p)
		if err != nil {
			fmt.Fprintln(w, strings.TrimSpace(err.Error()))
			continue
		}
		fmt.Fprintln(w, strings.TrimSpace(res.Line()))
	}
}

// reachDir is the absolute folder a reach acts on: the argument, or the current folder.
func reachDir(args []string) (string, error) {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		return "", fmt.Errorf("%s is not a folder", abs)
	}
	return abs, nil
}

// reachShowExisting reports a folder that already is a client. It reads and changes nothing else.
func reachShowExisting(c *cobra.Command, dir, name string) error {
	w := c.OutOrStdout()
	fmt.Fprintln(w, "This folder is already a client.")
	path := paths.ManifestPath(name)
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(dir, manifest.FileName) // an identity not yet migrated keeps it in the root
	}
	m, err := manifest.Load(path)
	if err != nil {
		fmt.Fprintf(w, "Its manifest can't be read: %v\n", err)
		return nil
	}
	fmt.Fprintf(w, "It can use: %s\n", strings.Join(m.Scope(), ", "))
	reachWarnOther(c, m)
	h, err := trust.HashFile(path)
	if err != nil {
		return err
	}
	if v := trust.Check(name, h); !v.OK {
		fmt.Fprintf(w, "It isn't trusted right now: %s\n", v.Reason)
	}
	return nil
}

// reachWarnOther says plainly what tq can't do for a provider it has no catalog entry for.
func reachWarnOther(c *cobra.Command, m *manifest.Manifest) {
	if len(m.OtherProviders) == 0 {
		return
	}
	fmt.Fprintf(c.OutOrStdout(), "Not managed by tentaqles: %s. The onboarding skill can help with these, but their logins aren't isolated per client, and the guard can't check them.\n",
		strings.Join(m.OtherProviders, ", "))
}

const (
	reachSkip  = "skip"
	reachOther = "other"
)

func huhOptions(opts []reach.Option) []huh.Option[string] {
	out := make([]huh.Option[string], 0, len(opts)+2)
	for _, o := range opts {
		out = append(out, huh.NewOption(o.Label, o.Value))
	}
	return append(out, huh.NewOption("Other", reachOther), huh.NewOption("Skip", reachSkip))
}

// runReachForm asks each question in turn. An Other answer asks for the name.
func runReachForm() (*reach.Answers, error) {
	run := func(f huh.Field) error { return huh.NewForm(huh.NewGroup(f)).Run() }
	a := &reach.Answers{}
	askOther := func(title string) error {
		name := ""
		if err := run(huh.NewInput().Title(title).Value(&name).Validate(reach.ValidateName)); err != nil {
			return err
		}
		a.Other = append(a.Other, strings.TrimSpace(name))
		return nil
	}
	one := func(title string, opts []reach.Option, into *string) error {
		v := reachSkip
		if err := run(huh.NewSelect[string]().Title(title).Options(huhOptions(opts)...).Value(&v)); err != nil {
			return err
		}
		switch v {
		case reachSkip:
		case reachOther:
			return askOther(title + ": which one?")
		default:
			*into = v
		}
		return nil
	}
	many := func(title string, opts []reach.Option, into *[]string) error {
		var vs []string
		if err := run(huh.NewMultiSelect[string]().Title(title).Options(huhOptions(opts)...).Value(&vs)); err != nil {
			return err
		}
		for _, v := range vs {
			switch v {
			case reachSkip:
			case reachOther:
				if err := askOther(title + ": which one?"); err != nil {
					return err
				}
			default:
				*into = append(*into, v)
			}
		}
		return nil
	}
	if err := run(huh.NewInput().Title("Identity email").Value(&a.Email).Validate(reach.ValidateEmail)); err != nil {
		return nil, err
	}
	a.Email = strings.TrimSpace(a.Email)
	for _, step := range []func() error{
		func() error { return one("Git provider", reach.GitOptions, &a.Git) },
		func() error { return many("Cloud providers", reach.CloudOptions, &a.Clouds) },
		func() error { return many("Team and meeting platforms", reach.TeamOptions, &a.Team) },
		func() error { return many("Data platforms", reach.DataOptions, &a.Data) },
		func() error { return one("Task management provider", reach.TaskOptions, &a.Task) },
		func() error { return one("Email provider", reach.EmailOptions, &a.EmailProvider) },
	} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	var browsers []string
	opts := make([]huh.Option[string], 0, 3)
	for _, o := range reach.BrowserOptions {
		opts = append(opts, huh.NewOption(o.Label, o.Value))
	}
	opts = append(opts, huh.NewOption("Skip", reachSkip))
	if err := run(huh.NewMultiSelect[string]().Title("Browsers").Options(opts...).Value(&browsers)); err != nil {
		return nil, err
	}
	for _, name := range browsers {
		if name == reachSkip {
			continue
		}
		profile := "Default"
		if err := run(huh.NewInput().Title(name + " profile for this client").Value(&profile).Validate(reach.ValidateName)); err != nil {
			return nil, err
		}
		a.Browsers = append(a.Browsers, manifest.Browser{Name: name, Profile: strings.TrimSpace(profile)})
	}
	return a, nil
}
