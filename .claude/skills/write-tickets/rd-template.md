# {NNN}: {Title}

**Status:** draft
**Risk:** {Medium | High}: {the worst break, one line}
**UI:** {none | [mockup.html](mockup.html)}

{One or two sentences: what this is, who it's for, and why. This tells the builder what to favour when a detail isn't covered here.}

## Requirements

- **REQ-1**: {One testable behaviour. Use "must". Say what input or action produces what observable result. Quote user-facing text exactly.}
- **REQ-2**: {One behaviour per requirement, so each can pass or fail on its own.}

## Out of scope

- {What a reader might expect that is deliberately left out. Write "None" if nothing is.}

## Affected areas

- `{path/to/file-or-dir}`: {what changes there}

{Team ticket only, otherwise delete this paragraph and the blocks below. One block per part, so each teammate owns different files.}

### Part A: {name}

- `{path/to/file-or-dir}`: {what changes there}

### Part B: {name}

- `{path/to/file-or-dir}`: {what changes there}

## Do not touch

- `{path/to/file-or-dir}`: {why, for example another ticket owns it. Write "None" if nothing applies.}

## Decisions

- **Agreed with the owner**: {each answer from the interview, one line each}
- **Decided by the ticket writer**: {each call made without asking, one line each, so the owner can overrule it}

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check {NNN}` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
- {Anything else this ticket needs to count as done. Delete this line if nothing.}
