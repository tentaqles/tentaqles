# Lobe: Recall (R-01 to R-06)

Recall brings stored knowledge back at the moment it applies, in as few characters as possible. This page follows
the [core page](vertical-lobe.md). R-01 ranks code in the graph. R-02 adds a small block to each prompt from that
ranking and from lessons. R-03 gives the agent four query commands over the same ranking, with an MCP server that
is optional and off by default. R-04 matches a lesson's `when` block just before a tool runs and right after a
call fails; it owns the trigger matcher, the lesson line and the session record that R-02 and R-05 reuse. R-05
prints a short brief at session start, and R-06 marks a lesson whose code is gone. R-04, R-05 and R-06 ship in
phase 2 and need no graph; R-01, R-02 and R-03 ship in phase 4.

## R-01 Search ranking

**Phase** 4 · **Origin** port: Trail-code · **Depends on** F-01, G-02, G-03, G-08

**What it is.** One function that takes a query in plain words and returns a short ranked list of graph nodes, each
with a file and line span. A structural question ("who calls X") is answered from the graph's edges. Other queries are
scored on name, path and text, then adjusted by a graph rank: how closely a node is wired to the other matches.

**Why it matters.** It serves "fewer tokens": one call returns the right function instead of several file reads. R-02
injects its top hits and R-03 prints them, so a wrong order wastes tokens in both.

**Mechanism**

1. Tokenize: split camelCase and non-alphanumeric characters, lowercase, drop tokens under 2 characters and Trail's 32
   stop words. A repeated term counts once.
2. Structural route: the query contains, in any letter case, `who calls`, `what calls`, `callers of`, `called by` or
   `used by` (incoming), or `callees of`, `what does <name> call`, `what does <name> import` or `depends on`
   (outgoing). Resolve the query's words longest first, by whole symbol name with case ignored, then by last
   dot-part. Return up to `limit` neighbors over `calls`, `references`, `implements` and `extends` (outgoing adds
   `imports`), sorted by path and line, with `mode: structural`.
3. If no word resolves, continue at step 4 and name the longest word in `note`. If the symbol has no such edge,
   continue at step 4 and name the symbol.
4. Lexical score per node: `3*S(name) + 2*S(path) + BM25(body)`. `S` sums `tf*idf` over the query terms, with `idf` as
   `ln(1 + N/(1+df))` over the `N` scored nodes. BM25 uses `k1 = 1.2`, `b = 0.75`. Body is the node's signature
   and the terms G-08 counts from its bytes in the file; there is no summary written by a model.
5. Divide by the top score. Run personalized PageRank over the five relations, undirected: the seeds are these
   scores as shares of their sum, the walk starts from the seeds, teleport is 0.25, a node with no edge returns its
   mass to the seeds, there are exactly 25 iterations, and the result is divided by its maximum.
6. Candidates: nodes with a lexical score, plus nodes whose rank is at least 0.15. Final score: `(lexical + 0.5 *
   rank) * factor`; the factor is 0.35 for a test path, and 1 otherwise or when the query names tests.
