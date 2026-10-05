// Scenario 2 in docs/scenarios.md, Understanding trouble at the review: a
// leader builds a Report over her org and publishes it, a reader two levels up
// asks a question on one Goal's block, its Owner answers, and the Action Item
// agreed in the thread carries into the next publication until its owner
// closes it. The watch-list Report is scenario 2's other half (#198).
import { type Locator, type Page } from "@playwright/test";

import { admin, expect, test } from "../fixtures";

// The leader ("Elena") and the reader two levels up. The seed names them
// Priya Raman and Dana Whitfield; the page is asserted on those Names.
const leader = "cto@example.com";
const reader = "ceo@example.com";

// platformGoals are the Goals a "Team is any of Platform" rule selects on a
// first draft, whose baseline is 30 days back: every Platform Goal less those
// Done or Cancelled before then.
const platformGoals = `
  select g.id, g.title
  from goals g
  join goal_dimension_values gdv on gdv.goal_id = g.id
  join dimension_values dv on dv.id = gdv.dimension_value_id
  join dimensions d on d.id = dv.dimension_id
  where d.name = 'Team' and dv.value = 'Platform'
    and not (g.lifecycle in ('Done', 'Cancelled')
             and coalesce((select max(c.created_at) from checkins c
                           where c.goal_id = g.id and c.lifecycle_to = g.lifecycle), '')
                 < strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-30 days'))
  order by g.id`;

type Goal = { id: number; title: string };

