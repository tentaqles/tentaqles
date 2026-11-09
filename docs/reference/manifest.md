# Files and environment

Reference for the files tentaqles reads and writes, and the environment variables it reads or sets. For what they
are for, see [how it works](../how-it-works.md).

## The manifest

`~/.tentaqles/identities/<client>/manifest.yaml` describes one client. `tentaqles reach` writes it;
`tentaqles add` and `tentaqles remove` change it one part at a time and keep every other key and comment. You can
also edit it by hand: run `tentaqles settings render <client>` afterwards. The manifest holds names only. A value
shaped like a token (`ghp_…`, `sk-…`, `AKIA…`, a JWT, `Bearer …`) makes it fail to load.

A manifest that fails to load makes its client unreadable: its terminal gets no identity, and the Claude Code hook
denies every shell command in its folder until it is fixed (`tentaqles doctor` names the error).

### Example

```yaml
schema: tentaqles-client-v2
client: acme
language: en-US
git:
    name: Ada Lovelace
    email: ada@acme.example.com
    provider: github
    user: ada-acme
    blocked_commands:
        - "gh repo delete"
identities:
    claude: {}
    gh: {}
    aws: {}
cloud:
    provider: aws
    also:
        - azure
    blocked_commands:
        - "aws s3 rb"
email:
    provider: gmail
scope:
    - jira
    - slack
browsers:
    - name: chrome
      profile: Profile 2
other_providers:
    - In-house CRM
```

### Keys

| Key | Type | Meaning | Written by |
|---|---|---|---|
| `schema` | string | Always `tentaqles-client-v2`. Any other value fails to load. | reach |
| `client` | string | The client's name. It must equal the client folder's name. | reach |
| `language` | tag | The language Claude writes the client's reports, documents, emails and messages in (see [Languages](#languages)). Empty means `en-US`. | reach, `add language` |
| `git.name` | string | The commit name for the client's folder. | reach |
| `git.email` | string | The commit email for the client's folder. Required. The hook denies a git command whose `-c user.email=` differs from it. | reach, `add email` |
| `git.provider` | string | The git host: `github`, `gitlab`, `bitbucket`, `azure-devops` or `gitea`. It brings its CLI into the client's scope (`gh`, `glab`, `bitbucket`, `azure-devops`, `gitea`). | reach, `add git`, `remove git` |
| `git.user` | string | The GitHub user `gh` must be logged in as. The hook denies a `gh` call made as another user. | by hand |
| `git.expected_user` | string | Stands in for `git.user` when that is empty, on a GitHub client (`git.provider` `github` or empty). | by hand |
| `git.blocked_commands` | list of strings | Command prefixes the hook denies in this client. | by hand |
| `identities` | map of provider id → `{}` | The providers with their own config home under the client's identity folder. Every key must be a catalog id (`tentaqles providers list`). `claude` is always there. | reach, `add`, `remove` |
| `claude.env` | map of strings | Variables `tentaqles settings render` merges into the client's Claude `settings.json` `env`. | by hand |
| `blocked_commands` | list of strings | Command prefixes the hook denies in this client. | by hand |
| `cloud.provider` | string | The primary cloud: `azure`, `aws`, `gcp`, `digitalocean` or `none` (`az`, `gcloud`, `do` and `doctl` are read as their cloud). Its CLI is always in the client's scope. | reach, `add cloud`, `remove cloud` |
| `cloud.also` | list of strings | Further clouds, from the same list, none equal to the primary and none twice. Needs a primary. | reach, `add cloud`, `remove cloud` |
| `cloud.blocked_commands` | list of strings | Command prefixes the hook denies in this client. | by hand |
| `email.provider` | string | The email provider: `gmail`, `outlook` or `none`. | reach, `add mail`, `remove mail` |
| `scope` | list of provider ids | Providers the client uses besides its identities: team, data, agent and task tools, and anything with no config home of its own. Each must be a catalog id, once. | reach, `add`, `remove` |
| `browsers` | list of `{name, profile}` | The client's browser profiles. `name` is `chrome` or `edge`, each at most once; `profile` is the profile's folder name (`Default`, `Profile 2`), not its display name. The first one is used by default. | reach, `add browser`, `remove browser` |
| `other_providers` | list of strings | Names of tools tentaqles has no catalog entry for. Recorded only: tentaqles can neither isolate nor guard them. | reach, `add other`, `remove other` |

