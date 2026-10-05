// Global setup: build the binaries and seed the template databases, once per
// run. Each test copies a template (see the app fixture in fixtures.ts), so
// every test starts from the same seeded org.
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

// Template is a seeded database a test can start from: default unless the
// test asks for another with test.use({ template: "due" }).
export type Template = "default" | "due";

// e2eDirEnv names the variable that hands the run's directory (binaries and
// templates) to the test workers.
export const e2eDirEnv = "GOAL_TRACKER_E2E_DIR";

export function templatePath(dir: string, template: Template): string {
  return join(dir, `${template}.db`);
}

const root = fileURLToPath(new URL("..", import.meta.url));

// The seed's -end for each template, or none for now.
//
// default: no -end, so the seed's last Check-ins land just before now. With
// -end <today>, the seed puts them at about 15:00 today, after a test's own
// Check-in until mid-afternoon, so the test's wouldn't be the latest.
//
// due: the last Check-ins are 4 days old, so 7-day Goals are due on Home but
// not Stale.
const templates: Record<Template, string | undefined> = {
  default: undefined,
  due: daysAgo(4),
};

export default function globalSetup() {
  const dir = mkdtempSync(join(tmpdir(), "goal-tracker-e2e-"));
  process.env[e2eDirEnv] = dir;

  for (const cmd of ["goal-tracker", "seed"]) {
    execFileSync("go", ["build", "-o", join(dir, cmd), `./cmd/${cmd}`], { cwd: root, stdio: "inherit" });
  }
  for (const [name, end] of Object.entries(templates)) {
    const args = ["-db", templatePath(dir, name as Template), "-seed", "23", "-admin", "admin@example.com"];
    if (end) args.push("-end", end);
    execFileSync(join(dir, "seed"), args, { stdio: "pipe" });
  }

  return () => rmSync(dir, { recursive: true, force: true });
}

// daysAgo is the UTC date n days before today, as YYYY-MM-DD: the calendar
// the seed reads -end in.
function daysAgo(n: number): string {
  return new Date(Date.now() - n * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
}
