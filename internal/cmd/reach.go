package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/banner"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/guardplugin"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// Seams for tests: the interactive form, the menu of an existing client, the part of the form it
// asks, the git global setup, and the plugin install.
var (
	reachForm        = runReachForm
	reachMenu        = runReachMenu
	reachEditForm    = runReachEditForm
	reachRunGit      = gitcfg.RunGit
	reachGuardPlugin = installGuardPlugin
)

func newReachCmd() *cobra.Command {
	var noBanner, noPlugin bool
	c := &cobra.Command{
		Use:   "reach [<path>]",
		Short: "Make a folder a client identity, or change the one it already is",
		Long: "tentaqles reach makes the current folder, or <path>, an isolated client identity. The folder's\n" +
			"name is the client's name, and its parent folder is where your clients live. It asks for the\n" +
			"client's email, its git host, its clouds, team, data and task tools, its email provider and its\n" +
			"browser profile, and writes them to the client's manifest. A terminal opened in the folder, or in\n" +
			"any subfolder, then carries that client's identity: its own Claude Code config home (and so its own\n" +
			"subscription), its git name and email, its own login for each CLI, its browser profile, and a PATH\n" +
			"without the CLIs of providers the client does not use.\n\n" +
			"For a new client it writes tq's activation block into the profile of each shell tq detects, and\n" +
			"installs the tentaqles plugin into the client's Claude config (--no-plugin skips this): the plugin's\n" +
			"hook blocks a git, gh or cloud command that would run under another identity.\n\n" +
			"In a folder that already is a client, it shows the client and offers to change its providers, its\n" +
			"browser and profile, or its email, through the same questions.\n\n" +
			"Nothing is written before you type yes.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if insideAgent() {
				return errors.New("tentaqles reach: run it in your own terminal, not from a Claude Code session")
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
				return reachShowExisting(c, name)
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
			profiles := hooks.ProfilesFn()
			shells := reachShells(profiles)
			about := fmt.Sprintf("tentaqles reach: about to make %s the client %s (%s)", dir, name, strings.Join(o.Identities, ", "))
			if missing := reachMissing(shells, profiles); len(missing) > 0 {
				about += ", and write the tq activation block into " + profilesOf(missing, profiles)
			}
			if err := confirm(c, about); err != nil {
				return err
			}
			ws, err := workspace.Add(o)
			if err != nil {
				return err
			}
			renderIdentityFiles(c, ws)
			w := c.OutOrStdout()
			if noPlugin {
				fmt.Fprintf(w, "The tentaqles plugin was not installed (--no-plugin). Claude won't start a session for %s until it is. Run these, in this order:\n  %s\n",
					name, strings.Join(guardplugin.GuardInstallLines(name), "\n  "))
			} else {
				reachGuardPlugin(c, ws)
			}
			reachHooks(c, shells, profiles)
			fmt.Fprintf(w, "\n%s is ready. It can use: %s\n", ws.Name, strings.Join(ws.Manifest.Scope(), ", "))
			reachWarnOther(c, ws.Manifest)
			fmt.Fprintf(w, "Open a new terminal in %s, then start claude and log in with this client's account.\n", dir)
			return nil
		},
	}
	c.Flags().BoolVar(&noBanner, "no-banner", false, "print no banner (TQ_NO_BANNER=1 does the same)")
	c.Flags().BoolVar(&noPlugin, "no-plugin", false, "do not install the tentaqles plugin into the client's Claude config")
	return c
}

// confirm prints about and asks for a typed yes on the command's input; anything else aborts with
// errAborted.
func confirm(c *cobra.Command, about string) error {
	fmt.Fprintln(c.OutOrStdout(), about)
	fmt.Fprint(c.OutOrStdout(), "Type yes to continue: ")
	line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
	if strings.TrimSpace(strings.ToLower(line)) != "yes" {
		fmt.Fprintln(c.OutOrStdout())
		return errAborted
	}
	return nil
}

// errAborted is a declined confirmation: nothing was written.
var errAborted = errors.New("aborted: nothing was written")

// reachPluginTimeout bounds each `claude plugin` command of reach. A seam for tests.
var reachPluginTimeout = 2 * time.Minute

