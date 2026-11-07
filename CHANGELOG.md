# Changelog

Notable changes to the `tq` CLI and the docs of this repository. The Claude Code plugin
keeps its own log in [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md). Releases before 0.9.0
are described on the [GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

### `tq run` refuses another client in an agent session (ADR-0020)

- In an agent session, `tq run <other> -- <cmd>` exits 1 and starts nothing when `<other>` is not
  the client of the folder the command runs in. The refusal is one `identity` line in the denial
  log. `tq run .` and `tq run <its own client>` still launch, and a human's `tq run <any client>`
  is unchanged.

### Cut down to IDEATION (ADR-0013, ADR-0014)

- Removed `tq add`, `tq init`, `tq onboard`, `tq setup`, `tq worktrees`, `tq run --off` and the
  `claude-off` and `codex-off` functions. `tentaqles reach` is how a client is set up.
- Removed Codex support. Claude Code is the only harness.
- The plugin is the onboarding skill and the guard hook. Session memory, the knowledge graph, the
  other skills, the agents and all the plugin's Python are gone; existing data stays on disk.
- With no Python fallback, a missing tq denies every tool, not only shell commands.
- After upgrading, each client needs one `tq hooks verify --accept`.

### `tentaqles reach` (ADR-0012)

- `tentaqles reach [<path>]` makes a folder a client identity: the banner, the form, the manifest
  and the trust, after one typed `yes`. It's human-only. In an existing client it shows the client.
- The CLI's name is `tentaqles`, built from `cmd/tentaqles`; `tq` is the same program. The hook treats
  both names alike. The release and the installers still ship `tq` only.
- The catalog gains Discord, Zoom, Google Meet, Google Colab and Trello.

### One guard, always on (ADR-0011)

- There is no `guard: v2` flag. Every trusted workspace gets the full guard: the identity PATH,
  the MCP filter and the provider-scope rules of the hook. An old `guard:` line still loads and
  means nothing.
- **Upgrading changes existing workspaces.** A tool the manifest does not declare is now blocked.
  Add it to the manifest's `scope:` list and run `tq allow`.
- A client's declared cloud (`cloud.provider`) is always in its scope and gets its own config
  folder, also when `identities` leaves it out. Such a client logs in to that CLI again.

### Human-only commands work in an IDE terminal (ADR-0010)

- A human in a VS Code integrated terminal was refused every human-only command, because the
  Claude Code IDE integration sets `CLAUDE_CODE_SSE_PORT` there. It is no longer a marker.
  `CLAUDECODE` and the process tree still catch an agent running in an IDE.
- A shell or runtime whose command line runs a script from a folder named `claude` or `codex`
  was taken for that harness. Only a path's last part counts now.
- The plugin's side: [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md).

### Deny only, fail closed, no permission settings, the denial log (ADR-0006)

- **tq never asks.** The hook has two answers, allow and deny (PRD v2 §6.3). A commit whose
  secret scan cannot finish (the hook budget, the size limits, an unreadable file,
  `--pathspec-from-file`, a repository it cannot place) is denied as
  `BLOCKED [secrets/tq/commit-secret]`, and a guard panic is denied as
  `BLOCKED [tq/guard-error]`. `policy.Ask`, the `CONFIRM` prefix and the PreToolUse ask JSON
  are gone; a rule whose action is not deny does not compile (`TestNoAskCodePath`,
  `TestNoAskVerdict`).
- **Fail closed.** `pre-tool-use` denies a call it cannot judge with `tq/guard-error`: a
  payload that is not valid JSON or UTF-8, `null`, empty or names no tool (it was allowed); a
  shell tool without a string `command`, a path field that is not a string, a Read naming no
  file, an unknown or lower-case tool that carries a command or a path; a decision past 6 s,
  a process still running at 7 s, and a usage error of the hook command (`tq.RunMain`), all
  under the harness hook timeouts (`TestHookDeadlinesUnderHarnessTimeouts`); a `config.yaml`
  tq cannot read; a folder whose marker or manifest is there but no usable manifest can be
  read, and a folder tq's evidence names as an identity (a signed record, the protected
  manifest) whose marker, manifest or `config.yaml` entry is gone, for every tool, until a
  human fixes it. A folder no evidence names stays neutral. Under `--agent codex` every deny
  writes the Codex deny JSON and exits 2; the Codex launch is refused when `tq` is not on its
  PATH. The plugin stops a hanging tq and turns any other exit into a deny
  (`plugin/CHANGELOG.md`).
