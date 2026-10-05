// Scenario 5 in docs/scenarios.md, Stating a Goal and connecting it: a Goal
// can't become Active until it can be judged, and it joins the graph only by
// the parent Owner's consent.
import type { Page } from "@playwright/test";

import { admin, appToday, expect, signIn, test } from "../fixtures";

// A team Goal to contribute to: Active, owned by a team lead, with its Team.
// Which of them shows Rolled-up Health Green, or none, is read from the page.
const teamGoals = `
  select g.id, g.title, a.email, a.name, dv.value as team
  from goals g
  join accounts a on a.id = g.owner_id
  join goal_dimension_values gdv on gdv.goal_id = g.id
  join dimension_values dv on dv.id = gdv.dimension_value_id
  join dimensions d on d.id = dv.dimension_id and d.name = 'Team'
  where g.lifecycle = 'Active' and a.departed = 0 and a.email like '%-lead@example.com'
  order by g.id`;

// A seeded project Owner: someone who isn't a lead, the leadership or the
// Admin, and owns an Active Goal contributing to one a lead owns. Those under
// the chosen team Goal come first.
const projectOwners = `
  select distinct a.email, a.name
  from accounts a
  join goals g on g.owner_id = a.id
  join links l on l.child_id = g.id and l.status = 'accepted'
  join goals p on p.id = l.parent_id
  join accounts pa on pa.id = p.owner_id
  where a.departed = 0 and g.lifecycle = 'Active'
    and pa.email like '%-lead@example.com'
    and a.email not like '%-lead@example.com'
    and a.email not in ('admin@example.com', 'ceo@example.com', 'cto@example.com', 'cpo@example.com')
  order by (p.id = ?) desc, a.email`;

type TeamGoal = { id: number; title: string; email: string; name: string; team: string };

// The Field the Admin makes required. The seed has none.
const fieldName = "Budget code";

