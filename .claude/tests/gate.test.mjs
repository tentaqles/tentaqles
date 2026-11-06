// Run: node --test .claude/tests/
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, utimesSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const GATE = resolve(here, "../hooks/gate.mjs");
const T = "001-invites";

const RD = (risk = "Medium: invites stop working") => `# ${T}: Invites

**Status:** draft
**Risk:** ${risk}
**UI:** none

## Requirements
- REQ-1 Send an invite. See also REQ-99 in the old doc.
`.replace(" See also REQ-99 in the old doc.", "");

const check = (id, method = "test TestX", covers = "REQ-1") => `### ${id} Title
- **Covers**: ${covers}
- **Method**: ${method}
- **Steps**:
  1. Do it.
- **Expect**: It works.
`;
const EVAL = (extra = "") =>
  `# ${T} eval\n\n## Checks\n\n${["U-01", "F-01", "Q-01", "R-01", "S-01"].map((i) => check(i)).join("\n")}${extra}`;

function git(dir, ...a) {
  const r = spawnSync("git", a, { cwd: dir, encoding: "utf8" });
  assert.equal(r.status, 0, `git ${a.join(" ")}: ${r.stderr}`);
  return r.stdout.trim();
}
function gate(dir, ...a) {
  const r = spawnSync(process.execPath, [GATE, ...a], { cwd: dir, encoding: "utf8" });
  return { code: r.status, out: r.stdout, err: r.stderr, all: r.stdout + r.stderr };
}
function hook(dir, kind, payload, raw) {
  const input = raw ?? JSON.stringify({ cwd: dir, ...payload });
  const r = spawnSync(process.execPath, [GATE, "hook", kind], { cwd: dir, encoding: "utf8", input });
  return { code: r.status, out: r.stdout, err: r.stderr };
}
const denied = (r) => r.out.includes('"permissionDecision":"deny"');

function project(cfgOver = {}) {
  const dir = mkdtempSync(join(tmpdir(), "gate-"));
  git(dir, "init", "-q", "-b", "main");
  git(dir, "config", "user.email", "t@example.com");
  git(dir, "config", "user.name", "t");
  mkdirSync(join(dir, ".claude"));
  writeFileSync(join(dir, ".claude/project.json"), JSON.stringify({
    confirmed: true, stack: "cli", capabilities: { browser: false, mockups: false },
    ticketsDir: "docs/tickets", jobDir: ".job", requirementId: "REQ-\\d+",
    gates: { round: [`node -e "process.exit(0)"`], release: [`node -e "process.exit(0)"`] },
    limits: { stopBlocks: 3 }, ...cfgOver,
  }));
  writeFileSync(join(dir, ".gitignore"), ".job/\n");
  mkdirSync(join(dir, "docs/tickets", T), { recursive: true });
  writeFileSync(join(dir, "docs/tickets", T, "rd.md"), RD());
  writeFileSync(join(dir, "docs/tickets", T, "eval.md"), EVAL());
  writeFileSync(join(dir, "docs/tickets/BOARD.md"), `| # | Title | Status | Risk | Runs |\n|---|---|---|---|---|\n| 001 | Invites | draft | Medium | alongside |\n`);
  git(dir, "add", "-A"); git(dir, "commit", "-qm", "init");
  return dir;
}
/** A project with an approved ticket, claimed, one code commit, gate run, hook seen. */
function built(cfgOver) {
  const dir = project(cfgOver);
  assert.equal(gate(dir, "approve", T).code, 0);
  assert.equal(gate(dir, "board", T, "ready").code, 0);
  git(dir, "add", "-A"); git(dir, "commit", "-qm", "approved");
  assert.equal(gate(dir, "claim", T).code, 0);
  assert.equal(denied(hook(dir, "pre", { tool_name: "Write", tool_input: { file_path: join(dir, "src.txt") } })), false);
  writeFileSync(join(dir, "src.txt"), "code\n");
  git(dir, "add", "-A"); git(dir, "commit", "-qm", "code");
  assert.equal(gate(dir, "run", T, "round").code, 0);
  return dir;
}
function results(dir, lines) {
  const f = join(dir, ".job", "r.md");
  writeFileSync(f, lines ?? ["U-01", "F-01", "Q-01", "R-01", "S-01"].map((i) => `${i} | PASS | TestX ok`).join("\n"));
  return gate(dir, "record", T, f);
}

