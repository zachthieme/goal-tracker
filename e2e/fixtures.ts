// The suite's fixtures and helpers: each test gets its own copy of a seeded
// database and its own server, and these helpers sign people in and look
// things up in the seed. See README.md for how the specs use them.
import { type ChildProcess, spawn } from "node:child_process";
import { copyFileSync, mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { join } from "node:path";
import { DatabaseSync, type SQLInputValue } from "node:sqlite";

import { type Browser, type Page, test as base, type TestInfo } from "@playwright/test";

import { admin, e2eDirEnv, referenceDate, startAt, type Template, templatePath } from "./global-setup";

export { expect } from "@playwright/test";
export { admin, startAt, type Template } from "./global-setup";

// appToday is the app's date, YYYY-MM-DD in UTC (the server's timezone): the
// reference date. Each test's app starts its clock at 18:05 that day, and no
// run lasts long enough to cross midnight.
export function appToday(): string {
  return referenceDate;
}

// App is the running copy of Goal Tracker a test drives.
export type App = {
  // url is the server's base URL, e.g. http://127.0.0.1:41234.
  url: string;
  // db is the path of the test's own database.
  db: string;
  // log is everything the server has written to stdout and stderr so far.
  log: () => string;
  // now is the app's current time: startAt plus the time since this test's
  // server started, as its clock (GOAL_TRACKER_START_AT) reads it. Use it, or
  // appToday, wherever a test needs "now", never the machine's clock.
  now: () => Date;
};

type Fixtures = {
  // template is the seeded database the test starts from; set it with
  // test.use({ template: "due" }).
  template: Template;
  // app is the test's own server over its own copy of the template.
  app: App;
  // as opens a page in a new browser context, already signed in as email, for
  // scenarios with more than one person. Its contexts close after the test.
  as: (email: string) => Promise<Page>;
  // seedLookup runs a read-only query on the test's database, to choose
  // actors and Goals by criteria. Never to assert an outcome: those are
  // asserted on the page. $now in the SQL is bound to app.now(), as an ISO
  // 8601 string SQLite's date functions read: write date($now), never
  // SQLite's own now, which reads the machine's clock.
  seedLookup: <T = Record<string, unknown>>(sql: string, ...params: SQLInputValue[]) => T[];
  // serverLog is what the server has logged so far, for what the app only
  // logs, such as email (email.LogSender).
  serverLog: () => string;
};

export const test = base.extend<Fixtures>({
  template: ["default", { option: true }],

  app: async ({ template }, use, testInfo) => {
    const dir = process.env[e2eDirEnv];
    if (!dir)
      throw new Error(`${e2eDirEnv} is unset: run the suite with npx playwright test (global-setup.ts sets it)`);
    const work = mkdtempSync(join(dir, "test-"));
    const db = join(work, "app.db");
    copyFileSync(templatePath(dir, template), db);

    const server = await startServer(join(dir, "goal-tracker"), db, testInfo);
    try {
      await use({ url: server.url, db, log: server.log, now: server.now });
    } finally {
      await server.stop();
      if (testInfo.status !== testInfo.expectedStatus) {
        await testInfo.attach("server.log", { body: server.log(), contentType: "text/plain" });
      }
      rmSync(work, { recursive: true, force: true });
    }
  },

  // Pages, and the contexts as opens, resolve paths such as "/home" against
  // the test's server.
  baseURL: async ({ app }, use) => {
    await use(app.url);
  },

  as: async ({ browser, app }, use) => {
    const pages: Page[] = [];
    await use(async (email) => {
      const page = await newSignedInPage(browser, app.url, email);
      pages.push(page);
      return page;
    });
    await Promise.all(pages.map((p) => p.context().close()));
  },

  seedLookup: async ({ app }, use) => {
    // The server may be writing; wait for it as the app's own connections do.
    function lookup<T>(sql: string, ...params: SQLInputValue[]): T[] {
      const conn = new DatabaseSync(app.db, { readOnly: true, timeout: 5000 });
      try {
        const stmt = conn.prepare(sql);
        // Bind $now only where the SQL names it: an unknown named parameter
        // is an error.
        const rows = /\$now\b/.test(sql) ? stmt.all({ now: app.now().toISOString() }, ...params) : stmt.all(...params);
        return rows as T[];
      } finally {
        conn.close();
      }
    }
    await use(lookup);
  },

  serverLog: async ({ app }, use) => {
    await use(app.log);
  },
});

// signIn signs page in as email through the development sign-in form, and
// waits for Home.
export async function signIn(page: Page, email: string): Promise<void> {
  await page.goto("/signin");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL("**/home");
}

async function newSignedInPage(browser: Browser, url: string, email: string): Promise<Page> {
  const context = await browser.newContext({ baseURL: url });
  const page = await context.newPage();
  await signIn(page, email);
  return page;
}

type Server = { url: string; log: () => string; now: () => Date; stop: () => Promise<void> };

// startServer starts the binary as scripts/scratch-app does: on a free port,
// over db, with the seed's Admin, and waits until /signin answers. The
// server's environment is pinned (UTC, the default weekly reminder), so a
// GOAL_TRACKER_* variable in the caller's shell can't change what tests see.
// Its clock starts at startAt (GOAL_TRACKER_START_AT), so it sees the frozen
// seed at the same moment on every run.
//
// The server migrates the database before it binds the port, so an answer on
// the port may come from another server that took it first. Each worker picks
// only ports congruent to its parallelIndex, so two tests' servers never pick
// the same port; a server that still can't bind (something else took the
// port) exits, and another port is tried.
async function startServer(bin: string, db: string, testInfo: TestInfo): Promise<Server> {
  for (let attempt = 1; ; attempt++) {
    const port = await freePort(testInfo.parallelIndex, testInfo.config.workers);
    try {
      return await tryStartServer(bin, db, port);
    } catch (err) {
      if (!(err instanceof ServerExited) || attempt === 3) throw err;
    }
  }
}

class ServerExited extends Error {}

async function tryStartServer(bin: string, db: string, port: number): Promise<Server> {
  const url = `http://127.0.0.1:${port}`;
  const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith("GOAL_TRACKER_")));
  // The binary starts its clock at startAt as it launches, a moment after
  // this. performance.now() is monotonic and isn't the machine's date.
  const launched = performance.now();
  const child = spawn(bin, [], {
    env: {
      ...env,
      GOAL_TRACKER_ADDR: `127.0.0.1:${port}`,
      GOAL_TRACKER_DB: db,
      GOAL_TRACKER_ADMINS: admin,
      GOAL_TRACKER_BASE_URL: url,
      GOAL_TRACKER_TIMEZONE: "UTC",
      GOAL_TRACKER_START_AT: startAt,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  child.stdout?.on("data", (b: Buffer) => (output += b));
  child.stderr?.on("data", (b: Buffer) => (output += b));
  // A binary that can't be spawned emits error, not exit.
  let spawnErr: Error | undefined;
  const exited = new Promise<void>((resolve) => {
    child.once("exit", () => resolve());
    child.once("error", (err) => {
      spawnErr = err;
      resolve();
    });
  });
  const gone = () => spawnErr !== undefined || child.exitCode !== null || child.signalCode !== null;
  const now = () => new Date(Date.parse(startAt) + (performance.now() - launched));
  const server = { url, log: () => output, now, stop: () => stopServer(child, exited, gone) };

  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    if (spawnErr) throw new Error(`can't start ${bin}: ${spawnErr.message}`);
    if (gone()) throw new ServerExited(`goal-tracker exited before answering on ${url}:\n${output}`);
    try {
      await fetch(`${url}/signin`, { signal: AbortSignal.timeout(1000) });
      return server;
    } catch {
      await new Promise((r) => setTimeout(r, 100));
    }
  }
  await server.stop();
  throw new Error(`goal-tracker didn't answer on ${url} within 20s:\n${output}`);
}

async function stopServer(child: ChildProcess, exited: Promise<void>, gone: () => boolean): Promise<void> {
  if (gone()) return;
  child.kill("SIGTERM");
  const timeout = setTimeout(() => child.kill("SIGKILL"), 5000);
  await exited;
  clearTimeout(timeout);
}

// freePort is a port nothing listens on, congruent to slot modulo slots. It
// picks from below Linux's ephemeral range (32768 up), where outgoing
// connections take their ports, and checks the port by binding it.
async function freePort(slot: number, slots: number): Promise<number> {
  const low = 20000;
  const candidates = Math.floor((32768 - low - slot) / slots);
  for (;;) {
    const port = low + slot + slots * Math.floor(Math.random() * candidates);
    if (await isFree(port)) return port;
  }
}

function isFree(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const srv = createServer();
    srv.once("error", () => resolve(false));
    srv.listen(port, "127.0.0.1", () => srv.close(() => resolve(true)));
  });
}
