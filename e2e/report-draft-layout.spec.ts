// A Report draft at 1440px: the narrative sits beside the Delegates and
// History, and the Goal sections span the page under both, wider than the
// narrative; each exception block's rail starts level with its title (#713).
import type { Page } from "@playwright/test";

import { admin, expect, signIn, test } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
});

// buildPlatformReport saves a Report of the Platform team's Goals, which the
// seed gives exceptions, and lands on its draft.
async function buildPlatformReport(page: Page): Promise<void> {
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  await builder.getByLabel("Name").fill("Platform layout");
  await builder.getByTestId("report-mode").getByText("Goals matching rules").click();
  const rule = builder.getByTestId("report-rule").first();
  await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
  await rule.getByLabel("Operator").selectOption({ label: "is any of" });
  await rule.getByTestId("rule-values").getByRole("checkbox", { name: "Platform", exact: true }).check();
  await builder.getByRole("button", { name: "Save report" }).click();
  await page.waitForURL(/\/reports\/\d+$/);
}

test("at 1440px a draft's Goal sections span the page under its sidebar", async ({ page }) => {
  await signIn(page, admin);
  await buildPlatformReport(page);
  const main = (await page.getByTestId("report-draft").boundingBox())!;
  const side = (await page.getByTestId("draft-side").boundingBox())!;
  const exceptions = (await page.getByTestId("needs-attention").boundingBox())!;
  expect(side.x, "the Delegates and History sit beside the narrative").toBeGreaterThanOrEqual(main.x + main.width);
  expect(exceptions.y, "the Exceptions follow the narrative").toBeGreaterThanOrEqual(main.y + main.height);
  expect(exceptions.width, "the Exceptions are wider than the narrative").toBeGreaterThan(main.width);
  expect(exceptions.x + exceptions.width, "the Exceptions run under the sidebar").toBeGreaterThan(side.x);

  const block = page.getByTestId("report-exception").first();
  const title = (await block.locator("h3").boundingBox())!;
  const owner = (await block.getByTestId("report-rail").locator("dt").first().boundingBox())!;
  expect(owner.x, "the rail sits right of the title").toBeGreaterThan(title.x + title.width);
  expect(Math.abs(owner.y - title.y), "the rail's Owner is level with the title").toBeLessThan(title.height);
});
