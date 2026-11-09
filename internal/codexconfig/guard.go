package codexconfig

import (
	"fmt"
	"path/filepath"
)

// ConfigPath is the config file of a Codex home: codexHome/config.toml.
func ConfigPath(codexHome string) string {
	return filepath.Join(codexHome, configFile)
}

// Guard is whether a Codex session with this CODEX_HOME runs tentaqles' hooks, as tentaqles run
// and tentaqles doctor report it.
type Guard struct {
	// OK is true when the tentaqles hooks are present, enabled and trusted.
	OK bool
	// Trust is the state HookTrust reported (Missing when the file could not be read).
	Trust Trust
	// Why says what is missing, in one line; "" when OK.
	Why string
	// Fix is the lines that fix it, in the order to run them; "" when OK.
	Fix []string
	// Err is set when the config file can't be read or parsed.
	Err error
}

// CheckGuard reports whether a Codex session in codexHome runs the tentaqles guard, for client.
// An empty codexHome is a client with no codex in its scope: there is no Codex config to guard.
// A config that cannot be read fails the check, and Err says why.
func CheckGuard(codexHome, client string) Guard {
	if codexHome == "" {
		return Guard{Why: "this client has no codex in its scope, so there is no Codex config for tentaqles to guard",
			Fix: []string{"add codex to the identities in " + client + "'s manifest, then: tentaqles settings render " + client}}
	}
	path := ConfigPath(codexHome)
	settings := "tentaqles settings render " + client
	t, err := HookTrust(codexHome)
	if err != nil {
		return Guard{Trust: Missing, Err: err,
			Why: fmt.Sprintf("the Codex config can't be read (%v)", err),
			Fix: []string{"repair " + path + " (it can't be read as TOML), then: " + settings}}
	}
	var why string
	fix := []string{settings}
	switch t {
	case Trusted:
		return Guard{OK: true, Trust: t}
	case Missing:
		why = "the tentaqles hooks are not in " + path
	case Disabled:
		why = "the tentaqles hooks are turned off in " + path + " (enabled = false, or hooks or codex_hooks = false under [features])"
		fix = []string{"in " + path + ", remove hooks = false or codex_hooks = false from [features] if it is there, or turn the tentaqles hooks on in Codex's /hooks", settings}
	case Untrusted:
		why = "Codex has not trusted the tentaqles hooks in " + path
	case Modified:
		why = "the tentaqles hooks in " + path + " changed since Codex trusted them"
	}
	return Guard{Trust: t, Why: why, Fix: fix}
}
