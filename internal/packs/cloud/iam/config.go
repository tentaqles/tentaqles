package iam

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

var (
	accountRe = regexp.MustCompile(`^[0-9]{12}$`)
	tenantRe  = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ValidateAccounts checks packs.cloud.accounts: a list of 12-digit AWS
// account ids (YAML may read them as integers; the text is what counts).
func ValidateAccounts(n yaml.Node) error {
	return validateList(n, "accounts", accountRe, "a 12-digit AWS account id")
}

// ValidateTenants checks packs.cloud.tenants: a list of Entra tenant GUIDs.
func ValidateTenants(n yaml.Node) error { return validateList(n, "tenants", tenantRe, "a tenant GUID") }

func validateList(n yaml.Node, key string, re *regexp.Regexp, what string) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("packs.cloud.%s (line %d): must be a list", key, n.Line)
	}
	seen := map[string]bool{}
	for i, c := range n.Content {
		v := strings.ToLower(strings.TrimSpace(c.Value))
		if c.Kind != yaml.ScalarNode || !re.MatchString(v) {
			return fmt.Errorf("packs.cloud.%s[%d] (line %d): must be %s", key, i, c.Line, what)
		}
		if seen[v] {
			return fmt.Errorf("packs.cloud.%s[%d] (line %d): duplicate", key, i, c.Line)
		}
		seen[v] = true
	}
	return nil
}

// Accounts and Tenants read the merged settings.
func Accounts(s packs.Settings) []string { return s.Strings("accounts") }
func Tenants(s packs.Settings) []string  { return s.Strings("tenants") }

// IaCPaths returns the cleaned packs.cloud.iac[].path values. The iac key
// is D-CP's (it validates it); this only reads the paths.
func IaCPaths(s packs.Settings) []string {
	n := s.Node("iac")
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, item := range n.Content {
		p, found := "", false
		switch item.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(item.Content); i += 2 {
				if item.Content[i].Value == "path" && item.Content[i+1].Kind == yaml.ScalarNode {
					p, found = item.Content[i+1].Value, true
				}
			}
		case yaml.ScalarNode:
			p, found = item.Value, true
		}
		if !found || strings.TrimSpace(p) == "" {
			continue
		}
		p = strings.Trim(path.Clean(strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")), "/")
		if p == "." {
			p = "" // the repository root
		} else if p == "" || p == ".." || strings.HasPrefix(p, "../") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Under reports whether a slash path is inside one of the IaC paths.
func Under(p string, paths []string) bool {
	for _, d := range paths {
		if d == "" || p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

// Candidate reports a file the check reads: JSON or YAML (templates,
// plans, policies), including cdk.out *.template.json.
func Candidate(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".json", ".yaml", ".yml", ".template":
		return true
	}
	return false
}

// IAMNamed reports a path whose base name says it holds IAM: *iam*,
// *role*, *policy* (the iam triage flag, spec D §15).
func IAMNamed(p string) bool {
	b := strings.ToLower(path.Base(p))
	return strings.Contains(b, "iam") || strings.Contains(b, "role") || strings.Contains(b, "polic")
}

// iamDirs are directory names that hold IAM policy JSON.
var iamDirs = map[string]bool{"iam": true, "policies": true, "policy": true, "roles": true, "role": true, "iam-policies": true, "iam_policies": true}

// IAMPolicyPath reports a JSON file in a directory named for IAM
// (iam/, policies/, roles/ ...): the "IAM policy JSON" of the iam flag
// (spec D §15), told by its path since flags read paths and config only
// (spec C §2.9). The content-based finding comes from cloud/iam.
func IAMPolicyPath(p string) bool {
	if strings.ToLower(path.Ext(p)) != ".json" {
		return false
	}
	dir := path.Dir(p)
	for dir != "." && dir != "/" && dir != "" {
		if iamDirs[strings.ToLower(path.Base(dir))] {
			return true
		}
		dir = path.Dir(dir)
	}
	return false
}
