package main

import (
	"context"
	"os/exec"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/tentaqles/tentaqles/cli/pkg/setupapi"
)

// App struct binds the setupapi facade to the Wails frontend.
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Providers returns the merged embedded+user provider catalog.
func (a *App) Providers() ([]setupapi.Provider, error) {
	return setupapi.Providers()
}

// DetectShells returns the shells tq can offer to install hooks into.
func (a *App) DetectShells() ([]string, error) {
	return setupapi.DetectShells()
}

// HooksStatus reports the install state for every known shell.
func (a *App) HooksStatus() ([]setupapi.HookStatus, error) {
	return setupapi.HooksStatus()
}

// DefaultBase returns the default workspace base directory.
func (a *App) DefaultBase() string {
	return setupapi.DefaultBase()
}

// ExistingWorkspaces lists every workspace under every registered base.
func (a *App) ExistingWorkspaces() ([]setupapi.Workspace, error) {
	return setupapi.ExistingWorkspaces()
}

// ValidatePlan checks structural rules on the plan.
func (a *App) ValidatePlan(p setupapi.Plan) error {
	return setupapi.ValidatePlan(p)
}

// Preview is read-only: it reports what Apply would do without writing
// anything.
func (a *App) Preview(p setupapi.Plan) ([]setupapi.Change, error) {
	return setupapi.Preview(p)
}

// ToolCheck probes each company's identities' CLI tools.
func (a *App) ToolCheck(p setupapi.Plan) (map[string][]setupapi.ToolResult, error) {
	return setupapi.ToolCheck(p)
}

// Apply executes the plan.
func (a *App) Apply(p setupapi.Plan) (setupapi.Report, error) {
	return setupapi.Apply(p)
}

// Doctor runs the read-only identity health checks.
func (a *App) Doctor() ([]setupapi.Finding, error) {
	return setupapi.Doctor()
}

// LoginCommand returns the "tq login <ws> <id>" invocation string.
func (a *App) LoginCommand(ws, id string) string {
	return setupapi.LoginCommand(ws, id)
}

// TQVersion runs "tq version" if tq is on PATH.
func (a *App) TQVersion() (string, error) {
	return setupapi.TQVersion()
}

// InstallTQ copies the tq binary at fromPath into tq's per-user install
// directory.
func (a *App) InstallTQ(fromPath string) (string, error) {
	return setupapi.InstallTQ(fromPath)
}

// AddCustomProvider validates and writes a minimal custom provider.
func (a *App) AddCustomProvider(id, name, category, command string, env map[string]string) (string, error) {
	return setupapi.AddCustomProvider(id, name, category, command, env)
}

// BundledTQPath looks for a tq binary bundled next to the running
// executable, falling back to the TQ_BUNDLED_PATH env var. It returns ""
// when no bundled binary is found.
func (a *App) BundledTQPath() string {
	return bundledTQPath()
}

// PickFolder opens a native directory picker and returns the chosen path
// (empty string if the user cancels).
func (a *App) PickFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a folder",
	})
}

// OpenTerminal opens a new terminal window and runs command in it.
func (a *App) OpenTerminal(command string) error {
	name, args, err := resolveTerminal(goos(), command)
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	return cmd.Start()
}

// goos is a seam over runtime.GOOS so terminalCommand can be tested for
// every OS regardless of the host running the test.
var goosFn = defaultGOOS

func goos() string { return goosFn() }
