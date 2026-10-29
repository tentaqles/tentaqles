# Spec C: the data engineering pack and the ML pack

**Parent:** `../../../docs/superpowers/specs/2026-10-20-development-process-design.md`, §13 release 3. **Sibling:** spec D (cloud and database packs, release 4), which builds on the pack framework in §2 of this spec.
**Status:** draft for approval, 2026-10-28.
**Out of scope:** the UI gate (prototype, visual and behaviour reviewers).

Release 3 adds two packs to the development process. A pack is a set of checks, guard rules, reviewer agents, skills and templates for one kind of work. The checks run in the definition of done, the guard rules run in the PreToolUse hook, and the reviewers join the build crew when triage raises their flag.

- **Data pack:** data contracts (ODCS v3) with breaking-change rules, dbt gates, pipeline review, and a warehouse cost guardrail.
- **ML pack:** a leakage lint, a reproducibility checklist, a model promotion gate, `MODEL_CARD.md`, and a drift check.

## 1. Decisions, goals and success criteria

### 1.1 Decisions

| # | Topic | Decision | From |
|---|---|---|---|
| 1 | Tooling | **Built-in plus external.** tq ships pure-Go lints for the core rules, so they always run. Established external tools run when they are installed. Once the manifest declares a tool, its check is a **required** definition-of-done item: declared but missing = fail, undeclared and missing = skip, undeclared but installed = runs as an advisory check. | human |
| 2 | Stacks | All four: **dbt + SQL warehouses** (Snowflake, BigQuery, Databricks SQL, Postgres as a warehouse), **Python pipelines** (Airflow, Dagster, Prefect, plain pandas/polars/PySpark jobs), **Python ML** (scikit-learn, PyTorch, MLflow-style registries), **Microsoft Fabric / Synapse** (notebooks, pipelines, lakehouse/warehouse). | human |
| 3 | UI gate | Not in this release. | human |
| 4 | Framework ownership | This spec defines the shared pack framework (§2). Spec D conforms to it. | human |
| 5 | Where checks run | In `tq pack check`, called by the build gate's definition of done. A hook never runs a pack check that does I/O. Pack guard rules are pure functions over the tool call and the loaded manifest. | designer |
| 6 | Pack config layers | The manifest `packs:` block is the trusted layer. A repo file `tq-packs.yaml` may only add to it or tighten it, never loosen it. The gate freezes the merged config's hash at plan approval. | designer |
| 7 | Model promotion | A promotion to a protected stage or alias is a hub **approval** decision that a human answers. Only a human may run `tq pack ml promote apply`, which checks the approval and runs the project's declared promotion command. Agents are denied the registry promotion commands. | designer |
| 8 | Warehouse access in checks | Built-in checks never connect to a warehouse. A check that would (`dbt build`, cost estimates) is opt-in, targets the dev target only, and needs `tq dotenv run` around the call for credentials. | designer |
| 9 | Drift data | tq never queries data for drift. It reads either a local CSV or a profile JSON, which holds aggregates only (bins and counts) and hashes the category labels of classified columns. The project produces these files. | designer |

### 1.2 Goals

- A dbt, pipeline, ML or Fabric change gets checks that suit its stack, with no setup beyond `packs: { data: {enabled: true} }`.
- A breaking data-contract change can't merge without a major version bump. The PR states it as breaking.
- A warehouse query an agent runs has a cost bound, or a human confirms it.
- A training-code change can't skip seeds, a pinned data snapshot or a locked environment without a recorded failure.
- A model reaches production only through a recorded human decision, tied to the exact model card, metrics, drift result and code commit.
- The framework lets spec D add the `cloud` and `db` packs without touching the files this spec's workstreams own.

### 1.3 Non-goals

- Running pipelines, training or backfills from tq. tq only checks, estimates and gates.
- A column-level lineage engine. The PII-to-output check is a heuristic, and the reviewer covers the rest.
- A model registry client in Go. The project's own command does the registry work.
- Data quality monitoring in production (Great Expectations, Soda and similar). A later release may add them as external tools.
- Estimating the cost of Spark jobs before they run. Spark is bounded through pool, cluster and capacity size instead.
- R, Scala and Java pipelines; SQL Server as an OLTP database (that is spec D).

### 1.4 Success criteria

- With a repo that has no `packs:` block, `tq pack check` finishes in under 200 ms and prints `no packs enabled`.
- On a 2,000-file fixture repo, the built-in checks of both packs finish in under 5 s.
- Golden tests cover every row of the contract breaking-change table (§3.2) and every leakage rule (§4.2). The `ml-starter` and `dbt-contract-starter` templates pass their own pack checks with no findings.
- The DoD records one `pack:<pack>/<check>` item per check that ran. A declared but missing tool fails it.
- `bq query` without `--maximum_bytes_billed` is denied in a workspace with the data pack and BigQuery enabled. A registry promotion command is denied to an agent.
- `tq pack ml promote apply` refuses to run without an approved decision whose evidence matches the files at HEAD.
- `go test ./...`, the plugin suite, the templates job and CI are green. No test calls a warehouse, a cloud, a registry or the network.

## 2. Pack framework (shared with spec D)

### 2.1 Concepts

| Term | Meaning |
|---|---|
| pack | A named group: `data`, `ml` (this spec), `cloud`, `db` (spec D). It holds checks, guard rules, triage flags, reviewers, config keys, doctor probes and subcommands. |
| check | One rule with the id `<pack>/<name>`, for example `data/contract-breaking`. It is either **built-in** (pure Go) or **external** (it wraps a tool). |
| result | `ok`, `fail` or `skip`, with a detail line, a `required` bit and findings. |
| finding | `file:line`, a level (`error` or `warn`), a rule id and a message. A check fails only on an `error` finding. `warn` findings are listed and never fail. |
| required | Built-in checks of an enabled pack are always required. An external check is required only when its tool is declared in `tools:`. |

### 2.2 Manifest `packs:` block (trusted, hash-pinned by `tq allow`)

```yaml
packs:
  data:
    enabled: true
    tools: [dbt, sqlfluff]          # external tools that become required checks
    timeouts: { dbt: 600s }         # optional per-tool override (cap 1800s)
    tool_paths: { dbt: C:/tools/dbt/dbt.exe }   # optional; trusted layer only
    # ...pack-specific keys (§3.1), validated by the pack
  ml: { enabled: true, tools: [ruff] }
  cloud: { ... }                    # spec D
  db: { ... }                       # spec D
```

- **Go type** (`internal/manifest/packs.go`): `Manifest.Packs map[string]PackConfig`. `PackConfig{Enabled *bool; Tools []string; Timeouts map[string]Duration; ToolPaths map[string]string; Baseline string; Raw yaml.Node}`. `Raw` is the whole `packs.<name>` node, so a pack decodes its own settings (environments, migration paths, drills, contracts, models) with line numbers for its errors.
- **Validation:** Load rejects an unknown pack name (known: `data`, `ml`, `cloud`, `db`) and a tool name with characters outside `[a-z0-9-]`. It runs the secret scan, as for the rest of the manifest. It then runs the validator registered with `manifest.RegisterPackValidator(name string, fn func(yaml.Node) error)`, so an unknown pack-specific key is an error. `internal/packs/<pack>` calls this from `init` (through `packs.RegisterPack`, which composes the per-key validators of §2.4). `manifest` never imports `packs`, so there is no import cycle. In a test binary without a pack linked, the pack's keys are kept and not validated.
- **Defaults:** if `enabled` is absent and the block is non-empty, the pack is enabled. `enabled: false` turns it off.

### 2.3 Repo layer `tq-packs.yaml` (untrusted, add-only)

The file sits at the repo root and uses the same schema as `packs:`. It lets one repo inside a client workspace hold its own contract paths, models and thresholds. When it is merged with the manifest, the stricter value always wins:

| Key kind | Merge rule |
|---|---|
| `enabled` | OR: a repo may enable a pack, but not disable one the manifest enabled |
| `tools` | union |
| path lists (`contracts.paths`, `ml.paths.train`, …) | union |
| upper bounds (cost caps, drift `warn`/`fail`, `valid_for`) | minimum |
| lower bounds (`drift.metrics.<m>.min`) | maximum |
| `tool_paths` | ignored in the repo file (executable locations are trusted-layer only) |
| commands (`ml.promotion.command`, `ml.drift.current_command`) | allowed. They run only from human-confirmed commands (§4.4) or frozen DoD runs (§2.8), like the `tq-project` commands |

`tq pack list --json` prints the merged config and its `config_hash` (sha256 over canonical JSON). The pack guard `tq/pack-config-change` asks before an agent edits `tq-packs.yaml` (§5.3).

### 2.4 Go package `internal/packs`

```go
type Status string // "ok" | "fail" | "skip"
type Finding struct {
    File  string `json:"file"`;  Line int `json:"line,omitempty"`
    Level string `json:"level"`  // error | warn
    Rule  string `json:"rule"`;  Message string `json:"message"`
}
type ToolRun struct{ Name, Version, Path string; ExitCode int; DurationMS int64; TimedOut bool }
type Result struct {
    ID, Pack string; Status Status; Required bool; Detail string
    Findings []Finding; Tool *ToolRun
}
type ChangedFile struct{ Path, OldPath string; Status byte } // A M D R, slash paths
type Input struct {
    Root     string               // repo root
    Base     string               // ref the old side is read from ("" = no old side)
    Changed  []ChangedFile
    All      bool                 // --all: every tracked file counts as changed
    Manifest *manifest.Manifest   // nil outside a trusted workspace
    Config   Config               // merged pack config (2.3), typed accessors
    Git      GitReader            // Show(ref, path) ([]byte, error); ListFiles()
    Tools    ToolRunner           // 2.6
    Baseline Baseline             // 2.6a; Match(checkID, f Finding) bool
    Budget   time.Time            // deadline for the whole run
}
func (in *Input) Workspace() string   // workspace name (registry), "" outside a trusted workspace
func (in *Input) WorkspaceRoot() string // the folder holding .tentaqles.yaml
func (in *Input) StateDir() string    // $TQ_HOME/packs/<ws>/<pack>/, created on first call (0700)
type Check struct {
    ID, Pack string
    Kind     Kind                 // Builtin | External
    Tool     string               // External only: the tool name used in tools:
    Applies  func(in *Input) bool // cheap: paths and config only, no file reads beyond config
    Run      func(ctx context.Context, in *Input) Result
}
func Register(c Check)                  // from init; panics on duplicate id
func RegisterPack(p Pack)               // name, config keys, flags, reviewers, doctor, commands
func RegisterGuard(g Guard)             // 2.10
func Checks(pack string) []Check
```