test("5 Stating a Goal and connecting it", async ({ page, as, seedLookup }) => {
  test.setTimeout(120_000);

  // A, saved Proposed and finished later; B, active at once; C, contributing
  // to the team Goal.
  const goalA = {
    title: "Shorten the refund wait for EU shoppers",
    soWhat: "Refunds take nine days and customers write in asking where their money is.",
  };
  const goalB = {
    title: "Publish the status page in five languages",
    soWhat: "Customers outside the US can't read our incident updates during an outage.",
  };
  const goalC = {
    title: "Cache the pricing lookups at the edge",
    soWhat: "Pricing calls add 300ms to every page and slow the whole app at peak.",
  };
  let goalAId = 0;
  let goalCId = 0;

  await test.step("Setup: the Admin requires a new Field and the Team Dimension", async () => {
    await signIn(page, admin);

    await page.goto("/fields");
    await page.locator("summary").filter({ hasText: "Define a Field" }).click();
    const create = page.getByTestId("create-field");
    await create.getByLabel("Name").fill(fieldName);
    await create.getByLabel("a short text").check();
    await create.getByRole("button", { name: "Create Field" }).click();
    const field = page.getByTestId("field").filter({ has: page.getByRole("heading", { name: fieldName }) });
    await field.getByRole("button", { name: "Make it required" }).click();
    await expect(field.getByTestId("field-required")).toHaveText("Required");

    await page.goto("/dimensions");
    const dim = page.getByTestId("dimension").filter({ has: page.getByRole("heading", { name: "Team", exact: true }) });
    await dim.locator("summary").filter({ hasText: /^Edit$/ }).click();
    await dim.getByRole("button", { name: "Make it required" }).click();
    await expect(dim.getByTestId("dimension-required")).toHaveText("Required");
  });

  // The parent's Owner ("Marcus"): a lead's Active team Goal whose page shows
  // Rolled-up Health Green, or none.
  let found: TeamGoal | undefined;
  let rollupAtSetup = "";
  await test.step("Setup: a team Goal showing Rolled-up Health Green, or none", async () => {
    for (const c of seedLookup<TeamGoal>(teamGoals)) {
      await page.goto(`/goals/${c.id}`);
      const rollup = page.getByTestId("goal-rollup-health");
      const shown = (await rollup.count()) > 0 ? ((await rollup.textContent()) ?? "").trim() : "";
      if (shown === "" || shown === "Green") {
        found = c;
        rollupAtSetup = shown;
        break;
      }
    }
  });
  if (!found) throw new Error("no Active team Goal shows Rolled-up Health Green, or none");
  const team = found;
  const marcus = await as(team.email);

  // The project Owner ("Priya"): never the team Goal's Owner, so the hint
  // asks someone else.
  const [owner] = seedLookup<{ email: string; name: string }>(projectOwners, team.id);
  expect(owner, "the seed has a project Owner").toBeDefined();
  expect(owner.email).not.toBe(team.email);
  const priya = await as(owner.email);

  await test.step("5.1 New goal opens one page, in sections", async () => {
    await priya.goto("/home");
    await priya.getByRole("link", { name: "New goal" }).click();
    await expect(priya).toHaveURL(/\/goals\/new$/);
    await expect(priya.getByRole("heading", { level: 1, name: "New goal" })).toBeVisible();

    const form = priya.getByTestId("goal-form");
    const what = form.getByRole("group", { name: "What and why" });
    await expect(what.getByLabel("Title")).toBeVisible();
    await expect(what.getByLabel(/^So What/)).toBeVisible();
    const delivery = form.getByRole("group", { name: "Delivery" });
    await expect(delivery.getByRole("group", { name: "Kind" }).getByRole("radio")).toHaveCount(2);
    await expect(delivery.getByRole("radio", { name: /^Dated/ })).toBeVisible();
    await expect(delivery.getByRole("radio", { name: /^Ongoing/ })).toBeVisible();
    await expect(delivery.getByRole("group", { name: "Check-in cadence" })).toBeVisible();
    // The delivery date shows once Dated is chosen; it's there all along.
    await expect(delivery.getByLabel("Delivery date", { exact: true })).toBeAttached();
    const know = form.getByRole("group", { name: "How you'll know" });
    await expect(know.getByRole("heading", { name: "Milestones" })).toBeVisible();
    await expect(know.getByRole("heading", { name: "Metrics" })).toBeVisible();
    await expect(form.getByRole("group", { name: "Contributes to" })).toBeVisible();
    const fits = form.getByRole("group", { name: "Where it fits" });
    await expect(fits.getByRole("combobox", { name: "Team", exact: true })).toBeVisible();
    await expect(fits.getByLabel(fieldName, { exact: true })).toBeVisible();
  });

  await test.step("It worked if: she wasn't asked what type of thing it is", async () => {
    // Every label, legend, heading and option the form shows, but the titles of
    // the Goals it offers to contribute to, which are other people's words.
    const words = await priya
      .getByTestId("goal-form")
      .locator("label, legend, h2, option, button")
      .evaluateAll((els) =>
        els.filter((el) => !el.closest('[data-testid="parent-select"]')).map((el) => (el.textContent ?? "").trim()),
      );
    expect(words.length).toBeGreaterThan(0);
    for (const w of words) expect(w).not.toMatch(/objective|key result|\bproject\b|\bOKR\b|\bKR\b/i);
    // The Delivery Kind is the one choice of kind, and it's Dated or Ongoing.
    await expect(priya.getByRole("group", { name: "Kind" })).toContainText("Dated");
    await expect(priya.getByRole("group", { name: "Kind" })).toContainText("Ongoing");
  });

  await test.step("5.2 Ready to activate ticks off the minimum standard as she types", async () => {
    const ready = priya.getByTestId("goal-form-ready");
    const count = priya.getByTestId("goal-form-ready-count");
    const activate = ready.getByRole("button", { name: "Create and activate" });

    // It worked if: she always knew what was still missing. The card the page
    // loads with marks every item but Owner missing.
    await expectChecklist(priya, {
      "So What": false,
      Owner: true,
      "Dated with a delivery date, or Ongoing": false,
      "A Milestone or Metric": false,
      "A value in Team": false,
      [`A value in ${fieldName}`]: false,
    });
    await expect(count).toHaveText("1 of 6");

    // The live card swaps in on load, empty form or not (the test below covers
    // the empty form), and Create and activate follows it.
    await expect(activate).toBeDisabled();
    await priya.getByLabel("Title").fill(goalB.title);
    await priya.getByLabel(/^So What/).fill(goalB.soWhat);
    await expect(activate).toBeDisabled();
    await expectItem(priya, "So What", true);
    await expect(count).toHaveText("2 of 6");

    // Ongoing asks for a Metric, not a Milestone.
    await chooseKind(priya, "Ongoing");
    await expectItem(priya, "A Metric", false);
    await expectItem(priya, "Dated with a delivery date, or Ongoing", true);
    await expect(count).toHaveText("3 of 6");

    // Dated counts only with a delivery date.
    await chooseKind(priya, "Dated");
    await expectItem(priya, "A Milestone or Metric", false);
    await expectItem(priya, "Dated with a delivery date, or Ongoing", false);
    await expect(count).toHaveText("2 of 6");
    await priya.getByLabel("Delivery date", { exact: true }).fill(isoDaysFromNow(120));
    await expectItem(priya, "Dated with a delivery date, or Ongoing", true);
    await expect(count).toHaveText("3 of 6");
    await expect(activate).toBeDisabled();

    // A forced submit before the checklist is met is refused, and says why.
    const refused = priya.waitForResponse(
      (r) => r.request().method() === "POST" && new URL(r.url()).pathname === "/goals/new",
    );
    await activate.evaluate((b: HTMLButtonElement) => {
      b.disabled = false;
      b.click();
    });
    expect((await refused).status()).toBe(422);
    const errors = priya.getByTestId("goal-form-errors");
    await expect(errors).toContainText("The Goal wasn't created.");
    await expect(errors).toContainText(/Milestone or Metric/);
    await expect(errors).toContainText("value in Team");
    await expect(errors).toContainText(`value in ${fieldName}`);
    await expect(errors.getByRole("listitem")).toHaveCount(3);
    // The refused form comes back as typed, its card live again.
    await expect(priya.getByLabel("Title")).toHaveValue(goalB.title);
    await expect(activate).toBeDisabled();
    await expect(count).toHaveText("3 of 6");

    const milestone = priya.getByTestId("milestone-row").first();
    await milestone.getByLabel("Name").fill("Translations reviewed");
    await milestone.getByLabel("Date").fill(isoDaysFromNow(60));
    await expectItem(priya, "A Milestone or Metric", true);
    await expect(count).toHaveText("4 of 6");

    await priya.getByTestId("goal-form-fits").scrollIntoViewIfNeeded();
    await expect(ready, "the card stays in view beside Where it fits").toBeInViewport();

    await priya.getByRole("combobox", { name: "Team", exact: true }).selectOption({ label: team.team });
    await expectItem(priya, "A value in Team", true);
    await expect(count).toHaveText("5 of 6");
    await expect(activate).toBeDisabled();

    await priya.getByLabel(fieldName, { exact: true }).fill("OPS-114");
    await expectItem(priya, `A value in ${fieldName}`, true);
    await expect(count).toHaveText("6 of 6");
    await expect(activate).toBeEnabled();
  });

  await test.step("5.3 Create and activate makes a Goal that meets the checklist Active at once", async () => {
    await priya.getByRole("button", { name: "Create and activate" }).click();
    await expect(priya).toHaveURL(/\/goals\/\d+$/);
    await expect(priya.getByTestId("goal-title")).toHaveText(goalB.title);
    await expect(priya.getByTestId("goal-lifecycle")).toHaveText("Active");
  });

  await test.step("5.3 She saves as Proposed and comes back through Finish defining", async () => {
    await priya.goto("/goals/new");
    await priya.getByLabel("Title").fill(goalA.title);
    await priya.getByLabel(/^So What/).fill(goalA.soWhat);
    await priya.getByRole("button", { name: "Save as Proposed" }).click();
    await expect(priya).toHaveURL(/\/goals\/\d+$/);
    goalAId = goalIdOf(priya);
    await expect(priya.getByTestId("goal-lifecycle")).toHaveText("Proposed");

    await priya.getByTestId("finish-defining").click();
    await expect(priya).toHaveURL(new RegExp(`/goals/${goalAId}/define$`));
    await expect(priya.getByRole("heading", { level: 1, name: "Finish defining" })).toBeVisible();
    const activate = priya.getByRole("button", { name: "Create and activate" });
    await expect(activate).toBeDisabled();
    await expectItem(priya, "So What", true);
    await fillDated(priya, "Refund flow live in all EU markets");
    await priya.getByRole("combobox", { name: "Team", exact: true }).selectOption({ label: team.team });
    await priya.getByLabel(fieldName, { exact: true }).fill("OPS-115");
    await expect(priya.getByTestId("goal-form-ready-count")).toHaveText("6 of 6");
    await activate.click();
    await expect(priya).toHaveURL(new RegExp(`/goals/${goalAId}$`));
    await expect(priya.getByTestId("goal-lifecycle")).toHaveText("Active");
  });

  await test.step("5.4 She picks the team Goal under Contributes to and is told its Owner will be asked", async () => {
    await priya.goto("/goals/new");
    await priya.getByLabel("Title").fill(goalC.title);
    await priya.getByLabel(/^So What/).fill(goalC.soWhat);
    await fillDated(priya, "Edge cache serving pricing");
    await priya.getByRole("combobox", { name: "Team", exact: true }).selectOption({ label: team.team });
    await priya.getByLabel(fieldName, { exact: true }).fill("OPS-116");

    const hint = priya.getByTestId("parent-hint");
    await expect(hint).toBeHidden();
    await priya.getByLabel("Search Goals to contribute to").fill(team.title);
    await priya.getByTestId("parent-result").filter({ hasText: team.title }).getByRole("button").click();
    await expect(priya.getByTestId("parent-chip").filter({ hasText: team.title })).toBeVisible();
    await expect(hint).toHaveText(`Each Owner is asked to accept: ${team.name}.`);

    await expect(priya.getByTestId("goal-form-ready-count")).toHaveText("6 of 6");
    await priya.getByRole("button", { name: "Create and activate" }).click();
    await expect(priya).toHaveURL(/\/goals\/\d+$/);
    goalCId = goalIdOf(priya);
    await expect(priya.getByTestId("goal-lifecycle")).toHaveText("Active");
  });

  await test.step("5.4 She checks in Red; the link is only a request, so the team Goal's Rolled-up Health is unchanged", async () => {
    await priya.goto(`/goals/${goalCId}/checkin`);
    const health = priya.getByTestId("checkin-health");
    await health.getByText("Red", { exact: true }).click();
    await expect(health.getByRole("radio", { name: "Red" })).toBeChecked();
    await priya.getByLabel("Plan").fill("Move the pricing cache behind the CDN this sprint.");
    await priya.getByLabel("Back to Green by").fill(isoDaysFromNow(30));
    await priya
      .getByRole("textbox", { name: /^Status/ })
      .fill("The edge cache missed its first rollout; pricing is still slow.");
    await priya.getByRole("button", { name: "Submit check-in" }).click();
    await expect(priya).toHaveURL(new RegExp(`/goals/${goalCId}$`));
    await expect(priya.getByTestId("goal-health")).toHaveText("Red");

    await marcus.goto(`/goals/${team.id}`);
    const rollup = marcus.getByTestId("goal-rollup-health");
    if (rollupAtSetup === "") await expect(rollup).toHaveCount(0);
    else await expect(rollup).toHaveText(rollupAtSetup);
  });

  await test.step("5.5 The team Goal's Owner accepts the request on Home, and her Goal counts toward its Rolled-up Health", async () => {
    await marcus.goto("/home");
    const request = marcus
      .getByTestId("home-requests")
      .getByTestId("home-request")
      .filter({ hasText: "Link request" })
      .filter({ hasText: goalC.title });
    await expect(request).toContainText(team.title);
    await request.getByRole("button", { name: "Accept" }).click();
    await expect(marcus.getByTestId("home-request").filter({ hasText: goalC.title })).toHaveCount(0);

    await marcus.goto(`/goals/${team.id}`);
    await expect(marcus.getByTestId("goal-rollup-health")).toHaveText("Red");
  });

  await test.step("5.6 With no parent, each Active Goal is Unaligned and listed on Risks", async () => {
    await priya.goto("/risks?group=plan");
    for (const g of [goalA, goalB]) {
      const unaligned = riskRow(priya, g.title).locator('[data-testid="risk-signal"][data-kind="unaligned"]');
      await expect(unaligned).toHaveText("Unaligned · no parent Goal, not Top-level");
    }
    // The Goal with a parent isn't Unaligned (it may show another plan signal,
    // such as a Schedule conflict with the team Goal).
    const linked = riskRow(priya, goalC.title).locator('[data-testid="risk-signal"][data-kind="unaligned"]');
    await expect(linked).toHaveCount(0);
  });

  await test.step("It worked if: the Team she set lets the Goal be found on /goals", async () => {
    await priya.goto("/goals");
    await priya.getByTestId("more-filters").locator("summary").click();
    await priya
      .getByTestId("more-filters")
      .getByRole("group", { name: "Team" })
      .getByRole("checkbox", { name: team.team, exact: true })
      .check();
    await expect(priya).toHaveURL(/[?&]value=\d+/);
    await expect(priya.getByTestId("goal-list").getByRole("link", { name: goalB.title, exact: true })).toBeVisible();
  });

  await test.step("It worked if: the Team she set lets the Goal be found in a Report draft", async () => {
    await priya.goto("/reports/new");
    await priya.getByLabel("Name", { exact: true }).fill(`${team.team} review`);
    const rule = priya.getByTestId("report-rule").first();
    await rule.getByTestId("rule-attribute").selectOption({ label: "Team" });
    await rule.getByTestId("rule-op").selectOption({ label: "is" });
    await rule.getByTestId("rule-values").selectOption({ label: team.team });
    await priya.getByRole("button", { name: "Save report" }).click();
    await expect(priya).toHaveURL(/\/reports\/\d+$/);
    await expect(priya.getByTestId("report-goal").filter({ hasText: goalB.title })).toHaveCount(1);
  });
});

