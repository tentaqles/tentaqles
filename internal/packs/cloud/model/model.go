// Package model is the cloud pack's shared resource model (spec D §6).
//
// The readers in internal/packs/cloud/readers turn every IaC input (a
// Terraform/OpenTofu plan, an ARM template, a what-if result, a
// CloudFormation or CDK template, a change set, a Pulumi preview) into one
// normalised []Resource, so that the iac-policy rules, the IAM lint and the
// apply gate are written once. Every value a reader puts into Before/After
// has already been through the redaction in redact.go: a Resource never
// carries a sensitive value.
package model

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Kind names an input format, and so the reader that produced a Resource.
type Kind string

const (
	KindTFPlan    Kind = "tfplan"    // terraform/tofu show -json <plan>
	KindARM       Kind = "arm"       // an ARM template (also bicep build output)
	KindWhatIf    Kind = "whatif"    // az deployment ... what-if -o json --no-pretty-print
	KindCFN       Kind = "cfn"       // a CloudFormation template (JSON or YAML) or cdk.out/*.template.json
	KindChangeSet Kind = "changeset" // aws cloudformation describe-change-set -o json
	KindPulumi    Kind = "pulumi"    // pulumi preview --json
)

// Kinds lists every kind, in a stable order.
func Kinds() []Kind {
	return []Kind{KindTFPlan, KindARM, KindWhatIf, KindCFN, KindChangeSet, KindPulumi}
}

// Normalised actions. A reader maps its tool's own vocabulary onto these.
const (
	ActCreate  = "create"
	ActUpdate  = "update"
	ActDelete  = "delete"
	ActReplace = "replace" // delete and create (either order), or a CFN Modify with Replacement True/Conditional
	ActNoOp    = "no-op"
	ActRead    = "read"   // data sources, Pulumi reads and refreshes
	ActImport  = "import" // adopted into state without a create
	ActForget  = "forget" // removed from state, the real object is kept (Terraform removed blocks)
	ActUnknown = "unknown"
)

// Pos is where a resource sits in its input. File is the name the caller
// passed to the reader. Line is 1-based (0 when unknown); for single-line
// plan JSON every resource is on line 1 and Pointer (an RFC 6901 JSON
// pointer, "" for YAML) tells them apart.
type Pos struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Pointer string `json:"pointer,omitempty"`
}

// String is "file:line", with "#pointer" appended when the input is a
// single-line document (every Line is 1) or the line is unknown.
func (p Pos) String() string {
	s := p.File
	if p.Line > 0 {
		s += ":" + strconv.Itoa(p.Line)
	}
	if p.Pointer != "" && p.Line <= 1 {
		s += "#" + p.Pointer
	}
	return s
}

// Resource is one resource of one input, normalised (spec D §6).
type Resource struct {
	// Addr is the Terraform address, the ARM resource id (what-if) or name
	// (template), the CloudFormation logical id, or the Pulumi URN.
	Addr string `json:"addr"`
	// Type is the provider type: aws_iam_policy,
	// Microsoft.Authorization/roleAssignments, AWS::IAM::Role,
	// aws:iam/policy:Policy.
	Type string `json:"type"`
	// Actions are the normalised Act* values for plans, what-if results,
	// change sets and previews; nil for plain templates.
	Actions []string `json:"actions,omitempty"`
	// After holds the planned attributes, already redacted:
	//   tfplan:    change.after
	//   arm:       the whole resource object (type, name, apiVersion,
	//              location, properties, sku, tags, ...), without its child
	//              "resources" (they are separate Resources)
	//   whatif:    changes[].after (the full resource payload)
	//   cfn:       Properties (short-form YAML tags in long form: !Ref X is
	//              {"Ref": X}, !Sub s is {"Fn::Sub": s})
	//   changeset: AfterContext.Properties (needs --include-property-values)
	//   pulumi:    newState.inputs
	// It is nil for a delete, or when the tool gave no payload.
	After map[string]any `json:"after,omitempty"`
	// Before is the prior state where the tool reports one (tfplan
	// change.before, whatif before, changeset BeforeContext, pulumi
	// oldState.inputs), redacted like After.
	Before map[string]any `json:"before,omitempty"`
	// Env is the resolved environment, "" when reading source.
	Env string `json:"env,omitempty"`
	// Pos is the resource's position in the input.
	Pos Pos `json:"pos"`
	// Source is the reader that produced this resource.
	Source Kind `json:"source"`
	// Meta holds reader-specific facts outside the attributes. Keys in use:
	//   all:       raw_action (the tool's own action words, comma-joined)
	//   tfplan:    mode (managed|data), provider, module, deposed, action_reason
	//   arm:       parent (the parent's Addr for nested and child resources),
	//              symbol (languageVersion 2.0 symbolic name), copy (copy loop name),
	//              deployment_mode (on Microsoft.Resources/deployments: Incremental|Complete)
	//   whatif:    potential ("true" for properties.potentialChanges), unsupported_reason
	//   cfn:       deletion_policy, update_replace_policy, condition, cdk_path
	//   changeset: replacement (True|False|Conditional), policy_action, physical_id
	//   pulumi:    provider, protect ("true")
	Meta map[string]string `json:"meta,omitempty"`
}

// Has reports whether the resource's actions include a.
func (r Resource) Has(a string) bool {
	for _, x := range r.Actions {
		if x == a {
			return true
		}
	}
	return false
}

// Destroys reports a delete or a replace: the existing object goes away.
func (r Resource) Destroys() bool { return r.Has(ActDelete) || r.Has(ActReplace) }

// Planned reports whether the resource came from a plan-like input (it has
// actions), as opposed to a plain template.
func (r Resource) Planned() bool { return r.Actions != nil }

