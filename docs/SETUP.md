# Setup plans

`tq setup` scaffolds several client workspaces — plus shell hooks — from a
single declarative plan, so onboarding a new machine or a batch of companies
doesn't mean running `tq add` and `tq activate` by hand for each one. It has
two modes:

- No flags: an interactive wizard walks through the plan step by step and
  applies it at the end.
- `--from <file>`: load a plan YAML and preview or apply it non-interactively
  (`--dry-run`, `--yes`, `--json`).

`tq setup --example` prints a starting-point plan you can copy, edit, and
feed back in with `--from`.

## Example plan

This is the exact output of `tq setup --example`:

```yaml
# tq setup plan — edit this file, then run:
#   tq setup --from <this file> --dry-run
#   tq setup --from <this file> --yes
#
# base: the folder each company gets a subdirectory under.
# companies: one entry per client; git_name/git_email are required.
# identities: CLI tools to isolate per workspace (defaults to claude, gh).
# hooks: shells to install tq's activation hook into.
# trust: whether newly scaffolded workspaces are auto-trusted.
base: C:/repos
companies:
    - name: acme
      display_name: Acme Corp
      color: blue
      git_name: Jane Doe
      git_email: jane@acme.com
      identities:
        - claude
        - gh
      permission_mode: acceptEdits
    - name: globex
      display_name: Globex Corporation
      color: green
      git_name: Jane Doe
      git_email: jane@globex.com
      identities:
        - claude
        - gh
      permission_mode: default
hooks:
    - bash
    - zsh
    - pwsh
trust: true
```

## Field reference

| Field | Required | Description |
|---|---|---|
| `base` | yes | The folder each company gets a subdirectory under, e.g. `C:/repos` or `~/work`. Created if missing (the wizard offers to create it; `--from` expects it to already exist unless a company scaffold creates it as a side effect). |
| `companies` | yes, at least one | List of client/company entries to scaffold as `tq` workspaces. A plan with zero companies fails validation with `plan has no companies`. |
| `companies[].name` | yes | Short workspace id. Must match the workspace naming rule (lowercase, alphanumeric/hyphen) and be unique within the plan. Becomes the subfolder name under `base`. |
| `companies[].display_name` | no | Human-friendly name shown in listings; defaults to `name` if omitted. |
| `companies[].color` | no | Cosmetic color hint for terminals/prompts that support it. |
| `companies[].git_name` | yes | Git `user.name` for this workspace's `.gitconfig-tentaqles`. |
| `companies[].git_email` | yes | Git `user.email` for this workspace. Must look like an email. |
| `companies[].git_user` | no | Optional platform username (e.g. GitHub handle), stored alongside git identity but not used by git itself. |
| `companies[].identities` | no | List of provider ids (from the provider catalog, e.g. `claude`, `gh`, `aws`) to isolate per workspace. Defaults to `[claude, gh]` if omitted. Each id must be known to the catalog (`tq providers list`) or `Validate` rejects the plan. |
| `companies[].permission_mode` | yes | Claude Code permission mode for this workspace. Must be one of the values in `manifest.PermissionModes` (e.g. `default`, `acceptEdits`, `bypass`). |
| `hooks` | no | List of shells (`bash`, `zsh`, `fish`, `pwsh`, `powershell`) to install the `tq activate` line into. Shells already carrying a hand-installed activation line are detected as "present (unmanaged)" and left untouched — never duplicated. |
| `trust` | no, defaults to `true` | Whether newly scaffolded workspaces are auto-trusted with `tq allow` as part of apply. If the key is absent from the YAML it defaults to `true`; set it to `false` explicitly to scaffold without trusting. |

## Preview, tool-check, and apply

With `--from`, `tq setup` always runs a preview and a tool-check before
touching anything:

- **Preview** lists every change the plan would make (`workspace-create`,
  `workspace-skip` for a company folder that already exists with a matching
  manifest, `hook-install`, etc.) as `KIND  TARGET  DETAIL` rows.
- **Tool-check** reports, per company, whether each identity's CLI is
  installed (`[ok]`), missing with an install hint (`[missing]`), or not
  applicable (`[n/a]`, no CLI for that provider).

`--dry-run` stops after preview + tool-check and writes nothing. Without
`--dry-run`, `tq setup --from` asks `Apply these changes? [y/N]` unless
`--yes` is passed; on a non-interactive stdin without `--yes` it refuses to
apply rather than silently doing nothing or silently applying.

`--json` switches preview, tool-check, and (once applied) the report to a
single JSON object (`{"preview": [...], "toolcheck": {...}, "report": {...}}`)
instead of the human-readable tables — useful for driving `tq setup` from
scripts or the desktop app.

`--write-plan <path>` additionally saves the wizard's answers to that path
as a plan file (on top of the always-written
`~/.tentaqles/last-setup.yaml`), so you can inspect what the wizard decided
or replay it non-interactively later with `--from`.

## The login step

`tq setup` scaffolds workspaces and installs hooks, but it does not log any
CLI in — that stays a separate, explicit step so credentials are never
handled implicitly by a setup script. After `apply` finishes, the report
prints a "Next — run these logins" section listing one `tq login <workspace>
<identity>` command per provider that has a login flow, e.g.:

```
Next — run these logins:
  tq login acme gh
  tq login acme claude
```

Run each of those yourself (interactively) to have that CLI's own login flow
write credentials into the workspace's private, isolated config home. This
mirrors `tq login`'s normal behavior outside of `setup` — see the main
README's "Quick start" and "Where things live" sections for how per-workspace
credential isolation works.

## Saved plan files and permissions

Because a plan file contains git identity metadata (name and email) for
every company, `tq setup`/the wizard write plan files (both `--write-plan`
and `~/.tentaqles/last-setup.yaml`) with owner-only permissions — `0600` for
the file, `0700` for any directory created to hold it — on platforms that
enforce POSIX permission bits. Windows does not enforce these bits, so the
file is still written there but without the same OS-level restriction.
