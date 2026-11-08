package manifest

import (
	"reflect"
	"strings"
	"testing"
)

// TestScope: the provider scope is derived from what an existing manifest already declares
// (the identities block, the cloud block, the git provider,
// the email provider) plus the optional scope: list;
// sorted, each id once.
func TestScope(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"cloud", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {} }\ncloud: { provider: azure }\n", []string{"az", "claude"}},
		{"identities and cloud.also", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {}, az: {} }\ncloud: { provider: azure, also: [aws] }\n", []string{"aws", "az", "claude"}},
		{"git provider", "schema: tentaqles-client-v2\nclient: x\ngit: { provider: azure-devops }\nidentities: { az: {}, claude: {} }\n", []string{"az", "azure-devops", "claude"}},
		{"git provider gitlab", "schema: tentaqles-client-v2\nclient: x\ngit: { provider: gitlab }\nidentities: { claude: {} }\n", []string{"claude", "glab"}},
		{"git provider github", "schema: tentaqles-client-v2\nclient: x\ngit: { provider: github }\nidentities: { claude: {}, gh: {} }\n", []string{"claude", "gh"}},
		{"email provider", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {} }\nemail: { provider: gmail }\n", []string{"claude", "gmail"}},
		{"explicit scope", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {} }\nscope: [vercel, terraform, claude]\n", []string{"claude", "terraform", "vercel"}},
	}
	for _, c := range cases {
		m, err := Load(write(t, c.body))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := m.Scope(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Scope() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestScopeRejectsUnknownIDs(t *testing.T) {
	for _, body := range []string{"scope: [fortran]\n", "scope: [aws, aws]\n"} {
		if _, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"+body)); err == nil || !strings.Contains(err.Error(), "scope") {
			t.Errorf("%q loaded: %v", body, err)
		}
	}
}

// TestDeclaredCloudIsInScope: a client's own cloud is in its scope and isolated, also when the
// identities block leaves it out.
func TestDeclaredCloudIsInScope(t *testing.T) {
	m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {} }\ncloud:\n  provider: azure\n  also: [gcloud]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.IdentityNames(), []string{"az", "claude", "gcloud"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IdentityNames() = %v, want %v", got, want)
	}
	if got, want := m.Scope(), []string{"az", "claude", "gcloud"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Scope() = %v, want %v", got, want)
	}
}
