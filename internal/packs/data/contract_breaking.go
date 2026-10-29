package data

// data/contract-breaking (spec C §3.2): compare each changed contract with
// its old side and apply the breaking-change table and the version rule.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// odcsChange is one row of the breaking-change table that a diff hit.
type odcsChange struct {
	Line     int
	Rule     string
	Breaking bool // false: a warn row (non-breaking but worth a look)
	Message  string
}

type odcsDiffOptions struct {
	EnumAddedBreaking bool
	Consumers         []string
}

type odcsDiff struct {
	opt     odcsDiffOptions
	changes []odcsChange
}

func (d *odcsDiff) breaking(line int, rule, format string, a ...any) {
	d.changes = append(d.changes, odcsChange{line, rule, true, fmt.Sprintf(format, a...)})
}

func (d *odcsDiff) warn(line int, rule, format string, a ...any) {
	d.changes = append(d.changes, odcsChange{line, rule, false, fmt.Sprintf(format, a...)})
}

// diffContracts returns the rows of the breaking-change table that the
// change old -> new hits (the version rule is applied by the caller).
func diffContracts(old, new *odcsContract, opt odcsDiffOptions) []odcsChange {
	d := &odcsDiff{opt: opt}
	pairs, removed, added := odcsMatch(old.Objects, new.Objects, func(o *odcsObject) (string, string) { return o.ID, o.Name })
	_ = added // a new object is non-breaking
	for _, o := range removed {
		d.breaking(new.schemaLine(), "object-removed", "object %s removed", odcsLabel(o.ID, o.Name))
	}
	for _, p := range pairs {
		d.diffObject(p[0], p[1])
	}
	d.diffSLA(old, new)
	d.diffServers(old, new)
	if old.ID != "" && old.ID != new.ID {
		d.warn(new.IDLine, "contract-id-changed", "contract id changed from %s to %s: consumers, catalogs and tooling keyed by the id lose it", old.ID, odcsOrNone(new.ID))
	}
	if strings.EqualFold(new.Status, "retired") && !strings.EqualFold(old.Status, "retired") && len(opt.Consumers) > 0 {
		d.breaking(new.StLine, "retired-with-consumers", "status set to retired while consumers are listed: %s", strings.Join(opt.Consumers, ", "))
	}
	return d.changes
}

func (c *odcsContract) schemaLine() int {
	if len(c.Objects) > 0 {
		return c.Objects[0].Line
	}
	return c.VersionLine
}

func (d *odcsDiff) diffObject(o, n *odcsObject) {
	name := odcsLabel(n.ID, n.Name)
	if o.Name != "" && n.Name != "" && o.Name != n.Name {
		d.breaking(n.Line, "object-renamed", "object %s renamed to %s", o.Name, n.Name)
	}
	if o.PhysicalName != n.PhysicalName && o.PhysicalName != "" {
		d.breaking(n.Line, "object-renamed", "object %s physicalName changed from %s to %s", name, o.PhysicalName, odcsOrNone(n.PhysicalName))
	}
	if op, np := odcsPKColumns(o.Props), odcsPKColumns(n.Props); strings.Join(op, ",") != strings.Join(np, ",") {
		d.breaking(n.Line, "primary-key-changed", "object %s primary key changed from (%s) to (%s)", name, strings.Join(op, ", "), strings.Join(np, ", "))
	}
	d.diffProps(name, n.Line, o.Props, n.Props)
	d.diffQuality("object "+name, n.Line, o.Quality, n.Quality)
}

func (d *odcsDiff) diffProps(owner string, ownerLine int, olds, news []*odcsProp) {
	pairs, removed, _ := odcsMatch(olds, news, func(p *odcsProp) (string, string) { return p.ID, p.Name })
	for _, p := range removed {
		d.breaking(ownerLine, "property-removed", "property %s.%s removed", owner, odcsLabel(p.ID, p.Name))
	}
	for _, pr := range pairs {
		d.diffProp(owner, pr[0], pr[1])
	}
}