// reachPluginRun runs one claude command with env and returns its stdout and stderr, apart. A seam
// for tests, which run a fake claude.
var reachPluginRun = func(ctx context.Context, name string, args, env []string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// The two commands that install the plugin. Their arguments are constants: nothing from the
// manifest, the environment or the folder reaches them.
var (
	guardMarketplaceArgs = []string{"plugin", "marketplace", "add", guardplugin.GuardMarketplace}
	guardInstallArgs     = []string{"plugin", "install", guardplugin.GuardPlugin}
)

// installGuardPlugin installs the tentaqles plugin into the client's Claude config, and says why
// and what to run when it cannot. It never fails reach: tentaqles run refuses a session without the
// plugin in the meantime.
func installGuardPlugin(c *cobra.Command, ws *resolve.Workspace) {
	w := c.OutOrStdout()
	if err := runGuardPluginInstall(ws); err != nil {
		fmt.Fprintf(w, "The tentaqles plugin was not installed: %v\n", err)
		fmt.Fprintf(w, "Claude won't start a session for %s until it is. Run these, in this order:\n  %s\n",
			ws.Name, strings.Join(guardplugin.GuardInstallLines(ws.Name), "\n  "))
		return
	}
	fmt.Fprintf(w, "The tentaqles plugin is installed: every session of %s runs its identity check.\n", ws.Name)
}

// runGuardPluginInstall adds the marketplace and installs the plugin with ws's Claude config dir. A
// failed marketplace add does not stop the install: the marketplace may be there already.
func runGuardPluginInstall(ws *resolve.Workspace) error {
	env := envplan.Environ(ws, os.Environ())
	claude, err := envplan.LookPath("claude", env)
	if err != nil {
		return errors.New("claude was not found on PATH")
	}
	addErr := runClaudePlugin(claude, guardMarketplaceArgs, env)
	if err := runClaudePlugin(claude, guardInstallArgs, env); err != nil {
		if addErr != nil {
			return fmt.Errorf("%v; and %v", addErr, err)
		}
		return err
	}
	return nil
}

// runClaudePlugin runs one claude plugin command under reachPluginTimeout and names it in the
// error, with the first line of its error output.
func runClaudePlugin(claude string, args, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), reachPluginTimeout)
	defer cancel()
	stdout, stderr, err := reachPluginRun(ctx, claude, args, env)
	what := "claude " + strings.Join(args, " ")
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s did not finish within %s", what, reachPluginTimeout)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %s", what, firstLine(string(stderr), string(stdout), err))
	}
	return nil
}

// firstLine is the first non-empty line of the error output, else of the output, else err.
func firstLine(stderr, stdout string, err error) string {
	for _, s := range []string{stderr, stdout} {
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				return l
			}
		}
	}
	return err.Error()
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

// reachShowExisting reports a folder that already is a client and offers to change its setup
// through the same form.
func reachShowExisting(c *cobra.Command, name string) error {
	w := c.OutOrStdout()
	fmt.Fprintln(w, "This folder is already a client.")
	ws, err := findWorkspace(name)
	if err != nil {
		fmt.Fprintf(w, "Its manifest can't be read: %v\n", err)
		return nil
	}
	fmt.Fprintf(w, "It can use: %s\n", strings.Join(ws.Manifest.Scope(), ", "))
	reachWarnOther(c, ws.Manifest)
	part, err := reachMenu()
	if err != nil {
		return err
	}
	if part == reachPartNothing {
		return nil
	}
	return reachEdit(c, ws, part)
}

// reachEdit changes one part of an existing client's setup: it asks the part, shows what changes,
// asks for the typed yes, rewrites the manifest, makes the new identities' folders, points the
// folder's git at a new email, and renders the client's files again. Nothing is written before the
// yes.
func reachEdit(c *cobra.Command, ws *resolve.Workspace, part string) error {
	cur := reach.AnswersFrom(ws.Manifest)
	next, err := reachEditForm(part, cur)
	if err != nil {
		return err
	}
	return applyEdit(c, "tentaqles reach", ws, cur, next, nil, false)
}

// applyEdit is the shared tail of reach's menu and of tentaqles add and remove: it builds the edited
// manifest (reach.Edit), lists every change and asks for the typed yes (skipped with yes), then
// rewrites the manifest, makes the new identities' folders, points the folder's git at a new email and
// renders the client's files again. Nothing is written before the yes.
func applyEdit(c *cobra.Command, what string, ws *resolve.Workspace, cur, next *reach.Answers, drop []string, yes bool) error {
	w := c.OutOrStdout()
	raw, err := os.ReadFile(ws.ManifestPath)
	if err != nil {
		return err
	}
	out, changes, added, removed, err := reach.Edit(raw, cur, next, drop...)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Fprintln(w, "Nothing changed.")
		return nil
	}
	about := fmt.Sprintf("%s: about to change the client %s (%s) and rewrite its manifest %s",
		what, ws.Name, strings.Join(changes, "; "), ws.ManifestPath)
	if yes {
		fmt.Fprintln(w, about)
	} else if err := confirm(c, about); err != nil {
		return err
	}
	if err := writeFileAtomic(ws.ManifestPath, out, 0o600); err != nil {
		return err
	}
	for _, id := range added {
		if err := os.MkdirAll(paths.IdentityDir(ws.Name, id), 0o700); err != nil {
			return err
		}
	}
	if next.Email != cur.Email {
		if err := gitcfg.WriteWorkspace(ws.Root, ws.Manifest.Git.Name, next.Email); err != nil {
			return fmt.Errorf("the manifest names %s, but the folder's git config was not updated: %w", next.Email, err)
		}
	}
	updated, err := findWorkspace(ws.Name)
	if err != nil {
		return err
	}
	renderIdentityFiles(c, updated)
	for _, id := range added {
		fmt.Fprintf(w, "%s has its own login folder now: log in with tentaqles login %s %s\n", id, ws.Name, id)
	}
	for _, id := range removed {
		fmt.Fprintf(w, "%s is no longer in this client's scope. Its folder %s stays, with any login in it; delete it if you no longer need it.\n",
			id, paths.IdentityDir(ws.Name, id))
	}
	fmt.Fprintf(w, "\n%s is updated. It can use: %s\n", ws.Name, strings.Join(updated.Manifest.Scope(), ", "))
	reachWarnOther(c, updated.Manifest)
	fmt.Fprintf(w, "Open a new terminal in %s (or cd out of it and back in), and restart its claude sessions, for the change to apply.\n", ws.Root)
	return nil
}