7. Sort by final score, then name; keep 2 hits per file at most (proposed). Return `limit` hits and two shares of the
   query's idf weight in the top hit: `coverage` (name, path or body) and `coverage_strong` (name; 0 for a file).

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/ask/ask.ts` L940](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L940): name weight 3, path weight 2 and BM25 body on the next two lines; [L229](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L229): the idf formula; [L266](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L266): `k1 = 1.2`, with `b = 0.75` on L267; [`src/ask/index-file.ts` L38](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/index-file.ts#L38): the tokenizer, stop words on L28 |
| Trail-code (MIT) | port | [`src/ask/graphrank.ts` L142](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/graphrank.ts#L142): teleport 0.25, 25 iterations on L143; [`src/ask/ask.ts` L989](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L989): the blend with weight 0.5 (L417) and the 0.15 floor (L423, used on L970); [`src/graph/relations.ts` L14](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/relations.ts#L14): the five relations; [`src/ask/ask.ts` L457](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L457): name-only coverage, 0 for a file node |
| Trail-code (MIT) | port, changed | [`src/ask/ask.ts` L305](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L305): the intent pattern has no `i` flag and includes bare `uses`; [L943](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L943): the 0.35 factor (L208) on the raw score, applied again on L989; [`src/ask/graphrank.ts` L54](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/graphrank.ts#L54): one neighbor entry per edge; [`src/ask/ask.ts` L522](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L522): the words that count as naming tests |
| tentaqles (new) | our design | Intent phrases that ignore case, one test factor, the cap of 2 hits per file |

**How to build it**

- Package `internal/lobe/search` (`tokenize.go`, `index.go`, `score.go`, `pagerank.go`, `structural.go`). `Search(ix
  *Index, query string, opt Options) Result`; `Options{Limit int; In string; Deadline time.Time}`; `Result{Mode
  string; Hits []Hit; Coverage, CoverageStrong float64; Note string}`; `Hit{Name, Kind, Path string; Start, End int;
  Score float64}`. `Mode` is `structural`, `lexical` or `empty`.
- `Index` holds per-node term counts for name, path and body, document frequencies, mean body length and adjacency.
  G-08 builds it with the graph, as a cache under `graph/<repo-key>/` (format: F-01). An index whose build id differs
  from the graph's is not used. `In` filters nodes by path prefix, by component, before scoring.
- The index also carries G-03's three lookups: by name; by file and name; by owner and name. Inside a hook nothing
  loads `graph.jsonl`: every hook that needs a node, this search included, is served by the index.
- Differences from Trail. (a) Intent phrases ignore case and omit bare `uses` and `import`: in Trail "What calls X"
  silently goes lexical and any prose with "uses" goes structural. (b) One test factor, applied last; Trail applies it
  to the raw score, after normalizing and on its file score (`src/ask/file-rank.ts` L204). (c) A self-loop is dropped
  and repeated edges count once. (d) Trail's per-file score and top lock are not ported; the cap of 2 per file stands
  in. (e) An acronym splits from the next word: `HTTPResponse` gives `http`, `response`.
- When `Deadline` passes before step 5, return the lexical order without the graph rank.

**How we know it works**

- `TestLobeSearchIDF`: `df = 9` and `N = 100` give 2.398 within 0.001.
- `TestLobeSearchBM25`: `tf = 1`, mean-length body, `idf = 1` gives exactly 1.0; `tf = 3` gives 1.571 within 0.001.
- `TestLobeSearchPageRankPair`: two nodes, one edge, seed on A: B divided by A is 0.7510 within 0.0005. The limit of
  the walk is 0.75; 25 iterations from the seed stop at 0.750988.
- `TestLobeSearchDefinitionAboveTest`: for "parse span", `ParseSpan` ranks above `TestParseSpan`: yes or no.
- `TestLobeSearchStable`: 100 runs of one query on one index give byte-identical output: yes or no.
- `TestLobeSearchSpeed`: on a 50,000-node index the 95th percentile is at most 100 ms (proposed).

**Tester brief**

Start from the standard tester setup in F-07. Then run:
```sh
cd "$W/clients/acme/app" && mkdir -p lib app && printf 'module example.com/fix\n' > go.mod
printf 'package lib\n\nfunc ParseSpan(s string) (int, int) { return 0, 0 }\n' > lib/span.go
printf 'package lib\n\nimport "testing"\n\nfunc TestParseSpan(t *testing.T) { ParseSpan("L1") }\n' > lib/span_test.go
printf 'package app\n\nimport "example.com/fix/lib"\n\nfunc LoadA() { lib.ParseSpan("L1") }\n' > app/a.go
tentaqles lobe graph build
```

1. `tentaqles lobe graph find "who calls ParseSpan" --json`: exit 0; `mode` is `structural`; exactly 2 hits, `LoadA`
   (`app/a.go`) then `TestParseSpan` (`lib/span_test.go`).
2. `tentaqles lobe graph find "What calls ParseSpan" --json`: the same `mode` and the same hits as check 1.
3. `tentaqles lobe graph find "parse span" --json`: `mode` is `lexical`; the first hit is `ParseSpan` in
   `lib/span.go`; `TestParseSpan` comes after it.
4. `tentaqles lobe graph find "who calls NoName" --json`: exit 0; `mode` is not `structural`; `note` has `NoName`.

**Risks and open questions**

- The constants were tuned on Trail's benchmarks; M-01 and M-07 must confirm them on ours.
- A hook loads the index, never `graph.jsonl`, and must answer inside R-02's cut-off. Trail's comment puts loading
  its code map at about 0.4 s on a big repository (`src/notes/stale.ts` L12). F-01's index format decides whether
  we fit.
- Document and configuration nodes (G-06) are scored like symbols; Trail scores its notes separately. Open.

## R-02 What is injected with each prompt

**Phase** 4 · **Origin** port: Trail-code · **Depends on** F-02, F-03, F-05, F-06, R-01, R-04, R-06

**What it is.** When the person sends a prompt, the lobe may add one small block of context before the agent starts:
up to 2 lessons the prompt triggers and up to 3 pointers to matching code. A pointer is a name, a file and a line
span, never the code itself. Most prompts get nothing.

**Why it matters.** It serves "fewer tokens" and "fewer errors": the agent starts from the right file and from what
went wrong before. Injected text is paid for on every turn, so this entry owns a hard budget.

**Mechanism**

1. The hook (`tentaqles lobe-hook claude user-prompt-submit`) prints nothing when F-02's client check fails (the
   folder resolves to no client, or the session's login folder is not that client's), `recall` is off, the prompt
   starts with `<task-notification>`, or the trimmed prompt is under 3 characters.
2. Lessons by words: each eligible lesson (R-04 step 1) is tested with R-04's whole-word matcher on `when.words`;
   order by phrases found, then `strength`, then newest `created`.
3. Code pointers, when `graph` is on, the prompt has 12 or more characters and an index is usable: `Search(prompt,
   Limit: 3)`. A lexical result shows only when `coverage_strong >= 0.1` or `coverage >= 0.5`; a structural one
   always. A weaker match prints nothing: no pointer, and no line that tells the agent to go and search.
4. Lessons by file: an eligible lesson whose `touches` names a file among those hits joins after step 2's.
5. Drop what the session record (R-04) says was already shown and move stale lessons (R-06) behind the fresh
   ones; only then cut to 2 lessons and 3 pointers.
6. Format: the lessons inside F-05's frame, each as R-04's lesson line, with R-06's note when stale; then
   `[tentaqles lobe] Code that matches this prompt:` and per hit `<n>. <name> · <kind>  <path>:L<start>-L<end>`
   plus a snippet of at most 140 characters. The snippet of a note or document node (G-06, G-07) is text someone
   wrote, not code: it prints in quotation marks with control characters removed. The block is at most 1,500
   characters, frame included (proposed, about 375 tokens at 4 characters each); pointers drop from the end until
   it fits.
7. Print `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"<block>"}}`, exit 0; with
   nothing to say, print zero bytes. Then record what was printed: the newest 40 lesson ids and 40 pointers.
8. Time: the hook is in the inject class of core page 5.2: 300 ms at the 95th percentile (proposed), cut off at
   1.5 seconds. Step 2 never waits for the search; no step starts after 1.0 s (proposed).

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/claude/prompt-hook.ts` L37](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L37): the 3-character floor; [L32](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L32): 12 characters before the graph is asked; [L229](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L229): 3 hits; [L262](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L262): task notices are skipped |
| Trail-code (MIT) | port | [`src/claude/format.ts` L333](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L333): the gate, with 0.1 and 0.5 from [`src/ask/fuse.ts` L100](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/fuse.ts#L100) and L108; [`src/claude/format.ts` L236](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L236): the 140-character snippet; [L118](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L118): 2 learnings; [L289](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L289): 40 remembered; [L228](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L228): tokens estimated as characters divided by 4 |
| Trail-code (MIT) | weakness, changed | [`src/notes/find-notes.ts` L57](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/find-notes.ts#L57): one title word is enough to show a learning; [L60](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/find-notes.ts#L60): cut to 2 before the seen filter at [`src/claude/format.ts` L132](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L132); [`src/claude/prompt-hook.ts` L285](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L285): the list is forced empty under one of its two product names, against the comment on L278 |
| BASE (FSL-1.1) | idea only | [`src/domain/rules.rs` L974](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L974): a cap that lets words shared with a rule's text raise its rank but never make it fire alone |
| Claude Code hooks reference | web, not machine-checked | <https://code.claude.com/docs/en/hooks>: the host caps `additionalContext` at 10,000 characters |

**How to build it**

- `internal/lobe/recall/prompt.go`: `Prompt(c Client, in HookInput, deadline time.Time) string`, on R-04's
  `trigger.go`, `session.go`, `format.go` and R-01's `Search` in `cwd`'s repository. F-02 owns the host envelope,
  and F-05's `safety.Wrap` writes the frame around the lessons.
- Differences from Trail. (a) A lesson reaches a prompt only by its own `when.words` phrase or a shared file; Trail
  shows a learning when one prompt word is in its title, too loose for a per-turn cost. (b) The seen filter runs
  before the cap; Trail cuts to 2 first, so a third match is never offered. (c) A weak match prints nothing; Trail
  prints up to 2 lines per session telling the agent to run its search (`src/claude/format.ts` L294). A turn with
  no good match costs zero characters here, and R-05's brief names the verbs once. (d) Trail allows 4,000 ms and a
  child process (`src/claude/prompt-hook.ts` L45); ours runs in process under the cut-off of core page 5.2.
- Edge cases: no index, a stale index, `graph: off` or a late search gives lessons only; `enforce: off` or
  `lessons: off` leaves no lesson eligible (R-04 step 1), so only pointers print; an unreadable session record
  counts as empty; a failed write does not stop the print. C-02 shares this hook process; F-02 orders the two.

**How we know it works**

- `TestLobePromptBudget`: 10 matching lessons and 10 hits give 2 lessons, 3 pointers and 1,500 characters at most.
- `TestLobePromptSilentCases`: `ok`, a `<task-notification>` prompt, `recall: off`, a neutral folder, another
  client's login folder: 0 bytes each.
- `TestLobePromptSwitches`: `lessons: off` or `enforce: off` prints pointers and no lesson; `graph: off` prints
  lessons and no pointer: 3 of 3.
- `TestLobePromptWeakMatchSilent`: coverage 0.3 with strong coverage 0.05 prints no pointer: yes or no.
- `TestLobePromptSeenBeforeCap`: 3 matching lessons, one prompt sent twice: the second run shows the third: yes or no.
- `TestLobePromptWholeWords`: `merge into` matches "MERGE INTO t" and not "emerge into": yes or no.
- M-06: the 95th percentile is at most 300 ms on a 50,000-node index (proposed).

**Tester brief**

Start from the standard tester setup in F-07. Then run:
```sh
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; C=acme
cd "$R" && mkdir -p lib "$L/lessons" && printf 'module example.com/fix\n' > go.mod
printf 'package lib\n\nfunc ParseSpan(s string) (int, int) { return 0, 0 }\n' > lib/span.go
tentaqles lobe graph build
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Lesson W" "when: {words: [merge into]}" "Use an alias." "The bare form." \
  "The engine rejects it because it needs an alias."
hook() { printf '{"session_id":"%s","cwd":"%s","prompt":"%s"}' "$1" "$R" "$2" |
  CLAUDE_CONFIG_DIR="$I/$C/claude" tentaqles lobe-hook claude user-prompt-submit; echo " exit=$?"; }
```

`put` writes a lesson file by hand, because `tentaqles lobe lessons add` answers only to a person, and runs C-04's
gate on it first: the `put` line must print `ok`; if it does not, stop and report FAIL. A lesson whose `source` is
`person` enters the ladder at `advice` (E-01), so it needs no `ladder.json`. `hook` sets the login folder that
F-02's client check asks for.

1. `hook s1 "fix the empty input bug in ParseSpan"`: `exit=0`; stdout is one JSON object whose `additionalContext`
   contains `lib/span.go:L3` and has at most 1,500 characters.
2. Run check 1 again: nothing is printed before ` exit=0`.
3. `hook s2 "ok"` and `hook s3 "thanks, that looks good to me"`: nothing before ` exit=0` in both.
4. `hook s4 "write a merge into statement for orders"`: `additionalContext` has `Lesson W` and `not instructions`.
5. `hook s5 "the two branches emerge into one"`: stdout does not contain `Lesson W`.
6. Set `C=globex` and `R="$W/clients/globex/app"` and run check 4's command with session `s6`: no `Lesson W`.
7. Keep `C=globex`, set `R="$W/clients/acme/app"` (acme's folder under globex's login) and run check 4's command
   with session `s7`: nothing is printed before ` exit=0`.

**Risks and open questions**

- The 1,500-character budget, the 1.0 s stop and Trail's coverage floors are untested on our data; M-01 decides.
- Silence on a weak match is the rule here, and it may leave the agent searching by hand. Trail defends its nudge
  with one traced session (`src/claude/format.ts` L284); M-01's counts will show what silence costs.
- That the host passes `additionalContext` on unchanged comes from a web page; phase 0 question 2 should capture it.

## R-03 Query tools for the agent

**Phase** 4 · **Origin** port: Trail-code · **Depends on** F-06, F-07, R-01, G-08, G-09, G-10

**What it is.** Four commands the agent runs in its shell to ask the graph instead of reading files: find code, list
callers, outline a file, map the repository. Answers are short, capped, and carry files and line spans. An MCP
server (tools a host loads from a helper process over the Model Context Protocol) over the same commands is
optional and off by default.

**Why it matters.** It serves "fewer tokens": R-02 only offers pointers, and these verbs are how the agent pulls the
rest on demand. Fewer tools is a goal: each description is paid for in every session, and core page section 11 rules
out dozens.

**Mechanism**

1. `tentaqles lobe graph find <query> [--limit N] [--in <path>] [--all] [--source] [--json]`: R-01's ranking,
   default limit 5 (proposed), highest 20. A hit prints `<n>. <name> · <kind>  <path>:L<start>-L<end>` and one
   signature or summary line of at most 140 characters. `--source` adds up to 30 lines of code per hit (proposed).
   Without `--all` it searches the current repository's graph. With `--all` it follows `graph/links.json` (G-10)
   across the client's repositories, and a hit from another repository carries that repository's `root` (G-10).
   It is a command and has no hook budget; inside a hook nothing crosses repositories (G-09).
2. `tentaqles lobe graph callers <symbol> [--out] [--in <path>] [--json]`: direct edges over R-01's five relations,
   incoming unless `--out`. A hit prints relation, name, pointer and the calling line; 50 at most (proposed). Deeper
   walks belong to G-09.
3. `tentaqles lobe graph outline <file>`: definitions with kind, name, signature and span, by line; 200 (proposed).
4. `tentaqles lobe graph map [--max-dirs N]`: folders with file and symbol counts and nothing else, 16 by default;
   a folder with more than 60% of the files is split one level deeper.
5. Each verb resolves the client from the working folder with `internal/resolve` and reads only that client's
   `graph/<repo-key>/`; `find --all` also reads `graph/links.json` and the graph folders of that same client's other
   repositories, never another client's. Outside a client folder, without a graph, or with `graph` off (F-06) it
   prints one line on stderr and exits 1. `recall` and `enforce` do not switch the verbs off: the agent chooses to run
   them, and the lobe adds nothing by itself. Inside an agent session, when `CLAUDE_CONFIG_DIR` or `CODEX_HOME` names
   another client's identity folder, the verb refuses the same way; unset is allowed. F-07 states this rule for every
   `tentaqles lobe` command that reads or writes a store.
6. Output is capped at 8,000 characters (proposed); a last line counts what was cut: `(N more not shown; narrow with
   --in)`. No advice to the model, no savings line. `--json` gives the same hits in the same order. G-09's
   `tentaqles lobe graph impact` has the same cap; its `--full` lifts the cap for a person.
7. MCP, optional and off by default: `tentaqles lobe graph mcp` serves the same registry over stdio as `lobe_find`,
   `lobe_callers`, `lobe_outline`, `lobe_map` and `lobe_impact` (G-09), checking the client on every call as step
   5 does. Nothing registers it: no render step, plugin file or setting writes an MCP entry, so a client has no
   such server until its person adds that command to the client's host configuration by hand (proposed).
8. A call to one of these verbs, or to a tool of that server, is one of "the lobe's own tools": S-01 records no
   event for a shell call whose program is `tentaqles` or `tq` with first word `lobe`, nor for a tool of the
   optional MCP server. That server's tools all carry the name prefix `lobe_`, which is how S-01 knows them.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/mcp/tools.ts` L46](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L46): the six tool definitions we choose four from; [`src/graph/map.ts` L92](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/map.ts#L92): 16 folders, with the 0.6 split on L97; [`src/ask/ask.ts` L1461](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L1461): the outline entry (name, kind, span, signature); [`src/mcp/server.ts` L93](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/server.ts#L93): tools are listed only when an index exists |
| Trail-code (MIT) | weakness, changed | [`src/mcp/tools.ts` L275](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L275): MCP default 5 hits, against 8 at [`src/cli.ts` L835](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L835); [`src/mcp/tools.ts` L278](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L278): MCP always inlines source; [L147](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L147): MCP omits the call-site line that [`src/graph/traverse-cli.ts` L238](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/traverse-cli.ts#L238) prints |
| Trail-code (MIT) | weakness, not copied | [`src/claude/skill-template.ts` L370](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/skill-template.ts#L370): "already capped", while [`src/graph/traverse.ts` L130](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/traverse.ts#L130) collects every edge and [`src/mcp/tools.ts` L318](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L318) allows an unbounded depth; [`src/claude/format.ts` L364](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L364): the model is told to close its reply with a savings tally; [`src/graph/refresh.ts` L66](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/refresh.ts#L66): a query may wait 90 s for a first build |
| tentaqles (new) | our design | One registry for both surfaces, a cap on every verb, a map without central symbols |

**How to build it**

- The four are actions of F-07's `tentaqles lobe graph` verb, beside `build` and `status` (G-08) and `impact`
  (G-09), so its list of verbs does not grow; `mcp` is one more action. Package `internal/lobe/query`:
  `registry.go` (`Tool{Name, Summary string; Run func(Client, Args) (Answer, error)}`), one file per verb,
  `render.go`, and `mcp.go` for the optional server. The registry holds five tools: this entry's four and
  `impact`, which G-09 specifies. The commands (F-07) and the MCP handlers both call `Run` and `render`, so
  defaults, caps and text cannot differ.
- Differences from Trail. (a) Six tools become four: no regex search (the agent has one) and no freshness check (G-08,
  `tentaqles doctor`). (b) Its two surfaces answer one question differently, as the table shows; one registry ends
  that. (c) Its skill text says every tool is capped, but its caller walk is not; we cap each verb and say what was
  cut. (d) Its output asks the model for a tally; ours asks the model for nothing, and M-01 counts savings in
  `stats/`. (e) Our verbs never build: a missing graph is exit 1 with the fix line `tentaqles lobe graph build`.
- `find --all` uses G-10's flag, the one `tentaqles lobe graph impact --all` (G-09) uses, and is built with
  G-10, last in phase 4; until then the command refuses it. Both follow `graph/links.json` across the client's
  repositories, and neither runs in a hook.
- `map` prints folders and counts only. Trail also lists each folder's most connected symbols; core page section 11
  leaves grouping code into areas and ranking central functions for later.
- No verb writes: none files a lesson, changes a setting or touches the ladder. A lesson's `source` (core page
  5.4) has no value for "proposed by the agent through a tool".
- Edge cases: several matches print under separate headers; an unknown symbol exits 1 and names it; an ambiguous file
  base name lists the candidates and exits 1; `--in` must stay inside the repository.

**How we know it works**

- `TestLobeQueryToolCount`: the registry holds exactly 5 tools: this entry's 4 and G-09's `impact`.
- `TestLobeQuerySurfacesAgree`: for 20 fixed inputs the CLI text and the MCP text are byte-identical: yes or no.
- `TestLobeQueryMCPOffByDefault`: after `tentaqles reach` and `tentaqles settings render`, no file under the
  client's identity folder names `lobe graph mcp`, and the plugin has no MCP entry: yes or no.
- `TestLobeQueryCaps`: a symbol with 500 callers prints 50 hits and a last line that counts 450: yes or no.
- `TestLobeQueryOtherClient`: run in `globex`, no verb prints a path from `acme`'s graph; run in `acme` with
  `globex`'s login folder set, each verb exits 1: 0 leaks.
- `TestLobeQueryFindAll`: with two linked repositories in one client, `find` prints hits from the current one
  only and `find --all` prints hits from both, each foreign hit with its `root`; neither prints a path from
  another client's graph: yes or no.
- `TestLobeQueryNeverBuilds`: with no graph, or with `graph: off`, each verb exits 1 in under 200 ms and creates no
  file: yes or no.

**Tester brief**

Start from the standard tester setup in F-07. Then run:
```sh
cd "$W/clients/acme/app" && mkdir -p lib app && printf 'module example.com/fix\n' > go.mod
printf 'package lib\n\nfunc ParseSpan(s string) (int, int) { return 0, 0 }\n' > lib/span.go
printf 'package lib\n\nimport "testing"\n\nfunc TestParseSpan(t *testing.T) { ParseSpan("L1") }\n' > lib/span_test.go
printf 'package app\n\nimport "example.com/fix/lib"\n\nfunc LoadA() { lib.ParseSpan("L1") }\n' > app/a.go
tentaqles lobe graph build
```

1. `tentaqles lobe graph find "parse span"`: exit 0; the first hit starts `1. ParseSpan` and has `lib/span.go:L3`.
2. `tentaqles lobe graph callers ParseSpan`: exit 0; exactly 2 hits, `LoadA` (`app/a.go`) and `TestParseSpan`
   (`lib/span_test.go`); the `LoadA` hit is followed by a line that contains `lib.ParseSpan("L1")`.
3. `tentaqles lobe graph outline lib/span.go`: exactly 1 entry; it contains `ParseSpan` and `L3`.
4. `tentaqles lobe graph map`: exit 0; one line names `lib` with 2 files and one names `app` with 1 file.
5. `tentaqles lobe graph callers NoSuchName`: exit 1; stderr contains `NoSuchName`.
6. `(cd "$W/clients/globex/app" && tentaqles lobe graph find "parse span")`: exit 1; no `span.go` in the output.
7. `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles lobe graph find "parse span"`, still in acme's
   `app` folder: exit 1; no `span.go` in the output.
8. `grep -rl "graph mcp" "$TQ_HOME/identities" | wc -l` prints `0`: no MCP server is registered for either client.

**Risks and open questions**

- The optional MCP server lives as long as the session, close to the "long-running background service" core page
  section 11 rules out, and its five descriptions cost tokens in every session of a client that adds it. That is
  why it is off by default and needs a config entry the person writes; M-01 should report its cost apart.
- The agent uses only verbs it knows; the one hint is in R-05's brief. M-01 must show they get called.
- The login check in step 5 is a convenience, like the agent marker tentaqles reads today: an agent with a shell
  can unset the variable. The boundary stays the client's own folder and store.

## R-04 Scoped triggers and recall just before a tool runs

**Phase** 2 · **Origin** idea: BASE · **Depends on** F-01, F-02, F-03, F-05, F-06, C-04, S-01, S-08, E-01

**What it is.** Just before a tool runs, the lobe checks the call against each lesson's `when` block, the header part
that says when the lesson is relevant. A match reaches the agent as one short line: at most 2 lessons per call, each
normally once per session. Right after a call fails, openly or as a hidden failure (S-04), a lesson that lists
that exact error is shown at once, in the same form. Blocking is E-01, not this.

**Why it matters.** It serves "fewer errors": the lesson arrives at the one moment it can still change the call, or
the retry. Without triggers every lesson would be shown always, which costs tokens and teaches the agent to skip
them.

**Mechanism**

1. Eligible lessons (one rule for R-02, R-04 and R-05): `recall` is on, `lessons` is on, and the lesson passed C-04,
   is not quarantined (F-05), is not replaced or retired (C-06), and acts at `advice` or `block` after E-01 applies
   the `enforce` cap. `enforce: off` therefore silences lessons everywhere (the brief, the prompt block, the note
   before a call and the one after a failure), and no trial is recorded (E-02). A deny from an approved block
   rule is E-01's and does not need `recall`. A lesson is eligible only where its `applies` says it counts:
   `client` everywhere in the client; `repo` only in the repository named by `repo` (for a shared lesson, the
   repository its file sits in); `path` only there and under a `when.place` entry, which must then be present
   (C-04 checks it). The same limit holds for R-05's brief and for E-01's deny.
2. Nothing is printed when F-02's client check fails (the folder resolves to no client, or the session's login
   folder is not that client's) or nothing is eligible.
3. How a `when` block matches (core page 5.4; this entry owns the matcher). Inside one key any listed value is
   enough. `tool`, `command` and `place` narrow each other: a call matches when every one of those keys that is
   present matches. `command` is read only for shell tools: on any other tool it is not tested, and a block
   whose only key of the three is `command` does not match that call. `fingerprint` matches on its own (steps 7
   and 8). `words` matches on its own and only against a prompt (step 6). C-04's gate refuses a `when` block
   whose only key is a shell tool, because it would match every shell call. `tool`: equals the payload's
   `tool_name` exactly, case included; no prefix, no pattern.
4. `command` (shell tools): cut the text into statements at `;`, newline, `|`, `||`, `&&` and `&` outside quotes, then
   into words, with `internal/lobe/shellwords`; drop leading `NAME=value` words; open wrappers (`bash -c`, `pwsh
   -Command`, `cmd /c`, `wsl --`) 3 levels deep (proposed). `git merge` matches when `git` is the program name
   (folder and `.exe` removed, case ignored) and `merge` is the next word after skipping each `-flag` and its
   value. `git -C /srv/x merge dev` matches; `git mergetool`, `git log --author merge` and `echo git merge` do not.
5. `place`: a touched path under the entry, by component from the repository root: `etl/` matches `etl/load.py`, not
   `etl-old/load.py` or `src/etl/load.py`. Touched: the tool's file argument, a shell tool's working folder, and its
   first 32 words (proposed) with a slash that name a path inside the repository. Paths are compared as text; no
   file needs to exist.
6. `words` (prompts only, R-02): the phrase as whole words, case ignored; letters, digits and `_` are word characters.
7. `fingerprint`, before a call: this session's newest event in the newest events file (tail, 64 KiB, proposed) is
   a `fail` or `hidden-fail` whose `fp` (S-08) the lesson lists. Every call leaves a line (core page 5.4), so a
   later success ends the match.
8. `fingerprint`, after a failure: whatever event carries a failure also carries the answer to it. After a `fail`
   or `hidden-fail` outcome, on Claude Code's `PostToolUseFailure`, on Claude Code's `PostToolUse` (a hidden
   failure, S-04) and on Codex's one post-tool event (S-02), once Sense has appended the event line, the same
   process takes the `fp` S-08 gave that failure and selects the eligible lessons that list it. No other key is
   tested here. What matches goes through steps 9 to 11 and is printed at once, always inside F-05's frame, so
   the agent reads the lesson before it retries. With `capture` off there is no event and no fingerprint, and
   this step does not run.
9. Skip a lesson in the session record, unless its text changed or it matches by fingerprint 10 minutes or more
   later. Steps 7 and 8 share the record: a lesson shown after the failure is not repeated before the next call.
10. Order: fresh lessons before stale ones (R-06); within each group an `avoid` pattern that matches the call
    (E-01), fingerprint, command, place, tool; then `strength`, `seen`, `id`. Keep 2 (proposed).
11. Print as `additionalContext` inside F-05's frame, exit 0, then record. Budget (proposed): 300 characters per
    lesson line and 700 for everything inside the frame; the frame's own fixed text comes on top.
12. Time: before a call the hook is in the inject class of core page 5.2, cut off at 1.5 seconds. After a failure
    the event line is appended first, against the capture budget, and the whole process is cut off at 1.5 seconds.
    A hook that reaches its cut-off prints nothing and exits 0; the event line is already written.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| BASE (FSL-1.1) | idea only | [`src/domain/rules.rs` L300](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L300): a rule declares the moment it matters, one of four kinds; [L864](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L864): a function decides whether one shell statement is the named command and treats the word after a flag as that flag's value; [L832](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L832): a line is cut into statements and wrappers are opened; [L948](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L948): paths are compared by whole segments; [`src/domain/matcher.rs` L208](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/matcher.rs#L208): a phrase must have a non-word character on both sides |
| BASE (FSL-1.1) | idea only, with a weakness | [`src/domain/session.rs` L539](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/session.rs#L539): a rule is due again when never shown, when its text changed, or when a throttle has run out, 10 minutes by default ([`src/config.rs` L682](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/config.rs#L682)); [`src/config.rs` L1399](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/config.rs#L1399): a size budget for output before a tool exists as a setting, and its own comment says no hook reads it |
| Trail-code (MIT) | port | [`src/claude/format.ts` L168](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L168): 2 learnings per tool call; [L203](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L203): the seen filter before that cap; [L141](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L141): wording that frames learnings as never instructions |
| tentaqles (new) | our design | The fingerprint trigger before a call and after a failure, the order in step 10, the 700-character budget |

**How to build it**

- `internal/lobe/recall`: `trigger.go`, `session.go`, `format.go`, `pretool.go`, `postfail.go`. F-03 fills
  `Call{Tool, Command, Cwd string; Paths []string}`. `tentaqles lobe lessons add` gains `--when <key>=<value>` and
  `--touches <entry>` (new flags). R-06's `stale.go` sits in the same package and is built with this entry,
  because step 10 calls it.
- Statements, words and quotes come from `internal/lobe/shellwords`, the lobe's own splitter (core page 5.1 bars
  `internal/guard`), which S-04, C-08 and E-04 share and which is tested against the same cases as the guard's.
  This entry adds no splitter: `trigger.go` holds only what is particular to a trigger (wrappers, flags and their
  values, the program name).
- A `when.command` entry is a program name and up to 3 subcommand words (proposed); C-04's `form` check carries the
  same limit, so `git merge` can be written.
- The hook reads lesson headers from F-01's `index/` cache and opens only the lessons it prints. Whoever files a
  lesson (C-04, F-04's intake) refreshes the cache; a hook never writes it and never walks a repository. The cache
  records each lesson file's size and modification time (proposed). When it is missing, or one listing of
  `lessons/` shows it is out of date, the hook reads the headers from the files this once: the private folder, and
  the shared files `intake.json` (F-04) lists as `ok` for `cwd`'s repository. A lesson written or edited by hand
  is therefore matched without a rebuild. A lesson id is read as `l-` plus 8 to 12 hex characters (core page 5.4).
- After a failure this entry runs last in the process that carries it: `post-tool-use-failure`, or
  `post-tool-use` when the outcome (`sense.Outcome`) is `hidden-fail`, or for Codex `fail`. The order is Sense's
  append, E-02's verdict, then C-08's fix line and the lessons of step 8 in one block. The `fp` is handed over in
  memory, not read back from the log, so a line the duplicate filter (S-06) folded into an earlier one still
  matches. After an `ok` outcome `post-tool-use` prints no lesson.
- Output: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"<block>"}}`, or the same with
  `PostToolUseFailure` or `PostToolUse`, whichever event the process serves, never with `permissionDecision`
  (principle 3). The opening and closing lines are F-05's
  frame, written by `safety.Wrap`; this entry adds no wording of its own around a lesson. Lesson line: `- <title>.
  Do: <Do line 1>. Avoid: <Avoid line 1>. Why: <Why line 1>`, each part without its closing period, the whole line
  cut at 300 characters.
- Session record `sessions/<session>.json`: `{"v":1,"shown":{"<id>":{"at":"<time>","hash":"<line
  hash>"}},"pointers":[],"brief":"<time>"}`, newest 40 ids, written under F-01's lock after the print. C-02, C-05,
  C-08, E-02, F-05 and G-09 keep keys of their own in this file: a writer changes only its keys. When the lock is
  busy (F-01 gives a hook 50 ms) the write is skipped, and the lesson may show once more. From phase 4 the same
  step appends M-01's line to `stats/`.
- Unlike BASE: its folder trigger matches anywhere in a path, ours from the repository root (lesson paths are
  repository-relative); its output before a tool has no working size limit; it throttles every command rule by time,
  we show once per session. An E-01 deny in the same run is the only output. C-08's starter line and E-06's
  findings before a commit print in this block, ahead of lessons and inside its 700 characters. E-05's argument
  notes and G-09's blast list print after an edit, at PostToolUse, in one `additionalContext` of their own under a
  budget of the same size: E-05's notes first, then the list, and the list is cut first. They are not part of
  this block. The 700 characters are the budget inside F-05's frame; the frame adds its own fixed text.

**How we know it works**

- `TestLobeTriggerCommandTable`: 12 cases, the four in step 4 among them, match as stated: 12 of 12.
- `TestLobeTriggerPlaceAndTool`: `etl/` against step 5's three paths gives yes, no, no; `Bash` does not match `bash`.
- `TestLobeTriggerKeysNarrow`: a lesson with `tool: [Edit]` and `place: [etl/]` matches an `Edit` of
  `etl/load.py` and neither a `Write` of it nor an `Edit` of `src/load.py`; one with `command: [git add]` and
  `place: [etl/]` matches `git add etl/load.py` and not `git add src/load.py`; one with only `command` matches
  no `Edit` call; a lesson with a listed `fp` and a `tool` that differs still matches by fingerprint: 7 of 7.
- `TestLobeTriggerApplies`: an `applies: repo` lesson matches in the repository `repo` names and not in a second
  repository of the client; an `applies: path` lesson matches under its `when.place` entry and not beside it;
  an `applies: client` lesson matches in both repositories: 6 of 6.
- `TestLobeTriggerFingerprint`: a listed `fp` on the session's newest `fail` event matches; after a later `ok`, no.
- `TestLobeFailureShowsLesson`: a failure whose `fp` an eligible lesson lists prints that lesson with
  `hookEventName` `PostToolUseFailure`; the same failure again in that session, and a failure with another `fp`,
  print 0 bytes; the event line is in the log each time: 3 of 3.
- `TestLobeHiddenFailureShowsLesson`: a `hidden-fail` outcome whose `fp` an eligible lesson lists prints that
  lesson on `post-tool-use` with `hookEventName` `PostToolUse`, inside F-05's frame; an `ok` outcome prints no
  lesson; a Codex post-tool event that carries a `fail` prints it too: 3 of 3.
- `TestLobePreToolBudget`: 10 matching lessons give 2 lines and at most 700 characters inside the frame, never
  `permissionDecision`.
- `TestLobePreToolEnforceOff`: with `enforce: off` a matching lesson gives 0 bytes before a call and after a
  failure, and `trials.jsonl` gains no line; back at `advice` it prints.
- `TestLobeRecallStageAndSwitches`: a matching lesson at `seen` or `trial`, or with `lessons: off` or `recall:
  off`, gives 0 bytes; at `advice` it prints: 5 of 5.
- `TestLobeRecallOutdatedCache`: a lesson file written after the cache was built is matched, and the hook writes 0
  bytes under `index/`: yes or no.
- M-06: before a call the 95th percentile is at most 100 ms with 500 lessons (proposed). After a failure M-06
  times the append against the capture budget and the whole process against the 1.5 second cut-off.

**Tester brief**

Start from the standard tester setup in F-07. Then run:
```sh
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; C=acme
cd "$R" && mkdir -p etl "$L/lessons"
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Lesson A" "when: {command: [git merge]}" \
  "Use an alias." "The bare form." "The engine rejects it because it needs an alias."
put l-aaaa0003 "Loader edits" "when: {tool: [Edit], place: [etl/]}" \
  "Run the loader test." "An untested change." "The nightly job broke because nobody ran it."
put l-aaaa0004 "Warehouse query" "when: {tool: [mcp__wh__query]}" \
  "Add a row limit." "A full scan." "The bill grew because scans were unbounded."
put l-aaaa0005 "Staging etl files" "when: {command: [git add], place: [etl/]}" \
  "Stage one file at a time." "Staging the whole folder." "A dump was committed because everything was staged."
run() { CLAUDE_CONFIG_DIR="$I/$C/claude" tentaqles lobe-hook claude "$1"; echo " exit=$?"; }
hook() { printf '{"session_id":"%s","cwd":"%s",%s}' "$1" "$R" "$2" | run pre-tool-use; }
T='"tool_name":"Bash","tool_input":{"command":"dbsql -f q.sql"}'; E="the MERGE source needs an alias"
fail() { printf '{"session_id":"%s","cwd":"%s",%s,"error":"%s"}' "$1" "$R" "$T" "$2" | run post-tool-use-failure; }
```

`put` writes a lesson file by hand, because `tentaqles lobe lessons add` answers only to a person, and runs C-04's
gate on it first: each `put` must print `ok`; if one does not, stop and report FAIL. A lesson whose `source` is
`person` enters the ladder at `advice` (E-01), so it needs no `ladder.json`. `run` sets the login folder that
F-02's client check asks for. The four titles share no word. `Loader edits` and `Staging etl files` each carry
two keys that narrow each other (step 3), so one key alone must not show them.

1. `hook s1 '"tool_name":"Bash","tool_input":{"command":"cd etl; git -C /srv/x merge dev"}'`: `exit=0`;
   `additionalContext` contains `Lesson A` and `not instructions` and has at most 900 characters (700 plus F-05's
   frame); no `permissionDecision`.
2. Run check 1 again: nothing is printed before ` exit=0`.
3. `hook s2 '"tool_name":"Bash","tool_input":{"command":"git mergetool && git log --author merge"}'`: nothing is
   printed before ` exit=0`.
4. `hook s3 '"tool_name":"Edit","tool_input":{"file_path":"'"$R"'/etl/load.py"}'`: `additionalContext` contains
   `Loader edits` and neither `Lesson A` nor `Staging etl files`. The same with `etl-old/load.py` and session
   `s4`: nothing. The same with `"tool_name":"Write"`, `etl/load.py` and session `s4b`: nothing, because the
   place matches and the tool does not.
5. `hook s5 '"tool_name":"mcp__wh__query","tool_input":{}'`: `Warehouse query`. With `mcp__wh__query2` and `s6`:
   nothing.
6. `hook s6a '"tool_name":"Bash","tool_input":{"command":"git add etl/load.py"}'`: `additionalContext` contains
   `Staging etl files` and not `Loader edits`. With `git add src/load.py` and session `s6b`: nothing, because the
   command matches and the place does not.
7. Set `C=globex` (acme's folder under globex's login) and run check 1's command with session `s7`: nothing is
   printed before ` exit=0`. Set `C=acme` again.
8. `fail s8 "$E"`: nothing is printed before ` exit=0`. Then
   `FP=$(cat "$L"/events/*.jsonl | grep -o 'fp_[0-9a-f]\{16\}' | tail -1); echo "$FP"` prints `fp_` and 16 hex
   characters: the failure was recorded and fingerprinted.
9. File one more lesson, which lists that fingerprint; `put` prints `ok`:
   ```sh
   put l-aaaa0002 "Alias the merge source" "when: {fingerprint: [$FP]}" "Give the source an alias." \
     "A subquery as the source." "The warehouse rejects it because the source has no alias."
   ```
   Then `fail s9 "$E"`: `exit=0`; stdout contains `PostToolUseFailure`, `Alias the merge source` and `not
   instructions`, and no `permissionDecision`.
10. `fail s9 "$E"` again: nothing is printed before ` exit=0`. `fail s10 "permission denied for table t"`: nothing
    is printed before ` exit=0`.

**Person checks** (for the owner, at a terminal, in the same shell after the checks above)

1. Run the two lines below. They print an id of `l-` plus 8 to 12 hex characters, and the new file under
   `$L/lessons/` holds `source: person` and both `when` entries.
   ```sh
   printf '## Do\nRebase onto main.\n## Avoid\nA merge commit.\n## Why\nIt fails because history is linear.\n' |
     tentaqles lobe lessons add --title "Rebase before a push" --when command="git rebase" --when place=etl/
   ```
2. `hook s11 '"tool_name":"Bash","tool_input":{"command":"git rebase main"}'`: nothing is printed before
   ` exit=0`, because the command matches and no touched path is under `etl/`. Then
   `(R="$R/etl"; hook s12 '"tool_name":"Bash","tool_input":{"command":"git rebase main"}')`, which sends `etl` as
   the working folder: `additionalContext` contains `Rebase before a push`.

**Risks and open questions**

- A lesson with only `words` never fires before a tool: `words` is tested only against a prompt (step 3).
- Step 4 treats the word after any flag as that flag's value, so `git --no-pager merge dev` is missed. A list of
  flags that take no value, per program, would fix it and is not planned.
- Two calls sent at once in one session can both read the record before either writes; a lesson then shows twice.
- Whether a host shows the agent context that a hook adds after a failed call is not confirmed (C-08 has the same
  doubt; phase 0 question 2 captures it). Where it does not, step 7 still brings the lesson before the next call.
- A lesson shown before a call and not followed is not repeated when that call then fails with the listed error,
  unless 10 minutes have passed (step 9). M-04 will show whether that costs repeats.
- When the day's log is full, successes are no longer written (S-07), so step 7 can keep matching after a later
  success. The session record limits that to once per 10 minutes.

## R-05 Session-start brief

**Phase** 2 · **Origin** port: Trail-code · **Depends on** F-01, F-02, F-03, F-05, F-06, R-04, R-06, E-01

**What it is.** When a session starts, the lobe prints a few lines: how many lessons this client has, how many
wait for the person to confirm them, and the lessons that apply to all work in the client and so have no
trigger. It is a separate hook from the identity guard's session preamble and says nothing the preamble already
says.

**Why it matters.** A lesson with no `when` block can never be recalled by R-04, so session start is its only chance.
Everything else stays out: text printed here is carried for the whole session, and the promise is to bring a lesson
back when it applies, not in bulk.

**Mechanism**

1. `tentaqles lobe-hook claude session-start` is its own hook entry and process (core page 5.2), beside the guard's.
2. It prints nothing when F-02's client check fails (the folder resolves to no client, or the session's login
   folder is not that client's), `recall` is off, or the client has no eligible lesson, no lesson waiting (step
   4) and no graph.
3. Standing lessons are the eligible lessons (R-04 step 1) with no `when` block that apply here: `applies: client`, or
   `applies: repo` in that repository. C-04 files a lesson with neither `when` nor `touches` only when its
   `source` is `person` or `correction`. Order: fresh before stale (R-06), then `source` `person` or
   `correction` first, then `strength`, then newest `created`. Keep 5 (proposed).
4. Line 1: `[tentaqles lobe] <N> lessons for this client, <B> can block.` The `<B>` part is left out when B is 0,
   as it always is before phase 3. With a graph (phase 4, `graph` on) it continues: ` Code graph: <K> symbols;
   query it with tentaqles lobe graph find, callers, outline, map or impact.` With a graph and no eligible lesson
   the line starts at `Code graph:`. When `lessons` is on and `enforce` is not `off`, and lessons are waiting for
   the person, one more line follows: `[tentaqles lobe] <W> waiting for you to confirm: tentaqles lobe lessons
   --waiting` (wording proposed). A lesson waits when it cannot earn a hit (it has no `avoid` block and lists no
   fingerprint: most correction lessons, and standing lessons not written by the person) and so stays at its
   entry stage until the person confirms it with `tentaqles lobe lessons confirm <id>` (F-07). E-02 states that
   limit. The line holds a count and a command, never a lesson's text, and `<W>` counts only lessons that
   `applies` lets count here.
5. With standing lessons, F-05's frame follows, holding per lesson `- <title>. <first line of Do>`, cut at 160
   characters, plus R-06's stale note.
6. The brief is at most 1,000 characters, frame included (proposed, about 250 tokens); standing lessons drop from
   the end until it fits. The waiting line is inside that budget and is never dropped. E-07's lines about rules
   that moved since the last brief are inside it too: 3 at most, then a count (proposed). It never prints the
   client's name, git identity, cloud or tools; the guard's preamble has them.
7. The payload's `source` decides repetition. `startup`: print and set `brief` in the session record. `resume`: print
   nothing when `brief` is set. `clear` and `compact`: empty the record's `shown` and `pointers`, because the context
   that held them is gone, and print again.
8. Lessons printed here are added to `shown`, so R-02 does not repeat them.
9. The text goes to F-02's writer, which owns each host's envelope, and the hook exits 0. It never loads the graph.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port, cut down | [`src/claude/format.ts` L411](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L411): 10 standing learnings at session start; [L421](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L421): the person's own first, then newest; [`src/notes/flatten.ts` L36](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/flatten.ts#L36): one line of 160 characters by default |
| Trail-code (MIT) | weakness, not copied | [`src/claude/hooks.ts` L578](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hooks.ts#L578): session start joins five parts into one string; [`src/claude/format.ts` L355](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L355): a fixed directive on using its tools, ten long lines; [L368](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L368): plus the first 1,500 characters (the default on L345) of a repository map; [`src/notes/find-notes.ts` L41](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/find-notes.ts#L41): the per-prompt path does not leave standing learnings out, so one can repeat |
| tentaqles (this repository) | constraint | `internal/cmd/claudehook.go` L133: `renderSessionStart` builds the guard's preamble, which opens with `Client:` (L142); `plugin/hooks/hooks.json` L8: the guard's session-start entry, 15 s timeout |
| Claude Code hooks reference | web, not machine-checked | <https://code.claude.com/docs/en/hooks>: SessionStart `source` is `startup`, `resume`, `clear` or `compact`; the research digest also lists `fork`, which step 7 treats as `startup` |

**How to build it**

- `internal/lobe/recall/brief.go`: `Brief(c Client, in HookInput, deadline time.Time) string`. `<N>` counts the
  eligible lessons, `<B>` those acting at `block` and `<W>` those waiting (step 4), the same set
  `tentaqles lobe lessons --waiting` lists; all three come from F-01's `index/` cache (R-04) and `ladder.json`,
  so the brief never walks a repository. `<K>` comes from the `summary.json` G-08 writes beside the graph of `cwd`'s
  repository. Without that file, with a build id that differs from the graph's, or with `graph` off, the graph
  sentence is left out.
- Differences from Trail. Its session start carries a fixed directive, up to 10 standing learnings and 1,500
  characters of repository map; the research digest puts it at 5,000 to 7,500 characters, estimated by hand, not
  measured. Ours is capped at 1,000 and holds no directive. Trail does not record standing learnings as shown; we do.
- Not delaying the guard: a slow or broken brief cannot change the preamble's text or exit code, because it is another
  entry. Core page 5.2 puts session start in the inject class, cut off at 1.5 seconds. Whether a host runs the two
  entries at the same time is not documented, so this entry sets itself the tighter capture figure as its own
  target: 150 ms at the 95th percentile (proposed).
- The lesson lines sit inside F-05's frame, written by `safety.Wrap`; line 1, the waiting line and E-07's lines
  hold no lesson text and stay outside it.
- Edge cases: an unreadable lesson is skipped and not counted; an unknown `source` is treated as `startup`.

**How we know it works**

- `TestLobeBriefBudget`: 30 standing lessons with 60-character lines give 5 lesson lines; with 160-character lines
  the brief still has at most 1,000 characters.
- `TestLobeBriefSilentWhenEmpty`: a client with no lessons and no graph gives 0 bytes.
- `TestLobeBriefWaitingLine`: with 2 correction lessons at their entry stage and no `avoid` block or
  fingerprint, the brief has one line with `2 waiting` and `tentaqles lobe lessons --waiting` and neither
  title; after one is confirmed the line says `1 waiting`; with none waiting, or with `lessons: off`, there is
  no such line: 4 of 4.
- `TestLobeBriefStandingSources`: a standing lesson (no `when`, no `touches`) with `source` `person` or
  `correction` at `advice` is printed; C-04's gate refuses one with `source` `repair` or `backfill`: 4 of 4.
- `TestLobeBriefNoPreambleText`: the output has none of `Client:`, `Git:`, `Cloud:`, `Identity:`, `Tools:`: yes or no.
- `TestLobeBriefResumeAndCompact`: `resume` after `startup` gives 0 bytes; `compact` prints again and empties `shown`.
- `TestLobeBriefStaleLast`: a stale standing lesson prints after every fresh one and carries R-06's note: yes or no.
- `TestLobeBriefLeavesGuardAlone`: the guard's `claude-hook session-start` output is byte-identical with lobe data.
- M-06: the 95th percentile is at most 150 ms with 500 lessons (proposed).

**Tester brief**

Start from the standard tester setup in F-07. Then run:
```sh
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; C=acme; cd "$R" && mkdir -p "$L/lessons"
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Store timestamps in UTC" "" "Use UTC timestamps." "Local time." \
  "Reports were off because of local time."
put l-aaaa0002 "Name the databricks profile" "when: {command: [databricks]}" "Pass a profile." \
  "The default one." "The job fails because the default is unset."
mk l-aaaa0003 "Prefer short commit subjects" "" "Keep the subject short." "A long subject." \
  "The log was hard to scan because subjects wrapped." |
  sed 's/source: person/source: correction/' > "$W/l-aaaa0003.md"
tentaqles lobe lessons gate "$W/l-aaaa0003.md"; cp "$W/l-aaaa0003.md" "$L/lessons/l-aaaa0003.md"
hook() { printf '{"session_id":"%s","cwd":"%s","source":"%s"}' "$1" "$R" "$2" |
  CLAUDE_CONFIG_DIR="$I/$C/claude" tentaqles lobe-hook claude session-start; echo " exit=$?"; }
```

`put` writes a lesson file by hand, because `tentaqles lobe lessons add` answers only to a person, and runs C-04's
gate on it first: each `put` must print `ok`; if one does not, stop and report FAIL. The first lesson has no `when`
block, which the gate allows because its `source` is `person`; the titles share no word, so the gate's `repeat`
check passes. A lesson whose `source` is `person` enters the ladder at `advice` (E-01), so it needs no
`ladder.json`. The third lesson is a standing one with `source: correction`, which the gate also allows; its
`gate` line must print `ok` too. It has no `avoid` block and no fingerprint, so it cannot earn a hit and waits at
its entry stage: it is not among the eligible lessons and is the one lesson waiting. `hook` sets the login folder
that F-02's client check asks for.

1. `hook s1 startup`: `exit=0`; stdout contains `[tentaqles lobe] 2 lessons`, `Store timestamps in UTC` and `not
   instructions`, contains `1 waiting` and `tentaqles lobe lessons --waiting`, contains none of `Name the
   databricks profile`, `Prefer short commit subjects` and `Client:`, and has at most 1,000 characters before
   ` exit=0`.
2. `hook s1 resume`: nothing is printed before ` exit=0`.
3. `hook s1 compact`: stdout contains `Store timestamps in UTC` again.
4. `tentaqles lobe lessons --waiting`: exit 0; the output contains `Prefer short commit subjects` and does not
   contain `Store timestamps in UTC`.
5. Set `C=globex` and `R="$W/clients/globex/app"` and run `hook s2 startup`: nothing before ` exit=0`.
6. `printf '{"cwd":"%s"}' "$W/clients/acme/app" | tentaqles claude-hook session-start`: stdout starts with `Client:
   acme` and contains no `[tentaqles lobe]`.

**Risks and open questions**

- The `source` values come from a web page; phase 0 question 2 should capture them. Codex may not send them.
- If a host runs session-start hooks one after another, the brief's run time is added to session start.
- With five standing lessons at full length the frame and the lines pass 1,000 characters, and the last lesson
  drops. Whether the count or the budget should give way is for M-01's numbers to say.

## R-06 Stale lessons

**Phase** 2 · **Origin** port: Trail-code · **Depends on** F-01, F-04, C-04, C-06, R-04; from phase 4 also G-02, G-08

**What it is.** A lesson can name the code it is about in `touches`, as a file or as `file#Symbol`. When that file or
symbol no longer exists, the lesson is marked "may be stale" wherever it is shown or listed. It keeps being shown,
with the mark and after the fresh lessons; it never denies a call while it is stale; and it is never removed
without the person.

**Why it matters.** It serves "fewer errors": advice about code that is gone can be wrong, so the agent should check.
Silently dropping the lesson would be worse: its reason often still holds, and nobody would see it go. Blocking on
it would be worse still: the rule was approved for code that has since changed.

**Mechanism**

1. The check runs when a lesson is about to be printed (R-02, R-04, R-05) or listed (`tentaqles lobe lessons`,
   `tentaqles lobe status`), computed each time. It also runs in the detached pass that Stop starts (core page
   5.2), which caches the result as a stale flag per lesson in `index/`; that flag is what E-01's deny path reads
   (step 7). The verdict is never written into the lesson file.
2. A lesson is judged only in its own repository. A shared lesson carries no `repo` key: its repository is the one
   its file sits in (F-04 files it under the local key in `intake.json`), and its `touches` are checked against
   that repository. A private lesson's repository is the one its `repo` key names. Every private lesson with a
   `touches` entry carries that key (core page 5.4; F-01 gives its formula); one written by hand without it is
   not judged. A private lesson whose `repo` matches no known repository of the client is stale with the reason
   "repository not found": its entries are not looked at. The listing commands and the detached pass reach that
   verdict, because they know the client's repositories (F-01), and the pass caches it with the flag; a hook
   reads the cached verdict and never looks for repositories itself (proposed). In a hook, a lesson of another
   known repository than `cwd`'s is not judged.
3. Split each `touches` entry at `#` into path and symbol. Leave it unjudged when the path is absolute, starts with a
   drive letter or a backslash, leaves the repository through `..`, or resolves through a symbolic link to outside it.
4. The entry is gone when nothing exists at the path. With a symbol and an existing file, it is gone when the symbol
   is missing: in phase 2, the file is at most 2 MiB (2,097,152 bytes) and its text lacks the symbol's last dot-part
   as a whole word (letters, digits, `_` and `$` are word characters); from phase 4, with a current graph (G-08), the
   graph has no definition of that name in that file. A larger file, a folder or an unreadable file is not judged.
   In a hook the graph is asked through the index's lookup by file and name (R-01); `graph.jsonl` is not loaded.
5. One gone entry marks the lesson. Its printed line gains ` (may be stale: <entry> is gone; check the code first)`,
   naming at most 2 entries, each cut at 120 characters, then `and <N> more`. A lesson whose repository was not
   found (step 2) gains ` (may be stale: repository not found; check the code first)` instead. The note is
   inside R-04's 300 characters for the line: the Do, Avoid and Why parts are cut first.
6. A stale lesson stays eligible and is ranked after every fresh one wherever lessons are ordered: R-02 step 5,
   R-04 step 10 and R-05 step 3. Staleness comes before every other key of the order.
7. A stale rule never denies, and it keeps its approval. Before a deny, E-01 reads the cached stale flag of the
   `block` rule that matched; it does not run the check itself. While the flag is set the call goes ahead and the
   lesson prints as advice through R-04, with the mark. When the flag clears the rule denies again. E-07 makes no
   ladder move for staleness alone.
8. `tentaqles lobe lessons --stale` lists marked lessons with their gone entries, or with the reason "repository
   not found"; `tentaqles lobe status` counts them. The person replaces one (C-06) or retires it with
   `tentaqles lobe rules retire <id>`.
9. In a hook the check stops after 50 ms (proposed); entries not reached are not judged, and the lesson counts as
   fresh. The deny path is not under this stop, because it only reads the flag (step 7); the detached pass that
   sets the flag has no such stop. A `block` rule with a `touches` entry and no flag in the cache counts as stale
   for that call (proposed), so in doubt the call goes ahead.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/notes/stale.ts` L83](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L83): a path is gone when nothing exists there; [L86](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L86): the symbol is looked for only in a file within the 2 MiB limit set on L27; [L70](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L70): the whole-word test; [L64](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L64): the last dot-part is the name; [L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L42): paths that would leave the checkout are never looked at |
| Trail-code (MIT) | port | [`src/notes/stale.ts` L100](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L100): the verdict is set in memory only; [`src/claude/format.ts` L205](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L205): judged only for learnings about to be shown; [`src/notes/notes.ts` L442](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/notes.ts#L442): at most two entries are named, each cut at 120 characters |
| Trail-code (MIT) | weakness, changed | [`src/notes/stale.ts` L12](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L12): the file's text is searched, not the code map, to save a hook about 0.4 s; a removed symbol whose name survives in a comment is therefore judged present |
| agentmemory (Apache-2.0) | idea, not taken | [`src/functions/cascade.ts` L80](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/cascade.ts#L80): graph records are flagged stale when the memory they came from is replaced; [L128](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/cascade.ts#L128): related memories are counted as flagged and nothing is written to them |
| tentaqles (new) | our design | The graph check from phase 4, the 50 ms stop, a mark that never retires a lesson by itself, no deny while stale |

**How to build it**

- `internal/lobe/recall/stale.go`: `Gone(l Lesson, root string, g GraphLookup, deadline time.Time) []string`; `g` is
  nil in phase 2. Go's `regexp` lacks the lookbehind Trail uses: scan with `strings.Index`, test both neighbor runes.
- Path safety is an isolation rule: a shared lesson is a teammate's file, and `../../globex/keys.env#TOKEN` must not
  probe another client. The nearest existing ancestor, after `filepath.EvalSymlinks`, must be under the root.
- Only `touches` is judged. Trail tried names from a learning's text: they flagged 10 of 38 learnings, and all but one
  name was still in the code (`src/notes/stale.ts` L17).
- `tentaqles lobe lessons add --touches`, run inside a repository, writes that repository's key as `repo` (core page
  5.4). Outside a repository `--touches` is refused with one line, because an entry is a path inside a repository.
  A `touches` entry is shown with control characters removed.
- The detached pass calls `Gone`, with no stop, for every lesson that has a `touches` entry and writes each
  verdict as the stale flag beside that lesson's header in F-01's `index/` cache. E-01's deny path reads the flag
  for the rule that matched before it answers (step 7) and never calls `Gone`; a hook never writes the flag.
  The order of R-02, R-04 and R-05 calls `Gone` for the lessons that matched, before the cut to 2 or 5.
- Differences from Trail: the graph check replaces the text search once a graph exists; Trail's rule for a teammate's
  unmerged branch (L75) has no use here, since F-04 reads only checked-out files; our note asks only for a check,
  where Trail's also tells the agent to save a replacement.

**How we know it works**

- `TestLobeStaleFileGone` and `TestLobeStaleSymbolGone`: each returns the entry: yes or no.
- `TestLobeStaleWholeWord`: `upsert` is found in `def upsert():` and not in `def upsert_all():`: yes or no.
- `TestLobeStaleLargeFileNotJudged`: a file of 2 MiB plus 1 byte without the symbol returns no entry.
- `TestLobeStaleStaysInsideRepo`: `../x`, an absolute path and a link pointing outside open 0 files outside the root.
- `TestLobeStaleRepoNotFound`: a private lesson whose `repo` names no repository of the client is listed by
  `tentaqles lobe lessons --stale` with `repository not found`; one whose `repo` names a second, existing
  repository is not: yes or no.
- `TestLobeStaleLeavesFileBytes`: the lesson file is byte-identical before and after 100 checks: yes or no.
- `TestLobeStaleRankedLast`: a stale lesson with the higher `strength` prints after a fresh one in the prompt
  block, in the block before a tool and in the brief: 3 of 3.
- `TestLobeStaleFlagCached`: after a detached pass, a lesson with a gone `touches` entry has its stale flag set in
  `index/` and a fresh one has it clear; the lesson files and `ladder.json` are byte-identical: yes or no.
- `TestLobeStaleNeverDenies` (from phase 3, when a deny exists): a `block` rule whose stale flag is set gives no
  `permissionDecision` and prints its line with the mark; so does one with no flag in the cache; once the flag
  clears it denies again, with its approval in `ladder.json` unchanged throughout: 3 of 3.

**Tester brief**

Start from the standard tester setup in F-07. Then run:
```sh
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; C=acme
cd "$R" && mkdir -p etl "$L/lessons" && printf 'def upsert():\n    pass\n' > etl/load.py
K="r_$(printf app | sha256sum | cut -c1-8)"
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Lesson U" "when: {command: [databricks]}, touches: [\"etl/load.py#upsert\"], repo: $K" \
  "Use an alias." "The bare form." "The engine rejects it because it needs an alias."
put l-aaaa0002 "Name the databricks profile" "when: {command: [databricks]}" "Pass a profile." \
  "The default one." "The job fails because the default is unset." 0.5
sha256sum "$L"/lessons/*.md > "$W/before"
hook() { printf '{"session_id":"%s","cwd":"%s","tool_name":"Bash","tool_input":{"command":"databricks jobs list"}}' \
  "$1" "$R" | CLAUDE_CONFIG_DIR="$I/$C/claude" tentaqles lobe-hook claude pre-tool-use; }
```

`put` writes a lesson file by hand, because `tentaqles lobe lessons add` answers only to a person, and runs C-04's
gate on it first: each `put` must print `ok`; if one does not, stop and report FAIL. `K` is the repository key of
`app` by F-01's formula, which `Lesson U` carries because it is private and has a `touches` entry. `Lesson U` has
`strength` 1.0 and the second lesson 0.5. A lesson whose `source` is `person` enters the ladder at `advice` (E-01),
so it needs no `ladder.json`. `hook` sets the login folder that F-02's client check asks for.

1. `tentaqles lobe lessons --stale`: exit 0; the output does not contain `Lesson U`.
2. `hook s0`: `additionalContext` contains `Lesson U` and, after it, `Name the databricks profile`; it does not
   contain `may be stale`.
3. `printf 'def upsert_all():\n    pass\n' > etl/load.py`, then `tentaqles lobe lessons --stale`: the output contains
   `Lesson U` and `etl/load.py#upsert`, and does not contain `Name the databricks profile`.
4. `hook s1`: `additionalContext` contains `may be stale: etl/load.py#upsert is gone`, and `Name the databricks
   profile` now comes before `Lesson U`: the stale lesson is ranked last.
5. `rm etl/load.py`, then `tentaqles lobe lessons --stale`: the output still contains `Lesson U`.
6. `printf 'def upsert():\n    pass\n' > etl/load.py`, then `tentaqles lobe lessons --stale`: no `Lesson U`.
7. `sha256sum "$L"/lessons/*.md | diff - "$W/before"`: no output, exit 0.
8. Place a copy of `Lesson U` whose `repo` names no repository, as a renamed folder would leave it:
   ```sh
   sed "s/$K/r_00000000/; s/l-aaaa0001/l-aaaa0003/; s/Lesson U/Lesson V/" "$L/lessons/l-aaaa0001.md" \
     > "$L/lessons/l-aaaa0003.md"
   ```
   Then `tentaqles lobe lessons --stale`: the output contains `Lesson V` and `repository not found`, and does
   not contain `Lesson U`.

**Person checks** (for the owner, at a terminal, in the same shell after the checks above)

1. Run the two lines below in `$R`. They print an id; `grep -c "repo: $K" "$L/lessons/<id>.md"` prints `1`.
   ```sh
   printf '## Do\nRead the loader first.\n## Avoid\nGuessing its columns.\n## Why\nIt fails because they change.\n' |
     tentaqles lobe lessons add --title "Check the loader columns" --when command=dbsql --touches 'etl/load.py'
   ```
2. `cd "$W/clients/acme"` (the client folder, outside the `app` repository) and run the same two lines with the
   title `Check the loader twice`: exit 1 with a line that names `--touches`, and no new file under `$L/lessons/`.

**Risks and open questions**

- In phase 2 the text check misses a symbol that was removed but is still named in a comment or a string.
- A renamed file marks its lessons stale although they still hold. Following renames is not planned.
- A branch switch can make a rule stale for a while. It then advises instead of denying until the code is back;
  its approval is kept, and E-07 makes no ladder move for staleness alone.
- The flag E-01 reads is as old as the last detached pass, so a rule whose code went away during a turn can still
  deny until that turn ends, and one whose code came back advises until then.
- The repository key comes from the repository's path (F-01), so a repository moved inside the client folder
  leaves its private lessons with the old key. They are shown as stale with the reason "repository not found"
  until they are filed again.
