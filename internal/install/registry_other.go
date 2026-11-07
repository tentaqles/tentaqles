//go:build !windows

package install

// RegistryPath has no meaning off Windows: a new shell reads its PATH from its profile.
func RegistryPath() (string, error) { return "", nil }
