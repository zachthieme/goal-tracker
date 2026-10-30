// Phone width: at 390px Home, with a Check-in due in Needs you, the Goals
// list, the Goals Table, Follow-ups, the Admin page, a Report draft, with a
// Schedule conflict's Owner's email disclosed, the New goal page, with its
// Metric editor open, a Goal page and the Check-in page fit the screen.
// Nothing inside main is clipped or scrolls sideways but the Goals Table's own
// container, which cues its scroll, and every control lies within the window
// (#343: #258, #268, #338, #229; #368; #369; #364; #387; #642; #686). The Goal
// page reads its title, its properties rail, then its column with History
// last, and the Check-in page its form before Contributed to by, with both
// laid out as before at 1280px (#674).
import type { Locator, Page } from "@playwright/test";

import { admin, expect, requirePick, signIn, test } from "./fixtures";

const phone = { width: 390, height: 800 };
const desktop = { width: 1280, height: 800 };

test.beforeEach(async ({ page }) => {
  await page.setViewportSize(phone);
});

// expectFitsAPhone asserts that no element inside main hides content behind
// its own sideways scroll or clip, and that every visible element on the page
// lies within the window. html's overflow-x:clip cuts off what passes the
// window's edge rather than letting the page scroll to it, so the page's
// scrollWidth can't tell; each element's box can. scrollers are the test ids
// of containers DESIGN.md § Phone width lets scroll sideways inside
// themselves, such as the Check-in page's add bar's buttons (#365); what they
// hold is left out, and a scroller itself must still fit.
async function expectFitsAPhone(page: Page, scrollers: string[] = []): Promise<void> {
  const clipped = await page.evaluate(
    (scrollers) =>
      [...document.querySelectorAll("main *")]
        .filter((e) => {
          if (scrollers.includes((e as HTMLElement).dataset.testid ?? "")) return false;
          const overflow = getComputedStyle(e).overflowX;
          return (
            overflow !== "visible" &&
            e.clientWidth > 1 &&
            e.getClientRects().length &&
            e.scrollWidth > e.clientWidth + 1
          );
        })
        .map(
          (e) => `${(e as HTMLElement).dataset.testid || e.className || e.tagName} ${e.scrollWidth}>${e.clientWidth}`,
        ),
    scrollers,
  );
  expect(clipped, "elements in main that clip or scroll sideways").toEqual([]);

  const outside = await page.evaluate(
    ({ width, scrollers }) => {
      return [...document.body.querySelectorAll("*")]
        .filter((e) => {
          if (scrollers.some((id) => e.parentElement?.closest(`[data-testid="${id}"]`))) return false;
          // checkVisibility is false inside a closed details, whose content
          // still has a box.
          if (!e.checkVisibility({ visibilityProperty: true })) return false;
          const r = e.getBoundingClientRect();
          return r.width > 0 && (r.left < -1 || r.right > width + 1);
        })
        .map((e) => {
          const r = e.getBoundingClientRect();
          const text = (e.textContent || (e as HTMLInputElement).name || "").trim().slice(0, 40);
          return `${e.tagName.toLowerCase()}${(e as HTMLElement).dataset.testid ? `[${(e as HTMLElement).dataset.testid}]` : ""} "${text}" (${Math.round(r.left)}–${Math.round(r.right)}px)`;
        });
    },
    { width: phone.width, scrollers },
  );
  expect(outside, `elements past the ${phone.width}px window`).toEqual([]);
}

// buildRuleReport saves a Report Definition named name with the rule Team is
// any of team, and lands on its draft. A local copy of the one in
// scenarios/02-watching-goals.spec.ts, which picks Growth.
async function buildRuleReport(page: Page, name: string, team = "Platform"): Promise<void> {
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  await builder.getByLabel("Name").fill(name);
  await builder.getByTestId("report-mode").getByText("Goals matching rules").click();
  const rule = builder.getByTestId("report-rule").first();
  await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
  await rule.getByLabel("Operator").selectOption({ label: "is any of" });
  await rule.getByLabel("Values").selectOption({ label: team });
  await builder.getByRole("button", { name: "Save report" }).click();
  await page.waitForURL(/\/reports\/\d+$/);
}

