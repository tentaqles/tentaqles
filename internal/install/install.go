// Package install puts a provider's CLI on the machine through a command-line package
// manager that the embedded catalog names (ADR-0015).
//
// Five rules hold the package together:
//
//   - only the six methods of Order run: winget, scoop, brew, apt, npm and pip. A value under
//     `url` is documentation and is never opened or run, and tq never launches an installer
//     file itself (winget may, behind the scenes: that is the package manager's own work);
//   - the package id comes from the catalog and goes to the package manager as one argv
//     element, with no shell. Nothing from the command line reaches the package manager;
//   - a package manager runs at most Timeout (ten minutes), then its whole process tree is
//     stopped;
//   - the package manager and the version check run with the environment the caller gives,
//     the identity's;
//   - a failure is reported as the package manager's own error line.
package install

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// Order is the order the methods are tried in: a machine's system package manager before a
// language one. The first whose package manager is on the PATH runs.
var Order = []string{"winget", "scoop", "brew", "apt", "npm", "pip"}

// DefaultTimeout is how long one package manager run may take.
const DefaultTimeout = 10 * time.Minute

// versionTimeout bounds the version command run after an install.
const versionTimeout = 30 * time.Second

// pkgRe is what a package id may look like: a plain name with the separators package ids use
// (Microsoft.AzureCLI, @openai/codex, tap/formula). It cannot start with a dash, so an id is
// never read as an option, and holds no space or shell character.
var pkgRe = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9@._/+:~=-]*$`)

// Plan is the package manager run chosen for a provider.
type Plan struct {
	Method  string   // winget, scoop, brew, apt, npm or pip
	Package string   // the catalog's package id
	Program string   // the program found on the PATH
	Args    []string // argv after the program: the method's arguments and the package id
}

// managers are the programs a method may run, in the order they are tried.
var managers = map[string][]string{
	"winget": {"winget"},
	"scoop":  {"scoop"},
	"brew":   {"brew"},
	"apt":    {"apt-get"},
	"npm":    {"npm"},
	"pip":    {"pip", "pip3"},
}

// MethodArgs is the argv of a method for a package id, without the program: fixed arguments
// of tq's, then the catalog's package id as the last element. winget's flags accept the
// vendor's licence and source agreements: that is how it runs without a prompt.
func MethodArgs(method, pkg string) ([]string, bool) {
	switch method {
	case "winget":
		return []string{"install", "--id", pkg, "-e", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}, true
	case "scoop":
		return []string{"install", pkg}, true
	case "brew":
		return []string{"install", pkg}, true
	case "apt":
		return []string{"install", "-y", pkg}, true
	case "npm":
		return []string{"install", "-g", pkg}, true
	case "pip":
		return []string{"install", pkg}, true
	}
	return nil, false
}

// methodPackage is the catalog's package id for a method in one OS block.
func methodPackage(o providers.InstallOS, method string) string {
	switch method {
	case "winget":
		return o.Winget
	case "scoop":
		return o.Scoop
	case "brew":
		return o.Brew
	case "apt":
		return o.Apt
	case "npm":
		return o.Npm
	case "pip":
		return o.Pip
	}
	return ""
}

// OSHints is the install block of p for goos ("windows", "darwin", anything else is linux).
func OSHints(p providers.Provider, goos string) providers.InstallOS {
	switch goos {
	case "windows":
		return p.Install.Windows
	case "darwin":
		return p.Install.Macos
	}
	return p.Install.Linux
}

// Deps are the seams of the package: tests replace them, the command fills in the real ones.
type Deps struct {
	// GOOS is runtime.GOOS.
	GOOS string
	// Env is the environment every run uses: the identity's (envplan.Environ).
	Env []string
	// LookPath resolves a program name on the PATH of env.
	LookPath func(name string, env []string) (string, error)
	// Masked reports a program the identity's PATH layer hides (an out-of-scope CLI): a
	// package manager that is masked is as good as absent. May be nil.
	Masked func(name string) bool
	// Run runs path with args in env and returns its output; it ends when ctx does, and
	// stops the whole process tree it started.
	Run func(ctx context.Context, path string, args, env []string) (stdout, stderr string, err error)
	// RegistryPath is the user and machine PATH read from the registry (Windows), joined with
	// the list separator. A package manager's PATH change does not reach a running process, so
	// the lookup after an install adds these folders. May be nil.
	RegistryPath func() (string, error)
	// Timeout bounds one package manager run; zero means DefaultTimeout.
	Timeout time.Duration
}

// Status is how an install ended.
type Status int

const (
	// Installed: a package manager ran and the CLI now answers its version command.
	Installed Status = iota
	// Present: the CLI already resolved on the identity PATH; nothing ran.
	Present
	// Manual: no method of this OS has a package manager here; the developer installs it.
	Manual
	// NoCLI: the provider has no CLI (an MCP-only provider).
	NoCLI
)

// Result is the outcome of a successful Install.
type Result struct {
	Status   Status
	Provider string
	Version  string // the first line of the version command's output
	Docs     string // the provider's documentation URL
	Method   string // the method that ran (Installed)
	// Then is a step the developer takes after the CLI is there (Azure DevOps' `az extension
	// add`), from a catalog note that starts with "then: ". tq prints it and never runs it.
	Then string
	// NewTerminal is set for a CLI found only on the registry PATH (Present): the line is
	// followed by OpenTerminalLine.
	NewTerminal bool
}

