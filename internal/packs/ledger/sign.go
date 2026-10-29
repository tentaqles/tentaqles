package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// The signed ledger (fix round 4). Every answered line carries an Ed25519
// signature over every field that decides the outcome: the decision id, the
// binding (pack, action, subject, target), the evidence hash, the granting
// option, the expiry, the asked time, the choice, the actor kind and the
// answer time (domain tag tq.packs.ledger/v2). Readers take those values
// from the signed line, never from the unsigned asked line, so rewriting
// the asked line cannot turn a reject into a grant, move a target or extend
// an expiry.
//
// Keys live in the OS keychain, service KeyService:
//   - account <ws>: the private key, read only on the terminal answer path
//     (tq hub answer, after its TTY and binding check);
//   - account <ws>/pub: the public key, which every reader loads (the hook,
//     tq pack approvals, Verify, doctor). Readers never touch the private key.
//   - account <ws>/<pack>/hwm: the pack's high-water mark (see SetHighWater).
//
// With no readable public key no answer counts (fail closed).

// KeyService is the keychain service of the ledger keys.
const KeyService = "tentaqles-packs"

// SigTag is the signature domain tag.
const SigTag = "tq.packs.ledger/v2"

// ErrNoKey is returned when the workspace has no ledger key yet.
var ErrNoKey = errors.New("ledger: no signing key in the OS keychain for this workspace")

// keychainSet is every ledger keychain write (a seam for the write-failure tests).
var keychainSet = keyring.Set

// FailKeychainWritesForTest makes every ledger keychain write fail with err
// (reads still work) until restore is called. For tests only.
func FailKeychainWritesForTest(err error) (restore func()) {
	prev := keychainSet
	keychainSet = func(string, string, string) error { return err }
	return func() { keychainSet = prev }
}

// tsTime parses a signed time (RFC 3339, nanoseconds allowed).
func tsTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

func pubAccount(ws string) string       { return ws + "/pub" }
func hwmAccount(ws, pack string) string { return ws + "/" + pack + "/hwm" }

// LoadPublicKey reads the workspace's public key (readers). Any keychain
// error is returned: callers fail closed.
func LoadPublicKey(ws string) (ed25519.PublicKey, error) {
	if !plain(ws) {
		return nil, fmt.Errorf("ledger: invalid workspace name")
	}
	v, err := keyring.Get(KeyService, pubAccount(ws))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: the OS keychain cannot be read: %w", err)
	}
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("ledger: the keychain entry is not a ledger public key")
	}
	return ed25519.PublicKey(b), nil
}

