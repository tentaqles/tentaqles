package manifest

import "fmt"

// BrowserNames are the browsers a client's profile may be in.
var BrowserNames = []string{"chrome", "edge"}

// Browser is one browser profile of a client.
type Browser struct {
	Name    string `yaml:"name"`
	Profile string `yaml:"profile"`
}

// ValidateBrowsers checks the browsers block: known browsers, a profile each, no browser twice.
func (m *Manifest) ValidateBrowsers() error {
	seen := map[string]bool{}
	for _, b := range m.Browsers {
		if !contains(BrowserNames, b.Name) {
			return fmt.Errorf("browsers: unknown browser %q (known: chrome, edge)", b.Name)
		}
		if b.Profile == "" {
			return fmt.Errorf("browsers: %s needs a profile", b.Name)
		}
		if seen[b.Name] {
			return fmt.Errorf("browsers: %s is listed twice", b.Name)
		}
		seen[b.Name] = true
	}
	return nil
}
