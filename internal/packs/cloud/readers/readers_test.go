package readers

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const fixtures = "../testdata/readers"

// secretsInFixtures are the clear-text sensitive values planted in the
// fixtures. None may appear in any reader output, redacted document or
// summary.
var secretsInFixtures = []string{
	"hunter2-tester-pass",
	"tester-db-pass-0000",
	"tok-1111-2222-3333",
	"tester-whole-secret-blob",
	"tester-storage-key-0000000000000000",
	"tester-cfn-pass-0000",
	"tester-pulumi-pass-0000",
	"tester0000000000000000000000000000000000000000==",
	"tester-literal-token-123456",
	// tfplan/encoded.json: a jsonencode'd ECS environment and helm values.
	"tester-ecs-pw1",
	"tester-hp1",
	"tester-hp2",
}

type fixture struct {
	kind model.Kind
	path string
	name string // the name passed to the reader: kind/base
}

func listFixtures(t *testing.T) []fixture {
	t.Helper()
	var out []fixture
	for _, k := range model.Kinds() {
		ents, err := os.ReadDir(filepath.Join(fixtures, string(k)))
		if err != nil {
			t.Fatalf("fixtures for %s: %v", k, err)
		}
		n := 0
		for _, e := range ents {
			b := e.Name()
			if e.IsDir() || strings.HasSuffix(b, ".golden.json") || strings.HasSuffix(b, ".redacted.json") {
				continue
			}
			out = append(out, fixture{kind: k, path: filepath.Join(fixtures, string(k), b), name: string(k) + "/" + b})
			n++
		}
		if n == 0 {
			t.Fatalf("no fixtures for %s", k)
		}
	}
	return out
}

func readFixture(t *testing.T, f fixture) []byte {
	t.Helper()
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func golden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (run with -update): %v", path, err)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(want, got) {
		t.Errorf("%s differs from the golden file.\n--- got ---\n%s", path, got)
	}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGolden(t *testing.T) {
	for _, f := range listFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			raw := readFixture(t, f)
			if got := Detect(f.name, raw); got != f.kind {
				t.Fatalf("Detect = %q, want %q", got, f.kind)
			}
			rs, err := Read(f.kind, f.name, raw, Options{})
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(rs) == 0 {
				t.Fatal("no resources")
			}
			for _, r := range rs {
				if r.Source != f.kind || r.Pos.File != f.name || r.Addr == "" || r.Type == "" {
					t.Errorf("incomplete resource %+v", r)
				}
				if r.Pos.Line < 1 {
					t.Errorf("%s: no line", r.Addr)
				}
			}
			golden(t, f.path+".golden.json", marshal(t, rs))
		})
	}
}

