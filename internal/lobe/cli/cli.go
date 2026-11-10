// Package cli holds the command rules every `tentaqles lobe` verb follows: which client a command works
// on, whether a Claude Code or Codex session may run it under its login, and which verbs only a person
// may run. It imports only the four repository packages the lobe may use, and internal/lobe/pathcmp.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/pathcmp"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Deps is what the command rules need from the process. Every field is required; internal/cmd fills
// them with the real functions, and tests with fakes.
type Deps struct {
	// InAgent reports whether the command runs inside an agent session, that is whether CLAUDECODE is
	// set (internal/cmd: insideAgent). It is a convenience check, not a boundary.
	InAgent func() bool
	// Getenv reads an environment variable (internal/cmd: os.Getenv). The login checks read
	// CLAUDE_CONFIG_DIR and CODEX_HOME through it.
	Getenv func(string) string
	// Getwd returns the folder the command runs in (internal/cmd: os.Getwd). The client rule resolves
	// it to a client.
	Getwd func() (string, error)
}

// Target is the client a command works on. Without clientFlag it is the client of the current folder,
// and a folder that is no client is an error. With clientFlag it is the client the flag names: a
// person's terminal may name any known client, and a name may also be a folder of a client, as
// `tentaqles edit` accepts. Inside an agent session the flag is accepted only when it names the client
// of the current folder; any other value, or any value from a folder that is no client, is refused
// before the name is looked up.
func Target(d Deps, clientFlag string) (*resolve.Workspace, error) {
	cwd, err := d.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot read the current folder: %w", err)
	}
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	if clientFlag == "" {
		return folderClient(cwd, cfg)
	}
	if d.InAgent() {
		here, err := folderClient(cwd, cfg)
		if err != nil {
			return nil, errors.New("a Claude Code session can name a client only from inside that client's folder, and this folder is not a client folder. Run the command from the client's folder, without --client")
		}
		if here.Name != clientFlag {
			return nil, fmt.Errorf("a Claude Code session works only on the client of its own folder, %s; --client %s is refused. Run the command from that client's folder, without --client", here.Name, clientFlag)
		}
		return here, nil
	}
	return namedClient(cfg, clientFlag)
}

// folderClient is the client of folder cwd, or an error that says what to do.
func folderClient(cwd string, cfg *registry.Config) (*resolve.Workspace, error) {
	r := resolve.Resolve(cwd, cfg)
	if r.Workspace != nil {
		return r.Workspace, nil
	}
	msg := "this folder is not a client folder: cd into a client's folder, or name the client with --client <name>"
	if r.Reason != "" {
		msg += " (" + r.Reason + ")"
	}
	return nil, errors.New(msg)
}

// namedClient is the client a --client value names. The value is a folder only when it is absolute or
// contains a path separator (/ or \); then an existing folder is resolved like the current folder. Any
// other value is a client's name and is looked up as one, never as a folder of the current directory: a
// subfolder called globex of acme's folder is not client globex. It follows changeTarget in
// internal/cmd/edit.go.
func namedClient(cfg *registry.Config, name string) (*resolve.Workspace, error) {
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) {
		if fi, err := os.Stat(name); err == nil && fi.IsDir() {
			abs, _ := filepath.Abs(name)
			r := resolve.Resolve(abs, cfg)
			if r.Workspace == nil {
				return nil, fmt.Errorf("%s belongs to no client (%s)", abs, r.Reason)
			}
			return r.Workspace, nil
		}
	}
	ws, err := resolve.Find(cfg, name)
	if err != nil {
		return nil, fmt.Errorf("%w; run tentaqles list to see the clients", err)
	}
	return ws, nil
}

// CheckLogin refuses, inside an agent session only, a session whose login points at anything but the
// client's own login folder. CLAUDE_CONFIG_DIR must be unset or be the client's claude folder, and
// CODEX_HOME must be unset or be the client's codex folder (paths.IdentityDir). Both unset is fine.
// Outside an agent session every login is fine. The comparison is pathcmp.SamePath: absolute, cleaned,
// case-insensitive on Windows, links not resolved.
func CheckLogin(d Deps, ws *resolve.Workspace) error {
	if !d.InAgent() {
		return nil
	}
	if ws == nil {
		return errors.New("no client to check the login of")
	}
	checks := []struct{ env, cli string }{
		{"CLAUDE_CONFIG_DIR", "claude"},
		{"CODEX_HOME", "codex"},
	}
	for _, c := range checks {
		v := d.Getenv(c.env)
		if v == "" {
			continue
		}
		want := paths.IdentityDir(ws.Name, c.cli)
		if !pathcmp.SamePath(v, want) {
			return fmt.Errorf("%s points at %s, which is not the %s login folder of client %s (%s). A session may use only its own client's login: unset %s, or point it at that folder", c.env, v, c.cli, ws.Name, want, c.env)
		}
	}
	return nil
}

// RefusePerson refuses inside an agent session. It is for verbs only a person may run, such as
// `tentaqles lobe set`, which changes settings after a typed yes.
func RefusePerson(d Deps) error {
	if d.InAgent() {
		return errors.New("this command is for a person and cannot run inside a Claude Code session. Run it in your own terminal, outside Claude Code")
	}
	return nil
}
