// Package iam is the cloud pack's IAM least-privilege lint (spec D §8):
// IAM001-IAM007 over AWS identity, trust and resource policies, AZR001-AZR004
// over Azure role assignments and custom roles, and GCP001 over Google IAM
// grants in Terraform plans. It is pure Go over the cloud model: no file
// system, process or network. The cloud/iam check (register_iam.go) feeds it.
package iam

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/secrets"
)

// Options is what the lint knows beyond the resources.
type Options struct {
	// Accounts are the AWS accounts that are "ours" (packs.cloud.accounts).
	// Empty: the cross-account part of IAM006 is not judged.
	Accounts []string
	// Tenants are the Entra tenants that are "ours" (packs.cloud.tenants).
	// Empty: AZR004 is not judged.
	Tenants []string
	// Scope is an ARM template's deployment scope, from its $schema:
	// ScopeResourceGroup (the default), ScopeSubscription,
	// ScopeManagementGroup or ScopeTenant. Role assignments without a
	// scope property are judged at it.
	Scope Level
}

// Result is the lint of a set of resources or one file.
type Result struct {
	Findings []packs.Finding
	// Suppressed counts tq-allow suppressions that applied, by rule id.
	Suppressed map[string]int
	// Documents is the number of policy documents, role assignments, role
	// definitions and grants judged.
	Documents int
}

func (r *Result) merge(o Result) {
	r.Findings = append(r.Findings, o.Findings...)
	r.Documents += o.Documents
	for k, v := range o.Suppressed {
		if r.Suppressed == nil {
			r.Suppressed = map[string]int{}
		}
		r.Suppressed[k] += v
	}
}

// RuleSup001 is the finding for a tq-allow entry without a reason (spec D
// §5: a suppression without a reason is itself an error). The db pack uses
// the same id for its SQL comments.
const RuleSup001 = "SUP001"

// Resources lints every resource that is created or changed. Deleted
// resources are not judged (their policies go away), and no-op resources of
// a plan are skipped (they were judged when they were introduced).
//
// A Terraform aws_iam_policy_document data source that the plan defers to
// apply time (mode data, action read) is linted from its statement blocks,
// since the rendered JSON is not in the plan yet.
func Resources(rs []model.Resource, o Options) Result {
	var res Result
	nested := deploymentScopes(rs, o.Scope)
	for _, r := range rs {
		if policyDocumentRead(r) {
			res.merge(policyDocument(r, o))
			continue
		}
		if !r.Changes() || r.After == nil {
			continue
		}
		ro := o
		if lvl, ok := nested[r.Meta["parent"]]; ok && r.Source == model.KindARM && r.Meta["parent"] != "" {
			ro.Scope = lvl
		}
		res.merge(resource(r, ro))
	}
	sortFindings(res.Findings)
	return res
}

// deploymentScopes maps each ARM nested deployment (by Addr) to the scope
// its inner resources deploy at: the inner template's $schema, else its
// resourceGroup, subscriptionId or scope property, else the scope of the
// template that holds it. Parents come before their children in rs.
func deploymentScopes(rs []model.Resource, outer Level) map[string]Level {
	out := map[string]Level{}
	for _, r := range rs {
		if r.Source != model.KindARM || !strings.EqualFold(r.Type, "Microsoft.Resources/deployments") || r.After == nil {
			continue
		}
		lvl := outer
		if p, ok := out[r.Meta["parent"]]; ok && r.Meta["parent"] != "" {
			lvl = p
		}
		schema, _ := model.Get(r.After, "properties", "template", "$schema").(string)
		switch {
		case schema != "":
			lvl = TemplateScope(schema)
		case first(r.After, "resourceGroup") != "":
			lvl = ScopeResourceGroup
		case first(r.After, "subscriptionId") != "":
			lvl = ScopeSubscription
		case ScopeOf(first(r.After, "scope")) >= ScopeManagementGroup:
			lvl = ScopeOf(first(r.After, "scope"))
		}
		out[r.Addr] = lvl
	}
	return out
}

// hit is a rule hit on one resource, before suppression.
type hit struct {
	Rule, Level, Msg string
}

