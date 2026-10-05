// Scenario 4, Reporting bad news (docs/scenarios.md): an Owner can't report
// trouble vaguely or bury it. Going Yellow needs a Path to Green, a later
// delivery date needs a reason and stays visible as a Date Slip, and the
// trouble reaches the parent's Owner as Rolled-up Health without turning
// every ancestor Red.
import type { Locator, Page } from "@playwright/test";
import { expect, signIn, test } from "../fixtures";

// The scenario's people are roles. "Priya" is the project's Owner and
// "Marcus" the Owner of the team Goal it contributes to; both are chosen from
// the seed by chooseBadNewsChain.
test("an Owner reports bad news: Path to Green, Date Slips, Rolled-up Health", async ({ page, as, seedLookup }) => {
  test.slow();
  const chain = chooseBadNewsChain(seedLookup);
  const { project } = chain;

  await signIn(page, project.email);

  await test.step("4.1 Yellow needs a plan and a date", async () => {
    // It worked if: she couldn't go Yellow without a plan and a date.
    await page.goto(`/goals/${project.id}/checkin`);
    const form = page.getByTestId("checkin-form");
    const path = form.getByTestId("path-to-green-field");
    await expect(form.getByRole("radio", { name: "Green" })).toBeChecked();
    await expect(path).toBeHidden();

    await pickHealth(form, "Yellow");
    await expect(path).toBeVisible();

    await statusBox(form).fill("A vendor delay puts us behind.");
    await submit(form);
    await expect(path.getByTestId("checkin-error")).toContainText("needs a Path to Green");
    await expect(form.getByRole("radio", { name: "Yellow" })).toBeChecked();
    await expect(statusBox(form)).toHaveValue("A vendor delay puts us behind.");

    await path.getByLabel("Plan").fill("Escalate with the vendor and ship behind a flag.");
    await submit(form);
    await expect(path.getByTestId("checkin-error")).toContainText("needs a target date");
    await expect(form.getByRole("radio", { name: "Yellow" })).toBeChecked();
    await expect(statusBox(form)).toHaveValue("A vendor delay puts us behind.");
    await expect(path.getByLabel("Plan")).toHaveValue("Escalate with the vendor and ship behind a flag.");
  });

  const today = isoDate(new Date());
  const oldDue = project.delivery_date;
  const newDue = addDays(oldDue, 21);
  const dueReason = "The vendor's API slipped three weeks.";
  const [slipped, done] = project.milestones;
  const slippedTo = addDays(slipped.target_date, 7);
  const slipReason = "Waiting on the vendor's sandbox.";

  await test.step("4.2 a later delivery date needs a reason and rules out Green", async () => {
    const form = page.getByTestId("checkin-form");
    const path = form.getByTestId("path-to-green-field");
    await path.getByLabel("Back to Green by").fill(addDays(today, 28));

    const dates = form.getByTestId("checkin-dates-section");
    await dates.getByText(/^Change dates or milestones/).click();
    await dates.getByLabel("Delivery date", { exact: true }).fill(newDue);
    await submit(form);
    await expect(fieldError(form, "delivery_date_reason")).toContainText("changing the delivery date needs a reason");

    await dates.getByLabel("Reason (required if the delivery date changes)").fill(dueReason);
    await pickHealth(form, "Green");
    await submit(form);
    await expect(fieldError(form, "delivery_date")).toContainText("moves the delivery date later can't be Green");
    await expect(dates.getByLabel("Delivery date", { exact: true })).toHaveValue(newDue);

    // Back to Yellow, the plan and date still there: the next submit, with the
    // Milestone changes, is accepted.
    await pickHealth(form, "Yellow");
    await expect(path.getByLabel("Plan")).toHaveValue("Escalate with the vendor and ship behind a flag.");
    await expect(path.getByLabel("Back to Green by")).toHaveValue(addDays(today, 28));
  });

  await test.step("4.3 one Milestone moves later and another is Done, in the same Check-in", async () => {
    const form = page.getByTestId("checkin-form");
    const row = form.getByTestId("checkin-milestone").filter({ hasText: slipped.name });
    await row.getByLabel(`Date of ${slipped.name}`).fill(slippedTo);
    await row.getByLabel("Reason, if the date changes").fill(slipReason);
    await form.getByLabel(`Status of ${done.name}`).selectOption("Done");
    await submit(form);
    await page.waitForURL(`**/goals/${project.id}`);
  });

  await test.step("4.4 the Goal page shows the slips and the Milestone change", async () => {
    // It worked if: the slip shows wherever the date does — the Goal page.
    const due = page.getByTestId("goal-delivery-date");
    await expect(due.locator("del")).toHaveText(oldDue);
    await expect(due).toHaveText(`${oldDue} ${newDue}`);

    await expect(milestoneRow(page, slipped.name).getByTestId("milestone-mark")).toHaveText("Yellow");
    await expect(milestoneRow(page, done.name).getByTestId("milestone-mark")).toHaveText("Done");

    const latest = page.getByTestId("history-entry").first();
    await expect(latest).toHaveAttribute("data-kind", "checkin");
    await expect(latest).toContainText(project.name);
    const slips = latest.getByTestId("entry-slips").getByTestId("date-slip");
    await expect(slips).toHaveText([
      `Delivery date: ${oldDue} ${newDue} — ${dueReason}`,
      `Milestone ${slipped.name}: ${slipped.target_date} ${slippedTo} — ${slipReason}`,
    ]);
    await expect(latest.getByTestId("milestone-change")).toHaveText([`Marked ${done.name} Done`]);
  });

  const marcus = await as(chain.team.email);

  await test.step("It worked if: the slip shows on the Goals list and in a Report draft", async () => {
    // It worked if: the slip shows wherever the date does — the Goals list.
    await page.goto("/goals");
    const due = goalRow(page, project.title).getByTestId("goal-row-due");
    await expect(due.locator("del")).toHaveText(oldDue);
    await expect(due).toHaveText(`${oldDue} ${newDue}`);

    // ...and a Report draft over the project, built by the team Goal's Owner
    // with a rule on the project's Team.
    await marcus.goto("/reports/new");
    const builder = marcus.getByTestId("report-builder");
    await builder.getByLabel("Name").fill("Bad news this week");
    const rule = builder.getByTestId("report-rule").first();
    await rule.getByTestId("rule-attribute").selectOption({ label: "Team" });
    await rule.getByTestId("rule-values").selectOption({ label: project.team });
    await builder.getByRole("button", { name: "Save report" }).last().click();
    await marcus.waitForURL(/\/reports\/\d+$/);
    const block = marcus
      .getByTestId("report-exception")
      .filter({ has: marcus.getByRole("link", { name: project.title, exact: true }) });
    const reportDue = block.getByTestId("report-due");
    await expect(reportDue.locator("del")).toHaveText(oldDue);
    await expect(reportDue).toHaveText(`${oldDue} ${newDue}`);
  });

  await test.step("4.5 the team Goal's Owner can't stay Green over a Yellow child without saying why", async () => {
    // It worked if: Marcus couldn't stay Green over a Yellow child without
    // saying why.
    await marcus.goto(`/goals/${chain.team.id}`);
    await expect(marcus.getByTestId("goal-owner")).toContainText(chain.team.name);
    await expect(marcus.getByTestId("goal-rollup-health")).toHaveText("Yellow");
    await expect(marcus.getByTestId("goal-health")).toHaveText("Green");

    await marcus.goto(`/goals/${chain.team.id}/checkin`);
    const form = marcus.getByTestId("checkin-form");
    await expect(form.getByTestId("checkin-form-rollup")).toContainText("Rolled-up Health: Yellow");
    // The Explanation is there whenever there is a Rolled-up Health, whatever
    // Health is picked.
    for (const health of ["Yellow", "Red", "Green"]) {
      await pickHealth(form, health);
      await expect(form.getByTestId("checkin-explanation")).toBeVisible();
    }

    await statusBox(form).fill("On track overall.");
    await form.getByTestId("checkin-explanation").fill("");
    await submit(form);
    await expect(fieldError(form, "explanation")).toContainText("differs from the Rolled-up Health (Yellow)");

    const why = "The vendor slip is covered by the project's plan; the team Goal still lands.";
    await form.getByTestId("checkin-explanation").fill(why);
    await submit(form);
    await marcus.waitForURL(`**/goals/${chain.team.id}`);
    await expect(marcus.getByTestId("goal-health")).toHaveText("Green");
    await expect(marcus.getByTestId("goal-rollup-explanation")).toContainText(why);
  });

  await test.step("4.6 a passed back-to-Green date is flagged on Risks and on the Goals list", async () => {
    // Two days ago, so it has passed in any org timezone.
    await page.goto(`/goals/${project.id}/checkin`);
    const form = page.getByTestId("checkin-form");
    await expect(form.getByRole("radio", { name: "Yellow" })).toBeChecked();
    await form.getByTestId("path-to-green-field").getByLabel("Back to Green by").fill(addDays(today, -2));
    await statusBox(form).fill("Still waiting on the vendor.");
    await submit(form);
    await page.waitForURL(`**/goals/${project.id}`);

    await marcus.goto("/risks");
    await marcus.getByTestId("risks-group-owner").click();
    await expect(marcus).toHaveURL(/[?&]group=owner\b/);
    const row = marcus
      .getByTestId("risk-row")
      .filter({ has: marcus.getByRole("link", { name: project.title, exact: true }) });
    await expect(row.getByTestId("risk-owner")).toContainText(project.name);
    await expect(row.locator('[data-testid="risk-signal"][data-kind="path-overdue"]')).toHaveText(
      /^Path to Green \d+d overdue$/,
    );
    await expect(row.getByTestId("risk-fix")).toHaveCount(1);

    // On the Goals list it sorts with the Stale and overdue Goals, above every
    // Yellow and Green Goal without a mark.
    await page.goto("/goals");
    await expect(goalRow(page, project.title).getByTestId("path-overdue")).toBeVisible();
    const rows = await page.getByTestId("goal-row").evaluateAll((trs) =>
      trs.map((tr) => ({
        title: tr.querySelector("td:nth-child(2) a")?.textContent?.trim() ?? "",
        health: tr.querySelector('[data-testid="goal-row-health"]')?.textContent?.trim() ?? "",
        marked:
          tr.querySelector('[data-testid="ownerless"], [data-testid="stale"], [data-testid="path-overdue"]') !== null,
      })),
    );
    const at = rows.findIndex((r) => r.title === project.title);
    expect(at, "the project is on the Goals list").toBeGreaterThanOrEqual(0);
    const quietAbove = rows
      .slice(0, at)
      .filter((r) => !r.marked && (r.health === "Yellow" || r.health === "Green"))
      .map((r) => r.title);
    expect(quietAbove, "no unmarked Yellow or Green Goal sorts above the overdue project").toEqual([]);
    expect(
      rows.slice(at + 1).some((r) => !r.marked && (r.health === "Yellow" || r.health === "Green")),
      "unmarked Yellow and Green Goals sort below it",
    ).toBe(true);
  });

  await test.step("It worked if: the slip can't be quietly overwritten", async () => {
    // The Goal page offers no delivery-date edit outside a Check-in.
    await page.goto(`/goals/${project.id}`);
    await expect(page.locator('input[name="delivery_date"]')).toHaveCount(0);
    await expect(page.locator('form[action$="/dated"]')).toHaveCount(0);

    // Posting one anyway is refused once the Goal is Active
    // (requireProposedToSetDelivery); without a valid date it's refused before
    // that check runs.
    const refused = await page.request.post(`/goals/${project.id}/dated`, {
      form: { delivery_date: addDays(newDue, 30) },
    });
    expect(refused.status()).toBe(422);
    expect(await refused.text()).toContain("delivery date changes only in a Check-in");
    const malformed = await page.request.post(`/goals/${project.id}/dated`, { form: { delivery_date: "soon" } });
    expect(malformed.status()).toBe(422);
    expect(await malformed.text()).toContain("invalid delivery date");

    // The define page, where a Proposed Goal's date is set, sends an Active
    // Goal back to its page.
    await page.goto(`/goals/${project.id}/define`);
    await expect(page).toHaveURL(new RegExp(`/goals/${project.id}$`));
    await expect(page.getByTestId("goal-delivery-date")).toHaveText(`${oldDue} ${newDue}`);
  });

  await test.step("It worked if: one Red child doesn't turn every ancestor Red", async () => {
    const org = chain.orgOutcome;
    await marcus.goto(`/goals/${org.id}`);
    await expect(marcus.getByTestId("goal-rollup-health"), `${org.title}'s Rolled-up Health`).toHaveText("Green");

    await page.goto(`/goals/${project.id}/checkin`);
    const form = page.getByTestId("checkin-form");
    await pickHealth(form, "Red");
    const path = form.getByTestId("path-to-green-field");
    await path.getByLabel("Plan").fill("Swap to the backup vendor; we need Platform's help to integrate.");
    await path.getByLabel("Back to Green by").fill(addDays(today, 42));
    await statusBox(form).fill("The vendor has pulled out.");
    await submit(form);
    await page.waitForURL(`**/goals/${project.id}`);
    await expect(page.getByTestId("goal-health")).toHaveText("Red");

    // The team Goal's Rolled-up Health is the worst among its direct Active
    // children; its Owner's Health stays as set.
    await marcus.goto(`/goals/${chain.team.id}`);
    await expect(marcus.getByTestId("goal-rollup-health")).toHaveText("Red");
    await expect(marcus.getByTestId("goal-health")).toHaveText("Green");

    // It worked if: the org outcome's Rolled-up Health reads the team Goal
    // Owner's Green, not Red. Rolled-up Health goes one level only
    // (RolledUpHealth, internal/domain/rollup.go): the org outcome reads the
    // team Goal Owner's Green, not the project's Red, and its other Active
    // children are Green (chooseBadNewsChain).
    await marcus.goto(`/goals/${org.id}`);
    await expect(marcus.getByTestId("goal-rollup-health"), `${org.title}'s Rolled-up Health`).toHaveText("Green");
  });
});

