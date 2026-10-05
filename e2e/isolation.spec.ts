// The harness's isolation check: two tests write to the same seeded Goal at
// once, and each sees only its own write. Each test has its own database and
// server (the app fixture), and fullyParallel runs these two side by side.
import type { Page } from "@playwright/test";

import { expect, signIn, test } from "./fixtures";

// A Goal whose Owner can press No change: Active, checked in before, no
// accepted contributors (so no Rolled-up Health to refuse a repeat) and no
// overdue Planned Milestone. The same query picks the same Goal in both
// tests.
const noChangeGoal = `
  select g.id, a.email
  from goals g join accounts a on a.id = g.owner_id
  where g.lifecycle = 'Active' and a.departed = 0
    and exists (select 1 from checkins c where c.goal_id = g.id)
    and not exists (select 1 from links l where l.parent_id = g.id and l.status = 'accepted')
    and not exists (select 1 from milestones m
                    where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
  order by g.id
  limit 1`;

for (const name of ["first", "second"]) {
  test(`No change adds exactly one History entry (${name} of two)`, async ({ page, app, seedLookup }) => {
    const [goal] = seedLookup<{ id: number; email: string }>(noChangeGoal);
    expect(goal, "the seed has a Goal its Owner can press No change on").toBeDefined();

    await signIn(page, goal.email);
    await page.goto(`/goals/${goal.id}`);
    const before = await historyCount(page);
    // No Check-in was written minutes ago: the seed's are hours old, so one
    // that recent is the other test's, landed in this test's database.
    expect(await justCheckedIn(page, app.now()), "the Goal's latest entry isn't another test's Check-in").toBe(false);

    await page.getByTestId("goal-actions").getByRole("button", { name: "No change" }).click();

    await expect(allChip(page)).toHaveAccessibleName(`All ${before + 1}`);
    // The Check-in it wrote is the Goal's latest, whatever the time of day.
    await expect(page.getByTestId("history-entry").first()).toHaveAttribute("data-kind", "checkin");
    expect(await justCheckedIn(page, app.now()), "the latest entry is the Check-in just written").toBe(true);
  });
}

// justCheckedIn reports whether the Goal's latest History entry is a Check-in
// written in the 5 minutes before now, the app's time.
async function justCheckedIn(page: Page, now: Date): Promise<boolean> {
  const latest = page.getByTestId("history-entry").first();
  if ((await latest.getAttribute("data-kind")) !== "checkin") return false;
  const at = Date.parse((await latest.locator("time").getAttribute("datetime")) ?? "");
  return Math.abs(now.getTime() - at) < 5 * 60 * 1000;
}

// allChip is the History's All chip, which counts every entry, shown or not.
function allChip(page: Page) {
  return page.getByRole("navigation", { name: "Show in History" }).getByRole("link", { name: /^All \d+$/ });
}

async function historyCount(page: Page): Promise<number> {
  const name = (await allChip(page).textContent()) ?? "";
  return Number(name.replace(/\D+/g, ""));
}
