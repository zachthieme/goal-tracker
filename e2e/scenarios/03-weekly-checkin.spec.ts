// Scenario 3 in docs/scenarios.md, The weekly Check-in: an Owner clears a
// routine week from Home in a few clicks, writing fresh only the Check-in on the
// Goal that moved, and a Delegate covers for an Owner who's away. Steps 3.1–3.6 are the week,
// 3.D1–3.D4 the Delegate. "Four Goals took under five minutes" can't be
// asserted, so it isn't.
import type { Page } from "@playwright/test";

import { appToday, expect, signIn, test } from "../fixtures";

type OwnedGoal = { id: number; title: string; metric: number };

// weekOwners are the Owners who could be the scenario's Priya, most Active
// Goals first: at least two Active Goals, exactly one with a Metric, and none
// a No change would be refused on (no accepted children, so no Rolled-up
// Health to differ from, and no overdue Planned Milestone). Never cto@, whom
// the seed names Priya Raman. Which of their Goals are due is read from Home.
const weekOwners = `
  select a.email, a.name
  from accounts a join goals g on g.owner_id = a.id
  where a.departed = 0 and a.email <> 'cto@example.com' and g.lifecycle = 'Active'
  group by a.id
  having count(*) >= 2
    and sum(exists (select 1 from metrics m where m.goal_id = g.id)) = 1
    and sum(exists (select 1 from links l where l.parent_id = g.id and l.status = 'accepted')) = 0
    and sum(exists (select 1 from milestones m
                    where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))) = 0
  order by count(*) desc, a.id`;

const activeGoalsOf = `
  select g.id, g.title, exists (select 1 from metrics m where m.goal_id = g.id) as metric
  from goals g join accounts a on a.id = g.owner_id
  where a.email = ? and g.lifecycle = 'Active'
  order by g.id`;

