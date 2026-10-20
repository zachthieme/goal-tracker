// What each Follow-ups signal means: each signal chip in the table names its
// signal with a button opening a popover saying what it means, and "What each
// signal means" beside the count opens them all. A tap opens one and a second
// tap, Escape or a tap outside closes it; at 390px an open one stays within
// the window, with no sideways scroll (#378).
import type { Locator, Page } from "@playwright/test";

import { expect, signIn, test } from "./fixtures";

const phone = { width: 390, height: 800 };

// expectWithinWindow asserts that pop lies inside the window and that the
// page doesn't scroll sideways.
async function expectWithinWindow(page: Page, pop: Locator): Promise<void> {
  const box = (await pop.boundingBox())!;
  const { width, height } = page.viewportSize()!;
  expect(box.x, "the popover's left is inside the window").toBeGreaterThanOrEqual(0);
  expect(box.x + box.width, "the popover's right is inside the window").toBeLessThanOrEqual(width);
  expect(box.y, "the popover's top is inside the window").toBeGreaterThanOrEqual(0);
  expect(box.y + box.height, "the popover's bottom is inside the window").toBeLessThanOrEqual(height);
  const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(scrollWidth, "the page scrolls sideways").toBeLessThanOrEqual(width);
}

for (const viewport of [phone, { width: 1280, height: 800 }]) {
  test(`at ${viewport.width}px a signal chip opens what it means within the window`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await signIn(page, "ceo@example.com");
    await page.goto("/follow-ups");
    const tips = page.locator('[data-testid="follow-up-signal"] button.rk-tip');
    await expect(tips.first()).toBeVisible();
    for (const tip of await tips.all()) {
      const pop = page.locator(`#${await tip.getAttribute("popovertarget")}`);
      await tip.click();
      await expect(pop).toBeVisible();
      await expectWithinWindow(page, pop);
      await tip.click();
      await expect(pop).toBeHidden();
    }

    const tip = tips.first();
    const pop = page.locator(`#${await tip.getAttribute("popovertarget")}`);
    await tip.focus();
    await page.keyboard.press("Enter");
    await expect(pop).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(pop).toBeHidden();
  });

  test(`at ${viewport.width}px What each signal means opens every definition within the window`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await signIn(page, "ceo@example.com");
    await page.goto("/follow-ups");
    const pop = page.locator("#follow-ups-meanings");
    await page.getByRole("button", { name: "What each signal means" }).click();
    await expect(pop).toBeVisible();
    await expect(pop.locator("dt")).toHaveCount(11);
    await expectWithinWindow(page, pop);
    await page.mouse.click(2, viewport.height - 2);
    await expect(pop).toBeHidden();
  });
}
