import { defineConfig, devices } from "@playwright/test";

// Chromium, headless. Where Playwright's own browser can't run (an Arch host,
// say), set CHROME to a Chromium on the machine, as for scripts/shots.mjs.
const executablePath = process.env.CHROME || undefined;

export default defineConfig({
  testDir: ".",
  testMatch: "**/*.spec.ts",
  // Every test runs against its own copy of a seeded database and its own
  // server (fixtures.ts), so tests never see each other's writes.
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  globalSetup: "./global-setup.ts",
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    ...devices["Desktop Chrome"],
    headless: true,
    launchOptions: { executablePath },
    trace: "retain-on-failure",
  },
});
