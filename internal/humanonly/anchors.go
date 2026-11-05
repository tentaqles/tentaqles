package humanonly

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
	"gopkg.in/yaml.v3"
)

// UserAnchorsPath is ~/.tentaqles/anchors.yaml (the protected area): the
// terminal names the developer added with `tq settings anchors add`.
func UserAnchorsPath() string { return filepath.Join(paths.Home(), "anchors.yaml") }

type anchorsFile struct {
	Anchors []string `yaml:"anchors"`
}

// NormalizeAnchor is the form an anchor is stored and matched in: lower
// case, no directory, no .exe.
func NormalizeAnchor(name string) string { return baseName(name) }

// ValidateAnchor refuses a name that is not an executable name, or that
// would widen the check: a harness, a shell or runtime (an anchor is a
// terminal; a shell is a login shell or nothing, a runtime is any program),
// and a root or service process (it starts services and sessions alike).
func ValidateAnchor(name string) (string, error) {
	n := NormalizeAnchor(name)
	switch {
	case n == "" || n == "." || n == "..":
		return "", errors.New("an anchor is an executable name, such as my-terminal or my-terminal.exe")
	case strings.ContainsAny(name, `/\`):
		return "", fmt.Errorf("an anchor is an executable name without a directory, not %q", name)
	case harnessNames[n]:
		return "", fmt.Errorf("%s is a harness, never an anchor", n)
	case interpreters[strings.TrimPrefix(n, "-")]:
		return "", fmt.Errorf("%s is a shell or runtime: a login shell already counts, a plain one never does", n)
	case runtimeNames[n]:
		if n == "java" {
			return "", errors.New("java is a runtime: a JetBrains IDE running as java already counts by its command line, any other java never does")
		}
		return "", fmt.Errorf("%s is a runtime: any program, a harness included, can run as it", n)
	case rootNames[n]:
		if qualified[n] {
			return "", fmt.Errorf("%s is a root process: it starts services and sessions alike; the user session's instance already counts by its command line", n)
		}
		return "", fmt.Errorf("%s is a root or service process: it starts services and sessions alike, never a terminal", n)
	case len(n) > 128:
		return "", errors.New("an anchor name is at most 128 characters")
	}
	return n, nil
}

// LoadUserAnchors reads anchors.yaml; a missing file is an empty list.
func LoadUserAnchors() ([]string, error) {
	b, err := os.ReadFile(UserAnchorsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f anchorsFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", UserAnchorsPath(), err)
	}
	seen := map[string]bool{}
	var out []string
	for _, a := range f.Anchors {
		// A hand-edited file cannot grant what the command refuses: a
		// harness, a shell, a runtime or a root process is dropped.
		n, err := ValidateAnchor(a)
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// SaveUserAnchors writes anchors.yaml, private to the user.
func SaveUserAnchors(names []string) error {
	sort.Strings(names)
	b, err := yaml.Marshal(anchorsFile{Anchors: names})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(UserAnchorsPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(UserAnchorsPath(), b, 0o600)
}

// AddUserAnchor adds name (normalized) and reports whether it was new.
func AddUserAnchor(name string) (string, bool, error) {
	n, err := ValidateAnchor(name)
	if err != nil {
		return "", false, err
	}
	cur, err := LoadUserAnchors()
	if err != nil {
		return "", false, err
	}
	for _, a := range cur {
		if a == n {
			return n, false, nil
		}
	}
	return n, true, SaveUserAnchors(append(cur, n))
}

// RemoveUserAnchor removes name (normalized) and reports whether it was there.
func RemoveUserAnchor(name string) (string, bool, error) {
	n := NormalizeAnchor(name)
	cur, err := LoadUserAnchors()
	if err != nil {
		return "", false, err
	}
	var kept []string
	found := false
	for _, a := range cur {
		if a == n {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return n, false, nil
	}
	return n, true, SaveUserAnchors(kept)
}

// loadUserAnchorSet fills the walker's user anchors from anchors.yaml; a
// file that cannot be read adds nothing.
func loadUserAnchorSet() {
	names, _ := LoadUserAnchors()
	userAnchors = set(names...)
}
