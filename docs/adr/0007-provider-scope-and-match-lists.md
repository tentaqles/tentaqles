# ADR-0007: The provider scope and per-provider match lists: the identity category for every tool, behind `guard: v2`

Status: proposed
Date: 2026-11-05
Cites: PRD v2 §6.1 (reference scenario), §6.2 (hook and filesystem layers), §6.3 (rule order, match lists as data, fixtures), §7.1 (identity category), §7.3 (deny message), §13 (open question: match list coverage and format); DISCOVERY.md §3 (6.2 layer 4, 6.2 layer 5), §7 (match lists: recommend the catalog extension), §9 (Phase 2, flag `guard: v2`); threat model boundary B1 (the agent and the hook); ADR-0005 (the self-protection category, the denial log)

## Context
PRD v2 §6 makes the manifest's provider scope the single source of truth for what an
identity may touch, and asks the hook to deny "everything the first three layers missed:
absolute paths, wrappers, network calls to out-of-scope endpoints, cross-identity paths"
(§6.2), in every tool (§6.4: Bash, Read, Write, Edit, every MCP tool). §6.3 lists what the
identity category must match: "executable names and paths of out-of-scope provider CLIs;
environment variable names and credential file paths of out-of-scope providers; MCP server
names outside scope; hostnames of out-of-scope provider APIs in network calls; file paths
under other identity roots", shipped "as data files per provider so the developer can
extend them", each rule with "a fixture of commands it must allow and deny".

Until now scope was implicit (`IdentityNames()` keys, the git provider, the `cloud` block),
and the hook's identity rules ran on shell tools only: `wrong-cloud` matched the first word
of six cloud CLIs; `env aws`, `sh -c`, `python -m awscli`, `curl` to an endpoint, a Read of
`~/.aws/credentials` and an MCP tool of another provider all passed it (DISCOVERY §3, 6.2
layer 4). Nothing denied a path under another identity's root (6.2 layer 5). §13 left the
match-list format OPEN; DISCOVERY §7 recommended extending the 62 catalog YAML files the
loader already reads.

## Decision
**Scope.** `manifest.(*Manifest).Scope() []string` returns the sorted catalog ids the
identity may touch: `IdentityNames()` (the identities block, or claude and gh for a v1
manifest, plus every cloud of `cloud.provider` and `cloud.also`), the catalog id of
`git.provider` (github → gh, gitlab → glab, azure-devops, bitbucket, gitea; an empty
provider adds nothing), the chat, email and data_platform providers, `agents`, the
providers whose MCP server `claude.bundle.mcp` lists, and an optional new `scope:` list of
catalog ids (providers used without a private config home, such as vercel or terraform).
An existing manifest needs no new field: its scope is what it already declares. `scope:`
entries must be catalog ids.

**The flag.** The v2 rules run only for a **trusted** workspace whose manifest sets the
top-level scalar `guard: v2` (DISCOVERY §9). `guard:` was the block tq 0.9.0 dropped (a
mapping); a mapping is still read as no flag and listed in `Manifest.Ignored`, a scalar
other than `v1`/`v2` fails the load. Without the flag the v1 identity rules behave exactly
as before. **A neutral session (no workspace) does not get the v2 rules**: it has no scope
to compare against, and the v1 `neutral-remote` rule already refuses every remote git, gh
and cloud command there; an untrusted or mismatched workspace is judged by the v1
`untrusted` and `neutral-remote` rules, which are stricter for remote commands.

**The match lists are catalog data.** Each `internal/providers/catalog/<id>.yaml` gains a
`match:` block, and a user file `~/.tentaqles/providers/<id>.yaml` (the protected tq home,
written by a human; the loader already read it) can add to it:

```yaml
match:
  executables: [aws, aws_completer, sam, eksctl]   # names; .exe/.cmd/.bat/.ps1/.com stripped from the word read
  modules: [awscli]                                # python -m <module>
  env: [AWS_ACCESS_KEY_ID, AWS_PROFILE, TF_TOKEN_*]  # a trailing * is a prefix
  paths: ["~/.aws", "%APPDATA%/gcloud"]            # ~/, %APPDATA%/, %LOCALAPPDATA%/; a folder covers its tree
  hosts: [amazonaws.com, api.aws]                  # the host and every subdomain
  mcp: [aws, awslabs]                              # tokens of the MCP server name
```