test.describe("on the due template", () => {
  test.use({ template: "due" });

  test("the week: No change on the routine Goals, a fresh Check-in over the ghost text on the one that moved", async ({
    page,
    seedLookup,
  }) => {
    // Priya is the first candidate whose Home lists at least two Check-ins
    // due, her Metric Goal among them.
    let due: OwnedGoal[] = [];
    let metricGoal: OwnedGoal | undefined;
    for (const owner of seedLookup<{ email: string; name: string }>(weekOwners)) {
      await signIn(page, owner.email);
      const goals = seedLookup<OwnedGoal>(activeGoalsOf, owner.email);
      const listed = await dueRows(page).getByRole("link").allTextContents();
      due = goals.filter((g) => listed.includes(g.title));
      metricGoal = due.find((g) => g.metric);
      if (due.length >= 2 && metricGoal) break;
    }
    expect(metricGoal, "an Owner has two Check-ins due, one on her Metric Goal").toBeDefined();
    if (!metricGoal) return;
    const metric = metricGoal;
    const routine = due.filter((g) => g !== metric);

    await test.step("3.1 Signing in lands on Home, where Needs you lists each Check-in due", async () => {
      await expect(page).toHaveURL(/\/home$/);
      await expect(page.getByRole("heading", { level: 1, name: "Your week" })).toBeVisible();
      await expect(dueRows(page)).toHaveCount(due.length);
      for (const g of due) {
        const row = dueRow(page, g);
        await expect(row.getByTestId("home-due-health")).toHaveText(/^\s*(Green|Yellow|Red|No Health yet)\s*$/);
        await expect(row).toContainText(/(Last check-in|No check-in since activation) .+ on a \d+-day cadence/);
      }
      // The seed has no pending Requests, so the count is the rows.
      await expect(page.getByTestId("home-requests")).toHaveCount(0);
      await expect(page.getByTestId("nav-home")).toHaveText(navHome(due.length));
    });

    await test.step("3.2 The optional parts of the Check-in form start folded", async () => {
      // It worked if: the optional parts stayed folded away until needed.
      const [first] = routine;
      await page.goto(`/goals/${first.id}/checkin`);
      const form = page.getByTestId("checkin-form");
      await expect(form).toBeVisible();
      await expect(form.getByText(/^Highlight \d+ · Draft$/)).toHaveCount(0);
      for (const section of ["checkin-highlight-section", "checkin-dates-section", "checkin-lifecycle-section"]) {
        await expect(form.getByTestId(section), `${section} is closed`).toHaveJSProperty("open", false);
      }
    });

    await test.step("3.3 No change on each routine Goal repeats its last Check-in and leaves Home", async () => {
      let left = due.length;
      for (const g of routine) {
        await page.goto(`/goals/${g.id}`);
        const readings = await page.getByTestId("metric-current").allTextContents();
        const status = ((await page.getByTestId("goal-status").textContent()) ?? "").trim();

        await page.goto("/home");
        await dueRow(page, g).getByRole("button", { name: "No change" }).click();
        await page.waitForURL(new RegExp(`/goals/${g.id}(/checkins/no-change)?$`));
        // A refused No change lands on the Check-in form with the reason;
        // weekOwners chose an Owner none of whose Goals should refuse one.
        const refusal = page.getByTestId("checkin-error");
        if (await refusal.isVisible()) {
          throw new Error(`No change on "${g.title}" was refused: ${await refusal.innerText()}`);
        }
        await expect(page).toHaveURL(new RegExp(`/goals/${g.id}$`));

        // The new entry repeats the previous Check-in, so History folds it
        // into a "No change" entry dated today by its one time element, with
        // the repeated Check-in in full below it, its Status unchanged.
        const [latest, previous] = [checkinEntries(page).first(), checkinEntries(page).nth(1)];
        await expect(latest).toHaveAttribute("data-folded", /^\d+$/);
        await expect(latest.locator(".tl-kind")).toHaveText(/^No change( ×\d+)?$/);
        await expect(latest.locator("time")).toHaveCount(1);
        await expect(latest.locator("time")).toHaveAttribute("datetime", today());
        await expect(previous).not.toHaveAttribute("data-folded", /.*/);
        await expect(previous.getByRole("paragraph").nth(1)).toContainText(status);
        // It carries no Metric reading: a reading is a measurement, not copied,
        // and the Goal's Metric still shows its last one.
        await expect(latest.getByTestId("checkin-detail")).toHaveCount(0);
        await expect(page.getByTestId("metric-current")).toHaveText(readings);

        await page.goto("/home");
        left--;
        await expect(dueRow(page, g)).toHaveCount(0);
        await expect(dueRows(page)).toHaveCount(left);
        await expect(page.getByTestId("nav-home")).toHaveText(navHome(left));
      }
    });

    // The Metric Goal's latest Check-in and reading, as its page shows them.
    const latest = { health: "", status: "", pathToGreen: "", reading: "" };
    const draftNote = "Cold-start fix landed on the beta channel";

    await test.step("3.4 She logs a Draft Highlight on the Goal that moved", async () => {
      await page.goto(`/goals/${metric.id}`);
      latest.health = ((await page.getByTestId("goal-health").textContent()) ?? "").trim();
      latest.status = (await page.getByTestId("goal-status").textContent()) ?? "";
      if (latest.health !== "Green") {
        const path = (await page.getByTestId("goal-path-to-green").textContent()) ?? "";
        latest.pathToGreen = path.match(/Path to Green:\s*([\s\S]*?)\s*— back to Green by/)?.[1] ?? "";
        expect(latest.pathToGreen, "the Goal has a Path to Green").not.toBe("");
      }
      latest.reading = ((await page.getByTestId("metric-current").textContent()) ?? "").trim();
      expect(latest.reading, "the Metric has a reading").toMatch(/^-?[\d.]+$/);

      const drafts = page.getByTestId("goal-draft-highlights");
      await expect(drafts.getByRole("heading", { name: "Draft Highlights" })).toBeVisible();
      await drafts.getByRole("link", { name: "Log a highlight" }).click();
      const log = drafts.getByTestId("log-draft-highlight");
      await log.getByLabel("Kind").selectOption("Accomplishment");
      await log.getByLabel("Note").fill(draftNote);
      await log.getByRole("button", { name: "Log Draft Highlight" }).click();
      await expect(page.getByTestId("draft-highlight")).toContainText(draftNote);
    });

    const newStatus = "Cold start p90 down after the lazy-init change; beta looks good.";
    const newReading = latest.reading === "3" ? "4" : "3";

    await test.step("3.5 Check in shows last week as ghost text, offers the Draft, and keeps it as a Highlight", async () => {
      await page.goto("/home");
      await dueRow(page, metric).getByRole("link", { name: "Check in" }).click();
      await expect(page).toHaveURL(new RegExp(`/goals/${metric.id}/checkin$`));
      const form = page.getByTestId("checkin-form");

      // It worked if: her Health didn't have to be picked again, and last
      // week's answers are in view as ghost text, none of them sent unless
      // she types it. The Status and reading start empty.
      await expect(form.getByRole("radio", { name: latest.health })).toBeChecked();
      const status = form.getByRole("textbox", { name: /^Status/ });
      await expect(status).toHaveValue("");
      await expect(status).toHaveAttribute("placeholder", latest.status);
      const reading = form.getByTestId("checkin-readings").getByRole("spinbutton");
      await expect(reading).toHaveValue("");
      await expect(reading).toHaveAttribute("placeholder", latest.reading);
      const path = form.getByTestId("path-to-green-field");
      if (latest.health !== "Green") {
        await expect(path.getByLabel("Plan")).toHaveValue("");
        await expect(path.getByLabel("Plan")).toHaveAttribute("placeholder", latest.pathToGreen);
        await expect(path.getByLabel("Back to Green by")).toHaveValue("");
        await expect(path.getByTestId("checkin-path-target-last")).toHaveText(/^Last time: [A-Z][a-z]{2} \d{1,2}$/);
      }

      // The Highlight section opens to offer the Draft, ticked to keep; the
      // other optional parts stay folded (It worked if: folded until needed).
      await expect(form.getByTestId("checkin-highlight-section")).toHaveJSProperty("open", true);
      const draftRow = form.getByTestId("checkin-highlight-row").filter({ hasText: /Highlight \d+ · Draft/ });
      await expect(draftRow.getByTestId("checkin-highlight-legend")).toHaveText(/^Highlight \d+ · Draft$/);
      await expect(draftRow.getByRole("checkbox", { name: "Keep" })).toBeChecked();
      await expect(draftRow.getByLabel("Note")).toHaveValue(draftNote);
      await expect(form.getByTestId("checkin-dates-section")).toHaveJSProperty("open", false);
      await expect(form.getByTestId("checkin-lifecycle-section")).toHaveJSProperty("open", false);

      // She writes the Status and the one reading, and a Yellow or Red Goal's
      // Path to Green.
      await status.fill(newStatus);
      await reading.fill(newReading);
      if (latest.health !== "Green") {
        await path.getByLabel("Plan").fill("Ship the lazy-init change to everyone.");
        await path.getByLabel("Back to Green by").fill(addDays(appToday(), 28));
      }
      // Enter in the reading submits the Check-in, not Add another.
      await reading.press("Enter");
      await page.waitForURL(new RegExp(`/goals/${metric.id}$`));

      await expect(page.getByTestId("goal-status")).toHaveText(newStatus);
      await expect(page.getByTestId("metric-current")).toHaveText(newReading);
      await expect(page.getByTestId("goal-health")).toHaveText(latest.health);
      // The Draft became one of this Check-in's Highlights: none waits as a
      // Draft, the Check-in discarded none, and it is the newest Highlight.
      await expect(page.getByTestId("draft-highlight")).toHaveCount(0);
      const entry = checkinEntries(page).first();
      await expect(entry).toContainText(newStatus);
      await expect(entry.getByTestId("discarded-draft-highlight")).toHaveCount(0);
      const highlight = page.getByTestId("goal-highlights").getByTestId("highlight").first();
      await expect(highlight).toContainText(`Accomplishment: ${draftNote}`);
    });

    await test.step("3.6 Home says she's all caught up", async () => {
      await page.goto("/home");
      await expect(page.getByTestId("home-caught-up")).toHaveText("You're all caught up.");
      await expect(dueRows(page)).toHaveCount(0);
      await expect(page.getByTestId("nav-home")).toHaveText(navHome(0));
    });
  });
});