func (d *odcsDiff) diffProp(owner string, o, n *odcsProp) {
	path := owner + "." + odcsLabel(n.ID, n.Name)
	if o.Name != "" && n.Name != "" && o.Name != n.Name {
		d.breaking(n.lineOf("name"), "property-renamed", "property %s.%s renamed to %s", owner, o.Name, n.Name)
	}
	if o.PhysicalName != "" && o.PhysicalName != n.PhysicalName {
		d.breaking(n.lineOf("physicalName"), "property-renamed", "property %s physicalName changed from %s to %s", path, o.PhysicalName, odcsOrNone(n.PhysicalName))
	}
	if o.LogicalType != "" && o.LogicalType != n.LogicalType && !odcsLogicalWidening(o.LogicalType, n.LogicalType) {
		d.breaking(n.lineOf("logicalType"), "logical-type-changed", "property %s logicalType changed from %s to %s", path, o.LogicalType, odcsOrNone(n.LogicalType))
	}
	if o.PhysicalType != "" {
		if n.PhysicalType == "" {
			d.breaking(n.Line, "physical-type-narrowed", "property %s physicalType %s removed", path, o.PhysicalType)
		} else if bad, why := odcsPhysNarrowed(o.PhysicalType, n.PhysicalType); bad {
			d.breaking(n.lineOf("physicalType"), "physical-type-narrowed", "property %s physicalType %s from %s to %s", path, why, o.PhysicalType, n.PhysicalType)
		}
	}
	if o.Required && !n.Required {
		d.breaking(n.lineOf("required"), "required-loosened", "property %s is no longer required (it may now be null)", path)
	}
	if o.Unique && !n.Unique {
		d.breaking(n.lineOf("unique"), "unique-loosened", "property %s is no longer unique", path)
	}
	d.diffOptions(path, o, n)
	d.diffEnum(path, o, n)
	if o.Partitioned != n.Partitioned || (o.Partitioned && o.PartPos != n.PartPos) {
		d.warn(n.lineOf("partitioned"), "partitioning-changed", "property %s partitioning changed (consumer query cost)", path)
	}
	if !strings.EqualFold(o.Classification, n.Classification) {
		d.warn(n.lineOf("classification"), "classification-changed", "property %s classification changed from %s to %s: privacy review and data/contract-pii", path, odcsOrNone(o.Classification), odcsOrNone(n.Classification))
	}
	d.diffQuality("property "+path, n.Line, o.Quality, n.Quality)
	d.diffProps(path, n.Line, o.Props, n.Props)
	switch {
	case o.Items != nil && n.Items != nil:
		d.diffProp(path, o.Items, n.Items)
	case o.Items != nil:
		d.breaking(n.Line, "property-removed", "property %s.items removed", path)
	}
}

// odcsLogicalWidening is the two widenings the table allows.
func odcsLogicalWidening(o, n string) bool {
	return (o == "integer" && n == "number") || (o == "date" && n == "timestamp")
}

// option direction: +1 when a larger value is looser, -1 when smaller is.
var odcsOptionDir = map[string]int{
	"maxLength": 1, "maximum": 1, "exclusiveMaximum": 1, "maxItems": 1, "maxProperties": 1,
	"minLength": -1, "minimum": -1, "exclusiveMinimum": -1, "minItems": -1, "minProperties": -1,
}

func (d *odcsDiff) diffOptions(path string, o, n *odcsProp) {
	line := n.lineOf("logicalTypeOptions")
	for _, k := range odcsSortedKeys(o.Options) {
		ov := o.Options[k]
		nv, ok := n.Options[k]
		if dir, bound := odcsOptionDir[k]; bound {
			if !ok {
				d.breaking(line, "type-options-loosened", "property %s logicalTypeOptions.%s removed", path, k)
				continue
			}
			if c, comparable := odcsCompare(ov.Value, nv.Value); !comparable {
				if ov.Value != nv.Value {
					d.breaking(nv.Line, "type-options-loosened", "property %s logicalTypeOptions.%s changed (not comparable)", path, k)
				}
			} else if c*dir > 0 {
				d.breaking(nv.Line, "type-options-loosened", "property %s logicalTypeOptions.%s loosened", path, k)
			}
			continue
		}
		if k == "uniqueItems" {
			// true -> false (or removed) lets duplicates through
			if odcsTrue(ov) && (!ok || !odcsTrue(nv)) {
				d.breaking(line, "type-options-loosened", "property %s logicalTypeOptions.uniqueItems loosened", path)
			}
			continue
		}
		if k == "pattern" || k == "format" {
			if !ok {
				d.breaking(line, "type-options-loosened", "property %s logicalTypeOptions.%s removed", path, k)
			} else if ov.Value != nv.Value {
				d.breaking(nv.Line, "type-options-loosened", "property %s logicalTypeOptions.%s changed (tq cannot prove it is stricter)", path, k)
			}
		}
	}
}

