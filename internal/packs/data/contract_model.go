package data

// ODCS v3 data contracts (spec C §3.2): the parsed model shared by the
// contract checks. Field names follow the ODCS v3 JSON Schema
// (odcs-json-schema-latest, v3.2.0 at build time; v3.0.x and v3.1.x use the
// same names for everything read here).

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// odcsCheckedVersion is the ODCS JSON Schema version the field names were
// verified against (recorded in docs/packs/data-contracts.md).
const odcsCheckedVersion = "v3.2.0"

// odcsLogicalTypes is the logicalType enum of ODCS v3.
var odcsLogicalTypes = []string{"string", "date", "timestamp", "time", "number", "integer", "object", "array", "boolean", "map", "vector"}

var (
	odcsAPIV3Re  = regexp.MustCompile(`^v3\.\d+\.\d+$`)
	odcsSemverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$`)
)

type odcsContract struct {
	Path                                           string
	APIVersion, Kind, ID, Version, Status, Name    string
	APILine, KindLine, IDLine, VersionLine, StLine int
	Objects                                        []*odcsObject
	SLA                                            []*odcsSLA
	Servers                                        []*odcsServer
	DBTModel                                       string
	DBTModelLine                                   int
	// Lines is the source split by line (for inline tq:allow-pii markers).
	Lines []string
	// Problems are structural errors found while parsing (missing required
	// fields, wrong node kinds, duplicate keys).
	Problems []odcsProblem
}

type odcsProblem struct {
	Line    int
	Rule    string
	Message string
}

type odcsObject struct {
	ID, Name, PhysicalName string
	Line                   int
	Props                  []*odcsProp
	Quality                []*odcsQuality
	DBTModel               string
	DBTModelLine           int
}

type odcsProp struct {
	ID, Name, PhysicalName  string
	LogicalType             string
	PhysicalType            string
	Classification          string
	Required, Unique, PK    bool
	Partitioned             bool
	PKPos, PartPos          int
	Options                 map[string]*yaml.Node
	HasEnum                 bool
	Enum                    []string
	Examples                []*yaml.Node
	Quality                 []*odcsQuality
	Props                   []*odcsProp
	Items                   *odcsProp
	Line                    int
	lines                   map[string]int
	ClassLine, ExamplesLine int
}

type odcsQuality struct {
	ID, Metric, Name, Type string
	Ops                    map[string]*yaml.Node
	Args                   map[string]*yaml.Node
	Line                   int
}

type odcsSLA struct {
	Property, Element, Unit string
	Value                   string
	Line                    int
}

type odcsServer struct {
	Server string
	Fields map[string]string
	Line   int
}

// odcsQualityOps are the ODCS v3 data quality operators.
var odcsQualityOps = []string{"mustBe", "mustNotBe", "mustBeGreaterThan", "mustBeGreaterOrEqualTo", "mustBeLessThan", "mustBeLessOrEqualTo", "mustBeBetween", "mustNotBeBetween"}

// odcsServerAddress are the server fields that locate the data: a change to any
// of them moves the dataset under its consumers.
var odcsServerAddress = []string{"type", "host", "port", "account", "database", "catalog", "project", "dataset", "schema", "table", "location", "path", "workspace", "warehouse", "endpointUrl"}

// label is the human name of an element in findings: the name, or the id.
func odcsLabel(id, name string) string {
	if name != "" {
		return name
	}
	if id != "" {
		return id
	}
	return "(unnamed)"
}

// parseContract parses one contract file. A YAML syntax error is returned as
// an error; structural problems land in Problems.
func parseContract(path string, data []byte) (*odcsContract, error) {
	root, err := scan.ParseYAML(data)
	if err != nil {
		return nil, err
	}
	c := &odcsContract{Path: path, Lines: strings.Split(string(data), "\n")}
	if root.Kind != yaml.MappingNode {
		c.Problems = append(c.Problems, odcsProblem{1, "contract-not-mapping", "the contract is not a YAML mapping"})
		return c, nil
	}
	seen := map[string]int{}
	for _, kv := range scan.Entries(root) {
		if l, dup := seen[kv.Key]; dup {
			c.Problems = append(c.Problems, odcsProblem{kv.Line, "duplicate-key", fmt.Sprintf("key %q is repeated (first on line %d)", kv.Key, l)})
		}
		seen[kv.Key] = kv.Line
		switch kv.Key {
		case "apiVersion":
			c.APIVersion, c.APILine = scan.Scalar(kv.Value), kv.Line
		case "kind":
			c.Kind, c.KindLine = scan.Scalar(kv.Value), kv.Line
		case "id":
			c.ID, c.IDLine = scan.Scalar(kv.Value), kv.Line
		case "version":
			c.Version, c.VersionLine = scan.Scalar(kv.Value), kv.Line
		case "status":
			c.Status, c.StLine = scan.Scalar(kv.Value), kv.Line
		case "name":
			c.Name = scan.Scalar(kv.Value)
		case "schema":
			c.Objects = c.parseObjects(kv)
		case "slaProperties":
			c.SLA = c.parseSLA(kv)
		case "servers":
			c.Servers = c.parseServers(kv)
		case "customProperties":
			c.DBTModel, c.DBTModelLine = odcsDBTModel(kv.Value)
		}
	}
	return c, nil
}

func (c *odcsContract) problem(line int, rule, format string, a ...any) {
	c.Problems = append(c.Problems, odcsProblem{line, rule, fmt.Sprintf(format, a...)})
}

func (c *odcsContract) parseObjects(kv scan.KeyValue) []*odcsObject {
	if kv.Value.Kind != yaml.SequenceNode {
		c.problem(kv.Line, "schema-not-list", "schema must be a list of objects")
		return nil
	}
	var out []*odcsObject
	for _, it := range scan.Items(kv.Value) {
		if it.Kind != yaml.MappingNode {
			c.problem(it.Line, "object-not-mapping", "a schema entry must be a mapping")
			continue
		}
		o := &odcsObject{Line: it.Line}
		for _, e := range scan.Entries(it) {
			switch e.Key {
			case "id":
				o.ID = scan.Scalar(e.Value)
			case "name":
				o.Name = scan.Scalar(e.Value)
			case "physicalName":
				o.PhysicalName = scan.Scalar(e.Value)
			case "properties":
				o.Props = c.parseProps(e, "object "+odcsLabel(o.ID, o.Name+o.PhysicalName))
			case "quality":
				o.Quality = c.parseQuality(e)
			case "customProperties":
				o.DBTModel, o.DBTModelLine = odcsDBTModel(e.Value)
			}
		}
		if o.Name == "" {
			c.problem(o.Line, "object-name-missing", "a schema object needs a name")
		}
		out = append(out, o)
	}
	odcsCheckUnique(c, out, func(o *odcsObject) (string, string, int) { return o.ID, o.Name, o.Line }, "schema object")
	return out
}

func (c *odcsContract) parseProps(kv scan.KeyValue, owner string) []*odcsProp {
	if kv.Value.Kind != yaml.SequenceNode {
		c.problem(kv.Line, "properties-not-list", "properties of %s must be a list", owner)
		return nil
	}
	var out []*odcsProp
	for _, it := range scan.Items(kv.Value) {
		if p := c.parseProp(it, owner); p != nil {
			if p.Name == "" {
				c.problem(p.Line, "property-name-missing", "a property of %s needs a name", owner)
			}
			out = append(out, p)
		}
	}
	odcsCheckUnique(c, out, func(p *odcsProp) (string, string, int) { return p.ID, p.Name, p.Line }, "property of "+owner)
	return out
}

func (c *odcsContract) parseProp(it *yaml.Node, owner string) *odcsProp {
	if it.Kind != yaml.MappingNode {
		c.problem(it.Line, "property-not-mapping", "a property of %s must be a mapping", owner)
		return nil
	}
	p := &odcsProp{Line: it.Line, PKPos: -1, PartPos: -1, lines: map[string]int{}}
	for _, e := range scan.Entries(it) {
		p.lines[e.Key] = e.Line
		switch e.Key {
		case "id":
			p.ID = scan.Scalar(e.Value)
		case "name":
			p.Name = scan.Scalar(e.Value)
		case "physicalName":
			p.PhysicalName = scan.Scalar(e.Value)
		case "logicalType":
			p.LogicalType = scan.Scalar(e.Value)
			if p.LogicalType != "" && !odcsContains(odcsLogicalTypes, p.LogicalType) {
				c.problem(e.Line, "logical-type-unknown", "logicalType %q is not an ODCS v3 type (%s)", p.LogicalType, strings.Join(odcsLogicalTypes, ", "))
			}
		case "physicalType":
			p.PhysicalType = scan.Scalar(e.Value)
		case "classification":
			p.Classification, p.ClassLine = scan.Scalar(e.Value), e.Line
		case "required":
			p.Required = c.boolOf(e)
		case "unique":
			p.Unique = c.boolOf(e)
		case "primaryKey":
			p.PK = c.boolOf(e)
		case "partitioned":
			p.Partitioned = c.boolOf(e)
		case "primaryKeyPosition":
			p.PKPos = c.intOf(e)
		case "partitionKeyPosition":
			p.PartPos = c.intOf(e)
		case "logicalTypeOptions":
			if e.Value.Kind == yaml.MappingNode {
				p.Options = map[string]*yaml.Node{}
				for _, o := range scan.Entries(e.Value) {
					p.Options[o.Key] = o.Value
				}
			} else {
				c.problem(e.Line, "type-options-not-mapping", "logicalTypeOptions must be a mapping")
			}
		case "enum":
			p.HasEnum = true
			for _, v := range scan.Items(e.Value) {
				if v.Kind == yaml.MappingNode {
					p.Enum = append(p.Enum, scan.Scalar(scan.Get(v, "value")))
				} else {
					p.Enum = append(p.Enum, scan.Scalar(v))
				}
			}
		case "examples":
			p.Examples, p.ExamplesLine = scan.Items(e.Value), e.Line
		case "quality":
			p.Quality = c.parseQuality(e)
		case "properties":
			p.Props = c.parseProps(e, "property "+odcsLabel(p.ID, p.Name))
		case "items":
			p.Items = c.parseProp(e.Value, "property "+odcsLabel(p.ID, p.Name))
			if p.Items != nil && p.Items.Name == "" {
				p.Items.Name = "items"
			}
		}
	}
	return p
}

func (c *odcsContract) boolOf(e scan.KeyValue) bool {
	var b bool
	if e.Value.Kind != yaml.ScalarNode || e.Value.Decode(&b) != nil {
		c.problem(e.Line, "not-boolean", "%s must be true or false", e.Key)
		return false
	}
	return b
}

func (c *odcsContract) intOf(e scan.KeyValue) int {
	n, err := strconv.Atoi(scan.Scalar(e.Value))
	if err != nil {
		c.problem(e.Line, "not-integer", "%s must be an integer", e.Key)
		return -1
	}
	return n
}

func (c *odcsContract) parseQuality(kv scan.KeyValue) []*odcsQuality {
	if kv.Value.Kind != yaml.SequenceNode {
		c.problem(kv.Line, "quality-not-list", "quality must be a list")
		return nil
	}
	var out []*odcsQuality
	for _, it := range scan.Items(kv.Value) {
		if it.Kind != yaml.MappingNode {
			c.problem(it.Line, "quality-not-mapping", "a quality rule must be a mapping")
			continue
		}
		q := &odcsQuality{Line: it.Line, Ops: map[string]*yaml.Node{}}
		for _, e := range scan.Entries(it) {
			switch e.Key {
			case "id":
				q.ID = scan.Scalar(e.Value)
			case "metric":
				q.Metric = scan.Scalar(e.Value)
			case "rule": // deprecated in v3.1 for metric; still read
				if q.Metric == "" {
					q.Metric = scan.Scalar(e.Value)
				}
			case "name":
				q.Name = scan.Scalar(e.Value)
			case "type":
				q.Type = scan.Scalar(e.Value)
			case "arguments":
				q.Args = map[string]*yaml.Node{}
				for _, a := range scan.Entries(e.Value) {
					q.Args[a.Key] = a.Value
				}
			default:
				if odcsContains(odcsQualityOps, e.Key) {
					q.Ops[e.Key] = e.Value
				}
			}
		}
		out = append(out, q)
	}
	return out
}

func (c *odcsContract) parseSLA(kv scan.KeyValue) []*odcsSLA {
	if kv.Value.Kind != yaml.SequenceNode {
		c.problem(kv.Line, "sla-not-list", "slaProperties must be a list")
		return nil
	}
	var out []*odcsSLA
	for _, it := range scan.Items(kv.Value) {
		s := &odcsSLA{Line: it.Line,
			Property: scan.Scalar(scan.Get(it, "property")),
			Element:  scan.Scalar(scan.Get(it, "element")),
			Unit:     scan.Scalar(scan.Get(it, "unit")),
			Value:    scan.Scalar(scan.Get(it, "value")),
		}
		if s.Property == "" {
			c.problem(it.Line, "sla-property-missing", "an slaProperties entry needs property")
		}
		out = append(out, s)
	}
	return out
}

func (c *odcsContract) parseServers(kv scan.KeyValue) []*odcsServer {
	if kv.Value.Kind != yaml.SequenceNode {
		c.problem(kv.Line, "servers-not-list", "servers must be a list")
		return nil
	}
	var out []*odcsServer
	for _, it := range scan.Items(kv.Value) {
		s := &odcsServer{Line: it.Line, Fields: map[string]string{}, Server: scan.Scalar(scan.Get(it, "server"))}
		for _, f := range odcsServerAddress {
			if v := scan.Get(it, f); v != nil {
				s.Fields[f] = scan.Scalar(v)
			}
		}
		if s.Server == "" || s.Fields["type"] == "" {
			c.problem(it.Line, "server-incomplete", "a server needs server and type")
		}
		out = append(out, s)
	}
	return out
}

// odcsCheckUnique reports two entries of one list sharing an id or a name: the
// change detection matches by them.
func odcsCheckUnique[T any](c *odcsContract, xs []T, key func(T) (string, string, int), what string) {
	ids, names := map[string]int{}, map[string]int{}
	for _, x := range xs {
		id, name, line := key(x)
		if id != "" {
			if l, dup := ids[id]; dup {
				c.problem(line, "duplicate-id", "%s id %q repeats line %d", what, id, l)
			}
			ids[id] = line
		}
		if name != "" {
			if l, dup := names[name]; dup {
				c.problem(line, "duplicate-name", "%s name %q repeats line %d", what, name, l)
			}
			names[name] = line
		}
	}
}

// odcsDBTModel reads customProperties: [{property: dbtModel, value: <model>}].
func odcsDBTModel(n *yaml.Node) (string, int) {
	for _, it := range scan.Items(n) {
		if scan.Scalar(scan.Get(it, "property")) == "dbtModel" {
			v := scan.Get(it, "value")
			return scan.Scalar(v), scan.Line(v)
		}
	}
	return "", 0
}

// semver parses MAJOR.MINOR.PATCH (a leading v allowed).
func odcsSemver(v string) (major, minor, patch int, ok bool) {
	m := odcsSemverRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	patch, _ = strconv.Atoi(m[3])
	return major, minor, patch, true
}

// isV3 reports whether apiVersion is an ODCS v3 version.
func odcsIsV3(api string) bool { return odcsAPIV3Re.MatchString(api) }

// isContractDoc is the content test of §3.2: apiVersion v3.* and kind
// DataContract.
func (c *odcsContract) isContractDoc() bool {
	return c.Kind == "DataContract" && odcsIsV3(c.APIVersion)
}

// propName is the column name a property maps to: physicalName, else name.
func (p *odcsProp) column() string {
	if p.PhysicalName != "" {
		return p.PhysicalName
	}
	return p.Name
}

// table is the physical table an object maps to.
func (o *odcsObject) table() string {
	if o.PhysicalName != "" {
		return o.PhysicalName
	}
	return o.Name
}

// line returns source line n (1-based), "" when out of range.
func (c *odcsContract) line(n int) string {
	if n < 1 || n > len(c.Lines) {
		return ""
	}
	return c.Lines[n-1]
}

// lineOf is the line of a property field, or the property's own line.
func (p *odcsProp) lineOf(field string) int {
	if l := p.lines[field]; l > 0 {
		return l
	}
	return p.Line
}

func odcsContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