// ---- lint
test("lint: a well formed ticket passes", () => {
  assert.equal(gate(project(), "lint", T).code, 0);
});
test("lint: empty Steps and Expect fail (the pack only checked the shape)", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "eval.md"),
    EVAL().replace("- **Expect**: It works.", "- **Expect**:").replace("  1. Do it.\n", ""));
  const r = gate(d, "lint", T);
  assert.equal(r.code, 1);
  assert.match(r.all, /U-01.*Steps/);
  assert.match(r.all, /U-01.*Expect/);
});
test("lint: an uncovered requirement fails; ids outside Requirements don't count", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "rd.md"), RD() + "- REQ-2 Another.\n\n## Decisions\nSee REQ-77 elsewhere.\n");
  const r = gate(d, "lint", T);
  assert.match(r.all, /REQ-2/);
  assert.doesNotMatch(r.all, /REQ-77/);
});
test("lint: an uncovered requirement fails when Requirements is the last section", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "rd.md"), RD() + "- REQ-3 Last line of the file.\n");
  assert.match(gate(d, "lint", T).all, /REQ-3/);
});
test("lint: High needs 3 S checks, one that runs", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "rd.md"), RD("High: leaks"));
  assert.match(gate(d, "lint", T).all, /High.*S checks/);
  writeFileSync(join(d, "docs/tickets", T, "eval.md"),
    EVAL(check("S-02", "inspect") + check("S-03", "inspect")).replace(check("S-01"), check("S-01", "inspect")));
  assert.match(gate(d, "lint", T).all, /attack|runs/i);
});
test("lint: browser method needs the browser capability", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "eval.md"), EVAL(check("U-02", "browser")));
  assert.match(gate(d, "lint", T).all, /browser/);
  const web = project({ capabilities: { browser: true } });
  writeFileSync(join(web, "docs/tickets", T, "eval.md"), EVAL(check("U-02", "browser")));
  assert.equal(gate(web, "lint", T).code, 0);
});

// ---- approval
test("approve: refuses a malformed ticket", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "eval.md"), "nothing");
  assert.equal(gate(d, "approve", T).code, 1);
  assert.equal(existsSync(join(d, "docs/tickets", T, "approval.json")), false);
});
test("board: draft can't jump to landed or ready (the pack's Draft to Passing hole)", () => {
  const d = project();
  assert.equal(gate(d, "board", T, "landed").code, 1);
  assert.equal(gate(d, "board", T, "ready").code, 1);
  assert.match(readFileSync(join(d, "docs/tickets/BOARD.md"), "utf8"), /\| 001 \| Invites \| draft/);
});
test("approval: appending a check stays valid; changing one or the rd doesn't", () => {
  const d = project();
  gate(d, "approve", T);
  const ev = join(d, "docs/tickets", T, "eval.md");
  const rd = join(d, "docs/tickets", T, "rd.md");
  writeFileSync(ev, readFileSync(ev, "utf8") + "\n" + check("F-02"));
  assert.equal(gate(d, "approval", T).code, 0);
  writeFileSync(ev, readFileSync(ev, "utf8").replace("It works.", "Anything."));
  assert.equal(gate(d, "approval", T).code, 1);
  writeFileSync(ev, EVAL());
  assert.equal(gate(d, "approval", T).code, 0);
  writeFileSync(rd, readFileSync(rd, "utf8").replace("**Status:** draft", "**Status:** ready"));
  assert.equal(gate(d, "approval", T).code, 0, "status line isn't part of the hash");
  writeFileSync(rd, readFileSync(rd, "utf8").replace("Send an invite", "Send nothing"));
  assert.equal(gate(d, "approval", T).code, 1);
});
test("approval: line endings don't break the hash", () => {
  const d = project();
  gate(d, "approve", T);
  const ev = join(d, "docs/tickets", T, "eval.md");
  writeFileSync(ev, readFileSync(ev, "utf8").replace(/\n/g, "\r\n"));
  assert.equal(gate(d, "approval", T).code, 0);
});

