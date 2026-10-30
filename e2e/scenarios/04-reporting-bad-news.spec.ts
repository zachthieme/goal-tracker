// Scenario 4, Reporting bad news (docs/scenarios.md): an Owner can't report
// trouble vaguely or bury it. Going Yellow needs a Path to Green, a later
// delivery date needs a reason and stays visible as a Date Slip, and the
// trouble reaches the parent's Owner as Rolled-up Health without turning
// every ancestor Red.
import type { Locator, Page } from "@playwright/test";
import { appToday, expect, requirePick, signIn, test } from "../fixtures";

// The scenario's people are roles. "Priya" is the project's Owner and
// "Marcus" the Owner of the team Goal it contributes to; both are the seed's
// manifest's badNewsChain: a project, Active, Dated and Green, with at least
// two Planned Milestones ahead and none overdue, and a Team; the team Goal it
// contributes to, Active and Green, another Owner's, whose other Active
// children are all Green and with no overdue Planned Milestone; and an org
// outcome the team Goal contributes to whose other Active children are all
// Green, so it rolls up Green. Owners who have departed are never the project
// or the team Goal.
test("an Owner reports bad news: Path to Green, Date Slips, Rolled-up Health", async ({ page, as, manifest }) => {
  test.slow();
  const chain = requirePick("badNewsChain", manifest.badNewsChain);
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
    // Both problems come back at once, each beside its field, which is
    // marked invalid.
    await expect(path.getByTestId("checkin-error")).toHaveText([
      "A Yellow Goal needs a Plan.",
      "A Path to Green needs a Back to Green by date.",
    ]);
    await expect(path.getByLabel("Plan")).toHaveAttribute("aria-invalid", "true");
    await expect(path.getByLabel("Back to Green by")).toHaveAttribute("aria-invalid", "true");
    await expect(form.getByRole("radio", { name: "Yellow" })).toBeChecked();
    await expect(statusBox(form)).toHaveValue("A vendor delay puts us behind.");

    await path.getByLabel("Plan").fill("Escalate with the vendor and ship behind a flag.");
  });

  const today = appToday();
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
    await giveBackToGreenReason(path, "The vendor's API slipped three weeks.");

    const dates = form.getByTestId("checkin-dates-section");
    await page.getByTestId("checkin-add-bar").getByRole("button", { name: "Move a date or Milestone" }).click();
    await dates.getByLabel("Delivery date", { exact: true }).fill(newDue);
    await submit(form);
    await expect(fieldError(form, "delivery_date_reason")).toContainText("Changing the delivery date needs a reason");

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
    await expect(due.locator("del")).toHaveText(shownDate(oldDue));
    await expect(due).toHaveText(`${shownDate(oldDue)} ${shownDate(newDue)}`);

    await expect(milestoneRow(page, slipped.name).getByTestId("milestone-mark")).toHaveText("Slipped");
    await expect(milestoneRow(page, done.name).getByTestId("milestone-mark")).toHaveText("Done");

    const latest = page.getByTestId("history-entry").first();
    await expect(latest).toHaveAttribute("data-kind", "checkin");
    await expect(latest).toContainText(project.name);
    const slips = latest.getByTestId("entry-slips").getByTestId("date-slip");
    await expect(slips).toHaveText([
      `Delivery date: ${shownDate(oldDue)} ${shownDate(newDue)} — ${dueReason}`,
      `Milestone ${slipped.name}: ${shownDate(slipped.target_date)} ${shownDate(slippedTo)} — ${slipReason}`,
    ]);
    await expect(latest.getByTestId("milestone-change")).toHaveText([`Marked ${done.name} Done`]);
  });

  const marcus = await as(chain.team.email);

  await test.step("It worked if: the slip shows on the Goals list and in a Report draft", async () => {
    // It worked if: the slip shows wherever the date does — the Goals list.
    await page.goto("/goals");
    const due = goalRow(page, project.title).getByTestId("goal-row-due");
    await expect(due.locator("del")).toHaveText(shownDate(oldDue));
    await expect(due).toHaveText(`${shownDate(oldDue)} ${shownDate(newDue)}`);

    // ...and a Report draft over the project, built by the team Goal's Owner
    // with a rule on the project's Team.
    await marcus.goto("/reports/new");
    const builder = marcus.getByTestId("report-builder");
    await builder.getByLabel("Name").fill("Bad news this week");
    const rule = builder.getByTestId("report-rule").first();
    await rule.getByTestId("rule-attribute").selectOption({ label: "Team" });
    await rule.getByTestId("rule-values").getByRole("radio", { name: project.team, exact: true }).check();
    await builder.getByRole("button", { name: "Save report" }).last().click();
    await marcus.waitForURL(/\/reports\/\d+$/);
    const block = marcus
      .getByTestId("report-exception")
      .filter({ has: marcus.getByRole("link", { name: project.title, exact: true }) });
    const reportDue = block.getByTestId("report-due");
    await expect(reportDue.locator("del")).toHaveText(reportDate(oldDue));
    await expect(reportDue).toHaveText(`${reportDate(oldDue)} ${reportDate(newDue)}`);
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
    // With script, the Explanation is asked only while the picked Health
    // differs from the Rolled-up Health.
    const explanation = form.getByTestId("checkin-explanation");
    for (const [health, asked] of [
      ["Yellow", false],
      ["Red", true],
      ["Yellow", false],
      ["Green", true],
    ] as const) {
      await pickHealth(form, health);
      await expect(explanation).toBeVisible({ visible: asked });
    }

    // What was typed stays while it's hidden.
    await explanation.fill("Typed before matching.");
    await pickHealth(form, "Yellow");
    await expect(explanation).toBeHidden();
    await pickHealth(form, "Green");
    await expect(explanation).toHaveValue("Typed before matching.");

    await statusBox(form).fill("On track overall.");
    await explanation.fill("");
    await submit(form);
    await expect(fieldError(form, "explanation")).toContainText("differs from the Rolled-up Health (Yellow)");
    await expect(explanation).toBeVisible();

    const why = "The vendor slip is covered by the project's plan; the team Goal still lands.";
    await form.getByTestId("checkin-explanation").fill(why);
    await submit(form);
    await marcus.waitForURL(`**/goals/${chain.team.id}`);
    await expect(marcus.getByTestId("goal-health")).toHaveText("Green");
    await expect(marcus.getByTestId("goal-rollup-explanation")).toContainText(why);
  });

  await test.step("4.6 a passed back-to-Green date is flagged on Follow-ups and on the Goals list", async () => {
    // Two days ago, so it has passed in any org timezone.
    await page.goto(`/goals/${project.id}/checkin`);
    const form = page.getByTestId("checkin-form");
    await expect(form.getByRole("radio", { name: "Yellow" })).toBeChecked();
    // The Plan starts empty, with no ghost text, so she writes it again.
    const path = form.getByTestId("path-to-green-field");
    const plan = "Escalate with the vendor and ship behind a flag.";
    await expect(path.getByLabel("Plan")).toHaveValue("");
    await expect(path.getByLabel("Plan")).not.toHaveAttribute("placeholder");
    await path.getByLabel("Plan").fill(plan);
    await path.getByLabel("Back to Green by").fill(addDays(today, -2));
    await giveBackToGreenReason(path, "Still waiting on the vendor.");
    await statusBox(form).fill("Still waiting on the vendor.");
    await submit(form);
    await page.waitForURL(`**/goals/${project.id}`);

    await marcus.goto("/follow-ups");
    await marcus.getByTestId("follow-ups-group-owner").click();
    await expect(marcus).toHaveURL(/[?&]group=owner\b/);
    const row = marcus
      .getByTestId("follow-up-row")
      .filter({ has: marcus.getByRole("link", { name: project.title, exact: true }) });
    await expect(row.getByTestId("follow-up-owner")).toContainText(project.name);
    await expect(row.locator('[data-testid="follow-up-signal"][data-kind="path-overdue"]')).toHaveText(
      /^Path to Green \d+ days? overdue$/,
    );
    await expect(row.getByTestId("follow-up-fix")).toHaveCount(1);

    // On everyone's Goals list it sorts with the Check-in overdue and overdue
    // Goals, a Green one marked Milestone overdue among them, above every
    // Yellow and Green Goal without a mark.
    await page.goto("/goals?org=all");
    await expect(goalRow(page, project.title).getByTestId("path-overdue")).toBeVisible();
    const rows = await page.getByTestId("goal-row").evaluateAll((trs) =>
      trs.map((tr) => ({
        title: tr.querySelector("td:nth-child(2) a")?.textContent?.trim() ?? "",
        health: tr.querySelector('[data-testid="goal-row-health"]')?.textContent?.trim() ?? "",
        marked:
          tr.querySelector(
            '[data-testid="ownerless"], [data-testid="checkin-overdue"], [data-testid="path-overdue"], [data-testid="milestone-overdue"]',
          ) !== null,
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

    // The define page, where a Proposed Goal's date is set, sends an Active
    // Goal back to its page.
    await page.goto(`/goals/${project.id}/define`);
    await expect(page).toHaveURL(new RegExp(`/goals/${project.id}$`));
    await expect(page.getByTestId("goal-delivery-date")).toHaveText(`${shownDate(oldDue)} ${shownDate(newDue)}`);
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
    // Later than the date that passed: a Date Slip, so the form asks why.
    await giveBackToGreenReason(path, "The backup vendor needs six weeks to integrate.");
    await statusBox(form).fill("The vendor has pulled out.");
    await submit(form);
    await page.waitForURL(`**/goals/${project.id}`);
    await expect(page.getByTestId("goal-health")).toHaveText("Red");

    // The team Goal's Rolled-up Health is the lowest among its direct Active
    // children; its Owner's Health stays as set.
    await marcus.goto(`/goals/${chain.team.id}`);
    await expect(marcus.getByTestId("goal-rollup-health")).toHaveText("Red");
    await expect(marcus.getByTestId("goal-health")).toHaveText("Green");

    // It worked if: the org outcome's Rolled-up Health reads the team Goal
    // Owner's Green, not Red. Rolled-up Health goes one level only
    // (RolledUpHealth, internal/domain/rollup.go): the org outcome reads the
    // team Goal Owner's Green, not the project's Red, and its other Active
    // children are Green (the manifest's badNewsChain).
    await marcus.goto(`/goals/${org.id}`);
    await expect(marcus.getByTestId("goal-rollup-health"), `${org.title}'s Rolled-up Health`).toHaveText("Green");
  });
});

// pickHealth picks a Health in the Check-in form. Its radios are hidden
// inside their labels, so the label is what a person clicks.
async function pickHealth(form: Locator, health: string): Promise<void> {
  await form.getByTestId("checkin-health").getByTitle(health, { exact: true }).click();
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

// months are the month names the pages write dates with.
const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// shownDate is the YYYY-MM-DD date as the pages show it: "Nov 7", with its
// year when it isn't the app's current year, "Nov 7, 2027".
function shownDate(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  const day = `${months[m - 1]} ${d}`;
  return String(y) === appToday().slice(0, 4) ? day : `${day}, ${y}`;
}

// reportDate is the YYYY-MM-DD date as a Report shows it, always with its
// year: "Nov 7, 2026".
function reportDate(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  return `${months[m - 1]} ${d}, ${y}`;
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
  return form.getByRole("textbox", { name: /^What changed since the last check-in/ });
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

// giveBackToGreenReason fills the Path to Green's "Reason for moving it later"
// when the form shows it: a Back to Green by date later than the last
// Check-in's is a Date Slip, which needs a reason.
async function giveBackToGreenReason(path: Locator, reason: string) {
  const field = path.getByLabel("Reason for moving it later");
  if (await field.isVisible()) await field.fill(reason);
}
