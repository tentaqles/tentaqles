package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func askedLine(exp string, ev map[string]string) Entry {
	return Entry{Event: Asked, Decision: "d1", Pack: "cloud", Action: "apply", Subject: "s", Target: "prod", Evidence: ev, ExpiresAt: exp, Grant: "apply", TS: "2026-10-29T10:00:00Z"}
}

func shownOf(a Entry) Shown {
	return Shown{Pack: a.Pack, Action: a.Action, Subject: a.Subject, Target: a.Target, ExpiresAt: a.ExpiresAt, Grant: a.Grant, Evidence: a.Evidence}
}

// F1, F4: Ed25519 over every outcome field; readers take them from the
// signed line; readers load only the public key.
func TestSignedAnswersOnly(t *testing.T) {
	isolate(t)
	keyring.MockInit()
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ev := map[string]string{"plan": "sha256:aa"}
	a := askedLine(exp, ev)
	if err := Append("acme", a); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPublicKey("acme"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("no key yet: %v", err)
	}
	priv, err := SigningKey("acme")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := LoadTrust("acme", "cloud")
	if err != nil || len(tr.Pub) == 0 {
		t.Fatalf("public key: %v", err)
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	forged := Entry{Event: Answered, Decision: "d1", Pack: "cloud", Action: "apply", Subject: "s", Target: "prod", EvidenceHash: EvidenceHash(ev),
		Grant: "apply", ExpiresAt: exp, AskedTS: a.TS, Choice: "apply", ActorKind: "hub", TS: ts}
	wrong := forged
	wrong.Sig = "00"
	for name, e := range map[string]Entry{"no signature": forged, "wrong signature": wrong} {
		if st := ResolveSigned([]Entry{a, e}, tr)["d1"]; st.Granted() {
			t.Fatalf("%s counted", name)
		}
	}
	sign := AnswerSigner("acme", priv)
	signed, err := sign(shownOf(a), "d1", "apply", "hub", ts)
	if err != nil {
		t.Fatal(err)
	}
	if st := ResolveSigned([]Entry{a, signed}, tr)["d1"]; !st.Granted() || st.EvidenceHash != EvidenceHash(ev) {
		t.Fatalf("a signed answer must count: %+v", st)
	}
	if st := ResolveSigned([]Entry{a, signed}, Trust{})["d1"]; st.Granted() {
		t.Fatal("no key: nothing counts")
	}
	// F1: a rewritten asked line changes nothing the signature covers.
	tampered := a
	tampered.Target, tampered.Grant, tampered.ExpiresAt = "staging", "reject", "2099-01-01T00:00:00Z"
	st := ResolveSigned([]Entry{tampered, signed}, tr)["d1"]
	if st.Target != "prod" || st.Grant != "apply" || st.ExpiresAt.Format(time.RFC3339) != exp {
		t.Fatalf("readers must take the signed values: %+v", st)
	}
	// A signed reject stays a reject when the asked grant is rewritten.
	rej, _ := sign(shownOf(a), "d1", "reject", "hub", ts)
	tg := a
	tg.Grant = "reject"
	if st := ResolveSigned([]Entry{tg, rej}, tr)["d1"]; st.Granted() {
		t.Fatal("a tampered grant turned a signed reject into a grant")
	}
	// F2: the signer refuses when the ledger differs from what was shown.
	shown := shownOf(a)
	shown.Target = "staging"
	if _, err := sign(shown, "d1", "apply", "hub", ts); !errors.Is(err, ErrShownMismatch) {
		t.Fatalf("shown mismatch: %v", err)
	}
	keyring.MockInitWithError(errors.New("no secret service"))
	if _, err := LoadTrust("acme", "cloud"); err == nil {
		t.Fatal("keychain error must surface")
	}
	if KeychainAvailable("acme") == nil {
		t.Fatal("KeychainAvailable")
	}
	keyring.MockInit()
}

// F3: the high-water mark makes a consumed approval, and every older answer
// of the pack, used even when the claim and consumed lines are gone.
func TestHighWaterMark(t *testing.T) {
	isolate(t)
	keyring.MockInit()
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ev := map[string]string{"plan": "sha256:aa"}
	priv, _ := SigningKey("acme")
	sign := AnswerSigner("acme", priv)
	old := askedLine(exp, ev)
	old.Decision = "d0"
	Append("acme", old)
	a := askedLine(exp, ev)
	Append("acme", a)
	s0, _ := sign(shownOf(old), "d0", "apply", "hub", "2026-10-29T10:00:00Z")
	s1, err := sign(shownOf(a), "d1", "apply", "hub", "2026-10-29T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	Append("acme", s0)
	Append("acme", s1)
	if err := SetHighWater("acme", "cloud", "d1"); err != nil {
		t.Fatal(err)
	}
	tr, _ := LoadTrust("acme", "cloud")
	es, _ := Read("acme", "cloud") // no claim file and no consumed line
	st := ResolveSigned(es, tr)
	if !st["d1"].Used() || !st["d0"].Used() {
		t.Fatalf("watermark: d1 used %v, d0 used %v", st["d1"].Used(), st["d0"].Used())
	}
	if err := SetHighWater("acme", "cloud", "nope"); err == nil {
		t.Fatal("a decision with no signed answer cannot be marked")
	}
}