// Step 2: "The card is server-rendered on first load. Create and activate is
// disabled only once htmx's first POST /goals/new/checklist swaps in the live
// card", and each item "ticks as it's met". That first post goes on the empty
// form too, and every change after it, with Title and So What still empty:
// the card is what says they're missing. A real submit still needs them.
test("5.2 The live Ready to activate card answers the empty form", async ({ page, seedLookup }) => {
  const [owner] = seedLookup<{ email: string }>(projectOwners, 0);
  await signIn(page, owner.email);
  const live = page.waitForResponse((r) => new URL(r.url()).pathname === "/goals/new/checklist", { timeout: 5000 });
  await page.goto("/goals/new");
  const activate = page.getByTestId("goal-form-ready").getByRole("button", { name: "Create and activate" });
  const count = page.getByTestId("goal-form-ready-count");

  await test.step("5.2 The first POST /goals/new/checklist disables Create and activate", async () => {
    expect((await live).ok()).toBe(true);
    await expect(activate).toBeDisabled();
    await expectItem(page, "So What", false);
    await expect(count).toHaveText("1 of 4");
  });

  await test.step("5.2 Choosing a Kind before a Title ticks its item", async () => {
    await chooseKind(page, "Ongoing");
    await expectItem(page, "Dated with a delivery date, or Ongoing", true);
    await expect(count).toHaveText("2 of 4");
  });

  await test.step("5.2 Saving with no Title is still refused by the browser", async () => {
    await page.getByLabel(/^So What/).fill("Refunds take a week to land.");
    await expectItem(page, "So What", true);
    await expectRefusedByTheBrowser(page, "Title", () => page.getByRole("button", { name: "Save as Proposed" }).click());
    await expect(page).toHaveURL(/\/goals\/new$/);
  });

  await test.step("5.2 On the define page, clearing So What still answers", async () => {
    await page.getByLabel("Title").fill("Refunds land in a day");
    await page.getByRole("button", { name: "Save as Proposed" }).click();
    await expect(page).toHaveURL(/\/goals\/\d+$/);
    await page.getByTestId("finish-defining").click();
    await expect(page).toHaveURL(/\/goals\/\d+\/define$/);
    await expectItem(page, "So What", true);

    const cleared = page.waitForResponse((r) => /^\/goals\/\d+\/define\/checklist$/.test(new URL(r.url()).pathname));
    await page.getByLabel(/^So What/).fill("");
    expect((await cleared).ok()).toBe(true);
    await expectItem(page, "So What", false);
    await expect(activate).toBeDisabled();
    await expectRefusedByTheBrowser(page, /^So What/, () => page.getByRole("button", { name: "Save as Proposed" }).click());
    await expect(page).toHaveURL(/\/goals\/\d+\/define$/);
  });
});

