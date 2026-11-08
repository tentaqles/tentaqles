# Providers

For anyone who wants to know which CLIs tentaqles isolates, or who needs to add one it doesn't know. It covers the
provider catalog, what a client's terminal does with each provider, the file format, and how to add your own.

A client's terminal points each CLI at a config home of the client's own, by setting that CLI's own variables
(`GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`, ...), and hides the CLIs of the providers the client doesn't use. Which CLI,
which variables, how to install it, how to log in and what to hide is data: the provider catalog. How the terminal
applies it is in [How it works](../how-it-works.md).

## Where it lives

- The built-in providers: `internal/providers/catalog/<id>.yaml`, embedded in the binary.
- Your own: `~/.tentaqles/providers/<id>.yaml` (`$TQ_HOME/providers`), written by `tentaqles providers add` or by
  hand.
  - A file with the id of a built-in replaces it, except its `match` lists, which are added to the built-in ones: an
    override can hide more, never less.
  - A file with a new id adds a provider. It may not claim a CLI, subcommand or variable that a built-in lists;
    extend the built-in instead, with a file named for its id.

`tentaqles providers list` shows the catalog and any file that did not load, with the reason.
`tentaqles providers show <id>` prints one provider; `tentaqles providers check` tells which CLIs are installed.
See [commands](commands.md) for their flags.

## Catalog