func odcsTrue(n *yaml.Node) bool {
	var b bool
	return n != nil && n.Kind == yaml.ScalarNode && n.Decode(&b) == nil && b
}

// odcsCompare returns sign(new - old) for two bounds (numbers, else ISO
// dates and times compared as text when both look like one).
func odcsCompare(oldV, newV string) (int, bool) {
	a, aerr := strconv.ParseFloat(strings.TrimSpace(oldV), 64)
	b, berr := strconv.ParseFloat(strings.TrimSpace(newV), 64)
	if aerr == nil && berr == nil {
		switch {
		case b > a:
			return 1, true
		case b < a:
			return -1, true
		}
		return 0, true
	}
	if odcsISODate(oldV) && odcsISODate(newV) {
		return strings.Compare(newV, oldV), true
	}
	return 0, false
}

func odcsISODate(s string) bool {
	return len(s) >= 10 && s[4] == '-' && s[7] == '-' && strings.Trim(s[:4], "0123456789") == ""
}

func (d *odcsDiff) diffEnum(path string, o, n *odcsProp) {
	if !o.HasEnum {
		return
	}
	line := n.lineOf("enum")
	if !n.HasEnum {
		d.enumAdded(line, "property %s enum removed: any value is now allowed", path)
		return
	}
	removed, added := 0, 0
	for _, v := range o.Enum {
		if !odcsContains(n.Enum, v) {
			removed++
		}
	}
	for _, v := range n.Enum {
		if !odcsContains(o.Enum, v) {
			added++
		}
	}
	// Values are data, not names: findings count them and never quote them.
	if removed > 0 {
		d.breaking(line, "enum-value-removed", "property %s lost %s", path, contractPlural(removed, "enum value"))
	}
	if added > 0 {
		d.enumAdded(line, "property %s gained %s", path, contractPlural(added, "enum value"))
	}
}

func (d *odcsDiff) enumAdded(line int, format string, a ...any) {
	if d.opt.EnumAddedBreaking {
		d.breaking(line, "enum-value-added", format+" (contracts.enum_added: breaking)", a...)
		return
	}
	d.warn(line, "enum-value-added", format, a...)
}

func (d *odcsDiff) diffQuality(owner string, ownerLine int, olds, news []*odcsQuality) {
	key := func(q *odcsQuality) (string, string) {
		if q.Metric == "" && q.Name == "" {
			return q.ID, ""
		}
		return q.ID, q.Metric + "/" + q.Name
	}
	pairs, removed, added := odcsMatch(olds, news, key)
	// Rules with no id, metric or name (a type: sql rule with a query and an
	// operator, a text or custom rule) pair by their content first (an
	// unchanged rule), then by type and operator set in order (an edited one).
	fp, removed, added := odcsMatch(removed, added, func(q *odcsQuality) (string, string) { return "", q.fingerprint() })
	pairs = append(pairs, fp...)
	sh, removed, _ := odcsMatch(removed, added, func(q *odcsQuality) (string, string) {
		if q.ID != "" || q.Metric != "" || q.Name != "" {
			return "", "" // a keyed rule that lost its key is removed
		}
		return "", "shape\x00" + q.shape()
	})
	pairs = append(pairs, sh...)
	for _, q := range removed {
		d.breaking(ownerLine, "quality-removed", "%s quality rule %s removed", owner, odcsQualityLabel(q))
	}
	for _, p := range pairs {
		o, n := p[0], p[1]
		ql := odcsQualityLabel(n)
		if o.Metric != n.Metric {
			d.breaking(n.Line, "quality-loosened", "%s quality rule %s metric changed", owner, ql)
		}
		if o.Type != n.Type {
			d.breaking(n.Line, "quality-loosened", "%s quality rule %s type changed", owner, ql)
		}
		// The query and implementation text are compared, never printed.
		if odcsSquash(o.Query) != odcsSquash(n.Query) {
			d.breaking(n.Line, "quality-loosened", "%s quality rule %s query changed (tq cannot prove it is stricter)", owner, ql)
		}
		if o.Impl != n.Impl {
			d.breaking(n.Line, "quality-loosened", "%s quality rule %s implementation changed (tq cannot prove it is stricter)", owner, ql)
		}
		d.diffQualityArgs(owner, ql, o, n)
		for _, op := range odcsQualityOps {
			ov, had := o.Ops[op]
			if !had {
				continue
			}
			nv, has := n.Ops[op]
			if !has {
				d.breaking(n.Line, "quality-loosened", "%s quality rule %s threshold %s removed", owner, ql, op)
				continue
			}
			if odcsThresholdLoosened(op, odcsNodeText(ov), odcsNodeText(nv), odcsPair(ov), odcsPair(nv)) {
				d.breaking(nv.Line, "quality-loosened", "%s quality rule %s threshold %s loosened", owner, ql, op)
			}
		}
	}
}