- **The identity's scope in the deny.** An identity deny ends with `Scope of identity <name>:
  git <host>; cloud <provider>; MCP <servers>`, so the model reports the mistake instead of
  retrying.
- **The denial log records the command, redacted.** Each line of `~/.tentaqles/denials.jsonl`
  now has `category`, `rule`, `layer` (`hook`, `filesystem` for a protected path,
  `human-only`), `caller` (`harness`, `subagent` from the payload's `agent_id`, `no-tty`),
  `harness` (`claude`, `codex`), `tool`, `command`, `subject`, `reason`, `identity`, `cwd`,
  `pid`. `command` is the shell line redacted by a structural rule: only operators, command
  words, flag names, assignment names, variable references, the matched subject and a list of
  subcommand words in the subcommand position are kept, every other word is `…` (0 leaks on
  a 78-command corpus with secrets in every argument shape). Control characters are dropped.
  The log rotates at 1 MiB into `denials.jsonl.1`. `kind` and `workspace` are renamed
  `category` and `identity`; old lines still read.
- **`tq status`**, the successor name of `tq doctor` (which stays): the doctor report, and with
  `--denials [--last N] [--identity <ws>] [--category <c>] [--json]` the newest entries of the
  denial log, read a line at a time, every control character printed escaped. `--denials`
  refuses an agent session (it writes nothing and needs no terminal).
- **No permission settings.** `claude.permission_mode` and `claude.permissions` leave the
  manifest (an old manifest that has them loads; they are reported as dropped);
  `tq settings render` writes no `permissions.defaultMode`, no allow, deny or ask list (the
  allow and deny baselines are gone) and no `skipDangerousModePermissionPrompt`, and the next
  render removes the ones tq wrote, keeping yours (decided from evidence: the state file's
  entries, the `defaultMode` an older render wrote for the manifest's old `permission_mode`;
  a corrupt state file claims nothing). The deny baseline (`Read(~/.ssh/id_*)`,
  `Bash(printenv)`, ...) goes too: the hook's secret rules cover it. Codex's `config.toml`
  gets no `approval_policy`/`sandbox_mode` (its marker is found with white space ignored; an
  edited marker is a conflict); the Codex launcher starts Codex with
  `--dangerously-bypass-approvals-and-sandbox` (after `exec`/`resume`/`fork`, which take it
  themselves), a typed approval or sandbox flag, glued forms included, going through as typed; `--permission-mode` leaves `tq add`, the setup wizard,
  `tq onboard`, setup plans and `pkg/setupapi`; the session preamble drops
  `permission_mode=`; doctor's `bypass-cloud` and `permission-mode-overridden` checks are gone.
  How the harness asks for permission is the harness's own setting.
- `commitscan.AskError`/`IsAsk` are `IncompleteError`/`IsIncomplete`; `resolve.Result` gains
  `Name` and `Unreadable`; `humanonly.CheckRead` is the human-only check of a read.

### The identity PATH, environment and MCP list from the scope (ADR-0008)

- **Layer 2, PATH** (`guard: v2` only): `tq env` and every launch (`tq run`) filter the
  inherited PATH. A folder whose runnable files are all out-of-scope CLIs (the AWS CLI's
  install folder for an Azure-only identity) is dropped, so `aws` is command-not-found; a
  folder shared with other tools (npm's global folder, scoop, WinGet Links, `/usr/local/bin`)
  stays, and `~/.tentaqles/identities/<ws>/path-mask/` goes first with a stub per out-of-scope
  name (`<name>: command not found`, exit 127). The mask is written by `tq allow` and
  `tq settings render`, never by `tq env`, and used only when its files are exactly tq's;
  otherwise the prompt says `tq: PATH mask missing`. Leaving the folder restores the PATH byte
  for byte (a change you made inside it is kept). The bash and zsh hooks pass their own PATH
  spelling to `tq env` on Windows (`__TQ_SH_PATH`, excluded from MSYS conversion), so Git Bash
  gets `/c/...` entries back. `tq run <ws> -- <cmd>` resolves `<cmd>` on the identity PATH.
  Cost: 0.24 ms per prompt in the folder, about 12 ms on entering it (Windows, 59 entries).
- **Layer 2, environment**: the out-of-scope providers' variables of the match lists
  (`AWS_PROFILE`, `AWS_ACCESS_KEY_ID`, `GOOGLE_APPLICATION_CREDENTIALS`, `GITHUB_TOKEN`,
  `TF_TOKEN_*`, ...) are unset in the identity terminal and launches, and restored on leaving;
  a variable an in-scope provider claims is kept.
- **Layer 3, MCP**: `tq settings render` (and `tq allow`) removes the MCP servers tq can tell
  are out of scope (name token, `url` host or `command` of an out-of-scope provider) from the
  identity's `.claude.json` (user and project scope) and Codex `config.toml`; servers that
  match no provider stay; removed entries are kept in `mcp-out-of-scope.json` beside the
  config. `tq bundle sync` skips an out-of-scope bundle server.
- **Layer 1**: the session-start preamble states the scope in one line.
- **Round 2** (critic findings): `__TQ_STATE` is zlib-compressed and holds one PATH (a 3.5 KB
  PATH now survives cmd.exe; one over cmd.exe's 8191-character line is left as it is with a
  notice at every prompt). cmd.exe takes every PATH character (`% ! ^ & | < > "`), with
  delayed expansion on too. The mask covers every spelling (`aws.exe`/`.com` link to the tq
  binary acting as a stub, plus `.bat .cmd .ps1 .vbs .js`), comes back once fixed, and `tq run`
  refuses an out-of-scope name however spelled. Generic tools' variables (`NPM_TOKEN`,
  `TF_TOKEN_*`) are never unset; ambiguous names (`cn tea snow render vc fly atlas copilot
  goose`) are never hidden. MCP servers are attributed by name, command, args (new match list
  `mcp_packages`), env keys and url, at render and in the hook (`mcp__<server>__*` judged by the
  server's config entry). `.claude.json` keeps its formatting; a corrupt
  `mcp-out-of-scope.json` is moved aside.
- **Round 3**: a server name defined in several sources is denied when any of its entries is
  out of scope (user, local, `.mcp.json`, installed plugins' `.mcp.json`, Codex, and the
  entries render removed); vendor remote endpoints (`mcp_hosts`: `mcp.vercel.com`,
  `mcp.supabase.com`, `*.mcp.cloudflare.com`, `api.githubcopilot.com`), more `mcp_packages`
  and images, PyPI-normalized names, a command's file name, shell-wrapped strings, docker
  `-e` variables and `--prebuilt` toolsets attribute a server; gh's env gains
  `GITHUB_PERSONAL_ACCESS_TOKEN`. A lost or corrupt `__TQ_STATE` with `TQ_WS` set fails
  closed with a notice; launches carry their own state. cmd.exe's line budget is measured
  (8192). The mask stub is always the tq binary.
- **Round 4**: the PowerShell and cmd hooks read tq's UTF-8 output as UTF-8 (accented and CJK
  PATH folders survive), every catalog vendor's official MCP host or package is catalog data,
  a package runner's out-of-scope CLI or module and a vendor's referenced credential variable
  attribute an MCP server, plugin `mcpServers` paths and arrays are read, a missing state is
  lost in harness shells too, and the scope line says claude.ai connectors stay listed.
