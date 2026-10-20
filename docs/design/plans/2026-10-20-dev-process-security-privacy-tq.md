# Security and Privacy Pack, part 1 (tq CLI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `tq` the deterministic data-protection and security controls of the development process: the manifest `data:` block, `tq redact` (output masking for personal and financial data), the new guard rules (personal data at the exits: commits, pushes, PR and issue text, fixture dumps; IaC apply, remote migrations, settings widening, branch-pinned actions, commit messages), two new triage flags, and a doctor check for repositories that handle PII without a `data:` block.

**Architecture:** Everything here is Go inside the existing layers. The manifest gains a validated `Data` struct (`internal/manifest`), consumed by three places: a new `internal/redact` package behind `tq redact` (regex detectors with check-digit and Luhn validation, class-selectable, a streaming writer like `dotenv.Masker`), the PreToolUse hook (`internal/cmd/claudehook_policy.go`), where four code rules guard the exits (a commit, a push, PR or issue text, a fixture dump) using the `redact` detectors selected by the manifest's declared classes, the regex rules live in `internal/policy/builtin.go`, and `tq decide triage`, which adds a deterministic `classified_data` flag next to a new Jev question `agentic`. Reads of personal data are not gated (user decision 2026-10-20): Claude may open classified files, query classified tables and run scripts over them; what it may not do is record or publish the values. `tq doctor` warns on PII-looking schema columns without a `data:` block.

**Tech Stack:** Go 1.23+, cobra, `gopkg.in/yaml.v3`, `go test`; the existing `internal/secrets`, `internal/policy`, `internal/guardcfg`, `internal/decide`, `internal/doctor`, `internal/commitscan` packages.

**Spec:** `C:\repos\tentaqles\docs\superpowers\specs\2026-10-20-development-process-design.md` §8.2, §9 (triage), §11.1, §11.2, §11.3; §14 assumptions corrected below. Companion plans: `2026-10-20-dev-process-spine.md` (engine, skills), `2026-10-20-dev-process-security-privacy-plugin.md` (templates, reviewer agents, security skills; it consumes `tq redact`, the `data:` block and the triage flags from this plan).

All paths are relative to `C:\repos\tentaqles\tentaqles`.

## Global Constraints

- The guard fails closed: new deny rules are regex or code checks that run before any Jev call; a hook error allows only the way today's hook does (malformed payload), never because a new check panicked. Wrap manifest-dependent checks so a nil manifest means "rule does not apply".
- Hook latency: no new I/O beyond what exists (`commitscan` already runs git). The classified-table check reads only the manifest already loaded; `commit-message` with `-F` reads one small file (cap 64 KiB).
- Manifests hold names only: `data.incident.contact` is validated with `manifest.LooksLikeSecret`; the whole file still fails `Load` on any secret-shaped value.
- `tq redact` never writes what it read anywhere but stdout; findings are counted by pattern name, values are never logged.
- Rule ids are stable and documented in `docs/CLAUDE-HOOK.md`; every new rule has a `Reason` that tells the agent the safe alternative.
- Spec §11.2 as revised 2026-10-20 applies: no read gating; `tq redact` is a helper. Deviation from §8.2 `tq/config-change`: implemented as PreToolUse rules on settings files (`tq/settings-widen`, `tq/settings-shell-write`) because the `ConfigChange` hook event cannot be verified from this plan; a follow-up may add the event when the installed Claude Code lists it. Deviation from §8.2 `tq/iac-apply` "deny when no environment variable names the target": the hook cannot see the Bash tool's environment, so the rule asks and its reason requires the target to be named in the command.
- Conventional commits; `go test ./...` green after every task; git identity `reach@tentaqles.ai`.

## Review Focus

1. **A CPF-shaped number that is not a CPF** (11 digits failing the check digits, such as a phone number without separators or an order id) must not be redacted; and a valid CPF with or without separators must be. Pinned in Task 2 (`TestCPF`).
2. **A `data:` block with a path glob written with backslashes** (Windows user) must still match slash-normalised tool paths. Pinned in Task 1 (`TestData_ClassifiedMatchesNormalisedPaths`).
3. **`git commit -F msg.txt` whose file is not under cwd or is missing** must ask (the message cannot be checked) rather than allow or crash. Pinned in Task 5 (`TestCommitMessage_FileMissingAsks`).
4. **A push from a branch with no upstream and no `origin/HEAD`** has no range to scan: tq must give no opinion rather than block every first push, while a branch with an upstream is scanned. Pinned in Task 4 (`TestPushPII_UnresolvableRangeAllows`, `TestPushPII_DeniedForUnpushedCommits`).
5. **A repository with PII-looking columns only in test fixtures** should still get the doctor warning (fixtures are exactly where PII leaks), but a repository whose only hit is a `.env.example` key name (`EMAIL_FROM`) should not. Pinned in Task 7 (`TestDataBlockMissing_*`).

---

## File structure

| Path | Responsibility |
|---|---|
| `internal/manifest/manifest.go` | `Data`, `DataPath`, `Incident`, `Prompts` structs; validation in `Load`; `Data.Classified(path)`, `Data.Tables()`, `Data.Enabled()` |
| `internal/manifest/data_test.go` | manifest data tests |
| `internal/redact/redact.go`, `redact_test.go` | detectors (cpf, cnpj, email, phone, card, iban) with validation; `Classes`, `Redact(text, classes)`, `NewWriter` |
| `internal/cmd/redact.go`, `redact_test.go` | `tq redact [--classes] [--file]`, `tq redact run [--classes] -- CMD` |
| `internal/policy/builtin.go`, `policy_test.go` | regex rules `tq/iac-apply`, `tq/remote-migration`, `tq/settings-widen`, `tq/settings-shell-write`, `tq/gha-branch-ref`; `tq/gha-unpinned` narrowed |
| `internal/cmd/claudehook_policy.go`, `claudehook_pii.go`, `claudehook_pii_test.go` | code rules `tq/commit-pii`, `tq/push-pii`, `tq/text-pii`, `tq/pii-fixture-dump`, `tq/commit-message` wired into `policyDecision` |
| `internal/commitscan/commitscan.go` | `Added(dir, command, run)`: the lines a commit would add (shared by the secret and PII scans) |
| `internal/guardcfg/guardcfg.go` | `HookRules` entries for the three code rules |
| `internal/commitmsg/commitmsg.go`, `commitmsg_test.go` | Conventional Commits check of a `git commit` command |
| `internal/decide/triage.go`, `triage_test.go` | `agentic` question; `ClassifiedFlag(files, data)` |
| `internal/cmd/decide.go` | triage adds `classified_data` from `git diff --name-only` |
| `internal/doctor/data.go`, `data_test.go` | `dataBlockMissing` check |
| `docs/CLAUDE-HOOK.md`, `docs/DECIDE.md`, `docs/CLAUDE-SETTINGS.md`, `docs/SETUP.md` | docs |

---

### Task 1: Manifest `data:` block

**Files:**
- Modify: `internal/manifest/manifest.go`
- Create: `internal/manifest/data_test.go`

**Interfaces:**
- Produces:

```go
var DataClasses = []string{"public", "internal", "pii", "sensitive", "financial", "children", "secrets"}

type DataPath struct {
    Path    string   `yaml:"path,omitempty"`  // repo-relative glob, slash or backslash
    Table   string   `yaml:"table,omitempty"` // schema.table or table
    Classes []string `yaml:"classes"`
}
type Prompts  struct{ Allow []string `yaml:"allow"` }
type Incident struct{ Authority, Contact string }
type Data struct {
    Classes          []string   `yaml:"classes"`
    Regions          []string   `yaml:"regions"`
    Prompts          Prompts    `yaml:"prompts"`
    Paths            []DataPath `yaml:"paths"`
    RetentionDefault string     `yaml:"retention_default"`
    Incident         Incident   `yaml:"incident"`
}
func (d Data) Enabled() bool                       // len(Classes) > 0
func (d Data) Classified(path string) []string     // classes of every DataPath glob matching the slash-normalised repo-relative path; nil when none
func (d Data) Tables() map[string][]string         // lower-cased table name (with and without schema) -> classes
func (d Data) Validate() error
```
- `Manifest` gains `Data Data \`yaml:"data"\``; `Load` calls `m.Data.Validate()`.
- Glob matching: `**` matches any number of segments, `*` one segment, via a helper `globMatch(pattern, path string) bool` exported from the manifest package as `GlobMatch` (the doctor and hook reuse it).

- [ ] **Step 1: Write the failing test**

```go
// internal/manifest/data_test.go
package manifest

import (
	"reflect"
	"strings"
	"testing"
)

const dataManifest = `
schema: tentaqles-client-v2
client: acme
data:
  classes: [pii, financial]
  regions: [BR]
  prompts: { allow: [public, internal] }
  paths:
    - { path: "db/schema/customers.sql", classes: [pii] }
    - { path: "data\\exports\\**", classes: [pii, financial] }
    - { table: public.payments, classes: [financial] }
  retention_default: 24m
  incident: { authority: ANPD, contact: dpo@example.test }
