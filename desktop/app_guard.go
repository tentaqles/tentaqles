package main

import (
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/setup"
)

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
	return g.Mode, nil
}

// RevokeGrant removes the workspace's grant; it runs at default (or
// stricter) until granted again.
func (a *App) RevokeGrant(ws string) error {
	w, err := setup.FindWorkspace(ws)
	if err != nil {
		return err
	}
	return setup.RevokeGrant(w)
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
	return ch, nil
}