// SigningKey returns the workspace's private key, creating the key pair
// (private and public entries) when absent. Only the human answer path
// calls it.
func SigningKey(ws string) (ed25519.PrivateKey, error) {
	if !plain(ws) {
		return nil, fmt.Errorf("ledger: invalid workspace name")
	}
	v, err := keyring.Get(KeyService, ws)
	switch {
	case err == nil:
		seed, derr := hex.DecodeString(v)
		if derr != nil || len(seed) != ed25519.SeedSize {
			return nil, errors.New("ledger: the keychain entry is not a ledger private key")
		}
		priv := ed25519.NewKeyFromSeed(seed)
		pub, perr := LoadPublicKey(ws)
		switch {
		case errors.Is(perr, ErrNoKey):
			if err := keychainSet(KeyService, pubAccount(ws), hex.EncodeToString(priv.Public().(ed25519.PublicKey))); err != nil {
				return nil, fmt.Errorf("ledger: the OS keychain cannot store the public key: %w", err)
			}
		case perr != nil:
			return nil, perr
		case !pub.Equal(priv.Public()):
			// readers would reject every answer signed now: refuse up front
			return nil, errors.New("ledger: the public key in the OS keychain does not match the private key; nothing can be signed (repair the tentaqles-packs entries)")
		}
		return priv, nil
	case !errors.Is(err, keyring.ErrNotFound):
		return nil, fmt.Errorf("ledger: the OS keychain cannot be read: %w", err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := keychainSet(KeyService, ws, hex.EncodeToString(priv.Seed())); err != nil {
		return nil, fmt.Errorf("ledger: the OS keychain cannot store the key: %w", err)
	}
	if err := keychainSet(KeyService, pubAccount(ws), hex.EncodeToString(pub)); err != nil {
		return nil, fmt.Errorf("ledger: the OS keychain cannot store the public key: %w", err)
	}
	return priv, nil
}

// KeychainAvailable reports whether the OS keychain answers, probing only
// the public entry (a missing one is fine): doctor's packs-keychain-missing.
func KeychainAvailable(ws string) error {
	_, err := keyring.Get(KeyService, pubAccount(ws))
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

// sigPayload is the canonical JSON an answered line's signature covers.
func sigPayload(e Entry) []byte {
	b, _ := json.Marshal([]string{SigTag, e.Event, e.Decision, e.Pack, e.Action, e.Subject, e.Target,
		e.EvidenceHash, e.Grant, e.ExpiresAt, e.AskedTS, e.Choice, e.ActorKind, e.TS})
	return b
}

// Sign sets e.Sig.
func Sign(priv ed25519.PrivateKey, e *Entry) {
	e.Sig = hex.EncodeToString(ed25519.Sign(priv, sigPayload(*e)))
}

// ValidSignature reports whether e carries a valid signature for pub.
func ValidSignature(pub ed25519.PublicKey, e Entry) bool {
	if len(pub) != ed25519.PublicKeySize || e.Sig == "" {
		return false
	}
	sig, err := hex.DecodeString(e.Sig)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, sigPayload(e), sig)
}

// Shown is the decision as the human was shown it (the hub store's row):
// what the signature must bind.
type Shown struct {
	Pack, Action, Subject, Target, ExpiresAt, Grant string
	Evidence                                        map[string]string
}

// SameShown compares two views of a decision field for field (the evidence
// by its hash).
func SameShown(a, b Shown) bool {
	return a.Pack == b.Pack && a.Action == b.Action && a.Subject == b.Subject && a.Target == b.Target &&
		a.ExpiresAt == b.ExpiresAt && a.Grant == b.Grant && EvidenceHash(a.Evidence) == EvidenceHash(b.Evidence)
}

// ErrShownMismatch: the ledger's asked line differs from what the human saw.
var ErrShownMismatch = errors.New("ledger: the approval in the ledger differs from the decision you were shown; nothing was signed")

// AnswerSigner builds the signed answered line for one decision. It refuses
// unless the decision's asked line in the pack ledger equals what the human
// was shown, field for field (binding, evidence, expiry, granting option).
func AnswerSigner(ws string, priv ed25519.PrivateKey) func(shown Shown, decisionID, choice, actorKind, ts string) (Entry, error) {
	return func(shown Shown, decisionID, choice, actorKind, ts string) (Entry, error) {
		es, err := Read(ws, shown.Pack)
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
			return Entry{}, fmt.Errorf("ledger: decision %s has no asked line in the %s ledger", decisionID, shown.Pack)
		}
		if !SameShown(Shown{Pack: asked.Pack, Action: asked.Action, Subject: asked.Subject, Target: asked.Target,
			ExpiresAt: asked.ExpiresAt, Grant: asked.Grant, Evidence: asked.Evidence}, shown) {
			return Entry{}, ErrShownMismatch
		}
		e := Entry{Event: Answered, Decision: decisionID, Pack: shown.Pack, Action: shown.Action, Subject: shown.Subject, Target: shown.Target,
			EvidenceHash: EvidenceHash(shown.Evidence), Grant: shown.Grant, ExpiresAt: shown.ExpiresAt, AskedTS: asked.TS,
			Choice: choice, ActorKind: actorKind, TS: ts}
		Sign(priv, &e)
		return e, nil
	}
}

// Mark is a pack's high-water mark: the signed answer time and the decision
// id of its newest consumed approval.
type Mark struct {
	TS       string
	Decision string
}

// LoadHighWater reads a pack's high-water mark (zero when none). A keychain
// error is returned: callers fail closed.
func LoadHighWater(ws, pack string) (Mark, error) {
	v, err := keyring.Get(KeyService, hwmAccount(ws, pack))
	if errors.Is(err, keyring.ErrNotFound) {
		return Mark{}, nil
	}
	if err != nil {
		return Mark{}, fmt.Errorf("ledger: the OS keychain cannot be read: %w", err)
	}
	ts, id, _ := strings.Cut(v, "|")
	return Mark{TS: ts, Decision: id}, nil
}

// SetHighWater raises the pack's high-water mark to the signed answer of
// decisionID (read from the ledger with the public key). Every consume
// calls it (the hook, approval.Claim, approval.MarkUsed); a failure must
// deny. Readers then refuse that decision and every answer signed at or
// before its time (answers carry nanosecond times, so ties are retired too),
// so deleting a claim file or re-appending compacted lines cannot reopen a
// used approval. The cost: consuming one approval also invalidates the
// older, still unused approvals of the same pack.
func SetHighWater(ws, pack, decisionID string) error {
	pub, err := LoadPublicKey(ws)
	if err != nil {
		return err
	}
	es, err := Read(ws, pack)
	if err != nil {
		return err
	}
	var ts string
	for _, e := range es {
		if e.Event == Answered && e.Decision == decisionID && ValidSignature(pub, e) {
			ts = e.TS
			break
		}
	}
	if ts == "" {
		return fmt.Errorf("ledger: decision %s has no signed answer", decisionID)
	}
	cur, err := LoadHighWater(ws, pack)
	if err != nil {
		return err
	}
	at, ok := tsTime(ts)
	if !ok {
		return fmt.Errorf("ledger: decision %s has a signed answer with a bad time", decisionID)
	}
	if ct, ok := tsTime(cur.TS); ok && ct.After(at) {
		return nil // already higher; this decision is older, hence already refused
	}
	return keychainSet(KeyService, hwmAccount(ws, pack), ts+"|"+decisionID)
}

// Trust is what a reader verifies the ledger against: the public key and
// the packs' high-water marks.
type Trust struct {
	Pub ed25519.PublicKey
	HWM map[string]Mark // pack -> mark
}

// LoadTrust reads the public key and the marks of packs. Any keychain error
// returns a Trust with no key (nothing counts) and the error.
func LoadTrust(ws string, packs ...string) (Trust, error) {
	pub, err := LoadPublicKey(ws)
	if err != nil {
		return Trust{}, err
	}
	t := Trust{Pub: pub, HWM: map[string]Mark{}}
	for _, p := range packs {
		m, err := LoadHighWater(ws, p)
		if err != nil {
			return Trust{}, err
		}
		t.HWM[p] = m
	}
	return t, nil
}

// ResolveSigned folds the entries with every outcome-deciding value taken
// from the first validly signed answered line per decision: binding,
// evidence hash, granting option, expiry and asked time come from the
// signature, never from the unsigned asked line. An unsigned or badly
// signed answered line is ignored; with no key no answer counts. A decision
// answered at or before its pack's high-water mark is used.
func ResolveSigned(entries []Entry, t Trust) map[string]State {
	asked := map[string]bool{}
	for _, e := range entries {
		if e.Event == Asked {
			asked[e.Decision] = true
		}
	}
	signed := map[string]Entry{}
	var keep []Entry
	for _, e := range entries {
		if e.Event == Answered {
			if _, done := signed[e.Decision]; done || !asked[e.Decision] || !ValidSignature(t.Pub, e) {
				continue
			}
			signed[e.Decision] = e
		}
		keep = append(keep, e)
	}
	out := Resolve(keep)
	for id, s := range out {
		e, ok := signed[id]
		if !ok {
			continue
		}
		s.Pack, s.Action, s.Subject, s.Target, s.Grant = e.Pack, e.Action, e.Subject, e.Target, e.Grant
		s.EvidenceHash = e.EvidenceHash
		s.ExpiresAt, _ = time.Parse(time.RFC3339, e.ExpiresAt)
		if at, err := time.Parse(time.RFC3339, e.AskedTS); err == nil {
			s.AskedAt = at
		}
		if m := t.HWM[e.Pack]; m.TS != "" && (id == m.Decision || !answeredAfter(e.TS, m.TS)) {
			s.Status = Consumed
			if s.ConsumedAt.IsZero() {
				s.ConsumedAt, _ = tsTime(m.TS)
				if s.ConsumedAt.IsZero() {
					s.ConsumedAt = time.Unix(1, 0)
				}
			}
		}
		out[id] = s
	}
	return out
}

// answeredAfter reports whether a signed answer time is strictly after the
// mark. An unparsable time counts as not after (used: fail closed).
func answeredAfter(ts, mark string) bool {
	a, ok1 := tsTime(ts)
	m, ok2 := tsTime(mark)
	return ok1 && ok2 && a.After(m)
}