`

func TestData_ParsesAndEnables(t *testing.T) {
	m, err := Load(write(t, dataManifest))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Data.Enabled() || m.Data.RetentionDefault != "24m" || m.Data.Incident.Authority != "ANPD" {
		t.Fatalf("%+v", m.Data)
	}
	if got := m.Data.Tables()["public.payments"]; !reflect.DeepEqual(got, []string{"financial"}) {
		t.Fatalf("tables %v", m.Data.Tables())
	}
	if got := m.Data.Tables()["payments"]; !reflect.DeepEqual(got, []string{"financial"}) {
		t.Fatalf("bare table name must resolve too: %v", m.Data.Tables())
	}
}

func TestData_ClassifiedMatchesNormalisedPaths(t *testing.T) {
	m, err := Load(write(t, dataManifest))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"db/schema/customers.sql":           {"pii"},
		`db\schema\customers.sql`:           {"pii"},
		"data/exports/2026/q3.csv":          {"financial", "pii"},
		"data/exports/x.csv":                {"financial", "pii"},
		"db/schema/orders.sql":              nil,
		"src/data/exports/x.csv":            nil, // anchored at the repo root
	}
	for p, want := range cases {
		if got := m.Data.Classified(p); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v", p, got, want)
		}
	}
}

func TestData_EmptyBlockIsDisabled(t *testing.T) {
	m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"))
	if err != nil || m.Data.Enabled() || m.Data.Classified("anything") != nil {
		t.Fatalf("%v %+v", err, m.Data)
	}
}

func TestData_Validation(t *testing.T) {
	bad := map[string]string{
		"unknown class":        "data: { classes: [pii, private] }",
		"allow not in set":     "data: { classes: [pii], prompts: { allow: [pii] } }",
		"path and table":       "data: { classes: [pii], paths: [ { path: a, table: b, classes: [pii] } ] }",
		"path without classes": "data: { classes: [pii], paths: [ { path: a } ] }",
		"bad retention":        "data: { classes: [pii], retention_default: forever }",
		"bad authority":        "data: { classes: [pii], incident: { authority: FBI } }",
		"region not ISO":       "data: { classes: [pii], regions: [Brazil] }",
	}
	for name, body := range bad {
		_, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"+body+"\n"))
		if err == nil || !strings.Contains(err.Error(), "data.") {
			t.Errorf("%s: want a data.* validation error, got %v", name, err)
		}
	}
	ok := "schema: tentaqles-client-v2\nclient: x\ndata: { classes: [pii], retention_default: 30d, regions: [BR, EU], incident: { authority: GDPR } }\n"
	if _, err := Load(write(t, ok)); err != nil {
		t.Fatal(err)
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"src/core/**", "src/core/a/b.go", true},
		{"src/core/**", "src/core", true},
		{"src/*/x.go", "src/a/x.go", true},
		{"src/*/x.go", "src/a/b/x.go", false},
		{"**/migrations/*.sql", "db/migrations/1.sql", true},
		{"*.sql", "db/1.sql", false},
	}
	for _, c := range cases {
		if got := GlobMatch(c.pat, c.path); got != c.want {
			t.Errorf("GlobMatch(%q,%q)=%v", c.pat, c.path, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/manifest/ -run 'TestData|TestGlobMatch' -v`
Expected: compile errors (`m.Data undefined`, `GlobMatch undefined`).

- [ ] **Step 3: Implement**

Append to `internal/manifest/manifest.go` (and add `Data Data \`yaml:"data"\`` to `Manifest`, plus the `Validate` call in `Load` right after `m.Decision.Validate()`):

```go
// DataClasses are the classification labels a manifest may use (spec §11.1).
var DataClasses = []string{"public", "internal", "pii", "sensitive", "financial", "children", "secrets"}

// DataPath classifies a repository path glob or a database table.
type DataPath struct {
	Path    string   `yaml:"path,omitempty"`
	Table   string   `yaml:"table,omitempty"`
	Classes []string `yaml:"classes"`
}

type Prompts struct {
	Allow []string `yaml:"allow"`
}

type Incident struct {
	Authority string `yaml:"authority"` // ANPD | GDPR | none
	Contact   string `yaml:"contact"`
}

// Data is the manifest's data-protection block. Empty means "not classified".
type Data struct {
	Classes          []string   `yaml:"classes"`
	Regions          []string   `yaml:"regions"`
	Prompts          Prompts    `yaml:"prompts"`
	Paths            []DataPath `yaml:"paths"`
	RetentionDefault string     `yaml:"retention_default"`
	Incident         Incident   `yaml:"incident"`
}

var (
	retentionRe = regexp.MustCompile(`^\d{1,4}[dmy]$`)
	regionRe    = regexp.MustCompile(`^[A-Z]{2}$`)
)

func (d Data) Enabled() bool { return len(d.Classes) > 0 }

// Validate rejects unknown classes, inconsistent paths and malformed values so
// a typo cannot silently leave data unclassified.
func (d Data) Validate() error {
	if !d.Enabled() && (len(d.Paths) > 0 || len(d.Prompts.Allow) > 0) {
		return fmt.Errorf("data.classes is required when data.paths or data.prompts are set")
	}
	for _, c := range d.Classes {
		if !contains(DataClasses, c) {
			return fmt.Errorf("data.classes: unknown class %q (known: %v)", c, DataClasses)
		}
	}
	for _, c := range d.Prompts.Allow {
		if c != "public" && c != "internal" && !contains(d.Classes, c) {
			return fmt.Errorf("data.prompts.allow: %q is not one of the declared classes", c)
		}
	}
	for i, p := range d.Paths {
		if (p.Path == "") == (p.Table == "") {
			return fmt.Errorf("data.paths[%d]: exactly one of path or table is required", i)
		}
		if len(p.Classes) == 0 {
			return fmt.Errorf("data.paths[%d]: classes is required", i)
		}
		for _, c := range p.Classes {
			if !contains(DataClasses, c) {
				return fmt.Errorf("data.paths[%d].classes: unknown class %q", i, c)
			}
		}
	}
	if d.RetentionDefault != "" && !retentionRe.MatchString(d.RetentionDefault) {
		return fmt.Errorf("data.retention_default: use <n>d, <n>m or <n>y, got %q", d.RetentionDefault)
	}
	for _, r := range d.Regions {
		if !regionRe.MatchString(r) {
			return fmt.Errorf("data.regions: %q is not a two-letter ISO country code", r)
		}
	}
	switch d.Incident.Authority {
	case "", "ANPD", "GDPR", "none":
	default:
		return fmt.Errorf("data.incident.authority must be ANPD, GDPR or none, got %q", d.Incident.Authority)
	}
	if LooksLikeSecret(d.Incident.Contact) {
		return fmt.Errorf("data.incident.contact looks like a secret")
	}
	return nil
}

// Classified returns the sorted, de-duplicated classes of every path glob
// matching the repo-relative path (slash or backslash), or nil.
func (d Data) Classified(path string) []string {
	rel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(path), `\`, "/"), "./")
	set := map[string]bool{}
	for _, p := range d.Paths {
		if p.Path == "" {
			continue
		}
		if GlobMatch(strings.ReplaceAll(p.Path, `\`, "/"), rel) {
			for _, c := range p.Classes {
				set[c] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Tables maps lower-cased table names (with and without their schema) to classes.
func (d Data) Tables() map[string][]string {
	out := map[string][]string{}
	for _, p := range d.Paths {
		if p.Table == "" {
			continue
		}
		name := strings.ToLower(strings.Trim(p.Table, `"`))
		out[name] = append(out[name], p.Classes...)
		if i := strings.LastIndex(name, "."); i >= 0 {
			out[name[i+1:]] = append(out[name[i+1:]], p.Classes...)
		}
	}
	return out
}

