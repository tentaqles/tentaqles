package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func newUnattendedCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "unattended",
		Short: "Skip tq's confirms in bypass/auto sessions of chosen workspaces or projects",
		Long: `Unattended mode lets tq's "ask" rules pass without a prompt when a Claude Code
session runs in bypassPermissions or auto mode inside a workspace (or project
folder) you listed. Deny rules are never skipped there, and neither are the rules in
guard.unattended.keep_asking (default: tq/iac-apply, tq/remote-migration) or
tq/guard-change. It only takes effect once bypass trust is granted:
tq allow --bypass NAME.

  tq unattended on  [--ws NAME] [--project PATH] [--codex-sandbox MODE]
  tq unattended off [--ws NAME] [--project PATH]
  tq unattended status [--ws NAME]
  tq unattended log [--ws NAME] [--since 24h]

on and off edit the workspace manifest's guard.unattended block and re-trust it.
Inside Claude Code, on and off ask you first (rule tq/guard-change), so an
agent cannot switch the mode by itself.`,
	}
	var ws, project, sandbox, since string
	on := &cobra.Command{
		Use: "on", Short: "Turn unattended mode on for the workspace or one project", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return unattendedChange(c, ws, true, project, sandbox)
		},
	}
	on.Flags().StringVar(&project, "project", "", "a folder inside the workspace (default: the whole workspace)")
	on.Flags().StringVar(&sandbox, "codex-sandbox", "", "Codex sandbox mode: "+strings.Join(manifest.CodexSandboxes, "|"))
	off := &cobra.Command{
		Use: "off", Short: "Turn unattended mode off for the workspace or one project", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return unattendedChange(c, ws, false, project, "")
		},
	}
	off.Flags().StringVar(&project, "project", "", "only this folder (default: the workspace and every project)")
	status := &cobra.Command{
		Use: "status", Short: "Show what unattended mode covers and whether it is effective", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			w, err := guardTarget(ws, false)
			if err != nil {
				return err
			}
			return unattendedStatus(c, w)
		},
	}
	logc := &cobra.Command{
		Use: "log", Short: "List the confirms unattended mode skipped", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			w, err := guardTarget(ws, false)
			if err != nil {
				return err
			}
			d, err := time.ParseDuration(since)
			if err != nil || d <= 0 {
				return fmt.Errorf("--since must be a positive duration such as 24h, got %q", since)
			}
			for _, r := range guardlog.ReadAll(w.Name, time.Now().Add(-d)) {
				fmt.Fprintf(c.OutOrStdout(), "%s  %s  %s  %s  %s  %s\n",
					r.Time.Format(time.RFC3339), r.Scope, r.Mode, strings.Join(r.Rules, ","), r.Tool, r.Command)
			}
			return nil
		},
	}
	logc.Flags().StringVar(&since, "since", "24h", "how far back to list")
	for _, s := range []*cobra.Command{on, off, status, logc} {
		s.Flags().StringVar(&ws, "ws", "", "workspace (default: the current folder's)")
	}
	c.AddCommand(on, off, status, logc)
	return c
}

func unattendedChange(c *cobra.Command, wsName string, on bool, project, sandbox string) error {
	w, err := guardTarget(wsName, false)
	if err != nil {
		return err
	}
	if err := unattendedEdit(w, on, project, sandbox); err != nil {
		return err
	}
	w, err = guardTarget(w.Name, false)
	if err != nil {
		return err
	}
	word := "off"
	if on {
		word = "on"
	}
	scope := unattendedScopes(w.Manifest.Guard.Unattended)
	if len(scope) == 0 {
		scope = []string{"nothing covered"}
	}
	out := c.OutOrStdout()
	fmt.Fprintf(out, "unattended %s for %s (%s); manifest re-trusted\n", word, w.Name, strings.Join(scope, ", "))
	if on && !trust.IsBypassAllowed(w.Hash) {
		fmt.Fprintf(out, "not effective until you run: tq allow --bypass %s\n", w.Name)
	}
	renderCodex(c, w, false)
	return nil
}

// renderCodex renders the unattended settings into ws's Codex config when the
// workspace isolates a codex identity. Callers pass a workspace resolved after
// any manifest rewrite so the bypass-trust check sees the current hash.
func renderCodex(c *cobra.Command, ws *resolve.Workspace, dryRun bool) {
	if ws == nil || ws.Manifest == nil || !slices.Contains(ws.Manifest.IdentityNames(), "codex") {
		return
	}
	res, err := codexcfg.Render(paths.IdentityDir(ws.Name, "codex"), codexcfg.Desired(ws.Manifest, trust.IsBypassAllowed(ws.Hash)), dryRun)
	switch {
	case err != nil:
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: %v\n", ws.Name, err)
	case res.Conflict != "":
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: not changed: %s (%s)\n", ws.Name, res.Conflict, res.Path)
	case res.Changed && dryRun:
		fmt.Fprintf(c.OutOrStdout(), "%s codex: would change (%s)\n", ws.Name, res.Path)
	case res.Changed:
		fmt.Fprintf(c.OutOrStdout(), "%s codex: updated (%s)\n", ws.Name, res.Path)
	}
}

