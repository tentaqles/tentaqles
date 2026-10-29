package data

// The external datacontract-cli checks (spec C §3.2): data/datacontract-lint
// (`datacontract lint`) and, as a cross-check of data/contract-breaking,
// data/datacontract-breaking (`datacontract breaking`). Both pass
// --no-inline-references so the tool never resolves authoritativeDefinitions
// over the network, and both read copies of the contract sides in the run's
// scratch directory, so --staged and the old side work the same way.

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// contractToolTimeout is the default per-invocation timeout of datacontract
// (packs.data.timeouts.datacontract overrides it).
const contractToolTimeout = 60 * time.Second

const contractTool = "datacontract"

// contractScratchFile writes b to <scratch>/<i>/<side>/<base name> (the tool
// infers the format from the file name) and returns the path.
func contractScratchFile(dir string, i int, side, p string, b []byte) (string, error) {
	d := filepath.Join(dir, strconv.Itoa(i), side)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	f := filepath.Join(d, path.Base(p))
	return f, os.WriteFile(f, b, 0o600)
}

func contractToolRun(ctx context.Context, in *packs.Input, args ...string) (toolrun.Output, error) {
	return in.Tools.Run(ctx, toolrun.Spec{Pack: "data", Tool: contractTool, Args: args, Dir: in.Root, Timeout: contractToolTimeout})
}

func runDatacontractLint(ctx context.Context, in *packs.Input) packs.Result {
	targets := contractTargets(in)
	if len(targets) == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no contract file changed"}
	}
	scratch, cleanup, err := toolrun.Scratch(in.Workspace())
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: "cannot create the scratch directory"}
	}
	defer cleanup()
	r := packs.Result{Status: packs.OK}
	var last toolrun.Output
	var tail string
	for i, p := range targets {
		b, err := in.ReadNew(p, contractMaxBytes)
		if err != nil {
			r.Findings = append(r.Findings, packs.Finding{File: p, Level: packs.LevelError, Rule: "datacontract-lint", Message: "cannot read the contract"})
			continue
		}
		f, err := contractScratchFile(scratch, i, "new", p, b)
		if err != nil {
			return packs.Result{Status: packs.Fail, Detail: "cannot write the scratch copy"}
		}
		out, err := contractToolRun(ctx, in, "lint", "--no-inline-references", f)
		last = out
		if err != nil || out.TimedOut {
			return contractToolResult(packs.Result{Status: packs.Fail, Detail: "datacontract lint did not finish", Findings: r.Findings}, out)
		}
		if out.ExitCode != 0 {
			r.Findings = append(r.Findings, packs.Finding{File: p, Level: packs.LevelError, Rule: "datacontract-lint",
				Message: fmt.Sprintf("datacontract lint failed (exit %d)", out.ExitCode)})
			if tail == "" {
				tail = out.Tail(20, nil)
			}
		}
	}
	if errs, _ := r.Counts(); errs > 0 {
		r.Status = packs.Fail
		r.Detail = fmt.Sprintf("%s failed datacontract lint", contractPlural(errs, "contract"))
		if tail != "" {
			r.Detail += ":\n" + tail
		}
	} else {
		r.Detail = contractPlural(len(targets), "contract") + " passed datacontract lint"
	}
	return contractToolResult(r, last)
}

func runDatacontractBreaking(ctx context.Context, in *packs.Input) packs.Result {
	if in.Base == "" || in.All {
		return packs.Result{Status: packs.Skip, Detail: "no old side to compare (--all or no base)"}
	}
	scratch, cleanup, err := toolrun.Scratch(in.Workspace())
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: "cannot create the scratch directory"}
	}
	defer cleanup()
	r := packs.Result{Status: packs.OK}
	var last toolrun.Output
	var tail string
	n := 0
	for i, ch := range contractChanges(in) {
		if ch.Status != 'M' && ch.Status != 'R' {
			continue // added: nothing to compare; deleted: data/contract-breaking
		}
		oldPath := ch.Path
		if ch.OldPath != "" {
			oldPath = ch.OldPath
		}
		ob, err := in.Git.Show(in.Base, oldPath)
		if err != nil {
			continue
		}
		nb, err := in.ReadNew(ch.Path, contractMaxBytes)
		if err != nil {
			continue
		}
		of, err1 := contractScratchFile(scratch, i, "old", oldPath, ob)
		nf, err2 := contractScratchFile(scratch, i, "new", ch.Path, nb)
		if err1 != nil || err2 != nil {
			return packs.Result{Status: packs.Fail, Detail: "cannot write the scratch copy"}
		}
		n++
		out, err := contractToolRun(ctx, in, "breaking", "--no-inline-references", of, nf)
		last = out
		if err != nil || out.TimedOut {
			return contractToolResult(packs.Result{Status: packs.Fail, Detail: "datacontract breaking did not finish", Findings: r.Findings}, out)
		}
		if out.ExitCode != 0 {
			f := packs.Finding{File: ch.Path, Level: packs.LevelError, Rule: "datacontract-breaking",
				Message: "datacontract breaking reports a breaking change without a major version bump; compare with data/contract-breaking"}
			if contractMajorBumped(ob, nb) {
				f.Level = packs.LevelWarn
				f.Message = "datacontract breaking reports a breaking change (the major version was bumped: mark the PR as breaking)"
			}
			r.Findings = append(r.Findings, f)
			if tail == "" {
				tail = out.Tail(20, nil)
			}
		}
	}
	if n == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no modified contract to compare"}
	}
	errs, warns := r.Counts()
	if errs > 0 {
		r.Status = packs.Fail
		r.Detail = fmt.Sprintf("datacontract breaking: %s with a breaking change and no major bump", contractPlural(errs, "contract"))
		if tail != "" {
			r.Detail += ":\n" + tail
		}
	} else if warns > 0 {
		r.Detail = fmt.Sprintf("datacontract breaking: %s breaking with a major bump", contractPlural(warns, "contract"))
	} else {
		r.Detail = fmt.Sprintf("datacontract breaking: %s, no breaking change", contractPlural(n, "contract"))
	}
	return contractToolResult(r, last)
}

// contractMajorBumped reports whether the new side's major version is above
// the old side's.
func contractMajorBumped(oldB, newB []byte) bool {
	o, err1 := parseContract("old", oldB)
	n, err2 := parseContract("new", newB)
	if err1 != nil || err2 != nil {
		return false
	}
	om, _, _, ok1 := odcsSemver(o.Version)
	nm, _, _, ok2 := odcsSemver(n.Version)
	return ok1 && ok2 && nm > om
}

func contractToolResult(r packs.Result, out toolrun.Output) packs.Result {
	r.Tool = &packs.ToolRun{Name: contractTool, Version: out.Version, Path: out.Path, ExitCode: out.ExitCode,
		DurationMS: out.Duration.Milliseconds(), TimedOut: out.TimedOut}
	return r
}
