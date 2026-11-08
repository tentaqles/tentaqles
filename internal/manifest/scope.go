package manifest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// The provider scope: the catalog ids an identity may touch. It is the
// single source of truth the guard's identity category, the PATH, the environment and the
// harness's MCP list are built from.

// gitProviderIDs maps git.provider to its catalog id.
var gitProviderIDs = map[string]string{"github": "gh", "gitlab": "glab", "azure-devops": "azure-devops", "bitbucket": "bitbucket", "gitea": "gitea"}

// Scope returns the sorted catalog ids this identity may touch: IdentityNames (the
// identities block, or claude and gh for a v1 manifest, plus every cloud of the cloud
// block), the git provider, the email provider, and the optional `scope:` list (providers the
// identity uses without a private config home, such as vercel or terraform). An existing
// manifest needs no new field: its scope is what it already declares.
func (m *Manifest) Scope() []string {
	seen := map[string]bool{}
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && id != "none" {
			seen[id] = true
		}
	}
	for _, id := range m.IdentityNames() {
		add(id)
	}
	gp := strings.ToLower(strings.TrimSpace(m.Git.Provider))
	if id, ok := gitProviderIDs[gp]; ok {
		add(id)
	} else if _, ok := providers.MustLoad().Get(gp); ok {
		add(gp)
	}
	if m.Email.Enabled() {
		add(m.Email.Provider)
	}
	for _, id := range m.ExtraScope {
		add(id)
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// unknownProviderHint is what an unknown-provider error adds when a user provider file named
// for the id exists but does not load: the file, its first error and the
// fix, so the developer is not told the id is unknown when it is the file that is broken.
func unknownProviderHint(id string) string {
	if fe := providers.UserFileError(id); fe != nil {
		return ": the user provider file " + fe.Error()
	}
	return ""
}

// validateScope checks the scope: list: catalog ids, each once.
func (m *Manifest) validateScope() error {
	seen := map[string]bool{}
	for _, id := range m.ExtraScope {
		if _, ok := providers.MustLoad().Get(id); !ok {
			return fmt.Errorf("scope: unknown provider %q (a catalog id: tq providers list)%s", id, unknownProviderHint(id))
		}
		if seen[id] {
			return fmt.Errorf("scope: %q is listed twice", id)
		}
		seen[id] = true
	}
	return nil
}
