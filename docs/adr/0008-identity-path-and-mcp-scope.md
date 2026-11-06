# ADR-0008: The identity PATH, environment and MCP list built from the provider scope, behind `guard: v2`

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
executable name, in every spelling a shell resolves from PATH, so `aws.exe`, `vercel.ps1`,
`aws.bat` typed out, or an edited `PATHEXT`, land on the stub too: an extensionless POSIX script
(bash, zsh, Git Bash, any POSIX system); on Windows `name.exe` and `name.com`, hard links to
`tq-stub.exe` (itself a hard link to the tq binary, a copy when the two are on different
volumes; no admin needed), which answers as a stub when it runs from a mask folder
(`envplan.StubMain`, the first thing `tq` does); and `name.bat`, `name.cmd`, `name.ps1`,
`name.vbs`, `name.js`. Being a program, the `.com`/`.exe` stub (first in `PATHEXT`) returns to a
batch file that runs `aws` without `call`, which a `.cmd` stub would end. cmd.exe and PowerShell
never run the extensionless file (verified). The names are the out-of-scope executables no
in-scope provider claims at all (wholesale or by a subcommand: `az` stays for an azure-devops
identity), minus the ambiguous words `providers.AmbiguousExecutable` lists (`cn`, `tea`,
`snow`, `render`, `vc`, `fly`, `atlas`, `copilot`, `goose`), which are neither dropped nor
masked because another program may own the word; the provider's unambiguous CLI (`flyctl`) still
is. A stub prints `<name>: command not found (outside
the scope of identity <ws>)` on stderr and exits 127. It is written by the human-only renders
(`tq allow`, `tq settings render`, `tq hooks verify --accept`), never by `tq env`: it is under the
protected tq home, and a file added there would run in place of a real command. `tq env` puts it
on the PATH only when its folder holds exactly the stubs expected (names and sizes); otherwise
it leaves it off, the out-of-scope CLI in the shared folder still resolves, and the prompt
prints `tq: PATH mask missing: ... run tq settings render <ws>`. It is not in the integrity
record: its check is that name-and-size comparison, and a tampered stub can only answer for a
name that is out of scope anyway. The check runs at every prompt the mask is wanted (one
directory read): a mask taken off because it changed goes back on as soon as it is as tq wrote
it, and the notice repeats at every prompt while it is missing (`State.MaskWanted`).

**Which folders.** A quoted entry (`"C:\Program Files\x"`) is read as its folder; a network
folder (`\\server\share`) is never read, nor anything under `%SystemRoot%`.

**Restoring.** The PATH before tq is recorded in `__TQ_STATE` like every variable tq changes,
with the PATH tq last wrote (`State.PathSet`). Leaving the folder puts the recorded PATH back
byte for byte when the shell's PATH is still the one tq wrote; a change the user made inside the
identity is kept, as mise keeps it on deactivation (`compute_deactivated_path`): an entry added
in front goes in front, one added elsewhere after, one removed is removed (`MergePath`). Moving
from one identity straight to another builds the second PATH from the recorded one, never from
the first identity's PATH. `envplan.Clean` (an off launch, a neutral launch) restores it the same
way. The state holds one PATH, not two: the PATH tq wrote is spelled as the indices of the
recorded entries it dropped plus the mask entry it put first (the full PATH only when it is not
of that shape), and the whole state is zlib-compressed JSON in base64 URL-safe, as direnv's
gzenv does (prefix `z1.`; the plain base64 JSON of round 1 still decodes; inflation is capped at
1 MiB). A 9 KB PATH records in under 2 KB.

**Spelling.** PowerShell, cmd.exe and any POSIX shell get the OS spelling. On Windows the bash
and zsh hooks pass their own spelling for the one call (`MSYS2_ENV_CONV_EXCL=__TQ_SH_PATH
__TQ_SH_PATH="$PATH" tq env`: MSYS converts any path-like variable a native program receives,
the exclusion keeps it as written; verified); tq pairs it entry by entry with the converted OS
PATH it receives, so `/usr/bin` is read as `C:\Program Files\Git\usr\bin`, and answers in the
shell's spelling. A bash hook from before this change, and fish on Windows, get no PATH change.
fish gets PATH as a list (`set -gx PATH 'a' 'b'`).

