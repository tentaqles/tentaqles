package main

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/tentaqles/tentaqles/pkg/setupapi"
)

// Hubs lists the running hubs (serve records that answer their ping), for the Board tab.
func (a *App) Hubs() []setupapi.Hub {
	ctx, cancel := context.WithTimeout(a.context(), 3*time.Second)
	defer cancel()
	return setupapi.LiveHubs(ctx)
}

// OpenBoard finds a hub serving ws, or starts `tq hub serve` for it as a child of this app, and
// returns the board URL for the frontend to navigate the window to.
//
// A successful call marks the window as on a board (the frontend navigates right away), which
// arms View > Setup.
func (a *App) OpenBoard(ws string) (BoardTarget, error) {
	t, err := a.board.open(a.context(), ws)
	if err == nil {
		a.onBoard.Store(true)
	}
	return t, err
}

// ServiceCatalog lists the accepted tracker, auth, chat and email names for the Services step.
func (a *App) ServiceCatalog() setupapi.Services {
	return setupapi.ServiceCatalog()
}

// shutdown stops the hubs this app started; hubs started elsewhere keep running.
func (a *App) shutdown(context.Context) {
	a.board.stopAll()
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// appMenu adds View > Setup, which brings the wizard back after the window navigated to a
// board (the hub page cannot call back into the app).
func appMenu(a *App, goos string) *menu.Menu {
	m := menu.NewMenu()
	if goos == "darwin" {
		m.Append(menu.AppMenu())
		m.Append(menu.EditMenu())
	}
	view := m.AddSubmenu("View")
	view.AddText("Setup", keys.CmdOrCtrl("1"), func(*menu.CallbackData) {
		if a.ctx != nil && a.leaveBoard() {
			runtime.WindowReloadApp(a.ctx)
		}
	})
	return m
}

// leaveBoard reports whether View > Setup should reload the app: only when the window was sent
// to a board. On the wizard itself the item does nothing, so it never throws away unapplied
// wizard state.
func (a *App) leaveBoard() bool {
	return a.onBoard.Swap(false)
}
