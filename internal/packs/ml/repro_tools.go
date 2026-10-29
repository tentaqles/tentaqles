package ml

// External ML checks (spec C §4.2, §4.3): ml/ruff (NPY and PD rules),
// ml/lock-check (uv lock --check), ml/lock-check-poetry (poetry check
// --lock) and ml/dvc-status (dvc status --json, local only).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

const (
	toolTimeout = 60 * time.Second
	// ruffMaxArgs caps the paths passed to ruff; above it ruff runs on the
	// repo root, which keeps the command line short on Windows.
	ruffMaxArgs = 200
)

// toolResult builds the result of a tool that did not finish or printed
// something unreadable.
func toolFailure(tool string, out toolrun.Output, err error, what string) packs.Result {
	switch {
	case err != nil:
		return packs.Result{Status: packs.Fail, Detail: tool + " did not run: " + shortErr(err)}
	case out.TimedOut:
		return packs.Result{Status: packs.Fail, Detail: tool + " timed out", Tool: toolRunOf(tool, out)}
	}
	return packs.Result{Status: packs.Fail, Detail: what + "\n" + out.Tail(20, nil), Tool: toolRunOf(tool, out)}
}

func toolRunOf(tool string, out toolrun.Output) *packs.ToolRun {
	return &packs.ToolRun{Name: tool, Version: out.Version, Path: out.Path, ExitCode: out.ExitCode, DurationMS: out.Duration.Milliseconds(), TimedOut: out.TimedOut}
}

// --- ml/ruff ------------------------------------------------------------------

type ruffFinding struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Filename string `json:"filename"`
	Cell     *int   `json:"cell"`
	Location struct {
		Row int `json:"row"`
	} `json:"location"`
}

func ruffTargets(in *packs.Input) []string {
	var ps []string
	for _, p := range in.ChangedPaths() {
		if pyLike(p) {
			ps = append(ps, p)
		}
	}
	sort.Strings(ps)
	return ps
}

func runRuff(ctx context.Context, in *packs.Input) packs.Result {
	targets := ruffTargets(in)
	if len(targets) == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no Python file changed"}
	}
	if len(targets) > ruffMaxArgs {
		targets = []string{"."}
	}
	args := append([]string{"check", "--no-cache", "--exit-zero", "--select", "NPY,PD", "--output-format", "json"}, targets...)
	out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "ml", Tool: "ruff", Args: args, Dir: in.Root, Timeout: toolTimeout})
	if err != nil || out.TimedOut || out.ExitCode != 0 {
		return toolFailure("ruff", out, err, fmt.Sprintf("ruff exited %d", out.ExitCode))
	}
	var rf []ruffFinding
	if err := json.Unmarshal(bytes.TrimSpace(out.Stdout), &rf); err != nil {
		return toolFailure("ruff", out, nil, "cannot parse ruff JSON output")
	}
	r := packs.Result{Status: packs.OK, Detail: fmt.Sprintf("ruff NPY,PD over %s: %s", lintPlural(len(targets), "path"), lintPlural(len(rf), "finding")), Tool: toolRunOf("ruff", out)}
	for _, f := range rf {
		msg := f.Message
		if f.Code == "NPY002" {
			msg += " (seeds: use a seeded np.random.default_rng; see ml/seeds)"
		}
		if f.Cell != nil {
			msg = fmt.Sprintf("cell %d, line %d: %s", *f.Cell, f.Location.Row, msg)
		}
		r.Findings = append(r.Findings, packs.Finding{File: relPath(in.Root, f.Filename), Line: f.Location.Row, Level: packs.LevelError, Rule: f.Code, Message: msg})
	}
	return r
}

// relPath turns a tool's file name into a repo-relative slash path.
func relPath(root, name string) string {
	if filepath.IsAbs(name) {
		if r, err := filepath.Rel(root, name); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		if rr, err := filepath.EvalSymlinks(root); err == nil {
			if r, err := filepath.Rel(rr, name); err == nil && !strings.HasPrefix(r, "..") {
				return filepath.ToSlash(r)
			}
		}
	}
	return filepath.ToSlash(name)
}

// --- ml/lock-check and ml/lock-check-poetry -------------------------------------

