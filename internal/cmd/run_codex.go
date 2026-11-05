package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// codexIsolated reports whether ws gives Codex its own CODEX_HOME.
func codexIsolated(ws *resolve.Workspace) bool {
	return ws != nil && ws.Manifest != nil && slices.Contains(ws.Manifest.IdentityNames(), "codex")
}

// codexHookResolves reports an error unless `tq` resolves on the PATH of the
// launch environment env, the PATH Codex runs its hook command with. Tests
// replace it.
var codexHookResolves = func(env []string) error {
	path := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			path = v
		}
	}
	for _, dir := range filepath.SplitList(path) {
		for _, name := range []string{"tq", "tq.exe"} {
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && fi.Mode().IsRegular() &&
				(runtime.GOOS == "windows" || fi.Mode()&0o111 != 0) {
				return nil
			}
		}
	}
	return errors.New("tq is not on the PATH Codex will run with: its hook (`tq claude-hook pre-tool-use --agent codex`) would not run and Codex would have no guard; put tq on PATH and launch again")
}

// codexArgs prepends Codex's full-bypass flag (codexcfg.LaunchArgs; PRD v2
// §7.2) when the workspace isolates a codex identity; a flag the user typed
// about asking or the sandbox passes through as typed. The shim folder that
// does this from an identity PATH is Phase 3.
func codexArgs(ws *resolve.Workspace, args []string) []string {
	if !codexIsolated(ws) {
		return args
	}
	return codexcfg.LaunchArgs(args)
}
