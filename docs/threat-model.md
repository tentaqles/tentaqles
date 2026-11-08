# Threat model

Tentaqles keeps a developer's clients apart in the terminal and in Claude Code. This page says what it protects
against and what it does not.

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
| An agent pushing, calling `gh` or a cloud CLI under the wrong identity | The plugin's PreToolUse hook (`docs/CLAUDE-HOOK.md`): wrong cloud, wrong git email, wrong gh user, a stale shell, a remote command outside every client folder | `TestCanonicalSuite`, `TestNeutralRemoteNamesReach`, `TestReach_GuardsTheNewClient` |
| A session without that check | `tentaqles run` refuses to start a session when the client's plugin is missing, off, or disabled by `disableAllHooks` | `TestRun_RefusesWithNoGuardHook`, `TestRun_DisabledPluginIsNoHook`, `TestRun_ForeignHookIsNoHook`, `TestRun_ProjectSettingsCountToo` |
| A session running another client's identity | From inside a Claude Code session, `tentaqles run` runs only the folder's client | `TestRunOtherClient_RefusedForAgents`, `TestRunOtherClient_NeutralFolderRefused`, `TestRunOtherClient_InheritedEnvIgnored`, `TestRunOtherClient_HumanAllowed` |
| A hook that hangs a session | A 6-second decision deadline, a 7-second process watchdog and the script's own kill at 8 seconds, all under the 15-second hook timeout; a stall or a crash denies | `TestHookDeadlinesUnderHarnessTimeouts`, `TestHookStall_SpinningDecisionIsDenied`, `TestPreToolUseGuardPanicDenies` |
| A folder that copies another client's marker or manifest | A marker must name its own folder, and the manifest's `client` must too | `TestResolve_MarkerOrManifestOfAnotherClient` |
| A link opened in the wrong browser profile | `BROWSER`, `xdg-open` and `open` shims, and `tentaqles open`, start the client's browser with its profile | `TestBrowserShims`, `TestOpen_UsesTheClientsProfile`, `TestOpen_Refusals` |

## What it does not do

- **It is not a sandbox.** Every client's files and logins are readable by your user, and so by any program you
  run, Claude Code included. A CLI run by its full path, a script, or a different shell gets past the PATH mask.
- **The hook reads text.** It checks the command line Claude Code is about to run. A command spelled to hide what it
  does (a variable, an alias, `sh -c`, a script file) gets past it. It only checks shell commands; it doesn't see
  file edits or MCP tools.
- **Nothing is signed.** The manifests, the shell profile blocks and the client's Claude settings are files your
  user can edit, and so can an agent. An edited manifest takes effect at the next prompt.
- **Some logins are shared by the CLI itself.** `aws sso login` caches tokens in `~/.aws/sso/cache`, the token from
  `terraform login` lives in the shared `terraform.d` folder, and `acli` keeps one login per machine. The catalog
  says so in each provider's `caveat` or `notes`.
- **MCP servers and claude.ai connectors** are whatever the client's Claude config holds; tentaqles neither adds
  nor removes them.

## Releases

The installers check each archive's SHA-256 against the release's `checksums.txt` and refuse a mismatch or a
missing entry; with `gh` logged in they also check the build provenance the release workflow attests. The plugin
installs from a marketplace entry pinned to a release tag and its commit (`TestRootMarketplacePin`).