// lockDirs returns the directories of changed pyproject.toml or lock files
// that hold the lock.
func lockDirs(in *packs.Input, lock string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in.ChangedPaths() {
		b := path.Base(p)
		if b != "pyproject.toml" && b != lock {
			continue
		}
		d := path.Dir(p)
		if !seen[d] && existsNew(in, joinDir(d, lock)) && existsNew(in, joinDir(d, "pyproject.toml")) {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

func lockCheck(tool, lock string, args []string) func(context.Context, *packs.Input) packs.Result {
	return func(ctx context.Context, in *packs.Input) packs.Result {
		dirs := lockDirs(in, lock)
		if len(dirs) == 0 {
			return packs.Result{Status: packs.Skip, Detail: "no " + lock + " next to a changed pyproject.toml"}
		}
		r := packs.Result{Status: packs.OK}
		var details []string
		for _, d := range dirs {
			out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "ml", Tool: tool, Args: args, Dir: filepath.Join(in.Root, filepath.FromSlash(d)), Timeout: toolTimeout})
			if err != nil || out.TimedOut {
				return toolFailure(tool, out, err, "")
			}
			r.Tool = toolRunOf(tool, out)
			if out.ExitCode != 0 {
				r.Findings = append(r.Findings, packs.Finding{File: joinDir(d, lock), Level: packs.LevelError, Rule: "lock-outdated",
					Message: fmt.Sprintf("%s %s failed (exit %d): %s does not match pyproject.toml; re-lock and commit it", tool, strings.Join(args, " "), out.ExitCode, lock)})
				details = append(details, out.Tail(20, nil))
			}
		}
		r.Detail = fmt.Sprintf("%s %s in %s", tool, strings.Join(args, " "), lintPlural(len(dirs), "project"))
		if len(details) > 0 {
			r.Detail += "\n" + strings.Join(details, "\n")
		}
		return r
	}
}

// --- ml/dvc-status --------------------------------------------------------------

func dvcApplies(in *packs.Input) bool {
	if in.AnyChanged("*.dvc", "dvc.yaml", "dvc.lock") {
		return true
	}
	fi, err := os.Stat(filepath.Join(in.Root, ".dvc"))
	return err == nil && fi.IsDir() && len(in.Changed) > 0
}

func runDVCStatus(ctx context.Context, in *packs.Input) packs.Result {
	out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "ml", Tool: "dvc", Args: []string{"status", "--json"}, Dir: in.Root, Timeout: toolTimeout})
	if err != nil || out.TimedOut || out.ExitCode != 0 {
		return toolFailure("dvc", out, err, fmt.Sprintf("dvc status exited %d", out.ExitCode))
	}
	raw := bytes.TrimSpace(out.Stdout)
	if i := bytes.IndexByte(raw, '{'); i > 0 {
		raw = raw[i:]
	}
	var st map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &st); err != nil {
		return toolFailure("dvc", out, nil, "cannot parse dvc status JSON output")
	}
	r := packs.Result{Status: packs.OK, Tool: toolRunOf("dvc", out)}
	stages := make([]string, 0, len(st))
	for k := range st {
		stages = append(stages, k)
	}
	sort.Strings(stages)
	for _, stage := range stages {
		file := "dvc.yaml"
		if strings.HasSuffix(stage, ".dvc") {
			file = stage
		} else if i := strings.LastIndex(stage, ":"); i > 0 && strings.HasSuffix(stage[:i], "dvc.yaml") {
			file = stage[:i]
		}
		for _, item := range st[stage] {
			var changes map[string]map[string]string
			if json.Unmarshal(item, &changes) != nil {
				continue // "always changed" and other plain strings
			}
			kinds := make([]string, 0, len(changes))
			for k := range changes {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			for _, kind := range kinds {
				level, rule := packs.LevelWarn, "dvc-changed-deps"
				if kind == "changed outs" {
					level, rule = packs.LevelError, "dvc-changed-outs"
				} else if kind != "changed deps" {
					continue
				}
				paths := make([]string, 0, len(changes[kind]))
				for p := range changes[kind] {
					paths = append(paths, p)
				}
				sort.Strings(paths)
				for _, p := range paths {
					r.Findings = append(r.Findings, packs.Finding{File: file, Level: level, Rule: rule,
						Message: fmt.Sprintf("stage %q: %s %q is %s", stage, strings.TrimPrefix(kind, "changed "), p, changes[kind][p])})
				}
			}
		}
	}
	if len(st) == 0 {
		r.Detail = "dvc: data and pipelines are up to date"
	} else {
		r.Detail = "dvc status: " + lintPlural(len(st), "stage") + " changed"
	}
	return r
}
