// internal/redact/redact_test.go
package redact

import (
	"bytes"
	"strings"
	"testing"
)

func TestCPF(t *testing.T) {
	valid := []string{"529.982.247-25", "52998224725"}
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
	if got := Redact("cnpj 11.222.333/0001-81", []string{"financial"}); !strings.Contains(got, "[REDACTED:cnpj]") {
		t.Fatalf("%q", got)
	}
	if got := Redact("cnpj 11.222.333/0001-82", []string{"financial"}); strings.Contains(got, "REDACTED") {
		t.Fatalf("bad check digit redacted: %q", got)
	}
	if got := Redact("card 4111 1111 1111 1111", []string{"financial"}); !strings.Contains(got, "[REDACTED:card]") {
		t.Fatalf("%q", got)
	}
	if got := Redact("id 4111111111111112", []string{"financial"}); strings.Contains(got, "REDACTED") {
		t.Fatalf("Luhn-invalid redacted: %q", got)
	}
	if got := Redact("iban GB82 WEST 1234 5698 7654 32", []string{"financial"}); !strings.Contains(got, "[REDACTED:iban]") {
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
	text := "cpf 529.982.247-25 key sk-ant-abcdefghijklmnopqrstuvwxyz0123"
	if got := Redact(text, []string{"financial"}); strings.Contains(got, "REDACTED") {
		t.Fatalf("financial must not redact a CPF: %q", got)
	}
	got := Redact(text, []string{"pii", "secrets"})
	if !strings.Contains(got, "[REDACTED:cpf]") || !strings.Contains(got, "[REDACTED:anthropic_key]") {
		t.Fatalf("%q", got)
	}
	if _, err := ParseClasses("pii,bogus"); err == nil {
		t.Fatal("unknown class accepted")
	}
	cls, err := ParseClasses("all")
	if err != nil || len(cls) < 3 {
		t.Fatalf("%v %v", cls, err)
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
