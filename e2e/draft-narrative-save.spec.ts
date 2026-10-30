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
  const log = page.getByTestId("report-history").getByTestId("report-history-entry");
  const misses = curation.getByLabel("Misses, note 1");

  await curation.getByTestId("section-text-Miss").getByTestId("section-text-summary").click();
  await misses.fill("The deploy slipped a week.");
  await expect(status).toHaveText("Unsaved changes");

  // A save whose answer is held back while she keeps typing: the answer's
  // History entry lands, but her typing stays, and so does Unsaved changes.
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

test("Save and publish saves the narrative, then publishes it", async ({ page }) => {
  await signIn(page, admin);
  await buildPlatformReport(page, "Platform save and publish");
  const curation = page.getByTestId("narrative-curation");
  const confirm = page.getByTestId("publish-confirm");
  const publish = confirm.getByTestId("report-publish").getByRole("button");

  // Nothing unsaved: Publish… publishes as ever.
  await confirm.getByText("Publish…").click();
  await expect(publish).toHaveText("Publish");
  await expect(confirm.getByTestId("publish-unsaved")).toBeHidden();
  await confirm.getByText("Publish…").click();

  await curation.getByTestId("section-text-Miss").getByTestId("section-text-summary").click();
  await curation.getByLabel("Misses, note 1").fill("The deploy slipped a week.");
  await confirm.getByText("Publish…").click();
  await expect(publish).toHaveText("Save and publish");
  await expect(publish).toBeEnabled();
  await expect(confirm.getByTestId("publish-unsaved")).toHaveText("Your narrative changes will be saved first.");

  await publish.click();
  await page.waitForURL(/\/reports\/\d+\/publications\/\d+$/);
  await expect(page.getByText("The deploy slipped a week.")).toBeVisible();
});

test("Save and publish doesn't publish when the save is refused or fails, and says why", async ({ page }) => {
  await signIn(page, admin);
  await buildPlatformReport(page, "Platform save refused");
  const draft = page.url();
  const curation = page.getByTestId("narrative-curation");
  const confirm = page.getByTestId("publish-confirm");
  const publish = confirm.getByTestId("report-publish").getByRole("button");
  const notSaved = confirm.getByTestId("publish-not-saved");
  const narrative = /\/reports\/\d+\/narrative$/;
  const publishes: string[] = [];
  page.on("request", (request) => {
    if (request.method() === "POST" && /\/publications/.test(request.url())) publishes.push(request.url());
  });

  await curation.getByTestId("section-text-Miss").getByTestId("section-text-summary").click();
  await curation.getByLabel("Misses, note 1").fill("The deploy slipped a week.");
  await confirm.getByText("Publish…").click();

  // Refused: the answer is the save status, out of band, saying why.
  await page.route(narrative, (route) =>
    route.fulfill({
      status: 200,
      contentType: "text/html",
      headers: { "HX-Reswap": "none" },
      body:
        '<span data-testid="save-status" id="save-status" class="small muted rp-save" data-unsaved hx-swap-oob="true">' +
        '<span class="rp-saving">Saving…</span>' +
        '<span data-testid="save-result" id="save-result">Not saved: Misses has too many notes.</span></span>',
    }),
  );
  await publish.click();
  await expect(notSaved).toHaveText("The narrative wasn't saved: Misses has too many notes.");
  await expect(page.getByTestId("save-result")).toHaveText("Not saved: Misses has too many notes.");
  await expect(publish).toHaveText("Save and publish");
  await expect(publish).toBeEnabled();
  expect(page.url()).toBe(draft);
  await page.unroute(narrative);

  // No answer at all.
  await page.route(narrative, (route) => route.abort());
  await publish.click();
  await expect(notSaved).toHaveText("The narrative wasn't saved.");
  await expect(page.getByTestId("save-result")).toHaveText("Not saved");
  await expect(publish).toHaveText("Save and publish");
  expect(page.url()).toBe(draft);
  await page.unroute(narrative);

  // Saved, but the author typed while it was on its way: those changes are unsaved
  // again, so it doesn't publish.
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route(narrative, async (route) => {
    await held;
    await route.continue();
  });
  const answered = page.waitForResponse(narrative);
  await publish.click();
  await curation.getByLabel("Misses, note 1").fill("The deploy slipped a week; the vendor owns it.");
  release();
  await answered;
  await expect(page.getByTestId("save-result")).toHaveText("Unsaved changes");
  await expect(publish).toHaveText("Save and publish");
  await expect(publish).toBeEnabled();
  expect(page.url()).toBe(draft);
  await page.unroute(narrative);

  // Nothing was published.
  await expect(page).toHaveURL(draft);
  expect(publishes).toEqual([]);
});