- `Pack{Name; ConfigKeys map[string]func(yaml.Node) error; Flags []Flag; Hints []Hint; Doctor func(*Input) []doctor.Finding; Commands func() []*cobra.Command}`. A `Hint{Glob string; Contains string; Why string}` is a detection hint, for example `dbt_project.yml`, or `*.py` containing `from airflow`. The framework's `packs-suggest` doctor finding (§7) evaluates the hints of disabled packs over at most 2,000 tracked files and 64 KiB per file. Hints never enable a pack. Several workstreams can each register their own config keys, flags and commands for one pack: `RegisterPack` merges entries with the same `Name`, and a duplicate key or command name panics.
- **Pack subcommands** are mounted under `tq pack <pack>`: `tq pack data cost estimate`, `tq pack ml drift`, `tq pack ml promote …` here, and `tq pack cloud plan|approve`, `tq pack db drill` in spec D. The framework owns the `tq pack` and `tq pack <pack>` parents.
- **State directory (confirmed):** `StateDir()` is per pack: `$TQ_HOME/packs/<ws>/<pack>/`, for example `$TQ_HOME/packs/acme/cloud/`. It is where a pack keeps its own records (spec D's plan store and ledger, this spec's drift results cache). Only tq writes there. The guard rule `tq/hub-store-write` of spec D covers `$TQ_HOME/packs/**`, so agents can't edit it.
- **Shared scanners** (`internal/packs/scan`, owned by the framework): a Python line tokenizer (strings and comments stripped, call sites with receiver and keyword args, top-level vs indented); notebook cell extraction for `.ipynb` JSON, Fabric `notebook-content.py` (cell markers) and Synapse notebook JSON; a SQL tokenizer (statements, `FROM`/`JOIN` relations, `SELECT *`, Jinja blocks kept opaque); YAML helpers over `yaml.v3` that keep line numbers. **No new third-party Go dependency.** YAML is `gopkg.in/yaml.v3`, which is already required, and everything else is stdlib.
- **Runtime budget:** checks run in parallel (`min(4, NumCPU)` workers). A built-in check whose run passes 10 s, or the run's deadline, is cancelled and returns `fail` (required) with the detail `timed out`.
- **Events:** each run appends one aggregate-only line per result to `$TQ_HOME/packs/events.jsonl`: ts, workspace, check id, status, required, duration, finding counts by level. It never writes file names or messages. `tq insights` reads it (§7).

### 2.5 Selecting what changed

| Flag | Changed set | Old side (`Base`) |
|---|---|---|
| (none) | working tree vs `HEAD`, plus untracked non-ignored files | `HEAD` |
| `--staged` | `git diff --cached --name-status` | `HEAD` (new side is the index, `git show :path`) |
| `--base <ref>` | `git diff --name-status <ref>` (working tree vs ref) plus untracked | `<ref>` |
| `--paths a b …` | the listed paths (status M, or A when absent at `HEAD`) | `HEAD` |
| `--all` | every tracked file, status A | none (diff checks skip) |

All git calls use `-z`, have a 10 s timeout, and run with the identity env (as `tq decide triage` does today).

### 2.6 External tool runner (`internal/packs/toolrun`)

- **Probe order:** `tool_paths.<tool>` (manifest only), then the project's `.venv/Scripts/<tool>.exe` or `.venv/bin/<tool>`, then `PATH` (`exec.LookPath`). Not found: `skip` when undeclared, `fail` when declared (detail: `declared in packs.<p>.tools but not installed; install it or remove it from tools`).
- **Version:** `<tool> --version` with a 5 s timeout, parsed to semver where possible and recorded in `ToolRun.Version`. A pack may declare a minimum version per tool. A declared tool below the minimum fails.
- **Execution:** argv only (never a shell string). Cwd is the repo root or the pack's project dir. Timeout comes from `timeouts.<tool>`, otherwise the check's default, and is capped at 1800 s. On timeout the whole process tree is killed: a Job Object on Windows (`taskkill /T /F` as the fallback), a process group elsewhere. `cmd.WaitDelay` is 5 s, so a child that holds the pipes can't stall the run.
- **Environment:** the parent environment is passed through unchanged, so `tq dotenv run -- tq pack check` supplies credentials. The runner adds `DO_NOT_TRACK=1` and the tool's own telemetry switch where one is known (dbt: `DBT_SEND_ANONYMOUS_USAGE_STATS=false`; *Assumption: verify the variable name for the pinned dbt version*). It never reads `.env*` files, never prints or logs the environment, and never puts an env value in a result.
  - **Amended 2026-10-29 (framework round 5):** tools no longer get the parent environment unchanged. They get a filtered one: the base list (`PATH`, `HOME`, `USERPROFILE`, `TEMP`, `TMP`, `TMPDIR`, `SystemRoot`, `ComSpec`, `PATHEXT`, `LANG`, `LC_*`) minus anything named like a secret, plus `DO_NOT_TRACK` and the telemetry switches. Each check declares in `Check.Env` (or a run in `Spec.PassEnv`) the other variables its tool needs, such as `DBT_PROFILES_DIR`, `DBT_TARGET`, a dbt profile's `env_var()` names, or `APPDATA`/`LOCALAPPDATA` on Windows. A declared name passes even when it looks like a secret, so the lists stay tight, and `tq dotenv run -- tq pack check` supplies a credential only through a declared name. A tool resolved inside the repository (the project `.venv`, `node_modules/.bin`) never gets a declared secret-looking name, and the check gets a `tq/tool-env` warning naming it (round 6): keep such a tool in `tool_paths` or outside the repo when it needs a credential from the environment.
- **Output:** stdout and stderr are capped at 1 MiB each. The check's parser reads the tool's machine format (`--format json` and similar). When parsing fails, the detail carries the last 20 lines, passed through `redact` with the workspace's `exit_classes`.
- **Scratch:** tools that write artifacts are pointed at `$TQ_HOME/packs/<ws>/scratch/<run-id>/` where they allow it (dbt `--target-path`, `--log-path`; *Assumption: both flags exist in dbt ≥ 1.8*). This keeps the DoD's `stable_tree` check valid. The scratch directory is removed after the run.

### 2.6a Baseline ratchet (external-tool findings)

- **Config:** `packs.<pack>.baseline: <repo path>` (for example `.tq/sqlfluff-baseline.json`). It holds entries `{check, tool, rule, location, fingerprint}`:
  - `location` is a repo path, or a resource address for IaC;
  - `fingerprint` is a sha256 of the rule plus the normalised location plus the finding message with digits stripped;
  - it never holds a value from the scanned file.
- **Effect:** a finding of an external check that matches a baseline entry is downgraded to `warn`, and a new one keeps its level. Built-in checks don't use the baseline: they use inline suppressions (`tq:allow <rule>: <reason>`, which the pack defines per syntax).
- **Writing:** `tq pack baseline [--pack X]` writes or refreshes the file from the current findings. It is human-only, with the same refusal as `tq secret`. It can only drop entries that no longer match, or add the current findings when run by the human.
- **Growth is visible:** when the diff grows a baseline file, the framework adds a `warn` finding `baseline-grew` with the count and raises the triage flag `baseline_change`, which maps to the pack's reviewer. `tq/pack-config-change` asks before an agent edits a baseline file.
- **Users:** the data pack (`data/sqlfluff`, `data/datacontract-lint`, `ml/ruff`) and spec D's cloud pack (checkov, trivy, conftest) use the same file format. Spec D's `internal/packs/cloud/baseline.go` is not needed.

### 2.7 CLI

| Command | What it does | Exit |
|---|---|---|
| `tq pack list [--enabled] [--json]` | the packs, enabled or not, the merged config and `config_hash`, and each check with kind, tool and required | 0 |
| `tq pack check [--pack X]… [--staged \| --paths … \| --base <ref> \| --all] [--json] [--builtin-only] [--budget 600s]` | runs the applicable checks | 0 when no required check failed; 1 when one failed; 2 on a usage or config error |
| `tq pack doctor [--json]` | per tool: declared?, installed?, path, version, minimum; plus pack-specific probes (§7) | 0, or 1 when a declared tool is missing |
| `tq pack crew --flags a,b [--json]` | the reviewer agents that pack flags map to (§2.9) | 0 |
| `tq pack approvals [--pack X] [--json]` | pending, answered, used and expired pack approvals for this workspace (§2.11) | 0 |
| `tq pack baseline [--pack X]` | human only: write or refresh the external-tool baseline (§2.6a) | 0 |
| `tq pack <pack> <subcommand>` | subcommands that packs register (§2.4) | per command |

`--json` output is schema `tq.packs/v1`: `{"schema", "workspace", "base", "config_hash", "results": [Result…], "summary": {"ok", "fail", "skip", "required_fail"}}`. Text output is one line per result (`ok   data/contract-breaking: 2 contracts, no breaking change`), plus up to 20 findings per check as `file:line level rule message`.

### 2.8 Definition of done (`plugin/tentaqles/workflow/gate.py`)

