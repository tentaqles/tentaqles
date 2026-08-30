# `tq migrate` — adopting a hand-rolled setup

`tq migrate` is for a machine that already worked before `tq` existed.

Multi-client identity isolation can be built by hand, and on a lot of boxes
it was: identity directories scattered under `~/.claude-<client>` and
`~/.cli-identities/`, one `includeIf` per repo pasted straight into
`~/.gitconfig`, an activation block copied into every shell profile, and a
`cmd.exe` `AutoRun` shim. All of that works. None of it is something `tq` can
upgrade, verify or remove, because `tq` did not write it: `tq hooks install`
refuses to append a second block next to a hand-written one, `tq doctor`
reports the identity directories as links to data living outside `tq`, and a
global `user.email` keeps quietly signing commits made outside any workspace.

`tq migrate` moves that arrangement under `tq`'s management without throwing
any of it away. It runs as a dry run by default:

```
tq migrate                          # show the plan, change nothing
tq migrate --apply                  # do it
tq migrate --steps git --apply      # do only the git step
tq migrate --json                   # the same plan as one JSON object
```

Every mutation is written to a journal **before** it happens, and
`tq uninstall --restore` replays that journal backwards.

## Steps

There are four steps. `--steps` takes a comma-separated list; the default is
`identity,git,shell`.

| Step | Default | What it does |
|---|---|---|
| `identity` | yes | Moves identity directories that live outside `tq` into `~/.tentaqles/identities/`, leaving a junction behind at the old path. |
| `git` | yes | Removes the global `user.email` and the hand-written `includeIf` blocks, and replaces them with `tq`'s managed include file. |
| `shell` | yes | Replaces hand-installed activation blocks in shell profiles with `tq`'s marker-delimited managed block. |
| `cmd` | **no** | Clears the legacy `cmd.exe` `AutoRun` value (Windows only). |

Steps always run in `tq`'s own order — `identity`, `git`, `shell`, `cmd` —
whatever order you list them in, because `identity` moves directories that
`git` and `shell` then point at. Duplicates collapse; an unknown name is an
error rather than a silent no-op.