// GlobMatch matches a slash path against a glob where * is one segment and
// ** any number of segments (including none). Both are anchored at the root.
func GlobMatch(pattern, path string) bool {
	pattern = strings.Trim(pattern, "/")
	path = strings.Trim(path, "/")
	return globSegs(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func globSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if globSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := filepath.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}
```

Add `"path/filepath"`, `"sort"`, `"regexp"`, `"strings"` to the imports as needed (`regexp` and `strings` may already be there).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/manifest/ -v`
Expected: all pass. Note `GlobMatch("src/core/**", "src/core")` is true because `**` matches zero segments.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/manifest.go internal/manifest/data_test.go
git commit -m "feat(manifest): data classification block (classes, paths, tables, retention, incident authority)"
```

---

### Task 2: `internal/redact` detectors

**Files:**
- Create: `internal/redact/redact.go`, `internal/redact/redact_test.go`

**Interfaces:**
- Produces:

```go
// Classes selects detectors: "pii" (cpf, email, phone), "financial" (card, cnpj, iban),
// "secrets" (internal/secrets.Redact), "all". Unknown classes are an error from ParseClasses.
func ParseClasses(csv string) ([]string, error)
func Redact(text string, classes []string) string           // [REDACTED:<name>] per match
func Count(text string, classes []string) map[string]int    // findings per detector, no values
func NewWriter(w io.Writer, classes []string) *Writer        // line-buffered; Close flushes the tail
```

- Detectors: `cpf` (11 digits, optional `.`/`-` separators, check digits valid, not all-same digits), `cnpj` (14 digits with optional `./-`, check digits valid), `email` (RFC-ish local@domain.tld), `phone` (`+NN` E.164 up to 15 digits, or Brazilian `(DD) 9NNNN-NNNN` / `DD 9NNNNNNNN` forms), `card` (13 to 19 digits with optional spaces or dashes, Luhn valid, first digit 3 to 6), `iban` (two letters, two digits, 11 to 30 alphanumerics, mod-97 valid).

- [ ] **Step 1: Write the failing test**

```go
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
		if got := Redact("x "+v+" y", []string{"pii"}); strings.Contains(got, "[REDACTED:cpf]") {
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/redact/ -v`
Expected: `no Go files` / undefined errors.

- [ ] **Step 3: Implement**

```go
// Package redact masks personal and financial data (CPF, CNPJ, e-mail, phone,
// card numbers, IBAN) and secrets in text, by data class. Detection is
// heuristic but validated where a checksum exists (CPF and CNPJ check digits,
// Luhn for cards, mod-97 for IBAN), so plain numbers of the same length are
// left alone. Values are never logged; Count reports names only.
package redact

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/secrets"
)

type detector struct {
	name  string
	re    *regexp.Regexp
	valid func(string) bool
}

var (
	cpfRe   = regexp.MustCompile(`\b\d{3}\.?\d{3}\.?\d{3}-?\d{2}\b`)
	cnpjRe  = regexp.MustCompile(`\b\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2}\b`)
	emailRe = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	phoneRe = regexp.MustCompile(`(?:\+\d{1,3}[\s.-]?)?(?:\(\d{2,3}\)|\b\d{2,3})[\s.-]?\d{4,5}[\s.-]?\d{4}\b|\+\d{8,15}\b`)
	cardRe  = regexp.MustCompile(`\b[3-6](?:\d[ -]?){12,18}\b`)
	ibanRe  = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?:[ ]?[A-Z0-9]{4}){2,7}(?:[ ]?[A-Z0-9]{1,4})?\b`)
)

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteByte(byte(r))
		}
	}
	return b.String()
}

func allSame(d string) bool {
	for i := 1; i < len(d); i++ {
		if d[i] != d[0] {
			return false
		}
	}
	return true
}

func validCPF(s string) bool {
	d := digits(s)
	if len(d) != 11 || allSame(d) {
		return false
	}
	for _, n := range []int{9, 10} {
		sum := 0
		for i := 0; i < n; i++ {
			sum += int(d[i]-'0') * (n + 1 - i)
		}
		r := (sum * 10) % 11
		if r == 10 {
			r = 0
		}
		if r != int(d[n]-'0') {
			return false
		}
	}
	return true
}

func validCNPJ(s string) bool {
	d := digits(s)
	if len(d) != 14 || allSame(d) {
		return false
	}
	w1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	w2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	check := func(w []int, n int) bool {
		sum := 0
		for i, x := range w {
			sum += int(d[i]-'0') * x
		}
		r := sum % 11
		exp := 0
		if r >= 2 {
			exp = 11 - r
		}
		return exp == int(d[n]-'0')
	}
	return check(w1, 12) && check(w2, 13)
}

func luhn(s string) bool {
	d := digits(s)
	if len(d) < 13 || len(d) > 19 {
		return false
	}
	sum, alt := 0, false
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

func validIBAN(s string) bool {
	s = strings.ReplaceAll(strings.ToUpper(s), " ", "")
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	r := s[4:] + s[:4]
	mod := 0
	for _, c := range r {
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'A' && c <= 'Z':
			v = int(c-'A') + 10
		default:
			return false
		}
		if v >= 10 {
			mod = (mod*100 + v) % 97
		} else {
			mod = (mod*10 + v) % 97
		}
	}
	return mod == 1
}

func validPhone(s string) bool {
	d := digits(s)
	return len(d) >= 10 && len(d) <= 15 && !allSame(d)
}

var byClass = map[string][]detector{
	"pii": {
		{"cpf", cpfRe, validCPF},
		{"email", emailRe, func(string) bool { return true }},
		{"phone", phoneRe, validPhone},
	},
	"financial": {
		{"card", cardRe, luhn},
		{"cnpj", cnpjRe, validCNPJ},
		{"iban", ibanRe, validIBAN},
	},
}

// Known lists the selectable classes.
var Known = []string{"pii", "financial", "sensitive", "children", "secrets", "all"}

// ParseClasses parses a comma-separated list; "sensitive" and "children" use
// the pii detectors; "all" selects everything including secrets.
func ParseClasses(csv string) ([]string, error) {
	var out []string
	for _, c := range strings.Split(csv, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		ok := false
		for _, k := range Known {
			if k == c {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("unknown data class %q (known: %s)", c, strings.Join(Known, ", "))
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no data class given")
	}
	return out, nil
}

func detectors(classes []string) ([]detector, bool) {
	var ds []detector
	withSecrets := false
	seen := map[string]bool{}
	for _, c := range classes {
		switch c {
		case "all":
			return append(append([]detector{}, byClass["pii"]...), byClass["financial"]...), true
		case "secrets":
			withSecrets = true
		case "sensitive", "children":
			c = "pii"
		}
		for _, d := range byClass[c] {
			if !seen[d.name] {
				seen[d.name] = true
				ds = append(ds, d)
			}
		}
	}
	return ds, withSecrets
}

// Redact replaces every validated match with [REDACTED:<detector>].
func Redact(text string, classes []string) string {
	ds, withSecrets := detectors(classes)
	for _, d := range ds {
		text = d.re.ReplaceAllStringFunc(text, func(m string) string {
			if d.valid(m) {
				return "[REDACTED:" + d.name + "]"
			}
			return m
		})
	}
	if withSecrets {
		text = secrets.Redact(text)
	}
	return text
}

// Count reports findings per detector without the values.
func Count(text string, classes []string) map[string]int {
	out := map[string]int{}
	ds, withSecrets := detectors(classes)
	for _, d := range ds {
		for _, m := range d.re.FindAllString(text, -1) {
			if d.valid(m) {
				out[d.name]++
			}
		}
	}
	if withSecrets {
		for _, f := range secrets.Scan(text) {
			out[f.Pattern]++
		}
	}
	return out
}

// Writer masks a stream line by line; a partial last line is held until the
// next newline or Close, so a value split across writes is still caught.
type Writer struct {
	w       io.Writer
	classes []string
	buf     bytes.Buffer
}

func NewWriter(w io.Writer, classes []string) *Writer { return &Writer{w: w, classes: classes} }

func (m *Writer) Write(p []byte) (int, error) {
	m.buf.Write(p)
	for {
		i := bytes.IndexByte(m.buf.Bytes(), '\n')
		if i < 0 {
			return len(p), nil
		}
		line := m.buf.Next(i + 1)
		if _, err := io.WriteString(m.w, Redact(string(line), m.classes)); err != nil {
			return len(p), err
		}
	}
}

func (m *Writer) Close() error {
	if m.buf.Len() == 0 {
		return nil
	}
	_, err := io.WriteString(m.w, Redact(m.buf.String(), m.classes))
	m.buf.Reset()
	return err
}

var _ = bufio.NewWriter // keep bufio available for a buffered stdout in the CLI
```

(Remove the trailing `bufio` line if the CLI does not use it; keep imports clean.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/redact/ -v`
Expected: all pass. If `TestEmailAndPhone` flags `2026-10-20`, tighten `phoneRe` by requiring the first group to be preceded by `+`, `(` or a word boundary and the whole match to have 10 to 15 digits (`validPhone` already enforces the digit count; a date has 8 digits and fails it).

- [ ] **Step 5: Commit**

```bash
git add internal/redact
git commit -m "feat(redact): validated detectors for CPF, CNPJ, e-mail, phone, card and IBAN by data class"
```

---

### Task 3: `tq redact` command

**Files:**
- Create: `internal/cmd/redact.go`, `internal/cmd/redact_test.go`
- Modify: `internal/cmd/root.go` (register `newRedactCmd()`), `docs/CLAUDE-HOOK.md` (section "Using classified data without printing it")

**Interfaces:**
- `tq redact [--classes pii,financial] [--file F]...` reads stdin (or files) and writes masked text to stdout; `--count` prints findings per detector only.
- `tq redact run [--classes C] -- CMD [ARGS...]` runs CMD with stdout and stderr masked, propagating its exit code (same shape as `tq dotenv run`). Default classes: the trusted workspace manifest's `data.classes` minus `prompts.allow`, plus `secrets`; when no workspace or no block: `all`.

- [ ] **Step 1: Write the failing test**

```go
// internal/cmd/redact_test.go
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

func runRedact(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"redact"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestRedact_StdinDefaultAll(t *testing.T) {
	isolateHome(t)
	out, err := runRedact(t, "cpf 529.982.247-25 card 4111 1111 1111 1111\n")
	if err != nil || strings.Contains(out, "529") || !strings.Contains(out, "[REDACTED:cpf]") || !strings.Contains(out, "[REDACTED:card]") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestRedact_ClassesAndCount(t *testing.T) {
	isolateHome(t)
	out, _ := runRedact(t, "cpf 529.982.247-25 card 4111 1111 1111 1111\n", "--classes", "financial")
	if !strings.Contains(out, "529.982.247-25") || !strings.Contains(out, "[REDACTED:card]") {
		t.Fatalf("%q", out)
	}
	out, _ = runRedact(t, "cpf 529.982.247-25\n", "--count")
	if strings.Contains(out, "529") || !strings.Contains(out, "cpf: 1") {
		t.Fatalf("%q", out)
	}
	if _, err := runRedact(t, "x", "--classes", "bogus"); err == nil {
		t.Fatal("unknown class accepted")
	}
}

func TestRedact_FileAndRun(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	f := filepath.Join(dir, "export.csv")
	if err := os.WriteFile(f, []byte("ana@x.io,529.982.247-25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runRedact(t, "", "--file", f)
	if err != nil || strings.Contains(out, "ana@") {
		t.Fatalf("%q %v", out, err)
	}
	sh, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		sh, flag = "cmd", "/c"
	}
	out, err = runRedact(t, "", "run", "--classes", "pii", "--", sh, flag, "echo mail ana@x.io")
	if err != nil || strings.Contains(out, "ana@") || !strings.Contains(out, "[REDACTED:email]") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestRedact_DefaultClassesFromManifest(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "data:\n  classes: [financial]\n  prompts: { allow: [] }\n")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	out, _ := runRedact(t, "cpf 529.982.247-25 card 4111 1111 1111 1111 key ghp_abcdefghijklmnopqrstuvwxyz0123\n")
	// financial from the manifest, secrets always; pii is not declared, so the CPF stays
	if !strings.Contains(out, "529.982.247-25") || !strings.Contains(out, "[REDACTED:card]") || !strings.Contains(out, "[REDACTED:github_token]") {
		t.Fatalf("%q", out)
	}
}
```

Read `setupTrustedWorkspaceWithManifest` in `claudehook_test.go` to confirm it appends `body` to a valid manifest and trusts it; adjust the data YAML indentation to what it expects.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cmd/ -run TestRedact -v`
Expected: `unknown command "redact"`.

- [ ] **Step 3: Implement**

```go
// internal/cmd/redact.go
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// defaultRedactClasses are the manifest's declared classes minus prompts.allow,
// plus secrets; "all" outside a classified workspace.
func defaultRedactClasses(cwd string) []string {
	ws, _ := resolveTrusted(cwd)
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() {
		return []string{"all"}
	}
	allow := map[string]bool{}
	for _, c := range ws.Manifest.Data.Prompts.Allow {
		allow[c] = true
	}
	var out []string
	for _, c := range ws.Manifest.Data.Classes {
		if !allow[c] && c != "public" && c != "internal" {
			out = append(out, c)
		}
	}
	return append(out, "secrets")
}

func newRedactCmd() *cobra.Command {
	var classesFlag string
	var files []string
	var count bool
	c := &cobra.Command{
		Use:   "redact [--classes C] [--file F]... [--count]",
		Short: "Mask personal, financial and secret values in text (stdin or files) by data class",
		Long: `Reads stdin (or --file) and writes the text with CPF, CNPJ, e-mail, phone,
card and IBAN values (validated by checksum where one exists) and secrets
replaced by [REDACTED:<kind>]. Classes default to the workspace manifest's
data.classes minus data.prompts.allow, plus secrets; outside a classified
workspace every detector runs.

  tq redact --classes pii < export.csv
  tq redact run --classes pii,financial -- psql "$DATABASE_URL" -c "select * from customers limit 5"

Reading personal data locally is allowed; the guard refuses to record it in
commits, pushes and PR or issue text. Use this command to make a shareable
copy (a bug report, a PR body) without the values.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			classes, err := resolveClasses(classesFlag, cwd)
			if err != nil {
				return err
			}
			var in io.Reader = c.InOrStdin()
			if len(files) > 0 {
				var rs []io.Reader
				for _, f := range files {
					fh, err := os.Open(f)
					if err != nil {
						return err
					}
					defer fh.Close()
					rs = append(rs, fh)
				}
				in = io.MultiReader(rs...)
			}
			raw, err := io.ReadAll(in)
			if err != nil {
				return err
			}
			if count {
				n := redact.Count(string(raw), classes)
				keys := make([]string, 0, len(n))
				for k := range n {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(c.OutOrStdout(), "%s: %d\n", k, n[k])
				}
				if len(keys) == 0 {
					fmt.Fprintln(c.OutOrStdout(), "no findings")
				}
				return nil
			}
			_, err = io.WriteString(c.OutOrStdout(), redact.Redact(string(raw), classes))
			return err
		},
	}
	c.Flags().StringVar(&classesFlag, "classes", "", "comma-separated data classes: pii, financial, sensitive, children, secrets, all")
	c.Flags().StringArrayVar(&files, "file", nil, "file to read instead of stdin (repeatable)")
	c.Flags().BoolVar(&count, "count", false, "print findings per detector instead of the masked text")
	c.AddCommand(newRedactRunCmd())
	return c
}

func resolveClasses(flag, cwd string) ([]string, error) {
	if strings.TrimSpace(flag) != "" {
		return redact.ParseClasses(flag)
	}
	return defaultRedactClasses(cwd), nil
}

func newRedactRunCmd() *cobra.Command {
	var classesFlag string
	c := &cobra.Command{
		Use:   "run [--classes C] -- CMD [ARGS...]",
		Short: "Run a command with its stdout and stderr masked by data class",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			classes, err := resolveClasses(classesFlag, cwd)
			if err != nil {
				return err
			}
			path, err := exec.LookPath(args[0])
			if err != nil {
				return err
			}
			stdout := redact.NewWriter(c.OutOrStdout(), classes)
			stderr := redact.NewWriter(c.ErrOrStderr(), classes)
			cmd := exec.Command(path, args[1:]...)
			cmd.Stdin = c.InOrStdin()
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			runErr := cmd.Run()
			_ = stdout.Close()
			_ = stderr.Close()
			var exit *exec.ExitError
			if errors.As(runErr, &exit) {
				exitFunc(exit.ExitCode())
				return nil
			}
			return runErr
		},
	}
	c.Flags().StringVar(&classesFlag, "classes", "", "comma-separated data classes (default: from the manifest, plus secrets)")
	c.Flags().SetInterspersed(false)
	return c
}
```

Register in `root.go`: add `newRedactCmd()` to `root.AddCommand(...)`.

Docs: in `docs/CLAUDE-HOOK.md` after "Using a .env without printing it" add a section `## Using classified data without printing it` describing the manifest `data:` block (one example), `tq redact`, `tq redact run`, default classes, and the exit rules `tq/commit-pii`, `tq/push-pii`, `tq/text-pii` that point here.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cmd/ -run 'TestRedact' -v && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cmd/redact.go internal/cmd/redact_test.go internal/cmd/root.go docs/CLAUDE-HOOK.md
git commit -m "feat(tq): redact command and redact run (class-selected masking of command output)"
```

---

### Task 4: PII exit guards: commits, pushes, GitHub text, fixtures

**Files:**
- Modify: `internal/redact/redact.go`, `internal/redact/redact_test.go` (class `checksummed`, `AllowMarker`)
- Modify: `internal/commitscan/commitscan.go`, `internal/commitscan/commitscan_test.go` (`Added`)
- Create: `internal/cmd/claudehook_pii.go`, `internal/cmd/claudehook_pii_test.go`
- Modify: `internal/cmd/claudehook_policy.go` (`policyDecision`), `internal/guardcfg/guardcfg.go` (`HookRules`)

**User rule (2026-10-20):** Claude may read files and tables with personal data and run scripts over them. Nothing personal may be *registered or published*: memory (plugin plan), commits, pushes, PR and issue text, fixtures. These rules guard those exits.

**Interfaces:**
- `redact`: new class `checksummed` = detectors `cpf`, `cnpj`, `card`, `iban` (the ones a checksum validates), added to `Known` and `byClass`; `const AllowMarker = "tq:allow-pii"`; `Count` and `Redact` skip a line carrying the marker (same convention as `tq:allow-secret`).
- `commitscan.Added(dir, command string, run Runner) []AddedLine` with `AddedLine{File string; Line int; Text string}`: every added line the command would commit (staged diff, plus the working tree and untracked files when the command also runs `git add` or `commit -a`), using `PlanFor` and the same git invocations `Scan` uses; binary files skipped. `Scan` keeps its behaviour.
- `piiClasses(ws *resolve.Workspace) []string`: `["checksummed"]` when there is no workspace or no `data:` block; otherwise `checksummed` plus `pii` when the manifest declares `pii`, `sensitive` or `children`, plus `financial` when it declares `financial`.
- Code rules, all `Deny`, all in `guardcfg.HookRules`:
  - `tq/commit-pii`: a `git commit` whose added lines contain a validated PII or financial value (`redact.Count` per line, classes from `piiClasses`), unless the line carries `tq:allow-pii`. Reason lists `file:line (detector)`, never values.
  - `tq/push-pii`: a `git push` (not `--delete`, not `--dry-run`) whose unpushed commits contain such values in added lines. Range: `@{push}..HEAD`, then `@{upstream}..HEAD`, then `origin/HEAD..HEAD`; when none resolves, no opinion. A diff over 4 MiB is not scanned: `Ask` with the reason "too large to scan; push smaller batches or confirm".
  - `tq/text-pii`: message or body text that would leave the machine: `git commit -m/-F`, `git tag -m/-F`, `git notes add/append -m/-F`, and `gh pr|issue|release create|edit|comment|review` (the command text plus any `--body-file`, `-F`, `--notes-file`, `--file` file read from cwd, capped at 256 KiB). Reason names the detector and whether it was in the command or the file.
  - `tq/pii-fixture-dump` (unchanged from the spec): a shell command that names a classified table or path *and* writes into `tests/`, `test/`, `fixtures/`, `__fixtures__/`, `seeds/`, `seed/`, `testdata/` (redirect, `-o/--out/--output/--file`, `\copy … to`, `COPY … TO`, `pg_dump -f`, `cp`/`copy` target). Needs the `data:` block (no block, no rule). Synthetic generators are not matched (they do not name a classified source).

- [ ] **Step 1: Write the failing tests**

Append to `internal/redact/redact_test.go`:

```go
func TestChecksummedClassAndAllowMarker(t *testing.T) {
	text := "cpf 529.982.247-25 mail ana@x.io"
	got := Redact(text, []string{"checksummed"})
	if !strings.Contains(got, "[REDACTED:cpf]") || !strings.Contains(got, "ana@x.io") {
		t.Fatalf("checksummed must mask the CPF and leave the e-mail: %q", got)
	}
	if n := Count("row 529.982.247-25 # tq:allow-pii", []string{"all"}); len(n) != 0 {
		t.Fatalf("allow marker ignored: %v", n)
	}
	if got := Redact("row 529.982.247-25 # tq:allow-pii", []string{"all"}); strings.Contains(got, "REDACTED") {
		t.Fatalf("allow marker ignored in Redact: %q", got)
	}
}
```

Append to `internal/commitscan/commitscan_test.go` (read the file first for its existing git fixture helper and reuse it):

```go
func TestAdded_StagedAndChainedAdd(t *testing.T) {
	dir := initRepo(t) // the existing helper that creates a repo with one commit
	write(t, dir, "a.txt", "one\ntwo\n")
	runGit(t, dir, "add", "a.txt")
	write(t, dir, "b.txt", "three\n")
	added := Added(dir, `git commit -m "x"`, RunGit)
	if len(added) != 2 || added[0].File != "a.txt" || added[1].Line != 2 {
		t.Fatalf("staged only: %+v", added)
	}
	added = Added(dir, `git add b.txt && git commit -m "x"`, RunGit)
	files := map[string]bool{}
	for _, a := range added {
		files[a.File] = true
	}
	if !files["a.txt"] || !files["b.txt"] {
		t.Fatalf("chained add must include the untracked file: %+v", added)
	}
	if Added(dir, "git log", RunGit) != nil {
		t.Fatal("no commit, no lines")
	}
}
```

(`initRepo`, `write`, `runGit`, `RunGit` are whatever the existing test file and package already provide; match their names.)

`internal/cmd/claudehook_pii_test.go`:

```go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const piiData = "data:\n  classes: [pii, financial]\n  paths:\n    - { path: \"data/exports/**\", classes: [pii] }\n    - { table: public.customers, classes: [pii] }\n"

func gitRepo(t *testing.T, ws string) func(args ...string) string {
	t.Helper()
	run := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = ws
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return run
}

func stage(t *testing.T, ws, run func(...string) string, name, body string) {
	t.Helper()
	p := filepath.Join(ws, name)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", name)
}

func TestCommitPII_DeniedWithCPFEvenWithoutDataBlock(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	run := gitRepo(t, ws)
	stage(t, ws, run, "seed.sql", "insert into c values ('529.982.247-25');\n")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: seed"`))
	if code != 2 || !strings.Contains(errOut, "tq/commit-pii") || !strings.Contains(errOut, "seed.sql:1 (cpf)") || strings.Contains(errOut, "529") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestCommitPII_EmailOnlyWhenPiiDeclared(t *testing.T) {
	plain := setupTrustedWorkspaceWithManifest(t, "beta", "")
	run := gitRepo(t, plain)
	stage(t, plain, run, "users.csv", "ana.silva@example.com\n")
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, plain, `git commit -m "feat: users"`)); code != 0 {
		t.Fatalf("no data block: e-mail must not be scanned: %q", errOut)
	}
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run = gitRepo(t, ws)
	stage(t, ws, run, "users.csv", "ana.silva@example.com\n")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: users"`))
	if code != 2 || !strings.Contains(errOut, "(email)") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestCommitPII_AllowMarkerAndCleanCommit(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "tests/fixtures/people.sql", "insert into c values ('529.982.247-25'); -- synthetic, tq:allow-pii\n")
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "test: fixture"`)); code != 0 {
		t.Fatalf("allow marker: %q", errOut)
	}
	stage(t, ws, run, "src/a.go", "package a\n")
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: a"`)); code != 0 {
		t.Fatalf("clean commit: %q", errOut)
	}
}

