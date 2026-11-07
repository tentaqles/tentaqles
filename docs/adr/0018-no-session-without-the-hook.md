# ADR-0018: No Claude session without the guard

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md` (the spec: "the tentaqles plugin is already installed for that harness, or the developer installs it", and "a hook blocks it"), ADR-0004 (signed record), ADR-0008 (identity PATH), ADR-0012 (`tentaqles reach`), ADR-0014 (the plugin is the onboarding and the hook)

## Context

The guard of a Claude session is the tentaqles plugin's `PreToolUse` hook: `plugin/hooks/hooks.json` runs
`tq_hook.sh pre-tool-use` before each Bash, PowerShell, file or MCP tool call (ADR-0014). The hook comes only from
the plugin. A client whose Claude config has no enabled plugin therefore starts a session with no guard at all, under
any identity: the git, gh and cloud rules, the secret rules and the self-protection rules are all in that hook.

Until now `tentaqles reach` made a client without installing the plugin, and `tq run <ws> -- claude` started claude
regardless. A developer who had not run the plugin install by hand worked unguarded and nothing said so. The
`hook-missing` finding of `tq doctor` looks at the shell hook, not at the plugin.

Crossing a trust boundary: this decides what a session may run under (`internal/trust/`, `internal/cmd/run.go`,
`internal/cmd/reach.go`), and it installs a plugin into a client's Claude config, the file the integrity record
covers (`docs/threat-model.md`, B1).

## Decision

- **`tentaqles reach` installs the plugin.** Once the client's trust is signed, reach runs
  `claude plugin marketplace add tentaqles/tentaqles` and `claude plugin install tentaqles@tentaqles`, each with the
  client's `CLAUDE_CONFIG_DIR` (through `envplan.Environ`), each under a two-minute limit, then signs the record
  again (the plugin changes `settings.json`'s `enabledPlugins` and `installed_plugins.json`, both covered). The
  arguments are constants: nothing from the manifest, the environment or the folder reaches them. A failure does not
  fail reach: the client stays trusted, reach prints the reason and the exact line to run, and exits 0. `--no-plugin`
  skips the step. A marketplace add that fails does not stop the install, since the marketplace may be there already.
- **`tq run <ws> -- claude` and `tq run . -- claude` refuse a session without the guard.** A session is claude with
  no subcommand and no `--help` or `--version`, in an identity's folder. The refusal is checked before anything
  launches, exits non-zero, and prints the reason and the fix line `tq run <ws> -- claude plugin install
  tentaqles@tentaqles`. The guard is present when the client's `settings.json` does not set `disableAllHooks`, the
  tentaqles plugin is installed (the same plugin folder the integrity record covers, `trust.PluginRoot`), its
  `enabledPlugins` entry is `true` under the marketplace `tentaqles`, and its `hooks/hooks.json` has a
  `PreToolUse` command hook that runs `tq_hook.sh pre-tool-use` under a matcher that selects Bash and an MCP tool.
  Anything the check cannot read is missing.
- **Not refused:** `claude` subcommands (the fix line itself), `--help` and `--version`, every other program through
  `tq run` (`gh pr list`), and `claude` in a neutral folder, which is unchanged. The refusal covers only `claude`
  started through tq; a `claude` a person starts by its path, outside the tq hook block, is outside it (AR1).
- **`disableAllHooks`** counts as no hook; its fix line names the setting and the `tq hooks verify --accept` that
  signs the edit, since the setting is a covered key.
- **`tq doctor`** reports `guard-hook-missing` as an error for each trusted client that would refuse, with the same
  fix line. It runs the same check.
- **The check** is `trust.CheckGuardHook`: a few small files and no process, so the hook path does not pay for it.
  It is not called from the hook: the hook judges a tool call, not a launch.

## Consequences

- A new client gets the guard at reach, and a session in a client that lost it is refused with the line that
  restores it. A client made before this release, with no plugin, is refused until its developer runs the line
  (`tq doctor` names it). Nothing is installed without reach or that line.
- A failed install leaves the record as it was before the attempt: a claude that changed a covered file before it
  failed makes the record a mismatch, and `tq run` names that file and `tq hooks verify --accept`.
- The install needs network access, and it installs what the root marketplace pins (`.claude-plugin/marketplace.json`,
  a release tag and its sha). The release that carries this change is cut separately, so until then a new client
  gets the release that is pinned now.
- The fix line installs the plugin and does not add the marketplace. Reach adds it. A client whose reach could not
  add it gets the install's own error, which names the marketplace.

## Verification

`TestRun_RefusesWithNoGuardHook`, `TestRun_StartsWithTheHook`, `TestRun_DisabledPluginIsNoHook`,
`TestRun_ForeignHookIsNoHook`, `TestRun_RefusalOnlyForASession`, `TestReach_InstallsThePlugin`,
`TestReach_NoPluginFlag`, `TestReach_PluginInstallFails`, `TestReach_PluginSourceIsFixed`,
`TestProtectedPaths_PluginRegistration`, `TestDoctor_GuardHookMissing`, `TestGuardHook_ShippedHooksPass`,
`TestGuardHook_FailsClosed`, and the benchmark `BenchmarkGuardHookPresent`.
