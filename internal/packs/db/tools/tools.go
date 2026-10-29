// Package tools runs the db pack's external migration linters (spec D §10):
// squawk (Postgres) and sqlfluff (Postgres and T-SQL), through the
// framework's tool runner (Input.Tools), and parses their JSON output into
// findings. Tool messages pass redact before they enter a finding.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// Batch is the most files passed to one tool run (keeps argv short).
const Batch = 100

const maxMessage = 300

// squawkViolation is one entry of `squawk --reporter json` (squawk 2.x: one
// flat array; line and column are 0-based).
type squawkViolation struct {
	File     string  `json:"file"`
	Line     int     `json:"line"`
	Level    string  `json:"level"`
	Message  string  `json:"message"`
	Help     *string `json:"help"`
	RuleName string  `json:"rule_name"`
}

// ParseSquawk parses squawk's JSON reporter output. Every squawk finding is
// an error (spec D §10: squawk findings fail); the framework baseline can
// downgrade known ones.
func ParseSquawk(stdout []byte, root string) ([]packs.Finding, error) {
	var vs []squawkViolation
	if err := json.Unmarshal(stdout, &vs); err != nil {
		return nil, fmt.Errorf("squawk output is not JSON: %w", err)
	}
	out := []packs.Finding{}
	for _, v := range vs {
		msg := v.Message
		if v.Help != nil && *v.Help != "" {
			msg += " " + *v.Help
		}
		out = append(out, packs.Finding{File: relPath(root, v.File), Line: v.Line + 1, Level: packs.LevelError,
			Rule: "squawk:" + v.RuleName, Message: clean(msg)})
	}
	return out, nil
}

type sqlfluffFile struct {
	Filepath   string `json:"filepath"`
	Violations []struct {
		StartLine *int   `json:"start_line_no"` // sqlfluff 2.x and 3.x
		Line      *int   `json:"line_no"`       // sqlfluff 1.x
		Code      string `json:"code"`
		Desc      string `json:"description"`
		Warning   bool   `json:"warning"`
	} `json:"violations"`
}

// ParseSqlfluff parses `sqlfluff lint --format json`. Findings are style
// warnings unless fail is set (packs.db.sqlfluff_fail); a violation sqlfluff
// itself marks as a warning stays a warning.
func ParseSqlfluff(stdout []byte, root string, fail bool) ([]packs.Finding, error) {
	var files []sqlfluffFile
	if err := json.Unmarshal(stdout, &files); err != nil {
		return nil, fmt.Errorf("sqlfluff output is not JSON: %w", err)
	}
	out := []packs.Finding{}
	for _, f := range files {
		for _, v := range f.Violations {
			line := 0
			if v.StartLine != nil {
				line = *v.StartLine
			} else if v.Line != nil {
				line = *v.Line
			}
			level := packs.LevelWarn
			if fail && !v.Warning {
				level = packs.LevelError
			}
			out = append(out, packs.Finding{File: relPath(root, f.Filepath), Line: line, Level: level, Rule: "sqlfluff:" + v.Code, Message: clean(v.Desc)})
		}
	}
	return out, nil
}

func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = redact.Redact(s, nil)
	if len(s) > maxMessage {
		s = s[:maxMessage] + "…"
	}
	return s
}

// relPath turns a tool's file name into a repo-relative slash path.
func relPath(root, p string) string {
	p = strings.TrimSpace(p)
	if filepath.IsAbs(p) && root != "" {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			p = r
		}
	}
	p = filepath.ToSlash(p)
	return strings.TrimPrefix(p, "./")
}

// run runs the tool over files in batches and collects the findings.
// okExit are the exit codes that mean "ran and reported".
func run(ctx context.Context, in *packs.Input, tool string, pre []string, files []string, okExit map[int]bool,
	parse func([]byte) ([]packs.Finding, error)) packs.Result {
	res := packs.Result{Status: packs.OK, Findings: []packs.Finding{}}
	for start := 0; start < len(files); start += Batch {
		end := start + Batch
		if end > len(files) {
			end = len(files)
		}
		args := append(append([]string{}, pre...), files[start:end]...)
		out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "db", Tool: tool, Args: args, Dir: in.Root})
		res.Tool = &packs.ToolRun{Name: tool, Version: out.Version, Path: out.Path, ExitCode: out.ExitCode, DurationMS: out.Duration.Milliseconds(), TimedOut: out.TimedOut}
		if err != nil {
			res.Status, res.Detail = packs.Fail, tool+" did not run: "+redact.Redact(err.Error(), nil)
			return res
		}
		if out.TimedOut {
			res.Status, res.Detail = packs.Fail, tool+" timed out"
			return res
		}
		if !okExit[out.ExitCode] {
			res.Status, res.Detail = packs.Fail, fmt.Sprintf("%s exited %d: %s", tool, out.ExitCode, out.Tail(20, nil))
			return res
		}
		fs, perr := parse(out.Stdout)
		if perr != nil {
			res.Status, res.Detail = packs.Fail, perr.Error()+": "+out.Tail(20, nil)
			return res
		}
		res.Findings = append(res.Findings, fs...)
	}
	errs, warns := res.Counts()
	res.Detail = fmt.Sprintf("%s: %d file(s), %d error(s), %d warning(s)", tool, len(files), errs, warns)
	if errs > 0 {
		res.Status = packs.Fail
	}
	return res
}

// Squawk runs `squawk --reporter json <files>` (Postgres migrations).
// Exit 0 is clean and 1 means violations; anything else fails the check.
func Squawk(ctx context.Context, in *packs.Input, files []string) packs.Result {
	return run(ctx, in, "squawk", []string{"--reporter", "json"}, files, map[int]bool{0: true, 1: true},
		func(b []byte) ([]packs.Finding, error) { return ParseSquawk(b, in.Root) })
}

// Sqlfluff runs `sqlfluff lint --format json --dialect <dialect> <files>`
// (dialect postgres or tsql). Exit 0 is clean and 1 means violations;
// anything else (2: a usage or config error) fails the check.
func Sqlfluff(ctx context.Context, in *packs.Input, dialect string, files []string, fail bool) packs.Result {
	return run(ctx, in, "sqlfluff", []string{"lint", "--format", "json", "--dialect", dialect}, files, map[int]bool{0: true, 1: true},
		func(b []byte) ([]packs.Finding, error) { return ParseSqlfluff(b, in.Root, fail) })
}
