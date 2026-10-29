// Package tools runs the cloud pack's external IaC checks (spec D §7.3):
// checkov, trivy, conftest, tflint, bicep lint and cfn-lint, through the
// framework's tool runner (Input.Tools), and parses their output into
// findings. The framework baseline (Input.Baseline) downgrades known
// findings after the check returns. Tool messages pass redact before they
// enter a finding; a raw plan is never given to a tool, only the redacted
// copy the plan store keeps.
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/policy"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/redact"
)

const pack = "cloud"

const maxMessage = 300

// RunFailed is the rule suffix of the error finding a failed tool run adds,
// so the failure survives the framework's normalisation.
const RunFailed = "run-failed"

// Batch is the most files given to one tool run.
const Batch = 100

// MaxStagedBytes caps the bytes copied into a staged scratch tree.
const MaxStagedBytes = 256 << 20

func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = redact.Redact(s, nil)
	if len(s) > maxMessage {
		s = s[:maxMessage] + "…"
	}
	return s
}

// result accumulates the findings and the tool runs of one check.
type result struct {
	res  packs.Result
	tool *packs.ToolRun
	runs int
}

func newResult() *result {
	return &result{res: packs.Result{Status: packs.OK, Findings: []packs.Finding{}}}
}

// fail marks the result as a failed run: Fail, the detail, and an error
// finding that keeps it failed whatever warnings it holds.
func (r *result) fail(tool, detail string) {
	detail = clean(detail)
	r.res.Status = packs.Fail
	if r.res.Detail == "" {
		r.res.Detail = detail
	}
	r.res.Findings = append(r.res.Findings, packs.Finding{Level: packs.LevelError, Rule: tool + ":" + RunFailed, Message: detail})
}

func (r *result) add(fs ...packs.Finding) { r.res.Findings = append(r.res.Findings, fs...) }

func (r *result) record(tool string, out toolrun.Output) {
	if r.tool == nil {
		r.tool = &packs.ToolRun{Name: tool, Version: out.Version, Path: out.Path}
	}
	r.runs++
	r.tool.ExitCode = out.ExitCode
	r.tool.DurationMS += out.Duration.Milliseconds()
	r.tool.TimedOut = r.tool.TimedOut || out.TimedOut
}

func (r *result) done(what string) packs.Result {
	if r.tool != nil {
		r.res.Tool = r.tool
	}
	if r.res.Detail == "" {
		errs, warns := r.res.Counts()
		r.res.Detail = fmt.Sprintf("%s: %d error(s), %d warning(s)", what, errs, warns)
	}
	return r.res
}

// exec runs one invocation and reports whether its output can be parsed:
// the tool finished in time, its exit code is one of ok, and its stdout was
// not truncated. Otherwise the result is marked failed.
func (r *result) exec(ctx context.Context, in *packs.Input, tool string, args []string, dir string, env []string, ok func(int) bool, classes []string) (toolrun.Output, bool) {
	out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: pack, Tool: tool, Args: args, Dir: dir, Env: env})
	r.record(tool, out)
	switch {
	case err != nil && errors.Is(err, toolrun.ErrNotFound):
		r.fail(tool, tool+" was not found")
	case err != nil:
		r.fail(tool, tool+" did not run: "+err.Error())
	case out.TimedOut:
		r.fail(tool, tool+" timed out")
	case ctx.Err() != nil:
		r.fail(tool, tool+" did not finish: "+ctx.Err().Error())
	case out.StdoutTruncated:
		r.fail(tool, tool+" printed more than the output cap; narrow the IaC paths")
	case !ok(out.ExitCode):
		r.fail(tool, fmt.Sprintf("%s exited %d: %s", tool, out.ExitCode, out.Tail(20, classes)))
	default:
		return out, true
	}
	return out, false
}

func exitIn(codes ...int) func(int) bool {
	return func(c int) bool {
		for _, x := range codes {
			if x == c {
				return true
			}
		}
		return false
	}
}

// repoPath turns a tool's file name into a repo-relative slash path: an
// absolute path (or file:// URI) is made relative to root; a relative one
// is joined to base (the directory the tool scanned).
func repoPath(root, base, p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "file:") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
			if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
				p = p[1:]
			}
		}
	}
	native := filepath.FromSlash(p)
	if filepath.IsAbs(native) && root != "" {
		if rel, err := filepath.Rel(root, native); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		if rr, err := filepath.EvalSymlinks(root); err == nil {
			if rel, err := filepath.Rel(rr, native); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(p)
	}
	p = strings.TrimPrefix(filepath.ToSlash(p), "./")
	if strings.HasPrefix(p, "/") {
		p = strings.TrimPrefix(p, "/")
	}
	if base == "" || base == "." {
		return path.Clean(p)
	}
	return path.Join(base, p)
}

// tree returns the directory the tools run in: the repository, or with
// --staged a scratch copy of the staged (index) versions of the tracked
// files under dirs (so a staged run checks what is staged).
func tree(in *packs.Input, dirs []string) (string, func(), error) {
	if !in.Staged {
		return in.Root, func() {}, nil
	}
	if in.Git == nil {
		return "", func() {}, errors.New("no git reader")
	}
	files, err := in.Git.ListFiles()
	if err != nil {
		return "", func() {}, errors.New("cannot list the tracked files")
	}
	dir, cleanup, err := toolrun.Scratch(in.Workspace())
	if err != nil {
		return "", func() {}, err
	}
	var total int64
	n := 0
	for _, f := range files {
		f = policy.CleanPath(f)
		under := func() bool {
			for _, d := range dirs {
				if policy.Under(d, f) {
					return true
				}
			}
			return false
		}()
		if !under {
			continue
		}
		n++
		if n > policy.MaxSourceFiles {
			cleanup()
			return "", func() {}, fmt.Errorf("more than %d staged files under the IaC paths", policy.MaxSourceFiles)
		}
		b, err := readNew(in, f)
		if err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("cannot read the staged %s", f)
		}
		total += int64(len(b))
		if total > MaxStagedBytes {
			cleanup()
			return "", func() {}, errors.New("the staged IaC files are larger than the staging cap")
		}
		dst := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			cleanup()
			return "", func() {}, err
		}
		if err := os.WriteFile(dst, b, 0o600); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dir, cleanup, nil
}

// readNew is a seam for tests.
var readNew = func(in *packs.Input, p string) ([]byte, error) { return in.ReadNew(p, policy.MaxSourceBytes) }

func paths(es []policy.Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Path)
	}
	return out
}
