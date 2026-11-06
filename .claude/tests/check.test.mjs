import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const CHECK = resolve(dirname(fileURLToPath(import.meta.url)), "../watch/check.mjs");
const T = "001-x";
const DAY = 24 * 3600 * 1000;

const git = (d, ...a) => spawnSync("git", a, { cwd: d, encoding: "utf8" }).stdout.trim();
function setup(status = { step: "build", updated: new Date().toISOString() }, rd = "# x\n\n## Affected areas\n- `src/`\n\n## Do not touch\n- `locked/`\n") {
  const d = mkdtempSync(join(tmpdir(), "check-"));
  git(d, "init", "-q", "-b", "main");
  git(d, "config", "user.email", "t@example.com");
  git(d, "config", "user.name", "t");
  mkdirSync(join(d, ".claude"));
  writeFileSync(join(d, ".claude/project.json"), JSON.stringify({ limits: { stepStaleMinutes: 45 } }));
  writeFileSync(join(d, ".gitignore"), ".job/\n");
  mkdirSync(join(d, "docs/tickets", T), { recursive: true });
  writeFileSync(join(d, "docs/tickets", T, "rd.md"), rd);
  git(d, "add", "-A"); git(d, "commit", "-qm", "base");
  const base = git(d, "rev-parse", "HEAD");
  mkdirSync(join(d, ".job", T), { recursive: true });
  writeFileSync(join(d, ".job/ticket.json"), JSON.stringify({ ticket: T, base }));
  if (status) writeFileSync(join(d, ".job", T, "status.json"), typeof status === "string" ? status : JSON.stringify(status));
  return d;
}
function run(d, now) {
  const r = spawnSync(process.execPath, [CHECK, ...(now ? ["--now", String(now)] : [])], { cwd: d, encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
  return r.stdout.split("\n").filter(Boolean).map((l) => JSON.parse(l));
}
function commit(d, path) {
  mkdirSync(dirname(join(d, path)), { recursive: true });
  writeFileSync(join(d, path), "x");
  git(d, "add", "-A"); git(d, "commit", "-qm", "work");
}

test("silence when all is well", () => {
  const d = setup();
  commit(d, "src/a.txt");
  assert.deepEqual(run(d), []);
});
test("stale after the limit, and the detail says to ask first", () => {
  const d = setup();
  const [p] = run(d, Date.now() + DAY);
  assert.equal(p.kind, "stale");
  assert.match(p.detail, /Ask before stopping/);
});
test("recent commit or edit counts as life though status is old", () => {
  const d = setup({ step: "build", updated: "2020-01-01T00:00:00Z" });
  commit(d, "src/a.txt");
  assert.deepEqual(run(d), []);
});
test("blocked and failed are reported with the note", () => {
  assert.match(run(setup({ step: "blocked", note: "which copy?" }))[0].detail, /which copy/);
  assert.equal(run(setup({ step: "failed", note: "gave up" }))[0].kind, "failed");
});
test("missing or corrupt status is reported, not fatal", () => {
  assert.equal(run(setup(null))[0].kind, "stale");
  assert.equal(run(setup("{nope"))[0].kind, "stale");
});
test("drift: outside affected areas and do-not-touch", () => {
  const d = setup();
  commit(d, "other/a.txt");
  commit(d, "locked/b.txt");
  const details = run(d).map((p) => p.detail).join("\n");
  assert.match(details, /other\/a\.txt is outside/);
  assert.match(details, /locked\/b\.txt is under Do not touch/);
});
test("drift survives CRLF in rd.md", () => {
  const d = setup(undefined, "# x\r\n\r\n## Affected areas\r\n- `src/`\r\n");
  commit(d, "other/a.txt");
  assert.match(run(d)[0].detail, /outside/);
});
test("a rejected alarm stays quiet, then repeats after an hour", () => {
  const d = setup();
  const t = Date.now() + DAY;
  assert.equal(run(d, t).length, 1);
  assert.equal(run(d, t + 10 * 60000).length, 0);
  assert.equal(run(d, t + 61 * 60000).length, 1);
});
test("foreign worktrees and done tickets are ignored", () => {
  const d = setup({ step: "done", updated: "2020-01-01T00:00:00Z" });
  mkdirSync(join(d, ".claude/worktrees/foreign"), { recursive: true });
  assert.deepEqual(run(d, Date.now() + DAY), []);
});
test("no project file: exits 0 and prints nothing", () => {
  const d = mkdtempSync(join(tmpdir(), "check-"));
  const r = spawnSync(process.execPath, [CHECK], { cwd: d, encoding: "utf8" });
  assert.equal(r.status, 0);
  assert.equal(r.stdout, "");
});
