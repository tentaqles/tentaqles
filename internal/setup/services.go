package setup

import (
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// hasServices is whether the company names any of the tracker, chat or email blocks.
func (c Company) hasServices() bool { return c.Tracker != nil || c.Chat != nil || c.Email != nil }

// validateServices runs the manifest's own checks on the blocks, so the wizard refuses exactly
// what a hand-written manifest would.
//
// Token-shaped values are refused first: the form's fields take names (an env variable, an org,
// a status), and a token pasted into one must never reach the file Apply writes (spec B §12).
// The manifest loader would refuse the file afterwards, but by then the token is on disk. The
// error names the field and never echoes the value.
func (c Company) validateServices() error {
	if err := c.secretShapedService(); err != nil {
		return err
	}
	m := manifest.Manifest{Tracker: c.Tracker, Chat: c.Chat, Email: c.Email}
	return m.ValidateServices()
}

func (c Company) secretShapedService() error {
	bad := func(field string) error {
		return fmt.Errorf("%s looks like a secret: name it (the manifest holds names only; store the token with tq secret set or tq dotenv)", field)
	}
	if t := c.Tracker; t != nil {
		for field, v := range map[string]string{"tracker.provider": t.Provider, "tracker.assigned_to": t.AssignedTo,
			"tracker.auth": t.Auth, "tracker.env": t.Env} {
			if manifest.LooksLikeSecret(v) {
				return bad(field)
			}
		}
		for _, kv := range []struct {
			name string
			m    map[string]string
		}{{"tracker.scope", t.Scope}, {"tracker.lanes", t.Lanes}} {
			for k, v := range kv.m {
				if manifest.LooksLikeSecret(k) {
					return bad(kv.name + " key")
				}
				if manifest.LooksLikeSecret(v) {
					return bad(kv.name + "." + k)
				}
			}
		}
	}
	for field, s := range map[string]*manifest.Service{"chat.provider": c.Chat, "email.provider": c.Email} {
		if s != nil && manifest.LooksLikeSecret(s.Provider) {
			return bad(field)
		}
	}
	return nil
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
