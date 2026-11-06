#!/usr/bin/env node
// PreToolUse guard for the first-time-user agent. It keeps the agent blind: it knows only
// what a stranger would. Two modes, from .claude/project.json:
//   browser (capabilities.browser)  only `agent-browser` on the app's local port, and its
//                                   own screenshots
//   cli                             only the help of the commands in blind.commands, and
//                                   Read of the files in blind.read
// Unlike the gate this fails closed: exit 2 blocks the call, because a leak spoils the test.
// It imports only Node built-ins, so a broken helper file can't turn it off.
import { existsSync, readFileSync, realpathSync } from "node:fs";
import { basename, extname, join, relative, resolve, sep } from "node:path";
import { spawnSync } from "node:child_process";

const SUBCOMMANDS = new Set(["open", "goto", "navigate", "click", "dblclick", "type", "fill", "press", "keyboard", "hover", "focus", "check", "uncheck", "select", "drag", "scroll", "scrollintoview", "wait", "screenshot", "snapshot", "close", "back", "forward", "reload", "get", "is", "find", "mouse", "set", "tab", "console", "errors", "diff", "session", "help"]);
const GET_OK = new Set(["text", "value", "attr", "title", "url", "count", "box", "styles"]);
const SET_OK = new Set(["viewport", "device", "media", "offline"]);
const VALUE_FLAGS = new Set(["--session", "-d", "--depth", "-s", "--selector", "--text", "--url", "--load", "--name", "--timeout"]);
const BARE_FLAGS = new Set(["--json", "-i", "--interactive", "-c", "--compact", "--full", "-f", "--annotate", "--exact", "--clear", "--help", "-h", "--version", "-V", "--all"]);
const INTERNAL = /^\/(_next|__nextjs|_vercel|_nuxt|__nuxt|@vite|@fs|@id|@react-refresh|__vite|node_modules|src|__webpack|webpack-dev-server|_app\/immutable)(\/|$)/;
// The production bundle is code too, wherever a framework puts it.
const CODE_EXT = new Set([".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".map", ".css", ".vue", ".svelte", ".wasm", ".json", ".env"]);
const IMAGE = new Set([".png", ".jpg", ".jpeg", ".webp"]);
let SHOTS = ".job/first-time-user"; // follows jobDir, set once the profile is read

const deny = (reason) => {
  process.stdout.write(JSON.stringify({ hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: `${reason} You only have what a stranger would have: treat anything else as out of reach, and note it in your report.` } }));
  process.exit(0);
};
const allow = () => process.exit(0);
const inside = (p, dir) => p === dir || p.startsWith(dir + sep);
const real = (p) => { try { return existsSync(p) ? realpathSync(p) : p; } catch { return p; } };

/** Shell words, or null if the command uses anything the shell would expand or chain. */
export function words(cmd) {
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
      if (cur !== null) { out.push(cur); cur = null; }
    } else if (/[;&|`$<>()\\{}[\]*?!~#\n\r]/.test(ch)) return null;
    else cur = (cur ?? "") + ch;
  }
  if (quote) return null;
  if (cur !== null) out.push(cur);
  return out;
}

function appUrl(u, port) {
  let url;
  try { url = new URL(u); } catch { return false; }
  if (url.protocol !== "http:" || !["localhost", "127.0.0.1"].includes(url.hostname)) return false;
  if (String(url.port || "80") !== String(port)) return false;
  let path;
  try { path = decodeURIComponent(url.pathname); } catch { return false; }
  if (INTERNAL.test(path)) return false;
  return !CODE_EXT.has(extname(path).toLowerCase());
}

function browserBash(cmd, cwd, root, port) {
  const w = words(cmd.trim());
  if (!w || !w.length) return deny("Run one plain `agent-browser` command per call: no pipes, redirects, chaining, substitution, globs or variables. Put typed text in single quotes.");
  let i = 0;
  if (w[0] === "npx") {
    i = 1;
    while (["-y", "--yes", "--no-install"].includes(w[i])) i++;
    if (!/^agent-browser(@[\w.-]+)?$/.test(w[i] ?? "")) return deny("Only `agent-browser` is available.");
  } else if (!/^agent-browser(\.cmd|\.exe|\.js|\.ps1)?$/i.test(basename(w[0]))) {
    return deny("Only `agent-browser` is available.");
  }
  const args = w.slice(i + 1);
  const pos = [];
  const urlish = (v) => /^([a-z][\w+.-]*:\/\/|(file|view-source|data|javascript|blob|about|chrome|devtools):)/i.test(v);
  for (let j = 0; j < args.length; j++) {
    const a = args[j];
    if (/^--?[a-zA-Z]/.test(a)) {
      const [flag, inline] = a.split(/=(.*)/s);
      if (VALUE_FLAGS.has(flag)) {
        const v = inline ?? args[++j];
        if (v === undefined) return deny(`\`${flag}\` needs a value.`);
        if (flag === "--session" && !/^[\w-]+$/.test(v)) return deny("Session names are letters, digits, - and _.");
        if (urlish(v) && !appUrl(v, port)) return deny(`Only pages of the app on localhost:${port} can be opened.`);
      } else if (!BARE_FLAGS.has(flag)) return deny(`The \`${flag}\` option isn't available.`);
    } else pos.push(a);
  }
  const [sub, a1] = pos;
  if (sub === undefined) return allow();
  if (!SUBCOMMANDS.has(sub)) return deny(`\`agent-browser ${sub}\` isn't available.`);
  for (const p of pos.slice(1)) {
    if (urlish(p) && !appUrl(p, port)) return deny(`Only pages of the app on localhost:${port} can be opened.`);
  }
  if (["open", "goto", "navigate"].includes(sub) && !appUrl(a1 ?? "", port)) return deny(`\`${sub}\` takes a full app URL, like http://localhost:${port}/`);
  if (sub === "get" && !GET_OK.has(a1)) return deny(`\`get ${a1 ?? ""}\` isn't available.`);
  if (sub === "set" && !SET_OK.has(a1)) return deny(`\`set ${a1 ?? ""}\` isn't available.`);
  if (sub === "diff" && a1 !== "snapshot") return deny("Only `diff snapshot` is available.");
  if (sub === "screenshot") {
    const paths = pos.slice(1).filter((p) => !/^@e\d+$/.test(p));
    if (!paths.length || !paths.every((p) => isShot(p, cwd, root))) return deny(`Save screenshots as .png under ${SHOTS}/.`);
  }
  return allow();
}