- **At approval** (`cmd_approve`), the gate runs `tq pack list --enabled --json` (timeout 10 s) and freezes `approved.packs = {"enabled": [...], "config_hash": "..."}`. If tq is missing it freezes `{"enabled": [], "tq": "missing"}`.
- **In `cmd_dod`**, after the frozen commands:
  - Frozen `enabled` is empty and tq is missing: `pack` is a skip (`skip: no packs frozen at approval`).
  - Otherwise the gate runs `tq pack check --json --base <approved.base> --budget <B>s` with subprocess timeout `B + 30` (`B` defaults to 600, from the `tq-project` block's `pack_budget` when set).
  - It records `pack:config` as ok when the current `config_hash` equals the frozen one. Otherwise it fails with "pack config changed since approval; re-approve the plan".
  - It records one item per result: id `pack:<pack>/<check>`; `ok` is true for `ok`; for `fail` it is false when `required` and None (advisory) when not; for `skip` it is None. The detail is the result detail plus the first 5 findings.
  - When tq is missing, crashes, times out or prints invalid JSON while packs were frozen, it records `pack:run` = false with the reason (fail closed).
- The Stop hook keeps reading `dod.json` only and never runs tq.
- **Reviewers:** `REVIEWERS` stays the core list. `record-review` also accepts a name matching `^[a-z][a-z0-9-]*-reviewer$` when `agents/<name>.md` exists in the plugin, so pack reviewers (this spec's and spec D's) need no gate edit.

### 2.9 Triage and the crew

- **Deterministic flags.** Each pack registers `Flag{Name, Reviewers []string, Raise func(in *Input) bool}`. `Raise` is path- and config-based only. `tq decide triage` (in `internal/cmd/decide.go`) builds an `Input` from the same changed-file list and appends every raised flag, as it does for `classified_data` today. Any raised flag makes the risk `high`.
- **Reviewers on core flags.** A `Flag` with no `Raise` (and no `Question`) whose `Name` is an existing core flag (`schema`, `migration`, `rls`, `data_loss`, `destructive`) raises nothing. It adds its `Reviewers` to that core flag while the pack is enabled: when triage raises `migration`, `tq pack crew` also prints spec D's `db-reviewer`. `RegisterPack` panics on a reviewer-only flag naming anything outside that list, so a typo can't silently attach to nothing.
- **Jev questions.** A flag may also carry a `Question`. `decide.TriageWithQuestions(ctx, c, p, diff, names, extra)` merges the questions of enabled packs into the Jev call, so a disabled pack adds no question cost.
- **Crew.** `tq pack crew --flags <flags printed by triage>` prints the reviewers, de-duplicated. The `build` skill (step d.3) and the `feature` skill (expected crew) add every reviewer it prints. If an agent is not installed, the gate records nothing for it and tells the user, as it does today.

### 2.10 Guard rules from packs

There are two kinds:

- **Decision guards (Go):** `packs.RegisterGuard(Guard{ID: "tq/<name>", Pack, Events []string /* Bash, PowerShell, mcp regex, Edit… */, Supersedes []string, Check func(call policy.ToolCall, g GuardCtx) (policy.Action, string, bool)})`.
  - `GuardCtx` gives the merged config, `Workspace()`, `WorkspaceRoot()` and `StateDir()`, plus three bounded reads:
    - `ReadState(name)` reads a file under `StateDir()`, at most 1 MiB.
    - `ReadLocal(path, max)` reads one named local file inside the workspace root or the identity's or user's config dirs, for example `.terraform/environment`, Pulumi's selected-stack file, `azureProfile.json` or `supabase/.temp/project-ref`. `max` is capped at 1 MiB. It has the same time limit as `ReadState`. It refuses `.env*` (any path segment starting with `.env`), the secret-store paths of `tq/secret-store-read`, symlinks that resolve outside those roots, and `..`.
    - `HashState(path)` returns the sha256 of a file under `StateDir()`, up to 64 MiB, within a 40 ms budget. A larger file or a blown budget returns an error, and the guard then denies (fail closed).
  - The third return value, `handled`, means the guard owns this call. Every built-in rule match whose id is in `Supersedes` is then dropped. This is spec D's supersede step, for example the apply gate replacing the `tq/iac-apply` ask.
- **Static regex rules:** `internal/policy/builtin_packs.go`, created by F with an empty `packRules()` and the one-line `append` in `Builtin()`. Each rule carries a new `Pack` field, and spec D's D-GR fills the file.

`tq claude-hook` (`claudehook_policy.go`, owned by F) runs both kinds for **enabled packs** after the core rules and takes the strongest action. Pack workstreams register guards; they never edit `claudehook_policy.go`. Spec D's `claudehook_iac.go` and `claudehook_db.go` hold their guard functions and register them.

- **Bounds:**
  - no process and no network;
  - file access only through `ReadState`, `ReadLocal` (1 MiB each) and `HashState` (64 MiB, 40 ms), plus the already loaded manifest and `tq-packs.yaml`;
  - under 50 ms on a 64 KiB command, excluding a `HashState` call, which has its own 40 ms budget (benchmark tests for both);
  - a panic, a read error, a refused path or a blown budget is a deny (fail closed, like the existing guards);
  - guard functions never write. When a guard allows a call by consuming a single-use approval (it returns `Consume: <decision id>`), the claude-hook process appends the `consumed` line to the ledger (§2.11) before it returns the allow. If that append fails, the call is denied.
- **Same layers as built-ins:** `guard.disable`, `guard.enable` and `guard.actions` (ask/deny) in the manifest apply to pack guard ids. `tq guard list` shows them with their pack.

### 2.11 Approvals (`internal/packs/approval`)

A gated action (an ML promotion here, an apply or destroy in spec D) becomes a hub decision a human answers.

```go
type Request struct {
    Pack, Action, Subject, Target string   // "ml", "promote", "churn@7", "production"
    Evidence map[string]string              // name -> sha256 (files, results, git HEAD)
    Summary  string                         // redacted with exit_classes before storage
    Options  []string                       // first = the granting choice: ["approve","reject"], spec D ["apply","reject"]
    ValidFor time.Duration                  // pack default and cap (ml: 7d, max 30d; spec D: 60m, max 24h)
    CardID   string                         // the run's card (TQ_HUB_TASK) when there is one
    ProjectDir string
}
func Ask(ws string, r Request) (decisionID, cardID string, err error)
func Verify(ws, id string, evidence map[string]string) (Approval, error)
func MarkUsed(ws, id string) error          // also appended to StateDir()/ledger.jsonl
```

- **`Ask`** uses the run's card when there is one. Otherwise it creates a card (kind `chore`, lane `todo`, tag `<pack>-approval`), which agents are allowed to do. It then adds a hub decision of the existing kind **`permission`** whose `Data` carries `{pack, action, subject, target, evidence, expires_at}`. A set `data.pack` marks a pack approval, so the hub store schema does not change (spec D's choice). The hub runner ignores `permission` decisions with `data.pack` set: they never resume or requeue a run. The board's decision view shows the evidence list and the summary. F owns both hunks.
- **`Verify`** succeeds only when:
  - the decision is answered with the granting option by a human actor (the hub's `ActorKind` is not `agent`);
  - it has not expired and has not been used (ledger);
  - its evidence map equals the one recomputed now.
- **Answering:** the hub's rules apply unchanged. Agents never answer decisions, and `tq hub answer` from an agent session is refused. The board shows the evidence list and the summary.
- **The ledger** is `$TQ_HOME/packs/<ws>/<pack>/ledger.jsonl`, append-only, with one JSON line per event: `asked`, `answered`, `consumed`, `expired`. Each line has `{decision, pack, action, subject, target, evidence, expires_at, choice, actor_kind, ts}`, and none carries the note text.
  - **Writers:** `approval.Ask` writes `asked`. The hub's decision-answer handler writes `answered`: when the hub store records a human answer to a `permission` decision with `data.pack` set (in `Store` `UpsertDecision`'s answer path, the code behind both the board's answer endpoint and `tq hub answer`), it calls `ledger.RecordAnswer`, in the same process and before the answer is acknowledged. The writer lives in the small package `internal/packs/ledger`, which imports neither the hub store nor `internal/packs`, so the store can call it without an import cycle; `approval` uses both. A failed ledger write fails the answer, which the human can retry. `MarkUsed` (human-run apply) and the claude-hook process (guard consumption, §2.10) write `consumed`.
  - **Readers:** `Verify` and every guard read only the ledger, through `ReadState`. They never open the hub's SQLite store, so a guard needs neither the hub running nor the database.
  - **State:** the latest line per decision wins. A `consumed` decision is used up, and one past `expires_at` is expired.
  - **Protection:** spec D's `tq/hub-store-write` denies agent writes to `$TQ_HOME/packs/**/ledger.jsonl`. The key-isolation caveat of the build gate applies.
- **Two ways to consume an approval:**
  - A human-run command, as in this spec's `tq pack ml promote apply`.
  - A decision guard that checks the ledger at PreToolUse and allows the call once, as in spec D's apply gate. Spec D's binding of a saved-plan hash and an identity is entries in `evidence`, which the guard recomputes with `HashState` and `ReadLocal`.
- **Execution of a human-run apply:** a pack's `apply` command is human-only. It uses the same agent-session refusal as `tq secret`, then runs `Verify`, shows the exact command it will run, asks for a typed `yes` on a TTY, runs it, and then calls `MarkUsed` and comments the result on the card.

### 2.12 Skills, agents and docs layout

| Path | Owner |
|---|---|
| `plugin/skills/pack/SKILL.md` | framework. It covers `tq pack check`, `doctor`, `crew`, reading results, and fixing a declared-missing tool |
| `plugin/skills/pack/references/onramps/<pack>-<topic>.md` | each pack workstream. On-ramps read every file whose prefix is an enabled pack (§6) |
| `plugin/skills/<skill>/` | the pack workstream that owns the skill |
| `plugin/agents/<name>-reviewer.md` | the pack workstream. Read-only (Read, Glob, Grep), answer-key verdict, checklist under its skill's `references/checklist.md` |
| `docs/PACKS.md` | framework: overview, config, CLI, DoD, merge rules, and an index linking `docs/packs/*.md` |
| `docs/packs/<pack>-<topic>.md` | each pack workstream |

### 2.13 `tq doctor`, `tq insights`

- `internal/doctor/packs.go` (framework) calls each enabled pack's `Doctor` and adds the framework findings (§7).
- `internal/insights/packs.go` (framework) reads `events.jsonl` and reports by check id: pass and fail counts, declared-missing count, p95 duration, advisory fails, and approvals asked, approved, rejected, expired and median time to answer (§7).
  - Packs add their own aggregates through `packs.Count(pack, counter string, n int)`. These are event lines with a counter name, such as `apply_denied:expired` or `drift:fail`, and never paths or values.
  - Insights shows the counters grouped by pack, so spec D's D-OI needs no edit to `insights/packs.go`.
  - Pack-specific doctor sections live in each pack's own `doctor.go`, registered through `Pack.Doctor`.

### 2.14 Time budgets (hooks never stall)

| Where | Bound |
|---|---|
| PreToolUse pack guards | < 50 ms; no process or network; reads only through `ReadState`/`ReadLocal` (≤ 1 MiB) and `HashState` (≤ 64 MiB, 40 ms); one ledger append by the hook on consumption |
| any hook | never runs `tq pack check`. A future hook use must pass `--builtin-only --budget 3s` |
| `tq pack check` | `--budget` default 600 s, max 1800 s. Built-in check 10 s. Tool timeouts per check (§3, §4), capped at 1800 s |
| git calls | 10 s each |
| `--version` probe | 5 s |
| gate DoD subprocess | budget + 30 s |

### 2.15 Changes to the framework contract

No name from the original contract changed. Below are the refinements and additions, then the answer to each request in spec D §22 ("Contract differences from spec C §2").

**Refinements and additions:**

1. **Finding levels and `required`.** Findings carry `level: error|warn` (only `error` fails), and results carry `required`. An advisory (undeclared-tool) fail is recorded in the DoD as `ok: None`, not as false.
2. **Guards.** Decision guards register in Go (`packs.RegisterGuard`, with `Supersedes` and a bounded `ReadState`). Static regex rules go in `internal/policy/builtin_packs.go` with a `Pack` field. Both are active only when their pack is enabled, and both honour `guard.disable` and `guard.actions`.
3. **Repo layer `tq-packs.yaml`** (add-only, stricter wins), and **`config_hash` frozen at approval** (`pack:config` DoD item).
4. **Approvals API** over the existing hub decision kind `permission` with `data.pack` set, plus a pack ledger. There is no hub schema change.
5. **Extra CLI:** `tq pack crew`, `tq pack approvals`, `tq pack baseline`, `tq pack check --all --builtin-only --budget`, and pack subcommands under `tq pack <pack>`.
6. **Default selection** without flags is the working tree vs `HEAD` plus untracked files.
7. **Config validation per key:** `RegisterPack` merges per pack name, so several workstreams can own the keys of one pack.
8. **Shared scanners** in `internal/packs/scan` (Python, notebooks, SQL, YAML with lines).
9. **Reviewer names** are accepted by the gate when they match `*-reviewer` and the agent file exists.
10. **DoD items `pack:config` and `pack:run`**, beside `pack:<pack>/<check>`.

**Spec D §22 requests:**

| # | Spec D asked | Resolution |
|---|---|---|
| 1 | per-pack settings; `RegisterPackValidator` in `manifest`; raw YAML kept | **Adopted** (§2.2): `PackConfig.Raw yaml.Node`, `manifest.RegisterPackValidator(name, func(yaml.Node) error)`. |
| 2 | `StateDir()` and `Workspace()` on the run context | **Adopted** (§2.4), plus `WorkspaceRoot()`. Guards get the same through `GuardCtx`. |
| 3 | a `Commands()` hook mounted under `tq pack <pack>` | **Adopted** (§2.4). This spec's own commands moved under it too: `tq pack data cost`, `tq pack ml drift`, `tq pack ml promote`. |
| 4 | the baseline ratchet in the framework | **Adopted** (§2.6a). Spec D's `cloud/baseline.go` becomes a user of `Input.Baseline`. `tq pack cloud baseline` becomes `tq pack baseline --pack cloud`. |
| 5 | finding `Level` | **Already in** (§2.1, §2.4). |
| A | `GuardCtx.ReadLocal(path, max)` | **Adopted** (§2.10): named local files inside the workspace root or the config dirs, 1 MiB cap, never `.env*` or the secret stores. |
| B | `GuardCtx.HashState(path)` | **Adopted** (§2.10): up to 64 MiB within 40 ms, otherwise deny. |
| C | the ledger written on a human answer, and guards that read only the ledger | **Adopted** (§2.11): the hub's decision-answer handler (the store's answer path behind the board and `tq hub answer`) calls `ledger.RecordAnswer` (package `internal/packs/ledger`, no import cycle). The claude-hook process writes `consumed`. Guards and `Verify` read only `ledger.jsonl`. |
| D | reviewer-only flags on core flags | **Adopted** (§2.9): limited to `schema`, `migration`, `rls`, `data_loss`, `destructive`. |
| E | `Pack.Hints` for `packs-suggest` | **Adopted** (§2.4, §7). This spec's §3.1 detection hints are registered as data-pack `Hints`. |
| F | per-pack `StateDir()` | **Confirmed:** `$TQ_HOME/packs/<ws>/<pack>/`. |

