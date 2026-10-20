// Scenario 1, Finding trouble between reviews (docs/scenarios.md): a leader
// learns what's in trouble, and why, from Goals and Follow-ups without anyone
// telling her, and every problem sits beside the one action that moves it.
// Goals is where Health shows; Follow-ups is where the reasons show, a Red or
// Yellow Goal under Not Green beside its other reasons and its Fix.
//
// The leader ("Elena" in the scenario) is cto@example.com, whom the seed names
// Priya Raman. The setup makes what the seed lacks: Ownerless Goals, an overdue
// Path to Green, a second Dimension, and an overdue Action Item.
import type { Locator, Page } from "@playwright/test";

import { admin, appToday, expect, signIn, test } from "../fixtures";

const leader = "cto@example.com";
const leaderName = "Priya Raman";

// The second Dimension the Admin defines, and its values.
const tier = { name: "Tier", values: ["Core", "Edge"] };

// isCheckinOverdue is SQL true for a Goal g whose last Check-in, or activation
// if it has none, is older than its cadence: the seed's Check-in overdue Goals
// are weeks past it, so the day boundary doesn't matter.
const isCheckinOverdue = `
  julianday($now) - julianday(coalesce(
    (select max(c.created_at) from checkins c where c.goal_id = g.id),
    nullif(g.activated_at, ''), g.created_at)) > g.cadence_days`;

// latestHealth is SQL for Goal g's latest Check-in's Health.
const latestHealth = `
  (select c.health from checkins c where c.goal_id = g.id order by c.created_at desc, c.id desc limit 1)`;

// onTeam is SQL true for a Goal g carrying the Team value ?.
const onTeam = `
  exists (select 1 from goal_dimension_values gdv
          join dimension_values v on v.id = gdv.dimension_value_id
          join dimensions d on d.id = v.dimension_id
          where gdv.goal_id = g.id and d.name = 'Team' and v.value = ?)`;

type Goal = { id: number; title: string; email: string };