function isShot(p, cwd, root) {
  const abs = real(resolve(cwd, p));
  return inside(abs, resolve(root, SHOTS)) && IMAGE.has(extname(abs).toLowerCase());
}

function cliBash(cmd, commands, cwd, root) {
  const w = words(cmd.trim().replace(/\\/g, "/"));
  if (!w || !w.length) return deny("Run one plain command per call: no pipes, redirects, chaining, substitution, globs or variables.");
  const name = w[0].replace(/\.exe$/i, "");
  // An entry with a path is that exact file. A bare name is whatever is on PATH.
  const matches = (c) => {
    if (!c.includes("/")) return name === c;
    return name.includes("/") && resolve(cwd, name).toLowerCase() === resolve(root, c).toLowerCase();
  };
  if (!commands.some(matches)) return deny(`Only ${commands.join(", ")} is available.`);
  const rest = w.slice(1);
  const help = rest.length === 0 || ["--help", "-h", "help", "--version", "version"].includes(rest[0]) ||
    (rest.at(-1) === "--help" || rest.at(-1) === "-h");
  if (!help || rest.length > 4 || rest.some((a) => /[=/]/.test(a))) return deny("You can only read the command's own help here: the command with --help, or a subcommand with --help.");
  return allow();
}

function loadConfig(root) {
  const p = join(root, ".claude", "project.json");
  if (!existsSync(p)) throw new Error("no .claude/project.json");
  return JSON.parse(readFileSync(p, "utf8"));
}

let raw = "";
try {
  const timer = setTimeout(() => { process.stderr.write("blind guard: no input\n"); process.exit(2); }, 2000);
  for await (const chunk of process.stdin) raw += chunk;
  clearTimeout(timer);
  const input = JSON.parse(raw || "{}");
  const cwd = input.cwd ?? process.cwd();
  const top = spawnSync("git", ["rev-parse", "--show-toplevel"], { cwd, encoding: "utf8", timeout: 1500, windowsHide: true });
  const root = top.status === 0 ? resolve(top.stdout.trim()) : resolve(cwd);
  const cfg = loadConfig(root);
  if (cfg.jobDir) SHOTS = `${cfg.jobDir.replace(/\/+$/, "")}/first-time-user`;
  const browser = !!cfg.capabilities?.browser;
  const tool = input.tool_name;
  const ti = input.tool_input ?? {};
  if (tool === "Bash") {
    if (browser) browserBash(String(ti.command ?? ""), cwd, root, cfg.blind?.port ?? cfg.app?.ports?.main ?? 3000);
    else cliBash(String(ti.command ?? ""), cfg.blind?.commands ?? [], cwd, root);
  } else if (tool === "Read") {
    const file = String(ti.file_path ?? "");
    const abs = real(resolve(cwd, file.replace(/\\/g, "/")));
    if (browser ? isShot(file, cwd, root) : (cfg.blind?.read ?? []).some((r) => {
      const rel = relative(resolve(root), abs).replace(/\\/g, "/");
      return rel === r;
    })) allow();
    deny(browser ? `You can only look at your own screenshots under ${SHOTS}/.` : `You can only read: ${(cfg.blind?.read ?? []).join(", ") || "nothing"}.`);
  } else {
    deny(`The ${tool ?? "requested"} tool isn't available.`);
  }
} catch (e) {
  process.stderr.write(`blind guard: ${e.message}\n`);
  process.exit(2);
}
