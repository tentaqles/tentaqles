package setupapi

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// Services lists the accepted names for the Services step, straight from the manifest's own
// lists so the wizard and the loader can never disagree.
type Services struct {
	Chat  []string
	Email []string
}

// ServiceCatalog returns the accepted chat and email names.
func ServiceCatalog() Services {
	cp := func(xs []string) []string { return append([]string(nil), xs...) }
	return Services{
		Chat:  cp(manifest.ChatProviders),
		Email: cp(manifest.EmailProviders),
	}
}

// serviceBlock turns a Chat/Email provider name into its block; "" skips it, "none" is kept
// (the company has no such tool, said explicitly).
func serviceBlock(provider string) *manifest.Service {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil
	}
	return &manifest.Service{Provider: provider}
}