**cmd.exe.** The hook runs each `set "K=V"` line tq prints from a `for /f` variable, whose text
cmd.exe does not parse again (verified): `%` is not expanded a second time, and `&`, `|`, `<`,
`>`, `^` and quotes stay as written, so every PATH character arrives. With delayed expansion on
(the hook tests `"!!"==""` and passes `--cmd-delayed`) tq escapes `!` and, on such a line, `^`.
The one thing cmd.exe cannot do is read a line over 8191 characters (its `for /f` and its own
parser both stop there, verified): an identity PATH that long is left as it is, the rest of the
identity (variables, state) still applies, and the prompt says
`tq: PATH over cmd.exe's 8191-character line ...` every time. Never silently.

**Cost.** The folders are read on entering the folder and after a PATH change only: a prompt
whose PATH is still the one tq wrote compares two strings (and reads the mask folder when it is
on the PATH). Measured in-process on the Windows dev box (59 PATH entries): compiling the scope
0.1 ms, a prompt in the folder 0.24 ms, the full pass on entering 11.7 ms (System32 skipped; 20
to 30 ms with it).

**Layer 2, environment** (`envplan.(*Layer).UnsetNames`). The match lists' `env` entries of the
providers outside the scope that no in-scope provider claims are unset in the identity terminal
(`tq env`) and in every launch (`envplan.Environ`: `tq run`, the editor launch), and restored on
leaving: a prefix entry unsets every variable it names, a variable tq sets for the identity is
never unset, and a generic tool's variables are never unset (`providers.GenericTool`: a provider
whose CLI command its match lists do not claim, wholesale or by a subcommand: docker, helm,
kubectl, terraform, pulumi, npm, psql, dbt; `NPM_TOKEN` for a private registry and `TF_TOKEN_*`
for Terraform Cloud belong to the project). That helper is the one place the rule lives, for the
hook to read too when the two branches settle the generic-tool question. For the Azure-only identity that is the AWS, GCP, GitHub, Cloudflare,
Vercel, ... credentials, profiles and config homes (`AWS_PROFILE`, `AWS_ACCESS_KEY_ID`,
`GOOGLE_APPLICATION_CREDENTIALS`, `GITHUB_TOKEN`, `CLOUDFLARE_API_TOKEN`, `VERCEL_TOKEN`, ...);
`AZURE_CONFIG_DIR` (az's and azure-devops's) and every unrelated variable stay. The in-scope
config homes point at the identity's folders as before. `tq run <ws> -- <cmd>` resolves `<cmd>`
on the launch's PATH, not on tq's own, and refuses an out-of-scope name however it is spelled
(`aws`, `AWS.EXE`, its install path), so `tq run company-x -- aws` from outside the folder fails
with command-not-found and launches nothing.