**Spec D §22 hunks that F now owns,** so spec D's workstreams don't edit these shared files:

| Spec D planned | Now |
|---|---|
| D-GR: the `append(packRules()...)` line in `Builtin()` | F creates `builtin_packs.go` (empty `packRules()`) and the line. D-GR fills only `builtin_packs.go`. |
| D-CG: entries in `claudehook_policy.go`'s decision list, and the supersede step | F wires registered decision guards generically, with `Supersedes`. D-CG/D-DG register from `claudehook_iac.go`/`claudehook_db.go` (their files) and don't edit `claudehook_policy.go`. |
| D-CG: `internal/hub/runner` ignoring `permission` decisions with `data.pack` | F (§2.11). |
| D-PL: `REVIEWERS` in `gate.py` | not needed: the gate accepts `*-reviewer` names with an agent file (§2.8). |
| D-PL: crew lines in `build/SKILL.md` step d.3 | not needed: the step adds whatever `tq pack crew` prints (§2.9). |
| D-PL: rows in `kickoff`/`feature`/`fix`/`dod`/`ship` SKILL.md | D-PL writes `plugin/skills/pack/references/onramps/{cloud,db}-*.md`. The on-ramps read them through F's generic step (§6). |
| D-OI: `internal/insights/packs.go` | F owns it. D-OI reports through `packs.Count` and `Pack.Doctor` in `internal/packs/{cloud,db}/doctor.go`. |
| D-DOC: cloud and db sections of `docs/PACKS.md` | D-DOC writes `docs/packs/{cloud,db}-*.md`. F's `PACKS.md` index links them. |
| triage flag plumbing | D-CG/D-DL register `iac`, `iam`, `db_migration` and `rls_change` through `RegisterPack` flags. No triage file edit. |

Spec D keeps `internal/guardcfg` (`tq/hub-store-write` never disableable) and `HUB.md` and `CLAUDE-HOOK.md` rows as its own small hunks. They don't overlap with this spec's workstreams.

## 3. Data pack

### 3.1 Config (`packs.data`)

```yaml
packs:
  data:
    enabled: true
    tools: [dbt, sqlfluff, datacontract]
    engines: [bigquery, snowflake, databricks, postgres, fabric, synapse]   # which warehouse rules apply
    contracts:
      paths: ["contracts/**/*.odcs.yaml", "**/*.odcs.yml"]   # default
      enum_added: warn            # warn | breaking
      consumers: { orders: [acme-finance-dashboard] }       # optional, quoted in the PR
    dbt:
      project: analytics          # dir holding dbt_project.yml; auto-detected when absent
      contract_paths: ["models/marts/**"]
      new_model_tests: [unique, not_null]   # required on the primary key of a new model
      build: off                  # off | dev  (dev: run state:modified+ against dev_target)
      dev_target: dev
      prod_targets: [prod, production]
      state: artifacts/prod       # dir with the production manifest.json for state:modified
    pipelines:
      paths: ["dags/**", "pipelines/**", "**/*.Notebook/**", "notebook/**", "pipeline/**"]
    cost:
      bigquery:   { max_bytes: 50GB }
      snowflake:  { max_warehouse_size: MEDIUM, statement_timeout_s: 900 }
      databricks: { max_workers: 8, warehouse_id: 0123abcd }   # an id, not a secret
      fabric:     { max_capacity_sku: F8 }
      synapse:    { max_dwu: DW500c, max_spark_nodes: 10 }
      postgres:   { max_plan_cost: 10000000, statement_timeout_s: 300 }
      estimate_in_dod: false
```

**Detection hints** (used by `tq doctor` to suggest enabling the pack, never to enable it): `dbt_project.yml`; `*.odcs.yaml`; `from airflow`, `import dagster`, `from prefect`; `*.Notebook/` with `.platform` (Fabric git format); `notebook/*.json` plus `pipeline/*.json` (Synapse git format). *Assumption: the Fabric and Synapse git-integration layouts as described; verify against current exports.*

### 3.2 `data-contract`: ODCS v3 contracts and breaking changes

**What a contract is here:** a YAML file matched by `contracts.paths`, with `apiVersion: v3.*` and `kind: DataContract`. *Assumption: ODCS v3.0.x field names as used below (`id`, `version`, `status`, `schema[]` objects with `properties[]`, `logicalType`, `physicalType`, `required`, `unique`, `primaryKey`, `partitioned`, `classification`, `logicalTypeOptions`, `quality[]`, `slaProperties[]`, `servers[]`, `customProperties[]`); the build verifies them against the ODCS v3 JSON Schema and records the version it checked.*

**How a change is detected.** For each changed contract file, the check reads the old side with `git show <base>:<path>` and the new side from disk or the index. It parses both with line numbers and matches:

- objects by `id`, falling back to `name`;
- properties by `id`, falling back to `name`, recursively for nested `object`/`array` properties;
- quality rules by `id`, falling back to `rule` + `name`;
- SLA entries by `property`.

A rename is detected only when an `id` is stable. Without ids, a rename shows as a removal plus an addition, which is breaking anyway. An added file has no old side, so only `contract-valid` runs. A deleted contract is breaking.

