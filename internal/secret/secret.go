// Package secret keeps tracker tokens in the OS keychain (spec B §7, §12): the Windows Credential
// Manager, the macOS Keychain or the Secret Service on Linux, through github.com/zalando/go-keyring.
// Every token sits under service "tentaqles", account "<workspace>/<provider>".
//
// A value never leaves this package except through Get, to the one caller that sends it. No error,
// log line or output names a value; errors name the account only.
package secret

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/zalando/go-keyring"
)

// Service is the keychain service every tq tracker token is stored under.
const Service = "tentaqles"

// Account is the keychain account of a workspace's provider token: "<ws>/<provider>".
func Account(workspace, provider string) string { return workspace + "/" + provider }

// ErrNotFound means the keychain holds no token for that workspace and provider.
var ErrNotFound = errors.New("no token in the OS keychain")

// Backend is a keychain. The default is the OS keychain; tests inject go-keyring's mock (MockInit)
// or their own.
type Backend interface {
	Get(service, user string) (string, error)
	Set(service, user, value string) error
	Delete(service, user string) error
}

// osBackend is the OS keychain through go-keyring (or its mock, after MockInit).
type osBackend struct{}

func (osBackend) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (osBackend) Set(s, u, v string) error        { return keyring.Set(s, u, v) }
func (osBackend) Delete(s, u string) error        { return keyring.Delete(s, u) }

var (
	mu      sync.RWMutex
	backend Backend = osBackend{}
)

func current() Backend {
	mu.RLock()
	defer mu.RUnlock()
	return backend
}

// SetBackend installs b (nil = the OS keychain) and returns a func restoring the previous one.
func SetBackend(b Backend) (restore func()) {
	if b == nil {
		b = osBackend{}
	}
	mu.Lock()
	prev := backend
	backend = b
	mu.Unlock()
	return func() {
		mu.Lock()
		backend = prev
		mu.Unlock()
	}
}

// MockInit replaces the OS keychain with go-keyring's empty in-memory mock for the rest of the
// process, and points the backend at it. Tests only.
func MockInit() {
	keyring.MockInit()
	SetBackend(osBackend{})
}

// MockInitWithError is MockInit with a mock whose every call fails with err. Tests only.
func MockInitWithError(err error) {
	keyring.MockInitWithError(err)
	SetBackend(osBackend{})
}

// ValidateNames checks a workspace name and a tracker provider id before they form an account.
func ValidateNames(workspace, provider string) error {
	switch {
	case workspace == "":
		return errors.New("a workspace name is required")
	case workspace != strings.TrimSpace(workspace), strings.ContainsAny(workspace, `/\`), hasControl(workspace), len(workspace) > 128:
		return fmt.Errorf("workspace %q: not a workspace name", workspace)
	}
	for _, p := range manifest.TrackerProviders {
		if p == provider {
			return nil
		}
	}
	return fmt.Errorf("provider %q: use one of %s", provider, strings.Join(manifest.TrackerProviders, ", "))
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// failure wraps a backend error with the account it was for, never the value.
func failure(op, account string, err error) error {
	return fmt.Errorf("keychain %s %s: %w", op, account, err)
}

// Set stores a token (surrounding whitespace and the line end trimmed) for workspace/provider,
// replacing any earlier one.
func Set(workspace, provider, value string) error {
	if err := ValidateNames(workspace, provider); err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("the token is empty; nothing was stored")
	}
	if hasControl(value) {
		return errors.New("the token holds control characters; nothing was stored")
	}
	acct := Account(workspace, provider)
	if err := current().Set(Service, acct, value); err != nil {
		if errors.Is(err, keyring.ErrSetDataTooBig) {
			return fmt.Errorf("keychain set %s: the token is too long for the OS keychain", acct)
		}
		return failure("set", acct, err)
	}
	return nil
}

// Get is the stored token, or ErrNotFound. The caller uses it for one request and keeps nothing.
func Get(workspace, provider string) (string, error) {
	if err := ValidateNames(workspace, provider); err != nil {
		return "", err
	}
	acct := Account(workspace, provider)
	v, err := current().Get(Service, acct)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("%s: %w", acct, ErrNotFound)
	}
	if err != nil {
		return "", failure("get", acct, err)
	}
	return v, nil
}

// Has reports whether a token is stored, without returning it.
func Has(workspace, provider string) (bool, error) {
	_, err := Get(workspace, provider)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// Delete removes the stored token, or reports ErrNotFound.
func Delete(workspace, provider string) error {
	if err := ValidateNames(workspace, provider); err != nil {
		return err
	}
	acct := Account(workspace, provider)
	err := current().Delete(Service, acct)
	if errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("%s: %w", acct, ErrNotFound)
	}
	if err != nil {
		return failure("delete", acct, err)
	}
	return nil
}
