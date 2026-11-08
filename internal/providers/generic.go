package providers

import "strings"

// Two catalog rules for what a client's terminal hides of a provider outside its scope.

// GenericTool reports a tool that talks to whatever the project configures (docker, helm,
// kubectl, terraform, pulumi, npm, psql, dbt, cursor's agent): its CLI command is not in its
// own match lists. Its variables (NPM_TOKEN for a private registry, TF_TOKEN_* for Terraform
// Cloud) belong to the project, not to a vendor account: the identity environment never unsets
// them.
func (p Provider) GenericTool() bool {
	if p.CLI == nil || strings.TrimSpace(p.CLI.Command) == "" {
		return false
	}
	for _, e := range p.Match.Executables {
		if strings.EqualFold(e, p.CLI.Command) {
			return false
		}
	}
	for exe := range p.Match.Subcommands { // a claim on part of the command (azure-devops: az devops)
		if strings.EqualFold(exe, p.CLI.Command) {
			return false
		}
	}
	return true
}

// ambiguous are executable names of the catalog that are also ordinary words or other
// projects' tools (continue's cn, gitea's tea, snowflake's snow, render's render, vercel's vc,
// flyctl's fly, mongodb's atlas, github copilot's copilot, goose the migration tool): the PATH
// layer neither drops nor masks a file of that name, so another program of the same name keeps
// working. The provider's unambiguous names (flyctl, vercel, snowsql) are still masked.
var ambiguous = map[string]bool{"cn": true, "tea": true, "snow": true, "render": true, "vc": true,
	"fly": true, "atlas": true, "copilot": true, "goose": true}

// AmbiguousExecutable reports whether an executable name (case and a Windows suffix ignored)
// is too generic for the PATH layer to hide.
func AmbiguousExecutable(name string) bool {
	n := strings.ToLower(name)
	for _, s := range []string{".exe", ".cmd", ".bat", ".ps1", ".com"} {
		n = strings.TrimSuffix(n, s)
	}
	return ambiguous[n]
}