test("a leader finds trouble between reviews on Goals and Follow-ups", async ({ page, as, seedLookup, serverLog }) => {
  test.setTimeout(180_000);

  // The actors and Goals, chosen by criteria. A Check-in overdue Platform Goal
  // the leader doesn't own, to nudge in step 5.
  const [checkinOverdue] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and a.email <> ? and a.departed = 0 and ${onTeam} and ${isCheckinOverdue}
     order by g.id limit 1`,
    leader,
    "Platform",
  );
  expect(checkinOverdue, "the seed has a Check-in overdue Platform Goal the leader doesn't own").toBeDefined();

  // An Owner to depart: owns an Active Platform Goal, owns no Check-in overdue
  // Goal (so not the one nudged) and no Top-level Goal, and is neither the
  // leader nor the Admin. The Check-in overdue subquery names its Goal g, as
  // isCheckinOverdue reads it.
  const [departing] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${onTeam} and a.email not in (?, ?) and a.is_admin = 0
       and not exists (select 1 from goals g2 where g2.owner_id = a.id and g2.top_level = 1)
       and not exists (select 1 from goals g where g.owner_id = a.id and g.lifecycle = 'Active' and ${isCheckinOverdue})
     order by g.id limit 1`,
    "Platform",
    leader,
    admin,
  );
  expect(departing, "the seed has a Platform Owner to depart").toBeDefined();

  // A Goal to put past its Path to Green: an Active Platform Goal whose Owner
  // stays, with no Active child Goals (so no Rolled-up Health asking for an
  // explanation), not Check-in overdue, and with no overdue Planned Milestone,
  // so the Check-in is only Health, status and Path to Green.
  const [overdue] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${onTeam} and a.email not in (?, ?) and a.departed = 0
       and g.id <> ? and not (${isCheckinOverdue})
       and not exists (select 1 from links l join goals c on c.id = l.child_id
                       where l.parent_id = g.id and l.status = 'accepted' and c.lifecycle = 'Active')
       and not exists (select 1 from milestones m
                       where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
     order by g.id limit 1`,
    "Platform",
    leader,
    departing.email,
    checkinOverdue.id,
  );
  expect(overdue, "the seed has a Platform Goal to put past its Path to Green").toBeDefined();

  // Two Active Platform Goals to carry the second Dimension's values.
  const tiered = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${onTeam} order by g.id limit 2`,
    "Platform",
  );
  expect(tiered, "the seed has two Active Platform Goals").toHaveLength(2);

  // An Unlinked Goal: Active, not Top-level, contributing to no Goal.
  const [unlinked] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and g.top_level = 0 and a.email <> ?
       and not exists (select 1 from links l where l.child_id = g.id and l.status = 'accepted')
     order by g.id limit 1`,
    departing.email,
  );
  expect(unlinked, "the seed has an Unlinked Goal").toBeDefined();

  // A Goal with two signals: Check-in overdue, and due later than a Goal it
  // contributes to (a Schedule conflict), with its Team.
  const [twoReasons] = seedLookup<Goal & { team: string }>(
    `select g.id, g.title, a.email, v.value as team from goals g
     join accounts a on a.id = g.owner_id
     join goal_dimension_values gdv on gdv.goal_id = g.id
     join dimension_values v on v.id = gdv.dimension_value_id
     join dimensions d on d.id = v.dimension_id and d.name = 'Team'
     where g.lifecycle = 'Active' and a.email <> ? and ${isCheckinOverdue} and g.delivery_date <> ''
       and exists (select 1 from links l join goals p on p.id = l.parent_id
                   where l.child_id = g.id and l.status = 'accepted'
                     and p.delivery_date <> '' and g.delivery_date > p.delivery_date)
     order by g.id limit 1`,
    departing.email,
  );
  expect(twoReasons, "the seed has a Check-in overdue Goal with a Schedule conflict").toBeDefined();

  // The departed Owner's Active Goals, which become Ownerless on Follow-ups.
  const ownerless = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where a.email = ? and g.lifecycle = 'Active' order by g.id`,
    departing.email,
  );

  // Two Red Goals to open: one with Active child Goals contributing to it
  // (Rolled-up Health), one whose delivery date has slipped. Seed 23 has no Red
  // Goal with both. The first is otherwise fine: not Check-in overdue,
  // contributing to a Goal, with an Owner who stays, so only its Health, under
  // Not Green, puts it on Follow-ups.
  const [redWithChildren] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${latestHealth} = 'Red' and not (${isCheckinOverdue}) and a.email <> ?
       and exists (select 1 from links l where l.child_id = g.id and l.status = 'accepted')
       and exists (select 1 from links l join goals c on c.id = l.child_id
                   where l.parent_id = g.id and l.status = 'accepted' and c.lifecycle = 'Active')
     order by g.id limit 1`,
    departing.email,
  );
  expect(redWithChildren, "the seed has a Red Goal with Active child Goals").toBeDefined();
  const [redSlipped] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${latestHealth} = 'Red' and g.delivery_date <> ''
       and exists (select 1 from date_slips s where s.goal_id = g.id and s.milestone_id is null and s.back_to_green = 0)
     order by g.id limit 1`,
  );
  expect(redSlipped, "the seed has a Red Goal with a Date Slip").toBeDefined();

  // Each Goal's Lifecycle, which Follow-ups' order reads: Active Goals first.
  const lifecycles = new Map(
    seedLookup<{ id: number; lifecycle: string }>("select id, lifecycle from goals").map((g) => [g.id, g.lifecycle]),
  );

  const adminPage = await as(admin);

  await test.step("Setup 1: the Admin departs an Owner, so their Goals are Ownerless", async () => {
    await adminPage.goto(`/goals/${departing.id}`);
    await adminPage.getByTestId("goal-more").getByTitle("More actions").click();
    await adminPage.getByRole("link", { name: "Mark owner departed…" }).click();
    adminPage.once("dialog", (d) => d.accept());
    await adminPage.getByTestId("depart-owner").getByRole("button").click();
    await expect(adminPage.getByTestId("goal-ownerless")).toBeVisible();
  });

  await test.step("Setup 2: an Owner checks in Yellow with a Path to Green two days overdue", async () => {
    const owner = await as(overdue.email);
    await owner.goto(`/goals/${overdue.id}/checkin`);
    const form = owner.getByTestId("checkin-form");
    await form.getByRole("radio", { name: "Yellow" }).check();
    await form
      .getByRole("textbox", { name: "Plan", exact: true })
      .fill("Move the last two services over before the freeze.");
    await form.getByLabel("Back to Green by", { exact: true }).fill(isoDate(-2));
    // A date later than the last Check-in's is a Date Slip, so the form asks
    // why.
    const reason = form.getByLabel("Reason for moving it later");
    if (await reason.isVisible()) await reason.fill("The cut-over needs another week.");
    await form
      .getByRole("textbox", { name: /^What changed since the last check-in/ })
      .fill("The cut-over slipped past the date we gave.");
    await form.getByRole("button", { name: "Submit check-in" }).click();
    await owner.waitForURL(`**/goals/${overdue.id}`);
    await expect(owner.getByTestId("goal-path-to-green")).toContainText("Move the last two services over");
  });

  await test.step("Setup 3: the Admin defines a second Dimension and gives each value to a Platform Goal", async () => {
    await adminPage.goto("/dimensions");
    const create = adminPage.getByTestId("create-dimension");
    await create.getByText("Define a Dimension").first().click();
    await create.getByLabel("Name").fill(tier.name);
    await create.getByLabel(/^Values/).fill(tier.values.join(", "));
    await create.getByRole("button", { name: "Create Dimension" }).click();
    await expect(adminPage.getByTestId("dimension").filter({ hasText: tier.name })).toBeVisible();

    for (const [i, goal] of tiered.entries()) {
      await adminPage.goto(`/goals/${goal.id}`);
      await adminPage.getByRole("link", { name: "Edit Dimensions" }).click();
      const select = adminPage.getByLabel(tier.name, { exact: true });
      await select.selectOption({ label: tier.values[i] });
      await adminPage.locator("form").filter({ has: select }).getByRole("button", { name: "Save" }).click();
      const value = adminPage.getByTestId("goal-dimension").filter({ hasText: tier.name });
      await expect(value.getByTestId("goal-dimension-value")).toHaveText(tier.values[i]);
    }
  });

  await test.step("Setup 4: the leader publishes a Report and raises an Action Item about the Check-in overdue Goal, due two days ago", async () => {
    const leaderPage = await as(leader);
    const reportName = "Platform follow-ups";
    await leaderPage.goto("/reports/new");
    const builder = leaderPage.getByTestId("report-builder");
    await builder.getByLabel("Name").fill(reportName);
    const rule = builder.getByTestId("report-rule");
    await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
    await rule.getByLabel("Operator").selectOption({ label: "is any of" });
    await rule.getByLabel("Values").selectOption({ label: "Platform" });
    await builder.getByRole("button", { name: "Save report" }).click();
    await expect(leaderPage.getByTestId("draft-header").getByTestId("report-name")).toHaveText(reportName);

    const confirm = leaderPage.getByTestId("publish-confirm");
    await confirm.getByText("Publish…").click();
    await confirm.getByRole("button", { name: "Publish", exact: true }).click();
    await leaderPage.waitForURL(/\/reports\/\d+\/publications\/\d+$/);

    const items = leaderPage.getByTestId("publication-action-items");
    await items.locator("summary").click();
    const form = items.getByTestId("action-item-form");
    await form.getByLabel("Action Item").fill("Agree a date for the overdue cut-over.");
    await form.getByLabel("Owner").fill(checkinOverdue.email);
    await form.getByLabel("Due").fill(isoDate(-2));
    await form.getByTestId("action-item-goal").selectOption(String(checkinOverdue.id));
    await form.getByRole("button", { name: "Add Action Item" }).click();
    await expect(items.getByTestId("raised-action-item")).toContainText(`about ${checkinOverdue.title}`);
  });

  await test.step("1. The top bar shows a count on Follow-ups before she clicks anything", async () => {
    await signIn(page, leader);
    const count = await navFollowUpsCount(page);
    expect(count, "Follow-ups shows a count from Home").toBeGreaterThan(0);
    // The count is her default scope, My org, as many as a bare /follow-ups lists.
    await page.goto("/follow-ups");
    await expect(page.getByTestId("follow-ups-attention")).toHaveText(
      new RegExp(`^${count} Goals to review in My org, most urgent first`),
    );
    expect(await followUpRows(page), "the count is a bare /follow-ups's rows").toHaveLength(count);
  });

  await test.step("2. Goals is led by Health, problems first, filtered to her org and grouped by another Dimension", async () => {
    await page.goto("/goals?org=all");
    const rows = await goalRows(page);
    expect(rows.length, "the Goal list lists the seed's Goals").toBeGreaterThan(10);

    // Problem order (problemRank): every Red row comes first, and no unmarked
    // Green row comes before a Yellow one.
    const firstNotRed = rows.findIndex((r) => r.health !== "Red");
    expect(
      rows.slice(firstNotRed).filter((r) => r.health === "Red"),
      "every Red row comes first",
    ).toEqual([]);
    const lastYellow = rows.findLastIndex((r) => r.health === "Yellow");
    const unmarkedGreen = rows.findIndex((r) => r.health === "Green" && !marked(r));
    expect(unmarkedGreen, "no unmarked Green row comes before a Yellow one").toBeGreaterThan(lastYellow);

    // Marks: the Check-in overdue Goal carries its Check-in overdue mark, the
    // departed Owner's Goal its ownerless mark.
    expect(
      rows.find((r) => r.title === checkinOverdue.title)?.checkinOverdue,
      `${checkinOverdue.title} is marked Check-in overdue`,
    ).toBe(true);
    expect(rows.find((r) => r.title === departing.title)?.ownerless, `${departing.title} is marked Ownerless`).toBe(
      true,
    );
    await expect(goalRow(page, checkinOverdue.title).getByTestId("checkin-overdue")).toBeVisible();
    await expect(goalRow(page, departing.title).getByTestId("ownerless")).toBeVisible();

    // Filter: Team = Platform, under More filters. Every row is a Platform Goal.
    await page.getByTestId("more-filters").getByText("More filters").click();
    const more = page.getByTestId("more-filters-panel");
    await more.getByRole("group", { name: "Team" }).getByLabel("Platform").check();
    await expect(page).toHaveURL(/[?&]value=\d+/);
    const platform = page.getByTestId("goal-row");
    await expect(platform.first()).toBeVisible();
    await expect(page.getByTestId("goal-count")).not.toHaveText(`${rows.length} Goals`);
    for (const row of await platform.all()) {
      await expect(row.getByTestId("goal-value-tag").filter({ hasText: /^Platform$/ })).toHaveCount(1);
    }

    // Group: by the second Dimension, one group per value a listed Goal
    // carries, plus Unassigned.
    await more.getByLabel("Group by").selectOption({ label: tier.name });
    await expect(page).toHaveURL(/[?&]group=\d+/);
    await expect(page.getByTestId("goal-group-label")).toHaveText([...tier.values, "Unassigned"]);
    for (const [i, goal] of tiered.entries()) {
      const label = page.getByTestId("goal-group-label").getByText(tier.values[i], { exact: true });
      const group = page.getByTestId("goal-group").filter({ has: label });
      await expect(goalRow(group, goal.title)).toBeVisible();
    }
  });

  await test.step("3. Follow-ups says how many Goals there are to review, in five groups by who has to act", async () => {
    await page.goto("/follow-ups?org=all");
    await expect(page.getByTestId("follow-ups-attention")).toContainText("Goals to review, most urgent first");
    const cards = [
      { key: "health", name: "Not Green", kinds: ["red", "yellow"] },
      { key: "owner", name: "Waiting on a Check-in", kinds: ["checkin-overdue", "path-overdue", "milestone-overdue"] },
      { key: "followup", name: "Action Items overdue", kinds: ["action-item-overdue"] },
      { key: "plan", name: "Placement and dates", kinds: ["unlinked", "schedule-conflicts"] },
      { key: "admin", name: "Needs an Admin", kinds: ["ownerless"] },
    ];
    for (const c of cards) {
      const card = page.getByTestId(`follow-ups-group-${c.key}`);
      await expect(card).toContainText(c.name);
      await expect(card.getByText(/^\d+$/)).toBeVisible();
      // A chip per signal that flags a Goal; the setup makes every one of
      // these flag at least one.
      for (const kind of c.kinds) {
        await expect(card.locator(`[data-kind="${kind}"]`), `${c.name} has a ${kind} chip`).toBeVisible();
      }
    }

    // Following a card narrows the one flat table to that group: each row
    // carries a chip of the group's signals, and the card counts the rows. Not
    // Green's rows take no chip: their Health badge says Red or Yellow.
    const expected: Record<string, { goal: Goal; kind: string }[]> = {
      health: [{ goal: redWithChildren, kind: "red" }],
      owner: [
        { goal: checkinOverdue, kind: "checkin-overdue" },
        { goal: overdue, kind: "path-overdue" },
      ],
      followup: [{ goal: checkinOverdue, kind: "action-item-overdue" }],
      plan: [
        { goal: unlinked, kind: "unlinked" },
        { goal: twoReasons, kind: "schedule-conflicts" },
      ],
      admin: ownerless.map((goal) => ({ goal, kind: "ownerless" })),
    };
    // Every card keeps Everyone (keys sorted), Needs an Admin included.
    for (const c of cards) {
      await page.goto("/follow-ups?org=all");
      const card = page.getByTestId(`follow-ups-group-${c.key}`);
      const count = Number(await card.getByText(/^\d+$/).textContent());
      await card.click();
      await expect(page).toHaveURL(new RegExp(`/follow-ups\\?group=${c.key}&org=all$`));
      const rows = await followUpRows(page);
      expect(rows, `the ${c.name} card counts its rows`).toHaveLength(count);
      const health = c.key === "health";
      for (const r of rows) {
        if (health) {
          expect(["Red", "Yellow"], `${r.title}'s Health is Red or Yellow`).toContain(r.health);
        } else {
          expect(r.kinds.length, `${r.title} has a chip`).toBeGreaterThan(0);
        }
        expect(c.kinds, `${r.title}'s chips are ${c.name}'s`).toEqual(expect.arrayContaining(r.kinds));
      }
      for (const { goal, kind } of expected[c.key]) {
        const row = rows.find((r) => r.id === goal.id);
        if (health) {
          expect(row, `${goal.title} is under ${c.name}`).toBeDefined();
        } else {
          expect(row?.kinds, `${goal.title} is under ${c.name} with a ${kind} chip`).toContain(kind);
        }
      }
      // The Check-in overdue chip on the card counts the Check-in overdue rows;
      // Not Green's Red and Yellow chips count the rows of that Health.
      for (const kind of c.kinds) {
        const n = health
          ? rows.filter((r) => r.health === (kind === "red" ? "Red" : "Yellow")).length
          : rows.filter((r) => r.kinds.includes(kind)).length;
        await expect(card.locator(`[data-kind="${kind}"]`)).toHaveText(new RegExp(` · ${n}$`));
      }
    }
  });

  await test.step("4. Narrowed to her org, each flagged Goal is one row, most urgent first, with a chip per reason and one Fix", async () => {
    // A bare /follow-ups is her default, My org, which the top bar counts.
    await page.goto("/follow-ups");
    const inOrg = await navFollowUpsCount(page);
    expect(await followUpRows(page), "the top bar counts a bare /follow-ups's rows").toHaveLength(inOrg);
    const teamSelect = page.locator('[data-testid="follow-ups-value"][data-dimension="Team"]');
    await expect(teamSelect.locator("option").first()).toHaveText("Team: any");
    await teamSelect.selectOption({ label: "Platform" });
    await expect(page).toHaveURL(/[?&]value=\d+/);
    const rows = await followUpRows(page);
    await expect(page.getByTestId("follow-ups-attention")).toHaveText(new RegExp(`^${rows.length} Goals? to review`));
    // The top bar counts her whole org; the narrowed page doesn't.
    expect(rows.length, "Platform has fewer flagged Goals than her org").toBeLessThan(inOrg);
    expect(await navFollowUpsCount(page), "the top bar's count doesn't narrow").toBe(inOrg);

    expect(new Set(rows.map((r) => r.id)).size, "one row per Goal").toBe(rows.length);
    for (const r of rows) {
      expect(["Red", "Yellow", "Green", "—"], `${r.title}'s Health`).toContain(r.health);
      // A Red or Yellow row's Health is its reason; any other has a chip.
      expect(
        r.kinds.length > 0 || ["Red", "Yellow"].includes(r.health),
        `${r.title} has a chip per reason, or is Red or Yellow`,
      ).toBe(true);
      expect(r.fixCells, `${r.title} has one Fix`).toBe(1);
      expect(r.fixes, `${r.title}'s Fix is one action: ${r.fix}`).toBe(1);
    }
    // Most urgent first (sortFollowUpRows): Active Goals before Proposed and On
    // Hold ones, then Ownerless, then by Health (Red, Yellow, none, Green),
    // then Path to Green overdue, then Milestone overdue, then an overdue
    // Action Item, then Check-in overdue.
    const keys = rows.map((r) => [
      lifecycles.get(r.id) === "Active" ? 0 : 1,
      r.kinds.includes("ownerless") ? 0 : 1,
      followUpHealthRank(r.health),
      r.kinds.includes("path-overdue") ? 0 : 1,
      r.kinds.includes("milestone-overdue") ? 0 : 1,
      r.kinds.includes("action-item-overdue") ? 0 : 1,
      r.kinds.includes("checkin-overdue") ? 0 : 1,
    ]);
    for (let i = 1; i < keys.length; i++) {
      expect(
        compareKeys(keys[i - 1], keys[i]),
        `${rows[i - 1].title} is no better off than ${rows[i].title}`,
      ).toBeLessThanOrEqual(0);
    }
    for (const { goal, kind } of [
      { goal: departing, kind: "ownerless" },
      { goal: overdue, kind: "path-overdue" },
      { goal: checkinOverdue, kind: "checkin-overdue" },
    ]) {
      expect(
        rows.find((r) => r.id === goal.id)?.kinds,
        `${goal.title} is a Platform row with a ${kind} chip`,
      ).toContain(kind);
    }

    // A Goal with two reasons is one row with two chips and one Fix.
    await teamSelect.selectOption({ label: twoReasons.team });
    await expect(teamSelect.locator("option:checked")).toHaveText(twoReasons.team);
    await expect(
      page.getByTestId("follow-ups-table").getByRole("link", { name: twoReasons.title, exact: true }),
    ).toBeVisible();
    const team = await followUpRows(page);
    const two = team.filter((r) => r.id === twoReasons.id);
    expect(two, `${twoReasons.title} is one row`).toHaveLength(1);
    expect(two[0].kinds.toSorted(), "with a chip for each reason").toEqual(["schedule-conflicts", "checkin-overdue"]);
    expect(two[0].fixes, "and one Fix").toBe(1);
  });

  await test.step("5. Nudge on a Check-in overdue Goal she doesn't own asks its Owner to check in, once that day", async () => {
    await page.goto("/follow-ups");
    const row = followUpRow(page, checkinOverdue.id);
    const fix = row.getByTestId("follow-up-fix");
    await fix.getByRole("button", { name: "Nudge" }).click();
    await expect(page).toHaveURL(/\/follow-ups$/);
    // No toast: the row's Fix now says who nudged it today, and can't be used.
    await expect(page.getByTestId("toast")).toHaveCount(0);
    await expect(fix.getByRole("button")).toHaveText(`Nudged today by ${leaderName}`);
    await expect(fix.getByRole("button")).toBeDisabled();

    // A second Nudge the same day is refused.
    const again = await page.request.post(`/goals/${checkinOverdue.id}/nudge`, {
      form: { return: "/follow-ups" },
      maxRedirects: 0,
    });
    expect(again.status(), "a second Nudge the same day is refused").toBe(422);
    expect(await again.text()).toMatch(/data-testid="nudge-refused">[^<]*already nudged this Goal today/);

    // The Goal's History shows the Nudge.
    await page.goto(`/goals/${checkinOverdue.id}`);
    const nudge = page
      .getByTestId("goal-history")
      .getByTestId("history-entry")
      .and(page.locator('[data-kind="nudge"]'));
    await expect(nudge).toHaveCount(1);
    await expect(nudge).toContainText(leaderName);
    await expect(nudge).toContainText("nudged for a Check-in");

    // The Owner and Delegates are asked only by email.
    expect(serverLog()).toContain(`Check-in requested: ${checkinOverdue.title}`);
  });

  await test.step("6. A Red Goal shows the Owner's status and Path to Green, Rolled-up Health, struck dates and History", async () => {
    for (const goal of [redWithChildren, redSlipped]) {
      await page.goto(`/goals/${goal.id}`);
      await expect(page.getByTestId("goal-title")).toHaveText(goal.title);
      await expect(page.getByTestId("goal-health")).toHaveText("Red");
      await expect(page.getByTestId("goal-status")).not.toBeEmpty();
      await expect(page.getByTestId("goal-path-to-green")).toContainText("Path to Green:");
      await expect(page.getByTestId("goal-history").getByTestId("history-entry").first()).toBeVisible();
    }

    // Rolled-up Health beside the Owner's Health, on the Red Goal with Active
    // child Goals contributing to it.
    await page.goto(`/goals/${redWithChildren.id}`);
    const cells = page.getByTestId("goal-status-cells");
    await expect(cells.getByTestId("goal-health")).toHaveText("Red");
    await expect(cells.getByTestId("goal-rollup-health")).toHaveText(/^(Red|Yellow|Green)$/);

    // A struck-through delivery date, on the Red Goal with a Date Slip.
    await page.goto(`/goals/${redSlipped.id}`);
    await expect(page.getByTestId("goal-delivery-date").locator("del").first()).toBeVisible();
  });

  await test.step("It worked if: a Goal nobody has updated is as loud as a Red one", async () => {
    // On Goals, the Check-in overdue row sorts above every unmarked Yellow and
    // Green row, and carries its own mark.
    await page.goto("/goals");
    const rows = await goalRows(page);
    const at = rows.findIndex((r) => r.title === checkinOverdue.title);
    expect(rows[at]?.checkinOverdue, `${checkinOverdue.title} carries its Check-in overdue mark`).toBe(true);
    const quieter = rows.findIndex((r) => (r.health === "Yellow" || r.health === "Green") && !marked(r));
    expect(at, `${checkinOverdue.title} sorts above every unmarked Yellow and Green row`).toBeLessThan(quieter);

    // On Follow-ups, it's a row in the main table, with its chip and a Fix, and
    // counted on the owner card.
    await page.goto("/follow-ups");
    const row = followUpRow(page, checkinOverdue.id);
    await expect(row.locator('[data-testid="follow-up-signal"][data-kind="checkin-overdue"]')).toBeVisible();
    await expect(row.getByTestId("follow-up-fix")).toHaveCount(1);
    await page.getByTestId("follow-ups-group-owner").click();
    expect(
      (await followUpRows(page)).map((r) => r.id),
      "the owner card counts it",
    ).toContain(checkinOverdue.id);
  });

  await test.step("It worked if: she could tell in trouble from silent from badly placed", async () => {
    // In trouble: Red or Yellow Health, on Goals, and under Not Green on
    // Follow-ups, a Red Goal nothing else is wrong with included.
    await page.goto("/goals?org=all");
    await expect(goalRow(page, redWithChildren.title).getByTestId("goal-row-health")).toHaveText("Red");
    await expect(goalRow(page, overdue.title).getByTestId("goal-row-health")).toHaveText("Yellow");
    await page.goto("/follow-ups?org=all");
    expect(
      (await followUpRows(page)).map((r) => r.id),
      `${redWithChildren.title} is on Follow-ups`,
    ).toContain(redWithChildren.id);
    await page.goto("/follow-ups?group=health&org=all");
    expect(
      (await followUpRows(page)).map((r) => r.id),
      `${redWithChildren.title} is under Not Green`,
    ).toContain(redWithChildren.id);

    // Silent: Check-in overdue, under Waiting on a Check-in, not Placement and
    // dates. Badly placed: Unlinked or a Schedule conflict, under Placement and
    // dates, not Waiting on a Check-in.
    await page.goto("/follow-ups?group=owner&org=all");
    const owner = (await followUpRows(page)).map((r) => r.id);
    await page.goto("/follow-ups?group=plan&org=all");
    const plan = (await followUpRows(page)).map((r) => r.id);
    expect(owner, "Check-in overdue is under Waiting on a Check-in").toContain(checkinOverdue.id);
    expect(plan, "Check-in overdue isn't under Placement and dates").not.toContain(checkinOverdue.id);
    expect(plan, "Unlinked is under Placement and dates").toContain(unlinked.id);
    expect(owner, "Unlinked isn't under Waiting on a Check-in").not.toContain(unlinked.id);
    expect(plan, "a Schedule conflict is under Placement and dates").toContain(twoReasons.id);
    expect(owner, "a Red Goal that's up to date isn't under Waiting on a Check-in").not.toContain(redWithChildren.id);
    expect(plan, "a Red Goal that's well placed isn't under Placement and dates").not.toContain(redWithChildren.id);
  });

  await test.step("It worked if: every row on Follow-ups answered what to do about it, with exactly one Fix", async () => {
    await page.goto("/follow-ups?org=all");
    const rows = await followUpRows(page);
    expect(rows.length).toBeGreaterThan(0);
    for (const r of rows) {
      expect(r.fixCells, `${r.title} has one Fix`).toBe(1);
      expect(r.fixes, `${r.title}'s Fix is one action: ${r.fix}`).toBe(1);
    }
  });
});