// Changes reports whether the resource is created, changed or destroyed.
// No-op, read, forget and import-only are not changes.
func (r Resource) Changes() bool {
	if !r.Planned() {
		return true // a template declares the resource
	}
	for _, a := range r.Actions {
		switch a {
		case ActCreate, ActUpdate, ActDelete, ActReplace, ActUnknown:
			return true
		}
	}
	return false
}

// Attrs is After, or Before when After is nil (a delete): the attributes the
// rules should look at to know what the resource is.
func (r Resource) Attrs() map[string]any {
	if r.After != nil {
		return r.After
	}
	return r.Before
}

// Primary is the action counted in a summary: the most severe one.
func (r Resource) Primary() string {
	if r.Actions == nil {
		return ""
	}
	best, rank := ActNoOp, -1
	for _, a := range r.Actions {
		if k := actionRank(a); k > rank {
			best, rank = a, k
		}
	}
	return best
}

func actionRank(a string) int {
	switch a {
	case ActReplace:
		return 8
	case ActDelete:
		return 7
	case ActUnknown:
		return 6
	case ActUpdate:
		return 5
	case ActCreate:
		return 4
	case ActImport:
		return 3
	case ActForget:
		return 2
	case ActRead:
		return 1
	}
	return 0
}

// Ref names a resource in a summary: address and type only.
type Ref struct {
	Addr string `json:"addr"`
	Type string `json:"type"`
}

// Summary is what an agent or a hub decision may see of a plan: counts and
// addresses only, never attribute values (spec D §20).
type Summary struct {
	Total            int            `json:"total"`
	Counts           map[string]int `json:"counts"`
	StatefulDeletes  []Ref          `json:"stateful_deletes,omitempty"`
	StatefulReplaces []Ref          `json:"stateful_replaces,omitempty"`
}

// Summarize counts the resources per primary action and lists the stateful
// deletes and replaces. Template resources (no actions) count as "declared".
// Addresses pass through RedactString.
func Summarize(rs []Resource) Summary {
	s := Summary{Counts: map[string]int{}}
	for _, r := range rs {
		s.Total++
		a := r.Primary()
		if a == "" {
			a = "declared"
		}
		s.Counts[a]++
		if !Stateful(r.Type) {
			continue
		}
		ref := Ref{Addr: RedactString(r.Addr), Type: r.Type}
		switch {
		case r.Has(ActReplace):
			s.StatefulReplaces = append(s.StatefulReplaces, ref)
		case r.Has(ActDelete):
			s.StatefulDeletes = append(s.StatefulDeletes, ref)
		}
	}
	return s
}

// HasStatefulDestroy reports a stateful delete or replace in the summary.
func (s Summary) HasStatefulDestroy() bool {
	return len(s.StatefulDeletes) > 0 || len(s.StatefulReplaces) > 0
}

// Text is a one-line rendering: "3 resources: 1 create, 1 delete, 1 update;
// stateful delete: aws_db_instance.main".
func (s Summary) Text() string {
	keys := make([]string, 0, len(s.Counts))
	for k := range s.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", s.Counts[k], k))
	}
	out := fmt.Sprintf("%d resources", s.Total)
	if len(parts) > 0 {
		out += ": " + strings.Join(parts, ", ")
	}
	if len(s.StatefulDeletes) > 0 {
		out += "; stateful delete: " + joinRefs(s.StatefulDeletes)
	}
	if len(s.StatefulReplaces) > 0 {
		out += "; stateful replace: " + joinRefs(s.StatefulReplaces)
	}
	return out
}

func joinRefs(rs []Ref) string {
	a := make([]string, len(rs))
	for i, r := range rs {
		a[i] = r.Addr
	}
	return strings.Join(a, ", ")
}

// SetEnv sets Env on every resource and returns the slice.
func SetEnv(rs []Resource, env string) []Resource {
	for i := range rs {
		rs[i].Env = env
	}
	return rs
}

// ---- value helpers: the readers keep JSON numbers as json.Number and YAML
// scalars as their decoded Go types; CloudFormation often writes numbers and
// booleans as strings. These helpers read all of those forms.

// Get walks maps (by key) and lists (by decimal index) from v. It returns
// nil when any step is missing or of the wrong kind.
func Get(v any, path ...string) any {
	for _, p := range path {
		switch t := v.(type) {
		case map[string]any:
			v = t[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

// Map returns v as a map, or nil.
func Map(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// List returns v as a list. A single non-nil non-list value becomes a
// one-element list (IAM Action and Resource may be a string or a list).
func List(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	default:
		return []any{t}
	}
}

// Str returns v as a string: strings as they are, numbers and booleans in
// their usual text form. ok is false for nil, maps and lists.
func Str(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		return strconv.FormatBool(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case uint64:
		return strconv.FormatUint(t, 10), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}
	return "", false
}

// Strings returns the string forms of List(v), skipping non-scalars.
func Strings(v any) []string {
	var out []string
	for _, x := range List(v) {
		if s, ok := Str(x); ok {
			out = append(out, s)
		}
	}
	return out
}

// Int returns v as an integer: numbers, and strings holding an integer.
func Int(v any) (int64, bool) {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i, true
		}
		if f, err := t.Float64(); err == nil && f == math.Trunc(f) {
			return int64(f), true
		}
	case int:
		return int64(t), true
	case int64:
		return t, true
	case uint64:
		if t <= math.MaxInt64 {
			return int64(t), true
		}
	case float64:
		if t == math.Trunc(t) {
			return int64(t), true
		}
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return i, true
		}
	}
	return 0, false
}

// Bool returns v as a boolean: booleans, and the strings "true"/"false"
// (any case). ok is false otherwise.
func Bool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// IsRedacted reports whether v is a redaction marker string.
func IsRedacted(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "[REDACTED:")
}
