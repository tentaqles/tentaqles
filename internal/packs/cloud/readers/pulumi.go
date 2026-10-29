package readers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// Pulumi reads `pulumi preview --json` (display.PreviewDigest):
// steps[].{op, urn, provider, newState.{type, inputs, protect},
// oldState.{type, inputs}}. A replacement shows as several steps for one URN
// (create-replacement, replace, delete-replaced); they merge into one
// Resource, positioned at the first step, with every op in Meta raw_action.
//
// op maps same/diff/discard/discard-replaced/remove-pending-replace/
// extend-parameterize→no-op, create→create, update→update, delete→delete,
// replace/create-replacement/delete-replaced→replace, read/read-replacement/
// refresh→read, import/import-replacement→import. After is newState.inputs,
// Before oldState.inputs; Pulumi secrets ("[secret]", or a secret object
// with --show-secrets) never keep a value.
func Pulumi(name string, raw []byte, o Options) ([]model.Resource, error) {
	if err := checkSize(raw); err != nil {
		return nil, err
	}
	d, err := decodeJSON(raw, 2)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	top := asMap(d.root)
	if top == nil || (top["steps"] == nil && top["changeSummary"] == nil) {
		return nil, fmt.Errorf("%s: %w: not a pulumi preview --json result", name, ErrUnknownFormat)
	}
	var out []model.Resource
	index := map[string]int{}
	ops := map[string][]string{}
	for i, item := range asList(top["steps"]) {
		s := asMap(item)
		urn := str(s["urn"])
		if s == nil || urn == "" {
			continue
		}
		op := str(s["op"])
		ns, ost := asMap(s["newState"]), asMap(s["oldState"])
		after, before := model.ScrubMap(asMap(ns["inputs"])), model.ScrubMap(asMap(ost["inputs"]))
		typ := str(ns["type"])
		if typ == "" {
			typ = str(ost["type"])
		}
		if typ == "" {
			typ = pulumiTypeFromURN(urn)
		}
		at, seen := index[urn]
		if !seen {
			ptr := "/steps/" + strconv.Itoa(i)
			out = append(out, model.Resource{
				Addr:   urn,
				Type:   typ,
				Source: model.KindPulumi,
				Env:    o.Env,
				Pos:    model.Pos{File: name, Line: d.line(ptr), Pointer: ptr},
			})
			at = len(out) - 1
			index[urn] = at
		}
		r := &out[at]
		ops[urn] = append(ops[urn], op)
		// The replace step carries the final inputs; otherwise the first
		// step with a payload wins.
		if after != nil && (r.After == nil || op == "replace") {
			r.After = after
		}
		if before != nil && r.Before == nil {
			r.Before = before
		}
		setMeta(r, "provider", str(s["provider"]))
		if b, _ := model.Bool(ns["protect"]); b {
			setMeta(r, "protect", "true")
		}
	}
	for i := range out {
		r := &out[i]
		raw := ops[r.Addr]
		r.Actions = pulumiActions(raw)
		setMeta(r, "raw_action", strings.Join(raw, ","))
		if r.Destroys() && !r.Has(model.ActCreate) && !r.Has(model.ActReplace) {
			r.After = nil // a pure delete plans nothing
		}
	}
	return out, nil
}

func pulumiOp(op string) string {
	switch op {
	case "create":
		return model.ActCreate
	case "update":
		return model.ActUpdate
	case "delete":
		return model.ActDelete
	case "replace", "create-replacement", "delete-replaced":
		return model.ActReplace
	case "read", "read-replacement", "refresh":
		return model.ActRead
	case "import", "import-replacement":
		return model.ActImport
	case "same", "diff", "discard", "discard-replaced", "remove-pending-replace", "extend-parameterize":
		return model.ActNoOp
	}
	return model.ActUnknown
}

// pulumiActions merges the ops of one URN into distinct actions, in first-
// seen order; a replace absorbs the create and delete of its own steps.
func pulumiActions(ops []string) []string {
	var acts []string
	seen := map[string]bool{}
	for _, op := range ops {
		a := pulumiOp(op)
		if !seen[a] {
			seen[a] = true
			acts = append(acts, a)
		}
	}
	if seen[model.ActReplace] {
		kept := []string{model.ActReplace}
		for _, a := range acts {
			if a != model.ActReplace && a != model.ActCreate && a != model.ActDelete && a != model.ActNoOp {
				kept = append(kept, a)
			}
		}
		return kept
	}
	if len(acts) == 0 {
		return []string{model.ActUnknown}
	}
	return acts
}

// pulumiTypeFromURN extracts the type token from a URN
// (urn:pulumi:<stack>::<project>::<parent$...$type>::<name>): the last
// "$"-separated part of the qualified type. The name may itself hold "::".
func pulumiTypeFromURN(urn string) string {
	if !strings.HasPrefix(urn, "urn:pulumi:") {
		return ""
	}
	parts := strings.SplitN(urn, "::", 4)
	if len(parts) < 4 {
		return ""
	}
	q := parts[2]
	if i := strings.LastIndexByte(q, '$'); i >= 0 {
		q = q[i+1:]
	}
	return q
}