type FollowUpRow = {
  id: number;
  title: string;
  health: string;
  kinds: string[];
  fixCells: number;
  fixes: number;
  fix: string;
};

// followUpRows reads the Follow-ups table's rows in order: each flagged Goal's id and
// title (its first link; a chip may link its parent), Health, the kinds of its
// chips, its Fix cells, and the controls in them and what they say.
function followUpRows(page: Page): Promise<FollowUpRow[]> {
  return page
    .getByTestId("follow-ups-table")
    .getByTestId("follow-up-row")
    .evaluateAll((trs) =>
      trs.map((tr) => {
        const link = tr.querySelector('[data-testid="follow-up-goal"] a');
        const fix = [...tr.querySelectorAll('[data-testid="follow-up-fix"]')];
        const controls = fix.flatMap((td) => [...td.querySelectorAll("a, button")]);
        return {
          id: Number(link?.getAttribute("href")?.replace("/goals/", "")),
          title: link?.textContent?.trim() ?? "",
          // innerText leaves out the hidden print-only shape.
          health: (tr.querySelector('[data-testid="follow-up-health"]') as HTMLElement | null)?.innerText.trim() ?? "",
          kinds: [...tr.querySelectorAll('[data-testid="follow-up-signal"]')].map((c) => c.getAttribute("data-kind") ?? ""),
          fixCells: fix.length,
          fixes: controls.length,
          fix: controls.map((c) => c.textContent?.trim()).join(" | "),
        };
      }),
    );
}

