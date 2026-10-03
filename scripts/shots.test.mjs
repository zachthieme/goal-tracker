// Tests for the parts of shots.mjs that don't need a browser:
//   node --test scripts/*.test.mjs
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { formatReport, normalise, statusProblem, wrapAction } from "./shots.mjs";

test("a shot's status must be an integer", () => {
  assert.throws(() => normalise({ path: "/goals", status: "404" }, 0), /shot 1: status must be an integer/);
  assert.throws(() => normalise({ path: "/goals", status: 404.5 }, 0), /status must be an integer/);
  assert.equal(normalise({ path: "/goals", status: 404 }, 0).status, 404);
});

test("a shot's name may end in .png without saving .png.png", () => {
  assert.equal(normalise({ path: "/goals", name: "b.png" }, 0).file, "b.png");
  assert.equal(normalise({ path: "/goals", name: "b" }, 0).file, "b.png");
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
