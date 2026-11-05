# ADR-0007: The identity PATH, environment and MCP list built from the provider scope, behind `guard: v2`

Status: proposed
Date: 2026-11-05
Cites: PRD v2 §3.1 (CLIs outside the scope must not resolve; PATH built from scratch), §3.2 (MCP list generated from the manifest, global harness config not loaded), §6.1 (reference scenario, steps 1 to 3), §6.2 (rows PATH, Environment, Harness config), §14 Phase 2 gate ("blocked at layer 2, at layer 3, and at layer 4 in separate tests") and Phase 3 (the shim folder); DISCOVERY.md §1.3 (PATH inherited, never rebuilt), §3 (6.2 layers 1 to 3 missing or partial), §9 (Phase 2, flag `guard: v2`); ADR-0006 (the scope, the match lists, the hook's identity rules); threat model boundaries B1 (the agent and the state folder: agent shells run the prompt hook too) and B2 (the human-only renders that write the mask)

## Context
ADR-0006 gave every identity a provider scope (`manifest.Scope()`) and per-provider match lists,
and made the hook (layer 4) deny an out-of-scope provider in every tool. The PRD asks for three
earlier layers so a wrong-provider action fails before the hook (§6.1): the instruction text
states the scope (1, advisory), `aws` is not on the identity PATH (2), no AWS MCP server is
registered in the identity's harness config (3). Until now the PATH was inherited untouched, the
environment only gained the identity's config homes (an `AWS_PROFILE` from the user's profile
survived), and `tq settings render` never looked at the MCP lists.

The PRD wants the identity PATH "built from scratch from the manifest; shims for in-scope CLIs".
The manifest does not declare the tools a developer needs (git, go, node, python, the editor:
§3.4 stack is informational), and the shim folder is Phase 3. A PATH built from the manifest
alone would break every terminal. Folders are also shared: npm's global folder, scoop's shims,
WinGet's Links, `/usr/local/bin`, `~/.local/bin` hold an out-of-scope CLI beside tools the
identity needs.

The prompt hook runs `tq env` at every prompt and cd (DISCOVERY §1.3), in bash, zsh, fish,
PowerShell and cmd.exe, and Git Bash on Windows spells PATH `/c/Users/...:/usr/bin` while tq, a
Windows program, only sees it converted by the MSYS runtime.

## Decision
Everything below runs only for a trusted workspace whose manifest sets `guard: v2`; without the
flag tq env, tq run and the renders behave as before. The layers read the scope compiled the way
the hook compiles it (`policy.ScopeFor`, the same match lists, the same in-scope-wins rule), so a
name an earlier layer hides is a name the hook denies, and a name the hook allows is never hidden.

**Layer 2, PATH: the inherited PATH filtered, not rebuilt** (`envplan.(*Layer).FilterPath`).
The identity PATH is the PATH as it was before tq, entry by entry, in order:
- an entry whose runnable files are all out-of-scope executables (the AWS CLI's install folder
  for an Azure-only identity: `aws`, `aws_completer`, its DLLs, which do not count) is
  **dropped**: the CLI resolves to nothing, `aws ec2 run-instances` is command-not-found;
- an entry that holds an out-of-scope executable beside anything else runnable is **kept**, and
  the identity's **mask folder** goes first on the PATH;
- every other entry is kept as it is, and so are an empty entry, a relative one, one tq cannot
  read, one it cannot map to an OS path, and on Windows any folder under `%SystemRoot%` (never
  read: no provider CLI installs there and System32 is the largest folder of a Windows PATH).

"Runnable" is, on Windows, an extension of `PATHEXT` or `.ps1`, or an extensionless file that
starts with `#!` (Git Bash runs it); on POSIX a regular file with an execute bit. Only a folder
that holds an out-of-scope name has its other files looked at.

**The mask folder** is `~/.tentaqles/identities/<ws>/path-mask/`: one stub per out-of-scope
executable name, an extensionless POSIX script (bash, zsh, Git Bash, any POSIX system) and on
Windows a `.cmd` beside it (cmd.exe and PowerShell resolve through PATHEXT and never run the
extensionless file; verified on this machine). A stub prints `<name>: command not found (outside
the scope of identity <ws>)` on stderr and exits 127. It is written by the human-only renders
(`tq allow`, `tq settings render`, `tq hooks verify --accept`), never by `tq env`: it is under the
protected tq home, and a file added there would run in place of a real command. `tq env` puts it
on the PATH only when its folder holds exactly the stubs expected (names and sizes); otherwise
it leaves it off, the out-of-scope CLI in the shared folder still resolves, and the prompt
prints `tq: PATH mask missing: ... run tq settings render <ws>`. It is not in the integrity
record: its check is that name-and-size comparison, and a tampered stub can only answer for a
name that is out of scope anyway.