// ---- pre hook
test("pre: no claim means no edits", () => {
  const d = project();
  assert.ok(denied(hook(d, "pre", { tool_name: "Write", tool_input: { file_path: join(d, "a.txt") } })));
});
test("pre: Windows backslash paths to the ticket are denied (the pack's bug)", () => {
  const d = built();
  const win = join(d, "docs/tickets", T, "eval.md").replace(/\//g, "\\");
  assert.ok(denied(hook(d, "pre", { tool_name: "Edit", tool_input: { file_path: win } })));
  assert.ok(denied(hook(d, "pre", { tool_name: "Write", tool_input: { file_path: "docs\\tickets\\" + T + "\\rd.md" } })));
  assert.ok(denied(hook(d, "pre", { tool_name: "Edit", tool_input: { file_path: join(d, "docs/tickets", T, "approval.json") } })));
});
test("pre: an approval that no longer matches stops edits", () => {
  const d = built();
  writeFileSync(join(d, "docs/tickets", T, "eval.md"), EVAL().replace("It works.", "Anything."));
  assert.ok(denied(hook(d, "pre", { tool_name: "Write", tool_input: { file_path: join(d, "b.txt") } })));
});
test("pre: builders can't push or merge; ordinary git and shell pass", () => {
  const d = built();
  for (const c of ["git push origin x", "git -C . push", "cd a && git merge main", "gh pr create", "git reset --hard HEAD~1", "git rebase main"])
    assert.ok(denied(hook(d, "pre", { tool_name: "Bash", tool_input: { command: c } })), c);
  for (const c of ["git status", "git commit -m 'push it'", "go test ./...", "echo git push"])
    assert.equal(denied(hook(d, "pre", { tool_name: "Bash", tool_input: { command: c } })), false, c);
});
test("pre: garbage input lets the call through and logs it", () => {
  const d = project();
  const r = hook(d, "pre", {}, "not json");
  assert.equal(r.code, 0);
  assert.equal(denied(r), false);
});
test("pre: answers quickly", () => {
  const d = built();
  const t = performance.now();
  hook(d, "pre", { tool_name: "Write", tool_input: { file_path: join(d, "c.txt") } });
  assert.ok(performance.now() - t < 3000);
});

// ---- stop hook
test("stop: a forged done status with no commit is blocked (the pack's bug)", () => {
  const d = project();
  gate(d, "approve", T); gate(d, "board", T, "ready");
  git(d, "add", "-A"); git(d, "commit", "-qm", "approved");
  gate(d, "claim", T);
  writeFileSync(join(d, ".job", T, "status.json"), JSON.stringify({ step: "done", evals: { failed: 0, total: 5 } }));
  const r = hook(d, "stop", {});
  assert.equal(r.code, 2);
  assert.match(r.err, /commit/);
});
test("stop: missing claim blocks a builder", () => {
  const d = project();
  assert.equal(hook(d, "stop", { agent_type: "builder" }).code, 2);
});
test("stop: no claim can't block forever (a stalling hook is a P0)", () => {
  const d = project();
  for (let i = 0; i < 3; i++) assert.equal(hook(d, "stop", {}).code, 2);
  assert.equal(hook(d, "stop", {}).code, 0);
});
test("pre: lookalike git commands aren't blocked", () => {
  const d = built();
  for (const c of ["git merge-base HEAD main", "git push-foo", "git rebase-x", "git log --merges", "git diff main...HEAD"])
    assert.equal(denied(hook(d, "pre", { tool_name: "Bash", tool_input: { command: c } })), false, c);
});
test("check: evidence like dir/x.js:12 means the file, without the line", () => {
  const d = built();
  mkdirSync(join(d, "dir"));
  writeFileSync(join(d, "dir/x.js"), "x");
  results(d, ["U-01", "F-01", "Q-01", "R-01"].map((i) => `${i} | PASS | ok`).join("\n") + "\nS-01 | PASS | see `dir/x.js:12`");
  assert.equal(gate(d, "check", T).code, 0);
});
test("step --round: a new round recovers a released ticket and its block count", () => {
  const d = built();
  writeFileSync(join(d, "dirty.txt"), "x");
  for (let i = 0; i < 4; i++) hook(d, "stop", {});
  assert.match(readFileSync(join(d, ".job", T, "status.json"), "utf8"), /"released": true/);
  gate(d, "step", T, "build", "--round", "2");
  const s = JSON.parse(readFileSync(join(d, ".job", T, "status.json"), "utf8"));
  assert.equal(s.released, false);
  assert.equal(hook(d, "stop", {}).code, 2, "the gate is on again");
});
test("stop: a dead gate process uses up blocks, so it can't loop", () => {
  const d = built();
  writeFileSync(join(d, "x.txt"), "x");
  git(d, "add", "-A"); git(d, "commit", "-qm", "more");
  writeFileSync(join(d, ".job", T, "gate-round.running"), JSON.stringify({ pid: 99999999, at: Date.now() }));
  for (let i = 0; i < 3; i++) assert.equal(hook(d, "stop", {}).code, 2);
  assert.equal(hook(d, "stop", {}).code, 0);
});
test("stop: passing writes a done marker naming the tree", () => {
  const d = built();
  gate(d, "step", T, "build", "--round", "1");
  mkdirSync(join(d, ".job", T, "round-1"), { recursive: true });
  writeFileSync(join(d, ".job", T, "round-1/builder.md"), "report");
  const done = join(d, ".job", T, "round-1/builder.done");
  assert.equal(hook(d, "stop", {}).code, 0);
  assert.equal(readFileSync(done, "utf8").trim(), git(d, "rev-parse", "HEAD^{tree}"));
});
test("human: commits its record, so the release's clean tree holds", () => {
  const d = project();
  assert.equal(gate(d, "human", T, "U-01", "pass", "ok").code, 0);
  assert.equal(git(d, "status", "--porcelain"), "");
});
test("stop: dirty tree blocks; report and round gate required; clean passes", () => {
  const d = built();
  writeFileSync(join(d, "dirty.txt"), "x");
  assert.match(hook(d, "stop", {}).err, /uncommitted|clean/);
  git(d, "add", "-A"); git(d, "commit", "-qm", "more");
  assert.match(hook(d, "stop", {}).err, /round gate/);
  gate(d, "run", T, "round");
  assert.match(hook(d, "stop", {}).err, /report/);
  gate(d, "step", T, "build", "--round", "1");
  mkdirSync(join(d, ".job", T, "round-1"), { recursive: true });
  writeFileSync(join(d, ".job", T, "round-1/builder.md"), "report");
  assert.equal(hook(d, "stop", {}).code, 0);
});
test("stop: a blocked builder with a note may stop", () => {
  const d = project();
  gate(d, "approve", T); gate(d, "board", T, "ready");
  git(d, "add", "-A"); git(d, "commit", "-qm", "approved");
  gate(d, "claim", T);
  gate(d, "block", T, "which error copy?");
  assert.equal(hook(d, "stop", {}).code, 0);
});
test("stop: releases after 3 blocks and marks the ticket failed; check then refuses", () => {
  const d = built();
  writeFileSync(join(d, "dirty.txt"), "x");
  for (let i = 0; i < 3; i++) assert.equal(hook(d, "stop", {}).code, 2);
  assert.equal(hook(d, "stop", {}).code, 0);
  assert.match(readFileSync(join(d, ".job", T, "status.json"), "utf8"), /released/);
  assert.match(gate(d, "check", T).all, /released/);
});
test("stop: a round gate still running doesn't use up a block", () => {
  const d = built();
  writeFileSync(join(d, "x.txt"), "x");
  git(d, "add", "-A"); git(d, "commit", "-qm", "more");
  writeFileSync(join(d, ".job", T, "gate-round.running"), JSON.stringify({ pid: process.pid, at: Date.now() }));
  for (let i = 0; i < 5; i++) assert.equal(hook(d, "stop", {}).code, 2);
});

// ---- run and check
test("run: a failing command fails the stamp", () => {
  const dir = built();
  const p = join(dir, ".claude/project.json");
  writeFileSync(p, JSON.stringify({ ...JSON.parse(readFileSync(p, "utf8")), gates: { round: [`node -e "process.exit(3)"`], release: [] } }));
  git(dir, "add", "-A"); git(dir, "commit", "-qm", "failing gate");
  const r = gate(dir, "run", T, "round");
  assert.equal(r.code, 1);
  assert.match(gate(dir, "check", T).all, /round gate/);
});
test("run: refuses a dirty tree, since the stamp names a tree", () => {
  const d = built();
  writeFileSync(join(d, "dirty.txt"), "x");
  assert.equal(gate(d, "run", T, "round").code, 1);
});
test("check: all good passes", () => {
  const d = built();
  results(d);
  const r = gate(d, "check", T);
  assert.equal(r.code, 0, r.all);
});
test("check: needs a confirmed profile with gate commands", () => {
  const d = built();
  results(d);
  const p = join(d, ".claude/project.json");
  writeFileSync(p, JSON.stringify({ ...JSON.parse(readFileSync(p, "utf8")), confirmed: false }));
  assert.match(gate(d, "check", T).all, /confirm/);
});
test("check: every check needs a result, PASS, and evidence", () => {
  const d = built();
  results(d, "U-01 | PASS | ok\nF-01 | FAIL | broke\nQ-01 | PASS |\nR-01 | HUMAN | later");
  const a = gate(d, "check", T).all;
  assert.match(a, /S-01.*no result/);
  assert.match(a, /F-01.*FAIL/);
  assert.match(a, /Q-01.*evidence/);
  assert.match(a, /R-01.*human/i);
});
test("check: a human check may be pending", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "eval.md"), EVAL(check("U-02", "human")));
  git(d, "add", "-A"); git(d, "commit", "-qm", "x");
  gate(d, "approve", T); gate(d, "board", T, "ready");
  git(d, "add", "-A"); git(d, "commit", "-qm", "approved");
  gate(d, "claim", T);
  hook(d, "pre", { tool_name: "Write", tool_input: { file_path: join(d, "s.txt") } });
  writeFileSync(join(d, "s.txt"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "code");
  gate(d, "run", T, "round");
  results(d, ["U-01", "F-01", "Q-01", "R-01", "S-01"].map((i) => `${i} | PASS | ok`).join("\n") + "\nU-02 | HUMAN | waiting");
  assert.equal(gate(d, "check", T).code, 0);
});
test("check: evidence paths in backticks must exist", () => {
  const d = built();
  results(d, ["U-01", "F-01", "Q-01", "R-01"].map((i) => `${i} | PASS | ok`).join("\n") + "\nS-01 | PASS | `.job/nope.txt`");
  assert.match(gate(d, "check", T).all, /nope/);
});
test("check: new code invalidates results; touch can't fake freshness (the pack's bug)", () => {
  const d = built();
  results(d);
  assert.equal(gate(d, "check", T).code, 0);
  writeFileSync(join(d, "src.txt"), "changed\n");
  git(d, "add", "-A"); git(d, "commit", "-qm", "change");
  const now = new Date(Date.now() + 60000);
  utimesSync(join(d, ".job", T, "gate-round.json"), now, now);
  const a = gate(d, "check", T).all;
  assert.match(a, /no results for this tree/);
  assert.match(a, /round gate/);
});
test("check: a corrupt stamp fails closed (the pack's bug)", () => {
  const d = built();
  results(d);
  writeFileSync(join(d, ".job", T, "gate-round.json"), "{not json");
  assert.equal(gate(d, "check", T).code, 1);
});
test("check: refuses when the builder's hooks never ran", () => {
  const d = project();
  gate(d, "approve", T); gate(d, "board", T, "ready");
  git(d, "add", "-A"); git(d, "commit", "-qm", "approved");
  gate(d, "claim", T);
  writeFileSync(join(d, "s.txt"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "code");
  gate(d, "run", T, "round");
  results(d);
  assert.match(gate(d, "check", T).all, /hook/);
});

// ---- landing and release
test("carry, landed: moves results across and refuses without proof", () => {
  const d = built();
  assert.equal(gate(d, "board", T, "landed").code, 1);
  results(d);
  assert.equal(gate(d, "board", T, "landed").code, 0);
  assert.ok(existsSync(join(d, "docs/tickets", T, "results.md")));
  assert.match(readFileSync(join(d, "docs/tickets/BOARD.md"), "utf8"), /\| 001 \| Invites \| landed/);
  assert.match(readFileSync(join(d, "docs/tickets", T, "rd.md"), "utf8"), /\*\*Status:\*\* landed/);
});
test("carry: copies job state from another worktree", () => {
  const a = built();
  results(a);
  const wt = join(mkdtempSync(join(tmpdir(), "wt-")), "merged");
  git(a, "worktree", "add", "-q", "--detach", wt, "HEAD");
  assert.equal(gate(wt, "carry", T, "--from", a).code, 0);
  assert.equal(gate(wt, "check", T).code, 0);
});
test("release: lists open human checks as commands, and human closes them", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "eval.md"), EVAL(check("U-02", "human")));
  git(d, "add", "-A"); git(d, "commit", "-qm", "x");
  gate(d, "approve", T); gate(d, "board", T, "ready");
  git(d, "add", "-A"); git(d, "commit", "-qm", "approved");
  gate(d, "claim", T);
  hook(d, "pre", { tool_name: "Write", tool_input: { file_path: join(d, "s.txt") } });
  writeFileSync(join(d, "s.txt"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "code");
  gate(d, "run", T, "round");
  results(d, ["U-01", "F-01", "Q-01", "R-01", "S-01"].map((i) => `${i} | PASS | ok`).join("\n") + "\nU-02 | HUMAN | waiting");
  gate(d, "board", T, "landed");
  git(d, "add", "-A"); git(d, "commit", "-qm", "landed");
  const r = gate(d, "release");
  assert.equal(r.code, 1);
  assert.match(r.all, /human 001-invites U-02 pass/);
  assert.equal(gate(d, "human", T, "U-02", "fail", "wrong prompt").code, 0);
  assert.match(gate(d, "release").all, /FAILED/);
  assert.match(gate(d, "release").all, /fix-a-bug/);
  assert.equal(gate(d, "human", T, "U-02", "pass", "looks right").code, 0);
  assert.match(gate(d, "release").all, /release gate/);
  assert.equal(gate(d, "release", "--run").code, 0);
});
test("release: a stray human record on a check that isn't human doesn't block", () => {
  const d = built();
  results(d);
  gate(d, "board", T, "landed");
  git(d, "add", "-A"); git(d, "commit", "-qm", "landed");
  gate(d, "human", T, "U-01", "fail", "no");
  assert.equal(gate(d, "release", "--run").code, 0);
});