// OpenTerminalLine is printed after the line of a CLI found only on the registry PATH.
const OpenTerminalLine = "Open a new terminal to use it."

// thenPrefix marks a catalog install note as a follow-up step to print. Every other note is
// an install alternative (a script piped to a shell, say) and is never printed.
const thenPrefix = "then: "

// thenNote is the follow-up note of one OS block, or "".
func thenNote(o providers.InstallOS) string {
	if strings.HasPrefix(o.Note, thenPrefix) {
		return clean(o.Note)
	}
	return ""
}

// Output is what the command prints: the line, then, for a CLI found only on the registry PATH,
// the open-terminal line, then the follow-up step when there is one.
func (r Result) Output() string {
	out := []string{r.Line()}
	if r.NewTerminal && r.Status == Present {
		out = append(out, OpenTerminalLine)
	}
	if r.Then != "" && (r.Status == Installed || r.Status == Present) {
		out = append(out, r.Then)
	}
	return strings.Join(out, "\n")
}

// Line is the one line the command prints.
func (r Result) Line() string {
	switch r.Status {
	case Installed:
		return r.Provider + ": installed " + r.Version
	case Present:
		return r.Provider + ": already present " + r.Version
	case NoCLI:
		return r.Provider + ": no CLI, nothing to install"
	}
	return r.Provider + ": manual, download from " + r.Docs
}

// Failure is a package manager that failed, timed out, or left no CLI behind.
type Failure struct {
	Provider string
	Msg      string // the package manager's error line
}

func (f *Failure) Error() string { return f.Provider + ": install failed: " + f.Msg }

func (d Deps) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return DefaultTimeout
}

func (d Deps) masked(name string) bool { return d.Masked != nil && d.Masked(name) }

// Pick chooses the method: the first of Order that has a package id for this OS and whose
// package manager is on the PATH. ok is false when none does. An id that is not a plain
// package name is an error (the catalog is wrong), never run.
func Pick(p providers.Provider, d Deps) (plan Plan, ok bool, err error) {
	hints := OSHints(p, d.GOOS)
	for _, m := range Order {
		pkg := methodPackage(hints, m)
		if pkg == "" {
			continue
		}
		for _, prog := range managers[m] {
			if d.masked(prog) {
				continue
			}
			path, lerr := d.LookPath(prog, d.Env)
			if lerr != nil {
				continue
			}
			if !pkgRe.MatchString(pkg) {
				return Plan{}, false, fmt.Errorf("the catalog's %s package id %q for %s is not a plain package name", m, pkg, p.ID)
			}
			args, _ := MethodArgs(m, pkg)
			return Plan{Method: m, Package: pkg, Program: path, Args: args}, true, nil
		}
	}
	return Plan{}, false, nil
}

