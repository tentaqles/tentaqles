package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

func newDropCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "drop <client>",
		Short: "Undo everything tentaqles set up for a client, as if it never existed",
		Long: "tentaqles drop undoes what tentaqles reach set up for a client: it deletes the client's folder under\n" +
			"~/.tentaqles/identities (its manifest, its Claude config and every CLI login in it), its browser shims,\n" +
			"the marker and the git identity file in its folder, the git rule that applied that identity, and the\n" +
			"clients folder's registration when no other client is left in it. The folder itself and your files in it\n" +
			"stay; the folder is deleted only when nothing else is left in it.\n\n" +
			"It names the client, lists everything it deletes and waits for the client's name typed back (--yes\n" +
			"skips that). It refuses to run from inside a Claude Code session.",
		Example: "  tentaqles drop acme\n  tentaqles drop C:\\repos\\acme",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if insideAgent() {
				return errors.New("tentaqles drop: run it in your own terminal, not from a Claude Code session")
			}
			p, err := planDrop(args[0])
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "tentaqles drop: about to remove the client %s. This deletes:\n", p.name)
			for _, d := range p.lines() {
				fmt.Fprintf(w, "  - %s\n", d)
			}
			fmt.Fprintln(w, "Logins in the identity folder are deleted with it; they can't be restored.")
			if !yes {
				fmt.Fprintf(w, "Type the client's name (%s) to continue: ", p.name)
				line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(line) != p.name {
					fmt.Fprintln(w)
					return errAborted
				}
			}
			return p.apply(c)
		},
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for the client's name")
	return c
}

// dropPlan is everything tentaqles drop deletes for one client. Every part is optional: a client half
// set up, or half removed by hand, is dropped as far as it exists.
type dropPlan struct {
	name     string
	root     string // the client's folder, "" when no folder holds its marker
	marker   string // root's .tentaqles.yaml, when it names this client
	gitFile  string // root's .gitconfig-tentaqles
	identity string // ~/.tentaqles/identities/<client>
	shims    string // ~/.tentaqles/shims/<client>
	base     string // the clients folder to unregister: no other client is left in it
}

// planDrop finds what drop deletes for target, a client's name or its folder. It deletes nothing.
func planDrop(target string) (*dropPlan, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	name, root := target, ""
	if fi, err := os.Stat(target); err == nil && fi.IsDir() {
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		kind, client, err := manifest.Peek(filepath.Join(abs, manifest.FileName))
		if err != nil || kind != manifest.KindMarker {
			return nil, fmt.Errorf("%s holds no tentaqles client marker: name the client instead", abs)
		}
		name, root = client, abs
		// A copied folder must never drop the real client.
		normAbs, err := registry.Normalize(abs)
		if err != nil {
			normAbs = abs
		}
		if rec, ok := cfg.Folder(name); ok && !registry.SamePath(rec, normAbs) {
			return nil, fmt.Errorf("%s holds a copy of the marker of client %s, which lives in %s: name the client instead, or delete the copied %s", abs, name, rec, manifest.FileName)
		}
	}
	// The name builds paths under ~/.tentaqles: only a client name is accepted, never a path.
	if !workspace.NameRe.MatchString(name) {
		return nil, fmt.Errorf("%q is not a client name or a client's folder", target)
	}
	if root == "" {
		root = markerFolder(cfg, name)
	}
	p := &dropPlan{name: name, root: root}
	if root != "" {
		mp := filepath.Join(root, manifest.FileName)
		if kind, client, err := manifest.Peek(mp); err == nil && kind == manifest.KindMarker && client == name {
			p.marker = mp
		}
		if exists(gitcfg.WorkspaceFile(root)) {
			p.gitFile = gitcfg.WorkspaceFile(root)
		}
		if b := filepath.Dir(root); !otherClientIn(b, root) {
			for _, rb := range cfg.Bases {
				if registry.SamePath(rb, b) {
					p.base = rb
				}
			}
		}
	}
	if d := filepath.Join(paths.IdentitiesRoot(), name); exists(d) {
		p.identity = d
	}
	if d := paths.ShimsDir(name); exists(d) {
		p.shims = d
	}
	if p.marker == "" && p.identity == "" && p.shims == "" {
		return nil, fmt.Errorf("no client %q: nothing of it under %s or in a clients folder", name, paths.Home())
	}
	return p, nil
}