The three `blocked_commands` lists are one list for the hook. An entry matches at the start of a command or right
after a separator (`&&`, `;`, `|`, `&`, `$(`, a backtick, a bracket).

The client's scope, which decides what its terminal hides, is: every key of `identities`, every cloud of `cloud`,
the git provider's CLI, the email provider and everything in `scope`. See [providers](providers.md).

An AI coding agent besides Claude Code is one of the catalog's `agent` providers: `codex`, `cline`, `continue`,
`copilot`, `cursor`, `gemini`, `goose` or `opencode`. `reach` and `tentaqles add` write it like a team or data tool:
its id goes into `scope`. An agent with a config home of its own also gets an `identities` entry and a folder there.
Codex has one (`CODEX_HOME`), so a client on Codex reads:

```yaml
identities:
    claude: {}
    codex: {}
scope:
    - codex
```

`claude` is not an agent to add: every client has it.

### Languages

| Tag | Name | Own name |
|---|---|---|
| `ar` | Arabic | العربية |
| `bn` | Bengali | বাংলা |
| `bg` | Bulgarian | български |
| `ca` | Catalan | català |
| `zh-CN` | Chinese (Simplified) | 简体中文 |
| `zh-TW` | Chinese (Traditional) | 繁體中文 |
| `hr` | Croatian | hrvatski |
| `cs` | Czech | čeština |
| `da` | Danish | dansk |
| `nl` | Dutch | Nederlands |
| `en-AU` | English (Australia) | |
| `en-CA` | English (Canada) | |
| `en-IN` | English (India) | |
| `en-GB` | English (United Kingdom) | |
| `en-US` | English (United States) | |
| `et` | Estonian | eesti |
| `fil` | Filipino | Filipino |
| `fi` | Finnish | suomi |
| `fr-CA` | French (Canada) | français (Canada) |
| `fr-FR` | French (France) | français (France) |
| `de-DE` | German | Deutsch |
| `el` | Greek | Ελληνικά |
| `he` | Hebrew | עברית |
| `hi` | Hindi | हिन्दी |
| `hu` | Hungarian | magyar |
| `id` | Indonesian | Bahasa Indonesia |
| `it` | Italian | italiano |
| `ja` | Japanese | 日本語 |
| `ko` | Korean | 한국어 |
| `lv` | Latvian | latviešu |
| `lt` | Lithuanian | lietuvių |
| `ms` | Malay | Bahasa Melayu |
| `nb` | Norwegian (Bokmål) | norsk bokmål |
| `fa` | Persian | فارسی |
| `pl` | Polish | polski |
| `pt-BR` | Portuguese (Brazil) | português (Brasil) |
| `pt-PT` | Portuguese (Portugal) | português (Portugal) |
| `ro` | Romanian | română |
| `ru` | Russian | русский |
| `sr` | Serbian | српски |
| `sk` | Slovak | slovenčina |
| `sl` | Slovenian | slovenščina |
| `es-419` | Spanish (Latin America) | español (Latinoamérica) |
| `es-MX` | Spanish (Mexico) | español (México) |
| `es-ES` | Spanish (Spain) | español (España) |
| `sw` | Swahili | Kiswahili |
| `sv` | Swedish | svenska |
| `th` | Thai | ไทย |
| `tr` | Turkish | Türkçe |
| `uk` | Ukrainian | українська |
| `ur` | Urdu | اردو |
| `vi` | Vietnamese | Tiếng Việt |

The tag is matched without regard to case. Any other value fails to load.

## The marker

`<client folder>/.tentaqles.yaml` makes a folder a client. It holds the client's name and nothing else:

```yaml
# tentaqles workspace marker — names only, never secrets; the manifest lives under ~/.tentaqles/identities/acme/manifest.yaml
schema: tentaqles-marker-v1
client: acme
```

