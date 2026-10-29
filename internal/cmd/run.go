package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// claudeArgs is the manifest-driven permission wrapper the hub runner passes
// to its headless claude (adapter.Claude.Args, hub_service.go): the
// manifest's claude.permission_mode, bypass only after `tq allow --bypass`.
// The interactive launcher (tq run, the claude shell function) uses
// launchClaudeArgs instead and always starts in bypass (spec E §8.1, §6.4).
func claudeArgs(ws *resolve.Workspace, args []string, warn func(string)) []string {
	mode := ws.Manifest.Claude.PermissionMode
	switch mode {
	case "", "default":
		return args
	case "bypass":
		if guardcfg.BypassAllowed(ws.Hash, ws.Name, ws.Root) {
			return append([]string{"--dangerously-skip-permissions"}, args...)
		}
		warn(fmt.Sprintf("workspace %s asks for bypass but `tq allow --bypass %s` was not run; using acceptEdits", ws.Name, ws.Name))
		return append([]string{"--permission-mode", "acceptEdits"}, args...)
	default:
		// Invariant: manifest.Load allowlists permission_mode, so only
		// "acceptEdits", "plan", "auto" and "dontAsk" can reach this arm.
		return append([]string{"--permission-mode", mode}, args...)
	}
}

// claudeSubcommands are claude's subcommands (claude --help, "Commands:").
// The launcher adds no permission flag in front of them.
var claudeSubcommands = []string{"agents", "attach", "auth", "auto-mode", "doctor", "gateway", "import", "install",
	"logs", "mcp", "plugin", "plugins", "purge", "respawn", "rm", "setup-token", "stop", "kill", "ultrareview",
	"update", "upgrade"}