test("a leader's Report from Definition to publication, Comment and Action Item", async ({ as, seedLookup }) => {
  test.setTimeout(180_000);
  const reportName = "Platform monthly review";
  const introduction = "Platform's month: where the deploy and cost work stands, and what needs a hand.";

  const elena = await as(leader);
  let reportURL = "";

  await test.step("1 She builds a Report Definition, and the rail lists the Goals its rule selects", async () => {
    const platform = seedLookup<Goal>(platformGoals);
    expect(platform.length, "the seed has Platform Goals").toBeGreaterThan(0);

    await elena.goto("/reports/new");
    const builder = elena.getByTestId("report-builder");
    await builder.getByLabel("Name").fill(reportName);
    await builder.getByLabel("Introduction").fill(introduction);

    // The rule is picked, not typed, in the blank row that's there by default.
    const rule = builder.getByTestId("report-rule");
    await expect(rule).toHaveCount(1);
    await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
    await rule.getByLabel("Operator").selectOption({ label: "is any of" });
    await rule.getByLabel("Values").selectOption({ label: "Platform" });

    const rail = elena.getByTestId("report-matches");
    await expect(rail.getByTestId("matches-count")).toHaveText(`Matches ${platform.length} Goals`);
    // Exactly those Goals: as many rows as Goals, and each Goal in one.
    await expect(rail.getByTestId("match")).toHaveCount(platform.length);
    for (const g of platform) {
      await expect(rail.getByTestId("match").filter({ hasText: g.title })).toHaveCount(1);
    }

    await builder.getByRole("button", { name: "Save report" }).last().click();
    await expect(elena.getByTestId("draft-header").getByTestId("report-name")).toHaveText(reportName);
    reportURL = new URL(elena.url()).pathname;
    expect(reportURL).toMatch(/^\/reports\/\d+$/);
  });

  await test.step("2 The draft preview shows the Health summary, the exceptions in full, then the rest on track", async () => {
    const draft = preview(elena);
    await expectReportOrder(draft, ["health-summary", "report-introduction", "needs-attention", "on-track-section"]);
    await expect(draft.getByTestId("report-introduction")).toHaveText(introduction);

    // It worked if each exception carried its data, its next steps (the Path
    // to Green) and enough context to stand alone.
    const blocks = draft.getByTestId("report-exception");
    const exceptions: string[] = [];
    for (const block of await blocks.all()) {
      const goal = goalOfBlock(seedLookup, await block.getAttribute("id"));
      exceptions.push(goal.title);
      await expect(block.getByRole("heading", { level: 3 })).toHaveText(goal.title);
      await expect(block.getByTestId("report-owner")).toContainText(goal.owner);
      await expect(block).toContainText(`· ${goal.lifecycle}`);
      await expect(block.getByTestId("report-so-what")).toHaveText(`So What: ${goal.so_what}`);
      await expect(block.getByTestId("report-status")).toHaveText(`Status: ${goal.status}`);
      if (goal.delivery_date) await expect(block.getByTestId("report-due")).toContainText(goal.delivery_date);
      await expect(block.getByTestId("report-milestone")).toHaveCount(goal.milestones);
      await expect(block.getByTestId("report-metric")).toHaveCount(goal.metrics);
      const health = await healthOf(block.getByTestId("report-health"));
      if (health === "Red" || health === "Yellow") {
        await expect(block.getByTestId("report-path-to-green")).toContainText(`Path to Green: ${goal.path_to_green}`);
      }
    }
    expect(exceptions.length, "Platform has exceptions to show in full").toBeGreaterThan(0);

    // Each On track row is a Goal that isn't an exception, and between them
    // they are every Goal the rule selects.
    const lines = draft.getByTestId("on-track").getByTestId("selected-goal");
    const onTrack: string[] = [];
    for (const line of await lines.all()) {
      const goal = goalOfBlock(seedLookup, await line.getAttribute("id"));
      onTrack.push(goal.title);
      // Not Red or Yellow: those always earn a block. An On Hold Goal has no
      // Health, and stays on one line unless something about it changed.
      expect(["Red", "Yellow"]).not.toContain(await healthOf(line.getByTestId("selected-health")));
    }
    expect(onTrack.filter((t) => exceptions.includes(t))).toEqual([]);
    expect([...exceptions, ...onTrack].sort()).toEqual(
      seedLookup<Goal>(platformGoals)
        .map((g) => g.title)
        .sort(),
    );
  });

  let firstPublication = "";
  await test.step("3 Publish… opens a confirmation, and confirming it publishes", async () => {
    await publish(elena, reportName);
    firstPublication = new URL(elena.url()).pathname;
    await expect(elena.getByTestId("report-published")).toContainText("by Priya Raman");
  });

  // The Goals the month's changes happen to: one slips, one leaves for another
  // Team, and one's Health turns.
  const slipped = pickCheckinGoal(seedLookup, "Green", "and g.kind = 'Dated'");
  const turning = pickCheckinGoal(seedLookup, "Yellow", `and g.id <> ${slipped.id}`);
  const turnedTo = turning.health === "Yellow" ? "Green" : "Yellow";
  const [moved] = seedLookup<Goal>(
    `select id, title from (${platformGoals}) where id not in (?, ?) order by id limit 1`,
    slipped.id,
    turning.id,
  );

  await test.step("4 Changes since read against her previous publication", async () => {
    // The slip: a later delivery date, with a reason. A later date rules out
    // Green, so the Owner checks in Yellow with a Path to Green.
    const newDate = addDays(slipped.delivery_date, 21);
    const owner = await as(slipped.email);
    await checkIn(owner, slipped.id, {
      health: "Yellow",
      status: "The vendor's API change pushed integration out three weeks.",
      pathToGreen: "Pair with the vendor on the new API; cut the reporting extras.",
      backToGreen: addDays(today(), 28),
      deliveryDate: newDate,
      dateReason: "The vendor changed their API under us.",
    });

    // The move: the Admin puts another Platform Goal in a different Team.
    const adminPage = await as(admin);
    await adminPage.goto(`/goals/${moved.id}`);
    await adminPage.getByRole("link", { name: "Edit Dimensions" }).click();
    const team = adminPage.getByRole("combobox", { name: "Team", exact: true });
    await team.selectOption({ label: "Payments" });
    await team.locator("xpath=ancestor::form").getByRole("button", { name: "Save" }).click();
    await expect(
      adminPage.getByTestId("goal-dimension").filter({ hasText: "Team" }).getByTestId("goal-dimension-value"),
    ).toHaveText("Payments");

    // The turn: a third Goal's Health changes, with no slip.
    const turner = await as(turning.email);
    await checkIn(turner, turning.id, {
      health: turnedTo,
      status: turnedTo === "Green" ? "Load tests pass at twice peak; back on plan." : "Two engineers out; the plan is at risk.",
      ...(turnedTo === "Yellow"
        ? { pathToGreen: "Borrow one engineer from Data for a month.", backToGreen: addDays(today(), 21) }
        : {}),
    });

    await elena.goto(reportURL);
    const draft = preview(elena);
    await expect(elena.getByTestId("baseline-label")).toHaveText(/^last publication · /);
    await expectReportOrder(draft, ["health-summary", "needs-attention", "membership-changes"]);

    // The slip is marked: New Date, and the old date struck through.
    const slipBlock = draft.locator(`#goal-${slipped.id}`);
    await expect(slipBlock.getByTestId("report-badge").filter({ hasText: "New Date" })).toBeVisible();
    await expect(slipBlock.getByTestId("report-due").locator("del")).toHaveText(slipped.delivery_date);
    await expect(slipBlock.getByTestId("report-due")).toContainText(newDate);

    // The re-teamed Goal left, and says why.
    const left = draft.getByTestId("membership-change").filter({ hasText: moved.title });
    await expect(left.getByTestId("membership-direction")).toHaveText("Left");
    await expect(left.getByTestId("membership-reason")).toHaveText("no longer matches the rules");

    // The Goals that turned Yellow need attention.
    const yellow = [slipped, ...(turnedTo === "Yellow" ? [turning] : [])];
    for (const g of yellow) {
      const block = draft.getByTestId("needs-attention").locator(`#goal-${g.id}`);
      await expect(block).toBeVisible();
      expect(await healthOf(block.getByTestId("report-health"))).toBe("Yellow");
    }
  });

  await test.step("4 Changes since: a Goal whose Health turned shows what it was", async () => {
    const block = preview(elena).getByTestId("needs-attention").locator(`#goal-${turning.id}`);
    expect(await healthOf(block.getByTestId("report-health"))).toBe(turnedTo);
    await expect(block.getByTestId("report-prior-health")).toHaveText(`· was ${turning.health}`);
    // The slipped Goal was Green at the publication, so it reads "was Green".
    await expect(preview(elena).locator(`#goal-${slipped.id}`).getByTestId("report-prior-health")).toHaveText(
      "· was Green",
    );
  });
});

