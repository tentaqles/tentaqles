package data

// data/contract-pii (spec C §3.2): a classified property's table must be in
// the manifest data.paths with that class, and an examples value on a
// classified property must not hit the redaction detectors.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/redact"
	"gopkg.in/yaml.v3"
)

// contractClassMap maps ODCS classification values to tq data classes. The
// tq classes map to themselves. Values not listed (public, internal,
// confidential, numeric levels) are not personal-data classes.
var contractClassMap = map[string]string{
	"pii": "pii", "personal": "pii", "personal-data": "pii", "personal_data": "pii", "pi": "pii", "gdpr": "pii", "lgpd": "pii",
	"sensitive": "sensitive", "restricted": "sensitive", "special-category": "sensitive", "special_category": "sensitive", "spi": "sensitive",
	"financial": "financial", "pci": "financial",
	"children": "children", "minors": "children",
}

// contractClass is the tq class of an ODCS classification ("" = none).
func contractClass(v string) string {
	return contractClassMap[strings.ToLower(strings.TrimSpace(v))]
}

func runContractPII(ctx context.Context, in *packs.Input) packs.Result {
	targets := contractTargets(in)
	if len(targets) == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no contract file changed"}
	}
	tables := map[string][]string{}
	if in.Manifest != nil {
		tables = in.Manifest.Data.Tables()
	}
	var fs []packs.Finding
	classified := 0
	for _, p := range targets {
		if ctx.Err() != nil {
			return packs.Result{Status: packs.Fail, Detail: "timed out"}
		}
		c, err := readContract(in, p)
		if err != nil || !c.isContractDoc() {
			continue // data/contract-valid reports it
		}
		var schemas []string
		for _, s := range c.Servers {
			if v := s.Fields["schema"]; v != "" && !odcsContains(schemas, v) {
				schemas = append(schemas, v)
			}
		}
		for _, o := range c.Objects {
			walkContractProps(o.Props, odcsLabel(o.ID, o.Name), func(path string, pr *odcsProp) {
				cls := contractClass(pr.Classification)
				if cls != "" {
					classified++
					if have := contractTableClasses(tables, o.table(), schemas); !odcsContains(have, cls) {
						as := pr.Classification
						if !strings.EqualFold(as, cls) {
							as += " (" + cls + ")"
						}
						fs = append(fs, packs.Finding{File: p, Line: pr.ClassLine, Level: packs.LevelError, Rule: "table-not-classified",
							Message: fmt.Sprintf("property %s is classified %s but table %s is not in the manifest data.paths with class %s; the human adds it to the manifest", path, as, o.table(), cls)})
					}
				}
				fs = append(fs, contractExampleFindings(c, path, pr, cls)...)
			})
		}
	}
	r := packs.Result{Status: packs.OK, Findings: fs}
	if errs, _ := r.Counts(); errs > 0 {
		r.Status = packs.Fail
		r.Detail = fmt.Sprintf("%s, %s", contractPlural(classified, "classified property"), contractPlural(errs, "problem"))
	} else if classified == 0 {
		r.Detail = "no classified properties in " + contractPlural(len(targets), "contract")
	} else {
		r.Detail = contractPlural(classified, "classified property") + ", tables classified in the manifest"
	}
	return r
}

// walkContractProps visits every property, nested ones and array items too,
// with its dotted path.
func walkContractProps(props []*odcsProp, owner string, fn func(path string, p *odcsProp)) {
	for _, p := range props {
		path := owner + "." + odcsLabel(p.ID, p.Name)
		fn(path, p)
		walkContractProps(p.Props, path, fn)
		if p.Items != nil {
			walkContractProps([]*odcsProp{p.Items}, path, fn)
		}
	}
}

// contractTableClasses looks a table up in the manifest tables, bare and
// qualified with each server schema (manifest names are lower-cased).
func contractTableClasses(tables map[string][]string, table string, schemas []string) []string {
	t := strings.ToLower(strings.Trim(table, `"`))
	var out []string
	add := func(k string) {
		for _, c := range tables[k] {
			if !odcsContains(out, c) {
				out = append(out, c)
			}
		}
	}
	add(t)
	for _, s := range schemas {
		add(strings.ToLower(s) + "." + t)
	}
	sort.Strings(out)
	return out
}

// contractExampleFindings runs the redaction detectors over a property's
// examples. A line (or the examples: key line) with tq:allow-pii marks
// known-synthetic values. Findings name the detector, never the value.
func contractExampleFindings(c *odcsContract, path string, p *odcsProp, cls string) []packs.Finding {
	if len(p.Examples) == 0 {
		return nil
	}
	classes := []string{"pii"}
	if cls != "" {
		classes = []string{cls}
	}
	keyAllowed := strings.Contains(c.line(p.ExamplesLine), redact.AllowMarker)
	var fs []packs.Finding
	for _, ex := range p.Examples {
		if keyAllowed || strings.Contains(c.line(ex.Line), redact.AllowMarker) || strings.Contains(ex.LineComment, redact.AllowMarker) {
			continue
		}
		hits := redact.CountAll(contractNodeText(ex), classes)
		if len(hits) == 0 {
			continue
		}
		names := make([]string, 0, len(hits))
		for n := range hits {
			names = append(names, n)
		}
		sort.Strings(names)
		f := packs.Finding{File: c.Path, Line: ex.Line, Rule: "pii-example", Level: packs.LevelError,
			Message: fmt.Sprintf("an example of %s matches the %s detector; use a synthetic value and mark it tq:allow-pii", path, strings.Join(names, ", "))}
		if cls == "" {
			f.Level, f.Rule = packs.LevelWarn, "unclassified-example"
			f.Message = fmt.Sprintf("an example of %s matches the %s detector but the property is not classified", path, strings.Join(names, ", "))
		}
		fs = append(fs, f)
	}
	return fs
}

// contractNodeText is every scalar under n, one per line.
func contractNodeText(n *yaml.Node) string {
	var parts []string
	scan.Walk(n, func(_ []string, x *yaml.Node) bool {
		if x.Kind == yaml.ScalarNode {
			parts = append(parts, x.Value)
		}
		return true
	})
	return strings.Join(parts, "\n")
}
