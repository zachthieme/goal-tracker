// The Download menu: on a Report's row on the Reports list and in a
// publication's header, Download opens a popover holding a Markdown link and a
// PDF link. At 390px the open popover lies inside the window and nothing on
// the page passes the window's edge; without script it still opens, and both
// items are links. From the keyboard, Enter opens it, Tab reaches its first
// item, and Escape closes it, putting focus back on Download (#388, #407,
// #433).
import type { Locator, Page } from "@playwright/test";

import { admin, expect, signIn, test } from "./fixtures";

const phone = { width: 390, height: 800 };

// publishRuleReport saves a Report Definition named name with the rule Team
// is any of Platform, publishes it, and returns the publication's path. The
// builder steps are a local copy of buildRuleReport in phone-width.spec.ts.
async function publishRuleReport(page: Page, name: string): Promise<string> {
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  await builder.getByLabel("Name").fill(name);
  await builder.getByTestId("report-mode").getByText("Goals matching rules").click();
  const rule = builder.getByTestId("report-rule").first();
  await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
  await rule.getByLabel("Operator").selectOption({ label: "is any of" });
  await rule.getByLabel("Values").selectOption({ label: "Platform" });
  await builder.getByRole("button", { name: "Save report" }).click();
  await page.waitForURL(/\/reports\/\d+$/);
  await page.getByTestId("publish-confirm").getByText("Publish…").click();
  await page.getByTestId("report-publish").getByRole("button", { name: "Publish" }).click();
  await page.waitForURL(/\/reports\/\d+\/publications\/\d+$/);
  return new URL(page.url()).pathname;
}

// reportRow is the Reports list's row for the Report named name.
function reportRow(page: Page, name: string): Locator {
  return page.getByTestId("report-row").filter({ has: page.getByRole("link", { name, exact: true }) });
}

// openDownload presses menu's Download button and returns its popover, once
// it shows its two items.
async function openDownload(menu: Locator): Promise<Locator> {
  await menu.getByRole("button", { name: "Download" }).click();
  const popover = menu.locator("[popover]");
  await expect(popover).toBeVisible();
  await expect(popover.getByRole("link")).toHaveCount(2);
  return popover;
}

// expectInTheWindow asserts the open popover lies wholly inside the window,
// and that no visible element on the page passes the window's left or right
// edge: html's overflow-x:clip would cut such an element off rather than let
// the page scroll to it, so scrollWidth alone can't tell.
async function expectInTheWindow(page: Page, popover: Locator): Promise<void> {
  const box = (await popover.boundingBox())!;
  expect(box.x, "the popover's left edge is in the window").toBeGreaterThanOrEqual(0);
  expect(box.x + box.width, "the popover's right edge is in the window").toBeLessThanOrEqual(phone.width);
  expect(box.y, "the popover's top edge is in the window").toBeGreaterThanOrEqual(0);
  expect(box.y + box.height, "the popover's bottom edge is in the window").toBeLessThanOrEqual(phone.height);

  const past = await page.evaluate((width) => {
    return [...document.body.querySelectorAll("*")]
      .filter((e) => {
        // checkVisibility is false inside a closed details, whose content
        // still has a box.
        if (!e.checkVisibility({ visibilityProperty: true })) return false;
        const r = e.getBoundingClientRect();
        return r.width > 0 && (r.left < -1 || r.right > width + 1);
      })
      .map((e) => {
        const r = e.getBoundingClientRect();
        return `${(e as HTMLElement).dataset.testid || e.className || e.tagName} (${Math.round(r.left)}–${Math.round(r.right)}px)`;
      });
  }, phone.width);
  expect(past, `elements past the ${phone.width}px window with the Download menu open`).toEqual([]);
}

// expectBothLinks asserts the popover's items are the publication's Markdown
// and PDF links.
async function expectBothLinks(popover: Locator, publication: string): Promise<void> {
  await expect(popover.getByRole("link", { name: "Markdown (.md)" })).toHaveAttribute(
    "href",
    `${publication}/markdown`,
  );
  await expect(popover.getByRole("link", { name: /^PDF/ })).toHaveAttribute(
    "href",
    new RegExp(`^${publication}/(pdf|print)$`),
  );
}

// expectKeyboardDownload drives menu's Download menu from the keyboard: Enter
// on the focused button shows the menu, Tab moves to its first item, and
// Escape hides it and puts focus back on the button. The menu is found by the
// button's popovertarget, not by its popover attribute, so a menu that isn't
// a popover fails here rather than going unfound.
async function expectKeyboardDownload(page: Page, menu: Locator): Promise<void> {
  const button = menu.getByRole("button", { name: "Download" });
  const popover = menu.locator(`[id="${await button.getAttribute("popovertarget")}"]`);
  await expect(popover, "the menu is hidden before Download is pressed").toBeHidden();

  await button.focus();
  await page.keyboard.press("Enter");
  await expect(popover, "Enter on Download shows the menu").toBeVisible();

  await page.keyboard.press("Tab");
  await expect(popover.getByRole("link").first(), "Tab moves to the menu's first item").toBeFocused();

  await page.keyboard.press("Escape");
  await expect(popover, "Escape hides the menu").toBeHidden();
  await expect(button, "Escape puts focus back on Download").toBeFocused();
}

test("Download's menu opens, is entered and closes from the keyboard on a publication and a Report row", async ({
  page,
}) => {
  await signIn(page, admin);
  await publishRuleReport(page, "Platform team");
  await expectKeyboardDownload(page, page.getByTestId("report-export").getByTestId("download-menu"));

  await page.goto("/reports");
  await expectKeyboardDownload(page, reportRow(page, "Platform team").getByTestId("download-menu"));
});

test.describe("at 390px", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize(phone);
  });

  test("a Report row's Download menu opens inside the window", async ({ page }) => {
    await signIn(page, admin);
    const publication = await publishRuleReport(page, "Platform team");
    await page.goto("/reports");
    const popover = await openDownload(reportRow(page, "Platform team").getByTestId("download-menu"));
    await expectBothLinks(popover, publication);
    await expectInTheWindow(page, popover);
  });

  test("a publication's Download menu opens inside the window", async ({ page }) => {
    await signIn(page, admin);
    const publication = await publishRuleReport(page, "Platform team");
    const popover = await openDownload(page.getByTestId("report-export").getByTestId("download-menu"));
    await expectBothLinks(popover, publication);
    await expectInTheWindow(page, popover);
  });
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("Download opens its menu of two links on a Report row and on a publication", async ({ page }) => {
    await signIn(page, admin);
    const publication = await publishRuleReport(page, "Platform team");

    await expectBothLinks(
      await openDownload(page.getByTestId("report-export").getByTestId("download-menu")),
      publication,
    );

    await page.goto("/reports");
    await expectBothLinks(
      await openDownload(reportRow(page, "Platform team").getByTestId("download-menu")),
      publication,
    );
  });
});