func resource(r model.Resource, o Options) Result {
	var hits []hit
	docs := 0
	t := strings.ToLower(r.Type)
	attrs := r.After
	if r.Source == model.KindARM || r.Source == model.KindWhatIf {
		attrs = model.Map(r.After["properties"])
		if attrs == nil {
			attrs = map[string]any{}
		}
	}
	if specs, ok := awsDocs[t]; ok {
		for _, sp := range specs {
			found := sp.collect(attrs)
			if len(found) == 0 && sp.required && !sp.arns {
				// Absent from a create or update: unknown at plan time
				// (after_unknown), or not shown by the tool.
				hits = append(hits, hit{"IAM000", packs.LevelWarn, sp.name() + " is not known in this input (unknown at plan time); its IAM rules were not judged"})
			}
			for _, d := range found {
				if sp.arns {
					docs++
					// Strings, and CloudFormation/CDK intrinsics (Fn::Sub,
					// Fn::Join with Ref AWS::Partition).
					for _, x := range model.List(d.value) {
						if s := intrinsicString(x); s != "" && adminManagedPolicyRe.MatchString(s) {
							hits = append(hits, hit{"IAM004", packs.LevelError, d.label + "attaches the AWS managed policy " + policyName(s) + "; grant a scoped policy instead"})
						}
					}
					continue
				}
				iss, ok := lintDoc(d.value, sp.kind, o)
				if !ok {
					if present(d.value) {
						hits = append(hits, hit{"IAM000", packs.LevelWarn, d.label + "the policy document is not resolvable here (unknown at plan time, redacted or an expression); its IAM rules were not judged"})
					}
					continue
				}
				docs++
				for _, i := range iss {
					hits = append(hits, hit{i.Rule, i.Level, d.label + i.Msg})
				}
			}
		}
	}
	switch {
	case azureAssignment[t]:
		docs++
		hits = append(hits, azureAssignmentRules(r, attrs, o)...)
	case azureRoleDef[t]:
		docs++
		hits = append(hits, azureRoleDefRules(attrs)...)
	case azureLighthouse[t]:
		docs++
		hits = append(hits, azureLighthouseRules(r, attrs, o)...)
	}
	if r.Source == model.KindTFPlan && gcpIAMRe.MatchString(t) {
		docs++
		hits = append(hits, gcpRules(t, attrs)...)
	}
	return suppress(r, hits, docs)
}

// suppress applies the resource's tq-allow entries: a tag (model's
// Suppressions) or, for Azure resources that take no tags, a "tq-allow"
// marker in the description.
func suppress(r model.Resource, hits []hit, docs int) Result {
	res := Result{Documents: docs}
	if len(hits) == 0 {
		return res
	}
	sups := append(r.Suppressions(), descriptionSuppressions(r)...)
	addr := secrets.Redact(r.Addr)
	reported := map[string]bool{}
	for _, h := range hits {
		var sup *model.Suppression
		for i := range sups {
			if sups[i].Rule == h.Rule {
				sup = &sups[i]
				break
			}
		}
		if sup != nil && sup.Valid() {
			if res.Suppressed == nil {
				res.Suppressed = map[string]int{}
			}
			res.Suppressed[h.Rule]++
			continue
		}
		res.Findings = append(res.Findings, packs.Finding{File: r.Pos.File, Line: r.Pos.Line, Level: h.Level, Rule: h.Rule, Message: addr + ": " + h.Msg})
		if sup != nil && !reported[h.Rule] {
			reported[h.Rule] = true
			res.Findings = append(res.Findings, packs.Finding{File: r.Pos.File, Line: r.Pos.Line, Level: packs.LevelError, Rule: RuleSup001,
				Message: addr + ": tq-allow " + h.Rule + " has no reason; write \"" + h.Rule + ": <reason>\""})
		}
	}
	return res
}

// descriptionSuppressions reads "tq-allow AZR001: <reason>" from a
// description. Azure role assignments and definitions take no tags, so
// this is their in-source suppression.
func descriptionSuppressions(r model.Resource) []model.Suppression {
	var texts []string
	for _, v := range []any{r.After["description"], model.Get(r.After, "properties", "description")} {
		if s, ok := v.(string); ok {
			texts = append(texts, s)
		}
	}
	var out []model.Suppression
	for _, s := range texts {
		i := strings.Index(strings.ToLower(s), model.AllowTag)
		if i < 0 {
			continue
		}
		rest := strings.TrimLeft(s[i+len(model.AllowTag):], " :=")
		out = append(out, model.ParseSuppressions(rest)...)
	}
	return out
}

// docSpec says where a resource type keeps a policy document (or a list of
// managed policy ARNs, arns true). path is a key path; a "[]" segment walks
// every element of a list.
type docSpec struct {
	path []string
	kind docKind
	arns bool
	// required: the resource always carries this document, so its absence
	// from a create or update means it is unknown here.
	required bool
}

func (s docSpec) name() string { return strings.Join(s.path, ".") }