// The due template's manifest names dueNoChangeGoal, a 7-day Goal, due on
// Home, whose Owner's Needs you row offers No change beside Check in: Active,
// checked in before, no accepted child Goals (so no Rolled-up Health to
// refuse a repeat), no overdue Planned Milestone, and no back-to-Green date
// passed on its latest Check-in, as isolation.spec.ts's noChangeGoal.
test.describe("on the due template", () => {
  test.use({ template: "due" });

  test("at 390px Home fits, a Check-in due in Needs you with its No change and Check in", async ({
    page,
    manifest,
  }) => {
    const goal = requirePick("dueNoChangeGoal", manifest.dueNoChangeGoal);
    await signIn(page, goal.email);
    const row = page
      .getByTestId("home-needs-you")
      .getByTestId("home-due-goal")
      .filter({ has: page.getByRole("link", { name: goal.title, exact: true }) });
    await expect(row).toBeVisible();
    await expect(row.getByRole("button", { name: "No change" })).toBeVisible();
    await expect(row.getByTestId("home-due-checkin")).toBeVisible();
    await expectFitsAPhone(page);
  });
});

test("at 390px the Goals list stacks each Goal's Owner and Due under its title", async ({ page }) => {
  await signIn(page, "ceo@example.com");
  await page.goto("/goals?org=all");
  const row = page.getByTestId("goal-row").first();
  await expect(row.locator(".gl-owner")).toBeVisible();
  await expect(row.getByTestId("goal-row-due")).toBeVisible();
  await expectFitsAPhone(page);
});

// The Goals Table doesn't stack at phone width: it keeps every column and
// scrolls sideways inside its own container, which fades the edge it can
// scroll toward (#686). The manifest's metricGoal's Owner has a Goal in Mine.
test("at 390px the Goals Table keeps its columns and cues its sideways scroll", async ({ page, manifest }) => {
  const goal = requirePick("metricGoal", manifest.metricGoal);
  await signIn(page, goal.email);
  await page.goto("/goals?layout=table");
  const scroll = page.getByTestId("goal-table-scroll");
  await expect(scroll).toBeVisible();
  const head = scroll.getByTestId("goal-table-head");
  for (const name of ["Owner", "Lifecycle", "Last check-in"]) {
    await expect(head.getByRole("columnheader", { name, exact: true }), `the ${name} column stays`).toBeAttached();
  }
  const { scrollWidth, clientWidth, attachment } = await scroll.evaluate((e) => ({
    scrollWidth: e.scrollWidth,
    clientWidth: e.clientWidth,
    attachment: getComputedStyle(e).backgroundAttachment,
  }));
  expect(scrollWidth, "the table is wider than its container").toBeGreaterThan(clientWidth);
  expect(attachment, "the fades are attached local and scroll").toContain("local");
  expect(attachment, "the fades are attached local and scroll").toContain("scroll");
  await scroll.evaluate((e) => {
    e.scrollLeft = e.scrollWidth;
  });
  await expectFitsAPhone(page, ["goal-table-scroll"]);
});

test("at 390px the Follow-ups filters wrap, both selects on screen", async ({ page }) => {
  await signIn(page, "ceo@example.com");
  await page.goto("/follow-ups");
  await expect(page.getByTestId("follow-ups-scope")).toBeVisible();
  await expect(page.getByTestId("follow-ups-value")).toBeVisible();
  await expectFitsAPhone(page);
});

test("at 390px the Admin page shows the whole Make Admin button inside its form row", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/admin");
  const row = page.locator("[data-testid=admin-admins] .ad-links");
  const button = row.getByRole("button", { name: "Make Admin" });
  await expect(button).toBeVisible();
  const box = (await row.boundingBox())!;
  const b = (await button.boundingBox())!;
  expect(b.x, "the button's left is inside the row").toBeGreaterThanOrEqual(box.x);
  expect(b.x + b.width, "the button's right is inside the row").toBeLessThanOrEqual(box.x + box.width);
  await expectFitsAPhone(page);
});

