# ADR-0001: The hook guards identity drift and secrets, allows or blocks, and never asks

Status: accepted
Date: 2026-11-04
Cites: PRD §13 D-29, D-30, D-31; threat model boundaries B1 (the hook), B2 (human-only actions)

## Context
tentaqles is an identity isolator with secrets protection for developers who work
across several clients. Every confirmation the hook asks for, in any Claude permission
mode, slows daily work; the repo owner weighed that cost against what each confirmation
protected and decided (D-29) that the product guards identity and secrets and nothing
else, that the hook allows or blocks and never asks (D-31), and that tq sets no
permission mode of its own beyond the launcher's skip (D-30).

## Decision
The PreToolUse hook applies exactly one rule set, the same in every Claude permission
mode: the identity guard (`guard.Decide` in `internal/guard`), the five secret rules of
`internal/policy/builtin.go` (`tq/secret-store-read`, `tq/secret-store-shell`,
`tq/env-file-read`, `tq/env-file-shell`, `tq/env-dump`), `tq/commit-secret`,
`tq/cross-client-mcp` and the `tq/guard-error` fail-safe. Every rule decision is allow or
block; the only confirmation is the fail-safe when the guard itself fails (a block in
Codex). Nothing a manifest, a global file or a repository file adds to or removes from
that set; `internal/policy` has no other rules and no configuration surface.

Trust stays human-only without a hook rule: `tq allow` and `tq deny` refuse an agent
session by its environment marker (`agentSession` in `internal/cmd/humanonly.go`, the
check `tq secret` uses) and never prompt; `tq add` and `tq setup` scaffold a workspace
from a session but leave it untrusted and print the `tq allow` line. The off launchers
(`claude-off`, `codex-off`, `tq run --off`) set no variable: they switch the plugin off
through Claude's `--settings` and clean the identity environment, and the plugin's
`tq_hook.sh` has no early exit. `TQ_ENABLED=0` keeps its one meaning, shell activation
only. Codex gets one pinned pair, `approval_policy = "on-request"` and `sandbox_mode =
"workspace-write"`, in `config.toml` and as launch flags; a `claude.permission_mode:
bypass` renders as `bypassPermissions` because the hash-trusted manifest is the human's
choice.

## Consequences
- One code path per hook call, no layered resolution, no per-launch state;
  `tq settings render` is idempotent with no mode input.
- The guard's shell tokenizer (`splitWords` in `internal/guard/gitinv.go`) is the only
  tokenizer in the binary.
- Nothing confirms a `git push --force`, a cloud delete or a `terraform apply`. That is
  Claude Code's own permission settings' business, by the owner's decision (threat model
  AR2).
- We must keep: `TestReadOnlyGitExemptFromEnvDriftAndUntrusted` (identity drift
  blocks), `TestBuiltinIsTheFiveSecretRules` and the secret corpus
  (`TestBuiltinDecisions`, `claudehook_secrets_test.go`),
  `TestEditedManifestLosesTrust`, `TestAllowAndDenyRefuseAgents` and
  `TestAddDoesNotTrustFromAgent` (trust is by hash and human-only), `TestRunOff` (an off
  launch sets no tq variable).
- Upgrading an install: manifests load as they are; a block tq does not read is ignored
  with a doctor warning; run `tq settings render --all` once (`docs/MIGRATE.md`,
  "Upgrading an existing install").

## Alternatives considered
| Option | Why not |
|---|---|
| Let a manifest or a repo file add rules or confirmations | Every rule outside identity and secrets asks or blocks at some point; that is the cost the owner removed, and a configurable rule set is also what an agent would reach for |
| Make `TQ_ENABLED=0` the off launcher's marker | It would widen the kill switch from "no shell activation" to "no hook" for every session started from such a shell; the plugin is switched off per launch through `--settings` instead, which needs no variable |
| Render `permission_mode: bypass` as `acceptEdits` unless a second confirmation exists | `tq allow` of the manifest is that confirmation, and tq sets no permission mode of its own (D-30) |
