# Changelog

Notable changes to the `tentaqles` CLI and the docs of this repository. The Claude Code plugin keeps its own log in
[`plugin/CHANGELOG.md`](plugin/CHANGELOG.md).

## [Unreleased]

- **A success that shows a failure is stored as `hidden-fail`.** A successful call whose result has an error flag, a test runner's failing summary in shell output, or a failing line behind a pipe that hides the exit code is written to the memory's event log as `hidden-fail`, with its full input, the failing part of the output as `error` and an `fp`. Commands that only read (`cat`, `git log` and the like) are never judged by their output text.

- **A successful call is recorded too, minimally.** After a tool call succeeds, the memory's event log gets one `ok` line
  with the tool, the time and the part of the input that identifies the call (a shell command, a file path, a search
  pattern, a URL, or the top-level values of an MCP tool cut to 200 characters). The output is never stored. A command
  that only went to the background writes nothing; a success after a failure of the same tool in the same session keeps
  its full input.

- **A repeated failed call is stored once.** The same payload sent twice leaves one line in the memory's event log. The same failure again within five minutes in the same session leaves a short reference line (no input, no error text) pointing at the first. `tentaqles lobe events` shows a reference line with its first line.
- **Every failure line carries a fingerprint: `fp`, `tentaqles lobe fingerprint`, and the count in `lobe status`.** Each failed
  call's line in the memory's event log gets `fp`, a short id of "the same error": errors that differ only in paths, numbers,
  ids or quoted values share one, and the same text from another kind of tool does not. `tentaqles lobe fingerprint --tool <name>`
  prints the fingerprint and the template of an error text read from stdin, with no store or client, in any folder and inside a
  session. `tentaqles lobe status` counts the distinct fingerprints still in the log (`fingerprints` in `--json`, inside `events`).
- **See the memory and switch it off: `tentaqles lobe status`, `lobe events`, `lobe settings` and `lobe set`.** `lobe status`
  prints the client, the store's state, the nine settings, the event counts and whether the plugin wires the memory's hooks.
  `lobe events` prints the newest 20 stored lines. `lobe settings` prints the nine lobe settings and where each comes from.
  `lobe set capture off` stops recording from the next tool call, and `lobe set capture on` starts it again; `lobe set`
  answers only to a person, after a typed yes. With capture off no event is written. A `stop` that runs while capture is
  off moves the session's cursor to the end of the transcript (reading at most its last 1 MiB, parsing nothing) when the
  client's store exists, so a call rejected while capture was off is not read later. Not covered: a call rejected in a
  turn whose `stop` runs after capture is back on, and a client with no store yet (not rendered since this version).
  `lobe set` writes only the lobe lines of a manifest tentaqles wrote; a hand-formatted manifest is re-indented to 4 spaces. `tentaqles doctor` reports the memory's eight findings with the client's
  name (a broken store, a missing or wrong hook, a wrong setting, failing hook calls). A render now creates each client's
  memory store, so `tentaqles settings render` and `tentaqles reach` leave it in place.
- **A call Claude Code rejected before it ran is recorded too.** An edit whose text is not in the file fails Claude
  Code's own check and fires no tool hook. The plugin's new `Stop` hook runs `tentaqles lobe-hook claude stop` when
  the agent ends its turn; it reads the new part of the session's transcript, only inside the session's own login
  folder, and adds each rejected call to the client's log, once. Subagent transcripts are not read yet.
- **The memory records a failed call of every tool**, not only shell commands: a failed edit, web fetch or MCP
  call adds its line to the client's event log, with the whole error and no exit code. A call the person
  interrupted or refused is not recorded, nor is a command of the memory itself (`tentaqles lobe ...`), and a
  `grep` that found nothing is recorded as a success. A linked git worktree is keyed by its main repository, read
  from its `.git` file without running git, so a repository and its worktrees share one memory.
