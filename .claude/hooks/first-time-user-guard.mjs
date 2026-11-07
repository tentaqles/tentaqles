#!/usr/bin/env node
// PreToolUse guard for the `first-time-user` subagent (.claude/agents/first-time-user.md).
//
// That agent must experience the app the way a stranger does: through the browser only.
// It may not read code, specs, docs, the database or anything else on disk. Allowed:
//   Bash  only `agent-browser …` driving pages on http://localhost:<port> / 127.0.0.1,
//         with an allowlist of subcommands and flags, no shell metacharacters, and
//         screenshots written only under <evidenceDir>/first-time-user/
//   Read  only .png/.jpg/.jpeg/.webp screenshots under that same folder
// Everything else is denied: Grep, Glob, Edit, cat, ls, curl, node, python… This is what stops the
// agent reading the code through the terminal when the Read tool is blocked.
// Unlike eval-gate.mjs this fails closed: a bug here blocks the call.
//
//   node .claude/hooks/first-time-user-guard.mjs   # reads the hook JSON on stdin

import { existsSync, readFileSync, realpathSync } from "node:fs";
import { basename, extname, join, resolve, sep } from "node:path";

const ROOT = process.env.CLAUDE_PROJECT_DIR ?? process.cwd();
const configPath = join(ROOT, ".claude/evals.config.json");
let evidenceDir = "evidence";
try {
  if (existsSync(configPath)) evidenceDir = JSON.parse(readFileSync(configPath, "utf8")).evidenceDir ?? evidenceDir;
} catch (e) {
  // Fail closed: exit 2 blocks the call.
  console.error(`first-time-user-guard: can't read .claude/evals.config.json: ${e.message}`);
  process.exit(2);
}
const SHOTS_REL = `${evidenceDir.replace(/\/+$/, "")}/first-time-user`;
const SHOTS = resolve(ROOT, SHOTS_REL);
const IMAGE = new Set([".png", ".jpg", ".jpeg", ".webp"]);
const STAY = "You're a first-time visitor: you only have the browser. Use `agent-browser` on the app URL you were given, and treat anything you can't do from the page as something a real visitor can't do either.";

// Subcommands a visitor's browser can do. Anything that runs JS, reads or writes local files,
// inspects framework internals or source, talks to other browsers, or changes cookies/headers is left out.
const SUBCOMMANDS = new Set([
  "open", "goto", "navigate", "click", "dblclick", "type", "fill", "press", "keyboard", "hover", "focus",
  "check", "uncheck", "select", "drag", "scroll", "scrollintoview", "wait", "screenshot", "snapshot",
  "close", "back", "forward", "reload", "get", "is", "find", "mouse", "set", "tab", "console", "errors",
  "diff", "session", "skills", "help",
]);
const GET_OK = new Set(["text", "value", "attr", "title", "url", "count", "box", "styles"]);
const SET_OK = new Set(["viewport", "device", "media", "offline"]);
const SKILLS_OK = new Set(["list", "get"]);
// Flags that take a value, and flags that don't. Any other flag is denied.
const VALUE_FLAGS = new Set(["--session", "-d", "--depth", "-s", "--selector", "--text", "--url", "--load", "--name", "--timeout"]);
const BARE_FLAGS = new Set(["--json", "-i", "--interactive", "-c", "--compact", "--full", "-f", "--annotate", "--exact", "--clear", "--help", "-h", "--version", "-V", "--all"]);
// Framework internals, dev-server source routes and source maps are code, not pages.
const INTERNAL = /^\/(_next|__nextjs|_vercel|_nuxt|__nuxt|@vite|@fs|@id|@react-refresh|__vite|node_modules|src|__webpack|webpack-dev-server|_app\/immutable)\b/;

const deny = (reason) => ({ hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: `${reason} ${STAY}` } });

const inside = (p, dir) => p === dir || p.startsWith(dir + sep);
const real = (p) => (existsSync(p) ? realpathSync(p) : p);