type Goal = {
  id: number;
  title: string;
  email: string;
  name: string;
  departed: number;
  lifecycle: string;
  kind: string;
  delivery_date: string;
  health: string | null;
};
type Link = { child_id: number; parent_id: number };
type Milestone = { id: number; goal_id: number; name: string; target_date: string; status: string };

type Chain = {
  // team is the project's Team, the Dimension value a Report rule matches.
  project: Goal & { milestones: Milestone[]; team: string };
  team: Goal;
  // orgOutcome is an org outcome the team Goal contributes to whose other
  // Active children are all Green, so it rolls up Green.
  orgOutcome: Goal;
};

type Lookup = <T>(sql: string) => T[];

// chooseBadNewsChain finds a project, the team Goal it contributes to and one
// of that Goal's org outcomes, meeting every rule the scenario needs. Health
// in the seed depends on its end date and time of day, so the chain is found
// at test time; when no chain meets a rule, the test fails naming it rather
// than using a weaker one. Goals whose Owner has departed still count as
// children, since Rolled-up Health counts them, but are never the project or
// the team Goal.
function chooseBadNewsChain(lookup: Lookup): Chain {
  const goals = lookup<Goal>(`
    select g.id, g.title, a.email, coalesce(a.name, a.email) name, a.departed,
           g.lifecycle, g.kind, g.delivery_date,
           (select c.health from checkins c where c.goal_id = g.id
            order by c.created_at desc, c.id desc limit 1) health
    from goals g join accounts a on a.id = g.owner_id`);
  const links = lookup<Link>(`select child_id, parent_id from links where status = 'accepted'`);
  const milestones = lookup<Milestone>(
    `select id, goal_id, name, target_date, status from milestones order by target_date, id`,
  );
  const teams = lookup<{ goal_id: number; value: string }>(`
    select gdv.goal_id, dv.value
    from goal_dimension_values gdv
    join dimension_values dv on dv.id = gdv.dimension_value_id
    join dimensions d on d.id = dv.dimension_id
    where d.name = 'Team'`);
  const teamOf = (g: Goal) => teams.find((t) => t.goal_id === g.id)?.value;
  const byID = new Map(goals.map((g) => [g.id, g]));
  const today = isoDate(new Date());
  const planned = (g: Goal) => milestones.filter((m) => m.goal_id === g.id && m.status === "Planned");
  const overdue = (g: Goal) => planned(g).some((m) => m.target_date < today);
  const children = (g: Goal) => links.filter((l) => l.parent_id === g.id).flatMap((l) => byID.get(l.child_id) ?? []);
  const parents = (g: Goal) => links.filter((l) => l.child_id === g.id).flatMap((l) => byID.get(l.parent_id) ?? []);
  const activeGreen = (g: Goal) => g.lifecycle === "Active" && g.health === "Green";
  // An org outcome rolls up Green over a Green team Goal when every other
  // Active child with a Health is Green too: Rolled-up Health is the worst
  // among them, so one Yellow sibling would keep it Yellow.
  const greenOrgOutcomes = (team: Goal) =>
    parents(team).filter((o) =>
      children(o).every(
        (c) => c.id === team.id || c.lifecycle !== "Active" || c.health === null || c.health === "Green",
      ),
    );

  type Candidate = { project: Goal; team: Goal };
  let candidates: Candidate[] = links.flatMap((l) => {
    const project = byID.get(l.child_id);
    const team = byID.get(l.parent_id);
    return project && team && !project.departed && !team.departed ? [{ project, team }] : [];
  });
  const rules: [string, (c: Candidate) => boolean][] = [
    ["the project is Active, Dated and Green", ({ project }) => activeGreen(project) && project.kind === "Dated"],
    [
      "the project has at least two Planned Milestones dated after today",
      ({ project }) => planned(project).filter((m) => m.target_date > today).length >= 2,
    ],
    ["the project has no overdue Planned Milestone", ({ project }) => !overdue(project)],
    ["the project has a Team", ({ project }) => teamOf(project) !== undefined],
    ["the team Goal is Active and Green", ({ team }) => activeGreen(team)],
    ["the team Goal has another Owner than the project", ({ project, team }) => project.email !== team.email],
    [
      "the team Goal's other Active children are all Green",
      ({ project, team }) =>
        children(team).every((c) => c.id === project.id || c.lifecycle !== "Active" || c.health === "Green"),
    ],
    // An overdue Milestone would refuse its Owner's Green on its own.
    ["the team Goal has no overdue Planned Milestone", ({ team }) => !overdue(team)],
    ["the team Goal contributes to an org outcome", ({ team }) => parents(team).length > 0],
    [
      "the team Goal contributes to an org outcome whose other Active children are all Green",
      ({ team }) => greenOrgOutcomes(team).length > 0,
    ],
  ];
  for (const [rule, meets] of rules) {
    candidates = candidates.filter(meets);
    if (candidates.length === 0) throw new Error(`no project and team Goal in the seed meet the rule: ${rule}`);
  }
  const { project, team } = candidates[0];
  return {
    project: {
      ...project,
      milestones: planned(project).filter((m) => m.target_date > today),
      team: teamOf(project) ?? "",
    },
    team,
    orgOutcome: greenOrgOutcomes(team)[0],
  };
}