func present(v any) bool {
	if v == nil {
		return false
	}
	if str, ok := v.(string); ok {
		return strings.TrimSpace(str) != ""
	}
	return true
}

type foundDoc struct {
	label string // "" for the main document, else "inline_policy[1]: "
	value any
}

func (s docSpec) collect(attrs map[string]any) []foundDoc {
	var out []foundDoc
	var walk func(v any, i int, label string)
	walk = func(v any, i int, label string) {
		if v == nil {
			return
		}
		if i == len(s.path) {
			out = append(out, foundDoc{label, v})
			return
		}
		seg := s.path[i]
		if seg == "[]" {
			for n, x := range model.List(v) {
				walk(x, i+1, fmt.Sprintf("%s[%d]", label, n))
			}
			return
		}
		m := model.Map(v)
		if m == nil {
			return
		}
		next := label
		if i+1 < len(s.path) && s.path[i+1] == "[]" {
			next = seg
		}
		walk(m[seg], i+1, next)
	}
	walk(attrs, 0, "")
	for i := range out {
		if out[i].label != "" {
			out[i].label += ": "
		}
	}
	return out
}

func p(path string, kind docKind) docSpec {
	return docSpec{path: strings.Split(path, "."), kind: kind}
}

// req is p for a document the resource always has.
func req(path string, kind docKind) docSpec {
	d := p(path, kind)
	d.required = true
	return d
}

func arns(path string) docSpec { return docSpec{path: strings.Split(path, "."), arns: true} }

// awsDocs maps a lowercased resource type to where its policies live:
// Terraform (aws provider), CloudFormation and CDK, and Pulumi (aws classic).
var awsDocs = map[string][]docSpec{
	// Terraform
	"aws_iam_policy":                            {req("policy", docIdentity)},
	"aws_iam_role_policy":                       {req("policy", docIdentity)},
	"aws_iam_user_policy":                       {req("policy", docIdentity)},
	"aws_iam_group_policy":                      {req("policy", docIdentity)},
	"aws_iam_role":                              {req("assume_role_policy", docTrust), p("inline_policy.[].policy", docIdentity), arns("managed_policy_arns")},
	"aws_iam_role_policy_attachment":            {arns("policy_arn")},
	"aws_iam_user_policy_attachment":            {arns("policy_arn")},
	"aws_iam_group_policy_attachment":           {arns("policy_arn")},
	"aws_iam_policy_attachment":                 {arns("policy_arn")},
	"aws_iam_role_policy_attachments_exclusive": {arns("policy_arns")},
	"aws_s3_bucket_policy":                      {req("policy", docResource)},
	"aws_s3_bucket":                             {p("policy", docResource)},
	"aws_sqs_queue_policy":                      {req("policy", docResource)},
	"aws_sqs_queue":                             {p("policy", docResource)},
	"aws_sns_topic_policy":                      {req("policy", docResource)},
	"aws_sns_topic":                             {p("policy", docResource)},
	"aws_kms_key":                               {p("policy", docKeyPolicy)},
	"aws_kms_key_policy":                        {req("policy", docKeyPolicy)},
	"aws_ecr_repository_policy":                 {req("policy", docResource)},
	"aws_secretsmanager_secret_policy":          {req("policy", docResource)},
	"aws_secretsmanager_secret":                 {p("policy", docResource)},
	// CloudFormation (After is Properties)
	"aws::iam::policy":                    {req("PolicyDocument", docIdentity)},
	"aws::iam::managedpolicy":             {req("PolicyDocument", docIdentity)},
	"aws::iam::rolepolicy":                {req("PolicyDocument", docIdentity)},
	"aws::iam::userpolicy":                {req("PolicyDocument", docIdentity)},
	"aws::iam::grouppolicy":               {req("PolicyDocument", docIdentity)},
	"aws::iam::role":                      {req("AssumeRolePolicyDocument", docTrust), p("Policies.[].PolicyDocument", docIdentity), arns("ManagedPolicyArns")},
	"aws::iam::user":                      {p("Policies.[].PolicyDocument", docIdentity), arns("ManagedPolicyArns")},
	"aws::iam::group":                     {p("Policies.[].PolicyDocument", docIdentity), arns("ManagedPolicyArns")},
	"aws::s3::bucketpolicy":               {req("PolicyDocument", docResource)},
	"aws::sqs::queuepolicy":               {req("PolicyDocument", docResource)},
	"aws::sns::topicpolicy":               {req("PolicyDocument", docResource)},
	"aws::kms::key":                       {p("KeyPolicy", docKeyPolicy)},
	"aws::secretsmanager::resourcepolicy": {req("ResourcePolicy", docResource)},
	"aws::ecr::repository":                {p("RepositoryPolicyText", docResource)},
	// Pulumi (aws classic; After is newState.inputs)
	"aws:iam/policy:policy":                               {req("policy", docIdentity)},
	"aws:iam/rolepolicy:rolepolicy":                       {req("policy", docIdentity)},
	"aws:iam/userpolicy:userpolicy":                       {req("policy", docIdentity)},
	"aws:iam/grouppolicy:grouppolicy":                     {req("policy", docIdentity)},
	"aws:iam/role:role":                                   {req("assumeRolePolicy", docTrust), p("inlinePolicies.[].policy", docIdentity), arns("managedPolicyArns")},
	"aws:iam/rolepolicyattachment:rolepolicyattachment":   {arns("policyArn")},
	"aws:iam/userpolicyattachment:userpolicyattachment":   {arns("policyArn")},
	"aws:iam/grouppolicyattachment:grouppolicyattachment": {arns("policyArn")},
	"aws:iam/policyattachment:policyattachment":           {arns("policyArn")},
	"aws:s3/bucketpolicy:bucketpolicy":                    {req("policy", docResource)},
	"aws:sqs/queuepolicy:queuepolicy":                     {req("policy", docResource)},
	"aws:sns/topicpolicy:topicpolicy":                     {req("policy", docResource)},
	"aws:kms/key:key":                                     {p("policy", docKeyPolicy)},
}

