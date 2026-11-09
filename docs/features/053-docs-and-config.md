# 053: Documents and configuration in the graph

**Status** not started · **Stage** Graph · **Entries** G-06 · **Needs** 045, and in fact also 044, 046, 048
and 050 · **Feeds** 057, 061

## Result

After `tentaqles lobe graph build`, a Markdown heading, a configuration key and a Terraform block are nodes of
the repository's graph, each with a file and a line span. Links between documents and code names mentioned in
prose are edges. "Where is the deploy job defined" and "which document describes `only_here`" get a pointer
where today the agent reads a whole README or workflow file. No model is called and no value is stored.

From the sandbox of [002](002-sandbox.md), with the `shop` fixture of the Evaluation section built:

```bash
tentaqles lobe graph find "deploy"
tentaqles lobe graph outline docs/guide.md
```

The first prints a hit `Deploy · heading  docs/ops.md:L1-L1`. The second lists one entry, the heading `Setup`
at `L1`. In `graph.jsonl` the heading `docs/guide.md#setup` has a `references` edge to the function
`f.py#only_here`, because the text under it names that function in a code span.

## Scope

**In**

- The package `internal/lobe/graph/docs`: `markdown.go`, `mentions.go`, `yaml.go`, `json.go`, `terraform.go`,
  `gate.go` (G-06 steps 1 to 8).
- Markdown (`.md`, `.mdx`, `.markdown`, `.qmd`): headings with spans, front matter and fenced blocks skipped,
  links, reference definitions and wikilinks, anchors that reach a heading, code spans as mentions.
- YAML (`.yaml`, `.yml`) and JSON (`.json`, `.jsonc`), configuration only, behind the gates and limits ported
  from Graphify; the dependency entries of `package.json`.
- Terraform (`.tf`, `.tfvars`, `.hcl`): blocks by address, per folder, and references between them.
- The two named exceptions to the lister's dot rule: the `.github/` folder, and the dot-named files of the
  gate lists at any depth the lister reaches (G-02 step 1, G-06 "How to build it").
- New rows in `schema.go`: the kinds `heading`, `key`, `block`, `dependency`; the node key `level`; the
  relation `depends_on`; the rules `doc-link`, `doc-mention`, `tf-ref` and one for a dependency entry; the
  meta key `skipped`.
- The four kinds added to the list of text kinds of [050](050-prompt-injection.md), so a prompt pointer
  prints their names in quotation marks.
- The Terraform parser decision: the `hcl` grammar on the runtime of [042](042-parser-runtime.md), or the
  fallback module `github.com/hashicorp/hcl/v2` with its notice.
- A new extractor version (G-02 step 5, G-08 step 1).

**Out**

- PDFs and images ([061](061-pdfs-and-images.md), G-11). The `documents` setting plays no part here: this
  feature needs only `graph` on.
- TOML, INI and Dockerfiles. `.env` files are never read (G-06 risks).
- Front matter as data, any value of a YAML, JSON or Terraform key, and Markdown sent to a model (G-06).
- Reference nodes for `ADR-n` and `RFC n` mentions. G-07 leaves it open; no feature takes it.
- A separate score for document nodes in the search. R-01 leaves it open; here they are scored like symbols.
- A comparison with Trail-code, which has no such nodes ([057](057-graph-accuracy.md)).

## What it builds on

- [042](042-parser-runtime.md): the written result for `hcl` (an extra with no pass mark), and
  `internal/lobe/graph/parse`.
- [043](043-graph-of-one-repository.md): the lister and its dot rule, the line shapes and key order, ids with
  `~N` on collision, the `ext` line, `tentaqles lobe graph build` and `outline`.
- [044](044-graph-soundness.md): `schema.go`, `Check`, `tentaqles lobe graph check`.
- [045](045-resolution-and-callers.md): the lookup by name that tells whether exactly one function or method
  has a name; the resolve stage, where mentions are resolved last.
- [046](046-find-and-map.md): the search index and `find`.
- [048](048-graph-stays-current.md): the parse cache, the extractor version, the build after a turn.
- [050](050-prompt-injection.md): the pointer line and its list of text kinds.
- [052](052-sql-in-the-graph.md): the meta key `sql`, after which `skipped` is written.
- `gopkg.in/yaml.v3`, already in `go.mod`.

## What it leaves for later features