test("at 390px a Report card's Health leads its title, and its badges drop under them", async ({ page }) => {
  await signIn(page, admin);
  await buildRuleReport(page, "Platform team");
  // The draft's narrow column, its Delegates and Report log, comes under
  // the Report at full width.
  const main = (await page.getByTestId("report-draft").boundingBox())!;
  const side = (await page.getByTestId("draft-side").boundingBox())!;
  expect(side.y, "the Delegates and Report log sit under the Report").toBeGreaterThanOrEqual(main.y + main.height);
  expect(Math.abs(side.width - main.width), "the narrow column spans the Report's width").toBeLessThanOrEqual(1);
  const cards = page.getByTestId("report-exception");
  await expect(cards.first()).toBeVisible();
  const n = await cards.count();
  expect(n, "the draft has a Goal card").toBeGreaterThan(0);
  for (let i = 0; i < n; i++) {
    const card = cards.nth(i);
    const name = await card.locator("h3").textContent();
    const title = (await card.locator("h3").boundingBox())!;
    const health = (await card.getByTestId("report-health").boundingBox())!;
    expect(health.x + health.width, `"${name}": Health sits left of the title`).toBeLessThanOrEqual(title.x + 1);
    expect(health.y, `"${name}": Health sits on the title's first line`).toBeLessThan(title.y + title.height / 2);
    for (const badge of await card.getByTestId("report-badge").all()) {
      const b = (await badge.boundingBox())!;
      expect(b.y, `"${name}": its badges sit under the title`).toBeGreaterThanOrEqual(title.y + title.height - 1);
    }
  }
  await expectFitsAPhone(page);
});