// markerFolder is the folder, in a registered clients folder, whose marker names client, or "".
func markerFolder(cfg *registry.Config, client string) string {
	if rec, ok := cfg.Folder(client); ok {
		if kind, c, err := manifest.Peek(filepath.Join(rec, manifest.FileName)); err == nil && kind == manifest.KindMarker && c == client {
			return rec
		}
	}
	for _, b := range cfg.Bases {
		root := filepath.Join(b, client)
		if kind, c, err := manifest.Peek(filepath.Join(root, manifest.FileName)); err == nil && kind == manifest.KindMarker && c == client {
			return root
		}
	}
	return ""
}

// otherClientIn reports whether a folder of base other than root holds a tentaqles marker.
func otherClientIn(base, root string) bool {
	entries, err := os.ReadDir(base)
	if err != nil {
		return true // unreadable: keep the registration
	}
	for _, e := range entries {
		d := filepath.Join(base, e.Name())
		if e.IsDir() && !registry.SamePath(d, root) && exists(filepath.Join(d, manifest.FileName)) {
			return true
		}
	}
	return false
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// lines are the plan's deletions, for the confirmation.
func (p *dropPlan) lines() []string {
	var out []string
	if p.identity != "" {
		out = append(out, p.identity+" (manifest, Claude config, logins)")
	}
	if p.shims != "" {
		out = append(out, p.shims+" (browser shims)")
	}
	if p.marker != "" {
		out = append(out, p.marker+" (the client marker)")
	}
	if p.gitFile != "" {
		out = append(out, p.gitFile+" (the folder's git identity)")
	}
	if p.root != "" {
		out = append(out, "the git rule for "+p.root+" in "+gitcfg.IncludeFile())
		out = append(out, p.root+" itself, only if nothing else is left in it")
	}
	if p.base != "" {
		out = append(out, p.base+" from the registered clients folders (no other client is in it)")
	}
	return out
}

// apply deletes the plan. The marker goes first: once it is gone the folder is no client, so a run
// stopped halfway never leaves a client that resolves to deleted files.
func (p *dropPlan) apply(c *cobra.Command) error {
	w := c.OutOrStdout()
	var failed []string
	rm := func(path string, all bool) {
		if path == "" {
			return
		}
		var err error
		if all {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil && !os.IsNotExist(err) {
			failed = append(failed, fmt.Sprintf("%s: %v", path, err))
		}
	}
	rm(p.marker, false)
	rm(p.gitFile, false)
	cfg, err := registry.Load()
	if err != nil {
		return err
	}
	changed := false
	if p.base != "" {
		kept := cfg.Bases[:0]
		for _, b := range cfg.Bases {
			if !registry.SamePath(b, p.base) {
				kept = append(kept, b)
			}
		}
		cfg.Bases = kept
		changed = true
	}
	if _, ok := cfg.Folder(p.name); ok {
		cfg.Unbind(p.name)
		changed = true
	}
	if changed {
		if err := cfg.Save(); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", paths.Config(), err))
		}
	}
	// Rewrites ~/.gitconfig-tentaqles from the clients left: the dropped folder has no marker now.
	if err := workspace.SyncGit(cfg); err != nil {
		failed = append(failed, fmt.Sprintf("%s: %v", gitcfg.IncludeFile(), err))
	}
	rm(p.shims, true)
	rm(p.identity, true)
	removedRoot := false
	if p.root != "" {
		if entries, err := os.ReadDir(p.root); err == nil && len(entries) == 0 && os.Remove(p.root) == nil {
			removedRoot = true
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%s is dropped, but these were not deleted (close what holds them, then delete them by hand):\n  %s",
			p.name, strings.Join(failed, "\n  "))
	}
	fmt.Fprintf(w, "\n%s is dropped: tentaqles keeps nothing of it.\n", p.name)
	if p.root != "" && !removedRoot {
		fmt.Fprintf(w, "%s stays, with your files in it, as an ordinary folder.\n", p.root)
	}
	fmt.Fprintln(w, "A terminal open in it loses the client's identity at its next prompt; restart any claude session running there.")
	return nil
}