- The four kinds, `depends_on` and the rules in `schema.go`.
- `"skipped"` on the meta line: one count per format (shape decided here: `{"yaml":N,"json":M}`).
- Heading and key nodes that a lesson's `touches` can name. 055 leaves such entries to the text check.
- `gate.go`, the one place that says whether a YAML or JSON file is configuration. 061 does not use it; a
  later format (TOML) would add its list there.
- The NOTICE entry and license text for `hashicorp/hcl/v2`, only if the fallback is taken.

## How it works

1. **Markdown headings** (G-06 step 1). Front matter (`---` on the first line, closed within 200 lines) has
   no headings. A fenced block (3 or more backticks or tildes; the closer is the same character, at least as
   long) is skipped. A heading line (at most 3 spaces, 1 to 6 `#`, a space, text, a closing `#` run removed)
   is a `heading` node with `level`, under the nearest shallower heading or the file by a `contains` edge.
   Its span stops before the next heading of its level or shallower. The scanner is hand-written, line by
   line.
2. **Links** (G-06 step 2). Each inline link, reference definition and `[[wikilink]]` gives one `references`
   edge per target, from the file: `extracted`, 1, rule `doc-link`. URLs are skipped. A `#anchor` retargets
   the edge to the heading with that slug, if exactly one has it. An edge is written only when its target
   node exists; otherwise an `ext` line.
3. **Mentions** (G-06 step 3). A code span in prose is a mention from its heading, or from the file above the
   first heading. Mentions are resolved last, rule `doc-mention`, relation `references`: with a path
   (`etl/load.py::upsert`) `extracted`, 1; a bare name with exactly one function or method of that name
   `inferred`, 0.95; anything else no edge.
4. **YAML** (G-06 step 4). Only configuration: one of the 29 known file names, a file under
   `.github/workflows/`, or a known top-level key; never OpenAPI or JSON schemas. Each mapping key is a `key`
   node. Limits: 1 MiB, 500 keys, depth 6. The lists and what happens at each limit are ported as data and
   behavior from the lines in "Where to read". Parsed with `gopkg.in/yaml.v3`.
5. **JSON** (G-06 step 5). The same idea and limits; a data file gives no nodes. The scanner is our own and
   accepts comments and trailing commas, as `tsconfig.json` has both. Each entry of a dependency block in
   `package.json` gives an `imports` edge from the file to a `dependency` node.
6. **Terraform** (G-06 step 6). Each block is a `block` node named by its address (`aws_s3_bucket.logs`,
   `module.<name>`, `var.<name>`), per folder. A reference to another address gives `references`, or
   `depends_on` inside that attribute, rule `tf-ref`. The heads `count`, `each`, `self`, `path` and
   `terraform` do not count. An edge is written only when its target exists; otherwise an `ext` line.
7. **Ids** (G-06 step 7). A heading is `<path>#<slug>`: lower case, spaces to `-`, punctuation dropped, `~N`
   on collision. A key is `<path>#<full.key.path>` with `[i]` for list items. A block is
   `tf:<folder>#<address>`.
8. **Nothing but names** (G-06 step 8). For YAML, JSON and Terraform only keys, addresses and lines are kept.
   A test plants `password: hunter2` and `token = "abc123"` and looks for them in every file the build writes.
9. **Untrusted text.** Heading text and keys are cut at 120 characters (proposed) with control characters
   stripped, at extraction. In a prompt block they appear only as pointers, with the name in quotation marks
   (050's rule for text kinds). A heading is never a lesson and never an instruction (principle 5).
10. **The lister.** It descends into `.github/` and picks the dot-named files of the gate lists
    (`.gitlab-ci.yml`, `.travis.yml`, `.pre-commit-config.yaml`, `.eslintrc.json` and the like) by exact name.
    No other dot folder is entered. Files the gates skip are counted under `skipped` on line 1.
11. **Decided in this brief, where G-06 is silent.** A `dependency` node has the id `dep:<name>`, one per
    package name for the repository, with `path` and `span` at its entry in the first `package.json` by
    sorted path; its `imports` edge is `extracted`, 1, rule `pkg-dependency`. A `tf-ref` edge is `extracted`,
    1. A `key` node's `name` is its full key path, and its span runs from the key's line to the last line of
    its value. A `block` node's `path` is the file that declares it. Heading, key, block and dependency nodes
    carry no `sig`.
12. **Hosts.** No hook is added or changed. Both hosts read the same graph; the prompt block of 050 prints a
    heading pointer the same way under Claude Code and Codex.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-06](../prd/lobe-graph.md), all of it; [G-02](../prd/lobe-graph.md) step 1 (the dot rule and its exceptions) and the table of line shapes; [G-12](../prd/lobe-graph.md) step 4; [R-02](../prd/lobe-recall.md) step 6 (the quoted text); [R-01](../prd/lobe-recall.md) risks, the last item; [F-07](../prd/lobe-foundation.md) step 6 (notices); [core page](../prd/vertical-lobe.md) section 3, principles 5 and 6 |
