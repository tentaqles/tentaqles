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
