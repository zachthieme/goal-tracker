// Scenario 1, Finding trouble between reviews (docs/scenarios.md): a leader
// learns what's in trouble, and why, from Goals and Risks without anyone
// telling her, and every problem sits beside the one action that moves it.
// Goals is where Health shows; Risks is where the reasons show, a Red or
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

// isStale is SQL true for a Goal g whose last Check-in, or activation if it
// has none, is older than its cadence: the seed's Stale Goals are weeks past
// it, so the day boundary doesn't matter.
const isStale = `
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

test("a leader finds trouble between reviews on Goals and Risks", async ({ page, as, seedLookup, serverLog }) => {
  test.setTimeout(180_000);

  // The actors and Goals, chosen by criteria.
  // A Stale Platform Goal the leader doesn't own, to nudge in step 5.
  const [stale] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and a.email <> ? and a.departed = 0 and ${onTeam} and ${isStale}
     order by g.id limit 1`,
    leader,
    "Platform",
  );
  expect(stale, "the seed has a Stale Platform Goal the leader doesn't own").toBeDefined();

  // An Owner to depart: owns an Active Platform Goal, owns no Stale Goal (so
  // not the one nudged) and no Top-level Goal, and is neither the leader nor
  // the Admin. The Stale subquery names its Goal g, as isStale reads it.
  const [departing] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${onTeam} and a.email not in (?, ?) and a.is_admin = 0
       and not exists (select 1 from goals g2 where g2.owner_id = a.id and g2.top_level = 1)
       and not exists (select 1 from goals g where g.owner_id = a.id and g.lifecycle = 'Active' and ${isStale})
     order by g.id limit 1`,
    "Platform",
    leader,
    admin,
  );
  expect(departing, "the seed has a Platform Owner to depart").toBeDefined();

  // A Goal to put past its Path to Green: an Active Platform Goal whose Owner
  // stays, with no Active child Goals (so no Rolled-up Health asking for an
  // explanation), not Stale, and with no overdue Planned Milestone, so the
  // Check-in is only Health, status and Path to Green.
  const [overdue] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${onTeam} and a.email not in (?, ?) and a.departed = 0
       and g.id <> ? and not (${isStale})
       and not exists (select 1 from links l join goals c on c.id = l.child_id
                       where l.parent_id = g.id and l.status = 'accepted' and c.lifecycle = 'Active')
       and not exists (select 1 from milestones m
                       where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
     order by g.id limit 1`,
    "Platform",
    leader,
    departing.email,
    stale.id,
  );
  expect(overdue, "the seed has a Platform Goal to put past its Path to Green").toBeDefined();

  // Two Active Platform Goals to carry the second Dimension's values.
  const tiered = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${onTeam} order by g.id limit 2`,
    "Platform",
  );
  expect(tiered, "the seed has two Active Platform Goals").toHaveLength(2);

  // An Unaligned Goal: Active, not Top-level, contributing to no Goal.
  const [unaligned] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and g.top_level = 0 and a.email <> ?
       and not exists (select 1 from links l where l.child_id = g.id and l.status = 'accepted')
     order by g.id limit 1`,
    departing.email,
  );
  expect(unaligned, "the seed has an Unaligned Goal").toBeDefined();

  // A Goal with two signals: Stale, and due later than a Goal it contributes
  // to (a Schedule conflict), with its Team.
  const [twoReasons] = seedLookup<Goal & { team: string }>(
    `select g.id, g.title, a.email, v.value as team from goals g
     join accounts a on a.id = g.owner_id
     join goal_dimension_values gdv on gdv.goal_id = g.id
     join dimension_values v on v.id = gdv.dimension_value_id
     join dimensions d on d.id = v.dimension_id and d.name = 'Team'
     where g.lifecycle = 'Active' and a.email <> ? and ${isStale} and g.delivery_date <> ''
       and exists (select 1 from links l join goals p on p.id = l.parent_id
                   where l.child_id = g.id and l.status = 'accepted'
                     and p.delivery_date <> '' and g.delivery_date > p.delivery_date)
     order by g.id limit 1`,
    departing.email,
  );
  expect(twoReasons, "the seed has a Stale Goal with a Schedule conflict").toBeDefined();

  // The departed Owner's Active Goals, which become Ownerless on Risks.
  const ownerless = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where a.email = ? and g.lifecycle = 'Active' order by g.id`,
    departing.email,
  );

  // Two Red Goals to open: one with Active child Goals contributing to it
  // (Rolled-up Health), one whose delivery date has slipped. Seed 23 has no
  // Red Goal with both. The first is otherwise fine: not Stale, contributing to
  // a Goal, with an Owner who stays, so only its Health, under Not Green, puts
  // it on Risks.
  const [redWithChildren] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${latestHealth} = 'Red' and not (${isStale}) and a.email <> ?
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

  // Each Goal's Lifecycle, which Risks' order reads: Active Goals first.
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
    await form.getByRole("textbox", { name: /^Status/ }).fill("The cut-over slipped past the date we gave.");
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

  await test.step("Setup 4: the leader publishes a Report and raises an Action Item about the Stale Goal, due two days ago", async () => {
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
    await form.getByLabel("Owner").fill(stale.email);
    await form.getByLabel("Due").fill(isoDate(-2));
    await form.getByTestId("action-item-goal").selectOption(String(stale.id));
    await form.getByRole("button", { name: "Add Action Item" }).click();
    await expect(items.getByTestId("raised-action-item")).toContainText(`about ${stale.title}`);
  });

  await test.step("1. The top bar shows a count on Risks before she clicks anything", async () => {
    await signIn(page, leader);
    const count = await navRisksCount(page);
    expect(count, "Risks shows a count from Home").toBeGreaterThan(0);
    // The count is her default scope, My org, as many as a bare /risks lists.
    await page.goto("/risks");
    await expect(page.getByTestId("risks-attention")).toHaveText(
      new RegExp(`^${count} Goals need attention in My org`),
    );
    expect(await riskRows(page), "the count is a bare /risks's rows").toHaveLength(count);
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

    // Marks: the Stale Goal carries its stale mark, the departed Owner's Goal
    // its ownerless mark.
    expect(rows.find((r) => r.title === stale.title)?.stale, `${stale.title} is marked Stale`).toBe(true);
    expect(rows.find((r) => r.title === departing.title)?.ownerless, `${departing.title} is marked Ownerless`).toBe(
      true,
    );
    await expect(goalRow(page, stale.title).getByTestId("stale")).toBeVisible();
    await expect(goalRow(page, departing.title).getByTestId("ownerless")).toBeVisible();

    // Filter: Team = Platform, under More filters. Every row is a Platform Goal.
    const more = page.getByTestId("more-filters");
    await more.getByText("More filters").click();
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

  await test.step("3. Risks says how many Goals need attention, in five groups by who has to act", async () => {
    await page.goto("/risks?org=all");
    await expect(page.getByTestId("risks-attention")).toContainText("Goals need attention");
    const cards = [
      { key: "health", name: "Not Green", kinds: ["red", "yellow"] },
      { key: "owner", name: "Waiting on a Check-in", kinds: ["stale", "path-overdue", "milestone-overdue"] },
      { key: "followup", name: "Action Items overdue", kinds: ["action-item-overdue"] },
      { key: "plan", name: "Placement and dates", kinds: ["unaligned", "schedule-conflicts"] },
      { key: "admin", name: "Needs an Admin", kinds: ["ownerless"] },
    ];
    for (const c of cards) {
      const card = page.getByTestId(`risks-group-${c.key}`);
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
        { goal: stale, kind: "stale" },
        { goal: overdue, kind: "path-overdue" },
      ],
      followup: [{ goal: stale, kind: "action-item-overdue" }],
      plan: [
        { goal: unaligned, kind: "unaligned" },
        { goal: twoReasons, kind: "schedule-conflicts" },
      ],
      admin: ownerless.map((goal) => ({ goal, kind: "ownerless" })),
    };
    // Every card keeps Everyone (keys sorted), Needs an Admin included.
    for (const c of cards) {
      await page.goto("/risks?org=all");
      const card = page.getByTestId(`risks-group-${c.key}`);
      const count = Number(await card.getByText(/^\d+$/).textContent());
      await card.click();
      await expect(page).toHaveURL(new RegExp(`/risks\\?group=${c.key}&org=all$`));
      const rows = await riskRows(page);
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
      // The Stale chip on the card counts the Stale rows; Not Green's Red and
      // Yellow chips count the rows of that Health.
      for (const kind of c.kinds) {
        const n = health
          ? rows.filter((r) => r.health === (kind === "red" ? "Red" : "Yellow")).length
          : rows.filter((r) => r.kinds.includes(kind)).length;
        await expect(card.locator(`[data-kind="${kind}"]`)).toHaveText(new RegExp(` · ${n}$`));
      }
    }
  });

  await test.step("4. Narrowed to her org, each flagged Goal is one row, worst first, with a chip per reason and one Fix", async () => {
    // A bare /risks is her default, My org, which the top bar counts.
    await page.goto("/risks");
    const inOrg = await navRisksCount(page);
    expect(await riskRows(page), "the top bar counts a bare /risks's rows").toHaveLength(inOrg);
    const teamSelect = page.locator('[data-testid="risks-value"][data-dimension="Team"]');
    await expect(teamSelect.locator("option").first()).toHaveText("Team: any");
    await teamSelect.selectOption({ label: "Platform" });
    await expect(page).toHaveURL(/[?&]value=\d+/);
    const rows = await riskRows(page);
    await expect(page.getByTestId("risks-attention")).toHaveText(new RegExp(`^${rows.length} Goals? needs? attention`));
    // The top bar counts her whole org; the narrowed page doesn't.
    expect(rows.length, "Platform has fewer flagged Goals than her org").toBeLessThan(inOrg);
    expect(await navRisksCount(page), "the top bar's count doesn't narrow").toBe(inOrg);

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
    // Worst first (sortRiskRows): Active Goals before Proposed and On Hold
    // ones, then Ownerless, then by Health (Red, Yellow, none, Green), then
    // Path to Green overdue, then Milestone overdue, then an overdue Action
    // Item, then Stale.
    const keys = rows.map((r) => [
      lifecycles.get(r.id) === "Active" ? 0 : 1,
      r.kinds.includes("ownerless") ? 0 : 1,
      riskHealthRank(r.health),
      r.kinds.includes("path-overdue") ? 0 : 1,
      r.kinds.includes("milestone-overdue") ? 0 : 1,
      r.kinds.includes("action-item-overdue") ? 0 : 1,
      r.kinds.includes("stale") ? 0 : 1,
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
      { goal: stale, kind: "stale" },
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
      page.getByTestId("risks-table").getByRole("link", { name: twoReasons.title, exact: true }),
    ).toBeVisible();
    const team = await riskRows(page);
    const two = team.filter((r) => r.id === twoReasons.id);
    expect(two, `${twoReasons.title} is one row`).toHaveLength(1);
    expect(two[0].kinds.toSorted(), "with a chip for each reason").toEqual(["schedule-conflicts", "stale"]);
    expect(two[0].fixes, "and one Fix").toBe(1);
  });

  await test.step("5. Nudge on a Stale Goal she doesn't own asks its Owner to check in, once that day", async () => {
    await page.goto("/risks");
    const row = riskRow(page, stale.id);
    const fix = row.getByTestId("risk-fix");
    await fix.getByRole("button", { name: "Nudge" }).click();
    await expect(page).toHaveURL(/\/risks$/);
    // No toast: the row's Fix now says who nudged it today, and can't be used.
    await expect(page.getByTestId("toast")).toHaveCount(0);
    await expect(fix.getByRole("button")).toHaveText(`Nudged today by ${leaderName}`);
    await expect(fix.getByRole("button")).toBeDisabled();

    // A second Nudge the same day is refused.
    const again = await page.request.post(`/goals/${stale.id}/nudge`, { form: { return: "/risks" }, maxRedirects: 0 });
    expect(again.status(), "a second Nudge the same day is refused").toBe(422);
    expect(await again.text()).toMatch(/data-testid="nudge-refused">[^<]*already nudged this Goal today/);

    // The Goal's History shows the Nudge.
    await page.goto(`/goals/${stale.id}`);
    const nudge = page
      .getByTestId("goal-history")
      .getByTestId("history-entry")
      .and(page.locator('[data-kind="nudge"]'));
    await expect(nudge).toHaveCount(1);
    await expect(nudge).toContainText(leaderName);
    await expect(nudge).toContainText("nudged for a Check-in");

    // The Owner and Delegates are asked only by email.
    expect(serverLog()).toContain(`Check-in requested: ${stale.title}`);
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
    // On Goals, the Stale row sorts above every unmarked Yellow and Green row,
    // and carries its own mark.
    await page.goto("/goals");
    const rows = await goalRows(page);
    const at = rows.findIndex((r) => r.title === stale.title);
    expect(rows[at]?.stale, `${stale.title} carries its Stale mark`).toBe(true);
    const quieter = rows.findIndex((r) => (r.health === "Yellow" || r.health === "Green") && !marked(r));
    expect(at, `${stale.title} sorts above every unmarked Yellow and Green row`).toBeLessThan(quieter);

    // On Risks, it's a row in the main table, not in the folded definitions,
    // with its chip and a Fix, and counted on the owner card.
    await page.goto("/risks");
    const row = riskRow(page, stale.id);
    await expect(row.locator('[data-testid="risk-signal"][data-kind="stale"]')).toBeVisible();
    await expect(row.getByTestId("risk-fix")).toHaveCount(1);
    await expect(page.getByTestId("risks-definitions").getByText(stale.title)).toHaveCount(0);
    await page.getByTestId("risks-group-owner").click();
    expect(
      (await riskRows(page)).map((r) => r.id),
      "the owner card counts it",
    ).toContain(stale.id);
  });

  await test.step("It worked if: she could tell in trouble from silent from badly placed", async () => {
    // In trouble: Red or Yellow Health, on Goals, and under Not Green on
    // Risks, a Red Goal nothing else is wrong with included.
    await page.goto("/goals?org=all");
    await expect(goalRow(page, redWithChildren.title).getByTestId("goal-row-health")).toHaveText("Red");
    await expect(goalRow(page, overdue.title).getByTestId("goal-row-health")).toHaveText("Yellow");
    await page.goto("/risks?org=all");
    expect(
      (await riskRows(page)).map((r) => r.id),
      `${redWithChildren.title} is on Risks`,
    ).toContain(redWithChildren.id);
    await page.goto("/risks?group=health&org=all");
    expect(
      (await riskRows(page)).map((r) => r.id),
      `${redWithChildren.title} is under Not Green`,
    ).toContain(redWithChildren.id);

    // Silent: Stale, under Waiting on a Check-in, not Placement and dates.
    // Badly placed: Unaligned or a Schedule conflict, under Placement and
    // dates, not Waiting on a Check-in.
    await page.goto("/risks?group=owner&org=all");
    const owner = (await riskRows(page)).map((r) => r.id);
    await page.goto("/risks?group=plan&org=all");
    const plan = (await riskRows(page)).map((r) => r.id);
    expect(owner, "Stale is under Waiting on a Check-in").toContain(stale.id);
    expect(plan, "Stale isn't under Placement and dates").not.toContain(stale.id);
    expect(plan, "Unaligned is under Placement and dates").toContain(unaligned.id);
    expect(owner, "Unaligned isn't under Waiting on a Check-in").not.toContain(unaligned.id);
    expect(plan, "a Schedule conflict is under Placement and dates").toContain(twoReasons.id);
    expect(owner, "a Red Goal that's up to date isn't under Waiting on a Check-in").not.toContain(redWithChildren.id);
    expect(plan, "a Red Goal that's well placed isn't under Placement and dates").not.toContain(redWithChildren.id);
  });

  await test.step("It worked if: every row on Risks answered what to do about it, with exactly one Fix", async () => {
    await page.goto("/risks?org=all");
    const rows = await riskRows(page);
    expect(rows.length).toBeGreaterThan(0);
    for (const r of rows) {
      expect(r.fixCells, `${r.title} has one Fix`).toBe(1);
      expect(r.fixes, `${r.title}'s Fix is one action: ${r.fix}`).toBe(1);
    }
  });
});