func TestPushPII_DeniedForUnpushedCommits(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	remote := filepath.Join(testutilTempDir(t), "remote.git")
	exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).Run()
	run("remote", "add", "origin", remote)
	run("push", "-q", "-u", "origin", "main")
	stage(t, ws, run, "export.csv", "4111 1111 1111 1111\n")
	run("-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "feat: export")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push"))
	if code != 2 || !strings.Contains(errOut, "tq/push-pii") || !strings.Contains(errOut, "export.csv") || !strings.Contains(errOut, "(card)") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push --dry-run")); code != 0 {
		t.Fatal("dry run must not be scanned")
	}
}

func TestPushPII_UnresolvableRangeAllows(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "export.csv", "4111 1111 1111 1111\n")
	run("-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "feat: export")
	// no remote at all: tq has no range to scan and gives no opinion (the identity guard still applies)
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push -u origin main"))
	if strings.Contains(errOut, "tq/push-pii") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestTextPII_CommitMessageAndGhBody(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	gitRepo(t, ws)
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "fix: customer 529.982.247-25 could not log in"`))
	if code != 2 || !strings.Contains(errOut, "tq/text-pii") || !strings.Contains(errOut, "(cpf)") || strings.Contains(errOut, "529") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	body := filepath.Join(ws, "body.md")
	if err := os.WriteFile(body, []byte("Reported by ana.silva@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `gh pr create --title "fix: login" --body-file body.md`))
	if code != 2 || !strings.Contains(errOut, "tq/text-pii") || !strings.Contains(errOut, "body.md") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `gh pr create --title "fix: login" --body "no personal data here"`)); strings.Contains(errOut, "text-pii") {
		t.Fatalf("clean body denied: code=%d %q", code, errOut)
	}
}

func TestPiiFixtureDump_Denied(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`psql "$DATABASE_URL" -c "\copy (select * from customers limit 100) to 'tests/fixtures/customers.csv' csv"`,
		`pg_dump "$DATABASE_URL" -t public.customers --data-only -f seeds/customers.sql`,
		`psql -c "select * from customers" > tests/customers.txt`,
		`cp data/exports/q3.csv tests/fixtures/q3.csv`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 2 || !strings.Contains(errOut, "tq/pii-fixture-dump") || !strings.Contains(errOut, "synthetic") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
}

func TestPiiFixtureDump_AllowedCases(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`head -n 5 data/exports/q3.csv`,                                       // reading is allowed
		`psql "$DATABASE_URL" -c "select * from customers limit 5"`,           // querying is allowed
		`psql -c "select * from orders" > tests/orders.txt`,                   // unclassified table
		`pg_dump "$DATABASE_URL" -t customers -f backups/customers.sql`,       // not a fixture dir
		`python scripts/make_fixtures.py --faker --out tests/fixtures/`,       // synthetic generator
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 0 || strings.Contains(errOut, "pii") {
			t.Errorf("%s: wrongly denied (code=%d): %q", cmd, code, errOut)
		}
	}
	plain := setupTrustedWorkspaceWithManifest(t, "beta", "")
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, plain, `cp data/exports/q3.csv tests/fixtures/q3.csv`)); code != 0 {
		t.Fatal("workspace without a data block must not be affected by the fixture rule")
	}
}

func TestPIIRules_CanBeDisabledInManifest(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData+"guard:\n  disable: [tq/commit-pii]\n")
	run := gitRepo(t, ws)
	stage(t, ws, run, "seed.sql", "insert into c values ('529.982.247-25');\n")
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: seed"`)); code != 0 {
		t.Fatalf("disable by id must work: %q", errOut)
	}
}
```

`testutilTempDir` is `testutil.TempDir`; import `github.com/tentaqles/tentaqles/internal/testutil` and call it directly. The tests commit with `core.hooksPath=/dev/null` so a user-level hook never runs; on Windows use `NUL`-equivalent by setting `core.hooksPath` to an empty temp dir instead.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/redact/ ./internal/commitscan/ ./internal/cmd/ -run 'TestChecksummed|TestAdded|TestCommitPII|TestPushPII|TestTextPII|TestPiiFixtureDump|TestPIIRules' -v`
Expected: compile errors (`Added`, `AllowMarker` undefined) and allowed calls where a deny is expected.