- **The hybrid MCP list** (owner's decision, 2026-11-05): an MCP server is kept only when the
  catalog attributes it to an in-scope provider, when it is a general-purpose server of
  `internal/providers/mcp_general.yaml` (playwright, context7, memory, filesystem, fetch, git,
  sequential-thinking, time, everything, postgres, sqlite, puppeteer, chrome-devtools,
  microsoft-learn), or when the manifest's new `mcp.allow` names it; everything else is moved
  to `mcp-out-of-scope.json` at render and denied by the hook (connectors and plugin servers
  too).
- Without `guard: v2` nothing changes.

### The provider scope and the identity category for every tool (ADR-0007)

- **Merged onto ADR-0006.** The deny's scope line (`Scope of identity <name>: ...`) now lists the
  catalog ids of `Manifest.Scope()`, from the one helper `scopeNote`. Identity denies log
  category `identity` and layer `hook` (`filesystem` for `other-identity`). A bad user provider
  file and an unreadable manifest deny through the same fail-closed path.

- **`manifest.Scope()`**: the sorted catalog ids an identity may touch, derived from what a
  manifest already declares (the identities block, or claude and gh for a v1 manifest, the
  `cloud` block, the catalog id of `git.provider`, the chat, email and data_platform
  providers, `agents`, the providers of `claude.bundle.mcp`) plus an optional `scope:` list
  of catalog ids. Existing manifests stay valid.
- **Match lists as catalog data.** Every one of the 62 files in
  `internal/providers/catalog/` gains a `match:` block (`executables`, `modules`, `env`,
  `paths`, `hosts`, `mcp`), validated at load; a user file under
  `~/.tentaqles/providers/<id>.yaml` adds to a built-in provider's lists and never shortens
  them. Generic tools (docker, helm, kubectl, terraform, pulumi, npm, psql, dbt) are never
  matched by command name. See `docs/PROVIDERS.md`, "Match lists".
- **`guard: v2`** (a top-level manifest scalar; the dropped `guard:` mapping still loads as
  no flag) turns on six identity rules in `tq claude-hook pre-tool-use`, for every tool, after
  the manifest's blocked commands and before secrets: `identity/out-of-scope-cli` (by name,
  path, wrapper, nested shell, `python -m`, package runner, `Start-Process`),
  `identity/out-of-scope-env`, `identity/out-of-scope-path`, `identity/out-of-scope-host`
  (URLs in shell lines, network verbs' hosts, WebFetch), `identity/out-of-scope-mcp` and
  `identity/other-identity` (another identity's workspace root or `~/.tentaqles/identities/<other>`).
  The deny names the identity's scope and asks the model to report instead of retrying.
  `wrong-cloud` is folded in: it does not run under the flag. Without the flag, and in a
  neutral cwd, the v1 rules are unchanged. See `docs/CLAUDE-HOOK.md`, "Identity scope".
- **Precision and fail-closed (round 2).** Claims are provider-specific: a shared CLI is
  owned per subcommand (`match.subcommands`; `azure-devops` owns `az devops|repos|...`, not
  `az vm`), a provider's claims come from the embedded catalog only (a user file can only
  widen denial), and `tq providers list` reports every provider overlap and every bad file
  without aborting. New `match` fields `subcommands` and `images` (docker vendor images);
  generic cross-cloud tools (kube, npm, docker, helm, terraform, pulumi, dbt, postgres) claim
  no credential path, so a kubeconfig/npmrc stays readable. A folder whose protected manifest
  cannot be read (`guard: v3`, an unknown `scope:` id, bad YAML) or an invalid user provider
  file denies every tool with `tq/guard-error`. grep patterns, commit messages, here-strings
  and single-quoted bash strings are treated as text, not expansions. More routes are
  matched: `wsl`/`find -exec`/`cmd start`/`docker run`/`Get-Command`/`Start-Process`, `a\ws`,
  `python -m` past `-X`/`-W`, env APIs (`[Environment]::`, `Env:AWS_*`, `${#NAME}`,
  `declare -p`, `process.env` destructuring), `tar -C`, `%APPDATA%` expansion, Glob/Grep
  `pattern`+`glob`+`path`, registrable-domain hosts, and MCP exact-allow via `bundle.mcp`.
- **Round 3: API hosts, per-shell quoting, bad provider files.**
  - *API hosts, not registrable domains.* A `hosts` entry may carry a path prefix
    (`gitlab.com/api/`). gh lists `api.github.com`, `uploads.github.com`,
    `github.com/login/oauth/` and `github.com/login/device/` (no longer
    `githubusercontent.com`); glab `gitlab.com/api/` and `gitlab.com/oauth/` (no longer
    `gitlab.com`); gcloud 26 named `*.googleapis.com` API hosts plus
    `storage.googleapis.com/storage/` and `/upload/storage/` (no longer `googleapis.com`).
    So an Azure-only identity runs the helm and nvm install scripts from
    raw.githubusercontent.com, Google Fonts, a public GCS download and `git clone` from
    gitlab.com or github.com; the variable and credential-file rules still catch a credential
    sent with them. `tq providers list` also reports a host under another provider's host.
  - *Shared az.* `azure-devops` also claims `az account`, `az config`, `az version` and the
    bare `az --version`/`az -h`: an azure-devops identity runs `az account show` and
    `az account get-access-token` (for any resource, ARM included) without `az` in scope.
    The subcommand is the first non-option word, so `az --debug vm list` is still `az vm`.
  - *Catalog.* Stripe (`STRIPE_API_KEY`, `import stripe`, the `stripe/stripe-cli` image),
    the Google Cloud client library (`google.cloud`), cloudflared (Cloudflare),
    `git.heroku.com`, `*.supabase.co`, the `digitalocean/doctl` image, `mcp__gh__*`. New
    `match` fields `psmodules` (`Import-Module AWS.Tools.S3`, `AWSPowerShell`, `Az`,
    `GoogleCloud`) and `cmdlets` (`Set-AWSCredential`, `*-S3*`, `*-EC2*`, `*-Az*`, ...).
  - *More routes.* Env reads `Deno.env.get('X')`, `Bun.env.X`, Ruby `ENV["X"]`/`ENV.fetch`,
    Perl `$ENV{X}`; a URL in any string of an MCP tool input (`mcp__fetch__fetch`); the LS
    tool; a quoted path segment (`$env:APPDATA\'GitHub CLI'`); every non-switch word of
    `cmd start` past `/d`, `/affinity` and `/node` values; `Start-Process -FilePath:aws`.
  - *Per-shell masking, fail closed.* The text masker reads PowerShell with PowerShell's
    quoting (backtick escape, doubled quotes, a backslash is ordinary) and the string of a
    nested `pwsh -Command`/`bash -c` with the inner shell's; it blanks nothing on an
    unterminated quote, here-string or `$(`, and never a span that crosses a separator the
    guard tokenizer saw (`guard.SeparatorSpans`).
  - *Bad user provider files.* One that overrides a built-in id leaves the built-in lists in
    force and the hook warns on stderr; one with a new id refuses calls only in a workspace
    whose scope names it (it used to deny every call in every `guard: v2` workspace). A new
    id that claims anything a built-in lists is refused at load. Every message gives the full
    path, the first error and `fix or delete <path>, then run tq providers list`;
    `tq providers list` exits non-zero and `tq doctor` reports `provider-file-invalid`.
- **Round 4: PowerShell command splitting, Google APIs, git transport.**
  - *PowerShell splitting.* The command splitter reads the PowerShell tool's line, a decoded
    `-EncodedCommand` and the string of `pwsh -Command` with PowerShell's quoting as well as
    bash's (`guard.CommandsIn`), so `Write-Host "C:\temp\"; aws ec2 run-instances` and
    ``git commit -m "fix`"" ; az vm list`` show their second command (a regression from
    34613fc). The self-protection rules use the same splitter.
  - *Code is scanned.* A here-string that is run (`iex`, `Invoke-Expression`,
    `[scriptblock]::Create`, `pwsh -Command`, piped to or assigned and run) is split as
    commands, and one that is sent (`Invoke-RestMethod -Uri @'...'@`) is scanned; only a
    here-string written to a file, printed, assigned and never run, or a commit message is
    text. `python - <<EOF` and `python3 <<EOF` have their imports read.
  - *Google APIs.* gcloud lists every `*.googleapis.com` host and the GCS API paths
    (`/storage/`, `/upload/`, `/download/`, `/batch/`), with a new `public` list for
    `fonts.googleapis.com`, `ajax.googleapis.com` and public GCS object reads; so
    `www.googleapis.com`, regional and bucket hosts are denied. URL paths are normalised
    (dot segments, repeated slashes, percent-encoding) before the prefix match.
  - *API hosts for Slack, Sentry, Oracle; installer CDNs.* Their documentation and status
    pages pass; `awscli.amazonaws.com`, `dotnetcli.blob.core.windows.net` and the Azure CLI
    blobs are public.
  - *Git transport.* A new `git_hosts` list: git over ssh to an out-of-scope git host
    (`git@github.com:`, `ssh://`), a `git push` over https to one and `ssh -T git@gitlab.com`
    are denied; an anonymous https clone, fetch, pull or ls-remote is allowed.
  - *MCP inputs.* A URL is judged only in a URL-shaped field (url, uri, endpoint, href,
    link, `*Url`) or a value that is one URL, not in free text.
  - *Provider files.* Unknown keys are errors with `path:line:col` and a did-you-mean;
    every bad file gives the hook warning; the deny names the file once; `tq doctor` names
    the workspaces a bad new id affects, or none.
  - *Stalls.* An empty PowerShell here-string (`@'` then `'@` on the next line) no longer
    panics the splitter (the hook denied every such call as a guard error). New tests run the
    real hook entry point with a decision, and the main goroutine, stuck in a tight loop
    while garbage collections run: the deadline and the watchdog deny on time. Fuzz targets
    `FuzzScopeShell` and `FuzzMaskText` bound every input to 50 ms.