// diffQualityArgs: arguments.validValues gaining a value lets more values
// through (loosened); any other argument change can't be proven stricter.
func (d *odcsDiff) diffQualityArgs(owner, ql string, o, n *odcsQuality) {
	for _, k := range odcsSortedKeys(o.Args) {
		ov := o.Args[k]
		nv, ok := n.Args[k]
		switch {
		case !ok:
			d.breaking(n.Line, "quality-loosened", "%s quality rule %s argument %s removed", owner, ql, k)
		case k == "validValues":
			olds := scan.Strings(ov)
			added := 0
			for _, v := range scan.Strings(nv) {
				if !odcsContains(olds, v) {
					added++
				}
			}
			if added > 0 {
				d.breaking(nv.Line, "quality-loosened", "%s quality rule %s gained %s", owner, ql, contractPlural(added, "valid value"))
			}
		case contractNodeText(ov) != contractNodeText(nv):
			d.breaking(nv.Line, "quality-loosened", "%s quality rule %s argument %s changed (tq cannot prove it is stricter)", owner, ql, k)
		}
	}
}

func odcsQualityLabel(q *odcsQuality) string {
	switch {
	case q.ID != "":
		return q.ID
	case q.Name != "":
		return q.Name
	case q.Metric != "":
		return q.Metric
	case q.Type != "":
		return fmt.Sprintf("%s (line %d)", q.Type, q.Line)
	}
	return fmt.Sprintf("(line %d)", q.Line)
}

// odcsThresholdLoosened applies the operator's direction. Values tq cannot
// compare count as loosened when they differ.
func odcsThresholdLoosened(op, ov, nv string, orng, nrng []float64) bool {
	switch op {
	case "mustBeGreaterThan", "mustBeGreaterOrEqualTo":
		c, ok := odcsCompare(ov, nv)
		return !ok && ov != nv || ok && c < 0
	case "mustBeLessThan", "mustBeLessOrEqualTo":
		c, ok := odcsCompare(ov, nv)
		return !ok && ov != nv || ok && c > 0
	case "mustBeBetween":
		if orng == nil || nrng == nil {
			return true
		}
		return nrng[0] < orng[0] || nrng[1] > orng[1]
	case "mustNotBeBetween":
		if orng == nil || nrng == nil {
			return true
		}
		return nrng[0] > orng[0] || nrng[1] < orng[1]
	}
	// mustBe, mustNotBe: any change may loosen.
	return ov != nv
}

// odcsPair reads a [low, high] range of mustBeBetween / mustNotBeBetween.
func odcsPair(n *yaml.Node) []float64 {
	items := scan.Items(n)
	if len(items) != 2 {
		return nil
	}
	a, aerr := strconv.ParseFloat(scan.Scalar(items[0]), 64)
	b, berr := strconv.ParseFloat(scan.Scalar(items[1]), 64)
	if aerr != nil || berr != nil {
		return nil
	}
	return []float64{a, b}
}

// odcsNodeText is a comparable text of a threshold value (scalar or list).
func odcsNodeText(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return strings.Join(scan.Strings(n), ",")
}