- [ ] **Step 3: Implement**

`internal/redact/redact.go`: add `"checksummed": {{"cpf", cpfRe, validCPF}, {"cnpj", cnpjRe, validCNPJ}, {"card", cardRe, luhn}, {"iban", ibanRe, validIBAN}}` to `byClass`, add `"checksummed"` to `Known`, `const AllowMarker = "tq:allow-pii"`, and in both `Redact` and `Count` process the text line by line, leaving a line that contains `AllowMarker` untouched.

`internal/commitscan/commitscan.go`: add

```go
// AddedLine is one line a commit would add.
type AddedLine struct {
	File string
	Line int
	Text string
}

// Added returns every added line the command would commit, in the same scope
// Scan uses (staged diff; plus the working tree and untracked files when the
// command also runs git add or commit -a). Binary files are skipped.
func Added(dir, command string, run Runner) []AddedLine
```

implemented by factoring the diff-gathering loop out of `Scan` into a shared helper that yields `(file, line, text)` for every `+` hunk line and every line of an in-scope untracked file; `Scan` then calls the helper and applies `secrets.ScanLine`. Keep the existing `Scan` tests green.

`internal/cmd/claudehook_pii.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/redact"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Exit guards for personal data (spec §11.2, revised): reading and processing
// personal data locally is allowed; recording it in git or publishing it in
// PR and issue text is not. Values never appear in reasons.

const maxPushScan = 4 << 20
const maxTextFile = 256 << 10

func piiClasses(ws *resolve.Workspace) []string {
	out := []string{"checksummed"}
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() {
		return out
	}
	pii, fin := false, false
	for _, c := range ws.Manifest.Data.Classes {
		switch c {
		case "pii", "sensitive", "children":
			pii = true
		case "financial":
			fin = true
		}
	}
	if pii {
		out = append(out, "pii")
	}
	if fin {
		out = append(out, "financial")
	}
	return out
}

// hits formats findings as "file:line (detector)" without values.
func hits(lines []commitscan.AddedLine, classes []string) []string {
	var out []string
	for _, l := range lines {
		for name := range redact.Count(l.Text, classes) {
			out = append(out, fmt.Sprintf("%s:%d (%s)", l.File, l.Line, name))
		}
	}
	sort.Strings(out)
	if len(out) > 10 {
		out = append(out[:10], fmt.Sprintf("… and %d more", len(out)-10))
	}
	return out
}

func commitPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() || !commitscan.PlanFor(call.Command).Commits {
		return policy.Decision{}, false
	}
	h := hits(commitscan.Added(cwd, call.Command, gitcfg.RunGitIn), piiClasses(ws))
	if len(h) == 0 {
		return policy.Decision{}, false
	}
	return ruleDecision(action, "tq/commit-pii", "this commit would record personal or financial data: "+strings.Join(h, ", ")+
		". Keep real values out of the repository (synthetic fixtures, environment variables, external storage); mark a known-synthetic line with `tq:allow-pii`"), true
}

var pushDiffLine = regexp.MustCompile(`^\+\+\+ |^\+`)

func pushPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() {
		return policy.Decision{}, false
	}
	isPush := false
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(call.Command)) {
		if inv.Sub != "push" {
			continue
		}
		skip := false
		for _, a := range inv.Args {
			if a == "--delete" || a == "-d" || a == "--dry-run" || a == "-n" {
				skip = true
			}
		}
		if !skip {
			isPush = true
		}
	}
	if !isPush {
		return policy.Decision{}, false
	}
	var diff string
	for _, rng := range []string{"@{push}..HEAD", "@{upstream}..HEAD", "origin/HEAD..HEAD"} {
		out, err := gitcfg.RunGitIn(cwd, "diff", "--no-color", "--no-ext-diff", "-U0", rng)
		if err == nil {
			diff = out
			break
		}
	}
	if diff == "" {
		return policy.Decision{}, false // nothing to compare against: no opinion
	}
	if len(diff) > maxPushScan {
		return ruleDecision(policy.Ask, "tq/push-pii", "the unpushed diff is too large to scan for personal data; push smaller batches or confirm"), true
	}
	var lines []commitscan.AddedLine
	file := ""
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ b/"):
			file = strings.TrimPrefix(l, "+++ b/")
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			lines = append(lines, commitscan.AddedLine{File: file, Text: l[1:]})
		}
	}
	h := hits(lines, piiClasses(ws))
	if len(h) == 0 {
		return policy.Decision{}, false
	}
	// line numbers are not tracked across hunks here; the file and detector are enough
	for i := range h {
		h[i] = strings.Replace(h[i], ":0 ", " ", 1)
	}
	return ruleDecision(action, "tq/push-pii", "the commits about to be pushed record personal or financial data: "+strings.Join(h, ", ")+
		". Rewrite those commits locally (remove the values, amend or rebase) before pushing"), true
}

var ghTextRe = regexp.MustCompile(`(?i)\bgh\s+(pr|issue|release)\s+(create|edit|comment|review|merge|close)\b`)
var textFileFlag = regexp.MustCompile(`(?i)\s(--body-file|--notes-file|--file|-F)[\s=]+("([^"]+)"|'([^']+)'|(\S+))`)

func textPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() {
		return policy.Decision{}, false
	}
	relevant := ghTextRe.MatchString(call.Command)
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(call.Command)) {
		if inv.Sub == "commit" || inv.Sub == "tag" || inv.Sub == "notes" {
			relevant = true
		}
	}
	if !relevant {
		return policy.Decision{}, false
	}
	classes := piiClasses(ws)
	var where []string
	for name := range redact.Count(call.Command, classes) {
		where = append(where, "command text ("+name+")")
	}
	for _, m := range textFileFlag.FindAllStringSubmatch(call.Command, -1) {
		p := m[3] + m[4] + m[5]
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if len(raw) > maxTextFile {
			raw = raw[:maxTextFile]
		}
		for name := range redact.Count(string(raw), classes) {
			where = append(where, filepath.Base(p)+" ("+name+")")
		}
	}
	if len(where) == 0 {
		return policy.Decision{}, false
	}
	sort.Strings(where)
	return ruleDecision(action, "tq/text-pii", "this text would publish personal or financial data: "+strings.Join(where, ", ")+
		". Describe the case without the values (a ticket id, a role, a masked sample)"), true
}
```

Keep `piiFixtureDumpDecision` as specified in the previous revision of this task (manifest `classesOf` over paths and tables, write targets under fixture directories), in the same file, with signature `piiFixtureDumpDecision(ws, call, action policy.Action) (policy.Decision, bool)`.

Wire into `policyDecision` after the commit-secret block, each behind `!eff.Off(id)` and with `eff.ActionFor(id, policy.Deny)` passed as `action`:

```go
	for _, r := range []struct {
		id string
		fn func(policy.Action) (policy.Decision, bool)
	}{
		{"tq/commit-pii", func(a policy.Action) (policy.Decision, bool) { return commitPIIDecision(ws, call, cwd, a) }},
		{"tq/push-pii", func(a policy.Action) (policy.Decision, bool) { return pushPIIDecision(ws, call, cwd, a) }},
		{"tq/text-pii", func(a policy.Action) (policy.Decision, bool) { return textPIIDecision(ws, call, cwd, a) }},
		{"tq/pii-fixture-dump", func(a policy.Action) (policy.Decision, bool) { return piiFixtureDumpDecision(ws, call, a) }},
	} {
		if eff.Off(r.id) {
			continue
		}
		if pd, ok := r.fn(eff.ActionFor(r.id, policy.Deny)); ok {
			d = policy.Merge(d, pd)
		}
	}
```

`guardcfg.HookRules` entries:

```go
	"tq/commit-pii":       "a git commit that would record personal or financial data",
	"tq/push-pii":         "a git push whose commits record personal or financial data",
	"tq/text-pii":         "a commit, tag or GitHub PR/issue/release text that carries personal or financial data",
	"tq/pii-fixture-dump": "copying rows from classified data into tests/, fixtures/ or seeds/",
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/redact/ ./internal/commitscan/ ./internal/cmd/ ./internal/guardcfg/` then `go test ./...`
Expected: pass. The `tq/commit-secret` tests in `claudehook_policy_test.go` stay green (same scan scope).

- [ ] **Step 5: Commit**

```bash
git add internal/redact internal/commitscan internal/cmd/claudehook_pii.go internal/cmd/claudehook_pii_test.go internal/cmd/claudehook_policy.go internal/guardcfg/guardcfg.go
git commit -m "feat(guard): deny personal data at the exits (commits, pushes, PR and issue text, fixtures)"
```

---

### Task 5: Regex guard rules and the commit-message check

**Files:**
- Modify: `internal/policy/builtin.go`, `internal/policy/policy_test.go`
- Create: `internal/commitmsg/commitmsg.go`, `internal/commitmsg/commitmsg_test.go`
- Modify: `internal/cmd/claudehook_policy.go`, `internal/cmd/claudehook_policy_test.go`, `internal/guardcfg/guardcfg.go`

**Interfaces:**
- New regex rules (ids, actions): `tq/iac-apply` (Ask), `tq/remote-migration` (Ask), `tq/settings-widen` (Deny, Edit/Write/MultiEdit on settings files with widening content), `tq/settings-shell-write` (Deny, shell redirect or write verb targeting settings files), `tq/gha-branch-ref` (Deny, `uses: x@main|master`); `tq/gha-unpinned` keeps Ask for `@latest|@head` only.
- `commitmsg.Check(command, cwd string) (Result)` with `Result{Checked bool; OK bool; Why string}`; `Checked` false when the command has no `git commit` with `-m`/`-F` (editor, `--amend --no-edit`, `--fixup`, `--squash`, merge).
- Code rule `tq/commit-message` (Ask) in `policyDecision`, listed in `HookRules`.

- [ ] **Step 1: Write the failing tests**

Append to the `cases` table in `TestBuiltinDecisions` (`internal/policy/policy_test.go`):

```go
		// IaC apply and remote migrations
		{"terraform apply asks", ToolCall{Tool: "Bash", Command: "terraform apply -auto-approve"}, Ask, "tq/iac-apply"},
		{"pulumi up asks", ToolCall{Tool: "Bash", Command: "pulumi up --yes --stack prod"}, Ask, "tq/iac-apply"},
		{"az deployment asks", ToolCall{Tool: "Bash", Command: "az deployment group create -g rg -f main.bicep"}, Ask, "tq/iac-apply"},
		{"cfn deploy asks", ToolCall{Tool: "Bash", Command: "aws cloudformation deploy --stack-name x --template-file t.yml"}, Ask, "tq/iac-apply"},
		{"terraform plan ok", ToolCall{Tool: "Bash", Command: "terraform plan"}, Allow, ""},
		{"prisma migrate deploy asks", ToolCall{Tool: "Bash", Command: "npx prisma migrate deploy"}, Ask, "tq/remote-migration"},
		{"supabase db push linked asks", ToolCall{Tool: "Bash", Command: "supabase db push --linked"}, Ask, "tq/remote-migration"},
		{"alembic upgrade asks", ToolCall{Tool: "Bash", Command: "alembic upgrade head"}, Ask, "tq/remote-migration"},
		{"alembic upgrade localhost ok", ToolCall{Tool: "Bash", Command: "DATABASE_URL=postgres://u:p@localhost/db alembic upgrade head"}, Allow, ""},
		{"supabase db reset local ok", ToolCall{Tool: "Bash", Command: "supabase db reset --local"}, Allow, ""},
		// settings widening
		{"settings bash star denied", ToolCall{Tool: "Edit", Path: "/r/x/.claude/settings.json", Content: `"allow": ["Bash(*)"]`}, Deny, "tq/settings-widen"},
		{"settings bypass denied", ToolCall{Tool: "Write", Path: `C:\r\x\.claude\settings.local.json`, Content: `{"permissions": {"defaultMode": "bypassPermissions"}}`}, Deny, "tq/settings-widen"},
		{"settings disable hooks denied", ToolCall{Tool: "Edit", Path: "/r/x/.claude/settings.json", Content: `"disableAllHooks": true`}, Deny, "tq/settings-widen"},
		{"settings harmless edit ok", ToolCall{Tool: "Edit", Path: "/r/x/.claude/settings.json", Content: `"theme": "dark"`}, Allow, ""},
		{"settings shell redirect denied", ToolCall{Tool: "Bash", Command: `echo '{}' > .claude/settings.local.json`}, Deny, "tq/settings-shell-write"},
		{"settings powershell write denied", ToolCall{Tool: "PowerShell", Command: `Set-Content .claude\settings.json '{}'`}, Deny, "tq/settings-shell-write"},
		{"settings cat ok", ToolCall{Tool: "Bash", Command: "cat .claude/settings.json"}, Allow, ""},
		// actions pinned to branches
		{"uses main denied", ToolCall{Tool: "Edit", Path: "/r/.github/workflows/ci.yml", Content: "uses: actions/checkout@main"}, Deny, "tq/gha-branch-ref"},
		{"uses master denied", ToolCall{Tool: "Write", Path: "/r/.github/workflows/ci.yml", Content: "- uses: foo/bar@master"}, Deny, "tq/gha-branch-ref"},
		{"uses latest asks", ToolCall{Tool: "Edit", Path: "/r/.github/workflows/ci.yml", Content: "uses: foo/bar@latest"}, Ask, "tq/gha-unpinned"},
		{"uses sha ok", ToolCall{Tool: "Edit", Path: "/r/.github/workflows/ci.yml", Content: "uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2"}, Allow, ""},
```

`internal/commitmsg/commitmsg_test.go`:

```go
package commitmsg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheck(t *testing.T) {
	cases := []struct {
		name, cmd string
		checked   bool
		ok        bool
	}{
		{"conventional", `git commit -m "feat(api): add export endpoint"`, true, true},
		{"conventional no scope", `git commit -m 'fix: handle empty email'`, true, true},
		{"breaking with body", `git commit -m "feat!: drop v1" -m "BREAKING CHANGE: v1 endpoints removed"`, true, true},
		{"breaking without body", `git commit -m "feat!: drop v1"`, true, false},
		{"free text", `git commit -m "updated stuff"`, true, false},
		{"capitalised type", `git commit -m "Feat: x"`, true, false},
		{"unknown type", `git commit -m "feature: x"`, true, false},
		{"chained with add", `git add a.go && git commit -m "refactor(core): split loader"`, true, true},
		{"amend no edit", `git commit --amend --no-edit`, false, true},
		{"fixup", `git commit --fixup HEAD~1`, false, true},
		{"editor", `git commit`, false, true},
		{"not a commit", `git log --oneline`, false, true},
		{"attribution trailer ok", "git commit -m \"docs: readme\" -m \"Co-Authored-By: X <x@y.z>\"", true, true},
	}
	for _, c := range cases {
		r := Check(c.cmd, t.TempDir())
		if r.Checked != c.checked || r.OK != c.ok {
			t.Errorf("%s: got checked=%v ok=%v (%s)", c.name, r.Checked, r.OK, r.Why)
		}
	}
}

func TestCommitMessage_FileMissingAsks(t *testing.T) {
	r := Check(`git commit -F /nonexistent/msg.txt`, t.TempDir())
	if !r.Checked || r.OK {
		t.Fatalf("missing -F file must be checked and not ok: %+v", r)
	}
}