### The signed integrity record (ADR-0004)

- **Trust is a signed record over the files that make the hook run.** `tq allow` writes
  `~/.tentaqles/trust/<ws>.json`: the manifest hash and the sha256 of every covered file
  (the protected manifest and the root marker, `settings.json#tq` (the `hooks`,
  `disableAllHooks`, `enabledPlugins`, `enabledMcpjsonServers`, `disableAllMcpServers`,
  `env`, `statusLine`, `apiKeyHelper`, `otelHeadersHelper`, `awsAuthRefresh`,
  `awsCredentialExport` and `forceLoginMethod` keys of the identity's Claude
  `settings.json`, and any key ending in `Helper` or `Command`; not `permissions`, and a
  theme or model change is no change), `installed_plugins.json#tq` (the tentaqles
  `installPath` and version), every file of the tentaqles plugin folder Claude Code loads
  (from `installPath`, else the highest cached version) except `skills/`, `agents/`,
  `templates/`, `tests/` and `__pycache__`, so its `bin/` is covered, the Codex
  `hooks.json` and the tq-owned block of its `config.toml`, every file under
  `~/.tentaqles/shims/`) under an HMAC-SHA256 signature with the id of the key. The tq
  binary on PATH is not covered. A covered file that is absent is recorded as absent and is
  a change when it appears. Keys are canonical (cleaned, links resolved, forward slashes,
  lower-cased on Windows), so another spelling of `TQ_HOME` verifies; only the roots are
  resolved, so a prompt in a signed workspace costs about 25 ms over a neutral folder on
  Windows. The key is 32 random bytes in the OS keychain (service `tentaqles`, account
  `integrity-key`, made the first time a human signs, never by a check); when the keychain is
  unavailable the key is the file `~/.tentaqles/integrity.key` (0600) and `tq doctor` warns
  `integrity-key-file`. A record signed with another key reports "integrity key changed".
