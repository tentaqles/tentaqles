package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/banner"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/install"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/termopen"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// Seams for tests: the interactive form and the git global setup.
var (
	reachForm   = runReachForm
	reachRunGit = gitcfg.RunGit
)

func newReachCmd() *cobra.Command {
	var noBanner, noInstall, noPlugin, noTerminal bool
	c := &cobra.Command{
		Use:   "reach [<path>]",
		Short: "Make a folder a client identity, or show the one it already is (human only)",
		Long: "tentaqles reach makes the current folder, or <path>, an isolated client identity. The folder's\n" +
			"name is the identity's name, and its parent folder is where your clients live. In a folder that\n" +
			"already is a client, it shows that client and changes nothing. Once the client is trusted it\n" +
			"installs the tentaqles plugin into the client's Claude config (--no-plugin skips this): the plugin\n" +
			"holds the guard, and claude won't start a session for the client without it. Then it installs the\n" +
			"CLI of each provider the client can use (--no-install skips this).\n\n" +
			"For a new client it also writes tq's activation block into the shell profile of each shell tq\n" +
			"detects and of the shell it runs in, after your yes (the confirmation says so). Then it opens a new\n" +
			"terminal in the client's folder and ends the terminal it ran in, after you press Enter. It ends\n" +
			"only that terminal, and only when it is an interactive shell; otherwise it leaves it open and says\n" +
			"so. --no-terminal skips both.\n\n" +
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
			profiles := hooks.ProfilesFn()
			parentShell := reachParentShell()
			shells := reachShells(profiles, parentShell)
			about := fmt.Sprintf("tentaqles reach: about to make %s the client %s (%s), and trust it", dir, name, strings.Join(o.Identities, ", "))
			if missing := reachMissing(shells, profiles); len(missing) > 0 {
				about += "; and write the tq activation block into " + profilesOf(missing, profiles)
			}
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
			if noPlugin {
				fmt.Fprintf(w, "The tentaqles plugin was not installed (--no-plugin). Claude won't start a session for %s until it is. Run these, in this order:\n  %s\n",
					name, strings.Join(trust.GuardInstallLines(name), "\n  "))
			} else {
				reachGuardPlugin(c, ws)
			}
			if !noInstall {
				reachInstall(c, ws)
			}
			hs := reachHooks(c, shells, profiles, parentShell)
			reachFinish(c, ws, dir, noTerminal, hs)
			return nil
		},
	}
	c.Flags().BoolVar(&noBanner, "no-banner", false, "print no banner (TQ_NO_BANNER=1 does the same)")
	c.Flags().BoolVar(&noInstall, "no-install", false, "do not install the CLIs of the client's providers")
	c.Flags().BoolVar(&noPlugin, "no-plugin", false, "do not install the tentaqles plugin (the guard) into the client's Claude config")
	c.Flags().BoolVar(&noTerminal, "no-terminal", false, "do not open a terminal in the client's folder, nor end the one reach runs in")
	return c
}

// reachInstall installs the CLI of each provider in the new client's scope, one line each (the
// lines of tentaqles install), and goes on past a failure: the client is trusted either way,
// and tentaqles install <provider> retries one. A seam for tests.
var reachInstall = installScope

// reachGuardPlugin installs the tentaqles plugin into the client's Claude config once the trust is
// signed (REQ-1), and says why and what to run when it cannot (REQ-2). It never fails reach: the
// refusal of tq run is what keeps a session unguarded in the meantime (ADR-0018). A seam for tests.
var reachGuardPlugin = installGuardPlugin

// reachPluginTimeout bounds each `claude plugin` command of reach (REQ-1). A seam for tests.
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

// The two commands that install the guard. Their arguments are constants: nothing from the
// manifest, the environment or the folder reaches them (S-04).
var (
	guardMarketplaceArgs = []string{"plugin", "marketplace", "add", trust.GuardMarketplace}
	guardInstallArgs     = []string{"plugin", "install", trust.GuardPlugin}
)

func installGuardPlugin(c *cobra.Command, ws *resolve.Workspace) {
	w := c.OutOrStdout()
	if err := runGuardPluginInstall(ws); err != nil {
		fmt.Fprintf(w, "The tentaqles plugin was not installed: %v\n", err)
		fmt.Fprintf(w, "Claude won't start a session for %s until it is. Run these, in this order:\n  %s\n",
			ws.Name, strings.Join(trust.GuardInstallLines(ws.Name), "\n  "))
		return
	}
	resignCovered(c, ws)
	fmt.Fprintf(w, "The tentaqles plugin is installed: every session of %s runs the guard.\n", ws.Name)
}

