// A Report rule's values list holds only the values of what the rule tests,
// as plain options with no group heading: choosing Team offers the Teams and
// nothing else, and switching to Health swaps them for the Healths. The other
// attributes' values aren't merely hidden: a phone's picker shows a hidden
// option.
import { admin, expect, signIn, test } from "./fixtures";

test("a rule's values are only those of what it tests", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/reports/new");
  const rule = page.getByTestId("report-rule").first();
  const attribute = rule.getByLabel("What the rule tests");
  const values = rule.getByLabel("Values");

  for (const label of ["Team", "Health", "Owner"]) {
    await attribute.selectOption({ label });
    const key = await attribute.inputValue();
    await expect(values.locator("optgroup")).toHaveCount(0);
    const offered = await values.locator("option").evaluateAll((options) =>
      options.map((o) => (o as HTMLOptionElement).value),
    );
    expect(offered.length, `${label} offers values`).toBeGreaterThan(0);
    for (const value of offered) expect(value, `a value offered for ${label}`).toMatch(new RegExp(`^${key}=`));
  }
});
