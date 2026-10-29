package readers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// TFPlan reads `terraform show -json <plan>` (OpenTofu writes the same
// format): resource_changes[].{address, type, mode, provider_name,
// module_address, deposed, action_reason, change.{actions, before, after,
// before_sensitive, after_sensitive, importing}}.
//
// Actions map one to one, except that ["delete","create"] and
// ["create","delete"] become ["replace"], and a change with "importing" set
// gains "import". Before and After are masked with the sensitivity masks,
// any copy of a masked value elsewhere in the plan is redacted, and then
// model.Scrub runs. A value that is sensitive as a whole leaves After (or
// Before) nil and sets Meta after_redacted (before_redacted).
func TFPlan(name string, raw []byte, o Options) ([]model.Resource, error) {
	if err := checkSize(raw); err != nil {
		return nil, err
	}
	d, err := decodeJSON(raw, 2)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	top := asMap(d.root)
	if top == nil || top["format_version"] == nil || (top["resource_changes"] == nil && top["planned_values"] == nil && top["terraform_version"] == nil) {
		return nil, fmt.Errorf("%s: %w: not a terraform plan (show -json)", name, ErrUnknownFormat)
	}
	known := &model.Known{}
	collectTFSensitive(top, known)
	var out []model.Resource
	for i, item := range asList(top["resource_changes"]) {
		rc := asMap(item)
		if rc == nil {
			continue
		}
		ch := asMap(rc["change"])
		ptr := "/resource_changes/" + strconv.Itoa(i)
		r := model.Resource{
			Addr:   str(rc["address"]),
			Type:   str(rc["type"]),
			Source: model.KindTFPlan,
			Env:    o.Env,
			Pos:    model.Pos{File: name, Line: d.line(ptr), Pointer: ptr},
		}
		rawActs := model.Strings(ch["actions"])
		r.Actions = tfActions(rawActs, ch["importing"] != nil)
		r.After = tfValue(&r, "after", ch["after"], ch["after_sensitive"], known)
		r.Before = tfValue(&r, "before", ch["before"], ch["before_sensitive"], known)
		setMeta(&r, "raw_action", strings.Join(rawActs, ","))
		setMeta(&r, "mode", str(rc["mode"]))
		setMeta(&r, "provider", str(rc["provider_name"]))
		setMeta(&r, "module", str(rc["module_address"]))
		setMeta(&r, "deposed", str(rc["deposed"]))
		setMeta(&r, "action_reason", str(rc["action_reason"]))
		out = append(out, r)
	}
	return out, nil
}

func tfActions(raw []string, importing bool) []string {
	acts := []string{}
	hasC, hasD := false, false
	for _, a := range raw {
		switch a {
		case "create":
			hasC = true
		case "delete":
			hasD = true
		}
	}
	if hasC && hasD {
		acts = append(acts, model.ActReplace)
		for _, a := range raw {
			if a != "create" && a != "delete" {
				acts = append(acts, tfAction(a))
			}
		}
	} else {
		for _, a := range raw {
			acts = append(acts, tfAction(a))
		}
	}
	if importing {
		acts = append(acts, model.ActImport)
	}
	if len(acts) == 0 {
		acts = append(acts, model.ActUnknown)
	}
	return acts
}

func tfAction(a string) string {
	switch a {
	case "create":
		return model.ActCreate
	case "update":
		return model.ActUpdate
	case "delete":
		return model.ActDelete
	case "no-op":
		return model.ActNoOp
	case "read":
		return model.ActRead
	case "forget":
		return model.ActForget
	}
	return model.ActUnknown
}

func tfValue(r *model.Resource, side string, v, mask any, known *model.Known) map[string]any {
	if v == nil {
		return nil
	}
	m := model.Mask(v, mask, known)
	mm, ok := m.(map[string]any)
	if !ok {
		setMeta(r, side+"_redacted", "true")
		return nil
	}
	mm, _ = known.Replace(mm).(map[string]any)
	return model.ScrubMap(mm)
}

// collectTFSensitive adds every value the plan marks sensitive anywhere to
// known: change masks of resource_changes and resource_drift, sensitive
// outputs, sensitive_values of planned_values and prior_state, and the
// values and defaults of variables declared sensitive.
func collectTFSensitive(top map[string]any, known *model.Known) {
	for _, list := range []string{"resource_changes", "resource_drift"} {
		for _, item := range asList(top[list]) {
			ch := asMap(asMap(item)["change"])
			model.Mask(ch["after"], ch["after_sensitive"], known)
			model.Mask(ch["before"], ch["before_sensitive"], known)
		}
	}
	for _, item := range asMap(top["output_changes"]) {
		ch := asMap(item)
		model.Mask(ch["after"], ch["after_sensitive"], known)
		model.Mask(ch["before"], ch["before_sensitive"], known)
	}
	for _, vals := range []any{top["planned_values"], model.Get(top, "prior_state", "values")} {
		v := asMap(vals)
		for _, out := range asMap(v["outputs"]) {
			o := asMap(out)
			if b, _ := model.Bool(o["sensitive"]); b {
				known.Add(o["value"])
			}
		}
		walkTFModules(asMap(v["root_module"]), func(res map[string]any) {
			model.Mask(res["values"], res["sensitive_values"], known)
		})
	}
	for name, decl := range sensitiveVariables(top) {
		known.Add(asMap(decl)["default"])
		known.Add(model.Get(top, "variables", name, "value"))
	}
}