func (d *odcsDiff) diffSLA(old, new *odcsContract) {
	key := func(s *odcsSLA) (string, string) { return "", s.Property + "@" + s.Element }
	pairs, removed, _ := odcsMatch(old.SLA, new.SLA, key)
	for _, s := range removed {
		d.breaking(new.slaLine(), "sla-loosened", "SLA %s removed", odcsSLALabel(s))
	}
	for _, p := range pairs {
		o, n := p[0], p[1]
		if o.Value == n.Value && odcsUnitSeconds(o.Unit) == odcsUnitSeconds(n.Unit) {
			continue
		}
		dir, known := odcsSLADir[strings.ToLower(n.Property)]
		if !known {
			d.warn(n.Line, "sla-changed", "SLA %s changed (tq does not know which way is looser)", odcsSLALabel(n))
			continue
		}
		c, ok := odcsSLACompare(o, n)
		if !ok {
			d.warn(n.Line, "sla-changed", "SLA %s changed and the values cannot be compared", odcsSLALabel(n))
			continue
		}
		if c*dir > 0 {
			d.breaking(n.Line, "sla-loosened", "SLA %s loosened", odcsSLALabel(n))
		}
	}
}

func (c *odcsContract) slaLine() int {
	if len(c.SLA) > 0 {
		return c.SLA[0].Line
	}
	return c.VersionLine
}

func odcsSLALabel(s *odcsSLA) string {
	if s.Element != "" {
		return s.Property + " (" + s.Element + ")"
	}
	return s.Property
}

// odcsSLADir: +1 when a larger value is looser, -1 when a smaller one is.
var odcsSLADir = map[string]int{
	"latency": 1, "frequency": 1, "errorrate": 1, "timetodetect": 1, "timetonotify": 1, "timetorepair": 1,
	"retention": -1, "availability": -1, "throughput": -1, "endofsupport": -1, "endoflife": -1,
}

// odcsSLACompare returns sign(new - old) in a common unit.
func odcsSLACompare(o, n *odcsSLA) (int, bool) {
	ou, nu := odcsUnitSeconds(o.Unit), odcsUnitSeconds(n.Unit)
	if ou == 0 || nu == 0 {
		if !strings.EqualFold(o.Unit, n.Unit) {
			return 0, false
		}
		// A percent (availability: 99.9%) compares as a number when both
		// sides are percents.
		ov, op := strings.CutSuffix(strings.TrimSpace(o.Value), "%")
		nv, np := strings.CutSuffix(strings.TrimSpace(n.Value), "%")
		if op != np {
			return 0, false
		}
		return odcsCompare(ov, nv)
	}
	a, aerr := strconv.ParseFloat(o.Value, 64)
	b, berr := strconv.ParseFloat(n.Value, 64)
	if aerr != nil || berr != nil {
		return 0, false
	}
	a, b = a*ou, b*nu
	switch {
	case math.Abs(b-a) < 1e-9:
		return 0, true
	case b > a:
		return 1, true
	}
	return -1, true
}

// odcsUnitSeconds converts an ODCS SLA time unit to seconds (0 = not a time unit).
func odcsUnitSeconds(u string) float64 {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "ms", "millisecond", "milliseconds":
		return 0.001
	case "s", "sec", "second", "seconds":
		return 1
	case "m", "min", "minute", "minutes":
		return 60
	case "h", "hr", "hour", "hours":
		return 3600
	case "d", "day", "days":
		return 86400
	case "w", "wk", "week", "weeks":
		return 7 * 86400
	case "mo", "month", "months":
		return 30 * 86400
	case "y", "yr", "year", "years":
		return 365 * 86400
	}
	return 0
}

func (d *odcsDiff) diffServers(old, new *odcsContract) {
	key := func(s *odcsServer) (string, string) { return "", s.Server }
	pairs, removed, _ := odcsMatch(old.Servers, new.Servers, key)
	for _, s := range removed {
		line := new.VersionLine
		if len(new.Servers) > 0 {
			line = new.Servers[0].Line
		}
		d.breaking(line, "server-removed", "server %s removed", s.Server)
	}
	for _, p := range pairs {
		o, n := p[0], p[1]
		var changed []string
		for _, f := range odcsServerAddress {
			if o.Fields[f] != n.Fields[f] {
				changed = append(changed, f)
			}
		}
		if len(changed) > 0 {
			d.breaking(n.Line, "server-changed", "server %s changed %s", n.Server, strings.Join(changed, ", "))
		}
	}
}

