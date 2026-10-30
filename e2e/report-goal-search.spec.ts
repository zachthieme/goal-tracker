// A Report builder picker's search offers only a pick that changes
// something, read from the rules as they stand (#658), so an open Also
// include or Leave out result list is searched again when a rule changes
// (#694): it never offers a Goal the rules no longer select or leave out,
// whether a rule's value changes, + Add rule adds a row or × removes one.
// A list whose box is empty closes instead, and one that isn't open sends
// nothing.
import { admin, expect, signIn, test } from "./fixtures";

test("an open Leave out search follows a rule change", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  const rule = builder.getByTestId("report-rule").first();
  const values = rule.getByTestId("rule-values");
  const leaveOut = builder.getByTestId("exclude-picker");
  const results = leaveOut.getByTestId("exclude-results");
  const titles = results.getByTestId("report-goal-result-title");
  const searches: string[] = [];
  page.on("request", (request) => {
    if (request.url().includes("/reports/goals/search")) searches.push(new URL(request.url()).searchParams.get("list") ?? "");
  });

  await rule.getByLabel("What the rule tests").selectOption({ label: "Owner" });
  await rule.getByLabel("Operator").selectOption({ label: "is" });
  await values.getByRole("radio", { name: "Priya Raman", exact: true }).check();
  await leaveOut.getByRole("searchbox", { name: "Search Goals: Leave out" }).fill("Grow");
  await expect(results).toHaveText("No Goals match.");

  // The Goal's Owner is Dana Whitfield: once the rule selects her Goals,
  // Leave out offers it without another keystroke.
  await values.getByRole("radio", { name: "Dana Whitfield", exact: true }).check();
  await expect(titles).toHaveText(["Grow net revenue retention"]);

  await values.getByRole("radio", { name: "Priya Raman", exact: true }).check();
  await expect(results).toHaveText("No Goals match.");

  // Emptied, the box lists every Goal the rule selects; a rule change then
  // closes the list rather than search for everything again.
  await leaveOut.getByRole("searchbox", { name: "Search Goals: Leave out" }).fill("");
  await expect(titles).not.toHaveCount(0);
  const sent = searches.length;
  await values.getByRole("radio", { name: "Dana Whitfield", exact: true }).check();
  await expect(results.locator("li")).toHaveCount(0);
  expect(searches.length).toBe(sent);
  expect(searches).not.toContain("include");
});

test("an open Also include search follows a rule change", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  const rule = builder.getByTestId("report-rule").first();
  const values = rule.getByTestId("rule-values");
  const alsoInclude = builder.getByTestId("include-picker");
  const results = alsoInclude.getByTestId("include-results");
  const titles = results.getByTestId("report-goal-result-title");

  // The Goal's Owner is Dana Whitfield: Also include offers it while the
  // rule leaves it out, and not once the rule selects it.
  await rule.getByLabel("What the rule tests").selectOption({ label: "Owner" });
  await rule.getByLabel("Operator").selectOption({ label: "is" });
  await values.getByRole("radio", { name: "Priya Raman", exact: true }).check();
  await alsoInclude.getByRole("searchbox", { name: "Search Goals: Also include" }).fill("Grow");
  await expect(titles).toHaveText(["Grow net revenue retention"]);

  await values.getByRole("radio", { name: "Dana Whitfield", exact: true }).check();
  await expect(results).toHaveText("No Goals match.");

  await values.getByRole("radio", { name: "Priya Raman", exact: true }).check();
  await expect(titles).toHaveText(["Grow net revenue retention"]);
});

test("an open search follows + Add rule and ×", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  const rules = builder.getByTestId("report-rule");
  const first = rules.first();
  const alsoInclude = builder.getByTestId("include-picker");
  const results = alsoInclude.getByTestId("include-results");
  const titles = results.getByTestId("report-goal-result-title");

  // Dana Whitfield's Top-level Goal is selected by the first rule, so Also
  // include doesn't offer it.
  await first.getByLabel("What the rule tests").selectOption({ label: "Owner" });
  await first.getByLabel("Operator").selectOption({ label: "is" });
  await first.getByTestId("rule-values").getByRole("radio", { name: "Dana Whitfield", exact: true }).check();
  await alsoInclude.getByRole("searchbox", { name: "Search Goals: Also include" }).fill("Grow");
  await expect(results).toHaveText("No Goals match.");

  // + Add rule adds a blank row, which selects nothing new, but the open
  // list is searched again with it all the same.
  const searchedAgain = page.waitForRequest(
    (request) =>
      request.url().includes("/reports/goals/search") &&
      new URL(request.url()).searchParams.get("list") === "include",
    { timeout: 5000 },
  );
  await builder.getByRole("button", { name: "+ Add rule" }).click();
  await expect(rules).toHaveCount(2);
  await searchedAgain;
  await expect(results).toHaveText("No Goals match.");

  // Once the added rule drops Top-level Goals, Also include offers it.
  const added = rules.nth(1);
  await added.getByLabel("What the rule tests").selectOption({ label: "Top-level" });
  await added.getByLabel("Operator").selectOption({ label: "is not Top-level" });
  await expect(titles).toHaveText(["Grow net revenue retention"]);

  // × on that rule selects the Goal again, so Also include no longer offers it.
  await added.getByRole("button", { name: "Remove rule" }).click();
  await expect(rules).toHaveCount(1);
  await expect(results).toHaveText("No Goals match.");
});
