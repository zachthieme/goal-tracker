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

// admin is the seed's Admin, and the server's GOAL_TRACKER_ADMINS.
export const admin = "admin@example.com";

export function templatePath(dir: string, template: Template): string {
  return join(dir, `${template}.db`);
}

const root = fileURLToPath(new URL("..", import.meta.url));

// referenceDate is the day the e2e org is frozen at: both templates are seeded
// relative to it, not to the machine's date, so every run sees the same org.
// The seed's org isn't the same org shifted in time (delivery dates snap to
// the calendar, so another -end changes the draws and which Goals are Red),
// so the specs' choices by Health only hold at this date. #226's follow-up,
// the seed invariants test in internal/seed, pins the same date: the two move
// together.
export const referenceDate = "2026-10-05";

// startAt is the instant each test's app starts its clock at
// (GOAL_TRACKER_START_AT), then ticks on from: 5 minutes after the 18:00 UTC
// the seed reads -end as. The seed's last Check-ins land at about 15:49 that
// day, so a Check-in a test writes is the Goal's latest.
export const startAt = `${referenceDate}T18:05:00Z`;

// The seed's -end for each template.
//
// default: the reference date, so the last Check-ins are a couple of hours
// old when each test's app starts.
//
// due: 4 days before the reference date, so the last Check-ins are 4 days old
// and 7-day Goals are due on Home but not Stale.
const templates: Record<Template, string> = {
  default: referenceDate,
  due: "2026-10-01",
};

export default function globalSetup() {
  const dir = mkdtempSync(join(tmpdir(), "goal-tracker-e2e-"));
  process.env[e2eDirEnv] = dir;

  for (const cmd of ["goal-tracker", "seed"]) {
    execFileSync("go", ["build", "-o", join(dir, cmd), `./cmd/${cmd}`], { cwd: root, stdio: "inherit" });
  }
  for (const [name, end] of Object.entries(templates)) {
    const args = ["-db", templatePath(dir, name as Template), "-seed", "23", "-admin", admin, "-end", end];
    execFileSync(join(dir, "seed"), args, { stdio: "pipe" });
  }

  return () => rmSync(dir, { recursive: true, force: true });
}
