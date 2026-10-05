// The harness's isolation check: two tests write to the same seeded Goal at
// once, and each sees only its own write. Each test has its own database and
// server (the app fixture), and fullyParallel runs these two side by side.
import { expect, type Page, signIn, test } from "./fixtures";

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
                    where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date('now'))
  order by g.id
  limit 1`;

for (const name of ["first", "second"]) {
  test(`No change adds exactly one History entry (${name} of two)`, async ({ page, seedLookup }) => {
    const [goal] = seedLookup<{ id: number; email: string }>(noChangeGoal);
    expect(goal, "the seed has a Goal its Owner can press No change on").toBeDefined();

    await signIn(page, goal.email);
    await page.goto(`/goals/${goal.id}`);
    const before = await historyCount(page);

    await page.getByTestId("goal-actions").getByRole("button", { name: "No change" }).click();

    await expect(allChip(page)).toHaveAccessibleName(`All ${before + 1}`);
    // The Check-in it wrote is the Goal's latest, whatever the time of day.
    const latest = page.getByTestId("history-entry").first();
    await expect(latest).toHaveAttribute("data-kind", "checkin");
    const at = Date.parse((await latest.locator("time").getAttribute("datetime")) ?? "");
    expect(Math.abs(Date.now() - at), "the latest entry is the Check-in just written").toBeLessThan(5 * 60 * 1000);
  });
}

// allChip is the History's All chip, which counts every entry, shown or not.
function allChip(page: Page) {
  return page.getByRole("navigation", { name: "Show in History" }).getByRole("link", { name: /^All \d+$/ });
}

async function historyCount(page: Page): Promise<number> {
  const name = (await allChip(page).textContent()) ?? "";
  return Number(name.replace(/\D+/g, ""));
}
