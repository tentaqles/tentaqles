package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/browser"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

const openUsage = "usage: tentaqles open [--browser chrome|edge] <http or https URL>"

// newOpenCmd is `tentaqles open <url>`: the URL in the client's browser profile (ADR-0016).
// It writes nothing under the tq home, so a session may run it; it opens only an http or https
// URL, by the browser's install path with --profile-directory, never through the system's
// default handler. Flags are read by hand: any word that starts with '-' and is not --browser
// is refused, so a value like --user-data-dir=x never reaches a browser.
func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [--browser chrome|edge] <url>",
		Short: "Open an http or https URL in this client's browser profile",
		Long: `tentaqles open opens an http or https URL in the client's browser: the first one in
the manifest's browsers: list, or the one --browser names, started from its install path
with --profile-directory=<the client's profile>. It never hands the URL to the system's
default browser, which opens whatever profile was used last.

It refuses any other URL scheme, a value that starts with '-', a folder that is not a
client's, a client with no browsers: list, and a listed browser that is not installed. A
page opened by the BROWSER variable, xdg-open or open inside a client's terminal goes the
same way (tq settings render writes those shims).`,
		DisableFlagParsing: true,
		RunE: func(c *cobra.Command, args []string) error {
			want, target, err := parseOpenArgs(args)
			if err != nil {
				if errors.Is(err, errOpenHelp) {
					return c.Help()
				}
				return err
			}
			// The URL is judged before the folder: nothing is read for a value that cannot open.
			if _, err := browser.ValidateURL(target); err != nil {
				return err
			}
			ws, err := cwdWorkspace()
			if err != nil {
				return err
			}
			if ws == nil {
				return errors.New("this folder is not a client folder: tentaqles open opens a page in a client's browser profile (run it inside a client folder)")
			}
			b, err := openFor(ws, want, target)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "opened %s in %s (profile %s)\n", target, b.Name, b.Profile)
			return nil
		},
	}
}

var errOpenHelp = errors.New("help")

// parseOpenArgs reads `open`'s words: --browser <name> or --browser=<name>, -h/--help, and one
// URL. Every other word that starts with '-' is refused, whether or not it follows `--`.
func parseOpenArgs(args []string) (want, target string, err error) {
	var pos []string
	endOpts := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case endOpts:
			pos = append(pos, a)
		case a == "--":
			endOpts = true
		case a == "-h" || a == "--help":
			return "", "", errOpenHelp
		case a == "--browser":
			if i+1 >= len(args) {
				return "", "", errors.New("--browser needs a browser name (chrome or edge)")
			}
			i++
			want = args[i]
		case strings.HasPrefix(a, "--browser="):
			want = strings.TrimPrefix(a, "--browser=")
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		return "", "", errors.New(openUsage)
	}
	if strings.HasPrefix(pos[0], "-") {
		return "", "", fmt.Errorf("refused: %q starts with '-' and would be read as a browser flag", pos[0])
	}
	return want, pos[0], nil
}

// openFor opens rawURL in ws's browser: the first listed, or want. Nothing starts unless the
// client lists a browser, it is installed and rawURL is a web URL.
func openFor(ws *resolve.Workspace, want, rawURL string) (manifest.Browser, error) {
	if ws == nil || ws.Manifest == nil || len(ws.Manifest.Browsers) == 0 {
		return manifest.Browser{}, errors.New("no browser is set for this client")
	}
	return browser.Open(ws.Manifest.Browsers, want, rawURL)
}

// ShimMain makes the tq binary answer as a browser shim when it runs from a client's shims
// folder ($TQ_HOME/shims/<ws>/tq-open, xdg-open, open; ADR-0016). Every other run of tq returns
// false.
func ShimMain(stderr io.Writer, args []string) (bool, int) {
	exe, err := os.Executable()
	if err != nil {
		return false, 0
	}
	return runShim(exe, args, stderr)
}

