// The flat frame's sidebar (#390): at 1280px it is a column beside the page,
// and at 390px it is a 48px strip whose Menu opens the nav, My org and the
// person's menu with no script, and nothing scrolls sideways.
import type { Page } from "@playwright/test";

import { expect, signIn, test } from "./fixtures";

// scrollsSideways reports whether the page is wider than the window.
async function scrollsSideways(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
}

test("at 1280px the sidebar shows the nav, My org and the person's menu", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await signIn(page, "cto@example.com");
  const sidebar = page.locator("header.sidebar");
  await expect(sidebar.getByRole("link", { name: "Goal Tracker" })).toBeVisible();
  await expect(sidebar.getByRole("searchbox", { name: "Search Goals" })).toBeVisible();
  await expect(page.getByTestId("nav-home")).toBeVisible();
  await expect(page.getByTestId("sidebar-org")).toBeVisible();
  await expect(sidebar.getByRole("button", { name: "Menu" })).toBeHidden();

  await page.getByTestId("person-menu").locator("summary").click();
  await expect(page.getByTestId("theme-dark")).toBeVisible();
  await expect(sidebar.getByRole("button", { name: "Sign out" })).toBeVisible();
});

test("the sidebar's Search asks the Goal list", async ({ page }) => {
  await signIn(page, "cto@example.com");
  const search = page.getByRole("searchbox", { name: "Search Goals" });
  await search.fill("revenue");
  await search.press("Enter");
  await expect(page).toHaveURL(/\/goals\?q=revenue$/);
  await expect(page.getByTestId("goal-search")).toHaveValue("revenue");
});

test.describe("at 390px with no script", () => {
  test.use({ javaScriptEnabled: false, viewport: { width: 390, height: 800 } });

  test("Menu opens the nav, My org and the person's menu, and nothing scrolls sideways", async ({ page }) => {
    await signIn(page, "cto@example.com");
    const strip = page.locator("header.sidebar");
    await expect(strip).toHaveCSS("height", "48px");
    await expect(strip.getByRole("link", { name: "Goal Tracker" })).toBeVisible();
    await expect(page.getByTestId("nav-home")).toBeHidden();
    expect(await scrollsSideways(page), "the page scrolls sideways").toBe(false);

    await strip.getByText("Menu", { exact: true }).click();
    await expect(page.getByTestId("nav-home")).toBeVisible();
    await expect(page.getByTestId("nav-goals")).toBeVisible();
    await expect(page.getByTestId("sidebar-org")).toBeVisible();
    await page.getByTestId("person-menu").locator("summary").click();
    await expect(page.getByTestId("theme-dark")).toBeVisible();
    expect(await scrollsSideways(page), "the open Menu scrolls sideways").toBe(false);

    await page.getByTestId("theme-dark").click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  });
});
