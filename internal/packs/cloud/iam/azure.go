package iam

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// Level is how wide an Azure scope is.
type Level int

const (
	ScopeUnknown         Level = iota // not resolvable (an expression, unknown at plan time)
	ScopeResource                     // below a resource group
	ScopeResourceGroup                // a whole resource group
	ScopeSubscription                 // a whole subscription
	ScopeManagementGroup              // a management group
	ScopeTenant                       // "/" or the tenant root
)

func (l Level) String() string {
	switch l {
	case ScopeResource:
		return "resource"
	case ScopeResourceGroup:
		return "resource group"
	case ScopeSubscription:
		return "subscription"
	case ScopeManagementGroup:
		return "management group"
	case ScopeTenant:
		return "tenant root"
	}
	return "unknown"
}

// TemplateScope reads an ARM template's deployment scope from its $schema
// (deploymentTemplate, subscriptionDeploymentTemplate,
// managementGroupDeploymentTemplate, tenantDeploymentTemplate).
func TemplateScope(schema string) Level {
	s := strings.ToLower(schema)
	switch {
	case strings.Contains(s, "subscriptiondeploymenttemplate"):
		return ScopeSubscription
	case strings.Contains(s, "managementgroupdeploymenttemplate"):
		return ScopeManagementGroup
	case strings.Contains(s, "tenantdeploymenttemplate"):
		return ScopeTenant
	}
	return ScopeResourceGroup
}

var (
	subRe = regexp.MustCompile(`(?i)^/subscriptions/[^/]+/?$`)
	rgRe  = regexp.MustCompile(`(?i)^/subscriptions/[^/]+/resourcegroups/[^/]+/?$`)
	mgRe  = regexp.MustCompile(`(?i)^(/providers/microsoft\.management/managementgroups/[^/]+)/?$`)
)

// ScopeOf classifies a scope: an id, an ARM expression for one, or "".
func ScopeOf(scope string) Level {
	s := strings.TrimSpace(scope)
	switch {
	case s == "":
		return ScopeUnknown
	case s == "/":
		return ScopeTenant
	case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
		e := strings.ToLower(strings.ReplaceAll(s[1:len(s)-1], " ", ""))
		switch e {
		case "subscription().id":
			return ScopeSubscription
		case "resourcegroup().id":
			return ScopeResourceGroup
		case "managementgroup().id":
			return ScopeManagementGroup
		case "tenant().id", "tenantresourceid()":
			return ScopeTenant
		}
		return ScopeUnknown
	case subRe.MatchString(s):
		return ScopeSubscription
	case rgRe.MatchString(s):
		return ScopeResourceGroup
	case mgRe.MatchString(s):
		return ScopeManagementGroup
	case strings.HasPrefix(strings.ToLower(s), "/subscriptions/"), strings.HasPrefix(strings.ToLower(s), "/providers/"):
		return ScopeResource
	}
	return ScopeUnknown
}

var (
	azureAssignment = map[string]bool{
		"azurerm_role_assignment":                   true,
		"microsoft.authorization/roleassignments":   true,
		"azure-native:authorization:roleassignment": true,
		"azure:authorization/assignment:assignment": true,
	}
	azureRoleDef = map[string]bool{
		"azurerm_role_definition":                           true,
		"microsoft.authorization/roledefinitions":           true,
		"azure-native:authorization:roledefinition":         true,
		"azure:authorization/roledefinition:roledefinition": true,
	}
	azureLighthouse = map[string]bool{
		"azurerm_lighthouse_definition":                       true,
		"microsoft.managedservices/registrationdefinitions":   true,
		"azure-native:managedservices:registrationdefinition": true,
	}
)

// first returns the first string attribute among keys.
func first(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" && !model.IsRedacted(s) {
			return s
		}
	}
	return ""
}

// assignmentScope finds a role assignment's scope: the scope attribute
// (Terraform, Pulumi, an ARM extension resource's "scope"), else the part
// of a what-if resource id before /providers/Microsoft.Authorization, else
// the template's deployment scope for ARM.
func assignmentScope(r model.Resource, attrs map[string]any, o Options) (Level, string) {
	if s := first(attrs, "scope"); s != "" {
		return ScopeOf(s), ""
	}
	if s := first(r.After, "scope"); s != "" {
		return ScopeOf(s), ""
	}
	if r.Source == model.KindWhatIf {
		low := strings.ToLower(r.Addr)
		if i := strings.Index(low, "/providers/microsoft.authorization/roleassignments/"); i >= 0 {
			if i == 0 {
				return ScopeTenant, ""
			}
			return ScopeOf(r.Addr[:i]), ""
		}
	}
	if r.Source == model.KindARM {
		if o.Scope == ScopeUnknown {
			return ScopeResourceGroup, " (the template's deployment scope)"
		}
		return o.Scope, " (the template's deployment scope)"
	}
	return ScopeUnknown, ""
}

func azureAssignmentRules(r model.Resource, attrs map[string]any, o Options) []hit {
	var hits []hit
	role := first(attrs, "role_definition_name", "roleDefinitionName", "role_definition_id", "roleDefinitionId")
	if br, ok := privilegedRole(role); ok {
		if h, ok := privilegedAt(br, r, attrs, o); ok {
			hits = append(hits, h)
		}
	}
	hits = append(hits, tenantRule(attrs, o)...)
	return hits
}

