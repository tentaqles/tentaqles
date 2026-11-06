package setup

import (
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/zalando/go-keyring"
)

// The integrity record's HMAC key lives in the OS keychain (internal/trust);
// these tests sign records, so the keychain is go-keyring's in-memory mock
// for the whole test binary, never the user's. Signing is human-only
// (trust.HumanCheck) and this test binary runs under a harness: the gate
// passes here; the cmd suite and internal/trust pin it.
func init() {
	keyring.MockInit()
	trust.HumanCheck = func(string) error { return nil }
}
