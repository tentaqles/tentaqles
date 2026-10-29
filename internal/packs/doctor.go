package packs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Suggest bounds (spec C §2.4).
const (
	SuggestMaxFiles = 2000
	SuggestMaxBytes = 64 << 10
)

// ToolStatus is one row of tq pack doctor.
type ToolStatus struct {
	Pack      string   `json:"pack"`
	Tool      string   `json:"tool"`
	Declared  bool     `json:"declared"`
	Installed bool     `json:"installed"`
	Path      string   `json:"path,omitempty"`
	Source    string   `json:"source,omitempty"`
	Version   string   `json:"version,omitempty"`
	Minimum   string   `json:"minimum,omitempty"`
	Checks    []string `json:"checks,omitempty"`
}

// Tools lists, per enabled pack, every tool that is declared or that an
// external check uses, with the probe result.
func Tools(in *Input) []ToolStatus {
	var out []ToolStatus
	for _, p := range in.Config.Enabled() {
		s := in.Config.Pack(p)
		rows := map[string]*ToolStatus{}
		for _, t := range s.Tools {
			rows[t] = &ToolStatus{Pack: p, Tool: t, Declared: true}
		}
		for _, c := range Checks(p) {
			if c.Kind != External {
				continue
			}
			r := rows[c.Tool]
			if r == nil {
				r = &ToolStatus{Pack: p, Tool: c.Tool, Declared: s.Declared(c.Tool)}
				rows[c.Tool] = r
			}
			r.Checks = append(r.Checks, c.ID)
			if c.MinVersion != "" && (r.Minimum == "" || toolrun.CompareVersions(c.MinVersion, r.Minimum) > 0) {
				r.Minimum = c.MinVersion
			}
		}
		names := make([]string, 0, len(rows))
		for n := range rows {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			r := rows[n]
			if in.Tools != nil {
				pr := in.Tools.Probe(p, n)
				r.Installed, r.Path, r.Source, r.Version = pr.Found, pr.Path, pr.Source, pr.Version
			}
			out = append(out, *r)
		}
	}
	return out
}

// Doctor returns the framework findings (packs-tool-missing,
// packs-tool-old, packs-suggest, packs-repo-file-invalid) plus every
// enabled pack's own probes. files lists the repository's files for the
// detection hints (nil: no packs-suggest). repoErr is the error LoadConfig
// returned for tq-packs.yaml.
func Doctor(in *Input, files []string, repoErr error) []doctor.Finding {
	var fs []doctor.Finding
	ws := in.Workspace()
	add := func(level, code, msg, fix string) {
		fs = append(fs, doctor.Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix})
	}
	if repoErr != nil {
		add("error", "packs-repo-file-invalid", repoErr.Error(), "fix tq-packs.yaml (same schema as the manifest's packs: block)")
	}
	if len(in.Config.Enabled()) > 0 && ws != "" {
		if err := ledger.KeychainAvailable(ws); err != nil {
			add("warn", "packs-keychain-missing", "the OS keychain cannot be used ("+err.Error()+"): pack approvals are signed with a key kept there, so no pack approval can be granted on this machine",
				"use a desktop session with a keychain (Windows Credential Manager, macOS Keychain, a Secret Service such as gnome-keyring on Linux)")
		}
	}
	for _, p := range in.Config.Problems {
		add("error", "packs-repo-file-invalid", "tq-packs.yaml: "+p.String(), "the repo file may only add to or tighten the manifest")
	}
	for _, t := range Tools(in) {
		switch {
		case t.Declared && !t.Installed:
			add("error", "packs-tool-missing", fmt.Sprintf("packs.%s.tools declares %s but it is not installed", t.Pack, t.Tool),
				"install "+t.Tool+" (on PATH, in the project .venv, or tool_paths in the manifest), or remove it from tools")
		case t.Declared && t.Minimum != "" && toolrun.CompareVersions(t.Version, t.Minimum) < 0:
			add("warn", "packs-tool-old", fmt.Sprintf("%s %s is below the %s pack minimum %s", t.Tool, orUnknown(t.Version), t.Pack, t.Minimum),
				"upgrade "+t.Tool)
		}
	}
	if files != nil {
		for _, s := range Suggest(in, files) {
			add("info", "packs-suggest", s, "enable it in the manifest: packs: { "+strings.SplitN(s, " ", 2)[0]+": {enabled: true} }")
		}
	}
	for _, p := range in.Config.Enabled() {
		for _, d := range doctors(p) {
			fs = append(fs, safeDoctor(d, in.ForPack(p))...)
		}
	}
	for i := range fs {
		if fs[i].Workspace == "" {
			fs[i].Workspace = ws
		}
	}
	return fs
}