// expectRefusedByTheBrowser says submitting leaves the labelled required input
// missing and posts nothing: the browser's own validation holds the submit.
async function expectRefusedByTheBrowser(page: Page, label: string | RegExp, submit: () => Promise<void>) {
  const posts: string[] = [];
  const record = (r: { method(): string; url(): string }) => {
    const path = new URL(r.url()).pathname;
    if (r.method() === "POST" && !path.endsWith("/checklist")) posts.push(path);
  };
  page.on("request", record);
  await submit();
  await expect.poll(() => page.getByLabel(label).evaluate((el: HTMLInputElement) => el.validity.valueMissing)).toBe(true);
  page.off("request", record);
  expect(posts).toEqual([]);
}

// expectItem says the Ready to activate card marks the item done or missing.
async function expectItem(page: Page, label: string, done: boolean) {
  const item = page
    .getByTestId("goal-form-ready")
    .getByTestId("activation-checklist")
    .getByRole("listitem")
    .filter({ has: page.getByText(label, { exact: true }) });
  await expect(item).toHaveAttribute("data-done", String(done));
  await expect(item).toContainText(done ? "done" : "missing");
}

// expectChecklist says the card lists exactly these items, each done or
// missing.
async function expectChecklist(page: Page, items: Record<string, boolean>) {
  const list = page.getByTestId("goal-form-ready").getByTestId("activation-checklist").getByRole("listitem");
  await expect(list).toHaveCount(Object.keys(items).length);
  for (const [label, done] of Object.entries(items)) await expectItem(page, label, done);
}

