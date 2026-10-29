package iam

import (
	"bytes"
	"encoding/json"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/readers"
)

// File lints one input file: an IaC document the readers know (plan JSON,
// ARM, what-if, CloudFormation or CDK template, change set, Pulumi preview),
// or a standalone IAM policy JSON (an object with a Statement). handled is
// false when the file is neither. An error is a reader error on a file
// that looked like a known format; the caller reports it, never its text.
func File(name string, raw []byte, o Options) (res Result, handled bool, err error) {
	if kind := readers.Detect(name, raw); kind != "" {
		rs, err := readers.Read(kind, name, raw, readers.Options{})
		if err != nil {
			return Result{}, true, err
		}
		if kind == model.KindARM {
			o.Scope = TemplateScope(schemaOf(raw))
		}
		return Resources(rs, o), true, nil
	}
	return Standalone(name, raw, o)
}

// Standalone lints a policy JSON file (spec D §8: "standalone *.json with a
// Statement array under IaC paths"). Each statement's finding is placed at
// the statement's line. The file has no tag, so its findings cannot be
// suppressed in the source: move the policy into the IaC resource that
// uses it to suppress one.
func Standalone(name string, raw []byte, o Options) (Result, bool, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	iss, ok := lintDoc(string(raw), docAuto, o)
	if !ok {
		return Result{}, false, nil
	}
	lines := statementLines(raw)
	res := Result{Documents: 1}
	for _, i := range iss {
		line := 1
		if i.Stmt < len(lines) {
			line = lines[i.Stmt]
		}
		res.Findings = append(res.Findings, packs.Finding{File: name, Line: line, Level: i.Level, Rule: i.Rule, Message: i.Msg})
	}
	sortFindings(res.Findings)
	return res, true, nil
}

func schemaOf(raw []byte) string {
	var v struct {
		Schema string `json:"$schema"`
	}
	_ = json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &v)
	return v.Schema
}