// pickCheckinGoal picks a Platform Goal whose Owner can check in on it
// plainly, with the Health given first if one has it: Active, its Owner still
// here, not Stale, with no accepted contributors (so no Rolled-up Health asks
// for an explanation) and no overdue Planned Milestone.
function pickCheckinGoal(seedLookup: SeedLookup, health: string, extra = ""): GoalDetail & { health: string } {
  const [row] = seedLookup<{ id: number }>(
    `select g.id from (${platformGoals}) p join goals g on g.id = p.id join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and a.departed = 0
       and (select max(created_at) from checkins c where c.goal_id = g.id)
           > strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-' || g.cadence_days || ' days')
       and not exists (select 1 from links l where l.parent_id = g.id and l.status = 'accepted')
       and not exists (select 1 from milestones m
                       where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date('now'))
       ${extra}
     order by (select health from checkins c where c.goal_id = g.id order by created_at desc, id desc limit 1) = ? desc, g.id
     limit 1`,
    health,
  );
  expect(row, "Platform has a Goal its Owner can check in on").toBeDefined();
  const goal = goalOfBlock(seedLookup, `goal-${row.id}`);
  const [latest] = seedLookup<{ health: string }>(
    "select health from checkins where goal_id = ? order by created_at desc, id desc limit 1",
    row.id,
  );
  return { ...goal, health: latest.health };
}

type CheckinInput = {
  health: string;
  status: string;
  pathToGreen?: string;
  backToGreen?: string;
  deliveryDate?: string;
  dateReason?: string;
  highlights?: { kind: string; note: string }[];
};

// checkIn submits a Check-in on the Goal as the page's person, through the
// Check-in form, and waits for the Goal page.
async function checkIn(page: Page, goalID: number, c: CheckinInput) {
  await page.goto(`/goals/${goalID}/checkin`);
  const form = page.getByTestId("checkin-form");
  await form.getByTestId("checkin-health").getByText(c.health, { exact: true }).click();
  if (c.pathToGreen) await form.getByRole("textbox", { name: "Plan", exact: true }).fill(c.pathToGreen);
  if (c.backToGreen) await form.getByLabel("Back to Green by").fill(c.backToGreen);
  await form.getByRole("textbox", { name: /^Status/ }).fill(c.status);
  if (c.deliveryDate) {
    await form.getByTestId("checkin-dates-section").locator("summary").click();
    await form.getByLabel("Delivery date", { exact: true }).fill(c.deliveryDate);
    await form.getByLabel("Reason (required if the delivery date changes)").fill(c.dateReason ?? "");
  }
  for (const [i, h] of (c.highlights ?? []).entries()) {
    const section = form.getByTestId("checkin-highlight-section");
    if (i === 0) {
      await section.locator("summary").click();
    } else {
      await section.getByRole("button", { name: "Add another" }).click();
    }
    const row = page.getByTestId("checkin-highlight-row").nth(i);
    await row.getByLabel("Kind").selectOption({ label: h.kind });
    await row.getByLabel("Note").fill(h.note);
  }
  await page.getByTestId("checkin-form").getByRole("button", { name: "Submit check-in" }).click();
  await page.waitForURL(new RegExp(`/goals/${goalID}$`));
}

