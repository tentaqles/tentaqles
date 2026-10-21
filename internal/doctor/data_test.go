package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func collect(fn func(add func(level, code, ws, msg, fix string))) []Finding {
	var out []Finding
	fn(func(level, code, ws, msg, fix string) {
		out = append(out, Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix})
	})
	return out
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDataBlockMissing_SchemaWithPIIColumns(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, "db/schema/customers.sql", "create table customers (id int, email text, cpf char(11));\n")
	fs := collect(func(add func(string, string, string, string, string)) {
		dataBlockMissing("acme", root, manifest.Data{}, add)
	})
	if len(fs) != 1 || fs[0].Code != "data-block-missing" || !strings.Contains(fs[0].Msg, "customers.sql") || !strings.Contains(fs[0].Fix, "classify-data") {
		t.Fatalf("%+v", fs)
	}
}

func TestDataBlockMissing_FixturesCount(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, "tests/fixtures/people.sql", "insert into people (email, phone) values ('a@b.c', '1');\n")
	fs := collect(func(add func(string, string, string, string, string)) {
		dataBlockMissing("acme", root, manifest.Data{}, add)
	})
	if len(fs) != 1 {
		t.Fatalf("fixtures are where PII leaks; want a finding, got %+v", fs)
	}
}

func TestDataBlockMissing_EnvExampleAndPlainCodeIgnored(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, ".env.example", "EMAIL_FROM=noreply@example.test\n")
	write(t, root, "src/mailer.go", "func send(email string) {}\n")
	fs := collect(func(add func(string, string, string, string, string)) {
		dataBlockMissing("acme", root, manifest.Data{}, add)
	})
	if len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

func TestDataBlockMissing_SilentWhenClassified(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, "db/schema/customers.sql", "create table customers (email text);\n")
	d := manifest.Data{Classes: []string{"pii"}}
	fs := collect(func(add func(string, string, string, string, string)) { dataBlockMissing("acme", root, d, add) })
	if len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

func TestDataBlockMissing_UnderscoreCompoundColumns(t *testing.T) {
	for _, body := range []string{"create table u (user_email TEXT);\n", "create table u (phone_number varchar);\n", "create table u (billing_address text, cpf_cliente text);\n"} {
		root := testutil.TempDir(t)
		write(t, root, "db/u.sql", body)
		fs := collect(func(add func(string, string, string, string, string)) {
			dataBlockMissing("acme", root, manifest.Data{}, add)
		})
		if len(fs) != 1 {
			t.Fatalf("%q: want a finding, got %+v", body, fs)
		}
	}
}

func TestDataBlockMissing_EmailedAtNotMatched(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, "db/u.sql", "create table u (id int, emailed_at timestamp);\n")
	fs := collect(func(add func(string, string, string, string, string)) {
		dataBlockMissing("acme", root, manifest.Data{}, add)
	})
	if len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

func TestDataBlockMissing_SymlinkIgnored(t *testing.T) {
	root := testutil.TempDir(t)
	target := filepath.Join(testutil.TempDir(t), "real.txt")
	if err := os.WriteFile(target, []byte("email"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "schema.sql")); err != nil {
		t.Skip("symlink unavailable: ", err)
	}
	fs := collect(func(add func(string, string, string, string, string)) {
		dataBlockMissing("acme", root, manifest.Data{}, add)
	})
	if len(fs) != 0 {
		t.Fatalf("symlink must not be followed: %+v", fs)
	}
}

func TestDataBlockMissing_ReadBoundedToLimit(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, "db/big.sql", strings.Repeat("x", dataScanMaxBytes)+" email\n")
	// file is slightly over the limit: skipped by size; and the reader itself is bounded
	fs := collect(func(add func(string, string, string, string, string)) {
		dataBlockMissing("acme", root, manifest.Data{}, add)
	})
	if len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
	if got := readBounded(filepath.Join(root, "db", "big.sql"), 100); strings.Contains(string(got), "email") || len(got) != 100 {
		t.Fatalf("readBounded len=%d", len(got))
	}
}
