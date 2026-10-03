#!/usr/bin/env node
// shots.mjs: screenshots of Goal Tracker pages in headless Chromium, with the
// facts a reviewer checks printed beside each one.
//
//   node scripts/shots.mjs <base-url> <shots> [--out <dir>]
//
// <shots> is a JSON file, `-` for stdin, or the JSON itself: an array of
// shots (or one shot), each
//
//   path    the page, e.g. "/goals/3" (required)
//   as      the email to sign in as; omit to stay signed out
//   theme   "light" or "dark": the colour scheme the OS asks for (light)
//   pin     "light" or "dark": pin the Theme menu's choice (none)
//   width   window width in CSS pixels (1280); height (800)
//   full    false to capture only the window, not the whole page (true)
//   action  JavaScript run in the page once it has loaded, e.g.
//           "document.querySelector('form.x').requestSubmit()"; it may
//           return a promise. The shot is taken once the page settles.
//   dialog  "dismiss" (default) or "accept" a native alert/confirm
//   name    the PNG's name (default from the index, path, theme and width)
//
// For each shot it saves <out>/<name>.png and prints the
// theme pin, horizontal overflow, the focused element, any toast, and any
// dialog that opened. It drives the Chromium already on the machine over the
// DevTools protocol, so nothing needs installing; set CHROME to its path if
// it isn't found. Exits 1 if any shot failed to load or sign in.

import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { Script } from "node:vm";

const usage = "usage: node scripts/shots.mjs <base-url> <shots.json|-|JSON> [--out <dir>]";

