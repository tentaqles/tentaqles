package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/notify"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// hubAgent: an agent session may add cards and comment, nothing else (spec §15). A session the
// hub launched (TQ_HUB_TASK) is one too, whatever else its environment says.
func hubAgent() bool { return packs.AgentSession() }

// hubCardFromTask reads the card id of the run this session belongs to (TQ_HUB_TASK).
func hubCardFromTask() (string, string, bool) {
	var t struct {
		Hub       string `json:"hub"`
		Workspace string `json:"workspace"`
	}
	raw := strings.TrimSpace(os.Getenv("TQ_HUB_TASK"))
	if raw == "" || json.Unmarshal([]byte(raw), &t) != nil || t.Hub == "" {
		return "", "", false
	}
	return t.Hub, t.Workspace, true
}

func untrustedErr(name string) error {
	return fmt.Errorf("workspace %s is untrusted; run: tq allow %s", name, name)
}

// manifestTrusted is the trust check tq commands make (trust.IsTrusted of the manifest's hash),
// re-hashing the manifest now: the hub checks it before every round, and a skill may have edited
// the manifest since the hub resolved the workspace.
func manifestTrusted(ws *resolve.Workspace) error {
	if ws.ManifestPath == "" {
		return nil
	}
	h, err := trust.HashFile(ws.ManifestPath)
	if err != nil {
		return fmt.Errorf("cannot read the workspace manifest (%v); run: tq allow %s", err, ws.Name)
	}
	if !trust.IsTrusted(h) {
		return untrustedErr(ws.Name)
	}
	return nil
}

// hubWorkspace is --workspace, else the workspace containing the cwd, else TQ_HUB_TASK's; always trusted.
func hubWorkspace(name string) (*resolve.Workspace, error) {
	if name == "" {
		cfg, err := registry.Load()
		if err != nil {
			return nil, err
		}
		cwd, _ := os.Getwd()
		res := resolve.Resolve(cwd, cfg)
		switch {
		case res.Workspace != nil:
			return res.Workspace, nil
		case res.Untrusted != nil:
			return nil, untrustedErr(res.Untrusted.Name)
		}
		if _, ws, ok := hubCardFromTask(); ok && ws != "" {
			name = ws
		} else {
			return nil, fmt.Errorf("no workspace here (%s); cd into a client workspace or pass --workspace", res.Reason)
		}
	}
	ws, err := findWorkspace(name)
	if err != nil {
		return nil, err
	}
	if !trust.IsTrusted(ws.Hash) {
		return nil, untrustedErr(ws.Name)
	}
	return ws, nil
}

// hubClasses: the manifest's data classes plus secrets; every detector when not classified.
func hubClasses(ws *resolve.Workspace) []string {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() {
		return []string{"all"}
	}
	return append(append([]string(nil), ws.Manifest.Data.Classes...), "secrets")
}

func newHubService(ws *resolve.Workspace, warn func(string), notifyOn bool) (*runner.Service, error) {
	st, err := store.Open(ws.Name, hubClasses(ws))
	if err != nil {
		return nil, err
	}
	env := func() []string { return envplan.Environ(ws, os.Environ()) }
	configDir := envplan.Desired(ws)["CLAUDE_CONFIG_DIR"]
	if configDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(h, ".claude")
		}
	}
	var g gate.API
	if gc, err := gate.ForWorkspace(configDir, env()); err == nil {
		g = gc
	} else {
		// The error names only the plugin/bash discovery outcome (a folder path at most).
		warn("gate unavailable: " + err.Error())
		g = gate.Unavailable{Err: err}
	}
	bin := []string{"claude"}
	if raw := strings.TrimSpace(os.Getenv("TQ_HUB_CLAUDE_CMD")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &bin); err != nil || len(bin) == 0 {
			st.Close()
			return nil, errors.New("TQ_HUB_CLAUDE_CMD must be a JSON array of strings")
		}
	}
	adapters := map[string]adapter.Adapter{
		store.AdapterClaude: adapter.Claude{Bin: bin, Args: func(a []string) []string { return claudeArgs(ws, a, warn) }},
	}
	if raw, ok, _ := st.Setting(store.SettingProcessCommand); ok {
		var argv []string
		if json.Unmarshal([]byte(raw), &argv) == nil && len(argv) > 0 {
			adapters[store.AdapterProcess] = adapter.Process{Command: argv}
		}
	}
	var notifyFn func(context.Context, string, string) error
	if notifyOn {
		notifyFn = notify.Send
	}
	svc, err := runner.New(runner.Config{Workspace: ws.Name, Root: ws.Root, Store: st, Gate: g, Adapters: adapters, Env: env,
		Notify: notifyFn, Actor: gate.Actor(), GateURL: "http://127.0.0.1:8765/",
		Trusted: func() error { return manifestTrusted(ws) }, RepoCheck: runner.GitPreflight, HasTracker: ws.Manifest.HasTracker()})
	if err != nil {
		st.Close()
		return nil, err
	}
	svc.OnApprove = func(c store.Card, r store.Run) {
		if _, err := web.ExportReview(context.Background(), svc, c, r); err != nil {
			warn("review export: " + err.Error())
		}
	}
	return svc, nil
}

// checkBrowserURL accepts only a plain-http loopback URL (any port): the opener never sees a
// file:, a scheme handler or a remote host.
func checkBrowserURL(raw string) error {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "http" && u.User == nil && u.Opaque == "" {
		switch u.Hostname() {
		case "127.0.0.1", "localhost", "::1":
			return nil
		}
	}
	return fmt.Errorf("refusing to open %q: only http://127.0.0.1, http://localhost or http://[::1] URLs are opened", raw)
}

// openBrowser opens a loopback URL with the platform's opener; nothing runs through a shell.
func openBrowser(raw string) error {
	if err := checkBrowserURL(raw); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", raw).Start()
	case "darwin":
		return exec.Command("open", raw).Start()
	}
	return exec.Command("xdg-open", raw).Start()
}