**Restoring.** The PATH before tq is recorded in `__TQ_STATE` like every variable tq changes,
with the PATH tq last wrote (`State.PathSet`). Leaving the folder puts the recorded PATH back
byte for byte when the shell's PATH is still the one tq wrote; a change the user made inside the
identity is kept, as mise keeps it on deactivation (`compute_deactivated_path`): an entry added
in front goes in front, one added elsewhere after, one removed is removed (`MergePath`). Moving
from one identity straight to another builds the second PATH from the recorded one, never from
the first identity's PATH. `envplan.Clean` (an off launch, a neutral launch) restores it the same
way.

**Spelling.** PowerShell, cmd.exe and any POSIX shell get the OS spelling. On Windows the bash
and zsh hooks pass their own spelling for the one call (`MSYS2_ENV_CONV_EXCL=__TQ_SH_PATH
__TQ_SH_PATH="$PATH" tq env`: MSYS converts any path-like variable a native program receives,
the exclusion keeps it as written; verified); tq pairs it entry by entry with the converted OS
PATH it receives, so `/usr/bin` is read as `C:\Program Files\Git\usr\bin`, and answers in the
shell's spelling. A bash hook from before this change, and fish on Windows, get no PATH change.
fish gets PATH as a list (`set -gx PATH 'a' 'b'`). cmd.exe gets no PATH change when the PATH holds
`%`, `!` or `"`, which it cannot quote, rather than the whole line refused.

**Cost.** The folders are read on entering the folder and after a PATH change only: a prompt
whose PATH is still the one tq wrote compares two strings (and reads the mask folder when it is
on the PATH). Measured in-process on the Windows dev box (59 PATH entries): compiling the scope
0.1 ms, a prompt in the folder 0.24 ms, the full pass on entering 11.7 ms (System32 skipped; 20
to 30 ms with it).

**Layer 2, environment** (`envplan.(*Layer).UnsetNames`). The match lists' `env` entries of the
providers outside the scope that no in-scope provider claims are unset in the identity terminal
(`tq env`) and in every launch (`envplan.Environ`: `tq run`, the editor launch), and restored on
leaving: a `TF_TOKEN_*` prefix entry unsets every variable it names, a variable tq sets for the
identity is never unset. For the Azure-only identity that is the AWS, GCP, GitHub, Cloudflare,
Vercel, ... credentials, profiles and config homes (`AWS_PROFILE`, `AWS_ACCESS_KEY_ID`,
`GOOGLE_APPLICATION_CREDENTIALS`, `GITHUB_TOKEN`, `CLOUDFLARE_API_TOKEN`, `VERCEL_TOKEN`, ...);
`AZURE_CONFIG_DIR` (az's and azure-devops's) and every unrelated variable stay. The in-scope
config homes point at the identity's folders as before. `tq run <ws> -- <cmd>` resolves `<cmd>`
on the launch's PATH, not on tq's own, so `tq run company-x -- aws` from outside the folder
fails with command-not-found and launches nothing.