| Source to port: Markdown | [`graphify/extractors/markdown.py` L65](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L65) (the heading pattern), [L71](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L71) (fences), [L109](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L109) (200 lines), [L659](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L659) (no model call) |
| Source to port: mentions | [`graphify/markdown_resolution.py` L208](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/markdown_resolution.py#L208) (with a path, 1.0), [L237](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/markdown_resolution.py#L237) (bare and unique, 0.95) |
| Source to port: gates, limits, Terraform | [`graphify/extractors/yaml_config.py` L74](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/yaml_config.py#L74) (file names; limits at L24, L25 and L71), [`graphify/extractors/json_config.py` L294](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/json_config.py#L294) (data JSON skipped; limits at L95, L169 and L174), [`graphify/extractors/terraform.py` L425](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/terraform.py#L425) (addresses; heads at L21; folder scope at L77) |
| Source: not copied | [`json_config.py` L186](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/json_config.py#L186) (keys merge), [`terraform.py` L32](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/terraform.py#L32) (values stored), [`terraform.py` L242](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/terraform.py#L242) (targets unchecked), [`markdown.py` L341](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L341) (anchors cut) |
| The fallback Terraform parser (web, not machine-checked) | [LICENSE](https://github.com/hashicorp/hcl/blob/main/LICENSE): Mozilla Public License 2.0. [Package page](https://pkg.go.dev/github.com/hashicorp/hcl/v2): the `hclsyntax` parser |
| Lobe code from earlier features | `internal/lobe/graph/schema.go`, `build.go`, the lister in `graph/extract`, `graph/resolve`, `internal/lobe/recall/format.go` (text kinds), `internal/lobe/search` |
| Existing tentaqles code | `go.mod` (`gopkg.in/yaml.v3`); `NOTICE` and `third_party/licenses/`; `test/integration` (`TestNoticeListsEveryPort`) |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: how the lister applies
   its dot rule, where a new extractor plugs into the build, how mentions can run after every other
   resolution, the text-kinds list of 050. The owner approves changes, and the decisions of step 11, before
   code is written.
2. **Decide the Terraform parser.** Read 042's result for `hcl`. If the runtime parses the Terraform fixture
   to the golden blocks, use it. If not, add `github.com/hashicorp/hcl/v2` as an unmodified module, list it
   in `NOTICE` with its license text under `third_party/licenses/`, and measure the growth of the stripped
   binary, which G-01's measure 7 did not count. Write the choice and the figure into Risks.
3. **`markdown.go` and `mentions.go`** (side by side with 4, 5 and 6). Tests: `TestLobeDocsMarkdownFences`,
   `TestLobeDocsMentionAmbiguous`, `TestLobeDocsFrontMatter`, `TestLobeDocsAnchorLink`.
4. **`gate.go` and `yaml.go`**, with the ported lists as data files. Tests: `TestLobeDocsYAMLKeyPaths`,
   `TestLobeDocsLimits`.
5. **`json.go`**: the tolerant scanner, the gate, `package.json` dependencies. Test: `TestLobeDocsJSONGate`.
6. **`terraform.go`** on the parser of task 2. Test: `TestLobeDocsTerraformFixtures`, one golden file for
   both parsers.
7. **Into the build**: the lister's two exceptions, routing by extension, the rows in `schema.go`, ids, the
   meta key, the cut and strip of names, the new extractor version, the four kinds in 050's list. Tests:
   `TestLobeDocsDotEntries`, `TestLobeDocsNoValuesStored`, `TestLobeDocsUntrustedText`,
   `TestLobePromptQuotesTextNodes` with a real heading, `TestLobeGraphIncrementalEqualsCold` and
   `TestLobeGraphDeterministic` with document files in their fixture.
8. **Docs.** A paragraph on documents and configuration in the `tentaqles lobe graph` section of
   `docs/reference/commands.md`; one line in `CHANGELOG.md` under `[Unreleased]`; `NOTICE` if task 2 took the
   fallback. Every ported Go file starts with its `// Ported from` line. The plugin does not change.
9. **Tester run** of the Evaluation section by a fresh agent.

## Tests that stay in the repository

- `TestLobeDocsMarkdownFences`: `Setup` with a closing `#` run, `C#`, and a fenced heading line give 2
  headings.
- `TestLobeDocsMentionAmbiguous`: two classes `Widget`; a bare mention gives 0 edges, a mention with a path
  gives 1.
- `TestLobeDocsYAMLKeyPaths`: a workflow with two jobs, each with named steps, gives 0 duplicate ids.
- `TestLobeDocsNoValuesStored`: `password: hunter2` and `token = "abc123"` appear nowhere in the graph bytes,
  the parse cache or the index.
- `TestLobeDocsTerraformFixtures`: the Terraform fixture gives its golden blocks and references with
  whichever parser the build uses.
- `TestLobeDocsDotEntries`: `.github/workflows/ci.yml` and a root `.gitlab-ci.yml` give key nodes; a
  workflow-shaped file under `.cache/` gives 0 nodes.
- New names in this brief: `TestLobeDocsFrontMatter` (a `#` line inside front matter is no heading; front
  matter not closed within 200 lines is body), `TestLobeDocsAnchorLink` (a link with an anchor one heading
  has targets the heading; with two such headings it targets the file), `TestLobeDocsLimits` (a file of 1 MiB
  plus 1 byte gives 0 nodes and 1 under `skipped`; 600 keys and depth 8 stop at the ported limits),
  `TestLobeDocsJSONGate` (a data file gives 0 nodes; a `tsconfig.json` with a comment and a trailing comma
  parses; two dependency entries give 2 `imports` edges and no version text),
  `TestLobeDocsUntrustedText` (a heading of 300 characters with a control character is stored as 120
  characters without it).
- From earlier features, kept green: `TestLobeGraphSoundFixtures` with a document fixture,
  `TestLobeResolveEveryEdgeHasRule`, `TestLobeGraphOffsetsAndCap`, `TestLobePromptBudget`.
- House rules: `TestLobeGuardNeverImportsLobe` (no `init`, no package-level call, also in the fallback
  module's use), `TestLobeImportsOnlyTheFour`, `TestNoticeListsEveryPort`, a `CGO_ENABLED=0` build for the
  six targets, and every test under "Must not trade off" in `CLAUDE.md`, unedited.

## Evaluation

A fresh tester agent gets the built `tentaqles` first on `PATH`, an empty working folder and this section.
Use bash (Git Bash on Windows). No network. The standard setup:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
```

If a line starts with `SETUP FAIL`, stop and report FAIL. Then the fixture:

```bash
S="$W/clients/acme/shop"; I="$TQ_HOME/identities"; git init -q "$S"; cd "$S"
mkdir -p docs infra .github/workflows
printf 'def only_here():\n    return 4\n' > f.py
printf '## Deploy\n' > docs/ops.md
printf '%s\n' 'variable "region" {}' 'resource "aws_s3_bucket" "logs" {' '  token = "abc123"' \
  '  bucket = var.region' '}' > infra/main.tf
printf '%s\n' 'jobs:' '  build:' '    steps:' '      - name: one' > .github/workflows/ci.yml
printf '%s\n' '## Setup ##' 'See `only_here`.' '[ops]: ./ops.md#deploy' '~~~' '# not a heading' '~~~' \
  > docs/guide.md
printf '{"dependencies":{"left-pad":"1.3.0"}}\n' > package.json
printf '[{"a":1},{"a":2}]\n' > data.json
tentaqles lobe graph build || echo "SETUP FAIL build"
J="$(ls -d "$I"/acme/lobe/graph/r_*)/graph.jsonl"
ask() {  # ask <host> <session> <prompt>: the prompt hook under acme's login for that host
  v=CLAUDE_CONFIG_DIR; [ "$1" = codex ] && v=CODEX_HOME
  printf '{"session_id":"%s","cwd":"%s","prompt":"%s"}' "$2" "$S" "$3" |
    env "$v=$I/acme/$1" tentaqles lobe-hook "$1" user-prompt-submit; echo " exit=$?"; }
```

`J` is the graph file of `shop`; there is one folder under `graph/`. Run every check in `$S`.

1. One line of `J` contains
   `"id":"docs/guide.md#setup","name":"Setup","kind":"heading","path":"docs/guide.md","span":[1,6]`.
2. One line contains `"source":"docs/guide.md","relation":"references","target":"docs/ops.md#deploy",`
   followed directly by `"confidence":"extracted","score":1,"rule":"doc-link"`.
3. One line contains `"source":"docs/guide.md#setup","relation":"references","target":"f.py#only_here",`
   followed directly by `"confidence":"inferred","score":0.95,"rule":"doc-mention"`.
4. One line contains `"id":".github/workflows/ci.yml#jobs.build.steps[0].name"`, and one contains
   `"source":"tf:infra#aws_s3_bucket.logs","relation":"references","target":"tf:infra#var.region"`.
5. `grep -c -E 'not a heading|abc123|1\.3\.0' "$J"` prints `0`.
6. One line contains `"id":"dep:left-pad"` and `"kind":"dependency"`, and one contains
   `"source":"package.json","relation":"imports","target":"dep:left-pad"`.
7. Line 1 of `J` contains `"skipped":{` with `"json":1` inside it, and no line of `J` contains `data.json#`.
8. `tentaqles lobe graph check --json --strict`: exit 0 and `"ok":true`.
9. `tentaqles lobe graph find "deploy"`: exit 0 and one hit line contains `Deploy · heading` and
   `docs/ops.md:L1-L1`.
10. `tentaqles lobe graph outline docs/guide.md`: exit 0 and exactly 1 entry; it contains `Setup` and `L1`.
11. `sha256sum "$J"`, then `tentaqles lobe graph build` again, then `sha256sum "$J"`: the same hash.
12. A prompt pointer quotes the heading. `ask claude d1 "what does the Deploy heading in ops say"`: ` exit=0`;
    `additionalContext` contains `docs/ops.md:L1` and `\"Deploy\"`, the name inside quotation marks as JSON
    escapes them. `ask codex d2 "what does the Deploy heading in ops say"`: ` exit=0` and the output contains
    `docs/ops.md:L1`.
13. `ls "$I"/globex/lobe/graph 2>/dev/null | wc -l` prints `0`, and
    `(cd "$W/clients/globex/app" && tentaqles lobe graph find "deploy"; echo "exit=$?")` prints `exit=1` and
    no `ops.md`.

There are no person checks. PASS needs 13 of 13. "Could not run" is a FAIL. The tester reports anything
surprising and fixes nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems with `CGO_ENABLED=0`).
- [ ] The tester brief passes, 13 of 13.
- [ ] The house rules hold: `TestLobeDocsNoValuesStored`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, `TestNoticeListsEveryPort`, and `tentaqles lobe bench leak` at zero leaks.
- [ ] The Terraform parser choice is written down; if it is the fallback, `NOTICE` and the license text are
  in place and the binary growth is recorded.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a heading found by a word and a document linked to the function it names.

## Risks and unknowns

- **The Terraform parser** (G-06). Unknown until task 2. If the fallback is taken, `hashicorp/hcl/v2` is the
  graph's only MPL-2.0 dependency and must stay unmodified: a patched copy in the tree would bring MPL duties
  on those files. Its license facts come from web pages that were not machine-checked.
- **Decided in this brief, not in the requirements** (step 11): the dependency node's id, place and rule; the
  label of a `tf-ref` edge; a key's `name` and span; the shape of `skipped`; no `sig` on these nodes. The
  owner approves them at task 1.
- **The gates are name and key lists.** An unknown format is skipped, and the `skipped` counts are the only
  sign (G-06 risk).
- **A bare mention such as `run`** resolves whenever one function has that name; the share of wrong mention
  edges is not measured yet (G-06 risk). The edge is `inferred`, 0.95, so a reader can discount it.
- **An `[i]` id shifts** when a list item is inserted, so a lesson that `touches` it can go stale (G-06 risk).
  055 leaves entries in such files to the text check.
- **Search rank.** Document nodes are scored like symbols (R-01 leaves this open). A repository with large
  documents may push headings above code for a plain-word query; M-01 and the first real use will show.
- **Planted text.** Anyone who can commit a document can word a heading to steer the agent. It reaches a
  prompt only as a quoted, capped pointer name, never as a lesson.
- **The number** 120 characters and the score 0.95 are proposed or ported; they stay as the requirements
  mark them.
