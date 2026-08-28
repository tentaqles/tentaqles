package setup

import (
	"github.com/tentaqles/tentaqles/cli/internal/detect"
	"github.com/tentaqles/tentaqles/cli/internal/providers"
)

// ToolCheck probes each company's identities' CLI tools, deduplicating
// probes across companies that share an identity (e.g. every company
// checking "claude" only runs detect.Check("claude") once).
func ToolCheck(p *SetupPlan, cat *providers.Catalog, d detect.Deps) map[string][]detect.Result {
	cache := map[string]detect.Result{}
	out := make(map[string][]detect.Result, len(p.Companies))

	for _, c := range p.Companies {
		ids := effectiveIdentities(c)
		results := make([]detect.Result, 0, len(ids))
		for _, id := range ids {
			r, ok := cache[id]
			if !ok {
				prov, known := cat.Get(id)
				if !known {
					r = detect.Result{ID: id, Err: "unknown identity"}
				} else {
					r = detect.Check(prov, d)
				}
				cache[id] = r
			}
			results = append(results, r)
		}
		out[c.Name] = results
	}
	return out
}
