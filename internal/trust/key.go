package trust

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/zalando/go-keyring"
)

// The HMAC key lives in the OS keychain through go-keyring, under the same
// service internal/secret uses for tracker tokens, account "integrity-key".
// It is generated on first use (32 random bytes) and never printed: no
// error, log line or output names its value.
const (
	Service    = "tentaqles"
	KeyAccount = "integrity-key"
	keyBytes   = 32
)

// KeyFile is the plain-file fallback, $TQ_HOME/integrity.key (0600, hex),
// written only when the OS keychain is unavailable. `tq doctor` warns
// integrity-key-file while it exists: any program running as the user can
// read it (threat model AR1).
func KeyFile() string { return filepath.Join(paths.Home(), "integrity.key") }

// KeyFromFile reports that the key comes from the fallback file.
func KeyFromFile() bool {
	_, err := os.Stat(KeyFile())
	return err == nil
}

// keychain is the OS keychain. Tests swap go-keyring for its in-memory mock
// (keyring.MockInit, as internal/secret's tests do); nothing else replaces it.
type keychain interface {
	Get(service, user string) (string, error)
	Set(service, user, value string) error
}

type osKeychain struct{}

func (osKeychain) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (osKeychain) Set(s, u, v string) error        { return keyring.Set(s, u, v) }

var kc keychain = osKeychain{}

// loadKey returns the HMAC key, generating one on first use. The keychain
// entry wins when it exists. When the keychain has no entry but the
// fallback file exists, the file's key is used, so a key never changes
// silently once records were signed with it. Otherwise a fresh key is
// stored in the keychain, or in the fallback file when the keychain is
// unavailable (any error but "not found").
func loadKey() ([]byte, error) {
	v, err := kc.Get(Service, KeyAccount)
	if err == nil {
		k, derr := decodeKey(v)
		if derr != nil {
			return nil, fmt.Errorf("the integrity key in the OS keychain (%s/%s) is malformed: delete that entry, then run tq allow <name> for every workspace", Service, KeyAccount)
		}
		return k, nil
	}
	if k, ok, ferr := readKeyFile(); ferr != nil {
		return nil, ferr
	} else if ok {
		return k, nil
	}
	k, err2 := newKey()
	if err2 != nil {
		return nil, err2
	}
	if errors.Is(err, keyring.ErrNotFound) {
		if serr := kc.Set(Service, KeyAccount, hex.EncodeToString(k)); serr == nil {
			// Another tq may have stored a key between the Get and the
			// Set; what the keychain holds now is the key.
			if v, gerr := kc.Get(Service, KeyAccount); gerr == nil {
				if stored, derr := decodeKey(v); derr == nil {
					return stored, nil
				}
			}
			return k, nil
		}
	}
	if werr := writeKeyFile(k); werr != nil {
		return nil, werr
	}
	return k, nil
}

func newKey() ([]byte, error) {
	k := make([]byte, keyBytes)
	if _, err := rand.Read(k); err != nil {
		return nil, fmt.Errorf("generating the integrity key: %w", err)
	}
	return k, nil
}

func decodeKey(s string) ([]byte, error) {
	k, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, err
	}
	if len(k) != keyBytes {
		return nil, fmt.Errorf("key is %d bytes, want %d", len(k), keyBytes)
	}
	return k, nil
}

func readKeyFile() ([]byte, bool, error) {
	raw, err := os.ReadFile(KeyFile())
	if err != nil {
		if absent(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading %s: %w", KeyFile(), err)
	}
	k, err := decodeKey(string(raw))
	if err != nil {
		return nil, false, fmt.Errorf("the integrity key file %s is malformed: delete it, then run tq allow <name> for every workspace", KeyFile())
	}
	return k, true, nil
}

func writeKeyFile(k []byte) error {
	p := KeyFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return writeAtomic(p, []byte(hex.EncodeToString(k)+"\n"))
}

// writeAtomic writes data to p (0600) through a temp file and a rename, so
// a reader never sees a half-written record or key.
func writeAtomic(p string, data []byte) error {
	tmp := p + ".tq.tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
