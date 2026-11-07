# ADR-0015: `tentaqles install` puts a provider's CLI on the machine

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md` (the spec), ADR-0003 (human-only), ADR-0005 (self-protection), ADR-0007 (provider scope), ADR-0008 (identity PATH), ADR-0012 (reach)

## Context

IDEATION says the system "installs the CLIs for the selected providers". The catalog already holds install
hints per OS. An install runs vendor code on the machine, so three questions need an answer: who may ask for
one, which providers, and what exactly runs.

## Decision

- `tentaqles install <provider>` is not human-only. The owner agreed an agent may call it, because the
  onboarding skill retries a failed install through it. It is not on the human-only list of the hook
  (`internal/policy/selfprotect.go` is unchanged) and writes nothing under the state folder.
- Only a provider in the current identity's scope installs. Anything else is refused with exit 1, the
  scope in the message, and one line in the denial log: category `identity`, rule `tq/out-of-scope-cli`,
  layer `command` (the command's own refusal, as `human-only` is for the human-only commands). A neutral or
  untrusted folder refuses too, naming the reason.
- What runs is fixed by the **embedded** catalog. The package id is one argv element after tq's own fixed
  arguments, with no shell, and nothing from the command line reaches the package manager (the command takes
  the provider id and nothing else). A user provider file can add a provider or widen the guard's denials, not
  name a package: the command looks the id up in the embedded catalog only.
- Six package managers, in this order: `winget`, `scoop`, `brew`, `apt` (`apt-get`), `npm`, `pip`. The first
  whose program is on the identity's PATH runs. tq adds no `sudo`; an `apt` run without the rights fails, and
  the line says so.
- A catalog `url` is documentation. tq never opens or runs it and never launches an installer file. `winget`
  may run a vendor installer; that is the package manager's own work. With no usable method the line names the
  docs page.
- winget runs with `--accept-package-agreements --accept-source-agreements --disable-interactivity`: tq accepts
  the vendor's licence for the person, and the docs say so.
- A run has a 10-minute limit. At the limit the package manager and its whole process tree are stopped (a
  process group on Unix, a job object on Windows), and the result is the same failure line as any other.
- The failure line is the package manager's error line: the first line of stderr, then stdout, that reads as an
  error, skipping progress lines, spinner frames and warnings.
- The package manager and the version check run with the identity's environment. After a Windows install
  the CLI is found through the user and machine PATH read from the registry.
- `tentaqles reach` runs the same function for each provider in scope with a CLI, after the trust is
  signed, and continues past a failure. `--no-install` skips it.

## Consequences

- An agent in a client whose scope names a provider can have that provider's package installed. The install
  is machine-wide: the PATH layer, not the install, decides which identity reaches the CLI. The threat model
  records this as accepted, since the scope is the human's choice in a signed manifest.
- A package manager that is out of scope itself (for example `npm` when `npm` is not in the scope) is hidden by
  the identity's PATH layer and counts as absent.
- Per-identity installs, upgrades of a CLI already present, MCP entries, logins and
  `az extension add` are not part of this change.