// Shell word splitting that refuses anything the shell would expand or chain. Single quotes are
// fully literal; double quotes still expand $, ` and \ (and ! in interactive shells); bare words
// also chain, redirect, glob and comment. Returns null when the command uses any of that.
function words(cmd) {
  const out = [];
  let cur = null;
  let quote = null;
  for (const ch of cmd) {
    if (quote === "'") {
      if (ch === "'") quote = null;
      else cur += ch;
    } else if (quote === '"') {
      if (ch === '"') quote = null;
      else if (/[$`\\!]/.test(ch)) return null;
      else cur += ch;
    } else if (ch === "'" || ch === '"') {
      quote = ch;
      cur ??= "";
    } else if (ch === " " || ch === "\t") {
      if (cur !== null) out.push(cur), (cur = null);
    } else if (/[;&|`$<>()\\{}[\]*?!~#\n\r]/.test(ch)) return null;
    else cur = (cur ?? "") + ch;
  }
  if (quote) return null;
  if (cur !== null) out.push(cur);
  return out;
}

function appUrl(u) {
  let url;
  try {
    url = new URL(u);
  } catch {
    return false;
  }
  if (url.protocol !== "http:" || !["localhost", "127.0.0.1"].includes(url.hostname)) return false;
  return !INTERNAL.test(url.pathname) && !url.pathname.endsWith(".map");
}

function shotPath(p, cwd) {
  const abs = real(resolve(cwd, p));
  return inside(abs, SHOTS) && IMAGE.has(extname(abs).toLowerCase());
}

function bash(cmd, cwd) {
  const w = words(cmd.trim());
  if (!w) return deny("Run one plain `agent-browser` command per call: no pipes, redirects, chaining, substitution, globs or variables. Put typed text in single quotes.");
  if (!w.length) return deny("That command is empty.");

  // The binary: agent-browser on PATH, a path to it, or `npx [-y] agent-browser[@version]`.
  let i = 0;
  if (w[0] === "npx") {
    i = 1;
    while (["-y", "--yes", "--no-install"].includes(w[i])) i++;
    if (!/^agent-browser(@[\w.-]+)?$/.test(w[i] ?? "")) return deny("Only `agent-browser` is available.");
  } else if (!["agent-browser", "agent-browser.js"].includes(basename(w[0]))) {
    return deny("Only `agent-browser` is available.");
  }
  const args = w.slice(i + 1);

  const pos = [];
  for (let j = 0; j < args.length; j++) {
    const a = args[j];
    if (/^--?[a-zA-Z]/.test(a)) {
      const [flag, inline] = a.split(/=(.*)/s);
      if (VALUE_FLAGS.has(flag)) {
        const v = inline ?? args[++j];
        if (v === undefined) return deny(`\`${flag}\` needs a value.`);
        if (flag === "--session" && !/^[\w-]+$/.test(v)) return deny("Session names are letters, digits, - and _.");
      } else if (!BARE_FLAGS.has(flag)) return deny(`The \`${flag}\` option isn't available.`);
    } else pos.push(a);
  }

  const [sub, a1] = pos;
  if (sub === undefined) return null; // bare `agent-browser --help` / `--version`
  if (!SUBCOMMANDS.has(sub)) return deny(`\`agent-browser ${sub}\` isn't available.`);

  // Every URL-looking argument must be a page on the local app.
  for (const p of pos.slice(1)) {
    if (/^([a-z][\w+.-]*:\/\/|(file|view-source|data|javascript|blob|about|chrome|devtools):)/i.test(p) && !appUrl(p)) return deny(`Only pages of the app on localhost can be opened (not \`${p}\`).`);
  }
  if (["open", "goto", "navigate"].includes(sub) && !appUrl(a1 ?? "")) return deny("`open` takes the full app URL, e.g. http://localhost:PORT/…");
  if (sub === "tab" && a1 === "new" && pos[2] !== undefined && !appUrl(pos[2])) return deny("New tabs can only open app pages.");
  if (sub === "get" && !GET_OK.has(a1)) return deny(`\`get ${a1 ?? ""}\` isn't available; use \`snapshot\` or \`get text\`.`);
  if (sub === "set" && !SET_OK.has(a1)) return deny(`\`set ${a1 ?? ""}\` isn't available.`);
  if (sub === "skills" && a1 !== undefined && !SKILLS_OK.has(a1)) return deny(`\`skills ${a1}\` isn't available.`);
  if (sub === "diff" && a1 !== "snapshot") return deny("Only `diff snapshot` is available.");
  if (sub === "screenshot") {
    const path = pos.slice(1).find((p) => !/^@e\d+$/.test(p));
    if (!path || !shotPath(path, cwd)) return deny(`Save screenshots as .png under ${SHOTS_REL}/<your run folder>/, e.g. \`agent-browser --session ftu screenshot ${SHOTS_REL}/<run>/01-home.png\`.`);
  }
  return null;
}

function read(file, cwd) {
  if (!file || !shotPath(file, cwd)) return deny(`You can only look at your own screenshots under ${SHOTS_REL}/.`);
  return null;
}

let out;
try {
  let raw = "";
  for await (const chunk of process.stdin) raw += chunk;
  const input = JSON.parse(raw || "{}");
  const ti = input.tool_input ?? {};
  const cwd = input.cwd ?? ROOT;
  if (input.tool_name === "Bash") out = bash(ti.command ?? "", cwd);
  else if (input.tool_name === "Read") out = read(ti.file_path, cwd);
  else out = deny(`The ${input.tool_name ?? "requested"} tool isn't available.`);
} catch (e) {
  console.error(`first-time-user-guard: ${e.stack ?? e}`);
  process.exit(2);
}
if (out) process.stdout.write(JSON.stringify(out));
process.exit(0);