func safeDoctor(d func(*Input) []doctor.Finding, in *Input) (out []doctor.Finding) {
	defer func() {
		if p := recover(); p != nil {
			out = []doctor.Finding{{Level: "error", Code: in.Pack() + "-doctor-failed", Msg: fmt.Sprint("the ", in.Pack(), " pack doctor failed: ", p)}}
		}
	}()
	return d(in)
}

// Suggest evaluates the hints of the disabled packs over at most 2,000 files
// (64 KiB read per file) and returns one line per pack whose hints match:
// "<pack> pack: <why> (<file>)". Hints never enable a pack.
func Suggest(in *Input, files []string) []string {
	if len(files) > SuggestMaxFiles {
		files = files[:SuggestMaxFiles]
	}
	var out []string
	for _, p := range Names() {
		if in.Config.IsEnabled(p) {
			continue
		}
		hints := Hints(p)
	files:
		for _, f := range files {
			for _, h := range hints {
				if !MatchGlob(h.Glob, f) {
					continue
				}
				if h.re != nil {
					b, err := readHead(filepath.Join(in.Root, filepath.FromSlash(f)), SuggestMaxBytes)
					if err != nil || !h.re.Match(b) {
						continue
					}
				}
				why := h.Why
				if why == "" {
					why = "files match " + h.Glob
				}
				out = append(out, fmt.Sprintf("%s pack: %s (%s)", p, why, f))
				break files
			}
		}
	}
	return out
}

func readHead(p string, n int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

// WalkFiles lists up to max files under root (slash, relative), skipping
// VCS, dependency and virtualenv folders, test fixtures (testdata),
// generated Databricks bundle state (.databricks) and agent worktrees
// (.claude/worktrees, copies of the repo). It is the file list of tq doctor,
// which runs over a workspace folder that need not be one repository.
func WalkFiles(root string, max int) []string {
	var out []string
	stop := errors.New("stop")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".venv", "venv", "vendor", "__pycache__", "target", "dist", "build", ".terraform",
				"testdata", ".databricks":
				return filepath.SkipDir
			case "worktrees":
				if filepath.Base(filepath.Dir(p)) == ".claude" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= max {
			return stop
		}
		return nil
	})
	return out
}

func init() {
	doctor.PackFindings = workspaceDoctor
}

// workspaceDoctor is tq doctor's packs section for one trusted workspace:
// the manifest plus the workspace root's tq-packs.yaml, tool probes, hints
// over a bounded walk, and the packs' own probes.
func workspaceDoctor(w *resolve.Workspace) []doctor.Finding {
	if w == nil || w.Manifest == nil {
		return nil
	}
	cfg, rerr := LoadConfig(w.Manifest, w.Root)
	in := &Input{Root: w.Root, Manifest: w.Manifest, Config: cfg, Budget: time.Now().Add(30 * time.Second)}
	in.SetWorkspace(w.Name, w.Root)
	tr := NewToolRunner(w.Root, cfg)
	tr.NoExecRepo = true // doctor never runs a repo-local binary
	in.Tools = tr
	if root, err := RepoRoot(context.Background(), w.Root); err == nil {
		in.Git = GitRepo{Root: root}
	}
	return Doctor(in, WalkFiles(w.Root, SuggestMaxFiles), rerr)
}

// NewToolRunner builds the tool runner for cfg (tool_paths and timeouts by pack).
func NewToolRunner(root string, cfg Config) *toolrun.Runner {
	r := &toolrun.Runner{Root: root, ToolPaths: map[string]map[string]string{}, Timeouts: map[string]map[string]time.Duration{}}
	for n, s := range cfg.Packs {
		r.ToolPaths[n] = s.ToolPaths
		r.Timeouts[n] = s.Timeouts
	}
	return r
}
