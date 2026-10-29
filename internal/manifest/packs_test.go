package manifest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const packsHead = "schema: tentaqles-client-v2\nclient: acme\n"

func TestLoad_PacksBlockParsesAndKeepsRaw(t *testing.T) {
	m, err := Load(write(t, packsHead+`
packs:
  data:
    tools: [dbt, sqlfluff]
    timeouts: { dbt: 600s }
    tool_paths: { dbt: C:/tools/dbt/dbt.exe }
    baseline: .tq/sqlfluff-baseline.json
    contracts: { paths: [contracts/] }
  ml: { enabled: false, tools: [ruff] }
`))
	if err != nil {
		t.Fatal(err)
	}
	d := m.Packs["data"]
	if !d.IsEnabled() || len(d.Tools) != 2 || time.Duration(d.Timeouts["dbt"]) != 600*time.Second || d.ToolPaths["dbt"] == "" || d.Baseline == "" {
		t.Fatalf("data: %+v", d)
	}
	if d.Raw.Kind != yaml.MappingNode || d.Raw.Line == 0 {
		t.Fatalf("raw not kept: %+v", d.Raw)
	}
	if m.Packs["ml"].IsEnabled() {
		t.Fatal("enabled: false must disable")
	}
}

func TestLoad_PacksRejects(t *testing.T) {
	for name, body := range map[string]string{
		"unknown pack":   "packs: { web: { enabled: true } }\n",
		"bad tool":       "packs: { data: { tools: [Dbt] } }\n",
		"bad tool chars": "packs: { data: { tools: [\"dbt;rm\"] } }\n",
		"timeout cap":    "packs: { data: { timeouts: { dbt: 3600s } } }\n",
		"bad duration":   "packs: { data: { timeouts: { dbt: soon } } }\n",
		"baseline out":   "packs: { data: { baseline: ../x.json } }\n",
		"not a mapping":  "packs: { data: [1, 2] }\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, packsHead+body)); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestLoad_PacksSecretScan(t *testing.T) {
	_, err := Load(write(t, packsHead+"packs: { data: { token: ghp_abcdefghijklmnopqrstuvwxyz0123 } }\n")) // tq:allow-secret: fake fixture
	if !errors.Is(err, ErrSecretLike) {
		t.Fatalf("want secret error, got %v", err)
	}
}

func TestPackValidatorRunsOnPackKeys(t *testing.T) {
	RegisterPackValidator("db", func(n yaml.Node) error {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if k != "enabled" && k != "tools" && k != "drill" {
				return errors.New("unknown key " + k)
			}
		}
		return nil
	})
	t.Cleanup(func() { RegisterPackValidator("db", nil) })
	if _, err := Load(write(t, packsHead+"packs: { db: { drill: {} } }\n")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(write(t, packsHead+"packs: { db: { dril: {} } }\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown key dril") {
		t.Fatalf("got %v", err)
	}
	// No validator: keys are kept and not checked.
	if _, err := Load(write(t, packsHead+"packs: { cloud: { anything: 1 } }\n")); err != nil {
		t.Fatal(err)
	}
}

func TestEnabledDefaults(t *testing.T) {
	m, err := Load(write(t, packsHead+"packs:\n  data: {}\n  ml:\n  cloud: { enabled: true }\n  db: { tools: [squawk] }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Packs["data"].IsEnabled() || m.Packs["ml"].IsEnabled() || !m.Packs["cloud"].IsEnabled() || !m.Packs["db"].IsEnabled() {
		t.Fatalf("defaults: %+v", m.Packs)
	}
}

func TestParseRepoPacks(t *testing.T) {
	got, err := ParseRepoPacks([]byte("data:\n  tools: [sqlfluff]\n"))
	if err != nil || len(got["data"].Tools) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	got, err = ParseRepoPacks([]byte("packs:\n  ml: { tools: [ruff] }\n"))
	if err != nil || len(got["ml"].Tools) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := ParseRepoPacks([]byte("web: {}\n")); err == nil {
		t.Fatal("unknown pack accepted")
	}
	if _, err := ParseRepoPacks([]byte("data: { k: Bearer abcdefghijkl }\n")); !errors.Is(err, ErrSecretLike) { // tq:allow-secret: fake fixture
		t.Fatalf("secret: %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"600s": 600 * time.Second, "10m": 10 * time.Minute, "7d": 7 * 24 * time.Hour, "30": 30 * time.Second} {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Fatalf("%s: %v %v", in, got, err)
		}
	}
}