- **Only a human signs.** Every signature (`tq allow`, `tq hooks verify --accept`, `tq deny`,
  `tq add` and `tq setup`, the re-sign after `tq settings render` or `tq bundle sync`,
  `tq migrate manifests`) passes the human-only check of ADR-0003 inside `internal/trust`;
  `tq allow` and `--accept` also confirm. `tq allow` renders the identity files first, for a
  workspace not yet trusted too, and signs once, after.
- **A mismatch is at least as strict as untrusted.** Every command and the hook verify
  through `resolve`, once per command: a changed, added or removed covered file, a record
  whose key or signature does not verify, makes the workspace untrusted with the reason
  naming the first changed file and `tq hooks verify <ws>`. The hook refuses every mutating
  git and every remote command (`git push`, `gh`, cloud CLIs) for it. `tq env` exports no
  identity and prints one fixed line at every prompt, "tq: integrity mismatch: run tq hooks
  verify <ws>" (a workspace never allowed: "tq: untrusted: run tq allow <ws>"; no path ever
  reaches the shell line); `tq run <ws>` and `tq run .` refuse and launch nothing; the
  session-start preamble and `tq doctor` report `integrity` and `untrusted` with every
  change.
- **`tq hooks verify [<ws>|--all] [--accept]`.** Prints the record's state, the key id,
  "plugin tentaqles 0.16.0 -> 0.17.0 (N hook files changed)" when the plugin moved to a new
  version folder, and each change with the old and new hash prefixes; exit 1 on a mismatch.
  `--accept --all` signs again, after one confirmation, the workspaces whose only change is a
  new plugin version whose folder is, byte for byte, the release this tq was built from (the
  commit the release build injects into the binary, `cmd.Commit`, printed by `tq version`,
  read from the tentaqles marketplace clone whose origin is the tentaqles repository on
  GitHub; the clone's own pin is never read; each file's git blob compared, git run with a
  cleaned environment and no replace objects; a clone with alternates, grafts, a shallow
  file or a replace ref is refused as altered), listed first under "Matches the published release tentaqles
  <version> (<sha12>)"; a file that differs, a file the release lacks, a release file
  missing, a dev build of tq, or a clone without tq's commit, and the workspace is "not
  accepted" with the reasons, left for `--accept <ws>`; so is a plugin release newer than
  the tq installed, until tq is upgraded. `tq version` prints the commit. With the `integrity`
  finding the hook's deny says "integrity mismatch: run tq hooks verify <ws>", not
  `tq allow`.
- **`tq deny` leaves a signed tombstone** (`revoked: true`); tq never deletes a record.
- **Older trust markers** (`trust/<hash>`, `trust/<ws>/<hash>`) count until the first
  signature on the machine, then only while the signed snapshot `trust/legacy.json` taken
  then lists them, and never for a workspace that has a signed record: a marker planted later,
  or a deleted record with a marker in its place, is untrusted. The first signature also
  writes `trust/.signed`, never deleted: with it, a missing key or snapshot makes no marker
  count, so deleting the record, the snapshot and the key does not bring a marker's trust
  back. While a marker counts,
  `tq doctor` warns `integrity-record-legacy` and every prompt prints "tq: legacy trust
  record: run tq allow <ws> to sign hooks and manifest". `tq migrate manifests --apply`
  replaces a counting marker by a signed record (`rekey-trust`, journaled with the snapshot,
  restorable) and signs again the record of a workspace it moves only when that record
  verifies in full (`sign-integrity`).