func TestCheck_FileMessage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "msg.txt")
	if err := os.WriteFile(p, []byte("fix(guard): close the gap\n\nwhy here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := Check("git commit -F msg.txt", dir); !r.Checked || !r.OK {
		t.Fatalf("%+v", r)
	}
	if r := Check("git commit --file=msg.txt", dir); !r.Checked || !r.OK {
		t.Fatalf("%+v", r)
	}
}
```

Append to `internal/cmd/claudehook_policy_test.go`:

```go
func TestPolicy_CommitMessageAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "did things"`))
	if code != 0 || !strings.Contains(askReason(t, out), "tq/commit-message") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat(x): did things"`))
	if code != 0 || strings.Contains(out, "commit-message") {
		t.Fatalf("conventional message must pass: %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/policy/ ./internal/commitmsg/ ./internal/cmd/ -run 'TestBuiltinDecisions|TestCheck|TestCommitMessage|TestPolicy_CommitMessage' -v`
Expected: policy cases fail (Allow where Ask/Deny expected); commitmsg does not compile.

- [ ] **Step 3: Implement**

`internal/policy/builtin.go`: add inside `Builtin()`:

```go
		// --- infrastructure and remote databases ----------------------------------
		{
			ID: "tq/iac-apply", Action: Ask,
			Command: `\b(terraform\s+apply|tofu\s+apply|pulumi\s+up|cdk\s+deploy|cdktf\s+deploy|az\s+deployment\s+\S+\s+create|aws\s+cloudformation\s+deploy|gcloud\s+deployment-manager\s+deployments\s+(create|update)|sam\s+deploy|serverless\s+deploy|sls\s+deploy|kubectl\s+apply|helm\s+(install|upgrade))\b`,
			Reason:  "applies infrastructure changes to a live environment; confirm the target environment is the one named in the command (workspace, stack, resource group or kube context) and that a plan was reviewed",
		},
		{
			ID: "tq/remote-migration", Action: Ask,
			Command: `\b(prisma\s+migrate\s+deploy|alembic\s+upgrade|flyway\s+migrate|liquibase\s+update|dbmate\s+up|goose\s+up|knex\s+migrate:latest|sqitch\s+deploy|dotnet\s+ef\s+database\s+update|supabase\s+db\s+push|supabase\s+migration\s+up|drizzle-kit\s+(push|migrate)|atlas\s+migrate\s+apply|rails\s+db:migrate|php\s+artisan\s+migrate|manage\.py\s+migrate)\b`,
			Except:  `\b(localhost|127\.0\.0\.1|--local)\b`,
			Reason:  "runs database migrations against a connection that is not local; confirm the target database and that a backup or rollback path exists (see db-migration)",
		},
		// --- Claude Code settings ---------------------------------------------------
		{
			ID: "tq/settings-widen", Action: Deny,
			Tool:    "Edit|MultiEdit|Write",
			Path:    `(^|/)(\.claude/settings(\.local)?\.json|\.claude\.json|settings(\.local)?\.json)$`,
			Content: `Bash\(\*\)|bypassPermissions|"disableAllHooks"\s*:\s*true|"hooks"\s*:\s*\{\s*\}|"skipDangerousModePermissionPrompt"\s*:\s*true`,
			Reason:  "this would widen permissions or switch hooks off from inside the session; permission changes go through `tq settings render` and the manifest",
		},
		{
			ID: "tq/settings-shell-write", Action: Deny,
			Command: `(>{1,2}\s*|\b(tee|cp|mv|sed\s+-i|set-content|add-content|out-file|copy-item|move-item)\b` + seg + `)\S*\.claude[/\\](settings(\.local)?\.json|tq-rules\.yaml)\b|(>{1,2}\s*|\b(tee|cp|mv|set-content|out-file)\b` + seg + `)\S*[/\\]\.claude\.json\b`,
			Reason:  "Claude Code settings are not written from the shell; use `tq settings render` (permissions) or the Edit tool, which the guard checks",
		},
```

Change the existing `tq/gha-unpinned` content regex to `uses:\s*[^\s@#]+@(latest|head)\b` and add after it:

```go
		{
			ID: "tq/gha-branch-ref", Action: Deny,
			Tool:    "Edit|MultiEdit|Write",
			Path:    `(^|/)\.github/workflows/[^/]+\.ya?ml$`,
			Content: `uses:\s*[^\s@#]+@(main|master)\b`,
			Reason:  "a GitHub Action pinned to a moving branch runs whatever is pushed there; pin a full commit SHA with the version in a comment",
		},
```

`internal/commitmsg/commitmsg.go`:

```go
// Package commitmsg checks the message a `git commit` command will record
// against Conventional Commits (type(scope)!: summary; a breaking change
// needs a body or a BREAKING CHANGE footer).
package commitmsg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
)

// Result of one check. Checked is false when the command carries no message
// tq can read (editor, --amend --no-edit, --fixup, --squash, no git commit).
type Result struct {
	Checked bool
	OK      bool
	Why     string
}

var (
	headerRe = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^()\s]+\))?(!)?: \S.*$`)
	maxFile  = 64 << 10
)

// Check inspects command. cwd resolves a relative -F file.
func Check(command, cwd string) Result {
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(command)) {
		if inv.Sub != "commit" {
			continue
		}
		var msgs []string
		var file string
		skip := false
		args := inv.Args
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--amend", a == "--no-edit", strings.HasPrefix(a, "--fixup"), strings.HasPrefix(a, "--squash"), a == "-C", a == "-c", a == "--reuse-message", a == "--reedit-message":
				skip = true
			case a == "-m" || a == "--message":
				if i+1 < len(args) {
					msgs = append(msgs, strings.Trim(args[i+1], `"'`))
					i++
				}
			case strings.HasPrefix(a, "-m"):
				msgs = append(msgs, strings.Trim(a[2:], `"'`))
			case strings.HasPrefix(a, "--message="):
				msgs = append(msgs, strings.Trim(strings.TrimPrefix(a, "--message="), `"'`))
			case a == "-F" || a == "--file":
				if i+1 < len(args) {
					file = strings.Trim(args[i+1], `"'`)
					i++
				}
			case strings.HasPrefix(a, "--file="):
				file = strings.Trim(strings.TrimPrefix(a, "--file="), `"'`)
			}
		}
		if skip && len(msgs) == 0 && file == "" {
			return Result{}
		}
		var text string
		switch {
		case file != "":
			if !filepath.IsAbs(file) {
				file = filepath.Join(cwd, file)
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return Result{Checked: true, OK: false, Why: "the -F message file could not be read; write it inside the repository first"}
			}
			if len(raw) > maxFile {
				raw = raw[:maxFile]
			}
			text = string(raw)
		case len(msgs) > 0:
			text = strings.Join(msgs, "\n\n")
		default:
			return Result{} // editor: nothing to check here
		}
		return judge(text)
	}
	return Result{}
}

func judge(text string) Result {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	header := strings.TrimSpace(lines[0])
	m := headerRe.FindStringSubmatch(header)
	if m == nil {
		return Result{Checked: true, OK: false, Why: "the summary is not Conventional Commits: `type(scope): imperative summary` with type one of feat fix docs style refactor perf test build ci chore revert, lower-case"}
	}
	if len(header) > 100 {
		return Result{Checked: true, OK: false, Why: "the summary line is over 100 characters"}
	}
	breaking := m[3] == "!" || strings.Contains(text, "BREAKING CHANGE:")
	if breaking {
		body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		if body == "" {
			return Result{Checked: true, OK: false, Why: "a breaking change (`!` or BREAKING CHANGE) needs a body that says what breaks and how to migrate (a MAJOR release note)"}
		}
	}
	return Result{Checked: true, OK: true}
}
```

Confirm `guard.GitInvocations` and `guard.StripHeredocs` exist and are exported (they are used by `commitscan.PlanFor`); if `Args` keeps quotes, the `strings.Trim` above handles them.

Wire into `policyDecision` after the fixture-dump block:

```go
	if call.IsShell() && !eff.Off("tq/commit-message") {
		if r := commitmsg.Check(call.Command, cwd); r.Checked && !r.OK {
			d = policy.Merge(d, ruleDecision(eff.ActionFor("tq/commit-message", policy.Ask), "tq/commit-message", "commit message: "+r.Why))
		}
	}
```

Add to `guardcfg.HookRules`: `"tq/commit-message": "a git commit whose message is not Conventional Commits"`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/policy/ ./internal/commitmsg/ ./internal/cmd/ ./internal/guardcfg/`
Expected: pass. The existing `ci-pipeline` skill text says tq *asks* on `@main`; the plugin plan updates that sentence.

- [ ] **Step 5: Commit**

```bash
git add internal/policy/builtin.go internal/policy/policy_test.go internal/commitmsg internal/cmd/claudehook_policy.go internal/cmd/claudehook_policy_test.go internal/guardcfg/guardcfg.go
git commit -m "feat(guard): iac-apply, remote-migration, settings-widen, gha-branch-ref rules and the commit-message check"
```

---

### Task 6: Triage flags `classified_data` and `agentic`

**Files:**
- Modify: `internal/decide/triage.go`, `internal/decide/triage_test.go` (create if absent)
- Modify: `internal/cmd/decide.go`, `internal/cmd/decide_triage_test.go` (create)
- Modify: `docs/DECIDE.md` (Review triage section)

**Interfaces:**
- `TriageQuestions()` gains `"agentic": "Does this change add or modify LLM calls, prompts, agent tool definitions, MCP servers, RAG or vector indexing, or Claude Code hooks or skills?"`.
- `func ClassifiedFlag(files []string, data manifest.Data) bool` in `internal/decide` (true when any file is classified).
- `tq decide triage` computes the diff file list with the same `--staged`/`--range` selection (`git diff --name-only ...`), appends `classified_data` to `Flags` and sets `Risk: "high"` when the flag fires, even when Jev is off or unreachable.

- [ ] **Step 1: Write the failing tests**

```go
// internal/decide/triage_test.go (append or create)
package decide

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func TestTriageQuestions_IncludeAgentic(t *testing.T) {
	qs := TriageQuestions()
	if _, ok := qs["agentic"]; !ok || len(qs) != 12 {
		t.Fatalf("want 12 questions incl. agentic, got %d", len(qs))
	}
}

func TestClassifiedFlag(t *testing.T) {
	d := manifest.Data{Classes: []string{"pii"}, Paths: []manifest.DataPath{{Path: "db/schema/**", Classes: []string{"pii"}}}}
	if !ClassifiedFlag([]string{"README.md", "db/schema/customers.sql"}, d) {
		t.Fatal("classified file not flagged")
	}
	if ClassifiedFlag([]string{"README.md"}, d) || ClassifiedFlag([]string{"db/schema/x.sql"}, manifest.Data{}) {
		t.Fatal("false positive")
	}
}
```

```go
// internal/cmd/decide_triage_test.go
package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTriage_ClassifiedDataFlagWithoutJev(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "data:\n  classes: [pii]\n  paths:\n    - { path: \"db/schema/**\", classes: [pii] }\n")
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = ws
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.MkdirAll(filepath.Join(ws, "db", "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "db", "schema", "customers.sql"), []byte("create table customers(email text);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "db/schema/customers.sql")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	var code int
	prevExit := exitFunc
	exitFunc = func(c int) { code = c; panic(hookExitSentinel{}) }
	t.Cleanup(func() { exitFunc = prevExit })
	var out bytes.Buffer
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"decide", "triage", "--staged"})
	func() {
		defer func() { _ = recover() }()
		_ = root.Execute()
	}()
	if code != 3 || !strings.Contains(out.String(), "classified_data") {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/decide/ ./internal/cmd/ -run 'TestTriageQuestions|TestClassifiedFlag|TestTriage_Classified' -v`