// runGuardPluginInstall adds the marketplace and installs the plugin with ws's Claude config dir
// (CLAUDE_CONFIG_DIR, through envplan.Environ). A failed marketplace add does not stop the install:
// the marketplace may be there already, and the install says whether the plugin is.
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

// runClaudePlugin runs one claude plugin command under reachPluginTimeout and names it in the error:
// the command, and the reason install.ErrorLine finds in its stderr and stdout (the first error line,
// not the last line, which is often a tip or a progress frame).
func runClaudePlugin(claude string, args, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), reachPluginTimeout)
	defer cancel()
	stdout, stderr, err := reachPluginRun(ctx, claude, args, env)
	what := "claude " + strings.Join(args, " ")
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s did not finish within %s", what, reachPluginTimeout)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %s", what, install.ErrorLine(string(stderr), string(stdout), err))
	}
	return nil
}

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
		fmt.Fprintln(w, strings.TrimSpace(res.Output()))
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

// The terminal seams of the handover (ADR-0019): the launcher, the killer, the PowerShell probe, and the
// shells beyond the one reach runs in whose profile reach checks (every shell tq hooks install detects).
// Tests replace them.
var (
	reachOpenTerminal = termopen.Open
	reachKill         = termopen.Kill
	reachProbe        = powershellLoads
	reachDetectShells = func(p hooks.Profiles) []hooks.Shell { return hooks.Detect(p, hooks.LookPath) }
)

// reachProbeTimeout bounds the PowerShell probe, a run of reach and not a hook.
var reachProbeTimeout = 20 * time.Second

// powershellProbe is the one script the probe runs. It is a constant: nothing from the folder, the
// manifest or the environment reaches it. The profile path goes out as base64 of its UTF-8 bytes, so the
// console's code page cannot change a non-ASCII character on the way (a João folder, L-10).
const powershellProbe = `[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($PROFILE.CurrentUserCurrentHost)); Get-ExecutionPolicy`

// powershellLoads asks the PowerShell exe on the PATH where it reads its profile and under what execution
// policy it runs. The profile tq wrote is loaded only at that path, and only under a policy that runs an
// unsigned local script: Restricted and AllSigned do not (ADR-0019). It runs PowerShell with no profile.
func powershellLoads(exe, profile string) error {
	path, err := exec.LookPath(exe)
	if err != nil {
		return fmt.Errorf("%s was not found on the PATH", exe)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reachProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-NoProfile", "-NonInteractive", "-Command", powershellProbe).Output()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s did not answer within %s", exe, reachProbeTimeout)
	}
	if err != nil {
		return fmt.Errorf("%s did not run: %v", exe, err)
	}
	return checkPowerShellProbe(exe, out, profile)
}

// checkPowerShellProbe reads the probe's output: the first line is the profile path (base64 of its UTF-8
// bytes), and the last line is the execution policy. The path must be the file tq wrote, and the policy
// must run an unsigned local script. CRLF line ends and blank lines are ignored.
func checkPowerShellProbe(exe string, out []byte, profile string) error {
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 {
		return fmt.Errorf("%s did not say where it reads its profile", exe)
	}
	raw, err := base64.StdEncoding.DecodeString(lines[0])
	if err != nil {
		return fmt.Errorf("%s said where it reads its profile in a form tq cannot read", exe)
	}
	read, policy := string(raw), lines[len(lines)-1]
	if !strings.EqualFold(filepath.Clean(read), filepath.Clean(profile)) {
		return fmt.Errorf("%s reads its profile from %s, not from %s", exe, read, profile)
	}
	if policy == "Restricted" || policy == "AllSigned" {
		return fmt.Errorf("%s's execution policy is %s, which does not run its profile (Set-ExecutionPolicy RemoteSigned -Scope CurrentUser allows it)", exe, policy)
	}
	return nil
}

// reachLoads says whether sh runs the profile tq wrote for it. A shell tq does not probe (bash, zsh, fish)
// is taken to; PowerShell is taken to only when the probe confirms it.
func reachLoads(sh hooks.Shell, p hooks.Profiles) error {
	if sh != "powershell" && sh != "pwsh" {
		return nil
	}
	return reachProbe(string(sh), p[sh])
}

