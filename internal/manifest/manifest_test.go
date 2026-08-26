package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