Expected: FAIL (11 questions; `ClassifiedFlag` undefined; triage prints `high ... [jev unavailable]` without the flag).

- [ ] **Step 3: Implement**

In `triage.go` add the question to `TriageQuestions()`:

```go
		"agentic":     q("Does this change add or modify LLM calls, prompts, agent tool definitions, MCP servers, RAG or vector indexing, or Claude Code hooks or skills?"),
```

and the helper:

```go
// ClassifiedFlag reports whether any changed file lies under a path the
// manifest's data: block classifies. Deterministic: no Jev call.
func ClassifiedFlag(files []string, data manifest.Data) bool {
	if !data.Enabled() {
		return false
	}
	for _, f := range files {
		if data.Classified(f) != nil {
			return true
		}
	}
	return false
}
```

(import `github.com/tentaqles/tentaqles/internal/manifest`; check for an import cycle: `manifest` imports `decide` for `Policy`. If so, move `ClassifiedFlag` to `internal/cmd/decide.go` as an unexported helper taking `manifest.Data`, and keep the decide test for the question count only.)

In `newDecideTriageCmd` (`internal/cmd/decide.go`), after computing `diff` and before the Jev call:

```go
			nameArgs := append([]string{"diff", "--name-only"}, args[2:]...)
			names, _ := gitcfg.RunGitIn(cwd, nameArgs...)
			var changed []string
			for _, l := range strings.Split(names, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					changed = append(changed, l)
				}
			}
			classified := false
			if ws, _ := resolveTrusted(cwd); ws != nil && ws.Manifest != nil {
				classified = decide.ClassifiedFlag(changed, ws.Manifest.Data)
			}
```

and after `res` is set (both the error and success branches):

```go
			if classified {
				res.Flags = append(res.Flags, "classified_data")
				sort.Strings(res.Flags)
				res.Risk = "high"
			}
```

Also move the `if strings.TrimSpace(diff) == ""` early return so that an empty diff still prints `low (empty diff)` (unchanged). Update the command's `Long` text to list `classified data path (from the manifest, no Jev)` and `agentic code` among the questions.

In `docs/DECIDE.md` "Review triage": the list becomes 12 Jev questions plus the deterministic `classified_data` flag; mention that the flags map to the reviewer crew (`security-reviewer`, `privacy-reviewer`, `llm-security-reviewer`) in the build skill.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/decide/ ./internal/cmd/ -run 'Triage|Classified' -v && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/decide/triage.go internal/decide/triage_test.go internal/cmd/decide.go internal/cmd/decide_triage_test.go docs/DECIDE.md
git commit -m "feat(decide): triage flags classified data paths deterministically and asks about agentic code"
```

---

### Task 7: Doctor check `data-block-missing`

**Files:**
- Create: `internal/doctor/data.go`, `internal/doctor/data_test.go`
- Modify: `internal/doctor/doctor.go` (call per trusted workspace), `docs/CLAUDE-SETTINGS.md` (table row)

**Interfaces:**
- `func dataBlockMissing(ws, root string, data manifest.Data, add func(level, code, ws, msg, fix string))`: when `!data.Enabled()`, walks `root` (skipping `.git`, `node_modules`, `vendor`, `.venv`, `dist`, `build`, at most 3000 files, each at most 256 KiB) looking at schema-like files (`*.sql`, `*.prisma`, `*.dbml`, `schema.rb`, `models.py`, `*.graphql`, `*.proto`, and anything under a `migrations` directory) for column-like identifiers matching `(?i)\b(email|e_mail|cpf|cnpj|rg|ssn|passport|phone|telefone|celular|birth_?date|data_nascimento|dob|address|endereco|cep|zip_?code|card_?number|iban)\b`; emits one `warn` `data-block-missing` naming the count of files and the first three, fix `tq classify: run /tentaqles:classify-data to write the data: block, then tq allow <ws>`. `.env*` files and anything not schema-like are not scanned.

- [ ] **Step 1: Write the failing test**

```go
// internal/doctor/data_test.go
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
	fn(func(level, code, ws, msg, fix string) { out = append(out, Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix}) })
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
	fs := collect(func(add func(string, string, string, string, string)) { dataBlockMissing("acme", root, manifest.Data{}, add) })
	if len(fs) != 1 || fs[0].Code != "data-block-missing" || !strings.Contains(fs[0].Msg, "customers.sql") || !strings.Contains(fs[0].Fix, "classify-data") {
		t.Fatalf("%+v", fs)
	}
}

func TestDataBlockMissing_FixturesCount(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, "tests/fixtures/people.sql", "insert into people (email, phone) values ('a@b.c', '1');\n")
	fs := collect(func(add func(string, string, string, string, string)) { dataBlockMissing("acme", root, manifest.Data{}, add) })
	if len(fs) != 1 {
		t.Fatalf("fixtures are where PII leaks; want a finding, got %+v", fs)
	}
}

func TestDataBlockMissing_EnvExampleAndPlainCodeIgnored(t *testing.T) {
	root := testutil.TempDir(t)
	write(t, root, ".env.example", "EMAIL_FROM=noreply@example.test\n")
	write(t, root, "src/mailer.go", "func send(email string) {}\n")
	fs := collect(func(add func(string, string, string, string, string)) { dataBlockMissing("acme", root, manifest.Data{}, add) })
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/doctor/ -run TestDataBlockMissing -v`
Expected: `undefined: dataBlockMissing`.

- [ ] **Step 3: Implement**

```go
// internal/doctor/data.go
package doctor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

const (
	dataScanMaxFiles = 3000
	dataScanMaxBytes = 256 << 10
)

var (
	piiColumnRe   = regexp.MustCompile(`(?i)\b(email|e_mail|cpf|cnpj|rg|ssn|passport|phone|telefone|celular|birth_?date|data_nascimento|dob|address|endereco|cep|zip_?code|card_?number|iban)\b`)
	schemaFileRe  = regexp.MustCompile(`(?i)(\.sql|\.prisma|\.dbml|\.graphql|\.proto)$|(^|/)(schema\.rb|models\.py)$`)
	dataSkipDirs  = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true, "dist": true, "build": true, ".next": true, "target": true}
)

// dataBlockMissing warns when a workspace has PII-looking columns in schema or
// fixture files but no data: block in its manifest (spec §11.1). It never
// reads .env files or application code.
func dataBlockMissing(ws, root string, data manifest.Data, add func(level, code, ws, msg, fix string)) {
	if data.Enabled() {
		return
	}
	var hits []string
	seen := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if dataSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if seen > dataScanMaxFiles {
			return filepath.SkipAll
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root))
		rel = strings.TrimPrefix(rel, "/")
		inMigrations := strings.Contains("/"+rel, "/migrations/")
		if !schemaFileRe.MatchString(rel) && !inMigrations {
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.Size() > dataScanMaxBytes {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if piiColumnRe.Match(raw) {
			hits = append(hits, rel)
		}
		return nil
	})
	if len(hits) == 0 {
		return
	}
	shown := hits
	if len(shown) > 3 {
		shown = shown[:3]
	}
	add("warn", "data-block-missing", ws,
		fmt.Sprintf("%d schema or fixture file(s) name personal-data columns (%s) and the manifest has no data: block", len(hits), strings.Join(shown, ", ")),
		"run /tentaqles:classify-data to write the data: block (classes, paths, retention, incident authority), then tq allow "+ws)
}
```

In `doctor.go`'s per-workspace loop, after `memoryStale(...)`: `if trusted { dataBlockMissing(w.Name, w.Root, w.Manifest.Data, add) }`.

In `docs/CLAUDE-SETTINGS.md` doctor table add: `| data-block-missing | schema or fixture files name personal-data columns (email, cpf, phone, …) and the manifest has no data: block; run /tentaqles:classify-data |`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/doctor/ -v && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/data.go internal/doctor/data_test.go internal/doctor/doctor.go docs/CLAUDE-SETTINGS.md
git commit -m "feat(doctor): warn when PII-looking columns exist without a manifest data block"
```

---

### Task 8: Docs for the new rules and verification

**Files:**
- Modify: `docs/CLAUDE-HOOK.md` (Built-in rules table and the code-rules list), `docs/SETUP.md` (one paragraph on the `data:` block pointing to CLAUDE-HOOK.md)

- [ ] **Step 1: Update the rules table**

Add rows: `tq/iac-apply` ask; `tq/remote-migration` ask (localhost/`--local` exempt); `tq/settings-widen`, `tq/settings-shell-write` deny; `tq/gha-branch-ref` deny (`@main`/`@master`), `tq/gha-unpinned` now ask on `@latest`/`@head` only. Extend the code-rules list with `tq/commit-pii`, `tq/push-pii`, `tq/text-pii` (deny; personal and financial data at the exits; `tq:allow-pii` marks synthetic lines; checksum detectors always, e-mail and phone when the manifest declares `pii`), `tq/pii-fixture-dump` (deny; synthetic fixtures), `tq/commit-message` (ask; Conventional Commits; `!` needs a body). State plainly that reading and querying personal data locally is allowed. Add a short `## Data classification (data: block)` section with the YAML example from spec §11.1 and the sentence "changing the manifest changes its trust hash: run `tq allow <ws>`".

- [ ] **Step 2: Verify**

Run: `go test ./... && go vet ./...`
Expected: green.

- [ ] **Step 3: Commit**

```bash
git add docs/CLAUDE-HOOK.md docs/SETUP.md
git commit -m "docs(tq): data classification block, redact, and the new guard rules"
```

---

## Self-review notes

- Spec coverage: §11.1 (Task 1, 7), §11.2 exits (Tasks 2 to 4), §11.3 fixtures (Task 4), §8.2 rules (Tasks 4, 5; `tq/config-change` as settings rules, `tq/actions-unpinned` as `gha-branch-ref`), §9 triage questions (Task 6), `tq doctor` warning from §11.1 (Task 7).
- Not in this plan: reviewer agents, templates, `classify-data`, `privacy-review`, `threat-model`, `supply-chain`, `llm-security-review` skills and the plugin-side DoD `privacy` check hookup: the plugin plan.
- Type consistency: `manifest.Data`, `Data.Classified`, `Data.Tables`, `GlobMatch`, `redact.ParseClasses/Redact/Count/NewWriter`, `decide.ClassifiedFlag`, rule ids as listed; `ruleDecision(action, id, reason)` order as in the existing code.