// checkinEntries are the Goal page's History entries that are Check-ins,
// newest first.
function checkinEntries(page: Page) {
  return page.getByTestId("history-entry").and(page.locator('[data-kind="checkin"]'));
}

// today is a datetime prefix for a History entry's time written on the app's
// today (UTC, the server's zone).
function today(): RegExp {
  return new RegExp(`^${appToday()}T`);
}

// staleGoal is a Goal on the default template whose Owner is away: Active,
// last checked in more than its cadence (and a day) ago, so Stale on Risks,
// with no accepted children or overdue Planned Milestone to complicate its
// Check-in, and no Delegates yet. The oldest first.
const staleGoal = `
  select g.id, g.title, a.email, a.name
  from goals g join accounts a on a.id = g.owner_id
  where g.lifecycle = 'Active' and a.departed = 0 and a.name is not null
    and julianday($now) - julianday((select max(c.created_at) from checkins c where c.goal_id = g.id))
        > g.cadence_days + 1
    and not exists (select 1 from links l where l.parent_id = g.id and l.status = 'accepted')
    and not exists (select 1 from milestones m
                    where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
    and not exists (select 1 from delegates d where d.goal_id = g.id)
  order by (select max(c.created_at) from checkins c where c.goal_id = g.id), g.id
  limit 1`;

