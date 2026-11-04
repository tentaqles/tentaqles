# ADR-0001: The guard narrows to identity drift and secrets; modes and the rule registry go

Status: accepted
Date: 2026-11-04
Cites: PRD §13 D-29, D-30, D-31; threat model boundaries B1 (the hook), B2 (human-only actions)

## Context
Until v0.8.2 the PreToolUse hook applied a registry of built-in rules
(git history, cloud and IaC, databases, MCP writes, Claude settings, system
folders, elevation, the guard's own files) under a guard mode (`off`,
`default`, `relaxed`, `strict`, `custom`) resolved from five layers (a
global `guard.yaml`, the trusted manifest, repo files, a per-launch `TQ_MODE`,
a human grant pinned to the manifest hash). On 2026-11-03 the repo owner
found that the confirmations and blocks this produced, even in bypass mode,
cost more than they protected, and decided (D-29) that tentaqles is an
identity isolator with secrets protection and nothing else: the hook allows
or blocks and never asks (D-31), and tq sets no permission mode of its own
beyond the launcher's skip (D-30). Slices 2 and 3 of the plan already trimmed
the hook to the identity guard and the secret rules and deleted the hub and
the packs; the modes, grants, the registry and the Codex rules file were
still in the binary, read by `tq mode`, `tq guard`, `tq run --tq-mode`,
`tq allow --mode` and `tq settings render`.

## Decision
We delete the guard modes and everything that fed them: `internal/guardcfg`,
`internal/guardlog`, the mode grants in `internal/trust`, the `guard:`
manifest block, `tq mode`, `tq guard`, `tq unattended`, the mode flags of
`tq allow`, `tq run --tq-mode` and the `TQ_MODE` variable, `tq env --off|--on`
and the `tq off`/`tq on` shell functions, the status line's mode segment, and
the Codex exec-policy file. `internal/policy` keeps its evaluator and exactly
five built-in rules (`tq/secret-store-read`, `tq/secret-store-shell`,
`tq/env-file-read`, `tq/env-file-shell`, `tq/env-dump`); the registry of
other rules (`builtin_system.go`, `builtin_agents.go`, `registry.go`) and the
manifest, global and project rule layers go. The hook's rule set is therefore
the identity guard (`guard.Decide`), the five secret rules, `tq/commit-secret`,
`tq/cross-client-mcp` and the `tq/guard-error` fail-safe, in every Claude
permission mode.

Trust stays human-only without a hook rule: `tq allow` and `tq deny` refuse
an agent session by its environment marker (`agentSession`, the check
`tq secret` uses) and never prompt. The off launchers (`claude-off`,
`codex-off`, `tq run --off`) set no variable: they switch the plugin off
through Claude's `--settings` and clean the identity environment, and the
plugin's `tq_hook.sh` has no early exit. `TQ_ENABLED=0` keeps its one meaning,
shell activation only. Codex gets one pinned pair, `approval_policy =
"on-request"` and `sandbox_mode = "workspace-write"`, in `config.toml` and as
launch flags; a `claude.permission_mode: bypass` renders as
`bypassPermissions` because the hash-trusted manifest is the human's choice.

## Consequences
- Easier: one code path per hook call, no layer resolution, no grant files,
  no per-launch state; `tq settings render` is idempotent with no mode input.
- Harder: nothing confirms a `git push --force`, a cloud delete or a
  `terraform apply` any more. That is Claude Code's own permission settings'
  business, by the owner's decision (threat model AR2).
- We must keep: `TestReadOnlyGitExemptFromEnvDriftAndUntrusted` (identity
  drift blocks), `TestBuiltinIsTheFiveSecretRules` and the secret corpus
  (`TestBuiltinDecisions`, `claudehook_secrets_test.go`),
  `TestEditedManifestLosesTrust` and `TestAllowAndDenyRefuseAgents` (trust is
  by hash and human-only), `TestRunOff` (an off launch sets no tq variable).
- Migration: existing manifests load; a `guard:` block is ignored with a
  doctor warning; Codex identities need one `tq settings render --all`
  (`docs/MIGRATE.md`, "Upgrading to 0.9.0").

## Alternatives considered
| Option | Why not |
|---|---|
| Keep `default` as the only mode, delete the others | Every rule outside identity and secrets still asks or blocks under `default`; that is the cost the owner removed |
| Keep the rule registry with every rule disabled by default | Dead code with a configuration surface (`guard.disable`, `guard.yaml`) that no longer means anything; a loosened registry is also what an agent would reach for |
| Make `TQ_ENABLED=0` the off launcher's marker | It would widen the kill switch from "no shell activation" to "no hook" for every session started from such a shell; the plugin is switched off per launch through `--settings` instead, which needs no variable |
| Render `permission_mode: bypass` as `acceptEdits` without a grant | The grant was the second confirmation; with grants gone, `tq allow` of the manifest is that confirmation, and tq sets no permission mode of its own (D-30) |