**Layer 3, harness config** (`internal/mcpscope`). `tq settings render` (and `tq allow`, `tq
hooks verify --accept`) removes from the identity's Claude home `.claude.json` (the top-level
`mcpServers` and every `projects[<path>].mcpServers`) and from its Codex home `config.toml`
(`[mcp_servers.<name>]` tables with their sub-tables, and one-line inline tables under
`[mcp_servers]`) **every server tq can tell is out of scope**: a name token an out-of-scope
provider's `mcp` list holds and no in-scope provider's (the hook's `out-of-scope-mcp` rule), a
`url` on an out-of-scope API host, or a `command` that is an out-of-scope CLI. A server that
matches no provider (a database, a docs server, the user's own tool) is never touched. A removed
entry is appended, as it was, to `mcp-out-of-scope.json` beside the config, so a human can put
it back after adding the provider to `scope:`; the rest of both files is left as it was (JSON
numbers as written, the TOML byte for byte outside the removed tables). `tq bundle sync` does not
write a bundle server that is out of scope. Neither file is in the integrity record, so the
render does not re-sign.

**Layer 1, advisory.** The session-start preamble (Claude's and Codex's, the "generated
instruction text" of the identity) carries one line: `Scope: az, azure-devops, claude.
Identity company-x may use these providers only: ...; if a task needs one, stop and tell the
developer.` Not relied on.

## Consequences
- The Phase 2 gate: `TestEnv_ScopeReferenceScenario_PATH` (layer 2, through `tq env` in
  PowerShell's and Git Bash's spelling, and `tq run`), `TestSettingsRender_ScopeReferenceScenario_MCP`
  (layer 3), `TestPreToolUse_ScopeReferenceScenario` (layer 4, ADR-0006), plus
  `TestSessionStart_ScopeLine` (layer 1). Unit tables: `TestPathLayer_*`, `TestPathSyntax_MSYS`,
  `TestMergePath`, `TestLayer_EnterAndLeaveRestoresExactly` (both separators),
  `TestLayer_UserPathChangeSurvivesLeave`, `TestLayer_SwitchIdentityRebuildsFromOriginal`,
  `TestEnviron_V2Layer`, `TestLayer_UnsetNames`, `TestJudge`, `TestFilterClaude`,
  `TestFilterCodex`. They join CLAUDE.md "Must not trade off".
- **It is a PATH filter, not an isolation.** Not caught by layer 2, by design: an absolute path
  (`C:\Program Files\Amazon\AWSCLIV2\aws.exe`), `python -m awscli`, a CLI copied or renamed, a
  shell alias or function, a CLI installed into a shared folder after entering the folder (masked
  from the next cd or PATH change: the fast path does not re-read folders), a folder added to PATH
  by a program the shell runs, `npx`/`uvx` fetching a CLI. Layer 4 denies what the hook sees of
  those. A shared folder's out-of-scope CLI resolves to tq's stub, not to nothing: `command -v aws`
  names the mask.
- **Everyday work.** Dropping is limited to folders whose runnable files are all out-of-scope
  CLIs; a folder tq is unsure of is kept. A false positive is a project tool whose name is a
  catalog executable (`atlas`, `render`, `fly`, `jira`, `vc`): masked, exactly as the hook already
  denies it by name; the developer adds the provider to `scope:` or renames nothing and runs it by
  path.
- **MCP sources tq does not render**, all of them still judged by the hook by server name: a
  project's `.mcp.json` (in the identity's folder, the repo's own), a plugin's MCP servers (the
  identity's enabled plugins; a plugins folder shared between identities by a junction carries
  them across), claude.ai connectors of the logged-in account, `managed-mcp.json` (machine-wide),
  `--mcp-config` on the command line, Codex's project `.codex/config.toml`, and an identity whose
  Claude is not isolated (no `claude` identity: `CLAUDE_CONFIG_DIR` unset, so the global
  `~/.claude.json` loads). A server whose name and entry spell no provider (`my-cloud` running
  `uvx awslabs.aws-api-mcp-server`) is not removed; its tool calls are judged by the hook by
  server name only, so it also passes layer 4 (the hook's text filter limit of ADR-0006).
- `.claude.json` is rewritten by Claude Code while it runs: a render while a session uses the
  config home may be undone; the render warns and a later render repeats the removal.
- Changes `internal/envplan`, `internal/shell` (the bash and zsh hooks), `internal/policy` (read
  accessors), `internal/bundle` and adds `internal/mcpscope`; the mask folder is written across boundary B2 and read under B1 of the threat model. Phase 3 replaces the mask folder with the shim folder on a PATH built
  from the manifest; the mask's limits then go away with it.

## Alternatives considered
| Option | Why not |
|---|---|
| PATH built from scratch from the manifest (the PRD's wording) | The manifest declares no runtimes or tools; every terminal would lose git, go, node and the editor. Phase 3's shim folder is what makes "from scratch" possible; reported as a PRD conflict |
| A per-identity view folder of links to everything in a shared folder but the out-of-scope names | Windows symlinks need Developer Mode or admin; a hard link to an npm `.cmd` or a scoop shim breaks its relative lookups (`%~dp0`, the `.shim` file); thousands of links to keep in step with the folders |
| Shell functions or aliases named after the out-of-scope CLIs | Shell-only: a child process (the harness's Bash tool, make, a script) does not see them; fish, cmd.exe and PowerShell each need their own |
| Mask folder always first on the PATH, folders never dropped | Simpler, but `aws` would always resolve to a stub; the §6.1 scenario asks for command-not-found, and the CLI's own install folder can go without risk |
| Re-read every PATH folder at every prompt | 12 to 30 ms per prompt on Windows; a CLI installed while inside the folder is rare and layer 4 still denies it |
| Mask folder written by `tq env` when missing, or under the user cache folder | `tq env` also runs in agent shells and every tq write under the tq home is human-only; a folder an agent can write, first on the human's PATH, would let it replace `git` |
| Mask folder in the integrity record | A file hash per stub at every prompt; the name-and-size check catches an added file, which is the dangerous change |
| Remove every MCP server not generated from the bundle | Deletes the user's own servers that belong to no provider; the PRD's aim is that out-of-scope tools do not appear, which needs only what tq can attribute |

Records are never edited after acceptance. A changed mind is a new record that
supersedes this one.