// Install makes p's CLI available. It never runs anything for a provider whose CLI already
// resolves, and nothing at all when there is no usable method.
func Install(p providers.Provider, d Deps) (Result, error) {
	res := Result{Provider: p.ID, Docs: p.Docs}
	if p.CLI == nil {
		res.Status = NoCLI
		return res, nil
	}
	cmdName := p.CLI.Command
	vargs := p.CLI.VersionArgs
	if len(vargs) == 0 {
		vargs = []string{"--version"}
	}
	res.Then = thenNote(OSHints(p, d.GOOS))
	// Look for the CLI on the PATH a new shell would have: on Windows a package manager's PATH
	// change is in the registry, not in this process. A CLI an earlier install put there (the
	// same CLI under another provider in one reach, or a retry from the same terminal) is
	// present.
	if path, err := d.LookPath(cmdName, d.Env); err == nil {
		res.Status = Present
		res.Version = d.version(path, vargs, d.Env)
		return res, nil
	}
	// Found only on the registry PATH: a package manager put it there, in an earlier run or in
	// another provider's install of the same CLI. Nothing runs; the developer's terminal has to
	// be reopened to see it.
	if env := d.lookupEnv(); len(env) > 0 {
		if path, err := d.LookPath(cmdName, env); err == nil {
			res.Status = Present
			res.NewTerminal = true
			res.Version = d.version(path, vargs, env)
			return res, nil
		}
	}
	plan, ok, err := Pick(p, d)
	if err != nil {
		return res, &Failure{Provider: p.ID, Msg: err.Error()}
	}
	if !ok {
		res.Status = Manual
		if res.Docs == "" {
			res.Docs = "the provider's documentation"
		}
		return res, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout())
	defer cancel()
	stdout, stderr, runErr := d.Run(ctx, plan.Program, plan.Args, d.Env)
	if ctx.Err() == context.DeadlineExceeded {
		return res, &Failure{Provider: p.ID, Msg: fmt.Sprintf("%s did not finish within %s and was stopped", plan.Method, d.timeout())}
	}
	if runErr != nil {
		return res, &Failure{Provider: p.ID, Msg: ErrorLine(stderr, stdout, runErr)}
	}

	// Verify with the PATH a new shell would have, read again: the package manager changed it.
	env := d.lookupEnv()
	path, lerr := d.LookPath(cmdName, env)
	if lerr != nil {
		return res, &Failure{Provider: p.ID, Msg: fmt.Sprintf("%s finished but %s is not on the PATH; open a new terminal and run it again", plan.Method, cmdName)}
	}
	ver, verr := d.versionErr(path, vargs, env)
	if verr != nil {
		return res, &Failure{Provider: p.ID, Msg: fmt.Sprintf("%s finished but %s %s failed: %s", plan.Method, cmdName, strings.Join(vargs, " "), verr)}
	}
	res.Status, res.Version, res.Method = Installed, ver, plan.Method
	return res, nil
}

// lookupEnv is Env, and on Windows with the registry's PATH folders added to the end of its
// PATH: the lookup of a CLI and its version command use it.
func (d Deps) lookupEnv() []string {
	if d.GOOS == "windows" && d.RegistryPath != nil {
		if reg, err := d.RegistryPath(); err == nil && reg != "" {
			return AppendPath(d.Env, reg)
		}
	}
	return d.Env
}

// version is the first line of the version command's output, or "unknown".
func (d Deps) version(path string, args, env []string) string {
	v, err := d.versionErr(path, args, env)
	if err != nil || v == "" {
		return "unknown"
	}
	return v
}

func (d Deps) versionErr(path string, args, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	stdout, stderr, err := d.Run(ctx, path, args, env)
	if ctx.Err() == context.DeadlineExceeded {
		return "", errors.New("it did not answer within " + versionTimeout.String())
	}
	if err != nil {
		return "", errors.New(ErrorLine(stderr, stdout, err))
	}
	if l := firstLine(stdout); l != "" {
		return clean(l), nil
	}
	return clean(firstLine(stderr)), nil
}

// AppendPath adds the folders of extra (a PATH value) that env's PATH does not hold yet to the
// end of it. The PATH variable keeps its spelling; env gains one when it had none.
func AppendPath(env []string, extra string) []string {
	const sep = ";" // only called for the Windows registry value
	out := append([]string{}, env...)
	idx := -1
	for i, kv := range out {
		if k, _, _ := strings.Cut(kv, "="); strings.EqualFold(k, "PATH") {
			idx = i
			break
		}
	}
	cur := ""
	key := "Path"
	if idx >= 0 {
		key, cur, _ = strings.Cut(out[idx], "=")
	}
	have := map[string]bool{}
	for _, e := range strings.Split(cur, sep) {
		have[strings.ToLower(strings.TrimRight(e, `\/`))] = true
	}
	merged := cur
	for _, e := range strings.Split(extra, sep) {
		e = strings.TrimSpace(e)
		if e == "" || have[strings.ToLower(strings.TrimRight(e, `\/`))] {
			continue
		}
		have[strings.ToLower(strings.TrimRight(e, `\/`))] = true
		if merged != "" {
			merged += sep
		}
		merged += e
	}
	if idx >= 0 {
		out[idx] = key + "=" + merged
	} else {
		out = append(out, key+"="+merged)
	}
	return out
}