- **Plugin:** `tq_hook.sh` no longer honours `TQ_BIN`; see `plugin/CHANGELOG.md`.

### The manifest moves under `~/.tentaqles` (ADR-0002)

- **Protected manifest location.** `tq add`, `tq onboard` and `tq setup` write the manifest
  to `~/.tentaqles/identities/<ws>/manifest.yaml` (0600 in a 0700 folder) and leave a
  name-only marker at `<root>/.tentaqles.yaml` (`schema: tentaqles-marker-v1`,
  `client: <ws>`). The guard and every command read only the protected copy; a marker
  whose `client` is not the folder name, or whose protected copy is missing, resolves
  neutral with the reason, and so does a root file that is not a regular file (a symlink)
  or a manifest whose `client` is not the folder name: a trusted manifest copied into
  another folder lends it nothing. `tq allow` and `tq deny` work as before; editing the
  marker never changes the hash.
- **Trust is keyed by workspace name and content.** A record is
  `~/.tentaqles/trust/<ws>/<hash>`; the same bytes trusted for one workspace are not
  trusted under another name. A record of an older tq (`trust/<hash>`) still counts for
  its workspace until `tq migrate manifests` rekeys it (`rekey-trust`, journaled);
  `tq deny` removes both forms.
- **`TQ_HOME` is ignored in an agent session.** Under an agent marker tq reads
  `~/.tentaqles` whatever `TQ_HOME` says, so a settings `env` block cannot point the hook at
  a forged state folder; `tq doctor` warns `tq-home-override` when `TQ_HOME` points away
  from the default.
- **Existing identities keep working.** A full manifest still at `<root>/.tentaqles.yaml`
  resolves exactly as before; `tq doctor` warns `manifest-in-root` naming the migration,
  and `manifest-root-stale` when both files exist (the protected copy is the one read).
- **`tq migrate manifests`** (also `tq migrate --steps manifests`; never a default step):
  a dry run by default; `--apply` is human-only, copies each manifest byte for byte (same
  hash, so trust carries over), rewrites the root file as the marker and journals both
  writes; `tq uninstall --restore` puts the manifest back in the root.
- **The state folder is private to the user.** 0700 directories (an existing
  `identities/<ws>/` is tightened when its manifest moves in) and 0600 files on macOS and
  Linux; on Windows `tq init`, `tq add` and `tq migrate manifests --apply` run `icacls`
  on `~/.tentaqles` once, granting the user and SYSTEM, record the outcome in
  `protected-acl.json`, and `tq doctor` reports `protected-acl`; an icacls failure never
  fails a command.
- Plugin: see `plugin/CHANGELOG.md` (the loader resolves the marker; the snapshot guard
  reads `tool_input.file_path`).
### Human-only commands (PRD v2 §8.1, ADR-0003)

- **Three detectors, one confirmation.** Every command that writes under `~/.tentaqles`
  or an identity config dir is human-only: `tq init`, `tq add`, `tq allow`, `tq deny`,
  `tq hooks install|remove`, `tq settings render`, `tq bundle sync|capture`, `tq migrate`,
  `tq uninstall`, `tq setup` (wizard and `--from`), `tq onboard` (wizard and `--from`),
  `tq secret set|rm`, `tq providers add`, `tq login`, `tq run --off` and
  `tq worktrees prune`. Each refuses, with no prompt, when stdin or stdout is not a
  terminal, when a harness or subagent (`claude`, `codex`, `opencode`, `kimi`, or a shell or
  runtime such as `node`, `bun`, `deno`, `npx`, `bash`, `pwsh` or `cmd` whose command line
  names one) is in the process tree, when the chain cannot be followed to a terminal, a
  login shell or the desktop shell (an orphaned child of a dead harness, a reused pid, an
  unreadable command line, a service), or when the environment carries a harness
  marker (the earlier list plus `TENTAQLES_HEADLESS_CHILD`, `CLAUDE_CODE_ENTRYPOINT`,
  `CLAUDE_CODE_SSE_PORT`, `CODEX_THREAD_ID`, `OPENCODE`, `KIMI_CODE`). The error names the
  command and the detector. A human then sees one line saying what is about to be written
  and types `yes` at `Type yes to continue:`; anything else exits 1 with nothing written.
  The logic lives in the new `internal/humanonly` package.
- **Denial log.** Every refusal appends one JSON line to `~/.tentaqles/denials.jsonl`
  (time, kind `human-only`, command, reason, pid, cwd; never an environment value).
- **`--yes` no longer skips the confirmation.** `tq setup --yes` is accepted for older
  scripts and asks anyway; `tq uninstall --restore --yes` and `tq worktrees prune --yes`
  still select the replay or the removal over the listing, then ask.
- **`tq add` no longer scaffolds from a session.** It used to create the folder untrusted;
  it now refuses, and a confirmed human run trusts the folder. `tq onboard --from` and
  `tq setup --from` refuse a session the same way.
- **`tq secret set`** reads the token from the hidden prompt only (a piped stdin is not a
  terminal). **`tq secret status`** is free to run from a session: it prints presence,
  never a value.