func privilegedAt(br azureRole, r model.Resource, attrs map[string]any, o Options) (hit, bool) {
	lvl, note := assignmentScope(r, attrs, o)
	switch lvl {
	case ScopeTenant, ScopeManagementGroup, ScopeSubscription:
		return hit{"AZR001", packs.LevelError, fmt.Sprintf("assigns %s at %s scope%s; assign a narrower role at a resource group or resource", br.Name, lvl, note)}, true
	case ScopeResourceGroup:
		return hit{"AZR001", packs.LevelWarn, fmt.Sprintf("assigns %s at resource group scope%s; prefer a narrower role", br.Name, note)}, true
	case ScopeUnknown:
		return hit{"AZR001", packs.LevelWarn, fmt.Sprintf("assigns %s at a scope that is not known here; at subscription scope or wider this fails", br.Name)}, true
	}
	return hit{}, false
}

var providerWildRe = regexp.MustCompile(`^[^/*]+/\*$`)

func azureRoleDefRules(attrs map[string]any) []hit {
	var actions, notActions []string
	perms := attrs["permissions"]
	for _, pm := range model.List(perms) {
		m := model.Map(pm)
		actions = append(actions, model.Strings(m["actions"])...)
		notActions = append(notActions, model.Strings(first2(m, "not_actions", "notActions"))...)
	}
	scopes := model.Strings(first2(attrs, "assignable_scopes", "assignableScopes"))
	if s := first(attrs, "scope"); s != "" && len(scopes) == 0 {
		scopes = []string{s}
	}
	var hits []hit
	var wild []string
	for _, a := range actions {
		a = strings.TrimSpace(a)
		if a == "*" || providerWildRe.MatchString(a) {
			wild = append(wild, a)
		}
	}
	if len(wild) > 0 {
		hits = append(hits, hit{"AZR002", packs.LevelError, "custom role with actions " + quoteList(wild) + "; list the operations it needs"})
	} else {
		var writes []string
		for _, a := range actions {
			if !azureReadOnly(a) {
				writes = append(writes, a)
			}
		}
		wide := ""
		for _, s := range scopes {
			if l := ScopeOf(s); l >= ScopeSubscription {
				wide = l.String()
				break
			}
		}
		if wide != "" && len(writes) > 0 {
			hits = append(hits, hit{"AZR002", packs.LevelError, "custom role assignable at " + wide + " scope grants write actions; limit assignableScopes to resource groups"})
		}
	}
	var grants []string
	for _, target := range azureAuthzWrites {
		if grantsAction(actions, notActions, target) {
			grants = append(grants, target)
		}
	}
	if len(grants) > 0 {
		hits = append(hits, hit{"AZR003", packs.LevelError, "custom role grants " + quoteList(grants) + ": its holders can grant themselves any role"})
	}
	return hits
}

func first2(m map[string]any, a, b string) any {
	if v, ok := m[a]; ok {
		return v
	}
	return m[b]
}

// azureReadOnly: an operation whose last segment is "read" ("*/read",
// "Microsoft.Compute/virtualMachines/read").
func azureReadOnly(a string) bool {
	a = strings.TrimSpace(a)
	i := strings.LastIndex(a, "/")
	return i >= 0 && strings.EqualFold(a[i+1:], "read")
}

func grantsAction(actions, notActions []string, target string) bool {
	granted := false
	for _, a := range actions {
		if wildMatch(strings.TrimSpace(a), target) {
			granted = true
			break
		}
	}
	if !granted {
		return false
	}
	for _, n := range notActions {
		if wildMatch(strings.TrimSpace(n), target) {
			return false
		}
	}
	return true
}

func azureLighthouseRules(r model.Resource, attrs map[string]any, o Options) []hit {
	hits := tenantRule(attrs, o)
	for _, a := range model.List(attrs["authorization"]) {
		hits = append(hits, lighthouseAuth(r, attrs, model.Map(a), o)...)
	}
	for _, a := range model.List(attrs["authorizations"]) {
		hits = append(hits, lighthouseAuth(r, attrs, model.Map(a), o)...)
	}
	return hits
}

func lighthouseAuth(r model.Resource, attrs, a map[string]any, o Options) []hit {
	br, ok := privilegedRole(first(a, "role_definition_id", "roleDefinitionId"))
	if !ok {
		return nil
	}
	// A Lighthouse delegation projects the role onto the delegated
	// subscription unless its scope says otherwise.
	lvl := ScopeSubscription
	if s := first(attrs, "scope"); s != "" {
		lvl = ScopeOf(s)
	}
	switch lvl {
	case ScopeResource:
		return nil
	case ScopeResourceGroup:
		return []hit{{"AZR001", packs.LevelWarn, "delegates " + br.Name + " to another tenant at resource group scope; prefer a narrower role"}}
	}
	return []hit{{"AZR001", packs.LevelError, "delegates " + br.Name + " to another tenant at " + lvl.String() + " scope; delegate a narrower role"}}
}

// tenantRule is AZR004: a tenant the input shows that is not ours.
func tenantRule(attrs map[string]any, o Options) []hit {
	if len(o.Tenants) == 0 {
		return nil
	}
	for _, k := range []string{"managing_tenant_id", "managedByTenantId", "tenant_id", "tenantId", "principal_tenant_id", "principalTenantId"} {
		t := first(attrs, k)
		if t == "" || !guidRe.MatchString(t) || strings.HasPrefix(t, "[") {
			continue
		}
		if !contains(o.Tenants, t) {
			return []hit{{"AZR004", packs.LevelWarn, "grants access to a principal in a tenant outside packs.cloud.tenants (cross-tenant)"}}
		}
	}
	return nil
}
