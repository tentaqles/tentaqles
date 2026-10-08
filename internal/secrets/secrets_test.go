package secrets

import (
	"strings"
	"testing"
)

// Fixtures are assembled at runtime so this file never holds a literal
// credential that a push-protection scanner would flag.
func j(parts ...string) string { return strings.Join(parts, "") }

func TestScanLine(t *testing.T) {
	cases := []struct {
		name, line, want string
	}{
		{"aws", j("key = AK", "IAABCDEFGHIJKLMNOP"), "aws_access_key"},
		{"github", j("token: gh", "p_", strings.Repeat("a", 36)), "github_token"},
		{"github fine-grained", j("github", "_pat_", strings.Repeat("B", 40)), "github_token"},
		{"anthropic", j("sk-", "ant-", strings.Repeat("c", 30)), "anthropic_key"},
		{"jwt", j("eyJ", "hbGciOi.eyJ", "zdWIiOi.abcdefgh"), "jwt"},
		{"conn string", j("postgres://app:", "s3cretpass@db.internal:5432/x"), "connection_string"},
		{"private key", j("-----BEGIN RSA ", "PRIVATE KEY-----"), "private_key"},
		{"assignment", j("API_KEY=", "abcdef1234567890xyz"), "api_key_assignment"},
		{"placeholder var", "API_KEY=${OPENAI_API_KEY}", ""},
		{"placeholder conn", "postgres://app:${DB_PASSWORD}@db:5432/x", ""},
		{"placeholder angle", "client_secret: <your-secret>", ""},
		{"allow marker", j("API_KEY=", "abcdef1234567890xyz # gitleaks:allow"), ""},
		{"tq allow marker", j("API_KEY=", "abcdef1234567890xyz # tq:allow-secret"), ""},
		{"plain code", "const apiKey = process.env.API_KEY", ""},
		{"no creds url", "redis://localhost:6379", ""},
		{"prefixed key name", j("API_TOKEN: ", "h7Kq2LmX9pRt4VwZ"), "api_key_assignment"},
		{"postgres env key", j("POSTGRES_PASSWORD=", "Sup3rS3cretPass"), "api_key_assignment"},
		{"percent-encoded password", j("postgresql://app:", "p%40ss%23w0rd@db.x:5432/d"), "connection_string"},
		{"windows var placeholder", "postgresql://app:%DB_PASS%@db.x:5432/d", ""},
		{"max tokens not a secret", "max_tokens: 4096", ""},
		{"tokenizer word ok", "tokenizer = load_tokenizer_from_disk_cache()", ""},
		{"portuguese text", "senha do usuário não é exibida 🔒", ""},
	}
	for _, c := range cases {
		if got := ScanLine(c.line); got != c.want {
			t.Errorf("%s: ScanLine = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestScanReportsLinesWithoutValues(t *testing.T) {
	text := j("a\nAPI_KEY=", "abcdef1234567890xyz\nb\n")
	got := Scan(text)
	if len(got) != 1 || got[0].Line != 2 || got[0].Pattern != "api_key_assignment" {
		t.Fatalf("Scan = %+v", got)
	}
}

func TestRedact(t *testing.T) {
	secret := j("gh", "p_", strings.Repeat("z", 36))
	out := Redact("use " + secret + " and API_KEY=${KEEP_ME}")
	if strings.Contains(out, secret) {
		t.Fatalf("secret survived redaction: %q", out)
	}
	if !strings.Contains(out, "[REDACTED:github_token]") || !strings.Contains(out, "${KEEP_ME}") {
		t.Fatalf("unexpected redaction: %q", out)
	}
}
