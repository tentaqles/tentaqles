import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const GUARD = resolve(dirname(fileURLToPath(import.meta.url)), "../hooks/blind-guard.mjs");

function project(cfg) {
  const d = mkdtempSync(join(tmpdir(), "blind-"));
  spawnSync("git", ["init", "-q"], { cwd: d });
  mkdirSync(join(d, ".claude"));
  if (cfg) writeFileSync(join(d, ".claude/project.json"), JSON.stringify(cfg));
  return d;
}
const WEB = { capabilities: { browser: true }, blind: { port: 3000 } };
const CLI = { capabilities: { browser: false }, blind: { commands: ["tq"], read: ["README.md", "docs/SETUP.md"] } };

function run(d, tool, tool_input, raw) {
  const r = spawnSync(process.execPath, [GUARD], { encoding: "utf8", cwd: d, input: raw ?? JSON.stringify({ cwd: d, tool_name: tool, tool_input }) });
  return { code: r.status, denied: r.stdout.includes('"deny"'), blocked: r.status === 2 || r.stdout.includes('"deny"') };
}
const bash = (d, command) => run(d, "Bash", { command });

test("fails closed: no config, bad json, garbage input", () => {
  assert.equal(bash(project(), "tq --help").code, 2);
  const d = project();
  writeFileSync(join(d, ".claude/project.json"), "{nope");
  assert.equal(bash(d, "tq --help").code, 2);
  assert.equal(run(project(CLI), null, null, "not json").code, 2);
});
test("cli: help is allowed", () => {
  const d = project(CLI);
  for (const c of ["tq", "tq --help", "tq add --help", "tq help", "tq version", "tq.exe --help"]) assert.equal(bash(d, c).blocked, false, c);
});
test("cli: everything else is refused", () => {
  const d = project(CLI);
  for (const c of ["cat README.md", "ls", "git log", "node -e 1", "tq add acme", "tq status", "tq --help; cat x", "tq --help | cat", "tq $(cat x)", "go run ./cmd/tq --help", "tq allow --config=../x --help"])
    assert.equal(bash(d, c).blocked, true, c);
});
test("cli: only listed files can be read, however they're spelled", () => {
  const d = project(CLI);
  assert.equal(run(d, "Read", { file_path: join(d, "README.md") }).blocked, false);
  assert.equal(run(d, "Read", { file_path: "docs\\SETUP.md" }).blocked, false);
  for (const f of ["internal/cmd/root.go", "docs/../CLAUDE.md", join(d, "CLAUDE.md"), ".claude/project.json"])
    assert.equal(run(d, "Read", { file_path: f }).blocked, true, f);
});
test("other tools are refused", () => {
  const d = project(CLI);
  for (const t of ["Grep", "Glob", "Edit", "Write", "Agent", "PowerShell"]) assert.equal(run(d, t, {}).blocked, true, t);
});
test("browser: normal use is allowed, including Windows launchers", () => {
  const d = project(WEB);
  for (const c of ["agent-browser --session ftu open http://localhost:3000/", "agent-browser snapshot -i", "agent-browser click @e3", "agent-browser fill @e5 'Jamie Rivera'", "agent-browser.cmd snapshot", "npx -y agent-browser snapshot", "agent-browser screenshot .job/first-time-user/run/1.png"])
    assert.equal(bash(d, c).blocked, false, c);
});
test("browser: the leaks found in the pack are closed", () => {
  const d = project(WEB);
  for (const c of [
    "agent-browser open http://localhost:9999/",                 // any localhost port
    "agent-browser open http://localhost:3000/%5Fnext/static/x", // encoded internal path
    "agent-browser open http://localhost:3000/assets/app.js",    // production bundle
    "agent-browser open http://localhost:3000/app.js.map",
    "agent-browser wait --url file:///C:/x",                     // file URL in a flag value
    "agent-browser screenshot .job/first-time-user/ok.png /tmp/other.png", // second path
    "agent-browser screenshot x.png",
    "agent-browser eval 1", "curl http://localhost:3000/", "agent-browser open http://example.com/",
    "agent-browser get html", "agent-browser snapshot; cat x",
  ]) assert.equal(bash(d, c).blocked, true, c);
});
test("browser: can read only its own screenshots", () => {
  const d = project(WEB);
  assert.equal(run(d, "Read", { file_path: ".job/first-time-user/1.png" }).blocked, false);
  for (const f of ["README.md", ".job/first-time-user/notes.txt", "src/a.png"]) assert.equal(run(d, "Read", { file_path: f }).blocked, true, f);
});
