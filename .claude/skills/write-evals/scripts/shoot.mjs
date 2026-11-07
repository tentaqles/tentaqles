#!/usr/bin/env node
// Screenshot a page or a mockup at several widths and measure horizontal scroll.
// Used by write-evals (check a mockup renders), the verifier (design checks) and reviews (360 px checks).
//
//   node .claude/skills/write-evals/scripts/shoot.mjs <url or .html file> <out prefix> [--widths 360,1280] [--cookie name=value] [--height 900]
//
//   node .claude/skills/write-evals/scripts/shoot.mjs specs/features/team-invites/mockup.html evidence/team-invites/mockup
//   node .claude/skills/write-evals/scripts/shoot.mjs http://localhost:3200/settings/team evidence/team-invites/built --cookie session=abc
//
// Writes <out prefix>-<width>.png (full page) for each width and prints one JSON line per width:
//   {"width":360,"file":"…-360.png","scrollWidth":360,"horizontalScroll":false}
// horizontalScroll is a measured fact: the page is wider than the viewport.
//
// Needs Playwright in the project (`playwright` or `playwright-core`). It uses installed Google Chrome when
// there is one, otherwise Playwright's own Chromium (`npx playwright install chromium`).

import { createRequire } from "node:module";
import { existsSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const i = args.indexOf(`--${name}`);
  return i === -1 ? fallback : args[i + 1];
};
const [target, prefix] = args.filter((a, i) => !a.startsWith("--") && !args[i - 1]?.startsWith("--"));
if (!target || !prefix) {
  console.error("usage: shoot.mjs <url or .html file> <out prefix> [--widths 360,1280] [--cookie name=value] [--height 900]");
  process.exit(2);
}
const widths = flag("widths", "360,1280").split(",").map(Number);
const height = Number(flag("height", "900"));
const cookie = flag("cookie", null);

// Resolve Playwright from the project, not from this script's folder.
const require = createRequire(resolve(process.cwd(), "package.json"));
let chromium;
for (const name of ["playwright", "playwright-core"]) {
  try {
    ({ chromium } = require(name));
    break;
  } catch {}
}
if (!chromium) {
  console.error("Playwright isn't installed in this project. Install it (`npm i -D playwright-core`) or take the screenshots with agent-browser instead.");
  process.exit(2);
}

const url = /^https?:\/\//.test(target) ? target : pathToFileURL(resolve(target)).href;
if (!url.startsWith("http") && !existsSync(resolve(target))) {
  console.error(`No such file: ${target}`);
  process.exit(2);
}

let browser;
try {
  browser = await chromium.launch({ channel: "chrome" });
} catch {
  browser = await chromium.launch();
}
try {
  for (const width of widths) {
    const context = await browser.newContext({ viewport: { width, height } });
    if (cookie && url.startsWith("http")) {
      const [name, ...rest] = cookie.split("=");
      await context.addCookies([{ name, value: rest.join("="), url }]);
    }
    const page = await context.newPage();
    await page.goto(url, { waitUntil: "networkidle" });
    const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    const file = `${prefix}-${width}.png`;
    mkdirSync(dirname(resolve(file)), { recursive: true });
    await page.screenshot({ path: file, fullPage: true });
    console.log(JSON.stringify({ width, file, scrollWidth, horizontalScroll: scrollWidth > width }));
    await context.close();
  }
} finally {
  await browser.close();
}