func walkTFModules(mod map[string]any, fn func(map[string]any)) {
	if mod == nil {
		return
	}
	for _, res := range asList(mod["resources"]) {
		if m := asMap(res); m != nil {
			fn(m)
		}
	}
	for _, child := range asList(mod["child_modules"]) {
		walkTFModules(asMap(child), fn)
	}
}

// sensitiveVariables are the root variables declared sensitive. Module
// variables are covered by the masks, since Terraform propagates them.
func sensitiveVariables(top map[string]any) map[string]any {
	out := map[string]any{}
	for name, decl := range asMap(model.Get(top, "configuration", "root_module", "variables")) {
		if b, _ := model.Bool(asMap(decl)["sensitive"]); b {
			out[name] = decl
		}
	}
	return out
}

// redactTFPlan returns the plan with every sensitive value replaced: the
// masks applied in resource_changes, resource_drift and output_changes,
// sensitive outputs and sensitive_values in planned_values and prior_state,
// sensitive variables (value and default), then every copy of a known value
// anywhere except in the plan's structure (addresses, types, names, modes,
// providers, actions and the version fields, which are restored so the copy
// reads back to the same resources), then model.Scrub over the whole
// document.
func redactTFPlan(top map[string]any) any {
	known := &model.Known{}
	collectTFSensitive(top, known)
	for _, list := range []string{"resource_changes", "resource_drift"} {
		for _, item := range asList(top[list]) {
			ch := asMap(asMap(item)["change"])
			if ch == nil {
				continue
			}
			ch["after"] = model.Mask(ch["after"], ch["after_sensitive"], nil)
			ch["before"] = model.Mask(ch["before"], ch["before_sensitive"], nil)
		}
	}
	for _, item := range asMap(top["output_changes"]) {
		ch := asMap(item)
		if ch == nil {
			continue
		}
		ch["after"] = model.Mask(ch["after"], ch["after_sensitive"], nil)
		ch["before"] = model.Mask(ch["before"], ch["before_sensitive"], nil)
	}
	for _, vals := range []any{top["planned_values"], model.Get(top, "prior_state", "values")} {
		v := asMap(vals)
		for _, out := range asMap(v["outputs"]) {
			o := asMap(out)
			if b, _ := model.Bool(o["sensitive"]); b && o["value"] != nil {
				o["value"] = model.RedactedSensitive
			}
		}
		walkTFModules(asMap(v["root_module"]), func(res map[string]any) {
			res["values"] = model.Mask(res["values"], res["sensitive_values"], nil)
		})
	}
	for name, decl := range sensitiveVariables(top) {
		if dm := asMap(decl); dm != nil && dm["default"] != nil {
			dm["default"] = model.RedactedSensitive
		}
		if vm := asMap(model.Get(top, "variables", name)); vm != nil && vm["value"] != nil {
			vm["value"] = model.RedactedSensitive
		}
	}
	red, _ := known.Replace(top).(map[string]any)
	restoreTFStructure(red, top)
	return model.Scrub(red)
}

// tfTopFields, tfChangeFields and tfResourceFields are the structural
// fields of a plan: identifiers and vocabulary, never attribute values.
var (
	tfTopFields      = []string{"format_version", "terraform_version", "applyable", "complete", "errored", "timestamp"}
	tfChangeFields   = []string{"address", "previous_address", "module_address", "mode", "type", "name", "index", "provider_name", "deposed", "action_reason"}
	tfResourceFields = []string{"address", "mode", "type", "name", "index", "provider_name", "schema_version"}
)

// restoreTFStructure copies the structural fields of orig back into red, a
// Known.Replace copy of it, so that a sensitive value equal to "delete" or
// to part of an address does not rewrite the plan's structure.
func restoreTFStructure(red, orig map[string]any) {
	if red == nil || orig == nil {
		return
	}
	copyFields(red, orig, tfTopFields)
	for _, list := range []string{"resource_changes", "resource_drift"} {
		rl, ol := asList(red[list]), asList(orig[list])
		for i := range rl {
			if i >= len(ol) {
				break
			}
			rm, om := asMap(rl[i]), asMap(ol[i])
			copyFields(rm, om, tfChangeFields)
			copyFields(asMap(rm["change"]), asMap(om["change"]), []string{"actions"})
		}
	}
	for _, path := range [][]string{{"planned_values"}, {"prior_state", "values"}} {
		restoreTFModule(asMap(model.Get(red, append(path, "root_module")...)), asMap(model.Get(orig, append(path, "root_module")...)))
	}
}

func restoreTFModule(red, orig map[string]any) {
	if red == nil || orig == nil {
		return
	}
	copyFields(red, orig, []string{"address"})
	for _, key := range []string{"resources", "child_modules"} {
		rl, ol := asList(red[key]), asList(orig[key])
		for i := range rl {
			if i >= len(ol) {
				break
			}
			if key == "resources" {
				copyFields(asMap(rl[i]), asMap(ol[i]), tfResourceFields)
			} else {
				restoreTFModule(asMap(rl[i]), asMap(ol[i]))
			}
		}
	}
}

func copyFields(dst, src map[string]any, keys []string) {
	if dst == nil || src == nil {
		return
	}
	for _, k := range keys {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
}
