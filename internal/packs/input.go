package packs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// ChangedFile is one changed path (slash form, repo relative). Status is
// 'A', 'M', 'D' or 'R' (OldPath set for R).
type ChangedFile struct {
	Path, OldPath string
	Status        byte
}

// GitReader reads the old side of a change and the tracked file list.
type GitReader interface {
	// Show returns the file at ref (":" is the index). A path absent at ref
	// is an error.
	Show(ref, path string) ([]byte, error)
	// ListFiles lists the tracked files (slash form).
	ListFiles() ([]string, error)
}

// ToolRunner finds and runs external tools (implemented by toolrun.Runner).
type ToolRunner interface {
	Probe(pack, tool string) toolrun.Probe
	Run(ctx context.Context, s toolrun.Spec) (toolrun.Output, error)
}

// Baseline matches external-tool findings against the pack baselines (§2.6a).
type Baseline interface {
	Match(checkID string, f Finding) bool
}

// Input is what a check (and a flag, and a doctor probe) sees.
type Input struct {
	Root     string // repo root
	Base     string // ref the old side is read from ("" = no old side)
	Changed  []ChangedFile
	All      bool               // --all: every tracked file counts as changed (status A)
	Staged   bool               // --staged: the new side is the index
	Manifest *manifest.Manifest // nil outside a trusted workspace
	Config   Config
	Git      GitReader
	Tools    ToolRunner
	Baseline Baseline
	Budget   time.Time // deadline for the whole run

	ws, wsRoot, pack string
	classes          []string
}

// Classes are the workspace's exit classes, for redact on anything that
// leaves the process (tool output tails: out.Tail(20, in.Classes())). Nil
// means every detector.
func (in *Input) Classes() []string { return in.classes }

// SetClasses sets the exit classes (the CLI does; RunOptions.Classes too).
func (in *Input) SetClasses(c []string) { in.classes = c }

// SetWorkspace records the workspace (registry name and the folder holding
// .tentaqles.yaml). The CLI calls it; tests use it to point StateDir at
// $TQ_HOME/packs/<ws>/.
func (in *Input) SetWorkspace(name, root string) { in.ws, in.wsRoot = name, root }

// Workspace is the registry name of the trusted workspace, "" outside one.
func (in *Input) Workspace() string { return in.ws }

// WorkspaceRoot is the folder holding .tentaqles.yaml, "" outside a workspace.
func (in *Input) WorkspaceRoot() string { return in.wsRoot }

// Pack is the pack whose check, flag or probe is running.
func (in *Input) Pack() string { return in.pack }

// ForPack returns a shallow copy of in bound to pack (what StateDir and
// Settings use). The framework calls it before each check.
func (in *Input) ForPack(pack string) *Input {
	c := *in
	c.pack = pack
	return &c
}

// Settings is the running pack's merged config.
func (in *Input) Settings() Settings { return in.Config.Pack(in.pack) }

// StateDir is $TQ_HOME/packs/<ws>/<pack>/, created (0700) on first call. It
// is "" outside a trusted workspace or when no pack is bound.
func (in *Input) StateDir() string {
	return StateDir(in.ws, in.pack)
}

// StateDir is $TQ_HOME/packs/<ws>/<pack>/, created (0700); "" when ws or
// pack is empty or not a plain name.
func StateDir(ws, pack string) string {
	if !plainName(ws) || !plainName(pack) {
		return ""
	}
	d := filepath.Join(paths.Home(), "packs", ws, pack)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return ""
	}
	return d
}

func stateDirPath(ws, pack string) string {
	if !plainName(ws) || !plainName(pack) {
		return ""
	}
	return filepath.Join(paths.Home(), "packs", ws, pack)
}

func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\:`) && strings.TrimSpace(s) == s
}

// ChangedPaths returns the new-side paths of the changed set, minus deletions.
func (in *Input) ChangedPaths() []string {
	var out []string
	for _, c := range in.Changed {
		if c.Status != 'D' {
			out = append(out, c.Path)
		}
	}
	return out
}

// AnyChanged reports whether a changed (not deleted) path matches one of the
// globs (manifest.GlobMatch; a glob without a slash matches the base name).
func (in *Input) AnyChanged(globs ...string) bool {
	for _, c := range in.Changed {
		if c.Status == 'D' {
			continue
		}
		for _, g := range globs {
			if MatchGlob(g, c.Path) {
				return true
			}
		}
	}
	return false
}

// MatchGlob matches a slash path against a glob: ** spans segments; a glob
// with no slash matches the base name at any depth; a trailing slash means
// "under this directory".
func MatchGlob(glob, p string) bool {
	glob = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(glob), `\`, "/"), "./")
	p = strings.TrimPrefix(strings.ReplaceAll(p, `\`, "/"), "./")
	if glob == "" {
		return false
	}
	if strings.HasSuffix(glob, "/") {
		glob += "**"
	}
	if !strings.Contains(glob, "/") {
		base := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			base = p[i+1:]
		}
		ok, _ := filepath.Match(glob, base)
		return ok
	}
	return manifest.GlobMatch(glob, p)
}

// ReadNew reads the new side of path: the index with --staged, otherwise the
// working tree. Reads are capped at max bytes (64 MiB when max <= 0).
func (in *Input) ReadNew(path string, max int64) ([]byte, error) {
	if max <= 0 {
		max = 64 << 20
	}
	if in.Staged && in.Git != nil {
		b, err := in.Git.Show(":", path)
		if err == nil && int64(len(b)) > max {
			return nil, fmt.Errorf("%s is larger than %d bytes", path, max)
		}
		return b, err
	}
	return readCapped(filepath.Join(in.Root, filepath.FromSlash(path)), max)
}

// ReadOld reads the old side of path at Base. The error wraps os.ErrNotExist
// when there is no old side (Base "" or --all) or the path is absent at Base
// (an added file). A renamed file's old side is ReadOld(c.OldPath).
func (in *Input) ReadOld(path string) ([]byte, error) {
	if in.Base == "" || in.Git == nil {
		return nil, os.ErrNotExist
	}
	return in.Git.Show(in.Base, path)
}