// odcsMatch pairs old and new elements by id, then the rest by name.
func odcsMatch[T any](olds, news []T, key func(T) (id, name string)) (pairs [][2]T, removed, added []T) {
	usedNew := make([]bool, len(news))
	usedOld := make([]bool, len(olds))
	for i, o := range olds {
		oid, _ := key(o)
		if oid == "" {
			continue
		}
		for j, n := range news {
			if nid, _ := key(n); !usedNew[j] && nid == oid {
				pairs = append(pairs, [2]T{o, n})
				usedOld[i], usedNew[j] = true, true
				break
			}
		}
	}
	for i, o := range olds {
		if usedOld[i] {
			continue
		}
		_, on := key(o)
		if on == "" {
			continue
		}
		for j, n := range news {
			if _, nn := key(n); !usedNew[j] && nn == on {
				pairs = append(pairs, [2]T{o, n})
				usedOld[i], usedNew[j] = true, true
				break
			}
		}
	}
	for i, o := range olds {
		if !usedOld[i] {
			removed = append(removed, o)
		}
	}
	for j, n := range news {
		if !usedNew[j] {
			added = append(added, n)
		}
	}
	return
}

// odcsPKColumns is the primary key in position order.
func odcsPKColumns(props []*odcsProp) []string {
	type col struct {
		name string
		pos  int
		idx  int
	}
	var cols []col
	for i, p := range props {
		if p.PK {
			cols = append(cols, col{odcsLabel(p.ID, p.Name), p.PKPos, i})
		}
	}
	for i := 1; i < len(cols); i++ { // insertion sort: stable, tiny lists
		for j := i; j > 0; j-- {
			a, b := cols[j-1], cols[j]
			ap, bp := a.pos, b.pos
			if ap < 0 {
				ap = 1 << 30
			}
			if bp < 0 {
				bp = 1 << 30
			}
			if bp < ap || bp == ap && b.idx < a.idx {
				cols[j-1], cols[j] = b, a
			}
		}
	}
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.name
	}
	return out
}

func odcsOrNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func odcsSortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// runContractBreaking is the Run of data/contract-breaking.
func runContractBreaking(ctx context.Context, in *packs.Input) packs.Result {
	changes := contractChanges(in)
	if in.Base == "" || in.All {
		return packs.Result{Status: packs.Skip, Detail: "no old side to compare (--all or no base)"}
	}
	s := in.Settings()
	var fs []packs.Finding
	n, breakingFiles := 0, 0
	for _, ch := range changes {
		if ctx.Err() != nil {
			return packs.Result{Status: packs.Fail, Detail: "timed out"}
		}
		oldPath := ch.Path
		if ch.OldPath != "" {
			oldPath = ch.OldPath
		}
		if ch.Status == 'A' {
			continue // no old side: only contract-valid runs
		}
		// M, R and D exist at Base by definition: an old side that can't be
		// read is a failure, never "nothing to compare".
		old, _, err := readOldContract(in, oldPath)
		if errors.Is(err, errContractOldRead) {
			n++
			fs = append(fs, packs.Finding{File: oldPath, Level: packs.LevelError, Rule: "contract-old-unreadable",
				Message: "cannot read the base side of this contract; the breaking-change rules could not run"})
			continue
		}
		if err != nil {
			continue // the old side does not parse: it was not a contract
		}
		switch ch.Status {
		case 'D':
			if !old.isContractDoc() {
				continue // the old side was not a contract either
			}
			n++
			cons := contractConsumers(s, old, oldPath)
			msg := "contract file deleted"
			if len(cons) > 0 {
				msg += "; consumers: " + strings.Join(cons, ", ")
			}
			if strings.EqualFold(old.Status, "retired") && len(cons) == 0 {
				fs = append(fs, packs.Finding{File: oldPath, Level: packs.LevelWarn, Rule: "contract-deleted", Message: msg + " (it was retired)"})
				continue
			}
			breakingFiles++
			fs = append(fs, packs.Finding{File: oldPath, Level: packs.LevelError, Rule: "contract-deleted", Message: msg + "; set status: retired and remove the consumers first"})
			continue
		}
		nw, err := readContract(in, ch.Path)
		if err != nil {
			fs = append(fs, packs.Finding{File: ch.Path, Level: packs.LevelError, Rule: "contract-unreadable", Message: "the new side does not parse; see data/contract-valid"})
			continue
		}
		if !old.isContractDoc() || !nw.isContractDoc() {
			continue // contract-valid reports a file that is not a contract
		}
		n++
		cons := contractConsumers(s, nw, ch.Path)
		if old.ID != nw.ID || oldPath != ch.Path {
			// consumers keyed by the old id (or file) still read this contract
			for _, c := range contractConsumers(s, old, oldPath) {
				if !odcsContains(cons, c) {
					cons = append(cons, c)
				}
			}
		}
		rows := diffContracts(old, nw, odcsDiffOptions{EnumAddedBreaking: contractEnumAddedBreaking(s), Consumers: cons})
		f, broke := applyVersionRule(ch.Path, old, nw, rows, cons)
		fs = append(fs, f...)
		if broke {
			breakingFiles++
		}
	}
	r := packs.Result{Status: packs.OK, Findings: fs}
	errs, _ := r.Counts()
	switch {
	case errs > 0 && breakingFiles == 0:
		r.Status = packs.Fail
		r.Detail = fmt.Sprintf("%s, %s", contractPlural(n, "contract"), contractPlural(errs, "error"))
	case errs > 0:
		r.Status = packs.Fail
		r.Detail = fmt.Sprintf("%s, %d with breaking changes and no major version bump", contractPlural(n, "contract"), breakingFiles)
	case breakingFiles > 0:
		r.Detail = fmt.Sprintf("%s, %d with breaking changes and a major version bump: mark the PR as breaking", contractPlural(n, "contract"), breakingFiles)
	default:
		r.Detail = contractPlural(n, "contract") + ", no breaking change"
	}
	return r
}

