package policy

import (
	"encoding/json"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// Namespace is the bundle namespace that judges a document kind, or "" when
// the bundle has no rules for it (Pulumi).
func Namespace(k model.Kind) string {
	switch k {
	case model.KindTFPlan:
		return NamespaceTerraform
	case model.KindARM, model.KindWhatIf:
		return NamespaceARM
	case model.KindCFN, model.KindChangeSet:
		return NamespaceCFN
	}
	return ""
}

// ConftestInput is the document Conftest reads for d: built from the
// reader's resources, so it holds only redacted values and every resource
// carries the address tq maps findings back with. The shapes follow the
// source formats (see the header of each .rego file).
func ConftestInput(d Doc) (string, []byte, error) {
	ns := Namespace(d.Kind)
	if ns == "" {
		return "", nil, nil
	}
	var doc any
	switch ns {
	case NamespaceTerraform:
		rcs := []any{}
		for _, r := range d.Resources {
			rcs = append(rcs, map[string]any{
				"address": r.Addr,
				"type":    r.Type,
				"change":  map[string]any{"actions": actions(r), "after": nilMap(r.After)},
			})
		}
		doc = map[string]any{"resource_changes": rcs}
	case NamespaceARM:
		rs := []any{}
		for _, r := range d.Resources {
			if !r.Changes() || r.After == nil {
				continue
			}
			m := map[string]any{}
			for k, v := range r.After {
				m[k] = v
			}
			m["tq_addr"] = r.Addr
			if _, ok := m["type"]; !ok || r.Source == model.KindARM {
				m["type"] = r.Type // the full type of a flattened child
			}
			rs = append(rs, m)
		}
		doc = map[string]any{"resources": rs}
	case NamespaceCFN:
		res := map[string]any{}
		for _, r := range d.Resources {
			if !r.Changes() || r.After == nil {
				continue
			}
			res[r.Addr] = map[string]any{"Type": r.Type, "Properties": r.After}
		}
		doc = map[string]any{"Resources": res}
	}
	b, err := json.Marshal(doc)
	return ns, b, err
}

func actions(r model.Resource) []string {
	if r.Actions == nil {
		return []string{model.ActCreate} // a template declares its resources
	}
	return r.Actions
}

func nilMap(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}
