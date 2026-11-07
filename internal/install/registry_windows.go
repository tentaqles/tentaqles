//go:build windows

package install

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// RegistryPath is the PATH a new Windows process would start with: the machine PATH then the
// user PATH, read from the registry with their %VARIABLES% expanded. A package manager that
// changed the PATH changed these values; the running process still has the old one.
func RegistryPath() (string, error) {
	var parts []string
	for _, src := range []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
		{registry.CURRENT_USER, `Environment`},
	} {
		v, err := readPath(src.root, src.path)
		if err != nil {
			continue // a hive without a Path value adds nothing
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, ";"), nil
}

func readPath(root registry.Key, path string) (string, error) {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return "", err
	}
	if exp, err := registry.ExpandString(v); err == nil {
		v = exp
	}
	return v, nil
}
