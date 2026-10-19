// The Check-in page's submit bar (#367): Cancel and Submit check-in stick to
// the bottom of the window while the form runs past it, beside a summary of
// what the Check-in will record as the form stands, which script fills and
// keeps up to date. At 390px the bar drops Cancel.
import type { Locator, Page } from "@playwright/test";

import { expect, signIn, test } from "./fixtures";

// goalWithMetric is an Active Goal with a Metric and a Check-in to start the
// form from, and its Owner, who can check in on it. It has no Draft Highlight,
// so the form starts with no Highlight, and no overdue Milestone, so its dates
// section starts closed.
const goalWithMetric = `
  select g.id, a.email
  from goals g join accounts a on a.id = g.owner_id
  where g.lifecycle = 'Active' and a.departed = 0
    and exists (select 1 from metrics m where m.goal_id = g.id)
    and exists (select 1 from checkins c where c.goal_id = g.id)
    and not exists (select 1 from draft_highlights d where d.goal_id = g.id)
    and not exists (select 1 from milestones m
                    where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
  order by g.id
  limit 1`;

type Goal = { id: number; email: string };

let goal: Goal;

test.beforeEach(async ({ page, seedLookup }) => {
  [goal] = seedLookup<Goal>(goalWithMetric);
  expect(goal, "the seed has an Active Goal with a Metric and a Check-in").toBeDefined();
  await signIn(page, goal.email);
});

async function openCheckin(page: Page): Promise<Locator> {
  await page.goto(`/goals/${goal.id}/checkin`);
  return page.getByTestId("checkin-form");
}

// pickHealth picks a Health by its label, as a tap does: the radios under the
// labels are hidden.
async function pickHealth(form: Locator, health: string): Promise<void> {
  await form.getByRole("group", { name: "Health" }).getByText(health, { exact: true }).click();
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
  await expect(summaryOf(form).getByText(health, { exact: true })).toBeVisible();
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
  await dates.locator("summary").click();
  await dates.getByLabel("New Milestone name").fill("Docs");
  await expect(summaryOf(form)).toContainText("dates changed");
  await dates.getByLabel("New Milestone name").fill("");
  await expect(summaryOf(form)).not.toContainText("dates changed");
});

test("(f) Mark Done shows → Done, and the Health badge goes", async ({ page }) => {
  const form = await openCheckin(page);
  const health = await pickedHealth(form);
  const lifecycle = form.getByTestId("checkin-lifecycle-section");
  await lifecycle.locator("summary").click();
  await lifecycle.getByRole("radio", { name: "Mark Done" }).check();
  await expect(summaryOf(form)).toContainText("→ Done");
  await expect(summaryOf(form).getByText(health, { exact: true })).toHaveCount(0);
});

test("(g) at 1280×800 Submit check-in stays in the window, and the bar ends the form", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const form = await openCheckin(page);
  const lastSection = form.getByTestId("checkin-lifecycle-section");
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

test("(h) at 390×800 the bar drops Cancel and fits the window", async ({ page }) => {
  const width = 390;
  await page.setViewportSize({ width, height: 800 });
  const form = await openCheckin(page);
  await pickHealth(form, "Yellow");
  await form.getByLabel("Plan").fill("Add a second on-call.");
  await form.getByTestId("checkin-readings").getByRole("spinbutton").first().fill("42");
  await expect(summaryOf(form)).toHaveText("Yellow · Path to Green · 1 Metric");

  const bar = form.getByTestId("checkin-submit-bar");
  await expect(bar.getByRole("link", { name: "Cancel" })).toBeHidden();
  await expect(bar.getByRole("button", { name: "Submit check-in" })).toBeInViewport();
  const outside = await page.evaluate((width) => {
    return [...document.querySelectorAll("main *")]
      .filter((e) => {
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
