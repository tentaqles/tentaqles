package redact

// Test values are public test numbers with valid checksums, never real data:
// CPF 529.982.247-25, CNPJ 11.222.333/0001-81, Visa test card
// 4111 1111 1111 1111, IBAN registry example GB82 WEST 1234 5698 7654 32.
// 4111 1111 1111 1112 fails Luhn on purpose.

import (
	"strings"
	"testing"
)

// I4 / R8: one class resolution for tq redact and the hook.
func TestResolveClasses(t *testing.T) {
	names := func(s Set) map[string]bool {
		m := map[string]bool{}
		for _, d := range s.detectors {
			m[d.name] = true
		}
		return m
	}
	for _, c := range []struct {
		classes []string
		want    []string
		not     []string
		secrets bool
	}{
		{[]string{"financial"}, []string{"cpf", "cnpj", "card", "iban"}, []string{"email", "phone"}, false},
		{[]string{"checksummed"}, []string{"cpf", "cnpj", "card", "iban"}, []string{"email", "phone"}, false},
		{[]string{"public"}, []string{"cpf", "card"}, []string{"email"}, false},
		{[]string{"pii"}, []string{"cpf", "cnpj", "card", "iban", "email", "phone"}, nil, false},
		{[]string{"sensitive"}, []string{"card", "email", "phone"}, nil, false},
		{[]string{"children"}, []string{"card", "email", "phone"}, nil, false},
		{[]string{"secrets"}, []string{"cpf", "card"}, []string{"email"}, true},
		{[]string{"all"}, []string{"cpf", "cnpj", "card", "iban", "email", "phone"}, nil, true},
	} {
		s, err := Resolve(c.classes)
		if err != nil {
			t.Fatalf("%v: %v", c.classes, err)
		}
		got := names(s)
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("%v: missing %s", c.classes, w)
			}
		}
		for _, w := range c.not {
			if got[w] {
				t.Errorf("%v: unexpected %s", c.classes, w)
			}
		}
		if s.secrets != c.secrets {
			t.Errorf("%v: secrets=%v", c.classes, s.secrets)
		}
	}
	for _, bad := range [][]string{nil, {}, {"bogus"}, {"pii", "bogus"}} {
		if _, err := Resolve(bad); err == nil {
			t.Errorf("%v: want an error (fail closed)", bad)
		}
	}
	// checksummed detectors run before phone, so a CNPJ is never half-masked
	got := Redact("11.222.333/0001-81 4111 1111 1111 1111", []string{"pii"}) // tq:allow-pii
	if got != "[REDACTED:cnpj] [REDACTED:card]" {
		t.Fatalf("pii copy: %q", got)
	}
	if got := Redact("cpf 529.982.247-25", []string{"financial"}); got != "cpf [REDACTED:cpf]" { // tq:allow-pii
		t.Fatalf("financial must mask the CPF too: %q", got)
	}
}

// I5 / R9: a card followed by its expiry or CVV is still a card.
func TestCardWithExpiryOrCVV(t *testing.T) {
	for in, want := range map[string]string{
		"4111 1111 1111 1111 12/28":    "[REDACTED:card] 12/28",    // tq:allow-pii
		"4111 1111 1111 1111 12/2028":  "[REDACTED:card] 12/2028",  // tq:allow-pii
		"4111 1111 1111 1111 123":      "[REDACTED:card] 123",      // tq:allow-pii
		"4111 1111 1111 1111 1234":     "[REDACTED:card] 1234",     // tq:allow-pii
		"cartao 4111111111111111 7":    "cartao [REDACTED:card] 7", // tq:allow-pii
		"4111-1111-1111-1111-123":      "[REDACTED:card]-123",      // tq:allow-pii
		"4111111111111111;123":         "[REDACTED:card];123",      // tq:allow-pii
		"4111 1111 1111 1111, 12/28":   "[REDACTED:card], 12/28",   // tq:allow-pii
		"card 4111 1111 1111 1111 end": "card [REDACTED:card] end", // tq:allow-pii
	} {
		if got := Redact(in, []string{"checksummed"}); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
		if n := CountAll(in, []string{"checksummed"}); n["card"] != 1 {
			t.Errorf("CountAll(%q) = %v", in, n)
		}
	}
	for _, in := range []string{
		"4111 1111 1111 1112 123", // Luhn-invalid 16 digits followed by a count
		"4111 1111 1111 1112 12/28",
		"order 4111111111111112 qty 300",
		"4111111111111111123", // 19 unbroken digits: a different number
	} {
		if n := CountAll(in, []string{"checksummed"}); n["card"] != 0 {
			t.Errorf("false positive card in %q: %v", in, n)
		}
	}
}

func TestParseClassesFailsClosed(t *testing.T) {
	if _, err := ParseClasses(""); err == nil {
		t.Fatal("empty list accepted")
	}
	if _, err := ParseClasses("pii,nope"); err == nil {
		t.Fatal("unknown class accepted")
	}
	if cls, err := ParseClasses("PII, secrets"); err != nil || len(cls) != 2 {
		t.Fatalf("%v %v", cls, err)
	}
	if !strings.Contains(strings.Join(Known, ","), "checksummed") {
		t.Fatal("Known lists checksummed")
	}
}