type RiskRow = {
  id: number;
  title: string;
  health: string;
  kinds: string[];
  fixCells: number;
  fixes: number;
  fix: string;
};

// riskRows reads the Risks table's rows in order: each flagged Goal's id and
// title (its first link; a chip may link its parent), Health, the kinds of its
// chips, its Fix cells, and the controls in them and what they say.
function riskRows(page: Page): Promise<RiskRow[]> {
  return page
    .getByTestId("risks-table")
    .getByTestId("risk-row")
    .evaluateAll((trs) =>
      trs.map((tr) => {
        const link = tr.querySelector('[data-testid="risk-goal"] a');
        const fix = [...tr.querySelectorAll('[data-testid="risk-fix"]')];
        const controls = fix.flatMap((td) => [...td.querySelectorAll("a, button")]);
        return {
          id: Number(link?.getAttribute("href")?.replace("/goals/", "")),
          title: link?.textContent?.trim() ?? "",
          // innerText leaves out the hidden print-only shape.
          health: (tr.querySelector('[data-testid="risk-health"]') as HTMLElement | null)?.innerText.trim() ?? "",
          kinds: [...tr.querySelectorAll('[data-testid="risk-signal"]')].map((c) => c.getAttribute("data-kind") ?? ""),
          fixCells: fix.length,
          fixes: controls.length,
          fix: controls.map((c) => c.textContent?.trim()).join(" | "),
        };
      }),
    );
}