It must be a regular file, its `client` must equal the folder's name, and the manifest it points to must exist. A
marker copied into another folder names the wrong client, so that folder stays neutral. A folder that has the
client's name and its marker, but is not the folder recorded for the client in `config.yaml`, is unreadable:
no identity, and the hook denies shell commands there.

## Other files in a client's folder

- `.gitconfig-tentaqles`: the client's git name and email, with `useConfigOnly = true`, applied to every repository
  under the folder by the `includeIf` rules in `~/.gitconfig-tentaqles`. tentaqles writes it; `tentaqles doctor`
  reports one that is missing or changed by hand.

## `~/.tentaqles`

`$TQ_HOME` moves this folder.

```text
~/.tentaqles/
├── config.yaml                   the registered clients folders (bases:) and each client's folder (clients:)
├── identities/
│   └── <client>/
│       ├── manifest.yaml         the client's manifest
│       ├── claude/               the client's Claude Code config home (CLAUDE_CONFIG_DIR)
│       │   ├── settings.json
│       │   └── .tq-settings-state.json   what tentaqles wrote in settings.json last
│       ├── <cli>/                one config home per identity (gh, az, aws, gcloud, …)
│       └── path-mask/            stubs for out-of-scope CLIs in shared PATH folders
├── shims/
│   └── <client>/                 tq-open, xdg-open, open: open URLs in the client's browser profile
└── providers/                    provider files of your own (tentaqles providers add)
```

`config.yaml` looks like this:

```yaml
bases:
    - /home/ada/clients
clients:
    acme: /home/ada/clients/acme
```

`clients` holds the folder each client was created in. `tentaqles reach` writes it, `tentaqles bind` changes
it after you move a client, `tentaqles settings render` adds it for a client that has none, and
`tentaqles drop` removes it. It holds names and folders, never a secret.

Outside `~/.tentaqles`, tentaqles writes `~/.gitconfig-tentaqles` (one `includeIf` rule per client folder), adds it
to `~/.gitconfig` as an `include.path` and sets `user.useConfigOnly = true` there, and adds its activation block
between `# >>> tq >>>` and `# <<< tq <<<` to your shell profiles. The first change to a profile keeps a copy beside
it (`.tq-backup`).

## Environment variables

### Read

| Variable | Meaning |
|---|---|
| `TQ_HOME` | Moves `~/.tentaqles`. |
| `TQ_ENABLED` | `0` turns the shell hook off in that shell. |
| `TQ_NO_BANNER` | `1` hides reach's banner (as `--no-banner`). |
| `NO_COLOR` | Prints the banner and `settings render` output without color. |
| `CLAUDECODE` | Set by Claude Code for the commands it runs. With it, `reach`, `add`, `remove` and `drop` refuse to run, and `tentaqles run` runs only the current folder's client. It is a convenience check: a session can unset it. |
| `TQ_BIN_DIR` | Where `install.sh` and `install.ps1` put the binaries (default `~/.local/bin`, and `%LOCALAPPDATA%\tentaqles\bin` on Windows). |

### Set in a client's terminal

| Variable | Value |
|---|---|
| `TQ_WS` | The client's name. |
| `TQ_WS_ROOT` | The client's folder. |
| `CLAUDE_CONFIG_DIR` | `~/.tentaqles/identities/<client>/claude`. |
| Each identity's config-home variables | `GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`, `AWS_CONFIG_FILE` and `AWS_SHARED_CREDENTIALS_FILE`, `CLOUDSDK_CONFIG`, `GLAB_CONFIG_DIR` and the rest: `tentaqles providers show <id>` names each one's. |
| `BROWSER` | The client's `tq-open` shim, when the client lists a browser. |
| `PATH` | The PATH without the out-of-scope CLIs (see [how it works](../how-it-works.md#the-path)). |
| `__TQ_STATE` | What tentaqles changed, so leaving the folder restores it exactly. Internal. |

The variables of out-of-scope providers (`AWS_PROFILE`, `AZURE_*` and the like, from each provider's `match.env`) are
unset in a client's terminal and put back when you leave.