// applyVersionRule turns the table rows into findings: breaking rows are
// errors unless the major version was bumped (then warns, so the PR still
// names them). The bool reports a breaking change.
func applyVersionRule(file string, old, nw *odcsContract, rows []odcsChange, consumers []string) ([]packs.Finding, bool) {
	var fs []packs.Finding
	hasBreaking := false
	for _, r := range rows {
		if r.Breaking {
			hasBreaking = true
		}
	}
	oM, oMi, oP, ook := odcsSemver(old.Version)
	nM, nMi, nP, nok := odcsSemver(nw.Version)
	bumped := ook && nok && nM > oM
	for _, r := range rows {
		lvl := packs.LevelWarn
		if r.Breaking && !bumped {
			lvl = packs.LevelError
		}
		rule := r.Rule
		fs = append(fs, packs.Finding{File: file, Line: r.Line, Level: lvl, Rule: rule, Message: r.Message})
	}
	switch {
	case hasBreaking && (!ook || !nok):
		fs = append(fs, packs.Finding{File: file, Line: nw.VersionLine, Level: packs.LevelError, Rule: "version-not-semver", Message: "a breaking change needs a semver version (MAJOR.MINOR.PATCH) to bump"})
	case hasBreaking && !bumped:
		msg := fmt.Sprintf("breaking change without a major version bump (%s -> %s); bump to %d.0.0", old.Version, nw.Version, oM+1)
		if len(consumers) > 0 {
			msg += "; consumers: " + strings.Join(consumers, ", ")
		}
		fs = append(fs, packs.Finding{File: file, Line: nw.VersionLine, Level: packs.LevelError, Rule: "version-not-bumped", Message: msg})
	case !hasBreaking && bumped:
		fs = append(fs, packs.Finding{File: file, Line: nw.VersionLine, Level: packs.LevelWarn, Rule: "major-without-breaking", Message: fmt.Sprintf("major version bumped (%s -> %s) with no breaking change", old.Version, nw.Version)})
	}
	if ook && nok && (nM < oM || nM == oM && (nMi < oMi || nMi == oMi && nP < oP)) {
		fs = append(fs, packs.Finding{File: file, Line: nw.VersionLine, Level: packs.LevelError, Rule: "version-decreased", Message: fmt.Sprintf("version went down (%s -> %s)", old.Version, nw.Version)})
	}
	return fs, hasBreaking
}
