package manifest

import (
	"fmt"
	"strings"
)

// The email block names the client's email provider. It is optional, and like the rest of the
// manifest it holds names only, never secrets.

// EmailProviders are the accepted email.provider values.
var EmailProviders = []string{"gmail", "outlook", "none"}

// Service is a one-provider block (chat, email, data_platform).
type Service struct {
	Provider string `yaml:"provider"`
}

// Enabled is whether the block names a provider other than none.
func (s *Service) Enabled() bool { return s != nil && s.Provider != "" && s.Provider != "none" }

func (s *Service) validate(field string, known []string) error {
	if s == nil {
		return nil
	}
	if s.Provider == "" {
		return fmt.Errorf("%s.provider is required (%s)", field, strings.Join(known, ", "))
	}
	if !contains(known, s.Provider) {
		return fmt.Errorf("%s.provider: unknown provider %q (known: %s)%s", field, s.Provider, strings.Join(known, ", "), unknownProviderHint(s.Provider))
	}
	return nil
}

// ValidateServices checks the email block (Load runs it; writers call it before writing).
func (m *Manifest) ValidateServices() error {
	return m.Email.validate("email", EmailProviders)
}
