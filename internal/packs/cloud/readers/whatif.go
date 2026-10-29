package readers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// WhatIf reads `az deployment {group|sub|mg|tenant} what-if
// --result-format FullResourcePayloads --no-pretty-print -o json`: the CLI
// prints changes[] at the top level; the REST API (and some SDKs) nest it
// as properties.changes. potentialChanges are read too, with Meta potential.
//
// changeType maps Create→create, Delete→delete, Modify→update,
// Deploy→update (ResourceIdOnly: may change), NoChange/Ignore/NoEffect→no-op,
// Unsupported→unknown (with Meta unsupported_reason). Addr is resourceId;
// Type is after.type, else before.type, else derived from the resource id.
func WhatIf(name string, raw []byte, o Options) ([]model.Resource, error) {
	if err := checkSize(raw); err != nil {
		return nil, err
	}
	d, err := decodeJSON(raw, 3)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	top := asMap(d.root)
	base := ""
	if top != nil && top["changes"] == nil && asMap(top["properties"])["changes"] != nil {
		base = "/properties"
		top = asMap(top["properties"])
	}
	if top == nil {
		return nil, fmt.Errorf("%s: %w: not a what-if result", name, ErrUnknownFormat)
	}
	if _, present := top["changes"]; !present {
		return nil, fmt.Errorf("%s: %w: not a what-if result", name, ErrUnknownFormat)
	}
	if top["changes"] == nil && asMap(d.root)["error"] != nil {
		// A failed what-if: no changes to lint, and nothing to approve. The
		// error text is not repeated (it can quote template values).
		return nil, fmt.Errorf("%s: %w (status %q)", name, ErrWhatIfFailed, str(asMap(d.root)["status"]))
	}
	var out []model.Resource
	for _, key := range []string{"changes", "potentialChanges"} {
		for i, item := range asList(top[key]) {
			c := asMap(item)
			if c == nil {
				continue
			}
			ptr := base + "/" + key + "/" + strconv.Itoa(i)
			ct := str(c["changeType"])
			after, before := asMap(c["after"]), asMap(c["before"])
			id := str(c["resourceId"])
			typ := str(after["type"])
			if typ == "" {
				typ = str(before["type"])
			}
			if typ == "" {
				typ = armTypeFromID(id)
			}
			addr := id
			if addr == "" {
				addr = str(c["symbolicName"])
			}
			r := model.Resource{
				Addr:    addr,
				Type:    typ,
				Actions: []string{whatIfAction(ct)},
				After:   model.ScrubMap(after),
				Before:  model.ScrubMap(before),
				Source:  model.KindWhatIf,
				Env:     o.Env,
				Pos:     model.Pos{File: name, Line: d.line(ptr), Pointer: ptr},
				Meta:    meta("raw_action", ct, "unsupported_reason", model.RedactString(str(c["unsupportedReason"]))),
			}
			if key == "potentialChanges" {
				setMeta(&r, "potential", "true")
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func whatIfAction(ct string) string {
	switch strings.ToLower(ct) {
	case "create":
		return model.ActCreate
	case "delete":
		return model.ActDelete
	case "modify", "deploy":
		return model.ActUpdate
	case "nochange", "ignore", "noeffect":
		return model.ActNoOp
	}
	return model.ActUnknown
}

// armTypeFromID derives the resource type from an ARM resource id: the
// namespace after the last "/providers/" followed by every other segment
// (…/providers/Microsoft.Sql/servers/s/databases/d →
// Microsoft.Sql/servers/databases). A bare subscription or resource group
// id gives Microsoft.Resources/subscriptions or /resourceGroups.
func armTypeFromID(id string) string {
	segs := strings.Split(strings.Trim(id, "/"), "/")
	last := -1
	for i, s := range segs {
		if strings.EqualFold(s, "providers") && i+1 < len(segs) {
			last = i
		}
	}
	if last < 0 {
		switch {
		case len(segs) == 2 && strings.EqualFold(segs[0], "subscriptions"):
			return "Microsoft.Resources/subscriptions"
		case len(segs) == 4 && strings.EqualFold(segs[0], "subscriptions") && strings.EqualFold(segs[2], "resourceGroups"):
			return "Microsoft.Resources/resourceGroups"
		}
		return ""
	}
	rest := segs[last+1:]
	if len(rest) < 2 {
		return ""
	}
	parts := []string{rest[0]}
	for i := 1; i < len(rest); i += 2 {
		parts = append(parts, rest[i])
	}
	return strings.Join(parts, "/")
}
