package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/setup"
	"github.com/tentaqles/tentaqles/pkg/setupapi"
)

// renderIdentity re-renders a workspace's Claude settings and Codex files
// ("" = every workspace) through `tq settings render`, after a guard write,
// grant or revoke, as tq mode does (final review m2). It is a test seam and
// best effort: the manifest and grant are already written, and tq doctor
// reports any drift.
var renderIdentity = func(ws string) error {
	path := setupapi.TQPath()
	if path == "" {
		return errors.New("tq is not installed")
	}
	args := []string{"settings", "render", ws}
	if ws == "" {
		args = []string{"settings", "render", "--all"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// renderWarning is the warning a failed re-render adds to a change.
func renderWarning(ws string, err error) string {
	name := ws
	if name == "" {
		name = "--all"
	}
	return fmt.Sprintf("identity settings not re-rendered (%v); run tq settings render %s", err, name)
}

// The Guard tab (spec E §10.2): bound methods over internal/setup, the same
// functions `tq mode` calls. The desktop app is a human GUI with no agent tool
// path into the Wails bindings, so it may grant; every write re-trusts the
// manifest like `tq mode set`.
//
// scope is "workspace", "global" or "project:<dir>". ws is ignored for the
// global scope.

// GuardChange is what a guard write did.
type GuardChange struct {
	Workspace string
	Scope     string
	// Looser: the layers now ask for more than the ceiling allows; the
	// session runs clamped until Proposal is granted (GrantMode).
	Looser   bool
	Proposal string
	Carried  bool   // the existing grant moved to the re-trusted manifest
	Dropped  string // a grant not carried because the change loosened past it
	Warnings []string
}

// GuardWorkspaces returns the resolved guard mode of every trusted workspace.
func (a *App) GuardWorkspaces() ([]setup.GuardView, error) {
	return setup.GuardWorkspaces()
}

// SetMode sets the mode (and the base, for custom) of one layer.
func (a *App) SetMode(ws, scope, mode, base string) (GuardChange, error) {
	return guardWrite(ws, scope, setup.GuardOp{Kind: setup.GuardOpMode, Mode: mode, Base: base})
}

// ToggleCategory sets one category of one layer (skip|ask|deny|rule).
func (a *App) ToggleCategory(ws, scope, category, value string) (GuardChange, error) {
	return guardWrite(ws, scope, setup.GuardOp{Kind: setup.GuardOpCategory, Category: category, Value: value})
}

// SetOverride sets one rules_override entry of one layer ("clear" removes it).
func (a *App) SetOverride(ws, scope, id, value string) (GuardChange, error) {
	return guardWrite(ws, scope, setup.GuardOp{Kind: setup.GuardOpOverride, RuleID: id, Value: value})
}

// GrantMode grants the workspace's current layers (the frontend confirms
// first) and returns the granted mode.
func (a *App) GrantMode(ws string) (string, error) {
	w, err := setup.FindWorkspace(ws)
	if err != nil {
		return "", err
	}
	g, err := setup.GrantMode(w)
	if err != nil {
		return "", err
	}
	_ = renderIdentity(w.Name)
	return g.Mode, nil
}

// RevokeGrant removes the workspace's grant; it runs at default (or
// stricter) until granted again.
func (a *App) RevokeGrant(ws string) error {
	w, err := setup.FindWorkspace(ws)
	if err != nil {
		return err
	}
	if err := setup.RevokeGrant(w); err != nil {
		return err
	}
	_ = renderIdentity(w.Name)
	return nil
}

func guardWrite(ws, scope string, op setup.GuardOp) (GuardChange, error) {
	sc, err := setup.ParseGuardScope(scope)
	if err != nil {
		return GuardChange{}, err
	}
	var w *resolve.Workspace
	if !sc.Global {
		if w, err = setup.FindWorkspace(ws); err != nil {
			return GuardChange{}, err
		}
	}
	res, err := setup.ApplyGuardOp(w, sc, op)
	if err != nil {
		return GuardChange{}, err
	}
	ch := GuardChange{Workspace: res.Workspace, Scope: res.Scope, Looser: res.Looser, Carried: res.Carried, Dropped: res.Dropped, Warnings: res.Warnings}
	if res.Grant != nil {
		ch.Proposal = res.Grant.Mode
	}
	name := ""
	if w != nil {
		name = w.Name
	}
	if err := renderIdentity(name); err != nil {
		ch.Warnings = append(ch.Warnings, renderWarning(name, err))
	}
	return ch, nil
}