function parseArgs(argv) {
  const args = [...argv];
  let out = join(tmpdir(), "goal-tracker-shots");
  const i = args.indexOf("--out");
  if (i >= 0) {
    out = args[i + 1];
    args.splice(i, 2);
  }
  if (args.length !== 2 || !out) fail(usage);
  const [base, spec] = args;
  const text = spec === "-" ? readFileSync(0, "utf8") : /^\s*[[{]/.test(spec) ? spec : readFileSync(spec, "utf8");
  let shots;
  try {
    shots = [JSON.parse(text)].flat();
  } catch (err) {
    fail(`shots aren't valid JSON: ${err.message}`);
  }
  try {
    shots = shots.map(normalise);
  } catch (err) {
    fail(err.message);
  }
  return { base: base.replace(/\/+$/, ""), shots, out: resolve(out) };
}

// normalise fills in a shot's defaults, and throws if a field is invalid.
export function normalise(shot, i) {
  const bad = (message) => {
    throw new Error(`shot ${i + 1}: ${message}`);
  };
  if (typeof shot?.path !== "string" || !shot.path.startsWith("/")) bad("path must start with /");
  const s = { theme: "light", width: 1280, height: 800, full: true, dialog: "dismiss", ...shot };
  if (!["light", "dark"].includes(s.theme)) bad("theme must be light or dark");
  if (s.pin !== undefined && !["light", "dark"].includes(s.pin)) bad("pin must be light or dark");
  if (!["accept", "dismiss"].includes(s.dialog)) bad("dialog must be accept or dismiss");
  if (s.status !== undefined && !Number.isInteger(s.status)) bad("status must be an integer");
  const slug = s.path.replace(/[^\w]+/g, "-").replace(/^-|-$/g, "") || "root";
  s.name ??= `${String(i + 1).padStart(2, "0")}-${slug}-${s.theme}-${s.width}`;
  return s;
}

// wrapAction is the script that runs a shot's action: the action itself if it
// compiles as a script, so it keeps its completion value, else the action as
// the body of an async function, so it can use return and await.
export function wrapAction(action) {
  try {
    new Script(action);
    return action;
  } catch (err) {
    if (!(err instanceof SyntaxError)) throw err;
    return `(async () => {\n${action}\n})()`;
  }
}

// statusProblem is why a page's final document status fails the shot, or null
// if it doesn't: an HTTP error unless the shot expects a status, else any
// status but the expected one. A status that couldn't be read never fails.
export function statusProblem(shot, status) {
  if (status == null) return null;
  if (shot.status === undefined) return status >= 400 ? `the page gave HTTP ${status}` : null;
  return status === shot.status ? null : `the page gave HTTP ${status}, not the expected ${shot.status}`;
}

function fail(message) {
  console.error(`shots.mjs: ${message}`);
  process.exit(2);
}

// findChrome is the Chromium to drive: $CHROME, else the first browser of the
// Chrome family on PATH, else the macOS app bundles.
function findChrome() {
  if (process.env.CHROME) return process.env.CHROME;
  const names = ["chromium", "chromium-browser", "google-chrome-stable", "google-chrome", "chrome", "chrome-headless-shell", "microsoft-edge"];
  for (const dir of (process.env.PATH ?? "").split(delimiter)) {
    for (const name of names) if (dir && existsSync(join(dir, name))) return join(dir, name);
  }
  for (const app of ["/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"]) {
    if (existsSync(app)) return app;
  }
  fail("no Chromium found; set CHROME to its path");
}

// launch starts headless Chromium and resolves to it and its DevTools
// WebSocket URL. Where the OS gives Chromium no sandbox (some containers), it
// runs again without one; it only ever loads the pages it is pointed at.
async function launch(chrome, sandbox = true) {
  const profile = mkdtempSync(join(tmpdir(), "shots-profile-"));
  const flags = ["--headless", "--remote-debugging-port=0", `--user-data-dir=${profile}`, "--hide-scrollbars",
    "--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-extensions", "--mute-audio"];
  if (!sandbox) flags.push("--no-sandbox");
  const proc = spawn(chrome, [...flags, "about:blank"], { stdio: ["ignore", "ignore", "pipe"] });
  const exited = new Promise((r) => proc.on("exit", r));
  let stderr = "";
  const ws = await new Promise((done, failed) => {
    proc.stderr.on("data", (chunk) => {
      stderr += chunk;
      const m = stderr.match(/DevTools listening on (ws:\/\/\S+)/);
      if (m) done(m[1]);
    });
    proc.on("error", failed);
    proc.on("exit", () => done(null));
  });
  const stop = async () => {
    proc.kill();
    await exited;
    rmSync(profile, { recursive: true, force: true, maxRetries: 5 });
  };
  if (ws) return { ws, stop };
  await stop();
  if (sandbox && /No usable sandbox/.test(stderr)) {
    console.error("shots.mjs: Chromium has no usable sandbox here, so it runs without one");
    return launch(chrome, false);
  }
  fail(`${chrome} didn't start:\n${stderr.trim().split("\n").slice(-5).join("\n")}`);
}

// CDP is a minimal DevTools protocol client: commands, and events, both
// optionally scoped to one attached page's session.
class CDP {
  static connect(url) {
    return new Promise((done, failed) => {
      const ws = new WebSocket(url);
      ws.onopen = () => done(new CDP(ws));
      ws.onerror = () => failed(new Error(`can't connect to ${url}`));
    });
  }

  constructor(ws) {
    this.ws = ws;
    this.next = 1;
    this.pending = new Map();
    this.listeners = new Set();
    ws.onmessage = ({ data }) => {
      const msg = JSON.parse(data);
      if (msg.id) {
        const p = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        if (msg.error) p?.failed(new Error(`${p.method}: ${msg.error.message}`));
        else p?.done(msg.result);
        return;
      }
      for (const l of this.listeners) if (l.method === msg.method && l.session === msg.sessionId) l.fn(msg.params);
    };
  }

  send(method, params = {}, session) {
    const id = this.next++;
    this.ws.send(JSON.stringify({ id, method, params, sessionId: session }));
    return new Promise((done, failed) => this.pending.set(id, { method, done, failed }));
  }

  on(method, session, fn) {
    const l = { method, session, fn };
    this.listeners.add(l);
    return () => this.listeners.delete(l);
  }

  close() {
    this.ws.close();
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// signIn signs in through the development sign-in form and returns the
// session cookies it sets, for the browser to carry.
async function signIn(base, email) {
  const res = await fetch(`${base}/signin`, {
    method: "POST",
    body: new URLSearchParams({ email }),
    redirect: "manual",
  });
  const cookies = res.headers.getSetCookie().map((c) => {
    const [name, ...value] = c.split(";")[0].split("=");
    return { name, value: value.join("="), url: base };
  });
  if (res.status !== 303 || cookies.length === 0) throw new Error(`signing in as ${email} gave HTTP ${res.status}`);
  return cookies;
}

// facts is evaluated in the page: what a reviewer checks without looking.
function facts() {
  // Rendered and not hidden: a closed <details> still lays its content out.
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && (el.checkVisibility?.({ visibilityProperty: true }) ?? true);
  };
  const text = (el, n = 60) => {
    const t = (el.innerText || el.value || el.getAttribute("aria-label") || "").replace(/\s+/g, " ").trim();
    return t.length > n ? `${t.slice(0, n - 1)}…` : t;
  };
  const describe = (el) => {
    let d = el.tagName.toLowerCase();
    if (el.id) d += `#${el.id}`;
    for (const a of ["data-testid", "name", "type"]) if (el.hasAttribute(a)) d += `[${a}=${el.getAttribute(a)}]`;
    if (el.classList.length) d += `.${[...el.classList].join(".")}`;
    return d;
  };

  const root = document.documentElement;
  const vw = root.clientWidth;
  // An element past the window's edges, unless an ancestor inside the window
  // scrolls or clips it (a table's scroller, an ellipsis). Only the outermost
  // of a run is listed.
  const past = [];
  for (const el of document.body.querySelectorAll("*")) {
    const r = el.getBoundingClientRect();
    if (r.right <= vw + 0.5 && r.left >= -0.5) continue;
    if (!visible(el) || past.some((p) => p.contains(el))) continue;
    let contained = false;
    for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
      if (getComputedStyle(p).overflowX === "visible") continue;
      const pr = p.getBoundingClientRect();
      if (pr.right <= vw + 0.5 && pr.left >= -0.5) {
        contained = true;
        break;
      }
    }
    if (!contained) past.push(el);
  }

  const active = document.activeElement;
  const toast = [...document.querySelectorAll('[data-testid="toast"], .toast')].find(visible);
  const dialogs = [...document.querySelectorAll('dialog[open], [role="dialog"], [role="alertdialog"]')].filter(visible);
  return {
    url: location.pathname + location.search,
    title: document.title,
    pin: root.dataset.theme ?? null,
    osDark: matchMedia("(prefers-color-scheme: dark)").matches,
    background: getComputedStyle(document.body).backgroundColor,
    scrollsBy: root.scrollWidth - vw,
    past: past.map((el) => `${describe(el)} (${Math.round(el.getBoundingClientRect().left)}–${Math.round(el.getBoundingClientRect().right)}px)`),
    width: vw,
    focus: !active || active === document.body ? null : `${describe(active)} "${text(active, 40)}"`,
    toast: toast ? text(toast) : null,
    dialogs: dialogs.map((d) => `${describe(d)} "${text(d)}"`),
  };
}

// settle waits for a navigation that has started to finish loading, then for
// htmx to finish its requests.
async function settle(cdp, session, navigating) {
  await sleep(300);
  if (navigating()) await navigating.loaded;
  for (let i = 0; i < 50; i++) {
    const { result } = await cdp.send("Runtime.evaluate", {
      expression: 'document.readyState === "complete" && !document.querySelector(".htmx-request")',
      returnByValue: true,
    }, session);
    if (result.value) break;
    await sleep(100);
  }
  await sleep(200);
}

// watchNavigation reports whether the page has started loading a new
// document since it was called, and resolves .loaded once that has loaded.
function watchNavigation(cdp, session) {
  let started = false;
  let loaded;
  const done = new Promise((r) => (loaded = r));
  const offStart = cdp.on("Page.frameStartedLoading", session, () => (started = true));
  const offLoad = cdp.on("Page.loadEventFired", session, () => loaded());
  const navigating = () => started;
  navigating.loaded = Promise.race([done, sleep(15000)]).finally(() => {
    offStart();
    offLoad();
  });
  navigating.stop = () => {
    offStart();
    offLoad();
    loaded();
  };
  return navigating;
}

async function shoot(cdp, base, out, shot) {
  const { browserContextId } = await cdp.send("Target.createBrowserContext");
  try {
    const cookies = shot.as ? await signIn(base, shot.as) : [];
    if (shot.pin) cookies.push({ name: "gt_theme", value: shot.pin, url: base });
    if (cookies.length) await cdp.send("Storage.setCookies", { cookies, browserContextId });

    const { targetId } = await cdp.send("Target.createTarget", { url: "about:blank", browserContextId });
    const { sessionId: s } = await cdp.send("Target.attachToTarget", { targetId, flatten: true });
    await cdp.send("Page.enable", {}, s);
    await cdp.send("Runtime.enable", {}, s);
    await cdp.send("Network.enable", {}, s);
    await cdp.send("Emulation.setDeviceMetricsOverride", { width: shot.width, height: shot.height, deviceScaleFactor: 1, mobile: false }, s);
    await cdp.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: shot.theme }] }, s);

    // The HTTP status of the document last loaded, after any redirects.
    let status = null;
    cdp.on("Network.responseReceived", s, ({ type, response }) => {
      if (type === "Document") status = response.status;
    });

    const native = [];
    cdp.on("Page.javascriptDialogOpening", s, ({ type, message }) => {
      native.push(`${type} "${message}" (${shot.dialog === "accept" ? "accepted" : "dismissed"})`);
      cdp.send("Page.handleJavaScriptDialog", { accept: shot.dialog === "accept" }, s);
    });

    const nav = watchNavigation(cdp, s);
    const { errorText } = await cdp.send("Page.navigate", { url: base + shot.path }, s);
    if (errorText) throw new Error(`loading ${shot.path}: ${errorText}`);
    await nav.loaded;
    await settle(cdp, s, () => false);

    if (shot.action) {
      const navigating = watchNavigation(cdp, s);
      const { exceptionDetails } = await cdp
        .send("Runtime.evaluate", { expression: wrapAction(shot.action), awaitPromise: true, userGesture: true }, s)
        // A submit that navigates away destroys the context the action ran in.
        .catch(() => ({}));
      if (exceptionDetails) throw new Error(`action: ${exceptionDetails.exception?.description ?? exceptionDetails.text}`);
      await settle(cdp, s, navigating);
      navigating.stop();
    }

    const { result } = await cdp.send("Runtime.evaluate", { expression: `(${facts})()`, returnByValue: true }, s);
    const f = result.value;
    if (shot.as && f.url.startsWith("/signin")) throw new Error(`landed on ${f.url}: the sign-in as ${shot.as} didn't hold`);

    const { cssContentSize } = await cdp.send("Page.getLayoutMetrics", {}, s);
    const height = shot.full ? Math.max(shot.height, Math.ceil(cssContentSize.height)) : shot.height;
    const { data } = await cdp.send("Page.captureScreenshot", {
      format: "png",
      captureBeyondViewport: shot.full,
      clip: { x: 0, y: 0, width: shot.width, height, scale: 1 },
    }, s);
    const file = join(out, `${shot.name}.png`);
    writeFileSync(file, Buffer.from(data, "base64"));
    return { file, facts: { ...f, status }, native };
  } finally {
    await cdp.send("Target.disposeBrowserContext", { browserContextId }).catch(() => {});
  }
}

