// internal/redact/redact_test.go
package redact

// Test values are public test numbers with valid checksums, never real data:
// CPF 529.982.247-25, CNPJ 11.222.333/0001-81, Visa test card tq:allow-pii
// 4111 1111 1111 1111, IBAN registry example GB82 WEST 1234 5698 7654 32. tq:allow-pii
// Lines carrying them are marked tq:allow-pii (a test file, see markerAllowed).

import (
	"bytes"
	"strings"
	"testing"
)

func TestCPF(t *testing.T) {
	valid := []string{"529.982.247-25", "52998224725"} // tq:allow-pii
	for _, v := range valid {
		if got := Redact("cpf "+v+" ok", []string{"pii"}); !strings.Contains(got, "[REDACTED:cpf]") || strings.Contains(got, v) {
			t.Errorf("valid CPF %s not redacted: %q", v, got)
		}
	}
	invalid := []string{"123.456.789-00", "11111111111", "52998224726", "4002-8922-0001"} // wrong check digits, repeated, order id
	for _, v := range invalid {
		if got := Redact("x "+v+" y", []string{"pii"}); strings.Contains(got, "REDACTED") {
			t.Errorf("non-CPF %s redacted: %q", v, got)
		}
	}
}

func TestCNPJAndCard(t *testing.T) {
	if got := Redact("cnpj 11.222.333/0001-81", []string{"financial"}); !strings.Contains(got, "[REDACTED:cnpj]") { // tq:allow-pii
		t.Fatalf("%q", got)
	}
	if got := Redact("cnpj 11.222.333/0001-82", []string{"financial"}); strings.Contains(got, "REDACTED") {
		t.Fatalf("bad check digit redacted: %q", got)
	}
	if got := Redact("card 4111 1111 1111 1111", []string{"financial"}); !strings.Contains(got, "[REDACTED:card]") { // tq:allow-pii
		t.Fatalf("%q", got)
	}
	if got := Redact("id 4111111111111112", []string{"financial"}); strings.Contains(got, "REDACTED") {
		t.Fatalf("Luhn-invalid redacted: %q", got)
	}
	if got := Redact("iban GB82 WEST 1234 5698 7654 32", []string{"financial"}); !strings.Contains(got, "[REDACTED:iban]") { // tq:allow-pii
		t.Fatalf("%q", got)
	}
}

func TestEmailAndPhone(t *testing.T) {
	got := Redact("mail ana.silva@example.com.br, tel +55 11 91234-5678 and (21) 98765-4321", []string{"pii"})
	for _, tok := range []string{"[REDACTED:email]", "[REDACTED:phone]"} {
		if !strings.Contains(got, tok) {
			t.Fatalf("%q lacks %s", got, tok)
		}
	}
	if strings.Contains(got, "ana.silva") || strings.Contains(got, "91234") || strings.Contains(got, "98765") {
		t.Fatalf("value survived: %q", got)
	}
	if got := Redact("version 1.2.3 built 2026-10-20 port 8080", []string{"pii"}); strings.Contains(got, "REDACTED") {
		t.Fatalf("false positive: %q", got)
	}
}

func TestClassesSelectDetectorsAndSecrets(t *testing.T) {
	text := "cpf 529.982.247-25 key sk-ant-abcdefghijklmnopqrstuvwxyz0123" // tq:allow-pii tq:allow-secret
	// checksummed detectors always run (final review I4): financial masks the
	// CPF too, but not the secret
	if got := Redact(text, []string{"financial"}); !strings.Contains(got, "[REDACTED:cpf]") || strings.Contains(got, "anthropic_key") {
		t.Fatalf("financial: %q", got)
	}
	got := Redact(text, []string{"pii", "secrets"})
	if !strings.Contains(got, "[REDACTED:cpf]") || !strings.Contains(got, "[REDACTED:anthropic_key]") {
		t.Fatalf("%q", got)
	}
	if _, err := ParseClasses("pii,bogus"); err == nil {
		t.Fatal("unknown class accepted")
	}
	cls, err := ParseClasses("all")
	if set, rerr := Resolve(cls); err != nil || rerr != nil || !set.secrets || len(set.detectors) < 6 {
		t.Fatalf("%v %v %v", cls, err, rerr)
	}
	if n := Count(text, []string{"all"}); n["cpf"] != 1 || n["anthropic_key"] != 1 {
		t.Fatalf("%v", n)
	}
}

