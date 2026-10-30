// The Check-in page's submit bar (#367): Back to Goal and Submit check-in
// stick to the bottom of the window while the form runs past it, beside a
// summary of what the Check-in will record as the form stands, which script
// fills and keeps up to date. At 390px the bar drops Back to Goal.
import type { Locator, Page } from "@playwright/test";

import { expect, requirePick, signIn, test } from "./fixtures";

// The manifest's metricGoal is an Active Goal with a Metric and a Check-in to
// start the form from, and its Owner, who can check in on it. It has no Draft
// Highlight, so the form starts with no Highlight, and no overdue Milestone,
// so its dates section starts closed.
type Goal = { id: number; email: string };

let goal: Goal;

test.beforeEach(async ({ page, manifest }) => {
  goal = requirePick("metricGoal", manifest.metricGoal);
  await signIn(page, goal.email);
});

async function openCheckin(page: Page): Promise<Locator> {
  await page.goto(`/goals/${goal.id}/checkin`);
  return page.getByTestId("checkin-form");
}

// pickHealth picks a Health by its label, as a tap does: the radios under the
// labels are hidden, and each label shows its Health's shape, titled with its
// word.
async function pickHealth(form: Locator, health: string): Promise<void> {
  await form.getByRole("group", { name: "Health" }).getByTitle(health, { exact: true }).click();
  await expect(form.getByRole("radio", { name: health })).toBeChecked();
}

function pickedHealth(form: Locator): Promise<string> {
  return form.getByRole("group", { name: "Health" }).getByRole("radio", { checked: true }).inputValue();
}

function summaryOf(form: Locator): Locator {
  return form.getByTestId("checkin-summary");
}

test("(a) on load the summary shows the picked Health's badge", async ({ page }) => {
  const form = await openCheckin(page);
  const health = await pickedHealth(form);
  await expect(summaryOf(form).getByTitle(health, { exact: true })).toBeVisible();
  await expect(summaryOf(form)).toHaveText(health);
});

test("(b) Yellow with a Plan adds Path to Green, and Green takes it away", async ({ page }) => {
  const form = await openCheckin(page);
  await pickHealth(form, "Yellow");
  await form.getByLabel("Plan").fill("Add a second on-call.");
  await expect(summaryOf(form)).toHaveText("Yellow · Path to Green");
  await pickHealth(form, "Green");
  await expect(summaryOf(form)).toHaveText("Green");
});

test("(c) one reading shows 1 Metric", async ({ page }) => {
  const form = await openCheckin(page);
  await form.getByTestId("checkin-readings").getByRole("spinbutton").first().fill("42");
  await expect(summaryOf(form)).toContainText("1 Metric");
  await expect(summaryOf(form)).not.toContainText("Metrics");
});

test("(d) a Highlight written in the Highlight editor shows 1 Highlight", async ({ page }) => {
  const form = await openCheckin(page);
  await expect(summaryOf(form)).not.toContainText("Highlight");
  await form.getByRole("button", { name: "+ Insight" }).click();
  const editor = form.getByTestId("highlight-editor");
  await editor.getByLabel("Note").fill("On-call load halves with a second rota.");
  await expect(summaryOf(form)).toContainText("1 Highlight");
  await editor.getByRole("button", { name: "Save" }).click();
  await expect(form.getByTestId("highlight-item")).toHaveCount(1);
  await expect(summaryOf(form)).toContainText("1 Highlight");
  await form.getByTestId("highlight-item").getByRole("button", { name: "Remove" }).click();
  await expect(summaryOf(form)).not.toContainText("Highlight");
});

test("(e) a new Milestone's name shows dates changed", async ({ page }) => {
  const form = await openCheckin(page);
  await expect(summaryOf(form)).not.toContainText("dates changed");
  const dates = form.getByTestId("checkin-dates-section");
  await page.getByTestId("checkin-add-bar").getByRole("button", { name: "Move a date or Milestone" }).click();
  await dates.getByLabel("New Milestone name").fill("Docs");
  await expect(summaryOf(form)).toContainText("dates changed");
  await dates.getByLabel("New Milestone name").fill("");
  await expect(summaryOf(form)).not.toContainText("dates changed");
});

