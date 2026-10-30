// The harness's isolation check: two tests write to the same seeded Goal at
// once, and each sees only its own write. Each test has its own database and
// server (the app fixture), and fullyParallel runs these two side by side.
import type { Page } from "@playwright/test";

import { expect, requirePick, signIn, test } from "./fixtures";

// The manifest's noChangeGoal is a Goal whose Owner can press No change:
// Active, checked in before, no accepted child Goals (so no Rolled-up Health
// to refuse a repeat), no overdue Planned Milestone, and no back-to-Green date
// passed on its latest Check-in. Both tests read the same pick.
for (const name of ["first", "second"]) {
  test(`No change adds exactly one History entry (${name} of two)`, async ({ page, app, manifest }) => {
    const goal = requirePick("noChangeGoal", manifest.noChangeGoal);

    await signIn(page, goal.email);
    await page.goto(`/goals/${goal.id}`);
    const before = await historyCount(page);
    // No Check-in was written minutes ago: the seed's are hours old, so one
    // that recent is the other test's, landed in this test's database.
    expect(await justCheckedIn(page, app.now()), "the Goal's latest entry isn't another test's Check-in").toBe(false);

    await page.getByTestId("goal-actions").getByRole("button", { name: "Check in: no change" }).click();

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