The built-in providers. "Config home" is the variable a client's terminal sets to move the CLI's login into
`~/.tentaqles/identities/<client>/<id>`; a provider with none has no login of its own to isolate (a web app, or a CLI
that keeps its login in the OS keychain). "generic" marks a tool that talks to whatever the project configures (see
[Generic and ambiguous tools](#generic-and-ambiguous-tools)); "partial" marks a provider whose variable does not move
every credential (its `caveat` says which).

| id | Name | Category | CLI | Config home |
|---|---|---|---|---|
| `aliyun` | Alibaba Cloud | cloud | `aliyun` | none |
| `asana` | Asana | pm | none | none |
| `aws` | AWS | cloud | `aws` | `AWS_CONFIG_FILE`, `AWS_SHARED_CREDENTIALS_FILE` (partial) |
| `az` | Azure | cloud | `az` | `AZURE_CONFIG_DIR` |
| `azure-devops` | Azure DevOps | pm | `az` | `AZURE_CONFIG_DIR` |
| `bitbucket` | Bitbucket | vcs | none | none |
| `claude` | Claude Code | agent | `claude` | `CLAUDE_CONFIG_DIR` |
| `cline` | Cline | agent | `cline` | `CLINE_DIR` |
| `cockroach` | CockroachDB | data | `cockroach` | `COCKROACH_CERTS_DIR` |
| `codex` | Codex CLI | agent | `codex` | `CODEX_HOME` |
| `colab` | Google Colab | other | none | none |
| `continue` | Continue | agent | `cn` | `CONTINUE_GLOBAL_DIR` |
| `copilot` | GitHub Copilot CLI | agent | `copilot` | `COPILOT_HOME` |
| `cursor` | Cursor | agent | `agent` | none (generic) |
| `databricks` | Databricks | data | `databricks` | `DATABRICKS_CONFIG_FILE` |
| `dbt` | dbt | data | `dbt` | `DBT_PROFILES_DIR` (generic) |
| `discord` | Discord | chat | none | none |
| `docker` | Docker | deploy | `docker` | none (generic) |
| `doctl` | DigitalOcean | cloud | `doctl` | `DIGITALOCEAN_CONFIG` |
| `firebase` | Firebase | deploy | `firebase` | none |
| `flyctl` | Fly.io | cloud | `flyctl` | `FLY_CONFIG_DIR` |
| `gcloud` | Google Cloud | cloud | `gcloud` | `CLOUDSDK_CONFIG` |
| `gemini` | Gemini CLI | agent | `gemini` | `GEMINI_CLI_HOME`, `GEMINI_FORCE_FILE_STORAGE` (partial) |
| `gh` | GitHub | vcs | `gh` | `GH_CONFIG_DIR` |
| `gitea` | Gitea / Forgejo / Codeberg | vcs | `tea` | none |
| `glab` | GitLab | vcs | `glab` | `GLAB_CONFIG_DIR` |
| `gmail` | Gmail | email | none | none |
| `google-meet` | Google Meet | chat | none | none |
| `goose` | Goose | agent | `goose` | `GOOSE_DISABLE_KEYRING`, `GOOSE_PATH_ROOT` (partial) |
| `hcloud` | Hetzner Cloud | cloud | `hcloud` | `HCLOUD_CONFIG` (partial) |
| `helm` | Helm | deploy | `helm` | `HELM_CONFIG_HOME` (generic) |
| `heroku` | Heroku | deploy | `heroku` | none |
| `hostinger` | Hostinger | deploy | `hostinger` | none |
| `ibmcloud` | IBM Cloud | cloud | `ibmcloud` | `IBMCLOUD_HOME` |
| `influx` | InfluxDB | data | `influx` | `INFLUX_CONFIGS_PATH` |
| `jira` | Jira | pm | `acli` | none |
| `kube` | Kubernetes | deploy | `kubectl` | `KUBECONFIG` (generic) |
| `linear` | Linear | pm | none | none |
| `linode-cli` | Linode / Akamai | cloud | `linode-cli` | `LINODE_CLI_CONFIG` |
| `mongodb-atlas` | MongoDB Atlas | data | `atlas` | none |
| `neon` | Neon | data | `neon` | none |
| `netlify` | Netlify | deploy | `netlify` | none |
| `notion` | Notion | pm | `ntn` | none |
| `npm` | npm | other | `npm` | `NPM_CONFIG_USERCONFIG` (generic) |
| `oci` | Oracle Cloud | cloud | `oci` | `OCI_CLI_CONFIG_FILE` (partial) |
| `opencode` | opencode | agent | `opencode` | none |
| `outlook` | Outlook | email | none | none |
| `planetscale` | PlanetScale | data | `pscale` | none |
| `postgres` | PostgreSQL | data | `psql` | `PGPASSFILE`, `PGSERVICEFILE` (generic) |
| `pulumi` | Pulumi | deploy | `pulumi` | `PULUMI_HOME` (generic) |
| `railway` | Railway | deploy | `railway` | none |
| `render` | Render | cloud | `render` | `RENDER_CLI_CONFIG_DIR` |
| `scw` | Scaleway | cloud | `scw` | `SCW_CONFIG_PATH` (partial) |
| `sentry` | Sentry | deploy | `sentry-cli` | none |
| `slack` | Slack | chat | none | none |
| `snowflake` | Snowflake | data | `snow` | `SNOWFLAKE_HOME` |
| `stripe` | Stripe | deploy | `stripe` | none |
| `supabase` | Supabase | data | `supabase` | none |
| `teams` | Microsoft Teams | chat | none | none |
| `terraform` | Terraform | deploy | `terraform` | `TF_CLI_CONFIG_FILE` (generic, partial) |
| `trello` | Trello | pm | none | none |
| `turso` | Turso | data | `turso` | `TURSO_CONFIG_FOLDER` (partial) |
| `twilio` | Twilio | deploy | `twilio` | `TWILIO_CONFIG_DIR` (partial) |
| `vercel` | Vercel | deploy | `vercel` | none |
| `vultr-cli` | Vultr | cloud | `vultr-cli` | none |
| `wrangler` | Cloudflare | deploy | `wrangler` | none |
| `zoom` | Zoom | chat | none | none |

Two caveats worth knowing: `aws sso login` tokens stay in the shared `~/.aws/sso/cache`, and the token
`terraform login` stores stays in the shared `terraform.d` folder. `tentaqles providers show <id>` prints every
provider's caveat.

## The scope and the PATH

A client's scope is the providers its manifest names: its `identities`, the clouds of its `cloud` block, its
`git.provider`, its `email.provider` and its `scope` list (what `tentaqles reach` writes; see
[the manifest](manifest.md)). For every provider outside the scope:

- each of its `match.executables` is hidden from the client's PATH. A PATH folder that holds only such CLIs is
  dropped; a folder shared with other tools gets a stub of the same name in the client's mask folder, earlier on the
  PATH. The stub prints `<name>: command not found (outside the scope of identity <client>)` and exits 127.
  `tentaqles run` refuses the name however it is spelled (`aws`, `aws.exe`, its full install path);
- each of its `match.env` variables is unset, unless a provider in scope claims the same variable.

A provider in scope always wins: a CLI it claims, even by one subcommand, stays (`azure-devops` claims `az devops`
and `az repos`, so a client on Azure DevOps keeps `az`).

### Generic and ambiguous tools

A generic tool talks to whatever the project configures: its CLI is not in its own `match.executables` (nor claimed
through `match.subcommands`). In the catalog these are `docker`, `kubectl` (kube), `terraform`, `pulumi`, `helm`,
`npm`, `psql` (postgres), `dbt` and Cursor's `agent`. A generic tool is never hidden, and its variables
(`NPM_TOKEN` for a private registry, `TF_TOKEN_*` for Terraform Cloud) are never unset: they belong to the project, not
to a vendor account.

A few short names that other programs also use are never hidden either, whichever provider lists them: `cn`, `tea`,
`snow`, `render`, `vc`, `fly`, `atlas`, `copilot`, `goose`. The provider's unambiguous names (`flyctl`, `vercel`,
`snowsql`) are still hidden.

## File format

One provider per file; the file name without `.yaml` is the `id`. Unknown keys are refused, with the nearest known
key named.

```yaml
id: gh                         # ^[a-z0-9][a-z0-9-]*$
name: GitHub                   # required
category: vcs                  # cloud, vcs, data, deploy, pm, agent, chat, email, other
docs: https://cli.github.com   # optional

cli:                           # omit, or null, for a provider with no CLI
  command: gh                  # required when cli is set
  version_args: ["--version"]

install:                       # per-OS install hints, shown by tentaqles doctor when the CLI is missing
  windows: { winget: "GitHub.cli", scoop: "gh" }
  macos:   { brew: "gh" }
  linux:   { apt: "gh" }       # also pip, npm, url, note

identity:                      # omit for a CLI with no per-client config home
  env:
    GH_CONFIG_DIR: "{dir}"     # {dir} is ~/.tentaqles/identities/<client>/<id>
  caveat: "..."                # optional: what the variable does not move

login:                         # what tentaqles login <client> <id> runs; command defaults to cli.command
  args: ["auth", "login"]

notes: "..."                   # optional, shown by tentaqles providers show

match:                         # what a client's terminal hides when this provider is outside its scope
  executables: [gh, hub]       # hidden on the client's PATH; no Windows suffix
  subcommands:                 # a claim on part of another provider's CLI
    az: [devops, repos]        # (azure-devops owns these az subcommands, so az stays on the PATH)
  env: [GH_TOKEN, GITHUB_TOKEN, "GH_*"]   # unset in the client's environment; a trailing * is a prefix
```

### Validation rules

A file that breaks any of these does not load; `tentaqles providers list` names it with the reason, and the rest of
the catalog still loads.

- `id` matches `^[a-z0-9][a-z0-9-]*$`; `name` is set; `category` is one of the nine above.
- `cli.command` is set when `cli` is.
- Each `identity.env` key is a valid variable name (`^[A-Za-z_][A-Za-z0-9_]*$`); its value has no control character
  and no `..`.
- The runnable install hints (`winget`, `scoop`, `brew`, `apt`, `pip`, `npm`) contain no shell metacharacter
  (`& | ; ^ < > $` backtick, quotes, line breaks) and no control character. `url` and `note` are exempt: a URL is
  opened, never run, and a note is prose.
- `notes` has no control character.
- Two providers may set the same `identity.env` variable only when they are the same CLI (`az` and `azure-devops`
  both set `AZURE_CONFIG_DIR`, so both log in to one folder).
- A new provider may not list a CLI, subcommand or variable a built-in already lists.

## Adding your own provider

1. Write the file, with the command or by hand:

   ```sh
   tentaqles providers add acme-cli --name "Acme Cloud" --category cloud --command acme \
     --env ACME_CONFIG_DIR={dir} --login "auth login" --docs https://docs.example.com/acme-cli
   ```

   This writes `~/.tentaqles/providers/acme-cli.yaml`. `--force` is needed to replace a file that is already there,
   or to write one with the id of a built-in. `--env` is repeatable; `--version-args` sets `cli.version_args`. Edit
   the file to add `install` hints and `match` lists.
2. Check that it loads: `tentaqles providers list` (a file that did not load is listed with the reason), and
   `tentaqles providers show acme-cli`.
3. Add it to a client: list it under `identities:` in the client's manifest when it has a config home, or under
   `scope:` when it doesn't, then run `tentaqles settings render <client>` so the client's PATH mask is rewritten.
4. Log in once, into the client's own folder: `tentaqles login <client> acme-cli`.

To hide more of a built-in provider, write a file with its id that holds only the extra `match` entries. To report a
provider the catalog should know, open an issue with its CLI, the variable that moves its config home, and its login
command.