test("(f) Mark Done shows → Done, and the Health badge goes", async ({ page }) => {
  const form = await openCheckin(page);
  const health = await pickedHealth(form);
  const lifecycle = form.getByTestId("checkin-lifecycle-section");
  await page.getByTestId("checkin-add-bar").getByRole("button", { name: "Pause, finish or cancel" }).click();
  await lifecycle.getByRole("radio", { name: "Mark Done" }).check();
  await expect(summaryOf(form)).toContainText("→ Done");
  await expect(summaryOf(form).getByTitle(health, { exact: true })).toHaveCount(0);
});

test("(g) at 1280×800 Submit check-in stays in the window, and the bar ends the form", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const form = await openCheckin(page);
  // The Lifecycle section shows once opened from the add bar (#365), the last
  // of the optional sections.
  await page.getByTestId("checkin-add-bar").getByRole("button", { name: "Pause, finish or cancel" }).click();
  const lastSection = form.getByTestId("checkin-lifecycle-section");
  await expect(lastSection).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, 0));
  const below = (await lastSection.boundingBox())!;
  expect(below.y, "the form runs past the window at the top of the page").toBeGreaterThan(800);
  await expect(form.getByRole("button", { name: "Submit check-in" })).toBeInViewport();

  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  const bar = form.getByTestId("checkin-submit-bar");
  await expect(form.getByRole("button", { name: "Submit check-in" })).toBeInViewport();
  const section = (await lastSection.boundingBox())!;
  const barBox = (await bar.boundingBox())!;
  expect(barBox.y, "the bar's top is at or below the last section's bottom").toBeGreaterThanOrEqual(
    section.y + section.height - 1,
  );
});

test("(h) at 390×800 the bar drops Back to Goal and fits the window", async ({ page }) => {
  const width = 390;
  await page.setViewportSize({ width, height: 800 });
  const form = await openCheckin(page);
  await pickHealth(form, "Yellow");
  await form.getByLabel("Plan").fill("Add a second on-call.");
  await form.getByTestId("checkin-readings").getByRole("spinbutton").first().fill("42");
  await expect(summaryOf(form)).toHaveText("Yellow · Path to Green · 1 Metric");

  const bar = form.getByTestId("checkin-submit-bar");
  await expect(bar.getByRole("link", { name: "Back to Goal" })).toBeHidden();
  await expect(bar.getByRole("button", { name: "Submit check-in" })).toBeInViewport();
  const outside = await page.evaluate((width) => {
    return [...document.querySelectorAll("main *")]
      .filter((e) => {
        // The add bar's buttons scroll sideways in their own row (#365).
        if (e.closest('[data-testid="checkin-add-buttons"]')) return false;
        if (!e.checkVisibility({ visibilityProperty: true })) return false;
        const r = e.getBoundingClientRect();
        return r.width > 0 && r.right > width + 1;
      })
      .map((e) => {
        const name = (e as HTMLElement).dataset.testid || e.className || e.tagName;
        return `${name} (${Math.round(e.getBoundingClientRect().right)}px)`;
      });
  }, width);
  expect(outside, `elements in main past the ${width}px window`).toEqual([]);
});

test("a refused submit swaps the form back with its summary filled again", async ({ page }) => {
  const form = await openCheckin(page);
  await pickHealth(form, "Yellow");
  await form.getByLabel("What changed since the last check-in?").fill("Slipping.");
  await form.getByLabel("Plan").fill("");
  await form.getByTestId("checkin-readings").getByRole("spinbutton").first().fill("42");
  await form.getByRole("button", { name: "Submit check-in" }).click();
  // The Path to Green is required while Yellow, so the form comes back
  // with the error, its summary empty from the server until script fills it.
  await expect(form.getByTestId("checkin-error")).toBeVisible();
  await expect(summaryOf(form)).toHaveText("Yellow · 1 Metric");
});