- **The memory begins: a failed shell command in Claude Code is recorded.** The plugin gains a hook on
  `PostToolUseFailure` that runs `tentaqles lobe-hook claude post-tool-use-failure`. When a `Bash` or
  `PowerShell` call fails in a client's folder, and the session is logged in as that client, one line is
  added to that client's event log, `~/.tentaqles/identities/<client>/lobe/events/events-<day>.jsonl`:
  the command and the error with every secret replaced by a marker, the exit code, the repository and the
  folder. Nothing is printed into the session. The hook fails open: on any problem it exits 0 with no
  output, under a deadline and a watchdog. A session logged in as another client, a folder that is no
  client, or `TQ_LOBE_NO_HOOKS=1` writes nothing. Other tools, Codex, duplicates, expiry and the use of
  the log come in the next features.
- **`tentaqles lobe selftest`** proves the memory's store on this machine: twenty writers at once, a
  thousand replacements under a reader, and the repository key.
- **`tentaqles version --notices`** prints the `NOTICE` file and the license texts of the projects whose
  logic tentaqles rebuilds (Trail-code, agentmemory, Graphify, Graft). The repository root now has the MIT
  `LICENSE`, `NOTICE` and `third_party/licenses/`, which ship in every release archive.
- `tentaqles drop` names the client's memory among what it deletes (it was always deleted with the
  identity folder).
- **`tentaqles lobe sandbox <folder>`** builds a throwaway tentaqles home with two test clients, `acme` and
  `globex`, for test agents, benchmarks and CI. It refuses, before it writes anything, unless `TQ_HOME`, `HOME`
  and `USERPROFILE` all point inside the folder, so it cannot touch a real home. It is the first command of the
  new `tentaqles lobe` group. A provider can no longer have the id `lobe`: that name is kept for a client's
  memory folder.
- A copy of a client's folder under another clients folder no longer gets that client's identity. tentaqles now
  records the folder each client was created in (`clients:` in `~/.tentaqles/config.yaml`), and only that folder
  is the client. A copy with the client's name and marker gets no identity, the hook denies shell commands
  there, and `tentaqles list` shows the client once. Before, the copy resolved to the client and
  `tentaqles list` showed it twice.
- **`tentaqles bind`** binds the client in the current folder to that folder: run it after moving a client to
  another clients folder. It shows the recorded folder and the new one, waits for `yes`, and refuses inside a
  Claude Code session.
- Clients created before this change keep working. `tentaqles settings render` records their folder, and
  `tentaqles doctor` flags a client whose folder is not recorded yet (`client-folder-unrecorded`) and a folder
  that is refused (`client-folder-refused`).
- `tentaqles drop` removes the record, and refuses to drop a client when pointed at a copy of its folder.
  `tentaqles reach` in such a copy names the client's real folder and no longer says it is already a client.

## [0.14.1] - 2026-11-09

- The Windows installer no longer breaks when a tentaqles process is running, such as the launcher of an open Claude
  session. It unpacks to a staging folder, moves each existing file aside, and puts them all back if a step fails, so
  an upgrade can't leave you without a working `tq`. A copy still in use is removed by the next install.

## [0.14.0] - 2026-11-09

