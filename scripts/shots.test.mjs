// Tests for the parts of shots.mjs that don't need a browser:
//   node --test scripts/
import assert from "node:assert/strict";
import { test } from "node:test";

import { normalise, statusProblem, wrapAction } from "./shots.mjs";

test("a shot's status must be an integer", () => {
  assert.throws(() => normalise({ path: "/goals", status: "404" }, 0), /shot 1: status must be an integer/);
  assert.throws(() => normalise({ path: "/goals", status: 404.5 }, 0), /status must be an integer/);
  assert.equal(normalise({ path: "/goals", status: 404 }, 0).status, 404);
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