// TestLines checks that each resource's line points into its own block in
// the multi-line fixtures: the block that starts there names the resource.
func TestLines(t *testing.T) {
	for _, f := range listFixtures(t) {
		raw := readFixture(t, f)
		if !bytes.Contains(bytes.TrimSpace(raw), []byte("\n")) {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		rs, err := Read(f.kind, f.name, raw, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			key := lineKey(f.kind, r)
			end := r.Pos.Line + 12
			if end > len(lines) {
				end = len(lines)
			}
			block := strings.Join(lines[r.Pos.Line-1:end], "\n")
			if !strings.Contains(block, key) {
				t.Errorf("%s %s: line %d does not start its block (want %q near it)", f.name, r.Addr, r.Pos.Line, key)
			}
		}
	}
}

func lineKey(k model.Kind, r model.Resource) string {
	switch k {
	case model.KindCFN, model.KindChangeSet:
		if i := strings.LastIndex(r.Addr, "/"); i >= 0 {
			return r.Addr[i+1:]
		}
		return r.Addr
	case model.KindARM:
		if s := r.Meta["symbol"]; s != "" {
			return s
		}
		return `"type"`
	case model.KindTFPlan:
		return `"address": "` + r.Addr
	}
	return r.Addr
}

func TestNoSecretLeaks(t *testing.T) {
	for _, f := range listFixtures(t) {
		raw := readFixture(t, f)
		rs, err := Read(f.kind, f.name, raw, Options{Env: "prod"})
		if err != nil {
			t.Fatal(err)
		}
		red, err := Redact(f.kind, raw)
		if err != nil {
			t.Fatalf("%s Redact: %v", f.name, err)
		}
		sum := model.Summarize(rs)
		outs := map[string]string{
			"resources": string(marshal(t, rs)),
			"redacted":  string(red),
			"summary":   string(marshal(t, sum)) + sum.Text(),
		}
		for what, s := range outs {
			for _, leak := range secretsInFixtures {
				if strings.Contains(s, leak) {
					t.Errorf("%s: %s leaks %q", f.name, what, leak)
				}
			}
		}
		for _, r := range rs {
			if r.Env != "prod" {
				t.Fatalf("%s: env not set", r.Addr)
			}
		}
		// The redacted copy reads back to the same resources.
		k2 := Detect(f.name, red)
		if k2 != f.kind {
			t.Fatalf("%s: redacted copy detects as %q", f.name, k2)
		}
		rs2, err := Read(f.kind, f.name, red, Options{Env: "prod"})
		if err != nil {
			t.Fatalf("%s: re-read redacted: %v", f.name, err)
		}
		if len(rs2) != len(rs) {
			t.Fatalf("%s: redacted copy has %d resources, original %d", f.name, len(rs2), len(rs))
		}
		for i := range rs {
			if rs[i].Addr != rs2[i].Addr || rs[i].Type != rs2[i].Type || strings.Join(rs[i].Actions, ",") != strings.Join(rs2[i].Actions, ",") {
				t.Errorf("%s: resource %d differs after redaction: %s vs %s", f.name, i, rs[i].Addr, rs2[i].Addr)
			}
		}
	}
}

func TestRedactGoldenTFPlan(t *testing.T) {
	for _, name := range []string{"changes.json", "builtin-create.json"} {
		p := filepath.Join(fixtures, "tfplan", name)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		red, err := Redact(model.KindTFPlan, raw)
		if err != nil {
			t.Fatal(err)
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, red, "", "  "); err != nil {
			t.Fatal(err)
		}
		pretty.WriteByte('\n')
		golden(t, p+".redacted.json", pretty.Bytes())
	}
}

func TestReadAuto(t *testing.T) {
	for _, f := range listFixtures(t) {
		raw := readFixture(t, f)
		k, rs, err := ReadAuto(f.name, raw, Options{})
		if err != nil || k != f.kind || len(rs) == 0 {
			t.Errorf("%s: ReadAuto = %q, %d, %v", f.name, k, len(rs), err)
		}
	}
}

func TestInvalid(t *testing.T) {
	dir := filepath.Join(fixtures, "invalid")
	read := func(n string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if _, err := Read(model.KindTFPlan, "truncated.json", read("truncated.json"), Options{}); err == nil {
		t.Error("truncated plan: no error")
	}
	for _, n := range []string{"unknown.json", "array.json"} {
		if k := Detect(n, read(n)); k != "" {
			t.Errorf("%s detected as %q", n, k)
		}
		if _, _, err := ReadAuto(n, read(n), Options{}); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("%s: ReadAuto err = %v", n, err)
		}
		if _, err := Redact("", read(n)); err == nil {
			t.Errorf("%s: Redact of unknown kind: no error", n)
		}
	}
	if _, err := Read(model.KindCFN, "bad.yaml", read("bad.yaml"), Options{}); err == nil {
		t.Error("bad yaml: no error")
	}
	// Each reader refuses the other formats.
	tmpl, _ := os.ReadFile(filepath.Join(fixtures, "arm", "template.json"))
	for _, k := range model.Kinds() {
		if k == model.KindARM {
			continue
		}
		if _, err := Read(k, "arm/template.json", tmpl, Options{}); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("%s reader on an ARM template: err = %v", k, err)
		}
	}
	if _, err := Read("nope", "x", []byte("{}"), Options{}); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("unknown kind: %v", err)
	}
}

func TestTooLarge(t *testing.T) {
	old := MaxInput
	MaxInput = 16
	defer func() { MaxInput = old }()
	raw := []byte(`{"format_version":"1.2","resource_changes":[]}`)
	if _, err := Read(model.KindTFPlan, "p.json", raw, Options{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Redact(model.KindTFPlan, raw); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Redact err = %v", err)
	}
}

func TestDeterministic(t *testing.T) {
	for _, f := range listFixtures(t) {
		raw := readFixture(t, f)
		a, _ := Read(f.kind, f.name, raw, Options{})
		b, _ := Read(f.kind, f.name, raw, Options{})
		if !bytes.Equal(marshal(t, a), marshal(t, b)) {
			t.Errorf("%s: two reads differ", f.name)
		}
		r1, _ := Redact(f.kind, raw)
		r2, _ := Redact(f.kind, raw)
		if !bytes.Equal(r1, r2) {
			t.Errorf("%s: two redactions differ", f.name)
		}
	}
}
