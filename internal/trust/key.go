package trust

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/zalando/go-keyring"
)

// The HMAC key lives in the OS keychain through go-keyring, under the same
// service internal/secret uses for tracker tokens, account "integrity-key".
// It is generated the first time a human signs (32 random bytes) and never
// printed: no error, log line or output names its value; only its id
// (KeyID) is shown. Any program running as the user can read or replace a
// keychain entry (threat model AR1); the id in every record makes a key
// swapped without re-signing visible as "integrity key changed".
const (
	Service    = "tentaqles"
	KeyAccount = "integrity-key"
	keyBytes   = 32
)

// HumanCheck refuses a signature unless a human at a terminal runs tq: it
// is the gate of tq allow (internal/humanonly: the TTY, process-tree and
// environment-marker detectors, the refusal logged). Every write of a
// signed record, tombstone or legacy list goes through it, whatever command
// reached it. Tests outside the cmd suite, which run under a harness,
// replace it; nothing else does.
var HumanCheck = func(what string) error { return humanonly.Check(what) }

// KeyFile is the plain-file fallback, $TQ_HOME/integrity.key (0600, hex),
// written only when the OS keychain is unavailable. `tq doctor` warns
// integrity-key-file while it exists.
func KeyFile() string { return filepath.Join(paths.Home(), "integrity.key") }

// KeyFromFile reports that the key comes from the fallback file.
func KeyFromFile() bool {
	_, err := os.Stat(KeyFile())
	return err == nil
}

// keyID is the key's public id: the first 12 hex digits of its sha256.
func keyID(k []byte) string {
	sum := sha256.Sum256(k)
	return hex.EncodeToString(sum[:])[:12]
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

// keyState is what readKey found.
type keyState struct {
	key []byte
	// ok: a key exists (in the keychain or the fallback file).
	ok bool
	// keychainEmpty: the keychain answered and holds no key (as opposed to
	// being unavailable), so a new key may be stored there.
	keychainEmpty bool
	err           error
}

// readKey returns the existing key and never creates one: the keychain
// entry wins; with none, the fallback file. A malformed key is an error that
// names the fix and never the value.
func readKey() keyState {
	if s, ok := memoKey(); ok {
		return s
	}
	s := readKeyUncached()
	storeKey(s)
	return s
}

func readKeyUncached() keyState {
	v, gerr := kc.Get(Service, KeyAccount)
	if gerr == nil {
		k, derr := decodeKey(v)
		if derr != nil {
			return keyState{ok: true, err: fmt.Errorf("the integrity key in the OS keychain (%s/%s) is malformed: delete that entry, then run tq allow <name> for every workspace", Service, KeyAccount)}
		}
		return keyState{key: k, ok: true}
	}
	s := keyState{keychainEmpty: errors.Is(gerr, keyring.ErrNotFound)}
	raw, err := os.ReadFile(KeyFile())
	if err != nil {
		if absent(err) {
			return s
		}
		s.ok, s.err = true, fmt.Errorf("reading %s: %w", KeyFile(), err)
		return s
	}
	k, derr := decodeKey(string(raw))
	if derr != nil {
		s.ok, s.err = true, fmt.Errorf("the integrity key file %s is malformed: delete it, then run tq allow <name> for every workspace", KeyFile())
		return s
	}
	s.key, s.ok = k, true
	return s
}

// signingKey returns the key, generating one the first time a human signs.
// A new key goes to the keychain when the keychain answered "no entry", and
// to the fallback file when it is unavailable; a key never changes
// silently, because an existing one (in either place) is always used. The
// first key also takes the snapshot of the legacy markers then on disk
// (snapshotLegacy), which is what lets them count afterwards. Callers have
// passed HumanCheck.
func signingKey() ([]byte, error) {
	s := readKey()
	if s.err != nil {
		return nil, s.err
	}
	if s.ok {
		if err := markSigned(); err != nil {
			return nil, err
		}
		return s.key, nil
	}
	k := make([]byte, keyBytes)
	if _, err := rand.Read(k); err != nil {
		return nil, fmt.Errorf("generating the integrity key: %w", err)
	}
	stored := false
	if s.keychainEmpty {
		if serr := kc.Set(Service, KeyAccount, hex.EncodeToString(k)); serr == nil {
			// Another tq may have stored a key between the Get and the
			// Set; what the keychain holds now is the key.
			if v, gerr := kc.Get(Service, KeyAccount); gerr == nil {
				if got, derr := decodeKey(v); derr == nil {
					k, stored = got, true
				}
			}
		}
	}
	if !stored {
		if err := writeKeyFile(k); err != nil {
			return nil, err
		}
	}
	invalidate()
	if err := snapshotLegacy(k); err != nil {
		return nil, err
	}
	if err := markSigned(); err != nil {
		return nil, err
	}
	return k, nil
}

// SignedMarkerPath is the machine marker trust/.signed: an empty file whose
// presence alone says that a human has signed on this machine. Every
// signature writes it when it is absent and tq never deletes it, so a
// machine whose key or legacy snapshot went missing is not mistaken for one
// that never signed (legacy.go): with the marker, no legacy marker counts.
func SignedMarkerPath() string { return filepath.Join(paths.TrustDir(), ".signed") }

func markSigned() error {
	p := SignedMarkerPath()
	if _, err := os.Lstat(p); err == nil {
		return nil
	}
	if err := writeAtomic(p, nil); err != nil {
		return err
	}
	invalidate()
	return nil
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
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
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
