// The suite's fixtures and helpers: each test gets its own copy of a seeded
// database and its own server, and these helpers sign people in and look
// things up in the seed. See README.md for how the specs use them.
import { type ChildProcess, spawn } from "node:child_process";
import { copyFileSync, mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { join } from "node:path";
import { DatabaseSync, type SQLInputValue } from "node:sqlite";

import { type Browser, type Page, test as base } from "@playwright/test";

import { e2eDirEnv, type Template, templatePath } from "./global-setup";

export { expect } from "@playwright/test";
export type { Template } from "./global-setup";

// admin is the seed's Admin, and the server's GOAL_TRACKER_ADMINS.
export const admin = "admin@example.com";

// App is the running copy of Goal Tracker a test drives.
export type App = {
  // url is the server's base URL, e.g. http://127.0.0.1:41234.
  url: string;
  // db is the path of the test's own database.
  db: string;
  // log is everything the server has written to stdout and stderr so far.
  log: () => string;
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
  // asserted on the page.
  seedLookup: <T = Record<string, unknown>>(sql: string, ...params: SQLInputValue[]) => T[];
  // serverLog is what the server has logged so far, for what the app only
  // logs, such as email (email.LogSender).
  serverLog: () => string;
};

export const test = base.extend<Fixtures>({
  template: ["default", { option: true }],

  app: async ({ template }, use, testInfo) => {
    const dir = process.env[e2eDirEnv];
    if (!dir) throw new Error(`${e2eDirEnv} is unset: run the suite with npx playwright test (global-setup.ts sets it)`);
    const work = mkdtempSync(join(dir, "test-"));
    const db = join(work, "app.db");
    copyFileSync(templatePath(dir, template), db);

    const server = await startServer(join(dir, "goal-tracker"), db);
    try {
      await use({ url: server.url, db, log: server.log });
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
    await use((sql, ...params) => {
      const conn = new DatabaseSync(app.db, { readOnly: true });
      try {
        return conn.prepare(sql).all(...params) as never;
      } finally {
        conn.close();
      }
    });
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

type Server = { url: string; log: () => string; stop: () => Promise<void> };

// startServer starts the binary as scripts/scratch-app does: on a free port,
// over db, with the seed's Admin, and waits until /signin answers. It tries a
// few ports, in case another test takes the one it picked first.
async function startServer(bin: string, db: string): Promise<Server> {
  let lastErr: unknown;
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      return await tryStartServer(bin, db, await freePort());
    } catch (err) {
      lastErr = err;
    }
  }
  throw lastErr;
}

async function tryStartServer(bin: string, db: string, port: number): Promise<Server> {
  const url = `http://127.0.0.1:${port}`;
  const child = spawn(bin, [], {
    env: {
      ...process.env,
      GOAL_TRACKER_ADDR: `127.0.0.1:${port}`,
      GOAL_TRACKER_DB: db,
      GOAL_TRACKER_ADMINS: admin,
      GOAL_TRACKER_BASE_URL: url,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  child.stdout.on("data", (b: Buffer) => (output += b));
  child.stderr.on("data", (b: Buffer) => (output += b));
  const exited = new Promise<void>((resolve) => child.once("exit", () => resolve()));
  const server = { url, log: () => output, stop: () => stopServer(child, exited) };

  // Each poll waits first, so a server that couldn't bind the port (another
  // process took it) has exited before something else's answer is taken for
  // its own.
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 100));
    let answered = false;
    try {
      await fetch(`${url}/signin`, { signal: AbortSignal.timeout(1000) });
      answered = true;
    } catch {
      // Not listening yet.
    }
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(`goal-tracker exited before answering on ${url}:\n${output}`);
    }
    if (answered) return server;
  }
  await server.stop();
  throw new Error(`goal-tracker didn't answer on ${url} within 20s:\n${output}`);
}

async function stopServer(child: ChildProcess, exited: Promise<void>): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  child.kill("SIGTERM");
  const timeout = setTimeout(() => child.kill("SIGKILL"), 5000);
  await exited;
  clearTimeout(timeout);
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      srv.close(() => (typeof addr === "object" && addr ? resolve(addr.port) : reject(new Error("no port"))));
    });
  });
}
