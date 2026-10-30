// A Report rule's values are only those of what the rule tests, as labelled
// radios under "is", which takes one, and checkboxes under "is any of" and
// "is not": choosing Team offers the Teams and nothing else, and switching to
// Health swaps them for the Healths. The other attributes' values aren't
// merely hidden. Switching the operator turns one kind into the other,
// keeping what is chosen, and back to "is" keeps the first only. + Add rule
// and × change the rows in place, refreshing Matches.
import { admin, expect, signIn, test } from "./fixtures";

test("a rule's values are only those of what it tests", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/reports/new");
  const rule = page.getByTestId("report-rule").first();
  const attribute = rule.getByLabel("What the rule tests");
  const values = rule.getByTestId("rule-values");

  for (const label of ["Team", "Health", "Owner"]) {
    await attribute.selectOption({ label });
    const key = await attribute.inputValue();
    await expect(values.getByTestId("rule-value-group")).toHaveCount(0);
    const offered = await values
      .locator("input")
      .evaluateAll((inputs) => inputs.map((i) => (i as HTMLInputElement).value));
    expect(offered.length, `${label} offers values`).toBeGreaterThan(0);
    for (const value of offered) expect(value, `a value offered for ${label}`).toMatch(new RegExp(`^${key}=`));
  }

  await attribute.selectOption({ label: "Top-level" });
  await expect(values).toBeHidden();
  await expect(values.locator("input")).toHaveCount(0);
});

test("a rule's values are radios under is and checkboxes otherwise, keeping what is chosen", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/reports/new");
  const rule = page.getByTestId("report-rule").first();
  const operator = rule.getByLabel("Operator");
  const values = rule.getByTestId("rule-values");
  const kinds = () =>
    values.locator("input").evaluateAll((inputs) => [...new Set(inputs.map((i) => (i as HTMLInputElement).type))]);

  await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
  await operator.selectOption({ label: "is" });
  expect(await kinds()).toEqual(["radio"]);
  const [first, second] = [values.getByRole("radio").nth(0), values.getByRole("radio").nth(1)];
  const [firstName, secondName] = [await first.inputValue(), await second.inputValue()];
  const byValue = (value: string) => values.locator(`input[value="${value}"]`);
  await byValue(firstName).check();

  await operator.selectOption({ label: "is any of" });
  expect(await kinds()).toEqual(["checkbox"]);
  await expect(byValue(firstName)).toBeChecked();
  await byValue(secondName).check();

  await operator.selectOption({ label: "is" });
  expect(await kinds()).toEqual(["radio"]);
  await expect(byValue(firstName)).toBeChecked();
  await expect(byValue(secondName)).not.toBeChecked();
});

test("+ Add rule and × change the rows in place", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  const rules = builder.getByTestId("report-rule");
  const count = page.getByTestId("report-matches").getByTestId("matches-count");
  const first = rules.first();
  await first.getByLabel("What the rule tests").selectOption({ label: "Team" });
  await first.getByLabel("Operator").selectOption({ label: "is any of" });
  await first.getByTestId("rule-values").getByRole("checkbox", { name: "Platform", exact: true }).check();
  await expect(count).toHaveText(/^Matches \d+ Goals?$/);
  const platform = await count.textContent();
  await expect(rules).toHaveCount(1);

  await page.evaluate(() => {
    (window as unknown as { builderMarker: string }).builderMarker = "still here";
  });
  const add = builder.getByRole("button", { name: "+ Add rule" });
  await add.click();
  await expect(rules).toHaveCount(2);
  const added = rules.nth(1);
  await expect(added.getByLabel("What the rule tests")).toBeFocused();
  expect(await page.evaluate(() => (window as unknown as { builderMarker?: string }).builderMarker)).toBe("still here");
  await expect(added.getByTestId("rule-values")).toBeHidden();

  await added.getByLabel("What the rule tests").selectOption({ label: "Top-level" });
  await expect(count).not.toHaveText(platform ?? "");

  await added.getByRole("button", { name: "Remove rule" }).click();
  await expect(rules).toHaveCount(1);
  await expect(add).toBeFocused();
  await expect(count).toHaveText(platform ?? "");
  expect(await page.evaluate(() => (window as unknown as { builderMarker?: string }).builderMarker)).toBe("still here");
});
