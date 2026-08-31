package providers

import (
	"reflect"
	"testing"
)

func TestCatalog_ExpectedIDs(t *testing.T) {
	wantIDs := []string{
		"github", "gitlab", "azure-devops", "bitbucket", "azure", "aws", "gcp",
		"digitalocean", "snowflake", "databricks", "supabase", "postgres",
		"vercel", "kube", "terraform", "npm", "jira", "asana", "linear",
		"claude", "codex", "gemini", "cursor", "opencode",
	}
	// The catalog reuses legacy short ids for a few providers instead of
	// the long-form names above (see task-2 brief): github->gh, azure->az,
	// gcp->gcloud, digitalocean->doctl, gitlab->glab.
	rename := map[string]string{
		"github":       "gh",
		"azure":        "az",
		"gcp":          "gcloud",
		"digitalocean": "doctl",
		"gitlab":       "glab",
	}

	c, err := loadEmbedded()
	if err != nil {
		t.Fatalf("loadEmbedded: %v", err)
	}
	if len(wantIDs) != 24 {
		t.Fatalf("test table has %d ids, want 24", len(wantIDs))
	}
	for _, want := range wantIDs {
		id := want
		if r, ok := rename[want]; ok {
			id = r
		}
		if _, ok := c.byID[id]; !ok {
			t.Errorf("expected provider id %q (for %q) not found", id, want)
		}
	}

	legacy := map[string]map[string]string{
		"claude": {"CLAUDE_CONFIG_DIR": "{dir}"},
		"codex":  {"CODEX_HOME": "{dir}"},
		// GEMINI_CLI_HOME alone moves the displayed account and leaves the
		// OAuth token in a shared keychain entry, so the pair is required.
		"gemini": {"GEMINI_CLI_HOME": "{dir}", "GEMINI_FORCE_FILE_STORAGE": "true"},
		"gh":     {"GH_CONFIG_DIR": "{dir}"},
		"az":     {"AZURE_CONFIG_DIR": "{dir}"},
		"aws": {
			"AWS_CONFIG_FILE":             "{dir}/config",
			"AWS_SHARED_CREDENTIALS_FILE": "{dir}/credentials",
		},
		"gcloud": {"CLOUDSDK_CONFIG": "{dir}"},
		"kube":   {"KUBECONFIG": "{dir}/config"},
		"npm":    {"NPM_CONFIG_USERCONFIG": "{dir}/npmrc"},
	}
	for id, want := range legacy {
		p, ok := c.byID[id]
		if !ok {
			t.Fatalf("legacy provider %q not found", id)
		}
		if !reflect.DeepEqual(p.Identity.Env, want) {
			t.Errorf("provider %q Identity.Env = %v, want %v", id, p.Identity.Env, want)
		}
	}
}