// runShim is ShimMain for the program at exe.
func runShim(exe string, args []string, stderr io.Writer) (bool, int) {
	wsName, name, mode := envplan.ShimName(exe)
	if mode == envplan.ShimNone {
		return false, 0
	}
	if mode == envplan.ShimURL {
		if len(args) != 1 {
			fmt.Fprintf(stderr, "%s: expects one http or https URL\n", name)
			return true, 2
		}
		return true, shimOpen(wsName, name, "", args, stderr)
	}
	// xdg-open and open: an http or https URL never reaches the real program (it would open
	// the system's default profile). It opens in the client's profile, or is refused and
	// opens nothing; what is a URL is decided by the scheme, not by whether tq likes the rest
	// of it. The one way past is `open -a <another app>`, which is that app's business.
	urls := webArgs(args)
	if len(urls) == 0 {
		return true, passThrough(exe, name, args, stderr)
	}
	want := ""
	if app, ok := openApp(args); ok {
		if want = appBrowser(app); want == "" {
			return true, passThrough(exe, name, args, stderr)
		}
	}
	return true, shimOpen(wsName, name, want, urls, stderr)
}

// webArgs are the arguments that start with http:// or https:// (any case).
func webArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if l := strings.ToLower(a); strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
			out = append(out, a)
		}
	}
	return out
}

// openApp is the value of macOS `open -a <app>`.
func openApp(args []string) (string, bool) {
	for i, a := range args {
		if a == "-a" && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// appBrowser is the manifest name of the browser an `open -a` app names (Google Chrome,
// Microsoft Edge), "" for any other app.
func appBrowser(app string) string {
	// An app given by its path (/Applications/Google Chrome.app) names the same app: its base name.
	app = app[strings.LastIndexAny(app, `/\`)+1:]
	switch strings.TrimSuffix(strings.ToLower(strings.TrimSpace(app)), ".app") {
	case "google chrome", "chrome":
		return "chrome"
	case "microsoft edge", "edge", "msedge":
		return "edge"
	}
	return ""
}

// shimOpen opens every URL in the browser of workspace wsName (want "": its first listed): the
// same steps as tentaqles open, for the identity the shim's folder names, which must be
// trusted. Every URL is checked before the first opens, so a refusal opens nothing. It
// returns the exit code.
func shimOpen(wsName, name, want string, urls []string, stderr io.Writer) int {
	for _, u := range urls {
		if _, err := browser.ValidateURL(u); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return 1
		}
	}
	ws, err := trustedByName(wsName)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	for _, u := range urls {
		if _, err := openFor(ws, want, u); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return 1
		}
	}
	return 0
}

// trustedByName is workspace name when its integrity record verifies: a changed shim, manifest
// or hook file makes the identity untrusted, and its shim refuses.
func trustedByName(name string) (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	all, _ := resolve.ListWorkspaces(cfg)
	for i := range all {
		if all[i].Name != name {
			continue
		}
		if v := trust.Check(name, all[i].Hash); !v.OK {
			return nil, fmt.Errorf("workspace %s is %s", name, v.Reason)
		}
		return &all[i], nil
	}
	return nil, fmt.Errorf("no workspace %q", name)
}

// passThrough runs the real program called name: the first on PATH that is not in the shim's
// own folder, with the arguments and the standard streams as they are. 127 when there is none.
func passThrough(exe, name string, args []string, stderr io.Writer) int {
	dir := filepath.Dir(exe)
	var kept []string
	for _, e := range filepath.SplitList(os.Getenv("PATH")) {
		if e == "" || sameDir(e, dir) {
			continue
		}
		kept = append(kept, e)
	}
	env := []string{"PATH=" + strings.Join(kept, string(os.PathListSeparator)), "PATHEXT=" + os.Getenv("PATHEXT")}
	real, err := envplan.LookPath(name, env)
	if err != nil {
		fmt.Fprintf(stderr, "%s: command not found\n", name)
		return 127
	}
	if a, e1 := os.Stat(real); e1 == nil {
		if b, e2 := os.Stat(exe); e2 == nil && os.SameFile(a, b) {
			fmt.Fprintf(stderr, "%s: command not found\n", name)
			return 127
		}
	}
	cmd := exec.Command(real, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 126
	}
	return 0
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(strings.Trim(a, `"`)), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
