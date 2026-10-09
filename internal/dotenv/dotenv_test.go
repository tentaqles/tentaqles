package dotenv

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretValue is built at runtime so repo secret scanners do not flag it.
func secretValue() string { return "sk" + strings.Repeat("Q7x", 8) }

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParse(t *testing.T) {
	p := writeEnv(t, "\ufeff# comment\n\nexport A=1\nB=\"quoted # not a comment\"\nC='single'\nD=bare # trailing\nE=\nbad line\nA=2\n")
	es, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range es {
		got[e.Key] = e.Value
	}
	want := map[string]string{"A": "2", "B": "quoted # not a comment", "C": "single", "D": "bare", "E": ""}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if Lookup(p, "A") != "2" || Lookup(p, "missing") != "" {
		t.Error("Lookup")
	}
}

func TestMaskerMasksSplitValuesAndKeepsShortOnes(t *testing.T) {
	sec := secretValue()
	var out bytes.Buffer
	m := NewMasker(&out, map[string]string{"API_TOKEN": sec, "DEBUG": "true"})
	half := len(sec) / 2
	m.Write([]byte("token=" + sec[:half]))
	m.Write([]byte(sec[half:] + " debug=true\nnext line\n"))
	m.Write([]byte("tail " + sec))
	m.Close()
	s := out.String()
	if strings.Contains(s, sec[:8]) {
		t.Fatalf("secret leaked: %q", s)
	}
	if !strings.Contains(s, "token=[REDACTED:API_TOKEN] debug=true\n") || !strings.HasSuffix(s, "tail [REDACTED:API_TOKEN]") {
		t.Fatalf("output = %q", s)
	}
}

func TestMaskerLongLineChunking(t *testing.T) {
	sec := secretValue()
	var out bytes.Buffer
	m := NewMasker(&out, map[string]string{"K": sec})
	m.maxLine = 64
	// Feed byte by byte across many chunk flushes with the secret spanning them.
	payload := strings.Repeat("a", 61) + sec + strings.Repeat("b", 100) + sec
	for i := 0; i < len(payload); i++ {
		m.Write([]byte{payload[i]})
	}
	m.Close()
	if strings.Contains(out.String(), sec[:6]) || strings.Count(out.String(), "[REDACTED:K]") != 2 {
		t.Fatalf("chunked output = %q", out.String())
	}
}

func TestParseNeverTreatsKeyMaterialAsAName(t *testing.T) {
	// Lines of a PEM/base64 blob end in "=", which looks like NAME= .
	b64 := "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSj" + "AgEAAoIBAQC7VJTUt9Us8cKj+MzEfYyjiWA4R4="
	pem := "-----BEGIN " + "PRIVATE KEY-----\n" // split so secret scanners skip the fixture
	p := writeEnv(t, pem+b64+"\nAbC1234567890xyzAbC1234567890xyz=\nGOOD_NAME=1\n")
	es, _ := Parse(p)
	if len(es) != 1 || es[0].Key != "GOOD_NAME" {
		t.Fatalf("entries = %+v", es)
	}
	for _, ok := range []string{".env", "x/.env.local", "prod.env", ".ENV.example"} {
		if !IsEnvFile(ok) {
			t.Errorf("%s should be an env file", ok)
		}
	}
	for _, bad := range []string{"id_rsa", "credentials", "catalog.yaml", ".envrc", "env.json"} {
		if IsEnvFile(bad) {
			t.Errorf("%s accepted as an env file", bad)
		}
	}
}
