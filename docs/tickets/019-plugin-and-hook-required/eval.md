# 019: `reach` installs the plugin, and Claude won't start without the guard: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use a fake `claude` that records its argv and environment. A plugin fixture holds `.claude-plugin/plugin.json` and `hooks/hooks.json`.

## Not covered

A real `claude plugin install` needs the network and a real release, so the owner checks it in the end-to-end run.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 Reach installs the plugin
- **Covers**: REQ-1
- **Method**: test TestReach_InstallsThePlugin
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_InstallsThePlugin$' -count=1`
- **Expect**: The fake records both commands under the client's `CLAUDE_CONFIG_DIR`, and the client is trusted afterwards.

### U-02 --no-plugin
- **Covers**: REQ-3
- **Method**: test TestReach_NoPluginFlag
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_NoPluginFlag$' -count=1`
- **Expect**: The fake records nothing.

### U-03 Claude starts with the hook there
- **Covers**: REQ-4
- **Method**: test TestRun_StartsWithTheHook
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRun_StartsWithTheHook$' -count=1`
- **Expect**: With the plugin fixture installed and enabled, the fake claude starts.

### F-01 The install fails
- **Covers**: REQ-2
- **Method**: test TestReach_PluginInstallFails
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_PluginInstallFails$' -count=1`
- **Expect**: For a failing fake, a hanging one at a lowered limit, and no `claude` on PATH, reach exits 0, prints the reason and the line to run, and the client is trusted.

### F-02 No hook, no session
- **Covers**: REQ-4, REQ-5, REQ-7
- **Method**: test TestRun_RefusesWithNoGuardHook
- **Steps**:
  1. `go test ./internal/cmd ./internal/doctor -run 'TestRun_RefusesWithNoGuardHook|TestDoctor_GuardHookMissing' -count=1`
- **Expect**: Exit non-zero with the exact fix line, the fake claude didn't start, and doctor reports `guard-hook-missing`.

### F-03 Disabled counts as missing
- **Covers**: REQ-8
- **Method**: test TestRun_DisabledPluginIsNoHook
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRun_DisabledPluginIsNoHook$' -count=1`
- **Expect**: `enabledPlugins` false, `disableAllHooks` true, and a plugin whose `hooks.json` has no `PreToolUse` each refuse, and the `disableAllHooks` line names the setting.

### Q-01 The check stays cheap
- **Covers**: REQ-4
- **Method**: test BenchmarkGuardHookPresent
- **Steps**:
  1. `go test ./internal/cmd -run '^$' -bench '^BenchmarkGuardHookPresent$' -benchtime=20x`
- **Expect**: Under 20 ms per run, with no subprocess.

### R-01 The way through works, and other commands pass
- **Covers**: REQ-6, REQ-9, REQ-10
- **Method**: test TestRun_RefusalOnlyForASession
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRun_RefusalOnlyForASession$' -count=1`
- **Expect**: With no hook, `claude plugin install x`, `claude --version` and `gh pr list` all run, and `claude` in a neutral folder runs.

### R-02 The launcher's flags hold
- **Covers**: REQ-4
- **Method**: test TestRunClaudeAlwaysBypass
- **Steps**:
  1. `go test ./internal/cmd -run 'TestRunClaudeAlwaysBypass|TestLaunchClaudeArgs' -count=1`
- **Expect**: `ok`.

### S-01 An agent can't remove the guard and start again
- **Covers**: REQ-11
- **Method**: test TestProtectedPaths_PluginRegistration
- **Steps**:
  1. `go test ./internal/cmd -run '^TestProtectedPaths_PluginRegistration$' -count=1`
  2. Feed the hook a Write on `settings.json`, on `installed_plugins.json` and on the plugin's `hooks.json`.
- **Expect**: Each is denied with `BLOCKED [self-protection/tq/protected-path]`.

### S-02 A hook that isn't tq's doesn't count
- **Covers**: REQ-4
- **Method**: test TestRun_ForeignHookIsNoHook
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRun_ForeignHookIsNoHook$' -count=1`
- **Expect**: A plugin whose `PreToolUse` runs something other than `tq_hook.sh pre-tool-use`, or whose matcher leaves out `Bash` or `^mcp__`, refuses.

### S-03 An edited hook still loses trust
- **Covers**: REQ-1, REQ-11
- **Method**: test TestPluginHookChangesAreNamed
- **Steps**:
  1. `go test ./internal/trust ./internal/cmd -run 'TestPluginHookChangesAreNamed|TestFlippedByteFailsVerification' -count=1`
- **Expect**: `ok`, with those tests unchanged.

### S-04 The plugin source is the pinned marketplace
- **Covers**: REQ-1
- **Method**: test TestReach_PluginSourceIsFixed
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_PluginSourceIsFixed$' -count=1`
- **Expect**: The commands hold only the constant marketplace and plugin names, with nothing from the manifest, the environment or the folder.