// formatReport is what's printed for a shot that was taken.
export function formatReport(shot, { file, facts: f, native }) {
  const who = shot.as ? `as ${shot.as}` : "signed out";
  const pin = f.pin ? `pinned ${f.pin}` : `not pinned, follows the OS (${f.osDark ? "dark" : "light"})`;
  const overflow = [];
  if (f.scrollsBy > 0) overflow.push(`the page scrolls sideways by ${f.scrollsBy}px`);
  if (f.past.length) overflow.push(`past the ${f.width}px window: ${f.past.slice(0, 5).join(", ")}${f.past.length > 5 ? ` and ${f.past.length - 5} more` : ""}`);
  const dialogs = [...native, ...f.dialogs];
  return `${file}
  shot:     ${shot.path} ${who}, OS ${shot.theme}, ${shot.width}px${shot.action ? ", after the action" : ""}
  page:     ${f.url} (HTTP ${f.status ?? "?"}) ${f.title ? `"${f.title}"` : "(none)"}
  theme:    ${pin}; body background ${f.background}
  overflow: ${overflow.length ? overflow.join("; ") : "none"}
  focus:    ${f.focus ?? "nothing (body)"}
  toast:    ${f.toast ? `"${f.toast}"` : "none"}
  dialog:   ${dialogs.length ? dialogs.join("; ") : "none"}
`;
}

async function main() {
  const { base, shots, out } = parseArgs(process.argv.slice(2));
  mkdirSync(out, { recursive: true });
  const chrome = await launch(findChrome());
  let failed = 0;
  try {
    const cdp = await CDP.connect(chrome.ws);
    for (const shot of shots) {
      try {
        const result = await shoot(cdp, base, out, shot);
        console.log(formatReport(shot, result));
        const problem = statusProblem(shot, result.facts.status);
        if (problem) {
          failed++;
          console.log(`${shot.name}: FAILED: ${problem}\n`);
        }
      } catch (err) {
        failed++;
        console.log(`${shot.name}: FAILED: ${err.message}\n`);
      }
    }
    cdp.close();
  } finally {
    await chrome.stop();
  }
  process.exit(failed ? 1 : 0);
}

// Run only when executed, not when imported by the tests.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) await main();
