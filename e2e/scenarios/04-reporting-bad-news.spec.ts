// Scenario 4, Reporting bad news (docs/scenarios.md): an Owner can't report
// trouble vaguely or bury it. Going Yellow needs a Path to Green, a later
// delivery date needs a reason and stays visible as a Date Slip, and the
// trouble reaches the parent's Owner as Rolled-up Health without turning
// every ancestor Red.
import { expect, type Locator, type Page, signIn, test } from "../fixtures";

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
});

type Goal = {
  id: number;
  title: string;
  email: string;
  name: string;
  lifecycle: string;
  kind: string;
  delivery_date: string;
  health: string | null;
};
type Link = { child_id: number; parent_id: number };
type Milestone = { id: number; goal_id: number; name: string; target_date: string; status: string };

type Chain = {
  project: Goal & { milestones: Milestone[] };
  team: Goal;
  orgOutcomes: Goal[];
};

type Lookup = <T>(sql: string) => T[];

// chooseBadNewsChain finds a project, the team Goal it contributes to and that
// Goal's org outcomes, meeting every rule the scenario needs. Health in the
// seed depends on its end date, so the chain is found at test time; when no
// chain meets a rule, the test fails naming it rather than using a weaker one.
function chooseBadNewsChain(lookup: Lookup): Chain {
  const goals = lookup<Goal>(`
    select g.id, g.title, a.email, coalesce(a.name, a.email) name, g.lifecycle, g.kind, g.delivery_date,
           (select c.health from checkins c where c.goal_id = g.id
            order by c.created_at desc, c.id desc limit 1) health
    from goals g join accounts a on a.id = g.owner_id
    where a.departed = 0`);
  const links = lookup<Link>(`select child_id, parent_id from links where status = 'accepted'`);
  const milestones = lookup<Milestone>(`select id, goal_id, name, target_date, status from milestones order by target_date, id`);
  const byID = new Map(goals.map((g) => [g.id, g]));
  const today = isoDate(new Date());
  const planned = (g: Goal) => milestones.filter((m) => m.goal_id === g.id && m.status === "Planned");
  const overdue = (g: Goal) => planned(g).some((m) => m.target_date < today);
  const children = (g: Goal) =>
    links.filter((l) => l.parent_id === g.id).flatMap((l) => byID.get(l.child_id) ?? []);
  const parents = (g: Goal) =>
    links.filter((l) => l.child_id === g.id).flatMap((l) => byID.get(l.parent_id) ?? []);
  const activeGreen = (g: Goal) => g.lifecycle === "Active" && g.health === "Green";

  type Candidate = { project: Goal; team: Goal };
  let candidates: Candidate[] = links.flatMap((l) => {
    const project = byID.get(l.child_id);
    const team = byID.get(l.parent_id);
    return project && team ? [{ project, team }] : [];
  });
  const rules: [string, (c: Candidate) => boolean][] = [
    ["the project is Active, Dated and Green", ({ project }) => activeGreen(project) && project.kind === "Dated"],
    [
      "the project has at least two Planned Milestones dated after today",
      ({ project }) => planned(project).filter((m) => m.target_date > today).length >= 2,
    ],
    ["the project has no overdue Planned Milestone", ({ project }) => !overdue(project)],
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
      "the team Goal's org outcomes aren't Red and have no Red Active child",
      ({ team }) =>
        parents(team).every(
          (o) => o.health !== "Red" && children(o).every((c) => c.lifecycle !== "Active" || c.health !== "Red"),
        ),
    ],
  ];
  for (const [rule, meets] of rules) {
    candidates = candidates.filter(meets);
    if (candidates.length === 0) throw new Error(`no project and team Goal in the seed meet the rule: ${rule}`);
  }
  const { project, team } = candidates[0];
  return {
    project: { ...project, milestones: planned(project).filter((m) => m.target_date > today) },
    team,
    orgOutcomes: parents(team),
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
  return page
    .getByTestId("goal-milestone")
    .filter({ has: page.getByRole("cell", { name, exact: true }) });
}