// pickHealth picks a Health in the Check-in form. Its radios are hidden
// inside their labels, so the label is what a person clicks.
async function pickHealth(form: Locator, health: string): Promise<void> {
  await form.getByTestId("checkin-health").getByText(health, { exact: true }).click();
  await expect(form.getByRole("radio", { name: health })).toBeChecked();
}

// submit submits the Check-in form; htmx swaps a refusal in place.
async function submit(form: Locator): Promise<void> {
  await form.getByRole("button", { name: "Submit check-in" }).click();
}

// isoDate is d as YYYY-MM-DD in UTC, the server's timezone.
function isoDate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

// addDays is the YYYY-MM-DD date n days after date.
function addDays(date: string, n: number): string {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return isoDate(d);
}

// statusBox is the Check-in form's Status, which is required in the browser,
// so every submit fills it.
function statusBox(form: Locator): Locator {
  return form.getByRole("textbox", { name: /^Status/ });
}

// fieldError is the Check-in form's refusal shown under the field named name.
function fieldError(form: Locator, name: string): Locator {
  return form.locator(`label:has([name="${name}"]) + [data-testid="checkin-error"]`);
}

// milestoneRow is the Goal page's row for the Milestone named name.
function milestoneRow(page: Page, name: string): Locator {
  return page.getByTestId("goal-milestone").filter({ has: page.getByRole("cell", { name, exact: true }) });
}

// goalRow is the Goals list's row for the Goal titled title.
function goalRow(page: Page, title: string): Locator {
  return page.getByTestId("goal-row").filter({ has: page.getByRole("link", { name: title, exact: true }) });
}
