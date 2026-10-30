// The Goal page's Edit Metric (#655) asks for a Direction only when the
// target equals the baseline, as the goal form does: with script on, the
// Direction field shows only while the baseline and target hold equal
// numbers. Without script it always shows, and the server counts it only when
// they're equal.
import type { Locator, Page } from "@playwright/test";

import { expect, requirePick, signIn, test } from "./fixtures";

// The manifest's metricGoal is an Active Goal with a Metric, whose Owner is
// still here to edit it.
async function openEditMetric(page: Page, goal: { id: number }): Promise<Locator> {
  await page.goto(`/goals/${goal.id}`);
  const edit = page.getByTestId("goal-metric").first().getByTestId("metric-edit");
  await edit.getByText("Edit Metric").click();
  await expect(edit.getByRole("button", { name: "Save" })).toBeVisible();
  return edit;
}

test("Edit Metric shows the Direction only while target equals baseline", async ({ page, manifest }) => {
  const goal = requirePick("metricGoal", manifest.metricGoal);
  await signIn(page, goal.email);
  const edit = await openEditMetric(page, goal);
  const baseline = edit.getByLabel("Baseline", { exact: true });
  const target = edit.getByLabel("Target", { exact: true });
  const direction = edit.getByLabel(/^Direction/);

  await baseline.fill("10");
  await target.fill("20");
  await expect(direction).toBeHidden();

  await target.fill("10");
  await expect(direction).toBeVisible();
  await expect(edit.getByText("— when target equals baseline")).toBeVisible();

  await baseline.fill("10.5");
  await expect(direction).toBeHidden();
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("Edit Metric always shows the Direction", async ({ page, manifest }) => {
    const goal = requirePick("metricGoal", manifest.metricGoal);
    await signIn(page, goal.email);
    const edit = await openEditMetric(page, goal);
    await edit.getByLabel("Baseline", { exact: true }).fill("10");
    await edit.getByLabel("Target", { exact: true }).fill("20");
    await expect(edit.getByLabel(/^Direction/)).toBeVisible();
  });
});
