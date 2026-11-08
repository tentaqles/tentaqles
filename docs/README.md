# Tentaqles documentation

Every page of the docs, by what you need. New here? Start with [Getting started](getting-started.md).

## Start

- [Getting started](getting-started.md): install, set up a first client, log in, and check that it works.

## Understand

- [How it works](how-it-works.md): how a folder becomes a client, what a client's terminal carries, git, Claude Code,
  the language and the browser profiles.
- [Threat model](threat-model.md): what tentaqles protects against, what it doesn't, and the tests that hold each
  promise.

## Look up

- [Commands](reference/commands.md): every command, its flags and examples.
- [Manifest and files](reference/manifest.md): every manifest key, the files under `~/.tentaqles`, the environment
  variables.
- [Providers](reference/providers.md): the catalog of CLIs tentaqles isolates, and how to add your own.
- [Claude Code hook](reference/claude-hook.md): what the plugin's hook checks, the reasons it blocks, and the
  session-start preamble.

## Fix

- [Troubleshooting](troubleshooting.md): common symptoms, and every `tentaqles doctor` finding with its fix.

## Maintain

- [Releasing](RELEASING.md): how a release is cut, published and pinned.