**Layer 3, harness config** (`internal/mcpscope`). `tq settings render` (and `tq allow`, `tq
hooks verify --accept`) removes from the identity's Claude home `.claude.json` (the top-level
`mcpServers` and every `projects[<path>].mcpServers`) and from its Codex home `config.toml`
(`[mcp_servers.<name>]` tables with their sub-tables, and one-line inline tables under
`[mcp_servers]`) **every server tq can attribute to a provider outside the scope**
(`policy.(*Scope).OutOfScopeMCPEntry`): by its name (the hook's `out-of-scope-mcp` rule of
ADR-0006: a token of an out-of-scope provider's `mcp` list, the bundle's exact names allowed), by
the package or image its `command` or `args` run (a new match list, `mcp_packages`, of prefixes:
`awslabs.`, `@aws/`, `@aws-sdk/`, `public.ecr.aws/awslabs/` for aws,
`@modelcontextprotocol/server-github` and `ghcr.io/github/github-mcp-server` for gh,
`@google-cloud/` for gcloud, ...; a script under `node_modules/<pkg>/`; a docker or podman image
of the `images` list), by a `command` that is an out-of-scope CLI, by an out-of-scope provider's
variable among its `env` keys (`AWS_PROFILE`), or by its `url`/`serverUrl` or a URL among its args
on an out-of-scope API host. An in-scope provider's claim on the same fact wins. gh's `mcp` list
gains `gh`. A server that matches no provider was kept in rounds 1 to 4; the hybrid list of
2026-11-05 (below) keeps it only when it is general-purpose or in `mcp.allow`. A removed entry is appended, as it was, to `mcp-out-of-scope.json` beside the
config, so a human can put it back after adding the provider to `scope:`; a file of that name tq
cannot read is moved aside to `mcp-out-of-scope.json.corrupt-<time>`, never overwritten.
`.claude.json` is edited member by member (a small JSON scanner finds each server's byte span),
so Claude Code's indentation, key order, numbers and escapes stay byte for byte; the TOML is
edited line by line, and the comments and blank lines before the next table stay with it. `tq
bundle sync` does not write a bundle server that is out of scope. Neither file is in the
integrity record, so the render does not re-sign.

**The hook reads the same entries** (`mcpscope.Entries`, `ScopeInputs.MCPEntries`): an
`mcp__<server>__*` call is judged by its name, then by that server's entry in the identity's
`.claude.json` (user scope and every project holding the cwd), the `.mcp.json` files from the cwd
up to the workspace root, and the Codex `config.toml`, read once per hook run and only for an MCP
call. A server renamed to a word of no provider (`cloud` running awslabs' package) is denied at
layer 4 as at layer 3. The scope the layers compile and the hook's share one catalog definition
(`policy.CatalogInputs`: the merged catalog, the embedded-only claims, the bundle's exact names).

**Round 3 (critic round 2).** A server name defined in several sources (user scope, a
project's local scope, a `.mcp.json`, a plugin, Codex) is judged by every entry: the hook
denies the name when any of them is out of scope, so precedence between the sources (Claude
Code's local, then project, then user) cannot hide one; projects are read in path order. The
entries render removed (`mcp-out-of-scope.json`) stay among them: a session started before the
render still has them loaded. The installed plugins' servers (`plugins/installed_plugins.json` →
each `installPath`'s `.mcp.json` and `.claude-plugin/plugin.json`) are read too, named
`plugin_<plugin>_<server>` as Claude Code names their tools; render reports an out-of-scope one
and never edits a plugin's files. Attribution gained: a vendor's remote MCP endpoints as a new
list `mcp_hosts` (`mcp.vercel.com`, `mcp.supabase.com`, `mcp.cloudflare.com` and its
subdomains, `api.githubcopilot.com`), kept apart from `hosts` (the hook's network rules); more
`mcp_packages` (`public.ecr.aws/awslabs-mcp/`, `mcp-proxy-for-aws`, `aws-api-mcp-server`,
`github.com/awslabs/`, `github.com/github/github-mcp-server`, the `mcp/<vendor>` Docker Hub
images, `us-docker.pkg.dev/cloud-run/mcp/`), compared PyPI-style (`-`, `_`, `.` alike) and after
removing a `git+`/URL scheme; gh's `images` gain `mcp/github`; gh's `env` gains
`GITHUB_PERSONAL_ACCESS_TOKEN`. The words judged are the command and its args with a
`--flag=value` split, a shell-wrapped string (`powershell -c "uvx awslabs..."`, `bash -c`)
split into words, each path also by its file name (`...\awslabs.aws-api-mcp-server.exe`,
`...\npm\aws-mcp.cmd`); docker `-e`/`--env NAME[=...]` names are variables; `--prebuilt
<toolset>` is judged by the `mcp` tokens. Every server of the critic's fixture is removed at
render and denied at the hook, and the kept set (playwright, context7, memory, postgres, fetch,
git, filesystem, the Azure servers) stays (`TestOutOfScopeMCPEntry_VendorFixtures`).

**A lost state fails closed.** `TQ_WS` set with `__TQ_STATE` missing, empty, truncated, not
base64 or not JSON (the decoder now reports it) is a shell tq cannot restore: every variable tq
set for an identity (a workspace marker, the state, or a catalog config home whose value is
under the tq identities folder; a user's own value elsewhere stays) and every identity's mask
folder on PATH go, everything else stays, the folder's identity is applied afresh, and the
prompt says `tq: shell state lost: ... open a new terminal to get them back`. A state that
decodes but does not record what tq set (an older or hand-written one) is swept the same way
(`Unrecorded`: the variables it does not record are hidden, then removed). (Round 3 excused a missing state under a harness marker; round 4 removed that exception.) A launch (`envplan.Environ`) now carries
its own `__TQ_STATE`, so a shell started in it can leave the identity.

**cmd.exe's budget, measured.** A line tq prints runs through the hook when it is at most 8192
bytes, delayed expansion on or off; at 8193 the line and every one after it are dropped
(`TestEmit_Cmd_LineBudget`): `CmdLineMax = 8192`.

**The stub is the tq binary** (`DefaultStubSource`): the running program when it is tq, else
the tq on PATH, never the program that called the render; with neither the render reports it
and writes no links (`TestPathLayer_StubIsTheTQBinary`).

**Round 4 (QA round 3).** Every catalog vendor that publishes an official MCP server carries
it as data (`TestMCPVendorsHaveMCPData` lists them: stripe, sentry, linear, notion, jira,
neon, render, netlify, heroku, mongodb-atlas, railway, firebase, asana, pulumi, terraform,
dbt, snowflake, twilio, hostinger, doctl, gitea, glab and the earlier ones). Every word of an
entry is also checked as an out-of-scope CLI or Python module with its `@version` stripped, so
a package runner cannot start one (`npx -y vercel@latest mcp`, `uvx --with awscli python -m
awscli`, `-p wrangler`). A vendor's credential variable referenced anywhere in the entry
(`${VERCEL_TOKEN}` in a header, an env value, an arg; docker `-eNAME`; Codex's
`bearer_token_env_var`, `env_vars`, `env_http_headers`), a header named for the vendor
(`X-Vercel-Token`) and an `--env-file` named for it (`aws.env`) attribute the server. A
plugin's `mcpServers` is read in every shape the plugin manifest takes (an object, a path, an
array of paths and objects). A missing state is lost in a harness shell too: only values under
the identities folder go and the folder's identity is applied again. The PowerShell hooks set
the console to UTF-8 around the tq call and put it back, as mise does; the cmd hook switches to
code page 65001 around it and back (`TestEnv_UnicodePathFoldersRoundTrip`: ação, João and 中文
folders through Windows PowerShell 5.1 and cmd.exe on code page 850, exact restore).

**claude.ai connectors** are the logged-in account's, not a file tq can edit: render cannot
remove one. The hook denies a connector's tools by name (`mcp__claude_ai_<Vendor>__*`), and the
session-start scope line says so in one clause.

**The MCP list: option (b), chosen by the owner on 2026-11-05, in a hybrid form.** Two options
were recorded:
(a) attribution, implemented: remove only what tq can attribute to an out-of-scope provider, keep
everything else; a server that hides its provider (a local script with no package, variable or
host of one) stays and its tools reach the model; (b) an allowlist: keep only the servers the
manifest declares (`claude.bundle.mcp`) plus those attributed to an in-scope provider, remove the
rest (kept aside the same way); stricter, and the PRD's "MCP server list generated from scope",
but every server a developer added by hand (a database, a docs server) is removed until it is
declared. The owner chose (b) on 2026-11-05 with two additions, so a provider-free server
needs no declaration: a server is **kept** when (a) the catalog attributes it to an in-scope
provider (a name token, a package, CLI, image or host of its entry, or one of its variables) or
the manifest declares it (claude.bundle.mcp), (b) it is a general-purpose, provider-free
server of internal/providers/mcp_general.yaml (playwright, context7, memory, filesystem,
fetch, git, sequential-thinking, time, everything, postgres, sqlite, puppeteer,
chrome-devtools, microsoft-learn), matched by what its entry runs or calls (a package or image
up to a version, path or tag; a host), never by its name alone, or (c) its exact name is in the
manifest's new list mcp.allow (names only: letters, digits, `.` `_` `-`; the protected
manifest, so only a human edits it). Every other server is moved to mcp-out-of-scope.json at
render. An out-of-scope attribution removes a server whatever (a) to (c) say, mcp.allow
included. The hook judges an mcp__<server>__* call the same way (policy.(*Scope).JudgeMCP):
denied unless every same-named entry in every source is kept; a claude.ai connector (no entry)
by its name: an in-scope token, a general server's token or mcp.allow; a name found in no
source keeps the name rule alone. Plugin servers follow the same rule in the hook; render
reports them. Two local scripts of the critic's fixtures (a python and a node server of no
provider) are now removed until mcp.allow names them.

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
- Round 2 scenario tests: `TestEnv_ScopeReferenceScenario_SharedFolder` (the mask answers aws,
  aws.exe and tq run), `TestEnv_ScopeReferenceScenario_CmdExe` (cmd.exe's real hook with a 3.5 KB
  and a 9 KB PATH, exact restore), `TestPathLayer_MaskCoversEverySpelling` (cmd, a batch file
  without `call`, PowerShell with `aws.exe`, `vercel.ps1` and an edited `PATHEXT`),
  `TestPathLayer_MaskComesBack`, `TestPathLayer_AmbiguousNamesLeftAlone`,
  `TestPathLayer_QuotedAndNetworkEntries`, `TestState_FitsCmdLine`,
  `TestEmit_Cmd_RoundTripsEveryCharacter`, `TestPreToolUse_MCPJudgedByConfigEntry`,
  `TestOutOfScopeMCPEntry`, `TestScope_MCPByConfigEntry`, `TestOutOfScopeEnv_SkipsGenericTools`,
  `TestGenericTool`, `TestDeleteMember`, `TestKeepRemoved_CorruptFileMovedAside`, `TestEntries`.
- **Everyday work.** Dropping is limited to folders whose runnable files are all out-of-scope
  CLIs; a folder tq is unsure of is kept. Ambiguous words are never hidden and generic tools'
  variables never unset. A remaining false positive is a project tool whose name is an
  unambiguous catalog executable: masked, exactly as the hook already denies it by name; the
  developer adds the provider to `scope:`.
- The `.vbe`, `.jse`, `.wsf`, `.wsh`, `.msc` and `.cpl` spellings are not masked (no CLI ships
  that way); the hook does not read them as the CLI's name either.
- **MCP sources tq does not render**, all of them still judged by the hook by server name: a
  project's `.mcp.json` (in the identity's folder, the repo's own), a plugin's MCP servers (the
  identity's enabled plugins; a plugins folder shared between identities by a junction carries
  them across), claude.ai connectors of the logged-in account, `managed-mcp.json` (machine-wide),
  `--mcp-config` on the command line, Codex's project `.codex/config.toml`, and an identity whose
  Claude is not isolated (no `claude` identity: `CLAUDE_CONFIG_DIR` unset, so the global
  `~/.claude.json` loads); the hook still reads `.mcp.json` and the identity's files for its
  entry judgement. A server whose name and entry spell no provider (a local `node server.js`
  that calls AWS with credentials it finds itself) is not removed at layer 3 and passes layer 4
  (option (a) above; option (b) would remove it).
- `.claude.json` is rewritten by Claude Code while it runs: a render while a session uses the
  config home may be undone; the render warns and a later render repeats the removal.
- Changes `internal/envplan`, `internal/shell` (the bash, zsh and cmd hooks), `internal/policy`
  (read accessors, the MCP entry judgement), `internal/providers` (`mcp_packages`, `GenericTool`,
  `AmbiguousExecutable`), `internal/bundle`, `cmd/tq` (`StubMain`) and adds `internal/mcpscope`; the mask folder is written across boundary B2 and read under B1 of the threat model. Phase 3 replaces the mask folder with the shim folder on a PATH built
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
| Attribution only (option (a)) | A server that hides its provider stays and its tools reach the model; the owner chose the hybrid allowlist |
| A strict allowlist (declared servers only) | Every provider-free server a developer uses (playwright, a database) would need declaring; the general list keeps those |
| A `.cmd` stub only on Windows | `aws.exe`, `aws.ps1` typed out and an edited `PATHEXT` reached the real CLI, and a batch file calling the stub without `call` ended (critic round 1) |
| A tiny stub program embedded in the repository | A binary blob in the source tree to build and sign per platform; the tq binary already is a program, and a hard link costs no space |
| Re-encode `.claude.json` with Go's JSON encoder | Reorders keys and changes indentation in a file Claude Code owns; the member-span edit keeps every other byte |
| Keep two full PATH copies in the state, uncompressed | About 2.8 times the PATH: a 3.5 KB PATH overflowed cmd.exe's 8191-character line and the state was lost (critic round 1) |

Records are never edited after acceptance. A changed mind is a new record that
supersedes this one.
