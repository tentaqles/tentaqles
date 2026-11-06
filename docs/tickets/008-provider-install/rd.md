# 008: Catalog install methods and `tentaqles install <provider>`

**Status:** ready
**Risk:** High: an agent-callable install that a scope bug lets through puts another client's CLI on the machine, and a wrong catalog entry runs an unintended package
**UI:** none

PRD v2 §4.4 step 3, §5.2 step 1 and §3.3. Provisioning and the onboarding skill install a provider's CLI only through a command-line package manager that the embedded catalog names, never a desktop installer. Also adds the providers the prompts name that the catalog lacks.

## Requirements

- **REQ-1**: The catalog's `install` block must accept the methods `winget`, `scoop`, `brew`, `apt`, `npm` and `pip`, under `windows`, `macos` and `linux`. tq must never run a value under `url`.
- **REQ-2**: `tentaqles install <provider>` must, for a provider in the current identity's scope with a method for this OS, run the first method in the order listed above whose package manager is on PATH, then run the catalog's `version_args`, and print one line.
- **REQ-3**: The line must be `<provider>: installed <version>`, `<provider>: already present <version>`, or `<provider>: manual, download from <docs URL>`, the last when there is no usable method.
- **REQ-4**: A provider already resolving on the identity PATH must print `already present` and run no package manager.
- **REQ-5**: A provider outside the scope must refuse with exit 1 and `<provider> is outside the scope of identity <ws>: <scope>`, run nothing, and log one line in the denial log with category `identity`.
- **REQ-6**: In a neutral folder it must refuse with exit 1 and `tentaqles install needs an identity`.
- **REQ-7**: A provider with no CLI (`cli: null`) must print `<provider>: no CLI, nothing to install` and exit 0.
- **REQ-8**: A failed package manager must print `<provider>: install failed: <first error line>`, exit 1, and leave the identity unchanged.
- **REQ-9**: Each package manager run must have a 10-minute limit; at the limit it is stopped and treated as REQ-8.
- **REQ-10**: After a Windows install, verification must look the CLI up with the user and machine PATH read from the registry, added to the current PATH, since a package manager's PATH change doesn't reach a running process.
- **REQ-11**: tq must pass the package id from the catalog as one argv element, with no shell, and pass no argument from the command line to the package manager.
- **REQ-12**: The catalog must gain `discord`, `zoom`, `google-meet`, `colab` and `trello`, each with a `category`, `docs`, `match` lists and `cli: null`, or a real CLI where one exists, and `TestScope_EveryProvider` must exercise them.
- **REQ-13**: The hook must allow `tentaqles install <in-scope provider>` for an agent. It is not human-only.
- **REQ-14**: `docs/PROVIDERS.md` must list the install methods and their order, and state that desktop installers are never run.

## Out of scope

- `reach` calling this: ticket 010.
- MCP entries for the new providers: ticket 009.
- A provider's login: the onboarding skill does that.
- Upgrading a CLI already present.
- Installing tentaqles itself.

## Affected areas

- `internal/providers/`: schema, loader
- `internal/providers/catalog/*.yaml`: new providers, and review of existing install blocks
- `internal/install/` (new): pick a method, run it, verify
- `internal/cmd/install.go` (new), `internal/cmd/root.go`
- `docs/PROVIDERS.md`, `docs/COMMANDS.md`, `CHANGELOG.md`, `docs/adr/0016-provider-install.md`

## Do not touch

- `internal/policy/selfprotect.go`: `install` isn't on the human-only list, and nothing needs adding.
- `internal/setup/toolcheck.go`: ticket 015 removes the package.

## Decisions

- **Agreed with the owner**: `reach` and the onboarding skill use the same install path, per the PRD.
- **Decided by the ticket writer**: the method order is `winget`, `scoop`, `brew`, `apt`, `npm`, `pip`, picking the first present, so a machine's system package manager wins over a language one.
- **Decided by the ticket writer**: `winget` may itself run a vendor installer behind the scenes. That counts as a command-line package manager, as the PRD's list names it, and tq never launches an installer file itself.
- **Decided by the ticket writer**: apt runs as `sudo`-free `apt-get install -y`; when it fails for lack of rights, REQ-8's line reports it. tq never adds `sudo`.
- **Decided by the ticket writer**: an existing `url` value stays in the catalog as data, for the docs only; the manual line prints `docs:` because a direct `.msi` link is not a download page.
- **Decided by the ticket writer**: `tq install` needs no integrity re-sign, because it writes nothing under `$TQ_HOME`.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 008` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
