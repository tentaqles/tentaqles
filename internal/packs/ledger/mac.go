package ledger

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/zalando/go-keyring"
)

// The ledger MAC (fix round 3): every answered line carries an HMAC-SHA256
// over its canonical JSON (decision, binding, evidence hash, choice, actor
// kind, time), keyed by a per-workspace secret in the OS keychain (service
// KeyService, account <workspace>). Only the human answer path (tq hub
// answer at a terminal, after the TTY and binding check) creates the key or
// signs with it. Readers ignore an answered line whose MAC is missing or
// wrong, and with no readable key every answered line counts as not
// answered: a raw append to the ledger can no longer forge a grant.

// KeyService is the keychain service of the ledger keys.
const KeyService = "tentaqles-packs"

// ErrNoKey is returned when the workspace has no ledger key yet.
var ErrNoKey = errors.New("ledger: no signing key in the OS keychain for this workspace")

// LoadKey reads the workspace's ledger key from the OS keychain (it never
// creates one). Any keychain error is returned: callers fail closed.
func LoadKey(ws string) ([]byte, error) {
	if !plain(ws) {
		return nil, fmt.Errorf("ledger: invalid workspace name")
	}
	v, err := keyring.Get(KeyService, ws)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: the OS keychain cannot be read: %w", err)
	}
	k, err := hex.DecodeString(v)
	if err != nil || len(k) < 32 {
		return nil, errors.New("ledger: the keychain entry is not a ledger key")
	}
	return k, nil
}

// SigningKey returns the workspace's ledger key, creating it when absent.
// Only the human answer path calls it.
func SigningKey(ws string) ([]byte, error) {
	k, err := LoadKey(ws)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, ErrNoKey) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := keyring.Set(KeyService, ws, hex.EncodeToString(b)); err != nil {
		return nil, fmt.Errorf("ledger: the OS keychain cannot store the key: %w", err)
	}
	return b, nil
}

// KeychainAvailable reports whether the OS keychain answers (a missing key
// is fine): doctor's packs-keychain-missing check.
func KeychainAvailable(ws string) error {
	_, err := keyring.Get(KeyService, ws)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// EvidenceHash is the sha256 of the evidence map in canonical JSON.
func EvidenceHash(ev map[string]string) string {
	keys := make([]string, 0, len(ev))
	for k := range ev {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([][2]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, [2]string{k, ev[k]})
	}
	b, _ := json.Marshal(pairs)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// macPayload is the canonical JSON an answered line's MAC covers.
func macPayload(e Entry) []byte {
	b, _ := json.Marshal([]string{"tq.packs.ledger/v1", e.Event, e.Decision, e.Pack, e.Action, e.Subject, e.Target,
		e.EvidenceHash, e.Choice, e.ActorKind, e.TS})
	return b
}

// Sign sets e.MAC.
func Sign(key []byte, e *Entry) {
	m := hmac.New(sha256.New, key)
	m.Write(macPayload(*e))
	e.MAC = hex.EncodeToString(m.Sum(nil))
}

// ValidMAC reports whether e carries the right MAC for key.
func ValidMAC(key []byte, e Entry) bool {
	if len(key) == 0 || e.MAC == "" {
		return false
	}
	got, err := hex.DecodeString(e.MAC)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, key)
	m.Write(macPayload(e))
	return hmac.Equal(got, m.Sum(nil))
}

// AnswerSigner builds the signed answered line for one decision: it reads
// the decision's asked line (the binding) from the pack ledger and signs the
// answer over that binding and its evidence hash.
func AnswerSigner(ws string, key []byte) func(pack, decisionID, choice, actorKind, ts string) (Entry, error) {
	return func(pack, decisionID, choice, actorKind, ts string) (Entry, error) {
		es, err := Read(ws, pack)
		if err != nil {
			return Entry{}, err
		}
		var asked *Entry
		for i := range es {
			if es[i].Event == Asked && es[i].Decision == decisionID {
				asked = &es[i]
				break
			}
		}
		if asked == nil {
			return Entry{}, fmt.Errorf("ledger: decision %s has no asked line in the %s ledger", decisionID, pack)
		}
		e := Entry{Event: Answered, Decision: decisionID, Pack: pack, Action: asked.Action, Subject: asked.Subject, Target: asked.Target,
			EvidenceHash: EvidenceHash(asked.Evidence), Choice: choice, ActorKind: actorKind, TS: ts}
		Sign(key, &e)
		return e, nil
	}
}

// ResolveSigned is Resolve that counts an answered line only when its MAC is
// valid for key and its binding and evidence hash equal the decision's
// asked line. With no key (nil), no answered line counts.
func ResolveSigned(entries []Entry, key []byte) map[string]State {
	asked := map[string]Entry{}
	for _, e := range entries {
		if e.Event == Asked {
			if _, ok := asked[e.Decision]; !ok {
				asked[e.Decision] = e
			}
		}
	}
	var keep []Entry
	for _, e := range entries {
		if e.Event == Answered {
			a, ok := asked[e.Decision]
			if !ok || !ValidMAC(key, e) || e.Pack != a.Pack || e.Action != a.Action || e.Subject != a.Subject ||
				e.Target != a.Target || e.EvidenceHash != EvidenceHash(a.Evidence) {
				continue
			}
		}
		keep = append(keep, e)
	}
	return Resolve(keep)
}
