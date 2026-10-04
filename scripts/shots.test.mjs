// Tests for shots.mjs: the parts that don't need a browser, and, where
// Chromium is on the machine, a shot taken in it. Run with
//   node --test scripts/*.test.mjs
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { inflateSync } from "node:zlib";

import { captureParams, formatReport, normalise, statusProblem, wrapAction } from "./shots.mjs";

test("a shot's status must be an integer", () => {
  assert.throws(() => normalise({ path: "/goals", status: "404" }, 0), /shot 1: status must be an integer/);
  assert.throws(() => normalise({ path: "/goals", status: 404.5 }, 0), /status must be an integer/);
  assert.equal(normalise({ path: "/goals", status: 404 }, 0).status, 404);
});

test("a shot's name may end in .png without saving .png.png", () => {
  assert.equal(normalise({ path: "/goals", name: "b.png" }, 0).file, "b.png");
  assert.equal(normalise({ path: "/goals", name: "b" }, 0).file, "b.png");
  assert.equal(normalise({ path: "/goals", name: "B.PNG" }, 0).file, "B.png");
  assert.equal(normalise({ path: "/goals/3", theme: "dark", width: 390 }, 1).file, "02-goals-3-dark-390.png");
});

test("a shot with no status fails on an HTTP error", () => {
  assert.equal(statusProblem({ path: "/goals" }, 200), null);
  assert.equal(statusProblem({ path: "/nope" }, 404), "the page gave HTTP 404");
  assert.equal(statusProblem({ path: "/boom" }, 500), "the page gave HTTP 500");
});

test("a shot with a status fails when the page gives another", () => {
  assert.equal(statusProblem({ path: "/nope", status: 404 }, 404), null);
  assert.equal(statusProblem({ path: "/goals", status: 200 }, 404), "the page gave HTTP 404, not the expected 200");
});

test("a status that couldn't be read never fails a shot", () => {
  assert.equal(statusProblem({ path: "/goals" }, null), null);
  assert.equal(statusProblem({ path: "/goals", status: 200 }, null), null);
});

test("an action that runs as a script is left as it is", () => {
  assert.equal(wrapAction("a(); b()"), "a(); b()");
  assert.equal(wrapAction("document.title"), "document.title");
  assert.equal(wrapAction("fetch('/x').then((r) => r.status)"), "fetch('/x').then((r) => r.status)");
});

test("an action that uses return or await runs in an async function", () => {
  const action = "await fetch('/x');\nreturn document.title";
  assert.equal(wrapAction(action), `(async () => {\n${action}\n})()`);
  assert.equal(wrapAction("return 1"), "(async () => {\nreturn 1\n})()");
});

test("an action that awaits a parenthesised expression runs in an async function", () => {
  // As a script, await (x) compiles as a call to a function named await.
  for (const action of ["await (new Promise(r => setTimeout(r, 10)))", "await(sleep(10)); go()"]) {
    assert.equal(wrapAction(action), `(async () => {\n${action}\n})()`);
  }
});

test("an action that awaits inside a template literal runs in an async function", () => {
  const action = "`a ${await(x)} b`";
  assert.equal(wrapAction(action), `(async () => {\n${action}\n})()`);
});

test("an action in sloppy-only code that doesn't await runs as a script", () => {
  for (const action of ["with (document) { title = 'x' }", "x = 010", "delete x"]) {
    assert.equal(wrapAction(action), action);
  }
});

test("an action that awaits but can't run in an async function runs as a script", () => {
  // Its wrapped form doesn't compile, or it's sloppy-only code, as before #119.
  for (const action of ["var await = 1; await", "with (document) { await(x) }"]) {
    assert.equal(wrapAction(action), action);
  }
});

test("an action that mentions await only in a string, comment, regex or name runs as a script", () => {
  for (const action of [
    "console.log('await')",
    "x = 'await'; 42",
    "// await later\n7",
    "fetch('/await').then((r) => r.text())",
    "obj.await",
    "foo(/await/)",
    "({await: 1})",
    "const awaitX = 1; awaitX",
  ]) {
    assert.equal(wrapAction(action), action);
  }
});

const facts = {
  url: "/dimensions",
  title: "Dimensions · Goal Tracker",
  pin: null,
  osDark: false,
  background: "rgb(246, 246, 243)",
  scrollsBy: 0,
  past: [],
  width: 1280,
  focus: null,
  toast: "Retired Platform from Team. Undo",
  dialogs: [],
  status: 200,
};

// metrics is what Page.getLayoutMetrics gives for a 2400px page in a 1280×800
// window scrolled down by scrollY.
const metrics = (scrollY) => ({
  cssContentSize: { x: 0, y: 0, width: 1280, height: 2400 },
  cssVisualViewport: { pageX: 0, pageY: scrollY, clientWidth: 1280, clientHeight: 800 },
});

