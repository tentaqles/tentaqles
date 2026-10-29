package data

// data/contract-valid (spec C §3.2): a contract parses, carries the required
// ODCS v3 fields, and its id is unique across the repo's contract files.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/tentaqles/tentaqles/internal/packs"
)

var contractYAMLLineRe = regexp.MustCompile(`line (\d+)`)

// contractTargets is what a contract check looks at: every contract file
// with --all, otherwise the changed (not deleted) ones.
func contractTargets(in *packs.Input) []string {
	if in.All {
		return allContractFiles(in)
	}
	var out []string
	for _, c := range contractChanges(in) {
		if c.Status != 'D' && isContractPath(in.Settings(), c.Path) {
			out = append(out, c.Path)
		}
	}
	return out
}

func runContractValid(ctx context.Context, in *packs.Input) packs.Result {
	targets := contractTargets(in)
	if len(targets) == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no contract file changed"}
	}
	isTarget := map[string]bool{}
	for _, t := range targets {
		isTarget[t] = true
	}
	// ids of every contract, so a changed one can't reuse an unchanged one's.
	idFiles := map[string][]string{}
	parsed := map[string]*odcsContract{}
	var fs []packs.Finding
	for _, p := range allContractFiles(in) {
		if ctx.Err() != nil {
			return packs.Result{Status: packs.Fail, Detail: "timed out"}
		}
		c, err := readContract(in, p)
		if err != nil {
			if isTarget[p] {
				fs = append(fs, contractParseFinding(p, err))
			}
			continue
		}
		parsed[p] = c
		if c.ID != "" {
			idFiles[c.ID] = append(idFiles[c.ID], p)
		}
	}
	for _, p := range targets {
		c := parsed[p]
		if c == nil {
			continue
		}
		fs = append(fs, contractValidFindings(c)...)
		if c.ID != "" {
			for _, other := range idFiles[c.ID] {
				if other != p {
					fs = append(fs, packs.Finding{File: p, Line: c.IDLine, Level: packs.LevelError, Rule: "duplicate-contract-id",
						Message: fmt.Sprintf("contract id %s is also used by %s", c.ID, other)})
					break
				}
			}
		}
	}
	r := packs.Result{Status: packs.OK, Findings: fs}
	if errs, _ := r.Counts(); errs > 0 {
		r.Status = packs.Fail
		r.Detail = fmt.Sprintf("%s, %s", contractPlural(len(targets), "contract"), contractPlural(errs, "problem"))
	} else {
		r.Detail = fmt.Sprintf("%s valid (ODCS %s field names)", contractPlural(len(targets), "contract"), odcsCheckedVersion)
	}
	return r
}

// contractParseFinding reports a file that is not YAML, with the line only
// (a YAML error can quote the file's text).
func contractParseFinding(p string, err error) packs.Finding {
	f := packs.Finding{File: p, Level: packs.LevelError, Rule: "contract-parse", Message: "the file is not valid YAML"}
	if m := contractYAMLLineRe.FindStringSubmatch(err.Error()); m != nil {
		f.Line, _ = strconv.Atoi(m[1])
	}
	return f
}

// contractValidFindings checks the required ODCS fields of one contract.
func contractValidFindings(c *odcsContract) []packs.Finding {
	var fs []packs.Finding
	add := func(line int, lvl, rule, msg string) {
		fs = append(fs, packs.Finding{File: c.Path, Line: line, Level: lvl, Rule: rule, Message: msg})
	}
	switch {
	case c.APIVersion == "":
		add(1, packs.LevelError, "field-missing", "apiVersion is required (v3.x.x)")
	case !odcsIsV3(c.APIVersion):
		add(c.APILine, packs.LevelError, "not-odcs-v3", "apiVersion "+c.APIVersion+" is not ODCS v3; tq checks v3.x.x contracts only")
	}
	if c.Kind != "DataContract" {
		line := c.KindLine
		if line == 0 {
			line = 1
		}
		add(line, packs.LevelError, "field-missing", "kind must be DataContract")
	}
	if c.ID == "" {
		add(odcsLineOr(c.IDLine), packs.LevelError, "field-missing", "id is required and must not be empty")
	}
	if c.Version == "" {
		add(odcsLineOr(c.VersionLine), packs.LevelError, "field-missing", "version is required and must not be empty")
	} else if _, _, _, ok := odcsSemver(c.Version); !ok {
		add(c.VersionLine, packs.LevelWarn, "version-not-semver", "version is not MAJOR.MINOR.PATCH; the version rule of data/contract-breaking needs semver")
	}
	for _, p := range c.Problems {
		add(p.Line, packs.LevelError, p.Rule, p.Message)
	}
	return fs
}

func odcsLineOr(l int) int {
	if l == 0 {
		return 1
	}
	return l
}