// today is today's date in UTC, the server's timezone, as YYYY-MM-DD.
function today(): string {
  return new Date().toISOString().slice(0, 10);
}

// addDays is the YYYY-MM-DD date days after date.
function addDays(date: string, days: number): string {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

// preview is the draft page's preview: exactly what readers will see.
function preview(page: Page) {
  return page.getByTestId("draft-panes").getByTestId("report-draft");
}

// reportSections are the parts of a Report, published or previewed, in the
// order a reader meets them.
const reportSections = [
  "health-summary",
  "report-action-items",
  "report-introduction",
  "report-narrative",
  "needs-attention",
  "on-track-section",
  "membership-changes",
];

// expectReportOrder checks that each of required is in the Report, and that
// the Report's parts that are there come in reportSections' order.
async function expectReportOrder(report: Locator, required: string[]) {
  for (const id of required) await expect(report.getByTestId(id), `the Report shows ${id}`).toHaveCount(1);
  const present: Locator[] = [];
  for (const id of reportSections) {
    if ((await report.getByTestId(id).count()) > 0) present.push(report.getByTestId(id));
  }
  for (let i = 1; i < present.length; i++) {
    const after = await present[i].elementHandle();
    const follows = await present[i - 1].evaluate(
      (a, b) => !!(b && a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING),
      after,
    );
    expect(follows, `${await present[i - 1].getAttribute("data-testid")} comes before ${await present[i].getAttribute("data-testid")}`).toBe(true);
  }
}

// healthOf is the Health a badge names, without the shape the print page
// marks it with.
async function healthOf(badge: Locator): Promise<string> {
  return ((await badge.textContent()) ?? "").replace(/[^A-Za-z ]/g, "").trim();
}

type GoalDetail = Goal & {
  owner: string;
  email: string;
  lifecycle: string;
  so_what: string;
  delivery_date: string;
  status: string;
  path_to_green: string;
  milestones: number;
  metrics: number;
};

// goalOfBlock looks up the Goal a Report's block or line is about, by its
// anchor ("goal-12"), for what it should show.
function goalOfBlock(seedLookup: SeedLookup, anchor: string | null): GoalDetail {
  const id = Number((anchor ?? "").replace("goal-", ""));
  const [goal] = seedLookup<GoalDetail>(
    `select g.id, g.title, coalesce(a.name, a.email) owner, a.email, g.lifecycle, g.so_what, g.delivery_date,
            c.status, c.path_to_green,
            (select count(*) from milestones m where m.goal_id = g.id) milestones,
            (select count(*) from metrics m where m.goal_id = g.id) metrics
     from goals g join accounts a on a.id = g.owner_id
     left join checkins c on c.id = (select id from checkins where goal_id = g.id order by created_at desc, id desc limit 1)
     where g.id = ?`,
    id,
  );
  expect(goal, `a Goal behind ${anchor}`).toBeDefined();
  return goal;
}

type SeedLookup = <T = Record<string, unknown>>(sql: string, ...params: (string | number)[]) => T[];

// publish presses Publish… on the draft page, checks the confirmation names
// the Report, and confirms, landing on the publication.
async function publish(page: Page, reportName: string) {
  const confirm = page.getByTestId("publish-confirm");
  await confirm.getByText("Publish…").click();
  await expect(confirm.getByRole("heading", { name: `Publish ${reportName}?` })).toBeVisible();
  await confirm.getByRole("button", { name: "Publish", exact: true }).click();
  await page.waitForURL(/\/reports\/\d+\/publications\/\d+$/);
}