func TestWriterMasksAcrossChunks(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, []string{"pii"})
	for _, chunk := range []string{"row: 529.98", "2.247-25 end\nnext ", "line\n", "tail ana@x.io"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "row: [REDACTED:cpf] end\nnext line\ntail [REDACTED:email]") {
		t.Fatalf("%q", out)
	}
}

func TestPhoneLeavesPlainDigitRuns(t *testing.T) {
	for _, v := range []string{"1700000000", "52998224726", "123456789012"} {
		if got := Redact("id "+v+" end", []string{"pii"}); strings.Contains(got, "REDACTED") {
			t.Errorf("plain digit run %s masked: %q", v, got)
		}
	}
}

func TestEmptyClassListFailsClosed(t *testing.T) {
	text := "cpf 529.982.247-25 key ghp_abcdefghijklmnopqrstuvwxyz0123" // tq:allow-pii tq:allow-secret
	for _, cls := range [][]string{nil, {}} {
		got := Redact(text, cls)
		if strings.Contains(got, "529.982") || strings.Contains(got, "ghp_abc") {
			t.Fatalf("empty class list failed open: %q", got)
		}
		if n := Count(text, cls); n["cpf"] != 1 || len(n) < 2 {
			t.Fatalf("Count under empty class list: %v", n)
		}
		var buf bytes.Buffer
		w := NewWriter(&buf, cls)
		_, _ = w.Write([]byte("cpf 529.982.247-25\n")) // tq:allow-pii
		_ = w.Close()
		if strings.Contains(buf.String(), "529.982") {
			t.Fatalf("writer failed open: %q", buf.String())
		}
	}
}

func TestUnknownClassFailsClosed(t *testing.T) {
	text := "cpf 529.982.247-25 key ghp_abcdefghijklmnopqrstuvwxyz0123" // tq:allow-pii tq:allow-secret
	got := Redact(text, []string{"bogus"})
	if strings.Contains(got, "529.982") || strings.Contains(got, "ghp_abc") {
		t.Fatalf("unknown class failed open: %q", got)
	}
	n := Count(text, []string{"bogus"})
	if n["cpf"] != 1 || len(n) < 2 {
		t.Fatalf("Count under unknown class: %v", n)
	}
	var buf bytes.Buffer
	w := NewWriter(&buf, []string{"bogus"})
	_, _ = w.Write([]byte("cpf 529.982.247-25\n")) // tq:allow-pii
	_ = w.Close()
	if strings.Contains(buf.String(), "529.982") {
		t.Fatalf("writer failed open: %q", buf.String())
	}
}

func TestChecksummedClassAndAllowMarker(t *testing.T) {
	text := "cpf 529.982.247-25 mail ana@x.io" // tq:allow-pii
	got := Redact(text, []string{"checksummed"})
	if !strings.Contains(got, "[REDACTED:cpf]") || !strings.Contains(got, "ana@x.io") {
		t.Fatalf("checksummed must mask the CPF and leave the e-mail: %q", got)
	}
	if n := Count("row 529.982.247-25 # tq:allow-pii", []string{"all"}); len(n) != 0 { // tq:allow-pii
		t.Fatalf("allow marker ignored: %v", n)
	}
	if got := Redact("row 529.982.247-25 # tq:allow-pii", []string{"all"}); strings.Contains(got, "REDACTED") { // tq:allow-pii
		t.Fatalf("allow marker ignored in Redact: %q", got)
	}
}

func TestAllowMarkerStillMasksSecrets(t *testing.T) {
	line := "KEY=sk-ant-abcdefghijklmnopqrstuvwxyz0123 # tq:allow-pii" // tq:allow-secret
	got := Redact(line, []string{"all"})
	if !strings.Contains(got, "[REDACTED:anthropic_key]") {
		t.Fatalf("secret on a marked line must still be masked: %q", got)
	}
	if n := Count(line, []string{"all"}); n["anthropic_key"] != 1 {
		t.Fatalf("secret on a marked line must still be counted: %v", n)
	}
	if n := CountAll("row 529.982.247-25 # tq:allow-pii", []string{"checksummed"}); n["cpf"] != 1 { // tq:allow-pii
		t.Fatalf("CountAll must ignore the marker: %v", n)
	}
}
