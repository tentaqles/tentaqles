// Package iam is the cloud pack's IAM least-privilege lint (spec D §8):
// IAM001-IAM007 over AWS identity, trust and resource policies, AZR001-AZR004
// over Azure role assignments and custom roles, and GCP001 over Google IAM
// grants in Terraform plans. It is pure Go over the cloud model: no file
// system, process or network. The cloud/iam check (register_iam.go) feeds it.
package iam

import (
	"fmt"
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
func Resources(rs []model.Resource, o Options) Result {
	var res Result
	for _, r := range rs {
		if !r.Changes() || r.After == nil {
			continue
		}
		res.merge(resource(r, o))
	}
	sortFindings(res.Findings)
	return res
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
			for _, d := range sp.collect(attrs) {
				if sp.arns {
					docs++
					for _, a := range model.Strings(d.value) {
						if adminManagedPolicyRe.MatchString(intrinsicString(a)) {
							hits = append(hits, hit{"IAM004", packs.LevelError, d.label + "attaches the AWS managed policy " + policyName(a) + "; grant a scoped policy instead"})
						}
					}
					// A CloudFormation Fn::Sub ARN is a map, not a string.
					for _, x := range model.List(d.value) {
						if s := intrinsicString(x); s != "" && model.Map(x) != nil && adminManagedPolicyRe.MatchString(s) {
							hits = append(hits, hit{"IAM004", packs.LevelError, d.label + "attaches the AWS managed policy " + policyName(s) + "; grant a scoped policy instead"})
						}
					}
					continue
				}
				iss, ok := lintDoc(d.value, sp.kind, o)
				if !ok {
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

func arns(path string) docSpec { return docSpec{path: strings.Split(path, "."), arns: true} }

// awsDocs maps a lowercased resource type to where its policies live:
// Terraform (aws provider), CloudFormation and CDK, and Pulumi (aws classic).
var awsDocs = map[string][]docSpec{
	// Terraform
	"aws_iam_policy":                            {p("policy", docIdentity)},
	"aws_iam_role_policy":                       {p("policy", docIdentity)},
	"aws_iam_user_policy":                       {p("policy", docIdentity)},
	"aws_iam_group_policy":                      {p("policy", docIdentity)},
	"aws_iam_role":                              {p("assume_role_policy", docTrust), p("inline_policy.[].policy", docIdentity), arns("managed_policy_arns")},
	"aws_iam_role_policy_attachment":            {arns("policy_arn")},
	"aws_iam_user_policy_attachment":            {arns("policy_arn")},
	"aws_iam_group_policy_attachment":           {arns("policy_arn")},
	"aws_iam_policy_attachment":                 {arns("policy_arn")},
	"aws_iam_role_policy_attachments_exclusive": {arns("policy_arns")},
	"aws_s3_bucket_policy":                      {p("policy", docResource)},
	"aws_s3_bucket":                             {p("policy", docResource)},
	"aws_sqs_queue_policy":                      {p("policy", docResource)},
	"aws_sqs_queue":                             {p("policy", docResource)},
	"aws_sns_topic_policy":                      {p("policy", docResource)},
	"aws_sns_topic":                             {p("policy", docResource)},
	"aws_kms_key":                               {p("policy", docResource)},
	"aws_kms_key_policy":                        {p("policy", docResource)},
	"aws_ecr_repository_policy":                 {p("policy", docResource)},
	"aws_secretsmanager_secret_policy":          {p("policy", docResource)},
	"aws_secretsmanager_secret":                 {p("policy", docResource)},
	// CloudFormation (After is Properties)
	"aws::iam::policy":                    {p("PolicyDocument", docIdentity)},
	"aws::iam::managedpolicy":             {p("PolicyDocument", docIdentity)},
	"aws::iam::rolepolicy":                {p("PolicyDocument", docIdentity)},
	"aws::iam::userpolicy":                {p("PolicyDocument", docIdentity)},
	"aws::iam::grouppolicy":               {p("PolicyDocument", docIdentity)},
	"aws::iam::role":                      {p("AssumeRolePolicyDocument", docTrust), p("Policies.[].PolicyDocument", docIdentity), arns("ManagedPolicyArns")},
	"aws::iam::user":                      {p("Policies.[].PolicyDocument", docIdentity), arns("ManagedPolicyArns")},
	"aws::iam::group":                     {p("Policies.[].PolicyDocument", docIdentity), arns("ManagedPolicyArns")},
	"aws::s3::bucketpolicy":               {p("PolicyDocument", docResource)},
	"aws::sqs::queuepolicy":               {p("PolicyDocument", docResource)},
	"aws::sns::topicpolicy":               {p("PolicyDocument", docResource)},
	"aws::kms::key":                       {p("KeyPolicy", docResource)},
	"aws::secretsmanager::resourcepolicy": {p("ResourcePolicy", docResource)},
	"aws::ecr::repository":                {p("RepositoryPolicyText", docResource)},
	// Pulumi (aws classic; After is newState.inputs)
	"aws:iam/policy:policy":                               {p("policy", docIdentity)},
	"aws:iam/rolepolicy:rolepolicy":                       {p("policy", docIdentity)},
	"aws:iam/userpolicy:userpolicy":                       {p("policy", docIdentity)},
	"aws:iam/grouppolicy:grouppolicy":                     {p("policy", docIdentity)},
	"aws:iam/role:role":                                   {p("assumeRolePolicy", docTrust), p("inlinePolicies.[].policy", docIdentity), arns("managedPolicyArns")},
	"aws:iam/rolepolicyattachment:rolepolicyattachment":   {arns("policyArn")},
	"aws:iam/userpolicyattachment:userpolicyattachment":   {arns("policyArn")},
	"aws:iam/grouppolicyattachment:grouppolicyattachment": {arns("policyArn")},
	"aws:iam/policyattachment:policyattachment":           {arns("policyArn")},
	"aws:s3/bucketpolicy:bucketpolicy":                    {p("policy", docResource)},
	"aws:sqs/queuepolicy:queuepolicy":                     {p("policy", docResource)},
	"aws:sns/topicpolicy:topicpolicy":                     {p("policy", docResource)},
	"aws:kms/key:key":                                     {p("policy", docResource)},
}

// intrinsicString returns a string, the template of a CloudFormation
// Fn::Sub, or a Fn::Join of strings; "" otherwise.
func intrinsicString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	m := model.Map(v)
	if sub, ok := m["Fn::Sub"]; ok {
		if s, ok := sub.(string); ok {
			return s
		}
		if l := model.List(sub); len(l) > 0 {
			s, _ := l[0].(string)
			return s
		}
	}
	if j, ok := m["Fn::Join"]; ok {
		l := model.List(j)
		if len(l) == 2 {
			sep, _ := l[0].(string)
			var parts []string
			for _, x := range model.List(l[1]) {
				s, ok := x.(string)
				if !ok {
					return ""
				}
				parts = append(parts, s)
			}
			return strings.Join(parts, sep)
		}
	}
	return ""
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
