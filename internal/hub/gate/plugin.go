package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// FindPluginRoot returns the tentaqles plugin's install folder for the Claude config dir
// (<configDir>/plugins/installed_plugins.json). TQ_HUB_PLUGIN_ROOT overrides it.
func FindPluginRoot(configDir string) (string, error) {
	if d := strings.TrimSpace(os.Getenv("TQ_HUB_PLUGIN_ROOT")); d != "" {
		return d, nil
	}
	file := filepath.Join(configDir, "plugins", "installed_plugins.json")
	raw, err := os.ReadFile(file)
	if err == nil {
		var doc struct {
			Plugins map[string][]struct {
				InstallPath string `json:"installPath"`
			} `json:"plugins"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			keys := make([]string, 0, len(doc.Plugins))
			for k := range doc.Plugins {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if name, _, _ := strings.Cut(k, "@"); name != "tentaqles" {
					continue
				}
				for _, in := range doc.Plugins[k] {
					if _, err := os.Stat(filepath.Join(in.InstallPath, "scripts", "build-gate.py")); err == nil {
						return in.InstallPath, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("the tentaqles plugin is not installed for this identity (%s); install it with /plugin in Claude Code, then retry", configDir)
}

// FindBash finds the bash that runs tq_run.sh: TQ_BASH, then bash on PATH (never WSL's
// System32 bash on Windows), then Git for Windows' usual locations.
func FindBash() (string, error) {
	if b := strings.TrimSpace(os.Getenv("TQ_BASH")); b != "" {
		return b, nil
	}
	if p, err := exec.LookPath("bash"); err == nil {
		if runtime.GOOS != "windows" || !strings.Contains(strings.ToLower(p), `\windows\system32\`) {
			return p, nil
		}
	}
	if runtime.GOOS == "windows" {
		for _, c := range []string{
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files (x86)\Git\bin\bash.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
		} {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}
	return "", errors.New("bash not found: install Git for Windows (Git Bash) or set TQ_BASH")
}

// ForWorkspace builds the client for one workspace: TQ_HUB_GATE_CMD (a JSON argv, for tests),
// else bash <plugin>/scripts/tq_run.sh build-gate.py.
func ForWorkspace(configDir string, env []string) (*Client, error) {
	if raw := strings.TrimSpace(os.Getenv("TQ_HUB_GATE_CMD")); raw != "" {
		var argv []string
		if err := json.Unmarshal([]byte(raw), &argv); err != nil || len(argv) == 0 {
			return nil, fmt.Errorf("TQ_HUB_GATE_CMD must be a JSON array of strings")
		}
		return &Client{Command: argv, Env: env}, nil
	}
	root, err := FindPluginRoot(configDir)
	if err != nil {
		return nil, err
	}
	bash, err := FindBash()
	if err != nil {
		return nil, err
	}
	return &Client{Command: []string{bash, filepath.Join(root, "scripts", "tq_run.sh"), "build-gate.py"}, Env: env}, nil
}