test("fix path: a stamp without a ticket, tied to the tree", () => {
  const d = project();
  assert.equal(gate(d, "stamped", "fix-x").code, 1);
  assert.equal(gate(d, "run", "fix-x", "round").code, 0);
  assert.equal(gate(d, "stamped", "fix-x").code, 0);
  writeFileSync(join(d, "n.txt"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "n");
  assert.equal(gate(d, "stamped", "fix-x").code, 1);
  assert.equal(gate(d, "stamped", "001").code, 1, "tickets don't use the lighter gate");
});
test("risky: names files on High-risk paths and exits 1", () => {
  const d = project({ highRiskPaths: ["internal/guard/", "installers"] });
  const base = git(d, "rev-parse", "HEAD");
  mkdirSync(join(d, "internal/guardian"), { recursive: true });
  writeFileSync(join(d, "internal/guardian/a.go"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "a");
  assert.equal(gate(d, "risky", base).code, 0, "a similar name isn't the path");
  mkdirSync(join(d, "internal/guard"), { recursive: true });
  writeFileSync(join(d, "internal/guard/g.go"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "g");
  const r = gate(d, "risky", base);
  assert.equal(r.code, 1);
  assert.match(r.out, /internal\/guard\/g\.go/);
});

test("risky: a trailing star is a prefix", () => {
  const d = project({ highRiskPaths: ["internal/cmd/claudehook*"] });
  const base = git(d, "rev-parse", "HEAD");
  mkdirSync(join(d, "internal/cmd"), { recursive: true });
  writeFileSync(join(d, "internal/cmd/other.go"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "a");
  assert.equal(gate(d, "risky", base).code, 0);
  writeFileSync(join(d, "internal/cmd/claudehook_x.go"), "x"); git(d, "add", "-A"); git(d, "commit", "-qm", "b");
  assert.equal(gate(d, "risky", base).code, 1);
});
test("lint: a human check isn't one that runs and attacks", () => {
  const d = project();
  writeFileSync(join(d, "docs/tickets", T, "rd.md"), RD("High: leaks"));
  writeFileSync(join(d, "docs/tickets", T, "eval.md"), EVAL(check("S-02", "human") + check("S-03", "inspect")).replace(check("S-01"), check("S-01", "inspect")));
  assert.match(gate(d, "lint", T).all, /attack/);
});
test("lint: real braces in an Expect are fine; placeholders aren't, in rd.md either", () => {
  const d = project();
  const ev = join(d, "docs/tickets", T, "eval.md");
  writeFileSync(ev, EVAL().replace("It works.", 'Prints {"ok":true} and %{http_code} 200.'));
  assert.equal(gate(d, "lint", T).code, 0, gate(d, "lint", T).all);
  writeFileSync(ev, EVAL().replace("It works.", "{what happens}"));
  assert.match(gate(d, "lint", T).all, /placeholder/);
  writeFileSync(ev, EVAL());
  writeFileSync(join(d, "docs/tickets", T, "rd.md"), RD() + "\n## Decisions\n{decision}\n");
  assert.match(gate(d, "lint", T).all, /placeholder/);
});
test("approval: a changed mockup loses the approval", () => {
  const d = project();
  const m = join(d, "docs/tickets", T, "mockup.html");
  writeFileSync(m, "<p>one</p>");
  gate(d, "approve", T);
  assert.equal(gate(d, "approval", T).code, 0);
  writeFileSync(m, "<p>two</p>");
  assert.match(gate(d, "approval", T).all, /mockup/);
});
test("rule: appends to REVIEW.md's learned section and commits only that", () => {
  const d = project();
  writeFileSync(join(d, "REVIEW.md"), "# Review\n\n## Rules learned over time\n\nFormat note.\n");
  git(d, "add", "-A"); git(d, "commit", "-qm", "review");
  writeFileSync(join(d, "other.txt"), "untouched");
  assert.equal(gate(d, "rule", "Always check the owner before deleting.").code, 0);
  assert.match(readFileSync(join(d, "REVIEW.md"), "utf8"), /## Rules learned over time[\s\S]*- Always check the owner/);
  assert.equal(git(d, "status", "--porcelain"), "?? other.txt");
  assert.equal(gate(d, "rule", "").code, 1);
});
test("park: sets the board and commits it; refuses an unknown ticket", () => {
  const d = project();
  assert.equal(gate(d, "park", T).code, 0);
  assert.match(readFileSync(join(d, "docs/tickets/BOARD.md"), "utf8"), /parked/);
  assert.equal(git(d, "status", "--porcelain"), "");
  assert.equal(gate(d, "park", "999").code, 1);
});

// ---- misc
test("jobs: the router's list survives a restart", () => {
  const d = project();
  assert.equal(gate(d, "job", "001", "agent=abc", "state=running").code, 0);
  assert.equal(gate(d, "job", "001", "state=merged").code, 0);
  const r = JSON.parse(gate(d, "jobs").out);
  assert.equal(r["001"].agent, "abc");
  assert.equal(r["001"].state, "merged");
});
test("status: lists tickets without crashing on odd folders", () => {
  const d = project();
  mkdirSync(join(d, "docs/tickets/notes"));
  const r = gate(d, "status");
  assert.equal(r.code, 0);
  assert.match(r.out, /001-invites/);
});
test("doctor: names what's wrong", () => {
  const d = project();
  const r = gate(d, "doctor");
  assert.match(r.all, /baseRef/);
});
test("no project file: commands say so instead of crashing", () => {
  const d = mkdtempSync(join(tmpdir(), "gate-"));
  git(d, "init", "-q");
  const r = gate(d, "lint", T);
  assert.equal(r.code, 1);
  assert.match(r.all, /project\.json/);
  assert.doesNotMatch(r.all, /at .*:\d+:\d+/);
});
