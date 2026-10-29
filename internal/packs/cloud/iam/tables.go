package iam

import (
	"regexp"
	"strings"
)

// The embedded role and verb tables of spec D §8. One place, so the rules
// and the docs agree.

// readOnlyVerbs are the action-name prefixes IAM002 treats as read-only
// (a warn on Resource "*", not a fail).
var readOnlyVerbs = []string{"Get", "List", "Describe", "Head"}

// sensitiveReads are read-verb actions that hand out data or secrets, so
// IAM002 treats them as writes: on Resource "*" they read every secret.
var sensitiveReads = []string{
	"secretsmanager:GetSecretValue",
	"ssm:GetParameter",
	"ssm:GetParameters",
	"ssm:GetParametersByPath",
	"ssm:GetParameterHistory",
	"kms:GetParametersForImport",
}

// escalationActions are the IAM005 privilege-escalation actions. Patterns
// use the IAM wildcard syntax.
var escalationActions = []string{
	"iam:PassRole",
	"iam:CreatePolicyVersion",
	"iam:SetDefaultPolicyVersion",
	"iam:AttachUserPolicy",
	"iam:AttachRolePolicy",
	"iam:AttachGroupPolicy",
	"iam:PutUserPolicy",
	"iam:PutRolePolicy",
	"iam:PutGroupPolicy",
	"iam:UpdateAssumeRolePolicy",
	"iam:CreateAccessKey",
	"sts:AssumeRole",
}

// adminManagedPolicies are the AWS managed policies IAM004 refuses.
var adminManagedPolicyRe = regexp.MustCompile(`(?i):iam::aws:policy/(AdministratorAccess|PowerUserAccess|IAMFullAccess)$`)

// assumeActions are the trust-policy actions: a statement with a
// Principal and one of these is a trust statement.
var assumeActions = []string{"sts:AssumeRole", "sts:AssumeRoleWithWebIdentity", "sts:AssumeRoleWithSAML", "sts:TagSession", "sts:SetSourceIdentity"}

// githubOIDCHost is the GitHub Actions OIDC issuer host (GHES and
// enterprise-customised issuers append a path to it).
const githubOIDCHost = "token.actions.githubusercontent.com"

// azureRole is one privileged Azure built-in role (AZR001). The GUIDs are
// the role definition ids from Microsoft Learn, "Azure built-in roles",
// Privileged (verified 2026-10-28).
type azureRole struct{ Name, ID string }

var privilegedAzureRoles = []azureRole{
	{"Owner", "8e3af657-a8ff-443c-a75c-2fe8c4bcb635"},
	{"Contributor", "b24988ac-6180-42a0-ab88-20f7382dd24c"},
	{"User Access Administrator", "18d7d88d-d35e-4fb5-a5c3-7773c20a72d9"},
	{"Role Based Access Control Administrator", "f58310d9-a9f6-439a-9e8d-f62e7b41a168"},
}

// azureAuthzWrites are the AZR003 actions: whoever holds them can grant
// themselves any role.
var azureAuthzWrites = []string{
	"Microsoft.Authorization/roleAssignments/write",
	"Microsoft.Authorization/roleDefinitions/write",
}

// gcpPrimitiveRoles and gcpPublicMembers are the GCP001 tables.
var (
	gcpPrimitiveRoles = []string{"roles/owner", "roles/editor"}
	gcpPublicMembers  = []string{"allUsers", "allAuthenticatedUsers"}
)

var guidRe = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// privilegedRole matches a role by name (case-insensitive) or by the last
// GUID in a role definition id ("/subscriptions/../roleDefinitions/<guid>",
// "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions',
// '<guid>')]" or the bare GUID).
func privilegedRole(nameOrID string) (azureRole, bool) {
	s := strings.TrimSpace(nameOrID)
	if s == "" {
		return azureRole{}, false
	}
	for _, r := range privilegedAzureRoles {
		if strings.EqualFold(s, r.Name) {
			return r, true
		}
	}
	ids := guidRe.FindAllString(s, -1)
	if len(ids) == 0 {
		return azureRole{}, false
	}
	last := strings.ToLower(ids[len(ids)-1])
	for _, r := range privilegedAzureRoles {
		if last == r.ID {
			return r, true
		}
	}
	return azureRole{}, false
}

// wildMatch reports whether an IAM/Azure wildcard pattern (* any run,
// ? one character) matches s, without regard to case.
func wildMatch(pattern, s string) bool {
	p, t := strings.ToLower(pattern), strings.ToLower(s)
	// Iterative glob with backtracking on the last star: linear enough for
	// action names, and no regexp built per call.
	pi, ti, star, mark := 0, 0, -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// overlaps reports whether an action pattern from a policy could grant the
// named action: the pattern matches it as a glob.
func overlaps(pattern, action string) bool { return wildMatch(pattern, action) }

// fullWildcard is "*" or "<service>:*" (IAM001).
var serviceWildRe = regexp.MustCompile(`^[A-Za-z0-9-]+:\*$`)

func fullWildcard(action string) bool {
	a := strings.TrimSpace(action)
	return a == "*" || serviceWildRe.MatchString(a)
}

// readOnly reports whether an action (or action pattern) only reads:
// its verb starts with a read-only verb and it cannot grant a sensitive
// read. "s3:Get*" is read-only; "s3:G*", "s3:*" and
// "secretsmanager:GetSecretValue" are not.
func readOnly(action string) bool {
	svc, name, ok := strings.Cut(strings.TrimSpace(action), ":")
	if !ok || svc == "" || name == "" {
		return false
	}
	prefix := name
	if i := strings.IndexAny(name, "*?"); i >= 0 {
		prefix = name[:i]
	}
	ro := false
	for _, v := range readOnlyVerbs {
		if len(prefix) >= len(v) && strings.EqualFold(prefix[:len(v)], v) {
			ro = true
			break
		}
	}
	if !ro {
		return false
	}
	for _, s := range sensitiveReads {
		if overlaps(action, s) {
			return false
		}
	}
	return true
}
