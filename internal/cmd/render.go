package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// renderShims writes the browser shims of a client that lists a browser, or removes the folder
// of one that no longer does. It reports whether anything changed (or would, dryRun).
func renderShims(c *cobra.Command, ws *resolve.Workspace, dryRun bool) bool {
	if envplan.HasBrowsers(ws) {
		if dryRun {
			if !envplan.ShimsReady(ws.Name) {
				fmt.Fprintf(c.OutOrStdout(), "%s: browser shims would be written (%s)\n", ws.Name, envplan.ShimsDir(ws.Name))
				return true
			}
			return false
		}
		ch, err := envplan.WriteShims(ws.Name)
		if err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s: browser shims not written: %v (run tentaqles settings render %s)\n", ws.Name, err, ws.Name)
			return false
		}
		if ch {
			fmt.Fprintf(c.OutOrStdout(), "%s: browser shims written (%s)\n", ws.Name, envplan.ShimsDir(ws.Name))
		}
		return ch
	}
	if dryRun {
		if _, err := os.Lstat(envplan.ShimsDir(ws.Name)); err == nil {
			fmt.Fprintf(c.OutOrStdout(), "%s: browser shims would be removed\n", ws.Name)
			return true
		}
		return false
	}
	had, err := envplan.RemoveShims(ws.Name)
	if err != nil {
		fmt.Fprintf(c.ErrOrStderr(), "warning: %s: browser shims not removed: %v\n", ws.Name, err)
		return false
	}
	if had {
		fmt.Fprintf(c.OutOrStdout(), "%s: browser shims removed\n", ws.Name)
	}
	return had
}

// renderScope writes what a client's terminal needs on disk: the PATH mask folder (the stubs that
// hide the CLIs of providers outside its scope) and its browser shims. It reports whether anything
// changed (or would, dryRun).
func renderScope(c *cobra.Command, ws *resolve.Workspace, dryRun bool) bool {
	if ws == nil || ws.Manifest == nil {
		return false
	}
	changed := false
	l := envplan.LayerFor(ws)
	switch {
	case l == nil && !dryRun:
		if had, err := envplan.RemoveMask(ws.Name); err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s: PATH mask not removed: %v\n", ws.Name, err)
		} else if had {
			fmt.Fprintf(c.OutOrStdout(), "%s: PATH mask removed\n", ws.Name)
			changed = true
		}
	case l != nil && dryRun:
		if !l.MaskReady() {
			fmt.Fprintf(c.OutOrStdout(), "%s: PATH mask would be written (%s, %d names)\n", ws.Name, l.MaskDir(), len(l.Masked()))
			changed = true
		}
	case l != nil:
		if ch, err := l.WriteMask(); err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s: PATH mask not written: %v (run tentaqles settings render %s)\n", ws.Name, err, ws.Name)
		} else if ch {
			fmt.Fprintf(c.OutOrStdout(), "%s: PATH mask written (%s, %d names)\n", ws.Name, l.MaskDir(), len(l.Masked()))
			changed = true
		}
	}
	if renderShims(c, ws, dryRun) {
		changed = true
	}
	return changed
}
