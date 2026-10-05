// Scenario 1, Finding trouble between reviews (docs/scenarios.md): a leader
// learns what's in trouble, and why, from Goals and Risks without anyone
// telling her, and every problem sits beside the one action that moves it.
// Goals is where Health shows; Risks is where the reasons show. Red Health
// isn't a Risks signal, by design.
//
// The leader ("Elena" in the scenario) is cto@example.com, whom the seed names
// Priya Raman. The setup makes what the seed lacks: Ownerless Goals, an overdue
// Path to Green, and a second Dimension.
import type { Locator } from "@playwright/test";

import { admin, expect, type Page, signIn, test } from "../fixtures";

const leader = "cto@example.com";
const leaderName = "Priya Raman";

// The second Dimension the Admin defines, and its values.
const tier = { name: "Tier", values: ["Core", "Edge"] };

// isStale is SQL true for a Goal g whose last Check-in, or activation if it
// has none, is older than its cadence: the seed's Stale Goals are weeks past
// it, so the day boundary doesn't matter.
const isStale = `
  julianday('now') - julianday(coalesce(
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
  // the Admin.
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
  // explanation), not Stale, and with no overdue Planned Milestone (which would
  // refuse nothing at Yellow, but keeps the Check-in plain).
  const [overdue] = seedLookup<Goal>(
    `select g.id, g.title, a.email from goals g join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and ${onTeam} and a.email not in (?, ?) and a.departed = 0
       and g.id <> ? and not (${isStale})
       and not exists (select 1 from links l join goals c on c.id = l.child_id
                       where l.parent_id = g.id and l.status = 'accepted' and c.lifecycle = 'Active')
       and not exists (select 1 from milestones m
                       where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date('now'))
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
    await form.getByLabel("Plan").fill("Move the last two services over before the freeze.");
    await form.getByLabel("Back to Green by").fill(isoDate(-2));
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

  await test.step("1. The top bar shows a count on Risks before she clicks anything", async () => {
    await signIn(page, leader);
    const count = await navRisksCount(page);
    expect(count, "Risks shows a count from Home").toBeGreaterThan(0);
    await page.goto("/risks");
    await expect(page.getByTestId("risks-attention")).toHaveText(new RegExp(`^${count} Goals need attention`));
  });

  await test.step("2. Goals is led by Health, problems first, filtered to her org and grouped by another Dimension", async () => {
    await page.goto("/goals");
    const rows = await goalRows(page);
    expect(rows.length, "the Goal list lists the seed's Goals").toBeGreaterThan(10);

    // Problem order (problemRank): every Red row comes first, and no unmarked
    // Green row comes before a Yellow one.
    const firstNotRed = rows.findIndex((r) => r.health !== "Red");
    expect(rows.slice(firstNotRed).filter((r) => r.health === "Red"), "every Red row comes first").toEqual([]);
    const lastYellow = rows.findLastIndex((r) => r.health === "Yellow");
    const unmarkedGreen = rows.findIndex((r) => r.health === "Green" && !marked(r));
    expect(unmarkedGreen, "no unmarked Green row comes before a Yellow one").toBeGreaterThan(lastYellow);

    // Marks: the Stale Goal carries its stale mark, the departed Owner's Goal
    // its ownerless mark.
    expect(rows.find((r) => r.title === stale.title)?.stale, `${stale.title} is marked Stale`).toBe(true);
    expect(rows.find((r) => r.title === departing.title)?.ownerless, `${departing.title} is marked Ownerless`).toBe(true);
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
      const group = page.getByTestId("goal-group").filter({ has: page.getByTestId("goal-group-label").getByText(tier.values[i], { exact: true }) });
      await expect(goalRow(group, goal.title)).toBeVisible();
    }
  });
});

type GoalListRow = { title: string; health: string; stale: boolean; ownerless: boolean; overdue: boolean };

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
      };
    }),
  );
}

// marked reports whether a Goal list row carries a problem mark.
function marked(r: GoalListRow): boolean {
  return r.stale || r.ownerless || r.overdue;
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

// isoDate is today plus days, as a date input takes it, in UTC: the org's
// timezone in the suite.
function isoDate(days: number): string {
  return new Date(Date.now() + days * 86_400_000).toISOString().slice(0, 10);
}
