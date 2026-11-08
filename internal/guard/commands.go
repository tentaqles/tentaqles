package guard

import "strings"

// BaseName returns the executable name a command word names: its last path
// element, quotes removed, lower-cased, without a .exe suffix.
func BaseName(w string) string {
	return strings.TrimSuffix(strings.ToLower(baseWord(w)), ".exe")
}