// chooseKind presses the Kind's segment, whose label covers its radio.
async function chooseKind(page: Page, kind: "Dated" | "Ongoing") {
  const group = page.getByRole("group", { name: "Kind" });
  await group.getByText(kind, { exact: true }).click();
  await expect(group.getByRole("radio", { name: new RegExp(`^${kind}`) })).toBeChecked();
}

// fillDated makes the form Dated, due in four months, with one Milestone.
async function fillDated(page: Page, milestone: string) {
  await chooseKind(page, "Dated");
  await page.getByLabel("Delivery date", { exact: true }).fill(isoDaysFromNow(120));
  const row = page.getByTestId("milestone-row").first();
  await row.getByLabel("Name").fill(milestone);
  await row.getByLabel("Date").fill(isoDaysFromNow(60));
}

// riskRow is the Risks row of the Goal titled title.
function riskRow(page: Page, title: string) {
  return page.getByTestId("risk-row").filter({ has: page.getByRole("link", { name: title, exact: true }) });
}

function goalIdOf(page: Page): number {
  return Number(new URL(page.url()).pathname.split("/").pop());
}

// isoDaysFromNow is the app's today plus days, as a date input takes it.
function isoDaysFromNow(days: number): string {
  return new Date(Date.parse(appToday()) + days * 86_400_000).toISOString().slice(0, 10);
}
