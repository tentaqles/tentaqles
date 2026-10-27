package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_V1_DefaultsIdentitiesFromCloud(t *testing.T) {
	p := write(t, `
schema: tentaqles-client-v1
client: acme
git: { email: a@acme.com, user: acme-bot, provider: github }
cloud: { provider: azure }
`)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Client != "acme" || m.Git.Email != "a@acme.com" {
		t.Fatalf("bad parse: %+v", m)
	}
	if got, want := m.IdentityNames(), []string{"az", "claude", "gh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("identities %v want %v", got, want)
	}
}

func TestLoad_V2_ExplicitIdentitiesAndPermission(t *testing.T) {
	p := write(t, `
schema: tentaqles-client-v2
client: globex
git: { name: Maria, email: m@globex.io }
identities:
  claude: { share_capabilities: false }
  aws: { profile: globex }
claude: { permission_mode: bypass }
`)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.IdentityNames(); !reflect.DeepEqual(got, []string{"aws", "claude"}) {
		t.Fatalf("identities %v", got)
	}
	if m.Claude.PermissionMode != "bypass" || m.Identities["aws"].Profile != "globex" {
		t.Fatalf("%+v", m)
	}
	if sc := m.Identities["claude"].ShareCapabilities; sc == nil || *sc {
		t.Fatal("share_capabilities false not parsed")
	}
}

func TestLoad_UnknownSchema_Rejected(t *testing.T) {
	_, err := Load(write(t, "schema: tentaqles-client-v9\nclient: x\n"))
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("want ErrUnsupportedSchema, got %v", err)
	}
}

func TestLoad_MissingSchema_Rejected(t *testing.T) {
	_, err := Load(write(t, "client: x\n"))
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("got %v", err)
	}
}

func TestLoad_MissingClient_Rejected(t *testing.T) {
	_, err := Load(write(t, "schema: tentaqles-client-v2\n"))
	if err == nil {
		t.Fatal("expected error for missing client")
	}
}

func TestLoad_InvalidPermissionMode_Rejected(t *testing.T) {
	_, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nclaude: { permission_mode: yolo }\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_SecretLikeValue_Rejected(t *testing.T) {
	for _, v := range []string{"ghp_abcdefghijklmnopqrstuvwxyz0123456789", "sk-ant-abcdef", "AKIAIOSFODNN7EXAMPLE", "Bearer xyz"} {
		_, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\ngit: { name: \""+v+"\" }\n"))
		if !errors.Is(err, ErrSecretLike) {
			t.Fatalf("%q: want ErrSecretLike, got %v", v, err)
		}
	}
}

func TestLoad_UnknownIdentity_Rejected(t *testing.T) {
	_, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nidentities: { fortran: {} }\n"))
	if err == nil {
		t.Fatal("expected error for unknown identity provider")
	}
}

func TestLoad_ClaudeBundle(t *testing.T) {
	m, err := Load(write(t, `
schema: tentaqles-client-v2
client: uplabs
claude:
  permission_mode: acceptEdits
  bundle:
    marketplaces: [claude-plugins-official, aws]
    plugins: [superpowers, aws-core@aws]
    skills: [snowflake-snowpark-patterns]
    mcp: [github, snowflake]
`))
	if err != nil {
		t.Fatal(err)
	}
	b := m.Claude.Bundle
	if b == nil || len(b.Plugins) != 2 || b.Plugins[1] != "aws-core@aws" || b.MCP[1] != "snowflake" {
		t.Fatalf("%+v", b)
	}
	m2, _ := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"))
	if m2.Claude.Bundle != nil {
		t.Fatal("bundle must be nil when absent")
	}
}

// cloud.also adds its clouds to the implicit identities, so each gets a
// private config home next to the primary cloud's.
func TestIdentityNamesCloudAlso(t *testing.T) {
	m := &Manifest{Cloud: map[string]any{"provider": "azure", "also": []any{"aws", "AWS"}}}
	got := strings.Join(m.IdentityNames(), ",")
	if got != "aws,az,claude,gh" {
		t.Fatalf("IdentityNames = %s, want aws,az,claude,gh", got)
	}
	m = &Manifest{Cloud: map[string]any{"provider": "azure"}}
	if got := strings.Join(m.IdentityNames(), ","); got != "az,claude,gh" {
		t.Fatalf("without also: %s", got)
	}
}

func TestCloudProviderName(t *testing.T) {
	for in, want := range map[string]string{
		"Azure": "azure", " az ": "azure", "aws": "aws", "GCloud": "gcp", "gcp": "gcp",
		"do": "digitalocean", "doctl": "digitalocean", "digitalocean": "digitalocean",
		"none": "none", "": "", "Hetzner": "hetzner",
	} {
		if got := CloudProviderName(in); got != want {
			t.Errorf("CloudProviderName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIdentityNamesCloudAliasesAndDigitalOcean(t *testing.T) {
	names := func(c map[string]any) string { return strings.Join((&Manifest{Cloud: c}).IdentityNames(), ",") }
	if got := names(map[string]any{"provider": "azure", "also": []any{"gcloud"}}); got != "az,claude,gcloud,gh" {
		t.Fatalf("also gcloud: %s", got)
	}
	if got := names(map[string]any{"provider": "gcloud"}); got != "claude,gcloud,gh" {
		t.Fatalf("provider gcloud: %s", got)
	}
	if got := names(map[string]any{"provider": "aws", "also": []any{"digitalocean"}}); got != "aws,claude,doctl,gh" {
		t.Fatalf("also digitalocean: %s", got)
	}
	if got := names(map[string]any{"provider": "digitalocean"}); got != "claude,doctl,gh" {
		t.Fatalf("provider digitalocean: %s", got)
	}
}

func TestLoad_CloudAlsoValidation(t *testing.T) {
	load := func(cloud string) error {
		_, err := Load(write(t, "schema: tentaqles-client-v1\nclient: acme\ncloud:\n"+cloud))
		return err
	}
	for name, cloud := range map[string]string{
		"scalar":        "  provider: azure\n  also: aws\n",
		"unknown":       "  provider: azure\n  also: [hetzner]\n",
		"same primary":  "  provider: azure\n  also: [az]\n",
		"non-string":    "  provider: azure\n  also: [1]\n",
		"alias primary": "  provider: gcloud\n  also: [gcp]\n",
	} {
		err := load(cloud)
		if err == nil || !strings.Contains(err.Error(), "cloud.also") {
			t.Errorf("%s: want a cloud.also load error, got %v", name, err)
		}
	}
	for name, cloud := range map[string]string{
		"list":    "  provider: azure\n  also: [aws, gcloud, doctl]\n",
		"none":    "  provider: azure\n",
		"no prov": "  also: [aws]\n",
	} {
		if err := load(cloud); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