Six flat lists of literal strings, validated at load (an embedded entry that fails stops
the build's catalog test; a user file that fails is reported by `tq providers` and skipped
by the hook). A user file for a catalog id replaces the entry but its match lists are the
**union** with the embedded ones: an override extends what the guard recognises and never
shortens it; to stop the guard matching a provider, a human puts it in the identity's scope.
A user file with a new id brings a new provider the guard matches. Generic tools (docker,
helm, kubectl, terraform, pulumi, npm, psql, dbt) talk to whatever the project configures:
their command is never matched by name, only their global credential files and vendor
endpoints. cursor's command, `agent`, is too generic a word; `cursor-agent` is matched.

**The rules** (`internal/policy/identity.go`, `policy.Scope`), category identity, deny
only, evaluated after the manifest's `blocked_commands` and the v1 identity rules of
`guard.Decide` and before secrets (§6.3); a provider claimed by an in-scope provider is
never denied because another lists it too (az is both az's and azure-devops's;
graph.microsoft.com is az's, teams' and outlook's):

| Rule | Denies |
|---|---|
| `identity/out-of-scope-cli` | a shell command whose verb (after `sudo`, `env`, `xargs`, `&`, assignments; in `bash -c`, `cmd /c`, `pwsh -c`, `iex`, `eval`, a decoded `-EncodedCommand`) is an out-of-scope executable by name or path; `python`/`py`/`python3.x -m <module>`; a runner's package (`npx`, `bunx`, `pnpx`, `uvx`, `pnpm dlx`, `yarn dlx`, `npm exec`, `pipx run`, `uv tool run`); `Start-Process [-FilePath] x`; an executable spelled with a Windows suffix anywhere in the line (`C:\Program Files\Amazon\AWSCLIV2\aws.exe` unquoted) |
| `identity/out-of-scope-env` | a reference to an out-of-scope variable: `$NAME`, `${NAME}`, `$env:NAME`, `%NAME%`, `env:NAME`, `NAME=...` at a word start, `export`/`unset`/`set`/`setx`/`printenv`/`readonly NAME`, `os.environ['NAME']`, `getenv("NAME")`, `GetEnvironmentVariable('NAME')`, `process.env.NAME`; a bare name in a message is not a reference |
| `identity/out-of-scope-path` | an out-of-scope credential path in a file tool (Read, Write, Edit, MultiEdit, Glob, Grep, NotebookRead, NotebookEdit), any shell word or redirect (placed against a preceding `cd`), a `cd` into it, a mention in the line or inline code (after `~`, `$HOME`, `%USERPROFILE%`, `$env:USERPROFILE`, `%APPDATA%` expansion, MSYS `/c/` spelling), a path string of an MCP input |
| `identity/out-of-scope-host` | an out-of-scope API host in a URL anywhere in a shell line (curl, wget, Invoke-WebRequest, inline code), a bare host argument of a network verb (`curl`, `wget`, `iwr`, `irm`, `http`, `xh`, `ssh`, `scp`, ...), a WebFetch URL |
| `identity/out-of-scope-mcp` | an `mcp__<server>__*` tool whose server name (split on `_ - .`) carries an out-of-scope provider's token and no in-scope provider's |
| `identity/other-identity` | a path under another identity's workspace root (`<base>/<other>`, every registered base) or its folder `~/.tentaqles/identities/<other>`, in any of the routes above |

The deny reads `BLOCKED [identity/<rule>]: <what> is <provider>, outside the scope of
identity <ws> (scope: <ids>). tq denies it in every tool; do not retry another way: tell the
developer ...` (§6.1 step 5, §7.3), and is one line in the denial log with the token it
matched (the executable, variable, path, host or server), never the command line.

**`wrong-cloud` is folded in.** Under `guard: v2`, `guard.Decide` skips `wrong-cloud`
(`Input.Scoped`): `out-of-scope-cli` covers its six CLIs (az, aws, gcloud, gsutil, bq,
doctl are in the catalog's lists) and every other provider. `blocked-command` and the drift
rules are unchanged. `tq/cross-client-mcp` stays as it is.

**Cost.** The catalog is loaded once per process (`providers.MustLoad`); the scope is
compiled once per hook run (once for all files of a Codex patch): one canonicalisation per
path root and per base, one directory read of the identities folder and of each base, one
stat per base child. Variable regexes are compiled only for names the line spells. The
hook run's protected path set memoizes its canonicalisation, so the identity and
self-protection rules resolve each path once. Measured in-process on the Windows dev box: a
Read judged by both categories costs 6.5 ms under v1 and 9.8 ms under v2, a shell line
11.4 ms and 14.0 ms (about 3 ms more per call, the three extra root resolutions).

## Consequences
- The §6.1 scenario is denied at layer 4 by every route the hook sees:
  `aws ec2 run-instances`, `C:\Program Files\Amazon\AWSCLIV2\aws.exe ...`, `python -m
  awscli`, `curl https://ec2.amazonaws.com -H ...`, a Read of `~/.aws/credentials`, an
  `mcp__aws__*` tool; the same identity's az commands and ordinary dev commands (git, go,
  npm, ls, grep in the repo, docker, kubectl, terraform) pass.
  `TestPreToolUse_ScopeReferenceScenario` drives it through the hook;
  `TestScope_OutOfScopeCLI`, `_OutOfScopeEnv`, `_OutOfScopePath`, `_OutOfScopeHost`,
  `_OutOfScopeMCP`, `_OtherIdentity`, `_InScopeWins` are the per-rule allow/deny tables;
  `TestPreToolUse_ScopeRuleOrder` pins identity before secrets;
  `TestPreToolUse_ScopeV1Unchanged` and `TestScopedSkipsWrongCloud` pin v1 parity;
  `TestCatalogMatchLists` checks every catalog file has a valid `match:` block naming its
  vendor CLI. They join CLAUDE.md "Must not trade off".
- **It is a text filter, a speed bump, not a sandbox** (the owner's bound, as in ADR-0005).
  It matches literal spellings in the tool call. Not caught, by design: a path, host,
  variable or command name built at run time (`a=aw; ${a}s`, `$(echo aws)`, string
  concatenation in inline code, `os.path.join(home, '.aws')`); a script file that runs
  `aws` (the hook sees `./deploy.sh`, not its body); an alias or shell function; an
  out-of-scope CLI renamed or copied under another name; an unquoted install path without
  a Windows suffix (`C:\...\aws ec2`), which the tokenizer reads with its backslashes
  dropped; a URL without a scheme outside a network verb's arguments; an endpoint the
  catalog does not list (a custom domain, an IP); a Glob or Grep pattern that reaches a
  credential folder only by its wildcard from an ancestor; a bare variable name read by a
  program from its own environment. The earlier layers (PATH, environment, harness config,
  P2.3) and the identity's private config homes are what make those routes fail.
- False positives are possible and visible: an Azure-only identity is denied `git commit -m
  "use $AWS_PROFILE"` (a real expansion) and `echo https://api.github.com` (a URL in a
  shell line). The message says what matched; the developer adds the provider to `scope:`
  or works from the identity that owns it.
- The lists are a first cut of 62 providers (§13 OPEN, now proposed): executables,
  modules, variables, credential folders, API hosts and MCP tokens as of 2026-11; a vendor
  that adds a host or variable needs a catalog change or a user file.
- `guard: v2` flips only the identity rules; Phase 2's other pieces (PATH, environment and
  MCP layers built from `Scope()`, the ask-path removal, `tq status --denials`) are separate.
- This record changes `internal/guard`, `internal/policy` and `internal/protect` (two
  accessors), trust boundaries of the threat model.

## Alternatives considered
| Option | Why not |
|---|---|
| A separate match-list file per provider (`~/.tentaqles/guard/<id>.yaml`) | A second file per provider beside the catalog entry; the catalog already has one YAML per provider, a loader, a user-override directory and validation |
| Regexes in the match lists | Harder for a developer to extend safely (an unanchored `.*` matches everything); literal lists with a few fixed matching rules (suffix, prefix *, host suffix, path tree, token) cover the cases |
| A user file replaces the match lists outright | A user file that forgets `match:` would switch the guard off for that provider silently; union keeps the embedded lists |
| Deny by executable for every catalog provider | docker, kubectl, terraform, npm and psql are everyday project tools; denying them by name on every identity that did not declare them breaks ordinary work |
| A bare variable name anywhere in the line | Denies `grep -rn AWS_PROFILE` and every commit message mentioning a variable; only references (expansions, assignments, lookups) are matched |
| v2 rules in a neutral session | No scope to compare against: either everything is out of scope (every vendor CLI denied in a plain folder) or nothing is; `neutral-remote` already refuses remote commands there |
| A new rule beside `wrong-cloud` | Two rules for the same AWS command; `wrong-cloud` is the six-CLI subset and is skipped under the flag |

## Round 2 (2026-11-05): precision, fail-closed, and the false-positive cut

A blind critic ran a 108-route attack corpus and a 65-command benign corpus (everyday
Azure work) against round 1 and found 32 missed attacks, 9 false positives and that "in-scope
wins" made scope not provider-specific. This round answers all of it; both corpora are now
clean (0 missed, 0 false positive). The changes:

**Claims are provider-specific, and only embedded data makes a claim.** A provider's CLAIMS
(what being in scope allows) come from the embedded catalog; its DENIES (what being out of
scope blocks) come from the merged catalog (embedded + user files). A user file can only
widen denial, never allow — overriding in-scope `az` to list `aws` does not unlock `aws`.

**Shared CLIs are claimed per subcommand** (the biggest round-1 gap). `match.subcommands`
(a new field: `exe -> [first-subcommands]`) lets one provider own part of a CLI another owns
wholesale: `az` belongs to the az provider; `azure-devops` claims only
`az devops|repos|pipelines|boards|artifacts|login|extension`. The most precise in-scope claim
wins, so an AWS + Azure-DevOps identity runs `az devops ...` but `az vm create` is denied, and
`~/.azure` (dropped from azure-devops; owned by az) is denied too. `tq providers list`
reports every executable / subcommand / path / host / MCP-token overlap between providers, so
nothing is silent.

**New `match` fields:** `subcommands` (above) and `images` (docker/podman image names whose
container is the CLI: `amazon/aws-cli`, `cloudsdktool/google-cloud-cli`). Generic cross-cloud
tools (docker, helm, kubectl, terraform, pulumi, npm, psql, dbt) carry no credential `paths`:
a kubeconfig, an npmrc, a docker config is the developer's own cross-cloud file and stays
readable under any identity (an AKS developer needs no `scope: [kube]`). Their SaaS endpoints
and vendor-specific tokens (Terraform Cloud, Pulumi, dbt Cloud) are still claimed.

**Fail closed and loud.** A workspace whose protected manifest cannot be read (`guard: v3`,
`guard: true`, an unknown `scope:` id, bad YAML) denies every tool in the folder with
`tq/guard-error`, naming the fix, instead of falling back to neutral. An invalid user
provider file under a `guard: v2` identity is likewise a guard error (deny every tool), and
`tq providers list` reports every bad file rather than aborting on the first. Both go through
the hook's one fail-closed path of ADR-0006 (`resolve.Result.Unreadable` for the manifest,
`scopeCatalogError` for the catalog, both answered by `failClosed`), not a second mechanism.
A `guard:` mapping stays a dropped block (v1 rules) and `tq doctor` reports it
(`manifest-dropped-block`).

