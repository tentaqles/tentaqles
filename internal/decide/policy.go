package decide

import (
	"fmt"
	"path/filepath"
	"time"
)

// Policy is a workspace's `decision:` manifest block.
type Policy struct {
	// Backend is "typesafe" or "off" (the default: Jev is opt-in per client).
	Backend string `yaml:"backend"`
	// Mode is "shadow" (the default: log what Jev would do, act on nothing)
	// or "enforce" (Jev may add an ask or a deny).
	Mode string `yaml:"mode"`
	// BlockThreshold: a yes-probability at or above it escalates to deny.
	BlockThreshold float64 `yaml:"block_threshold"`
	// WarnThreshold: at or above it (and below block) escalates to ask.
	WarnThreshold float64 `yaml:"warn_threshold"`
	// Model overrides the pinned model; leave empty unless re-evaluated.
	Model string `yaml:"model"`
	// BaseURL swaps the endpoint (a local laya-serve, a gateway).
	BaseURL string `yaml:"base_url"`
	// EnvFile is a dotenv file holding TYPESAFE_API_KEY, relative to the
	// manifest's directory or absolute. Manifests hold names only; the key
	// stays in that file.
	EnvFile string `yaml:"env_file"`
	// TimeoutMS overrides the 800ms hook deadline.
	TimeoutMS int `yaml:"timeout_ms"`
}

// Enabled reports whether Jev may be called at all.
func (p Policy) Enabled() bool { return p.Backend == "typesafe" }

// Enforcing reports whether Jev answers may change a decision.
func (p Policy) Enforcing() bool { return p.Enabled() && p.Mode == "enforce" }

// Thresholds returns block and warn thresholds with defaults (0.8 / 0.5).
func (p Policy) Thresholds() (block, warn float64) {
	block, warn = p.BlockThreshold, p.WarnThreshold
	if block <= 0 || block > 1 {
		block = 0.8
	}
	if warn <= 0 || warn > block {
		warn = 0.5
		if warn > block {
			warn = block
		}
	}
	return block, warn
}

// Validate rejects unknown values so a typo cannot silently turn Jev on.
func (p Policy) Validate() error {
	switch p.Backend {
	case "", "off", "typesafe":
	default:
		return fmt.Errorf("decision.backend must be typesafe or off, got %q", p.Backend)
	}
	switch p.Mode {
	case "", "shadow", "enforce":
	default:
		return fmt.Errorf("decision.mode must be shadow or enforce, got %q", p.Mode)
	}
	if p.BlockThreshold < 0 || p.BlockThreshold > 1 || p.WarnThreshold < 0 || p.WarnThreshold > 1 {
		return fmt.Errorf("decision thresholds must be between 0 and 1")
	}
	return nil
}

// EnvFilePath resolves EnvFile against the manifest directory.
func (p Policy) EnvFilePath(manifestDir string) string {
	if p.EnvFile == "" {
		return ""
	}
	if filepath.IsAbs(p.EnvFile) {
		return p.EnvFile
	}
	return filepath.Join(manifestDir, p.EnvFile)
}

// Client builds a client for this policy, or ErrDisabled / ErrNoKey.
func (p Policy) Client(manifestDir string, timeout time.Duration, fallbackEnvFiles ...string) (*Client, error) {
	if !p.Enabled() {
		return nil, ErrDisabled
	}
	key := ResolveKey(append([]string{p.EnvFilePath(manifestDir)}, fallbackEnvFiles...)...)
	if key == "" {
		return nil, ErrNoKey
	}
	if p.TimeoutMS > 0 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	c := New(key, timeout)
	if p.Model != "" {
		c.Model = p.Model
	}
	if p.BaseURL != "" {
		c.BaseURL = p.BaseURL
	}
	return c, nil
}

// Action is what a Jev answer asks for on top of the deterministic result.
type Action string

const (
	None Action = ""
	Ask  Action = "ask"
	Deny Action = "deny"
)

// Escalation is the outcome of one judgment rule.
type Escalation struct {
	// Would is what Jev's answer maps to under the thresholds.
	Would Action
	// Apply is what the caller may act on: Would in enforce mode, None in
	// shadow mode or on any error.
	Apply Action
	P     float64
	Err   error
}

// Escalate maps a yes-probability ("this call is risky") to an action. It
// can only add strictness: there is no Allow, and errors map to None so the
// caller keeps its deterministic decision.
func Escalate(p Policy, prob float64, err error) Escalation {
	e := Escalation{P: prob, Err: err}
	if err != nil {
		return e
	}
	block, warn := p.Thresholds()
	switch {
	case prob >= block:
		e.Would = Deny
	case prob >= warn:
		e.Would = Ask
	}
	if p.Enforcing() {
		e.Apply = e.Would
	}
	return e
}
