// The one-pick Goal picker (#656): the Goal page's Contribute to a Goal and the
// Suggest a parent page pick the parent the way the define page's
// Contributes to does, by a search whose results show each Goal's Health,
// title and Owner, but one Goal at a time, and with nothing picked until the
// person picks one. A submit with nothing picked is refused beside the picker.
import type { Locator, Page } from "@playwright/test";

import { admin, expect, requirePick, signIn, test } from "./fixtures";

// The manifest's quietGoal is an Active Goal with no accepted child Goals,
// whose Owner may link it under another.
type Goal = { id: number; title: string; email: string };

function picker(page: Page): Locator {
  return page.getByTestId("parent-picker");
}

// results types a letter into the picker's search and waits for its matches.
async function results(page: Page, label: string): Promise<Locator> {
  await picker(page).getByLabel(label).fill("e");
  const found = picker(page).getByTestId("parent-result");
  await expect(found.first()).toBeVisible();
  return found;
}

// titleOf is a search result's Goal title, as its pick shows it.
async function titleOf(result: Locator): Promise<string> {
  return (await result.locator("button > span:not(.badge):not(.muted)").textContent()) ?? "";
}

test.describe("the one-pick Goal picker", () => {
  let goal: Goal;

  test.beforeEach(async ({ manifest }) => {
    goal = requirePick("quietGoal", manifest.quietGoal);
  });

  test("Contribute to a Goal opens with nothing picked, picks one Goal, and requests the link", async ({ page }) => {
    await signIn(page, goal.email);
    await page.goto(`/goals/${goal.id}?open=parent-link`);
    const form = page.getByTestId("request-link");
    await expect(picker(page).locator("legend")).toHaveText("Contributes to");
    await expect(picker(page).getByTestId("parent-chip")).toHaveCount(0);

    await test.step("Request link with nothing picked is refused beside the picker", async () => {
      await form.getByRole("button", { name: "Request link" }).click();
      await expect(picker(page).getByTestId("input-error")).toHaveText("Pick a Goal to contribute to.");
      await expect(page.getByTestId("form-error")).toHaveCount(0);
      await expect(picker(page).getByTestId("parent-chip")).toHaveCount(0);
    });

    let second = "";
    await test.step("typing a letter lists Goals, each with its Health badge and Owner", async () => {
      const found = await results(page, "Search Goals to contribute to");
      const count = await found.count();
      expect(count, "the search finds more than one Goal to pick between").toBeGreaterThan(1);
      for (let i = 0; i < count; i++) {
        const pick = found.nth(i).getByRole("button");
        await expect(pick.locator(".badge")).toHaveCount(1);
        await expect(pick.locator(".muted")).not.toBeEmpty();
      }
    });

    await test.step("picking one shows its chip, and picking a second replaces it", async () => {
      let found = picker(page).getByTestId("parent-result");
      const first = await titleOf(found.first());
      await found.first().getByRole("button").click();
      const chips = picker(page).getByTestId("parent-chip");
      await expect(chips).toHaveCount(1);
      await expect(chips.locator(".gf-chip-title")).toHaveText(first);
      await expect(picker(page).getByLabel("Search Goals to contribute to")).toHaveValue("");
      await expect(picker(page).getByTestId("parent-result")).toHaveCount(0);

      found = await results(page, "Search Goals to contribute to");
      second = await titleOf(found.first());
      await found.first().getByRole("button").click();
      await expect(chips).toHaveCount(1);
      await expect(chips.locator(".gf-chip-title")).toHaveText(second);
    });

    await test.step("Request link lands on the Goal page with the form closed", async () => {
      await form.getByRole("button", { name: "Request link" }).click();
      await expect(page).toHaveURL(new RegExp(`/goals/${goal.id}$`));
      await expect(page.locator("[data-open-form]")).toHaveCount(0);
    });
  });

  test("Suggest a parent with nothing picked is refused beside the picker", async ({ as, manifest }) => {
    const someoneElse = Object.keys(manifest.accountIDs).find((email) => email !== goal.email && email !== admin);
    const other = await as(requirePick("an account other than quietGoal's Owner", someoneElse));
    await other.goto(`/goals/${goal.id}/suggest-parent`);
    await expect(picker(other).getByTestId("parent-chip")).toHaveCount(0);
    await other.getByTestId("suggest-parent").getByRole("button", { name: "Suggest" }).click();
    await expect(picker(other).getByTestId("input-error")).toHaveText("Pick a Goal for it to contribute to.");
    await expect(other.getByTestId("form-error")).toHaveCount(0);
  });
});