**Merged onto ADR-0006 (deny only, fail closed, the denial log).** Identity denies go through
that ADR's deny path: category `identity`, layer `hook` (`filesystem` for `other-identity`),
the redacted command and the matched subject. The scope line of every identity deny comes
from one helper, `scopeNote`, which prints `Manifest.Scope()`; `policy.Scope`'s reason no
longer prints the scope itself.

**Text is not code.** The env, host and credential-path text scans run on a masked copy of
the command: grep/rg/findstr/Select-String patterns, `git commit` messages and PowerShell
here-strings (`@'...'@`, `@"..."@`) are blanked, and the bash expansion/assignment scan also
blanks single-quoted spans (no expansion). So `rg 'process.env.AWS_REGION'`, a commit message
mentioning `AWS_PROFILE=prod` or a URL, and a here-string of Go source are text; a real
`$AWS_PROFILE` expansion, assignment or `[Environment]::SetEnvironmentVariable('AWS_PROFILE')`
is still denied.

**More routes matched:** executables behind `wsl`, `find -exec/-execdir`, `cmd /c start`,
`docker run <vendor image>`, `Get-Command <cli>`, `Start-Process`, and a backslash-joined word
(`a\ws`); `python -m` past option values (`-X utf8`, `-W ignore`) and a module named in `-c`
code (`run_module('awscli')`, `import boto3`); env forms `$env:`, `%NAME%`, `Env:NAME` and
`Env:AWS_*`, `[Environment]::SetEnvironmentVariable`, `${#NAME}`, `declare -p`,
`os.environ[...]`, `process.env` destructuring; `tar -C <dir>` resolving a relative word;
`%APPDATA%`/`%LOCALAPPDATA%` expansion in file-tool paths and shell mentions; Glob/Grep
checking `pattern` and `glob` together with `path` (and a recursive search rooted above a
credential tree); and registrable-domain hosts (`googleapis.com`, `githubusercontent.com`).