// intrinsicString renders a string or a CloudFormation intrinsic as text:
// a Ref becomes a placeholder (AWS::Partition "aws", AWS::URLSuffix
// "amazonaws.com", anything else "${Name}" with "::" as "."), a Fn::Sub
// template gets the same substitutions, and a Fn::Join joins its rendered
// parts (CDK writes ARNs as Fn::Join of "arn:", Ref AWS::Partition, ...).
// A part that cannot be rendered (Fn::GetAtt, Fn::If) becomes "${?}".
// It returns "" for anything else. Placeholders hold no ":" so the ARN
// regexps still split the fields, and never match "*" or an account id.
func intrinsicString(v any) string { return renderIntrinsic(v, 0) }

const unknownPart = "${?}"

func renderIntrinsic(v any, depth int) string {
	if s, ok := model.Str(v); ok {
		return s
	}
	m := model.Map(v)
	if len(m) != 1 || depth > 8 {
		return ""
	}
	if ref, ok := m["Ref"].(string); ok {
		return pseudoRef(ref)
	}
	if sub, ok := m["Fn::Sub"]; ok {
		tmpl, _ := sub.(string)
		if l := model.List(sub); tmpl == "" && len(l) > 0 {
			tmpl, _ = l[0].(string)
		}
		return subVarRe.ReplaceAllStringFunc(tmpl, func(x string) string { return pseudoRef(x[2 : len(x)-1]) })
	}
	if j, ok := m["Fn::Join"]; ok {
		l := model.List(j)
		if len(l) != 2 {
			return ""
		}
		sep, _ := l[0].(string)
		var parts []string
		for _, x := range model.List(l[1]) {
			s := renderIntrinsic(x, depth+1)
			if s == "" {
				s = unknownPart
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, sep)
	}
	for k := range m {
		if strings.HasPrefix(k, "Fn::") {
			return unknownPart
		}
	}
	return ""
}

// subVarRe matches a Fn::Sub variable; "${!Literal}" is an escape and stays.
var subVarRe = regexp.MustCompile(`\$\{[^!}][^}]*\}`)

func pseudoRef(name string) string {
	switch name {
	case "AWS::Partition":
		return "aws"
	case "AWS::URLSuffix":
		return "amazonaws.com"
	}
	return "${" + strings.ReplaceAll(name, ":", ".") + "}"
}

// unresolved reports a rendered value that still holds a placeholder.
func unresolved(s string) bool { return s == "" || strings.Contains(s, "${") }

// intrinsicStrings renders each element of List(v), skipping the ones that
// render to "".
func intrinsicStrings(v any) []string {
	var out []string
	for _, x := range model.List(v) {
		if s := intrinsicString(x); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// intrinsicMap: a one-key map whose key is a CloudFormation function
// (Fn::If, Fn::GetAtt, Ref): an expression, not data.
func intrinsicMap(m map[string]any) bool {
	if len(m) != 1 {
		return false
	}
	for k := range m {
		return k == "Ref" || strings.HasPrefix(k, "Fn::")
	}
	return false
}

func policyName(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

func sortFindings(fs []packs.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
}