test("a window shot captures where the window is scrolled to", () => {
  const shot = normalise({ path: "/goals", full: false }, 0);
  assert.deepEqual(captureParams(shot, metrics(1200)), {
    captureBeyondViewport: false,
    clip: { x: 0, y: 1200, width: 1280, height: 800, scale: 1 },
  });
});

test("a full shot captures the whole page from its top, however it's scrolled", () => {
  const shot = normalise({ path: "/goals" }, 0);
  assert.deepEqual(captureParams(shot, metrics(1200)), {
    captureBeyondViewport: true,
    clip: { x: 0, y: 0, width: 1280, height: 2400, scale: 1 },
  });
});

test("a full shot of a page shorter than the window is the window's height", () => {
  const shot = normalise({ path: "/goals" }, 0);
  const short = { ...metrics(0), cssContentSize: { x: 0, y: 0, width: 1280, height: 300 } };
  assert.equal(captureParams(shot, short).clip.height, 800);
});

test("the facts are reported as the README shows them", () => {
  const shot = normalise({ path: "/dimensions", as: "admin@example.com", full: false, action: "x()" }, 1);
  const file = "/tmp/goal-tracker-shots/02-dimensions-light-1280.png";
  assert.equal(formatReport(shot, { file, facts, native: [] }), `/tmp/goal-tracker-shots/02-dimensions-light-1280.png
  shot:     /dimensions as admin@example.com, OS light, 1280px, after the action
  page:     /dimensions (HTTP 200) "Dimensions · Goal Tracker"
  theme:    not pinned, follows the OS (light); body background rgb(246, 246, 243)
  overflow: none
  focus:    nothing (body)
  toast:    "Retired Platform from Team. Undo"
  dialog:   none
`);
});

test("an empty title is reported as (none)", () => {
  const shot = normalise({ path: "/plain" }, 0);
  const report = formatReport(shot, { file: "x.png", facts: { ...facts, url: "/plain", title: "", status: null }, native: [] });
  assert.match(report, /^  page:     \/plain \(HTTP \?\) \(none\)$/m);
});

test("the README's shots parse and normalise", () => {
  // Each example is the single-quoted JSON after `node scripts/shots.mjs <base-url>`.
  const readme = readFileSync(new URL("README.md", import.meta.url), "utf8");
  const examples = [...readme.matchAll(/node scripts\/shots\.mjs [^'\n]*'([^']*)'/g)].map((m) => m[1]);
  assert.ok(examples.length >= 2, `found ${examples.length} examples`);
  const shots = examples.flatMap((json) => [JSON.parse(json)].flat()).map(normalise);
  assert.ok(shots.some((s) => s.status !== undefined), "an example sets status");
  const actions = shots.filter((s) => s.action).map((s) => s.action);
  assert.ok(actions.some((a) => wrapAction(a) === a), "an example's action runs as a script");
  assert.ok(actions.some((a) => wrapAction(a) !== a), "an example's action uses return or await");
});

// firstPixel is the RGB of a PNG's top-left pixel. The first byte of a
// scanline's first pixel is stored as it is under every PNG filter, since
// there is nothing to its left or above it.
function firstPixel(png) {
  const idat = [];
  for (let at = 8; at < png.length; ) {
    const length = png.readUInt32BE(at);
    if (png.toString("latin1", at + 4, at + 8) === "IDAT") idat.push(png.subarray(at + 8, at + 8 + length));
    at += 12 + length;
  }
  const rows = inflateSync(Buffer.concat(idat));
  return [...rows.subarray(1, 4)];
}

test("a window shot after a scroll shows what the window scrolled to", async (t) => {
  // White for the first 1200px, red below it.
  const page = `<!doctype html><body style="margin:0"><div style="height:1200px;background:#fff"></div><div style="height:1200px;background:#f00"></div>`;
  const server = createServer((_, res) => res.end(page)).listen(0, "127.0.0.1");
  await new Promise((r) => server.once("listening", r));
  const out = mkdtempSync(join(tmpdir(), "shots-test-"));
  t.after(() => {
    server.close();
    rmSync(out, { recursive: true, force: true });
  });

  const shot = JSON.stringify({ path: "/", full: false, name: "scrolled", action: "scrollTo(0, 1600)" });
  const base = `http://127.0.0.1:${server.address().port}`;
  const script = new URL("shots.mjs", import.meta.url).pathname;
  const run = await new Promise((done) =>
    execFile(process.execPath, [script, base, shot, "--out", out], (err, stdout, stderr) => done({ code: err?.code ?? 0, stdout, stderr })),
  );
  if (/no Chromium found/.test(run.stderr)) return t.skip("no Chromium on this machine");
  assert.equal(run.code, 0, run.stdout + run.stderr);
  assert.deepEqual(firstPixel(readFileSync(join(out, "scrolled.png"))), [255, 0, 0]);
});