**MCP exact-allow.** An MCP server is allowed only when its exact name is in the identity's
own `claude.bundle.mcp`; otherwise any out-of-scope token in its name (split on `_ - .`, with
adjacent pairs joined so `google-cloud` → `googlecloud`) denies — `mcp__aws_on_azure__call_aws`
is denied even though `azure` is in scope.

Tests: `TestScope_EveryProvider` (a table over all 62 catalog entries),
`TestScope_SharedCLIPrecision`, `TestScope_UserOverrideCannotAllow`,
`TestScope_MCPExactAllowAndPrecedence`, `TestScope_Mutants` (the seven round-1 mutants),
`TestPreToolUse_UnreadableManifestDeniesAll`, `TestPreToolUse_BadProviderFileDeniesAll`,
`TestPreToolUse_GuardMappingIsV1`, and the expanded fixtures of the round-1 rule tables.

## Round 3 (2026-11-05): API hosts, per-shell quoting, bad provider files

A second review found routes round 2 still let through, everyday commands it stopped, and
that one bad user provider file switched every `guard: v2` workspace off. The decisions:

**Hosts are API hosts, never whole registrable domains** (reverses round 2's
`googleapis.com` and `githubusercontent.com`). A domain that also serves public content
made the guard deny the helm and nvm install scripts (raw.githubusercontent.com), Google
Fonts (fonts.googleapis.com), a public GCS download (storage.googleapis.com) and
`git clone https://gitlab.com/...` for an Azure-only identity. A `hosts` entry may now carry
a path prefix, for a vendor whose API shares a host with public content: gh lists
`api.github.com`, `uploads.github.com`, `github.com/login/oauth/` and
`github.com/login/device/` (the OAuth and device-flow endpoints its CLI logs in through);
glab `gitlab.com/api/` and `gitlab.com/oauth/`; gcloud 26 named `*.googleapis.com` API hosts
(OAuth, STS, IAM, Resource Manager, Compute, GKE, Run, Functions, Build, Artifact Registry,
Secret Manager, KMS, SQL Admin, BigQuery, Pub/Sub, Logging, Monitoring, DNS, App Engine,
Firestore, Spanner, Dataproc, Dataflow, Vertex AI, Service Usage, IAM credentials) plus
`storage.googleapis.com/storage/` and `/upload/storage/` (the GCS JSON API) and `gcr.io`. **A
public content download with no credential is allowed**: it reaches nothing of the
identity, and the variable and credential-file rules still deny a credential sent with it
(`curl -H "Authorization: token $GH_TOKEN" https://raw.githubusercontent.com/...` is
`out-of-scope-env`). Accepted cost: a GCS bucket read by its virtual-host name
(`<bucket>.storage.googleapis.com`) or a regional API host (`us-central1-aiplatform...`) is
not matched; the identity's private config home is what makes those fail without a token.
Overlaps report a host under another provider's host too. Other providers' host lists
(slack.com, sentry.io, oraclecloud.com) were not changed in this round.

