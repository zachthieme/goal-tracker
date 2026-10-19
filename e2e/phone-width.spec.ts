// Phone width: at 390px the Goals list, Risks, the Admin page, a Report draft,
// the New goal page, with its Metric editor open, and the Check-in page fit
// the screen. Nothing inside main is clipped or scrolls sideways, and every
// control lies within the window (#343: #258, #268, #338, #229; #368; #369;
// #364).
import type { Page } from "@playwright/test";

import { admin, expect, signIn, test } from "./fixtures";

const phone = { width: 390, height: 800 };

test.beforeEach(async ({ page }) => {
  await page.setViewportSize(phone);
});

// expectFitsAPhone asserts that no element inside main hides content behind
// its own sideways scroll or clip, and that every visible link, button, input
// and select inside main lies within the window.
async function expectFitsAPhone(page: Page): Promise<void> {
  const clipped = await page.evaluate(() =>
    [...document.querySelectorAll("main *")]
      .filter((e) => {
        const overflow = getComputedStyle(e).overflowX;
        return (
          overflow !== "visible" && e.clientWidth > 1 && e.getClientRects().length && e.scrollWidth > e.clientWidth + 1
        );
      })
      .map((e) => `${(e as HTMLElement).dataset.testid || e.className || e.tagName} ${e.scrollWidth}>${e.clientWidth}`),
  );
  expect(clipped, "elements in main that clip or scroll sideways").toEqual([]);

  const outside = await page.evaluate((width) => {
    return [...document.querySelectorAll("main a, main button, main input, main select")]
      .filter((e) => {
        // checkVisibility is false inside a closed details, whose content
        // still has a box.
        if (!e.checkVisibility({ visibilityProperty: true })) return false;
        const r = e.getBoundingClientRect();
        return r.width > 0 && (r.left < -1 || r.right > width + 1);
      })
      .map((e) => {
        const r = e.getBoundingClientRect();
        return `${e.tagName.toLowerCase()} "${(e.textContent || (e as HTMLInputElement).name || "").trim()}" (${Math.round(r.left)}–${Math.round(r.right)}px)`;
      });
  }, phone.width);
  expect(outside, `controls in main past the ${phone.width}px window`).toEqual([]);
}

// buildRuleReport saves a Report Definition named name with the rule Team is
// any of Platform, and lands on its draft. A local copy of the one in
// scenarios/02-watching-goals.spec.ts, which picks Growth.
async function buildRuleReport(page: Page, name: string): Promise<void> {
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

test("at 390px the Goals list stacks each Goal's Owner and Due under its title", async ({ page }) => {
  await signIn(page, "ceo@example.com");
  await page.goto("/goals?org=all");
  const row = page.getByTestId("goal-row").first();
  await expect(row.locator(".gl-owner")).toBeVisible();
  await expect(row.getByTestId("goal-row-due")).toBeVisible();
  await expectFitsAPhone(page);
});

test("at 390px the Risks filters wrap, both selects on screen", async ({ page }) => {
  await signIn(page, "ceo@example.com");
  await page.goto("/risks");
  await expect(page.getByTestId("risks-scope")).toBeVisible();
  await expect(page.getByTestId("risks-value")).toBeVisible();
  await expectFitsAPhone(page);
});

test("at 390px the Admin page shows the whole Make Admin button inside its card", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/admin");
  const card = page.locator("[data-testid=admin-admins] .card.ad-links");
  const button = card.getByRole("button", { name: "Make Admin" });
  await expect(button).toBeVisible();
  const box = (await card.boundingBox())!;
  const b = (await button.boundingBox())!;
  expect(b.x, "the button's left is inside the card").toBeGreaterThanOrEqual(box.x);
  expect(b.x + b.width, "the button's right is inside the card").toBeLessThanOrEqual(box.x + box.width);
  await expectFitsAPhone(page);
});

test("at 390px a Report card's Health drops under its title", async ({ page }) => {
  await signIn(page, admin);
  await buildRuleReport(page, "Platform team");
  const cards = page.getByTestId("report-exception");
  await expect(cards.first()).toBeVisible();
  const n = await cards.count();
  expect(n, "the draft has a Goal card").toBeGreaterThan(0);
  for (let i = 0; i < n; i++) {
    const card = cards.nth(i);
    const title = (await card.locator("h3").boundingBox())!;
    const health = (await card.getByTestId("report-health").boundingBox())!;
    expect(health.y, `"${await card.locator("h3").textContent()}": Health sits under the title`).toBeGreaterThanOrEqual(
      title.y + title.height - 1,
    );
  }
  await expectFitsAPhone(page);
});

test("at 390px the New goal page stacks its Kind tiles and fits its cadence and Metric editor", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/goals/new");
  const form = page.getByTestId("goal-form");
  const kind = form.getByRole("group", { name: "Kind" });
  await kind.getByText("Dated", { exact: true }).click();
  await expect(kind.getByRole("radio", { name: /^Dated/ })).toBeChecked();
  await expect(form.getByLabel("Delivery date", { exact: true })).toBeVisible();
  const cadence = form.getByRole("group", { name: "Check-in cadence" });
  await cadence.getByText("Custom", { exact: true }).click();
  await expect(cadence.getByRole("radio", { name: "Custom" })).toBeChecked();
  await expect(form.getByLabel("Days between Check-ins")).toBeVisible();
  const parents = form.getByTestId("goal-form-parents-row");
  await parents.locator("summary").click();
  await expect(form.getByRole("group", { name: "Contributes to" })).toBeVisible();
  await form.getByRole("button", { name: "+ Metric" }).click();
  await expect(form.getByTestId("metric-editor")).toBeVisible();

  const dated = (await kind.locator(".gf-tile").first().boundingBox())!;
  const ongoing = (await kind.locator(".gf-tile").last().boundingBox())!;
  expect(ongoing.y, "the Ongoing tile sits under the Dated one").toBeGreaterThanOrEqual(dated.y + dated.height - 1);
  await expectFitsAPhone(page);
});

// goalWithMetric is an Active Goal with a Metric, and its Owner, who can
// check in on it.
const goalWithMetric = `
  select g.id, a.email
  from goals g join accounts a on a.id = g.owner_id
  where g.lifecycle = 'Active' and a.departed = 0
    and exists (select 1 from metrics m where m.goal_id = g.id)
  order by g.id
  limit 1`;

test("at 390px the Check-in page fits", async ({ page, seedLookup }) => {
  const [goal] = seedLookup<{ id: number; email: string }>(goalWithMetric);
  expect(goal, "the seed has an Active Goal with a Metric").toBeDefined();
  await signIn(page, goal.email);
  await page.goto(`/goals/${goal.id}/checkin`);
  const form = page.getByTestId("checkin-form");
  // The Health choices are labels over hidden radios, so pick one by its
  // label, as a tap does.
  await form.getByRole("group", { name: "Health" }).getByText("Yellow", { exact: true }).click();
  await expect(form.getByRole("radio", { name: "Yellow" })).toBeChecked();
  await expect(form.getByTestId("path-to-green-field")).toBeVisible();
  await expect(form.getByTestId("checkin-readings")).toBeVisible();
  await expectFitsAPhone(page);
});