- **`tq doctor`** reports a `human-only` check naming the detectors this OS has; a warning
  on an OS whose process-tree walker is stubbed.
- **Real terminals pass.** The process tree proves a human by reaching an anchor: a
  terminal emulator (Windows Terminal, ConEmu, mintty, Terminal.app, iTerm2, GNOME,
  Konsole, kitty, WezTerm and the rest), a login shell, the desktop shell and its helpers,
  a session manager (`systemd --user`, `gnome-session-binary`, `plasma_session`,
  `loginwindow`), `sshd`, `mosh-server`, `tmux`, `screen`, `getty`, WSL's `init`, or an
  editor's integrated terminal (VS Code, Zed, Fleet, the JetBrains IDEs, also as `java`);
  Cursor and Windsurf are not anchors. Above the first anchor a dead, reused, looping or
  unreadable link ends the chain as a human's (the desktop shell's own parent is a dead pid
  on every Windows box). Git Bash under mintty counts as a terminal: its MSYS2 / Cygwin pty
  pipes are recognised and the confirmation is read from them.
- **`tq settings anchors`** lists the accepted terminals; `tq settings anchors add <exe>` and
  `remove <exe>` (human-only, confirmed) keep yours in `~/.tentaqles/anchors.yaml`. A
  refusal that cannot establish a human ancestry names every process it saw and prints the
  exact `add` line for the top-most one that is yours.
- A denial log that cannot be written never lets a command through: the refusal stands and
  the error adds `refusal could not be logged: <err>`.

### Self-protection, the hook's third category (PRD v2 §8.2, ADR-0005)

- **Three categories, one order.** `tq claude-hook pre-tool-use` evaluates the manifest's
  `blocked_commands` and the identity guard, then the secret rules, then self-protection;
  the first category that denies is the one reported. Every deny now reads
  `BLOCKED [<category>/<rule>]: ...` (`identity/wrong-cloud`, `secrets/tq/env-file-read`,
  `self-protection/tq/protected-path`); `--json` adds `category`. `tq/cross-client-mcp` is
  in the identity category and `tq/commit-secret` in secrets; `tq/guard-error` and the
  commit scan's ask are unchanged.
- **`tq/protected-path`.** A session can no longer read or write tq's state
  (`~/.tentaqles`: `config.yaml`, `trust/`, the manifests, the logs, `anchors.yaml`,
  `integrity.key`, `bundles/`, `providers/`, `shims/`; in an identity's Claude or Codex
  home `settings.json`, `settings.local.json`, `hooks.json` and `config.toml`), nor write
  the root marker `.tentaqles.yaml`, the installed tentaqles plugin
  (`plugins/cache/tentaqles/**` and the plugin registration), the tq binary or tq's OS
  keychain entry (`cmdkey`, `security`, `secret-tool` on service `tentaqles`). It covers
  the file tools, Codex's `apply_patch`, `mcp__*` inputs that carry a path, and the shell:
  redirects, copies, moves, deletes, `mkdir`, ACL and mode changes, links, `truncate`,
  `tee`, `Set-Content`, `sed -i`, interpreters' inline code, `[IO.File]::...`, `git -C` and
  a `cd` into the folder. Paths are canonical (symlinks, case on Windows and macOS, `~`,
  `$HOME`, `$env:USERPROFILE`, `%USERPROFILE%`, `$TQ_HOME`, `$CLAUDE_CONFIG_DIR`,
  `$CODEX_HOME`). The marker, the plugin's files and the binary stay readable; Claude's
  memory files and projects in the identity's config home stay writable.
- **`tq/human-only-command`.** The hook denies a shell line that runs a human-only tq verb
  (`tq allow`, `C:\tools\tq.exe hooks install`, `./tq init`, `go run ./cmd/tq deny`, behind
  `sudo`, `env` or a nested shell) before it runs; `tq doctor`, `tq list`, `tq env` and
  the other reads stay allowed.
- **Dry runs are free.** `tq migrate` without `--apply` and `tq settings render --dry-run`
  no longer refuse a session; `--apply` and a real render still do.
- **Every hook deny is logged** to `~/.tentaqles/denials.jsonl` (kind = category, layer
  `hook`, tool, command, rule, reason, workspace, pid, cwd); never an environment value or
  file content, and for a shell command no arguments at all: the `command` field is the
  verb (the first word, after `sudo`/`env` and `NAME=value`/`$env:` assignments are
  stripped) plus the one protected path or token the denial named, never the rest of the
  line — so a pasted password, an inline token, a flag value or a URL never reaches the log.
- **The path rule catches common spellings** of the §8.2 routes (it stays a text filter,
  a speed bump, not a sandbox): glued redirects (`x>path`), `${HOME}` expansion, a
  destructive `find` rooted at or above a protected path, `… | xargs rm`, `eval`/`iex` of
  a literal string, interpreters matched by prefix (`python3.12`, `node20`, `ruby`, `perl`,
  `php`, `deno`, `bun`) — their `-c`/`-e` code, any path argument, `ruby -i`/`perl -i`, and
  a here-doc, stdin or piped body that names a protected path; download, archive, export
  and convert programs (`curl -o`, `wget -O`, `7z -o`, `unzip -d`, `Invoke-WebRequest
  -OutFile`, `Invoke-RestMethod`, `Start-BitsTransfer`, `Expand-Archive`, `Export-* -Path`,
  `certutil`, `fsutil file`, `patch`, `sort -o`); an editor (`vim`, `code`); a recursive
  search (`grep -r`, `rg`, `findstr /s`, `Get-ChildItem -Recurse`, the Grep/Glob tools)
  rooted at the home folder above the state; and a PowerShell `-EncodedCommand` (any
  accepted prefix `-e`…`-enco`, and `-ec`; decoded from base64 UTF-16LE, an undecodable
  one denied). A glob matches a protected path only when it could expand to one (`rm *.log`
  in a workspace is free; `rm .t*`, `rm *` and `~/.tent*/config.yaml` are denied). A path
  built at run time (a `$var`, string concatenation, `Join-Path`, a pipe into a shell, an
  alias, a script on disk, or a write program tq does not list) is not caught; P1.4's
  signed record makes a change it misses visible at the next start.