**Shared az: the bare invocation and `account`, `config`, `version`.** azure-devops also
claims `az account|config|version` and `az` run with options only (`az --version`, `az -h`),
so an azure-devops identity without `az` runs `az account show` and
`az account get-access-token --resource 499b84ac-...`. The claim is per first subcommand, so
`az account get-access-token` for ARM is allowed too: an account logged in for Azure DevOps
can mint an ARM token. Recorded, not prevented: the subcommand schema has no second level,
and the private `AZURE_CONFIG_DIR` limits what that account is. The first subcommand is now
the first word that is not an option (past the values of `-o/--output`, `--query`,
`--subscription`), so `az --debug vm list` is `az vm`.

**Catalog and route gaps.** Stripe (`STRIPE_API_KEY`, `import stripe`, the
`stripe/stripe-cli` image), the Google Cloud client library (`google.cloud`), cloudflared
(Cloudflare), `git.heroku.com`, `supabase.co` (every project's `*.supabase.co` API), the
`digitalocean/doctl` image, `mcp__gh__*`. The rule of thumb changes: a vendor's SDK modules
and per-project API hosts are listed (an application talking to an out-of-scope vendor is
that vendor's work, and `scope:` is how an identity takes it in). Two new `match` lists:
`psmodules` (Import-Module and the other module verbs: `AWS.Tools`, `AWSPowerShell`, `Az`,
`AzureRM`, `AzureAD`, `GoogleCloud`, a dotted child covered) and `cmdlets` (a command verb:
an exact `Verb-Noun` or `*-<NounPrefix>*`; aws lists `*-AWS*`, `*-S3*`, `*-EC2*` and 15 more
service prefixes no core PowerShell noun starts with, az `*-Az*`, gcloud `*-Gcs*`, `*-Gce*`).
Env reads in other languages: `Deno.env.get('X')`, `Bun.env.X`/`Bun.env['X']`, Ruby
`ENV["X"]`/`ENV.fetch("X")`, Perl `$ENV{X}`. A URL in any string of an MCP tool input
(`mcp__fetch__fetch {"url": ...}`) is judged like a WebFetch. The LS tool is judged like a
Read of its path (identity category only). A path with a quoted segment
(`$env:APPDATA\'GitHub CLI'`) is matched once its quotes are removed. `cmd /c start` checks
every non-switch word (skipping the values of `/d`, `/affinity`, `/node`), so a title, a
wrapper (`start "" cmd /k aws`) or a value switch cannot hide the target. `Start-Process
-FilePath:aws` binds the option at the first ':' (a drive letter in the value stays in the
value). `docker [global options] [container] run` and grep/commit text blanked at its own
span were already right in round 2 (the offset masker of 8ff7b51) and are pinned as tests.

**Per-shell masking, fail closed.** The masker read every line with bash quoting, so in a
PowerShell line `git commit -m "x\" ; curl https://api.github.com/user ; echo \"y"` it
blanked the curl as part of the message (PowerShell ends the string at the second quote; a
backslash is ordinary). It now reads the Bash tool's line with bash rules, the PowerShell
tool's and a decoded `-EncodedCommand` with PowerShell's (a backtick escapes and marks the
word as code, `''` and `""` inside a string are a quote, a here-string runs to `'@`/`"@` at
a line start), and the string of a nested `pwsh -Command "..."` or `bash -c '...'` with the
inner shell's rules. It blanks nothing when a quote, a here-string or a `$(` is left open,
and never a span that crosses a separator the guard tokenizer saw: `guard.SeparatorSpans`
exports the tokenizer's separator offsets, so where the two readers disagree about what is
quoted the span is scanned, not blanked. The single-quote blanking of the expansion scan
follows the same rules.

**A bad user provider file is judged per workspace** (replaces round 2's "deny every call in
every `guard: v2` workspace"). A file that cannot be read, parse, validate or set its
identity variable without colliding is a `providers.FileError` naming the full path, the
first error and the fix (`fix or delete <path>, then run tq providers list`):
- an override of a built-in id contributes nothing: the built-in entry and its lists stay in
  force, and the hook prints the error as a warning on stderr with every answer in a
  `guard: v2` workspace;
- a new id is refused only where the scope names it: the hook denies every call with
  `tq/guard-error` when the identity's `scope:`, identities or git provider names the id, and
  a manifest that names an id whose file does not load says so in its load error (so the
  workspace is denied through the unreadable-manifest path with the file named);
- a new id that claims any entry a built-in provider lists (an executable, a subcommand, an
  image, a Python or PowerShell module, a cmdlet, a variable or prefix, a path or one under
  it, a host or one under it, an MCP token) is refused at load. Round 2 took an in-scope
  provider's claims from the embedded catalog and fell back to the merged entry for an id the
  embedded catalog lacks, so a user file `acme.yaml` claiming `aws` and an identity with
  `scope: [acme]` would have allowed `aws`. To extend a built-in, the file is named for its id.
`tq providers list` prints every bad file and exits non-zero; `tq doctor` reports
`provider-file-invalid` (a warning for an override, an error for a new id). A provider folder
tq cannot list stays a guard error for a `guard: v2` workspace.

This round changes `internal/guard` (the exported `SeparatorSpans`), `internal/policy`,
`internal/providers` and the catalog, `internal/manifest` (the unknown-id hint),
`internal/doctor` and `internal/cmd`.

Tests: `TestScope_Round3Missed`, `TestScope_Round3FalsePositives`,
`TestScope_PerShellQuoting`, `TestMaskTextArgs_FailClosed`, `TestScope_EveryProvider` (now
every entry of every list, hosts through WebFetch and a shell command, `%LOCALAPPDATA%`
paths, exact counts: 57 providers, 584 checks, plus LS and a quoted `%APPDATA%` segment),
`TestScope_UserOverrideCannotAllow` (a new id claiming a built-in's entry, one row per list),
`TestPreToolUse_BadProviderFiles`, `TestProviders_BadFilesReported`.

Records are never edited after acceptance. A changed mind is a new record that
supersedes this one.