// followUpRow is the Follow-ups table's row for the Goal with id: the row whose first
// link is the Goal's (a chip may link its parent).
function followUpRow(page: Page, id: number): Locator {
  return page
    .getByTestId("follow-ups-table")
    .getByTestId("follow-up-row")
    .filter({ has: page.getByTestId("follow-up-goal").locator(`a[href="/goals/${id}"]`).first() });
}

// followUpHealthRank orders Health on Follow-ups most urgent first: Red, Yellow, none,
// Green.
function followUpHealthRank(health: string): number {
  return { Red: 0, Yellow: 1, Green: 3 }[health] ?? 2;
}

// compareKeys compares two sort keys element by element.
function compareKeys(a: number[], b: number[]): number {
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return a[i] - b[i];
  }
  return 0;
}

type GoalListRow = {
  title: string;
  health: string;
  checkinOverdue: boolean;
  ownerless: boolean;
  overdue: boolean;
  milestoneOverdue: boolean;
};

// goalRows reads the Goal list's rows in order: each Goal's title, Health
// (or Lifecycle when it has none) and marks.
function goalRows(page: Page): Promise<GoalListRow[]> {
  return page.getByTestId("goal-row").evaluateAll((trs) =>
    trs.map((tr) => {
      const has = (id: string) => tr.querySelector(`[data-testid="${id}"]`) !== null;
      return {
        title: tr.querySelector("a")?.textContent?.trim() ?? "",
        health: tr.querySelector('[data-testid="goal-row-health"]')?.textContent?.trim() ?? "",
        checkinOverdue: has("checkin-overdue"),
        ownerless: has("ownerless"),
        overdue: has("path-overdue"),
        milestoneOverdue: has("milestone-overdue"),
      };
    }),
  );
}

// marked reports whether a Goal list row carries a problem mark.
function marked(r: GoalListRow): boolean {
  return r.checkinOverdue || r.ownerless || r.overdue || r.milestoneOverdue;
}

// goalRow is the Goal list's row for the Goal titled title, within scope.
function goalRow(scope: Page | Locator, title: string): Locator {
  const page = "page" in scope ? scope.page() : scope;
  return scope.getByTestId("goal-row").filter({ has: page.getByRole("link", { name: title, exact: true }) });
}

// navFollowUpsCount is the count on the top bar's Follow-ups item.
async function navFollowUpsCount(page: Page): Promise<number> {
  const text = (await page.getByTestId("nav-follow-ups").textContent()) ?? "";
  const m = text.match(/\d+/);
  expect(m, `the Follow-ups item "${text.trim()}" shows a count`).not.toBeNull();
  return Number(m?.[0]);
}

// isoDate is the app's today plus days, as a date input takes it, in UTC: the
// org's timezone in the suite.
function isoDate(days: number): string {
  const d = new Date(`${appToday()}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}
