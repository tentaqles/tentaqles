// Package readers turns IaC inputs into the cloud pack's normalised
// []model.Resource (spec D §6). Each reader is a pure function over bytes:
// no file system, no process, no network. The caller reads the file (with
// Input.ReadNew, GuardCtx.ReadState or the plan store) and passes its name
// for positions.
//
// Every value a reader returns is redacted (model.Mask, model.Known,
// model.Scrub). Redact produces the redacted copy of a whole document for
// the plan store, so that the rules and the agent never read a raw plan.
package readers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// MaxInput is the largest input a reader accepts (the framework's
// ReadNew and HashState cap). A variable so tests can lower it.
var MaxInput = 64 << 20

var (
	// ErrUnknownFormat: the bytes are not the format the reader reads (or,
	// for ReadAuto and Detect, no known format).
	ErrUnknownFormat = errors.New("unknown IaC input format")
	// ErrTooLarge: the input is over MaxInput.
	ErrTooLarge = errors.New("IaC input too large")
	// ErrWhatIfFailed: a what-if result that carries an error and no changes.
	ErrWhatIfFailed = errors.New("what-if operation failed")
)

// Options carries what the caller knows and the bytes do not.
type Options struct {
	// Env is copied into every Resource.Env ("" when reading source).
	Env string
}

// Reader is the signature every reader has.
type Reader func(name string, raw []byte, o Options) ([]model.Resource, error)

// For returns the reader of a kind, or nil.
func For(k model.Kind) Reader {
	switch k {
	case model.KindTFPlan:
		return TFPlan
	case model.KindARM:
		return ARM
	case model.KindWhatIf:
		return WhatIf
	case model.KindCFN:
		return CFN
	case model.KindChangeSet:
		return ChangeSet
	case model.KindPulumi:
		return Pulumi
	}
	return nil
}

// Read runs the reader of kind k.
func Read(k model.Kind, name string, raw []byte, o Options) ([]model.Resource, error) {
	r := For(k)
	if r == nil {
		return nil, fmt.Errorf("%w: kind %q", ErrUnknownFormat, k)
	}
	return r(name, raw, o)
}

// ReadAuto detects the kind and reads it.
func ReadAuto(name string, raw []byte, o Options) (model.Kind, []model.Resource, error) {
	if len(raw) > MaxInput {
		return "", nil, ErrTooLarge
	}
	k := Detect(name, raw)
	if k == "" {
		return "", nil, fmt.Errorf("%s: %w", name, ErrUnknownFormat)
	}
	rs, err := Read(k, name, raw, o)
	return k, rs, err
}

// Detect sniffs the format of raw: "" when it is none of the known kinds.
// name is used only to allow YAML (a .yaml, .yml or .template name, or YAML
// content that is clearly a CloudFormation template).
func Detect(name string, raw []byte) model.Kind {
	raw = trimBOM(raw)
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return ""
	}
	if t[0] != '{' {
		if t[0] == '[' {
			return ""
		}
		if looksCFNYAML(raw) {
			return model.KindCFN
		}
		return ""
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(t, &top); err != nil {
		// A truncated or broken JSON document: guess from its prefix so the
		// reader reports the parse error.
		return detectPrefix(t)
	}
	return detectTop(top)
}

func detectTop(top map[string]json.RawMessage) model.Kind {
	has := func(k string) bool { _, ok := top[k]; return ok }
	switch {
	case has("format_version") && (has("resource_changes") || has("planned_values") || has("terraform_version")):
		return model.KindTFPlan
	case has("Changes") && (has("ChangeSetName") || has("ChangeSetId") || has("StackName")):
		return model.KindChangeSet
	case has("steps") && (has("changeSummary") || has("config") || has("duration")):
		return model.KindPulumi
	case has("changeSummary"):
		return model.KindPulumi
	case has("changes") && (has("status") || has("error") || has("potentialChanges")):
		return model.KindWhatIf
	case has("properties") && has("status") && whatIfProps(top["properties"]):
		return model.KindWhatIf
	case armSchema(top["$schema"]) || (has("contentVersion") && has("resources")):
		return model.KindARM
	case has("AWSTemplateFormatVersion") || cfnTypedResources(top["Resources"]):
		return model.KindCFN
	}
	return ""
}

func whatIfProps(raw json.RawMessage) bool {
	var p map[string]json.RawMessage
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	_, ok := p["changes"]
	return ok
}

func armSchema(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	return strings.Contains(strings.ToLower(s), "deploymenttemplate.json")
}

func cfnTypedResources(raw json.RawMessage) bool {
	var res map[string]struct {
		Type string `json:"Type"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return false
	}
	for _, r := range res {
		if strings.Contains(r.Type, "::") {
			return true
		}
	}
	return false
}

func detectPrefix(t []byte) model.Kind {
	head := string(t)
	if len(head) > 4096 {
		head = head[:4096]
	}
	switch {
	case strings.Contains(head, `"format_version"`) && (strings.Contains(head, `"resource_changes"`) || strings.Contains(head, `"terraform_version"`) || strings.Contains(head, `"planned_values"`)):
		return model.KindTFPlan
	case strings.Contains(head, `"ChangeSetName"`) || strings.Contains(head, `"ResourceChange"`):
		return model.KindChangeSet
	case strings.Contains(head, `"steps"`) && strings.Contains(head, `urn:pulumi:`):
		return model.KindPulumi
	case strings.Contains(head, `"changeType"`):
		return model.KindWhatIf
	case strings.Contains(strings.ToLower(head), "deploymenttemplate.json"):
		return model.KindARM
	case strings.Contains(head, `"AWSTemplateFormatVersion"`):
		return model.KindCFN
	}
	return ""
}

// looksCFNYAML: a YAML document with a top-level AWSTemplateFormatVersion,
// or a top-level Resources key and at least one AWS:: type.
func looksCFNYAML(raw []byte) bool {
	topRes, awsType := false, false
	for _, line := range strings.Split(string(raw), "\n") {
		l := strings.TrimRight(line, "\r")
		if strings.HasPrefix(l, "AWSTemplateFormatVersion:") {
			return true
		}
		if strings.HasPrefix(l, "Resources:") {
			topRes = true
		}
		if strings.Contains(l, "Type:") && strings.Contains(l, "AWS::") {
			awsType = true
		}
	}
	return topRes && awsType
}

func trimBOM(b []byte) []byte { return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) }

func checkSize(raw []byte) error {
	if len(raw) > MaxInput {
		return fmt.Errorf("%w: %d bytes, cap %d", ErrTooLarge, len(raw), MaxInput)
	}
	return nil
}

// meta builds a Meta map, dropping empty values; nil when nothing is left.
func meta(kv ...string) map[string]string {
	var m map[string]string
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "" {
			continue
		}
		if m == nil {
			m = map[string]string{}
		}
		m[kv[i]] = kv[i+1]
	}
	return m
}

func setMeta(r *model.Resource, k, v string) {
	if v == "" {
		return
	}
	if r.Meta == nil {
		r.Meta = map[string]string{}
	}
	r.Meta[k] = v
}

// str is model.Str without the ok.
func str(v any) string {
	s, _ := model.Str(v)
	return s
}