// identityFreeEnv is this process's environment without tq's identity: what the tq shell hook set is
// restored, and every catalog provider's config variable and the workspace markers are dropped. A new
// terminal starts clean, and its own hook sets the client's variables (REQ-3, ADR-0019).
func identityFreeEnv() []string {
	var out []string
	for _, kv := range envplan.Clean(os.Environ()) {
		k, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "TQ_WS") || strings.EqualFold(k, "TQ_WS_ROOT") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// reachParentShell names the shell reach runs in, as one of the shells tq hooks know, or "" when its
// parent is none of them: cmd.exe has no profile hook (tq run covers it), and nothing is hooked then.
func reachParentShell() string {
	p, ok := humanonly.ParentOf(os.Getpid())
	if !ok || p.Reused {
		return ""
	}
	sh, _ := termopen.ShellName(p.Name)
	return sh
}

// reachShells are the shells whose profile reach makes hold the tq block: every shell tq hooks install
// detects, and the one reach runs in. A shell without a profile is left out.
func reachShells(p hooks.Profiles, parent string) []hooks.Shell {
	var out []hooks.Shell
	seen := map[hooks.Shell]bool{}
	add := func(sh hooks.Shell) {
		if !seen[sh] && p[sh] != "" {
			seen[sh] = true
			out = append(out, sh)
		}
	}
	for _, sh := range reachDetectShells(p) {
		add(sh)
	}
	add(hooks.Shell(parent))
	return out
}

// reachMissing are the shells whose profile has no tq block yet: the confirmation names their profiles.
func reachMissing(shells []hooks.Shell, p hooks.Profiles) []hooks.Shell {
	var out []hooks.Shell
	for _, sh := range shells {
		if st := hooks.StatusOf(sh, p).State; st == "missing" || st == "no profile" {
			out = append(out, sh)
		}
	}
	return out
}

// hookState is what reach's profile step found. parentFailed: the shell reach runs in cannot take the hook,
// so the handover does not happen (Q2). ok: the shells whose block is in place, the ones a new window in
// that shell may route through (Q1).
type hookState struct {
	parentFailed bool
	ok           map[hooks.Shell]bool
	profiles     hooks.Profiles
}

// reachHooks writes the tq activation block into each shell's profile, after the typed yes. Install
// leaves a profile that already has the block alone, so a second reach writes nothing. The shell reach
// runs in must take the block, and PowerShell must load its profile (probed): otherwise the handover does
// not happen. Any other shell's failure is a warning naming it, and the handover goes on (Q2).
func reachHooks(c *cobra.Command, shells []hooks.Shell, p hooks.Profiles, parent string) hookState {
	w := c.OutOrStdout()
	hs := hookState{ok: map[hooks.Shell]bool{}, profiles: p}
	for _, sh := range shells {
		before := hooks.StatusOf(sh, p).State
		st, err := hooks.Install(sh, p)
		if err != nil {
			if string(sh) == parent {
				hs.parentFailed = true
				fmt.Fprintf(w, "The tq activation block could not be written: %v\n", err)
			} else {
				fmt.Fprintf(w, "Warning: %s: the tq activation block could not be written: %v. A new %s terminal would not route into the client.\n", sh, err, sh)
			}
			continue
		}
		if (before == "missing" || before == "no profile") && st.State == "installed" {
			fmt.Fprintf(w, "The tq activation block is in %s.\n", st.Profile)
		}
		if string(sh) == parent {
			if err := reachLoads(sh, p); err != nil {
				hs.parentFailed = true
				fmt.Fprintf(w, "The tq activation block is in %s, but %v. The terminal reach runs in is not ended.\n", st.Profile, err)
				continue
			}
		}
		hs.ok[sh] = true
	}
	return hs
}

// reachRouted says whether the new window will run tq's hook for the client. A window in the system's
// default shell is taken to (the ticket's decision for wt). The Windows fallback, PowerShell, is taken to
// only when its block is in place and PowerShell loads it (ADR-0019).
func reachRouted(l termopen.Launch, hs hookState) bool {
	if l.Shell == "" {
		return true
	}
	sh := hooks.Shell(l.Shell)
	return hs.ok[sh] && reachLoads(sh, hs.profiles) == nil
}

// reachFinish ends reach's output with what to do next. For a new client with a terminal to hand over,
// it opens the client's terminal, prints the rest, and then ends the terminal reach was run from
// (reachEndOld). Otherwise it prints the next step and opens nothing. It never fails reach: the client
// is trusted either way.
func reachFinish(c *cobra.Command, ws *resolve.Workspace, dir string, noTerminal bool, hs hookState) {
	w := c.OutOrStdout()
	summary := func() {
		fmt.Fprintf(w, "\n%s is ready. It can use: %s\n", ws.Name, strings.Join(ws.Manifest.Scope(), ", "))
		reachWarnOther(c, ws.Manifest)
	}
	switch {
	case noTerminal:
		summary()
		fmt.Fprintf(w, "Open a new terminal in %s, then start claude and log in with this client's account.\n", dir)
	case hs.parentFailed:
		summary()
		fmt.Fprintf(w, "Once the activation block works, open a new terminal in %s to start working.\n", dir)
	default:
		l, err := reachOpenTerminal(dir, identityFreeEnv())
		summary()
		if err != nil {
			fmt.Fprintf(w, "Could not open a terminal (%v). Open one in %s to start working.\n", err, dir)
			return
		}
		if !reachRouted(l, hs) {
			// The window's hook is not confirmed: no login line, the old terminal stays, and the next step
			// is tq run, which sets the identity itself (ADR-0019).
			fmt.Fprintf(w, "Opened a new terminal in %s, but tq's shell hook is not confirmed to load there, so it is not set up for %s. Run tq run %s -- claude in it to start claude for %s.\n", dir, ws.Name, ws.Name, ws.Name)
			fmt.Fprintln(w, "Close this terminal when you are ready.")
			return
		}
		fmt.Fprintf(w, "Opened a new terminal in %s: start claude there and log in with this client's account.\n", dir)
		reachEndOld(c, c.InOrStdin())
	}
}

// reachEndTarget is reach's direct parent when tq may end it, or false. It must be readable, not a reused
// pid, with its command line known; a shell tq knows and that is interactive; and not itself a child of a
// shell. The last rule is Git Bash: a native program run from bash, directly or in a subshell or pipeline,
// has a bash.exe stub as its direct parent, and the stub's parent is the interactive shell. Ending the stub
// leaves that shell open with no message (ADR-0019). An unreadable grandparent counts as a shell.
func reachEndTarget() (humanonly.Parent, bool) {
	p, ok := humanonly.ParentOf(os.Getpid())
	if !ok || p.Reused || !p.CmdlineKnown {
		return p, false
	}
	shell, known := termopen.ShellName(p.Name)
	if !known || !termopen.Interactive(shell, p.Cmdline) {
		return p, false
	}
	g, ok := humanonly.ParentOf(p.PID)
	if !ok {
		return p, false
	}
	if _, isShell := termopen.ShellName(g.Name); isShell {
		return p, false
	}
	return p, true
}

// reachEndOld ends the terminal reach was run from (REQ-4, REQ-5, REQ-10). Only reach's direct parent is
// ever ended, and only when reachEndTarget allows it. Otherwise, or when the parent has changed by the time
// the developer presses Enter, or the kill fails, the terminal stays open and reach says so. Keys typed
// while the installs ran are discarded first, so an earlier Enter does not answer the pause; a closed input
// counts as Enter.
func reachEndOld(c *cobra.Command, in io.Reader) {
	w := c.OutOrStdout()
	first, ok := reachEndTarget()
	if !ok {
		fmt.Fprintln(w, "Close this terminal when you are ready.")
		return
	}
	if f, isFile := in.(*os.File); isFile {
		termopen.FlushInput(f)
	}
	fmt.Fprintln(w, "Press Enter to close this terminal.")
	_, _ = bufio.NewReader(in).ReadString('\n')
	// The pause may have lasted long: the parent is read again, and must still be the same process.
	now, ok := reachEndTarget()
	if !ok || now.PID != first.PID || now.Name != first.Name || now.Cmdline != first.Cmdline {
		fmt.Fprintln(w, "Close this terminal when you are ready.")
		return
	}
	if err := reachKill(now.PID); err != nil {
		fmt.Fprintln(w, "Close this terminal when you are ready.")
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
