# Threat model

For anyone deciding whether to rely on tentaqles, and for reviewers of a change to it. It says what tentaqles
protects against, what it does not, and which tests keep each guarantee.

Tentaqles keeps a developer's clients apart in the terminal and in Claude Code. How it does that is in
[How it works](how-it-works.md); the hook's rules are in [the Claude Code hook](reference/claude-hook.md).

## What it is for

The risk it addresses is **a mistake**: a command, typed by the developer or run by an agent, that lands on the
wrong client's account. A commit with another client's email, a `gh pr create` as the wrong GitHub user, an
`az vm create` in another client's subscription, a Claude Code session billed to the wrong subscription, a link
opened in the wrong browser profile.

| Mistake | What prevents it | Tests |
|---|---|---|
| A commit with the wrong name or email | The client folder's `.gitconfig-tentaqles`, included for every repository under it by `includeIf`; `user.useConfigOnly` outside client folders; the hook's `git-email-drift` rule | `TestReadOnlyGitExemptFromEnvDrift`, `TestCanonicalSuite` (guard) |
| A CLI acting on another client's account | The client's config home for each CLI, set by the shell hook in the folder and every subfolder, and by `tentaqles run` | `TestRunClaudeHasTheClientsIdentity`, `TestRunDotResolvesCwd`, `TestEnv_ScopeLayerRestore` |
| A CLI the client doesn't use being reachable | The PATH mask and the unset variables of providers outside the client's scope; `tentaqles run` refuses them by any spelling | `TestEnv_ScopeReferenceScenario_PATH`, `TestEnv_ScopeReferenceScenario_SharedFolder`, `TestRun_OutOfScopeCLIRefused`, `TestOutOfScopeExecutables` |
| Claude Code on the wrong account | `CLAUDE_CONFIG_DIR` per client; `tentaqles run . -- claude` starts the folder's client | `TestRunClaudeHasTheClientsIdentity` |
| An agent pushing, calling `gh` or a cloud CLI under the wrong identity | The plugin's PreToolUse hook: the manifest's blocked commands, the wrong cloud, a remote command outside every client folder, a stale shell, the wrong `CLAUDE_CONFIG_DIR`, git email or gh user | `TestCanonicalSuite`, `TestNeutralRemoteNamesReach`, `TestReach_GuardsTheNewClient` |
| A session without that check | `tentaqles run` refuses to start a session when the client's plugin is missing or off, or its hook is not the guard, or `disableAllHooks` or a project setting turns it off | `TestRun_RefusesWithNoGuardHook`, `TestRun_DisabledPluginIsNoHook`, `TestRun_ForeignHookIsNoHook`, `TestRun_ProjectSettingsCountToo` |
| A session running another client's identity | From inside a Claude Code session, `tentaqles run` runs only the folder's client | `TestRunOtherClient_RefusedForAgents`, `TestRunOtherClient_NeutralFolderRefused`, `TestRunOtherClient_InheritedEnvIgnored`, `TestRunOtherClient_HumanAllowed` |
| An agent changing or deleting a client | `tentaqles reach`, `add`, `remove` and `drop` refuse to run inside a Claude Code session, and wait for a typed confirmation (`drop`: the client's name) | `TestReach_RefusedInsideClaudeCode`, `TestChange_Refusals`, `TestChange_DeclinedWritesNothing`, `TestDrop_Refusals`, `TestDrop_DeclinedDeletesNothing` |
| A hook that hangs a session | A 6-second decision deadline, a 7-second process watchdog and the script's own kill at 8 seconds, all under the 15-second hook timeout; a stall or a crash denies | `TestHookDeadlinesUnderHarnessTimeouts`, `TestHookStall_SpinningDecisionIsDenied`, `TestPreToolUseGuardPanicDenies` |
| A folder that copies another client's marker or manifest | A marker must name its own folder, and the manifest's `client` must too | `TestResolve_MarkerOrManifestOfAnotherClient` |
| A copy of a client's folder, with its name and marker, under another clients folder | The registry records the folder each client was created in, and only that folder resolves; the copy gets no identity and the hook denies shell commands there. Only a person moves the record, with `tentaqles bind` and a typed `yes` | `TestResolve_SecondFolderWithAClientsName`, `TestResolve_ClientWithNoRecordedFolder`, `TestPreToolUse_CopiedFolderDenies`, `TestEnv_SecondFolderGetsNoIdentity`, `TestBind_RefusedForAgents`, `TestBind_DeclinedWritesNothing`, `TestDrop_RefusesACopiedFolder` |
| A link opened in the wrong browser profile | `BROWSER`, `xdg-open` and `open` shims, and `tentaqles open`, start the client's browser with its profile | `TestBrowserShims`, `TestOpen_UsesTheClientsProfile`, `TestOpen_Refusals` |
| A Codex command under the wrong identity | The client's Codex home (`CODEX_HOME`) with tentaqles' PreToolUse hook, which applies the Claude Code hook's rules and answers with Codex's deny; `tentaqles run` refuses a Codex session unless both hooks are present, enabled and trusted | `TestCodexPreToolUse_WrongGitEmailDenies`, `TestCodexPreToolUse_NeutralFolderBlocksPush`, `TestCodexPreToolUse_MatchingIdentityAllows` |
| A Codex hook that errors, or is missing from the client's config | Every error path of `codex-hook pre-tool-use` answers the deny object, so a call tq cannot judge is refused. `tentaqles run` checks the two hooks are there, and `tentaqles doctor` reports a missing, disabled or untrusted one | `TestCodexPreToolUse_UnreadablePayloadDenies`, `TestCodexPreToolUse_PanicDenies`, `TestCodexPreToolUse_DeadlineDenies`, `TestCodexPreToolUse_WatchdogDenies`, `TestRunCodex_RefusesWithoutTheHooks`, `TestRunCodex_RefusesHooksOffOptions`, `TestCodexHookProgram`, `TestDoctor_CodexGuard` |

## What it does not do

- **It is not a sandbox.** Every client's files and logins are readable by your user, and so by any program you
  run, Claude Code included. A CLI run by its full path, a script, or a different shell gets past the PATH mask.
- **The hook reads text.** It checks the command line Claude Code is about to run, and only shell commands (Bash and
  PowerShell); it doesn't see file edits or MCP tools. The Codex hook checks Bash calls only, and Codex reports
  PowerShell commands as Bash too. A command spelled to hide what it does (a variable, an alias,
  a script file) gets past it. A `gh` or cloud command wrapped in `sh -c`, `pwsh -Command`, `cmd /c` or `iex`
  gets past it too; a git call wrapped that way is read, three levels deep.
- **Codex fails open when its hook is missing or crashes.** Codex allows a call when its hook gives no answer: a hook
  whose binary is gone, or one that dies outside tentaqles' own error paths. tentaqles answers deny on every error it
  can see, and `tentaqles run` checks the hooks are present, enabled and trusted, but a Codex started by its full path,
  outside `tentaqles run`, skips that check. So does a setting only Codex reads: tentaqles reads the top-level
  `[features]` of the client's config, not a `[profiles.<name>.features]` table or a project's `.codex/config.toml`.
- **A failed lookup allows.** The `gh-user` rule asks `gh` for the active user with a 4-second limit; when the
  lookup fails or times out, the call is allowed.
- **"Inside Claude Code" is a convenience check, not a boundary.** The refusals of `reach`, `add`, `remove`,
  `drop` and of another client's `tentaqles run` read the `CLAUDECODE` variable Claude Code sets for the commands it
  runs. A session can unset it.
- **Sessions start without Claude Code's permission prompts.** `tentaqles run <client> -- claude`, and the `claude`
  launcher the shell hook defines, add `--dangerously-skip-permissions` unless the command line sets a permission
  mode, runs a `claude` subcommand, or asks for `--help` or `--version` (`TestLaunchClaudeArgs`). The hook is the
  check that remains; a `claude` started from inside a session gets no such flag.
- **`tentaqles drop` deletes logins for good.** The client's identity folder, with every CLI's login and its Claude
  config, is deleted after the typed name; nothing keeps a copy.
- **Nothing is signed.** The manifests, the shell profile blocks and the client's Claude settings are files your
  user can edit, and so can an agent. An edited manifest takes effect at the next prompt.
- **Some logins are shared by the CLI itself.** `aws sso login` caches tokens in `~/.aws/sso/cache`, the token from
  `terraform login` lives in the shared `terraform.d` folder, and `acli` keeps one login per machine. The catalog
  says so in each provider's `caveat` or `notes` ([providers](reference/providers.md)).
- **MCP servers and claude.ai connectors** are whatever the client's Claude config holds; tentaqles neither adds
  nor removes them.

## Releases

The installers check each archive's SHA-256 against the release's `checksums.txt` and refuse a mismatch or a
missing entry; with `gh` logged in they also check the build provenance the release workflow attests. The plugin
installs from a marketplace entry pinned to a release tag and its commit (`TestRootMarketplacePin`).