// riskRow is the Risks table's row for the Goal with id: the row whose first
// link is the Goal's (a chip may link its parent).
function riskRow(page: Page, id: number): Locator {
  return page
    .getByTestId("risks-table")
    .getByTestId("risk-row")
    .filter({ has: page.getByTestId("risk-goal").locator(`a[href="/goals/${id}"]`).first() });
}

// riskHealthRank orders Health on Risks worst first: Red, Yellow, none, Green.
function riskHealthRank(health: string): number {
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
  stale: boolean;
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
        stale: has("stale"),
        ownerless: has("ownerless"),
        overdue: has("path-overdue"),
        milestoneOverdue: has("milestone-overdue"),
      };
    }),
  );
}

// marked reports whether a Goal list row carries a problem mark.
function marked(r: GoalListRow): boolean {
  return r.stale || r.ownerless || r.overdue || r.milestoneOverdue;
}

// goalRow is the Goal list's row for the Goal titled title, within scope.
function goalRow(scope: Page | Locator, title: string): Locator {
  const page = "page" in scope ? scope.page() : scope;
  return scope.getByTestId("goal-row").filter({ has: page.getByRole("link", { name: title, exact: true }) });
}

// navRisksCount is the count on the top bar's Risks item.
async function navRisksCount(page: Page): Promise<number> {
  const text = (await page.getByTestId("nav-risks").textContent()) ?? "";
  const m = text.match(/\d+/);
  expect(m, `the Risks item "${text.trim()}" shows a count`).not.toBeNull();
  return Number(m?.[0]);
}

// isoDate is the app's today plus days, as a date input takes it, in UTC: the
// org's timezone in the suite.
function isoDate(days: number): string {
  const d = new Date(`${appToday()}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}
