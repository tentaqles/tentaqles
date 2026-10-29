package readers

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// maxNesting bounds the recursion into nested deployments and child
// resources (ARM itself allows 500 nested templates, what-if expands that
// far; a template deeper than this is cut, never a stack overflow).
const maxNesting = 32

// ARM reads an ARM template (and `bicep build` / `az bicep build --stdout`
// output). resources[] may be a list or, with languageVersion 2.0, a map of
// symbolic names; "existing" resources are not declared and are skipped.
// Child resources (a resource's own "resources") are flattened with the
// full type (parent type + "/" + child type) and the full name (parent name
// + "/" + child name). Inline nested deployments
// (Microsoft.Resources/deployments with properties.template) are kept as a
// resource, without the inner template's resources, and their resources are
// flattened after them with Meta parent. Names stay template expressions.
func ARM(name string, raw []byte, o Options) ([]model.Resource, error) {
	if err := checkSize(raw); err != nil {
		return nil, err
	}
	d, err := decodeJSON(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	top := asMap(d.root)
	if top == nil || top["resources"] == nil || !(isARMSchema(top["$schema"]) || top["contentVersion"] != nil) {
		return nil, fmt.Errorf("%s: %w: not an ARM template", name, ErrUnknownFormat)
	}
	a := &armReader{name: name, d: d, env: o.Env}
	a.resources(top["resources"], "", "", "", "/resources", 0)
	return a.out, nil
}

func isARMSchema(v any) bool {
	s, _ := v.(string)
	return strings.Contains(strings.ToLower(s), "deploymenttemplate.json")
}

type armReader struct {
	name string
	d    *doc
	env  string
	out  []model.Resource
}

type armItem struct {
	ptr, symbol string
	res         map[string]any
}

// resources reads one resources collection. parentType/parentName are set
// for child resources; parentAddr is the Meta parent of every resource here.
func (a *armReader) resources(coll any, parentType, parentName, parentAddr, ptr string, depth int) {
	if depth > maxNesting {
		return
	}
	var items []armItem
	switch t := coll.(type) {
	case []any:
		for i, x := range t {
			if m := asMap(x); m != nil {
				items = append(items, armItem{ptr: ptr + "/" + strconv.Itoa(i), res: m})
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if m := asMap(t[k]); m != nil {
				items = append(items, armItem{ptr: ptr + "/" + escPointer(k), symbol: k, res: m})
			}
		}
	}
	for _, it := range items {
		if b, _ := model.Bool(it.res["existing"]); b {
			continue
		}
		typ, nm := str(it.res["type"]), str(it.res["name"])
		if parentType != "" && !strings.Contains(typ, ".") {
			// A child resource: its type and name are relative to the parent.
			typ = parentType + "/" + typ
			nm = parentName + "/" + nm
		}
		addr := nm
		if addr == "" {
			addr = it.symbol
		}
		body := map[string]any{}
		for k, v := range it.res {
			if k == "resources" {
				continue
			}
			body[k] = v
		}
		r := model.Resource{
			Addr:   addr,
			Type:   typ,
			Source: model.KindARM,
			Env:    a.env,
			Pos:    model.Pos{File: a.name, Line: a.d.line(it.ptr), Pointer: it.ptr},
			Meta:   meta("parent", parentAddr, "symbol", it.symbol, "copy", str(model.Get(it.res, "copy", "name"))),
		}
		nested := strings.EqualFold(typ, "Microsoft.Resources/deployments")
		var inner any
		if nested {
			setMeta(&r, "deployment_mode", str(model.Get(it.res, "properties", "mode")))
			inner = model.Get(it.res, "properties", "template", "resources")
			if inner != nil {
				body = stripInnerResources(body)
			}
		}
		r.After = model.ScrubMap(body)
		a.out = append(a.out, r)
		if c := it.res["resources"]; c != nil {
			a.resources(c, typ, nm, addr, it.ptr+"/resources", depth+1)
		}
		if inner != nil {
			a.resources(inner, "", "", addr, it.ptr+"/properties/template/resources", depth+1)
		}
	}
}

// stripInnerResources copies body with properties.template.resources removed.
func stripInnerResources(body map[string]any) map[string]any {
	props := asMap(body["properties"])
	tmpl := asMap(props["template"])
	if tmpl == nil {
		return body
	}
	nt := map[string]any{}
	for k, v := range tmpl {
		if k != "resources" {
			nt[k] = v
		}
	}
	np := map[string]any{}
	for k, v := range props {
		np[k] = v
	}
	np["template"] = nt
	nb := map[string]any{}
	for k, v := range body {
		nb[k] = v
	}
	nb["properties"] = np
	return nb
}
