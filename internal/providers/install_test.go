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