// delegateFor is another seeded person, neither the Owner nor an Admin, to
// cover for them.
const delegateFor = `
  select email, name from accounts
  where departed = 0 and is_admin = 0 and name is not null and email <> ?
  order by id
  limit 1`;

test("a Delegate checks in for an Owner who's away, and the Goal stops being Stale", async ({
  page,
  as,
  seedLookup,
}) => {
  type Person = { email: string; name: string };
  const [goal] = seedLookup<{ id: number; title: string } & Person>(staleGoal);
  expect(goal, "the seed has a Stale Goal to delegate").toBeDefined();
  const owner: Person = { email: goal.email, name: goal.name };
  const [delegate] = seedLookup<Person>(delegateFor, owner.email);
  expect(delegate, "the seed has someone to delegate to").toBeDefined();

  await test.step("3.D1 The Goal is Stale on Risks and on its Owner's Home", async () => {
    await signIn(page, owner.email);
    await page.goto("/risks");
    await expect(staleChip(page, goal.title)).toBeVisible();
    await page.goto("/home");
    const row = dueRows(page).filter({ has: page.getByRole("link", { name: goal.title, exact: true }) });
    await expect(row.getByTestId("home-due-stale")).toBeVisible();
  });

  await test.step("3.D2 The Owner authorizes a Delegate from the Goal page", async () => {
    await page.goto(`/goals/${goal.id}`);
    await page.getByRole("link", { name: "Manage Delegates" }).click();
    const add = page.getByTestId("add-delegate");
    await add.getByRole("textbox", { name: "Delegate email" }).fill(delegate.email);
    await add.getByRole("button", { name: "Add Delegate" }).click();
    await expect(page.getByTestId("delegate-list").getByRole("button", { name: delegate.name })).toBeVisible();
  });

  const covering = await as(delegate.email);
  const coveringRow = dueRows(covering).filter({ has: covering.getByRole("link", { name: goal.title, exact: true }) });

  await test.step("3.D3 The Delegate finds the Goal delegated to them, for its Owner", async () => {
    const link = covering.getByTestId("home-delegate-link");
    await expect(link).toContainText("Goals delegated to you");
    await link.click();
    await expect(covering.getByRole("heading", { level: 1, name: "Goals delegated to me" })).toBeVisible();
    await expect(
      covering.getByTestId("delegated-goal").getByRole("link", { name: goal.title, exact: true }),
    ).toBeVisible();

    await covering.goto("/home");
    // The Name is visible; the email sits only in a title and a hidden span.
    await expect(coveringRow.getByTestId("home-due-for")).toHaveText(`for ${owner.name}`, { useInnerText: true });
  });

  await test.step("3.D4 The Delegate checks in; the Check-in records who wrote it, and the Goal isn't Stale", async () => {
    await coveringRow.getByRole("link", { name: "Check in" }).click();
    await expect(covering).toHaveURL(new RegExp(`/goals/${goal.id}/checkin$`));
    const status = `Covering while ${owner.name} is away: on track.`;
    const form = covering.getByTestId("checkin-form");
    await form.getByRole("textbox", { name: /^Status/ }).fill(status);
    // The Owner's Health stays picked; a Yellow or Red one needs a Path to
    // Green written afresh, as the form shows last week's only as ghost text.
    const path = form.getByTestId("path-to-green-field");
    if (await path.isVisible()) {
      await path.getByLabel("Plan").fill(`Keep to ${owner.name}'s plan until they're back.`);
      await path.getByLabel("Back to Green by").fill(addDays(appToday(), 28));
    }
    await covering.getByRole("button", { name: "Submit check-in" }).click();
    await covering.waitForURL(new RegExp(`/goals/${goal.id}$`));

    const entry = checkinEntries(covering).first();
    await expect(entry).toContainText(status);
    await expect(entry.getByRole("button", { name: delegate.name })).toBeVisible();
    await expect(entry.getByRole("button", { name: owner.name })).toBeVisible();
    await expect(entry).toContainText(`by ${delegate.name} for ${owner.name}`, { useInnerText: true });

    // The Owner's Risks opens on their default scope, which may now hold
    // nothing: the page has loaded once it shows its count or its empty line.
    await page.goto("/risks");
    await expect(page.getByTestId("risks-attention").or(page.getByTestId("risks-empty"))).toBeVisible();
    await expect(staleChip(page, goal.title)).toHaveCount(0);
    await page.goto("/home");
    await expect(page.getByRole("heading", { level: 1, name: "Your week" })).toBeVisible();
    const row = dueRows(page).filter({ has: page.getByRole("link", { name: goal.title, exact: true }) });
    await expect(row.getByTestId("home-due-stale")).toHaveCount(0);
  });
});

// staleChip is the Stale signal on title's row on Risks.
function staleChip(page: Page, title: string) {
  return page
    .getByTestId("risk-row")
    .filter({ has: page.getByRole("link", { name: title, exact: true }) })
    .getByTestId("risk-signal")
    .filter({ hasText: /^Stale/ });
}

// dueRows are Home's Check-ins due.
function dueRows(page: Page) {
  return page.getByTestId("home-due-goal");
}

// dueRow is the Check-in due row for g on Home.
function dueRow(page: Page, g: OwnedGoal) {
  return dueRows(page).filter({ has: page.getByRole("link", { name: g.title, exact: true }) });
}

// addDays is the YYYY-MM-DD date days after date.
function addDays(date: string, days: number): string {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

// navHome is the top bar's Home item carrying count, or no count at 0.
function navHome(count: number): RegExp {
  return count === 0 ? /^\s*Home\s*$/ : new RegExp(`^\\s*Home\\s*${count}\\s*$`);
}