// writeFileAtomic replaces p with b through a temporary file in the same folder and a rename.
func writeFileAtomic(p string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// reachWarnOther says plainly what tq can't do for a provider it has no catalog entry for.
func reachWarnOther(c *cobra.Command, m *manifest.Manifest) {
	if len(m.OtherProviders) == 0 {
		return
	}
	fmt.Fprintf(c.OutOrStdout(), "Not managed by tentaqles: %s. Their logins aren't isolated per client.\n",
		strings.Join(m.OtherProviders, ", "))
}

// reachDetectShells are the shells whose profile reach checks (every shell tentaqles hooks install
// detects). Tests replace it.
var reachDetectShells = func(p hooks.Profiles) []hooks.Shell { return hooks.Detect(p, hooks.LookPath) }

// reachShells are the shells whose profile reach makes hold the tq block. A shell without a profile
// is left out.
func reachShells(p hooks.Profiles) []hooks.Shell {
	var out []hooks.Shell
	for _, sh := range reachDetectShells(p) {
		if p[sh] != "" && !slices.Contains(out, sh) {
			out = append(out, sh)
		}
	}
	return out
}

// reachMissing are the shells whose profile has no tq block yet: the confirmation names their
// profiles.
func reachMissing(shells []hooks.Shell, p hooks.Profiles) []hooks.Shell {
	var out []hooks.Shell
	for _, sh := range shells {
		if st := hooks.StatusOf(sh, p).State; st == "missing" || st == "no profile" {
			out = append(out, sh)
		}
	}
	return out
}

// reachHooks writes the tq activation block into each shell's profile, after the typed yes. A
// profile that already has the block is left alone; a failure is a warning naming the shell.
func reachHooks(c *cobra.Command, shells []hooks.Shell, p hooks.Profiles) {
	w := c.OutOrStdout()
	for _, sh := range shells {
		before := hooks.StatusOf(sh, p).State
		st, err := hooks.Install(sh, p)
		if err != nil {
			fmt.Fprintf(w, "Warning: %s: the tq activation block could not be written: %v. A new %s terminal would not carry the client's identity.\n", sh, err, sh)
			continue
		}
		switch {
		case (before == "missing" || before == "no profile") && st.State == "installed":
			fmt.Fprintf(w, "The tentaqles activation block is in %s.\n", st.Profile)
		case before == "outdated" && st.State == "installed":
			fmt.Fprintf(w, "The tentaqles activation block in %s is updated.\n", st.Profile)
		}
	}
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

// errNoTerminal refuses the interactive form when stdin or stdout is not a terminal: the form would
// otherwise wait on the console for an answer no one can give (a CI runner, a pipe).
var errNoTerminal = errors.New("tentaqles reach needs a terminal: run it in your own terminal")

// interactive reports whether stdin and stdout are terminals.
func interactive() bool {
	return xterm.IsTerminal(os.Stdin.Fd()) && xterm.IsTerminal(os.Stdout.Fd())
}

// runReachForm asks each question in turn. An Other answer asks for the name.
func runReachForm() (*reach.Answers, error) {
	if !interactive() {
		return nil, errNoTerminal
	}
	a := &reach.Answers{}
	for _, part := range []string{reachPartEmail, reachPartProviders, reachPartBrowsers} {
		if err := askReachPart(part, a); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// The parts of the form, which the menu of an existing client offers one at a time.
const (
	reachPartEmail     = "email"
	reachPartProviders = "providers"
	reachPartBrowsers  = "browsers"
	reachPartNothing   = "nothing"
)

// askReachPart asks one part of the form, starting from what a already holds (an existing client's
// answers, or none) and writing the new answers into it.
func askReachPart(part string, a *reach.Answers) error {
	run := func(f huh.Field) error { return huh.NewForm(huh.NewGroup(f)).Run() }
	switch part {
	case reachPartEmail:
		email := a.Email
		if err := run(huh.NewInput().Title("Identity email").Value(&email).Validate(reach.ValidateEmail)); err != nil {
			return err
		}
		a.Email = strings.TrimSpace(email)
		return nil
	case reachPartBrowsers:
		return askBrowsers(run, a)
	case reachPartProviders:
		return askProviders(run, a)
	}
	return fmt.Errorf("no form part %q", part)
}

func askProviders(run func(huh.Field) error, a *reach.Answers) error {
	// Names typed under Other before are kept unless the developer unticks them.
	if len(a.Other) > 0 {
		var opts []huh.Option[string]
		for _, n := range a.Other {
			opts = append(opts, huh.NewOption(n, n).Selected(true))
		}
		keep := slices.Clone(a.Other)
		if err := run(huh.NewMultiSelect[string]().Title("Providers tentaqles does not manage: keep").Options(opts...).Value(&keep)); err != nil {
			return err
		}
		a.Other = keep
	}
	askOther := func(title string) error {
		name := ""
		if err := run(huh.NewInput().Title(title).Value(&name).Validate(reach.ValidateName)); err != nil {
			return err
		}
		a.Other = append(a.Other, strings.TrimSpace(name))
		return nil
	}
	// one and many start on the current choice (Skip when there is none) and replace it.
	one := func(title string, opts []reach.Option, into *string) error {
		v := reachSkip
		if *into != "" {
			v = *into
		}
		if err := run(huh.NewSelect[string]().Title(title).Options(huhOptions(opts)...).Value(&v)); err != nil {
			return err
		}
		*into = ""
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
		vs := slices.Clone(*into)
		if err := run(huh.NewMultiSelect[string]().Title(title).Options(huhOptions(opts)...).Value(&vs)); err != nil {
			return err
		}
		*into = nil
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
	for _, step := range []func() error{
		func() error { return one("Git provider", reach.GitOptions, &a.Git) },
		func() error { return many("Cloud providers", reach.CloudOptions, &a.Clouds) },
		func() error { return many("Team and meeting platforms", reach.TeamOptions, &a.Team) },
		func() error { return many("Data platforms", reach.DataOptions, &a.Data) },
		func() error { return one("Task management provider", reach.TaskOptions, &a.Task) },
		func() error { return one("Email provider", reach.EmailOptions, &a.EmailProvider) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func askBrowsers(run func(huh.Field) error, a *reach.Answers) error {
	current := map[string]string{}
	var browsers []string
	for _, b := range a.Browsers {
		current[b.Name] = b.Profile
		browsers = append(browsers, b.Name)
	}
	opts := make([]huh.Option[string], 0, 3)
	for _, o := range reach.BrowserOptions {
		opts = append(opts, huh.NewOption(o.Label, o.Value))
	}
	opts = append(opts, huh.NewOption("Skip", reachSkip))
	if err := run(huh.NewMultiSelect[string]().Title("Browsers").Options(opts...).Value(&browsers)); err != nil {
		return err
	}
	a.Browsers = nil
	for _, name := range browsers {
		if name == reachSkip {
			continue
		}
		profile := "Default"
		if p, ok := current[name]; ok {
			profile = p
		}
		if err := run(huh.NewInput().Title(name + " profile for this client").Value(&profile).Validate(reach.ValidateName)); err != nil {
			return err
		}
		a.Browsers = append(a.Browsers, manifest.Browser{Name: name, Profile: strings.TrimSpace(profile)})
	}
	return nil
}

// runReachMenu asks what to change in a client that exists. Nothing is the default.
func runReachMenu() (string, error) {
	if !interactive() {
		return "", errNoTerminal
	}
	v := reachPartNothing
	err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("This folder is already a client. What do you want to change?").Options(
		huh.NewOption("Nothing, just show it", reachPartNothing),
		huh.NewOption("Add or remove providers", reachPartProviders),
		huh.NewOption("The browser and its profile", reachPartBrowsers),
		huh.NewOption("The email", reachPartEmail),
	).Value(&v))).Run()
	return v, err
}

// runReachEditForm asks one part of the form for an existing client, starting from its answers.
func runReachEditForm(part string, cur *reach.Answers) (*reach.Answers, error) {
	if !interactive() {
		return nil, errNoTerminal
	}
	a := *cur
	a.Clouds, a.Team, a.Data = slices.Clone(cur.Clouds), slices.Clone(cur.Team), slices.Clone(cur.Data)
	a.Browsers, a.Other = slices.Clone(cur.Browsers), slices.Clone(cur.Other)
	if err := askReachPart(part, &a); err != nil {
		return nil, err
	}
	return &a, nil
}
