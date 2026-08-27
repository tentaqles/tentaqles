// Package detect probes whether provider CLIs are installed and, when they
// are not, renders per-OS install hints from the provider catalog.
package detect

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/cli/internal/providers"
)

// Result is the outcome of checking a single provider's CLI.
type Result struct {
	ID        string
	Command   string
	Path      string
	Version   string
	Installed bool
	Err       string
}

// Deps abstracts the OS calls Check needs, so tests can supply fakes.
type Deps struct {
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, name string, args ...string) (string, error)
	GOOS     string
	// Timeout bounds a single version probe. Zero means DefaultTimeout.
	Timeout time.Duration
}

// DefaultDeps returns Deps backed by the real OS and exec package.
func DefaultDeps() Deps {
	return Deps{
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			// A surviving grandchild (common with Windows .cmd shims) keeps the
			// output pipes open; WaitDelay stops that blocking us past the deadline.
			cmd.WaitDelay = 2 * time.Second
			out, err := cmd.CombinedOutput()
			return string(out), err
		},
		GOOS:    runtime.GOOS,
		Timeout: DefaultTimeout,
	}
}

// DefaultTimeout bounds one CLI version probe. Some CLIs (cloud SDKs behind
// shims) are genuinely slow to start on a cold cache.
const DefaultTimeout = 15 * time.Second

// firstLine returns the first non-empty trimmed line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// Check probes whether p's CLI is installed and, if so, its version.
func Check(p providers.Provider, d Deps) Result {
	r := Result{ID: p.ID}
	if p.CLI == nil {
		r.Err = "no CLI"
		return r
	}
	r.Command = p.CLI.Command

	path, err := d.LookPath(p.CLI.Command)
	if err != nil {
		r.Installed = false
		r.Err = err.Error()
		return r
	}
	r.Path = path
	r.Installed = true

	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := d.Run(ctx, path, p.CLI.VersionArgs...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			r.Version = ""
			r.Err = "timeout"
			return r
		}
		r.Version = firstLine(out)
		r.Err = err.Error()
		return r
	}
	r.Version = firstLine(out)
	return r
}

// CheckAll runs Check for every provider with bounded concurrency, returning
// results in the same order as ps.
func CheckAll(ps []providers.Provider, d Deps) []Result {
	results := make([]Result, len(ps))
	sem := make(chan struct{}, 8)
	done := make(chan struct{})
	for i := range ps {
		go func(i int) {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = Check(ps[i], d)
			done <- struct{}{}
		}(i)
	}
	for range ps {
		<-done
	}
	return results
}

// Hints renders ordered install-hint lines for p on the given OS.
func Hints(p providers.Provider, goos string) []string {
	var os providers.InstallOS
	switch goos {
	case "windows":
		os = p.Install.Windows
	case "darwin":
		os = p.Install.Macos
	default:
		os = p.Install.Linux
	}

	var hints []string
	switch goos {
	case "windows":
		if os.Winget != "" {
			hints = append(hints, "winget install "+os.Winget)
		}
		if os.Scoop != "" {
			hints = append(hints, "scoop install "+os.Scoop)
		}
	case "darwin":
		if os.Brew != "" {
			hints = append(hints, "brew install "+os.Brew)
		}
	default:
		if os.Apt != "" {
			hints = append(hints, "sudo apt install "+os.Apt)
		}
	}

	if os.Pip != "" {
		hints = append(hints, "pip install "+os.Pip)
	}
	if os.Npm != "" {
		hints = append(hints, "npm install -g "+os.Npm)
	}
	if os.URL != "" {
		hints = append(hints, "see "+os.URL)
	}
	if os.Note != "" {
		hints = append(hints, os.Note)
	}

	if len(hints) == 0 && p.CLI == nil {
		return []string{"no CLI to install"}
	}
	return hints
}