// stripTqMode removes every `--tq-mode <m>` / `--tq-mode=<m>` from args and
// returns the last mode given ("" when none).
func stripTqMode(args []string) ([]string, string) {
	out := make([]string, 0, len(args))
	mode := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if v, ok := strings.CutPrefix(a, "--tq-mode="); ok {
			mode = v
			continue
		}
		if a == "--tq-mode" {
			if i+1 < len(args) {
				mode = args[i+1]
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out, mode
}

// launchClaudeArgs is the launcher's claude command line (spec E §8.1): it
// strips --tq-mode (returned as mode) and adds --dangerously-skip-permissions,
// whatever claude.permission_mode says. A --permission-mode or
// --dangerously-skip-permissions typed on this command line replaces it, and
// claude's subcommands, --help and --version get nothing. An agent's nested
// claude (agent true) never gets bypass from the launcher: it keeps claude's
// own prompts on top of the parent's guard (spec E §9).
func launchClaudeArgs(args []string, agent bool) ([]string, string) {
	args, mode := stripTqMode(args)
	if agent {
		return args, mode
	}
	if len(args) > 0 && slices.Contains(claudeSubcommands, args[0]) {
		return args, mode
	}
	for _, a := range args {
		switch {
		case a == "--permission-mode", strings.HasPrefix(a, "--permission-mode="),
			a == "--dangerously-skip-permissions",
			a == "--help", a == "-h", a == "--version", a == "-v":
			return args, mode
		}
	}
	return append([]string{"--dangerously-skip-permissions"}, args...), mode
}

// offClaudeSettings switches the tentaqles plugin off for an off launch
// (spec E §4.1, Assumption A3: --settings merges enabledPlugins per key).
const offClaudeSettings = `{"enabledPlugins":{"tentaqles@tentaqles":false}}`

// offBanner is printed on stderr by every off launch.
const offBanner = "tq: off — main logins, no guard (git, cloud and MCP use your default accounts)"

// agentProg names the agent a program is: "claude" or "codex" for those
// names, with or without a directory and a .exe/.cmd/.bat/.ps1 extension,
// and "" for any other program.
func agentProg(name string) string {
	b := strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		b = strings.TrimSuffix(b, ext)
	}
	if b == "claude" || b == "codex" {
		return b
	}
	return ""
}

// offArgs is an off launch's command line: claude in bypass with the plugin
// switched off, codex with approvals and sandbox bypassed. A permission flag
// the user typed replaces the bypass flag; subcommands get nothing.
func offArgs(name string, args []string) []string {
	args, _ = stripTqMode(args)
	switch agentProg(name) {
	case "claude":
		if len(args) > 0 && slices.Contains(claudeSubcommands, args[0]) {
			return args
		}
		pre := []string{"--settings", offClaudeSettings}
		if !slices.ContainsFunc(args, func(a string) bool {
			return a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") || a == "--dangerously-skip-permissions"
		}) {
			pre = append([]string{"--dangerously-skip-permissions"}, pre...)
		}
		return append(pre, args...)
	case "codex":
		if slices.ContainsFunc(args, func(a string) bool {
			return a == "--dangerously-bypass-approvals-and-sandbox" || a == "--yolo"
		}) {
			return args
		}
		return append([]string{"--dangerously-bypass-approvals-and-sandbox"}, args...)
	}
	return args
}

// modeRank orders guard modes from strict to off; relaxed and custom share a
// rank but are not interchangeable.
func modeRank(m string) int {
	switch m {
	case guardcfg.ModeStrict:
		return 0
	case guardcfg.ModeDefault, "":
		return 1
	case guardcfg.ModeOff:
		return 3
	}
	return 2
}

// noLooser reports whether a launch at mode req is no looser than parent.
func noLooser(req, parent string) bool {
	if req == parent || (req == guardcfg.ModeDefault && parent == "") {
		return true
	}
	return modeRank(req) < modeRank(parent)
}

// envKeyEq compares env var names the way the OS does.
func envKeyEq(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// withoutVars returns env without the named variables.
func withoutVars(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if slices.ContainsFunc(names, func(n string) bool { return envKeyEq(k, n) }) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// execCommand runs name+args with env and the process's stdio, and exits
// with the child's code when it fails. Tests replace it, so no real claude or
// codex ever runs.
var execCommand = func(name string, args, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	return err
}

// launch is one tq run: ws (nil: neutral), the command, and the guard mode
// given with --tq-mode on tq run itself ("" when none).
type launch struct {
	ws   *resolve.Workspace
	name string
	args []string
	mode string
	warn func(string)
}

// run starts the launch (spec E §8.1). For claude and codex it strips
// --tq-mode from the command's own args. It sets TQ_MODE when a mode is
// given; otherwise a human launch drops an inherited TQ_MODE (the layers
// decide) and an agent's nested launch passes the parent's through. An agent
// may not ask for a mode looser than its own.
func (l launch) run() error {
	_, agent := agentSession()
	args := l.args
	mode := l.mode
	switch agentProg(l.name) {
	case "claude":
		var m string
		args, m = launchClaudeArgs(args, agent)
		if mode == "" {
			mode = m
		}
	case "codex":
		var m string
		args, m = stripTqMode(args)
		if mode == "" {
			mode = m
		}
	}
	if mode != "" {
		if mode == guardcfg.ModeOff {
			return errors.New("--tq-mode off is not a mode: use tq run --off -- <claude|codex>")
		}
		if !guardcfg.ValidStoredMode(mode) {
			return fmt.Errorf("--tq-mode %q: must be one of %s", mode, strings.Join(guardcfg.StoredModes(), ", "))
		}
	}
	env := withoutVars(envplan.Environ(l.ws, os.Environ()), envplan.HeadlessModeVar)
	if agent {
		parent := strings.TrimSpace(launchMode())
		if parent == "" {
			cwd, _ := os.Getwd()
			parent = resolveMode(l.ws, cwd, "").Mode
		}
		if mode != "" && !noLooser(mode, parent) {
			return fmt.Errorf("--tq-mode %s is looser than this agent session's mode (%s); an agent may only launch at its own mode or stricter", mode, orDefault(parent))
		}
	}
	if agentProg(l.name) == "codex" && l.ws != nil {
		// The flags follow the mode the child gets in TQ_MODE.
		cwd, _ := os.Getwd()
		withLaunchMode(mode, agent, func() { args = codexArgs(l.ws, cwd, args) })
	}
	if mode != "" || !agent {
		env = withoutVars(env, guardcfg.EnvMode)
	}
	if mode != "" {
		env = append(env, guardcfg.EnvMode+"="+mode)
	}
	return execCommand(l.name, args, env)
}

func orDefault(m string) string {
	if m == "" {
		return guardcfg.ModeDefault
	}
	return m
}

// runOff is tq run --off (spec E §8.1): the main install and logins, no tq.
// It refuses agent sessions.
func runOff(name string, args []string, errw func(string)) error {
	if err := refuseAgent("tq run --off"); err != nil {
		return err
	}
	if agentProg(name) == "" {
		return fmt.Errorf("tq run --off starts claude or codex only, not %q: tq run --off -- <claude|codex> [args]", name)
	}
	errw(offBanner)
	return execCommand(name, offArgs(name, args), envplan.OffEnviron(os.Environ()))
}

// execIn runs name+args with ws's environment. applyClaudeArgs must be false for
// flows that drive claude's own subcommands (e.g. `tq login <ws> claude`), where
// injecting --permission-mode / --dangerously-skip-permissions is wrong; true
// is the launcher (launch.run).
func execIn(ws *resolve.Workspace, name string, args []string, applyClaudeArgs bool, errw func(string)) error {
	if applyClaudeArgs {
		return launch{ws: ws, name: name, args: args, warn: errw}.run()
	}
	return execCommand(name, args, envplan.Environ(ws, os.Environ()))
}

// cwdWorkspace resolves `.`: the current folder's trusted workspace, or nil
// (neutral) with a warning when it is untrusted or invalid.
func cwdWorkspace(warn func(string)) (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	res := resolve.Resolve(cwd, cfg)
	if res.Workspace == nil && res.Untrusted != nil {
		warn(fmt.Sprintf("workspace %s is untrusted; running without its identity (tq allow %s)", res.Untrusted.Name, res.Untrusted.Name))
	}
	return res.Workspace, nil
}

func newRunCmd() *cobra.Command {
	var tqMode string
	var off bool
	c := &cobra.Command{
		Use:   "run <workspace>|. [--tq-mode MODE] -- <command> [args...]",
		Short: "Run a command with a workspace's identity without cd-ing into it",
		Long: "Run a command with a workspace's identity. `.` is the current folder's workspace (neutral when none).\n" +
			"For claude, the launcher always starts in bypass (tq's guard mode is the policy layer); --tq-mode sets\n" +
			"the guard mode for this launch (TQ_MODE). `tq run --off -- <claude|codex>` starts it with no tq at all:\n" +
			"the main install and logins, no guard.",
		Example: "  tq run acme -- claude\n  tq run . --tq-mode strict -- claude -p \"...\"\n  tq run --off -- claude",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			warn := func(s string) { fmt.Fprintln(c.ErrOrStderr(), "warning:", s) }
			if off {
				if c.ArgsLenAtDash() > 0 {
					return errors.New("tq run --off takes no workspace: tq run --off -- <claude|codex> [args]")
				}
				if tqMode != "" {
					return errors.New("tq run --off takes no --tq-mode")
				}
				return runOff(args[0], args[1:], func(s string) { fmt.Fprintln(c.ErrOrStderr(), s) })
			}
			// Flags are not interspersed: after <workspace> only --tq-mode
			// and an optional -- may come before the command word, and
			// everything after it is the command's own.
			rest, m, err := runTail(args[1:])
			if err != nil {
				return err
			}
			if m != "" {
				tqMode = m
			}
			args = append(args[:1:1], rest...)
			if len(args) < 2 {
				return errors.New("usage: tq run <workspace>|. -- <command> [args...]")
			}
			var ws *resolve.Workspace
			if args[0] == "." {
				w, err := cwdWorkspace(warn)
				if err != nil {
					return err
				}
				ws = w
			} else {
				w, err := findWorkspace(args[0])
				if err != nil {
					return err
				}
				if !trust.IsTrusted(w.Hash) {
					return fmt.Errorf("workspace %s is untrusted; run: tq allow %s", w.Name, w.Name)
				}
				ws = w
			}
			return launch{ws: ws, name: args[1], args: args[2:], mode: tqMode, warn: warn}.run()
		},
	}
	c.Flags().StringVar(&tqMode, "tq-mode", "", "guard mode for this launch: "+strings.Join(guardcfg.StoredModes(), "|"))
	c.Flags().BoolVar(&off, "off", false, "start claude or codex with no tq: main install and logins, no guard (human only)")
	c.Flags().SetInterspersed(false)
	return c
}

// runTail reads what follows <workspace> in tq run: --tq-mode MODE or
// --tq-mode=MODE, then an optional --, then the command and its args.
func runTail(args []string) (rest []string, mode string, err error) {
	for len(args) > 0 {
		switch a := args[0]; {
		case a == "--":
			return args[1:], mode, nil
		case a == "--tq-mode":
			if len(args) < 2 {
				return nil, "", errors.New("--tq-mode needs a mode")
			}
			mode, args = args[1], args[2:]
		case strings.HasPrefix(a, "--tq-mode="):
			mode, args = strings.TrimPrefix(a, "--tq-mode="), args[1:]
		case a == "--off" || strings.HasPrefix(a, "--off="):
			return nil, "", errors.New("tq run --off takes no workspace: tq run --off -- <claude|codex> [args]")
		default:
			return args, mode, nil
		}
	}
	return args, mode, nil
}
