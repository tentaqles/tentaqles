package data

import (
	"context"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// data/synapse-artifacts (spec C §3.4): Azure Synapse git-integration JSON
// (`linkedService/`, `pipeline/`, `sqlscript/`, `notebook/`, ... one JSON
// file per resource: {"name", "properties": {...}}).
const (
	ruleSynapseInline = "synapse-inline-secret"
	ruleSynapsePool   = "synapse-hardcoded-pool"
	ruleSynapseParse  = "synapse-unparsed"
)

// secretKeys are property names that hold a credential (lower case).
var secretKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range strings.Fields("password pwd accountkey serviceprincipalkey serviceprincipalcredential clientsecret sastoken sasuri accesskey secretaccesskey apikey credential") {
		m[k] = true
	}
	return m
}()

var connSecretRe = regexp.MustCompile(`(?i)(?:^|;)\s*(?:password|pwd|accountkey|sharedaccesskey|sharedaccesssignature|sig|client_secret)\s*=\s*[^;\s]`)

func synapseApplies(in *packs.Input) bool { return in.AnyChanged(synapseGlobs...) }

func runSynapseArtifacts(_ context.Context, in *packs.Input) packs.Result {
	var fs findings
	n, skipped := 0, 0
	for _, p := range in.ChangedPaths() {
		if !matchAny(synapseGlobs, p) {
			continue
		}
		b, err := in.ReadNew(p, maxPipelineFile+1)
		if err != nil || len(b) > maxPipelineFile {
			skipped++
			continue
		}
		root, err := parseJSONLines(b)
		if err != nil {
			// Fail visibly: an unparsed file may hold an inline secret.
			fs.addFile(p, 1, packs.LevelWarn, ruleSynapseParse,
				"the file does not parse as JSON; its credentials and pools were not checked")
			continue
		}
		if scan.Get(root, "properties") == nil {
			continue // valid JSON, but not a Synapse resource
		}
		n++
		synapseSecrets(p, root, &fs)
		synapsePools(p, root, &fs)
	}
	return result(n, "Synapse resource", skipped, &fs)
}

// synapseSecrets flags credentials held inline: a secret-named property
// with a literal or SecureString value, or a connection string carrying a
// password or key. Azure Key Vault references and @expressions are fine.
func synapseSecrets(file string, root *yaml.Node, fs *findings) {
	for _, kv := range walkEntries(root) {
		k := strings.ToLower(kv.Key)
		switch {
		case secretKeys[k]:
			if inlineValue(kv.Value, nil) {
				fs.addFile(file, kv.Line, packs.LevelError, ruleSynapseInline,
					"property "+kv.Key+" holds an inline credential; reference an Azure Key Vault secret (type AzureKeyVaultSecret) or use a managed identity")
			}
		case k == "connectionstring":
			if inlineValue(kv.Value, connSecretRe) {
				fs.addFile(file, kv.Line, packs.LevelError, ruleSynapseInline,
					"connectionString holds a password or key inline; keep the secret in Azure Key Vault (type AzureKeyVaultSecret) and leave it out of the string")
			}
		}
	}
}

// inlineValue reports a literal credential: a non-empty scalar (or a
// SecureString value) that is not an @expression, not masked with *, and,
// when want is set, matches it.
func inlineValue(n *yaml.Node, want *regexp.Regexp) bool {
	v := ""
	switch n.Kind {
	case yaml.ScalarNode:
		v = scan.Scalar(n)
	case yaml.MappingNode:
		t := scan.Scalar(scan.Get(n, "type"))
		if t == "AzureKeyVaultSecret" || t == "Expression" {
			return false
		}
		v = scan.Scalar(scan.Get(n, "value"))
	default:
		return false
	}
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "@") || strings.Trim(v, "*") == "" {
		return false
	}
	return want == nil || want.MatchString(v)
}

// synapsePools warns on a dedicated SQL pool named literally: a pipeline's
// sqlPool.referenceName, or a SQL script's currentConnection.poolName
// (the serverless "Built-in" pool is fine).
func synapsePools(file string, root *yaml.Node, fs *findings) {
	for _, kv := range walkEntries(root) {
		switch kv.Key {
		case "sqlPool":
			ref := scan.Get(kv.Value, "referenceName")
			if v := scan.Scalar(ref); v != "" && !strings.HasPrefix(v, "@") {
				fs.addFile(file, scan.Line(ref), packs.LevelWarn, ruleSynapsePool,
					"the dedicated SQL pool is hard-coded; take it from a pipeline parameter so dev and prod use their own pool")
			}
		case "currentConnection":
			pool := scan.Get(kv.Value, "poolName")
			if v := scan.Scalar(pool); v != "" && !strings.EqualFold(v, "Built-in") && !strings.HasPrefix(v, "@") {
				fs.addFile(file, scan.Line(pool), packs.LevelWarn, ruleSynapsePool,
					"the SQL script is bound to a named dedicated pool; parameterise the pool per environment")
			}
		}
	}
}

// walkEntries returns every mapping entry of the tree.
func walkEntries(root *yaml.Node) []scan.KeyValue {
	var out []scan.KeyValue
	scan.Walk(root, func(_ []string, n *yaml.Node) bool {
		out = append(out, scan.Entries(n)...)
		return true
	})
	return out
}