- A child shell started from an activated client folder (an agent's `powershell -Command`, a `bash -lc`) no longer
  prints `tq: shell state lost` on every command when it is in the same folder and already has that client's
  identity: nothing needs restoring there, so the notice is silent. The notice still shows when the shell is elsewhere.
- **`tentaqles codex-hook`** is the OpenAI Codex CLI counterpart of `claude-hook`: `pre-tool-use` runs the same identity
  check and answers Codex's deny object (stdout, exit 0), and every error path denies the same way; `session-start`
  prints the same preamble. The two share one decision path; `claude-hook` is unchanged.
- A codex session in a client folder starts only with the tentaqles hooks enabled and trusted in the client's Codex
  config, and refuses `--disable hooks` and `-c features.hooks=false`. `tentaqles settings render` writes and trusts those
  hooks (and the client's language) when codex is in the client's scope; `tentaqles doctor` reports a client whose
  hooks are missing, off, untrusted or unreadable, and `codex` in a client folder runs through `tentaqles run` in every
  shell.
- **An AI coding agent besides Claude Code** is a category of `tentaqles add`, `remove` and the reach form: `add codex`
  (or `add agent codex`) puts a catalog agent in the client's scope, and gives codex its own Codex home.

## [0.13.2] - 2026-11-08

- **Docs rebuilt.** The README says what tentaqles is for, shows it working, and gets you to a first client in three
  steps. `docs/` has an index, a getting-started tutorial, a how-it-works page, troubleshooting for every doctor
  finding, and references for commands, the manifest, providers and the Claude Code hook, all checked against the
  code. Tests fail when a command has no section in the reference, a docs link is broken, or a page is missing from
  the index.
- The hook compares `gh` with `git.expected_user` only for a GitHub client: on Azure DevOps that key is an email,
  and every `gh` command was blocked.
- The session-start `Git:` line no longer prints an empty user.
- `tentaqles doctor` drops the Python-alias check (the plugin runs no Python), and `identity-dir-missing` names the
  folder to create.

## [0.13.1] - 2026-11-08

- A manifest holds only what tentaqles reads: `git.host` and the per-identity `share_capabilities`,
  `expected_account`, `expected_subscription` and `profile` are gone, and a new client's manifest has no empty keys.

## [0.13.0] - 2026-11-08

- **Each client has a language.** `tentaqles reach` asks for it after the email, from a list that starts on English
  (United States), then every language by name; typing filters the list (`por` leaves the Portuguese ones) and the
  arrows move through it. The manifest stores the tag (`language: pt-BR`). The client's Claude `settings.json` gets
  Claude Code's `language` setting, and each session starts with the instruction to write the client's reports,
  documents, emails and messages in that language. `tentaqles add language pt-BR` (or a name: `brasil`,
  `japanese`) changes it, reach's menu offers it, and `tentaqles list` shows it. A manifest without `language` is
  English (United States).

## [0.12.0] - 2026-11-08

Tentaqles is an identity isolator for developers who work for several clients: one folder per client, and a terminal
opened in it, or in any subfolder, carries that client's identity and nothing else.

- **`tentaqles reach [<path>]`** makes a folder a client: a banner, a form (email, git host, clouds, team, data and
  task tools, email provider, browsers and profiles), and after a typed `yes` the client's manifest under
  `~/.tentaqles/identities/<client>/`, a marker in the folder, the folder's git identity, the client's Claude
  settings, PATH mask and browser shims, the tentaqles plugin in the client's Claude config, and the activation
  block in your shell profiles. In a folder that already is a client, reach changes its providers, browser or email
  through the same questions.
- **`tentaqles add` and `tentaqles remove`** change one part of a client's setup without the menu:
  `tentaqles add git azure-devops`, `tentaqles add cloud aws`, `tentaqles remove gh`, `tentaqles add browser chrome
  "Profile 2"`. A value alone finds its category; with no value, the category's part of the form opens.
- **`tentaqles drop <client>`** undoes a client as if it never existed: its identity folder (manifest, Claude config,
  logins), its browser shims, the marker and git identity in its folder, its git rule, and the clients folder's
  registration when no other client is left there. Your files stay; an emptied folder is deleted.
- **A client's terminal**, in its folder and every subfolder, carries `CLAUDE_CONFIG_DIR` (its own Claude account),
  each CLI's config home, `BROWSER`, and a PATH without the CLIs of providers outside its scope, whose variables are
  unset. Leaving the folder restores what was there.
- **`tentaqles run`** runs a command with a client's environment, refuses a Claude session without the plugin, and
  from inside a session runs only the folder's client.
- **The plugin's hook** runs on Bash and PowerShell calls and blocks a git, gh or cloud command under the wrong
  identity: a manifest's blocked commands, the wrong cloud, a remote command outside every client folder, a stale
  shell, the wrong `CLAUDE_CONFIG_DIR`, git email or gh user.
- `tentaqles login`, `open`, `settings render`, `doctor`, `list`, `providers`, `hooks` and `version` complete the
  set; see [docs/COMMANDS.md](docs/COMMANDS.md).
- Every command is `tentaqles <command>`, with `tq <command>` as its alias: the release ships both binaries, the same
  bytes.
- Commands that change a client (`reach`, `add`, `remove`, `drop`) wait for a typed confirmation and refuse to run
  from inside Claude Code.
