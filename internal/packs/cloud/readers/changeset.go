package readers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// ChangeSet reads `aws cloudformation describe-change-set -o json`:
// Changes[].ResourceChange.{Action, LogicalResourceId, PhysicalResourceId,
// ResourceType, Replacement, PolicyAction, BeforeContext, AfterContext}.
//
// Action maps Add→create, Remove→delete, Import→import,
// SyncWithActual→no-op, Dynamic→unknown, and Modify→update, or →replace
// when Replacement is True or Conditional (fail closed: a conditional
// replacement may destroy). BeforeContext and AfterContext are JSON strings,
// present only with --include-property-values; After and Before are their
// Properties (or the whole decoded object when it has none). Non-Resource
// changes (hook entries) are skipped. describe-change-set is paginated: a
// result with a NextToken is one page only and is refused with
// ErrIncomplete (a later page could hold a Remove).
func ChangeSet(name string, raw []byte, o Options) ([]model.Resource, error) {
	if err := checkSize(raw); err != nil {
		return nil, err
	}
	d, err := decodeJSON(raw, 2)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	top := asMap(d.root)
	if top == nil || top["Changes"] == nil || (top["ChangeSetName"] == nil && top["ChangeSetId"] == nil && top["StackName"] == nil) {
		return nil, fmt.Errorf("%s: %w: not a describe-change-set result", name, ErrUnknownFormat)
	}
	if nt, ok := top["NextToken"]; ok && nt != nil && str(nt) != "" {
		// The token itself is not repeated.
		return nil, fmt.Errorf("%s: %w: change set has more pages (NextToken); run describe-change-set without --max-items or --no-paginate", name, ErrIncomplete)
	}
	var out []model.Resource
	for i, item := range asList(top["Changes"]) {
		c := asMap(item)
		if t := str(c["Type"]); t != "" && t != "Resource" {
			continue
		}
		rc := asMap(c["ResourceChange"])
		if rc == nil {
			continue
		}
		ptr := "/Changes/" + strconv.Itoa(i)
		act, repl := str(rc["Action"]), str(rc["Replacement"])
		r := model.Resource{
			Addr:    str(rc["LogicalResourceId"]),
			Type:    str(rc["ResourceType"]),
			Actions: []string{changeSetAction(act, repl)},
			After:   contextProps(rc["AfterContext"]),
			Before:  contextProps(rc["BeforeContext"]),
			Source:  model.KindChangeSet,
			Env:     o.Env,
			Pos:     model.Pos{File: name, Line: d.line(ptr), Pointer: ptr},
			Meta: meta("raw_action", act, "replacement", repl, "policy_action", str(rc["PolicyAction"]),
				"physical_id", model.RedactString(str(rc["PhysicalResourceId"]))),
		}
		out = append(out, r)
	}
	return out, nil
}

func changeSetAction(action, replacement string) string {
	switch strings.ToLower(action) {
	case "add":
		return model.ActCreate
	case "remove":
		return model.ActDelete
	case "import":
		return model.ActImport
	case "syncwithactual":
		return model.ActNoOp
	case "modify":
		switch strings.ToLower(replacement) {
		case "true", "conditional":
			return model.ActReplace
		}
		return model.ActUpdate
	}
	return model.ActUnknown
}

// decodeContext decodes a Before/AfterContext string (numbers as
// json.Number); nil when it is absent or not a JSON object.
func decodeContext(v any) map[string]any {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	d, err := decodeJSON([]byte(s), -1)
	if err != nil {
		return nil
	}
	return asMap(d.root)
}

func contextProps(v any) map[string]any {
	m := decodeContext(v)
	if m == nil {
		return nil
	}
	if p := asMap(m["Properties"]); p != nil {
		m = p
	}
	return model.ScrubMap(m)
}

// redactChangeSet scrubs the document and the JSON inside each context
// string, re-encoding the contexts as strings.
func redactChangeSet(top map[string]any) (any, error) {
	for _, item := range asList(top["Changes"]) {
		rc := asMap(asMap(item)["ResourceChange"])
		for _, k := range []string{"BeforeContext", "AfterContext", "PreviousDeploymentContext"} {
			if _, ok := rc[k].(string); !ok {
				continue
			}
			m := decodeContext(rc[k])
			if m == nil {
				rc[k] = model.RedactString(rc[k].(string))
				continue
			}
			b, err := json.Marshal(model.Scrub(m))
			if err != nil {
				return nil, err
			}
			rc[k] = string(b)
		}
	}
	return model.Scrub(top), nil
}
