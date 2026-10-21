# tq hub core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship release 2, spec A of the development process: `tq hub`, a local per-workspace board inside the `tq` binary that runs agent-ready cards headless through the release-1 build engine, mirrors the engine's decisions, lets the human answer them from a web page, the CLI or the terminal (signed with a hub actor token), builds a review page from gate records only, enforces per-run budgets, and logs masked activity that `tq insights` reports; plus the plugin changes that let a hub-launched session carry its card (`task` block, headless worktree hand-off, `/tentaqles:hub`, `ship` comment, preamble count).

**Architecture:** New Go packages under `internal/hub/`: `store` (one SQLite database per client workspace at `$TQ_HOME/hub/<workspace>/hub.db`, masking every text at write), `activity` (append-only `activity.jsonl` plus the reader `internal/insights` calls), `adapter` (the `Adapter` contract, a `process` adapter and the `claude -p --output-format json` adapter), `gate` (runs the plugin's `build-gate.py` through `tq_run.sh`, mints the hub actor token byte-identically to `gate.py`), `runner` (queue, checkout, rounds, budgets, outcome classification, the human actions, decisions mirroring), `notify` (bounded desktop notification), `serveinfo` (which hub serves which workspace on which port) and `web` (embedded templates, plain JavaScript, JSON API, SSE, review page, static export). `internal/cmd/hub.go` and `hub_serve.go` wire `tq hub …`; every command talks to a running hub over its API, or to the store directly when none serves. The plugin gains a validated `task` block in the gate's plan schema and a `--records` state export the review page reads, the headless `TQ_HUB_RELOCATE` hand-off line in the build skill, the `/tentaqles:hub` skill, the `ship` PR comment and a 300 ms preamble read.

**Tech Stack:** Go 1.26.7, cobra, yaml.v3, `modernc.org/sqlite` (pure Go, no cgo), `net/http` (Go 1.22 method/wildcard patterns), `html/template`, `embed`; Python 3.10+ standard library for the plugin and the test fakes, pytest; Markdown skills with YAML frontmatter.

**Spec:** `docs/design/2026-10-21-tq-hub-core-design.md`

All paths are relative to the monorepo root `C:\repos\tentaqles\tentaqles`. Go commands run from that root in PowerShell or Git Bash (`go test ./internal/hub/... -count=1`). Plugin tests run as `cd plugin; python -m pytest tests/<file> -q` (when bare `python` is not the right interpreter: `source scripts/tq_env.sh && "$TENTAQLES_PY" -m pytest ...` from Git Bash). Go tests that need Python (the fake `claude` and the fake `build-gate.py`) find it with `testutil.Python(t)` and skip when none is installed.

## Global Constraints

- SQLite only through `modernc.org/sqlite` (driver name `sqlite`, pure Go, no cgo); it is the only new `go.mod` dependency. HTTP, templates, SSE, HMAC and JSON come from the standard library (spec §4).
- Storage per workspace under `$TQ_HOME/hub/<workspace>/`: `hub.db` (WAL), `runs/<run-id>.log` (adapter stdout and stderr, masked), `activity.jsonl` (append-only, masked). Directories `0700`, files `0600`. Nothing is written inside a client repository except what the engine already writes (spec §4).
- Default bind `127.0.0.1:7470`. A non-loopback bind requires a token (`--token`, or one generated and printed once); its SHA-256 is stored as the `web_token_hash` setting. POST requests also need the same-origin check (spec §4, §9.3).
- Status vocabulary, one token set everywhere: `queued running waiting finished failed cancelled over-budget` (spec §6, §11).
- Lanes (default rows): `backlog` (kind backlog), `todo` (todo), `in progress` (active), `review` (review), `done` (done); lane kinds `backlog todo active review done other`. Card kinds `project feature fix analysis hotfix chore`; `agent-ready` is the only tag with behaviour (spec §6).
- Default budget per run: 2,000,000 tokens, 10 USD, 120 minutes, 12 rounds. Tokens count input + output + cache write, like the overnight loop. Default concurrency 2 running adapters per workspace (spec §6, §8.5).
- Ids: `c-`, `r-` and hub-owned `d-` followed by 12 random base32 characters (`a-z2-7`); decisions mirrored from the gate keep the gate's own id (spec §6).
- Hub token = HMAC-SHA256 hex over the compact ASCII JSON `["hub",action,slug,actor]` (Python `json.dumps(..., ensure_ascii=True, separators=(",", ":"))`), key = the raw bytes of `$TQ_HOME/build-gate/hmac.key`, `action` = `answer`, actor `hub:<os user name>`. The key is read only inside the answer path and zeroed after use (spec §9.2, §15).
- `TQ_HUB_TASK` is the JSON `{"hub":"<card id>","workspace":"<name>","kind":"<card kind>","external":null}` (keys in that order). The relocate line is the last line of the final text: `TQ_HUB_RELOCATE <absolute worktree path> <slug>` (spec §13.1, §13.4).
- Every text the hub stores (card titles and bodies, comments, decision text and notes, stop reasons, review page content, run logs, activity data) is masked with `redact.Redact(text, classes)` where `classes` = the workspace manifest's `data.classes` plus `secrets`, or `all` when the workspace is not classified (fail closed). Use `internal/redact` directly; never shell out to `tq redact` (spec §1, §15).
- Bounded calls: the session preamble's hub read is bounded to 300 ms; skill calls to `tq hub` pass `--timeout 2s`; desktop notifications are bounded to 2 s and never retried; gate CLI calls are bounded (60 s); hooks never wait on the network ([[hooks-must-never-stall]]).
- Human-only: the hub only ever calls `build-gate answer` (never `approve`, `finish`, `judge`, `dod --override`, `unlock-tests`). A request marked as coming from an agent session (`TENTAQLES_HEADLESS=1`, `CLAUDE_PLUGIN_ROOT` or `CLAUDECODE=1` in the CLI's environment, or the `X-TQ-Hub-Agent: 1` API header) cannot answer a decision, approve or request changes, move a card to a `done` lane, or set a budget; it may add cards and comment (spec §15).
- Runs never carry `TQ_BUILD_ACTOR`/`TQ_BUILD_TOKEN` and never have `TENTAQLES_HEADLESS_CHILD` set; they get `TENTAQLES_HEADLESS=1` and `TQ_HUB_TASK`. `--dangerously-skip-permissions` appears only through the existing `claudeArgs` bypass path (`tq allow --bypass`) (spec §8.3, §15).
- Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE` (all three, in every test that touches state), clear `TENTAQLES_HEADLESS`, `CLAUDE_PLUGIN_ROOT`, `CLAUDECODE`, `TQ_HUB_TASK`, `TQ_BUILD_ACTOR` and `TQ_BUILD_TOKEN`, and use Windows-safe paths (`filepath.Join`, never hand-built `/` paths for the file system).
- `go vet ./...`, `go test ./... -count=1` and `python -m pytest plugin/tests -q` stay green after every task.
- Conventional commits; stage explicit paths only (never `git add -A` or `git add .`); git identity for this repo is `reach@tentaqles.ai` (check `git config user.email` before the first commit).
- Plugin entries go under `## [Unreleased]` in `plugin/CHANGELOG.md`; no version bump in this plan.

## Review Focus

1. **A run that prints the relocate line twice.** A second `TQ_HUB_RELOCATE` for a slug the run already relocated for must end the run `failed` with `stop_reason: relocate loop`, release the lock and start no further round; when one final text carries two relocate lines only the last line counts. Pinned in Task 7 (`TestClassifyRelocateLoop`, `TestParseRelocateOnlyLastLine`) and Task 8 (`TestRunnerRelocateLoopStops`).
2. **`--all` with an untrusted workspace.** It is listed as unavailable with `run: tq allow <name>`, its `hub.db` is never created or opened, every page and API call for it answers 403, and `/waiting` skips it. Pinned in Task 13 (`TestServeAllUntrustedNeverOpened`).
3. **A decision answered in the terminal while the hub also answers it.** The gate refuses the hub's call (`no pending decision`); the hub must re-read the state, mirror the terminal's answer (actor `terminal`), report "already answered in the terminal" instead of an error, and enqueue exactly one resume. Pinned in Task 9 (`TestAnswerRaceWithTerminal`).
4. **Cost missing from the `claude` JSON.** With usage and a priced model the cost comes from `insights.CostUSD`; with neither, the round is recorded with cost 0 and `cost_unknown`, a system comment says the dollar budget could not count it, and the rounds and minutes budgets still stop the run. Pinned in Task 5 (`TestClaudeAdapterCostFallback`, `TestClaudeAdapterNoUsageNoCost`) and Task 8 (`TestBudgetRoundsStopsWithoutCost`).
5. **A card moved out of `active` mid-run.** A queued run is cancelled with a system comment; a running or waiting run makes the move fail (CLI: needs `--cancel-run`; page and API: 409, the page asks for confirmation) and the card stays where it was; with the confirmation the process is terminated, the run is `cancelled` and the lock released. Pinned in Task 9 (`TestMoveOutOfActiveQueuedCancels`, `TestMoveOutOfActiveRunningNeedsCancelRun`), Task 10 (`TestAPIMoveRunningNeedsConfirm`) and Task 12 (`TestCLIMoveRunningNeedsCancelRun`).

---

## File structure

| Path | Responsibility |
|---|---|
| `internal/hub/store/store.go`, `schema.go`, `ids.go`, `settings.go` | open/migrate the per-workspace database, change notifications, masking, ids, settings and budgets |
| `internal/hub/store/cards.go`, `frontmatter.go`, `log.go` | cards, lanes, comments, attachments, parent completion, atomic checkout, activity rows |
| `internal/hub/store/runs.go`, `decisions.go` | runs (budget, usage, round log), decision mirror rows, hub-owned decisions, lock report |
| `internal/hub/activity/activity.go` | `activity.jsonl` append/read and the aggregate `Report` |
| `internal/insights/report.go`, `render.go` (modify) | `Options.HubDir`, `Report.Hub`, the "Hub" text section |
| `internal/hub/adapter/adapter.go`, `process.go`, `claude.go`, `terminate_unix.go`, `terminate_windows.go` | the contract, the plain-process adapter, the `claude` adapter, launch environment, termination |
| `internal/hub/adapter/testdata/fake_claude.py` | platform-neutral fake `claude -p --output-format json` |
| `internal/testutil/python.go` | find a working Python 3 or skip |
| `internal/hub/gate/gate.go`, `token.go`, `plugin.go`, `testdata/*` | `build-gate state/answer` client, token minting, plugin and bash discovery, fake gate, token vectors |
| `internal/hub/runner/classify.go`, `testdata/states/*.json` | outcome classification table and relocate parsing |
| `internal/hub/notify/notify.go` | one bounded desktop notification per platform |
| `internal/hub/runner/service.go`, `prompt.go`, `rounds.go` | the service: queue, coalescing, checkout, rounds, budgets, retries, cancel, read views |
| `internal/hub/runner/human.go` | answers (gate and hub-owned), terminal-answer polling, review approve/request changes/mark done, moves |
| `internal/hub/serveinfo/serveinfo.go` | `serve-<port>.json` records and the liveness ping |
| `internal/hub/web/*.go`, `templates/*.html`, `static/hub.css`, `static/hub.js` | pages, API, auth, SSE, markdown subset, client, review page, export, multi-workspace backend |
| `internal/registry/registry.go` (modify) | keep `hub.mode`/`hub.port` from `config.yaml` across `Save` |
| `internal/cmd/hub.go`, `hub_service.go`, `hub_serve.go`, `root.go` (modify) | the `tq hub` command tree |
| `internal/doctor/hub.go`, `doctor.go` (modify) | `hub-db-unreadable`, `hub-stale-lock`, `hub-port-busy` |
| `test/integration/hub_test.go` | end to end with the fake `claude` and fake gate |
| `plugin/tentaqles/workflow/gate.py` (modify) | `task` schema + env check, approval snapshot, `state --records` |
| `plugin/agents/build-planner.md`, `plugin/skills/{build,feature,fix,kickoff,ship}/SKILL.md` (modify), `plugin/skills/hub/SKILL.md` (new) | task block, relocate hand-off, hub skill, PR comment |
| `plugin/scripts/session-preamble.py` (modify) | bounded hub count line |
| `plugin/tests/test_hub_task_block.py`, `test_hub_skill.py`, `test_hub_preamble.py` | plugin tests |
| `docs/HUB.md`, `docs/INSIGHTS.md`, `CLAUDE.md`, `plugin/CHANGELOG.md`, `plugin/README.md`, `plugin/CLAUDE.md` | docs |

## Execution lanes

Tasks compile against the interfaces of earlier tasks, so the Go lane runs in this order: 1 → 2 and 3 (parallel) → 4 → 5 and 6 (parallel) → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 16. The plugin lane (14, 15) touches only `plugin/` files and runs in parallel with the whole Go lane; 14 and 15 touch disjoint files and can run in parallel with each other. Task 16 runs last. Every task commits only the paths it lists (`git add <paths>`), so parallel implementers in one worktree never stage each other's files. Every test file belongs to exactly one task.

---
### Task 1: Store foundation (schema, migrations, ids, masking, settings)

**Files:**
- Modify: `go.mod`, `go.sum` (add `modernc.org/sqlite`)
- Create: `internal/hub/store/store.go`, `internal/hub/store/schema.go`, `internal/hub/store/ids.go`, `internal/hub/store/settings.go`
- Test: `internal/hub/store/store_test.go`, `internal/hub/store/settings_test.go`

**Interfaces:**
- Consumes: `paths.Home() string`, `redact.Redact(text string, classes []string) string`.
- Produces (package `store`, import `github.com/tentaqles/tentaqles/internal/hub/store`):
  - Paths: `Dir(ws) string`, `DBPath(ws) string`, `RunsDir(ws) string`, `ActivityPath(ws) string`, `ValidWorkspace(name string) error`.
  - `type Store struct { DB *sql.DB; Workspace string }` (plus unexported fields); `Open(ws string, classes []string) (*Store, error)` (creates the layout, migrates), `OpenExisting(ws string, classes []string) (*Store, error)` (returns `ErrNoHub` when `hub.db` is missing, never creates), `OpenReadOnly(ws string) (*Store, error)` (no migration, no write: for `tq doctor`), `(*Store).Close() error`, `SchemaVersion() (int, error)`.
  - Masking: `(*Store).Mask(text string) string`, `(*Store).Classes() []string`.
  - Changes: `type Change struct { Workspace, Kind, CardID, RunID string }` (JSON `workspace`, `kind`, `card`, `run`); `(*Store).Subscribe() (<-chan Change, func())`, `(*Store).Publish(Change)`, `(*Store).ChangeStamp() (string, error)` (changes when any process wrote).
  - `Now() string` (UTC RFC 3339), `ErrNoHub`, `ErrNotFound`.
  - Ids: `NewID(prefix string) string` (`"c"`, `"r"`, `"d"` → `c-` + 12 of `a-z2-7`), `ValidID(prefix, id string) bool`.
  - Vocabulary: `StatusQueued`…`StatusOverBudget`, `Statuses []string`; `CardKinds []string`; `LaneBacklog`, `LaneTodo`, `LaneActive`, `LaneReview`, `LaneDone`, `LaneOther`, `LaneKinds []string`; `TagAgentReady = "agent-ready"`.
  - Settings: `type Budget struct { Tokens int64; USD float64; Minutes float64; Rounds int }` (JSON `tokens usd minutes rounds`), `DefaultBudget`, `DefaultConcurrency = 2`, `ParseBudget(spec string, base Budget) (Budget, error)`, `(Budget).String() string`; setting keys `SettingConcurrency`, `SettingDefaultBudget`, `SettingNotify`, `SettingWebTokenHash`, `SettingProcessCommand`; `(*Store).Setting(key) (string, bool, error)`, `SetSetting(key, value string) error`, `Concurrency() int`, `DefaultBudget() Budget`, `Notify() bool`.

- [ ] **Step 1: Add the dependency**

Run: `go get modernc.org/sqlite@latest`
Expected: `go.mod` gains a `require modernc.org/sqlite vX.Y.Z` line and its indirect dependencies (`modernc.org/libc`, `modernc.org/mathutil`, `modernc.org/memory`, …). It must not add any cgo package. `go mod tidy` runs at Step 5, once a file imports it.

- [ ] **Step 2: Write the failing tests**

`internal/hub/store/store_test.go`:

```go
package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// isolate points TQ_HOME, HOME and USERPROFILE at a temp dir: no test touches the real home.
func isolate(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func mustOpen(t *testing.T, ws string) *Store {
	t.Helper()
	s, err := Open(ws, []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesLayoutAndMigrates(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	for _, p := range []string{DBPath("acme"), RunsDir("acme")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	var mode string
	if err := s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q, %v", mode, err)
	}
	if v, err := s.SchemaVersion(); err != nil || v != len(migrations) {
		t.Fatalf("schema version %d, %v", v, err)
	}
	rows, err := s.DB.Query("SELECT name, kind FROM lanes ORDER BY position")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var n, k string
		if err := rows.Scan(&n, &k); err != nil {
			t.Fatal(err)
		}
		got = append(got, n+"="+k)
	}
	rows.Close()
	want := []string{"backlog=backlog", "todo=todo", "in progress=active", "review=review", "done=done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lanes %v, want %v", got, want)
	}
}

func TestOpenTwiceIsIdempotent(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2 := mustOpen(t, "acme")
	var n int
	if err := s2.DB.QueryRow("SELECT COUNT(*) FROM lanes").Scan(&n); err != nil || n != 5 {
		t.Fatalf("lanes after reopen: %d, %v", n, err)
	}
}

func TestNewerSchemaIsRefused(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE schema_version SET version = 99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open("acme", []string{"all"}); err == nil || !strings.Contains(err.Error(), "newer tq") {
		t.Fatalf("want a newer-tq error, got %v", err)
	}
}

func TestWorkspaceNameIsValidated(t *testing.T) {
	isolate(t)
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "../x", "c:", " acme"} {
		if _, err := Open(bad, []string{"all"}); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestOpenExistingNeverCreates(t *testing.T) {
	isolate(t)
	if _, err := OpenExisting("ghost", []string{"all"}); !errors.Is(err, ErrNoHub) {
		t.Fatalf("want ErrNoHub, got %v", err)
	}
	if _, err := os.Stat(Dir("ghost")); !os.IsNotExist(err) {
		t.Fatalf("OpenExisting created %s", Dir("ghost"))
	}
	if _, err := OpenReadOnly("ghost"); !errors.Is(err, ErrNoHub) {
		t.Fatalf("OpenReadOnly: want ErrNoHub, got %v", err)
	}
}

func TestOpenReadOnlyDoesNotWrite(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	ro, err := OpenReadOnly("acme")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err := ro.DB.Exec("INSERT INTO settings(key, value) VALUES ('x', 'y')"); err == nil {
		t.Fatal("a read-only store accepted a write")
	}
}

func TestMaskUsesWorkspaceClasses(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"pii", "secrets"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	in := "mail ana@example.com token ghp_" + strings.Repeat("a", 36)
	got := s.Mask(in)
	if strings.Contains(got, "ana@example.com") || strings.Contains(got, "ghp_aaaa") {
		t.Fatalf("not masked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED:") {
		t.Fatalf("no redaction marker: %q", got)
	}
}

func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID("c")
		if !ValidID("c", id) || len(id) != 14 {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
	for _, bad := range []string{"c-ABCDEFGHIJKL", "c-abc", "r-abcdefghijkl", "c-abcdefghijk1"} {
		if ValidID("c", bad) {
			t.Fatalf("%q accepted as a card id", bad)
		}
	}
}

func TestSubscribeReceivesPublishedChanges(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	ch, cancel := s.Subscribe()
	defer cancel()
	s.Publish(Change{Kind: "card", CardID: "c-abcdefghijkl"})
	c := <-ch
	if c.Workspace != "acme" || c.Kind != "card" || c.CardID != "c-abcdefghijkl" {
		t.Fatalf("change %+v", c)
	}
}

func TestChangeStampMovesOnWrite(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	before, err := s.ChangeStamp()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO activity(ts, workspace, actor, verb, data) VALUES ('2026-10-21T10:00:00Z','acme','x','card.create','{}')`); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ChangeStamp()
	if before == after {
		t.Fatalf("stamp did not move: %q", after)
	}
}
```

`internal/hub/store/settings_test.go`:

```go
package store

import (
	"strings"
	"testing"
)

func TestSettingsDefaults(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if s.Concurrency() != 2 {
		t.Fatalf("concurrency %d", s.Concurrency())
	}
	if s.DefaultBudget() != (Budget{Tokens: 2_000_000, USD: 10, Minutes: 120, Rounds: 12}) {
		t.Fatalf("budget %+v", s.DefaultBudget())
	}
	if !s.Notify() {
		t.Fatal("notify defaults to true")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if err := s.SetSetting(SettingConcurrency, "3"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(SettingDefaultBudget, `{"tokens":100,"usd":1,"minutes":5,"rounds":2}`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(SettingNotify, "false"); err != nil {
		t.Fatal(err)
	}
	if s.Concurrency() != 3 || s.DefaultBudget().Rounds != 2 || s.Notify() {
		t.Fatalf("settings not applied: %d %+v %v", s.Concurrency(), s.DefaultBudget(), s.Notify())
	}
	if err := s.SetSetting(SettingConcurrency, "0"); err != nil {
		t.Fatal(err)
	}
	if s.Concurrency() != DefaultConcurrency {
		t.Fatal("a concurrency below 1 falls back to the default")
	}
}

func TestParseBudget(t *testing.T) {
	b, err := ParseBudget("tokens=500_000, usd=2.5,minutes=30,rounds=4", DefaultBudget)
	if err != nil {
		t.Fatal(err)
	}
	if b != (Budget{Tokens: 500000, USD: 2.5, Minutes: 30, Rounds: 4}) {
		t.Fatalf("%+v", b)
	}
	partial, err := ParseBudget("usd=3", DefaultBudget)
	if err != nil || partial.USD != 3 || partial.Tokens != DefaultBudget.Tokens {
		t.Fatalf("partial %+v %v", partial, err)
	}
	for _, bad := range []string{"tokens", "tokens=-1", "usd=abc", "dollars=3", "rounds=0"} {
		if _, err := ParseBudget(bad, DefaultBudget); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if got := (Budget{Tokens: 10, USD: 1.5, Minutes: 2, Rounds: 3}).String(); got != "tokens=10,usd=1.5,minutes=2,rounds=3" {
		t.Fatalf("String() = %q", got)
	}
	if !strings.Contains(DefaultBudget.String(), "tokens=2000000") {
		t.Fatal(DefaultBudget.String())
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/hub/store/ -count=1`
Expected: FAIL, build errors such as `undefined: Open`, `undefined: migrations`, `undefined: Budget`.

- [ ] **Step 4: Implement the store foundation**

`internal/hub/store/store.go`:

```go
// Package store is the hub's per-workspace SQLite database: schema,
// migrations, queries and masking at write. There is one database per client
// workspace under $TQ_HOME/hub/<workspace>/, never inside a repository, and
// no query ever joins two workspaces.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/redact"
	_ "modernc.org/sqlite" // pure-Go driver "sqlite"; no cgo
)

var (
	// ErrNoHub means the workspace has no hub.db yet.
	ErrNoHub = errors.New("no hub database for this workspace")
	// ErrNotFound wraps every missing card, run, lane or decision.
	ErrNotFound = errors.New("not found")
)

func Dir(workspace string) string          { return filepath.Join(paths.Home(), "hub", workspace) }
func DBPath(workspace string) string       { return filepath.Join(Dir(workspace), "hub.db") }
func RunsDir(workspace string) string      { return filepath.Join(Dir(workspace), "runs") }
func ActivityPath(workspace string) string { return filepath.Join(Dir(workspace), "activity.jsonl") }

// ValidWorkspace refuses names that would escape $TQ_HOME/hub/.
func ValidWorkspace(name string) error {
	if name == "" || name == "." || name == ".." || strings.TrimSpace(name) != name || strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("invalid workspace name %q", name)
	}
	return nil
}

// Change is one store mutation, fanned out to subscribers (the web page's SSE stream).
type Change struct {
	Workspace string `json:"workspace"`
	Kind      string `json:"kind"` // card | comment | run | decision | activity | external
	CardID    string `json:"card,omitempty"`
	RunID     string `json:"run,omitempty"`
}

type Store struct {
	DB        *sql.DB
	Workspace string
	classes   []string
	readOnly  bool

	mu      sync.Mutex
	subs    map[int]chan Change
	nextSub int
	logMu   sync.Mutex // serializes activity.jsonl appends from this process
}

func dsn(path string) string {
	return path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
}

// fileURI is the SQLite URI form of an absolute path (needed for mode=ro).
func fileURI(path string) string {
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + p
	}
	return "file://" + p
}

// Open opens (creating when needed) the workspace's database and migrates it.
func Open(workspace string, classes []string) (*Store, error) {
	if err := ValidWorkspace(workspace); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(RunsDir(workspace), 0o700); err != nil {
		return nil, err
	}
	return open(workspace, classes)
}

// OpenExisting is Open for a database that must already exist.
func OpenExisting(workspace string, classes []string) (*Store, error) {
	if err := ValidWorkspace(workspace); err != nil {
		return nil, err
	}
	if _, err := os.Stat(DBPath(workspace)); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHub
		}
		return nil, err
	}
	return open(workspace, classes)
}

// OpenReadOnly opens an existing database without migrating or writing (tq doctor).
func OpenReadOnly(workspace string) (*Store, error) {
	if err := ValidWorkspace(workspace); err != nil {
		return nil, err
	}
	if _, err := os.Stat(DBPath(workspace)); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHub
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", fileURI(DBPath(workspace))+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, Workspace: workspace, classes: []string{"all"}, readOnly: true, subs: map[int]chan Change{}}
	if _, err := s.SchemaVersion(); err != nil {
		db.Close()
		return nil, fmt.Errorf("hub database %s: %w", DBPath(workspace), err)
	}
	return s, nil
}

func open(workspace string, classes []string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn(DBPath(workspace)))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if len(classes) == 0 {
		classes = []string{"all"}
	}
	s := &Store{DB: db, Workspace: workspace, classes: append([]string(nil), classes...), subs: map[int]chan Change{}}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("hub database %s: %w", DBPath(workspace), err)
	}
	_ = os.Chmod(DBPath(workspace), 0o600)
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
	return s.DB.Close()
}

// Mask applies the workspace's data classes; every stored text goes through it.
func (s *Store) Mask(text string) string {
	if text == "" {
		return text
	}
	return redact.Redact(text, s.classes)
}

func (s *Store) Classes() []string { return append([]string(nil), s.classes...) }

func (s *Store) Subscribe() (<-chan Change, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan Change, 64)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c, ok := s.subs[id]; ok {
			close(c)
			delete(s.subs, id)
		}
	}
}

// Publish never blocks: a slow subscriber misses a change and refreshes on the next one.
func (s *Store) Publish(c Change) {
	c.Workspace = s.Workspace
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- c:
		default:
		}
	}
}

// ChangeStamp changes whenever any process (a CLI writing directly) changed cards, runs or activity.
func (s *Store) ChangeStamp() (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT (SELECT COALESCE(MAX(rowid), 0) FROM activity) || ':' ||
		(SELECT COALESCE(MAX(updated_at), '') FROM cards) || ':' || (SELECT COALESCE(MAX(updated_at), '') FROM runs)`).Scan(&v)
	return v, err
}

// Now is the timestamp format of every column: UTC RFC 3339.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

func (s *Store) SchemaVersion() (int, error) {
	var v sql.NullInt64
	if err := s.DB.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	v, err := s.SchemaVersion()
	if err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("hub.db is from a newer tq (schema %d, this tq knows %d); upgrade tq", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.DB.Begin() // _txlock=immediate: a second process waits here
		if err != nil {
			return err
		}
		var cur sql.NullInt64
		if err := tx.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&cur); err != nil {
			tx.Rollback()
			return err
		}
		if int(cur.Int64) > i { // another process applied it meanwhile
			tx.Rollback()
			continue
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// nullStr stores "" as NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

`internal/hub/store/schema.go`:

```go
package store

const (
	StatusQueued     = "queued"
	StatusRunning    = "running"
	StatusWaiting    = "waiting"
	StatusFinished   = "finished"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
	StatusOverBudget = "over-budget"
)

// Statuses is the one status vocabulary used by the store, the CLI and the page.
var Statuses = []string{StatusQueued, StatusRunning, StatusWaiting, StatusFinished, StatusFailed, StatusCancelled, StatusOverBudget}

var CardKinds = []string{"project", "feature", "fix", "analysis", "hotfix", "chore"}

const (
	LaneBacklog = "backlog"
	LaneTodo    = "todo"
	LaneActive  = "active"
	LaneReview  = "review"
	LaneDone    = "done"
	LaneOther   = "other"
)

var LaneKinds = []string{LaneBacklog, LaneTodo, LaneActive, LaneReview, LaneDone, LaneOther}

// TagAgentReady is the only tag with behaviour: entering an active lane enqueues a run.
const TagAgentReady = "agent-ready"

// migrations[i] takes the schema from version i to i+1. Append only; never edit a shipped entry.
var migrations = []string{schemaV1}

const schemaV1 = `
CREATE TABLE lanes (
  name TEXT PRIMARY KEY COLLATE NOCASE,
  position INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('backlog','todo','active','review','done','other'))
);
INSERT INTO lanes(name, position, kind) VALUES
  ('backlog', 0, 'backlog'), ('todo', 1, 'todo'), ('in progress', 2, 'active'), ('review', 3, 'review'), ('done', 4, 'done');
CREATE TABLE cards (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  lane TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('project','feature','fix','analysis','hotfix','chore')),
  tags TEXT NOT NULL DEFAULT '[]',
  parent_id TEXT REFERENCES cards(id),
  project_dir TEXT NOT NULL DEFAULT '',
  external_provider TEXT, external_id TEXT, external_url TEXT, external_lane TEXT,
  locked_by_run TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, done_at TEXT
);
CREATE INDEX cards_lane ON cards(lane);
CREATE INDEX cards_parent ON cards(parent_id);
CREATE TABLE comments (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  author_kind TEXT NOT NULL CHECK (author_kind IN ('human','agent','system')),
  author TEXT NOT NULL,
  body TEXT NOT NULL,
  run_id TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX comments_card ON comments(card_id, created_at);
CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  run_id TEXT, slug TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('plan-approval','result','judge','question','permission')),
  checkpoint TEXT, reviewer TEXT,
  text TEXT NOT NULL DEFAULT '',
  options TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL CHECK (status IN ('pending','answered')),
  choice TEXT, note TEXT, actor TEXT, asked_at TEXT, answered_at TEXT,
  data TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX decisions_status ON decisions(status, asked_at);
CREATE INDEX decisions_card ON decisions(card_id);
CREATE TABLE attachments (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  path TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  workspace TEXT NOT NULL,
  adapter TEXT NOT NULL CHECK (adapter IN ('claude','process')),
  on_ramp TEXT NOT NULL CHECK (on_ramp IN ('kickoff','feature','fix','build','resume')),
  project_dir TEXT NOT NULL DEFAULT '',
  worktree TEXT, slug TEXT, session_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('queued','running','waiting','finished','failed','cancelled','over-budget')),
  budget TEXT NOT NULL,
  usage TEXT NOT NULL DEFAULT '{}',
  cost_usd REAL NOT NULL DEFAULT 0,
  rounds INTEGER NOT NULL DEFAULT 0,
  seconds REAL NOT NULL DEFAULT 0,
  started_at TEXT, ended_at TEXT,
  log_path TEXT NOT NULL DEFAULT '',
  state TEXT, stop_reason TEXT,
  queued_at TEXT NOT NULL, not_before TEXT NOT NULL, updated_at TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT 'start',
  retried INTEGER NOT NULL DEFAULT 0,
  relocations TEXT NOT NULL DEFAULT '[]',
  allowed_tools TEXT NOT NULL DEFAULT '[]',
  round_log TEXT NOT NULL DEFAULT '[]',
  kind TEXT NOT NULL DEFAULT ''
);
CREATE INDEX runs_status ON runs(status, queued_at);
CREATE INDEX runs_card ON runs(card_id);
CREATE TABLE activity (
  ts TEXT NOT NULL, workspace TEXT NOT NULL, actor TEXT NOT NULL, verb TEXT NOT NULL,
  card_id TEXT, run_id TEXT, data TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX activity_ts ON activity(ts);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`
```

`internal/hub/store/ids.go`:

```go
package store

import (
	"crypto/rand"
	"regexp"
	"strings"
)

const idAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

var idRe = regexp.MustCompile(`^[a-z]-[a-z2-7]{12}$`)

// NewID returns prefix + "-" + 12 random base32 characters (60 bits).
func NewID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails on supported platforms (Go 1.24+)
	out := make([]byte, 0, len(prefix)+13)
	out = append(out, prefix...)
	out = append(out, '-')
	for _, x := range b {
		out = append(out, idAlphabet[x&31])
	}
	return string(out)
}

func ValidID(prefix, id string) bool {
	return strings.HasPrefix(id, prefix+"-") && idRe.MatchString(id)
}
```

`internal/hub/store/settings.go`:

```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Budget caps one run. Tokens count input + output + cache write (cache reads are not counted).
type Budget struct {
	Tokens  int64   `json:"tokens"`
	USD     float64 `json:"usd"`
	Minutes float64 `json:"minutes"`
	Rounds  int     `json:"rounds"`
}

var DefaultBudget = Budget{Tokens: 2_000_000, USD: 10, Minutes: 120, Rounds: 12}

const DefaultConcurrency = 2

const (
	SettingConcurrency    = "concurrency"
	SettingDefaultBudget  = "default_budget"
	SettingNotify         = "notify"
	SettingWebTokenHash   = "web_token_hash"
	SettingProcessCommand = "process_command" // JSON argv for the process adapter (cards tagged adapter:process)
)

func (b Budget) String() string {
	return fmt.Sprintf("tokens=%d,usd=%s,minutes=%s,rounds=%d", b.Tokens,
		strconv.FormatFloat(b.USD, 'f', -1, 64), strconv.FormatFloat(b.Minutes, 'f', -1, 64), b.Rounds)
}

// ParseBudget reads "tokens=…,usd=…,minutes=…,rounds=…"; keys left out keep base's value.
func ParseBudget(spec string, base Budget) (Budget, error) {
	b := base
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return b, nil
	}
	for _, part := range strings.Split(spec, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return base, fmt.Errorf("budget %q: want key=value (tokens, usd, minutes, rounds)", part)
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.ReplaceAll(strings.TrimSpace(v), "_", "")
		switch k {
		case "tokens":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n <= 0 {
				return base, fmt.Errorf("budget tokens=%q: want a positive integer", v)
			}
			b.Tokens = n
		case "usd":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f <= 0 {
				return base, fmt.Errorf("budget usd=%q: want a positive number", v)
			}
			b.USD = f
		case "minutes":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f <= 0 {
				return base, fmt.Errorf("budget minutes=%q: want a positive number", v)
			}
			b.Minutes = f
		case "rounds":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return base, fmt.Errorf("budget rounds=%q: want a positive integer", v)
			}
			b.Rounds = n
		default:
			return base, fmt.Errorf("budget key %q: use tokens, usd, minutes or rounds", k)
		}
	}
	return b, nil
}

func (s *Store) Setting(key string) (string, bool, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) Concurrency() int {
	v, ok, err := s.Setting(SettingConcurrency)
	if err != nil || !ok {
		return DefaultConcurrency
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return DefaultConcurrency
	}
	return n
}

func (s *Store) DefaultBudget() Budget {
	v, ok, err := s.Setting(SettingDefaultBudget)
	if err != nil || !ok {
		return DefaultBudget
	}
	b := DefaultBudget
	if json.Unmarshal([]byte(v), &b) != nil {
		return DefaultBudget
	}
	return b
}

func (s *Store) Notify() bool {
	v, ok, err := s.Setting(SettingNotify)
	if err != nil || !ok {
		return true
	}
	return strings.TrimSpace(strings.ToLower(v)) != "false"
}
```

- [ ] **Step 5: Tidy and run the tests to verify they pass**

Run: `go mod tidy; go vet ./internal/hub/store/; go test ./internal/hub/store/ -count=1`
Expected: PASS (`ok  github.com/tentaqles/tentaqles/internal/hub/store`). Then `go test ./... -count=1` stays green.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/hub/store/store.go internal/hub/store/schema.go internal/hub/store/ids.go internal/hub/store/settings.go internal/hub/store/store_test.go internal/hub/store/settings_test.go
git commit -m "feat(hub): per-workspace SQLite store with migrations, ids, masking and settings"
```

---
### Task 2: Activity log and the `tq insights` Hub section

**Files:**
- Create: `internal/hub/activity/activity.go`
- Modify: `internal/insights/report.go` (`Options`, `Report`, `Run`), `internal/insights/render.go` (`WriteText`), `docs/INSIGHTS.md`
- Test: `internal/hub/activity/activity_test.go`, `internal/insights/hub_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (standalone package, so `internal/insights` can import it without a cycle).
- Produces (package `activity`):
  - `type Entry struct { TS, Workspace, Actor, Verb, CardID, RunID string; Data map[string]any }` with JSON keys `ts workspace actor verb card_id run_id data`.
  - Verb constants: `VerbCardCreate = "card.create"`, `VerbCardMove = "card.move"`, `VerbCardUpdate = "card.update"`, `VerbCommentAdd = "comment.add"`, `VerbRunStart = "run.start"`, `VerbRunWait = "run.wait"`, `VerbRunEnd = "run.end"`, `VerbDecisionAsk = "decision.ask"`, `VerbDecisionAnswer = "decision.answer"`, `VerbReviewApprove = "review.approve"`, `VerbReviewFeedback = "review.feedback"`, `VerbBudgetStop = "budget.stop"`; `Verbs []string`.
  - Data keys every writer uses (aggregate-only, never free text): `card.create {lane, kind}`, `card.move {from, to, kind}`, `card.update {fields}`, `comment.add {author_kind}`, `run.start {adapter, on_ramp, kind}`, `run.wait {decisions}`, `run.end {status, cost_usd, tokens, rounds, kind}`, `decision.ask {decision, kind}`, `decision.answer {decision, kind, choice, actor_kind}` (`actor_kind` is `hub`, `terminal` or `other`), `review.approve {}`, `review.feedback {}`, `budget.stop {limit}`.
  - `Append(path string, e Entry) error`, `Read(path string, since time.Time) ([]Entry, error)`, `Summarize(entries []Entry) Report`, `ReadAll(hubDir string, since time.Time, keep func(ws string) bool) (Report, error)`.
  - `type Report struct { Workspaces []string; CardsByLane map[string]int; RunsStarted int; RunsEnded map[string]int; CostByCard, CostByKind map[string]float64; DecisionsAnswered int; MedianWaitMin float64; Overrides, RequestChanges, BudgetStops int; ReadError string }`.
- Produces (package `insights`): `Options.HubDir string` (`""` means `$TQ_HOME/hub`), `Report.Hub activity.Report` (JSON key `hub`), the "Hub (tq hub)" text section.

- [ ] **Step 1: Write the failing tests**

`internal/hub/activity/activity_test.go`:

```go
package activity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndReadSkipOldAndBadLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "acme", "activity.jsonl")
	if err := Append(p, Entry{TS: "2026-01-01T00:00:00Z", Workspace: "acme", Actor: "renato", Verb: VerbCardCreate}); err != nil {
		t.Fatal(err)
	}
	if err := Append(p, Entry{Workspace: "acme", Actor: "renato", Verb: VerbCardMove, CardID: "c-abcdefghijkl", Data: map[string]any{"from": "backlog", "to": "review"}}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("not json\n")
	f.Close()
	got, err := Read(p, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Verb != VerbCardMove || got[0].TS == "" || got[0].Data["to"] != "review" {
		t.Fatalf("entries %+v", got)
	}
	if fi, err := os.Stat(p); err != nil || (fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/') {
		t.Fatalf("activity.jsonl must be user-only: %v %v", fi.Mode(), err)
	}
}

func TestReadMissingFileIsEmpty(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "none.jsonl"), time.Time{})
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestSummarize(t *testing.T) {
	es := []Entry{
		{TS: "2026-10-20T10:00:00Z", Verb: VerbCardCreate, CardID: "c-a", Data: map[string]any{"lane": "backlog", "kind": "feature"}},
		{TS: "2026-10-20T10:00:00Z", Verb: VerbCardCreate, CardID: "c-b", Data: map[string]any{"lane": "backlog", "kind": "fix"}},
		{TS: "2026-10-20T10:01:00Z", Verb: VerbCardMove, CardID: "c-a", Data: map[string]any{"from": "backlog", "to": "in progress"}},
		{TS: "2026-10-20T10:02:00Z", Verb: VerbRunStart, CardID: "c-a", RunID: "r-1"},
		{TS: "2026-10-20T10:03:00Z", Verb: VerbDecisionAsk, CardID: "c-a", Data: map[string]any{"decision": "d1", "kind": "plan-approval"}},
		{TS: "2026-10-20T10:13:00Z", Verb: VerbDecisionAnswer, CardID: "c-a", Data: map[string]any{"decision": "d1", "kind": "plan-approval", "choice": "approve", "actor_kind": "hub"}},
		{TS: "2026-10-20T10:20:00Z", Verb: VerbDecisionAsk, CardID: "c-a", Data: map[string]any{"decision": "d2", "kind": "question"}},
		{TS: "2026-10-20T10:50:00Z", Verb: VerbDecisionAnswer, CardID: "c-a", Data: map[string]any{"decision": "d2", "kind": "question", "choice": "override", "actor_kind": "terminal"}},
		{TS: "2026-10-20T11:00:00Z", Verb: VerbDecisionAnswer, CardID: "c-a", Data: map[string]any{"decision": "d3", "kind": "result", "choice": "request-changes", "actor_kind": "hub"}},
		{TS: "2026-10-20T11:30:00Z", Verb: VerbRunEnd, CardID: "c-a", RunID: "r-1", Data: map[string]any{"status": "finished", "cost_usd": 1.5, "kind": "feature"}},
		{TS: "2026-10-20T11:31:00Z", Verb: VerbRunEnd, CardID: "c-b", RunID: "r-2", Data: map[string]any{"status": "over-budget", "cost_usd": 10.0, "kind": "fix"}},
		{TS: "2026-10-20T11:31:00Z", Verb: VerbBudgetStop, CardID: "c-b", RunID: "r-2", Data: map[string]any{"limit": "usd"}},
	}
	r := Summarize(es)
	if r.CardsByLane["in progress"] != 1 || r.CardsByLane["backlog"] != 1 {
		t.Fatalf("lanes %v", r.CardsByLane)
	}
	if r.RunsStarted != 1 || r.RunsEnded["finished"] != 1 || r.RunsEnded["over-budget"] != 1 {
		t.Fatalf("runs %d %v", r.RunsStarted, r.RunsEnded)
	}
	if r.CostByCard["c-a"] != 1.5 || r.CostByKind["fix"] != 10 {
		t.Fatalf("cost %v %v", r.CostByCard, r.CostByKind)
	}
	if r.DecisionsAnswered != 3 || r.MedianWaitMin != 20 { // waits 10 and 30 minutes; d3 was never asked in the window
		t.Fatalf("decisions %d median %v", r.DecisionsAnswered, r.MedianWaitMin)
	}
	if r.Overrides != 1 || r.RequestChanges != 1 || r.BudgetStops != 1 {
		t.Fatalf("counts %+v", r)
	}
}

func TestReadAllFiltersWorkspaces(t *testing.T) {
	hub := t.TempDir()
	for _, ws := range []string{"acme", "globex"} {
		if err := Append(filepath.Join(hub, ws, "activity.jsonl"), Entry{Workspace: ws, Verb: VerbRunStart, CardID: "c-x"}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := ReadAll(hub, time.Time{}, nil)
	if err != nil || all.RunsStarted != 2 || len(all.Workspaces) != 2 {
		t.Fatalf("all %+v %v", all, err)
	}
	one, _ := ReadAll(hub, time.Time{}, func(ws string) bool { return ws == "acme" })
	if one.RunsStarted != 1 || len(one.Workspaces) != 1 || one.Workspaces[0] != "acme" {
		t.Fatalf("filtered %+v", one)
	}
	none, err := ReadAll(filepath.Join(hub, "missing"), time.Time{}, nil)
	if err != nil || len(none.Workspaces) != 0 {
		t.Fatalf("missing hub dir %+v %v", none, err)
	}
}
```

`internal/insights/hub_test.go`:

```go
package insights

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestReportIncludesHubSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	hub := filepath.Join(home, "hub")
	now := time.Now().UTC()
	if err := activity.Append(filepath.Join(hub, "acme", "activity.jsonl"), activity.Entry{
		TS: now.Format(time.RFC3339), Workspace: "acme", Verb: activity.VerbRunEnd, CardID: "c-a",
		Data: map[string]any{"status": "finished", "cost_usd": 2.25, "kind": "feature"},
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(Options{Since: now.Add(-time.Hour), Until: now.Add(time.Minute), Sources: []Source{}, HubDir: hub,
		DecideDir: filepath.Join(home, "decide"), BuildGateDir: filepath.Join(home, "build-gate")})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Hub.RunsEnded["finished"] != 1 || rep.Hub.CostByKind["feature"] != 2.25 {
		t.Fatalf("hub %+v", rep.Hub)
	}
	var buf bytes.Buffer
	WriteText(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "== Hub (tq hub)") || !strings.Contains(out, "feature $2.25") {
		t.Fatalf("text:\n%s", out)
	}
	filtered, _ := Run(Options{Since: now.Add(-time.Hour), Until: now, Sources: []Source{}, HubDir: hub, Workspaces: []string{"globex"},
		DecideDir: filepath.Join(home, "decide"), BuildGateDir: filepath.Join(home, "build-gate")})
	if filtered.Hub.RunsEnded["finished"] != 0 {
		t.Fatal("the --ws filter applies to hub activity")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/hub/activity/ ./internal/insights/ -run 'Test(Append|ReadMissing|Summarize|ReadAll|ReportIncludesHub)' -count=1`
Expected: FAIL, `no Go files in .../internal/hub/activity` / `unknown field HubDir in struct literal of type Options`.

- [ ] **Step 3: Implement the activity package**

`internal/hub/activity/activity.go`:

```go
// Package activity is the hub's append-only activity log ($TQ_HOME/hub/<ws>/activity.jsonl)
// and its aggregate reader for `tq insights`. Entries carry ids, verbs and
// aggregate data only; the store masks every string before it appends.
package activity

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	VerbCardCreate     = "card.create"
	VerbCardMove       = "card.move"
	VerbCardUpdate     = "card.update"
	VerbCommentAdd     = "comment.add"
	VerbRunStart       = "run.start"
	VerbRunWait        = "run.wait"
	VerbRunEnd         = "run.end"
	VerbDecisionAsk    = "decision.ask"
	VerbDecisionAnswer = "decision.answer"
	VerbReviewApprove  = "review.approve"
	VerbReviewFeedback = "review.feedback"
	VerbBudgetStop     = "budget.stop"
)

var Verbs = []string{VerbCardCreate, VerbCardMove, VerbCardUpdate, VerbCommentAdd, VerbRunStart, VerbRunWait, VerbRunEnd,
	VerbDecisionAsk, VerbDecisionAnswer, VerbReviewApprove, VerbReviewFeedback, VerbBudgetStop}

type Entry struct {
	TS        string         `json:"ts"`
	Workspace string         `json:"workspace"`
	Actor     string         `json:"actor"`
	Verb      string         `json:"verb"`
	CardID    string         `json:"card_id,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// Append writes one line. The caller (the store) has already masked e.
func Append(path string, e Entry) error {
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Read returns the entries at or after since; unparseable lines are skipped.
func Read(path string, since time.Time) ([]Entry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.TS)
		if err != nil || ts.Before(since) {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

type Report struct {
	Workspaces        []string           `json:"workspaces"`
	CardsByLane       map[string]int     `json:"cards_by_lane"`
	RunsStarted       int                `json:"runs_started"`
	RunsEnded         map[string]int     `json:"runs_ended"`
	CostByCard        map[string]float64 `json:"cost_by_card"`
	CostByKind        map[string]float64 `json:"cost_by_kind"`
	DecisionsAnswered int                `json:"decisions_answered"`
	MedianWaitMin     float64            `json:"median_wait_min"`
	Overrides         int                `json:"overrides"`
	RequestChanges    int                `json:"request_changes"`
	BudgetStops       int                `json:"budget_stops"`
	ReadError         string             `json:"read_error,omitempty"`
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

// Summarize aggregates entries (any order; sorted by time here).
func Summarize(entries []Entry) Report {
	r := Report{Workspaces: []string{}, CardsByLane: map[string]int{}, RunsEnded: map[string]int{},
		CostByCard: map[string]float64{}, CostByKind: map[string]float64{}}
	es := append([]Entry(nil), entries...)
	sort.SliceStable(es, func(i, j int) bool { return es[i].TS < es[j].TS })
	lane := map[string]string{}
	asked := map[string]time.Time{}
	var waits []float64
	for _, e := range es {
		switch e.Verb {
		case VerbCardCreate:
			lane[e.CardID] = str(e.Data["lane"])
		case VerbCardMove:
			lane[e.CardID] = str(e.Data["to"])
		case VerbRunStart:
			r.RunsStarted++
		case VerbRunEnd:
			r.RunsEnded[str(e.Data["status"])]++
			c := num(e.Data["cost_usd"])
			r.CostByCard[e.CardID] += c
			if k := str(e.Data["kind"]); k != "" {
				r.CostByKind[k] += c
			}
		case VerbDecisionAsk:
			if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
				asked[str(e.Data["decision"])] = t
			}
		case VerbDecisionAnswer:
			r.DecisionsAnswered++
			switch str(e.Data["choice"]) {
			case "override":
				r.Overrides++
			case "request-changes":
				r.RequestChanges++
			}
			if a, ok := asked[str(e.Data["decision"])]; ok {
				if t, err := time.Parse(time.RFC3339, e.TS); err == nil && !t.Before(a) {
					waits = append(waits, t.Sub(a).Minutes())
				}
			}
		case VerbBudgetStop:
			r.BudgetStops++
		}
	}
	for _, l := range lane {
		if l != "" {
			r.CardsByLane[l]++
		}
	}
	if len(waits) > 0 {
		sort.Float64s(waits)
		m := len(waits) / 2
		if len(waits)%2 == 1 {
			r.MedianWaitMin = waits[m]
		} else {
			r.MedianWaitMin = (waits[m-1] + waits[m]) / 2
		}
	}
	return r
}

// ReadAll reads every <hubDir>/<ws>/activity.jsonl that keep accepts (nil keeps all).
func ReadAll(hubDir string, since time.Time, keep func(ws string) bool) (Report, error) {
	ents, err := os.ReadDir(hubDir)
	if os.IsNotExist(err) {
		return Summarize(nil), nil
	}
	if err != nil {
		return Summarize(nil), err
	}
	var all []Entry
	var names []string
	var firstErr error
	for _, d := range ents {
		if !d.IsDir() || (keep != nil && !keep(d.Name())) {
			continue
		}
		es, err := Read(filepath.Join(hubDir, d.Name(), "activity.jsonl"), since)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if len(es) > 0 {
			names = append(names, d.Name())
			all = append(all, es...)
		}
	}
	r := Summarize(all)
	sort.Strings(names)
	if names != nil {
		r.Workspaces = names
	}
	return r, firstErr
}
```

- [ ] **Step 4: Wire it into `internal/insights`**

In `internal/insights/report.go`, add the import `"github.com/tentaqles/tentaqles/internal/hub/activity"` and change `Options`:

```go
	DecideDir    string   // "" means $TQ_HOME/decide
	BuildGateDir string   // "" means $TQ_HOME/build-gate
	HubDir       string   // "" means $TQ_HOME/hub
```

Add the field to `Report` after `Process`:

```go
	Process      ProcessReport    `json:"process"`
	Hub          activity.Report  `json:"hub"`
```

In `Run`, after the `if o.BuildGateDir == "" { … }` block:

```go
	if o.HubDir == "" {
		o.HubDir = filepath.Join(paths.Home(), "hub")
	}
```

and after the `rep.Process, perr = ReadProcessEvents(...)` block, before `rep.Targets = Targets(rep)`:

```go
	var keepWS func(string) bool
	if len(filter) > 0 {
		keepWS = func(ws string) bool { return filter[strings.ToLower(ws)] }
	}
	var herr error
	rep.Hub, herr = activity.ReadAll(o.HubDir, o.Since, keepWS)
	if herr != nil {
		rep.Hub.ReadError = "activity.jsonl could not be read; hub counts are incomplete"
	}
```

In `internal/insights/render.go`, insert before the line `fmt.Fprintln(w, "\n== Background review sessions")`:

```go
	h := r.Hub
	fmt.Fprintln(w, "\n== Hub (tq hub)")
	if len(h.Workspaces) == 0 {
		fmt.Fprintln(w, "(no hub activity)")
	} else {
		fmt.Fprintf(w, "workspaces: %s\n", strings.Join(h.Workspaces, ", "))
		fmt.Fprintf(w, "cards by lane: %s\n", countList(h.CardsByLane))
		fmt.Fprintf(w, "runs started %d, ended: %s   budget stops %d\n", h.RunsStarted, countList(h.RunsEnded), h.BudgetStops)
		fmt.Fprintf(w, "decisions answered %d, median wait for the human %s, overrides %d, request-changes %d\n",
			h.DecisionsAnswered, dur(h.MedianWaitMin), h.Overrides, h.RequestChanges)
		if len(h.CostByKind) > 0 {
			var parts []string
			for _, k := range sortedKeys(h.CostByKind) {
				parts = append(parts, fmt.Sprintf("%s $%.2f", k, h.CostByKind[k]))
			}
			fmt.Fprintf(w, "cost by kind: %s\n", strings.Join(parts, ", "))
		}
		if len(h.CostByCard) > 0 {
			cards := sortedKeys(h.CostByCard)
			sort.SliceStable(cards, func(i, j int) bool { return h.CostByCard[cards[i]] > h.CostByCard[cards[j]] })
			if len(cards) > 10 {
				cards = cards[:10]
			}
			tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "card\tcost")
			for _, c := range cards {
				fmt.Fprintf(tw, "%s\t$%.2f\n", c, h.CostByCard[c])
			}
			tw.Flush()
		}
	}
	if h.ReadError != "" {
		fmt.Fprintf(w, "note: %s\n", h.ReadError)
	}
```

(Add `"sort"` to the `render.go` imports if it is not already there; `tw`, `countList`, `sortedKeys` and `dur` already exist in that file.)

In `docs/INSIGHTS.md`, under "## Sections", add a subsection right before `### Background review sessions` (after `### Process (build gate)`):

```markdown
### Hub (tq hub)

Read from `$TQ_HOME/hub/<workspace>/activity.jsonl` (written by `tq hub`, masked at write;
`--ws` filters by workspace name). Shows cards per lane (as of their last move in the
window), runs started and ended by status, budget stops, decisions answered and the median
time a decision waited for the human, override and request-changes answers, cost per kind
and the ten most expensive cards. JSON: the `hub` object.
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/hub/activity/ ./internal/insights/ -count=1`
Expected: PASS (the existing insights tests still pass: the new JSON key is additive).

- [ ] **Step 6: Commit**

```bash
git add internal/hub/activity/activity.go internal/hub/activity/activity_test.go internal/insights/report.go internal/insights/render.go internal/insights/hub_test.go docs/INSIGHTS.md
git commit -m "feat(hub): activity log and a Hub section in tq insights"
```

---
### Task 3: Store: cards, lanes, comments, attachments, parent completion, atomic checkout

**Files:**
- Create: `internal/hub/store/cards.go`, `internal/hub/store/frontmatter.go`, `internal/hub/store/log.go`
- Test: `internal/hub/store/cards_test.go`, `internal/hub/store/checkout_test.go`

**Interfaces:**
- Consumes: Task 1 (`Store`, `Now`, `NewID`, `nullStr`, `ErrNotFound`, lane/kind constants, `Budget`, `ParseBudget`), Task 2 (`activity.Entry`, `activity.Append`, verb constants).
- Produces:
  - `type Card struct { ID, Title, Body, Lane, Kind string; Tags []string; ParentID, ProjectDir, ExternalProvider, ExternalID, ExternalURL, ExternalLane, LockedByRun, CreatedAt, UpdatedAt, DoneAt string }` (JSON snake case, empty optional fields omitted); `(Card).HasTag(tag string) bool`.
  - `type Lane struct { Name string; Position int; Kind string }`; `(*Store).Lanes() ([]Lane, error)`, `Lane(name string) (Lane, error)` (case-insensitive), `FirstLane(kind string) (Lane, error)`.
  - `type NewCard struct { Title, Body, Kind, Lane string; Tags []string; ParentID, ProjectDir string }`; `(*Store).AddCard(nc NewCard, actor string) (Card, error)`; `Card(id) (Card, error)`; `type CardFilter struct { Lane, Kind, Tag string }`; `ListCards(f CardFilter) ([]Card, error)`; `Children(parentID) ([]Card, error)`; `type CardUpdate struct { Title, Body, ProjectDir *string; Tags *[]string }`; `UpdateCard(id string, u CardUpdate, actor string) (Card, error)`.
  - `type MoveResult struct { Card Card; From, To Lane }`, `(MoveResult).EnteredActive() bool`, `LeftActive() bool`; `(*Store).MoveCard(id, lane, actor string) (MoveResult, error)` (sets `done_at` for a `done` lane, runs the parent-completion rule).
  - Comments: `AuthorHuman`, `AuthorAgent`, `AuthorSystem`; `type Comment struct { ID, CardID, AuthorKind, Author, Body, RunID, CreatedAt string }`; `(*Store).AddComment(cardID, authorKind, author, body, runID string) (Comment, error)`, `Comments(cardID) ([]Comment, error)` (oldest first).
  - `type Attachment struct { ID, CardID, Path, Label, CreatedAt string }`; `AddAttachment(cardID, path, label string) (Attachment, error)`, `Attachments(cardID) ([]Attachment, error)`.
  - Checkout: `(*Store).Checkout(cardID, runID string) (bool, error)` (one `UPDATE … WHERE locked_by_run IS NULL`), `Release(cardID, runID string) error`, `ForceRelease(cardID string) error`.
  - Activity rows: `(*Store).Log(e activity.Entry) error` (masks every string in `Data`, inserts the row, appends `activity.jsonl`, publishes), `Activity(limit int) ([]activity.Entry, error)` (newest first).
  - Front matter: `FrontMatter(body string) map[string]string`, `StripFrontMatter(body string) string`, `WithBudgetFrontMatter(body string, b Budget) string`, `CardBudget(c Card, def Budget) (Budget, error)`.

- [ ] **Step 1: Write the failing tests**

`internal/hub/store/cards_test.go`:

```go
package store

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestAddCardDefaultsAndMasking(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, err := s.AddCard(NewCard{Title: "Export invoices", Body: "ask ana@example.com", Tags: []string{"agent-ready", "agent-ready"}}, "renato")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID("c", c.ID) || c.Lane != "backlog" || c.Kind != "feature" || len(c.Tags) != 1 || !c.HasTag(TagAgentReady) {
		t.Fatalf("card %+v", c)
	}
	got, _ := s.Card(c.ID)
	if strings.Contains(got.Body, "ana@example.com") {
		t.Fatalf("body stored unmasked: %q", got.Body)
	}
	acts, err := s.Activity(10)
	if err != nil || len(acts) != 1 || acts[0].Verb != activity.VerbCardCreate || acts[0].Data["lane"] != "backlog" {
		t.Fatalf("activity %+v %v", acts, err)
	}
	raw, _ := os.ReadFile(ActivityPath("acme"))
	if !strings.Contains(string(raw), `"verb":"card.create"`) || strings.Contains(string(raw), "ana@") {
		t.Fatalf("activity.jsonl: %s", raw)
	}
}

func TestAddCardValidation(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	cases := []NewCard{
		{Title: ""},
		{Title: "x", Kind: "epic"},
		{Title: "x", Lane: "nowhere"},
		{Title: "x", ParentID: "c-abcdefghijkl"},
		{Title: "x", ProjectDir: "../outside"},
		{Title: "x", ProjectDir: `C:\abs`},
		{Title: "x", ProjectDir: "/abs"},
	}
	for _, nc := range cases {
		if _, err := s.AddCard(nc, "renato"); err == nil {
			t.Fatalf("%+v accepted", nc)
		}
	}
	c, err := s.AddCard(NewCard{Title: "ok", ProjectDir: `apps\billing`}, "renato")
	if err != nil || c.ProjectDir != "apps/billing" {
		t.Fatalf("project dir %q %v", c.ProjectDir, err)
	}
}

func TestListCardsFilters(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	a, _ := s.AddCard(NewCard{Title: "a", Kind: "fix", Tags: []string{"agent-ready"}}, "renato")
	s.AddCard(NewCard{Title: "b", Kind: "feature", Lane: "todo"}, "renato")
	byTag, _ := s.ListCards(CardFilter{Tag: "agent-ready"})
	byKind, _ := s.ListCards(CardFilter{Kind: "feature"})
	byLane, _ := s.ListCards(CardFilter{Lane: "TODO"})
	all, _ := s.ListCards(CardFilter{})
	if len(byTag) != 1 || byTag[0].ID != a.ID || len(byKind) != 1 || len(byLane) != 1 || len(all) != 2 {
		t.Fatalf("tag %d kind %d lane %d all %d", len(byTag), len(byKind), len(byLane), len(all))
	}
	if all[0].Lane != "backlog" {
		t.Fatalf("cards are ordered by lane position first: %+v", all)
	}
}

func TestMoveCardLaneKinds(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	m, err := s.MoveCard(c.ID, "In Progress", "renato")
	if err != nil || !m.EnteredActive() || m.LeftActive() || m.Card.Lane != "in progress" {
		t.Fatalf("move %+v %v", m, err)
	}
	m, _ = s.MoveCard(c.ID, "done", "renato")
	if !m.LeftActive() || m.Card.DoneAt == "" {
		t.Fatalf("done move %+v", m)
	}
	m, _ = s.MoveCard(c.ID, "todo", "renato")
	if m.Card.DoneAt != "" {
		t.Fatal("leaving done clears done_at")
	}
	if _, err := s.MoveCard(c.ID, "nowhere", "renato"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown lane: %v", err)
	}
}

func TestParentCompletionMovesParentToReview(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	p, _ := s.AddCard(NewCard{Title: "parent", Kind: "project"}, "renato")
	c1, _ := s.AddCard(NewCard{Title: "one", ParentID: p.ID}, "renato")
	c2, _ := s.AddCard(NewCard{Title: "two", ParentID: p.ID}, "renato")
	s.MoveCard(c1.ID, "done", "renato")
	if got, _ := s.Card(p.ID); got.Lane != "backlog" {
		t.Fatalf("parent moved too early: %s", got.Lane)
	}
	s.MoveCard(c2.ID, "done", "renato")
	got, _ := s.Card(p.ID)
	if got.Lane != "review" {
		t.Fatalf("parent lane %q, want review (never done)", got.Lane)
	}
	cs, _ := s.Comments(p.ID)
	if len(cs) != 1 || cs[0].AuthorKind != AuthorSystem || !strings.Contains(cs[0].Body, "2 child cards are done") {
		t.Fatalf("comments %+v", cs)
	}
}

func TestCommentsAndAttachments(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	if _, err := s.AddComment(c.ID, "robot", "x", "hi", ""); err == nil {
		t.Fatal("unknown author kind accepted")
	}
	if _, err := s.AddComment(c.ID, AuthorAgent, "agent", "   ", ""); err == nil {
		t.Fatal("empty comment accepted")
	}
	if _, err := s.AddComment(c.ID, AuthorAgent, "agent", "mail bob@example.com", "r-abcdefghijkl"); err != nil {
		t.Fatal(err)
	}
	cs, _ := s.Comments(c.ID)
	if len(cs) != 1 || strings.Contains(cs[0].Body, "bob@") || cs[0].RunID != "r-abcdefghijkl" {
		t.Fatalf("comments %+v", cs)
	}
	if _, err := s.AddAttachment(c.ID, "docs/prd.md", "PRD"); err != nil {
		t.Fatal(err)
	}
	as, _ := s.Attachments(c.ID)
	if len(as) != 1 || as[0].Path != "docs/prd.md" {
		t.Fatalf("attachments %+v", as)
	}
}

func TestCardBudgetFrontMatter(t *testing.T) {
	body := WithBudgetFrontMatter("Do the thing.", Budget{Tokens: 1000, USD: 2, Minutes: 30, Rounds: 3})
	if !strings.HasPrefix(body, "---\nbudget: tokens=1000,usd=2,minutes=30,rounds=3\n---\n") || StripFrontMatter(body) != "Do the thing." {
		t.Fatalf("body %q", body)
	}
	b, err := CardBudget(Card{Body: body}, DefaultBudget)
	if err != nil || b.Rounds != 3 || b.USD != 2 {
		t.Fatalf("%+v %v", b, err)
	}
	if b, _ := CardBudget(Card{Body: "no front matter"}, DefaultBudget); b != DefaultBudget {
		t.Fatal("no front matter means the default budget")
	}
	if _, err := CardBudget(Card{Body: "---\nbudget: dollars=3\n---\nx"}, DefaultBudget); err == nil {
		t.Fatal("bad budget accepted")
	}
}

func TestLogMasksData(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if err := s.Log(activity.Entry{Actor: "hub", Verb: activity.VerbCardUpdate, Data: map[string]any{"fields": []any{"ana@example.com"}}}); err != nil {
		t.Fatal(err)
	}
	acts, _ := s.Activity(1)
	if strings.Contains(acts[0].Data["fields"].([]any)[0].(string), "ana@") {
		t.Fatalf("data not masked: %+v", acts[0].Data)
	}
}
```

`internal/hub/store/checkout_test.go`:

```go
package store

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestCheckoutIsAtomicAcrossConnections(t *testing.T) {
	isolate(t)
	s1 := mustOpen(t, "acme")
	s2 := mustOpen(t, "acme") // a second process: its own connection pool on the same file
	c, _ := s1.AddCard(NewCard{Title: "a"}, "renato")
	var wins int32
	var winner atomic.Value
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := s1
			if i%2 == 1 {
				st = s2
			}
			run := NewID("r")
			ok, err := st.Checkout(c.ID, run)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				atomic.AddInt32(&wins, 1)
				winner.Store(run)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d checkouts won, want exactly 1", wins)
	}
	got, _ := s1.Card(c.ID)
	if got.LockedByRun != winner.Load().(string) {
		t.Fatalf("locked by %q, winner %q", got.LockedByRun, winner.Load())
	}
	if err := s1.Release(c.ID, "r-notthewinner"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s1.Card(c.ID); got.LockedByRun == "" {
		t.Fatal("release by another run must not unlock")
	}
	s1.Release(c.ID, winner.Load().(string))
	if got, _ := s1.Card(c.ID); got.LockedByRun != "" {
		t.Fatal("release by the owner unlocks")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/hub/store/ -count=1`
Expected: FAIL, `undefined: NewCard`, `undefined: (*Store).AddCard`, `undefined: WithBudgetFrontMatter`.

- [ ] **Step 3: Implement**

`internal/hub/store/log.go`:

```go
package store

import (
	"encoding/json"
	"fmt"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func (s *Store) maskAny(v any) any {
	switch x := v.(type) {
	case string:
		return s.Mask(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = s.maskAny(e)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = s.Mask(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = s.maskAny(e)
		}
		return out
	}
	return v
}

// Log records one mutation: a row in `activity` and a line in activity.jsonl, both masked.
func (s *Store) Log(e activity.Entry) error {
	e.Workspace = s.Workspace
	if e.TS == "" {
		e.TS = Now()
	}
	e.Actor = s.Mask(e.Actor)
	if e.Data != nil {
		e.Data = s.maskAny(e.Data).(map[string]any)
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	if e.Data == nil {
		data = []byte("{}")
	}
	if _, err := s.DB.Exec(`INSERT INTO activity(ts, workspace, actor, verb, card_id, run_id, data) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.TS, e.Workspace, e.Actor, e.Verb, nullStr(e.CardID), nullStr(e.RunID), string(data)); err != nil {
		return err
	}
	s.logMu.Lock()
	err = activity.Append(ActivityPath(s.Workspace), e)
	s.logMu.Unlock()
	if err != nil {
		return fmt.Errorf("activity.jsonl: %w", err)
	}
	s.Publish(Change{Kind: "activity", CardID: e.CardID, RunID: e.RunID})
	return nil
}

func (s *Store) Activity(limit int) ([]activity.Entry, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.DB.Query(`SELECT ts, workspace, actor, verb, COALESCE(card_id, ''), COALESCE(run_id, ''), data
		FROM activity ORDER BY rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []activity.Entry
	for rows.Next() {
		var e activity.Entry
		var data string
		if err := rows.Scan(&e.TS, &e.Workspace, &e.Actor, &e.Verb, &e.CardID, &e.RunID, &data); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(data), &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}
```

`internal/hub/store/frontmatter.go`:

```go
package store

import (
	"sort"
	"strings"
)

// FrontMatter reads "key: value" lines between a leading "---" line and the next "---" line.
func FrontMatter(body string) map[string]string {
	out := map[string]string{}
	b := strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(b, "---\n") {
		return out
	}
	end := strings.Index(b[4:], "\n---")
	if end < 0 {
		return out
	}
	for _, line := range strings.Split(b[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(k) != "" {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return out
}

func StripFrontMatter(body string) string {
	b := strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(b, "---\n") {
		return body
	}
	end := strings.Index(b[4:], "\n---")
	if end < 0 {
		return body
	}
	rest := b[4+end+4:]
	return strings.TrimPrefix(rest, "\n")
}

func WithBudgetFrontMatter(body string, b Budget) string {
	fm := FrontMatter(body)
	fm["budget"] = b.String()
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("---\n")
	for _, k := range keys {
		sb.WriteString(k + ": " + fm[k] + "\n")
	}
	sb.WriteString("---\n")
	sb.WriteString(StripFrontMatter(body))
	return sb.String()
}

// CardBudget is the card's front-matter budget on top of def, or def.
func CardBudget(c Card, def Budget) (Budget, error) {
	if v, ok := FrontMatter(c.Body)["budget"]; ok {
		return ParseBudget(v, def)
	}
	return def, nil
}
```

`internal/hub/store/cards.go`:

```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

const (
	AuthorHuman  = "human"
	AuthorAgent  = "agent"
	AuthorSystem = "system"
)

type Card struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Body             string   `json:"body"`
	Lane             string   `json:"lane"`
	Kind             string   `json:"kind"`
	Tags             []string `json:"tags"`
	ParentID         string   `json:"parent_id,omitempty"`
	ProjectDir       string   `json:"project_dir,omitempty"`
	ExternalProvider string   `json:"external_provider,omitempty"`
	ExternalID       string   `json:"external_id,omitempty"`
	ExternalURL      string   `json:"external_url,omitempty"`
	ExternalLane     string   `json:"external_lane,omitempty"`
	LockedByRun      string   `json:"locked_by_run,omitempty"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	DoneAt           string   `json:"done_at,omitempty"`
}

func (c Card) HasTag(tag string) bool { return slices.Contains(c.Tags, tag) }

type Lane struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
	Kind     string `json:"kind"`
}

type NewCard struct {
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Lane       string   `json:"lane,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	ParentID   string   `json:"parent_id,omitempty"`
	ProjectDir string   `json:"project_dir,omitempty"`
}

type CardFilter struct {
	Lane string `json:"lane,omitempty"`
	Kind string `json:"kind,omitempty"`
	Tag  string `json:"tag,omitempty"`
}

type CardUpdate struct {
	Title, Body, ProjectDir *string
	Tags                    *[]string
}

type MoveResult struct {
	Card     Card `json:"card"`
	From, To Lane
}

func (m MoveResult) EnteredActive() bool { return m.To.Kind == LaneActive && m.From.Kind != LaneActive }
func (m MoveResult) LeftActive() bool    { return m.From.Kind == LaneActive && m.To.Kind != LaneActive }

type Comment struct {
	ID         string `json:"id"`
	CardID     string `json:"card_id"`
	AuthorKind string `json:"author_kind"`
	Author     string `json:"author"`
	Body       string `json:"body"`
	RunID      string `json:"run_id,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type Attachment struct {
	ID        string `json:"id"`
	CardID    string `json:"card_id"`
	Path      string `json:"path"`
	Label     string `json:"label,omitempty"`
	CreatedAt string `json:"created_at"`
}

const cardCols = `c.id, c.title, c.body, c.lane, c.kind, c.tags, COALESCE(c.parent_id, ''), c.project_dir,
	COALESCE(c.external_provider, ''), COALESCE(c.external_id, ''), COALESCE(c.external_url, ''), COALESCE(c.external_lane, ''),
	COALESCE(c.locked_by_run, ''), c.created_at, c.updated_at, COALESCE(c.done_at, '')`

type scanner interface{ Scan(dest ...any) error }

func scanCard(r scanner) (Card, error) {
	var c Card
	var tags string
	err := r.Scan(&c.ID, &c.Title, &c.Body, &c.Lane, &c.Kind, &tags, &c.ParentID, &c.ProjectDir,
		&c.ExternalProvider, &c.ExternalID, &c.ExternalURL, &c.ExternalLane, &c.LockedByRun, &c.CreatedAt, &c.UpdatedAt, &c.DoneAt)
	if err != nil {
		return c, err
	}
	_ = json.Unmarshal([]byte(tags), &c.Tags)
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return c, nil
}

func (s *Store) Lanes() ([]Lane, error) {
	rows, err := s.DB.Query(`SELECT name, position, kind FROM lanes ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lane
	for rows.Next() {
		var l Lane
		if err := rows.Scan(&l.Name, &l.Position, &l.Kind); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) Lane(name string) (Lane, error) {
	var l Lane
	err := s.DB.QueryRow(`SELECT name, position, kind FROM lanes WHERE name = ? COLLATE NOCASE`, strings.TrimSpace(name)).Scan(&l.Name, &l.Position, &l.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		ls, _ := s.Lanes()
		names := make([]string, len(ls))
		for i, x := range ls {
			names[i] = x.Name
		}
		return l, fmt.Errorf("lane %q: %w (lanes: %s)", name, ErrNotFound, strings.Join(names, ", "))
	}
	return l, err
}

func (s *Store) FirstLane(kind string) (Lane, error) {
	var l Lane
	err := s.DB.QueryRow(`SELECT name, position, kind FROM lanes WHERE kind = ? ORDER BY position LIMIT 1`, kind).Scan(&l.Name, &l.Position, &l.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return l, fmt.Errorf("no lane of kind %s: %w", kind, ErrNotFound)
	}
	return l, err
}

// cleanProjectDir keeps a workspace-relative, forward-slash path that cannot escape the workspace.
func cleanProjectDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || (len(p) > 1 && p[1] == ':') {
		return "", fmt.Errorf("project folder %q must be relative to the workspace root", p)
	}
	c := path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("project folder %q leaves the workspace", p)
	}
	if c == "." {
		return "", nil
	}
	return c, nil
}

func dedupe(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func (s *Store) AddCard(nc NewCard, actor string) (Card, error) {
	title := strings.TrimSpace(nc.Title)
	if title == "" {
		return Card{}, errors.New("a card needs a title")
	}
	kind := nc.Kind
	if kind == "" {
		kind = "feature"
	}
	if !slices.Contains(CardKinds, kind) {
		return Card{}, fmt.Errorf("kind %q: use one of %s", kind, strings.Join(CardKinds, ", "))
	}
	var lane Lane
	var err error
	if nc.Lane == "" {
		lane, err = s.FirstLane(LaneBacklog)
	} else {
		lane, err = s.Lane(nc.Lane)
	}
	if err != nil {
		return Card{}, err
	}
	if nc.ParentID != "" {
		if _, err := s.Card(nc.ParentID); err != nil {
			return Card{}, fmt.Errorf("parent %s: %w", nc.ParentID, err)
		}
	}
	pd, err := cleanProjectDir(nc.ProjectDir)
	if err != nil {
		return Card{}, err
	}
	tags, _ := json.Marshal(dedupe(nc.Tags))
	now := Now()
	id := NewID("c")
	if _, err := s.DB.Exec(`INSERT INTO cards(id, title, body, lane, kind, tags, parent_id, project_dir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, s.Mask(title), s.Mask(nc.Body), lane.Name, kind, string(tags),
		nullStr(nc.ParentID), pd, now, now); err != nil {
		return Card{}, err
	}
	if err := s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardCreate, CardID: id, Data: map[string]any{"lane": lane.Name, "kind": kind}}); err != nil {
		return Card{}, err
	}
	s.Publish(Change{Kind: "card", CardID: id})
	return s.Card(id)
}

func (s *Store) Card(id string) (Card, error) {
	c, err := scanCard(s.DB.QueryRow(`SELECT `+cardCols+` FROM cards c WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("card %s: %w", id, ErrNotFound)
	}
	return c, err
}

func (s *Store) ListCards(f CardFilter) ([]Card, error) {
	q := `SELECT ` + cardCols + ` FROM cards c JOIN lanes l ON l.name = c.lane COLLATE NOCASE WHERE 1 = 1`
	var args []any
	if f.Lane != "" {
		q += ` AND c.lane = ? COLLATE NOCASE`
		args = append(args, f.Lane)
	}
	if f.Kind != "" {
		q += ` AND c.kind = ?`
		args = append(args, f.Kind)
	}
	if f.Tag != "" {
		q += ` AND EXISTS (SELECT 1 FROM json_each(c.tags) WHERE value = ?)`
		args = append(args, f.Tag)
	}
	q += ` ORDER BY l.position, c.updated_at DESC, c.id`
	return s.cards(q, args...)
}

func (s *Store) cards(q string, args ...any) ([]Card, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Children(parentID string) ([]Card, error) {
	return s.cards(`SELECT `+cardCols+` FROM cards c WHERE c.parent_id = ? ORDER BY c.created_at`, parentID)
}

func (s *Store) UpdateCard(id string, u CardUpdate, actor string) (Card, error) {
	c, err := s.Card(id)
	if err != nil {
		return c, err
	}
	var fields []any
	if u.Title != nil {
		if strings.TrimSpace(*u.Title) == "" {
			return c, errors.New("a card needs a title")
		}
		c.Title = s.Mask(strings.TrimSpace(*u.Title))
		fields = append(fields, "title")
	}
	if u.Body != nil {
		c.Body = s.Mask(*u.Body)
		fields = append(fields, "body")
	}
	if u.ProjectDir != nil {
		pd, err := cleanProjectDir(*u.ProjectDir)
		if err != nil {
			return c, err
		}
		c.ProjectDir = pd
		fields = append(fields, "project_dir")
	}
	if u.Tags != nil {
		c.Tags = dedupe(*u.Tags)
		fields = append(fields, "tags")
	}
	tags, _ := json.Marshal(c.Tags)
	if _, err := s.DB.Exec(`UPDATE cards SET title = ?, body = ?, project_dir = ?, tags = ?, updated_at = ? WHERE id = ?`,
		c.Title, c.Body, c.ProjectDir, string(tags), Now(), id); err != nil {
		return c, err
	}
	if err := s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardUpdate, CardID: id, Data: map[string]any{"fields": fields}}); err != nil {
		return c, err
	}
	s.Publish(Change{Kind: "card", CardID: id})
	return s.Card(id)
}

func (s *Store) MoveCard(id, lane, actor string) (MoveResult, error) {
	c, err := s.Card(id)
	if err != nil {
		return MoveResult{}, err
	}
	from, err := s.Lane(c.Lane)
	if err != nil {
		return MoveResult{}, err
	}
	to, err := s.Lane(lane)
	if err != nil {
		return MoveResult{}, err
	}
	now := Now()
	var doneAt any
	if to.Kind == LaneDone {
		doneAt = now
	}
	if _, err := s.DB.Exec(`UPDATE cards SET lane = ?, done_at = ?, updated_at = ? WHERE id = ?`, to.Name, doneAt, now, id); err != nil {
		return MoveResult{}, err
	}
	if err := s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardMove, CardID: id,
		Data: map[string]any{"from": from.Name, "to": to.Name, "kind": c.Kind}}); err != nil {
		return MoveResult{}, err
	}
	s.Publish(Change{Kind: "card", CardID: id})
	if to.Kind == LaneDone && c.ParentID != "" {
		if err := s.completeParent(c.ParentID); err != nil {
			return MoveResult{}, err
		}
	}
	moved, err := s.Card(id)
	return MoveResult{Card: moved, From: from, To: to}, err
}

// completeParent moves the parent to review (never done: the parent's own review is the human's)
// once every child is in a done lane.
func (s *Store) completeParent(parentID string) error {
	kids, err := s.Children(parentID)
	if err != nil || len(kids) == 0 {
		return err
	}
	for _, k := range kids {
		l, err := s.Lane(k.Lane)
		if err != nil {
			return err
		}
		if l.Kind != LaneDone {
			return nil
		}
	}
	p, err := s.Card(parentID)
	if err != nil {
		return err
	}
	pl, err := s.Lane(p.Lane)
	if err != nil {
		return err
	}
	if pl.Kind == LaneDone || pl.Kind == LaneReview {
		return nil
	}
	rev, err := s.FirstLane(LaneReview)
	if err != nil {
		return err
	}
	if _, err := s.AddComment(parentID, AuthorSystem, "hub",
		fmt.Sprintf("All %d child cards are done; this card moves to review for your own review.", len(kids)), ""); err != nil {
		return err
	}
	_, err = s.MoveCard(parentID, rev.Name, "hub")
	return err
}

func (s *Store) AddComment(cardID, authorKind, author, body, runID string) (Comment, error) {
	if !slices.Contains([]string{AuthorHuman, AuthorAgent, AuthorSystem}, authorKind) {
		return Comment{}, fmt.Errorf("author kind %q: use human, agent or system", authorKind)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, errors.New("a comment needs text")
	}
	if _, err := s.Card(cardID); err != nil {
		return Comment{}, err
	}
	masked := s.Mask(body)
	if len(masked) > 20000 {
		masked = masked[:20000] + "\n[truncated]"
	}
	c := Comment{ID: NewID("m"), CardID: cardID, AuthorKind: authorKind, Author: s.Mask(author), Body: masked, RunID: runID, CreatedAt: Now()}
	if _, err := s.DB.Exec(`INSERT INTO comments(id, card_id, author_kind, author, body, run_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.CardID, c.AuthorKind, c.Author, c.Body, nullStr(runID), c.CreatedAt); err != nil {
		return Comment{}, err
	}
	if _, err := s.DB.Exec(`UPDATE cards SET updated_at = ? WHERE id = ?`, c.CreatedAt, cardID); err != nil {
		return Comment{}, err
	}
	if err := s.Log(activity.Entry{Actor: author, Verb: activity.VerbCommentAdd, CardID: cardID, RunID: runID,
		Data: map[string]any{"author_kind": authorKind}}); err != nil {
		return Comment{}, err
	}
	s.Publish(Change{Kind: "comment", CardID: cardID, RunID: runID})
	return c, nil
}

func (s *Store) Comments(cardID string) ([]Comment, error) {
	rows, err := s.DB.Query(`SELECT id, card_id, author_kind, author, body, COALESCE(run_id, ''), created_at
		FROM comments WHERE card_id = ? ORDER BY created_at, rowid`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.CardID, &c.AuthorKind, &c.Author, &c.Body, &c.RunID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddAttachment stores a reference, never a copy.
func (s *Store) AddAttachment(cardID, p, label string) (Attachment, error) {
	if strings.TrimSpace(p) == "" {
		return Attachment{}, errors.New("an attachment needs a path")
	}
	if _, err := s.Card(cardID); err != nil {
		return Attachment{}, err
	}
	a := Attachment{ID: NewID("a"), CardID: cardID, Path: strings.TrimSpace(p), Label: s.Mask(label), CreatedAt: Now()}
	_, err := s.DB.Exec(`INSERT INTO attachments(id, card_id, path, label, created_at) VALUES (?, ?, ?, ?, ?)`, a.ID, a.CardID, a.Path, a.Label, a.CreatedAt)
	if err == nil {
		s.Publish(Change{Kind: "card", CardID: cardID})
	}
	return a, err
}

func (s *Store) Attachments(cardID string) ([]Attachment, error) {
	rows, err := s.DB.Query(`SELECT id, card_id, path, label, created_at FROM attachments WHERE card_id = ? ORDER BY created_at`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.CardID, &a.Path, &a.Label, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Checkout is the one atomic statement of spec §8.1: zero rows means another run owns the card.
func (s *Store) Checkout(cardID, runID string) (bool, error) {
	res, err := s.DB.Exec(`UPDATE cards SET locked_by_run = ?, updated_at = ? WHERE id = ? AND locked_by_run IS NULL`, runID, Now(), cardID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if n == 1 {
		s.Publish(Change{Kind: "card", CardID: cardID, RunID: runID})
	}
	return n == 1, err
}

func (s *Store) Release(cardID, runID string) error {
	_, err := s.DB.Exec(`UPDATE cards SET locked_by_run = NULL, updated_at = ? WHERE id = ? AND locked_by_run = ?`, Now(), cardID, runID)
	if err == nil {
		s.Publish(Change{Kind: "card", CardID: cardID})
	}
	return err
}

func (s *Store) ForceRelease(cardID string) error {
	_, err := s.DB.Exec(`UPDATE cards SET locked_by_run = NULL, updated_at = ? WHERE id = ?`, Now(), cardID)
	return err
}
```

Note: comment ids use the prefix `m-` and attachment ids `a-` (internal rows the spec gives no prefix for); `ValidID("m", …)` works for them too.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/hub/store/; go test ./internal/hub/store/ -count=1 -race`
Expected: PASS (without `-race` on a machine with no cgo toolchain for the race detector: `go test ./internal/hub/store/ -count=1`).

- [ ] **Step 5: Commit**

```bash
git add internal/hub/store/cards.go internal/hub/store/frontmatter.go internal/hub/store/log.go internal/hub/store/cards_test.go internal/hub/store/checkout_test.go
git commit -m "feat(hub): cards, lanes, comments, parent completion and atomic checkout"
```

---
### Task 4: Store: runs, decisions and the lock report

**Files:**
- Create: `internal/hub/store/runs.go`, `internal/hub/store/decisions.go`
- Test: `internal/hub/store/runs_test.go`, `internal/hub/store/decisions_test.go`

**Interfaces:**
- Consumes: Tasks 1 to 3 (`Store`, `Budget`, `Card`, `Log`, `Publish`, `nullStr`, `Now`, `NewID`).
- Produces:
  - Constants: `AdapterClaude = "claude"`, `AdapterProcess = "process"`; `OnRampKickoff`, `OnRampFeature`, `OnRampFix`, `OnRampBuild`, `OnRampResume`; `PhaseStart = "start"`, `PhaseRelocated = "relocated"`, `PhaseResume = "resume"`; `ActiveStatuses = []string{queued, running, waiting}`.
  - `type Usage struct { Input, Output, CacheRead, CacheWrite int64 }` (JSON `input output cache_read cache_write`), `(Usage).Tokens() int64` (input + output + cache write), `(Usage).Add(Usage) Usage`.
  - `type RoundRecord struct { N int; StartedAt string; Seconds float64; Usage Usage; CostUSD float64; CostUnknown bool; ExitCode int; Subtype, Outcome string }`.
  - `type Run struct { ID, CardID, Workspace, Adapter, OnRamp, ProjectDir, Worktree, Slug, SessionID, Status string; Budget Budget; Usage Usage; CostUSD float64; Rounds int; Seconds float64; StartedAt, EndedAt, LogPath string; State json.RawMessage; StopReason, QueuedAt, NotBefore, UpdatedAt, Phase string; Retried int; Relocations, AllowedTools []string; RoundLog []RoundRecord; Kind string }`; `(Run).Active() bool`, `(Run).Exceeded() string` (`"rounds"`, `"tokens"`, `"usd"`, `"minutes"` or `""`).
  - `(*Store).CreateRun(r Run) (Run, error)`, `Run(id) (Run, error)`, `SaveRun(r Run) error`, `RunsForCard(cardID) ([]Run, error)` (newest first), `ActiveRun(cardID) (Run, bool, error)`, `DueRuns(now time.Time, limit int) ([]Run, error)` (FIFO), `RunsWithStatus(statuses ...string) ([]Run, error)`.
  - `type Lock struct { CardID, RunID, RunStatus string }`; `(*Store).Locks() ([]Lock, error)` (`RunStatus` is `missing` when the run row is gone).
  - Decisions: `KindPlanApproval`, `KindResult`, `KindJudge`, `KindQuestion`, `KindPermission`; `DecisionPending`, `DecisionAnswered`; `type Decision struct { ID, CardID, RunID, Slug, Kind, Checkpoint, Reviewer, Text string; Options []string; Status, Choice, Note, Actor, AskedAt, AnsweredAt string; Data map[string]any }`; `(Decision).HubOwned() bool`; `type MirrorResult struct { Inserted, NowAnswered bool }`; `(*Store).UpsertDecision(d Decision) (MirrorResult, error)`, `AskHub(cardID, runID, slug, text string, options []string, data map[string]any) (Decision, error)`, `AnswerHub(id, choice, note, actor string) (Decision, error)`, `Decision(id) (Decision, error)`, `DecisionsForCard(cardID) ([]Decision, error)` (newest first), `PendingDecisions() ([]Decision, error)` (newest first), `PendingCount(cardID) (int, error)`; `ActorKind(actor string) string` (`hub`, `terminal`, `other`).

- [ ] **Step 1: Write the failing tests**

`internal/hub/store/runs_test.go`:

```go
package store

import (
	"strings"
	"testing"
	"time"
)

func newRunFor(t *testing.T, s *Store, c Card) Run {
	t.Helper()
	r, err := s.CreateRun(Run{CardID: c.ID, Adapter: AdapterClaude, OnRamp: OnRampFeature, ProjectDir: c.ProjectDir, Kind: c.Kind})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreateAndSaveRun(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	r := newRunFor(t, s, c)
	if !ValidID("r", r.ID) || r.Status != StatusQueued || r.Budget != s.DefaultBudget() || r.Phase != PhaseStart || r.Workspace != "acme" {
		t.Fatalf("run %+v", r)
	}
	if !strings.HasSuffix(r.LogPath, r.ID+".log") {
		t.Fatalf("log path %q", r.LogPath)
	}
	r.Status = StatusRunning
	r.Usage = r.Usage.Add(Usage{Input: 10, Output: 5, CacheRead: 1000, CacheWrite: 2})
	r.RoundLog = append(r.RoundLog, RoundRecord{N: 1, CostUSD: 0.5, CostUnknown: true})
	r.Relocations = []string{"demo"}
	r.StopReason = "mail ana@example.com"
	if err := s.SaveRun(r); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Run(r.ID)
	if got.Status != StatusRunning || got.Usage.Tokens() != 17 || len(got.RoundLog) != 1 || !got.RoundLog[0].CostUnknown || got.Relocations[0] != "demo" {
		t.Fatalf("saved %+v", got)
	}
	if strings.Contains(got.StopReason, "ana@") {
		t.Fatal("stop reason must be masked")
	}
	if _, err := s.CreateRun(Run{CardID: c.ID, Adapter: "codex", OnRamp: OnRampFeature}); err == nil {
		t.Fatal("unknown adapter accepted")
	}
}

func TestExceeded(t *testing.T) {
	b := Budget{Tokens: 100, USD: 1, Minutes: 10, Rounds: 2}
	cases := map[string]Run{
		"":        {Budget: b, Rounds: 1, Usage: Usage{Input: 10}, CostUSD: 0.5, Seconds: 60},
		"rounds":  {Budget: b, Rounds: 2},
		"tokens":  {Budget: b, Usage: Usage{Input: 60, Output: 40}},
		"usd":     {Budget: b, CostUSD: 1},
		"minutes": {Budget: b, Seconds: 600},
	}
	for want, r := range cases {
		if got := r.Exceeded(); got != want {
			t.Fatalf("want %q got %q for %+v", want, got, r)
		}
	}
	if (Run{Budget: b, Usage: Usage{CacheRead: 10_000}}).Exceeded() != "" {
		t.Fatal("cache reads do not count against the token budget")
	}
}

func TestDueRunsFIFOAndNotBefore(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c1, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	c2, _ := s.AddCard(NewCard{Title: "b"}, "renato")
	now := time.Now().UTC()
	r1, _ := s.CreateRun(Run{CardID: c1.ID, Adapter: AdapterClaude, OnRamp: OnRampFeature, QueuedAt: now.Add(-2 * time.Minute).Format(time.RFC3339)})
	r2, _ := s.CreateRun(Run{CardID: c2.ID, Adapter: AdapterClaude, OnRamp: OnRampFeature, QueuedAt: now.Add(-1 * time.Minute).Format(time.RFC3339),
		NotBefore: now.Add(2 * time.Minute).Format(time.RFC3339)})
	due, err := s.DueRuns(now, 10)
	if err != nil || len(due) != 1 || due[0].ID != r1.ID {
		t.Fatalf("due %+v %v", due, err)
	}
	later, _ := s.DueRuns(now.Add(3*time.Minute), 10)
	if len(later) != 2 || later[0].ID != r1.ID || later[1].ID != r2.ID {
		t.Fatalf("FIFO %+v", later)
	}
	if act, ok, _ := s.ActiveRun(c1.ID); !ok || act.ID != r1.ID {
		t.Fatal("active run")
	}
}

func TestLocksReport(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	r := newRunFor(t, s, c)
	s.Checkout(c.ID, r.ID)
	r.Status = StatusFailed
	s.SaveRun(r)
	c2, _ := s.AddCard(NewCard{Title: "b"}, "renato")
	s.Checkout(c2.ID, "r-gonegonegone")
	locks, err := s.Locks()
	if err != nil || len(locks) != 2 {
		t.Fatalf("locks %+v %v", locks, err)
	}
	got := map[string]string{}
	for _, l := range locks {
		got[l.CardID] = l.RunStatus
	}
	if got[c.ID] != StatusFailed || got[c2.ID] != "missing" {
		t.Fatalf("%v", got)
	}
}
```

`internal/hub/store/decisions_test.go`:

```go
package store

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestUpsertDecisionMirrorsGateRecords(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	d := Decision{ID: "d1a2b3c4", CardID: c.ID, RunID: "r-x", Slug: "demo", Kind: KindPlanApproval,
		Text: "approve? reply to ana@example.com", Options: []string{"approve", "edit", "stop"}, Status: DecisionPending, AskedAt: "2026-10-21T10:00:00Z"}
	res, err := s.UpsertDecision(d)
	if err != nil || !res.Inserted || res.NowAnswered {
		t.Fatalf("%+v %v", res, err)
	}
	if res, _ := s.UpsertDecision(d); res.Inserted {
		t.Fatal("second mirror of the same pending record is a no-op")
	}
	if n, _ := s.PendingCount(c.ID); n != 1 {
		t.Fatalf("pending %d", n)
	}
	got, _ := s.Decision("d1a2b3c4")
	if strings.Contains(got.Text, "ana@") {
		t.Fatal("decision text must be masked")
	}
	answered := Decision{ID: "d1a2b3c4", CardID: c.ID, Kind: KindPlanApproval, Status: DecisionAnswered, Choice: "approve", Actor: "terminal", AnsweredAt: "2026-10-21T10:05:00Z"}
	res, _ = s.UpsertDecision(answered)
	if !res.NowAnswered {
		t.Fatal("answered record must flip the row")
	}
	got, _ = s.Decision("d1a2b3c4")
	if got.Status != DecisionAnswered || got.Actor != "terminal" || got.Text == "" {
		t.Fatalf("answered row %+v (the text of the pending record is kept)", got)
	}
	again := answered
	again.Choice = "stop"
	s.UpsertDecision(again)
	if got, _ := s.Decision("d1a2b3c4"); got.Choice != "approve" {
		t.Fatal("an answer is final")
	}
	acts, _ := s.Activity(10)
	verbs := map[string]int{}
	for _, a := range acts {
		verbs[a.Verb]++
		if a.Verb == activity.VerbDecisionAnswer && a.Data["actor_kind"] != "terminal" {
			t.Fatalf("actor kind %v", a.Data)
		}
	}
	if verbs[activity.VerbDecisionAsk] != 1 || verbs[activity.VerbDecisionAnswer] != 1 {
		t.Fatalf("verbs %v", verbs)
	}
}

func TestHubOwnedDecisions(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	d, err := s.AskHub(c.ID, "r-x", "demo", "The run was refused Bash(npm test). Allow it for one round?", []string{"allow-once", "stop"}, map[string]any{"tool": "Bash(npm test)"})
	if err != nil || !ValidID("d", d.ID) || !d.HubOwned() || d.Kind != KindPermission {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := s.AnswerHub(d.ID, "approve", "", "renato"); err == nil {
		t.Fatal("a choice outside the options is refused")
	}
	got, err := s.AnswerHub(d.ID, "allow-once", "", "renato")
	if err != nil || got.Status != DecisionAnswered || got.Data["tool"] != "Bash(npm test)" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.AnswerHub(d.ID, "stop", "", "renato"); err == nil {
		t.Fatal("answering twice is refused")
	}
	if _, err := s.AnswerHub("d1a2b3c4", "x", "", "renato"); err == nil {
		t.Fatal("a gate decision cannot be answered as a hub decision")
	}
	pend, _ := s.PendingDecisions()
	if len(pend) != 0 {
		t.Fatalf("pending %+v", pend)
	}
}

func TestActorKind(t *testing.T) {
	for in, want := range map[string]string{"hub:renato": "hub", "terminal": "terminal", "renato": "other", "": "other"} {
		if got := ActorKind(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/hub/store/ -run 'Test(CreateAndSaveRun|Exceeded|DueRuns|Locks|UpsertDecision|HubOwned|ActorKind)' -count=1`
Expected: FAIL, `undefined: Run`, `undefined: Decision`.

- [ ] **Step 3: Implement**

`internal/hub/store/runs.go`:

```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	AdapterClaude  = "claude"
	AdapterProcess = "process"

	OnRampKickoff = "kickoff"
	OnRampFeature = "feature"
	OnRampFix     = "fix"
	OnRampBuild   = "build"
	OnRampResume  = "resume"

	PhaseStart     = "start"     // the on-ramp's prompt in the card's project folder
	PhaseRelocated = "relocated" // the on-ramp's prompt again, in the worktree TQ_HUB_RELOCATE named
	PhaseResume    = "resume"    // /tentaqles:build resume <slug> --headless
)

var ActiveStatuses = []string{StatusQueued, StatusRunning, StatusWaiting}

type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Tokens is what the token budget counts: input + output + cache write (like the overnight loop).
func (u Usage) Tokens() int64 { return u.Input + u.Output + u.CacheWrite }

func (u Usage) Add(o Usage) Usage {
	return Usage{u.Input + o.Input, u.Output + o.Output, u.CacheRead + o.CacheRead, u.CacheWrite + o.CacheWrite}
}

type RoundRecord struct {
	N           int     `json:"n"`
	StartedAt   string  `json:"started_at"`
	Seconds     float64 `json:"seconds"`
	Usage       Usage   `json:"usage"`
	CostUSD     float64 `json:"cost_usd"`
	CostUnknown bool    `json:"cost_unknown,omitempty"`
	ExitCode    int     `json:"exit_code"`
	Subtype     string  `json:"subtype,omitempty"`
	Outcome     string  `json:"outcome,omitempty"`
}

type Run struct {
	ID           string          `json:"id"`
	CardID       string          `json:"card_id"`
	Workspace    string          `json:"workspace"`
	Adapter      string          `json:"adapter"`
	OnRamp       string          `json:"on_ramp"`
	ProjectDir   string          `json:"project_dir"`
	Worktree     string          `json:"worktree,omitempty"`
	Slug         string          `json:"slug,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	Status       string          `json:"status"`
	Budget       Budget          `json:"budget"`
	Usage        Usage           `json:"usage"`
	CostUSD      float64         `json:"cost_usd"`
	Rounds       int             `json:"rounds"`
	Seconds      float64         `json:"seconds"`
	StartedAt    string          `json:"started_at,omitempty"`
	EndedAt      string          `json:"ended_at,omitempty"`
	LogPath      string          `json:"log_path"`
	State        json.RawMessage `json:"state,omitempty"`
	StopReason   string          `json:"stop_reason,omitempty"`
	QueuedAt     string          `json:"queued_at"`
	NotBefore    string          `json:"not_before"`
	UpdatedAt    string          `json:"updated_at"`
	Phase        string          `json:"phase"`
	Retried      int             `json:"retried"`
	Relocations  []string        `json:"relocations"`
	AllowedTools []string        `json:"allowed_tools"`
	RoundLog     []RoundRecord   `json:"round_log"`
	Kind         string          `json:"kind"`
}

func (r Run) Active() bool { return slices.Contains(ActiveStatuses, r.Status) }

// Exceeded names the first limit the run has reached, or "".
func (r Run) Exceeded() string {
	b := r.Budget
	switch {
	case b.Rounds > 0 && r.Rounds >= b.Rounds:
		return "rounds"
	case b.Tokens > 0 && r.Usage.Tokens() >= b.Tokens:
		return "tokens"
	case b.USD > 0 && r.CostUSD >= b.USD:
		return "usd"
	case b.Minutes > 0 && r.Seconds/60 >= b.Minutes:
		return "minutes"
	}
	return ""
}

const runCols = `id, card_id, workspace, adapter, on_ramp, project_dir, COALESCE(worktree, ''), COALESCE(slug, ''), COALESCE(session_id, ''),
	status, budget, usage, cost_usd, rounds, seconds, COALESCE(started_at, ''), COALESCE(ended_at, ''), log_path, COALESCE(state, ''),
	COALESCE(stop_reason, ''), queued_at, not_before, updated_at, phase, retried, relocations, allowed_tools, round_log, kind`

func scanRun(r scanner) (Run, error) {
	var x Run
	var budget, usage, state, reloc, tools, rlog string
	err := r.Scan(&x.ID, &x.CardID, &x.Workspace, &x.Adapter, &x.OnRamp, &x.ProjectDir, &x.Worktree, &x.Slug, &x.SessionID,
		&x.Status, &budget, &usage, &x.CostUSD, &x.Rounds, &x.Seconds, &x.StartedAt, &x.EndedAt, &x.LogPath, &state,
		&x.StopReason, &x.QueuedAt, &x.NotBefore, &x.UpdatedAt, &x.Phase, &x.Retried, &reloc, &tools, &rlog, &x.Kind)
	if err != nil {
		return x, err
	}
	_ = json.Unmarshal([]byte(budget), &x.Budget)
	_ = json.Unmarshal([]byte(usage), &x.Usage)
	if state != "" {
		x.State = json.RawMessage(state)
	}
	_ = json.Unmarshal([]byte(reloc), &x.Relocations)
	_ = json.Unmarshal([]byte(tools), &x.AllowedTools)
	_ = json.Unmarshal([]byte(rlog), &x.RoundLog)
	return x, nil
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Store) CreateRun(r Run) (Run, error) {
	if !slices.Contains([]string{AdapterClaude, AdapterProcess}, r.Adapter) {
		return Run{}, fmt.Errorf("adapter %q: use claude or process", r.Adapter)
	}
	if !slices.Contains([]string{OnRampKickoff, OnRampFeature, OnRampFix, OnRampBuild, OnRampResume}, r.OnRamp) {
		return Run{}, fmt.Errorf("on-ramp %q is not known", r.OnRamp)
	}
	if _, err := s.Card(r.CardID); err != nil {
		return Run{}, err
	}
	now := Now()
	r.ID = NewID("r")
	r.Workspace = s.Workspace
	if r.Status == "" {
		r.Status = StatusQueued
	}
	if r.Budget == (Budget{}) {
		r.Budget = s.DefaultBudget()
	}
	if r.Phase == "" {
		r.Phase = PhaseStart
	}
	if r.QueuedAt == "" {
		r.QueuedAt = now
	}
	if r.NotBefore == "" {
		r.NotBefore = r.QueuedAt
	}
	r.UpdatedAt = now
	r.LogPath = filepath.Join(RunsDir(s.Workspace), r.ID+".log")
	if r.Relocations == nil {
		r.Relocations = []string{}
	}
	if r.AllowedTools == nil {
		r.AllowedTools = []string{}
	}
	if r.RoundLog == nil {
		r.RoundLog = []RoundRecord{}
	}
	_, err := s.DB.Exec(`INSERT INTO runs(id, card_id, workspace, adapter, on_ramp, project_dir, worktree, slug, session_id, status, budget,
		usage, log_path, queued_at, not_before, updated_at, phase, relocations, allowed_tools, round_log, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.CardID, r.Workspace, r.Adapter, r.OnRamp, r.ProjectDir, nullStr(r.Worktree), nullStr(r.Slug), nullStr(r.SessionID),
		r.Status, jsonText(r.Budget), jsonText(r.Usage), r.LogPath, r.QueuedAt, r.NotBefore, r.UpdatedAt, r.Phase,
		jsonText(r.Relocations), jsonText(r.AllowedTools), jsonText(r.RoundLog), r.Kind)
	if err != nil {
		return Run{}, err
	}
	s.Publish(Change{Kind: "run", CardID: r.CardID, RunID: r.ID})
	return s.Run(r.ID)
}

func (s *Store) Run(id string) (Run, error) {
	r, err := scanRun(s.DB.QueryRow(`SELECT `+runCols+` FROM runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("run %s: %w", id, ErrNotFound)
	}
	return r, err
}

func (s *Store) SaveRun(r Run) error {
	r.UpdatedAt = Now()
	var state any
	if len(r.State) > 0 {
		state = string(r.State)
	}
	_, err := s.DB.Exec(`UPDATE runs SET status = ?, worktree = ?, slug = ?, session_id = ?, budget = ?, usage = ?, cost_usd = ?,
		rounds = ?, seconds = ?, started_at = ?, ended_at = ?, state = ?, stop_reason = ?, not_before = ?, updated_at = ?, phase = ?,
		retried = ?, relocations = ?, allowed_tools = ?, round_log = ? WHERE id = ?`,
		r.Status, nullStr(r.Worktree), nullStr(r.Slug), nullStr(r.SessionID), jsonText(r.Budget), jsonText(r.Usage), r.CostUSD,
		r.Rounds, r.Seconds, nullStr(r.StartedAt), nullStr(r.EndedAt), state, nullStr(s.Mask(r.StopReason)), r.NotBefore, r.UpdatedAt,
		r.Phase, r.Retried, jsonText(nonNil(r.Relocations)), jsonText(nonNil(r.AllowedTools)), jsonText(r.RoundLog), r.ID)
	if err == nil {
		s.Publish(Change{Kind: "run", CardID: r.CardID, RunID: r.ID})
	}
	return err
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func (s *Store) runs(q string, args ...any) ([]Run, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RunsForCard(cardID string) ([]Run, error) {
	return s.runs(`SELECT `+runCols+` FROM runs WHERE card_id = ? ORDER BY queued_at DESC, rowid DESC`, cardID)
}

func (s *Store) ActiveRun(cardID string) (Run, bool, error) {
	rs, err := s.runs(`SELECT `+runCols+` FROM runs WHERE card_id = ? AND status IN ('queued','running','waiting')
		ORDER BY queued_at DESC, rowid DESC LIMIT 1`, cardID)
	if err != nil || len(rs) == 0 {
		return Run{}, false, err
	}
	return rs[0], true, nil
}

func (s *Store) DueRuns(now time.Time, limit int) ([]Run, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.runs(`SELECT `+runCols+` FROM runs WHERE status = 'queued' AND not_before <= ? ORDER BY queued_at, rowid LIMIT ?`,
		now.UTC().Format(time.RFC3339), limit)
}

func (s *Store) RunsWithStatus(statuses ...string) ([]Run, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	q := `SELECT ` + runCols + ` FROM runs WHERE status IN (?` + strings.Repeat(",?", len(statuses)-1) + `) ORDER BY queued_at`
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}
	return s.runs(q, args...)
}

type Lock struct {
	CardID    string `json:"card_id"`
	RunID     string `json:"run_id"`
	RunStatus string `json:"run_status"`
}

func (s *Store) Locks() ([]Lock, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.locked_by_run, COALESCE(r.status, 'missing') FROM cards c
		LEFT JOIN runs r ON r.id = c.locked_by_run WHERE c.locked_by_run IS NOT NULL ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lock
	for rows.Next() {
		var l Lock
		if err := rows.Scan(&l.CardID, &l.RunID, &l.RunStatus); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
```

`internal/hub/store/decisions.go`:

```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

const (
	KindPlanApproval = "plan-approval"
	KindResult       = "result"
	KindJudge        = "judge"
	KindQuestion     = "question"
	KindPermission   = "permission" // the hub's own kind: permission asks and over-budget questions; no gate record

	DecisionPending  = "pending"
	DecisionAnswered = "answered"
)

type Decision struct {
	ID         string         `json:"id"`
	CardID     string         `json:"card_id"`
	RunID      string         `json:"run_id,omitempty"`
	Slug       string         `json:"slug,omitempty"`
	Kind       string         `json:"kind"`
	Checkpoint string         `json:"checkpoint,omitempty"`
	Reviewer   string         `json:"reviewer,omitempty"`
	Text       string         `json:"text"`
	Options    []string       `json:"options"`
	Status     string         `json:"status"`
	Choice     string         `json:"choice,omitempty"`
	Note       string         `json:"note,omitempty"`
	Actor      string         `json:"actor,omitempty"`
	AskedAt    string         `json:"asked_at,omitempty"`
	AnsweredAt string         `json:"answered_at,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

func (d Decision) HubOwned() bool { return d.Kind == KindPermission }

type MirrorResult struct{ Inserted, NowAnswered bool }

// ActorKind buckets an answer's actor for the activity log.
func ActorKind(actor string) string {
	switch {
	case strings.HasPrefix(actor, "hub:"):
		return "hub"
	case actor == "terminal":
		return "terminal"
	}
	return "other"
}

const decisionCols = `id, card_id, COALESCE(run_id, ''), COALESCE(slug, ''), kind, COALESCE(checkpoint, ''), COALESCE(reviewer, ''),
	text, options, status, COALESCE(choice, ''), COALESCE(note, ''), COALESCE(actor, ''), COALESCE(asked_at, ''), COALESCE(answered_at, ''), data`

func scanDecision(r scanner) (Decision, error) {
	var d Decision
	var opts, data string
	err := r.Scan(&d.ID, &d.CardID, &d.RunID, &d.Slug, &d.Kind, &d.Checkpoint, &d.Reviewer, &d.Text, &opts, &d.Status,
		&d.Choice, &d.Note, &d.Actor, &d.AskedAt, &d.AnsweredAt, &data)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(opts), &d.Options)
	_ = json.Unmarshal([]byte(data), &d.Data)
	return d, nil
}

func (s *Store) Decision(id string) (Decision, error) {
	d, err := scanDecision(s.DB.QueryRow(`SELECT `+decisionCols+` FROM decisions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("decision %s: %w", id, ErrNotFound)
	}
	return d, err
}

// UpsertDecision mirrors one gate record keyed by the gate's id. A pending row flips to answered once;
// an answered row never changes again.
func (s *Store) UpsertDecision(d Decision) (MirrorResult, error) {
	cur, err := s.Decision(d.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MirrorResult{}, err
	}
	if errors.Is(err, ErrNotFound) {
		if d.Status == "" {
			d.Status = DecisionPending
		}
		if d.AskedAt == "" {
			d.AskedAt = Now()
		}
		if d.Options == nil {
			d.Options = []string{}
		}
		if d.Data == nil {
			d.Data = map[string]any{}
		}
		if _, err := s.DB.Exec(`INSERT INTO decisions(id, card_id, run_id, slug, kind, checkpoint, reviewer, text, options, status,
			choice, note, actor, asked_at, answered_at, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.CardID, nullStr(d.RunID), nullStr(d.Slug), d.Kind, nullStr(d.Checkpoint), nullStr(d.Reviewer), s.Mask(d.Text),
			jsonText(d.Options), d.Status, nullStr(d.Choice), nullStr(s.Mask(d.Note)), nullStr(d.Actor), d.AskedAt, nullStr(d.AnsweredAt),
			jsonText(d.Data)); err != nil {
			return MirrorResult{}, err
		}
		if err := s.Log(activity.Entry{Actor: "hub", Verb: activity.VerbDecisionAsk, CardID: d.CardID, RunID: d.RunID,
			Data: map[string]any{"decision": d.ID, "kind": d.Kind}}); err != nil {
			return MirrorResult{}, err
		}
		res := MirrorResult{Inserted: true}
		if d.Status == DecisionAnswered {
			if err := s.logAnswer(d); err != nil {
				return res, err
			}
			res.NowAnswered = true
		}
		s.Publish(Change{Kind: "decision", CardID: d.CardID, RunID: d.RunID})
		return res, nil
	}
	if cur.Status == DecisionAnswered || d.Status != DecisionAnswered {
		return MirrorResult{}, nil
	}
	if d.AnsweredAt == "" {
		d.AnsweredAt = Now()
	}
	if _, err := s.DB.Exec(`UPDATE decisions SET status = 'answered', choice = ?, note = ?, actor = ?, answered_at = ? WHERE id = ? AND status = 'pending'`,
		d.Choice, s.Mask(d.Note), d.Actor, d.AnsweredAt, d.ID); err != nil {
		return MirrorResult{}, err
	}
	cur.Choice, cur.Actor = d.Choice, d.Actor
	if err := s.logAnswer(cur); err != nil {
		return MirrorResult{}, err
	}
	s.Publish(Change{Kind: "decision", CardID: cur.CardID, RunID: cur.RunID})
	return MirrorResult{NowAnswered: true}, nil
}

func (s *Store) logAnswer(d Decision) error {
	return s.Log(activity.Entry{Actor: d.Actor, Verb: activity.VerbDecisionAnswer, CardID: d.CardID, RunID: d.RunID,
		Data: map[string]any{"decision": d.ID, "kind": d.Kind, "choice": d.Choice, "actor_kind": ActorKind(d.Actor)}})
}

// AskHub records one of the hub's own decisions (permission and over-budget).
func (s *Store) AskHub(cardID, runID, slug, text string, options []string, data map[string]any) (Decision, error) {
	d := Decision{ID: NewID("d"), CardID: cardID, RunID: runID, Slug: slug, Kind: KindPermission, Text: text,
		Options: options, Status: DecisionPending, AskedAt: Now(), Data: data}
	if _, err := s.UpsertDecision(d); err != nil {
		return Decision{}, err
	}
	return s.Decision(d.ID)
}

func (s *Store) AnswerHub(id, choice, note, actor string) (Decision, error) {
	d, err := s.Decision(id)
	if err != nil {
		return d, err
	}
	if !d.HubOwned() {
		return d, fmt.Errorf("decision %s belongs to the gate: answer it with tq hub answer", id)
	}
	if d.Status != DecisionPending {
		return d, fmt.Errorf("decision %s is already answered (%s by %s)", id, d.Choice, d.Actor)
	}
	if !slices.Contains(d.Options, choice) {
		return d, fmt.Errorf("choice must be one of %s", strings.Join(d.Options, ", "))
	}
	if _, err := s.UpsertDecision(Decision{ID: id, Status: DecisionAnswered, Choice: choice, Note: note, Actor: actor, AnsweredAt: Now()}); err != nil {
		return d, err
	}
	return s.Decision(id)
}

func (s *Store) decisions(q string, args ...any) ([]Decision, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DecisionsForCard(cardID string) ([]Decision, error) {
	return s.decisions(`SELECT `+decisionCols+` FROM decisions WHERE card_id = ? ORDER BY asked_at DESC, rowid DESC`, cardID)
}

func (s *Store) PendingDecisions() ([]Decision, error) {
	return s.decisions(`SELECT ` + decisionCols + ` FROM decisions WHERE status = 'pending' ORDER BY asked_at DESC, rowid DESC`)
}

func (s *Store) PendingCount(cardID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM decisions WHERE card_id = ? AND status = 'pending'`, cardID).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/hub/store/; go test ./internal/hub/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hub/store/runs.go internal/hub/store/decisions.go internal/hub/store/runs_test.go internal/hub/store/decisions_test.go
git commit -m "feat(hub): runs with budgets and round log, mirrored and hub-owned decisions"
```

---
### Task 5: Adapters: the contract, `process`, `claude`, the launch environment and the fake `claude`

**Files:**
- Create: `internal/hub/adapter/adapter.go`, `internal/hub/adapter/process.go`, `internal/hub/adapter/claude.go`, `internal/hub/adapter/terminate_unix.go`, `internal/hub/adapter/terminate_windows.go`, `internal/hub/adapter/testdata/fake_claude.py`, `internal/testutil/python.go`
- Test: `internal/hub/adapter/process_test.go`, `internal/hub/adapter/claude_test.go`

**Interfaces:**
- Consumes: `insights.LookupPrice(model string) (PriceRow, bool)`, `insights.CostUSD(model string, u insights.Usage) float64`, `insights.Usage{Input, Output, CacheRead, CacheWrite int64}`.
- Produces (package `adapter`):
  - The contract of spec §8.3: `type Adapter interface { Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error) }`; `type RunRequest struct { Workspace, Dir, Prompt, SessionID string; Env []string; AllowedTools []string }`; `type RunResult struct { ExitCode int; SessionID string; Usage Usage; CostUSD float64; StopReason string; Transcript string; CostKnown, UsageKnown bool; Model string; NumTurns int; PermissionDenials []PermissionDenial }`; `type Usage struct { Input, Output, CacheRead, CacheWrite int64 }` (same fields as `store.Usage`, so `store.Usage(res.Usage)` converts); `type PermissionDenial struct { Tool string; Input map[string]any }` (JSON `tool_name`, `tool_input`).
  - `Start` returns an error only when the process could not start or `ctx` was cancelled; a non-zero exit is `RunResult.ExitCode`. Cancelling `ctx` sends the platform's termination signal (Unix `SIGTERM`, Windows `taskkill /T`), then kills after the grace period (default `DefaultGrace = 10 * time.Second`).
  - `type Process struct { Command []string; Grace time.Duration }` (prompt on stdin and in `TQ_HUB_PROMPT`; no session, no usage; `StopReason` = `"exit N"`).
  - `type Claude struct { Bin []string; Args func([]string) []string; Grace time.Duration }`; `(Claude).Argv(req RunRequest) []string`; `ParseResult(out []byte) RunResult`; `ToolSpec(d PermissionDenial) string` (`Bash(<command>)` for Bash, else the tool name).
  - `LaunchEnv(base []string, hubTask string) []string`: drops `TENTAQLES_HEADLESS_CHILD`, `TQ_BUILD_ACTOR`, `TQ_BUILD_TOKEN`, any old `TENTAQLES_HEADLESS`/`TQ_HUB_TASK`, then adds `TENTAQLES_HEADLESS=1` and `TQ_HUB_TASK=<hubTask>` (keys compared case-insensitively on Windows).
- Produces (package `testutil`): `Python(t testing.TB) []string` (argv prefix of a working Python 3, or `t.Skip`).
- Produces (test fixture): `internal/hub/adapter/testdata/fake_claude.py`, driven by `$FAKE_CLAUDE_SCRIPT` (format in its docstring); later tasks and the integration test use it.

- [ ] **Step 1: Write the fake `claude` and the Python finder**

`internal/hub/adapter/testdata/fake_claude.py`:

```python
"""Stand-in for `claude -p <prompt> --output-format json` in the hub tests.

Each call plays the next round of the JSON script named by $FAKE_CLAUDE_SCRIPT and
prints a result document shaped like Claude Code's JSON output:

    {"counter": "<file>", "argv_log": "<file>",
     "rounds": [{"subtype": "success",            # success | error_max_turns | error_during_execution
                 "result": "final text",
                 "session_id": "s1",
                 "cost": 0.25,                    # missing or null: no total_cost_usd field
                 "usage": {"input_tokens": 10, "output_tokens": 5,
                           "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0},  # null: no usage
                 "model": "claude-opus-5-5",       # names the modelUsage key; null: no modelUsage
                 "num_turns": 3, "exit": 0,
                 "permission_denials": [{"tool_name": "Bash", "tool_input": {"command": "npm test"}}],
                 "write": {"<absolute path>": "<text>"},
                 "write_json": {"<absolute path>": {"any": "json"}},   # gate state fixtures
                 "sleep": 0,
                 "raw": null}]}                   # a string: printed instead of the JSON

Rounds past the end repeat the last one. Every call appends what it saw (argv, cwd and the
hub environment) to argv_log. Standard library only, platform-neutral.
"""

import json
import os
import sys
import time

with open(os.environ["FAKE_CLAUDE_SCRIPT"], encoding="utf-8") as fh:
    script = json.load(fh)
counter = script["counter"]
n = 0
if os.path.exists(counter):
    with open(counter, encoding="utf-8") as fh:
        n = int(fh.read() or "0")
with open(counter, "w", encoding="utf-8") as fh:
    fh.write(str(n + 1))
with open(script["argv_log"], "a", encoding="utf-8") as fh:
    fh.write(json.dumps({
        "round": n,
        "argv": sys.argv[1:],
        "cwd": os.getcwd(),
        "headless": os.environ.get("TENTAQLES_HEADLESS"),
        "headless_child": "TENTAQLES_HEADLESS_CHILD" in os.environ,
        "build_actor": "TQ_BUILD_ACTOR" in os.environ,
        "build_token": "TQ_BUILD_TOKEN" in os.environ,
        "hub_task": os.environ.get("TQ_HUB_TASK"),
        "ws": os.environ.get("TQ_WS"),
    }) + "\n")
rounds = script["rounds"]
act = rounds[n] if n < len(rounds) else rounds[-1]
if act.get("sleep"):
    time.sleep(act["sleep"])
for path, text in (act.get("write") or {}).items():
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(text)
for path, obj in (act.get("write_json") or {}).items():
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        json.dump(obj, fh, indent=2)
sys.stderr.write("fake claude round %d\n" % n)
if act.get("raw") is not None:
    print(act["raw"])
else:
    subtype = act.get("subtype", "success")
    out = {"type": "result", "subtype": subtype, "is_error": subtype != "success",
           "num_turns": act.get("num_turns", 1), "result": act.get("result", ""),
           "session_id": act.get("session_id", "sess-%d" % n)}
    if act.get("cost") is not None:
        out["total_cost_usd"] = act["cost"]
    if act.get("usage") is not None:
        out["usage"] = act["usage"]
    if act.get("model"):
        out["modelUsage"] = {act["model"]: {"inputTokens": 1, "outputTokens": 1}}
    if act.get("permission_denials"):
        out["permission_denials"] = act["permission_denials"]
    print(json.dumps(out))
sys.exit(int(act.get("exit", 0)))
```

`internal/testutil/python.go`:

```go
package testutil

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
)

var (
	pythonOnce sync.Once
	pythonArgv []string
)

// Python returns the argv prefix of a working Python 3 (python3, python, then py -3; the
// Windows Store alias is skipped), or skips the test when there is none.
func Python(t testing.TB) []string {
	t.Helper()
	pythonOnce.Do(func() { pythonArgv = findPython() })
	if pythonArgv == nil {
		t.Skip("python 3 not found")
	}
	return append([]string(nil), pythonArgv...)
}

func findPython() []string {
	for _, c := range [][]string{{"python3"}, {"python"}, {"py", "-3"}} {
		p, err := exec.LookPath(c[0])
		if err != nil || strings.Contains(strings.ToLower(p), "windowsapps") {
			continue
		}
		argv := append([]string{p}, c[1:]...)
		out, err := exec.Command(argv[0], append(argv[1:], "-c", "import sys; print(sys.version_info[0])")...).Output()
		if err == nil && strings.TrimSpace(string(out)) == "3" {
			return argv
		}
	}
	return nil
}
```

- [ ] **Step 2: Write the failing tests**

`internal/hub/adapter/process_test.go`:

```go
package adapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain doubles as the helper process the process adapter runs (the test binary re-executed).
func TestMain(m *testing.M) {
	switch os.Getenv("TQ_HUB_HELPER") {
	case "echo":
		prompt, _ := io.ReadAll(os.Stdin)
		wd, _ := os.Getwd()
		fmt.Printf("prompt=%s env=%s wd=%s\n", prompt, os.Getenv("TQ_HUB_PROMPT"), filepath.Base(wd))
		os.Exit(0)
	case "fail":
		fmt.Println("boom on stdout")
		fmt.Fprintln(os.Stderr, "boom on stderr")
		os.Exit(3)
	case "sleep":
		fmt.Println("sleeping")
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string               { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func helperReq(t *testing.T, name, prompt string) RunRequest {
	dir := t.TempDir()
	return RunRequest{Workspace: "acme", Dir: dir, Prompt: prompt, Env: append(os.Environ(), "TQ_HUB_HELPER="+name)}
}

func TestProcessAdapterRunsInDirWithPrompt(t *testing.T) {
	var log syncBuf
	req := helperReq(t, "echo", "do it")
	res, err := Process{Command: []string{os.Args[0]}}.Start(context.Background(), req, &log)
	if err != nil {
		t.Fatal(err)
	}
	want := "prompt=do it env=do it wd=" + filepath.Base(req.Dir)
	if res.ExitCode != 0 || !strings.Contains(log.String(), want) || !strings.Contains(res.Transcript, want) {
		t.Fatalf("res %+v log %q", res, log.String())
	}
	if res.SessionID != "" || res.Usage != (Usage{}) || res.CostKnown {
		t.Fatal("the process adapter has no session and no usage")
	}
}

func TestProcessAdapterReportsExitCode(t *testing.T) {
	var log syncBuf
	res, err := Process{Command: []string{os.Args[0]}}.Start(context.Background(), helperReq(t, "fail", ""), &log)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.StopReason != "exit 3" || !strings.Contains(log.String(), "boom on stderr") {
		t.Fatalf("res %+v log %q", res, log.String())
	}
}

func TestProcessAdapterCancelTerminates(t *testing.T) {
	var log syncBuf
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := Process{Command: []string{os.Args[0]}, Grace: time.Second}.Start(ctx, helperReq(t, "sleep", ""), &log)
		done <- err
	}()
	for !strings.Contains(log.String(), "sleeping") {
		if time.Since(start) > 20*time.Second {
			t.Fatal("helper never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled run returns the context error")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancel did not stop the process within the grace period")
	}
}

func TestLaunchEnv(t *testing.T) {
	base := []string{"PATH=/bin", "TENTAQLES_HEADLESS_CHILD=1", "TQ_BUILD_ACTOR=hub:x", "TQ_BUILD_TOKEN=abc", "TENTAQLES_HEADLESS=0", "TQ_HUB_TASK=old"}
	got := LaunchEnv(base, `{"hub":"c-abcdefghijkl"}`)
	joined := strings.Join(got, "\n")
	for _, bad := range []string{"TENTAQLES_HEADLESS_CHILD", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN", "TENTAQLES_HEADLESS=0", "TQ_HUB_TASK=old"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("%s survived: %v", bad, got)
		}
	}
	if !strings.Contains(joined, "TENTAQLES_HEADLESS=1") || !strings.Contains(joined, `TQ_HUB_TASK={"hub":"c-abcdefghijkl"}`) || !strings.Contains(joined, "PATH=/bin") {
		t.Fatalf("env %v", got)
	}
}
```

`internal/hub/adapter/claude_test.go`:

```go
package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

type fakeClaude struct {
	bin     []string
	dir     string
	argvLog string
}

func newFakeClaude(t *testing.T, rounds []map[string]any) fakeClaude {
	t.Helper()
	py := testutil.Python(t)
	dir := t.TempDir()
	scriptPath, err := filepath.Abs(filepath.Join("testdata", "fake_claude.py")) // the round runs in dir, not here
	if err != nil {
		t.Fatal(err)
	}
	f := fakeClaude{bin: append(py, scriptPath), dir: dir, argvLog: filepath.Join(dir, "argv.jsonl")}
	script := map[string]any{"counter": filepath.Join(dir, "counter"), "argv_log": f.argvLog, "rounds": rounds}
	raw, _ := json.Marshal(script)
	p := filepath.Join(dir, "script.json")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CLAUDE_SCRIPT", p)
	return f
}

func (f fakeClaude) calls(t *testing.T) []map[string]any {
	t.Helper()
	fh, err := os.Open(f.argvLog)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	var out []map[string]any
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		var m map[string]any
		json.Unmarshal(sc.Bytes(), &m)
		out = append(out, m)
	}
	return out
}

func run(t *testing.T, c Claude, req RunRequest) (RunResult, string) {
	t.Helper()
	var log syncBuf
	res, err := c.Start(context.Background(), req, &log)
	if err != nil {
		t.Fatal(err)
	}
	return res, log.String()
}

func TestClaudeArgvAndPermissionWrapper(t *testing.T) {
	c := Claude{Bin: []string{"claude"}, Args: func(a []string) []string { return append([]string{"--permission-mode", "acceptEdits"}, a...) }}
	got := c.Argv(RunRequest{Prompt: "/tentaqles:feature x --headless", SessionID: "s1", AllowedTools: []string{"Bash(npm test)", "Edit"}})
	want := []string{"claude", "--permission-mode", "acceptEdits", "-p", "/tentaqles:feature x --headless", "--output-format", "json",
		"--resume", "s1", "--allowedTools", "Bash(npm test),Edit"}
	if !slices.Equal(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
	plain := Claude{}.Argv(RunRequest{Prompt: "p"})
	if !slices.Equal(plain, []string{"claude", "-p", "p", "--output-format", "json"}) || slices.Contains(plain, "--dangerously-skip-permissions") {
		t.Fatalf("plain argv %q", plain)
	}
}

func TestClaudeAdapterSuccess(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "done\nall good", "session_id": "s-42", "cost": 0.75, "num_turns": 4,
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "cache_creation_input_tokens": 5, "cache_read_input_tokens": 1000}}})
	res, log := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "/tentaqles:feature x --headless", Env: LaunchEnv(os.Environ(), `{"hub":"c-x"}`)})
	if res.ExitCode != 0 || res.SessionID != "s-42" || res.CostUSD != 0.75 || !res.CostKnown || res.StopReason != "success" || res.NumTurns != 4 {
		t.Fatalf("res %+v", res)
	}
	if res.Usage != (Usage{Input: 100, Output: 20, CacheRead: 1000, CacheWrite: 5}) || res.Transcript != "done\nall good" {
		t.Fatalf("usage %+v transcript %q", res.Usage, res.Transcript)
	}
	if !strings.Contains(log, "fake claude round 0") {
		t.Fatalf("stderr goes to the log: %q", log)
	}
	call := f.calls(t)[0]
	if call["headless"] != "1" || call["headless_child"] != false || call["build_token"] != false || call["hub_task"] != `{"hub":"c-x"}` {
		t.Fatalf("environment seen by claude: %v", call)
	}
}

func TestClaudeAdapterCostFallback(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "ok", "model": "claude-opus-5-5",
		"usage": map[string]any{"input_tokens": 1_000_000, "output_tokens": 100_000, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}}})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	// opus-5-5 list price: $4 per MTok in, $20 per MTok out
	if !res.CostKnown || math.Abs(res.CostUSD-6.0) > 1e-9 || res.Model != "claude-opus-5-5" {
		t.Fatalf("cost %v known %v model %q", res.CostUSD, res.CostKnown, res.Model)
	}
}

func TestClaudeAdapterNoUsageNoCost(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "ok"}})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.CostKnown || res.UsageKnown || res.CostUSD != 0 || res.Usage != (Usage{}) {
		t.Fatalf("res %+v", res)
	}
	g := newFakeClaude(t, []map[string]any{{"result": "ok", "model": "some-unpriced-model", "usage": map[string]any{"input_tokens": 10}}})
	res, _ = run(t, Claude{Bin: g.bin}, RunRequest{Dir: g.dir, Prompt: "p", Env: os.Environ()})
	if res.CostKnown || !res.UsageKnown || res.Usage.Input != 10 {
		t.Fatalf("an unpriced model leaves the cost unknown: %+v", res)
	}
}

func TestClaudeAdapterErrorShapes(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{
		{"subtype": "error_max_turns", "result": "ran out", "cost": 0.1},
		{"subtype": "error_during_execution", "result": "Bash was not allowed", "exit": 1,
			"permission_denials": []any{map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "npm test"}}}},
		{"raw": "this is not json"},
	})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "error_max_turns" || res.ExitCode != 0 {
		t.Fatalf("max turns %+v", res)
	}
	res, _ = run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "error_during_execution" || res.ExitCode != 1 || len(res.PermissionDenials) != 1 || ToolSpec(res.PermissionDenials[0]) != "Bash(npm test)" {
		t.Fatalf("denial %+v", res)
	}
	res, _ = run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "unparseable output" || !strings.Contains(res.Transcript, "this is not json") {
		t.Fatalf("raw %+v", res)
	}
}

func TestToolSpec(t *testing.T) {
	if got := ToolSpec(PermissionDenial{Tool: "WebFetch"}); got != "WebFetch" {
		t.Fatal(got)
	}
	if got := ToolSpec(PermissionDenial{Tool: "Bash", Input: map[string]any{"command": "  go test ./...  "}}); got != "Bash(go test ./...)" {
		t.Fatal(got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/hub/adapter/ -count=1`
Expected: FAIL, `undefined: Process`, `undefined: Claude`, `undefined: LaunchEnv`.

- [ ] **Step 4: Implement**

`internal/hub/adapter/adapter.go`:

```go
// Package adapter runs one round of an agent for the hub runner: the contract of
// spec §8.3, the plain-process adapter used by tests and scripted tasks, and the
// headless `claude -p --output-format json` adapter.
package adapter

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DefaultGrace is how long a cancelled run gets between the termination signal and the kill.
const DefaultGrace = 10 * time.Second

type Adapter interface {
	// Start launches one round in req.Dir with req.Env, writes stdout/stderr to log, and
	// returns when the process exits. It never blocks on a prompt.
	Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error)
}

type RunRequest struct {
	Workspace, Dir, Prompt, SessionID string
	Env                               []string
	AllowedTools                      []string
}

type Usage struct {
	Input, Output, CacheRead, CacheWrite int64
}

type PermissionDenial struct {
	Tool  string         `json:"tool_name"`
	Input map[string]any `json:"tool_input"`
}

type RunResult struct {
	ExitCode          int
	SessionID         string
	Usage             Usage
	CostUSD           float64
	StopReason        string
	Transcript        string
	CostKnown         bool
	UsageKnown        bool
	Model             string
	NumTurns          int
	PermissionDenials []PermissionDenial
}

func command(ctx context.Context, argv []string, req RunRequest, grace time.Duration) *exec.Cmd {
	if grace <= 0 {
		grace = DefaultGrace
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	cmd.Cancel = func() error { return terminate(cmd.Process, grace) }
	cmd.WaitDelay = grace + 5*time.Second
	return cmd
}

// exitCode maps cmd.Run's error: a non-zero exit is a result, a failure to start or a cancel is an error.
func exitCode(ctx context.Context, err error) (int, error) {
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

// tail keeps the last max bytes written to it.
type tail struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { t.mu.Lock(); defer t.mu.Unlock(); return string(t.b) }

// locked serializes writes from stdout and stderr into one log writer.
type locked struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *locked) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.w.Write(p) }

func envKey(kv string) string {
	k, _, _ := strings.Cut(kv, "=")
	if runtime.GOOS == "windows" {
		return strings.ToUpper(k)
	}
	return k
}

// LaunchEnv is the environment of every hub-launched round: the workspace's plan (base), never a
// hub token, never the plugin's headless-child marker (the session hooks must run), plus the
// headless switch and the card.
func LaunchEnv(base []string, hubTask string) []string {
	drop := map[string]bool{"TENTAQLES_HEADLESS_CHILD": true, "TQ_BUILD_ACTOR": true, "TQ_BUILD_TOKEN": true,
		"TENTAQLES_HEADLESS": true, "TQ_HUB_TASK": true}
	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if !drop[strings.ToUpper(envKey(kv))] {
			out = append(out, kv)
		}
	}
	return append(out, "TENTAQLES_HEADLESS=1", "TQ_HUB_TASK="+hubTask)
}
```

`internal/hub/adapter/process.go`:

```go
package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Process runs an arbitrary command with the run's environment (tests, scripted tasks).
type Process struct {
	Command []string
	Grace   time.Duration
}

func (p Process) Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error) {
	if len(p.Command) == 0 {
		return RunResult{}, errors.New("process adapter: no command configured")
	}
	r := req
	r.Env = append(append([]string(nil), req.Env...), "TQ_HUB_PROMPT="+req.Prompt)
	cmd := command(ctx, p.Command, r, p.Grace)
	cmd.Stdin = strings.NewReader(req.Prompt)
	t := &tail{max: 8 << 10}
	w := &locked{w: io.MultiWriter(log, t)}
	cmd.Stdout, cmd.Stderr = w, w
	code, err := exitCode(ctx, cmd.Run())
	return RunResult{ExitCode: code, StopReason: fmt.Sprintf("exit %d", code), Transcript: t.String()}, err
}
```

`internal/hub/adapter/claude.go`:

```go
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/insights"
)

// Claude runs `claude -p <prompt> --output-format json` headless. Args is tq run's
// permission-mode wrapper (claudeArgs): the manifest's mode, bypass only after
// `tq allow --bypass`; the hub never adds a permission flag of its own.
type Claude struct {
	Bin   []string
	Args  func([]string) []string
	Grace time.Duration
}

func (c Claude) Argv(req RunRequest) []string {
	args := []string{"-p", req.Prompt, "--output-format", "json"}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if len(req.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(req.AllowedTools, ","))
	}
	if c.Args != nil {
		args = c.Args(args)
	}
	bin := c.Bin
	if len(bin) == 0 {
		bin = []string{"claude"}
	}
	return append(append([]string(nil), bin...), args...)
}

// capped keeps at most max bytes (the JSON result); the log still gets everything.
type capped struct {
	max int
	b   bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

func (c Claude) Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error) {
	cmd := command(ctx, c.Argv(req), req, c.Grace)
	out := &capped{max: 16 << 20}
	lw := &locked{w: log}
	cmd.Stdout = io.MultiWriter(out, lw)
	cmd.Stderr = lw
	code, err := exitCode(ctx, cmd.Run())
	res := ParseResult(out.b.Bytes())
	res.ExitCode = code
	return res, err
}

type resultDoc struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	Result    string   `json:"result"`
	SessionID string   `json:"session_id"`
	TotalCost *float64 `json:"total_cost_usd"`
	NumTurns  int      `json:"num_turns"`
	Model     string   `json:"model"`
	Usage     *struct {
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	ModelUsage        map[string]json.RawMessage `json:"modelUsage"`
	PermissionDenials []PermissionDenial         `json:"permission_denials"`
}

// ParseResult reads the result document (the whole output, or its last JSON line). Missing cost
// is computed with insights.CostUSD from the usage and the model named in the output.
func ParseResult(out []byte) RunResult {
	var doc resultDoc
	ok := json.Unmarshal(bytes.TrimSpace(out), &doc) == nil && doc.Type == "result"
	if !ok {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			var d resultDoc
			if json.Unmarshal([]byte(strings.TrimSpace(lines[i])), &d) == nil && d.Type == "result" {
				doc, ok = d, true
				break
			}
		}
	}
	if !ok {
		t := string(out)
		if len(t) > 4000 {
			t = t[len(t)-4000:]
		}
		return RunResult{StopReason: "unparseable output", Transcript: t}
	}
	res := RunResult{SessionID: doc.SessionID, StopReason: doc.Subtype, Transcript: doc.Result, NumTurns: doc.NumTurns,
		PermissionDenials: doc.PermissionDenials}
	if doc.Usage != nil {
		res.Usage = Usage{doc.Usage.Input, doc.Usage.Output, doc.Usage.CacheRead, doc.Usage.CacheWrite}
		res.UsageKnown = true
	}
	res.Model = doc.Model
	if res.Model == "" && len(doc.ModelUsage) > 0 {
		models := make([]string, 0, len(doc.ModelUsage))
		for m := range doc.ModelUsage {
			models = append(models, m)
		}
		sort.Strings(models)
		res.Model = models[0] // several models (subagents): the whole usage is priced at the first; an estimate
	}
	switch {
	case doc.TotalCost != nil:
		res.CostUSD, res.CostKnown = *doc.TotalCost, true
	case res.UsageKnown && res.Model != "":
		if _, priced := insights.LookupPrice(res.Model); priced {
			res.CostUSD = insights.CostUSD(res.Model, insights.Usage{Input: res.Usage.Input, Output: res.Usage.Output,
				CacheRead: res.Usage.CacheRead, CacheWrite: res.Usage.CacheWrite})
			res.CostKnown = true
		}
	}
	return res
}

// ToolSpec is the --allowedTools entry that allows exactly what was refused.
func ToolSpec(d PermissionDenial) string {
	if d.Tool == "Bash" {
		if cmd, ok := d.Input["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			return fmt.Sprintf("Bash(%s)", strings.TrimSpace(cmd))
		}
	}
	return d.Tool
}
```

`internal/hub/adapter/terminate_unix.go`:

```go
//go:build !windows

package adapter

import (
	"os"
	"syscall"
	"time"
)

// terminate sends SIGTERM now and SIGKILL after grace.
func terminate(p *os.Process, grace time.Duration) error {
	if p == nil {
		return nil
	}
	_ = p.Signal(syscall.SIGTERM)
	go func() {
		time.Sleep(grace)
		_ = p.Kill()
	}()
	return nil
}
```

`internal/hub/adapter/terminate_windows.go`:

```go
//go:build windows

package adapter

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

// terminate asks the process tree to close (taskkill /T), then forces it after grace (taskkill /T /F):
// claude on Windows is a .cmd shim with a node child, so the tree must go, not only the shim.
func terminate(p *os.Process, grace time.Duration) error {
	if p == nil {
		return nil
	}
	pid := strconv.Itoa(p.Pid)
	_ = exec.Command("taskkill", "/PID", pid, "/T").Run()
	go func() {
		time.Sleep(grace)
		_ = exec.Command("taskkill", "/PID", pid, "/T", "/F").Run()
		_ = p.Kill()
	}()
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go vet ./internal/hub/adapter/ ./internal/testutil/; go test ./internal/hub/adapter/ -count=1 -v -run 'Test(Process|LaunchEnv|Claude|ToolSpec)'`
Expected: PASS. Without Python installed the `TestClaudeAdapter*` tests report SKIP; `TestClaudeArgvAndPermissionWrapper`, `TestToolSpec` and the process tests still run.

- [ ] **Step 6: Commit**

```bash
git add internal/hub/adapter/adapter.go internal/hub/adapter/process.go internal/hub/adapter/claude.go internal/hub/adapter/terminate_unix.go internal/hub/adapter/terminate_windows.go internal/hub/adapter/testdata/fake_claude.py internal/hub/adapter/process_test.go internal/hub/adapter/claude_test.go internal/testutil/python.go
git commit -m "feat(hub): adapter contract with process and headless claude adapters"
```

---
### Task 6: Gate client: `state`, `answer`, hub token minting, plugin discovery

**Files:**
- Create: `internal/hub/gate/gate.go`, `internal/hub/gate/token.go`, `internal/hub/gate/plugin.go`, `internal/hub/gate/testdata/fake_build_gate.py`, `internal/hub/gate/testdata/gen_hub_token_vectors.py`, `internal/hub/gate/testdata/hub_token_vectors.json`, `internal/hub/gate/testdata/state_pending.json`
- Test: `internal/hub/gate/token_test.go`, `internal/hub/gate/gate_test.go`, `internal/hub/gate/plugin_test.go`

**Interfaces:**
- Consumes: `paths.Home()`, `testutil.Python(t)` (Task 5).
- Produces (package `gate`):
  - The `build-gate state <slug> --json [--records]` document: `type State struct { Slug, Feature, Workflow string; Hotfix bool; Task *Task; Approved, RolledBack, Finished, Cleaned bool; Halted *Halted; Checkpoints []Checkpoint; DoD *DoD; DoDFresh bool; PendingDecisions, Decisions []Decision; Bases map[string]string; Records *Records; Raw json.RawMessage }` (`Raw` is the exact JSON the gate printed, not decoded from it); `type Task struct { Hub, Workspace, Kind string; External *External }`; `type External struct { Provider, ID, URL string }`; `type Halted struct { Reason string; Checkpoint CheckpointID }` (accepts a JSON string too); `type CheckpointID string` (accepts a JSON number or string); `type Checkpoint struct { ID CheckpointID; Title, Status, Evidence string; Gates, Reviews map[string]string }`; `type DoD struct { OK bool; Tree string; Override *string }`; `type Decision struct { ID, Kind string; Checkpoint CheckpointID; Reviewer, Text string; Options []string; Status, Choice, Note, Actor, AskedAt, AnsweredAt string }`.
  - `--records` (Task 14 adds it to the gate): `type Records struct { Plan *PlanRecord; Evidence []Evidence; Reviews []Review; DoD *DoDRecord }`; `type PlanRecord struct { Summary, PlainSummary, Spec string; Checkpoints []PlanCheckpoint }`; `type PlanCheckpoint struct { ID CheckpointID; Title string; Approved bool; Tests []PlanTest }`; `type PlanTest struct { File, Layer string }`; `type Evidence struct { Checkpoint CheckpointID; TS, Tree string; OK, Current bool; Summary string; Results []VerifyResult }`; `type VerifyResult struct { Cmd string; Exit int; Seconds float64; TimedOut bool }`; `type Review struct { Checkpoint CheckpointID; Reviewer, Result, Tree, TS string; Human bool; Blockers, Majors, Minors, Unjudged int }`; `type DoDRecord struct { OK bool; Tree, TS string; Override *string; Checks []DoDCheck }`; `type DoDCheck struct { ID string; OK *bool; Detail string }`. JSON keys are the snake-case names Task 14's `cmd_records` writes (`plain_summary`, `timed_out`, …).
  - `type API interface { State(ctx context.Context, dir, slug string, records bool) (*State, error); Answer(ctx context.Context, dir, slug, id, choice, note, actor string) (string, error); Slugs(dir string, since time.Time) []string }`.
  - `type Client struct { Command, Env []string; KeyPath string; Timeout time.Duration }` implements `API`; `type Unavailable struct{ Err error }` implements `API` (every call returns `Err`).
  - `type GateError struct { Code int; Msg string }`; `IsNoPlan(err) bool`, `IsNotPending(err) bool`, `IsHumanRefusal(err) bool`.
  - `Message(action, slug, actor string) string` (the compact ASCII JSON), `MintToken(key []byte, action, slug, actor string) string`, `KeyPath() string`, `Actor() string` (`hub:<os user>`), `Slugs(dir string, since time.Time) []string`.
  - `FindPluginRoot(configDir string) (string, error)` (`TQ_HUB_PLUGIN_ROOT` overrides), `FindBash() (string, error)` (`TQ_BASH` overrides; never WSL's `System32\bash.exe`), `ForWorkspace(configDir string, env []string) (*Client, error)` (`TQ_HUB_GATE_CMD`, a JSON argv, overrides everything; used by tests).
  - Fixture: `testdata/fake_build_gate.py` (state in `$FAKE_GATE_DIR/<slug>.json`, calls logged to `$FAKE_GATE_DIR/calls.jsonl`, `answer` verifies the token like `gate.hub_actor_ok`).

- [ ] **Step 1: Write the fixtures**

`internal/hub/gate/testdata/gen_hub_token_vectors.py`:

```python
"""Regenerate hub_token_vectors.json from the plugin's own token code (gate._hub_mac).

From the monorepo root:
    python internal/hub/gate/testdata/gen_hub_token_vectors.py > internal/hub/gate/testdata/hub_token_vectors.json
"""

import json
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "..", "..", "plugin"))
from tentaqles.workflow.gate import _hub_mac  # noqa: E402

KEY = bytes(range(32))
CASES = [
    ("answer", "demo", "hub:renato"),
    ("answer", "fix-login", "hub:Jos\u00e9"),
    ("answer", "demo", 'hub:a"b\\c'),
    ("answer", "demo", "hub:<x>&y"),
    ("answer", "demo", "hub:\U0001F600"),
    ("answer", "demo", "hub:tab\there\x7f"),
    ("approve", "demo", "hub:renato"),
]
out = {"key_hex": KEY.hex(), "vectors": [
    {"action": a, "slug": s, "actor": u,
     "message": json.dumps(["hub", a, s, u], ensure_ascii=True, separators=(",", ":")),
     "mac": _hub_mac(KEY, a, s, u)} for a, s, u in CASES]}
print(json.dumps(out, indent=2, ensure_ascii=True))
```

The one-liner that produced the committed fixture (run once from the monorepo root; re-run it whenever `gate._hub_mac` changes, and the Go test below must still pass):

```bash
python internal/hub/gate/testdata/gen_hub_token_vectors.py > internal/hub/gate/testdata/hub_token_vectors.json
```

`internal/hub/gate/testdata/hub_token_vectors.json` (generated by the command above against `plugin/tentaqles/workflow/gate.py` as merged on `main`):

```json
{
  "key_hex": "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
  "vectors": [
    {
      "action": "answer",
      "slug": "demo",
      "actor": "hub:renato",
      "message": "[\"hub\",\"answer\",\"demo\",\"hub:renato\"]",
      "mac": "84d6304294fd9b2090ca84890e4f08b31e5f1d35dfa894795fc8e136738605a4"
    },
    {
      "action": "answer",
      "slug": "fix-login",
      "actor": "hub:Jos\u00e9",
      "message": "[\"hub\",\"answer\",\"fix-login\",\"hub:Jos\\u00e9\"]",
      "mac": "1a1f21c11ba647ae743d0235f6263f6363824dae25e335e83577f80171399f67"
    },
    {
      "action": "answer",
      "slug": "demo",
      "actor": "hub:a\"b\\c",
      "message": "[\"hub\",\"answer\",\"demo\",\"hub:a\\\"b\\\\c\"]",
      "mac": "78d4fb675a0e152b102c6f674913374efbea47a4e4ade9ad0bd06ca8c5e34b10"
    },
    {
      "action": "answer",
      "slug": "demo",
      "actor": "hub:<x>&y",
      "message": "[\"hub\",\"answer\",\"demo\",\"hub:<x>&y\"]",
      "mac": "5f9024ae405319a2cfd4ca21ca95de8cbb9587a1e0e7a40840fddefa84335856"
    },
    {
      "action": "answer",
      "slug": "demo",
      "actor": "hub:\ud83d\ude00",
      "message": "[\"hub\",\"answer\",\"demo\",\"hub:\\ud83d\\ude00\"]",
      "mac": "bb880759969b9eeb2f8298cef9d741fe11d0552a5c3a24a249d61ebd748e2d34"
    },
    {
      "action": "answer",
      "slug": "demo",
      "actor": "hub:tab\there\u007f",
      "message": "[\"hub\",\"answer\",\"demo\",\"hub:tab\\there\\u007f\"]",
      "mac": "c0adb2b64401472843d5003ea096a549a2fc6f18ff756982df17ee63a0bb8727"
    },
    {
      "action": "approve",
      "slug": "demo",
      "actor": "hub:renato",
      "message": "[\"hub\",\"approve\",\"demo\",\"hub:renato\"]",
      "mac": "8a425d16e832eab491b95ae21537ed784066f923fe40735145df01d804b228cb"
    }
  ]
}
```

`internal/hub/gate/testdata/state_pending.json` (a `state --json` document as `gate.cmd_state` prints it, with a string checkpoint id and a string `halted` to pin the tolerant decoding):

```json
{
  "slug": "demo",
  "feature": "Export invoices",
  "workflow": "feature",
  "hotfix": false,
  "task": {"hub": "c-abcdefghijkl", "workspace": "acme", "kind": "feature", "external": null},
  "approved": true,
  "rolled_back": false,
  "gen": 3,
  "cleaned": false,
  "bases": {"1": "0123456789abcdef0123456789abcdef01234567"},
  "halted": null,
  "finished": false,
  "checkpoints": [
    {"id": 1, "title": "Parse rows", "status": "passed", "gates": {"behavior": "passed", "review": "passed"}, "evidence": "fresh", "reviews": {"build-critic": "PASS"}},
    {"id": "2b", "title": "Write CSV", "status": "pending", "gates": {"behavior": "pending", "review": "pending"}, "evidence": "-", "reviews": {}}
  ],
  "dod": null,
  "dod_fresh": false,
  "pending_decisions": [
    {"id": "d1a2b3c4", "kind": "result", "checkpoint": null, "reviewer": null, "text": "Try: export March. 2 checkpoints passed.", "options": ["approve", "request-changes", "stop"], "asked_at": "2026-10-21T10:00:00+00:00"}
  ],
  "decisions": [
    {"id": "d0000001", "kind": "plan-approval", "checkpoint": null, "reviewer": null, "status": "answered", "options": ["approve", "edit", "stop"], "choice": "approve", "note": "", "actor": "hub:renato", "asked_at": "2026-10-21T09:00:00+00:00", "answered_at": "2026-10-21T09:05:00+00:00"},
    {"id": "d1a2b3c4", "kind": "result", "checkpoint": null, "reviewer": null, "status": "pending", "options": ["approve", "request-changes", "stop"], "choice": null, "note": null, "actor": null, "asked_at": "2026-10-21T10:00:00+00:00", "answered_at": null}
  ],
  "events_path": "C:/Users/x/.tentaqles/build-gate/events.jsonl",
  "records": {
    "plan": {"summary": "Export invoices as CSV.", "plain_summary": "You can download March as a CSV.", "spec": "docs/specs/demo.md",
             "checkpoints": [{"id": 1, "title": "Parse rows", "approved": true, "tests": [{"file": "tests/test_rows.py", "layer": "unit"}]}]},
    "evidence": [{"checkpoint": 1, "ts": "2026-10-21T09:30:00+00:00", "tree": "abcdef0123456789", "ok": true, "current": true, "summary": "all verify commands passed",
                  "results": [{"cmd": "python -m pytest tests/test_rows.py -q", "exit": 0, "seconds": 1.2, "timed_out": false}]}],
    "reviews": [{"checkpoint": 1, "reviewer": "build-critic", "result": "PASS", "tree": "abcdef0123456789", "ts": "2026-10-21T09:40:00+00:00", "human": false, "blockers": 0, "majors": 0, "minors": 1, "unjudged": 0}],
    "dod": {"ok": true, "tree": "abcdef0123456789", "ts": "2026-10-21T09:50:00+00:00", "override": null, "checks": [{"id": "test", "ok": true, "detail": "exit 0: python -m pytest -q"}, {"id": "changelog", "ok": null, "detail": "skip: project keeps no changelog"}]}
  }
}
```

`internal/hub/gate/testdata/fake_build_gate.py`:

```python
"""Stand-in for the plugin's build-gate.py in the hub tests.

State lives in $FAKE_GATE_DIR/<slug>.json (the document `state --json` prints; a `records`
key is printed only with --records). `answer` checks TQ_BUILD_ACTOR/TQ_BUILD_TOKEN exactly as
gate.hub_actor_ok does (HMAC-SHA256 over the compact ASCII JSON ["hub","answer",slug,actor] with
the key at $TQ_HOME/build-gate/hmac.key), then moves the decision from pending_decisions to
decisions. Every call is appended to $FAKE_GATE_DIR/calls.jsonl. Standard library only.
"""

import hashlib
import hmac
import json
import os
import sys


def mac(key, action, slug, actor):
    msg = json.dumps(["hub", action, slug, actor], ensure_ascii=True, separators=(",", ":"))
    return hmac.new(key, msg.encode("ascii"), hashlib.sha256).hexdigest()


def main(argv):
    d = os.environ["FAKE_GATE_DIR"]
    root = os.getcwd()
    if argv[:1] == ["--root"]:
        root, argv = argv[1], argv[2:]
    with open(os.path.join(d, "calls.jsonl"), "a", encoding="utf-8") as fh:
        fh.write(json.dumps({"argv": argv, "root": root, "cwd": os.getcwd(),
                             "actor": os.environ.get("TQ_BUILD_ACTOR"),
                             "has_token": bool(os.environ.get("TQ_BUILD_TOKEN"))}) + "\n")
    cmd, slug = argv[0], argv[1]
    path = os.path.join(d, slug + ".json")
    if not os.path.exists(path):
        print(f"no plan at docs/checkpoints/{slug}.json", file=sys.stderr)
        return 1
    with open(path, encoding="utf-8") as fh:
        st = json.load(fh)
    if cmd == "state":
        if "--records" not in argv:
            st.pop("records", None)
        print(json.dumps(st, ensure_ascii=False, indent=2))
        return 0
    if cmd == "answer":
        did = argv[2]
        choice = argv[argv.index("--choice") + 1]
        note = argv[argv.index("--note") + 1] if "--note" in argv else ""
        actor = (os.environ.get("TQ_BUILD_ACTOR") or "").strip()
        tok = (os.environ.get("TQ_BUILD_TOKEN") or "").strip()
        home = os.environ.get("TQ_HOME") or os.path.join(os.path.expanduser("~"), ".tentaqles")
        try:
            with open(os.path.join(home, "build-gate", "hmac.key"), "rb") as fh:
                key = fh.read()
        except OSError:
            key = None
        if not actor or not tok or key is None or not hmac.compare_digest(mac(key, "answer", slug, actor), tok):
            print("build-gate: this step needs a human at an interactive terminal. Refusing.", file=sys.stderr)
            return 2
        if st.get("rolled_back"):
            print("build state was rolled back or altered; a human must re-approve", file=sys.stderr)
            return 2
        pend = [p for p in st.get("pending_decisions", []) if p["id"] == did]
        if not pend:
            print(f"no pending decision '{did}' in '{slug}'.", file=sys.stderr)
            return 1
        rec = pend[0]
        if rec.get("options") and choice not in rec["options"]:
            print(f"choice must be one of {', '.join(rec['options'])}.", file=sys.stderr)
            return 1
        st["pending_decisions"] = [p for p in st["pending_decisions"] if p["id"] != did]
        done = {k: v for k, v in rec.items() if k != "text"}
        done.update({"status": "answered", "choice": choice, "note": note, "actor": actor, "answered_at": "2026-10-21T12:00:00+00:00"})
        st["decisions"] = [x for x in st.get("decisions", []) if x["id"] != did] + [done]
        if rec["kind"] == "result" and choice == "approve":
            st["finished"] = True
        if rec["kind"] == "plan-approval" and choice == "approve":
            st["approved"] = True
        with open(path, "w", encoding="utf-8") as fh:
            json.dump(st, fh, indent=2)
        print(f"decision {did}: {choice} by {actor}")
        return 0
    print(f"fake gate: unsupported command {cmd}", file=sys.stderr)
    return 64


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

- [ ] **Step 2: Write the failing tests**

`internal/hub/gate/token_test.go`:

```go
package gate

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type vectorFile struct {
	KeyHex  string `json:"key_hex"`
	Vectors []struct {
		Action, Slug, Actor, Message, MAC string
	} `json:"vectors"`
}

func TestTokenMatchesPythonVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "hub_token_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	key, _ := hex.DecodeString(vf.KeyHex)
	if len(vf.Vectors) < 7 {
		t.Fatalf("fixture has %d vectors", len(vf.Vectors))
	}
	for _, v := range vf.Vectors {
		if got := Message(v.Action, v.Slug, v.Actor); got != v.Message {
			t.Fatalf("message for %q:\n got %s\nwant %s", v.Actor, got, v.Message)
		}
		if got := MintToken(key, v.Action, v.Slug, v.Actor); got != v.MAC {
			t.Fatalf("token for %q: got %s want %s", v.Actor, got, v.MAC)
		}
	}
}

func TestActorIsHubPrefixedOSUser(t *testing.T) {
	a := Actor()
	if !strings.HasPrefix(a, "hub:") || len(a) <= 4 || strings.ContainsAny(a[4:], `\/`) {
		t.Fatalf("actor %q", a)
	}
}

func TestKeyPathIsUnderTQHome(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join(t.TempDir(), "tq"))
	if KeyPath() != filepath.Join(os.Getenv("TQ_HOME"), "build-gate", "hmac.key") {
		t.Fatal(KeyPath())
	}
}
```

`internal/hub/gate/gate_test.go`:

```go
package gate

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

type fakeGate struct {
	client *Client
	dir    string // FAKE_GATE_DIR
	root   string // the project folder the hub passes as --root
}

func newFakeGate(t *testing.T) fakeGate {
	t.Helper()
	py := testutil.Python(t)
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TQ_BUILD_ACTOR", "hub:leaked") // must never reach a state call
	t.Setenv("TQ_BUILD_TOKEN", "leaked")
	dir := t.TempDir()
	t.Setenv("FAKE_GATE_DIR", dir)
	script, _ := filepath.Abs(filepath.Join("testdata", "fake_build_gate.py"))
	raw, _ := os.ReadFile(filepath.Join("testdata", "state_pending.json"))
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return fakeGate{client: &Client{Command: append(py, script), Env: os.Environ()}, dir: dir, root: t.TempDir()}
}

func (f fakeGate) calls(t *testing.T) []map[string]any {
	fh, err := os.Open(filepath.Join(f.dir, "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	var out []map[string]any
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		var m map[string]any
		json.Unmarshal(sc.Bytes(), &m)
		out = append(out, m)
	}
	return out
}

func writeKey(t *testing.T) {
	t.Helper()
	p := KeyPath()
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStateParsesTheGateDocument(t *testing.T) {
	f := newFakeGate(t)
	st, err := f.client.State(context.Background(), f.root, "demo", false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Slug != "demo" || st.Workflow != "feature" || st.Task == nil || st.Task.Hub != "c-abcdefghijkl" || st.Task.External != nil {
		t.Fatalf("state %+v", st)
	}
	if len(st.Checkpoints) != 2 || st.Checkpoints[0].ID != "1" || st.Checkpoints[1].ID != "2b" {
		t.Fatalf("checkpoint ids %+v", st.Checkpoints)
	}
	if len(st.PendingDecisions) != 1 || st.PendingDecisions[0].Kind != "result" || st.Records != nil || len(st.Raw) == 0 {
		t.Fatalf("pending %+v records %v raw %d", st.PendingDecisions, st.Records, len(st.Raw))
	}
	withRecords, err := f.client.State(context.Background(), f.root, "demo", true)
	if err != nil || withRecords.Records == nil || withRecords.Records.Evidence[0].Results[0].Cmd != "python -m pytest tests/test_rows.py -q" {
		t.Fatalf("records %+v %v", withRecords.Records, err)
	}
	if withRecords.Records.DoD.Checks[1].OK != nil {
		t.Fatal("a skipped DoD check keeps ok=null")
	}
	for _, c := range f.calls(t) {
		if c["has_token"] == true || c["actor"] != nil {
			t.Fatalf("a state call carried hub credentials: %v", c)
		}
		if c["root"] != f.root {
			t.Fatalf("--root %v", c["root"])
		}
	}
}

func TestHaltedAcceptsStringOrObject(t *testing.T) {
	var a, b State
	json.Unmarshal([]byte(`{"halted": "review failed 3x"}`), &a)
	json.Unmarshal([]byte(`{"halted": {"reason": "behavior failed 3x after split", "checkpoint": 2}}`), &b)
	if a.Halted == nil || a.Halted.Reason != "review failed 3x" || b.Halted.Reason != "behavior failed 3x after split" || b.Halted.Checkpoint != "2" {
		t.Fatalf("%+v %+v", a.Halted, b.Halted)
	}
}

func TestNoPlanIsRecognised(t *testing.T) {
	f := newFakeGate(t)
	_, err := f.client.State(context.Background(), f.root, "unknown-slug", false)
	if !IsNoPlan(err) {
		t.Fatalf("want a no-plan error, got %v", err)
	}
}

func TestAnswerSendsAValidToken(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	msg, err := f.client.Answer(context.Background(), f.root, "demo", "d1a2b3c4", "approve", "looks right", "hub:renato")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "decision d1a2b3c4: approve by hub:renato") {
		t.Fatalf("message %q", msg)
	}
	st, _ := f.client.State(context.Background(), f.root, "demo", false)
	if len(st.PendingDecisions) != 0 || !st.Finished {
		t.Fatalf("after answer %+v", st)
	}
	calls := f.calls(t)
	last := calls[len(calls)-2] // the answer, before the state read above
	if last["actor"] != "hub:renato" || last["has_token"] != true {
		t.Fatalf("answer call %v", last)
	}
}

func TestAnswerAlreadyAnsweredIsRecognised(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	_, err := f.client.Answer(context.Background(), f.root, "demo", "d9999999", "approve", "", "hub:renato")
	if !IsNotPending(err) {
		t.Fatalf("want not-pending, got %v", err)
	}
}

func TestAnswerWithAWrongKeyIsAHumanRefusal(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	c := *f.client
	c.KeyPath = filepath.Join(t.TempDir(), "other.key")
	os.WriteFile(c.KeyPath, []byte("not the gate key"), 0o600)
	if _, err := c.Answer(context.Background(), f.root, "demo", "d1a2b3c4", "approve", "", "hub:renato"); !IsHumanRefusal(err) {
		t.Fatalf("want a human-only refusal, got %v", err)
	}
}

func TestAnswerWithoutAKeySaysAnswerInTheTerminal(t *testing.T) {
	f := newFakeGate(t)
	_, err := f.client.Answer(context.Background(), f.root, "demo", "d1a2b3c4", "approve", "", "hub:renato")
	if err == nil || !strings.Contains(err.Error(), "build-gate answer") {
		t.Fatalf("err %v", err)
	}
}

func TestSlugsNewerThan(t *testing.T) {
	root := t.TempDir()
	for _, s := range []string{"old", "new"} {
		os.MkdirAll(filepath.Join(root, ".claude", "build", s), 0o700)
	}
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(root, ".claude", "build", "old"), past, past)
	got := Slugs(root, time.Now().Add(-10*time.Minute))
	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("slugs %v", got)
	}
}

func TestUnavailableReturnsItsError(t *testing.T) {
	u := Unavailable{Err: os.ErrNotExist}
	if _, err := u.State(context.Background(), ".", "x", false); err != os.ErrNotExist {
		t.Fatal(err)
	}
	if u.Slugs(".", time.Time{}) != nil {
		t.Fatal("no slugs")
	}
}
```

`internal/hub/gate/plugin_test.go`:

```go
package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPluginRootReadsInstalledPlugins(t *testing.T) {
	t.Setenv("TQ_HUB_PLUGIN_ROOT", "")
	cfg := t.TempDir()
	install := filepath.Join(cfg, "plugins", "cache", "tentaqles", "tentaqles", "0.12.0")
	os.MkdirAll(filepath.Join(install, "scripts"), 0o755)
	os.WriteFile(filepath.Join(install, "scripts", "build-gate.py"), []byte("#"), 0o644)
	doc := `{"version":2,"plugins":{"other@x":[{"installPath":"/nope"}],"tentaqles@tentaqles":[{"installPath":` + quote(install) + `}]}}`
	os.WriteFile(filepath.Join(cfg, "plugins", "installed_plugins.json"), []byte(doc), 0o644)
	got, err := FindPluginRoot(cfg)
	if err != nil || got != install {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := FindPluginRoot(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing plugin: %v", err)
	}
	t.Setenv("TQ_HUB_PLUGIN_ROOT", install)
	if got, _ := FindPluginRoot(t.TempDir()); got != install {
		t.Fatal("TQ_HUB_PLUGIN_ROOT overrides")
	}
}

func TestForWorkspaceHonoursTheTestOverride(t *testing.T) {
	t.Setenv("TQ_HUB_GATE_CMD", `["python","fake_build_gate.py"]`)
	c, err := ForWorkspace(t.TempDir(), nil)
	if err != nil || len(c.Command) != 2 || c.Command[1] != "fake_build_gate.py" {
		t.Fatalf("%+v %v", c, err)
	}
	t.Setenv("TQ_HUB_GATE_CMD", "not json")
	if _, err := ForWorkspace(t.TempDir(), nil); err == nil {
		t.Fatal("a malformed override is an error")
	}
}

func quote(p string) string { return `"` + strings.ReplaceAll(p, `\`, `\\`) + `"` }
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/hub/gate/ -count=1`
Expected: FAIL, `undefined: Message`, `undefined: Client`, `undefined: FindPluginRoot`.

- [ ] **Step 4: Implement**

`internal/hub/gate/token.go`:

```go
package gate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// KeyPath is the gate's signing key; the hub reads it only inside the answer path.
func KeyPath() string { return filepath.Join(paths.Home(), "build-gate", "hmac.key") }

// Actor is the hub actor the gate records: "hub:<os user name>" (Windows DOMAIN\user → user).
func Actor() string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		name = os.Getenv("USERNAME")
	}
	if name == "" {
		name = os.Getenv("USER")
	}
	if name == "" {
		name = "user"
	}
	return "hub:" + name
}

// Message is json.dumps(["hub", action, slug, actor], ensure_ascii=True, separators=(",", ":")),
// byte for byte (gate._hub_mac).
func Message(action, slug, actor string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range []string{"hub", action, slug, actor} {
		if i > 0 {
			b.WriteByte(',')
		}
		writePyString(&b, s)
	}
	b.WriteByte(']')
	return b.String()
}

// writePyString writes s the way Python's json module does with ensure_ascii=True: printable ASCII
// as is, the short escapes, and everything else as \uXXXX (lowercase hex, surrogate pairs above
// U+FFFF). Invalid UTF-8 cannot come from Python, so it is written as U+FFFD.
func writePyString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r >= 0x20 && r <= 0x7e:
				b.WriteRune(r)
			case r > 0xffff:
				r1, r2 := utf16.EncodeRune(r)
				fmt.Fprintf(b, `\u%04x\u%04x`, r1, r2)
			default:
				fmt.Fprintf(b, `\u%04x`, r)
			}
		}
	}
	b.WriteByte('"')
}

// MintToken is gate.mint_hub_token with an explicit key (the raw bytes of hmac.key).
func MintToken(key []byte, action, slug, actor string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(Message(action, slug, actor)))
	return hex.EncodeToString(m.Sum(nil))
}
```

`internal/hub/gate/gate.go`:

```go
// Package gate is the hub's client for the plugin's build gate: it reads
// `build-gate state <slug> --json` and, for a human's answer only, runs
// `build-gate answer` with a hub actor token minted exactly as gate.py does.
// The hub never writes gate state and never calls approve/finish/judge/dod.
package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CheckpointID string

func (c *CheckpointID) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "null":
		*c = ""
	case strings.HasPrefix(s, `"`):
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*c = CheckpointID(v)
	default:
		var n json.Number
		if err := json.Unmarshal(b, &n); err != nil {
			return err
		}
		*c = CheckpointID(n.String())
	}
	return nil
}

type Halted struct {
	Reason     string       `json:"reason"`
	Checkpoint CheckpointID `json:"checkpoint"`
}

func (h *Halted) UnmarshalJSON(b []byte) error {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, `"`) {
		return json.Unmarshal(b, &h.Reason)
	}
	type plain Halted
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*h = Halted(p)
	return nil
}

type External struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`
}

type Task struct {
	Hub       string    `json:"hub"`
	Workspace string    `json:"workspace"`
	Kind      string    `json:"kind"`
	External  *External `json:"external"`
}

type Checkpoint struct {
	ID       CheckpointID      `json:"id"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Gates    map[string]string `json:"gates"`
	Evidence string            `json:"evidence"`
	Reviews  map[string]string `json:"reviews"`
}

type DoD struct {
	OK       bool    `json:"ok"`
	Tree     string  `json:"tree"`
	Override *string `json:"override"`
}

type Decision struct {
	ID         string       `json:"id"`
	Kind       string       `json:"kind"`
	Checkpoint CheckpointID `json:"checkpoint"`
	Reviewer   string       `json:"reviewer"`
	Text       string       `json:"text"`
	Options    []string     `json:"options"`
	Status     string       `json:"status"`
	Choice     string       `json:"choice"`
	Note       string       `json:"note"`
	Actor      string       `json:"actor"`
	AskedAt    string       `json:"asked_at"`
	AnsweredAt string       `json:"answered_at"`
}

type PlanTest struct {
	File  string `json:"file"`
	Layer string `json:"layer"`
}

type PlanCheckpoint struct {
	ID       CheckpointID `json:"id"`
	Title    string       `json:"title"`
	Approved bool         `json:"approved"`
	Tests    []PlanTest   `json:"tests"`
}

type PlanRecord struct {
	Summary      string           `json:"summary"`
	PlainSummary string           `json:"plain_summary"`
	Spec         string           `json:"spec"`
	Checkpoints  []PlanCheckpoint `json:"checkpoints"`
}

type VerifyResult struct {
	Cmd      string  `json:"cmd"`
	Exit     int     `json:"exit"`
	Seconds  float64 `json:"seconds"`
	TimedOut bool    `json:"timed_out"`
}

type Evidence struct {
	Checkpoint CheckpointID   `json:"checkpoint"`
	TS         string         `json:"ts"`
	Tree       string         `json:"tree"`
	OK         bool           `json:"ok"`
	Current    bool           `json:"current"`
	Summary    string         `json:"summary"`
	Results    []VerifyResult `json:"results"`
}

type Review struct {
	Checkpoint CheckpointID `json:"checkpoint"`
	Reviewer   string       `json:"reviewer"`
	Result     string       `json:"result"`
	Tree       string       `json:"tree"`
	TS         string       `json:"ts"`
	Human      bool         `json:"human"`
	Blockers   int          `json:"blockers"`
	Majors     int          `json:"majors"`
	Minors     int          `json:"minors"`
	Unjudged   int          `json:"unjudged"`
}

type DoDCheck struct {
	ID     string `json:"id"`
	OK     *bool  `json:"ok"`
	Detail string `json:"detail"`
}

type DoDRecord struct {
	OK       bool       `json:"ok"`
	Tree     string     `json:"tree"`
	TS       string     `json:"ts"`
	Override *string    `json:"override"`
	Checks   []DoDCheck `json:"checks"`
}

type Records struct {
	Plan     *PlanRecord `json:"plan"`
	Evidence []Evidence  `json:"evidence"`
	Reviews  []Review    `json:"reviews"`
	DoD      *DoDRecord  `json:"dod"`
}

type State struct {
	Slug             string            `json:"slug"`
	Feature          string            `json:"feature"`
	Workflow         string            `json:"workflow"`
	Hotfix           bool              `json:"hotfix"`
	Task             *Task             `json:"task"`
	Approved         bool              `json:"approved"`
	RolledBack       bool              `json:"rolled_back"`
	Finished         bool              `json:"finished"`
	Cleaned          bool              `json:"cleaned"`
	Halted           *Halted           `json:"halted"`
	Checkpoints      []Checkpoint      `json:"checkpoints"`
	DoD              *DoD              `json:"dod"`
	DoDFresh         bool              `json:"dod_fresh"`
	PendingDecisions []Decision        `json:"pending_decisions"`
	Decisions        []Decision        `json:"decisions"`
	Bases            map[string]string `json:"bases"`
	Records          *Records          `json:"records,omitempty"`
	Raw              json.RawMessage   `json:"-"`
}

type API interface {
	State(ctx context.Context, dir, slug string, records bool) (*State, error)
	Answer(ctx context.Context, dir, slug, id, choice, note, actor string) (string, error)
	Slugs(dir string, since time.Time) []string
}

// GateError is a non-zero exit of build-gate.py with its message.
type GateError struct {
	Code int
	Msg  string
}

func (e *GateError) Error() string { return fmt.Sprintf("build-gate exited %d: %s", e.Code, e.Msg) }

func gateMsg(err error) (string, bool) {
	var ge *GateError
	if errors.As(err, &ge) {
		return ge.Msg, true
	}
	return "", false
}

func IsNoPlan(err error) bool {
	m, ok := gateMsg(err)
	return ok && strings.Contains(m, "no plan at")
}

func IsNotPending(err error) bool {
	m, ok := gateMsg(err)
	return ok && strings.Contains(m, "no pending decision")
}

func IsHumanRefusal(err error) bool {
	var ge *GateError
	return errors.As(err, &ge) && ge.Code == 2
}

type Client struct {
	Command []string      // argv prefix that runs build-gate.py
	Env     []string      // the workspace environment (hub credentials are stripped)
	KeyPath string        // "" → KeyPath()
	Timeout time.Duration // per call; 0 → 60 s
}

func scrub(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (c *Client) run(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	if len(c.Command) == 0 {
		return "", errors.New("build gate: no command")
	}
	t := c.Timeout
	if t <= 0 {
		t = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	argv := append(append([]string(nil), c.Command...), append([]string{"--root", dir}, args...)...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(scrub(c.Env), extraEnv...)
	cmd.WaitDelay = 2 * time.Second
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("build gate timed out after %s", t)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return "", &GateError{Code: ee.ExitCode(), Msg: strings.TrimSpace(se.String() + "\n" + so.String())}
	}
	if err != nil {
		return "", err
	}
	return so.String(), nil
}

func (c *Client) State(ctx context.Context, dir, slug string, records bool) (*State, error) {
	args := []string{"state", slug, "--json"}
	if records {
		args = append(args, "--records")
	}
	out, err := c.run(ctx, dir, nil, args...)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		return nil, fmt.Errorf("build-gate state %s: unreadable JSON: %w", slug, err)
	}
	st.Raw = json.RawMessage(strings.TrimSpace(out))
	return &st, nil
}

// Answer reads the key, mints the token for this one call and runs build-gate answer.
func (c *Client) Answer(ctx context.Context, dir, slug, id, choice, note, actor string) (string, error) {
	kp := c.KeyPath
	if kp == "" {
		kp = KeyPath()
	}
	key, err := os.ReadFile(kp)
	if err != nil {
		return "", fmt.Errorf("the gate key %s is not readable (%v); answer this decision in your terminal with build-gate answer", kp, err)
	}
	tok := MintToken(key, "answer", slug, actor)
	for i := range key {
		key[i] = 0
	}
	out, err := c.run(ctx, dir, []string{"TQ_BUILD_ACTOR=" + actor, "TQ_BUILD_TOKEN=" + tok},
		"answer", slug, id, "--choice", choice, "--note", note, "--actor", actor)
	return strings.TrimSpace(out), err
}

func (c *Client) Slugs(dir string, since time.Time) []string { return Slugs(dir, since) }

// Slugs lists <dir>/.claude/build/<slug>/ directories modified at or after since, newest first.
func Slugs(dir string, since time.Time) []string {
	ents, err := os.ReadDir(filepath.Join(dir, ".claude", "build"))
	if err != nil {
		return nil
	}
	type item struct {
		name string
		t    time.Time
	}
	var items []item
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().Before(since) {
			continue
		}
		items = append(items, item{e.Name(), fi.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.After(items[j].t) })
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.name
	}
	return out
}

// Unavailable stands in when the plugin cannot be found: runs still start; state reads report Err.
type Unavailable struct{ Err error }

func (u Unavailable) State(context.Context, string, string, bool) (*State, error) { return nil, u.Err }
func (u Unavailable) Answer(context.Context, string, string, string, string, string, string) (string, error) {
	return "", u.Err
}
func (u Unavailable) Slugs(string, time.Time) []string { return nil }

```

`internal/hub/gate/plugin.go`:

```go
package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// FindPluginRoot returns the tentaqles plugin's install folder for the Claude config dir
// (<configDir>/plugins/installed_plugins.json). TQ_HUB_PLUGIN_ROOT overrides it.
func FindPluginRoot(configDir string) (string, error) {
	if d := strings.TrimSpace(os.Getenv("TQ_HUB_PLUGIN_ROOT")); d != "" {
		return d, nil
	}
	file := filepath.Join(configDir, "plugins", "installed_plugins.json")
	raw, err := os.ReadFile(file)
	if err == nil {
		var doc struct {
			Plugins map[string][]struct {
				InstallPath string `json:"installPath"`
			} `json:"plugins"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			keys := make([]string, 0, len(doc.Plugins))
			for k := range doc.Plugins {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if name, _, _ := strings.Cut(k, "@"); name != "tentaqles" {
					continue
				}
				for _, in := range doc.Plugins[k] {
					if _, err := os.Stat(filepath.Join(in.InstallPath, "scripts", "build-gate.py")); err == nil {
						return in.InstallPath, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("the tentaqles plugin is not installed for this identity (%s); install it with /plugin in Claude Code, then retry", configDir)
}

// FindBash finds the bash that runs tq_run.sh: TQ_BASH, then bash on PATH (never WSL's
// System32 bash on Windows), then Git for Windows' usual locations.
func FindBash() (string, error) {
	if b := strings.TrimSpace(os.Getenv("TQ_BASH")); b != "" {
		return b, nil
	}
	if p, err := exec.LookPath("bash"); err == nil {
		if runtime.GOOS != "windows" || !strings.Contains(strings.ToLower(p), `\windows\system32\`) {
			return p, nil
		}
	}
	if runtime.GOOS == "windows" {
		for _, c := range []string{
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files (x86)\Git\bin\bash.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
		} {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}
	return "", errors.New("bash not found: install Git for Windows (Git Bash) or set TQ_BASH")
}

// ForWorkspace builds the client for one workspace: TQ_HUB_GATE_CMD (a JSON argv, for tests),
// else bash <plugin>/scripts/tq_run.sh build-gate.py.
func ForWorkspace(configDir string, env []string) (*Client, error) {
	if raw := strings.TrimSpace(os.Getenv("TQ_HUB_GATE_CMD")); raw != "" {
		var argv []string
		if err := json.Unmarshal([]byte(raw), &argv); err != nil || len(argv) == 0 {
			return nil, fmt.Errorf("TQ_HUB_GATE_CMD must be a JSON array of strings")
		}
		return &Client{Command: argv, Env: env}, nil
	}
	root, err := FindPluginRoot(configDir)
	if err != nil {
		return nil, err
	}
	bash, err := FindBash()
	if err != nil {
		return nil, err
	}
	return &Client{Command: []string{bash, filepath.Join(root, "scripts", "tq_run.sh"), "build-gate.py"}, Env: env}, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go vet ./internal/hub/gate/; go test ./internal/hub/gate/ -count=1 -v`
Expected: PASS (`TestTokenMatchesPythonVectors` passes without Python: it reads the committed fixture; the fake-gate tests SKIP when no Python is installed).

Cross-check the fixture once: `python internal/hub/gate/testdata/gen_hub_token_vectors.py` prints the same `token` values as the committed file.

- [ ] **Step 6: Commit**

```bash
git add internal/hub/gate/gate.go internal/hub/gate/token.go internal/hub/gate/plugin.go internal/hub/gate/testdata/fake_build_gate.py internal/hub/gate/testdata/gen_hub_token_vectors.py internal/hub/gate/testdata/hub_token_vectors.json internal/hub/gate/testdata/state_pending.json internal/hub/gate/token_test.go internal/hub/gate/gate_test.go internal/hub/gate/plugin_test.go
git commit -m "feat(hub): build-gate client with byte-identical hub tokens and plugin discovery"
```

---
### Task 7: Outcome classification and desktop notifications

**Files:**
- Create: `internal/hub/runner/classify.go`, `internal/hub/runner/testdata/states/waiting.json`, `finished_feature.json`, `finished_feature_dod_failed.json`, `finished_build.json`, `halted.json`, `rolled_back.json`, `in_progress.json` (all under `internal/hub/runner/testdata/states/`), `internal/hub/notify/notify.go`
- Test: `internal/hub/runner/classify_test.go`, `internal/hub/notify/notify_test.go`

**Interfaces:**
- Consumes: `gate.State`, `gate.Halted`, `gate.Decision`, `gate.DoD` (Task 6); status constants (Task 1).
- Produces (package `runner`, file `classify.go`):
  - `type Relocate struct { Worktree, Slug string }`; `ParseRelocate(finalText string) (*Relocate, bool)`: only the last non-empty line counts; it must be `TQ_HUB_RELOCATE <absolute path> <slug>` (the path may contain spaces; on Windows the Git Bash form `/c/...` is read as `C:/...`; the slug matches `^[a-z0-9][a-z0-9-]*$`).
  - `StripRelocate(finalText string) string` (the text without that last line).
  - `StateKey(st *gate.State) string` (fingerprint of what a round can change: approval, finished, rolled back, halted, checkpoint statuses, gates and evidence, decision ids and statuses, DoD).
  - `type Observation struct { ExitCode int; Subtype, FinalText string; State *gate.State; PrevStateKey string; Relocations []string; PermissionTool string; Retried int }`.
  - `type Verdict string` with `VerdictRelocate`, `VerdictRelocateLoop`, `VerdictRolledBack`, `VerdictHalted`, `VerdictWaiting`, `VerdictFinished`, `VerdictPermission`, `VerdictMaxTurns`, `VerdictRetry`, `VerdictExitFailed`, `VerdictNoStop`.
  - `type Outcome struct { Verdict Verdict; Status, StopReason string; Relocate *Relocate }`; `Classify(o Observation) Outcome`. Precedence: relocate (or relocate loop) → rolled back → halted → pending decisions → finished → permission → max turns → non-zero exit with unchanged state (retry once, then exit-failed) → no stop.
- Produces (package `notify`): `Timeout = 2 * time.Second`; `type Command struct { Name string; Args, Env []string }`; `Build(goos, title, body string) (Command, bool)` (title and body travel only in `TQ_NOTIFY_TITLE`/`TQ_NOTIFY_BODY`, never inside a script); `Send(ctx context.Context, title, body string) error`; `SendWith(ctx context.Context, c Command) error` (bounded by `Timeout`, never retried).

- [ ] **Step 1: Write the fixture state documents**

`internal/hub/runner/testdata/states/waiting.json`:

```json
{"slug": "demo", "workflow": "feature", "approved": false, "rolled_back": false, "finished": false, "halted": null,
 "task": {"hub": "c-abcdefghijkl", "workspace": "acme", "kind": "feature", "external": null},
 "checkpoints": [{"id": 1, "title": "Parse rows", "status": "pending", "gates": {"behavior": "pending", "review": "pending"}, "evidence": "-", "reviews": {}}],
 "dod": null, "pending_decisions": [{"id": "d1a2b3c4", "kind": "plan-approval", "checkpoint": null, "reviewer": null,
   "text": "1 checkpoint: parse rows. verify: python -m pytest -q", "options": ["approve", "edit", "stop"], "asked_at": "2026-10-21T10:00:00+00:00"}],
 "decisions": [{"id": "d1a2b3c4", "kind": "plan-approval", "status": "pending", "options": ["approve", "edit", "stop"]}]}
```

`internal/hub/runner/testdata/states/finished_feature.json`:

```json
{"slug": "demo", "workflow": "feature", "approved": true, "rolled_back": false, "finished": true, "halted": null,
 "checkpoints": [{"id": 1, "title": "Parse rows", "status": "passed", "gates": {"behavior": "passed", "review": "passed"}, "evidence": "fresh", "reviews": {"build-critic": "PASS"}}],
 "dod": {"ok": true, "tree": "abc", "override": null}, "pending_decisions": [], "decisions": []}
```

`internal/hub/runner/testdata/states/finished_feature_dod_failed.json`:

```json
{"slug": "demo", "workflow": "feature", "approved": true, "rolled_back": false, "finished": true, "halted": null,
 "checkpoints": [{"id": 1, "title": "Parse rows", "status": "passed", "gates": {"behavior": "passed", "review": "passed"}, "evidence": "fresh", "reviews": {}}],
 "dod": {"ok": false, "tree": "abc", "override": null}, "pending_decisions": [], "decisions": []}
```

`internal/hub/runner/testdata/states/finished_build.json`:

```json
{"slug": "demo", "workflow": "build", "approved": true, "rolled_back": false, "finished": true, "halted": null,
 "checkpoints": [{"id": 1, "title": "Parse rows", "status": "passed", "gates": {"behavior": "passed", "review": "passed"}, "evidence": "fresh", "reviews": {}}],
 "dod": null, "pending_decisions": [], "decisions": []}
```

`internal/hub/runner/testdata/states/halted.json`:

```json
{"slug": "demo", "workflow": "feature", "approved": true, "rolled_back": false, "finished": false,
 "halted": {"reason": "behavior failed 3x after split", "checkpoint": 2},
 "checkpoints": [{"id": 2, "title": "Write CSV", "status": "pending", "gates": {"behavior": "failed", "review": "pending"}, "evidence": "failing", "reviews": {}}],
 "dod": null, "pending_decisions": [{"id": "d5", "kind": "question", "text": "?", "options": ["a", "b"]}], "decisions": []}
```

`internal/hub/runner/testdata/states/rolled_back.json`:

```json
{"slug": "demo", "workflow": "feature", "approved": false, "rolled_back": true, "finished": false, "halted": null,
 "checkpoints": [], "dod": null,
 "pending_decisions": [{"id": "d6", "kind": "result", "text": "try it", "options": ["approve", "request-changes", "stop"]}], "decisions": []}
```

`internal/hub/runner/testdata/states/in_progress.json`:

```json
{"slug": "demo", "workflow": "feature", "approved": true, "rolled_back": false, "finished": false, "halted": null,
 "checkpoints": [{"id": 1, "title": "Parse rows", "status": "pending", "gates": {"behavior": "pending", "review": "pending"}, "evidence": "-", "reviews": {}}],
 "dod": null, "pending_decisions": [], "decisions": []}
```

- [ ] **Step 2: Write the failing tests**

`internal/hub/runner/classify_test.go`:

```go
package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func loadState(t *testing.T, name string) *gate.State {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "states", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var st gate.State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	st.Raw = raw
	return &st
}

func absPath(t *testing.T, parts ...string) string {
	p, err := filepath.Abs(filepath.Join(append([]string{t.TempDir()}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClassifyTable(t *testing.T) {
	inProgress := loadState(t, "in_progress")
	cases := []struct {
		name string
		obs  Observation
		v    Verdict
		st   string
		why  string
	}{
		{"pending decisions wait", Observation{State: loadState(t, "waiting")}, VerdictWaiting, store.StatusWaiting, ""},
		{"finished feature with DoD ok", Observation{State: loadState(t, "finished_feature")}, VerdictFinished, store.StatusFinished, ""},
		{"finished plain build without DoD", Observation{State: loadState(t, "finished_build")}, VerdictFinished, store.StatusFinished, ""},
		{"finished feature with a failed DoD is no stop", Observation{State: loadState(t, "finished_feature_dod_failed")}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
		{"halted beats pending", Observation{State: loadState(t, "halted")}, VerdictHalted, store.StatusFailed, "behavior failed 3x after split"},
		{"rolled back beats pending", Observation{State: loadState(t, "rolled_back")}, VerdictRolledBack, store.StatusFailed, "rolled back"},
		{"permission denial waits", Observation{ExitCode: 1, Subtype: "error_during_execution", PermissionTool: "Bash(npm test)", State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictPermission, store.StatusWaiting, "needs permission: Bash(npm test)"},
		{"max turns is a round boundary", Observation{Subtype: "error_max_turns", State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictMaxTurns, store.StatusQueued, ""},
		{"exit non-zero, state unchanged, first time: retry", Observation{ExitCode: 1, State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictRetry, store.StatusQueued, ""},
		{"exit non-zero, no state at all: retry", Observation{ExitCode: 2}, VerdictRetry, store.StatusQueued, ""},
		{"exit non-zero again: failed", Observation{ExitCode: 1, Retried: 1, State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictExitFailed, store.StatusFailed, "exit 1"},
		{"exit non-zero but state changed: no stop", Observation{ExitCode: 1, State: inProgress, PrevStateKey: "older"}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
		{"exit zero, nothing to show: no stop", Observation{State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
	}
	for _, c := range cases {
		got := Classify(c.obs)
		if got.Verdict != c.v || got.Status != c.st || got.StopReason != c.why {
			t.Errorf("%s: got %+v, want %s/%s/%q", c.name, got, c.v, c.st, c.why)
		}
	}
}

func TestClassifyRelocate(t *testing.T) {
	wt := absPath(t, "billing build demo") // a path with spaces
	got := Classify(Observation{FinalText: "Worktree ready.\nTQ_HUB_RELOCATE " + wt + " demo\n", State: loadState(t, "waiting")})
	if got.Verdict != VerdictRelocate || got.Status != store.StatusRunning || got.Relocate.Worktree != wt || got.Relocate.Slug != "demo" {
		t.Fatalf("%+v", got)
	}
}

func TestClassifyRelocateLoop(t *testing.T) {
	wt := absPath(t, "wt")
	got := Classify(Observation{FinalText: "TQ_HUB_RELOCATE " + wt + " demo", Relocations: []string{"demo"}})
	if got.Verdict != VerdictRelocateLoop || got.Status != store.StatusFailed || got.StopReason != "relocate loop" {
		t.Fatalf("%+v", got)
	}
	other := Classify(Observation{FinalText: "TQ_HUB_RELOCATE " + wt + " other", Relocations: []string{"demo"}})
	if other.Verdict != VerdictRelocate {
		t.Fatalf("a different slug is a new relocation: %+v", other)
	}
}

func TestParseRelocateOnlyLastLine(t *testing.T) {
	a, b := absPath(t, "a"), absPath(t, "b")
	r, ok := ParseRelocate("TQ_HUB_RELOCATE " + a + " first\nsome words\nTQ_HUB_RELOCATE " + b + " second\n\n")
	if !ok || r.Worktree != b || r.Slug != "second" {
		t.Fatalf("%+v %v", r, ok)
	}
	if _, ok := ParseRelocate("TQ_HUB_RELOCATE " + a + " demo\nand then more text"); ok {
		t.Fatal("a relocate line that is not the last line does not count")
	}
	for _, bad := range []string{"TQ_HUB_RELOCATE relative/path demo", "TQ_HUB_RELOCATE " + a + " Bad_Slug", "TQ_HUB_RELOCATE " + a, "tq_hub_relocate " + a + " demo"} {
		if _, ok := ParseRelocate(bad); ok {
			t.Fatalf("%q accepted", bad)
		}
	}
	if StripRelocate("done\nTQ_HUB_RELOCATE "+a+" demo") != "done" {
		t.Fatal("StripRelocate")
	}
	if runtime.GOOS == "windows" {
		r, ok := ParseRelocate("TQ_HUB_RELOCATE /c/repos/acme-build-demo demo")
		if !ok || r.Worktree != `C:\repos\acme-build-demo` {
			t.Fatalf("Git Bash path: %+v %v", r, ok)
		}
	}
}

func TestStateKeyChangesWithProgress(t *testing.T) {
	a := loadState(t, "in_progress")
	b := loadState(t, "in_progress")
	b.Checkpoints[0].Status = "passed"
	if StateKey(a) == StateKey(b) || StateKey(a) != StateKey(loadState(t, "in_progress")) || StateKey(nil) != "" {
		t.Fatal("StateKey must be stable and change with a checkpoint status")
	}
}
```

`internal/hub/notify/notify_test.go`:

```go
package notify

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("TQ_NOTIFY_HELPER") == "hang" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestBuildPerPlatformKeepsTextOutOfTheScript(t *testing.T) {
	evil := `x"; rm -rf / #$(whoami)`
	for _, goos := range []string{"windows", "darwin", "linux"} {
		c, ok := Build(goos, "tq hub: waiting on you", evil)
		if !ok {
			t.Fatalf("%s: no command", goos)
		}
		joined := c.Name + " " + strings.Join(c.Args, " ")
		if goos != "linux" && strings.Contains(joined, "whoami") {
			t.Fatalf("%s: body inside the command line: %s", goos, joined)
		}
		if !strings.Contains(strings.Join(c.Env, "\n"), "TQ_NOTIFY_BODY=") {
			t.Fatalf("%s: env %v", goos, c.Env)
		}
	}
	if c, _ := Build("windows", "t", "b"); c.Name != "powershell" {
		t.Fatal(c.Name)
	}
	if c, _ := Build("darwin", "t", "b"); c.Name != "osascript" {
		t.Fatal(c.Name)
	}
	if c, _ := Build("linux", "t", "b"); c.Name != "notify-send" || c.Args[len(c.Args)-1] != "b" {
		t.Fatalf("%+v", c)
	}
	if _, ok := Build("plan9", "t", "b"); ok {
		t.Fatal("unknown platforms send nothing")
	}
}

func TestSendIsBoundedToTwoSeconds(t *testing.T) {
	start := time.Now()
	err := SendWith(context.Background(), Command{Name: os.Args[0], Env: []string{"TQ_NOTIFY_HELPER=hang"}})
	if err == nil {
		t.Fatal("a hanging notifier returns an error")
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("took %s", d)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/hub/runner/ ./internal/hub/notify/ -count=1`
Expected: FAIL, `undefined: Classify`, `undefined: Build`.

- [ ] **Step 4: Implement**

`internal/hub/runner/classify.go`:

```go
package runner

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

const relocatePrefix = "TQ_HUB_RELOCATE "

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Relocate struct {
	Worktree string `json:"worktree"`
	Slug     string `json:"slug"`
}

func lastLine(text string) (string, int) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i]), i
		}
	}
	return "", -1
}

// ParseRelocate reads the hand-off line of spec §13.4 from the last non-empty line only.
func ParseRelocate(text string) (*Relocate, bool) {
	line, _ := lastLine(text)
	if !strings.HasPrefix(line, relocatePrefix) {
		return nil, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, relocatePrefix))
	i := strings.LastIndexByte(rest, ' ')
	if i <= 0 {
		return nil, false
	}
	wt, slug := fromMSYS(strings.TrimSpace(rest[:i])), rest[i+1:]
	if !slugRe.MatchString(slug) || !filepath.IsAbs(wt) {
		return nil, false
	}
	return &Relocate{Worktree: filepath.Clean(wt), Slug: slug}, true
}

// fromMSYS turns Git Bash's /c/Users/... into C:/Users/... on Windows (the skill prints a native
// path, but a session that used pwd in Git Bash prints the MSYS form).
func fromMSYS(p string) string {
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == '/' && unicode.IsLetter(rune(p[1])) {
		return strings.ToUpper(p[1:2]) + ":" + p[2:]
	}
	return p
}

func StripRelocate(text string) string {
	if _, ok := ParseRelocate(text); !ok {
		return strings.TrimSpace(text)
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	_, idx := lastLine(text)
	return strings.TrimSpace(strings.Join(lines[:idx], "\n"))
}

// StateKey fingerprints what a round can change, to tell "state unchanged" from progress.
func StateKey(st *gate.State) string {
	if st == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "a=%v f=%v rb=%v", st.Approved, st.Finished, st.RolledBack)
	if st.Halted != nil {
		fmt.Fprintf(&b, " h=%s", st.Halted.Reason)
	}
	for _, c := range st.Checkpoints {
		gates := make([]string, 0, len(c.Gates))
		for k, v := range c.Gates {
			gates = append(gates, k+":"+v)
		}
		sort.Strings(gates)
		fmt.Fprintf(&b, " cp%s=%s/%s/%s", c.ID, c.Status, c.Evidence, strings.Join(gates, ","))
	}
	for _, d := range st.Decisions {
		fmt.Fprintf(&b, " d%s=%s", d.ID, d.Status)
	}
	for _, d := range st.PendingDecisions {
		fmt.Fprintf(&b, " p%s", d.ID)
	}
	if st.DoD != nil {
		fmt.Fprintf(&b, " dod=%v/%s", st.DoD.OK, st.DoD.Tree)
	}
	return b.String()
}

type Observation struct {
	ExitCode       int
	Subtype        string
	FinalText      string
	State          *gate.State // nil when the slug is unknown or the gate has no plan yet
	PrevStateKey   string
	Relocations    []string
	PermissionTool string
	Retried        int
}

type Verdict string

const (
	VerdictRelocate     Verdict = "relocate"
	VerdictRelocateLoop Verdict = "relocate-loop"
	VerdictRolledBack   Verdict = "rolled-back"
	VerdictHalted       Verdict = "halted"
	VerdictWaiting      Verdict = "waiting"
	VerdictFinished     Verdict = "finished"
	VerdictPermission   Verdict = "permission"
	VerdictMaxTurns     Verdict = "max-turns"
	VerdictRetry        Verdict = "retry"
	VerdictExitFailed   Verdict = "exit-failed"
	VerdictNoStop       Verdict = "no-stop"
)

type Outcome struct {
	Verdict    Verdict
	Status     string
	StopReason string
	Relocate   *Relocate
}

// Classify is the outcome table of spec §8.4.
func Classify(o Observation) Outcome {
	if r, ok := ParseRelocate(o.FinalText); ok {
		if slices.Contains(o.Relocations, r.Slug) {
			return Outcome{Verdict: VerdictRelocateLoop, Status: store.StatusFailed, StopReason: "relocate loop", Relocate: r}
		}
		return Outcome{Verdict: VerdictRelocate, Status: store.StatusRunning, Relocate: r}
	}
	if st := o.State; st != nil {
		if st.RolledBack {
			return Outcome{Verdict: VerdictRolledBack, Status: store.StatusFailed, StopReason: "rolled back"}
		}
		if st.Halted != nil && strings.TrimSpace(st.Halted.Reason) != "" {
			return Outcome{Verdict: VerdictHalted, Status: store.StatusFailed, StopReason: st.Halted.Reason}
		}
		if len(st.PendingDecisions) > 0 {
			return Outcome{Verdict: VerdictWaiting, Status: store.StatusWaiting}
		}
		if st.Finished && ((st.DoD != nil && st.DoD.OK) || (st.DoD == nil && st.Workflow == "build")) {
			return Outcome{Verdict: VerdictFinished, Status: store.StatusFinished}
		}
	}
	if o.PermissionTool != "" {
		return Outcome{Verdict: VerdictPermission, Status: store.StatusWaiting, StopReason: "needs permission: " + o.PermissionTool}
	}
	if o.Subtype == "error_max_turns" {
		return Outcome{Verdict: VerdictMaxTurns, Status: store.StatusQueued}
	}
	if o.ExitCode != 0 && StateKey(o.State) == o.PrevStateKey {
		if o.Retried == 0 {
			return Outcome{Verdict: VerdictRetry, Status: store.StatusQueued}
		}
		return Outcome{Verdict: VerdictExitFailed, Status: store.StatusFailed, StopReason: fmt.Sprintf("exit %d", o.ExitCode)}
	}
	return Outcome{Verdict: VerdictNoStop, Status: store.StatusFailed, StopReason: "ended without a stop"}
}
```

`internal/hub/notify/notify.go`:

```go
// Package notify shows one desktop notification, bounded to two seconds and never
// retried (spec §8.7). The hub's waiting list is the source of truth; this is a nudge.
package notify

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const Timeout = 2 * time.Second

type Command struct {
	Name string
	Args []string
	Env  []string
}

const psToast = `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$x = $t.GetElementsByTagName('text')
$x.Item(0).AppendChild($t.CreateTextNode($env:TQ_NOTIFY_TITLE)) > $null
$x.Item(1).AppendChild($t.CreateTextNode($env:TQ_NOTIFY_BODY)) > $null
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('tq hub').Show([Windows.UI.Notifications.ToastNotification]::new($t))`

const appleScript = `display notification (system attribute "TQ_NOTIFY_BODY") with title (system attribute "TQ_NOTIFY_TITLE")`

func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len([]rune(s)) > 200 {
		s = string([]rune(s)[:200])
	}
	return strings.TrimSpace(s)
}

// Build returns the platform's notifier. Text travels in the environment, never inside a script.
func Build(goos, title, body string) (Command, bool) {
	env := []string{"TQ_NOTIFY_TITLE=" + clean(title), "TQ_NOTIFY_BODY=" + clean(body)}
	switch goos {
	case "windows":
		return Command{Name: "powershell", Args: []string{"-NoProfile", "-NonInteractive", "-Command", psToast}, Env: env}, true
	case "darwin":
		return Command{Name: "osascript", Args: []string{"-e", appleScript}, Env: env}, true
	case "linux", "freebsd", "openbsd", "netbsd":
		return Command{Name: "notify-send", Args: []string{"--app-name=tq-hub", "--", clean(title), clean(body)}, Env: env}, true
	}
	return Command{}, false
}

func Send(ctx context.Context, title, body string) error {
	c, ok := Build(runtime.GOOS, title, body)
	if !ok {
		return nil
	}
	return SendWith(ctx, c)
}

func SendWith(ctx context.Context, c Command) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.WaitDelay = 500 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return err
	}
	return ctx.Err()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go vet ./internal/hub/runner/ ./internal/hub/notify/; go test ./internal/hub/runner/ ./internal/hub/notify/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/hub/runner/classify.go internal/hub/runner/classify_test.go internal/hub/runner/testdata/states/waiting.json internal/hub/runner/testdata/states/finished_feature.json internal/hub/runner/testdata/states/finished_feature_dod_failed.json internal/hub/runner/testdata/states/finished_build.json internal/hub/runner/testdata/states/halted.json internal/hub/runner/testdata/states/rolled_back.json internal/hub/runner/testdata/states/in_progress.json internal/hub/notify/notify.go internal/hub/notify/notify_test.go
git commit -m "feat(hub): outcome classification table and bounded desktop notifications"
```

---
### Task 8: Runner core: queue, coalescing, checkout, rounds, budgets, retry, relocate, permission asks, cancel

**Files:**
- Create: `internal/hub/runner/service.go`, `internal/hub/runner/prompt.go`, `internal/hub/runner/rounds.go`
- Test: `internal/hub/runner/harness_test.go`, `internal/hub/runner/prompt_test.go`, `internal/hub/runner/rounds_test.go`, `internal/hub/runner/budget_test.go`

**Interfaces:**
- Consumes: Tasks 1, 3, 4 (store), Task 2 (activity verbs), Task 5 (`adapter.Adapter`, `RunRequest`, `RunResult`, `LaunchEnv`, `ToolSpec`, `DefaultGrace`), Task 6 (`gate.API`, `gate.State`, `gate.IsNoPlan`, `gate.Unavailable`, `gate.Actor`), Task 7 (`Classify`, `Observation`, `Outcome`, `StateKey`, `StripRelocate`); `redact.NewWriter`.
- Produces (package `runner`):
  - `type Config struct { Workspace, Root string; Store *store.Store; Gate gate.API; Adapters map[string]adapter.Adapter; Adapter string; Env func() []string; Notify func(ctx context.Context, title, body string) error; Actor string; Now func() time.Time; Tick, RetryDelay, PollEvery time.Duration; GateURL string }`; `New(cfg Config) (*Service, error)`.
  - `type Service` with `OnApprove func(card store.Card, run store.Run)` (set by the CLI factory; Task 9 calls it); accessors `Store() *store.Store`, `Gate() gate.API`, `Workspace() string`, `Root() string`, `GateURL() string`, `RunDir(r store.Run) string`.
  - Lifecycle: `Start(ctx)` (recovers stale runs, ticks until `ctx` ends), `Tick(ctx) error`, `Wait()`, `Drain(ctx) error` (run every due run to its next stop, then return), `RecoverStale() error`, `Enqueue(ctx, cardID, reason string) (store.Run, bool, error)` (`true` when a run was created or resumed), `Cancel(ctx, runID string) (store.Run, error)`.
  - Read and simple ops: `Add(ctx, nc store.NewCard, budget *store.Budget) (store.Card, error)`, `List(ctx, f store.CardFilter, waitingOnly bool) ([]CardView, error)`, `Show(ctx, id string) (CardDetail, error)`, `Run(ctx, id string) (store.Run, error)`, `Comment(ctx, cardID, body string) (store.Comment, error)`, `Waiting(ctx) ([]WaitingItem, error)`.
  - Views: `CardView{store.Card; Waiting int; Running bool; RunID, RunStatus string; CostUSD float64}`, `CardDetail{CardView; Comments; Decisions; Runs; Attachments; Children; Parent *store.Card}`, `WaitingItem{Workspace string; Card store.Card; Decision store.Decision}`, `MoveOutcome{Card store.Card; Enqueued, Cancelled *store.Run}`, `AnswerOutcome{Decision store.Decision; AlreadyAnswered bool; Resumed *store.Run; Message string}`.
  - `type Ops interface` (implemented by `*Service` once Task 9 adds `Move` and `Answer`, and by `web.Client` in Task 10): `Add`, `List`, `Show`, `Move(ctx, id, lane string, cancelRun bool) (MoveOutcome, error)`, `Run`, `Answer(ctx, cardID, decisionID, choice, note string) (AnswerOutcome, error)`, `Cancel`, `Comment`, `Waiting`.
  - Caller marking: `WithAgent(ctx) context.Context`, `IsAgent(ctx) bool`, `ErrHumanOnly`; `ErrLocked`; `type RunActiveError struct{ Run store.Run }`.
  - Prompts: `OnRampFor(kind string) string`, `TaskText(c store.Card) string`, `Prompt(r store.Run, c store.Card) string`, `HubTaskJSON(workspace string, c store.Card) string`.
  - Internal helpers Task 9 uses: `(*Service).mirror(card, run, st)`, `end(run, card, status, reason)`, `fail(...)`, `comment(card, run, text)`, `save(run)`, `stamp() string`, `moveToKind(card, kind)`, `notify(title, body)`, `decodeState(raw) *gate.State`, `human() string`.

- [ ] **Step 1: Write the test harness and the failing tests**

`internal/hub/runner/harness_test.go`:

```go
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

type step func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error)

type fakeAdapter struct {
	mu    sync.Mutex
	reqs  []adapter.RunRequest
	steps []step
}

func (f *fakeAdapter) Start(ctx context.Context, req adapter.RunRequest, log io.Writer) (adapter.RunResult, error) {
	f.mu.Lock()
	i := len(f.reqs)
	f.reqs = append(f.reqs, req)
	var s step
	if i < len(f.steps) {
		s = f.steps[i]
	} else if len(f.steps) > 0 {
		s = f.steps[len(f.steps)-1]
	}
	f.mu.Unlock()
	fmt.Fprintf(log, "fake round %d\n", i)
	if s == nil {
		return adapter.RunResult{StopReason: "success", CostKnown: true}, nil
	}
	return s(ctx, req)
}

func (f *fakeAdapter) calls() []adapter.RunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]adapter.RunRequest(nil), f.reqs...)
}

type fakeGate struct {
	mu        sync.Mutex
	states    map[string]*gate.State
	slugs     map[string][]string // dir → slugs, for discovery
	answers   []string
	answerErr error
}

func newFakeGate() *fakeGate {
	return &fakeGate{states: map[string]*gate.State{}, slugs: map[string][]string{}}
}

func (g *fakeGate) set(slug string, st *gate.State) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cp := *st
	cp.Slug = slug
	g.states[slug] = &cp
}

func (g *fakeGate) State(_ context.Context, dir, slug string, _ bool) (*gate.State, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.states[slug]
	if !ok {
		return nil, &gate.GateError{Code: 1, Msg: "no plan at docs/checkpoints/" + slug + ".json"}
	}
	cp := *st
	cp.Raw, _ = json.Marshal(st)
	return &cp, nil
}

func (g *fakeGate) Answer(_ context.Context, dir, slug, id, choice, note, actor string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answers = append(g.answers, strings.Join([]string{slug, id, choice, actor}, " "))
	if g.answerErr != nil {
		return "", g.answerErr
	}
	if !g.resolve(slug, id, choice, note, actor) {
		return "", &gate.GateError{Code: 1, Msg: "no pending decision '" + id + "' in '" + slug + "'."}
	}
	return "decision " + id + ": " + choice + " by " + actor, nil
}

// resolve moves a pending decision to the answered list, as the real gate does. Caller holds g.mu.
func (g *fakeGate) resolve(slug, id, choice, note, actor string) bool {
	st, ok := g.states[slug]
	if !ok {
		return false
	}
	found := false
	var keep []gate.Decision
	for _, p := range st.PendingDecisions {
		if p.ID == id {
			found = true
			st.Decisions = append(st.Decisions, gate.Decision{ID: p.ID, Kind: p.Kind, Options: p.Options, Status: "answered",
				Choice: choice, Note: note, Actor: actor, AnsweredAt: "2026-10-21T12:00:00+00:00"})
			if p.Kind == "result" && choice == "approve" {
				st.Finished = true
			}
			continue
		}
		keep = append(keep, p)
	}
	st.PendingDecisions = keep
	return found
}

// terminalAnswer is the human answering in their own terminal with build-gate answer.
func (g *fakeGate) terminalAnswer(slug, id, choice string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resolve(slug, id, choice, "", "terminal")
}

func (g *fakeGate) Slugs(dir string, _ time.Time) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.slugs[dir]
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type harness struct {
	t     *testing.T
	st    *store.Store
	svc   *Service
	ad    *fakeAdapter
	gt    *fakeGate
	clock *fakeClock
	root  string
	mu    sync.Mutex
	notes []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "apps", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &harness{t: t, st: st, ad: &fakeAdapter{}, gt: newFakeGate(), root: root,
		clock: &fakeClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}}
	svc, err := New(Config{Workspace: "acme", Root: root, Store: st, Gate: h.gt,
		Adapters: map[string]adapter.Adapter{store.AdapterClaude: h.ad},
		Env:      func() []string { return []string{"PATH=/bin", "TQ_BUILD_TOKEN=leak", "TQ_BUILD_ACTOR=hub:x", "TENTAQLES_HEADLESS_CHILD=1"} },
		Notify: func(_ context.Context, title, body string) error {
			h.mu.Lock()
			h.notes = append(h.notes, title+": "+body)
			h.mu.Unlock()
			return nil
		},
		Actor: "hub:tester", Now: h.clock.Now, RetryDelay: 2 * time.Minute, PollEvery: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	t.Cleanup(svc.Wait)
	return h
}

func (h *harness) card(kind string, tags ...string) store.Card {
	h.t.Helper()
	c, err := h.st.AddCard(store.NewCard{Title: "Export invoices", Body: "CSV per month.", Kind: kind, Tags: tags, ProjectDir: "apps/billing"}, "renato")
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func (h *harness) enqueue(c store.Card) store.Run {
	h.t.Helper()
	r, _, err := h.svc.Enqueue(context.Background(), c.ID, "test")
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *harness) tick() {
	h.t.Helper()
	if err := h.svc.Tick(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	h.svc.Wait()
}

func (h *harness) run(id string) store.Run {
	h.t.Helper()
	r, err := h.st.Run(id)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *harness) comments(cardID string) string {
	cs, _ := h.st.Comments(cardID)
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.AuthorKind + ": " + c.Body + "\n")
	}
	return b.String()
}

func (h *harness) notifications() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.notes...)
}

func result(text string) step {
	return func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{StopReason: "success", Transcript: text, CostKnown: true, SessionID: "s1"}, nil
	}
}

func withState(g *fakeGate, slug string, st *gate.State, then step) step {
	return func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		g.set(slug, st)
		return then(ctx, req)
	}
}
```

`internal/hub/runner/prompt_test.go`:

```go
package runner

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestPromptPerKind(t *testing.T) {
	card := func(kind string) store.Card {
		return store.Card{ID: "c-abcdefghijkl", Title: "Login fails", Kind: kind, Body: "---\nbudget: usd=3\n---\nSteps: open /login."}
	}
	cases := map[string]string{
		"project":  "/tentaqles:kickoff Login fails\n\nSteps: open /login. --headless",
		"feature":  "/tentaqles:feature Login fails\n\nSteps: open /login. --headless",
		"analysis": "/tentaqles:feature Login fails\n\nSteps: open /login. --headless",
		"chore":    "/tentaqles:feature Login fails\n\nSteps: open /login. --headless",
		"fix":      "/tentaqles:fix Login fails\n\nSteps: open /login. --headless",
		"hotfix":   "/tentaqles:fix Login fails\n\nSteps: open /login. --hotfix --headless",
	}
	for kind, want := range cases {
		c := card(kind)
		r := store.Run{OnRamp: OnRampFor(kind), Phase: store.PhaseStart}
		if got := Prompt(r, c); got != want {
			t.Errorf("%s:\n got %q\nwant %q", kind, got, want)
		}
	}
	relocated := store.Run{OnRamp: store.OnRampFeature, Phase: store.PhaseRelocated, Slug: "login"}
	if got := Prompt(relocated, card("feature")); got != cases["feature"] {
		t.Errorf("after the hand-off the on-ramp runs again in the worktree: %q", got)
	}
	resume := store.Run{OnRamp: store.OnRampResume, Phase: store.PhaseResume, Slug: "login"}
	if got := Prompt(resume, card("fix")); got != "/tentaqles:build resume login --headless" {
		t.Errorf("resume: %q", got)
	}
	noSlug := store.Run{OnRamp: store.OnRampResume, Phase: store.PhaseResume}
	if got := Prompt(noSlug, card("fix")); got != cases["fix"] {
		t.Errorf("resume without a slug falls back to the on-ramp: %q", got)
	}
}

func TestHubTaskJSONShape(t *testing.T) {
	got := HubTaskJSON("acme", store.Card{ID: "c-abcdefghijkl", Kind: "hotfix"})
	if got != `{"hub":"c-abcdefghijkl","workspace":"acme","kind":"hotfix","external":null}` {
		t.Fatal(got)
	}
}
```

`internal/hub/runner/rounds_test.go`:

```go
package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestEnqueueCoalescesWakeups(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	first, created, _ := h.svc.Enqueue(context.Background(), c.ID, "move")
	for i := 0; i < 4; i++ {
		again, made, err := h.svc.Enqueue(context.Background(), c.ID, "wake")
		if err != nil || made || again.ID != first.ID {
			t.Fatalf("wake %d: %+v %v %v", i, again, made, err)
		}
	}
	runs, _ := h.st.RunsForCard(c.ID)
	got, _ := h.st.Card(c.ID)
	if !created || len(runs) != 1 || got.LockedByRun != first.ID || first.OnRamp != store.OnRampFeature {
		t.Fatalf("runs %d lock %q first %+v", len(runs), got.LockedByRun, first)
	}
}

func TestEnqueueOnALockedCardIsDroppedWithAComment(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature")
	h.st.Checkout(c.ID, "r-stalestalest")
	if _, _, err := h.svc.Enqueue(context.Background(), c.ID, "requested"); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	if !strings.Contains(h.comments(c.ID), "locked by run r-stalestalest") {
		t.Fatal(h.comments(c.ID))
	}
}

func TestRelocateThenSecondRoundInTheWorktree(t *testing.T) {
	h := newHarness(t)
	wt := filepath.Join(h.root, "billing-build-demo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	h.ad.steps = []step{
		result("Worktree and hooks ready.\nTQ_HUB_RELOCATE " + wt + " demo"),
		withState(h.gt, "demo", loadState(t, "waiting"), result("Plan written; waiting for approval.")),
	}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	mid := h.run(r.ID)
	if mid.Status != store.StatusQueued || mid.Worktree != wt || mid.Slug != "demo" || mid.Phase != store.PhaseRelocated || mid.Retried != 0 || mid.Rounds != 1 {
		t.Fatalf("after hand-off %+v", mid)
	}
	h.tick()
	calls := h.ad.calls()
	if len(calls) != 2 || calls[0].Dir != filepath.Join(h.root, "apps", "billing") || calls[1].Dir != wt {
		t.Fatalf("dirs %q", []string{calls[0].Dir, calls[len(calls)-1].Dir})
	}
	if calls[1].SessionID != "" || calls[1].Prompt != calls[0].Prompt || !strings.HasPrefix(calls[0].Prompt, "/tentaqles:feature ") {
		t.Fatalf("second round prompt %q session %q", calls[1].Prompt, calls[1].SessionID)
	}
	end := h.run(r.ID)
	pend, _ := h.st.PendingDecisions()
	if end.Status != store.StatusWaiting || len(pend) != 1 || pend[0].ID != "d1a2b3c4" || pend[0].Slug != "demo" || pend[0].RunID != r.ID {
		t.Fatalf("run %+v pending %+v", end, pend)
	}
	if !strings.Contains(h.comments(c.ID), "agent: Plan written") || len(h.notifications()) != 1 {
		t.Fatalf("comments %q notes %v", h.comments(c.ID), h.notifications())
	}
	if strings.Contains(h.comments(c.ID), "TQ_HUB_RELOCATE") {
		t.Fatal("the relocate line is not an agent comment")
	}
}

func TestRunnerRelocateLoopStops(t *testing.T) {
	h := newHarness(t)
	wt := filepath.Join(h.root, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	h.ad.steps = []step{result("TQ_HUB_RELOCATE " + wt + " demo")}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	h.tick()
	h.tick()
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusFailed || got.StopReason != "relocate loop" || len(h.ad.calls()) != 2 || card.LockedByRun != "" {
		t.Fatalf("run %+v calls %d lock %q", got, len(h.ad.calls()), card.LockedByRun)
	}
}

func TestSlugIsDiscoveredFromTheTaskBlock(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	st := loadState(t, "waiting")
	st.Task.Hub = c.ID
	dir := filepath.Join(h.root, "apps", "billing")
	h.gt.slugs[dir] = []string{"other", "demo"}
	other := loadState(t, "waiting")
	other.Task.Hub = "c-someoneelse1"
	h.gt.set("other", other)
	h.gt.set("demo", st)
	r := h.enqueue(c)
	h.tick()
	if got := h.run(r.ID); got.Slug != "demo" || got.Status != store.StatusWaiting {
		t.Fatalf("%+v", got)
	}
}

func TestWaitingOnAResultMovesTheCardToReview(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	h.st.MoveCard(c.ID, "in progress", "renato")
	st := loadState(t, "waiting")
	st.PendingDecisions[0].Kind = "result"
	st.PendingDecisions[0].Options = []string{"approve", "request-changes", "stop"}
	r := h.enqueue(c)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.gt.set("demo", st)
	h.tick()
	if got, _ := h.st.Card(c.ID); got.Lane != "review" || got.LockedByRun != r.ID {
		t.Fatalf("card %+v (a waiting run keeps its lock)", got)
	}
}

func TestFinishedHaltedRolledBack(t *testing.T) {
	for name, want := range map[string]struct{ status, reason, comment string }{
		"finished_build": {store.StatusFinished, "", ""},
		"halted":         {store.StatusFailed, "behavior failed 3x after split", "The build halted at checkpoint 2"},
		"rolled_back":    {store.StatusFailed, "rolled back", "Re-approve in your own terminal"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			c := h.card("feature", store.TagAgentReady)
			r := h.enqueue(c)
			r.Slug = "demo"
			h.st.SaveRun(r)
			h.gt.set("demo", loadState(t, name))
			h.tick()
			got := h.run(r.ID)
			card, _ := h.st.Card(c.ID)
			if got.Status != want.status || got.StopReason != want.reason || card.LockedByRun != "" {
				t.Fatalf("%+v lock %q", got, card.LockedByRun)
			}
			if want.comment != "" && !strings.Contains(h.comments(c.ID), want.comment) {
				t.Fatal(h.comments(c.ID))
			}
			if name == "finished_build" && card.Lane != "review" {
				t.Fatalf("a finished run moves the card to review: %s", card.Lane)
			}
		})
	}
}

func TestRetryOnceAfterTwoMinutes(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{ExitCode: 1, StopReason: "", CostKnown: true}, nil
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	got := h.run(r.ID)
	if got.Status != store.StatusQueued || got.Retried != 1 || got.NotBefore != h.clock.Now().Add(2*time.Minute).Format(time.RFC3339) {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(h.comments(c.ID), "retrying once in 2m0s") || !strings.Contains(h.comments(c.ID), "fake round 0") {
		t.Fatal(h.comments(c.ID))
	}
	h.tick()
	if len(h.ad.calls()) != 1 {
		t.Fatal("no retry before the delay")
	}
	h.clock.Advance(2*time.Minute + time.Second)
	h.tick()
	got = h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if len(h.ad.calls()) != 2 || got.Status != store.StatusFailed || got.StopReason != "exit 1" || card.LockedByRun != "" {
		t.Fatalf("%+v calls %d", got, len(h.ad.calls()))
	}
	if !strings.Contains(h.comments(c.ID), "failed again (exit 1)") {
		t.Fatal(h.comments(c.ID))
	}
}

func TestPermissionDenialBecomesAHubDecision(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{ExitCode: 1, StopReason: "error_during_execution", Transcript: "Bash was not allowed", CostKnown: true,
			PermissionDenials: []adapter.PermissionDenial{{Tool: "Bash", Input: map[string]any{"command": "npm test"}}}}, nil
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if h.run(r.ID).Status != store.StatusWaiting || len(pend) != 1 || pend[0].Kind != store.KindPermission || pend[0].Data["tool"] != "Bash(npm test)" {
		t.Fatalf("pending %+v", pend)
	}
	if strings.Join(pend[0].Options, ",") != "allow-once,stop" {
		t.Fatal(pend[0].Options)
	}
}

func TestMaxTurnsResumesTheSameSession(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{
		func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{StopReason: "error_max_turns", SessionID: "s-7", CostKnown: true}, nil
		},
		result("done"),
	}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	if got := h.run(r.ID); got.Status != store.StatusQueued || got.SessionID != "s-7" {
		t.Fatalf("%+v", got)
	}
	h.tick()
	if calls := h.ad.calls(); calls[1].SessionID != "s-7" {
		t.Fatalf("resume session %q", calls[1].SessionID)
	}
}

func TestLaunchEnvironmentReachesTheAdapter(t *testing.T) {
	h := newHarness(t)
	c := h.card("hotfix", store.TagAgentReady)
	h.enqueue(c)
	h.tick()
	env := strings.Join(h.ad.calls()[0].Env, "\n")
	if !strings.Contains(env, "TENTAQLES_HEADLESS=1") || !strings.Contains(env, `TQ_HUB_TASK={"hub":"`+c.ID+`","workspace":"acme","kind":"hotfix","external":null}`) {
		t.Fatal(env)
	}
	for _, bad := range []string{"TQ_BUILD_TOKEN", "TQ_BUILD_ACTOR", "TENTAQLES_HEADLESS_CHILD"} {
		if strings.Contains(env, bad) {
			t.Fatalf("%s reached a run", bad)
		}
	}
}

func TestConcurrencyLimitAndFIFO(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return adapter.RunResult{StopReason: "success", CostKnown: true}, nil
	}}
	var cards []store.Card
	for i := 0; i < 3; i++ {
		c := h.card("feature", store.TagAgentReady)
		h.enqueue(c)
		cards = append(cards, c)
	}
	if err := h.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.ad.calls()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	h.svc.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)
	if n := len(h.ad.calls()); n != 2 {
		t.Fatalf("%d rounds running, concurrency is 2", n)
	}
	running, _ := h.st.RunsWithStatus(store.StatusRunning)
	ids := map[string]bool{}
	for _, r := range running {
		ids[r.CardID] = true
	}
	if !ids[cards[0].ID] || !ids[cards[1].ID] {
		t.Fatal("the first two queued runs start first")
	}
	close(release)
	h.svc.Wait()
	h.tick()
	if n := len(h.ad.calls()); n != 3 {
		t.Fatalf("third run did not start: %d", n)
	}
}

func TestCancelARunningRound(t *testing.T) {
	h := newHarness(t)
	started := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		close(started)
		<-ctx.Done()
		return adapter.RunResult{ExitCode: -1}, ctx.Err()
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.svc.Tick(context.Background())
	<-started
	got, err := h.svc.Cancel(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusCancelled || card.LockedByRun != "" || len(h.gt.answers) != 0 {
		t.Fatalf("%+v lock %q answers %v", got, card.LockedByRun, h.gt.answers)
	}
	if !strings.Contains(h.comments(c.ID), "gate state stays as it is") {
		t.Fatal(h.comments(c.ID))
	}
	if _, err := h.svc.Cancel(context.Background(), r.ID); err == nil {
		t.Fatal("cancelling an ended run is an error")
	}
}

func TestRecoverStaleRunningRuns(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	r.Status = store.StatusRunning
	h.st.SaveRun(r)
	if err := h.svc.RecoverStale(); err != nil {
		t.Fatal(err)
	}
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusFailed || !strings.Contains(got.StopReason, "hub stopped") || card.LockedByRun != "" {
		t.Fatalf("%+v", got)
	}
	orphan := h.card("feature")
	h.st.Checkout(orphan.ID, "r-gonegonegone")
	h.svc.RecoverStale()
	if got, _ := h.st.Card(orphan.ID); got.LockedByRun != "" {
		t.Fatal("a lock held by a run that does not exist is released")
	}
}

func TestViewsAndAgentBudget(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.Add(WithAgent(ctx), store.NewCard{Title: "x"}, &store.Budget{Rounds: 1}); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent cannot set a budget: %v", err)
	}
	b := store.Budget{Tokens: 1000, USD: 1, Minutes: 5, Rounds: 2}
	c, err := h.svc.Add(ctx, store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	if err != nil || !strings.HasPrefix(c.Body, "---\nbudget: ") {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := h.svc.Add(WithAgent(ctx), store.NewCard{Title: "agent card"}, nil); err != nil {
		t.Fatal("agents may add cards")
	}
	r, _ := h.svc.Run(ctx, c.ID)
	if r.Budget != b {
		t.Fatalf("budget from front matter: %+v", r.Budget)
	}
	h.st.AskHub(c.ID, r.ID, "", "?", []string{"a"}, nil)
	views, _ := h.svc.List(ctx, store.CardFilter{}, true)
	if len(views) != 1 || views[0].Waiting != 1 || views[0].RunStatus != store.StatusQueued {
		t.Fatalf("%+v", views)
	}
	detail, err := h.svc.Show(ctx, c.ID)
	if err != nil || len(detail.Runs) != 1 || len(detail.Decisions) != 1 {
		t.Fatalf("%+v %v", detail, err)
	}
	if cm, err := h.svc.Comment(WithAgent(ctx), c.ID, "PR: https://github.com/acme/x/pull/7"); err != nil || cm.AuthorKind != store.AuthorAgent {
		t.Fatalf("%+v %v", cm, err)
	}
	items, _ := h.svc.Waiting(ctx)
	if len(items) != 1 || items[0].Workspace != "acme" || items[0].Card.ID != c.ID {
		t.Fatalf("%+v", items)
	}
}
```

`internal/hub/runner/budget_test.go`:

```go
package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// maxTurns keeps a run going round after round, each round costing what it is given.
func maxTurns(h *harness, usage adapter.Usage, cost float64, known bool, advance time.Duration) step {
	return func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		h.clock.Advance(advance)
		return adapter.RunResult{StopReason: "error_max_turns", Usage: usage, CostUSD: cost, CostKnown: known, SessionID: "s1"}, nil
	}
}

func TestEachBudgetLimitStopsTheRun(t *testing.T) {
	cases := []struct {
		limit  string
		budget store.Budget
		usage  adapter.Usage
		cost   float64
		adv    time.Duration
		rounds int // rounds that run before the stop
	}{
		{"tokens", store.Budget{Tokens: 1000, USD: 100, Minutes: 1000, Rounds: 50}, adapter.Usage{Input: 400, Output: 100, CacheRead: 99999}, 0.01, 0, 2},
		{"usd", store.Budget{Tokens: 1e9, USD: 10, Minutes: 1000, Rounds: 50}, adapter.Usage{}, 6, 0, 2},
		{"minutes", store.Budget{Tokens: 1e9, USD: 100, Minutes: 120, Rounds: 50}, adapter.Usage{}, 0.01, 70 * time.Minute, 2},
		{"rounds", store.Budget{Tokens: 1e9, USD: 100, Minutes: 1000, Rounds: 3}, adapter.Usage{}, 0.01, 0, 3},
	}
	for _, c := range cases {
		t.Run(c.limit, func(t *testing.T) {
			h := newHarness(t)
			h.ad.steps = []step{maxTurns(h, c.usage, c.cost, true, c.adv)}
			card, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &c.budget)
			r, _ := h.svc.Run(context.Background(), card.ID)
			for i := 0; i < 10; i++ {
				h.tick()
			}
			got := h.run(r.ID)
			if got.Status != store.StatusOverBudget || got.StopReason != "budget: "+c.limit || len(h.ad.calls()) != c.rounds {
				t.Fatalf("%+v calls %d", got, len(h.ad.calls()))
			}
			after, _ := h.st.Card(card.ID)
			pend, _ := h.st.PendingDecisions()
			if after.LockedByRun != "" || len(pend) != 1 || pend[0].Kind != store.KindPermission || strings.Join(pend[0].Options, ",") != "raise-and-resume,stop" {
				t.Fatalf("lock %q pending %+v", after.LockedByRun, pend)
			}
			if !strings.Contains(h.comments(card.ID), "Budget reached ("+c.limit+")") {
				t.Fatal(h.comments(card.ID))
			}
			acts, _ := h.st.Activity(100)
			stops := 0
			for _, a := range acts {
				if a.Verb == activity.VerbBudgetStop {
					stops++
				}
			}
			if stops != 1 {
				t.Fatalf("budget.stop logged %d times", stops)
			}
		})
	}
}

func TestBudgetRoundsStopsWithoutCost(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{maxTurns(h, adapter.Usage{}, 0, false, 0)}
	b := store.Budget{Tokens: 1e9, USD: 1, Minutes: 1000, Rounds: 3}
	card, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	r, _ := h.svc.Run(context.Background(), card.ID)
	for i := 0; i < 6; i++ {
		h.tick()
	}
	got := h.run(r.ID)
	if got.Status != store.StatusOverBudget || got.StopReason != "budget: rounds" || got.CostUSD != 0 || len(got.RoundLog) != 3 || !got.RoundLog[0].CostUnknown {
		t.Fatalf("%+v", got)
	}
	if strings.Count(h.comments(card.ID), "reported no cost") != 3 {
		t.Fatal(h.comments(card.ID))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/hub/runner/ -count=1`
Expected: FAIL, `undefined: New`, `undefined: Config`, `undefined: Prompt`.

- [ ] **Step 3: Implement the service, prompts and rounds**

`internal/hub/runner/service.go`:

```go
// Package runner drives hub runs: it queues work per card, checks the card out
// atomically, launches adapter rounds with the workspace's environment, reads the
// gate state after every round, classifies the outcome, enforces budgets, and
// carries the human's answers back to the gate. It is the only writer of runs.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

type Config struct {
	Workspace  string
	Root       string // the workspace root folder; card project folders are relative to it
	Store      *store.Store
	Gate       gate.API
	Adapters   map[string]adapter.Adapter
	Adapter    string          // default adapter; "" → claude
	Env        func() []string // the workspace's environment plan; nil → os.Environ
	Notify     func(ctx context.Context, title, body string) error
	Actor      string // "hub:<os user>"; "" → gate.Actor()
	Now        func() time.Time
	Tick       time.Duration // 0 → 2 s
	RetryDelay time.Duration // 0 → 2 min
	PollEvery  time.Duration // waiting runs re-read the gate this often; 0 → 15 s
	GateURL    string        // the gate's own live view, linked while a run is active
}

type flight struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Service struct {
	cfg Config
	st  *store.Store

	mu       sync.Mutex // flights and lastPoll
	flights  map[string]*flight
	lastPoll map[string]time.Time
	enqMu    sync.Mutex // serializes Enqueue within this process
	wg       sync.WaitGroup

	// OnApprove runs after the human approved a result (Task 11: the static review export).
	OnApprove func(card store.Card, run store.Run)
}

func New(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("runner: no store")
	}
	if cfg.Gate == nil {
		cfg.Gate = gate.Unavailable{Err: errors.New("no build gate configured")}
	}
	if cfg.Adapter == "" {
		cfg.Adapter = store.AdapterClaude
	}
	if cfg.Env == nil {
		cfg.Env = os.Environ
	}
	if cfg.Actor == "" {
		cfg.Actor = gate.Actor()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Tick <= 0 {
		cfg.Tick = 2 * time.Second
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 2 * time.Minute
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = 15 * time.Second
	}
	return &Service{cfg: cfg, st: cfg.Store, flights: map[string]*flight{}, lastPoll: map[string]time.Time{}}, nil
}

func (s *Service) Store() *store.Store { return s.st }
func (s *Service) Gate() gate.API      { return s.cfg.Gate }
func (s *Service) Workspace() string   { return s.cfg.Workspace }
func (s *Service) Root() string        { return s.cfg.Root }
func (s *Service) GateURL() string     { return s.cfg.GateURL }

func (s *Service) human() string { return strings.TrimPrefix(s.cfg.Actor, "hub:") }
func (s *Service) stamp() string { return s.cfg.Now().UTC().Format(time.RFC3339) }

// RunDir is where a round runs: the worktree once the hand-off named it, else the card's project folder.
func (s *Service) RunDir(r store.Run) string {
	if r.Worktree != "" {
		return r.Worktree
	}
	if r.ProjectDir == "" {
		return s.cfg.Root
	}
	return filepath.Join(s.cfg.Root, filepath.FromSlash(r.ProjectDir))
}

func (s *Service) adapterFor(c store.Card) string {
	if c.HasTag("adapter:process") {
		if _, ok := s.cfg.Adapters[store.AdapterProcess]; ok {
			return store.AdapterProcess
		}
	}
	return s.cfg.Adapter
}

type agentKey struct{}

// WithAgent marks a request as coming from an agent session (spec §15).
func WithAgent(ctx context.Context) context.Context { return context.WithValue(ctx, agentKey{}, true) }

func IsAgent(ctx context.Context) bool {
	v, _ := ctx.Value(agentKey{}).(bool)
	return v
}

var (
	ErrHumanOnly = errors.New("human-only: run it in your own terminal or on the hub page (agent sessions may add cards and comment, nothing else)")
	ErrLocked    = errors.New("the card is locked by another run")
)

type RunActiveError struct{ Run store.Run }

func (e *RunActiveError) Error() string {
	return fmt.Sprintf("card %s has a %s run %s; pass --cancel-run (or confirm on the page) to cancel it and move the card",
		e.Run.CardID, e.Run.Status, e.Run.ID)
}

type CardView struct {
	store.Card
	Waiting   int     `json:"waiting"`
	Running   bool    `json:"running"`
	RunID     string  `json:"run_id,omitempty"`
	RunStatus string  `json:"run_status,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
}

type CardDetail struct {
	CardView
	Comments    []store.Comment    `json:"comments"`
	Decisions   []store.Decision   `json:"decisions"`
	Runs        []store.Run        `json:"runs"`
	Attachments []store.Attachment `json:"attachments"`
	Children    []store.Card       `json:"children"`
	Parent      *store.Card        `json:"parent,omitempty"`
}

type WaitingItem struct {
	Workspace string         `json:"workspace"`
	Card      store.Card     `json:"card"`
	Decision  store.Decision `json:"decision"`
}

type MoveOutcome struct {
	Card      store.Card `json:"card"`
	Enqueued  *store.Run `json:"enqueued,omitempty"`
	Cancelled *store.Run `json:"cancelled,omitempty"`
}

type AnswerOutcome struct {
	Decision        store.Decision `json:"decision"`
	AlreadyAnswered bool           `json:"already_answered"`
	Resumed         *store.Run     `json:"resumed,omitempty"`
	Message         string         `json:"message"`
}

// Ops is what the CLI needs, served by *Service directly or by web.Client over a running hub.
type Ops interface {
	Add(ctx context.Context, nc store.NewCard, budget *store.Budget) (store.Card, error)
	List(ctx context.Context, f store.CardFilter, waitingOnly bool) ([]CardView, error)
	Show(ctx context.Context, id string) (CardDetail, error)
	Move(ctx context.Context, id, lane string, cancelRun bool) (MoveOutcome, error)
	Run(ctx context.Context, id string) (store.Run, error)
	Answer(ctx context.Context, cardID, decisionID, choice, note string) (AnswerOutcome, error)
	Cancel(ctx context.Context, runID string) (store.Run, error)
	Comment(ctx context.Context, cardID, body string) (store.Comment, error)
	Waiting(ctx context.Context) ([]WaitingItem, error)
}

func (s *Service) Add(ctx context.Context, nc store.NewCard, budget *store.Budget) (store.Card, error) {
	if budget != nil {
		if IsAgent(ctx) {
			return store.Card{}, ErrHumanOnly
		}
		nc.Body = store.WithBudgetFrontMatter(nc.Body, *budget)
	}
	actor := s.human()
	if IsAgent(ctx) {
		actor = "agent"
	}
	return s.st.AddCard(nc, actor)
}

func (s *Service) view(c store.Card) (CardView, error) {
	v := CardView{Card: c}
	n, err := s.st.PendingCount(c.ID)
	if err != nil {
		return v, err
	}
	v.Waiting = n
	runs, err := s.st.RunsForCard(c.ID)
	if err != nil {
		return v, err
	}
	for _, r := range runs {
		v.CostUSD += r.CostUSD
	}
	if act, ok, err := s.st.ActiveRun(c.ID); err == nil && ok {
		v.RunID, v.RunStatus, v.Running = act.ID, act.Status, act.Status == store.StatusRunning
	}
	return v, nil
}

func (s *Service) List(ctx context.Context, f store.CardFilter, waitingOnly bool) ([]CardView, error) {
	cards, err := s.st.ListCards(f)
	if err != nil {
		return nil, err
	}
	out := []CardView{}
	for _, c := range cards {
		v, err := s.view(c)
		if err != nil {
			return nil, err
		}
		if waitingOnly && v.Waiting == 0 {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) Show(ctx context.Context, id string) (CardDetail, error) {
	c, err := s.st.Card(id)
	if err != nil {
		return CardDetail{}, err
	}
	v, err := s.view(c)
	if err != nil {
		return CardDetail{}, err
	}
	d := CardDetail{CardView: v}
	if d.Comments, err = s.st.Comments(id); err != nil {
		return d, err
	}
	if d.Decisions, err = s.st.DecisionsForCard(id); err != nil {
		return d, err
	}
	if d.Runs, err = s.st.RunsForCard(id); err != nil {
		return d, err
	}
	if d.Attachments, err = s.st.Attachments(id); err != nil {
		return d, err
	}
	if d.Children, err = s.st.Children(id); err != nil {
		return d, err
	}
	if c.ParentID != "" {
		if p, err := s.st.Card(c.ParentID); err == nil {
			d.Parent = &p
		}
	}
	return d, nil
}

func (s *Service) Comment(ctx context.Context, cardID, body string) (store.Comment, error) {
	if IsAgent(ctx) {
		return s.st.AddComment(cardID, store.AuthorAgent, "agent", body, "")
	}
	return s.st.AddComment(cardID, store.AuthorHuman, s.human(), body, "")
}

func (s *Service) Waiting(ctx context.Context) ([]WaitingItem, error) {
	ds, err := s.st.PendingDecisions()
	if err != nil {
		return nil, err
	}
	out := []WaitingItem{}
	for _, d := range ds {
		c, err := s.st.Card(d.CardID)
		if err != nil {
			continue
		}
		out = append(out, WaitingItem{Workspace: s.cfg.Workspace, Card: c, Decision: d})
	}
	return out, nil
}

// Run enqueues (or resumes) a run for the card now (tq hub run).
func (s *Service) Run(ctx context.Context, id string) (store.Run, error) {
	r, _, err := s.Enqueue(ctx, id, "requested")
	return r, err
}
```

`internal/hub/runner/prompt.go`:

```go
package runner

import (
	"encoding/json"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// OnRampFor maps a card kind to its on-ramp (spec §8.2). analysis and chore go through feature.
func OnRampFor(kind string) string {
	switch kind {
	case "project":
		return store.OnRampKickoff
	case "fix", "hotfix":
		return store.OnRampFix
	}
	return store.OnRampFeature
}

// TaskText is the card's title and body (front matter removed): the whole task text.
func TaskText(c store.Card) string {
	body := strings.TrimSpace(store.StripFrontMatter(c.Body))
	if body == "" {
		return c.Title
	}
	return c.Title + "\n\n" + body
}

// Prompt is the only text the hub gives the adapter; context comes from the project's files and skills.
func Prompt(r store.Run, c store.Card) string {
	if r.Slug != "" && (r.Phase == store.PhaseResume || r.OnRamp == store.OnRampResume || r.OnRamp == store.OnRampBuild) {
		return "/tentaqles:build resume " + r.Slug + " --headless"
	}
	onRamp := r.OnRamp
	if onRamp == store.OnRampResume {
		onRamp = OnRampFor(c.Kind)
	}
	task := TaskText(c)
	switch onRamp {
	case store.OnRampKickoff:
		return "/tentaqles:kickoff " + task + " --headless"
	case store.OnRampFix:
		if c.Kind == "hotfix" { // only this kind passes --hotfix; the hub never infers it
			return "/tentaqles:fix " + task + " --hotfix --headless"
		}
		return "/tentaqles:fix " + task + " --headless"
	case store.OnRampBuild:
		return "/tentaqles:build " + task + " --headless"
	}
	return "/tentaqles:feature " + task + " --headless"
}

// HubTaskJSON is TQ_HUB_TASK: {"hub","workspace","kind","external"} in that order (spec §13.1).
func HubTaskJSON(workspace string, c store.Card) string {
	b, _ := json.Marshal(struct {
		Hub       string    `json:"hub"`
		Workspace string    `json:"workspace"`
		Kind      string    `json:"kind"`
		External  *struct{} `json:"external"`
	}{c.ID, workspace, c.Kind, nil})
	return string(b)
}
```

`internal/hub/runner/rounds.go`:

```go
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// Enqueue coalesces wakeups: an active run is reused (a waiting run with nothing pending is
// resumed); otherwise a new run is created and the card checked out in one statement.
func (s *Service) Enqueue(ctx context.Context, cardID, reason string) (store.Run, bool, error) {
	s.enqMu.Lock()
	defer s.enqMu.Unlock()
	card, err := s.st.Card(cardID)
	if err != nil {
		return store.Run{}, false, err
	}
	act, ok, err := s.st.ActiveRun(cardID)
	if err != nil {
		return store.Run{}, false, err
	}
	if ok {
		if act.Status != store.StatusWaiting {
			return act, false, nil
		}
		n, err := s.st.PendingCount(cardID)
		if err != nil || n > 0 {
			return act, false, err
		}
		act.Status, act.NotBefore = store.StatusQueued, s.stamp()
		if act.Slug != "" {
			act.Phase = store.PhaseResume
		}
		return act, true, s.st.SaveRun(act)
	}
	onRamp, phase, slug, wt := OnRampFor(card.Kind), store.PhaseStart, "", ""
	if prev, err := s.st.RunsForCard(cardID); err == nil {
		for _, p := range prev {
			if p.Slug != "" {
				onRamp, phase, slug, wt = store.OnRampResume, store.PhaseResume, p.Slug, p.Worktree
				break
			}
		}
	}
	budget, berr := store.CardBudget(card, s.st.DefaultBudget())
	if berr != nil {
		budget = s.st.DefaultBudget()
		s.comment(card, store.Run{}, "The card's budget front matter is unreadable ("+berr.Error()+"); the workspace default budget applies.")
	}
	now := s.stamp()
	run, err := s.st.CreateRun(store.Run{CardID: cardID, Adapter: s.adapterFor(card), OnRamp: onRamp, ProjectDir: card.ProjectDir,
		Slug: slug, Worktree: wt, Phase: phase, Budget: budget, Kind: card.Kind, QueuedAt: now, NotBefore: now})
	if err != nil {
		return store.Run{}, false, err
	}
	got, err := s.st.Checkout(cardID, run.ID)
	if err != nil {
		return run, false, err
	}
	if !got {
		cur, _ := s.st.Card(cardID)
		run.Status, run.EndedAt, run.StopReason = store.StatusCancelled, now, "card locked by run "+cur.LockedByRun
		s.save(run)
		s.comment(cur, run, fmt.Sprintf("A run was requested (%s) but the card is locked by run %s; the request was dropped. If that run is gone, `tq hub cancel %s` releases the card.",
			reason, cur.LockedByRun, cur.LockedByRun))
		return run, false, ErrLocked
	}
	return run, true, nil
}

// Tick starts due queued runs up to the workspace's concurrency limit, oldest first.
func (s *Service) Tick(ctx context.Context) error {
	limit := s.st.Concurrency()
	s.mu.Lock()
	free := limit - len(s.flights)
	s.mu.Unlock()
	if free <= 0 {
		return nil
	}
	due, err := s.st.DueRuns(s.cfg.Now(), free)
	if err != nil {
		return err
	}
	for _, r := range due {
		s.startRound(ctx, r)
	}
	return nil
}

func (s *Service) startRound(parent context.Context, r store.Run) {
	s.mu.Lock()
	if _, busy := s.flights[r.ID]; busy {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	f := &flight{cancel: cancel, done: make(chan struct{})}
	s.flights[r.ID] = f
	s.mu.Unlock()
	finish := func() {
		s.mu.Lock()
		delete(s.flights, r.ID)
		s.mu.Unlock()
		cancel()
		close(f.done)
	}
	first := r.StartedAt == ""
	r.Status = store.StatusRunning
	if first {
		r.StartedAt = s.stamp()
	}
	if err := s.st.SaveRun(r); err != nil {
		finish()
		return
	}
	if first {
		_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunStart, CardID: r.CardID, RunID: r.ID,
			Data: map[string]any{"adapter": r.Adapter, "on_ramp": r.OnRamp, "kind": r.Kind}})
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer finish()
		s.round(ctx, r)
	}()
}

func decodeState(raw json.RawMessage) *gate.State {
	if len(raw) == 0 {
		return nil
	}
	var st gate.State
	if json.Unmarshal(raw, &st) != nil {
		return nil
	}
	st.Raw = raw
	return &st
}

func (s *Service) round(ctx context.Context, r store.Run) {
	card, err := s.st.Card(r.CardID)
	if err != nil {
		s.end(r, store.Card{}, store.StatusFailed, "card missing")
		return
	}
	if why := r.Exceeded(); why != "" {
		s.overBudget(r, card, why)
		return
	}
	dir := s.RunDir(r)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		s.fail(r, card, "project folder missing", fmt.Sprintf("The run's folder %s does not exist. Set the card's project folder (relative to the workspace root) and run it again.", dir))
		return
	}
	ad, ok := s.cfg.Adapters[r.Adapter]
	if !ok {
		s.fail(r, card, "adapter "+r.Adapter+" is not configured", "")
		return
	}
	prevKey := StateKey(decodeState(r.State))
	if err := os.MkdirAll(filepath.Dir(r.LogPath), 0o700); err != nil {
		s.fail(r, card, "cannot create the runs folder", err.Error())
		return
	}
	logf, err := os.OpenFile(r.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.fail(r, card, "cannot open the run log", err.Error())
		return
	}
	mw := redact.NewWriter(logf, s.st.Classes())
	start := s.cfg.Now()
	fmt.Fprintf(mw, "\n=== round %d at %s in %s\n", r.Rounds+1, start.UTC().Format(time.RFC3339), dir)
	req := adapter.RunRequest{Workspace: s.cfg.Workspace, Dir: dir, Prompt: Prompt(r, card), SessionID: r.SessionID,
		Env: adapter.LaunchEnv(s.cfg.Env(), HubTaskJSON(s.cfg.Workspace, card)), AllowedTools: r.AllowedTools}
	res, runErr := ad.Start(ctx, req, mw)
	mw.Close()
	logf.Close()
	secs := s.cfg.Now().Sub(start).Seconds()
	if cur, err := s.st.Run(r.ID); err == nil {
		r = cur
	}
	r.Rounds++
	r.Seconds += secs
	r.Usage = r.Usage.Add(store.Usage(res.Usage))
	r.CostUSD += res.CostUSD
	r.AllowedTools = []string{} // allow-once means one round
	if res.SessionID != "" {
		r.SessionID = res.SessionID
	}
	rec := store.RoundRecord{N: r.Rounds, StartedAt: start.UTC().Format(time.RFC3339), Seconds: secs, Usage: store.Usage(res.Usage),
		CostUSD: res.CostUSD, CostUnknown: !res.CostKnown && r.Adapter == store.AdapterClaude, ExitCode: res.ExitCode, Subtype: res.StopReason}
	if ctx.Err() != nil {
		rec.Outcome = "cancelled"
		r.RoundLog = append(r.RoundLog, rec)
		s.end(r, card, store.StatusCancelled, "cancelled by human")
		s.comment(card, r, "The run was cancelled. The gate state stays as it is; `tq hub run "+card.ID+"` resumes from it.")
		return
	}
	if runErr != nil {
		rec.Outcome = "start-error"
		r.RoundLog = append(r.RoundLog, rec)
		s.fail(r, card, "the adapter could not start", "The adapter could not start: "+runErr.Error())
		return
	}
	if rec.CostUnknown {
		s.comment(card, r, fmt.Sprintf("Round %d reported no cost and no priced usage; the dollar budget could not count it (rounds and minutes still apply).", r.Rounds))
	}
	final := strings.TrimSpace(res.Transcript)
	if text := StripRelocate(final); text != "" {
		_, _ = s.st.AddComment(card.ID, store.AuthorAgent, "agent", text, r.ID)
	}
	st := s.readState(ctx, &r, card, start)
	if st != nil {
		r.State = st.Raw
		s.mirror(card, r, st)
	}
	tool := ""
	if len(res.PermissionDenials) > 0 {
		tool = adapter.ToolSpec(res.PermissionDenials[0])
	}
	out := Classify(Observation{ExitCode: res.ExitCode, Subtype: res.StopReason, FinalText: final, State: st,
		PrevStateKey: prevKey, Relocations: r.Relocations, PermissionTool: tool, Retried: r.Retried})
	rec.Outcome = string(out.Verdict)
	r.RoundLog = append(r.RoundLog, rec)
	s.apply(r, card, out, tool, final, res.ExitCode)
}

// readState reads the gate state for the run's slug, or finds the slug: the build folder newer than
// the run whose plan task.hub is this card.
func (s *Service) readState(ctx context.Context, r *store.Run, card store.Card, roundStart time.Time) *gate.State {
	dir := s.RunDir(*r)
	if r.Slug != "" {
		st, err := s.cfg.Gate.State(ctx, dir, r.Slug, false)
		if err == nil {
			return st
		}
		if !gate.IsNoPlan(err) {
			s.comment(card, *r, "Could not read the gate state of "+r.Slug+": "+err.Error())
		}
		return nil
	}
	since := roundStart.Add(-time.Minute)
	if t, err := time.Parse(time.RFC3339, r.StartedAt); err == nil && t.Add(-time.Minute).Before(since) {
		since = t.Add(-time.Minute)
	}
	for _, slug := range s.cfg.Gate.Slugs(dir, since) {
		st, err := s.cfg.Gate.State(ctx, dir, slug, false)
		if err == nil && st.Task != nil && st.Task.Hub == card.ID {
			r.Slug = slug
			return st
		}
	}
	return nil
}

// mirror copies the gate's decision records into the store, keyed by the gate's ids.
func (s *Service) mirror(card store.Card, r store.Run, st *gate.State) {
	for _, p := range st.PendingDecisions {
		_, _ = s.st.UpsertDecision(store.Decision{ID: p.ID, CardID: card.ID, RunID: r.ID, Slug: st.Slug, Kind: p.Kind,
			Checkpoint: string(p.Checkpoint), Reviewer: p.Reviewer, Text: p.Text, Options: p.Options, Status: store.DecisionPending, AskedAt: p.AskedAt})
	}
	for _, d := range st.Decisions {
		if d.Status != store.DecisionAnswered {
			continue
		}
		_, _ = s.st.UpsertDecision(store.Decision{ID: d.ID, CardID: card.ID, RunID: r.ID, Slug: st.Slug, Kind: d.Kind,
			Checkpoint: string(d.Checkpoint), Reviewer: d.Reviewer, Options: d.Options, Status: store.DecisionAnswered,
			Choice: d.Choice, Note: d.Note, Actor: d.Actor, AskedAt: d.AskedAt, AnsweredAt: d.AnsweredAt})
	}
}

func (s *Service) apply(r store.Run, card store.Card, out Outcome, tool, final string, exit int) {
	switch out.Verdict {
	case VerdictRelocate:
		r.Worktree, r.Slug = out.Relocate.Worktree, out.Relocate.Slug
		r.Relocations = append(r.Relocations, out.Relocate.Slug)
		r.Phase, r.SessionID = store.PhaseRelocated, "" // a session belongs to its folder
		r.Status, r.NotBefore = store.StatusQueued, s.stamp()
		s.save(r)
		s.comment(card, r, "The build moved into its worktree "+r.Worktree+" ("+r.Slug+"); the next round starts there.")
	case VerdictRelocateLoop:
		s.fail(r, card, out.StopReason, "The run asked to move into the worktree of "+out.Relocate.Slug+" a second time and was stopped (relocate loop). Check that the session in the worktree has the gate hooks active (build-gate ping).")
	case VerdictRolledBack:
		s.fail(r, card, out.StopReason, fmt.Sprintf("The gate state of %s was rolled back or altered. Re-approve in your own terminal (build-gate approve %s); the hub answers nothing for this build until then.", r.Slug, r.Slug))
	case VerdictHalted:
		where := ""
		if st := decodeState(r.State); st != nil && st.Halted != nil && st.Halted.Checkpoint != "" {
			where = " at checkpoint " + string(st.Halted.Checkpoint)
		}
		s.fail(r, card, out.StopReason, "The build halted"+where+": "+out.StopReason+". The last findings are in the plan; fix it, then `tq hub run "+card.ID+"` resumes.")
	case VerdictWaiting:
		r.Status = store.StatusWaiting
		s.save(r)
		n, result := 0, false
		if st := decodeState(r.State); st != nil {
			n = len(st.PendingDecisions)
			for _, p := range st.PendingDecisions {
				result = result || p.Kind == store.KindResult
			}
		}
		_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunWait, CardID: card.ID, RunID: r.ID, Data: map[string]any{"decisions": n}})
		if result {
			s.moveToKind(card, store.LaneReview)
		}
		s.notify("tq hub: waiting on you", card.Title)
	case VerdictFinished:
		s.end(r, card, store.StatusFinished, "")
		s.moveToKind(card, store.LaneReview)
	case VerdictPermission:
		_, _ = s.st.AskHub(card.ID, r.ID, r.Slug, "The run was refused the tool "+tool+". Allow it for one round, or stop?",
			[]string{"allow-once", "stop"}, map[string]any{"tool": tool})
		r.Status = store.StatusWaiting
		s.save(r)
		s.notify("tq hub: permission needed", card.Title+": "+tool)
	case VerdictMaxTurns:
		r.Status, r.NotBefore = store.StatusQueued, s.stamp()
		s.save(r)
	case VerdictRetry:
		if r.Budget.Rounds > 0 && r.Rounds >= r.Budget.Rounds {
			s.fail(r, card, fmt.Sprintf("exit %d", exit), fmt.Sprintf("The run failed (exit %d) and has no rounds left.\n\nLast log lines:\n%s", exit, tailLines(r.LogPath, 20)))
			return
		}
		r.Retried++
		r.Status, r.NotBefore = store.StatusQueued, s.cfg.Now().Add(s.cfg.RetryDelay).UTC().Format(time.RFC3339)
		s.save(r)
		s.comment(card, r, fmt.Sprintf("Round %d exited with code %d and the gate state did not change; retrying once in %s.\n\nLast log lines:\n%s",
			r.Rounds, exit, s.cfg.RetryDelay, tailLines(r.LogPath, 20)))
	case VerdictExitFailed:
		s.fail(r, card, out.StopReason, fmt.Sprintf("The run failed again (exit %d).\n\nLast log lines:\n%s", exit, tailLines(r.LogPath, 20)))
	default:
		s.fail(r, card, out.StopReason, "The run ended without a stop.\n\n"+tailText(final, 2000))
	}
}

func (s *Service) overBudget(r store.Run, card store.Card, limit string) {
	b := r.Budget
	s.end(r, card, store.StatusOverBudget, "budget: "+limit)
	_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbBudgetStop, CardID: card.ID, RunID: r.ID, Data: map[string]any{"limit": limit}})
	s.comment(card, r, fmt.Sprintf("Budget reached (%s): tokens %d of %d, $%.2f of $%.2f, %.0f of %.0f minutes, %d of %d rounds. No further round was started.",
		limit, r.Usage.Tokens(), b.Tokens, r.CostUSD, b.USD, r.Seconds/60, b.Minutes, r.Rounds, b.Rounds))
	_, _ = s.st.AskHub(card.ID, r.ID, r.Slug, fmt.Sprintf("The run stopped at its %s budget. Raise the budget and resume (a note like tokens=4000000,usd=20 sets the new limits; an empty note repeats the same budget), or stop?", limit),
		[]string{"raise-and-resume", "stop"}, map[string]any{"limit": limit, "budget": b.String()})
}

func (s *Service) end(r store.Run, card store.Card, status, reason string) {
	r.Status, r.StopReason, r.EndedAt = status, reason, s.stamp()
	s.save(r)
	if r.CardID != "" {
		_ = s.st.Release(r.CardID, r.ID)
	}
	_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunEnd, CardID: r.CardID, RunID: r.ID,
		Data: map[string]any{"status": status, "cost_usd": r.CostUSD, "tokens": r.Usage.Tokens(), "rounds": r.Rounds, "kind": r.Kind}})
	if status == store.StatusFailed || status == store.StatusOverBudget {
		s.notify("tq hub: run "+status, card.Title+": "+reason)
	}
}

func (s *Service) fail(r store.Run, card store.Card, reason, comment string) {
	s.end(r, card, store.StatusFailed, reason)
	if comment != "" {
		s.comment(card, r, comment)
	}
}

func (s *Service) comment(card store.Card, r store.Run, text string) {
	if card.ID != "" {
		_, _ = s.st.AddComment(card.ID, store.AuthorSystem, "hub", text, r.ID)
	}
}

func (s *Service) save(r store.Run) { _ = s.st.SaveRun(r) }

func (s *Service) notify(title, body string) {
	if s.cfg.Notify != nil && s.st.Notify() {
		_ = s.cfg.Notify(context.Background(), title, body)
	}
}

// moveToKind moves the card to the first lane of kind (a system move: it triggers nothing).
func (s *Service) moveToKind(card store.Card, kind string) {
	lane, err := s.st.FirstLane(kind)
	if err != nil {
		return
	}
	if cur, err := s.st.Card(card.ID); err == nil {
		if l, err := s.st.Lane(cur.Lane); err == nil && l.Kind == kind {
			return
		}
	}
	_, _ = s.st.MoveCard(card.ID, lane.Name, "hub")
}

func tailLines(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return "(no log)"
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}

func tailText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// RecoverStale fails runs left "running" by a hub that stopped (no live round in this process) and
// releases card locks held by runs that ended or no longer exist.
func (s *Service) RecoverStale() error {
	runs, err := s.st.RunsWithStatus(store.StatusRunning)
	if err != nil {
		return err
	}
	for _, r := range runs {
		s.mu.Lock()
		_, live := s.flights[r.ID]
		s.mu.Unlock()
		if live {
			continue
		}
		card, _ := s.st.Card(r.CardID)
		s.fail(r, card, "the hub stopped during this round", "The hub stopped while this run's round was in progress. The gate state is kept; `tq hub run "+r.CardID+"` resumes it.")
	}
	locks, err := s.st.Locks()
	if err != nil {
		return err
	}
	for _, l := range locks {
		if !slices.Contains(store.ActiveStatuses, l.RunStatus) {
			_ = s.st.ForceRelease(l.CardID)
		}
	}
	return nil
}

func (s *Service) Start(ctx context.Context) {
	_ = s.RecoverStale()
	_ = s.Tick(ctx)
	t := time.NewTicker(s.cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for _, f := range s.flights {
				f.cancel()
			}
			s.mu.Unlock()
			s.wg.Wait()
			return
		case <-t.C:
			_ = s.Tick(ctx)
		}
	}
}

func (s *Service) Wait() { s.wg.Wait() }

// Drain runs every due run to its next stop, then returns (tq hub serve --drain).
func (s *Service) Drain(ctx context.Context) error {
	_ = s.RecoverStale()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.Tick(ctx); err != nil {
			return err
		}
		s.Wait()
		due, err := s.st.DueRuns(s.cfg.Now(), 1)
		if err != nil {
			return err
		}
		if len(due) == 0 {
			return nil
		}
	}
}

// Cancel stops a run: a live round is terminated (signal, 10 s grace, kill); a queued, waiting
// or stale run is ended. The gate state stays as it is.
func (s *Service) Cancel(ctx context.Context, runID string) (store.Run, error) {
	r, err := s.st.Run(runID)
	if err != nil {
		return r, err
	}
	if !r.Active() {
		return r, fmt.Errorf("run %s is already %s", runID, r.Status)
	}
	s.mu.Lock()
	f, live := s.flights[runID]
	s.mu.Unlock()
	if live {
		f.cancel()
		select {
		case <-f.done:
		case <-ctx.Done():
			return r, ctx.Err()
		case <-time.After(adapter.DefaultGrace + 15*time.Second):
			return r, fmt.Errorf("run %s did not stop in time", runID)
		}
		return s.st.Run(runID)
	}
	card, _ := s.st.Card(r.CardID)
	reason := "cancelled by human"
	if r.Status == store.StatusRunning {
		reason = "cancelled by human (stale: no hub was running this round)"
	}
	s.end(r, card, store.StatusCancelled, reason)
	s.comment(card, r, "The run was cancelled. The gate state stays as it is; `tq hub run "+r.CardID+"` resumes from it.")
	return s.st.Run(runID)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/hub/runner/; go test ./internal/hub/runner/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hub/runner/service.go internal/hub/runner/prompt.go internal/hub/runner/rounds.go internal/hub/runner/harness_test.go internal/hub/runner/prompt_test.go internal/hub/runner/rounds_test.go internal/hub/runner/budget_test.go
git commit -m "feat(hub): runner queue, checkout, rounds, budgets, retry, relocate hand-off and cancel"
```

---
### Task 9: Runner human actions: answers, terminal answers, review actions, moves

**Files:**
- Create: `internal/hub/runner/human.go`
- Modify: `internal/hub/runner/rounds.go` (`Tick` polls waiting runs first)
- Test: `internal/hub/runner/answer_test.go`, `internal/hub/runner/move_test.go`, `internal/hub/runner/poll_test.go`

**Interfaces:**
- Consumes: Task 8 (`Service`, `Enqueue`, `Cancel`, `mirror`, `end`, `save`, `stamp`, `moveToKind`, `comment`, `decodeState`, `human`, `IsAgent`, `ErrHumanOnly`, `RunActiveError`, `AnswerOutcome`, `MoveOutcome`), Task 6 (`gate.IsNotPending`), Task 4 (`AskHub`, `AnswerHub`, `UpsertDecision`).
- Produces (package `runner`):
  - `(*Service).Answer(ctx, cardID, decisionID, choice, note string) (AnswerOutcome, error)`: human-only; refuses a rolled-back state, an unknown slug or a choice outside the record's options; calls only `gate.API.Answer`; a gate that says "no pending decision" means the terminal answered first: the hub re-reads, mirrors (actor `terminal`), returns `AlreadyAnswered` and enqueues one resume; afterwards enqueues a resume unless the answer ends the run (`stop`, or `result` + `approve`, which finishes the run, moves the card to `done`, comments on the parent and calls `OnApprove`).
  - Hub-owned answers: `allow-once` (the next round gets `--allowedTools <tool>` once), `raise-and-resume` (a new resume run with the old budget or the one in the note), `stop`.
  - `(*Service).Move(ctx, id, lane string, cancelRun bool) (MoveOutcome, error)`: entering an `active` lane with `agent-ready` enqueues; leaving `active` (or `review`, or entering `done`) with a queued run cancels it; with a running or waiting run it returns `*RunActiveError` unless `cancelRun`; a waiting run may move to `review`; an agent cannot move a card to `done`.
  - `(*Service).Approve(ctx, cardID) (AnswerOutcome, error)`, `RequestChanges(ctx, cardID, note string) (AnswerOutcome, error)`, `MarkDone(ctx, cardID) (store.Card, error)`, `PendingResult(cardID) (store.Decision, bool)`.
  - `PRLink(st *store.Store, cardID string) string` (newest `…/pull/N` URL in the card's comments).
  - `var _ Ops = (*Service)(nil)`.

- [ ] **Step 1: Write the failing tests**

`internal/hub/runner/answer_test.go`:

```go
package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// waitingRun leaves a card with a run waiting on the decision in states/<fixture>.json (slug demo).
func waitingRun(t *testing.T, h *harness, kind string) (store.Card, store.Run) {
	t.Helper()
	if h.ad.steps == nil {
		h.ad.steps = []step{result("Waiting for you.")} // session s1
	}
	c := h.card("feature", store.TagAgentReady)
	st := loadState(t, "waiting")
	if kind != "" {
		st.PendingDecisions[0].Kind = kind
		if kind == store.KindResult {
			st.PendingDecisions[0].Options = []string{"approve", "request-changes", "stop"}
		}
	}
	h.gt.set("demo", st)
	r := h.enqueue(c)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.tick()
	if got := h.run(r.ID); got.Status != store.StatusWaiting {
		t.Fatalf("setup: %+v", got)
	}
	return c, h.run(r.ID)
}

func TestAnswerResumesTheRun(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	out, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "approve", "looks right")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.gt.answers) != 1 || h.gt.answers[0] != "demo d1a2b3c4 approve hub:tester" {
		t.Fatalf("gate answers %v", h.gt.answers)
	}
	if out.Decision.Status != store.DecisionAnswered || out.Decision.Actor != "hub:tester" || out.Resumed == nil || out.AlreadyAnswered {
		t.Fatalf("%+v", out)
	}
	if got := h.run(r.ID); got.Status != store.StatusQueued || got.Phase != store.PhaseResume {
		t.Fatalf("%+v", got)
	}
	h.tick()
	calls := h.ad.calls()
	last := calls[len(calls)-1]
	if last.Prompt != "/tentaqles:build resume demo --headless" || last.SessionID != "s1" {
		t.Fatalf("resume round %q %q", last.Prompt, last.SessionID)
	}
}

func TestAnswerRaceWithTerminal(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve") // the human answered in the terminal first
	out, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "approve", "")
	if err != nil {
		t.Fatalf("the hub must not error when the terminal answered first: %v", err)
	}
	if !out.AlreadyAnswered || !strings.Contains(out.Message, "already answered in the terminal") || out.Decision.Actor != "terminal" || out.Resumed == nil {
		t.Fatalf("%+v", out)
	}
	again, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "stop", "")
	if err != nil || !again.AlreadyAnswered || again.Resumed != nil {
		t.Fatalf("second click: %+v %v", again, err)
	}
	runs, _ := h.st.RunsForCard(c.ID)
	queued := 0
	for _, x := range runs {
		if x.Status == store.StatusQueued {
			queued++
		}
	}
	if len(runs) != 1 || queued != 1 || h.run(r.ID).Status != store.StatusQueued {
		t.Fatalf("exactly one resume: %d runs, %d queued", len(runs), queued)
	}
	before := len(h.ad.calls())
	h.tick()
	if len(h.ad.calls()) != before+1 {
		t.Fatal("one resumed round")
	}
}

func TestAnswerRefusals(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	if _, err := h.svc.Answer(WithAgent(context.Background()), c.ID, "d1a2b3c4", "approve", ""); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent: %v", err)
	}
	if _, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "yes please", ""); err == nil || !strings.Contains(err.Error(), "approve, edit, stop") {
		t.Fatalf("bad choice: %v", err)
	}
	rb := loadState(t, "rolled_back")
	r.State = rb.Raw
	h.st.SaveRun(r)
	if _, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "approve", ""); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("rolled back: %v", err)
	}
	if len(h.gt.answers) != 0 {
		t.Fatalf("the gate was called: %v", h.gt.answers)
	}
}

func TestApproveFinishesMovesDoneAndCommentsTheParent(t *testing.T) {
	h := newHarness(t)
	parent := h.card("project")
	h.ad.steps = nil
	child, _ := h.st.AddCard(store.NewCard{Title: "Export invoices", ParentID: parent.ID, ProjectDir: "apps/billing", Tags: []string{store.TagAgentReady}}, "renato")
	st := loadState(t, "waiting")
	st.PendingDecisions[0].Kind = store.KindResult
	st.PendingDecisions[0].Options = []string{"approve", "request-changes", "stop"}
	h.gt.set("demo", st)
	r := h.enqueue(child)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.tick()
	h.st.AddComment(child.ID, store.AuthorAgent, "agent", "PR: https://github.com/acme/billing/pull/7", r.ID)
	var approved []string
	h.svc.OnApprove = func(c store.Card, run store.Run) { approved = append(approved, c.ID+" "+run.ID) }
	if _, err := h.svc.Approve(context.Background(), child.ID); err != nil {
		t.Fatal(err)
	}
	got := h.run(r.ID)
	card, _ := h.st.Card(child.ID)
	if got.Status != store.StatusFinished || card.Lane != "done" || card.LockedByRun != "" || len(approved) != 1 {
		t.Fatalf("run %+v card %+v approved %v", got, card, approved)
	}
	if !strings.Contains(h.comments(parent.ID), "Export invoices: approved, PR https://github.com/acme/billing/pull/7") {
		t.Fatal(h.comments(parent.ID))
	}
	acts, _ := h.st.Activity(100)
	found := false
	for _, a := range acts {
		found = found || a.Verb == activity.VerbReviewApprove
	}
	if !found {
		t.Fatal("review.approve not logged")
	}
}

func TestRequestChangesCommentsAnswersAndResumes(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, store.KindResult)
	if _, err := h.svc.RequestChanges(context.Background(), c.ID, ""); err == nil {
		t.Fatal("request changes needs a note")
	}
	out, err := h.svc.RequestChanges(context.Background(), c.ID, "Use ISO dates in the file name.")
	if err != nil || out.Resumed == nil {
		t.Fatalf("%+v %v", out, err)
	}
	card, _ := h.st.Card(c.ID)
	if card.Lane != "in progress" || !strings.Contains(h.comments(c.ID), "human: Use ISO dates") || h.gt.answers[0] != "demo d1a2b3c4 request-changes hub:tester" {
		t.Fatalf("lane %q answers %v comments %s", card.Lane, h.gt.answers, h.comments(c.ID))
	}
	if h.run(r.ID).Status != store.StatusQueued {
		t.Fatal("the run resumes into the engine's request-changes path")
	}
}

func TestMarkDone(t *testing.T) {
	h := newHarness(t)
	c, _ := waitingRun(t, h, store.KindResult)
	if _, err := h.svc.MarkDone(context.Background(), c.ID); err == nil {
		t.Fatal("a pending result must be approved, not marked done")
	}
	plain := h.card("chore")
	got, err := h.svc.MarkDone(context.Background(), plain.ID)
	if err != nil || got.Lane != "done" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAllowOnceAppliesToOneRound(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{
		func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{ExitCode: 1, StopReason: "error_during_execution", CostKnown: true,
				PermissionDenials: []adapter.PermissionDenial{{Tool: "Bash", Input: map[string]any{"command": "npm test"}}}}, nil
		},
		func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{StopReason: "error_max_turns", CostKnown: true}, nil
		},
	}
	c := h.card("feature", store.TagAgentReady)
	h.enqueue(c)
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if _, err := h.svc.Answer(context.Background(), c.ID, pend[0].ID, "allow-once", ""); err != nil {
		t.Fatal(err)
	}
	h.tick()
	h.tick()
	calls := h.ad.calls()
	if len(calls) != 3 || strings.Join(calls[1].AllowedTools, ",") != "Bash(npm test)" || len(calls[2].AllowedTools) != 0 {
		t.Fatalf("allowed tools per round: %v / %v", calls[1].AllowedTools, calls[2].AllowedTools)
	}
}

func TestRaiseAndResumeStartsANewRun(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{maxTurns(h, adapter.Usage{}, 0.01, true, 0)}
	b := store.Budget{Tokens: 1e9, USD: 100, Minutes: 1000, Rounds: 1}
	c, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	first, _ := h.svc.Run(context.Background(), c.ID)
	first.Slug = "demo"
	h.st.SaveRun(first)
	h.tick()
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if len(pend) != 1 {
		t.Fatalf("over-budget decision %+v", pend)
	}
	out, err := h.svc.Answer(context.Background(), c.ID, pend[0].ID, "raise-and-resume", "rounds=5")
	if err != nil || out.Resumed == nil || out.Resumed.ID == first.ID {
		t.Fatalf("%+v %v", out, err)
	}
	nr := h.run(out.Resumed.ID)
	card, _ := h.st.Card(c.ID)
	if nr.Budget.Rounds != 5 || nr.OnRamp != store.OnRampResume || nr.Slug != "demo" || card.LockedByRun != nr.ID {
		t.Fatalf("%+v lock %q", nr, card.LockedByRun)
	}
}
```

`internal/hub/runner/move_test.go`:

```go
package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestMoveIntoActiveEnqueuesAgentReadyOnly(t *testing.T) {
	h := newHarness(t)
	ready := h.card("feature", store.TagAgentReady)
	plain := h.card("feature")
	out, err := h.svc.Move(context.Background(), ready.ID, "in progress", false)
	if err != nil || out.Enqueued == nil || out.Card.Lane != "in progress" {
		t.Fatalf("%+v %v", out, err)
	}
	out, _ = h.svc.Move(context.Background(), plain.ID, "in progress", false)
	if out.Enqueued != nil {
		t.Fatal("a card without agent-ready is not run")
	}
}

func TestMoveOutOfActiveQueuedCancels(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	in, _ := h.svc.Move(context.Background(), c.ID, "in progress", false)
	out, err := h.svc.Move(context.Background(), c.ID, "backlog", false)
	if err != nil || out.Cancelled == nil || out.Cancelled.ID != in.Enqueued.ID || out.Card.Lane != "backlog" {
		t.Fatalf("%+v %v", out, err)
	}
	if got := h.run(in.Enqueued.ID); got.Status != store.StatusCancelled {
		t.Fatalf("%+v", got)
	}
	card, _ := h.st.Card(c.ID)
	if card.LockedByRun != "" {
		t.Fatal("lock released")
	}
}

func TestMoveOutOfActiveRunningNeedsCancelRun(t *testing.T) {
	h := newHarness(t)
	started := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		close(started)
		<-ctx.Done()
		return adapter.RunResult{ExitCode: -1}, ctx.Err()
	}}
	c := h.card("feature", store.TagAgentReady)
	in, _ := h.svc.Move(context.Background(), c.ID, "in progress", false)
	h.svc.Tick(context.Background())
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("round did not start")
	}
	_, err := h.svc.Move(context.Background(), c.ID, "backlog", false)
	var active *RunActiveError
	if !errors.As(err, &active) || active.Run.ID != in.Enqueued.ID {
		t.Fatalf("want RunActiveError, got %v", err)
	}
	if card, _ := h.st.Card(c.ID); card.Lane != "in progress" {
		t.Fatalf("the card must stay put: %s", card.Lane)
	}
	out, err := h.svc.Move(context.Background(), c.ID, "backlog", true)
	if err != nil || out.Cancelled == nil || out.Cancelled.Status != store.StatusCancelled || out.Card.Lane != "backlog" {
		t.Fatalf("%+v %v", out, err)
	}
	if card, _ := h.st.Card(c.ID); card.LockedByRun != "" {
		t.Fatal("lock released")
	}
}

func TestWaitingCardMayMoveToReviewAndAgentNeverToDone(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	if _, err := h.svc.Move(context.Background(), c.ID, "review", false); err != nil {
		t.Fatalf("waiting → review is the normal flow: %v", err)
	}
	if h.run(r.ID).Status != store.StatusWaiting {
		t.Fatal("the run keeps waiting")
	}
	if _, err := h.svc.Move(WithAgent(context.Background()), c.ID, "done", true); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent → done: %v", err)
	}
}
```

`internal/hub/runner/poll_test.go`:

```go
package runner

import (
	"context"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestTerminalAnswerIsNoticedByThePoll(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve")
	h.clock.Advance(2 * time.Second)
	h.tick() // polls, mirrors, resumes, starts the round
	d, _ := h.st.Decision("d1a2b3c4")
	if d.Status != store.DecisionAnswered || d.Actor != "terminal" {
		t.Fatalf("%+v", d)
	}
	calls := h.ad.calls()
	if calls[len(calls)-1].Prompt != "/tentaqles:build resume demo --headless" {
		t.Fatalf("no resumed round: %v", calls[len(calls)-1].Prompt)
	}
	_ = c
	_ = r
}

func TestTerminalFinishIsNoticedByThePoll(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, store.KindResult)
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve") // the fake marks the state finished
	h.clock.Advance(2 * time.Second)
	if err := h.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	card, _ := h.st.Card(c.ID)
	if got := h.run(r.ID); got.Status != store.StatusFinished || card.Lane != "done" {
		t.Fatalf("run %+v lane %s", got, card.Lane)
	}
}

func TestPollSkipsRunsWaitingOnHubDecisions(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	h.st.AskHub(c.ID, r.ID, "demo", "permission?", []string{"allow-once", "stop"}, map[string]any{"tool": "Bash"})
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve")
	h.clock.Advance(2 * time.Second)
	h.tick()
	if h.run(r.ID).Status != store.StatusWaiting {
		t.Fatal("a pending hub decision keeps the run waiting")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/hub/runner/ -count=1 -run 'Test(Answer|Approve|RequestChanges|MarkDone|AllowOnce|RaiseAndResume|Move|WaitingCard|Terminal|Poll)'`
Expected: FAIL, `h.svc.Answer undefined (type *Service has no field or method Answer)`.

- [ ] **Step 3: Implement**

`internal/hub/runner/human.go`:

```go
package runner

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

var _ Ops = (*Service)(nil)

func endsRun(kind, choice string) bool {
	return choice == "stop" || (kind == store.KindResult && choice == "approve")
}

// Answer carries a human's answer to the gate (spec §9.2). The hub only ever calls build-gate answer.
func (s *Service) Answer(ctx context.Context, cardID, decisionID, choice, note string) (AnswerOutcome, error) {
	if IsAgent(ctx) {
		return AnswerOutcome{}, ErrHumanOnly
	}
	d, err := s.st.Decision(decisionID)
	if err != nil {
		return AnswerOutcome{}, err
	}
	if d.CardID != cardID {
		return AnswerOutcome{}, fmt.Errorf("decision %s belongs to card %s, not %s", decisionID, d.CardID, cardID)
	}
	if d.Status == store.DecisionAnswered {
		return AnswerOutcome{Decision: d, AlreadyAnswered: true, Message: fmt.Sprintf("decision %s was already answered: %s by %s", d.ID, d.Choice, d.Actor)}, nil
	}
	if len(d.Options) > 0 && !slices.Contains(d.Options, choice) {
		return AnswerOutcome{}, fmt.Errorf("choice must be one of %s", strings.Join(d.Options, ", "))
	}
	card, err := s.st.Card(cardID)
	if err != nil {
		return AnswerOutcome{}, err
	}
	if d.HubOwned() {
		return s.answerHub(ctx, card, d, choice, note)
	}
	r, err := s.st.Run(d.RunID)
	if err != nil {
		return AnswerOutcome{}, fmt.Errorf("decision %s has no run in this hub (%v); answer it in your terminal with build-gate answer", d.ID, err)
	}
	if st := decodeState(r.State); st != nil && st.RolledBack {
		return AnswerOutcome{}, fmt.Errorf("the gate state of %s was rolled back; re-approve in your own terminal (build-gate approve %s) before answering", d.Slug, d.Slug)
	}
	if d.Slug == "" {
		return AnswerOutcome{}, fmt.Errorf("decision %s has no build slug; answer it in your terminal with build-gate answer", d.ID)
	}
	msg, err := s.cfg.Gate.Answer(ctx, s.RunDir(r), d.Slug, d.ID, choice, note, s.cfg.Actor)
	if err != nil {
		if gate.IsNotPending(err) {
			return s.answeredElsewhere(ctx, card, r, d)
		}
		return AnswerOutcome{}, err
	}
	s.refresh(ctx, card, &r, d.Slug)
	if cur, _ := s.st.Decision(d.ID); cur.Status != store.DecisionAnswered {
		// the gate confirmed the answer; record it even if its state document lags
		_, _ = s.st.UpsertDecision(store.Decision{ID: d.ID, Status: store.DecisionAnswered, Choice: choice, Note: note, Actor: s.cfg.Actor, AnsweredAt: s.stamp()})
	}
	out := AnswerOutcome{Message: msg}
	out.Decision, _ = s.st.Decision(d.ID)
	s.afterAnswer(ctx, card, r, d.Kind, choice, &out)
	return out, nil
}

// refresh re-reads the gate state of the run and mirrors it.
func (s *Service) refresh(ctx context.Context, card store.Card, r *store.Run, slug string) *gate.State {
	if slug == "" {
		slug = r.Slug
	}
	st, err := s.cfg.Gate.State(ctx, s.RunDir(*r), slug, false)
	if err != nil {
		return nil
	}
	r.State = st.Raw
	s.save(*r)
	s.mirror(card, *r, st)
	return st
}

func (s *Service) answeredElsewhere(ctx context.Context, card store.Card, r store.Run, d store.Decision) (AnswerOutcome, error) {
	s.refresh(ctx, card, &r, d.Slug)
	cur, _ := s.st.Decision(d.ID)
	out := AnswerOutcome{Decision: cur, AlreadyAnswered: true}
	if cur.Status != store.DecisionAnswered {
		out.Message = fmt.Sprintf("the gate no longer has decision %s pending; the board was refreshed", d.ID)
		return out, nil
	}
	out.Message = fmt.Sprintf("decision %s was already answered in the terminal (%s by %s)", d.ID, cur.Choice, cur.Actor)
	s.afterAnswer(ctx, card, r, cur.Kind, cur.Choice, &out)
	return out, nil
}

func (s *Service) afterAnswer(ctx context.Context, card store.Card, r store.Run, kind, choice string, out *AnswerOutcome) {
	switch {
	case kind == store.KindResult && choice == "approve":
		s.finishApproved(card, r)
	case choice == "stop":
		if cur, err := s.st.Run(r.ID); err == nil && cur.Active() {
			s.end(cur, card, store.StatusCancelled, "stopped by human")
		}
	default:
		if kind == store.KindResult && choice == "request-changes" {
			_ = s.st.Log(activity.Entry{Actor: s.human(), Verb: activity.VerbReviewFeedback, CardID: card.ID, RunID: r.ID})
			s.moveToKind(card, store.LaneActive)
		}
		if run, created, err := s.Enqueue(ctx, card.ID, "answered"); err == nil && created {
			out.Resumed = &run
		}
	}
}

var prRe = regexp.MustCompile(`https?://[^\s<>"']+/pull/\d+`)

// PRLink is the newest pull-request URL in the card's comments (ship posts it with tq hub comment).
func PRLink(st *store.Store, cardID string) string {
	cs, err := st.Comments(cardID)
	if err != nil {
		return ""
	}
	for i := len(cs) - 1; i >= 0; i-- {
		if m := prRe.FindString(cs[i].Body); m != "" {
			return m
		}
	}
	return ""
}

// finishApproved: the gate ran finish for an approved result; close the run, move the card to done,
// tell the parent, export the review.
func (s *Service) finishApproved(card store.Card, r store.Run) {
	if cur, err := s.st.Run(r.ID); err == nil {
		r = cur
	}
	if r.Active() {
		s.end(r, card, store.StatusFinished, "")
		r, _ = s.st.Run(r.ID)
	}
	if lane, err := s.st.FirstLane(store.LaneDone); err == nil {
		_, _ = s.st.MoveCard(card.ID, lane.Name, s.human())
	}
	_ = s.st.Log(activity.Entry{Actor: s.human(), Verb: activity.VerbReviewApprove, CardID: card.ID, RunID: r.ID})
	if card.ParentID != "" {
		msg := card.Title + ": approved"
		if pr := PRLink(s.st, card.ID); pr != "" {
			msg += ", PR " + pr
		}
		_, _ = s.st.AddComment(card.ParentID, store.AuthorSystem, "hub", msg, r.ID)
	}
	if s.OnApprove != nil {
		s.OnApprove(card, r)
	}
}

func (s *Service) answerHub(ctx context.Context, card store.Card, d store.Decision, choice, note string) (AnswerOutcome, error) {
	d, err := s.st.AnswerHub(d.ID, choice, note, s.cfg.Actor)
	if err != nil {
		return AnswerOutcome{}, err
	}
	out := AnswerOutcome{Decision: d, Message: fmt.Sprintf("decision %s: %s by %s", d.ID, choice, s.cfg.Actor)}
	r, err := s.st.Run(d.RunID)
	if err != nil {
		return out, nil
	}
	switch choice {
	case "allow-once":
		tool, _ := d.Data["tool"].(string)
		if r.Active() && tool != "" {
			r.AllowedTools = []string{tool}
			r.Status, r.NotBefore = store.StatusQueued, s.stamp()
			s.save(r)
			out.Resumed = &r
		}
	case "raise-and-resume":
		b := r.Budget
		if strings.TrimSpace(note) != "" {
			nb, err := store.ParseBudget(note, r.Budget)
			if err != nil {
				return out, fmt.Errorf("the decision is answered, but the note is not a budget (%v); run tq hub run %s to resume with the same budget", err, card.ID)
			}
			b = nb
		}
		phase := store.PhaseStart
		if r.Slug != "" {
			phase = store.PhaseResume
		}
		now := s.stamp()
		nr, err := s.st.CreateRun(store.Run{CardID: card.ID, Adapter: r.Adapter, OnRamp: store.OnRampResume, ProjectDir: r.ProjectDir,
			Worktree: r.Worktree, Slug: r.Slug, SessionID: r.SessionID, Phase: phase, Budget: b, Kind: r.Kind, QueuedAt: now, NotBefore: now})
		if err != nil {
			return out, err
		}
		ok, err := s.st.Checkout(card.ID, nr.ID)
		if err != nil || !ok {
			s.end(nr, card, store.StatusCancelled, "card locked by another run")
			return out, ErrLocked
		}
		out.Resumed = &nr
	case "stop":
		if r.Active() {
			s.end(r, card, store.StatusCancelled, "stopped by human")
		}
	}
	return out, nil
}

// Move is a human (or agent) moving a card; system moves inside the runner use moveToKind.
func (s *Service) Move(ctx context.Context, id, laneName string, cancelRun bool) (MoveOutcome, error) {
	card, err := s.st.Card(id)
	if err != nil {
		return MoveOutcome{}, err
	}
	to, err := s.st.Lane(laneName)
	if err != nil {
		return MoveOutcome{}, err
	}
	if to.Kind == store.LaneDone && IsAgent(ctx) {
		return MoveOutcome{}, ErrHumanOnly
	}
	from, err := s.st.Lane(card.Lane)
	if err != nil {
		return MoveOutcome{}, err
	}
	out := MoveOutcome{}
	act, ok, err := s.st.ActiveRun(id)
	if err != nil {
		return out, err
	}
	if ok && to.Kind != store.LaneActive && !(to.Kind == store.LaneReview && act.Status != store.StatusRunning) &&
		(from.Kind == store.LaneActive || from.Kind == store.LaneReview || to.Kind == store.LaneDone) {
		if act.Status != store.StatusQueued && !cancelRun {
			return MoveOutcome{}, &RunActiveError{Run: act}
		}
		c, err := s.Cancel(ctx, act.ID)
		if err != nil {
			return out, err
		}
		out.Cancelled = &c
	}
	actor := s.human()
	if IsAgent(ctx) {
		actor = "agent"
	}
	res, err := s.st.MoveCard(id, to.Name, actor)
	if err != nil {
		return out, err
	}
	out.Card = res.Card
	if res.EnteredActive() && res.Card.HasTag(store.TagAgentReady) {
		run, created, err := s.Enqueue(ctx, id, "moved to "+to.Name)
		switch {
		case err == nil && created:
			out.Enqueued = &run
		case err != nil && !errors.Is(err, ErrLocked):
			return out, err
		}
	}
	return out, nil
}

// PendingResult is the card's pending result decision, the one the review page answers.
func (s *Service) PendingResult(cardID string) (store.Decision, bool) {
	ds, err := s.st.DecisionsForCard(cardID)
	if err != nil {
		return store.Decision{}, false
	}
	for _, d := range ds {
		if d.Kind == store.KindResult && d.Status == store.DecisionPending {
			return d, true
		}
	}
	return store.Decision{}, false
}

func (s *Service) Approve(ctx context.Context, cardID string) (AnswerOutcome, error) {
	if IsAgent(ctx) {
		return AnswerOutcome{}, ErrHumanOnly
	}
	d, ok := s.PendingResult(cardID)
	if !ok {
		return AnswerOutcome{}, fmt.Errorf("card %s has no pending result decision; use Mark done", cardID)
	}
	return s.Answer(ctx, cardID, d.ID, "approve", "")
}

func (s *Service) RequestChanges(ctx context.Context, cardID, note string) (AnswerOutcome, error) {
	if IsAgent(ctx) {
		return AnswerOutcome{}, ErrHumanOnly
	}
	if strings.TrimSpace(note) == "" {
		return AnswerOutcome{}, errors.New("say what to change: the note goes to the engine's request-changes path")
	}
	d, ok := s.PendingResult(cardID)
	if !ok {
		return AnswerOutcome{}, fmt.Errorf("card %s has no pending result decision", cardID)
	}
	if _, err := s.st.AddComment(cardID, store.AuthorHuman, s.human(), note, d.RunID); err != nil {
		return AnswerOutcome{}, err
	}
	return s.Answer(ctx, cardID, d.ID, "request-changes", note)
}

// MarkDone is the board action for a card with no pending result (no gate call).
func (s *Service) MarkDone(ctx context.Context, cardID string) (store.Card, error) {
	if IsAgent(ctx) {
		return store.Card{}, ErrHumanOnly
	}
	if _, ok := s.PendingResult(cardID); ok {
		return store.Card{}, fmt.Errorf("card %s has a pending result: approve it or request changes on the review page", cardID)
	}
	if act, ok, _ := s.st.ActiveRun(cardID); ok {
		return store.Card{}, &RunActiveError{Run: act}
	}
	lane, err := s.st.FirstLane(store.LaneDone)
	if err != nil {
		return store.Card{}, err
	}
	res, err := s.st.MoveCard(cardID, lane.Name, s.human())
	return res.Card, err
}

// pollWaiting notices answers given in the terminal (spec §9.2): waiting runs re-read the gate
// at most every PollEvery; nothing pending and nothing hub-owned pending means resume or close.
func (s *Service) pollWaiting(ctx context.Context) {
	runs, err := s.st.RunsWithStatus(store.StatusWaiting)
	if err != nil {
		return
	}
	now := s.cfg.Now()
	for _, r := range runs {
		if r.Slug == "" {
			continue
		}
		s.mu.Lock()
		last, seen := s.lastPoll[r.ID]
		due := !seen || now.Sub(last) >= s.cfg.PollEvery
		if due {
			s.lastPoll[r.ID] = now
		}
		s.mu.Unlock()
		if !due || s.hubPending(r) {
			continue
		}
		card, err := s.st.Card(r.CardID)
		if err != nil {
			continue
		}
		st := s.refresh(ctx, card, &r, r.Slug)
		if st == nil || len(st.PendingDecisions) > 0 {
			continue
		}
		if st.Finished {
			s.finishApproved(card, r)
			continue
		}
		if last := lastAnswered(st); last != nil && last.Choice == "stop" {
			s.end(r, card, store.StatusCancelled, "stopped by human in the terminal")
			continue
		}
		_, _, _ = s.Enqueue(ctx, card.ID, "answered in the terminal")
	}
}

func (s *Service) hubPending(r store.Run) bool {
	ds, err := s.st.DecisionsForCard(r.CardID)
	if err != nil {
		return false
	}
	for _, d := range ds {
		if d.HubOwned() && d.Status == store.DecisionPending && d.RunID == r.ID {
			return true
		}
	}
	return false
}

func lastAnswered(st *gate.State) *gate.Decision {
	var best *gate.Decision
	for i := range st.Decisions {
		d := &st.Decisions[i]
		if d.Status == store.DecisionAnswered && (best == nil || d.AnsweredAt >= best.AnsweredAt) {
			best = d
		}
	}
	return best
}
```

In `internal/hub/runner/rounds.go`, change the start of `Tick`:

```go
func (s *Service) Tick(ctx context.Context) error {
	s.pollWaiting(ctx)
	limit := s.st.Concurrency()
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/hub/runner/; go test ./internal/hub/runner/ -count=1`
Expected: PASS (all Task 7, 8 and 9 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/hub/runner/human.go internal/hub/runner/rounds.go internal/hub/runner/answer_test.go internal/hub/runner/move_test.go internal/hub/runner/poll_test.go
git commit -m "feat(hub): human answers through the gate, terminal-answer polling, review actions and guarded moves"
```

---
### Task 10: Web core: serve records, pages, JSON API, auth and origin checks, SSE, markdown, client

**Files:**
- Create: `internal/hub/serveinfo/serveinfo.go`
- Create: `internal/hub/web/server.go`, `internal/hub/web/auth.go`, `internal/hub/web/pages.go`, `internal/hub/web/api.go`, `internal/hub/web/events.go`, `internal/hub/web/markdown.go`, `internal/hub/web/client.go`
- Create: `internal/hub/web/templates/layout.html`, `internal/hub/web/templates/board.html`, `internal/hub/web/templates/card.html`, `internal/hub/web/templates/waiting.html`, `internal/hub/web/static/hub.css`, `internal/hub/web/static/hub.js`
- Test: `internal/hub/serveinfo/serveinfo_test.go`, `internal/hub/web/harness_test.go`, `internal/hub/web/markdown_test.go`, `internal/hub/web/pages_test.go`, `internal/hub/web/api_test.go`, `internal/hub/web/events_test.go`, `internal/hub/web/client_test.go`

**Interfaces:**
- Consumes: `runner.Service` and its methods (Tasks 8 and 9), `runner.Ops`, `runner.WithAgent`, `runner.ErrHumanOnly`, `runner.ErrLocked`, `runner.RunActiveError`, views; `store` types and `store.ErrNotFound`, `store.Change`, `(*Store).Subscribe`, `ChangeStamp`; `paths.Home()`.
- Produces (package `serveinfo`): `type Info struct { PID int; Addr string; Port int; Mode string; Workspaces, Unavailable []string; LocalKey, StartedAt string }`; `HeaderLocal = "X-TQ-Hub-Local"`; `Path(port int) string` (`$TQ_HOME/hub/serve-<port>.json`), `Write(Info) error`, `Remove(port int) error`, `ReadAll() []Info`, `(Info).Serves(ws string) bool`, `BaseURL(Info) string`, `Ping(ctx, Info) bool` (500 ms), `Find(ctx, ws string) (Info, bool)`, `FindByPort(ctx, port int) (Info, bool)`.
- Produces (package `web`):
  - `type WorkspaceInfo struct { Name string; Available bool; Reason string }`; `type Backend interface { Workspaces() []WorkspaceInfo; Service(name string) (*runner.Service, error) }`; `ErrUnavailable`, `ErrUnknownWorkspace`; `type StaticBackend struct { Infos []WorkspaceInfo; Services map[string]*runner.Service }`.
  - `type Options struct { Backend Backend; Mode, TokenHash, LocalKey string; AllowedHosts []string; PollEvery time.Duration }`; `New(opts Options) (*Server, error)`; `(*Server).Handler() http.Handler`; `Templates() (*template.Template, error)`; `HashToken(token string) string`; header constants `HeaderToken` (`X-TQ-Hub-Token`), `HeaderAgent` (`X-TQ-Hub-Agent`); cookie name `CookieToken` (`tq_hub_token`).
  - `var extraRoutes []func(*Server)` and `(*Server).handle(pattern string, h http.HandlerFunc)`, `(*Server).service(w, r) (*runner.Service, string, bool)`, `(*Server).render(w, r, name string, p page)`, `(*Server).fail(w, r, err error)`, `(*Server).basePage(r, title, ws string) page`, `(*Server).reqCtx(r) context.Context` for Task 11's handlers.
  - Routes: pages `GET /{$}`, `GET /cards/{id}`, `GET /waiting`, `GET /events`, `GET /login`, `GET /static/`; forms `POST /cards`, `POST /cards/{id}/move`, `POST /cards/{id}/comment`, `POST /cards/{id}/run`, `POST /cards/{id}/decisions/{did}/answer`, `POST /runs/{id}/cancel`; API `GET /api/ping`, `GET|POST /api/cards`, `GET /api/cards/{id}`, `POST /api/cards/{id}/move`, `POST /api/cards/{id}/run`, `POST /api/cards/{id}/comments`, `POST /api/cards/{id}/decisions/{did}/answer`, `POST /api/runs/{id}/cancel`, `GET /api/waiting`. Every route takes `?ws=<workspace>` (default: the first available one). API errors are `{"error", "code", "run"}` with codes `human_only` (403), `run_active` (409), `locked` (409), `not_found` (404), `unavailable` (403), `bad_request` (400).
  - `RenderMarkdown(src string) template.HTML` (headings, lists, fenced and inline code, http/https/relative links; everything else escaped).
  - `type Client struct { Base, WS, LocalKey string; Agent bool; HTTP *http.Client }`, `NewClient(info serveinfo.Info, ws string, agent bool) *Client`, implements `runner.Ops`; `(*Client).Ping(ctx) error`.

- [ ] **Step 1: Write the serve records package and its test**

`internal/hub/serveinfo/serveinfo.go`:

```go
// Package serveinfo records which `tq hub serve` process serves which workspaces on which port
// ($TQ_HOME/hub/serve-<port>.json, user-only), so the CLI can talk to a running hub instead of
// writing the store behind its back.
package serveinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// HeaderLocal carries the per-process key that lets the CLI (same OS user) call the API.
const HeaderLocal = "X-TQ-Hub-Local"

type Info struct {
	PID         int      `json:"pid"`
	Addr        string   `json:"addr"` // host:port as bound
	Port        int      `json:"port"`
	Mode        string   `json:"mode"` // single | all
	Workspaces  []string `json:"workspaces"`
	Unavailable []string `json:"unavailable,omitempty"`
	LocalKey    string   `json:"local_key"`
	StartedAt   string   `json:"started_at"`
}

func (i Info) Serves(ws string) bool { return slices.Contains(i.Workspaces, ws) }

func dir() string        { return filepath.Join(paths.Home(), "hub") }
func Path(port int) string { return filepath.Join(dir(), fmt.Sprintf("serve-%d.json", port)) }

func Write(i Info) error {
	if err := os.MkdirAll(dir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(i.Port), raw, 0o600)
}

func Remove(port int) error {
	err := os.Remove(Path(port))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func ReadAll() []Info {
	ents, err := os.ReadDir(dir())
	if err != nil {
		return nil
	}
	var out []Info
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "serve-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir(), e.Name()))
		if err != nil {
			continue
		}
		var i Info
		if json.Unmarshal(raw, &i) == nil && i.Port > 0 {
			out = append(out, i)
		}
	}
	return out
}

// BaseURL dials loopback for wildcard binds, else the bound host.
func BaseURL(i Info) string {
	host, _, err := net.SplitHostPort(i.Addr)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(i.Port))
}

func Ping(ctx context.Context, i Info) bool {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL(i)+"/api/ping", nil)
	if err != nil {
		return false
	}
	req.Header.Set(HeaderLocal, i.LocalKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Find returns a live hub serving ws.
func Find(ctx context.Context, ws string) (Info, bool) {
	for _, i := range ReadAll() {
		if i.Serves(ws) && Ping(ctx, i) {
			return i, true
		}
	}
	return Info{}, false
}

func FindByPort(ctx context.Context, port int) (Info, bool) {
	raw, err := os.ReadFile(Path(port))
	if err != nil {
		return Info{}, false
	}
	var i Info
	if json.Unmarshal(raw, &i) != nil {
		return Info{}, false
	}
	return i, Ping(ctx, i)
}
```

`internal/hub/serveinfo/serveinfo_test.go`:

```go
package serveinfo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteFindRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ping" && r.Header.Get(HeaderLocal) == "k1" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := Write(Info{PID: 1, Addr: "127.0.0.1:" + u.Port(), Port: port, Mode: "single", Workspaces: []string{"acme"}, LocalKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := Find(context.Background(), "acme"); !ok || got.Port != port {
		t.Fatalf("%+v %v", got, ok)
	}
	if _, ok := Find(context.Background(), "globex"); ok {
		t.Fatal("globex is not served")
	}
	if BaseURL(Info{Addr: "0.0.0.0:7470", Port: 7470}) != "http://127.0.0.1:7470" {
		t.Fatal("wildcard binds dial loopback")
	}
	Remove(port)
	if len(ReadAll()) != 0 {
		t.Fatal("removed")
	}
}
```

- [ ] **Step 2: Write the web test harness and the failing tests**

`internal/hub/web/harness_test.go`:

```go
package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

type webGate struct {
	mu      sync.Mutex
	states  map[string]*gate.State
	answers []string
}

func (g *webGate) State(_ context.Context, _, slug string, _ bool) (*gate.State, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.states[slug]
	if !ok {
		return nil, &gate.GateError{Code: 1, Msg: "no plan at docs/checkpoints/" + slug + ".json"}
	}
	cp := *st
	cp.Raw, _ = json.Marshal(st)
	return &cp, nil
}

func (g *webGate) Answer(_ context.Context, _, slug, id, choice, note, actor string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answers = append(g.answers, slug+" "+id+" "+choice+" "+actor)
	st := g.states[slug]
	var keep []gate.Decision
	found := false
	for _, p := range st.PendingDecisions {
		if p.ID == id {
			found = true
			st.Decisions = append(st.Decisions, gate.Decision{ID: id, Kind: p.Kind, Status: "answered", Choice: choice, Note: note, Actor: actor, AnsweredAt: "2026-10-21T12:00:00+00:00"})
			st.Finished = st.Finished || (p.Kind == "result" && choice == "approve")
			continue
		}
		keep = append(keep, p)
	}
	st.PendingDecisions = keep
	if !found {
		return "", &gate.GateError{Code: 1, Msg: "no pending decision '" + id + "'"}
	}
	return "decision " + id + ": " + choice + " by " + actor, nil
}

func (g *webGate) Slugs(string, time.Time) []string { return nil }

type nopAdapter struct{}

func (nopAdapter) Start(context.Context, adapter.RunRequest, io.Writer) (adapter.RunResult, error) {
	return adapter.RunResult{StopReason: "success", CostKnown: true}, nil
}

type env struct {
	t    *testing.T
	svcs map[string]*runner.Service
	gate *webGate
	root string
}

func newEnv(t *testing.T, workspaces ...string) *env {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	e := &env{t: t, svcs: map[string]*runner.Service{}, gate: &webGate{states: map[string]*gate.State{}}, root: testutil.TempDir(t)}
	for _, ws := range workspaces {
		st, err := store.Open(ws, []string{"all"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		svc, err := runner.New(runner.Config{Workspace: ws, Root: e.root, Store: st, Gate: e.gate,
			Adapters: map[string]adapter.Adapter{store.AdapterClaude: nopAdapter{}}, Actor: "hub:tester"})
		if err != nil {
			t.Fatal(err)
		}
		e.svcs[ws] = svc
	}
	return e
}

func (e *env) backend() StaticBackend {
	b := StaticBackend{Services: e.svcs}
	for ws := range e.svcs {
		b.Infos = append(b.Infos, WorkspaceInfo{Name: ws, Available: true})
	}
	sortInfos(b.Infos)
	return b
}

func (e *env) server(opts Options) *httptest.Server {
	e.t.Helper()
	if opts.Backend == nil {
		opts.Backend = e.backend()
	}
	s, err := New(opts)
	if err != nil {
		e.t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	e.t.Cleanup(ts.Close)
	return ts
}

// pendingDecision puts a gate decision on a card the way a round would have mirrored it.
func (e *env) pendingDecision(ws string, c store.Card, kind string) (store.Run, store.Decision) {
	e.t.Helper()
	svc := e.svcs[ws]
	r, _, err := svc.Enqueue(context.Background(), c.ID, "test")
	if err != nil {
		e.t.Fatal(err)
	}
	opts := []string{"approve", "edit", "stop"}
	if kind == store.KindResult {
		opts = []string{"approve", "request-changes", "stop"}
	}
	st := &gate.State{Slug: "demo", Workflow: "feature", PendingDecisions: []gate.Decision{{ID: "d1a2b3c4", Kind: kind, Text: "Approve the plan?", Options: opts}}}
	e.gate.mu.Lock()
	e.gate.states["demo"] = st
	e.gate.mu.Unlock()
	raw, _ := json.Marshal(st)
	r.Slug, r.Status, r.State = "demo", store.StatusWaiting, raw
	svc.Store().SaveRun(r)
	d := store.Decision{ID: "d1a2b3c4", CardID: c.ID, RunID: r.ID, Slug: "demo", Kind: kind, Text: "Approve the plan?", Options: opts, Status: store.DecisionPending}
	if _, err := svc.Store().UpsertDecision(d); err != nil {
		e.t.Fatal(err)
	}
	return r, d
}

func get(t *testing.T, ts *httptest.Server, path string, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func postForm(t *testing.T, ts *httptest.Server, path string, form url.Values, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func origin(ts *httptest.Server) map[string]string { return map[string]string{"Origin": ts.URL} }
```

`internal/hub/web/markdown_test.go`:

```go
package web

import (
	"strings"
	"testing"
)

func TestMarkdownSubset(t *testing.T) {
	got := string(RenderMarkdown("# Goal\n\nShip **it** today.\n\n- one\n- two `code`\n\n1. first\n\n```\nx < y\n```\n\nSee [docs](https://example.com/a?b=1&c=2) and [local](docs/prd.md)."))
	for _, want := range []string{"<h3>Goal</h3>", "<li>one</li>", "<li>two <code>code</code></li>", "<ol>", "<pre><code>x &lt; y\n</code></pre>",
		`<a href="https://example.com/a?b=1&amp;c=2" rel="noopener noreferrer">docs</a>`, `<a href="docs/prd.md" rel="noopener noreferrer">local</a>`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
}

func TestMarkdownIsSafe(t *testing.T) {
	cases := []string{
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		"[click](javascript:alert(1))",
		"[click](JaVaScRiPt:alert(1))",
		"[click](data:text/html;base64,PHNjcmlwdD4=)",
		"[x](//evil.example/a)",
		"\" onmouseover=\"alert(1)",
	}
	for _, c := range cases {
		got := string(RenderMarkdown(c))
		if strings.Contains(got, "<script") || strings.Contains(got, "<img") || strings.Contains(got, "javascript:") ||
			strings.Contains(strings.ToLower(got), "href=\"javascript") || strings.Contains(got, "data:text") && strings.Contains(got, "href") ||
			strings.Contains(got, `href="//`) || strings.Contains(got, `" onmouseover="`) {
			t.Fatalf("%q rendered unsafely: %s", c, got)
		}
	}
}
```

`internal/hub/web/pages_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestBoardShowsLanesCardsAndBadges(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Export invoices", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, want := range []string{"in progress", "Export invoices", "waiting on you", "st-waiting", `data-card="` + c.ID + `"`, "/static/hub.js"} {
		if !strings.Contains(body, want) {
			t.Fatalf("board lacks %q", want)
		}
	}
	_, frag, _ := get(t, ts, "/?ws=acme&fragment=main", nil)
	if strings.Contains(frag, "<html") || !strings.Contains(frag, "Export invoices") {
		t.Fatal("fragment=main renders the main section only")
	}
}

func TestTemplatesEscapeCardText(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "<script>alert(1)</script>", Body: "<b>bold</b>"}, nil)
	ts := e.server(Options{})
	for _, path := range []string{"/?ws=acme", "/cards/" + c.ID + "?ws=acme"} {
		_, body, _ := get(t, ts, path, nil)
		if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, "<b>bold</b>") || !strings.Contains(body, "&lt;script&gt;") {
			t.Fatalf("%s is not escaped", path)
		}
	}
}

func TestCardPageHasAnswerForms(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	for _, want := range []string{"Waiting on you", `/cards/` + c.ID + `/decisions/d1a2b3c4/answer?ws=acme`, `value="approve"`, `value="stop"`, "Approve the plan?"} {
		if !strings.Contains(body, want) {
			t.Fatalf("card page lacks %q", want)
		}
	}
	code, _, hdr := postForm(t, ts, "/cards/"+c.ID+"/decisions/d1a2b3c4/answer?ws=acme", url.Values{"choice": {"approve"}, "note": {"ok"}}, origin(ts))
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), c.ID) || len(e.gate.answers) != 1 {
		t.Fatalf("%d %v %v", code, hdr, e.gate.answers)
	}
}

func TestWaitingCombinesWorkspaces(t *testing.T) {
	e := newEnv(t, "acme", "globex")
	a, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "acme card", Tags: []string{store.TagAgentReady}}, nil)
	g, _ := e.svcs["globex"].Add(context.Background(), store.NewCard{Title: "globex card", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", a, store.KindPlanApproval)
	e.svcs["globex"].Store().AskHub(g.ID, "", "", "Raise the budget?", []string{"raise-and-resume", "stop"}, nil)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/waiting", nil)
	for _, want := range []string{"acme card", "globex card", `class="ws-label">acme`, `class="ws-label">globex`, "Raise the budget?"} {
		if !strings.Contains(body, want) {
			t.Fatalf("waiting lacks %q:\n%s", want, body)
		}
	}
}

func TestPostNeedsSameOrigin(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	form := url.Values{"title": {"From the page"}, "kind": {"feature"}}
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", form, nil); code != http.StatusForbidden {
		t.Fatalf("no Origin: %d", code)
	}
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", form, map[string]string{"Origin": "http://evil.example"}); code != http.StatusForbidden {
		t.Fatalf("cross origin: %d", code)
	}
	code, _, hdr := postForm(t, ts, "/cards?ws=acme", form, origin(ts))
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "/cards/c-") {
		t.Fatalf("same origin: %d %v", code, hdr)
	}
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", form, map[string]string{"Sec-Fetch-Site": "same-origin"}); code != http.StatusSeeOther {
		t.Fatalf("Sec-Fetch-Site same-origin: %d", code)
	}
}

func TestHostHeaderIsChecked(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{AllowedHosts: []string{"127.0.0.1:7470"}})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Host = "evil.example:7470"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("DNS-rebinding Host must be refused: %d", resp.StatusCode)
	}
}

func TestTokenRequiredWhenConfigured(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{TokenHash: HashToken("s3cret"), LocalKey: "lk"})
	if code, _, _ := get(t, ts, "/?ws=acme", nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code, _, _ := get(t, ts, "/?ws=acme", map[string]string{HeaderToken: "s3cret"}); code != 200 {
		t.Fatalf("header token: %d", code)
	}
	if code, _, _ := get(t, ts, "/?ws=acme", map[string]string{"X-TQ-Hub-Local": "lk"}); code != 200 {
		t.Fatalf("local key: %d", code)
	}
	if code, _, _ := get(t, ts, "/login?token=wrong", nil); code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d", code)
	}
	code, _, hdr := get(t, ts, "/login?token=s3cret", nil)
	cookie := hdr.Get("Set-Cookie")
	if code != http.StatusSeeOther || !strings.Contains(cookie, CookieToken+"=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatalf("login %d %q", code, cookie)
	}
	val := strings.SplitN(strings.SplitN(cookie, ";", 2)[0], "=", 2)[1]
	if code, _, _ := get(t, ts, "/?ws=acme", map[string]string{"Cookie": CookieToken + "=" + val}); code != 200 {
		t.Fatalf("cookie: %d", code)
	}
}

func TestUnknownWorkspaceIs404(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	if code, _, _ := get(t, ts, "/?ws=nope", nil); code != http.StatusNotFound {
		t.Fatalf("%d", code)
	}
}
```

`internal/hub/web/api_test.go`:

```go
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func apiDo(t *testing.T, base, method, path string, body any, hdr map[string]string) (int, map[string]any, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TQ-Hub-Local", "lk")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	json.Unmarshal(raw, &m)
	return resp.StatusCode, m, string(raw)
}

func TestAPIAddListShow(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards?ws=acme", map[string]any{"card": store.NewCard{Title: "From the CLI", Kind: "fix"}}, nil)
	if code != 200 || m["kind"] != "fix" {
		t.Fatalf("%d %s", code, raw)
	}
	id := m["id"].(string)
	code, _, raw = apiDo(t, ts.URL, "GET", "/api/cards?ws=acme&kind=fix", nil, nil)
	if code != 200 || !strings.Contains(raw, id) {
		t.Fatalf("%d %s", code, raw)
	}
	code, m, raw = apiDo(t, ts.URL, "GET", "/api/cards/"+id+"?ws=acme", nil, nil)
	if _, has := m["comments"]; code != 200 || m["title"] != "From the CLI" || !has {
		t.Fatalf("%d %s", code, raw)
	}
	if code, m, _ := apiDo(t, ts.URL, "GET", "/api/cards/c-aaaaaaaaaaaa?ws=acme", nil, nil); code != 404 || m["code"] != "not_found" {
		t.Fatalf("%d %v", code, m)
	}
}

func TestAPIMoveRunningNeedsConfirm(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	out, _ := svc.Move(context.Background(), c.ID, "in progress", false)
	r := *out.Enqueued
	r.Status = store.StatusRunning
	svc.Store().SaveRun(r)
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/move?ws=acme", map[string]any{"lane": "backlog"}, nil)
	if code != http.StatusConflict || m["code"] != "run_active" || !strings.Contains(raw, r.ID) {
		t.Fatalf("%d %s", code, raw)
	}
	if got, _ := svc.Store().Card(c.ID); got.Lane != "in progress" {
		t.Fatal("the card stays put without confirmation")
	}
	code, _, raw = apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/move?ws=acme", map[string]any{"lane": "backlog", "cancel_run": true}, nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if got, _ := svc.Store().Run(r.ID); got.Status != store.StatusCancelled {
		t.Fatalf("%+v", got)
	}
	page := e.server(Options{})
	c2, _ := svc.Add(context.Background(), store.NewCard{Title: "y", Tags: []string{store.TagAgentReady}}, nil)
	out2, _ := svc.Move(context.Background(), c2.ID, "in progress", false)
	r2 := *out2.Enqueued
	r2.Status = store.StatusRunning
	svc.Store().SaveRun(r2)
	hdr := origin(page)
	hdr["X-TQ-Hub-Fetch"] = "1"
	code, body, _ := postForm(t, page, "/cards/"+c2.ID+"/move?ws=acme", map[string][]string{"lane": {"backlog"}}, hdr)
	if code != http.StatusConflict || !strings.Contains(body, "--cancel-run") {
		t.Fatalf("page move: %d %s", code, body)
	}
}

func TestAPIAgentHeaderRefusesHumanOnly(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{LocalKey: "lk"})
	agent := map[string]string{HeaderAgent: "1"}
	code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/decisions/d1a2b3c4/answer?ws=acme", map[string]any{"choice": "approve"}, agent)
	if code != http.StatusForbidden || m["code"] != "human_only" || len(e.gate.answers) != 0 {
		t.Fatalf("%d %v", code, m)
	}
	code, m, _ = apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/comments?ws=acme", map[string]any{"body": "PR: https://github.com/a/b/pull/1"}, agent)
	if code != 200 || m["author_kind"] != store.AuthorAgent {
		t.Fatalf("%d %v", code, m)
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/move?ws=acme", map[string]any{"lane": "done", "cancel_run": true}, agent); code != http.StatusForbidden {
		t.Fatalf("agent → done: %d %v", code, m)
	}
	_ = runner.ErrHumanOnly
}
```

`internal/hub/web/events_test.go`:

```go
package web

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func readUntil(t *testing.T, br *bufio.Reader, want string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := br.ReadString('\n')
			b.WriteString(line)
			if strings.Contains(b.String(), want) || err != nil {
				done <- b.String()
				return
			}
		}
	}()
	select {
	case s := <-done:
		if !strings.Contains(s, want) {
			t.Fatalf("stream ended without %q:\n%s", want, s)
		}
		return s
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q within 5 s", want)
	}
	return ""
}

func TestSSEDeliversStoreChanges(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{PollEvery: 100 * time.Millisecond})
	resp, err := http.Get(ts.URL + "/events?ws=acme")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatal(ct)
	}
	br := bufio.NewReader(resp.Body)
	readUntil(t, br, ": connected")
	svc.Comment(context.Background(), c.ID, "hello")
	got := readUntil(t, br, "event: change")
	if !strings.Contains(got, `"workspace":"acme"`) {
		t.Fatal(got)
	}
}

func TestSSEStreamsTheRunLog(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	os.WriteFile(r.LogPath, []byte("=== round 1\nhello from the run\n"), 0o600)
	ts := e.server(Options{PollEvery: 100 * time.Millisecond})
	resp, err := http.Get(ts.URL + "/events?ws=acme&run=" + r.ID + "&from=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := readUntil(t, bufio.NewReader(resp.Body), "hello from the run")
	if !strings.Contains(got, "event: log") {
		t.Fatal(got)
	}
}
```

`internal/hub/web/client_test.go`:

```go
package web

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestClientImplementsOpsOverTheAPI(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	info := serveinfo.Info{Addr: u.Host, Port: port, LocalKey: "lk", Workspaces: []string{"acme"}}
	cl := NewClient(info, "acme", false)
	ctx := context.Background()
	if err := cl.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := cl.Add(ctx, store.NewCard{Title: "via client", Tags: []string{store.TagAgentReady}}, nil)
	if err != nil || c.ID == "" {
		t.Fatalf("%+v %v", c, err)
	}
	out, err := cl.Move(ctx, c.ID, "in progress", false)
	if err != nil || out.Enqueued == nil {
		t.Fatalf("%+v %v", out, err)
	}
	r := *out.Enqueued
	r.Status = store.StatusRunning
	e.svcs["acme"].Store().SaveRun(r)
	_, err = cl.Move(ctx, c.ID, "backlog", false)
	var active *runner.RunActiveError
	if !errors.As(err, &active) || active.Run.ID != r.ID {
		t.Fatalf("want RunActiveError, got %v", err)
	}
	e.pendingDecision("acme", c, store.KindPlanApproval)
	agent := NewClient(info, "acme", true)
	if _, err := agent.Answer(ctx, c.ID, "d1a2b3c4", "approve", ""); !errors.Is(err, runner.ErrHumanOnly) {
		t.Fatalf("want ErrHumanOnly, got %v", err)
	}
	views, err := cl.List(ctx, store.CardFilter{}, true)
	if err != nil || len(views) != 1 || views[0].Waiting != 1 {
		t.Fatalf("%+v %v", views, err)
	}
	items, err := cl.Waiting(ctx)
	if err != nil || len(items) != 1 || items[0].Decision.ID != "d1a2b3c4" {
		t.Fatalf("%+v %v", items, err)
	}
	var _ runner.Ops = cl
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/hub/serveinfo/ ./internal/hub/web/ -count=1`
Expected: the `serveinfo` test passes (Step 1 wrote the package); `web` FAILS with `undefined: New`, `undefined: Options`, `undefined: RenderMarkdown`.

- [ ] **Step 4: Implement the server, auth, pages, API, events, markdown and client**

`internal/hub/web/server.go`:

```go
// Package web serves the hub's page and JSON API: embedded templates, plain JavaScript, SSE.
// Every screen answers what is happening, does it need me, what do I do. Every handler takes
// ?ws=<workspace>; the backend decides which workspaces exist and which are available.
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

var (
	ErrUnavailable      = errors.New("workspace unavailable")
	ErrUnknownWorkspace = errors.New("unknown workspace")
)

type WorkspaceInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Backend interface {
	Workspaces() []WorkspaceInfo
	// Service returns the runner for an available workspace. It must never open the store of an
	// unavailable one: those return ErrUnavailable.
	Service(name string) (*runner.Service, error)
}

// StaticBackend serves fixed services (tests, and a single workspace).
type StaticBackend struct {
	Infos    []WorkspaceInfo
	Services map[string]*runner.Service
}

func (b StaticBackend) Workspaces() []WorkspaceInfo { return b.Infos }

func (b StaticBackend) Service(name string) (*runner.Service, error) {
	for _, i := range b.Infos {
		if i.Name != name {
			continue
		}
		if !i.Available {
			return nil, fmt.Errorf("%w: %s is %s", ErrUnavailable, name, i.Reason)
		}
		if s := b.Services[name]; s != nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownWorkspace, name)
}

func sortInfos(xs []WorkspaceInfo) { sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name }) }

type Options struct {
	Backend      Backend
	Mode         string        // single | all (shown in /api/ping)
	TokenHash    string        // sha256 hex of the LAN token; "" on a loopback bind
	LocalKey     string        // the CLI's key (serveinfo); "" disables it
	AllowedHosts []string      // Host headers accepted (DNS-rebinding guard); empty accepts any
	PollEvery    time.Duration // SSE poll for writes by other processes; 0 → 1 s
}

type Server struct {
	opts Options
	tpl  *template.Template
	mux  *http.ServeMux
}

// extraRoutes lets later files (review, runs, activity) add their handlers.
var extraRoutes []func(*Server)

type page struct {
	Title        string
	WS           string
	RunID        string
	Mode         string
	Workspaces   []WorkspaceInfo
	WaitingCount int
	Flash        string
	Data         any
}

func kTok(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

var funcs = template.FuncMap{
	"md":      func(s string) template.HTML { return RenderMarkdown(store.StripFrontMatter(s)) },
	"money":   func(f float64) string { return fmt.Sprintf("$%.2f", f) },
	"ktok":    kTok,
	"minutes": func(sec float64) string { return fmt.Sprintf("%.0f", sec/60) },
	"when": func(ts string) string {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
		return ts
	},
	"first": func(s string) string { l, _, _ := strings.Cut(strings.TrimSpace(s), "\n"); return l },
	"join":  strings.Join,
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[fmt.Sprint(kv[i])] = kv[i+1]
		}
		return m
	},
}

// Templates parses every embedded template once; RenderReview (Task 11) uses it too.
func Templates() (*template.Template, error) {
	return template.New("hub").Funcs(funcs).ParseFS(assets, "templates/*.html")
}

func New(opts Options) (*Server, error) {
	if opts.Backend == nil {
		return nil, errors.New("web: no backend")
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = time.Second
	}
	tpl, err := Templates()
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, tpl: tpl, mux: http.NewServeMux()}
	s.routes()
	for _, add := range extraRoutes {
		add(s)
	}
	return s, nil
}

func (s *Server) handle(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

func (s *Server) Handler() http.Handler { return s.guard(s.mux) }

func (s *Server) routes() {
	static, _ := fs.Sub(assets, ".")
	s.mux.Handle("GET /static/", http.FileServerFS(static))
	s.handle("GET /login", s.login)
	s.handle("GET /{$}", s.board)
	s.handle("GET /cards/{id}", s.cardPage)
	s.handle("GET /waiting", s.waitingPage)
	s.handle("GET /events", s.events)
	s.handle("POST /cards", s.formAdd)
	s.handle("POST /cards/{id}/move", s.formMove)
	s.handle("POST /cards/{id}/comment", s.formComment)
	s.handle("POST /cards/{id}/run", s.formRun)
	s.handle("POST /cards/{id}/decisions/{did}/answer", s.formAnswer)
	s.handle("POST /runs/{id}/cancel", s.formCancel)
	s.handle("GET /api/ping", s.apiPing)
	s.handle("GET /api/cards", s.apiList)
	s.handle("POST /api/cards", s.apiAdd)
	s.handle("GET /api/cards/{id}", s.apiShow)
	s.handle("POST /api/cards/{id}/move", s.apiMove)
	s.handle("POST /api/cards/{id}/run", s.apiRun)
	s.handle("POST /api/cards/{id}/comments", s.apiComment)
	s.handle("POST /api/cards/{id}/decisions/{did}/answer", s.apiAnswer)
	s.handle("POST /api/runs/{id}/cancel", s.apiCancel)
	s.handle("GET /api/waiting", s.apiWaiting)
}

func isAPI(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/") }

// reqCtx marks API requests from an agent session (the CLI sets X-TQ-Hub-Agent: 1).
func (s *Server) reqCtx(r *http.Request) context.Context {
	if isAPI(r) && r.Header.Get(HeaderAgent) == "1" {
		return runner.WithAgent(r.Context())
	}
	return r.Context()
}

// service resolves ?ws= (default: the first available workspace) without opening an unavailable one.
func (s *Server) service(w http.ResponseWriter, r *http.Request) (*runner.Service, string, bool) {
	ws := r.URL.Query().Get("ws")
	if ws == "" {
		for _, i := range s.opts.Backend.Workspaces() {
			if i.Available {
				ws = i.Name
				break
			}
		}
	}
	if ws == "" {
		s.fail(w, r, fmt.Errorf("%w: this hub serves no trusted workspace", ErrUnknownWorkspace))
		return nil, "", false
	}
	svc, err := s.opts.Backend.Service(ws)
	if err != nil {
		s.fail(w, r, err)
		return nil, ws, false
	}
	return svc, ws, true
}

func errStatus(err error) (int, string) {
	var active *runner.RunActiveError
	switch {
	case errors.Is(err, runner.ErrHumanOnly):
		return http.StatusForbidden, "human_only"
	case errors.As(err, &active):
		return http.StatusConflict, "run_active"
	case errors.Is(err, runner.ErrLocked):
		return http.StatusConflict, "locked"
	case errors.Is(err, ErrUnavailable):
		return http.StatusForbidden, "unavailable"
	case errors.Is(err, store.ErrNotFound), errors.Is(err, ErrUnknownWorkspace):
		return http.StatusNotFound, "not_found"
	}
	return http.StatusBadRequest, "bad_request"
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code := errStatus(err)
	if isAPI(r) {
		body := map[string]any{"error": err.Error(), "code": code}
		var active *runner.RunActiveError
		if errors.As(err, &active) {
			body["run"] = active.Run
		}
		writeJSON(w, status, body)
		return
	}
	if r.Header.Get("X-TQ-Hub-Fetch") == "1" {
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(status)
	p := s.basePage(r, http.StatusText(status), r.URL.Query().Get("ws"))
	p.Data = err.Error()
	_ = s.tpl.ExecuteTemplate(w, "message", p)
}

func (s *Server) basePage(r *http.Request, title, ws string) page {
	p := page{Title: title, WS: ws, Mode: s.opts.Mode, Workspaces: s.opts.Backend.Workspaces(), Flash: r.URL.Query().Get("flash")}
	if ws != "" {
		if svc, err := s.opts.Backend.Service(ws); err == nil {
			if ds, err := svc.Store().PendingDecisions(); err == nil {
				p.WaitingCount = len(ds)
			}
		}
	}
	return p
}

// render executes name, or only name-main when ?fragment=main (the live refresh).
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.URL.Query().Get("fragment") == "main" {
		name += "-main"
	}
	if err := s.tpl.ExecuteTemplate(w, name, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("X-TQ-Hub-Fetch") == "1" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}
```

`internal/hub/web/auth.go`:

```go
package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
)

const (
	HeaderToken = "X-TQ-Hub-Token" // tq:allow-secret (a header name)
	HeaderAgent = "X-TQ-Hub-Agent"
	CookieToken = "tq_hub_token" // tq:allow-secret (a cookie name)
)

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func eq(a, b string) bool { return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (s *Server) tokenOK(r *http.Request) bool {
	if v := r.Header.Get(HeaderToken); v != "" && eq(HashToken(v), s.opts.TokenHash) {
		return true
	}
	if c, err := r.Cookie(CookieToken); err == nil && eq(HashToken(c.Value), s.opts.TokenHash) {
		return true
	}
	return false
}

func sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return o == "http://"+r.Host
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// guard: Host check (DNS rebinding), token when configured, same-origin for every write.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.opts.AllowedHosts) > 0 && !slices.Contains(s.opts.AllowedHosts, strings.ToLower(r.Host)) {
			http.Error(w, "unexpected Host header", http.StatusMisdirectedRequest)
			return
		}
		local := eq(r.Header.Get(serveinfo.HeaderLocal), s.opts.LocalKey)
		if s.opts.TokenHash != "" && !local && r.URL.Path != "/login" && !s.tokenOK(r) {
			http.Error(w, "this hub needs its token: open /login?token=<token> once in this browser, or send "+HeaderToken, http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !local && !sameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if s.opts.TokenHash == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !eq(HashToken(tok), s.opts.TokenHash) {
		http.Error(w, "wrong token", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieToken, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
```

`internal/hub/web/pages.go`:

```go
package web

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// formKinds puts the common kinds first in the new-card form.
var formKinds = []string{"feature", "fix", "hotfix", "analysis", "chore", "project"}

type laneView struct {
	Lane  store.Lane
	Cards []runner.CardView
}

type boardData struct {
	Lanes []laneView
	Kinds []string
}

func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	lanes, err := svc.Store().Lanes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views, err := svc.List(r.Context(), store.CardFilter{}, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := boardData{Kinds: formKinds}
	for _, l := range lanes {
		lv := laneView{Lane: l}
		for _, v := range views {
			if strings.EqualFold(v.Lane, l.Name) {
				lv.Cards = append(lv.Cards, v)
			}
		}
		data.Lanes = append(data.Lanes, lv)
	}
	p := s.basePage(r, "Board", ws)
	p.Data = data
	s.render(w, r, "board", p)
}

type cardData struct {
	runner.CardDetail
	Lanes       []store.Lane
	Active      *store.Run
	Pending     []store.Decision
	Answered    []store.Decision
	GateURL     string
	ReviewReady bool
}

func (s *Server) cardPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	d, err := svc.Show(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lanes, _ := svc.Store().Lanes()
	data := cardData{CardDetail: d, Lanes: lanes, GateURL: svc.GateURL()}
	for i := range d.Runs {
		if d.Runs[i].Active() {
			run := d.Runs[i]
			data.Active = &run
			break
		}
	}
	for _, dec := range d.Decisions {
		if dec.Status == store.DecisionPending {
			data.Pending = append(data.Pending, dec)
		} else {
			data.Answered = append(data.Answered, dec)
		}
	}
	if _, ok := svc.PendingResult(d.ID); ok {
		data.ReviewReady = true
	}
	for _, run := range d.Runs {
		if run.Status == store.StatusFinished {
			data.ReviewReady = true
		}
	}
	p := s.basePage(r, d.Title, ws)
	if data.Active != nil {
		p.RunID = data.Active.ID
	}
	p.Data = data
	s.render(w, r, "card", p)
}

func (s *Server) waitingItems(r *http.Request, only string) []runner.WaitingItem {
	var items []runner.WaitingItem
	for _, info := range s.opts.Backend.Workspaces() {
		if !info.Available || (only != "" && info.Name != only) {
			continue
		}
		svc, err := s.opts.Backend.Service(info.Name)
		if err != nil {
			continue
		}
		got, err := svc.Waiting(r.Context())
		if err != nil {
			continue
		}
		items = append(items, got...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Decision.AskedAt > items[j].Decision.AskedAt })
	return items
}

func (s *Server) waitingPage(w http.ResponseWriter, r *http.Request) {
	p := s.basePage(r, "Waiting on you", r.URL.Query().Get("ws"))
	p.Data = s.waitingItems(r, "")
	s.render(w, r, "waiting", p)
}

func cardURL(ws, id string) string { return "/cards/" + id + "?ws=" + url.QueryEscape(ws) }

func (s *Server) formAdd(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	nc := store.NewCard{Title: r.FormValue("title"), Body: r.FormValue("body"), Kind: r.FormValue("kind"),
		Lane: r.FormValue("lane"), ProjectDir: r.FormValue("project")}
	if r.FormValue("agent_ready") == "1" {
		nc.Tags = []string{store.TagAgentReady}
	}
	c, err := svc.Add(r.Context(), nc, nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, c.ID))
}

func (s *Server) formMove(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := svc.Move(r.Context(), id, r.FormValue("lane"), r.FormValue("cancel_run") == "1"); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, id))
}

func (s *Server) formComment(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := svc.Comment(r.Context(), id, r.FormValue("body")); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, id))
}

func (s *Server) formRun(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := svc.Run(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, id))
}

func (s *Server) formAnswer(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	out, err := svc.Answer(r.Context(), id, r.PathValue("did"), r.FormValue("choice"), r.FormValue("note"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, id)+"&flash="+url.QueryEscape(out.Message))
}

func (s *Server) formCancel(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	run, err := svc.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, run.CardID))
}
```

`internal/hub/web/api.go`:

```go
package web

import (
	"encoding/json"
	"net/http"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func (s *Server) apiPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "mode": s.opts.Mode, "workspaces": s.opts.Backend.Workspaces()})
}

func (s *Server) apiList(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	views, err := svc.List(s.reqCtx(r), store.CardFilter{Lane: q.Get("lane"), Kind: q.Get("kind"), Tag: q.Get("tag")}, q.Get("waiting") == "1")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, views)
}

type addRequest struct {
	Card   store.NewCard `json:"card"`
	Budget *store.Budget `json:"budget,omitempty"`
}

func (s *Server) apiAdd(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	var req addRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := svc.Add(s.reqCtx(r), req.Card, req.Budget)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) apiShow(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	d, err := svc.Show(s.reqCtx(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) apiMove(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	var req struct {
		Lane      string `json:"lane"`
		CancelRun bool   `json:"cancel_run"`
	}
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := svc.Move(s.reqCtx(r), r.PathValue("id"), req.Lane, req.CancelRun)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) apiRun(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	run, err := svc.Run(s.reqCtx(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, run)
}

func (s *Server) apiComment(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := svc.Comment(s.reqCtx(r), r.PathValue("id"), req.Body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) apiAnswer(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	var req struct {
		Choice string `json:"choice"`
		Note   string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := svc.Answer(s.reqCtx(r), r.PathValue("id"), r.PathValue("did"), req.Choice, req.Note)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) apiCancel(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	run, err := svc.Cancel(s.reqCtx(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, run)
}

func (s *Server) apiWaiting(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.waitingItems(r, r.URL.Query().Get("ws")))
}
```

`internal/hub/web/events.go`:

```go
package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// events is the page's one SSE stream: store changes of the served workspaces, a poll that
// catches writes by other processes (a CLI writing the store directly), and the run log tail.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	svcs := map[string]*runner.Service{}
	for _, info := range s.opts.Backend.Workspaces() {
		if !info.Available || (q.Get("ws") != "" && info.Name != q.Get("ws")) {
			continue
		}
		if svc, err := s.opts.Backend.Service(info.Name); err == nil {
			svcs[info.Name] = svc
		}
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		fl.Flush()
	}
	merged := make(chan store.Change, 64)
	stamps := map[string]string{}
	for name, svc := range svcs {
		ch, cancel := svc.Store().Subscribe()
		defer cancel()
		go func(ch <-chan store.Change) {
			for c := range ch {
				select {
				case merged <- c:
				default:
				}
			}
		}(ch)
		stamps[name], _ = svc.Store().ChangeStamp()
	}
	logPath, offset := "", int64(0)
	if runID := q.Get("run"); runID != "" {
		for _, svc := range svcs {
			if run, err := svc.Store().Run(runID); err == nil {
				logPath = run.LogPath
				break
			}
		}
		offset, _ = strconv.ParseInt(q.Get("from"), 10, 64)
	}
	poll := time.NewTicker(s.opts.PollEvery)
	defer poll.Stop()
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case c := <-merged:
			send("change", c)
		case <-poll.C:
			for name, svc := range svcs {
				if st, err := svc.Store().ChangeStamp(); err == nil && st != stamps[name] {
					stamps[name] = st
					send("change", store.Change{Workspace: name, Kind: "external"})
				}
			}
			if logPath != "" {
				if chunk, next := readFrom(logPath, offset, 64<<10); len(chunk) > 0 {
					offset = next
					send("log", map[string]any{"text": chunk, "offset": offset})
				}
			}
		case <-beat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func readFrom(path string, offset, max int64) (string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return "", offset
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() <= offset {
		return "", offset
	}
	if fi.Size()-offset > max {
		offset = fi.Size() - max
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", offset
	}
	b, _ := io.ReadAll(io.LimitReader(f, max))
	return string(b), offset + int64(len(b))
}
```

`internal/hub/web/markdown.go`:

```go
package web

import (
	"fmt"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"
)

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	ulRe      = regexp.MustCompile(`^[-*]\s+(.*)$`)
	olRe      = regexp.MustCompile(`^\d+[.)]\s+(.*)$`)
	codeRe    = regexp.MustCompile("`([^`]+)`")
	linkRe    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

// RenderMarkdown renders the small safe subset of spec §11: headings, lists, fenced and inline
// code, links to http/https or relative paths. Everything else, raw HTML included, is escaped.
func RenderMarkdown(src string) template.HTML {
	var b strings.Builder
	var para []string
	list, inCode := "", false
	flushPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	openList := func(kind string) {
		if list != kind {
			closeList()
			b.WriteString("<" + kind + ">\n")
			list = kind
		}
	}
	for _, ln := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			if inCode {
				b.WriteString("</code></pre>\n")
				inCode = false
			} else {
				flushPara()
				closeList()
				b.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(ln) + "\n")
			continue
		}
		switch {
		case t == "":
			flushPara()
			closeList()
		case headingRe.MatchString(t):
			flushPara()
			closeList()
			m := headingRe.FindStringSubmatch(t)
			level := len(m[1]) + 2 // card headings never outrank the page's own
			if level > 6 {
				level = 6
			}
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, inline(m[2]), level)
		case ulRe.MatchString(t):
			flushPara()
			openList("ul")
			b.WriteString("<li>" + inline(ulRe.FindStringSubmatch(t)[1]) + "</li>\n")
		case olRe.MatchString(t):
			flushPara()
			openList("ol")
			b.WriteString("<li>" + inline(olRe.FindStringSubmatch(t)[1]) + "</li>\n")
		default:
			closeList()
			para = append(para, t)
		}
	}
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	flushPara()
	closeList()
	return template.HTML(b.String())
}

func safeHref(raw string) (string, bool) {
	if strings.HasPrefix(raw, "//") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch u.Scheme {
	case "http", "https", "":
		return u.String(), true
	}
	return "", false
}

// inline escapes first, then re-introduces code spans and safe links on the escaped text.
func inline(s string) string {
	e := html.EscapeString(s)
	e = codeRe.ReplaceAllString(e, "<code>$1</code>")
	return linkRe.ReplaceAllStringFunc(e, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		href, ok := safeHref(html.UnescapeString(sub[2]))
		if !ok {
			return sub[1]
		}
		return `<a href="` + html.EscapeString(href) + `" rel="noopener noreferrer">` + sub[1] + `</a>`
	})
}
```

`internal/hub/web/client.go`:

```go
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// Client is runner.Ops over a running hub's API (so the page updates live).
type Client struct {
	Base, WS, LocalKey string
	Agent              bool
	HTTP               *http.Client
}

var _ runner.Ops = (*Client)(nil)

func NewClient(info serveinfo.Info, ws string, agent bool) *Client {
	return &Client{Base: serveinfo.BaseURL(info), WS: ws, LocalKey: info.LocalKey, Agent: agent, HTTP: &http.Client{}}
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) Unwrap() error {
	switch e.Code {
	case "human_only":
		return runner.ErrHumanOnly
	case "locked":
		return runner.ErrLocked
	case "not_found":
		return store.ErrNotFound
	case "unavailable":
		return ErrUnavailable
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("ws", c.WS)
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path+"?"+q.Encode(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(serveinfo.HeaderLocal, c.LocalKey)
	if c.Agent || runner.IsAgent(ctx) {
		req.Header.Set(HeaderAgent, "1")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("tq hub at %s: %w", c.Base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Error string     `json:"error"`
			Code  string     `json:"code"`
			Run   *store.Run `json:"run"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Code == "run_active" && e.Run != nil {
			return &runner.RunActiveError{Run: *e.Run}
		}
		if e.Error == "" {
			e.Error = fmt.Sprintf("tq hub answered %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
		}
		return &APIError{Status: resp.StatusCode, Code: e.Code, Message: e.Error}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error { return c.do(ctx, http.MethodGet, "/api/ping", nil, nil, nil) }

func (c *Client) Add(ctx context.Context, nc store.NewCard, budget *store.Budget) (store.Card, error) {
	var out store.Card
	err := c.do(ctx, http.MethodPost, "/api/cards", nil, addRequest{Card: nc, Budget: budget}, &out)
	return out, err
}

func (c *Client) List(ctx context.Context, f store.CardFilter, waitingOnly bool) ([]runner.CardView, error) {
	q := url.Values{}
	for k, v := range map[string]string{"lane": f.Lane, "kind": f.Kind, "tag": f.Tag} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if waitingOnly {
		q.Set("waiting", "1")
	}
	var out []runner.CardView
	err := c.do(ctx, http.MethodGet, "/api/cards", q, nil, &out)
	return out, err
}

func (c *Client) Show(ctx context.Context, id string) (runner.CardDetail, error) {
	var out runner.CardDetail
	err := c.do(ctx, http.MethodGet, "/api/cards/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

func (c *Client) Move(ctx context.Context, id, lane string, cancelRun bool) (runner.MoveOutcome, error) {
	var out runner.MoveOutcome
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(id)+"/move", nil, map[string]any{"lane": lane, "cancel_run": cancelRun}, &out)
	return out, err
}

func (c *Client) Run(ctx context.Context, id string) (store.Run, error) {
	var out store.Run
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(id)+"/run", nil, nil, &out)
	return out, err
}

func (c *Client) Answer(ctx context.Context, cardID, decisionID, choice, note string) (runner.AnswerOutcome, error) {
	var out runner.AnswerOutcome
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(cardID)+"/decisions/"+url.PathEscape(decisionID)+"/answer", nil,
		map[string]string{"choice": choice, "note": note}, &out)
	return out, err
}

func (c *Client) Cancel(ctx context.Context, runID string) (store.Run, error) {
	var out store.Run
	err := c.do(ctx, http.MethodPost, "/api/runs/"+url.PathEscape(runID)+"/cancel", nil, nil, &out)
	return out, err
}

func (c *Client) Comment(ctx context.Context, cardID, body string) (store.Comment, error) {
	var out store.Comment
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(cardID)+"/comments", nil, map[string]string{"body": body}, &out)
	return out, err
}

func (c *Client) Waiting(ctx context.Context) ([]runner.WaitingItem, error) {
	var out []runner.WaitingItem
	err := c.do(ctx, http.MethodGet, "/api/waiting", nil, nil, &out)
	return out, err
}
```

- [ ] **Step 5: Write the templates and static files**

`internal/hub/web/templates/layout.html`:

```html
{{define "head"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · tq hub</title>
<link rel="stylesheet" href="/static/hub.css">
</head>
<body data-ws="{{.WS}}" data-run="{{.RunID}}">
<header class="top">
  <a class="brand" href="/?ws={{.WS}}">tq hub</a>
  {{if gt (len .Workspaces) 1}}
  <form class="switch" method="get" action="/">
    <label>workspace
      <select name="ws" onchange="this.form.submit()">
        {{range .Workspaces}}<option value="{{.Name}}"{{if eq .Name $.WS}} selected{{end}}{{if not .Available}} disabled{{end}}>{{.Name}}{{if not .Available}} (unavailable: {{.Reason}}){{end}}</option>{{end}}
      </select>
    </label>
  </form>
  {{else}}<span class="ws">{{.WS}}</span>{{end}}
  <nav>
    <a href="/?ws={{.WS}}">Board</a>
    <a href="/waiting?ws={{.WS}}">Waiting <span class="count mono" id="waiting-count">{{.WaitingCount}}</span></a>
    <a href="/activity?ws={{.WS}}">Activity</a>
  </nav>
</header>
{{if .Flash}}<p class="flash">{{.Flash}}</p>{{end}}
<main id="main">{{end}}

{{define "foot"}}</main>
<script src="/static/hub.js"></script>
</body>
</html>{{end}}

{{define "message"}}{{template "head" .}}{{template "message-main" .}}{{template "foot" .}}{{end}}
{{define "message-main"}}<section class="message"><h1>{{.Title}}</h1><p>{{.Data}}</p></section>{{end}}

{{define "decision-form"}}{{$d := index . "item"}}
<form class="decision" method="post" action="/cards/{{index . "card"}}/decisions/{{$d.ID}}/answer?ws={{index . "ws"}}">
  <p><span class="badge">{{$d.Kind}}</span>{{if $d.Checkpoint}} checkpoint <span class="mono">{{$d.Checkpoint}}</span>{{end}}{{if $d.Reviewer}} · {{$d.Reviewer}}{{end}} <span class="mono muted">{{$d.ID}}</span>{{if $d.AskedAt}} <span class="mono muted">{{when $d.AskedAt}}</span>{{end}}</p>
  <pre class="text">{{$d.Text}}</pre>
  <label>Note <input name="note" autocomplete="off"></label>
  <div class="choices">{{range $d.Options}}<button name="choice" value="{{.}}">{{.}}</button>{{end}}</div>
</form>{{end}}
```

`internal/hub/web/templates/board.html`:

```html
{{define "board"}}{{template "head" .}}{{template "board-main" .}}{{template "foot" .}}{{end}}
{{define "board-main"}}
<section class="lanes">
{{range .Data.Lanes}}
  <div class="lane" data-lane="{{.Lane.Name}}">
    <h2>{{.Lane.Name}} <span class="mono muted">{{len .Cards}}</span></h2>
    {{range .Cards}}
    <article class="card" draggable="true" data-card="{{.ID}}">
      <a class="title" href="/cards/{{.ID}}?ws={{$.WS}}">{{.Title}}</a>
      <div class="meta">
        <span class="kind">{{.Kind}}</span>{{range .Tags}} <span class="tag">{{.}}</span>{{end}}
        {{if .Waiting}}<span class="badge st-waiting">waiting on you</span>{{end}}
        {{if .RunStatus}}<span class="badge st-{{.RunStatus}}">{{.RunStatus}}</span>{{end}}
        {{if .CostUSD}}<span class="mono">{{money .CostUSD}}</span>{{end}}
      </div>
    </article>
    {{end}}
  </div>
{{end}}
</section>
<details class="new"><summary>New card</summary>
  <form method="post" action="/cards?ws={{.WS}}">
    <label>Title <input name="title" required></label>
    <label>Body (markdown) <textarea name="body" rows="5"></textarea></label>
    <label>Kind <select name="kind">{{range .Data.Kinds}}<option>{{.}}</option>{{end}}</select></label>
    <label>Lane <select name="lane">{{range .Data.Lanes}}<option>{{.Lane.Name}}</option>{{end}}</select></label>
    <label>Project folder <input name="project" placeholder="apps/billing"></label>
    <label class="check"><input type="checkbox" name="agent_ready" value="1"> agent-ready</label>
    <button>Add card</button>
  </form>
</details>
{{end}}
```

`internal/hub/web/templates/card.html`:

```html
{{define "card"}}{{template "head" .}}{{template "card-main" .}}{{template "foot" .}}{{end}}
{{define "card-main"}}{{$ws := .WS}}{{with .Data}}
<article class="detail">
  <h1>{{.Title}}</h1>
  <p class="meta"><span class="kind">{{.Kind}}</span> in <strong>{{.Lane}}</strong>{{range .Tags}} <span class="tag">{{.}}</span>{{end}}
    {{if .ProjectDir}} · project <span class="mono">{{.ProjectDir}}</span>{{end}}
    {{if .Parent}} · parent <a href="/cards/{{.Parent.ID}}?ws={{$ws}}">{{.Parent.Title}}</a>{{end}}
    · <span class="mono">{{.ID}}</span>{{if .CostUSD}} · <span class="mono">{{money .CostUSD}}</span>{{end}}</p>
  <form class="inline" method="post" action="/cards/{{.ID}}/move?ws={{$ws}}" data-move data-card="{{.ID}}">
    <select name="lane">{{$lane := .Lane}}{{range .Lanes}}<option{{if eq .Name $lane}} selected{{end}}>{{.Name}}</option>{{end}}</select>
    <button>Move</button>
  </form>
  <form class="inline" method="post" action="/cards/{{.ID}}/run?ws={{$ws}}"><button>Run now</button></form>
  {{if .ReviewReady}}<p><a class="button primary" href="/cards/{{.ID}}/review?ws={{$ws}}">Open the review page</a></p>{{end}}
  <div class="body">{{md .Body}}</div>

  {{with .Active}}
  <section class="run">
    <h2>Run <a class="mono" href="/runs/{{.ID}}?ws={{$ws}}">{{.ID}}</a> <span class="badge st-{{.Status}}">{{.Status}}</span></h2>
    <p class="mono">rounds {{.Rounds}}/{{.Budget.Rounds}} · {{money .CostUSD}}/{{money .Budget.USD}} · {{ktok .Usage.Tokens}}/{{ktok .Budget.Tokens}} tokens · {{minutes .Seconds}}/{{.Budget.Minutes}} min{{if .Slug}} · build {{.Slug}}{{end}}</p>
    {{if and (eq .Status "running") $.Data.GateURL}}<p><a href="{{$.Data.GateURL}}">Gate live view</a></p>{{end}}
    <form method="post" action="/runs/{{.ID}}/cancel?ws={{$ws}}"><button class="danger">Cancel run</button></form>
  </section>
  {{end}}

  {{if .Pending}}
  <section class="decisions">
    <h2>Waiting on you</h2>
    {{$card := .ID}}{{range .Pending}}{{template "decision-form" (dict "ws" $ws "card" $card "item" .)}}{{end}}
  </section>
  {{end}}

  <section class="comments">
    <h2>Comments</h2>
    {{range .Comments}}
    <div class="comment {{.AuthorKind}}"><p class="meta"><span class="badge">{{.AuthorKind}}</span> {{.Author}} <span class="mono muted">{{when .CreatedAt}}</span></p><pre class="text">{{.Body}}</pre></div>
    {{else}}<p class="muted">No comments yet.</p>{{end}}
    <form method="post" action="/cards/{{.ID}}/comment?ws={{$ws}}"><textarea name="body" rows="3" required></textarea><button>Comment</button></form>
  </section>

  {{if .Answered}}
  <section class="answered"><h2>Answered</h2>
    <table><tr><th>decision</th><th>kind</th><th>choice</th><th>by</th><th>when</th></tr>
    {{range .Answered}}<tr><td class="mono">{{.ID}}</td><td>{{.Kind}}</td><td>{{.Choice}}</td><td>{{.Actor}}</td><td class="mono">{{when .AnsweredAt}}</td></tr>{{end}}</table>
  </section>
  {{end}}

  {{if .Runs}}
  <section class="runs"><h2>Runs</h2>
    <table><tr><th>run</th><th>status</th><th>rounds</th><th>cost</th><th>stop reason</th></tr>
    {{range .Runs}}<tr><td><a class="mono" href="/runs/{{.ID}}?ws={{$ws}}">{{.ID}}</a></td><td><span class="badge st-{{.Status}}">{{.Status}}</span></td><td class="mono">{{.Rounds}}</td><td class="mono">{{money .CostUSD}}</td><td>{{.StopReason}}</td></tr>{{end}}</table>
  </section>
  {{end}}

  {{if .Children}}<section><h2>Child cards</h2><ul>{{range .Children}}<li><a href="/cards/{{.ID}}?ws={{$ws}}">{{.Title}}</a> <span class="muted">{{.Lane}}</span></li>{{end}}</ul></section>{{end}}
  {{if .Attachments}}<section><h2>Attachments</h2><ul>{{range .Attachments}}<li><span class="mono">{{.Path}}</span> {{.Label}}</li>{{end}}</ul></section>{{end}}
</article>
{{end}}{{end}}
```

`internal/hub/web/templates/waiting.html`:

```html
{{define "waiting"}}{{template "head" .}}{{template "waiting-main" .}}{{template "foot" .}}{{end}}
{{define "waiting-main"}}
<section class="waiting">
  <h1>Waiting on you</h1>
  {{range .Data}}
  <div class="item">
    <p><span class="ws-label">{{.Workspace}}</span> <a href="/cards/{{.Card.ID}}?ws={{.Workspace}}">{{.Card.Title}}</a></p>
    {{template "decision-form" (dict "ws" .Workspace "card" .Card.ID "item" .Decision)}}
  </div>
  {{else}}<p class="muted">Nothing is waiting on you.</p>{{end}}
</section>
{{end}}
```

`internal/hub/web/static/hub.css`:

```css
:root { --bg: #fafaf8; --fg: #1d1d1b; --muted: #6b6b66; --line: #deded8; --card: #ffffff; --accent: #2457c5;
  --queued: #6b6b66; --running: #2457c5; --waiting: #b26b00; --finished: #1f7a3a; --failed: #b3261e; --cancelled: #6b6b66; --over-budget: #8a3ffc; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #161615; --fg: #ececea; --muted: #a0a09a; --line: #34342f; --card: #1f1f1d; --accent: #7aa2ff;
    --queued: #a0a09a; --running: #7aa2ff; --waiting: #f0a640; --finished: #5cc27a; --failed: #ff7a6e; --cancelled: #a0a09a; --over-budget: #b892ff; }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 15px/1.45 system-ui, -apple-system, "Segoe UI", sans-serif; }
a { color: var(--accent); }
.mono, code, pre, .mono a { font-family: ui-monospace, "Cascadia Mono", Menlo, Consolas, monospace; font-size: 13px; }
.muted { color: var(--muted); }
header.top { display: flex; gap: 16px; align-items: center; padding: 10px 16px; border-bottom: 1px solid var(--line); flex-wrap: wrap; }
header .brand { font-weight: 700; text-decoration: none; color: var(--fg); }
header nav { margin-left: auto; display: flex; gap: 14px; }
.count { background: var(--waiting); color: #fff; border-radius: 9px; padding: 0 6px; }
main { padding: 16px; max-width: 1400px; margin: 0 auto; }
.flash { margin: 8px 16px; padding: 8px 12px; border: 1px solid var(--line); background: var(--card); }
.lanes { display: grid; grid-auto-flow: column; grid-auto-columns: minmax(220px, 1fr); gap: 12px; overflow-x: auto; }
.lane { background: transparent; border: 1px solid var(--line); border-radius: 8px; padding: 8px; min-height: 200px; }
.lane.over { outline: 2px dashed var(--accent); }
.lane h2 { font-size: 14px; margin: 0 0 8px; text-transform: none; }
.card { background: var(--card); border: 1px solid var(--line); border-radius: 6px; padding: 8px; margin-bottom: 8px; cursor: grab; }
.card .title { display: block; font-weight: 600; text-decoration: none; color: var(--fg); margin-bottom: 4px; }
.meta { color: var(--muted); font-size: 13px; display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.kind, .tag { border: 1px solid var(--line); border-radius: 4px; padding: 0 5px; }
.badge { border-radius: 4px; padding: 0 6px; font-size: 12px; border: 1px solid currentColor; }
.st-queued { color: var(--queued); } .st-running { color: var(--running); } .st-waiting { color: var(--waiting); }
.st-finished { color: var(--finished); } .st-failed { color: var(--failed); } .st-cancelled { color: var(--cancelled); }
.st-over-budget { color: var(--over-budget); }
.detail h1 { margin-top: 0; }
section { margin: 18px 0; }
pre.text { white-space: pre-wrap; background: var(--card); border: 1px solid var(--line); padding: 8px; border-radius: 6px; }
form.decision { border: 1px solid var(--waiting); border-radius: 8px; padding: 10px; margin: 10px 0; background: var(--card); }
.choices { display: flex; gap: 8px; margin-top: 8px; flex-wrap: wrap; }
button, .button { font: inherit; padding: 4px 12px; border-radius: 6px; border: 1px solid var(--line); background: var(--card); color: var(--fg); cursor: pointer; text-decoration: none; display: inline-block; }
button.danger { border-color: var(--failed); color: var(--failed); }
.button.primary, button.primary { background: var(--accent); color: #fff; border-color: var(--accent); }
form.inline { display: inline-flex; gap: 6px; margin-right: 8px; }
label { display: block; margin: 6px 0; } label.check { display: flex; gap: 6px; align-items: center; }
input, textarea, select { font: inherit; padding: 4px 6px; border: 1px solid var(--line); border-radius: 4px; background: var(--card); color: var(--fg); width: 100%; max-width: 640px; }
.inline select, .choices input { width: auto; }
table { border-collapse: collapse; width: 100%; } th, td { text-align: left; padding: 4px 8px; border-bottom: 1px solid var(--line); }
.ws-label { font-weight: 600; border: 1px solid var(--line); border-radius: 4px; padding: 0 6px; }
pre#log { max-height: 60vh; overflow: auto; }
```

`internal/hub/web/static/hub.js`:

```js
(function () {
  "use strict";
  var body = document.body;
  var ws = body.dataset.ws || "";
  var run = body.dataset.run || "";

  function query(extra) {
    var p = new URLSearchParams();
    if (ws) p.set("ws", ws);
    Object.keys(extra || {}).forEach(function (k) { p.set(k, extra[k]); });
    return p.toString();
  }

  function typing() {
    var a = document.activeElement;
    return a && /^(TEXTAREA|INPUT|SELECT)$/.test(a.tagName) && a.closest("form");
  }

  var timer = null;
  function refresh() {
    if (timer) return;
    timer = setTimeout(function () {
      timer = null;
      if (typing()) return; // never clobber what the human is typing; the next change refreshes
      var u = new URL(location.href);
      u.searchParams.set("fragment", "main");
      fetch(u, { credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.text() : null; })
        .then(function (html) {
          var m = document.getElementById("main");
          if (html !== null && m) { m.innerHTML = html; bind(); }
        });
    }, 300);
  }

  function appendLog(text) {
    var pre = document.getElementById("log");
    if (!pre) return;
    pre.textContent += text;
    pre.scrollTop = pre.scrollHeight;
  }

  function connect() {
    if (!window.EventSource) return;
    var extra = {};
    if (run) {
      extra.run = run;
      var pre = document.getElementById("log");
      if (pre) extra.from = pre.dataset.from || "0";
    }
    var es = new EventSource("/events?" + query(extra));
    es.addEventListener("change", refresh);
    es.addEventListener("log", function (e) {
      try { appendLog(JSON.parse(e.data).text); } catch (_) { /* ignore a malformed frame */ }
    });
  }

  function move(card, lane, cancel) {
    var data = new URLSearchParams();
    data.set("lane", lane);
    if (cancel) data.set("cancel_run", "1");
    return fetch("/cards/" + encodeURIComponent(card) + "/move?" + query(), {
      method: "POST", body: data, credentials: "same-origin", headers: { "X-TQ-Hub-Fetch": "1" }
    }).then(function (r) {
      if (r.status === 409) {
        return r.text().then(function (msg) {
          if (window.confirm(msg + "\n\nCancel the run and move the card?")) return move(card, lane, true);
        });
      }
      if (!r.ok) return r.text().then(function (msg) { window.alert(msg); });
      refresh();
    });
  }

  function bind() {
    document.querySelectorAll(".card[draggable]").forEach(function (el) {
      el.addEventListener("dragstart", function (e) { e.dataTransfer.setData("text/plain", el.dataset.card); });
    });
    document.querySelectorAll(".lane").forEach(function (lane) {
      lane.addEventListener("dragover", function (e) { e.preventDefault(); lane.classList.add("over"); });
      lane.addEventListener("dragleave", function () { lane.classList.remove("over"); });
      lane.addEventListener("drop", function (e) {
        e.preventDefault();
        lane.classList.remove("over");
        var id = e.dataTransfer.getData("text/plain");
        if (id) move(id, lane.dataset.lane, false);
      });
    });
    document.querySelectorAll("form[data-move]").forEach(function (f) {
      f.addEventListener("submit", function (e) {
        e.preventDefault();
        move(f.dataset.card, f.querySelector("[name=lane]").value, false);
      });
    });
  }

  bind();
  connect();
})();
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go vet ./internal/hub/web/ ./internal/hub/serveinfo/; go test ./internal/hub/web/ ./internal/hub/serveinfo/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/hub/serveinfo/serveinfo.go internal/hub/serveinfo/serveinfo_test.go internal/hub/web/server.go internal/hub/web/auth.go internal/hub/web/pages.go internal/hub/web/api.go internal/hub/web/events.go internal/hub/web/markdown.go internal/hub/web/client.go internal/hub/web/templates/layout.html internal/hub/web/templates/board.html internal/hub/web/templates/card.html internal/hub/web/templates/waiting.html internal/hub/web/static/hub.css internal/hub/web/static/hub.js internal/hub/web/harness_test.go internal/hub/web/markdown_test.go internal/hub/web/pages_test.go internal/hub/web/api_test.go internal/hub/web/events_test.go internal/hub/web/client_test.go
git commit -m "feat(hub): web board, card and waiting pages, JSON API, token and origin checks, SSE"
```

---
### Task 11: Web review page, runs page, activity page, static export

**Files:**
- Create: `internal/hub/web/review.go`, `internal/hub/web/runs.go`, `internal/hub/web/templates/review.html`, `internal/hub/web/templates/run.html`, `internal/hub/web/templates/activity.html`
- Modify: `internal/hub/web/server.go` (one template func, `deref`)
- Test: `internal/hub/web/review_test.go`, `internal/hub/web/runs_test.go`

**Interfaces:**
- Consumes: Task 10 (`Server`, `extraRoutes`, `handle`, `service`, `render`, `fail`, `basePage`, `redirect`, `cardURL`, `Templates`, `assets`), Task 9 (`Approve`, `RequestChanges`, `MarkDone`, `PendingResult`, `runner.PRLink`), Task 6 (`gate.State` with `Records`), Task 8 (`RunDir`).
- Produces (package `web`):
  - `type Commit struct { Hash, Subject string }`; `type ReviewData struct { Card store.Card; Parent *store.Card; Workspace string; Run store.Run; State *gate.State; Records *gate.Records; RecordsNote, SpecPath string; Commits []Commit; DiffStat, PRURL string; Pending *store.Decision; Rounds []store.RoundRecord; Standalone bool; CSS template.CSS; GeneratedAt string }`.
  - `ErrNoReview`; `BuildReview(ctx context.Context, svc *runner.Service, cardID string) (ReviewData, error)` (latest run that is waiting on a `result` or `finished`; evidence only from `build-gate state --records`; every repository- or agent-originated string masked); `RenderReview(w io.Writer, d ReviewData) error` (standalone: inline CSS, no forms, no script); `ExportReview(ctx, svc, card store.Card, run store.Run) (string, error)` → `$TQ_HOME/hub/<ws>/runs/<run>.review.html`.
  - Routes: `GET /cards/{id}/review`, `POST /cards/{id}/review/approve`, `POST /cards/{id}/review/request-changes`, `POST /cards/{id}/review/done`, `GET /runs/{id}`, `GET /activity`.

- [ ] **Step 1: Write the failing tests**

`internal/hub/web/review_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=T", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func section(body, id string) string {
	i := strings.Index(body, `<section id="`+id+`"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	j := strings.Index(rest, "</section>")
	if j < 0 {
		return rest
	}
	return rest[:j]
}

func ok(b bool) *bool { return &b }

// reviewEnv: a card waiting on a result, a git repo with one commit after the base, gate records.
func reviewEnv(t *testing.T) (*env, store.Card, store.Run) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	repo := filepath.Join(e.root, "apps", "billing")
	os.MkdirAll(repo, 0o755)
	git(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	git(t, repo, "add", "a.txt")
	git(t, repo, "commit", "-q", "-m", "chore: base")
	base := git(t, repo, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(repo, "export.py"), []byte("print(1)\n"), 0o644)
	git(t, repo, "add", "export.py")
	git(t, repo, "commit", "-q", "-m", "feat(demo#1): export csv")
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Export invoices", Body: "CSV per month.", ProjectDir: "apps/billing", Tags: []string{store.TagAgentReady}}, nil)
	r, _ := e.pendingDecision("acme", c, store.KindResult)
	e.gate.mu.Lock()
	st := e.gate.states["demo"]
	st.Bases = map[string]string{"1": base}
	st.Checkpoints = []gate.Checkpoint{{ID: "1", Title: "Parse rows", Status: "passed", Gates: map[string]string{"behavior": "passed", "review": "passed"}, Evidence: "fresh", Reviews: map[string]string{"build-critic": "PASS"}}}
	st.Records = &gate.Records{
		Plan:     &gate.PlanRecord{Summary: "Export invoices as CSV.", Spec: "docs/specs/demo.md", Checkpoints: []gate.PlanCheckpoint{{ID: "1", Title: "Parse rows", Approved: true, Tests: []gate.PlanTest{{File: "tests/test_rows.py", Layer: "unit"}}}}},
		Evidence: []gate.Evidence{{Checkpoint: "1", TS: "2026-10-21T09:30:00+00:00", Tree: "abcdef0123456789", OK: true, Current: true, Summary: "all verify commands passed", Results: []gate.VerifyResult{{Cmd: "python -m pytest tests/test_rows.py -q", Exit: 0, Seconds: 1.2}}}},
		Reviews:  []gate.Review{{Checkpoint: "1", Reviewer: "build-critic", Result: "PASS", Tree: "abcdef0123456789", TS: "2026-10-21T09:40:00+00:00", Minors: 1}},
		DoD:      &gate.DoDRecord{OK: true, Tree: "abcdef0123456789", Checks: []gate.DoDCheck{{ID: "test", OK: ok(true), Detail: "exit 0: python -m pytest -q"}}},
	}
	e.gate.mu.Unlock()
	svc.Store().AddComment(c.ID, store.AuthorAgent, "agent", "ALL GREEN, trust me. PR: https://github.com/acme/billing/pull/9", r.ID)
	svc.OnApprove = func(card store.Card, run store.Run) {
		if _, err := ExportReview(context.Background(), svc, card, run); err != nil {
			t.Errorf("export: %v", err)
		}
	}
	return e, c, r
}

func TestReviewPageIsBuiltFromGateRecords(t *testing.T) {
	e, c, _ := reviewEnv(t)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, id := range []string{"goal", "approach", "done", "stands", "evidence", "cost", "feedback", "approve"} {
		if section(body, id) == "" {
			t.Fatalf("section %s missing", id)
		}
	}
	ev := section(body, "evidence")
	if !strings.Contains(ev, "python -m pytest tests/test_rows.py -q") || !strings.Contains(ev, "abcdef012345") {
		t.Fatalf("evidence:\n%s", ev)
	}
	if strings.Contains(ev, "trust me") || strings.Contains(section(body, "stands"), "trust me") {
		t.Fatal("agent text must never appear as evidence or status")
	}
	if !strings.Contains(section(body, "done"), "feat(demo#1): export csv") || !strings.Contains(section(body, "done"), "pull/9") {
		t.Fatalf("done:\n%s", section(body, "done"))
	}
	if !strings.Contains(section(body, "approach"), "tests/test_rows.py") || !strings.Contains(section(body, "goal"), "docs/specs/demo.md") {
		t.Fatal("approach and goal come from the signed plan record")
	}
	if !strings.Contains(section(body, "approve"), `action="/cards/`+c.ID+`/review/approve?ws=acme"`) || strings.Contains(body, "/review/done") {
		t.Fatal("a pending result offers Approve, not Mark done")
	}
}

func TestApproveFromTheReviewPageExportsIt(t *testing.T) {
	e, c, r := reviewEnv(t)
	ts := e.server(Options{})
	code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/review/approve?ws=acme", url.Values{}, origin(ts))
	if code != http.StatusSeeOther {
		t.Fatalf("%d", code)
	}
	got, _ := e.svcs["acme"].Store().Card(c.ID)
	if got.Lane != "done" || e.gate.answers[0] != "demo d1a2b3c4 approve hub:tester" {
		t.Fatalf("lane %s answers %v", got.Lane, e.gate.answers)
	}
	out := filepath.Join(store.RunsDir("acme"), r.ID+".review.html")
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	if strings.Contains(html, "<script") || strings.Contains(html, "<form") || !strings.Contains(html, "python -m pytest tests/test_rows.py -q") || !strings.Contains(html, "<style>") {
		t.Fatalf("export:\n%s", html)
	}
}

func TestRequestChangesFromTheReviewPage(t *testing.T) {
	e, c, _ := reviewEnv(t)
	ts := e.server(Options{})
	code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/review/request-changes?ws=acme", url.Values{"note": {"Name the file by month."}}, origin(ts))
	if code != http.StatusSeeOther || e.gate.answers[0] != "demo d1a2b3c4 request-changes hub:tester" {
		t.Fatalf("%d %v", code, e.gate.answers)
	}
	cs, _ := e.svcs["acme"].Store().Comments(c.ID)
	if cs[len(cs)-1].Body != "Name the file by month." || cs[len(cs)-1].AuthorKind != store.AuthorHuman {
		t.Fatalf("%+v", cs[len(cs)-1])
	}
}

func TestReviewWithoutAResultOffersMarkDone(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{})
	if code, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil); code != http.StatusNotFound || !strings.Contains(body, "No finished run") {
		t.Fatalf("%d %s", code, body)
	}
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.Status = store.StatusFinished
	svc.Store().SaveRun(r)
	svc.Store().Release(c.ID, r.ID)
	_, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	if !strings.Contains(body, "/review/done?ws=acme") || strings.Contains(body, "/review/approve") || !strings.Contains(body, "no build slug") {
		t.Fatalf("%s", body)
	}
	code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/review/done?ws=acme", url.Values{}, origin(ts))
	if got, _ := svc.Store().Card(c.ID); code != http.StatusSeeOther || got.Lane != "done" {
		t.Fatalf("%d %s", code, got.Lane)
	}
}
```

`internal/hub/web/runs_test.go`:

```go
package web

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestRunPageShowsBudgetUsageAndLogTail(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.Rounds, r.CostUSD = 2, 1.25
	r.Usage = store.Usage{Input: 1500, Output: 500}
	r.RoundLog = []store.RoundRecord{{N: 1, CostUSD: 1, Seconds: 60}, {N: 2, CostUSD: 0.25, Seconds: 30, CostUnknown: true}}
	svc.Store().SaveRun(r)
	os.WriteFile(r.LogPath, []byte("=== round 2\nlast line of the log\n"), 0o600)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/runs/"+r.ID+"?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, want := range []string{`data-run="` + r.ID + `"`, "last line of the log", `id="log"`, "$1.25", "$10.00", "2.0k", "cost unknown", "st-queued"} {
		if !strings.Contains(body, want) {
			t.Fatalf("run page lacks %q", want)
		}
	}
}

func TestActivityPageListsVerbs(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	svc.Comment(context.Background(), c.ID, "hello")
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/activity?ws=acme", nil)
	if !strings.Contains(body, "card.create") || !strings.Contains(body, "comment.add") || !strings.Contains(body, c.ID) {
		t.Fatal(body)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/hub/web/ -count=1 -run 'Test(Review|Approve|RequestChanges|Run|Activity)'`
Expected: FAIL, `undefined: ExportReview` (and 404s for the new routes).

- [ ] **Step 3: Implement**

`internal/hub/web/review.go`:

```go
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

var ErrNoReview = errors.New("no finished run to review yet")

type Commit struct{ Hash, Subject string }

type ReviewData struct {
	Card        store.Card
	Parent      *store.Card
	Workspace   string
	Run         store.Run
	State       *gate.State
	Records     *gate.Records
	RecordsNote string
	SpecPath    string
	Commits     []Commit
	DiffStat    string
	PRURL       string
	Pending     *store.Decision
	Rounds      []store.RoundRecord
	Standalone  bool
	CSS         template.CSS
	GeneratedAt string
}

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) {
		s.handle("GET /cards/{id}/review", s.reviewPage)
		s.handle("POST /cards/{id}/review/approve", s.reviewApprove)
		s.handle("POST /cards/{id}/review/request-changes", s.reviewRequestChanges)
		s.handle("POST /cards/{id}/review/done", s.reviewDone)
	})
}

// BuildReview assembles the review page of spec §10 for the card's latest reviewable run. Status and
// evidence come only from gate records; agent text appears only as comments, never here.
func BuildReview(ctx context.Context, svc *runner.Service, cardID string) (ReviewData, error) {
	st := svc.Store()
	card, err := st.Card(cardID)
	if err != nil {
		return ReviewData{}, err
	}
	runs, err := st.RunsForCard(cardID)
	if err != nil {
		return ReviewData{}, err
	}
	pending, hasPending := svc.PendingResult(cardID)
	var run *store.Run
	for i := range runs {
		if (hasPending && runs[i].ID == pending.RunID) || runs[i].Status == store.StatusFinished {
			r := runs[i]
			run = &r
			break
		}
	}
	if run == nil {
		return ReviewData{}, ErrNoReview
	}
	d := ReviewData{Card: card, Workspace: svc.Workspace(), Run: *run, Rounds: run.RoundLog, GeneratedAt: store.Now()}
	if card.ParentID != "" {
		if p, err := st.Card(card.ParentID); err == nil {
			d.Parent = &p
		}
	}
	if hasPending {
		p := pending
		d.Pending = &p
	}
	dir := svc.RunDir(*run)
	if run.Slug == "" {
		d.RecordsNote = "this run has no build slug, so there are no gate records"
	} else if gst, err := svc.Gate().State(ctx, dir, run.Slug, true); err == nil {
		d.State, d.Records = gst, gst.Records
	} else {
		d.RecordsNote = "gate records unavailable: " + err.Error()
	}
	if d.State == nil && len(run.State) > 0 {
		var gs gate.State
		if json.Unmarshal(run.State, &gs) == nil {
			d.State = &gs
		}
	}
	if d.Records != nil && d.Records.Plan != nil {
		d.SpecPath = d.Records.Plan.Spec
	}
	d.Commits, d.DiffStat = gitSummary(ctx, dir, firstBase(d.State))
	d.PRURL = runner.PRLink(st, card.ID)
	maskReview(st, &d)
	return d, nil
}

func firstBase(st *gate.State) string {
	if st == nil || len(st.Bases) == 0 {
		return ""
	}
	keys := make([]string, 0, len(st.Bases))
	for k := range st.Bases {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, ea := strconv.Atoi(keys[i])
		b, eb := strconv.Atoi(keys[j])
		if ea == nil && eb == nil {
			return a < b
		}
		return keys[i] < keys[j]
	})
	return st.Bases[keys[0]]
}

var hashRe = func(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// gitSummary reads commits since the run's base and a diff summary, bounded to 10 s.
func gitSummary(ctx context.Context, dir, base string) ([]Commit, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	run := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		if cmd.Run() != nil {
			return ""
		}
		return out.String()
	}
	logArgs := []string{"log", "--format=%h%x09%s", "-n", "20"}
	diff := ""
	if hashRe(base) {
		logArgs = []string{"log", "--format=%h%x09%s", base + "..HEAD"}
		diff = strings.TrimSpace(run("diff", "--stat", base+"..HEAD"))
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(run(logArgs...)), "\n") {
		h, s, ok := strings.Cut(line, "\t")
		if ok {
			commits = append(commits, Commit{Hash: h, Subject: s})
		}
	}
	return commits, diff
}

// maskReview masks every string that came from the repository, the plan or the gate's detail text.
func maskReview(st *store.Store, d *ReviewData) {
	for i := range d.Commits {
		d.Commits[i].Subject = st.Mask(d.Commits[i].Subject)
	}
	d.DiffStat = st.Mask(d.DiffStat)
	d.RecordsNote = st.Mask(d.RecordsNote)
	if r := d.Records; r != nil {
		if r.Plan != nil {
			r.Plan.Summary, r.Plan.PlainSummary = st.Mask(r.Plan.Summary), st.Mask(r.Plan.PlainSummary)
			for i := range r.Plan.Checkpoints {
				r.Plan.Checkpoints[i].Title = st.Mask(r.Plan.Checkpoints[i].Title)
			}
		}
		for i := range r.Evidence {
			r.Evidence[i].Summary = st.Mask(r.Evidence[i].Summary)
			for j := range r.Evidence[i].Results {
				r.Evidence[i].Results[j].Cmd = st.Mask(r.Evidence[i].Results[j].Cmd)
			}
		}
		if r.DoD != nil {
			for i := range r.DoD.Checks {
				r.DoD.Checks[i].Detail = st.Mask(r.DoD.Checks[i].Detail)
			}
			if r.DoD.Override != nil {
				o := st.Mask(*r.DoD.Override)
				r.DoD.Override = &o
			}
		}
	}
	if d.State != nil {
		for i := range d.State.Checkpoints {
			d.State.Checkpoints[i].Title = st.Mask(d.State.Checkpoints[i].Title)
		}
	}
}

// RenderReview writes the review document; standalone adds the CSS inline and drops forms and script.
func RenderReview(w io.Writer, d ReviewData) error {
	tpl, err := Templates()
	if err != nil {
		return err
	}
	if d.Standalone {
		css, err := assets.ReadFile("static/hub.css")
		if err != nil {
			return err
		}
		d.CSS = template.CSS(css)
		return tpl.ExecuteTemplate(w, "review-standalone", d)
	}
	return tpl.ExecuteTemplate(w, "review-body", d)
}

// ExportReview saves the approved review next to the run log, so the record survives the hub.
func ExportReview(ctx context.Context, svc *runner.Service, card store.Card, run store.Run) (string, error) {
	d, err := BuildReview(ctx, svc, card.ID)
	if err != nil {
		return "", err
	}
	d.Standalone = true
	var buf bytes.Buffer
	if err := RenderReview(&buf, d); err != nil {
		return "", err
	}
	path := filepath.Join(store.RunsDir(svc.Workspace()), run.ID+".review.html")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, buf.Bytes(), 0o600)
}

func (s *Server) reviewPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	d, err := BuildReview(r.Context(), svc, r.PathValue("id"))
	if errors.Is(err, ErrNoReview) {
		err = errors.Join(store.ErrNotFound, errors.New("No finished run to review yet: the card's run has not reached its result."))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.basePage(r, "Review: "+d.Card.Title, ws)
	p.Data = d
	s.render(w, r, "review", p)
}

func (s *Server) reviewApprove(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	out, err := svc.Approve(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, id)+"&flash="+urlQuery(out.Message))
}

func (s *Server) reviewRequestChanges(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	out, err := svc.RequestChanges(r.Context(), id, r.FormValue("note"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, id)+"&flash="+urlQuery(out.Message))
}

func (s *Server) reviewDone(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := svc.MarkDone(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, cardURL(ws, id))
}
```

`internal/hub/web/runs.go`:

```go
package web

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func urlQuery(s string) string { return url.QueryEscape(s) }

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) {
		s.handle("GET /runs/{id}", s.runPage)
		s.handle("GET /activity", s.activityPage)
	})
}

type runData struct {
	Run     store.Run
	Card    store.Card
	LogTail string
	LogSize int64
	GateURL string
}

func tailFile(path string, max int64) (string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0
	}
	start := fi.Size() - max
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", fi.Size()
	}
	b, _ := io.ReadAll(f)
	return string(b), start + int64(len(b))
}

func (s *Server) runPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	run, err := svc.Store().Run(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	card, _ := svc.Store().Card(run.CardID)
	d := runData{Run: run, Card: card, GateURL: svc.GateURL()}
	d.LogTail, d.LogSize = tailFile(run.LogPath, 64<<10)
	p := s.basePage(r, "Run "+run.ID, ws)
	p.RunID = run.ID
	p.Data = d
	s.render(w, r, "run", p)
}

type activityData struct {
	Entries []activity.Entry
}

func (s *Server) activityPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	es, err := svc.Store().Activity(limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.basePage(r, "Activity", ws)
	p.Data = activityData{Entries: es}
	s.render(w, r, "activity", p)
}
```

`internal/hub/web/templates/review.html`:

```html
{{define "review"}}{{template "head" .}}{{template "review-main" .}}{{template "foot" .}}{{end}}
{{define "review-main"}}{{template "review-body" .Data}}{{end}}

{{define "review-standalone"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Review: {{.Card.Title}}</title><style>{{.CSS}}</style></head>
<body><main>{{template "review-body" .}}<p class="muted mono">exported {{.GeneratedAt}} by tq hub · workspace {{.Workspace}} · run {{.Run.ID}}</p></main></body></html>{{end}}

{{define "review-body"}}
<article class="review">
<h1>Review: {{.Card.Title}}</h1>

<section id="goal"><h2>1. Goal</h2>
  <p><span class="kind">{{.Card.Kind}}</span>{{if .Parent}} · parent <strong>{{.Parent.Title}}</strong>{{end}}{{if .SpecPath}} · spec slice <span class="mono">{{.SpecPath}}</span>{{end}}{{if .State}}{{if .State.Feature}} · feature {{.State.Feature}}{{end}}{{end}}</p>
  <div class="body">{{md .Card.Body}}</div>
</section>

<section id="approach"><h2>2. Approach</h2>
  {{with .Records}}{{with .Plan}}
  <p>{{.Summary}}</p>{{if .PlainSummary}}<p class="muted">{{.PlainSummary}}</p>{{end}}
  <table><tr><th>#</th><th>checkpoint</th><th>approved</th><th>tests</th></tr>
  {{range .Checkpoints}}<tr><td class="mono">{{.ID}}</td><td>{{.Title}}</td><td>{{if .Approved}}yes{{else}}<strong>changed since approval</strong>{{end}}</td>
    <td>{{range .Tests}}<span class="mono">{{.File}}</span> ({{.Layer}}) {{end}}</td></tr>{{end}}</table>
  {{else}}<p class="muted">No approved plan record.</p>{{end}}{{else}}<p class="muted">No gate records.</p>{{end}}
</section>

<section id="done"><h2>3. What was done</h2>
  {{if .PRURL}}<p>Pull request: <a href="{{.PRURL}}">{{.PRURL}}</a></p>{{end}}
  {{if .Commits}}<ul>{{range .Commits}}<li><span class="mono">{{.Hash}}</span> {{.Subject}}</li>{{end}}</ul>{{else}}<p class="muted">No commits found.</p>{{end}}
  {{if .DiffStat}}<pre class="text">{{.DiffStat}}</pre>{{end}}
</section>

<section id="stands"><h2>4. Where it stands</h2>
  {{with .State}}
  <table><tr><th>#</th><th>checkpoint</th><th>status</th><th>behavior</th><th>review</th><th>evidence</th><th>reviewers</th></tr>
  {{range .Checkpoints}}<tr><td class="mono">{{.ID}}</td><td>{{.Title}}</td><td>{{.Status}}</td><td>{{index .Gates "behavior"}}</td><td>{{index .Gates "review"}}</td><td>{{.Evidence}}</td>
    <td>{{range $k, $v := .Reviews}}{{$k}}: {{$v}} {{end}}</td></tr>{{end}}</table>
  {{end}}
  {{with .Records}}{{with .DoD}}
  <h3>Definition of done: {{if .OK}}PASS{{else}}FAIL{{end}} <span class="mono">{{.Tree}}</span></h3>
  {{if .Override}}<p><strong>Overridden:</strong> {{.Override}}</p>{{end}}
  <table><tr><th>check</th><th>result</th><th>detail</th></tr>
  {{range .Checks}}<tr><td class="mono">{{.ID}}</td><td>{{if not .OK}}skip{{else if deref .OK}}ok{{else}}MISSING{{end}}</td><td>{{.Detail}}</td></tr>{{end}}</table>
  {{end}}{{end}}
  {{if .RecordsNote}}<p class="muted">{{.RecordsNote}}</p>{{end}}
</section>

<section id="evidence"><h2>5. Evidence</h2>
  {{with .Records}}
  <table><tr><th>#</th><th>result</th><th>command</th><th>exit</th><th>tree</th><th>time</th></tr>
  {{range .Evidence}}{{$e := .}}{{range .Results}}<tr><td class="mono">{{$e.Checkpoint}}</td><td>{{if $e.OK}}pass{{else}}fail{{end}}{{if not $e.Current}} (older approval){{end}}</td>
    <td class="mono">{{.Cmd}}</td><td class="mono">{{.Exit}}{{if .TimedOut}} timed out{{end}}</td><td class="mono">{{printf "%.12s" $e.Tree}}</td><td class="mono">{{when $e.TS}}</td></tr>{{end}}{{end}}</table>
  <h3>Reviewer verdicts</h3>
  <table><tr><th>#</th><th>reviewer</th><th>result</th><th>blockers/majors/minors/unjudged</th><th>tree</th><th>by</th></tr>
  {{range .Reviews}}<tr><td class="mono">{{.Checkpoint}}</td><td>{{.Reviewer}}</td><td>{{.Result}}</td><td class="mono">{{.Blockers}}/{{.Majors}}/{{.Minors}}/{{.Unjudged}}</td><td class="mono">{{printf "%.12s" .Tree}}</td><td>{{if .Human}}human{{else}}agent{{end}}</td></tr>{{end}}</table>
  {{else}}<p class="muted">No gate evidence recorded.</p>{{end}}
  {{with .State}}{{if .Bases}}<p class="muted">review bases: {{range $k, $v := .Bases}}<span class="mono">#{{$k}} {{printf "%.12s" $v}}</span> {{end}}</p>{{end}}{{end}}
</section>

<section id="cost"><h2>6. Cost</h2>
  <p class="mono">{{.Run.Rounds}} rounds · {{ktok .Run.Usage.Tokens}} tokens · {{money .Run.CostUSD}} · {{minutes .Run.Seconds}} min</p>
  <table><tr><th>round</th><th>tokens in/out</th><th>cost</th><th>seconds</th><th>outcome</th></tr>
  {{range .Rounds}}<tr><td class="mono">{{.N}}</td><td class="mono">{{.Usage.Input}}/{{.Usage.Output}}</td><td class="mono">{{money .CostUSD}}{{if .CostUnknown}} (cost unknown){{end}}</td><td class="mono">{{printf "%.0f" .Seconds}}</td><td>{{.Outcome}}</td></tr>{{end}}</table>
</section>

{{if not .Standalone}}
<section id="feedback"><h2>7. Feedback</h2>
  {{if .Pending}}
  <form method="post" action="/cards/{{.Card.ID}}/review/request-changes?ws={{.Workspace}}">
    <textarea name="note" rows="4" required placeholder="What should change?"></textarea>
    <button>Request changes</button>
  </form>
  {{else}}<p class="muted">No result is waiting for feedback.</p>{{end}}
</section>
<section id="approve"><h2>8. Approve</h2>
  {{if .Pending}}
  <form method="post" action="/cards/{{.Card.ID}}/review/approve?ws={{.Workspace}}"><button class="primary">Approve</button></form>
  <p class="muted">Approving answers the result record; the gate runs finish, the card moves to done.</p>
  {{else}}
  <form method="post" action="/cards/{{.Card.ID}}/review/done?ws={{.Workspace}}"><button>Mark done</button></form>
  <p class="muted">This run has no pending result record; marking done is a board action only.</p>
  {{end}}
</section>
{{else}}
<section id="feedback"><h2>7. Feedback</h2><p class="muted">Exported after approval.</p></section>
<section id="approve"><h2>8. Approve</h2><p>Approved. Run <span class="mono">{{.Run.ID}}</span> {{.Run.Status}}.</p></section>
{{end}}
</article>
{{end}}
```

Add a `deref` template func to `funcs` in `server.go` (the DoD check's `ok` is a `*bool`):

```go
	"deref": func(b *bool) bool { return b != nil && *b },
```

`internal/hub/web/templates/run.html`:

```html
{{define "run"}}{{template "head" .}}{{template "run-main" .}}{{template "foot" .}}{{end}}
{{define "run-main"}}{{$ws := .WS}}{{with .Data}}
<article class="detail">
  <h1>Run <span class="mono">{{.Run.ID}}</span> <span class="badge st-{{.Run.Status}}">{{.Run.Status}}</span></h1>
  <p>Card <a href="/cards/{{.Card.ID}}?ws={{$ws}}">{{.Card.Title}}</a> · {{.Run.Adapter}} · on-ramp {{.Run.OnRamp}}{{if .Run.Slug}} · build <span class="mono">{{.Run.Slug}}</span>{{end}}{{if .Run.Worktree}} · <span class="mono">{{.Run.Worktree}}</span>{{end}}</p>
  {{if .Run.StopReason}}<p><strong>Stop reason:</strong> {{.Run.StopReason}}</p>{{end}}
  {{if and (eq .Run.Status "running") .GateURL}}<p><a href="{{.GateURL}}">Gate live view</a></p>{{end}}
  <table><tr><th></th><th>used</th><th>budget</th></tr>
    <tr><td>tokens</td><td class="mono">{{ktok .Run.Usage.Tokens}}</td><td class="mono">{{ktok .Run.Budget.Tokens}}</td></tr>
    <tr><td>dollars</td><td class="mono">{{money .Run.CostUSD}}</td><td class="mono">{{money .Run.Budget.USD}}</td></tr>
    <tr><td>minutes</td><td class="mono">{{minutes .Run.Seconds}}</td><td class="mono">{{.Run.Budget.Minutes}}</td></tr>
    <tr><td>rounds</td><td class="mono">{{.Run.Rounds}}</td><td class="mono">{{.Run.Budget.Rounds}}</td></tr>
  </table>
  <h2>Rounds</h2>
  <table><tr><th>round</th><th>started</th><th>seconds</th><th>tokens in/out</th><th>cost</th><th>exit</th><th>outcome</th></tr>
  {{range .Run.RoundLog}}<tr><td class="mono">{{.N}}</td><td class="mono">{{when .StartedAt}}</td><td class="mono">{{printf "%.0f" .Seconds}}</td><td class="mono">{{.Usage.Input}}/{{.Usage.Output}}</td>
    <td class="mono">{{money .CostUSD}}{{if .CostUnknown}} (cost unknown){{end}}</td><td class="mono">{{.ExitCode}}</td><td>{{.Outcome}}</td></tr>{{end}}</table>
  <h2>Log</h2>
  <pre id="log" class="text" data-from="{{.LogSize}}">{{.LogTail}}</pre>
</article>
{{end}}{{end}}
```

`internal/hub/web/templates/activity.html`:

```html
{{define "activity"}}{{template "head" .}}{{template "activity-main" .}}{{template "foot" .}}{{end}}
{{define "activity-main"}}{{$ws := .WS}}
<section>
  <h1>Activity</h1>
  <table><tr><th>when</th><th>who</th><th>what</th><th>card</th><th>run</th></tr>
  {{range .Data.Entries}}<tr><td class="mono">{{when .TS}}</td><td>{{.Actor}}</td><td class="mono">{{.Verb}}</td>
    <td>{{if .CardID}}<a class="mono" href="/cards/{{.CardID}}?ws={{$ws}}">{{.CardID}}</a>{{end}}</td>
    <td>{{if .RunID}}<a class="mono" href="/runs/{{.RunID}}?ws={{$ws}}">{{.RunID}}</a>{{end}}</td></tr>
  {{else}}<tr><td colspan="5" class="muted">No activity yet.</td></tr>{{end}}</table>
</section>
{{end}}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/hub/web/; go test ./internal/hub/web/ -count=1`
Expected: PASS (all Task 10 and Task 11 web tests; the review tests SKIP without git).

- [ ] **Step 5: Commit**

```bash
git add internal/hub/web/review.go internal/hub/web/runs.go internal/hub/web/server.go internal/hub/web/templates/review.html internal/hub/web/templates/run.html internal/hub/web/templates/activity.html internal/hub/web/review_test.go internal/hub/web/runs_test.go
git commit -m "feat(hub): review page from gate records, run and activity pages, static export on approval"
```

---
### Task 12: CLI `tq hub add|list|show|move|run|answer|review|open|cancel|comment`

**Files:**
- Create: `internal/cmd/hub.go`, `internal/cmd/hub_service.go`
- Modify: `internal/cmd/root.go` (register `newHubCmd()`)
- Test: `internal/cmd/hub_test.go`

**Interfaces:**
- Consumes: `findWorkspace`, `claudeArgs` (`internal/cmd/allow.go`, `run.go`), `resolve.Resolve`, `registry.Load`, `trust.IsTrusted`, `envplan.Environ`, `envplan.Desired`; Task 5 (`adapter.Claude`, `adapter.Process`), Task 6 (`gate.ForWorkspace`, `gate.Unavailable`, `gate.Actor`), Task 7 (`notify.Send`), Tasks 8 and 9 (`runner.New`, `runner.Ops`, `runner.WithAgent`, `runner.ErrHumanOnly`, `runner.RunActiveError`), Task 10 (`serveinfo.Find`, `serveinfo.BaseURL`, `web.NewClient`), Task 11 (`web.BuildReview`, `web.RenderReview`, `web.ExportReview`); test helpers `runTQ`, `setupTrustedWorkspaceWithManifest`, `mkUntrusted`, `unsetenv` (existing in `internal/cmd`).
- Produces (package `cmd`):
  - `type hubFlags struct { workspace string; timeout time.Duration }`; `var hubExtra []func(*hubFlags) *cobra.Command` (Task 13 appends `serve` and `status`).
  - `newHubCmd() *cobra.Command` with persistent `--workspace NAME` and `--timeout D`.
  - `hubWorkspace(name string) (*resolve.Workspace, error)` (cwd, else `TQ_HUB_TASK`'s workspace; must be trusted), `hubClasses(ws) []string`, `hubAgent() bool` (`TENTAQLES_HEADLESS=1`, `CLAUDE_PLUGIN_ROOT` or `CLAUDECODE=1`), `newHubService(ws *resolve.Workspace, warn func(string), notifyOn bool) (*runner.Service, error)` (the production factory: store, gate through the identity's plugin, `claude` adapter wrapped in `claudeArgs`, optional `process` adapter from the `process_command` setting, `OnApprove` → static export), `openHub(c *cobra.Command, f *hubFlags) (*hubConn, context.Context, func(), error)` (API client when a hub serves the workspace, else the store directly), `openBrowser(url string) error`, `hubCardFromTask() (card, workspace string, ok bool)`, `warnTo(c *cobra.Command) func(string)`, `hubDefaultPort = 7470`.
  - Test-only environment seams (documented in `docs/HUB.md`): `TQ_HUB_CLAUDE_CMD` (JSON argv instead of `claude`) and `TQ_HUB_GATE_CMD` (Task 6).

- [ ] **Step 1: Write the failing tests**

`internal/cmd/hub_test.go`:

```go
package cmd

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
)

// hubWS makes a trusted workspace "acme", isolates TQ_HOME, HOME and USERPROFILE, clears the agent
// markers and chdirs into the workspace.
func hubWS(t *testing.T) string {
	t.Helper()
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	home := filepath.Dir(os.Getenv("TQ_HOME"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		unsetenv(t, k)
	}
	t.Setenv("TQ_HUB_GATE_CMD", `["tq-hub-test-no-gate"]`)
	os.MkdirAll(filepath.Join(root, "apps", "billing"), 0o755)
	t.Chdir(root)
	return root
}

func addCard(t *testing.T, args ...string) store.Card {
	t.Helper()
	_, out, errOut, err := runTQ(t, append([]string{"hub", "add"}, append(args, "--json")...)...)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	var c store.Card
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return c
}

func TestHubAddListShow(t *testing.T) {
	hubWS(t)
	c := addCard(t, "Export invoices", "--kind", "feature", "--tag", "agent-ready", "--project", "apps/billing", "--body", "CSV per month.")
	if !strings.HasPrefix(c.ID, "c-") || c.Lane != "backlog" || c.ProjectDir != "apps/billing" {
		t.Fatalf("%+v", c)
	}
	_, out, _, err := runTQ(t, "hub", "list")
	if err != nil || !strings.Contains(out, c.ID) || !strings.Contains(out, "Export invoices") || !strings.Contains(out, "backlog") {
		t.Fatalf("%v\n%s", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "list", "--json", "--kind", "fix")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("filter: %v %q", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "show", c.ID)
	if err != nil || !strings.Contains(out, "CSV per month.") || !strings.Contains(out, "feature") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(store.DBPath("acme")); err != nil {
		t.Fatal("the store lives under TQ_HOME/hub/<workspace>")
	}
}

func TestHubRefusesUntrustedWorkspaces(t *testing.T) {
	root := mkUntrusted(t, "evil")
	home := filepath.Dir(os.Getenv("TQ_HOME"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(root)
	_, _, _, err := runTQ(t, "hub", "list")
	if err == nil || !strings.Contains(err.Error(), "untrusted") || !strings.Contains(err.Error(), "tq allow evil") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(store.DBPath("evil")); !os.IsNotExist(err) {
		t.Fatal("no database for an untrusted workspace")
	}
}

func TestHubAgentMayAddAndCommentOnly(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x", "--tag", "agent-ready")
	t.Setenv("TENTAQLES_HEADLESS", "1")
	if _, _, _, err := runTQ(t, "hub", "add", "y", "--budget", "rounds=99"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("budget: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "move", c.ID, "done"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("move to done: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "answer", c.ID, "d1a2b3c4", "--choice", "approve"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("answer: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "comment", c.ID, "progress note"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "add", "agent card"); err != nil {
		t.Fatalf("add: %v", err)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	defer st.Close()
	cs, _ := st.Comments(c.ID)
	if len(cs) != 1 || cs[0].AuthorKind != store.AuthorAgent {
		t.Fatalf("%+v", cs)
	}
}

func TestHubCommentOnTheTaskCard(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x")
	t.Setenv("TQ_HUB_TASK", `{"hub":"`+c.ID+`","workspace":"acme","kind":"feature","external":null}`)
	t.Setenv("TENTAQLES_HEADLESS", "1")
	if _, _, _, err := runTQ(t, "hub", "comment", "--timeout", "2s", "-", "PR: https://github.com/acme/billing/pull/7"); err != nil {
		t.Fatal(err)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	defer st.Close()
	if pr := runner.PRLink(st, c.ID); pr != "https://github.com/acme/billing/pull/7" {
		t.Fatalf("%q", pr)
	}
}

func TestCLIMoveRunningNeedsCancelRun(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x", "--tag", "agent-ready", "--project", "apps/billing")
	_, out, _, err := runTQ(t, "hub", "move", c.ID, "in progress")
	if err != nil || !strings.Contains(out, "queued run r-") {
		t.Fatalf("%v %s", err, out)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	r, _, _ := st.ActiveRun(c.ID)
	r.Status = store.StatusRunning
	st.SaveRun(r)
	st.Close()
	_, _, _, err = runTQ(t, "hub", "move", c.ID, "backlog")
	if err == nil || !strings.Contains(err.Error(), "--cancel-run") {
		t.Fatalf("want a --cancel-run refusal, got %v", err)
	}
	st, _ = store.OpenExisting("acme", []string{"all"})
	card, _ := st.Card(c.ID)
	st.Close()
	if card.Lane != "in progress" {
		t.Fatal("the card must not move")
	}
	_, out, _, err = runTQ(t, "hub", "move", c.ID, "backlog", "--cancel-run")
	if err != nil || !strings.Contains(out, "cancelled run "+r.ID) {
		t.Fatalf("%v %s", err, out)
	}
}

func TestHubTalksToARunningHub(t *testing.T) {
	hubWS(t)
	ws, err := hubWorkspace("")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newHubService(ws, func(string) {}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Store().Close()
	srv, _ := web.New(web.Options{Backend: web.StaticBackend{Infos: []web.WorkspaceInfo{{Name: "acme", Available: true}},
		Services: map[string]*runner.Service{"acme": svc}}, LocalKey: "lk"})
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		srv.Handler().ServeHTTP(w, r)
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := serveinfo.Write(serveinfo.Info{PID: os.Getpid(), Addr: u.Host, Port: port, Mode: "single", Workspaces: []string{"acme"}, LocalKey: "lk"}); err != nil {
		t.Fatal(err)
	}
	defer serveinfo.Remove(port)
	c := addCard(t, "via the API")
	if atomic.LoadInt32(&hits) < 2 { // ping + add
		t.Fatalf("the CLI wrote the store directly (%d API calls)", hits)
	}
	_, out, _, err := runTQ(t, "hub", "review", c.ID)
	if err != nil || !strings.Contains(out, ts.URL+"/cards/"+c.ID+"/review?ws=acme") {
		t.Fatalf("%v %s", err, out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cmd/ -run 'TestHub|TestCLIMove' -count=1`
Expected: FAIL, `unknown command "hub" for "tq"` and `undefined: hubWorkspace`.

- [ ] **Step 3: Implement the service factory**

`internal/cmd/hub_service.go`:

```go
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/notify"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// hubAgent: an agent session may add cards and comment, nothing else (spec §15).
func hubAgent() bool {
	return os.Getenv("TENTAQLES_HEADLESS") == "1" || os.Getenv("CLAUDE_PLUGIN_ROOT") != "" || os.Getenv("CLAUDECODE") == "1"
}

// hubCardFromTask reads the card id of the run this session belongs to (TQ_HUB_TASK).
func hubCardFromTask() (string, string, bool) {
	var t struct {
		Hub       string `json:"hub"`
		Workspace string `json:"workspace"`
	}
	raw := strings.TrimSpace(os.Getenv("TQ_HUB_TASK"))
	if raw == "" || json.Unmarshal([]byte(raw), &t) != nil || t.Hub == "" {
		return "", "", false
	}
	return t.Hub, t.Workspace, true
}

func untrustedErr(name string) error {
	return fmt.Errorf("workspace %s is untrusted; run: tq allow %s", name, name)
}

// hubWorkspace is --workspace, else the workspace containing the cwd, else TQ_HUB_TASK's; always trusted.
func hubWorkspace(name string) (*resolve.Workspace, error) {
	if name == "" {
		cfg, err := registry.Load()
		if err != nil {
			return nil, err
		}
		cwd, _ := os.Getwd()
		res := resolve.Resolve(cwd, cfg)
		switch {
		case res.Workspace != nil:
			return res.Workspace, nil
		case res.Untrusted != nil:
			return nil, untrustedErr(res.Untrusted.Name)
		}
		if _, ws, ok := hubCardFromTask(); ok && ws != "" {
			name = ws
		} else {
			return nil, fmt.Errorf("no workspace here (%s); cd into a client workspace or pass --workspace", res.Reason)
		}
	}
	ws, err := findWorkspace(name)
	if err != nil {
		return nil, err
	}
	if !trust.IsTrusted(ws.Hash) {
		return nil, untrustedErr(ws.Name)
	}
	return ws, nil
}

// hubClasses: the manifest's data classes plus secrets; every detector when not classified.
func hubClasses(ws *resolve.Workspace) []string {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() {
		return []string{"all"}
	}
	return append(append([]string(nil), ws.Manifest.Data.Classes...), "secrets")
}

func newHubService(ws *resolve.Workspace, warn func(string), notifyOn bool) (*runner.Service, error) {
	st, err := store.Open(ws.Name, hubClasses(ws))
	if err != nil {
		return nil, err
	}
	env := func() []string { return envplan.Environ(ws, os.Environ()) }
	configDir := envplan.Desired(ws)["CLAUDE_CONFIG_DIR"]
	if configDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(h, ".claude")
		}
	}
	var g gate.API
	if gc, err := gate.ForWorkspace(configDir, env()); err == nil {
		g = gc
	} else {
		g = gate.Unavailable{Err: err}
	}
	bin := []string{"claude"}
	if raw := strings.TrimSpace(os.Getenv("TQ_HUB_CLAUDE_CMD")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &bin); err != nil || len(bin) == 0 {
			st.Close()
			return nil, errors.New("TQ_HUB_CLAUDE_CMD must be a JSON array of strings")
		}
	}
	adapters := map[string]adapter.Adapter{
		store.AdapterClaude: adapter.Claude{Bin: bin, Args: func(a []string) []string { return claudeArgs(ws, a, warn) }},
	}
	if raw, ok, _ := st.Setting(store.SettingProcessCommand); ok {
		var argv []string
		if json.Unmarshal([]byte(raw), &argv) == nil && len(argv) > 0 {
			adapters[store.AdapterProcess] = adapter.Process{Command: argv}
		}
	}
	var notifyFn func(context.Context, string, string) error
	if notifyOn {
		notifyFn = notify.Send
	}
	svc, err := runner.New(runner.Config{Workspace: ws.Name, Root: ws.Root, Store: st, Gate: g, Adapters: adapters, Env: env,
		Notify: notifyFn, Actor: gate.Actor(), GateURL: "http://127.0.0.1:8765/"})
	if err != nil {
		st.Close()
		return nil, err
	}
	svc.OnApprove = func(c store.Card, r store.Run) {
		if _, err := web.ExportReview(context.Background(), svc, c, r); err != nil {
			warn("review export: " + err.Error())
		}
	}
	return svc, nil
}

// openBrowser opens url with the platform's opener; nothing runs through a shell.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}
```

- [ ] **Step 4: Implement the commands**

`internal/cmd/hub.go`:

```go
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

const hubDefaultPort = 7470

type hubFlags struct {
	workspace string
	timeout   time.Duration
}

// hubExtra lets hub_serve.go add `serve` and `status`.
var hubExtra []func(*hubFlags) *cobra.Command

func newHubCmd() *cobra.Command {
	f := &hubFlags{}
	c := &cobra.Command{
		Use:   "hub",
		Short: "A local board that runs agent-ready cards through the build engine (docs/HUB.md)",
		Long: `tq hub keeps one board per client workspace ($TQ_HOME/hub/<workspace>/), runs agent-ready
cards headless through /tentaqles:build with the workspace's own environment, and shows every
decision a run waits on. You answer from the page, from tq hub answer, or in the terminal with
build-gate answer. Commands talk to a running tq hub serve when there is one, else to the store.`,
	}
	c.PersistentFlags().StringVar(&f.workspace, "workspace", "", "workspace name (default: the one containing the current folder)")
	c.PersistentFlags().DurationVar(&f.timeout, "timeout", 0, "give up after this long (skills pass 2s; 0 means no limit)")
	c.AddCommand(newHubAddCmd(f), newHubListCmd(f), newHubShowCmd(f), newHubMoveCmd(f), newHubRunCmd(f), newHubAnswerCmd(f),
		newHubReviewCmd(f), newHubOpenCmd(f), newHubCancelCmd(f), newHubCommentCmd(f))
	for _, mk := range hubExtra {
		c.AddCommand(mk(f))
	}
	return c
}

type hubConn struct {
	ws   *resolve.Workspace
	ops  runner.Ops
	svc  *runner.Service // direct mode
	info *serveinfo.Info // a hub serves this workspace
}

func (h *hubConn) close() {
	if h.svc != nil {
		h.svc.Store().Close()
	}
}

func warnTo(c *cobra.Command) func(string) {
	return func(s string) { fmt.Fprintln(c.ErrOrStderr(), "warning:", s) }
}

func openHub(c *cobra.Command, f *hubFlags) (*hubConn, context.Context, func(), error) {
	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cancel := func() {}
	if f.timeout > 0 {
		var cf context.CancelFunc
		ctx, cf = context.WithTimeout(ctx, f.timeout)
		cancel = cf
	}
	if hubAgent() {
		ctx = runner.WithAgent(ctx)
	}
	ws, err := hubWorkspace(f.workspace)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	if info, ok := serveinfo.Find(ctx, ws.Name); ok {
		return &hubConn{ws: ws, ops: web.NewClient(info, ws.Name, hubAgent()), info: &info}, ctx, cancel, nil
	}
	svc, err := newHubService(ws, warnTo(c), true)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return &hubConn{ws: ws, ops: svc, svc: svc}, ctx, cancel, nil
}

func (h *hubConn) url(path string) (string, bool) {
	base := fmt.Sprintf("http://127.0.0.1:%d", hubDefaultPort)
	if h.info != nil {
		base = serveinfo.BaseURL(*h.info)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return base + path + sep + "ws=" + url.QueryEscape(h.ws.Name), h.info != nil
}

func printJSON(c *cobra.Command, v any) error {
	enc := json.NewEncoder(c.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newHubAddCmd(f *hubFlags) *cobra.Command {
	var body, bodyFile, kind, lane, parent, project, budget string
	var tags []string
	var asJSON bool
	c := &cobra.Command{
		Use:   `add "<title>"`,
		Short: "Add a card (agents may add cards; setting a budget is human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if budget != "" && hubAgent() {
				return runner.ErrHumanOnly
			}
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			if bodyFile != "" {
				raw, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				body = string(raw)
			}
			var b *store.Budget
			if budget != "" {
				bb, err := store.ParseBudget(budget, store.DefaultBudget)
				if err != nil {
					return err
				}
				b = &bb
			}
			card, err := h.ops.Add(ctx, store.NewCard{Title: args[0], Body: body, Kind: kind, Lane: lane, Tags: tags, ParentID: parent, ProjectDir: project}, b)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, card)
			}
			fmt.Fprintf(c.OutOrStdout(), "%s  %s  (%s, %s)\n", card.ID, card.Title, card.Kind, card.Lane)
			return nil
		},
	}
	c.Flags().StringVar(&body, "body", "", "card body (markdown)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read the body from a file")
	c.Flags().StringVar(&kind, "kind", "feature", "project | feature | fix | analysis | hotfix | chore")
	c.Flags().StringVar(&lane, "lane", "", "lane name (default: backlog)")
	c.Flags().StringArrayVar(&tags, "tag", nil, "tag (repeatable; agent-ready makes the card run when it enters in progress)")
	c.Flags().StringVar(&parent, "parent", "", "parent card id")
	c.Flags().StringVar(&project, "project", "", "project folder relative to the workspace root (the run's folder)")
	c.Flags().StringVar(&budget, "budget", "", "per-run budget: tokens=…,usd=…,minutes=…,rounds=…")
	c.Flags().BoolVar(&asJSON, "json", false, "print the card as JSON")
	return c
}

func newHubListCmd(f *hubFlags) *cobra.Command {
	var lane, kind, tag string
	var waiting, asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List cards, or with --waiting every decision waiting on you",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			if waiting {
				items, err := h.ops.Waiting(ctx)
				if err != nil {
					return err
				}
				if asJSON {
					return printJSON(c, items)
				}
				if len(items) == 0 {
					fmt.Fprintln(c.OutOrStdout(), "nothing is waiting on you")
				}
				for _, it := range items {
					fmt.Fprintf(c.OutOrStdout(), "%s  %s  %-13s %s: %s\n    options: %s   answer: tq hub answer %s %s --choice <option>\n",
						it.Card.ID, it.Decision.ID, it.Decision.Kind, it.Card.Title, firstLine(it.Decision.Text),
						strings.Join(it.Decision.Options, " | "), it.Card.ID, it.Decision.ID)
				}
				return nil
			}
			views, err := h.ops.List(ctx, store.CardFilter{Lane: lane, Kind: kind, Tag: tag}, false)
			if err != nil {
				return err
			}
			if asJSON {
				if views == nil {
					views = []runner.CardView{}
				}
				return printJSON(c, views)
			}
			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "card\tlane\tkind\trun\twaiting\tcost\ttitle")
			for _, v := range views {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t$%.2f\t%s\n", v.ID, v.Lane, v.Kind, dash(v.RunStatus), v.Waiting, v.CostUSD, v.Title)
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&lane, "lane", "", "only this lane")
	c.Flags().StringVar(&kind, "kind", "", "only this kind")
	c.Flags().StringVar(&tag, "tag", "", "only cards with this tag")
	c.Flags().BoolVar(&waiting, "waiting", false, "list the decisions waiting on you instead of cards")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

func newHubShowCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <card>",
		Short: "Show a card: body, decisions, runs, comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			d, err := h.ops.Show(ctx, args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, d)
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "%s  %s\n%s in %s", d.ID, d.Title, d.Kind, d.Lane)
			if d.ProjectDir != "" {
				fmt.Fprintf(w, " · project %s", d.ProjectDir)
			}
			if len(d.Tags) > 0 {
				fmt.Fprintf(w, " · tags %s", strings.Join(d.Tags, ", "))
			}
			fmt.Fprintf(w, " · cost $%.2f\n\n%s\n", d.CostUSD, strings.TrimSpace(store.StripFrontMatter(d.Body)))
			for _, dec := range d.Decisions {
				if dec.Status == store.DecisionPending {
					fmt.Fprintf(w, "\nwaiting on you: %s %s: %s\n  options: %s\n  answer: tq hub answer %s %s --choice <option>\n",
						dec.Kind, dec.ID, firstLine(dec.Text), strings.Join(dec.Options, " | "), d.ID, dec.ID)
				}
			}
			for _, r := range d.Runs {
				fmt.Fprintf(w, "\nrun %s %s: %d rounds, $%.2f", r.ID, r.Status, r.Rounds, r.CostUSD)
				if r.StopReason != "" {
					fmt.Fprintf(w, ", %s", r.StopReason)
				}
			}
			if len(d.Runs) > 0 {
				fmt.Fprintln(w)
			}
			start := 0
			if len(d.Comments) > 5 {
				start = len(d.Comments) - 5
			}
			for _, cm := range d.Comments[start:] {
				fmt.Fprintf(w, "\n[%s] %s: %s\n", cm.AuthorKind, cm.Author, cm.Body)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newHubMoveCmd(f *hubFlags) *cobra.Command {
	var cancelRun bool
	c := &cobra.Command{
		Use:   `move <card> "<lane>"`,
		Short: "Move a card; into an active lane an agent-ready card starts a run",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			out, err := h.ops.Move(ctx, args[0], args[1], cancelRun)
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			if out.Cancelled != nil {
				fmt.Fprintf(w, "cancelled run %s\n", out.Cancelled.ID)
			}
			fmt.Fprintf(w, "moved %s to %s\n", out.Card.ID, out.Card.Lane)
			if out.Enqueued != nil {
				fmt.Fprintf(w, "queued run %s\n", out.Enqueued.ID)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&cancelRun, "cancel-run", false, "cancel the card's running or waiting run to move it")
	return c
}

func newHubRunCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "run <card>",
		Short: "Enqueue (or resume) a run for the card now",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			r, err := h.ops.Run(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "run %s %s\n", r.ID, r.Status)
			if h.svc != nil {
				fmt.Fprintln(c.OutOrStdout(), "no hub is serving this workspace: start one with tq hub serve to run it")
			}
			return nil
		},
	}
}

func newHubAnswerCmd(f *hubFlags) *cobra.Command {
	var choice, note string
	c := &cobra.Command{
		Use:   "answer <card> <decision> --choice C [--note TEXT]",
		Short: "Answer a decision a run is waiting on (human only; signed with a hub token)",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if hubAgent() {
				return runner.ErrHumanOnly
			}
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			out, err := h.ops.Answer(ctx, args[0], args[1], choice, note)
			if err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), out.Message)
			if out.Resumed != nil {
				fmt.Fprintf(c.OutOrStdout(), "resumed run %s\n", out.Resumed.ID)
			}
			return nil
		},
	}
	c.Flags().StringVar(&choice, "choice", "", "one of the decision's options")
	c.Flags().StringVar(&note, "note", "", "note recorded with the answer")
	_ = c.MarkFlagRequired("choice")
	return c
}

func newHubReviewCmd(f *hubFlags) *cobra.Command {
	var open, html bool
	c := &cobra.Command{
		Use:   "review <card> [--open] [--html]",
		Short: "Print the review page URL, open it, or render it (--html) from gate records",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			if html {
				svc := h.svc
				if svc == nil {
					if svc, err = newHubService(h.ws, warnTo(c), false); err != nil {
						return err
					}
					defer svc.Store().Close()
				}
				d, err := web.BuildReview(ctx, svc, args[0])
				if err != nil {
					return err
				}
				d.Standalone = true
				return web.RenderReview(c.OutOrStdout(), d)
			}
			u, serving := h.url("/cards/" + url.PathEscape(args[0]) + "/review")
			fmt.Fprintln(c.OutOrStdout(), u)
			if !serving {
				fmt.Fprintf(c.ErrOrStderr(), "no hub is serving %s; start one with: tq hub serve\n", h.ws.Name)
			}
			if open && serving {
				return openBrowser(u)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&open, "open", false, "open the page in the browser")
	c.Flags().BoolVar(&html, "html", false, "render the review page to stdout (standalone HTML)")
	return c
}

func newHubOpenCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "open [<card>]",
		Short: "Open the board or a card in the browser",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, _, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			path := "/"
			if len(args) == 1 {
				path = "/cards/" + url.PathEscape(args[0])
			}
			u, serving := h.url(path)
			if !serving {
				return fmt.Errorf("no hub is serving %s; start one with: tq hub serve", h.ws.Name)
			}
			fmt.Fprintln(c.OutOrStdout(), u)
			return openBrowser(u)
		},
	}
}

func newHubCancelCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run>",
		Short: "Cancel a run (the gate state stays; tq hub run resumes it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			r, err := h.ops.Cancel(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "run %s %s\n", r.ID, r.Status)
			return nil
		},
	}
}

func newHubCommentCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   `comment <card|-> "<text>"`,
		Short: "Comment on a card (- is the card in TQ_HUB_TASK; ship posts the PR link this way)",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			card := args[0]
			if card == "-" {
				id, _, ok := hubCardFromTask()
				if !ok {
					return errors.New("tq hub comment -: TQ_HUB_TASK is not set (this session was not launched by the hub)")
				}
				card = id
			}
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			cm, err := h.ops.Comment(ctx, card, args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "commented on %s (%s)\n", card, cm.AuthorKind)
			return nil
		},
	}
}
```

In `internal/cmd/root.go`, append `newHubCmd()` to the `root.AddCommand(...)` list (after `newUninstallCmd()`):

```go
	root.AddCommand(newVersionCmd(), newEnvCmd(), newActivateCmd(), newInitCmd(), newAddCmd(), newAllowCmd(), newDenyCmd(), newListCmd(), newRunCmd(), newLoginCmd(), newDoctorCmd(), newBundleCmd(), newProvidersCmd(), newHooksCmd(), newSetupCmd(), newClaudeHookCmd(), newSecretsCmd(), newSettingsCmd(), newStatuslineCmd(), newWorktreesCmd(), newInsightsCmd(), newDecideCmd(), newDotenvCmd(), newRedactCmd(), newGuardCmd(), newMigrateCmd(), newUninstallCmd(), newHubCmd())
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go vet ./internal/cmd/; go test ./internal/cmd/ -run 'TestHub|TestCLIMove' -count=1 -v`
Expected: PASS. Then `go test ./internal/cmd/ -count=1` stays green.

- [ ] **Step 6: Commit**

```bash
git add internal/cmd/hub.go internal/cmd/hub_service.go internal/cmd/root.go internal/cmd/hub_test.go
git commit -m "feat(hub): tq hub add, list, show, move, run, answer, review, open, cancel and comment"
```

---
### Task 13: `tq hub serve` and `tq hub status`: the topology switch, untrusted listing, port-busy message, LAN token

**Files:**
- Create: `internal/hub/web/backend.go`, `internal/cmd/hub_serve.go`
- Modify: `internal/registry/registry.go` (`Config.Hub`)
- Test: `internal/hub/web/backend_test.go`, `internal/cmd/hub_serve_test.go`, `internal/registry/registry_hub_test.go`

**Interfaces:**
- Consumes: Task 10 (`Backend`, `WorkspaceInfo`, `ErrUnavailable`, `ErrUnknownWorkspace`, `New`, `Options`, `HashToken`, `serveinfo.*`), Task 12 (`hubFlags`, `hubExtra`, `hubWorkspace`, `newHubService`, `warnTo`, `hubDefaultPort`), `resolve.ListWorkspaces`, `resolve.Resolve`, `trust.IsTrusted`, `registry.Load`.
- Produces:
  - `registry.HubConfig{ Mode string; Port int }` (YAML `hub: {mode: all, port: 7470}`), `registry.Config.Hub`; `Save` keeps it.
  - Package `web`: `type Entry struct { Info WorkspaceInfo; WS *resolve.Workspace }`; `type Factory func(ws *resolve.Workspace) (*runner.Service, error)`; `NewMultiBackend(ctx context.Context, entries []Entry, f Factory) *MultiBackend` (implements `Backend`; opens a workspace on first use and starts its runner; never calls the factory for an unavailable entry); `(*MultiBackend).Opened() []string`, `Close()`; `DiscoverAll(cfg *registry.Config) []Entry` (trusted → available, untrusted → `untrusted (run: tq allow <name>)`); `DiscoverCwd(cwd string, cfg *registry.Config) ([]Entry, error)`.
  - Package `cmd`: `newHubServeCmd(*hubFlags)`, `newHubStatusCmd(*hubFlags)` (registered through `hubExtra`); `servedElsewhere(ctx, entries []web.Entry, mode string) ([]web.Entry, error)` (a workspace already served by another live hub is refused in single mode and unavailable in `--all`, so two hubs never run one queue); `serveToken(host, token string) (string, bool)` (`true` when a token was generated and must be printed once), `isLoopback(host string) bool`, `allowedHosts(host string, port int) []string`, `portBusyError(ctx context.Context, port int) error`.
  - Hidden flag `tq hub serve --drain`: run every queued run of the served workspaces to its next stop, then exit (scripts, the integration test).

- [ ] **Step 1: Write the failing tests**

`internal/registry/registry_hub_test.go`:

```go
package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

func TestHubSettingsSurviveSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(paths.Home(), 0o700)
	os.WriteFile(paths.Config(), []byte("bases: []\nhub:\n  mode: all\n  port: 7471\n"), 0o600)
	cfg, err := Load()
	if err != nil || cfg.Hub.Mode != "all" || cfg.Hub.Port != 7471 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if _, err := cfg.AddBase(home); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(paths.Config())
	if !strings.Contains(string(raw), "mode: all") || !strings.Contains(string(raw), "port: 7471") {
		t.Fatalf("Save dropped the hub block:\n%s", raw)
	}
	empty := &Config{}
	empty.Save()
	raw, _ = os.ReadFile(paths.Config())
	if strings.Contains(string(raw), "hub:") {
		t.Fatalf("an empty hub block is omitted:\n%s", raw)
	}
}
```

`internal/hub/web/backend_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// fourWorkspaces registers acme, globex and initech (trusted) and evil (untrusted) under one base.
func fourWorkspaces(t *testing.T) (*registry.Config, string) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	base := filepath.Join(home, "work")
	cfg := &registry.Config{}
	os.MkdirAll(base, 0o755)
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"acme", "globex", "initech", "evil"} {
		root := filepath.Join(base, name)
		os.MkdirAll(root, 0o755)
		mp := filepath.Join(root, manifest.FileName)
		os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: "+name+"\nidentities: { gh: {} }\n"), 0o600)
		if name != "evil" {
			h, _ := trust.HashFile(mp)
			trust.Allow(h)
		}
	}
	return cfg, base
}

func TestServeAllUntrustedNeverOpened(t *testing.T) {
	cfg, _ := fourWorkspaces(t)
	entries := DiscoverAll(cfg)
	if len(entries) != 4 {
		t.Fatalf("%+v", entries)
	}
	var opened int32
	g := &webGate{states: map[string]*gate.State{}}
	factory := func(ws *resolve.Workspace) (*runner.Service, error) {
		atomic.AddInt32(&opened, 1)
		if ws.Name == "evil" {
			t.Errorf("the factory was called for the untrusted workspace")
		}
		st, err := store.Open(ws.Name, []string{"all"})
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { st.Close() })
		svc, err := runner.New(runner.Config{Workspace: ws.Name, Root: ws.Root, Store: st, Gate: g,
			Adapters: map[string]adapter.Adapter{store.AdapterClaude: nopAdapter{}}, Actor: "hub:tester"})
		if err == nil {
			c, _ := svc.Add(context.Background(), store.NewCard{Title: ws.Name + " card"}, nil)
			st.AskHub(c.ID, "", "", "Raise the budget for "+ws.Name+"?", []string{"raise-and-resume", "stop"}, nil)
		}
		return svc, err
	}
	b := NewMultiBackend(context.Background(), entries, factory)
	defer b.Close()
	var evil WorkspaceInfo
	for _, i := range b.Workspaces() {
		if i.Name == "evil" {
			evil = i
		}
	}
	if evil.Available || !strings.Contains(evil.Reason, "tq allow evil") {
		t.Fatalf("%+v", evil)
	}
	if atomic.LoadInt32(&opened) != 0 {
		t.Fatal("databases open lazily, on first request")
	}
	srv, err := New(Options{Backend: b, Mode: "all"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptestServer(t, srv)
	code, body, _ := get(t, ts, "/waiting", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, ws := range []string{"acme", "globex", "initech"} {
		if !strings.Contains(body, `class="ws-label">`+ws) || !strings.Contains(body, "Raise the budget for "+ws) {
			t.Fatalf("waiting lacks %s:\n%s", ws, body)
		}
	}
	if strings.Contains(body, `class="ws-label">evil`) || !strings.Contains(body, "evil (unavailable: untrusted (run: tq allow evil))") {
		t.Fatal("evil is listed as unavailable in the switcher and never in the waiting list")
	}
	for _, path := range []string{"/?ws=evil", "/cards/c-aaaaaaaaaaaa?ws=evil", "/api/cards?ws=evil"} {
		code, body, _ := get(t, ts, path, nil)
		if code != http.StatusForbidden || !strings.Contains(body, "tq allow evil") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	if _, err := os.Stat(store.Dir("evil")); !os.IsNotExist(err) {
		t.Fatal("no hub folder was created for the untrusted workspace")
	}
	if got := b.Opened(); len(got) != 3 {
		t.Fatalf("opened %v", got)
	}
}

func httptestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestDiscoverCwd(t *testing.T) {
	cfg, base := fourWorkspaces(t)
	es, err := DiscoverCwd(filepath.Join(base, "acme"), cfg)
	if err != nil || len(es) != 1 || es[0].Info.Name != "acme" || !es[0].Info.Available {
		t.Fatalf("%+v %v", es, err)
	}
	if _, err := DiscoverCwd(filepath.Join(base, "evil"), cfg); err == nil || !strings.Contains(err.Error(), "tq allow evil") {
		t.Fatalf("untrusted cwd: %v", err)
	}
	if _, err := DiscoverCwd(base, cfg); err == nil {
		t.Fatal("the base folder itself is no workspace")
	}
}
```

`internal/cmd/hub_serve_test.go`:

```go
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *lockedBuf) String() string               { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestServePortBusyNamesTheRunningHub(t *testing.T) {
	hubWS(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer ln.Close()
	serveinfo.Write(serveinfo.Info{PID: 4242, Addr: ln.Addr().String(), Port: port, Mode: "single", Workspaces: []string{"globex"}, LocalKey: "k"})
	defer serveinfo.Remove(port)
	_, _, _, err = runTQ(t, "hub", "serve", "--port", itoa(port))
	if err == nil {
		t.Fatal("a second hub on the same port must not start")
	}
	for _, want := range []string{"running tq hub", "pid 4242", "globex", "--all", "--port"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message lacks %q: %v", want, err)
		}
	}
	serveinfo.Remove(port)
	_, _, _, err = runTQ(t, "hub", "serve", "--port", itoa(port))
	if err == nil || !strings.Contains(err.Error(), "another program") {
		t.Fatalf("plain port conflict: %v", err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestServeWritesItsRecordAndStops(t *testing.T) {
	hubWS(t)
	port := freePort(t)
	var out lockedBuf
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"hub", "serve", "--port", itoa(port), "--no-notify"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	deadline := time.Now().Add(10 * time.Second)
	var info serveinfo.Info
	var ok bool
	for time.Now().Before(deadline) {
		if info, ok = serveinfo.FindByPort(context.Background(), port); ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok || info.Mode != "single" || !info.Serves("acme") {
		cancel()
		t.Fatalf("no live serve record: %+v\n%s", info, out.String())
	}
	if !strings.Contains(out.String(), "serving acme on http://127.0.0.1:") {
		t.Fatal(out.String())
	}
	if _, _, _, err := runTQ(t, "hub", "status"); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
	if _, err := os.Stat(serveinfo.Path(port)); !os.IsNotExist(err) {
		t.Fatal("the serve record is removed on exit")
	}
}

func TestServeDrainRunsQueuedWorkAndHonoursHubMode(t *testing.T) {
	py := testutil.Python(t)
	fake, _ := filepath.Abs(filepath.Join("..", "hub", "adapter", "testdata", "fake_claude.py")) // before hubWS changes the cwd
	root := hubWS(t)
	dir := t.TempDir()
	script, _ := json.Marshal(map[string]any{"counter": filepath.Join(dir, "n"), "argv_log": filepath.Join(dir, "argv.jsonl"),
		"rounds": []map[string]any{{"result": "I looked around.", "cost": 0.1}}})
	os.WriteFile(filepath.Join(dir, "script.json"), script, 0o600)
	t.Setenv("FAKE_CLAUDE_SCRIPT", filepath.Join(dir, "script.json"))
	argv, _ := json.Marshal(append(py, fake))
	t.Setenv("TQ_HUB_CLAUDE_CMD", string(argv))
	c := addCard(t, "x", "--tag", "agent-ready", "--project", "apps/billing")
	if _, _, _, err := runTQ(t, "hub", "move", c.ID, "in progress"); err != nil {
		t.Fatal(err)
	}
	_, out, errOut, err := runTQ(t, "hub", "serve", "--drain", "--no-notify")
	if err != nil || !strings.Contains(out, "drained 1 workspace(s) (mode single)") {
		t.Fatalf("%v\n%s\n%s", err, out, errOut)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	runs, _ := st.RunsForCard(c.ID)
	st.Close()
	if len(runs) != 1 || runs[0].Status != store.StatusFailed || runs[0].StopReason != "ended without a stop" || runs[0].CostUSD != 0.1 {
		t.Fatalf("%+v", runs)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "argv.jsonl"))
	var call map[string]any
	if err := json.Unmarshal(bytes.SplitN(raw, []byte("\n"), 2)[0], &call); err != nil {
		t.Fatalf("argv log %q: %v", raw, err)
	}
	if cwd, _ := call["cwd"].(string); !strings.EqualFold(filepath.Clean(cwd), filepath.Join(root, "apps", "billing")) {
		t.Fatalf("the round ran in %q, want the card's project folder", cwd)
	}
	if call["headless"] != "1" || call["build_token"] != false {
		t.Fatalf("launch environment %v", call)
	}
	cfg, _ := registry.Load()
	cfg.Hub.Mode = "all"
	cfg.Save()
	_, out, _, err = runTQ(t, "hub", "serve", "--drain", "--no-notify")
	if err != nil || !strings.Contains(out, "(mode all)") {
		t.Fatalf("hub.mode all in config.yaml: %v %s", err, out)
	}
}

func TestASecondHubForTheSameWorkspaceIsRefused(t *testing.T) {
	hubWS(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer ln.Close()
	serveinfo.Write(serveinfo.Info{PID: 4243, Addr: ln.Addr().String(), Port: port, Mode: "single", Workspaces: []string{"acme"}, LocalKey: "k"})
	defer serveinfo.Remove(port)
	_, _, _, err = runTQ(t, "hub", "serve", "--drain", "--no-notify")
	if err == nil || !strings.Contains(err.Error(), "already served by the hub on port "+itoa(port)) {
		t.Fatalf("two hubs must never run one queue: %v", err)
	}
}

func TestServeTokenAndHosts(t *testing.T) {
	if tok, gen := serveToken("127.0.0.1", ""); tok != "" || gen {
		t.Fatal("loopback needs no token")
	}
	if tok, gen := serveToken("192.168.1.20", ""); len(tok) < 24 || !gen {
		t.Fatalf("a LAN bind generates a token: %q %v", tok, gen)
	}
	if tok, gen := serveToken("0.0.0.0", "mine"); tok != "mine" || gen {
		t.Fatal("--token is kept and not reprinted")
	}
	if !isLoopback("localhost") || !isLoopback("::1") || isLoopback("10.0.0.1") {
		t.Fatal("isLoopback")
	}
	hosts := allowedHosts("127.0.0.1", 7470)
	if len(hosts) == 0 || hosts[0] != "127.0.0.1:7470" {
		t.Fatal(hosts)
	}
	if allowedHosts("0.0.0.0", 7470) != nil {
		t.Fatal("a wildcard bind accepts any Host; the token protects it")
	}
	if got := allowedHosts("192.168.1.20", 7470); got[len(got)-1] != "192.168.1.20:7470" {
		t.Fatal(got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/registry/ ./internal/hub/web/ ./internal/cmd/ -run 'TestHubSettingsSurviveSave|TestServe|TestDiscoverCwd' -count=1`
Expected: FAIL, `cfg.Hub undefined`, `undefined: DiscoverAll`, `unknown command "serve" for "tq hub"`.

- [ ] **Step 3: Implement**

In `internal/registry/registry.go`, replace the `Config` type:

```go
// HubConfig is the `hub:` block of config.yaml (tq hub serve's defaults).
type HubConfig struct {
	Mode string `yaml:"mode,omitempty"` // "all" serves every trusted workspace by default
	Port int    `yaml:"port,omitempty"`
}

type Config struct {
	Bases []string  `yaml:"bases"`
	Hub   HubConfig `yaml:"hub,omitempty"`
}
```

`internal/hub/web/backend.go`:

```go
package web

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

type Entry struct {
	Info WorkspaceInfo
	WS   *resolve.Workspace
}

type Factory func(ws *resolve.Workspace) (*runner.Service, error)

// MultiBackend serves several workspaces from one process (spec §5): one store and one runner per
// workspace, opened on first use; an unavailable workspace is listed and never opened.
type MultiBackend struct {
	ctx     context.Context
	cancel  context.CancelFunc
	entries []Entry
	factory Factory
	mu      sync.Mutex
	svcs    map[string]*runner.Service
	done    map[string]chan struct{}
}

func NewMultiBackend(ctx context.Context, entries []Entry, f Factory) *MultiBackend {
	cctx, cancel := context.WithCancel(ctx)
	es := append([]Entry(nil), entries...)
	sort.Slice(es, func(i, j int) bool { return es[i].Info.Name < es[j].Info.Name })
	return &MultiBackend{ctx: cctx, cancel: cancel, entries: es, factory: f, svcs: map[string]*runner.Service{}, done: map[string]chan struct{}{}}
}

func (b *MultiBackend) Workspaces() []WorkspaceInfo {
	out := make([]WorkspaceInfo, len(b.entries))
	for i, e := range b.entries {
		out[i] = e.Info
	}
	return out
}

func (b *MultiBackend) Service(name string) (*runner.Service, error) {
	var entry *Entry
	for i := range b.entries {
		if b.entries[i].Info.Name == name {
			entry = &b.entries[i]
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("%w %q", ErrUnknownWorkspace, name)
	}
	if !entry.Info.Available {
		return nil, fmt.Errorf("%w: %s is %s", ErrUnavailable, name, entry.Info.Reason)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if svc, ok := b.svcs[name]; ok {
		return svc, nil
	}
	svc, err := b.factory(entry.WS)
	if err != nil {
		return nil, err
	}
	b.svcs[name] = svc
	done := make(chan struct{})
	b.done[name] = done
	go func() {
		defer close(done)
		svc.Start(b.ctx)
	}()
	return svc, nil
}

func (b *MultiBackend) Opened() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.svcs))
	for n := range b.svcs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Close stops every runner (cancelling live rounds) and closes the stores.
func (b *MultiBackend) Close() {
	b.cancel()
	b.mu.Lock()
	defer b.mu.Unlock()
	for name, svc := range b.svcs {
		<-b.done[name]
		svc.Store().Close()
	}
	b.svcs = map[string]*runner.Service{}
}

// DiscoverAll lists every workspace under the registered bases; untrusted ones are unavailable.
func DiscoverAll(cfg *registry.Config) []Entry {
	all, _ := resolve.ListWorkspaces(cfg)
	out := make([]Entry, 0, len(all))
	for i := range all {
		w := all[i]
		info := WorkspaceInfo{Name: w.Name, Available: trust.IsTrusted(w.Hash)}
		if !info.Available {
			info.Reason = "untrusted (run: tq allow " + w.Name + ")"
		}
		out = append(out, Entry{Info: info, WS: &w})
	}
	return out
}

// DiscoverCwd is the default topology: the trusted workspace containing cwd.
func DiscoverCwd(cwd string, cfg *registry.Config) ([]Entry, error) {
	res := resolve.Resolve(cwd, cfg)
	switch {
	case res.Workspace != nil:
		return []Entry{{Info: WorkspaceInfo{Name: res.Workspace.Name, Available: true}, WS: res.Workspace}}, nil
	case res.Untrusted != nil:
		return nil, fmt.Errorf("workspace %s is untrusted; run: tq allow %s", res.Untrusted.Name, res.Untrusted.Name)
	}
	return nil, fmt.Errorf("no workspace here (%s); cd into a client workspace, pass --workspace, or use tq hub serve --all", res.Reason)
}
```

`internal/cmd/hub_serve.go`:

```go
package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

func init() { hubExtra = append(hubExtra, newHubServeCmd, newHubStatusCmd) }

func randomKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isWildcard(host string) bool { return host == "0.0.0.0" || host == "::" || host == "[::]" }

// serveToken: a non-loopback bind requires a token; one is generated (and printed once) when none was given.
func serveToken(host, token string) (string, bool) {
	if token != "" || isLoopback(host) {
		return token, false
	}
	return randomKey() + randomKey()[:8], true
}

// allowedHosts guards against DNS rebinding; a wildcard bind accepts any Host (the token protects it).
func allowedHosts(host string, port int) []string {
	if isWildcard(host) {
		return nil
	}
	p := strconv.Itoa(port)
	hosts := []string{"127.0.0.1:" + p, "localhost:" + p, "[::1]:" + p}
	if !isLoopback(host) {
		hosts = append(hosts, strings.ToLower(net.JoinHostPort(strings.Trim(host, "[]"), p)))
	}
	return hosts
}

func addrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	m := err.Error()
	return strings.Contains(m, "address already in use") || strings.Contains(m, "Only one usage of each socket address")
}

func portBusyError(ctx context.Context, port int) error {
	if info, ok := serveinfo.FindByPort(ctx, port); ok {
		return fmt.Errorf("port %d is in use by a running tq hub (pid %d, mode %s, workspaces %s). Use `tq hub serve --all` to serve several workspaces from one hub, or `--port N` for another one",
			port, info.PID, info.Mode, strings.Join(info.Workspaces, ", "))
	}
	return fmt.Errorf("port %d is in use by another program; pass --port N (or set hub.port in %s)", port, paths.Config())
}

// servedElsewhere keeps one runner per workspace: a workspace another live hub already serves is
// refused (single mode) or listed as unavailable (--all), so two hubs never run the same queue.
func servedElsewhere(ctx context.Context, entries []web.Entry, mode string) ([]web.Entry, error) {
	out := append([]web.Entry(nil), entries...)
	for i := range out {
		if !out[i].Info.Available {
			continue
		}
		if info, ok := serveinfo.Find(ctx, out[i].Info.Name); ok {
			if mode == "single" {
				return nil, fmt.Errorf("workspace %s is already served by the hub on port %d (pid %d); open it with tq hub open, or stop that hub first",
					out[i].Info.Name, info.Port, info.PID)
			}
			out[i].Info.Available = false
			out[i].Info.Reason = fmt.Sprintf("served by the hub on port %d", info.Port)
		}
	}
	return out, nil
}

func cmdCtx(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func newHubServeCmd(f *hubFlags) *cobra.Command {
	var all, noNotify, drain bool
	var port int
	var bind, token string
	c := &cobra.Command{
		Use:   "serve [--all] [--port N] [--bind ADDR] [--token T] [--no-notify]",
		Short: "Serve the board on 127.0.0.1:7470 for this workspace, or every trusted one with --all",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			mode := "single"
			if all || strings.EqualFold(cfg.Hub.Mode, "all") {
				mode = "all"
			}
			if port == 0 {
				port = cfg.Hub.Port
			}
			if port == 0 {
				port = hubDefaultPort
			}
			var entries []web.Entry
			switch {
			case mode == "all":
				entries = web.DiscoverAll(cfg)
			case f.workspace != "":
				ws, err := hubWorkspace(f.workspace)
				if err != nil {
					return err
				}
				entries = []web.Entry{{Info: web.WorkspaceInfo{Name: ws.Name, Available: true}, WS: ws}}
			default:
				cwd, _ := os.Getwd()
				if entries, err = web.DiscoverCwd(cwd, cfg); err != nil {
					return err
				}
			}
			ctx, stop := signal.NotifyContext(cmdCtx(c), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if entries, err = servedElsewhere(ctx, entries, mode); err != nil {
				return err
			}
			warn := warnTo(c)
			out := c.OutOrStdout()
			host := bind
			if host == "" {
				host = "127.0.0.1"
			}
			tok, generated := serveToken(host, token)
			tokenHash := ""
			if tok != "" {
				tokenHash = web.HashToken(tok)
			}
			factory := func(ws *resolve.Workspace) (*runner.Service, error) {
				svc, err := newHubService(ws, warn, !noNotify)
				if err == nil && tokenHash != "" {
					_ = svc.Store().SetSetting(store.SettingWebTokenHash, tokenHash)
				}
				return svc, err
			}
			if drain {
				n := 0
				for _, e := range entries {
					if !e.Info.Available {
						continue
					}
					svc, err := factory(e.WS)
					if err != nil {
						return err
					}
					err = svc.Drain(ctx)
					svc.Store().Close()
					if err != nil {
						return err
					}
					n++
				}
				fmt.Fprintf(out, "drained %d workspace(s) (mode %s)\n", n, mode)
				return nil
			}
			ln, err := net.Listen("tcp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
			if err != nil {
				if addrInUse(err) {
					return portBusyError(ctx, port)
				}
				return err
			}
			actual := ln.Addr().(*net.TCPAddr).Port
			backend := web.NewMultiBackend(ctx, entries, factory)
			defer backend.Close()
			var served, unavailable []string
			for _, e := range entries {
				if !e.Info.Available {
					unavailable = append(unavailable, e.Info.Name+" ("+e.Info.Reason+")")
					continue
				}
				served = append(served, e.Info.Name)
				if _, err := os.Stat(store.DBPath(e.Info.Name)); mode == "single" || err == nil {
					if _, err := backend.Service(e.Info.Name); err != nil {
						warn(e.Info.Name + ": " + err.Error())
					}
				}
			}
			localKey := randomKey()
			srv, err := web.New(web.Options{Backend: backend, Mode: mode, TokenHash: tokenHash, LocalKey: localKey, AllowedHosts: allowedHosts(host, actual)})
			if err != nil {
				ln.Close()
				return err
			}
			if err := serveinfo.Write(serveinfo.Info{PID: os.Getpid(), Addr: ln.Addr().String(), Port: actual, Mode: mode,
				Workspaces: served, Unavailable: unavailable, LocalKey: localKey, StartedAt: store.Now()}); err != nil {
				warn("serve record: " + err.Error())
			}
			defer serveinfo.Remove(actual)
			shown := host
			if isWildcard(host) {
				shown = "127.0.0.1"
			}
			fmt.Fprintf(out, "tq hub (%s) serving %s on http://%s/\n", mode, strings.Join(served, ", "), net.JoinHostPort(strings.Trim(shown, "[]"), strconv.Itoa(actual)))
			for _, u := range unavailable {
				fmt.Fprintf(out, "unavailable: %s\n", u)
			}
			if generated {
				fmt.Fprintf(out, "token (shown once; browsers on other machines open /login?token=<token> once): %s\n", tok)
			}
			hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			go func() {
				<-ctx.Done()
				sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = hs.Shutdown(sctx)
			}()
			if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "serve every registered, trusted workspace (or set hub.mode: all in config.yaml)")
	c.Flags().IntVar(&port, "port", 0, "port (default 7470, or hub.port in config.yaml)")
	c.Flags().StringVar(&bind, "bind", "", "address to bind (default 127.0.0.1; any other bind requires a token)")
	c.Flags().StringVar(&token, "token", "", "token for a non-loopback bind (generated and printed once when omitted)")
	c.Flags().BoolVar(&noNotify, "no-notify", false, "no desktop notifications")
	c.Flags().BoolVar(&drain, "drain", false, "run every queued run to its next stop, then exit (no web server)")
	_ = c.Flags().MarkHidden("drain")
	return c
}

func newHubStatusCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show running hubs: mode, port, workspaces and counts",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := cmdCtx(c)
			out := c.OutOrStdout()
			live := 0
			for _, info := range serveinfo.ReadAll() {
				if !serveinfo.Ping(ctx, info) {
					fmt.Fprintf(out, "stale record for port %d (pid %d): no hub answers; it is removed when a hub starts there\n", info.Port, info.PID)
					continue
				}
				live++
				fmt.Fprintf(out, "hub on %s  pid %d  mode %s\n", serveinfo.BaseURL(info), info.PID, info.Mode)
				for _, ws := range info.Workspaces {
					cl := web.NewClient(info, ws, false)
					views, err := cl.List(ctx, store.CardFilter{}, false)
					if err != nil {
						fmt.Fprintf(out, "  %s: %v\n", ws, err)
						continue
					}
					running, waiting := 0, 0
					for _, v := range views {
						if v.RunStatus == store.StatusRunning {
							running++
						}
						waiting += v.Waiting
					}
					fmt.Fprintf(out, "  %s: %d cards, %d running, %d decisions waiting\n", ws, len(views), running, waiting)
				}
				for _, u := range info.Unavailable {
					fmt.Fprintf(out, "  unavailable: %s\n", u)
				}
			}
			if live == 0 {
				fmt.Fprintln(out, "no hub is running; start one with: tq hub serve")
			}
			return nil
		},
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/registry/ ./internal/hub/web/ ./internal/cmd/; go test ./internal/registry/ ./internal/hub/web/ ./internal/cmd/ -count=1`
Expected: PASS (`TestServeDrainRunsQueuedWorkAndHonoursHubMode` SKIPs without Python).

- [ ] **Step 5: Commit**

```bash
git add internal/registry/registry.go internal/registry/registry_hub_test.go internal/hub/web/backend.go internal/hub/web/backend_test.go internal/cmd/hub_serve.go internal/cmd/hub_serve_test.go
git commit -m "feat(hub): tq hub serve for one workspace or --all, untrusted listing, port-busy message, LAN token"
```

---
### Task 14: Plugin: the `task` block, the signed records export, the headless hand-off line

**Files:**
- Modify: `plugin/tentaqles/workflow/gate.py` (task schema and env check, approval snapshot, `cmd_state` task source, `cmd_records`, `state --records`)
- Modify: `plugin/agents/build-planner.md`, `plugin/skills/build/SKILL.md`, `plugin/skills/feature/SKILL.md`, `plugin/skills/fix/SKILL.md`, `plugin/skills/kickoff/SKILL.md`
- Modify: `plugin/tests/test_build_gate.py` (line 1127: the release-1 `task` fixture uses the old `hub_id` shape)
- Test: `plugin/tests/test_hub_task_block.py`

**Interfaces:**
- Consumes: `gate.plan_problem`, `check_plan`, `cmd_approve`, `cmd_state`, `evidence`, `_Binding`, `reviews_path`, `dod_path`, `bound`, `checkpoint_approved`, `redact`, `read_text`, `read_json`, `load_plan`, `load_approved`, `main` (all in `gate.py`).
- Produces (Python, `tentaqles.workflow.gate`):
  - `TASK_KEYS = ("hub", "workspace", "kind", "external")`, `TASK_KINDS`, `EXTERNAL_KEYS = ("provider", "id", "url")`, `HUB_CARD_RE`.
  - `task_problem(task) -> str` ('' for absent or valid; called by `plan_problem`, so the hook refuses a malformed `task`), `task_env_problem(task, env_value) -> str` (the hook path: a `task` must equal `TQ_HUB_TASK` verbatim, and is refused when `TQ_HUB_TASK` is unset).
  - The approval snapshots the plan's valid `task`; `cmd_state(...)["task"]` is the approved one (the plan's before approval or for approvals made before this release).
  - `cmd_records(root, slug) -> dict` with exactly the keys Task 6's `gate.Records` decodes: `{"plan": {"summary", "plain_summary", "spec", "checkpoints": [{"id", "title", "approved", "tests": [{"file", "layer"}]}]}, "evidence": [{"checkpoint", "ts", "tree", "ok", "current", "summary", "results": [{"cmd", "exit", "seconds", "timed_out"}]}], "reviews": [{"checkpoint", "reviewer", "result", "tree", "ts", "human", "blockers", "majors", "minors", "unjudged"}], "dod": {"ok", "tree", "ts", "override", "checks": [{"id", "ok", "detail"}]} | null}`; CLI `build-gate state <slug> --json --records` adds it as `"records"`.
  - Skill contract: on-ramps pass `task=<TQ_HUB_TASK JSON>` to the planner; the build skill's Phase 0 ends a headless turn with the last line `TQ_HUB_RELOCATE <absolute worktree path> <slug>` (Task 7 parses it).

- [ ] **Step 1: Write the failing tests**

`plugin/tests/test_hub_task_block.py`:

```python
"""The hub's task block and review records (spec A §13.1, §10, §13.4).

The gate accepts a top-level `task` object with exactly the hub's keys, refuses one the
environment did not set, reports the task the human approved, and exports only the records it
signed itself for the hub's review page. The skills carry the block from TQ_HUB_TASK to the plan
and end a headless Phase 0 with the relocate line.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

from tentaqles.workflow import gate

ROOT = Path(__file__).resolve().parents[1]
PY = sys.executable.replace("\\", "/")
PASS_CMD = f'"{PY}" -c "import sys; sys.exit(0)"'
TASK = {"hub": "c-abcdefghijkl", "workspace": "acme", "kind": "feature", "external": None}


def _git(root, *args):
    subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)


def _plan(task=TASK):
    cp = {"id": 1, "title": "Parse rows", "status": "pending", "verify": [PASS_CMD], "tests": ["tests/test_a.py"],
          "gates": {"behavior": {"status": "pending", "attempts": 0, "findings": []},
                    "review": {"status": "pending", "attempts": 0, "findings": []}}}
    plan = {"feature": "Demo", "slug": "demo", "workflow": "feature", "summary": "Export invoices.",
            "spec": "docs/specs/demo.md", "checkpoints": [cp]}
    if task is not None:
        plan["task"] = task
    return plan


def save(root, plan):
    (root / "docs" / "checkpoints" / "demo.json").write_text(json.dumps(plan, indent=2), encoding="utf-8")


@pytest.fixture(autouse=True)
def tq_home(tmp_path, monkeypatch):
    """The signing key lives under TQ_HOME; never touch the real one, never inherit a hub task."""
    monkeypatch.setenv("TQ_HOME", str(tmp_path / "tqhome"))
    for k in ("TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"):
        monkeypatch.delenv(k, raising=False)


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "proj"
    (root / "docs" / "checkpoints").mkdir(parents=True)
    (root / "tests").mkdir()
    _git(root, "init", "-q", "-b", "work")
    _git(root, "config", "user.email", "dev@example.com")
    _git(root, "config", "user.name", "Dev")
    (root / "tests" / "test_a.py").write_text("def test_a():\n    assert True\n", encoding="utf-8")
    save(root, _plan())
    _git(root, "add", "tests/test_a.py", "docs/checkpoints/demo.json")
    _git(root, "commit", "-q", "-m", "init")
    (root / ".claude" / "build").mkdir(parents=True)
    return root


def test_plan_problem_accepts_the_task_block():
    assert gate.plan_problem(_plan()) == ""
    assert gate.plan_problem(_plan(task=None)) == ""
    ext = {**TASK, "external": {"provider": "linear", "id": "ACME-7", "url": "https://linear.app/x"}}
    assert gate.plan_problem(_plan(task=ext)) == ""


@pytest.mark.parametrize("task,needle", [
    ("c-abcdefghijkl", "task must be an object"),
    ({**TASK, "hub_id": "t-42"}, "unknown key"),
    ({k: v for k, v in TASK.items() if k != "kind"}, "missing kind"),
    ({**TASK, "hub": "t-42"}, "task.hub"),
    ({**TASK, "workspace": ""}, "task.workspace"),
    ({**TASK, "kind": "epic"}, "task.kind"),
    ({**TASK, "external": {"provider": "linear"}}, "task.external"),
    ({**TASK, "external": "ACME-7"}, "task.external"),
])
def test_plan_problem_refuses_a_malformed_task(task, needle):
    assert needle in gate.plan_problem(_plan(task=task))


def test_hook_refuses_a_task_without_tq_hub_task(repo):
    with pytest.raises(gate.Block) as e:
        gate.check_plan(json.dumps(_plan()), json.dumps(_plan()), repo, "demo", gate.TreeHash(repo))
    assert "TQ_HUB_TASK" in str(e.value)


def test_hook_accepts_only_the_task_the_hub_set(repo, monkeypatch):
    monkeypatch.setenv("TQ_HUB_TASK", json.dumps(TASK))
    gate.check_plan(json.dumps(_plan()), json.dumps(_plan()), repo, "demo", gate.TreeHash(repo))
    other = {**TASK, "hub": "c-zzzzzzzzzzzz"}
    with pytest.raises(gate.Block) as e:
        gate.check_plan(json.dumps(_plan(task=other)), json.dumps(_plan()), repo, "demo", gate.TreeHash(repo))
    assert "verbatim" in str(e.value)


def test_hook_without_a_task_is_unchanged(repo):
    gate.check_plan(json.dumps(_plan(task=None)), json.dumps(_plan(task=None)), repo, "demo", gate.TreeHash(repo))


def test_state_reports_the_task_the_human_approved(repo):
    assert gate.cmd_state(repo, "demo")["task"] == TASK  # before approval: the plan's
    gate.cmd_approve(repo, "demo")
    save(repo, _plan(task={**TASK, "hub": "c-zzzzzzzzzzzz"}))  # edited behind the hook's back
    assert gate.cmd_state(repo, "demo")["task"] == TASK  # the signed approval wins


def test_state_records_export_only_signed_records(repo, capsys):
    gate.cmd_approve(repo, "demo")
    gate.cmd_verify(repo, "demo", 1)
    gate.cmd_record_review(repo, "demo", 1, "build-critic", "PASS", {"minors": 1})
    with open(gate.state_dir(repo, "demo") / "evidence.jsonl", "a", encoding="utf-8") as fh:
        fh.write(json.dumps({"checkpoint": 1, "ok": True, "results": [{"cmd": "forged", "exit": 0}]}) + "\n")
    capsys.readouterr()
    assert gate.main(["--root", str(repo), "state", "demo", "--json", "--records"]) == 0
    rec = json.loads(capsys.readouterr().out)["records"]
    assert [r["cmd"] for e in rec["evidence"] for r in e["results"]] == [PASS_CMD]
    assert rec["evidence"][0]["current"] is True and rec["evidence"][0]["ok"] is True
    assert rec["reviews"][0]["reviewer"] == "build-critic" and rec["reviews"][0]["minors"] == 1
    assert rec["plan"]["summary"] == "Export invoices." and rec["plan"]["spec"] == "docs/specs/demo.md"
    assert rec["plan"]["checkpoints"][0]["approved"] is True
    assert rec["plan"]["checkpoints"][0]["tests"] == [{"file": "tests/test_a.py", "layer": ""}]
    assert rec["dod"] is None
    gate.main(["--root", str(repo), "state", "demo", "--json"])
    assert "records" not in json.loads(capsys.readouterr().out)


def test_planner_and_on_ramps_document_the_task_block():
    planner = (ROOT / "agents" / "build-planner.md").read_text(encoding="utf-8")
    assert "TQ_HUB_TASK" in planner and '"task"' in planner and "verbatim" in planner
    for name in ("feature", "fix", "kickoff"):
        text = (ROOT / "skills" / name / "SKILL.md").read_text(encoding="utf-8")
        assert "TQ_HUB_TASK" in text and "task=" in text, name
        assert "reject the plan" in text, name
    build = (ROOT / "skills" / "build" / "SKILL.md").read_text(encoding="utf-8")
    assert '`{"hub", "workspace", "kind", "external"}`' in build


def test_build_skill_headless_hand_off_line():
    text = (ROOT / "skills" / "build" / "SKILL.md").read_text(encoding="utf-8")
    phase0 = text.split("## Phase 0", 1)[1].split("## Phase 1", 1)[0]
    assert "TQ_HUB_RELOCATE <absolute worktree path> <slug>" in phase0
    assert "TENTAQLES_HEADLESS=1" in phase0 and "only once per slug" in phase0
    assert "rev-parse --show-toplevel" in phase0  # a native path on Windows, not /c/...
```

In `plugin/tests/test_build_gate.py`, the release-1 test `test_state_json_shape` writes the pre-spec `task` shape, which `task_problem` now refuses. Change line 1127 from:

```python
    save_plan(repo, {**PLAN, "workflow": "feature", "task": {"hub_id": "t-42", "external": {"provider": "linear", "id": "ACME-7", "url": "https://linear.app/x"}}})
```

to:

```python
    save_plan(repo, {**PLAN, "workflow": "feature", "task": {"hub": "c-abcdefghijkl", "workspace": "acme", "kind": "feature", "external": {"provider": "linear", "id": "ACME-7", "url": "https://linear.app/x"}}})
```

(the assertion on line 1133, `st["task"]["external"]["id"] == "ACME-7"`, stays).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd plugin; python -m pytest tests/test_hub_task_block.py -q`
Expected: FAIL, `AssertionError` in `test_plan_problem_refuses_a_malformed_task` (`plan_problem` returns `''`), `Failed: DID NOT RAISE` in `test_hook_refuses_a_task_without_tq_hub_task`, `KeyError: 'records'`.

- [ ] **Step 3: Implement in `gate.py`**

After `DOD_WORKFLOWS = (...)` add:

```python
# The hub's task block (spec A §13.1): TQ_HUB_TASK copied verbatim into the plan by the on-ramp.
TASK_KEYS = ("hub", "workspace", "kind", "external")
TASK_KINDS = ("project", "feature", "fix", "analysis", "hotfix", "chore")
EXTERNAL_KEYS = ("provider", "id", "url")
HUB_CARD_RE = re.compile(r"c-[a-z2-7]{12}")
```

After `plan_problem`, add:

```python
def task_problem(task) -> str:
    """'' when the plan's `task` block is absent or has the hub's shape, else a one-line reason."""
    if task is None:
        return ""
    if not isinstance(task, dict):
        return "task must be an object"
    extra = sorted(set(task) - set(TASK_KEYS))
    if extra:
        return f"task has unknown key(s) {', '.join(extra)} (allowed: {', '.join(TASK_KEYS)})"
    missing = [k for k in TASK_KEYS if k not in task]
    if missing:
        return f"task is missing {', '.join(missing)}"
    if not isinstance(task["hub"], str) or not HUB_CARD_RE.fullmatch(task["hub"]):
        return "task.hub must be a hub card id (c- and 12 base32 characters)"
    if not isinstance(task["workspace"], str) or not task["workspace"].strip():
        return "task.workspace must be a non-empty string"
    if task["kind"] not in TASK_KINDS:
        return f"task.kind must be one of {', '.join(TASK_KINDS)}"
    ext = task["external"]
    if ext is not None and (not isinstance(ext, dict) or set(ext) != set(EXTERNAL_KEYS)
                            or not all(isinstance(ext[k], str) for k in EXTERNAL_KEYS)):
        return "task.external must be null or an object with string provider, id and url"
    return ""


def task_env_problem(task, env_value) -> str:
    """The hook's check: a task block is written only for a run the hub launched, verbatim."""
    if task is None:
        return ""
    if not (env_value or "").strip():
        return ("a task block is only written for a run the hub launched (TQ_HUB_TASK is not set): "
                "remove the top-level task")
    try:
        want = json.loads(env_value)
    except ValueError:
        return "TQ_HUB_TASK is not valid JSON; remove the top-level task"
    if task != want:
        return "task must be TQ_HUB_TASK verbatim"
    return ""
```

In `plan_problem`, right after the `if not isinstance(doc, dict): return "the plan must be a JSON object"` line, add:

```python
    bad = task_problem(doc.get("task"))
    if bad:
        return bad
```

In `check_plan`, right after the block

```python
    bad = plan_problem(doc)
    if bad:
        raise Block(f"invalid plan: {bad}.")
```

add:

```python
    if check_evidence:
        env_bad = task_env_problem(doc.get("task"), os.environ.get("TQ_HUB_TASK"))
        if env_bad:
            raise Block(f"invalid plan: {env_bad}.")
```

In `cmd_approve`, inside the `approved = {...}` dict, after the `"hotfix": plan.get("hotfix") is True,` entry, add:

```python
        "task": plan.get("task") if not task_problem(plan.get("task")) else None,
```

In `cmd_state`, replace `"task": plan.get("task"),` in the returned dict with:

```python
            "task": signed.get("task") if approved is None or "task" in approved else plan.get("task"),
```

After `cmd_state`, add:

```python
def _test_entries(tests) -> list[dict]:
    out = []
    for t in tests or []:
        if isinstance(t, str):
            out.append({"file": t, "layer": ""})
        elif isinstance(t, dict):
            out.append({"file": str(t.get("file") or ""), "layer": str(t.get("layer") or "")})
    return out


def cmd_records(root: Path, slug: str) -> dict:
    """The gate's own records for the hub's review page (spec A §10): only rows this module signed;
    plan checkpoints are flagged by whether the human's approval still covers them."""
    plan = load_plan(root, slug) or {"checkpoints": []}
    approved = load_approved(root, slug)
    current = (approved or {}).get("approval_id")
    plan_out = {"summary": str(plan.get("summary") or ""), "plain_summary": str(plan.get("plain_summary") or ""),
                "spec": str(plan.get("spec") or ""),
                "checkpoints": [{"id": cp.get("id"), "title": str(cp.get("title") or ""),
                                 "approved": checkpoint_approved(approved, cp)[0], "tests": _test_entries(cp.get("tests"))}
                                for cp in plan.get("checkpoints") or [] if isinstance(cp, dict)]}
    ev = []
    for r in evidence(root, slug):
        ev.append({"checkpoint": r.get("checkpoint"), "ts": r.get("ts"), "tree": r.get("tree"), "ok": bool(r.get("ok")),
                   "current": bool(current) and r.get("approval_id") == current,
                   "summary": redact(str(r.get("summary") or ""))[:300],
                   "results": [{"cmd": str(x.get("cmd") or ""), "exit": x.get("exit"), "seconds": x.get("seconds"),
                                "timed_out": bool(x.get("timed_out"))}
                               for x in (r.get("results") or []) if isinstance(x, dict)]})
    b = _Binding(root, slug)
    revs = []
    for line in read_text(reviews_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if b.ok(rec):
            revs.append({k: rec.get(k) for k in ("checkpoint", "reviewer", "result", "tree", "ts", "human",
                                                 "blockers", "majors", "minors", "unjudged")})
    dod = read_json(dod_path(root, slug))
    dod_out = None
    if bound(root, slug, dod):
        dod_out = {"ok": bool(dod.get("ok")), "tree": dod.get("tree"), "ts": dod.get("ts"), "override": dod.get("override"),
                   "checks": [{"id": str(c.get("id")), "ok": c.get("ok"),
                               "detail": redact(str(c.get("detail") or "").splitlines()[0] if c.get("detail") else "")[:200]}
                              for c in (dod.get("checks") or []) if isinstance(c, dict)]}
    return {"plan": plan_out, "evidence": ev, "reviews": revs, "dod": dod_out}
```

In `main()`, after `stt.add_argument("--json", action="store_true")` add:

```python
    stt.add_argument("--records", action="store_true", help="add the gate's signed records (the hub's review page)")
```

and in the `elif a.cmd == "state":` branch, right after `st = cmd_state(root, a.slug)`:

```python
        if a.records:
            st["records"] = cmd_records(root, a.slug)
```

- [ ] **Step 4: Update the planner and the skills**

`plugin/agents/build-planner.md`: after the paragraph that starts `` `workflow` is one of `build`, `feature`, `fix`, `kickoff` ``, add:

```markdown
`task` is written only when the orchestrator passes `task=<json>` (the on-ramp read it from the `TQ_HUB_TASK` environment variable because the hub launched this run): copy that JSON verbatim as the plan's top-level `"task"` (keys `hub`, `workspace`, `kind`, `external`, in that form). Never write `task` otherwise and never edit it: the gate refuses a `task` the environment did not set or that differs from it.
```

`plugin/skills/build/SKILL.md`, in "## Headless mode", replace the sentence

```markdown
The plan's top-level `task` block (hub id, external tracker id) is set by the on-ramp when the hub launched the run; never invent one.
```

with

```markdown
The plan's top-level `task` block, `{"hub", "workspace", "kind", "external"}`, is copied verbatim from `TQ_HUB_TASK` by the on-ramp when the hub launched the run (the gate refuses a `task` the environment did not set, and the hub finds its build by `task.hub`); never invent one.
```

and in "## Phase 0: Worktree and hooks", after the sentence `Don't continue with inactive hooks.` that ends step 5, add a new paragraph indented under step 5:

```markdown
   **Headless** (`TENTAQLES_HEADLESS=1` or `--headless`): there is no user to reopen Claude Code. Instead of the message, end the turn with this exact line as the last line of your final output, nothing after it: `TQ_HUB_RELOCATE <absolute worktree path> <slug>`. Take the path from `git -C "<worktree>" rev-parse --show-toplevel` (a native path on Windows, `C:/...`, not Git Bash's `/c/...`). The hub starts the next round in the worktree with the same on-ramp (or `/tentaqles:build resume <slug> --headless` once the plan exists). Write the line only in headless mode and only once per slug: in the worktree session step 3 finds the linked worktree and this step runs `build-gate ping` instead.
```

`plugin/skills/feature/SKILL.md`: between `defined there too.` (end of the Headless mode paragraph) and `## 1. Locate it in the PRD`, add:

```markdown
**Hub task.** When `TQ_HUB_TASK` is set, the hub launched this run for one card. Read it with
`printf '%s\n' "${TQ_HUB_TASK:-}"` and pass `task=<that JSON>` to the planner together with
`workflow=feature`; the planner copies it verbatim as the plan's top-level `task`. When
`TQ_HUB_TASK` is not set, the plan has no `task`: if the planner wrote one anyway, reject the
plan and re-run the planner without it (the gate refuses such a plan too).
```

`plugin/skills/fix/SKILL.md`: between `ends; the `build-gate` prefix is defined there too.` and `## 1. Intake`, add the same paragraph with `workflow=fix`:

```markdown
**Hub task.** When `TQ_HUB_TASK` is set, the hub launched this run for one card. Read it with
`printf '%s\n' "${TQ_HUB_TASK:-}"` and pass `task=<that JSON>` to the planner together with
`workflow=fix`; the planner copies it verbatim as the plan's top-level `task`. When
`TQ_HUB_TASK` is not set, the plan has no `task`: if the planner wrote one anyway, reject the
plan and re-run the planner without it (the gate refuses such a plan too). The card's kind is
`hotfix` exactly when the hub passed `--hotfix`; never infer it.
```

`plugin/skills/kickoff/SKILL.md`: between `` `build-gate` prefix is defined there too. `` and `## 1. Branch, then PRD, planning-ready or nothing`, add:

```markdown
**Hub task.** When `TQ_HUB_TASK` is set, the hub launched this run for one card. Read it with
`printf '%s\n' "${TQ_HUB_TASK:-}"` and pass `task=<that JSON>` to the planner together with
`workflow=kickoff`; the planner copies it verbatim as the plan's top-level `task`. When
`TQ_HUB_TASK` is not set, the plan has no `task`: if the planner wrote one anyway, reject the
plan and re-run the planner without it (the gate refuses such a plan too).
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd plugin; python -m pytest tests/test_hub_task_block.py tests/test_build_gate.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py tests/test_process_skills.py tests/test_agents.py -q`
Expected: PASS. Then `python -m pytest tests -q` stays green.

- [ ] **Step 6: Commit**

```bash
git add plugin/tentaqles/workflow/gate.py plugin/agents/build-planner.md plugin/skills/build/SKILL.md plugin/skills/feature/SKILL.md plugin/skills/fix/SKILL.md plugin/skills/kickoff/SKILL.md plugin/tests/test_build_gate.py plugin/tests/test_hub_task_block.py
git commit -m "feat(build-gate): validated hub task block, signed records export and the headless worktree hand-off"
```

---
### Task 15: Plugin: `/tentaqles:hub`, the `ship` PR comment, the bounded preamble count

**Files:**
- Create: `plugin/skills/hub/SKILL.md`
- Modify: `plugin/skills/ship/SKILL.md` (step 7), `plugin/scripts/session-preamble.py`
- Test: `plugin/tests/test_hub_skill.py`, `plugin/tests/test_hub_preamble.py`

**Interfaces:**
- Consumes: the CLI of Task 12 (`tq hub list --waiting --json`, `tq hub add`, `tq hub show`, `tq hub comment -`, persistent `--timeout`), the store schema of Task 1 (`decisions.status`), `$TQ_HOME/hub/<workspace>/hub.db` (Global Constraints), `TQ_WS` (set by tq for the session).
- Produces:
  - Skill `hub` (`/tentaqles:hub`): every bash block calls `tq hub … --timeout 2s`, never `tq hub answer`, never `build-gate`, never `--budget`; it prints `tq hub answer <card> <decision> --choice <option>` for the user.
  - `ship` step 7: `if [ -n "${TQ_HUB_TASK:-}" ]; then tq hub comment --timeout 2s - "PR: <pr-url>" || true; fi`.
  - `session-preamble.py`: `HUB_READ_BUDGET = 0.3`, `_hub_db_path(context) -> Path | None`, `_hub_context(context) -> str` (`"N decisions waiting on you (tq hub open)"`, singular for 1, `""` when there is nothing, no database, or the read does not finish in 300 ms); printed in both the `--memory-only` and the full branch. No network, read-only SQLite.

- [ ] **Step 1: Write the failing tests**

`plugin/tests/test_hub_skill.py`:

```python
"""/tentaqles:hub and ship's PR comment (spec A §13.3): bounded tq hub calls, and the
human-only rule (the skill prints the answer command; it never runs it)."""

from __future__ import annotations

import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
HUB = ROOT / "skills" / "hub" / "SKILL.md"
SHIP = ROOT / "skills" / "ship" / "SKILL.md"


def _frontmatter(path: Path) -> dict:
    parts = re.split(r"^---\s*$", path.read_text(encoding="utf-8"), maxsplit=2, flags=re.MULTILINE)
    return yaml.safe_load(parts[1])


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, flags=re.DOTALL)


def test_hub_skill_frontmatter():
    fm = _frontmatter(HUB)
    assert fm["name"] == "hub"
    desc = fm["description"]
    assert 40 <= len(desc) <= 1024
    for phrase in ("/tentaqles:hub", "waiting", "tq hub answer", "never runs it"):
        assert phrase in desc, phrase


def test_hub_skill_bash_blocks_are_bounded_and_never_answer():
    blocks = _bash_blocks(HUB.read_text(encoding="utf-8"))
    assert len(blocks) >= 3
    for block in blocks:
        for line in block.splitlines():
            if "tq hub" in line:
                assert "--timeout 2s" in line, line
        assert "tq hub answer" not in block
        assert "build-gate" not in block
        assert "--budget" not in block
        assert "move" not in block


def test_hub_skill_prints_the_answer_command_for_the_user():
    text = HUB.read_text(encoding="utf-8")
    assert "tq hub answer <card> <decision> --choice <option>" in text
    assert "Never run `tq hub answer`" in text
    assert "tq hub list --waiting --json --timeout 2s" in text


def test_ship_posts_the_pr_link_when_the_hub_launched_the_run():
    text = SHIP.read_text(encoding="utf-8")
    blocks = [b for b in _bash_blocks(text) if "tq hub comment" in b]
    assert len(blocks) == 1
    block = blocks[0]
    assert "TQ_HUB_TASK" in block and "--timeout 2s" in block and "|| true" in block and 'tq hub comment --timeout 2s - "PR: ' in block
    assert text.index("gh pr create --base") < text.index("tq hub comment")
    assert text.index("tq hub comment") < text.index("## 8. Hand off")
```

`plugin/tests/test_hub_preamble.py`:

```python
"""The session preamble's hub line (spec A §13.3): a read-only SQLite count bounded to 300 ms."""

from __future__ import annotations

import importlib.util
import json
import os
import sqlite3
import subprocess
import sys
import textwrap
import time
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = PLUGIN_ROOT / "scripts" / "session-preamble.py"


def _workspace(root: Path) -> Path:
    root.mkdir(parents=True, exist_ok=True)
    (root / ".tentaqles.yaml").write_text(textwrap.dedent("""\
        schema: tentaqles-client-v2
        client: acme
        display_name: Acme Corp
        language: en
        """), encoding="utf-8")
    return root


def _hub_db(home: Path, ws: str, pending: int, answered: int = 0) -> Path:
    db = home / "hub" / ws / "hub.db"
    db.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(db)
    conn.execute("CREATE TABLE decisions (id TEXT PRIMARY KEY, status TEXT NOT NULL)")
    rows = [(f"d{i}", "pending") for i in range(pending)] + [(f"a{i}", "answered") for i in range(answered)]
    conn.executemany("INSERT INTO decisions(id, status) VALUES (?, ?)", rows)
    conn.commit()
    conn.close()
    return db


def _run(cwd: Path, env_extra: dict, *args: str) -> str:
    env = dict(os.environ)
    env.update(env_extra)
    extra = [p for p in sys.path if p and ("site-packages" in p or p == str(PLUGIN_ROOT))]
    env["PYTHONPATH"] = os.pathsep.join([str(PLUGIN_ROOT), *extra])
    proc = subprocess.run([sys.executable, str(SCRIPT), *args], input=json.dumps({"cwd": str(cwd)}),
                          capture_output=True, text=True, timeout=60, env=env)
    assert proc.returncode == 0, proc.stderr
    return proc.stdout


def test_preamble_counts_pending_decisions(tmp_path):
    ws = _workspace(tmp_path / "work" / "acme")
    home = tmp_path / "tqhome"
    _hub_db(home, "acme", pending=2, answered=1)
    out = _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"}, "--memory-only")
    assert "2 decisions waiting on you (tq hub open)" in out
    full = _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"})
    assert "2 decisions waiting on you (tq hub open)" in full


def test_preamble_singular_none_and_no_database(tmp_path):
    ws = _workspace(tmp_path / "work" / "acme")
    home = tmp_path / "tqhome"
    assert "waiting on you" not in _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"}, "--memory-only")
    _hub_db(home, "acme", pending=1)
    assert "1 decision waiting on you (tq hub open)" in _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"}, "--memory-only")
    other = tmp_path / "tqhome2"
    _hub_db(other, "acme", pending=0, answered=3)
    assert "waiting on you" not in _run(ws, {"TQ_HOME": str(other), "TQ_WS": "acme"}, "--memory-only")


def _load_module():
    spec = importlib.util.spec_from_file_location("session_preamble_under_test", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def test_hub_read_is_bounded_to_300_ms(tmp_path, monkeypatch):
    home = tmp_path / "tqhome"
    db = _hub_db(home, "acme", pending=4)
    monkeypatch.setenv("TQ_HOME", str(home))
    monkeypatch.setenv("TQ_WS", "acme")
    mod = _load_module()
    assert mod._hub_context({}) == "4 decisions waiting on you (tq hub open)"
    holder = sqlite3.connect(db, isolation_level=None)
    holder.execute("BEGIN EXCLUSIVE")  # a writer holds the database: readers must give up, not wait
    try:
        t0 = time.monotonic()
        line = mod._hub_context({})
        elapsed = time.monotonic() - t0
    finally:
        holder.execute("ROLLBACK")
        holder.close()
    assert line == "" and elapsed < 1.0, (line, elapsed)


def test_workspace_name_falls_back_to_the_client_root(tmp_path, monkeypatch):
    home = tmp_path / "tqhome"
    _hub_db(home, "acme", pending=1)
    monkeypatch.setenv("TQ_HOME", str(home))
    monkeypatch.delenv("TQ_WS", raising=False)
    mod = _load_module()
    assert mod._hub_db_path({"client_root": str(tmp_path / "work" / "acme")}) == home / "hub" / "acme" / "hub.db"
    assert mod._hub_db_path({"client_root": str(tmp_path / "work" / "..")}) is None
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd plugin; python -m pytest tests/test_hub_skill.py tests/test_hub_preamble.py -q`
Expected: FAIL, `FileNotFoundError` for `skills/hub/SKILL.md`, `AssertionError` (no hub line), `AttributeError: module ... has no attribute '_hub_context'`.

- [ ] **Step 3: Write the skill**

`plugin/skills/hub/SKILL.md`:

````markdown
---
name: hub
description: Work the local tq hub board of this client workspace - see the decisions agent runs are waiting on, explain each in plain language, add a card, or show one. Reads tq hub list --waiting --json (bounded to 2 s). Answering is human-only, so when you pick an answer it prints the exact tq hub answer command for you to run in your own terminal and never runs it. Use when the user says "what's waiting on me", "hub", "pending decisions", "answer the plan approval", "what does the run need", "add a card", "show card c-...", or "/tentaqles:hub".
argument-hint: '[waiting | add "<title>" | show <card>]'
---

# Hub

`tq hub` keeps one board per client workspace and runs agent-ready cards headless
through `/tentaqles:build` (tq's `docs/HUB.md`). Every human stop of a run becomes a
decision waiting on the user. This skill shows those decisions, explains them, and
hands the user the command to answer. It never answers anything itself.

`$ARGUMENTS` is empty or `waiting` (what is waiting), `add "<title>"` (add a card) or
`show <card>`.

## 1. What is waiting

```bash
tq hub list --waiting --json --timeout 2s 2>&1; echo "hub_exit=$?"
```

- `hub_exit=0` and `[]`: say "Nothing is waiting on you in this workspace." and stop.
- `hub_exit=0` with items (newest first): for each, one short paragraph in plain
  words, then the options. By `decision.kind`:
  - `plan-approval`: the run planned the work and waits for approval; summarise the
    checkpoints and verify commands from `decision.text` (approving freezes them).
  - `result`: the run finished its checkpoints; say what to try. Approving finishes the
    build; `request-changes` sends a note back into the engine. The review page
    (`tq hub review <card> --open`) shows the evidence from the gate's records.
  - `judge`: a reviewer could not judge something; list the rows from `decision.text`.
  - `question`: the question as asked.
  - `permission`: the run was refused a tool (`allow-once` allows it for one round) or
    stopped at its budget (`raise-and-resume` starts a new run; a note such as
    `rounds=20` sets the new limits).
- Any other exit: no hub data for this workspace. Say so in one line with the first
  line of the output (for example "No hub data for this workspace: no workspace here")
  and stop. One bounded call; never retry in a loop.

## 2. Answering is the user's

When the user picks an answer in the conversation, run nothing. Print the exact
command for them to run in their own terminal (PowerShell, Windows Terminal or a
macOS/Linux terminal), with the card id, the decision id and one of the options
exactly as listed:

    tq hub answer <card> <decision> --choice <option> --note "<their note, if any>"

Mention that the hub page (`tq hub open`) has the same buttons. Never run `tq hub answer`,
`tq hub move <card> done`, `build-gate answer`, `build-gate approve` or `build-gate token`
yourself: tq and the gate refuse them from an agent session, and the answer is the
human's.

## 3. Add a card

If the body has more than one line, write it to a temporary file first, then:

```bash
tq hub add "<title>" --kind <feature|fix|hotfix|analysis|chore|project> --body-file "<path>" --timeout 2s 2>&1
```

Add `--tag agent-ready` and `--project <folder relative to the workspace root>` only
when the user asks for the card to run. Budgets are the user's: tell them to pass one
themselves if they want it.

## 4. Show a card

```bash
tq hub show <card> --timeout 2s 2>&1
```

## Never

- run `tq hub answer`, move a card to a done lane, or any human-only `build-gate` command
- set a budget for the user
- retry a failed `tq hub` call in a loop; one bounded call per step
- copy card text into another tool; the hub masked what it stored, keep it there
````

- [ ] **Step 4: Add the `ship` comment**

In `plugin/skills/ship/SKILL.md`, insert right before the line `## 8. Hand off`:

````markdown
**Hub card.** If `TQ_HUB_TASK` is set (the hub launched this run), post the PR link on
the card. The call is bounded to two seconds and its failure is ignored; type the URL
`gh pr create` printed:

```bash
if [ -n "${TQ_HUB_TASK:-}" ]; then tq hub comment --timeout 2s - "PR: <pr-url>" || true; fi
```

````

- [ ] **Step 5: Add the preamble line**

In `plugin/scripts/session-preamble.py`, add after `_prd_context`:

```python
HUB_READ_BUDGET = 0.3  # seconds: the preamble never waits longer on the hub database (spec A §13.3)


def _hub_db_path(context: dict) -> Path | None:
    """$TQ_HOME/hub/<workspace>/hub.db when it exists; the workspace is TQ_WS, else the client root's name."""
    ws = (os.environ.get("TQ_WS") or "").strip()
    if not ws:
        root = (context or {}).get("client_root") or ""
        ws = Path(root).name if root else ""
    if not ws or ws in (".", "..") or any(c in ws for c in "/\\:"):
        return None
    home = os.environ.get("TQ_HOME") or str(Path.home() / ".tentaqles")
    p = Path(home) / "hub" / ws / "hub.db"
    return p if p.is_file() else None


def _count_pending(db: Path, out: list) -> None:
    import sqlite3

    try:
        conn = sqlite3.connect(db.as_uri() + "?mode=ro", uri=True, timeout=HUB_READ_BUDGET)
        try:
            out.append(int(conn.execute("SELECT COUNT(*) FROM decisions WHERE status = 'pending'").fetchone()[0]))
        finally:
            conn.close()
    except Exception:
        pass


def _hub_context(context: dict) -> str:
    """One line when decisions wait on the human; a read-only SQLite read, never the network."""
    try:
        db = _hub_db_path(context)
        if db is None:
            return ""
        import threading

        out: list = []
        t = threading.Thread(target=_count_pending, args=(db, out), daemon=True)
        t.start()
        t.join(HUB_READ_BUDGET)
        if t.is_alive() or not out or out[0] <= 0:
            return ""
        n = out[0]
        return f"{n} decision{'s' if n != 1 else ''} waiting on you (tq hub open)"
    except Exception:
        return ""
```

In `main()`, in the `if memory_only:` branch, after the `prd_line` lines and before `temporal = ...`:

```python
            hub_line = _hub_context(context)
            if hub_line:
                print(hub_line)
```

and in the full branch, after `if prd_line: summary += "\n\n" + prd_line`:

```python
        hub_line = _hub_context(context)
        if hub_line:
            summary += "\n\n" + hub_line
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd plugin; python -m pytest tests/test_hub_skill.py tests/test_hub_preamble.py tests/test_session_preamble.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py tests/test_ship_addproject_skills.py -q`
Expected: PASS. Then `python -m pytest tests -q` stays green.

- [ ] **Step 7: Commit**

```bash
git add plugin/skills/hub/SKILL.md plugin/skills/ship/SKILL.md plugin/scripts/session-preamble.py plugin/tests/test_hub_skill.py plugin/tests/test_hub_preamble.py
git commit -m "feat(plugin): /tentaqles:hub skill, ship posts the PR link to the hub card, bounded preamble count"
```

---
### Task 16: `tq doctor` hub checks, docs, and the end-to-end integration test

**Files:**
- Create: `internal/doctor/hub.go`, `docs/HUB.md`, `test/integration/hub_test.go`
- Modify: `internal/doctor/doctor.go` (call the hub checks), `CLAUDE.md` (layout and docs list), `plugin/CHANGELOG.md` (`## [Unreleased]`), `plugin/README.md` (skills table), `plugin/CLAUDE.md` (skill count, hub conventions)
- Test: `internal/doctor/hub_test.go`, `plugin/tests/test_hub_docs.py`

**Interfaces:**
- Consumes: everything above; `store.OpenReadOnly`, `(*Store).Locks`, `store.ActiveStatuses`, `serveinfo.ReadAll`, `serveinfo.Ping`, `serveinfo.FindByPort`, `cmd.NewRoot`, `gate.Actor`, `testutil.Python`, `registry.Config.Hub.Port`.
- Produces (package `doctor`): `hubChecks(ws string, add func(level, code, ws, msg, fix string)) bool` (`hub-db-unreadable`, `hub-stale-lock`; returns whether the workspace has a hub database), `hubPortCheck(port int, add …)` (`hub-port-busy`, run only when some workspace uses the hub, so machines and tests without a hub see no new finding); all three at level `info` (informational: `doctor.Exit` ignores them), each with a fix line.

- [ ] **Step 1: Write the failing tests**

`internal/doctor/hub_test.go`:

```go
package doctor

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

type finding struct{ level, code, ws, msg, fix string }

func collect() (*[]finding, func(level, code, ws, msg, fix string)) {
	var fs []finding
	return &fs, func(level, code, ws, msg, fix string) { fs = append(fs, finding{level, code, ws, msg, fix}) }
}

func isolateHub(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestHubChecks(t *testing.T) {
	isolateHub(t)
	fs, add := collect()
	if hubChecks("nohub", add) {
		t.Fatal("no database means the hub is not in use")
	}
	if len(*fs) != 0 {
		t.Fatalf("no database, no finding: %+v", *fs)
	}
	st, err := store.Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := st.AddCard(store.NewCard{Title: "x"}, "renato")
	st.Checkout(c.ID, "r-gonegonegone")
	st.Close()
	if !hubChecks("acme", add) {
		t.Fatal("acme uses the hub")
	}
	if len(*fs) != 1 || (*fs)[0].code != "hub-stale-lock" || (*fs)[0].level != "info" || !strings.Contains((*fs)[0].msg, c.ID) || (*fs)[0].fix == "" {
		t.Fatalf("%+v", *fs)
	}
	os.MkdirAll(store.Dir("bad"), 0o700)
	os.WriteFile(store.DBPath("bad"), []byte("this is not a database"), 0o600)
	*fs = nil
	hubChecks("bad", add)
	if len(*fs) != 1 || (*fs)[0].code != "hub-db-unreadable" || (*fs)[0].level != "info" {
		t.Fatalf("%+v", *fs)
	}
}

func TestHubPortCheck(t *testing.T) {
	isolateHub(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	fs, add := collect()
	hubPortCheck(port, add)
	if len(*fs) != 1 || (*fs)[0].code != "hub-port-busy" || (*fs)[0].level != "info" {
		t.Fatalf("another program on the port: %+v", *fs)
	}
	serveinfo.Write(serveinfo.Info{PID: 1, Addr: ln.Addr().String(), Port: port, Workspaces: []string{"acme"}})
	*fs = nil
	hubPortCheck(port, add)
	if len(*fs) != 0 {
		t.Fatalf("our own hub on the port is fine: %+v", *fs)
	}
}
```

`plugin/tests/test_hub_docs.py`:

```python
"""The hub ships with its docs: README row, changelog entry, skill count."""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_readme_lists_the_hub_skill():
    assert "| `/tentaqles:hub` |" in (ROOT / "README.md").read_text(encoding="utf-8")


def test_changelog_unreleased_mentions_the_hub():
    text = (ROOT / "CHANGELOG.md").read_text(encoding="utf-8")
    unreleased = text.split("## [Unreleased]", 1)[1].split("\n## [", 1)[0]
    for needle in ("/tentaqles:hub", "TQ_HUB_TASK", "TQ_HUB_RELOCATE", "--records"):
        assert needle in unreleased, needle


def test_claude_md_skill_count_matches_the_tree():
    n = len([p for p in (ROOT / "skills").iterdir() if (p / "SKILL.md").is_file()])
    m = re.search(r"(\d+) skill directories", (ROOT / "CLAUDE.md").read_text(encoding="utf-8"))
    assert m and int(m.group(1)) == n, (m and m.group(1), n)
```

`test/integration/hub_test.go`:

```go
package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/cmd"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func tq(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	root := cmd.NewRoot()
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("tq %s: %v\n%s%s", strings.Join(args, " "), err, out.String(), errb.String())
	}
	return out.String()
}

func unsetEnv(t *testing.T, k string) {
	t.Helper()
	if v, ok := os.LookupEnv(k); ok {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Setenv(k, v) })
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=T", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func jsonLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestHubEndToEndWithFakeClaude: tq hub add → move to in progress → the run hands off into its
// worktree → a plan-approval waits → tq hub answer (hub token, checked by the fake gate) → the
// run resumes → a result waits → the review page shows gate evidence only → approve → done.
func TestHubEndToEndWithFakeClaude(t *testing.T) {
	py := testutil.Python(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	wd, _ := os.Getwd()
	repo := filepath.Dir(filepath.Dir(wd)) // test/integration → monorepo root
	fakeClaude := filepath.Join(repo, "internal", "hub", "adapter", "testdata", "fake_claude.py")
	fakeGate := filepath.Join(repo, "internal", "hub", "gate", "testdata", "fake_build_gate.py")

	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN", "__TQ_STATE", "TQ_WS"} {
		unsetEnv(t, k)
	}
	base := filepath.Join(home, "work")
	ws := filepath.Join(base, "acme")
	project := filepath.Join(ws, "apps", "billing")
	wt := filepath.Join(ws, "apps", "billing-build-demo")
	for _, d := range []string{project, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(ws, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\n"), 0o600)
	h, _ := trust.HashFile(mp)
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(home, ".tentaqles", "build-gate")
	os.MkdirAll(keyDir, 0o700)
	os.WriteFile(filepath.Join(keyDir, "hmac.key"), bytes.Repeat([]byte{7}, 32), 0o600)

	gitIn(t, wt, "init", "-q")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("billing\n"), 0o644)
	gitIn(t, wt, "add", "README.md")
	gitIn(t, wt, "commit", "-q", "-m", "chore: base")
	baseHash := gitIn(t, wt, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(wt, "export.py"), []byte("print('csv')\n"), 0o644)
	gitIn(t, wt, "add", "export.py")
	gitIn(t, wt, "commit", "-q", "-m", "feat(demo#1): export invoices as CSV")

	gateDir := t.TempDir()
	t.Setenv("FAKE_GATE_DIR", gateDir)
	argv := func(script string) string { b, _ := json.Marshal(append(append([]string(nil), py...), script)); return string(b) }
	t.Setenv("TQ_HUB_GATE_CMD", argv(fakeGate))
	t.Setenv("TQ_HUB_CLAUDE_CMD", argv(fakeClaude))
	t.Chdir(ws)

	var card store.Card
	if err := json.Unmarshal([]byte(tq(t, "hub", "add", "Export invoices", "--kind", "feature", "--tag", "agent-ready",
		"--project", "apps/billing", "--body", "CSV per month.", "--json")), &card); err != nil {
		t.Fatal(err)
	}
	actor := gate.Actor()
	statePath := filepath.Join(gateDir, "demo.json")
	records := map[string]any{
		"plan": map[string]any{"summary": "Export invoices as CSV.", "plain_summary": "", "spec": "docs/specs/demo.md",
			"checkpoints": []any{map[string]any{"id": 1, "title": "Write CSV", "approved": true, "tests": []any{map[string]any{"file": "tests/test_export.py", "layer": "unit"}}}}},
		"evidence": []any{map[string]any{"checkpoint": 1, "ts": "2026-10-21T10:30:00+00:00", "tree": "abcdef0123456789", "ok": true, "current": true,
			"summary": "all verify commands passed", "results": []any{map[string]any{"cmd": "go test ./...", "exit": 0, "seconds": 2.0, "timed_out": false}}}},
		"reviews": []any{map[string]any{"checkpoint": 1, "reviewer": "build-critic", "result": "PASS", "tree": "abcdef0123456789", "ts": "2026-10-21T10:40:00+00:00",
			"human": false, "blockers": 0, "majors": 0, "minors": 0, "unjudged": 0}},
		"dod": map[string]any{"ok": true, "tree": "abcdef0123456789", "ts": "2026-10-21T10:50:00+00:00", "override": nil,
			"checks": []any{map[string]any{"id": "test", "ok": true, "detail": "exit 0: go test ./..."}}},
	}
	state := func(pending, decisions []any) map[string]any {
		return map[string]any{"slug": "demo", "feature": "Export invoices", "workflow": "feature", "approved": true, "rolled_back": false,
			"finished": false, "halted": nil, "task": map[string]any{"hub": card.ID, "workspace": "acme", "kind": "feature", "external": nil},
			"bases": map[string]any{"1": baseHash},
			"checkpoints": []any{map[string]any{"id": 1, "title": "Write CSV", "status": "passed", "gates": map[string]any{"behavior": "passed", "review": "passed"},
				"evidence": "fresh", "reviews": map[string]any{"build-critic": "PASS"}}},
			"dod": map[string]any{"ok": true, "tree": "abcdef0123456789", "override": nil}, "pending_decisions": pending, "decisions": decisions, "records": records}
	}
	planApproval := map[string]any{"id": "d0000001", "kind": "plan-approval", "checkpoint": nil, "reviewer": nil, "text": "1 checkpoint: write CSV. verify: go test ./...",
		"options": []any{"approve", "edit", "stop"}, "asked_at": "2026-10-21T10:00:00+00:00"}
	result := map[string]any{"id": "d0000002", "kind": "result", "checkpoint": nil, "reviewer": nil, "text": "Try: export March.",
		"options": []any{"approve", "request-changes", "stop"}, "asked_at": "2026-10-21T11:00:00+00:00"}
	approvedPlan := map[string]any{"id": "d0000001", "kind": "plan-approval", "status": "answered", "choice": "approve", "actor": actor,
		"options": []any{"approve", "edit", "stop"}, "answered_at": "2026-10-21T10:05:00+00:00"}
	argvLog := filepath.Join(t.TempDir(), "argv.jsonl")
	script, _ := json.Marshal(map[string]any{"counter": filepath.Join(t.TempDir(), "n"), "argv_log": argvLog, "rounds": []any{
		map[string]any{"result": "Worktree and hooks ready.\nTQ_HUB_RELOCATE " + filepath.ToSlash(wt) + " demo", "cost": 0.05, "session_id": "s-0"},
		map[string]any{"result": "Plan written; waiting for approval.", "cost": 0.5, "session_id": "s-1",
			"write_json": map[string]any{statePath: state([]any{planApproval}, []any{map[string]any{"id": "d0000001", "kind": "plan-approval", "status": "pending"}})}},
		map[string]any{"result": "All checkpoints passed. Everything is green, trust me.", "cost": 1.25, "session_id": "s-1",
			"write_json": map[string]any{statePath: state([]any{result}, []any{approvedPlan, map[string]any{"id": "d0000002", "kind": "result", "status": "pending"}})}},
	}})
	scriptPath := filepath.Join(t.TempDir(), "script.json")
	os.WriteFile(scriptPath, script, 0o600)
	t.Setenv("FAKE_CLAUDE_SCRIPT", scriptPath)

	if out := tq(t, "hub", "move", card.ID, "in progress"); !strings.Contains(out, "queued run r-") {
		t.Fatal(out)
	}
	tq(t, "hub", "serve", "--drain", "--no-notify") // round 1 hands off, round 2 waits on the plan

	var waiting []map[string]any
	json.Unmarshal([]byte(tq(t, "hub", "list", "--waiting", "--json")), &waiting)
	if len(waiting) != 1 || waiting[0]["decision"].(map[string]any)["id"] != "d0000001" {
		t.Fatalf("waiting %+v", waiting)
	}
	out := tq(t, "hub", "answer", card.ID, "d0000001", "--choice", "approve", "--note", "ok")
	if !strings.Contains(out, "decision d0000001: approve by "+actor) || !strings.Contains(out, "resumed run") {
		t.Fatal(out)
	}
	var gateState map[string]any
	raw, _ := os.ReadFile(statePath)
	json.Unmarshal(raw, &gateState)
	answered := gateState["decisions"].([]any)
	if last := answered[len(answered)-1].(map[string]any); last["actor"] != actor || last["choice"] != "approve" {
		t.Fatalf("the gate's own record must show the hub actor: %+v", last)
	}
	for _, c := range jsonLines(t, filepath.Join(gateDir, "calls.jsonl")) {
		argv := c["argv"].([]any)
		if argv[0] == "answer" && (c["has_token"] != true || c["actor"] != actor) {
			t.Fatalf("answer without a hub token: %v", c)
		}
		if argv[0] == "state" && c["has_token"] == true {
			t.Fatalf("a state read carried the hub token: %v", c)
		}
	}

	tq(t, "hub", "serve", "--drain", "--no-notify") // round 3 resumes and waits on the result
	st, _ := store.OpenExisting("acme", []string{"all"})
	got, _ := st.Card(card.ID)
	runs, _ := st.RunsForCard(card.ID)
	st.Close()
	if got.Lane != "review" || len(runs) != 1 || runs[0].Status != store.StatusWaiting || runs[0].Rounds != 3 || runs[0].Worktree == "" {
		t.Fatalf("card %+v runs %+v", got, runs)
	}

	html := tq(t, "hub", "review", card.ID, "--html")
	evidence := between(html, `<section id="evidence"`, "</section>")
	if !strings.Contains(evidence, "go test ./...") || !strings.Contains(evidence, "abcdef012345") {
		t.Fatalf("evidence:\n%s", evidence)
	}
	if strings.Contains(html, "trust me") {
		t.Fatal("agent text never reaches the review page; it is only a card comment")
	}
	if !strings.Contains(between(html, `<section id="done"`, "</section>"), "feat(demo#1): export invoices as CSV") {
		t.Fatal("commits since the base")
	}

	tq(t, "hub", "answer", card.ID, "d0000002", "--choice", "approve")
	st, _ = store.OpenExisting("acme", []string{"all"})
	got, _ = st.Card(card.ID)
	run, _ := st.Run(runs[0].ID)
	st.Close()
	if got.Lane != "done" || run.Status != store.StatusFinished || run.CostUSD < 1.79 || run.CostUSD > 1.81 {
		t.Fatalf("card %+v run %+v", got, run)
	}
	if _, err := os.Stat(filepath.Join(store.RunsDir("acme"), run.ID+".review.html")); err != nil {
		t.Fatal("the approved review is exported next to the run log")
	}

	calls := jsonLines(t, argvLog)
	if len(calls) != 3 {
		t.Fatalf("%d rounds", len(calls))
	}
	for i, c := range calls {
		want := wt
		if i == 0 {
			want = project
		}
		if !strings.EqualFold(filepath.Clean(c["cwd"].(string)), want) {
			t.Fatalf("round %d ran in %v, want %s", i, c["cwd"], want)
		}
		var task map[string]any
		json.Unmarshal([]byte(c["hub_task"].(string)), &task)
		if c["headless"] != "1" || c["headless_child"] != false || c["build_token"] != false || c["build_actor"] != false || task["hub"] != card.ID {
			t.Fatalf("round %d environment %v", i, c)
		}
	}
	last := calls[2]["argv"].([]any)
	joined := strings.Join(func() []string {
		var s []string
		for _, a := range last {
			s = append(s, a.(string))
		}
		return s
	}(), " ")
	if !strings.Contains(joined, "/tentaqles:build resume demo --headless") || !strings.Contains(joined, "--resume s-1") {
		t.Fatalf("resume round argv %s", joined)
	}
	acts, _ := os.ReadFile(store.ActivityPath("acme"))
	for _, verb := range []string{`"run.start"`, `"decision.ask"`, `"decision.answer"`, `"review.approve"`, `"run.end"`} {
		if !bytes.Contains(acts, []byte(verb)) {
			t.Fatalf("activity.jsonl lacks %s", verb)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/doctor/ ./test/integration/ -run 'TestHub' -count=1; cd plugin; python -m pytest tests/test_hub_docs.py -q`
Expected: the doctor tests FAIL with `undefined: hubChecks`; the integration test PASSES or SKIPs (it only uses earlier tasks; it is added here as the end-to-end check); the docs tests FAIL (`/tentaqles:hub` row missing, `43` vs `44`).

- [ ] **Step 3: Implement the doctor checks**

`internal/doctor/hub.go`:

```go
package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

const hubDefaultPort = 7470

// hubChecks reports, informationally, a hub database that cannot be opened and cards locked by
// runs that are not running (spec A §13.5). It opens the database read-only and writes nothing.
// It returns whether the workspace has a hub database at all.
func hubChecks(ws string, add func(level, code, ws, msg, fix string)) bool {
	if _, err := os.Stat(store.DBPath(ws)); err != nil {
		return false
	}
	st, err := store.OpenReadOnly(ws)
	if err != nil {
		add("info", "hub-db-unreadable", ws, "hub database cannot be opened: "+err.Error(),
			"move "+store.DBPath(ws)+" aside; tq hub serve starts a fresh board")
		return true
	}
	defer st.Close()
	locks, err := st.Locks()
	if err != nil {
		add("info", "hub-db-unreadable", ws, "hub database cannot be read: "+err.Error(),
			"move "+store.DBPath(ws)+" aside; tq hub serve starts a fresh board")
		return true
	}
	serving := false
	for _, i := range serveinfo.ReadAll() {
		if i.Serves(ws) && serveinfo.Ping(context.Background(), i) {
			serving = true
		}
	}
	for _, l := range locks {
		ended := !slices.Contains(store.ActiveStatuses, l.RunStatus)
		if !ended && !(l.RunStatus == store.StatusRunning && !serving) {
			continue
		}
		fix := "start tq hub serve: it fails interrupted runs and releases their locks"
		if !ended {
			fix = "tq hub cancel " + l.RunID + ", or start tq hub serve (it recovers interrupted runs)"
		}
		add("info", "hub-stale-lock", ws, fmt.Sprintf("card %s is locked by run %s, which is %s", l.CardID, l.RunID, l.RunStatus), fix)
	}
	return true
}

// hubPortCheck reports another program on the hub's port (a live tq hub there is fine).
func hubPortCheck(port int, add func(level, code, ws, msg, fix string)) {
	if port == 0 {
		port = hubDefaultPort
	}
	if _, ok := serveinfo.FindByPort(context.Background(), port); ok {
		return
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return
	}
	conn.Close()
	add("info", "hub-port-busy", "", fmt.Sprintf("port %d (tq hub's) is in use by another program", port),
		"tq hub serve --port <free port>, or set hub.port in config.yaml")
}
```

In `internal/doctor/doctor.go`, declare `hubUsed := false` next to `verifyCovers := map[string]bool{}` (before the `for _, w := range all` loop), and inside the loop extend the trusted block:

```go
		if trusted {
			claudeChecks(&w, add)
			dataBlockMissing(w.Name, w.Root, w.Manifest.Data, add)
			if hubChecks(w.Name, add) {
				hubUsed = true
			}
		}
```

and immediately before the line `// The pre-tq shell branch is active, so nothing tq sets is in effect here.` (after the workspace loop and the global git checks), add:

```go
	if hubUsed {
		hubPortCheck(cfg.Hub.Port, add)
	}
```

- [ ] **Step 4: Write the docs**

`docs/HUB.md`:

````markdown
# `tq hub` — a local board that runs agent-ready cards

`tq hub` keeps one board per client workspace and runs cards headless through the
plugin's gated build engine (`/tentaqles:kickoff`, `/tentaqles:feature`,
`/tentaqles:fix`, `/tentaqles:build`). Every human stop of a run becomes a decision
waiting on you; you answer it from the page, from `tq hub answer`, or in your terminal
with `build-gate answer`. Nothing leaves the machine.

## Quick start

```powershell
cd C:\work\acme                       # a trusted workspace
tq hub add "Export invoices" --kind feature --tag agent-ready --project apps/billing --body "CSV per month."
tq hub serve                          # http://127.0.0.1:7470/
tq hub move c-xxxxxxxxxxxx "in progress"   # an agent-ready card in an active lane starts a run
tq hub list --waiting                 # what the run waits on
tq hub answer c-xxxxxxxxxxxx d1a2b3c4 --choice approve
tq hub review c-xxxxxxxxxxxx --open   # evidence from the gate's records; Approve or Request changes
```

## One workspace or all of them

| | `tq hub serve` | `tq hub serve --all` (or `hub: {mode: all}` in `$TQ_HOME/config.yaml`) |
|---|---|---|
| serves | the trusted workspace containing the current folder | every trusted workspace under the registered bases; untrusted ones are listed as unavailable and never opened |
| databases | one | one per workspace, opened on first use (those that already have a board open at start so their queues run) |
| pages | that board | a workspace switcher; `/waiting` lists every workspace's decisions, labelled |
| runs | that workspace's environment | each card's own workspace environment; concurrency per workspace |

The mode is fixed when the hub starts. A second hub on the same port stops with a
message naming the running one; another port works, but a workspace is served by one
hub at a time (a second hub refuses it). `tq hub status` lists running hubs.

Default bind `127.0.0.1:7470` (`--port`, `hub.port`). Any other `--bind` needs a token:
`--token T`, or one is generated and printed once; a browser on another machine opens
`/login?token=<token>` once. Every POST also needs a same-origin request; the Host header
is checked against the bind (DNS rebinding).

## Cards, lanes, runs

Lanes: `backlog`, `todo`, `in progress` (kind `active`), `review`, `done`. Kinds:
`project` → `/tentaqles:kickoff`, `feature` / `analysis` / `chore` → `/tentaqles:feature`,
`fix` → `/tentaqles:fix`, `hotfix` → `/tentaqles:fix --hotfix` (only this kind). The
prompt is the card's title and body; context comes from the project's `CLAUDE.md`, PRD
and skills. A run starts when an `agent-ready` card enters an active lane, on
`tq hub run <card>`, or when an answer resumes it. Wakeups coalesce; a card is checked
out by one run at a time.

Every round runs `claude -p <prompt> --output-format json` in the card's project folder
with the workspace's environment plan (as `tq run` builds it), `TENTAQLES_HEADLESS=1` and
`TQ_HUB_TASK={"hub","workspace","kind","external"}`; never with a hub token, never
with `--dangerously-skip-permissions` unless `tq allow --bypass` was run for the workspace.
When the build creates its worktree, the session ends with
`TQ_HUB_RELOCATE <worktree> <slug>` and the next round starts there. After each round
the hub reads `build-gate state <slug> --json`: pending decisions → `waiting`; a halted
or rolled-back build → `failed`; a non-zero exit with no progress → one retry after two
minutes; anything else that ends without a stop → `failed` with the transcript tail.

Statuses: `queued running waiting finished failed cancelled over-budget`.

## Budgets

Per run: 2,000,000 tokens (input + output + cache write), 10 USD, 120 minutes, 12
rounds, or the card's `--budget tokens=…,usd=…,minutes=…,rounds=…`. Reaching one stops
the run (`over-budget`), comments the numbers and asks "raise-and-resume / stop".
When Claude Code reports no cost, it is computed from the usage and the model; when it
reports neither, the round is marked "cost unknown" and only rounds and minutes count it.

## Decisions

The hub mirrors the gate's decision records (`plan-approval`, `result`, `judge`,
`question`) and adds its own `permission` kind (a refused tool: `allow-once`; a budget
stop: `raise-and-resume`). Answering from the hub mints a hub actor token
(HMAC-SHA256 with the gate key, actor `hub:<your user>`) for one `build-gate answer` call;
the gate validates it and applies the consequence. The hub never calls `approve`,
`finish`, `judge`, `dod --override` or `unlock-tests`. If you answered in the terminal
first, the hub notices on its next read and resumes. Agent sessions
(`TENTAQLES_HEADLESS=1`, `CLAUDE_PLUGIN_ROOT` or `CLAUDECODE=1`) may add cards and
comment; they cannot answer, approve, move a card to done or set a budget.

## Review page

`/cards/<id>/review` (or `tq hub review <card> --open|--html`): goal, approach (the plan
record, flagged when it changed since approval), what was done (commits since the base,
diff summary, PR link), where it stands (checkpoints, reviewer verdicts, definition of
done), evidence (verify records with command, exit code, tree and time), cost per round,
feedback (Request changes) and Approve. Status and evidence come only from
`build-gate state --records`. On approval the page is exported to
`$TQ_HOME/hub/<workspace>/runs/<run>.review.html`.

## Files and privacy

`$TQ_HOME/hub/<workspace>/hub.db` (SQLite, WAL), `runs/<run>.log`, `activity.jsonl`,
user-only permissions. Every stored text is masked with the workspace's data classes
(`data.classes` plus secrets; every detector when the workspace is not classified).
`tq insights` reads `activity.jsonl` for its Hub section.

## Commands

```
tq hub serve [--all] [--port N] [--bind ADDR] [--token T] [--no-notify]
tq hub add "<title>" [--body TEXT|--body-file F] [--kind K] [--lane L] [--tag T]... [--parent ID] [--project DIR] [--budget …] [--json]
tq hub list [--lane L] [--kind K] [--tag T] [--waiting] [--json]
tq hub show <card> [--json]
tq hub move <card> "<lane>" [--cancel-run]
tq hub run <card>
tq hub answer <card> <decision> --choice C [--note TEXT]
tq hub review <card> [--open] [--html]
tq hub open [<card>]
tq hub cancel <run>
tq hub comment <card|-> "<text>"        # - is the card in TQ_HUB_TASK
tq hub status
```

All take `--workspace NAME` and `--timeout D` (skills pass `--timeout 2s`). They talk to
a running hub's API when one serves the workspace, else to the store directly.

## Troubleshooting

`tq doctor` reports `hub-db-unreadable`, `hub-stale-lock` and `hub-port-busy` (all
informational, each with a fix). Test seams: `TQ_HUB_CLAUDE_CMD` and `TQ_HUB_GATE_CMD`
(JSON argv) replace `claude` and the plugin's `build-gate.py`; `TQ_HUB_PLUGIN_ROOT` and
`TQ_BASH` override plugin and bash discovery. `tq hub serve --drain` runs the queue to the
next stop and exits.
````

In `CLAUDE.md` (monorepo root), add after the `cmd/tq/`, `internal/` bullet:

```markdown
- `internal/hub/` — `tq hub` (release 2, spec A): per-workspace SQLite board under `$TQ_HOME/hub/<workspace>/` (`store`, `activity`), headless runner (`runner`, `adapter`, `gate`, `notify`), web page and API (`web`, `serveinfo`). `docs/HUB.md`.
```

and in the `docs/` bullet replace `and `INSIGHTS.md` (`tq insights`).` with:

```markdown
`INSIGHTS.md` (`tq insights`), and `HUB.md` (`tq hub`: board, headless runs, decisions, review page, budgets).
```

`plugin/CHANGELOG.md`, under `## [Unreleased]`:

```markdown
### Added

- **`/tentaqles:hub`.** Lists the decisions hub runs are waiting on (`tq hub list
  --waiting --json`, bounded to 2 s), explains them, adds and shows cards, and prints the
  exact `tq hub answer` line for the user; it never answers itself.
- **Hub task block.** When `TQ_HUB_TASK` is set, `feature`, `fix` and `kickoff` pass it to
  the planner, which writes it verbatim as the plan's top-level `task`
  (`{"hub","workspace","kind","external"}`). The gate validates the shape, refuses a `task`
  the environment did not set or changed, snapshots it at approval and reports the
  approved one in `state --json`.
- **`build-gate state <slug> --json --records`.** Adds the gate's own signed records (plan
  summary and checkpoints with their approval status, verify evidence, reviewer verdicts,
  DoD) for the hub's review page.
- **Headless worktree hand-off.** A headless build ends Phase 0 with the last line
  `TQ_HUB_RELOCATE <absolute worktree path> <slug>`, once per slug; the hub starts the next
  round in the worktree.
- **`ship` posts the PR link** to the hub card (`tq hub comment -`, bounded, failure
  ignored) when `TQ_HUB_TASK` is set.
- **Session preamble** prints "N decisions waiting on you (tq hub open)" from a read-only
  SQLite read bounded to 300 ms.

### Changed

- A plan's `task` block in the old `{"hub_id", "external"}` shape is refused; the hub
  writes the new shape.
```

`plugin/README.md`: in the "Engineering-practice skills" table, after the `/tentaqles:loop` row, add:

```markdown
| `/tentaqles:hub` | See what `tq hub` runs are waiting on, in plain language, add or show cards; prints the `tq hub answer` line for you (answering is yours) |
```

`plugin/CLAUDE.md`: change `43 skill directories` to `44 skill directories`, and add under "## Key conventions":

```markdown
- **Hub-launched sessions**: `tq hub` runs `claude -p` with `TENTAQLES_HEADLESS=1` and `TQ_HUB_TASK` (never `TENTAQLES_HEADLESS_CHILD`, so the session hooks run; never `TQ_BUILD_ACTOR`/`TQ_BUILD_TOKEN`). On-ramps copy `TQ_HUB_TASK` verbatim into the plan's `task`; a headless Phase 0 ends with `TQ_HUB_RELOCATE <worktree> <slug>`. Skills call `tq hub` with `--timeout 2s` and never answer, approve or move a card to done (`/tentaqles:hub` prints `tq hub answer` for the user).
```

- [ ] **Step 5: Run everything**

Run: `go vet ./...; go test ./... -count=1`
Expected: PASS (the integration test SKIPs only without Python or git).

Run: `cd plugin; python -m pytest tests -q`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/doctor/hub.go internal/doctor/doctor.go internal/doctor/hub_test.go test/integration/hub_test.go docs/HUB.md CLAUDE.md plugin/CHANGELOG.md plugin/README.md plugin/CLAUDE.md plugin/tests/test_hub_docs.py
git commit -m "feat(hub): doctor hub checks, docs and the end-to-end hub test"
```
