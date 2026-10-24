// The Report draft's narrative, composed in place in its main column (#473):
// typing marks the save status Unsaved changes and the Highlights count
// follows the boxes ticked, Save narrative swaps in the Narrative section as
// saved, and what the author types while a save is on its way is never wiped.
import type { Page } from "@playwright/test";

import { admin, expect, signIn, test } from "./fixtures";

// buildPlatformReport saves a Report Definition named name over Team
// Platform, and lands on its draft.
async function buildPlatformReport(page: Page, name: string): Promise<void> {
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  await builder.getByLabel("Name").fill(name);
  await builder.getByTestId("report-mode").getByText("Goals matching rules").click();
  const rule = builder.getByTestId("report-rule").first();
  await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
  await rule.getByLabel("Operator").selectOption({ label: "is any of" });
  await rule.getByLabel("Values").selectOption({ label: "Platform" });
  await builder.getByRole("button", { name: "Save report" }).click();
  await page.waitForURL(/\/reports\/\d+$/);
}

test("Save narrative saves in place, and keeps what was typed while it saved", async ({ page }) => {
  await signIn(page, admin);
  await buildPlatformReport(page, "Platform narrative");
  const curation = page.getByTestId("narrative-curation");
  const status = page.getByTestId("save-result");
  const log = page.getByTestId("report-log-entry");
  const misses = curation.getByLabel("Misses, note 1");

  await curation.getByTestId("section-text-Miss").getByTestId("section-text-summary").click();
  await misses.fill("The deploy slipped a week.");
  await expect(status).toHaveText("Unsaved changes");

  // A save whose answer is held back while she keeps typing: the answer's
  // Report log entry lands, but her typing stays, and so does Unsaved changes.
  const logged = await log.count();
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route(/\/reports\/\d+\/narrative$/, async (route) => {
    await held;
    await route.continue();
  });
  const answered = page.waitForResponse(/\/reports\/\d+\/narrative$/);
  await curation.getByRole("button", { name: "Save narrative" }).click();
  await misses.fill("The deploy slipped a week; the vendor owns it.");
  release();
  await answered;
  await expect(log).toHaveCount(logged + 1);
  await expect(misses).toHaveValue("The deploy slipped a week; the vendor owns it.");
  await expect(status).toHaveText("Unsaved changes");
  await page.unroute(/\/reports\/\d+\/narrative$/);

  // Saved again, untouched while it saves, the section comes back as saved.
  await curation.getByRole("button", { name: "Save narrative" }).click();
  await expect(status).toHaveText("Saved");
  await expect(log).toHaveCount(logged + 2);
  await expect(misses).toHaveValue("The deploy slipped a week; the vendor owns it.");
  await page.reload();
  await expect(misses).toHaveValue("The deploy slipped a week; the vendor owns it.");
  await expect(status).toHaveText("");
});
