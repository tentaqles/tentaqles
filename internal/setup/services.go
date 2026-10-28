package setup

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// hasServices is whether the company names any of the tracker, chat or email blocks.
func (c Company) hasServices() bool { return c.Tracker != nil || c.Chat != nil || c.Email != nil }

// validateServices runs the manifest's own checks on the blocks, so the wizard refuses exactly
// what a hand-written manifest would.
func (c Company) validateServices() error {
	m := manifest.Manifest{Tracker: c.Tracker, Chat: c.Chat, Email: c.Email}
	return m.ValidateServices()
}

// servicesDetail is the one-line summary of the blocks for Preview and the Apply report: names
// only, like the manifest itself.
func (c Company) servicesDetail() string {
	var parts []string
	if t := c.Tracker; t != nil {
		s := "tracker " + t.Provider
		if k := t.ScopeKey(); k != "" {
			s += " (" + k + ")"
		}
		parts = append(parts, s+", auth "+t.AuthOrDefault())
	}
	if c.Chat != nil {
		parts = append(parts, "chat "+c.Chat.Provider)
	}
	if c.Email != nil {
		parts = append(parts, "email "+c.Email.Provider)
	}
	return strings.Join(parts, ", ")
}

// servicesChange is the "services" change for a workspace Apply creates.
func (c Company) servicesChange() (Change, bool) {
	if !c.hasServices() {
		return Change{}, false
	}
	return Change{Kind: "services", Target: c.Name, Detail: c.servicesDetail()}, true
}

// skipDetail extends a workspace-skip detail when the company asked for services: setup never
// rewrites an existing manifest (that would also drop its trust), so it says so.
func (c Company) skipDetail(detail string) string {
	if !c.hasServices() {
		return detail
	}
	return detail + "; services not added (edit .tentaqles.yaml, then tq allow " + c.Name + ")"
}