**Breaking-change rules** (the consumer's view: anything a reader of the data could rely on):

| Change | Class |
|---|---|
| contract file deleted; object removed or renamed | breaking |
| property removed, or renamed (same `id`, new `name`) | breaking |
| `logicalType` changed, except the widenings `integer → number` and `date → timestamp` | breaking |
| `physicalType` narrowed: shorter `varchar(n)`, `bigint → int`, decimal precision or scale down; widened is non-breaking | breaking |
| `required: true → false` (a column may now be null) | breaking |
| `unique: true → false`; `primaryKey` set changed or reordered | breaking |
| `logicalTypeOptions` loosened: `maxLength` up, `minimum` down or `maximum` up, `pattern` or `format` removed | breaking |
| enum value removed | breaking |
| enum value added | `contracts.enum_added` (default `warn`) |
| quality rule removed, or its threshold loosened (`mustBeGreaterThan` down, `mustBeLessThan` up) | breaking |
| SLA loosened (latency or frequency up, availability or retention down) | breaking |
| server removed, or its location, schema or table changed | breaking |
| `partitioned` or partition key changed | warn (consumer query cost) |
| new property; new optional field anywhere; quality or SLA tightened; `required: false → true` | non-breaking |
| `classification` changed, either way | non-breaking, but raises `classified_data` (privacy reviewer) and requires the manifest `data.paths` update (`data/contract-pii`) |
| `status` set to `retired` while `consumers` lists anyone | breaking |

**Version rule:** any breaking change needs the major part of `version` bumped (semver). A breaking change without it fails with the list of breaking rows. A major bump with no breaking change is a `warn`.

**Checks:**

| Id | Kind | Fails when |
|---|---|---|
| `data/contract-valid` | built-in | the file does not parse, a required ODCS field is missing, or `id` or `version` is empty or duplicated across files |
| `data/contract-breaking` | built-in | a breaking row (table above) without a major version bump |
| `data/contract-pii` | built-in | a property is classified (`pii`, `sensitive`, `financial`, `children`, or the ODCS values mapped to them in `docs/packs/data-contracts.md`) while its table (`physicalName` or the server table) is not in the manifest `data.paths` with that class; or an `examples` value on a classified property hits the redaction detectors without `tq:allow-pii` |
| `data/contract-dbt-sync` | built-in | a contract with `customProperties: [{property: dbtModel, value: <model>}]` disagrees with that dbt model's YAML columns (missing column, `data_type` mismatch, `not_null` vs `required`) |
| `data/datacontract-lint` | external `datacontract` | `datacontract lint <file>` exits non-zero (timeout 60 s). *Assumption: datacontract-cli lints ODCS v3 files; if its `breaking` subcommand supports ODCS, the build adds `data/datacontract-breaking` as a cross-check* |

**Skill `data-contract`:** writes a contract from a dbt model or table description (template `plugin/templates/process/data-contract.odcs.yaml`). It evolves a contract with the version rule, writes the consumer migration note into the spec slice (`## Consumer migration`) for a breaking change, and runs `tq pack check --pack data`. For a breaking change, `ship` titles the PR with `!` and a `BREAKING CHANGE:` footer naming the contract and its consumers.

### 3.3 dbt gates

The dbt project is found from `dbt.project`, or else the nearest `dbt_project.yml`. Its `model-paths` (default `models`) come from that file.

| Id | Kind | Fails when (error) / warns |
|---|---|---|
| `data/dbt-new-model-tests` | built-in | A new model (status A, `.sql` or `.py` under model-paths) has no YAML entry, or its primary key has no `unique` + `not_null` (`data_tests:` or `tests:`). `dbt_utils.unique_combination_of_columns` counts, and so does a `primary_key` constraint under an enforced contract. The primary key is the column with `constraints: [{type: primary_key}]`, the contract's `primaryKey`, or the first column named `id`/`<model>_id`. Missing description: warn. |
| `data/dbt-contract-enforced` | built-in | A model under `contract_paths` lacks `contract: {enforced: true}` in its YAML config, in `{{ config(contract=…) }}` in the SQL, or as `+contract` in `dbt_project.yml` folder config; or an enforced model has a column without `data_type`. |
| `data/dbt-incremental` | built-in | An incremental model has no `unique_key` and no explicit `incremental_strategy: append` (error). An `is_incremental()` filter compares to `max(<ts>)` with no lookback (`- interval`, `dateadd`, `timestamp_sub`) (warn, late data). |
| `data/sql-select-star` | built-in | `select *` in a model under `contract_paths` (error), or elsewhere in model-paths (warn). Staging models selecting from `source()` are allowed. |
| `data/sql-hardcoded-relation` | built-in | A model reads `db.schema.table` or `schema.table` as a literal instead of `ref()` or `source()`. |
| `data/dbt-parse` | external `dbt` | `dbt parse --target-path <scratch>` fails (timeout 180 s). *Assumption: `dbt parse` needs a profile entry but opens no warehouse connection; verify per adapter (BigQuery, Snowflake, Databricks, Postgres, Fabric).* |
| `data/dbt-build-modified` | external `dbt`, opt-in `dbt.build: dev` | `dbt build --select state:modified+ --defer --state <dbt.state> --target <dev_target>` fails (timeout 1200 s). A missing state dir fails with "export the production manifest to dbt.state". The check refuses to run when `dev_target` is in `prod_targets`. *Assumption: dbt reports breaking changes to enforced contracts of modified models during this build (dbt ≥ 1.5).* |
| `data/sqlfluff` | external `sqlfluff` | `sqlfluff lint --format json <changed .sql>` reports violations (timeout 120 s). The dialect comes from the dbt adapter or `.sqlfluff`. *Assumption: with the default jinja templater this needs no warehouse; the dbt templater is not used by tq.* |

The minimum dbt version when declared is 1.8 (for `data_tests:` and unit tests). *Assumption: confirm this version floor at build time.*

### 3.4 Python pipelines, Fabric and Synapse

The built-in checks scan changed files under `pipelines.paths`, plus any changed `.py`/`.ipynb` importing `airflow`, `dagster` or `prefect`. They use the shared Python and notebook scanners.

| Id | Rules (error unless marked warn) |
|---|---|
| `data/airflow-dag` | `start_date` dynamic (`datetime.now()`, `days_ago(`, `pendulum.now(`); `catchup` not set explicitly (warn); no `retries` in `default_args` or on tasks (warn); top-level I/O in a DAG file (`requests.`, `pd.read_*`, DB `connect(` at indent 0) (warn) |
| `data/pipeline-backfill` | wall-clock time inside a transform or filter: `datetime.now()`, `date.today()`, `now()`, `current_date`, `getdate()`, `current_timestamp()` used in a `WHERE`, a filter or a partition value, rather than the run's logical date (`ds`, `data_interval_start`, a partition key, a parameter) (warn; error in a file under `contract_paths`) |
| `data/pipeline-idempotency` | append writes with no dedupe: `.mode("append")`, `to_sql(..., if_exists="append")`, `INSERT INTO … SELECT` with no `MERGE`, delete-insert or partition overwrite in the same file (warn) |
| `data/pipeline-pii-output` | A file reads a table classified `pii`/`sensitive`/`financial`/`children` in `data.paths` (by name, `source()` or `ref()`), selects a column whose name the contract classifies, or a common personal-data name (`email`, `cpf`, `phone`, `birth_date`, `address`, `name`). It writes to an output that is not classified with that class, and the column is not wrapped by a masking function (`sha2`, `md5`, `hash`, `farm_fingerprint`, a project macro listed in `docs/packs/data-pipelines.md`). Heuristic: the reviewer judges the rest. |
| `data/fabric-artifacts` | a notebook used by a pipeline has no parameters cell (warn); a notebook pins a default lakehouse by workspace GUID (warn: environment portability); code holds a literal OneLake or ABFSS path with a workspace GUID; a pipeline activity has `retry: 0` on a copy activity (warn) |
| `data/synapse-artifacts` | a linked service or pipeline JSON holds an inline connection string or password instead of a Key Vault reference; a dedicated pool script hard-codes a pool name that is not a parameter (warn) |
| `data/bq-job-max-bytes` | Python `bigquery.QueryJobConfig(` without `maximum_bytes_billed`, in a workspace with `bigquery` in `engines` |
| `data/notebook-outputs` | a committed notebook has outputs (warn); an output cell's text hits the redaction detectors (error: the commit is an exit) |
| `data/dagster-validate` | external `dagster`: `dagster definitions validate` fails (timeout 120 s). *Assumption: the subcommand exists in the pinned Dagster version* |

### 3.5 `pipeline-review`: the `pipeline-reviewer` agent

- **Agent:** `plugin/agents/pipeline-reviewer.md`. Read-only (Read, Glob, Grep), with the answer-key verdict and `file:line` on every finding.
- **Selected on:** the flags `pipeline`, `data_contract` or `warehouse_cost`.
- **Input:** the bundle, the spec slice, the contracts touched, the manifest `data:` block, `docs/privacy.md` and the `tq pack check` results for the data pack.
- **Checklist:** `plugin/skills/pipeline-review/references/checklist.md`:

| Row | Question |
|---|---|
| D1 Idempotency | Does a rerun of the same interval give the same result: MERGE or upsert on a key, partition overwrite, or delete-insert in one transaction? |
| D2 Backfill | Is the job parameterised by interval (logical date), free of wall-clock time, safe to run for past intervals in any order, and is the backfill command documented? |
| D3 Late and out-of-order data | Is there a lookback or watermark, and a stated lateness bound, so a late row is merged and not dropped or duplicated? |
| D4 Schema evolution | Does the change follow the contract version rule? Are downstream models and consumers listed? Is `on_schema_change` set for incremental models? |
| D5 Personal data into outputs | Does any `pii`/`sensitive`/`financial`/`children` column (manifest `data.paths`, contract classification) reach an output, export, log or metric without masking? Is the output classified and does `docs/privacy.md` say why? |
| D6 Tests | Do new models have key tests, do changed logic paths have dbt unit tests or pytest, and are the fixtures synthetic (`tester`, `acme`)? |
| D7 Cost | Partition and cluster pruning, no full scans in incremental paths, warehouse or pool sizing within `packs.data.cost`, and an estimate (§3.6) for heavy queries |
| D8 Failure handling | Retries with backoff, timeouts, side effects (emails, API calls) exactly once or guarded, alerts on failure |
| D9 Observability | Row counts, freshness, and a recorded run id per interval |
| D10 Secrets and environments | Connections from the orchestrator, Key Vault or env; no literal endpoints or workspace GUIDs; dev and prod split by parameter |
| D11 Orchestrator specifics | Airflow (static `start_date`, explicit `catchup`, no top-level work), Dagster (partitions and asset checks), Prefect (retries, cache keys), Fabric and Synapse (parameter cells, pipeline retry policy, no pinned lakehouse) |

- **Skill `pipeline-review`:** runs the agent standalone on a diff or path, and in the engine through the crew.

### 3.6 Warehouse cost guardrail

There are two parts. The guards bound what an agent runs. `tq pack data cost estimate` measures a query before it runs.

**Guards** (pure; active when the data pack is enabled and the engine is in `engines`):

| Id | Engine | Matches | Action |
|---|---|---|---|
| `tq/bq-unbounded-query` | BigQuery | `bq query` without `--maximum_bytes_billed`, or with a value above `cost.bigquery.max_bytes`. `--dry_run` is exempt. | deny, with the exact flag to add |
| `tq/warehouse-resize` | Snowflake | `ALTER WAREHOUSE … SET WAREHOUSE_SIZE` or `CREATE WAREHOUSE … WAREHOUSE_SIZE` above `max_warehouse_size`, in `snow sql`/`snowsql` commands or a SQL MCP call | deny above the cap, ask at or below |
| | Databricks | `databricks clusters create\|edit`, `databricks warehouses create\|edit`: inline JSON `num_workers` or `autoscale.max_workers` above `max_workers`; `--json @file` | deny above the cap; ask on `@file` (the guard reads no files) |
| | Synapse | `az synapse sql pool create\|update --performance-level` above `max_dwu`; `az synapse spark pool create\|update --node-count`/`--max-node-count` above `max_spark_nodes` | deny above the cap, ask otherwise |
| | Fabric | `az fabric capacity create\|update --sku` above `max_capacity_sku`. *Assumption: verify the az extension command names* | deny above the cap, ask otherwise |
| `tq/warehouse-query-unbounded` | Snowflake, Postgres | a `snow sql -q`/`snowsql -q`/`psql -c` query that does not set the statement timeout in the same command (`ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS`, `SET statement_timeout`, `PGOPTIONS=-c statement_timeout=`) and has no `LIMIT` | ask |
| | Databricks | `databricks api post …/sql/statements` without `wait_timeout` and `on_wait_timeout: CANCEL`. *Assumption: Statement Execution API field names* | ask |
| | Fabric, Synapse | `sqlcmd` against `*.datawarehouse.fabric.microsoft.com` or `*.sql.azuresynapse.net` with a `SELECT` and no `TOP` | ask |
| `tq/dbt-prod-target` | dbt | `dbt build\|run\|seed\|snapshot\|run-operation\|test` with `--target` in `dbt.prod_targets` | deny |
| `tq/dbt-full-refresh` | dbt | `--full-refresh` on any target | ask ("drops and rebuilds incremental tables") |
| `tq/pipeline-run` | orchestrators | `airflow dags trigger\|backfill`, `dagster job launch`, `dagster asset materialize`, `prefect deployment run`, `az datafactory pipeline create-run`, `az synapse pipeline create-run` | ask (backfill: "check D1 and D2 idempotency first") |

A size compares on the engine's own ordered scale: Snowflake `XSMALL…X6LARGE`, Synapse `DW100c…DW30000c`, Fabric `F2…F2048`. An unknown value asks. The guards can't see queries inside scripts: that is what the lints (`data/bq-job-max-bytes`) and the reviewer (D7) are for.

**`tq pack data cost estimate --engine <e> (--file q.sql | --stdin) [--json]`:**
- It reads one `SELECT` statement. It refuses DML, DDL, multiple statements and `ANALYZE`, so the query itself never runs.
- It runs only the engine's plan-only form through the engine's CLI with the identity's env (wrap it in `tq dotenv run` when the CLI needs a token).
- It prints the numbers and the verdict against the cap, and never echoes the query text. Timeout 60 s.

| Engine | Plan-only call | Number compared |
|---|---|---|
| BigQuery | `bq query --dry_run --use_legacy_sql=false --format=json` | `totalBytesProcessed` vs `max_bytes` |
| Snowflake | `snow sql -q "EXPLAIN USING JSON …" --format json` | `GlobalStats.bytesAssigned`, partitions assigned/total (reported); `bytesAssigned` vs `cost.snowflake.max_bytes` when set |
| Databricks SQL | `EXPLAIN COST …` through the Statement Execution API on `warehouse_id` | root `sizeInBytes` vs `cost.databricks.max_bytes` when set |
| Synapse dedicated | `EXPLAIN …` via `sqlcmd -G` | `total_cost` reported; operations count |
| Fabric warehouse | `SET SHOWPLAN_XML ON` via `sqlcmd -G` | `StatementSubTreeCost` reported |
| Postgres | `psql -c "EXPLAIN (FORMAT JSON) …"` | `Total Cost` vs `max_plan_cost` |

*Assumption: every plan-only call and the output field named in this table; the build verifies each against current engine docs and keeps a recorded sample as a golden file. Where a field is not available, the estimate reports `unknown` and the verdict is `ask`.*

`data/cost-estimate` (external, opt-in `estimate_in_dod: true`) runs the estimate on the compiled SQL of changed dbt models under the scratch target path. It needs a dev connection, and it is never on by default.

**Skill `warehouse-cost`:** before a heavy query or model change, it runs the estimate, explains partition and cluster pruning per engine, and proposes the cap flags. It never raises a cap: that is a manifest edit for the human.

## 4. ML pack

### 4.1 Config (`packs.ml`)

```yaml
packs:
  ml:
    enabled: true
    tools: [ruff, dvc, uv]
    paths:
      train: ["src/train/**", "notebooks/**"]
      features: ["src/features/**"]
      eval: ["src/eval/**"]
    seed_helper: src/common/seed.py:set_seed    # counts as setting every seed
    data_manifest: data/snapshots.yaml           # name -> uri + sha256 (or dvc.lock)
    models:
      - name: churn
        card: models/churn/MODEL_CARD.md
        metrics: reports/churn/metrics.json
    promotion:
      protected: [production, champion]          # stages or aliases that need approval
      command: "uv run python scripts/promote.py {model} {version} {target}"
      valid_for: 7d
      require: [model_card, drift, repro]
    drift:
      reference: data/profiles/churn-train.json
      current: .tq/drift/churn-current.json
      current_command: "uv run python -m drift_profile --reference data/profiles/churn-train.json --out .tq/drift/churn-current.json"
      metric: psi                                 # psi | js
      warn: 0.1
      fail: 0.25
      max_warn_share: 0.3
      features: { tenure_days: { fail: 0.2 } }
      metrics: { auc: { min: 0.78 } }
      in_dod: false
```

When `paths.train` is empty, a training file is any changed `.py` or `.ipynb` that calls `.fit(`, `optimizer.step(` or `trainer.train(`.

### 4.2 `ml-review` leakage lint (`ml/leakage`, built-in)

The lint uses the shared Python and notebook scanners. Notebook cells are scanned in order as one file, and positions are reported as `cell N, line M`. It is a heuristic over call order and names, not a dataflow analysis. Each rule has a golden positive and negative fixture.

| Rule | Detects | Level |
|---|---|---|
| `L1 fit-before-split` | `.fit(` or `.fit_transform(` of a preprocessing object (`StandardScaler`, `MinMaxScaler`, `RobustScaler`, `OneHotEncoder`, `OrdinalEncoder`, `LabelEncoder`, `SimpleImputer`, `KNNImputer`, `PCA`, `TfidfVectorizer`, `CountVectorizer`, `SelectKBest`, `TargetEncoder`) that appears before the first split call (`train_test_split`, `*.split(`, `KFold`, `TimeSeriesSplit`, `GroupKFold`) and whose argument is not named like train | error |
| `L2 test-in-fit` | `.fit(`/`.fit_transform(` whose argument name contains `test`, `holdout` or `val` (the validation set used for early stopping is allowed when passed as `eval_set=`) | error |
| `L3 cv-outside-pipeline` | `cross_val_score`, `cross_validate` or `GridSearchCV.fit` on an array produced by an earlier `fit_transform` of the full data, instead of a `Pipeline` | error |
| `L4 target-in-features` | `y = df["t"]` with `X = df` (no drop of `t`), or `X = df[[… "t" …]]` | error |
| `L5 lookahead` | `.shift(-n)`, `rolling(…, center=True)` or `bfill()` in feature files, not on the target column | warn |
| `L6 timeseries-shuffle` | `train_test_split` without `shuffle=False` in a file that sorts by, or indexes on, a datetime column, or uses `TimeSeriesSplit` elsewhere | warn |
| `L7 group-leak` | a split without `groups=` (or `Group*` splitter) when the frame has an entity id column that `paths.features` code groups by (`groupby("customer_id")`) | warn |
| `L8 torch-eval` | a validation or test loop without `model.eval()` and `torch.no_grad()`/`inference_mode()` | warn |

External companion: `ml/ruff` runs `ruff check --select NPY,PD --output-format json` on changed files (timeout 60 s). NPY002 (legacy `np.random` global state) feeds the seed rule. No established standalone leakage linter is wired in this release. *Assumption: a notebook leakage analyser may be added later as an external tool once one is packaged and maintained.*

### 4.3 Reproducibility checklist

| Id | Kind | Fails when |
|---|---|---|
| `ml/seeds` | built-in | A changed training file sets no seed: no `random.seed`, `np.random.seed`/`default_rng(<int>)`, `torch.manual_seed` (plus `torch.cuda.manual_seed_all` when `cuda` appears), and no call to `seed_helper`. Or an sklearn estimator or splitter that accepts `random_state` is built without it. Missing `torch.use_deterministic_algorithms` and DataLoader `worker_init_fn`/`generator`: warn. |
| `ml/env-lock` | built-in | A dependency manifest changed (`pyproject.toml` dependencies, `requirements*.in`, `environment.yml`) and its lock did not (`uv.lock`, `poetry.lock`, `requirements*.txt` from pip-compile, `conda-lock.yml`, `pixi.lock`). Or a `requirements*.txt` used by training has an unpinned line (no `==`). Missing hashes: warn. |
| `ml/data-snapshot` | built-in | A training file reads a data source not pinned by `data_manifest` (name with `sha256`) or a `.dvc`/`dvc.lock` entry; or it reads a moving reference (`latest`, a table with no `versionAsOf`/`timestampAsOf`/snapshot id, a `SELECT` without an `as of` bound) |
| `ml/run-tracking` | built-in | warn only: no logging of params, metrics and code version (`mlflow.log_params`/`autolog`, a run config written next to the metrics) |
| `ml/unsafe-deserialize` | built-in | `pickle.load`, `joblib.load`, `torch.load` without `weights_only=True`, or `cloudpickle.load` on a path that is not repo-relative or comes from a download (error); in tests (warn) |
| `ml/promotion-in-code` | built-in | code calls a registry promotion API (`transition_model_version_stage`, `set_registered_model_alias`, `update_model_package(… ModelApprovalStatus="Approved")`) with a target in `promotion.protected`, outside `scripts/promote.py` or the path `promotion.command` runs |
| `ml/notebook-outputs` | built-in | as `data/notebook-outputs` |
| `ml/lock-check` | external `uv` or `poetry` | `uv lock --check` or `poetry check --lock` fails (timeout 60 s). *Assumption: flag names for the pinned versions* |
| `ml/dvc-status` | external `dvc` | `dvc status --json` (local only, no `-c`) reports changed outputs (timeout 60 s). *Assumption: JSON output shape* |

### 4.4 Model promotion gate

1. **Request.** An agent (or the human) runs `tq pack ml promote request <model> <version> --to <target>`. With a target outside `promotion.protected`, it prints "not protected, no approval needed" and exits 0. Otherwise it:
   - runs the `require` items: `model_card` (`ml/model-card` on that model), `drift` (`tq pack ml drift <model>`), `repro` (`ml/seeds`, `ml/env-lock`, `ml/data-snapshot` with `--all` on the training paths);
   - refuses with the list when any of them fails;
   - builds the evidence map: sha256 of the model card, the metrics file and the drift result JSON; the `tq pack check` results hash; the git `HEAD` commit; a clean tree (required);
   - calls `approval.Ask` with action `promote`, subject `<model>@<version>`, target `<target>`, and a summary (metrics table, drift verdict, model card path), redacted with `exit_classes`.

   It prints the decision id and the card.
2. **Decide.** The human answers `approve` or `reject` on the board, in `/waiting` or with `tq hub answer` in their own terminal. Agents never answer.
3. **Apply.** The human runs `tq pack ml promote apply <decision>` (human-only). It:
   - calls `approval.Verify` with the evidence recomputed at the current `HEAD`. A changed card, metrics file, drift result, commit or dirty tree refuses;
   - prints the `promotion.command` with `{model}`, `{version}` and `{target}` filled in (shell-quoted, values limited to `[A-Za-z0-9._@-]`);
   - asks for `yes`, runs it with the identity env and a 300 s timeout, and records the exit code;
   - on exit 0, calls `MarkUsed`, appends the decision id to the model card's "Promotion history" table for the next commit, and comments on the card.
4. **Guard `tq/model-promote` (deny, agents):** registry promotion commands outside `tq pack ml promote apply`:
   - `curl`/`Invoke-RestMethod` to `…/model-versions/transition-stage` or `…/registered-models/alias`;
   - `python -c` containing the promotion APIs of §4.3;
   - `aws sagemaker update-model-package … --model-approval-status Approved`;
   - `gcloud ai models update … --update-version-aliases` *(Assumption: flag name)*;
   - `databricks registered-models set-alias` *(Assumption: CLI command name)*;
   - `az ml model update … --stage` *(Assumption)*;
   - running the declared `promotion.command` directly.

   The human can still run them in their own terminal: the guard only sees agent tool calls.

`tq pack ml promote status [<model>]` lists requests and their state. A rejected or expired request needs a new one.

### 4.5 `MODEL_CARD.md`

The template is `plugin/templates/process/MODEL_CARD.md`, written fresh under the plugin's MIT license. One card per model, at `models[].card`.

| Section (required heading) | Content |
|---|---|
| Model details | name, version, registry URI (no credentials), owner role, date, licence |
| Intended use / Out of scope | who uses the predictions for what decision, and known misuse |
| Training data | snapshot name and sha256 (must match `data_manifest`), date range, data classes (manifest names only), sampling, **no example rows of personal data** |
| Evaluation data | holdout definition (time or group split), size, how it was kept untouched |
| Metrics | a table of metric, value, confidence interval, baseline; per-slice rows for the slices that matter |
| Fairness and slices | slices checked, gaps found, mitigations; `NEEDS-DECISION` allowed |
| Limitations and risks | failure modes, drift sensitivity, feedback loops |
| Reproducibility | git commit, lock file hash, seeds, the training command, the environment (Python version, accelerator) |
| Monitoring and drift | reference profile, monitored features, thresholds (from the config), alert route |
| Promotion history | a table of date, version, target, decision id, actor role (written by `promote apply`) |

`ml/model-card` (built-in) fails when:
- training, feature or eval code of a declared model changed and its card did not;
- a required heading is missing or holds only `TODO`;
- the metrics table is empty;
- the snapshot hash differs from `data_manifest`;
- the card text hits the redaction detectors.

**Skill `model-card`:** writes or updates the card from the metrics file, the data manifest and the git state, and asks the human for intended use and limitations (grill-me style). It never invents fairness results.

### 4.6 Drift check (`tq pack ml drift <model> [--json]`, check `ml/drift`)

- **Inputs:** `drift.reference` and `drift.current`. Each is either a CSV (tq builds the profile itself with stdlib `encoding/csv`: numeric bins from the reference deciles, categorical counts) or a **profile JSON** `tq.drift-profile/v1`: `{rows, source, created_at, features: {name: {type: numeric, edges: [...], counts: [...]} | {type: categorical, counts: {label: n}, hashed: bool}}}`.
- **Producing `current`:** when `current_command` is set and `current` is missing or older than 24 h, tq runs the command (timeout 600 s). The `ml-starter` template ships `drift_profile.py` (pandas/polars, reads parquet, reuses the reference edges, hashes labels of columns classified in `data.paths`).
- **Edges:** a numeric feature whose edges differ from the reference is an error ("rebuild the current profile with the reference edges").
- **Metric:** PSI = Σ (aᵢ − eᵢ)·ln(aᵢ/eᵢ), with empty bins floored at 1e-4. Or Jensen-Shannon distance when `metric: js`. Per feature: `ok` below `warn`, `warn` from `warn`, `fail` from `fail`. Per-feature overrides apply.
- **Overall result:**
  - `fail` when any feature fails, when the share of warned features exceeds `max_warn_share`, or when a `drift.metrics.<m>.min` is not met by the current metrics file;
  - `__prediction__` is the predicted score's distribution when the profile has it;
  - the result JSON is written to `.tq/drift/<model>-result.json` (gitignored) and hashed into the promotion evidence.
- **Privacy:** categorical features with more than 50 labels, or from classified columns, are compared on hashed labels. Output names features and numbers only, never labels of classified columns.
- **In the DoD** only with `in_dod: true`. It is always part of a promotion request when `require` has `drift`.

**Skill `drift-check`:** builds or refreshes the profiles with the template script, runs `tq pack ml drift`, explains which features moved, and proposes retraining or a threshold review. Changing a threshold is a manifest edit for the human; the repo file may only tighten one.

### 4.7 `ml-reviewer` agent

- **Agent:** `plugin/agents/ml-reviewer.md`. Read-only, with the answer-key verdict.
- **Selected on:** the flags `ml` or `model_promotion`.
- **Checklist:** `plugin/skills/ml-review/references/checklist.md`:

| Row | Question |
|---|---|
| M1 Leakage | the L1–L8 findings judged, plus leakage the lint can't see: duplicates across splits, features computed after the label time, target encoding without folds |
| M2 Split strategy | time, group or stratified split that matches how the model is used; holdout untouched until the final evaluation |
| M3 Metrics | the metric matches the decision (cost of errors), a baseline is compared, confidence intervals are given, calibration is checked when scores are used as probabilities |
| M4 Reproducibility | seeds, snapshot hash, lock, and a recorded command; the `ml/*` repro results judged |
| M5 Training/serving skew | the same feature code at training and inference; no notebook-only transforms |
| M6 Personal data as features | classified columns used as features: purpose and legal basis in `docs/privacy.md`, proxies for protected attributes, slice analysis in the model card |
| M7 Model card | updated, accurate against the metrics file, no personal data |
| M8 Serialization and supply chain | safe formats (`safetensors`, `skops`), `weights_only=True`, pinned pretrained weights by hash |
| M9 Promotion path | promotion only through `tq pack ml promote`; no promotion API calls in code outside the promotion script |
| M10 Cost and resources | GPU or cluster hours bounded, early stopping, no unbounded hyperparameter search |
| M11 PyTorch specifics | `eval()`/`no_grad()`, device determinism notes, DataLoader worker seeding |

**Skill `ml-review`:** runs the agent standalone or through the crew, and quotes `ml/leakage` and the repro results.

## 5. New skills, agents, guard rules, triage flags and templates

### 5.1 Skills (`plugin/skills/`)

| Skill | Trigger | Output |
|---|---|---|
| `pack` (framework) | "pack check", "which packs", a declared-missing tool, DoD `pack:` items | results explained, fixes |
| `data-contract` | "data contract", "ODCS", a contract file in the diff, a breaking change | contract file, version bump, `## Consumer migration` |
| `pipeline-review` | "review this pipeline/DAG/dbt model", flag `pipeline` | `pipeline-reviewer` verdict |
| `warehouse-cost` | "how much will this query cost", "dry run", a denied `bq query` | estimate, cap flags |
| `ml-review` | "review the training code", "leakage", flag `ml` | `ml-reviewer` verdict |
| `model-card` | "model card", a promotion request missing a card | `MODEL_CARD.md` |
| `ml-promote` | "promote model", "move to production", "set champion" | `tq pack ml promote request` and the human's `apply` line (never runs `apply`) |
| `drift-check` | "drift", "is the model still valid", before a promotion | drift report |

All skills follow the plugin conventions (identity-aware bash prefix, `tq_run.sh`, no bare `python`, `tq` calls with explicit timeouts).

### 5.2 Agents

`pipeline-reviewer`, `ml-reviewer`: both read-only (tools Read, Glob, Grep; `disallowedTools` as for `privacy-reviewer`), and both say whether they built the change.

### 5.3 Guard rules (all through `packs.RegisterGuard`, fail closed on panic)

| Id | Pack | Action |
|---|---|---|
| `tq/bq-unbounded-query` | data | deny |
| `tq/warehouse-resize` | data | deny above the cap, ask otherwise |
| `tq/warehouse-query-unbounded` | data | ask |
| `tq/dbt-prod-target` | data | deny |
| `tq/dbt-full-refresh` | data | ask |
| `tq/pipeline-run` | data | ask |
| `tq/model-promote` | ml | deny |
| `tq/pack-config-change` | framework (any enabled pack) | ask on an agent edit of `tq-packs.yaml` or a pack baseline file |

Human-only commands: `tq pack ml promote apply`. `tq hub answer` keeps its existing agent refusal.

### 5.4 Triage flags

| Flag | Raised (deterministic) | Jev question (enabled pack only) | Reviewers |
|---|---|---|---|
| `data_contract` | a changed file matches `contracts.paths` | — | `pipeline-reviewer` (and `classified_data` when a classification changed) |
| `pipeline` | changed files under the dbt model-paths, `pipelines.paths`, Fabric or Synapse artifacts | "Does this change alter a data pipeline, dbt model or scheduled job in a way that could duplicate, drop or reorder rows on a rerun or backfill?" | `pipeline-reviewer` |
| `warehouse_cost` | `packs.data.cost` changed; a materialization changed to `table`; pool or cluster sizing files changed | "Does this change add or alter warehouse queries, models or compute sizing so that it could scan much more data or cost more to run?" | `pipeline-reviewer` |
| `ml` | changed files under `ml.paths`, or training files (§4.1) | "Does this change alter how a model is trained or evaluated, or which data reaches its features?" | `ml-reviewer` |
| `model_promotion` | promotion script, `packs.ml.promotion`, or a model card changed | — | `ml-reviewer` |

### 5.5 Templates

| Path | What | Tests |
|---|---|---|
| `templates/dbt-contract-starter/` | A small dbt project: a staging and a mart model on synthetic `acme` orders, an ODCS contract for the mart, an enforced contract, key tests, a dbt unit test, `.sqlfluff`, `tq-packs.yaml`, seeds with customers named `tester`. | pytest over the files with `tq pack check --all --builtin-only` through a fake tq-less harness (the Go side is tested in Go); no warehouse |
| `templates/ml-starter/` | A scikit-learn `Pipeline` (no leakage), seeds helper, `uv.lock`, `data/snapshots.yaml`, a `MODEL_CARD.md`, `drift_profile.py`, `scripts/promote.py` for MLflow aliases (with the tracking URI from env), and synthetic data generated at test time | pytest with a fake MLflow server (`http.server`) and generated data |
| `plugin/templates/process/data-contract.odcs.yaml` | the contract template | plugin tests |
| `plugin/templates/process/MODEL_CARD.md` | §4.5 | plugin tests |

`templates/README.md` gains rows for both templates. The framework changes the CI `templates` job so it loops over `templates/*/tests` and installs each template that has a `pyproject.toml`.

## 6. On-ramp changes

The framework adds one generic step to each on-ramp: "run `tq pack list --enabled --json` (timeout 10 s); for each enabled pack read `skills/pack/references/onramps/<pack>-*.md` and follow its section for this on-ramp". The pack workstreams write those reference files.

| On-ramp | Data pack (`onramps/data-*.md`) | ML pack (`onramps/ml-*.md`) |
|---|---|---|
| `feature` | The spec slice gains **Contracts touched** (contract, change class, version bump) and **Outputs and classes**. A breaking change needs `## Consumer migration`. The expected crew adds `pipeline-reviewer` via `tq pack crew`. Acceptance checks name dbt tests or unit tests. | The slice gains **Models touched**, **Data snapshot** and **Evaluation plan** (metric, baseline, split). The crew adds `ml-reviewer`. When the feature ends in a promotion, the last checkpoint ends at `promote request`, never `apply`. |
| `fix` | The regression test is at the lowest data layer: a dbt singular or unit test, or a pytest over a synthetic frame. A fix to a contract-breaking bug still follows the version rule. | A leakage or repro fix gets a lint fixture (positive case) plus a test. A model-quality fix records before and after metrics in the fix spec. |
| `kickoff` | When the stack has dbt, a pipeline orchestrator or Fabric/Synapse: propose the `packs.data` block (headless: in `docs/privacy-manifest-proposal.yaml`, as today), create `contracts/`, add an ADR for the warehouse engine and cost caps, and set `contract_paths`. | Propose `packs.ml`, the data manifest, the model card from the template, the drift reference plan and the promotion command. ADR for the registry. |
| `dod` | Rows for `pack:data/*`, `pack:config` and `pack:run` in the item table: who fixes what. A declared-missing tool: install it, or the human removes it from `tools`. | Rows for `pack:ml/*`. |
| `ship` | The PR body quotes the pack results summary. A breaking contract gets the `!` title and a `BREAKING CHANGE:` footer naming the consumers. A cost estimate is attached when `warehouse_cost` was raised. | The PR body links the model card diff and any pending promotion decision id. It never says "promoted" unless `promote status` shows it applied. |

## 7. `tq doctor` and `tq insights`

**Doctor** (framework findings, plus pack probes):

| Code | Level | When |
|---|---|---|
| `packs-tool-missing` | error | a declared tool is not installed (with the install hint) |
| `packs-tool-old` | warn | a declared tool is below the pack minimum |
| `packs-suggest` | info | detection hints (§3.1) match but the pack is not enabled |
| `packs-repo-file-invalid` | error | `tq-packs.yaml` does not parse or tries to loosen a trusted value (the loosening is ignored and reported) |
| `data-cost-cap-missing` | warn | an engine in `engines` has no cap in `cost` |
| `data-dbt-state-missing` | warn | `dbt.build: dev` and `dbt.state` has no `manifest.json` |
| `data-contracts-none` | info | `contract_paths` models exist but no contract file does |
| `ml-card-missing` | warn | a declared model has no card |
| `ml-drift-reference-missing` | warn | `drift.reference` is missing |
| `ml-promotion-command-missing` | warn | `promotion.protected` is set but `command` is not |

**Insights:** a new **Packs** section in the report and in `tq.insights/v1`:
- per check id: runs, ok, fail, skip, advisory fails, p95 duration;
- declared-missing count;
- pack guard hits by id (from the existing guard log);
- approvals: asked, approved, rejected, expired, median time to answer;
- promotions applied.

Aggregates only, as for the rest of insights.

## 8. Security and data protection

- **PII only at the exits** (parent §11.2). Reading tables, contracts and data files for checks is allowed. What leaves the process does not carry personal data:
  - findings carry `file:line`, column and feature names, never values;
  - tool output tails, approval summaries and comments pass `redact` with `exit_classes`;
  - drift output hashes classified labels;
  - `cost estimate` never echoes query text;
  - `events.jsonl` is aggregate-only.
- **Contract examples, model cards, notebook outputs:** checked by the redaction detectors (`data/contract-pii`, `ml/model-card`, `*/notebook-outputs`), on top of `tq/commit-pii` and `tq/push-pii`.
- **Fixtures:** synthetic only, with people named `tester` and the company `acme` (`tester@acme.test`). dbt `seeds/` is already covered by `tq/pii-fixture-dump`. Templates generate data at test time.
- **Secrets:** the manifest and `tq-packs.yaml` hold names only, and both run the secret scan. The runner never reads `.env*`, never prints the environment, and passes credentials only through the inherited env (`tq dotenv run`). `profiles.yml` outside the repo is never read; inside the repo it is read only for the adapter type, and values are never printed.
- **Untrusted repo content:** `tq-packs.yaml` can't loosen, and its hash is frozen at approval. Commands from it run only in the frozen DoD or after a human's typed confirmation. `tool_paths` is trusted-layer only. Placeholders filled into the promotion command are charset-limited and quoted.
- **Gated actions:** promotion needs a human-answered approval bound to evidence hashes and a clean commit, single-use and expiring. Agents are denied registry promotion, prod dbt targets and unbounded BigQuery queries. The key-isolation caveat of the build gate applies: the gate's text checks stop a careless agent, not one set on defeating them, until key isolation lands.
- **Warehouse calls** happen only in opt-in checks and the human- or agent-invoked `cost estimate`, as plan-only statements. They are never made in hooks or in tests.

## 9. Testing strategy

- **No real warehouse, cloud, registry or network** in any test.
- **Fake CLIs on PATH:** small Go programs built in `TestMain` into a temp dir (via `internal/testutil`), standing in for `dbt`, `sqlfluff`, `datacontract`, `ruff`, `dvc`, `uv`, `bq`, `snow`, `databricks`, `sqlcmd` and `psql`. Each echoes golden JSON chosen by an env var, can sleep (timeout tests), can exit non-zero, and can fork a grandchild that holds the pipes (tree-kill test). `--version` prints a configurable version.
- **Golden files:** `internal/packs/<pack>/testdata/<check>/<case>/{old,new}/…` plus `want.json`. There is one case per row of the breaking-change table, one per leakage rule (positive and negative), and one per Fabric and Synapse artifact shape. Plan-only engine outputs are recorded samples.
- **Guards:** table tests per guard id for allow, ask and deny; the `disable`/`actions` layers; a panic means deny; and a benchmark of < 50 ms on a 64 KiB command.
- **Framework:**
  - merge rules (each key kind, loosening ignored);
  - `config_hash` stability;
  - change selection over a temp git repo for each flag;
  - budget and timeout;
  - JSON schema;
  - exit codes;
  - events lines that carry no file names.
- **Isolation:** tests isolate `TQ_HOME`, `HOME` and `USERPROFILE` (repo rule), and run on Windows and Linux in CI.
- **Hub approvals:** an `httptest` hub and a temp store cover ask, answer by a human vs an agent actor, expiry, evidence mismatch, single use (ledger), the runner ignoring `permission` decisions with `data.pack`, and the agent refusal of `apply`.
- **Baseline:** fingerprints that carry no source values; a match is downgraded to warn; a new finding keeps its level; growth raises `baseline_change`; `tq pack baseline` is refused in an agent session.
- **Decision guards:**
  - `Supersedes` drops the listed core matches only when `handled`;
  - a `ReadState` or `ReadLocal` over 1 MiB, or a read error, means deny;
  - `ReadLocal` refuses `.env`, `.env.prod`, a secret-store path, `..` and a symlink out of the roots;
  - `HashState` over 64 MiB or past 40 ms means deny;
  - a failed `consumed` append means deny;
  - a reviewer-only flag on a core flag adds its reviewer, and one on an unknown name panics at registration.
- **Ledger:** the hub's answer handler writes `answered` for a human answer to a pack `permission` decision and refuses the answer when the write fails; guards resolve state from the ledger alone with the hub store absent.
- **gate.py:** pytest with a fake `tq` on PATH covering the freeze at approval, `pack:` items, advisory mapping, `pack:config` mismatch, `pack:run` on a crash or bad JSON, and tq missing with and without frozen packs. Reviewer name acceptance gets its own test.
- **Skills and agents:** plugin tests in the style of `test_privacy_review_skill.py` (required tokens, checklist rows, read-only tools, the bootstrap snippet) in new test files per workstream.
- **Templates:** pytest with fakes (a fake MLflow HTTP server, generated data), run by the CI templates loop.
- **Manual (human only):** one run per engine against a real dev account to confirm each *Assumption* in §3 and §4. It is never run by an agent.

## 10. Workstreams

`F` goes first and lands the seams. After it merges, the other workstreams run in parallel in separate worktrees. Ownership is by file or directory, and the sets don't overlap. "May touch" lists the only shared files a workstream edits, each with a minimal, additive change.

| WS | Scope | Owns (exclusive) | Depends on | May touch |
|---|---|---|---|---|
| **F** framework | §2 in full: registry, Input, change selection, runner, JSON, events, approvals API and ledger, baseline ratchet, scanners, CLI (`list`, `check`, `doctor`, `crew`, `approvals`, `baseline`, the `tq pack <pack>` mount), manifest `packs:` + validators + repo layer, triage wiring, crew wiring, guard wiring, gate.py DoD and reviewer acceptance, generic on-ramp step, `pack` skill, PACKS.md index, doctor and insights wiring, CI templates loop. Creates **stub packages** `internal/packs/{data,ml,cloud,db}/doc.go` and `cmd/tq/packs.go` importing all four, so no pack workstream edits `cmd/` | `internal/packs/` (root, `scan/`, `toolrun/`, `approval/`, `ledger/`, `baseline/`), `internal/manifest/packs.go`, `internal/policy/builtin_packs.go` (empty stub, then D-GR's), `internal/cmd/pack.go`, `internal/doctor/packs.go`, `internal/insights/packs.go`, `cmd/tq/packs.go`, `plugin/skills/pack/SKILL.md`, `docs/PACKS.md`, `plugin/tests/test_pack_framework.py` | — | `internal/manifest/manifest.go` (one field + one Load call), `internal/cmd/root.go` (one `AddCommand` line), `internal/cmd/decide.go` + `internal/decide/triage.go` (`TriageWithQuestions`), `internal/cmd/claudehook_policy.go`, `internal/cmd/guard.go` (list), `internal/policy/builtin.go` (one append line), `internal/hub/runner` (ignore `permission` with `data.pack`), `internal/hub/store/decisions.go` (answer path calls `ledger.RecordAnswer`) + hub web decision view (evidence list), `internal/doctor/doctor.go`, `internal/insights/report.go`/`render.go`, `plugin/tentaqles/workflow/gate.py`, `plugin/skills/{build,feature,fix,kickoff,dod,ship}/SKILL.md` (generic pack step only), `.github/workflows/test.yml` |
| **D1** contracts | §3.2 | `internal/packs/data/contract*.go` + `testdata/contract*/`, `internal/packs/data/register_contract.go`, `plugin/skills/data-contract/`, `plugin/templates/process/data-contract.odcs.yaml`, `plugin/skills/pack/references/onramps/data-contracts.md`, `docs/packs/data-contracts.md`, `plugin/tests/test_pack_data_contract.py` | F | — |
| **D2** dbt | §3.3, guards `tq/dbt-prod-target`, `tq/dbt-full-refresh`, template `dbt-contract-starter` | `internal/packs/data/dbt*.go`, `sql*.go`, `register_dbt.go`, `guard_dbt.go`, `testdata/dbt*/`, `testdata/sql*/`, `templates/dbt-contract-starter/`, `onramps/data-dbt.md`, `docs/packs/data-dbt.md` | F (D1 contract format for the template contract only) | `templates/README.md` (one row) |
| **D3** pipelines + reviewer | §3.4, §3.5, guard `tq/pipeline-run` | `internal/packs/data/pipeline*.go`, `fabric*.go`, `synapse*.go`, `airflow*.go`, `notebook*.go`, `register_pipeline.go`, `guard_pipeline.go`, `testdata/pipeline*/`, `plugin/agents/pipeline-reviewer.md`, `plugin/skills/pipeline-review/`, `onramps/data-pipelines.md`, `docs/packs/data-pipelines.md`, `plugin/tests/test_pack_pipeline_review.py` | F | — |
| **D4** cost | §3.6: cost guards, `tq pack data cost estimate`, `data/bq-job-max-bytes`, `data/cost-estimate`, `cost` config keys | `internal/packs/data/cost*.go`, `guard_cost.go`, `register_cost.go`, `testdata/cost*/`, `plugin/skills/warehouse-cost/`, `onramps/data-cost.md`, `docs/packs/data-cost.md` | F | — |
| **M1** ML lints | §4.2, §4.3 (except `ml/promotion-in-code`) | `internal/packs/ml/leak*.go`, `repro*.go`, `deserialize*.go`, `register_lint.go`, `testdata/leak*/`, `testdata/repro*/`, `onramps/ml-lints.md`, `docs/packs/ml-lints.md` | F | — |
| **M2** promotion, card, drift | §4.4–§4.6, guard `tq/model-promote`, `ml/promotion-in-code`, `ml/model-card`, `ml/drift`, `tq pack ml promote`/`drift` | `internal/packs/ml/promote*.go`, `card*.go`, `drift*.go`, `guard_ml.go`, `register_promote.go`, `testdata/promote*/`, `testdata/drift*/`, `plugin/skills/{ml-promote,model-card,drift-check}/`, `plugin/templates/process/MODEL_CARD.md`, `onramps/ml-promotion.md`, `docs/packs/ml-promotion.md` | F (approvals API) | — |
| **M3** reviewer + template | §4.7, template `ml-starter` (uses M2's card format and drift profile schema from this spec) | `plugin/agents/ml-reviewer.md`, `plugin/skills/ml-review/`, `templates/ml-starter/`, `onramps/ml-review.md`, `docs/packs/ml-review.md`, `plugin/tests/test_pack_ml_review.py` | F | `templates/README.md` (one row; D2 adds the other) |
| **R** release | CHANGELOGs, plugin README and `plugin/CLAUDE.md` skill and agent counts, version bump, SETUP mention, PACKS.md polish | those files | all | — |

**Rules for the pack workstreams:**
- Each one registers its checks, config keys, flags, guards, doctor probes and subcommands from its own `register_*.go` file, using `init`.
- No workstream edits another's files or `doc.go`.
- `docs/packs/*.md` and `onramps/*.md` are one file per workstream.
- Package `internal/packs/data` is shared only by directory: the file names above don't overlap.

**Seams shared with spec D** (all owned by F, so spec D's workstreams don't touch them):
- the manifest `packs:` struct and validators;
- triage (`decide.go`, `TriageWithQuestions`) and `tq pack crew`;
- `gate.py` DoD and reviewer acceptance;
- `docs/PACKS.md`, the `pack` skill and the on-ramp step;
- `cmd/tq/packs.go` (already imports `cloud` and `db`);
- `claudehook_policy.go` guard wiring;
- the approvals API and ledger over hub `permission` decisions with `data.pack`, the runner hunk, the baseline ratchet, `builtin_packs.go` and the `Builtin()` append;
- doctor and insights wiring;
- the CI templates loop.

Spec D's workstreams own `internal/packs/{cloud,db}/` (minus F's `doc.go`), their skills, agents and `docs/packs/{cloud,db}-*.md`. If spec D's F-equivalent work is merged into this F, one framework PR serves both releases.

## 11. Done when

- `tq pack list|check|doctor|crew|approvals` work against the fixture repos. `--json` matches `tq.packs/v1`.
- Every breaking-change row, dbt gate, pipeline rule, leakage rule and repro rule has passing golden cases. Both templates pass their own pack checks with no error findings.
- The DoD records `pack:*` items, maps advisory fails to skip, and fails closed on a missing or crashed tq when packs were frozen.
- The guards deny or ask per §5.3, with a panic meaning deny, and pass the 50 ms benchmark.
- A promotion is requested, approved by a human actor on the hub, and applied by a human through the fake promotion command. Agent `apply`, an agent answer, a stale evidence map and a reused approval are each refused.
- `go test ./...`, the plugin suite, the templates loop and CI are green on Windows and Linux.
- The human has run the per-engine manual checks and confirmed or corrected each *Assumption*.

## 12. Assumptions to verify in the build

1. ODCS v3.0.x field names and structure (§3.2), and the classification values that map to tq data classes.
2. Fabric and Synapse git-integration file layouts (§3.1, §3.4).
3. dbt: `parse` without a connection; `--target-path` and `--log-path` flags; contract breaking-change detection under `state:modified`; the version floor of 1.8 for `data_tests:`; the telemetry env var name.
4. `sqlfluff` with the jinja templater needs no warehouse; `datacontract lint` and `breaking` support ODCS v3; `dagster definitions validate` exists.
5. Every plan-only cost call and output field in §3.6 (BigQuery `totalBytesProcessed`, Snowflake `EXPLAIN USING JSON` `GlobalStats`, Databricks `EXPLAIN COST` `sizeInBytes` and the Statement Execution API fields, Synapse `EXPLAIN`, Fabric `SHOWPLAN_XML`, Postgres `EXPLAIN (FORMAT JSON)`), and the `az fabric`, `az synapse` and `databricks` CLI command and flag names.
6. `uv lock --check`, `poetry check --lock` and the `dvc status --json` shape.
7. Registry promotion command and flag names for SageMaker, Vertex AI, Databricks and Azure ML (§4.4).
8. The hub store can be opened from the CLI to create a card and an approval without a running server, as `tq hub add` does today.

## 13. Decided at spec review (2026-10-28)

The human accepted every proposal:

1. Undeclared but installed tools run as advisory (recorded, never failing).
2. `bq query` without a byte cap is denied.
3. The add-only repo layer `tq-packs.yaml` exists.
4. `promote apply` runs the project's declared promotion command after the human types `yes`.
5. An enum value added to a contract is `warn` by default.
6. dbt build and drift stay out of the DoD by default.
7. One framework PR (F) covers the seams of both spec C and spec D.