// unattendedEdit turns unattended mode on or off in w's manifest: for the
// whole workspace, or for one project folder inside it. sandbox, when set,
// is stored as codex_sandbox (on only).
func unattendedEdit(w *resolve.Workspace, on bool, project, sandbox string) error {
	if sandbox != "" {
		ok := false
		for _, s := range manifest.CodexSandboxes {
			ok = ok || s == sandbox
		}
		if !ok {
			return fmt.Errorf("--codex-sandbox must be one of %s, got %q", strings.Join(manifest.CodexSandboxes, "|"), sandbox)
		}
	}
	rel := ""
	if project != "" {
		var err error
		if rel, err = resolveUnattendedProject(w.Root, project); err != nil {
			return err
		}
	}
	return rewriteManifestGuard(w, func(gb *guardBlock) error {
		u := gb.Unattended
		if on {
			if u == nil {
				u = &manifest.Unattended{}
			}
			if rel == "" {
				u.Workspace = true
			} else {
				u.Projects = appendUniq(u.Projects, rel)
			}
			if sandbox != "" {
				u.CodexSandbox = sandbox
			}
			gb.Unattended = u
			return nil
		}
		if u == nil {
			if rel != "" {
				return fmt.Errorf("no project %s in guard.unattended.projects", rel)
			}
			return nil
		}
		if rel == "" {
			// Off for everything, but keep_asking and codex_sandbox are
			// settings the user chose: they stay for the next `on`.
			u.Workspace = false
			u.Projects = nil
		} else {
			var keep []string
			for _, p := range u.Projects {
				if !sameProject(manifest.CleanProject(p), rel) {
					keep = append(keep, p)
				}
			}
			if len(keep) == len(u.Projects) {
				return fmt.Errorf("no project %s in guard.unattended.projects", rel)
			}
			u.Projects = keep
		}
		if !u.Configured() && u.KeepAsking == nil && u.CodexSandbox == "" {
			u = nil
		}
		gb.Unattended = u
		return nil
	})
}

// sameProject compares two cleaned project paths, ignoring case on Windows.
func sameProject(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// resolveUnattendedProject resolves p (relative paths against root) to a slash
// path strictly inside root. The root itself, outside paths and missing
// folders are refused.
func resolveUnattendedProject(root, p string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("--project %s: not a folder", abs)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return "", errors.New("--project is outside the workspace")
	}
	rel = filepath.ToSlash(rel)
	switch {
	case rel == ".":
		return "", errors.New("--project is the workspace root: use `tq unattended on` without --project")
	case rel == ".." || strings.HasPrefix(rel, "../"):
		return "", fmt.Errorf("--project %s is outside the workspace %s", abs, absRoot)
	}
	return rel, nil
}

func unattendedScopes(u *manifest.Unattended) []string {
	var s []string
	if u == nil {
		return s
	}
	if u.Workspace {
		s = append(s, "workspace")
	}
	for _, p := range u.Projects {
		s = append(s, "project "+manifest.CleanProject(p))
	}
	return s
}

func unattendedStatus(c *cobra.Command, w *resolve.Workspace) error {
	out := c.OutOrStdout()
	u := w.Manifest.Guard.Unattended
	fmt.Fprintf(out, "unattended mode for %s\n", w.Name)
	if scopes := unattendedScopes(u); u.Configured() {
		fmt.Fprintf(out, "scope: %s\n", strings.Join(scopes, ", "))
		if trust.IsBypassAllowed(w.Hash) {
			fmt.Fprintln(out, "effective")
		} else {
			fmt.Fprintf(out, "not effective (needs tq allow --bypass %s)\n", w.Name)
		}
	} else {
		fmt.Fprintln(out, "scope: off")
	}
	fmt.Fprintln(out, "applies in: bypassPermissions, auto sessions")
	fmt.Fprintf(out, "always asks: %s\n", strings.Join(guardcfg.AlwaysAsk, ", "))
	fmt.Fprintf(out, "keeps asking: %s\n", strings.Join(u.KeepAskingIDs(), ", "))
	g, _ := guardcfg.Load()
	for _, id := range guardcfg.UnknownKeepAsking(w.Manifest, g) {
		fmt.Fprintf(out, "warning: unknown rule id %s\n", id)
	}
	fmt.Fprintf(out, "codex sandbox: %s\n", u.Sandbox())
	n := len(guardlog.Read(w.Name, time.Now().Add(-24*time.Hour), 1<<20))
	fmt.Fprintf(out, "skipped in 24h: %d\n", n)
	return nil
}
