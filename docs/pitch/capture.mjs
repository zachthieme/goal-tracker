#!/usr/bin/env node
// capture.mjs: the pitch deck's screenshots and its check-in recording, taken
// from a running Goal Tracker over the seeded fake org.
//
//   node docs/pitch/capture.mjs <base-url> [assets-dir]
//
// It writes <assets-dir>/shots/*.png and <assets-dir>/checkin.mp4 (default
// docs/pitch/assets), which build.mjs reads. It writes to the app: it submits
// two Check-ins and publishes one Report, so point it at a throwaway copy such
// as `scripts/scratch-app start`, never at a database you want to keep.
//
// Playwright comes from e2e/node_modules (`make e2e` or `npm ci` in e2e/
// installs it). Set CHROME to a Chromium binary where Playwright's own can't
// run. ffmpeg converts the recording to MP4, which PowerPoint plays.
import { execFileSync } from "node:child_process";
import { mkdirSync, readdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "../../e2e/node_modules/playwright/index.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const [base, assetsArg] = process.argv.slice(2);
if (!base) {
  console.error("usage: node docs/pitch/capture.mjs <base-url> [assets-dir]");
  process.exit(2);
}
const assets = resolve(assetsArg ?? join(here, "assets"));
const shots = join(assets, "shots");
const rawVideo = join(assets, "video");
mkdirSync(shots, { recursive: true });
rmSync(rawVideo, { recursive: true, force: true });

// The seed's people and Goals the deck shows.
const leader = "cto@example.com";
// The Owner in the recording has a Stale Goal that No change is allowed on (no
// overdue Milestone) and a second Goal for the full Check-in.
const owner = "grace.oduya@example.com";
const staleGoal = "Macro library for the top 50 issues";
const updatedGoal = "Chatbot deflection for billing questions";
const redGoal = "Launch a referral program";

const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {});

async function signIn(page, email) {
  await page.goto("/signin");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL("**/home");
}

async function shot(page, name) {
  await page.waitForLoadState("networkidle");
  await page.screenshot({ path: join(shots, `${name}.png`) });
  console.log(`shot   ${name}.png`);
}

// goalPath finds a Goal's page by its title, from the Goals list.
async function goalPath(page, title) {
  await page.goto("/goals");
  const href = await page.getByRole("link", { name: title, exact: true }).first().getAttribute("href");
  if (!href) throw new Error(`no Goal titled ${JSON.stringify(title)} on /goals; is this the seeded org?`);
  return href;
}

// The leader's pages: Risks and one Red Goal. Taken before the recording's
// Check-ins, so Risks still lists the Stale Goal.
const leaderContext = await browser.newContext({
  baseURL: base,
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
});
const cto = await leaderContext.newPage();
await signIn(cto, leader);
const redGoalPath = await goalPath(cto, redGoal);
const updatedGoalPath = await goalPath(cto, updatedGoal);
await cto.goto("/risks");
await shot(cto, "risks");
// The title slide's closer view: the counts and the first rows.
await cto.screenshot({ path: join(shots, "risks-top.png"), clip: { x: 160, y: 150, width: 1120, height: 700 } });
console.log("shot   risks-top.png");
await cto.goto(redGoalPath);
await shot(cto, "goal");

// The recording: an Owner lands on Home and clears a Stale Goal with one
// click on No change, then checks in Yellow on a second Goal. Its poster
// frame is the filled-in form.
const videoContext = await browser.newContext({
  baseURL: base,
  viewport: { width: 1280, height: 720 },
  recordVideo: { dir: rawVideo, size: { width: 1280, height: 720 } },
});
const page = await videoContext.newPage();
const pause = (ms) => page.waitForTimeout(ms);
await signIn(page, owner);
await pause(2500);
const due = page.getByTestId("home-due-goal").filter({ hasText: staleGoal });
await due.getByRole("button", { name: "No change" }).hover();
await pause(800);
await due.getByRole("button", { name: "No change" }).click();
// No change lands on the Goal's page; a refusal lands on its Check-in form.
await page.waitForURL(/\/goals\/\d+$/);
await pause(2500);
await page.goto(`${updatedGoalPath}/checkin`);
await pause(1500);
const form = page.getByTestId("checkin-form");
await form.getByTestId("checkin-health").getByText("Yellow", { exact: true }).click();
await pause(800);
await form
  .getByRole("textbox", { name: "Plan", exact: true })
  .pressSequentially("Retrain on last quarter's billing tickets; ship behind a flag.", { delay: 25 });
const backToGreen = new Date(Date.now() + 21 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
await form.getByLabel("Back to Green by").fill(backToGreen);
await pause(500);
const status = form.getByRole("textbox", { name: /^Status/ });
await status.fill("");
await status.pressSequentially("Deflection fell to 31% after the pricing change; answers are out of date.", { delay: 25 });
await pause(800);
await page.screenshot({ path: join(shots, "checkin-filled.png") });
console.log("shot   checkin-filled.png");
const submit = form.getByRole("button", { name: "Submit check-in" });
await submit.scrollIntoViewIfNeeded();
await pause(800);
await submit.click();
await page.waitForLoadState("networkidle");
await pause(2500);
await page.goto("/home");
await pause(2500);
await videoContext.close();

// The Report: a Definition by one rule, published as it stands.
await cto.goto("/reports/new");
const builder = cto.getByTestId("report-builder");
await builder.getByLabel("Name").fill("Growth monthly review");
await builder
  .getByLabel("Introduction")
  .fill("Growth's month: referral and conversion work is behind, and here is what it needs.");
const rule = builder.getByTestId("report-rule");
await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
await rule.getByLabel("Operator").selectOption({ label: "is any of" });
await rule.getByLabel("Values").selectOption({ label: "Growth" });
await cto.getByTestId("matches-count").waitFor();
await builder.getByRole("button", { name: "Save report" }).click();
await cto.getByTestId("draft-header").waitFor();
const confirm = cto.getByTestId("publish-confirm");
await confirm.getByText("Publish…").click();
await confirm.getByRole("button", { name: "Publish", exact: true }).click();
await cto.waitForURL(/\/reports\/\d+\/publications\/\d+$/);
await shot(cto, "report-published");

await browser.close();

const [webm] = readdirSync(rawVideo).filter((f) => f.endsWith(".webm"));
const mp4 = join(assets, "checkin.mp4");
execFileSync("ffmpeg", [
  "-y", "-loglevel", "error", "-i", join(rawVideo, webm),
  "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "20", "-movflags", "+faststart", "-an", mp4,
]);
rmSync(rawVideo, { recursive: true, force: true });
console.log("video  checkin.mp4");
