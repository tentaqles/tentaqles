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

func TestIsEnvFile(t *testing.T) {
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

func TestParseMultilineQuotedValue(t *testing.T) {
	pem := "-----BEGIN " + "PRIVATE KEY-----"
	// base64url-ish body lines contain '_' and end in '=', so a line-by-line
	// parser would read them as NAME= entries.
	body := "abc_DEF_ghi_JKL_mno_PQR_stu_VWX=\nzz_YY_xx_WW_vv_UU="
	p := writeEnv(t, "A=1\nKEY=\""+pem+"\n"+body+"\n-----END "+"PRIVATE KEY-----\"\nB=2\nOPEN='never closed\nabc_DEF_ghi_JKL=\n")
	es, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range es {
		keys = append(keys, e.Key)
	}
	if strings.Join(keys, ",") != "A,KEY,B,OPEN" {
		t.Fatalf("keys = %v (body lines must never become names)", keys)
	}
	if !strings.Contains(es[1].Value, "zz_YY_xx_WW_vv_UU=") || !strings.HasSuffix(es[1].Value, "PRIVATE KEY-----") {
		t.Fatalf("multiline value = %q", es[1].Value)
	}
}

func TestMaskerMasksEachLineOfMultilineValue(t *testing.T) {
	v := "first-line-QQQ111\nsecond-line-ZZZ222"
	var out bytes.Buffer
	m := NewMasker(&out, map[string]string{"PEM": v})
	m.Write([]byte("dump:\n" + v + "\nend\n"))
	m.Close()
	if strings.Contains(out.String(), "QQQ111") || strings.Contains(out.String(), "ZZZ222") {
		t.Fatalf("multiline value leaked: %q", out.String())
	}
}
