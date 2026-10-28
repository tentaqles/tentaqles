# demo-todo — Product Requirements

## 1. Problem and evidence
Developers jot tasks in whatever is open. Nothing lives where they work, the terminal.

## 2. Users and roles
- One developer, the only user and the only role.

## 3. Goals
- Capture a task in one command without leaving the terminal.

## 4. Functional requirements

### FR-1 Add a todo
`todo add <text>` appends a todo with the next integer id, the text, `done: false` and a creation timestamp, then prints the id.
**Acceptance checks**
- `todo add "buy milk"` prints `1` on an empty store and exits 0.
- A second add prints `2`; ids never repeat even after a remove.
- An empty text exits 2 with `todo: add needs a text` on stderr and stores nothing.

### FR-2 List todos
`todo list` prints open todos one per line as `<id>  <text>`; `todo list --all` includes done ones marked with `[x]`.
**Acceptance checks**
- With todos 1 (open) and 2 (done), `todo list` prints only `1  buy milk`.
- `todo list --all` prints both, the done one as `2  [x] call mum`.
- An empty store prints nothing and exits 0.

### FR-3 Complete a todo
`todo done <id>` marks the todo done and records the completion timestamp.
**Acceptance checks**
- `todo done 1` exits 0 and `todo list` no longer shows id 1.
- `todo done 99` exits 1 with `todo: no todo 99` on stderr.
- Completing an already done todo exits 0 and changes nothing.

### FR-4 Remove a todo
`todo remove <id>` deletes the todo from the store.
**Acceptance checks**
- `todo remove 2` exits 0 and `todo list --all` no longer shows id 2.
- `todo remove 99` exits 1 with `todo: no todo 99` on stderr.

### FR-5 Store location and format
Todos live in `$TODO_FILE` when set, else `~/.todo.json`, as a JSON object `{"next_id": N, "todos": [...]}` written atomically (write to a temp file, then rename).
**Acceptance checks**
- With `TODO_FILE` pointing at a temp path, all commands read and write that file only.
- A missing file is treated as an empty store; a corrupt file exits 1 with `todo: store is not valid JSON` and is left untouched.
- After any write the file parses as JSON and `next_id` equals the highest id ever issued plus one.

### FR-6 Edit a todo
`todo edit <id> <text>` replaces the text of an existing todo. Its id, done flag and timestamps do not change.
**Acceptance checks**
- With todo 1 `buy milk`, `todo edit 1 "buy oat milk"` exits 0 and `todo list` prints `1  buy oat milk`.
- `todo edit 99 "x"` exits 1 with `todo: no todo 99` on stderr and the store file is byte-identical.

## 5. Data
The store holds only todo text the user typed and timestamps.

## 10. Open questions
Decisions made: Go stdlib; one JSON file; integer ids that never repeat.
- DECIDED (accepted by tester on 2026-10-27): the local-only risks of the threat model.

## 11. Releases
- Release 1: FR-1 to FR-5, built through the gate from this PRD.
- Release 2: FR-6, built through the feature path.
- Later, as separate hub cards: due dates and `list --due`; a deliberate bug and its fix card; the ship.