- Plugin: the PreToolUse matcher gains `Glob` (guarded like `Grep`).
- **The tentaqles marketplace clone is write-protected.**
  `<config dir>/plugins/marketplaces/tentaqles/**`, P1.4's trust anchor for batch accepts,
  joins the plugin cache: writes, deletes and `git -C` into it are denied, reads allowed. A
  recursive search's `/`-prefixed root (`grep -r x /home/me`, an MSYS `/c/Users/me`) is now
  checked; only a `findstr`/`dir` switch (`/s`, `/i`, `/c:text`) is skipped.
- **One marker list.** `internal/markers` holds the harness environment markers;
  `TQ_HOME` is now ignored under any of the twelve (it used to be six).
- Plugin: see `plugin/CHANGELOG.md` (the fallback guard protects `~/.tentaqles` and the
  plugin cache; `client-settings` and `rollback` no longer write the manifest).

### Added
- The build loop, in `.claude/`: this repo's own development process, which doesn't ship. A
  router session dispatches tickets, each ticket is built in its own worktree and judged by
  agents that never saw the build, and a gate keeps the checks fixed once approved. It's
  portable to other projects: see `.claude/README.md` and `.claude/PORTING.md`.

### Fixed
- `tq migrate` on Linux and macOS keeps a moved workspace trusted: the identity step re-signs what it moved, so the git step no longer refuses partway through.
- A provider of your own may not claim a built-in's `git_hosts`, `mcp_hosts` or `mcp_packages`, and an `mcp_packages` entry that names no package (`npm:`) is refused.
- The `tq dotenv run` masking test finds a working bash on Windows (Git for Windows) even
  when the WSL launcher comes first on `PATH`, and skips with a reason when none works.

## [0.9.0] — 2026-11-04

tq is an identity isolator with secrets protection for developers who work across several
clients. One folder per client; the terminal, the agent sessions and the external tools
carry that client's identity and nothing else. The requirements are in `docs/prd.md`, the
trust boundaries in `docs/threat-model.md`, the hook's rule set in ADR-0001.

### The hook

- **One rule set, in every Claude permission mode:** the identity-drift block (a `git push`,
  `gh` or cloud command under the wrong workspace, in an untrusted folder or from a stale
  shell), the five secret rules (`.env` reads, environment dumps, credential-store reads, in
  file tools and in the shell), `tq/commit-secret` and `tq/cross-client-mcp`, with
  `tq/guard-error` as the fail-safe. Every rule decision is allow or block; the only
  confirmation is the fail-safe when the guard itself fails. Nothing a manifest, a global
  file or a repository file adds or removes.
- **Trust is human-only.** `tq allow <ws>` and `tq deny <ws>` refuse an agent session by its
  environment marker and never prompt. `tq add` and `tq setup` scaffold a workspace from a
  session but leave it untrusted and print the `tq allow` line. An edited manifest loses its
  trust until `tq allow` runs again.
- **Launchers.** `tq activate` defines `claude` and `codex` (always `tq run . -- ...`) and
  `claude-off` / `codex-off` (`tq run --off`: the plugin switched off through `--settings`,
  the identity env cleaned, no variable set; refused from an agent session). `TQ_ENABLED=0`
  switches shell activation off and nothing else.
- **Codex.** The identity's `config.toml` block pins `approval_policy = "on-request"` and
  `sandbox_mode = "workspace-write"`; a render replaces only what tq owns.

### Commands and doctor

- `tq doctor` warns `manifest-dropped-block` for a top-level manifest block tq does not read
  (remove it, then `tq allow <ws>`) and `state-dir-unused` for folders under `~/.tentaqles`
  tq does not use; both are warnings, never failures, and tq deletes nothing.
- The status line shows the client and the git email.
- `tq secret` keeps git-host and issue-tracker tokens in the OS keychain and never prints
  them; `tq secrets audit` lists secret-shaped values in agent config files; `tq dotenv run`
  runs a command with a `.env` loaded and every value masked in the output.

### Install and release

- With a logged-in `gh`, `install.ps1` and `install.sh` check the archive's checksum and its
  build provenance (`gh attestation verify`, signer: the release workflow) and refuse to
  install when either fails. Without `gh` they check the checksum only.
- The plugin installs from this repo's root marketplace, pinned to a release tag and its
  commit (`docs/RELEASING.md`). CI runs `go` on three OSes, `govulncheck`,
  `marketplace-pin` and `plugin`; every GitHub Action is pinned by commit sha.

### Upgrading an existing install

See `docs/MIGRATE.md`, "Upgrading an existing install": manifests need nothing, run
`tq settings render --all` once, and `tq doctor` tells you what else to tidy.
