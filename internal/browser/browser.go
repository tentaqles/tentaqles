// Package browser finds a client's browser and builds the command that opens a URL in the
// client's profile (ADR-0016): Chrome or Edge, by install path, with --profile-directory, never
// through the system's default handler, which opens whatever profile the OS last used.
//
// It never runs a shell: the program and every argument are separate argv elements, so a URL
// or a profile name cannot be read as a command.
package browser

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// ErrNotInstalled is Find's answer for a browser with no install on this machine.
var ErrNotInstalled = errors.New("not installed")

// ProfileFlag and DataDirFlag are the browser flags that choose a profile.
const (
	ProfileFlag = "--profile-directory"
	DataDirFlag = "--user-data-dir"
)

// Locate returns the install paths a browser may have, best first. Tests replace it with a
// fake program; nothing else does.
var Locate = defaultLocate

// Start launches the browser: it never waits for it (a first launch is the browser itself).
// Tests replace it to run the fake to its end.
var Start = start

func defaultLocate(name string) []string {
	var out []string
	add := func(parts ...string) {
		out = append(out, filepath.Join(parts...))
	}
	switch runtime.GOOS {
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			base := os.Getenv(env)
			if base == "" {
				continue
			}
			switch name {
			case "chrome":
				add(base, "Google", "Chrome", "Application", "chrome.exe")
			case "edge":
				add(base, "Microsoft", "Edge", "Application", "msedge.exe")
			}
		}
	case "darwin":
		switch name {
		case "chrome":
			add("/Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome")
		case "edge":
			add("/Applications", "Microsoft Edge.app", "Contents", "MacOS", "Microsoft Edge")
		}
	default:
		var names []string
		switch name {
		case "chrome":
			names = []string{"google-chrome", "google-chrome-stable"}
		case "edge":
			names = []string{"microsoft-edge", "microsoft-edge-stable"}
		}
		for _, n := range names {
			if p, err := exec.LookPath(n); err == nil {
				if abs, err := filepath.Abs(p); err == nil {
					out = append(out, abs)
				}
			}
		}
	}
	return out
}

// Find is the install path of a listed browser (chrome, edge): the first candidate that is a
// regular file. ErrNotInstalled when there is none.
func Find(name string) (string, error) {
	if !known(name) {
		return "", fmt.Errorf("unknown browser %q (known: chrome, edge)", name)
	}
	for _, p := range Locate(name) {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", ErrNotInstalled
}

func known(name string) bool {
	for _, n := range manifest.BrowserNames {
		if n == name {
			return true
		}
	}
	return false
}

// UserDataRoot is the folder a browser keeps its profiles in, for tq doctor: the default user
// data folder of the OS. "" when it cannot be told (no home folder).
var UserDataRoot = defaultUserDataRoot

func defaultUserDataRoot(name string) string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LocalAppData")
		if base == "" {
			return ""
		}
		switch name {
		case "chrome":
			return filepath.Join(base, "Google", "Chrome", "User Data")
		case "edge":
			return filepath.Join(base, "Microsoft", "Edge", "User Data")
		}
	case "darwin":
		if home == "" {
			return ""
		}
		switch name {
		case "chrome":
			return filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
		case "edge":
			return filepath.Join(home, "Library", "Application Support", "Microsoft Edge")
		}
	default:
		if home == "" {
			return ""
		}
		switch name {
		case "chrome":
			return filepath.Join(home, ".config", "google-chrome")
		case "edge":
			return filepath.Join(home, ".config", "microsoft-edge")
		}
	}
	return ""
}

// ValidateURL accepts an http or https URL with a host, as one word: no space, quote or control
// character, and not a value that starts with '-' (a browser flag). It returns the URL as
// given.
func ValidateURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("no URL given")
	}
	if strings.HasPrefix(raw, "-") {
		return "", fmt.Errorf("refused: %q starts with '-' and would be read as a browser flag", raw)
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f || r == '"' || r == '\'' || r == '`' || r == '\\' || r == '<' || r == '>' || r == '^' || r == '|' {
			return "", fmt.Errorf("refused: %q is not a plain http or https URL", raw)
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("refused: %q is not a URL", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("refused: %q is not an http or https URL (tq opens only those)", raw)
	}
	if u.Host == "" || u.Opaque != "" {
		return "", fmt.Errorf("refused: %q has no host", raw)
	}
	return raw, nil
}

// IsWebURL reports whether raw is a URL ValidateURL accepts.
func IsWebURL(raw string) bool {
	_, err := ValidateURL(raw)
	return err == nil
}

// Pick chooses the client's browser: the first listed, or the one named by want ("" for the
// first). The error says what the client has.
func Pick(list []manifest.Browser, want string) (manifest.Browser, error) {
	if len(list) == 0 {
		return manifest.Browser{}, errors.New("no browser is set for this client")
	}
	if want == "" {
		return list[0], nil
	}
	want = strings.ToLower(strings.TrimSpace(want))
	var names []string
	for _, b := range list {
		if b.Name == want {
			return b, nil
		}
		names = append(names, b.Name)
	}
	return manifest.Browser{}, fmt.Errorf("%s is not one of this client's browsers (%s)", want, strings.Join(names, ", "))
}

// Args is the browser's argument list for opening rawURL in b's profile.
func Args(b manifest.Browser, rawURL string) ([]string, error) {
	for _, r := range b.Profile {
		if r < 0x20 || r == 0x7f {
			return nil, fmt.Errorf("profile name of %s has a control character", b.Name)
		}
	}
	u, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}
	return []string{ProfileFlag + "=" + b.Profile, u}, nil
}

// Open opens rawURL in the client's browser list (Pick), by install path, in the client's
// profile. Nothing starts unless every step before it holds: the URL is a web URL, a browser is
// chosen, it is installed.
func Open(list []manifest.Browser, want, rawURL string) (manifest.Browser, error) {
	if _, err := ValidateURL(rawURL); err != nil {
		return manifest.Browser{}, err
	}
	b, err := Pick(list, want)
	if err != nil {
		return b, err
	}
	exe, err := Find(b.Name)
	if err != nil {
		if errors.Is(err, ErrNotInstalled) {
			return b, fmt.Errorf("%s is not installed", b.Name)
		}
		return b, err
	}
	args, err := Args(b, rawURL)
	if err != nil {
		return b, err
	}
	if err := Start(exe, args); err != nil {
		return b, fmt.Errorf("could not start %s: %w", b.Name, err)
	}
	return b, nil
}

// ProfileExists reports whether browser name has a folder for profile in its user data folder.
// known is false when the user data folder cannot be told.
func ProfileExists(name, profile string) (exists, known bool) {
	root := UserDataRoot(name)
	if root == "" {
		return false, false
	}
	if profile == "" || strings.ContainsAny(profile, `/\`) || profile == "." || profile == ".." {
		return false, true
	}
	fi, err := os.Stat(filepath.Join(root, profile))
	return err == nil && fi.IsDir(), true
}
