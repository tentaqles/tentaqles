package resolve

import "github.com/zalando/go-keyring"

// The integrity record's HMAC key lives in the OS keychain (internal/trust);
// these tests sign records, so the keychain is go-keyring's in-memory mock
// for the whole test binary, never the user's.
func init() { keyring.MockInit() }
