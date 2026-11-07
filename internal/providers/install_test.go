package providers

import (
	"strings"
	"testing"
)

// The install block takes the six command-line package managers and nothing else: a method
// outside them (an installer, a script runner) is an unknown key and the file does not load.
func TestInstallMethods_OnlyTheSix(t *testing.T) {
	file := func(method string) []byte {
		return []byte("id: widget\nname: Widget\ncategory: other\ndocs: https://example.com\ncli:\n  command: widget\ninstall:\n  linux:\n    " + method + ": \"widget\"\nmatch:\n  executables: [widget]\n")
	}
	for _, m := range []string{"winget", "scoop", "brew", "apt", "npm", "pip"} {
		if _, err := parseProvider(file(m), "widget", "test"); err != nil {
			t.Errorf("method %s refused: %v", m, err)
		}
	}
	for _, m := range []string{"choco", "msi", "curl", "sh", "exe", "snap", "dnf"} {
		if _, err := parseProvider(file(m), "widget", "test"); err == nil {
			t.Errorf("method %s accepted, want an unknown key error", m)
		}
	}
	// a shell metacharacter in a runnable method is refused
	bad := strings.Replace(string(file("npm")), `"widget"`, `"widget & calc"`, 1)
	if _, err := parseProvider([]byte(bad), "widget", "test"); err == nil {
		t.Error("an install value with & accepted")
	}
}

// Every runnable package id of the embedded catalog is a plain package name: nothing a package
// manager could read as an option or a second argument.
func TestCatalogInstallIDsArePlain(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range c.All() {
		for osName, o := range map[string]InstallOS{"windows": p.Install.Windows, "macos": p.Install.Macos, "linux": p.Install.Linux} {
			for method, id := range map[string]string{"winget": o.Winget, "scoop": o.Scoop, "brew": o.Brew, "apt": o.Apt, "npm": o.Npm, "pip": o.Pip} {
				if id == "" {
					continue
				}
				if strings.HasPrefix(id, "-") || strings.ContainsAny(id, " \t") {
					t.Errorf("%s install.%s.%s = %q is not a plain package id", p.ID, osName, method, id)
				}
			}
		}
	}
}

// The winget id of terraform is the one `winget show --id <id> -e` finds (checked on the owner's
// winget). A wrong id is "No package found", and the install could never end in "installed": a
// change to this id is a change to this test as well.
func TestCatalog_PinnedWingetIDs(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"terraform": "Hashicorp.Terraform"} {
		p, ok := c.Get(id)
		if !ok {
			t.Fatalf("%s missing from the catalog", id)
		}
		if p.Install.Windows.Winget != want {
			t.Errorf("%s install.windows.winget = %q, want %q", id, p.Install.Windows.Winget, want)
		}
	}
}

// Install methods that can never end in "installed" stay out of the catalog: Homebrew's libpq is
// keg-only (psql is never linked onto the PATH), the postgres winget package is a server installer
// that puts no psql on the PATH, and the supabase npm package refuses global installs. A provider
// with no method prints its docs page instead.
func TestCatalog_NoInstallMethodThatCannotEnd(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := c.Get("postgres")
	if pg.Install.Macos.Brew != "" {
		t.Errorf("postgres: brew %q is keg-only, psql is not linked", pg.Install.Macos.Brew)
	}
	if pg.Install.Windows.Winget != "" {
		t.Errorf("postgres: winget %q installs the server, and no psql is put on the PATH", pg.Install.Windows.Winget)
	}
	sb, _ := c.Get("supabase")
	for osName, o := range map[string]InstallOS{"windows": sb.Install.Windows, "macos": sb.Install.Macos, "linux": sb.Install.Linux} {
		if o.Npm != "" {
			t.Errorf("supabase install.%s.npm %q: the npm package refuses global installs", osName, o.Npm)
		}
	}
}
