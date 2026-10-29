package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// R2-2: an answered line counts only with a valid MAC over its binding.
func TestSignedAnswersOnly(t *testing.T) {
	isolate(t)
	keyring.MockInit()
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ev := map[string]string{"plan": "sha256:aa"}
	if err := Append("acme", Entry{Event: Asked, Decision: "d1", Pack: "cloud", Action: "apply", Subject: "s", Target: "prod", Evidence: ev, ExpiresAt: exp, Grant: "apply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey("acme"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("no key yet: %v", err)
	}
	key, err := SigningKey("acme")
	if err != nil {
		t.Fatal(err)
	}
	if k2, err := LoadKey("acme"); err != nil || string(k2) != string(key) {
		t.Fatal("the key must persist in the keychain")
	}
	forged := Entry{Event: Answered, Decision: "d1", Pack: "cloud", Action: "apply", Subject: "s", Target: "prod", EvidenceHash: EvidenceHash(ev), Choice: "apply", ActorKind: "hub", TS: time.Now().UTC().Format(time.RFC3339)}
	wrong := forged
	Sign([]byte("not the key, but 32 bytes long....."), &wrong)
	for name, e := range map[string]Entry{"no mac": forged, "wrong mac": wrong} {
		st := ResolveSigned([]Entry{{Event: Asked, Decision: "d1", Pack: "cloud", Action: "apply", Subject: "s", Target: "prod", Evidence: ev, ExpiresAt: exp, Grant: "apply"}, e}, key)["d1"]
		if !st.AnsweredAt.IsZero() || st.Granted() {
			t.Fatalf("%s: a forged answer counted", name)
		}
	}
	signed, err := AnswerSigner("acme", key)("cloud", "d1", "apply", "hub", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	es := []Entry{{Event: Asked, Decision: "d1", Pack: "cloud", Action: "apply", Subject: "s", Target: "prod", Evidence: ev, ExpiresAt: exp, Grant: "apply"}, signed}
	if st := ResolveSigned(es, key)["d1"]; !st.Granted() {
		t.Fatal("a signed answer must count")
	}
	if st := ResolveSigned(es, nil)["d1"]; st.Granted() {
		t.Fatal("no key: nothing counts")
	}
	// A signed answer replayed onto another binding does not count.
	es[0].Target = "staging"
	if st := ResolveSigned(es, key)["d1"]; st.Granted() {
		t.Fatal("binding changed under the MAC")
	}
	keyring.MockInitWithError(errors.New("no secret service"))
	if _, err := LoadKey("acme"); err == nil || errors.Is(err, ErrNoKey) {
		t.Fatalf("keychain error must surface: %v", err)
	}
	if KeychainAvailable("acme") == nil {
		t.Fatal("KeychainAvailable")
	}
	keyring.MockInit()
}