Two things about `--apply` that hold for every step. First, apply does not
trust the plan you read: it re-derives the plan from the machine and refuses
if the two no longer match ("the machine changed since the plan was
computed"). Something you did between the dry run and the apply cannot slip
in unseen. Second, `migrate.Run` plans everything first and then applies in
order, stopping at the first failure — so a failed apply leaves a partial
migration, and the command tells you which journal undoes it.

### `identity`

For every identity directory of every **trusted** workspace that is really a
link — `~/.tentaqles/identities/dirtybird/claude` pointing at
`~/.claude-dbi`, say — the step does three things, in this order, each
journalled before it happens:

1. `remove-link` the link at `~/.tentaqles/identities/<ws>/<cli>`;
2. `move-dir` the real data from `~/.claude-dbi` into that path;
3. `make-link` a junction at `~/.claude-dbi` pointing back at it.

The junction is the point. Aliases, shortcuts, scripts and half-remembered
paths that name the old location keep working; the data is simply somewhere
`tq` can manage it.

What it will **not** touch:

- **Untrusted workspaces.** A workspace you have not run `tq allow` on is
  skipped whole, with a `skip:` line saying so.
- **Junctions *inside* a moved directory.** A Claude identity directory
  typically has `plugins/`, `agents/` and `skills/` junctioned to
  `~/.claude/...`. That sharing is intentional and copying it would fork
  your plugin set, so the inner links move along with the directory
  untouched.
- **Legacy directories nothing references.** A `~/.claude-work` sitting
  beside a `~/.claude-dbi` that no workspace points at is reported as a
  warning and left exactly where it is. `tq` will not delete data on a
  guess.
- **Dangling or unreadable links**, and a link whose target is a file rather
  than a directory. Each is warned about and skipped.

**The in-use refusal.** The step refuses, all or nothing, while a CLI it
would move is running: moving `~/.claude-dbi` out from under a live `claude`
is exactly the accident this exists to prevent. Two checks feed it — a
process whose command line mentions the directory, and a process whose
executable *is* the CLI that owns it (`claude` for a `claude` directory,
`gh` for a `gh` one), because on Windows the command line of another user's
process often comes back empty.

The documented answer is to **close those sessions and re-run**. Not
`--force`. `--force` does exist and does clear this refusal, but using it
means asking the OS to move a directory a running process is holding open,
and the failure mode is a half-copied credential store. If the refusal looks
like a false positive, the fix is still to close the process it names.

The refusal is evaluated at **apply** time, not at plan time. A dry run
therefore always shows you the complete plan and then a warning naming what
is running — `applying will refuse: claude is running, which owns
C:\Users\renato\.claude-dbi: ...` — rather than hiding the plan behind the
blocker. That is deliberate: you can read the whole migration before
deciding which windows to close.

### `git`

The hand-rolled arrangement is a global `user.email`, one `includeIf` per
workspace written straight into `~/.gitconfig`, and workspace
`.gitconfig-tentaqles` files edited by hand. The step replaces it with the
one `tq` maintains: no global email, one managed include file
(`~/.gitconfig-tentaqles`), one `tq`-written file per workspace.

**`~/.gitconfig` is backed up whole before the first edit.** Every change
below goes through `git config --global`, which rewrites that one file, and
the whole-file copy is the only inverse that is actually exact. Git has no
"undo one `--add` of a multi-valued key": reversing an `--add include.path`
with `--unset-all` would take your own includes with it. So the journal
records one byte-exact backup of the file, and the per-key entries exist
only so the journal *names* what was removed instead of hiding it inside a
blob.

What changes:

- **`user.email` is unset globally. `user.name` is kept.** Only the email
  picks the wrong identity, and git still wants a name. In its place,
  `user.useConfigOnly=true` is set, so git refuses to commit outside a
  workspace rather than guessing an address from the hostname — the failure
  becomes loud instead of silent.
- **`includeIf` blocks are removed only for registered, trusted
  workspaces**, whose identity `tq`'s own include file already carries. An
  `includeIf` pointing at an unregistered directory, at a nested repo, or at
  a workspace you have not trusted is left exactly where it is, with its
  reason in the `skip:` lines. Removing an untrusted workspace's `includeIf`
  would leave that repo with no identity at all.
- **Orphans are removed.** An `includeIf` or a plain `include.path` whose
  target file no longer exists is removed, whether or not it names a
  workspace `tq` knows.
- **Workspace files are rewritten** when they are missing or no longer look
  like something `tq` wrote (no `# managed by tq` header, or a config
  section other than `[user]`). The identity kept is the one **the existing
  file uses**, not the manifest's — the file is what git actually reads
  today. When the two disagree you get a warning naming both, and are
  expected to fix one of them.
- **`~/.gitconfig-tentaqles` is regenerated** to cover exactly the trusted
  workspaces, and `include.path` plus `user.useConfigOnly` are ensured in
  `~/.gitconfig`.

### `shell`

Only profiles whose state is `present (unmanaged)` — a hand-installed block,
no `tq` markers — are candidates; `tq hooks status` names them. A profile
`tq` already manages, one with only one of its two markers (`corrupt`), one
it cannot read, and one with no hook at all are each reported and skipped.

Adoption replaces the hand-installed block with `tq`'s marker-delimited one:

```
# >>> tq >>>
if ($env:TQ_ENABLED -ne '0' -and (Get-Command tq -ErrorAction SilentlyContinue)) { tq activate pwsh | Out-String | Invoke-Expression }
# <<< tq <<<
```

Everything outside the located block survives byte for byte — BOM, CRLF,
unrelated aliases and functions. Inside it, two things are carried over:

- **The legacy `TQ_ENABLED=0` branch, verbatim**, wrapped in
  `# >>> tq-legacy >>>` / `# <<< tq-legacy <<<`. See the kill switch section
  below.
- **Your `claude` wrapper function**, under the same `if ($env:TQ_ENABLED
  -ne '0')` guard it used to sit behind. Without the guard, `TQ_ENABLED=0`
  would hand control to the legacy launcher and then have the legacy
  launcher's own `claude` router clobbered by the carried wrapper.

**What gets dropped: the hand-rolled PATH lines.** The managed block does
not put `%LOCALAPPDATA%\tentaqles\bin` on `PATH`; `tq`'s installer puts it on
the persistent user `PATH` instead. Every dropped line is printed as a
warning, and **you should check the bin directory really is on your
persistent `PATH` before applying** — on a machine where the installer never
did that, `tq` stops resolving in new shells and the managed block then
silently does nothing. On Windows:

```powershell
[Environment]::GetEnvironmentVariable('Path','User') -split ';' | Where-Object { $_ -like '*tentaqles*' }
```

**Adopted profiles gain a `<profile>.tq-backup` sidecar.** `internal/hooks`
writes it the first time `tq` is about to modify a profile, once ever, so it
always reflects the file as it was before `tq` ever touched it. It is
separate from the migration journal, and `tq uninstall --restore` does not
remove it — the restore puts the profile back from the journal's own copy
and leaves the sidecar sitting there. Delete it yourself when you are
satisfied.

### `cmd`

Windows only, and **not in the default `--steps`**. It clears
`HKCU\Software\Microsoft\Command Processor\AutoRun`, which the pre-`tq`
setup used to source a shim script into every `cmd.exe`.

It is opt-in because it is the one step whose effect is invisible until the
next console starts, and it changes every console you open, not just a
shell you can restart to check. Ask for it explicitly:

```
tq migrate --steps cmd            # dry run
tq migrate --steps cmd --apply
```

Two refusals guard it:

- **A registry type `tq` cannot restore byte-for-byte.** `REG_SZ`,
  `REG_EXPAND_SZ`, `REG_DWORD`, `REG_QWORD` and `REG_BINARY` round-trip
  through `reg.exe` faithfully. `REG_MULTI_SZ` does not: `reg.exe` separates its
  elements with a literal `\0` sequence, so a value whose data contains that
  sequence cannot be told apart from a two-element value. Rather than record
  something that would restore wrong, the step skips with a `manual:` line
  telling you to clear it yourself.
- **A value that does not look like `tq`'s.** `AutoRun` is a shared hook and
  commonly belongs to something else entirely — clink is the usual case. If
  the value mentions none of `tentaqles`, `cli-identities` or `tq.exe`, the
  step still plans to clear it but warns that clearing it also disables
  whatever else it starts. Read that warning before applying.

At apply time the value is re-read, and if it changed since the plan was
made the step stops and asks you to re-plan (`--force` overrides). The old
value and its type are journalled before deletion, so the restore writes it
back as `REG_EXPAND_SZ` rather than freezing a `%VARIABLE%` into a literal
path. The shims directory itself is left alone: deleting it is a separate
decision.

## The journal

Every `--apply` opens a journal at:

```
~/.tentaqles/backups/<ts>/            # $TQ_HOME/backups/<ts>/ if TQ_HOME is set
    journal.json                      # the entries, in order
    files/1, files/2, ...             # byte-exact copies of every file overwritten
    restore.state                      # how far a restore got (written by restore)
    restore.log                        # what each restore did (appended)
```

`<ts>` is a UTC timestamp like `20260828T140233Z`, and the path is printed
*before anything is touched*, so a run you interrupt halfway still tells you
where its undo lives.

Two properties are worth knowing:

- **Record first, mutate second.** Every entry is written and fsynced before
  the mutation it describes. A crash therefore leaves a journal that is
  ahead of reality — it legitimately describes mutations that never
  happened. Each reverse is written to accept that: finding the world
  already in the entry's pre-operation state is a success ("nothing to
  undo"), and only a state it does not recognise is a failure.
- **Backed-up files are checked before they are written back.** Size and
  SHA-256 travel with the entry and are verified before the copy goes over
  your file.

`tq migrate --json` reports the journal timestamp in its `ts` field. On a
dry run `ts` is empty — a timestamp naming no journal would be a trap for a
script that pipes it into `tq uninstall --restore`. After a **failed**
`--apply` it is set, with `"applied": false`, because that is exactly when
you need it.

## Undo: `tq uninstall --restore`

```
tq uninstall --restore latest              # list what it would undo
tq uninstall --restore 20260828T140233Z    # a specific journal
tq uninstall --restore latest --yes        # actually do it
```

Without `--yes` it prints the journal path and every entry, newest first —
the order it reverses them in — and changes nothing. `latest` resolves to the
newest backup directory containing a `journal.json`; if a *newer* directory
holds only a half-written temp file, `latest` refuses to resolve at all
rather than quietly offering you an older, unrelated migration's restore.

Restore is **resumable, and deliberately not idempotent.** It records the
lowest sequence number it has reversed in `restore.state`, and a re-run
continues below that point instead of starting again at the newest entry.
That marker is load-bearing, not a convenience: entries interact. Reversing
a `move-dir` puts a real directory back where a newer `make-link` entry
expects to find the link it created, and that newer entry will then rightly
refuse to touch it. Replaying a journal that already restored cleanly stops
on the first such pair. So: to finish an interrupted restore, just run it
again; do not try to "restore twice".

The journal file itself is never modified by a restore. What was done is
appended to `restore.log`, so a state you fix up by hand can be restored
again.

What restore does **not** do:

- It is not an uninstaller. Removing `tq`'s own shell hooks and its global
  `include.path` is a separate job this version does not do.
- It does not remove the `<profile>.tq-backup` sidecars the shell step left.
- It stops at the first entry it cannot reverse and tells you the sequence
  number, operation and path. It does not skip ahead.

## The `TQ_ENABLED=0` kill switch

The hand-installed PowerShell block had two branches: a legacy launcher when
`TQ_ENABLED=0`, and the `tq` activation otherwise. Adoption keeps that
escape hatch working.

The legacy branch is preserved **verbatim** — the exact bytes, not a
regenerated equivalent — inside a wrapper:

```powershell
# >>> tq-legacy >>>  (your pre-tq setup, kept verbatim by tq migrate; delete this block by hand when you no longer need TQ_ENABLED=0)
if ($env:TQ_ENABLED -eq '0') {
    ... your original launcher, unchanged ...
}
# <<< tq-legacy <<<

# >>> tq >>>
if ($env:TQ_ENABLED -ne '0' -and (Get-Command tq -ErrorAction SilentlyContinue)) { tq activate pwsh | Out-String | Invoke-Expression }
# <<< tq <<<
```

So `$env:TQ_ENABLED = '0'` in a new shell gives you exactly the pre-`tq`
behaviour, and unsetting it gives you `tq`. Both `tq`'s managed block and
the carried `claude` wrapper honour the same variable. bash and zsh have no
legacy branch to preserve — their hand-installed block was a single
conditional — so there `TQ_ENABLED=0` simply switches `tq` off, with nothing
to fall back to.

`tq doctor` reports `legacy-active` (a warning) in a shell with
`TQ_ENABLED=0`, so a shell you left switched off does not look like a broken
migration.

One honest caveat: `tq` cannot verify the preserved branch still works. It
kept your bytes, it did not test them. Nothing removes the branch
automatically either — it is your fallback, and it stays until you delete it
by hand.

## This machine, before and after

The captures below are from the development box this feature was built on —
five workspaces (`tentaqles`, `dirtybird`, `yduqs`, `personal`, `uplabs`)
set up by hand over about a year.

### Before: `tq doctor`

```
[error] dirtybird: C:\repos\dirtybird\.gitconfig-tentaqles: does not start with the tq header (hand-edited or replaced)  → delete it and re-run tq add, or restore the tq-managed contents
[warn] dirtybird: C:\Users\renato\.tentaqles\identities\dirtybird\az is a link to C:\Users\renato\.cli-identities\az-ppu: the identity data lives outside tq  → tq migrate --steps identity
[warn] dirtybird: C:\Users\renato\.tentaqles\identities\dirtybird\claude is a link to C:\Users\renato\.claude-dbi: the identity data lives outside tq  → tq migrate --steps identity
[warn] dirtybird: C:\Users\renato\.tentaqles\identities\dirtybird\gh is a link to C:\Users\renato\.cli-identities\gh-dirtybird: the identity data lives outside tq  → tq migrate --steps identity
[error] personal: C:\repos\personal\.gitconfig-tentaqles: does not start with the tq header (hand-edited or replaced)  → delete it and re-run tq add, or restore the tq-managed contents
[error] tentaqles: C:\repos\tentaqles\.gitconfig-tentaqles: does not start with the tq header (hand-edited or replaced)  → delete it and re-run tq add, or restore the tq-managed contents
[error] uplabs: C:\repos\uplabs\.gitconfig-tentaqles: does not start with the tq header (hand-edited or replaced)  → delete it and re-run tq add, or restore the tq-managed contents
[warn] uplabs: C:\Users\renato\.tentaqles\identities\uplabs\claude is a link to C:\Users\renato\.claude-uplabs: the identity data lives outside tq  → tq migrate --steps identity
[warn] uplabs: C:\Users\renato\.tentaqles\identities\uplabs\gh is a link to C:\Users\renato\.cli-identities\gh-uplabs: the identity data lives outside tq  → tq migrate --steps identity
[error] yduqs: C:\repos\yduqs\.gitconfig-tentaqles: does not start with the tq header (hand-edited or replaced)  → delete it and re-run tq add, or restore the tq-managed contents
[warn] yduqs: C:\Users\renato\.tentaqles\identities\yduqs\az is a link to C:\Users\renato\.cli-identities\az-estruturante: the identity data lives outside tq  → tq migrate --steps identity
[error] global user.email is set: commits outside workspaces will silently use rdomingues@pitcherco.com  → tq migrate --steps git
[warn] tentaqles: ~/.gitconfig has a hand-written includeIf "gitdir:C:/repos/tentaqles/" → C:/repos/tentaqles/.gitconfig-tentaqles, which tq's include file already covers  → tq migrate --steps git
[warn] dirtybird: ~/.gitconfig has a hand-written includeIf "gitdir:C:/repos/dirtybird/" → C:/repos/dirtybird/.gitconfig-tentaqles, which tq's include file already covers  → tq migrate --steps git
[warn] yduqs: ~/.gitconfig has a hand-written includeIf "gitdir:C:/repos/yduqs/" → C:/repos/yduqs/.gitconfig-tentaqles, which tq's include file already covers  → tq migrate --steps git
[warn] personal: ~/.gitconfig has a hand-written includeIf "gitdir:C:/repos/personal/" → C:/repos/personal/.gitconfig-tentaqles, which tq's include file already covers  → tq migrate --steps git
[warn] uplabs: ~/.gitconfig has a hand-written includeIf "gitdir:C:/repos/uplabs/" → C:/repos/uplabs/.gitconfig-tentaqles, which tq's include file already covers  → tq migrate --steps git
[warn] global git config includes C:/repos/booster/.gitconfig-tentaqles, which does not exist  → tq migrate --steps git
[warn] global git config includes C:/repos/tentaqles/author.ai/.gitconfig-tentaqles, which does not exist  → tq migrate --steps git
```

Findings unrelated to the migration (bundle drift, two workspaces not logged
into Claude, a stale `TQ_WS` in the shell that ran it) are omitted here;
`tq migrate` neither reports nor fixes those.

That is the whole shape of a hand-built setup: six identity directories
living outside `tq`, five workspace git files `tq` did not write, a global
email that signs anything committed outside a workspace, five redundant
`includeIf` blocks, and two includes pointing at directories that no longer
exist.

### The plan: `tq migrate` (dry run)

```
identity: 18 changes
  ~ remove-link        C:\Users\renato\.tentaqles\identities\dirtybird\az -> link to C:\Users\renato\.cli-identities\az-ppu
  ! move-dir           C:\Users\renato\.cli-identities\az-ppu -> to C:\Users\renato\.tentaqles\identities\dirtybird\az (dirtybird/az)
  ~ make-link          C:\Users\renato\.cli-identities\az-ppu -> junction back to C:\Users\renato\.tentaqles\identities\dirtybird\az, so the old path keeps working
  ~ remove-link        C:\Users\renato\.tentaqles\identities\dirtybird\claude -> link to C:\Users\renato\.claude-dbi
  ! move-dir           C:\Users\renato\.claude-dbi -> to C:\Users\renato\.tentaqles\identities\dirtybird\claude (dirtybird/claude)
  ~ make-link          C:\Users\renato\.claude-dbi -> junction back to C:\Users\renato\.tentaqles\identities\dirtybird\claude, so the old path keeps working
  ... (the same three lines for dirtybird/gh, uplabs/claude, uplabs/gh, yduqs/az)
  warn: C:\Users\renato\.claude-personal looks like a legacy identity directory but no workspace references it; tq leaves it alone
  warn: C:\Users\renato\.claude-work looks like a legacy identity directory but no workspace references it; tq leaves it alone
  warn: applying will refuse: claude is running, which owns C:\Users\renato\.claude-dbi: claude.exe "C:\Users\renato\.local\bin\claude.exe" --dangerously-skip-permissions; ... (close them and re-run, or pass --force)
  skip: junctions *inside* the moved directories (plugins/agents/skills to ~/.claude/...) are left as they are: the sharing is intentional
git: 14 changes
  ~ unset-global       user.email -> "rdomingues@pitcherco.com" -> (unset); with user.useConfigOnly, git refuses to commit outside a workspace instead of guessing
  ~ remove-includeif   C:/repos/tentaqles/.gitconfig-tentaqles -> includeIf "gitdir:C:/repos/tentaqles/" for workspace tentaqles; tq's own include file already covers it
  ~ remove-includeif   C:/repos/dirtybird/.gitconfig-tentaqles -> includeIf "gitdir:C:/repos/dirtybird/" for workspace dirtybird; tq's own include file already covers it
  ~ remove-includeif   C:/repos/booster/.gitconfig-tentaqles -> includeIf "gitdir:C:/repos/booster/" points at a file that does not exist
  ~ remove-includeif   C:/repos/yduqs/.gitconfig-tentaqles -> includeIf "gitdir:C:/repos/yduqs/" for workspace yduqs; tq's own include file already covers it
  ~ remove-includeif   C:/repos/personal/.gitconfig-tentaqles -> includeIf "gitdir:C:/repos/personal/" for workspace personal; tq's own include file already covers it
  ~ remove-includeif   C:/repos/uplabs/.gitconfig-tentaqles -> includeIf "gitdir:C:/repos/uplabs/" for workspace uplabs; tq's own include file already covers it
  ~ remove-includeif   C:/repos/tentaqles/author.ai/.gitconfig-tentaqles -> includeIf "gitdir:C:/repos/tentaqles/author.ai/" points at a file that does not exist
  ! rewrite-ws-file    C:\repos\dirtybird\.gitconfig-tentaqles -> user.name = "Dirtybird Industries", user.email = "rdomingues@pitcherco.com" (from the existing file)
  ! rewrite-ws-file    C:\repos\personal\.gitconfig-tentaqles -> user.name = "Personal Projects", user.email = "renatodomingues09@hotmail.com" (from the existing file)
  ! rewrite-ws-file    C:\repos\tentaqles\.gitconfig-tentaqles -> user.name = "Tentaqles (Freelance/Consulting)", user.email = "reach@tentaqles.ai" (from the existing file)
  ! rewrite-ws-file    C:\repos\uplabs\.gitconfig-tentaqles -> user.name = "Uplabs", user.email = "renato.domingues@uplabs.us" (from the existing file)
  ! rewrite-ws-file    C:\repos\yduqs\.gitconfig-tentaqles -> user.name = "YDUQS", user.email = "renato.domingues@yduqs.com.br" (from the existing file)
  ~ sync-include-file  C:\Users\renato\.gitconfig-tentaqles -> 5 workspace(s): dirtybird, personal, tentaqles, uplabs, yduqs
  warn: dirtybird: C:\repos\dirtybird\.gitconfig-tentaqles is not tq-managed (does not start with the tq header, so it was hand-edited or replaced); tq rewrites it, keeping Dirtybird Industries <rdomingues@pitcherco.com>
  ... (the same warning for personal, tentaqles, uplabs, yduqs)
  skip: global user.name ("Renato Domingues") is kept: only the email picks the wrong identity, and git still wants a name
  skip: include.path C:/Users/renato/AppData/Local/Temp/tmp.CCyykuuQB6/.gitconfig-tentaqles exists and is not tq's; left alone
shell: 3 changes
  ~ adopt-hook         C:\Users\renato\.bashrc -> replace hand-installed block with the managed one
  ~ adopt-hook         C:\Users\renato\Documents\PowerShell\Microsoft.PowerShell_profile.ps1 -> replace hand-installed block with the managed one; keep legacy launcher verbatim; carry over the claude wrapper
  ~ adopt-hook         C:\Users\renato\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1 -> replace hand-installed block with the managed one; keep legacy launcher verbatim; carry over the claude wrapper
  warn: bash: dropped from the old block: case ":$PATH:" in *"/tentaqles/bin:"*) ;; *) PATH="$PATH:$LOCALAPPDATA/tentaqles/bin";; esac
  warn: pwsh: dropped from the old block: $tqBin = Join-Path $env:LOCALAPPDATA 'tentaqles\bin'
  warn: pwsh: dropped from the old block: if (($env:Path -split ';') -notcontains $tqBin) { $env:Path = "$env:Path;$tqBin" }
  warn: powershell: dropped from the old block: $tqBin = Join-Path $env:LOCALAPPDATA 'tentaqles\bin'
  warn: powershell: dropped from the old block: if (($env:Path -split ';') -notcontains $tqBin) { $env:Path = "$env:Path;$tqBin" }
! marks changes that move or delete real data.
dry run — nothing changed. Re-run with --apply.
```

The `...` lines above are elisions of repeated blocks; nothing else has been
edited. Six things in that output are worth reading closely, and they are the
same six on any hand-built machine:

1. **`!` marks the changes that move or delete real data** — the five
   `move-dir`s and the five `rewrite-ws-file`s. Everything marked `~` is a
   link or a config key.
2. **`booster` and `author.ai` are removed as orphans, not as workspaces.**
   `booster` is not a registered workspace here and `author.ai` is a nested
   directory; both `includeIf`s survive on merit alone — they are removed
   only because the files they point at no longer exist. A live `includeIf`
   for an unregistered directory would have been left alone.
3. **The last `skip:` line is the counter-example.** An `include.path` in a
   temp directory exists and is not `tq`'s, so it is untouched.
4. **`~/.claude-personal` and `~/.claude-work` are warnings, not changes.**
   Two directories that look like abandoned identity homes; `tq` says so and
   leaves them.
5. **The identity step will refuse.** Six `claude` processes were running
   against `.claude-dbi` and `.claude-uplabs` at capture time. The plan is
   shown in full anyway. The fix is to close those six sessions, not to add
   `--force`.
6. **The dropped PATH lines.** On this machine dropping them is safe,
   because `C:\Users\renato\AppData\Local\tentaqles\bin` is on the
   persistent user `PATH` — checked, not assumed. Two of them were also
   already broken: the profiles contain a literal `0x08` byte where
   `tentaqles\bin` was meant, so `$tqBin` resolved to a directory that does
   not exist and the line had been a no-op for as long as it had been there.
   That is exactly the kind of thing a hand-maintained profile accumulates,
   and exactly why the warnings print the dropped text.

### After

Applying the default steps is expected to clear these `tq doctor` codes:
`global-email-set`, `includeif-unmanaged`, `include-orphan`,
`identity-dir-linked` and `git-ws-file-tampered`, and to leave
`tq hooks status` reporting `installed` for bash, pwsh and powershell.

What will *not* change: `~/.claude-personal` and `~/.claude-work` are still
there, the `booster` and `author.ai` workspace directories are still gone,
global `user.name` is still `Renato Domingues`, the temp-directory
`include.path` is still in `~/.gitconfig`, and the two `claude`-credential
and bundle-drift warnings are still open — they are `tq login` and
`tq bundle sync` jobs, not migration ones.

## Known rough edges

- **The preserved legacy branch is never removed for you.** Delete it by
  hand once you are confident you no longer need `TQ_ENABLED=0`.
- **`.tq-backup` sidecars survive a restore.** Clean them up yourself.
- **A restore is not something to run twice.** Re-run it to *finish* an
  interrupted one; it will refuse to usefully replay a completed one.
- **`--force` is a real risk, not a formality.** For the identity in-use
  refusal specifically, the answer is always to close the process.
- **Manifests are not converted.** `tq` reads both v1 and v2, so migration
  deliberately leaves manifest schemas alone.
- **`tq uninstall` is not an uninstaller in this version.** Only
  `--restore` is implemented.