// The manifest's findingTrouble.twoReasons is a Goal due later than a Goal it
// contributes to, so its block on a Report draft names that Goal and its Owner
// in a Schedule conflict, the Owner's Name in brackets (#642).
test("at 390px a Schedule conflict's Owner discloses their email, and the draft still fits", async ({
  page,
  manifest,
}) => {
  const goal = requirePick("findingTrouble.twoReasons", manifest.findingTrouble.twoReasons);
  await signIn(page, admin);
  await buildRuleReport(page, `${goal.team} schedule conflicts`, goal.team);
  const block = page.getByTestId("report-exception").filter({ has: page.locator("h3", { hasText: goal.title }) });
  const owner = block.getByTestId("report-schedule-conflict").first().locator(".person");
  const name = owner.getByRole("button");
  const email = owner.locator(".person-email");
  await expect(email).toBeHidden();
  await name.click();
  await expect(name).toHaveAttribute("aria-expanded", "true");
  await expect(email).toBeVisible();
  await expect(email).toHaveText((await name.getAttribute("title"))!);
  await expectFitsAPhone(page);
  await name.click();
  await expect(name).toHaveAttribute("aria-expanded", "false");
  await expect(email).toBeHidden();
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

// The manifest's metricParentGoal is an Active Goal with a Metric, a Check-in
// and an accepted child, so its Check-in page's context column has Last time,
// Contributed to by and Dates, and its Owner, who can check in on it.
test("at 390px the Check-in page fits", async ({ page, manifest }) => {
  const goal = requirePick("metricParentGoal", manifest.metricParentGoal);
  await signIn(page, goal.email);
  await page.goto(`/goals/${goal.id}/checkin`);
  const form = page.getByTestId("checkin-form");
  // The Health choices are labels over hidden radios, so pick one by its
  // label, as a tap does.
  await form.getByRole("group", { name: "Health" }).getByTitle("Yellow", { exact: true }).click();
  await expect(form.getByRole("radio", { name: "Yellow" })).toBeChecked();
  await expect(form.getByTestId("path-to-green-field")).toBeVisible();
  await expect(form.getByTestId("checkin-readings")).toBeVisible();
  // The context column keeps only Contributed to by, which the form's ghost
  // text doesn't cover (#366, #370).
  const context = page.getByTestId("checkin-context");
  for (const id of ["checkin-last-time", "checkin-context-dates"]) {
    await expect(context.getByTestId(id), `${id} is on the page`).toBeAttached();
    await expect(context.getByTestId(id), `${id} is hidden`).toBeHidden();
  }
  const children = context.getByTestId("checkin-children");
  await expect(children).toBeVisible();
  await expect(children).toHaveCSS("border-top-style", "none");
  await expect(children).toHaveCSS("padding-top", "0px");
  // How is it going? comes before Contributed to by (#674).
  const formBox = (await box(form))!;
  const contextBox = (await box(context))!;
  expect(formBox.bottom, "the form ends above the context column").toBeLessThanOrEqual(contextBox.top + 1);
  await expectFitsAPhone(page, ["checkin-add-buttons"]);
});

// At 1280px the Check-in page keeps its context column to the form's right,
// starting level with the form, though it comes after the form in the page
// (#674).
test("at 1280px the Check-in page's context column sits right of the form", async ({ page, manifest }) => {
  await page.setViewportSize(desktop);
  const goal = requirePick("metricParentGoal", manifest.metricParentGoal);
  await signIn(page, goal.email);
  await page.goto(`/goals/${goal.id}/checkin`);
  const form = (await box(page.getByTestId("checkin-form")))!;
  const context = (await box(page.getByTestId("checkin-context")))!;
  expect(context.left, "the context column is right of the form").toBeGreaterThanOrEqual(form.right - 1);
  expect(context.top, "the context column starts above the form's end").toBeLessThanOrEqual(form.bottom + 1);
});

// At 390px a Goal's page reads its title, then its properties rail, then the
// document column with History last, so Health, Lifecycle, Owner, Delegates,
// Dimensions and Contributes to come right after the title (#674).
test("at 390px the Goal page shows its properties after the title and History last", async ({
  page,
  manifest,
}) => {
  const goal = requirePick("metricParentGoal", manifest.metricParentGoal);
  await signIn(page, goal.email);
  await page.goto(`/goals/${goal.id}`);
  const at = async (id: string) => (await box(page.getByTestId(id)))!;
  const title = await at("goal-title");
  const sidebar = await at("goal-sidebar");
  const latest = await at("goal-checkins");
  expect(title.bottom, "the title ends above the rail").toBeLessThanOrEqual(sidebar.top + 1);
  expect(sidebar.bottom, "the rail ends above Latest status").toBeLessThanOrEqual(latest.top + 1);
  const history = await at("goal-history");
  for (const id of ["goal-sidebar", "goal-checkins", "goal-metrics", "goal-comments"]) {
    expect(history.top, `History starts below ${id}`).toBeGreaterThanOrEqual((await at(id)).bottom - 1);
  }
  for (const id of ["goal-meta", "goal-delegates", "goal-dimensions", "goal-parents"]) {
    await expect(page.getByTestId(id), `${id} shows`).toBeVisible();
  }
  await expectFitsAPhone(page);
});

// At 1280px the same Goal page looks as it did before the rail moved up the
// page: the title over the column on the left, and the rail down the right
// from the top (#674).
test("at 1280px the Goal page's rail runs down the right beside the title and column", async ({
  page,
  manifest,
}) => {
  await page.setViewportSize(desktop);
  const goal = requirePick("metricParentGoal", manifest.metricParentGoal);
  await signIn(page, goal.email);
  await page.goto(`/goals/${goal.id}`);
  const at = async (id: string) => (await box(page.getByTestId(id)))!;
  const sidebar = await at("goal-sidebar");
  const main = await at("goal-main");
  const head = await at("goal-head");
  expect(sidebar.left, "the rail is right of the column").toBeGreaterThanOrEqual(main.right - 1);
  expect(sidebar.left, "the rail is right of the title").toBeGreaterThanOrEqual(head.right - 1);
  const title = await at("goal-title");
  expect(sidebar.top, "the rail starts level with the title or above").toBeLessThanOrEqual(title.top + 1);
  expect(head.bottom, "the title sits over the column").toBeLessThanOrEqual(main.top + 1);
});

// box is the locator's bounding box by its edges, or null when it has none.
async function box(
  locator: Locator,
): Promise<{ top: number; bottom: number; left: number; right: number } | null> {
  const b = await locator.boundingBox();
  return b && { top: b.y, bottom: b.y + b.height, left: b.x, right: b.x + b.width };
}
