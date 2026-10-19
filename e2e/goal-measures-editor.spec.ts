// The New goal form's Milestone and Metric editor, with script on (#369):
// How you'll know starts as + Milestone and + Metric, each opens an editor,
// Save makes it an item with Edit and Remove, and the Ready card asks for an
// editor left open to be saved. Nothing is recorded until the form is
// submitted.
import type { Locator, Page } from "@playwright/test";

import { admin, appToday, expect, signIn, test } from "./fixtures";

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const thisYear = Number(appToday().slice(0, 4));
// nextYear is a date in the year after the app's, which an item shows with
// its year.
const nextYear = `${thisYear + 1}-03-15`;

test.describe("the New goal form's Milestone and Metric editor", () => {
  test.beforeEach(async ({ page }) => {
    await signIn(page, admin);
    await page.goto("/goals/new");
  });

  test("(a) starts as the two add buttons, with no rows and no labels", async ({ page }) => {
    const section = know(page);
    await expect(section.getByTestId("milestone-row")).toHaveCount(0);
    await expect(section.getByTestId("metric-row")).toHaveCount(0);
    await expect(addButtons(page)).toHaveText(["+ Milestone", "+ Metric"]);
    for (const b of await addButtons(page).all()) await expect(b).toBeEnabled();
    await expect(label(page, "Milestones")).toBeHidden();
    await expect(label(page, "Metrics")).toBeHidden();
    await expect(section.getByTestId("milestone-editor")).toHaveCount(0);
    await expect(section.getByTestId("metric-editor")).toHaveCount(0);
  });

  test("(b) + Milestone opens an editor, and Save waits for Name and Date", async ({ page }) => {
    await page.getByRole("button", { name: "+ Milestone" }).click();
    const editor = know(page).getByTestId("milestone-editor");
    await expect(editor.getByRole("heading", { name: "New Milestone" })).toBeVisible();
    await expect(editor.getByLabel("Name")).toBeFocused();
    for (const b of await addButtons(page).all()) await expect(b).toBeDisabled();
    const save = editor.getByRole("button", { name: "Save" });
    await expect(save).toBeDisabled();
    await editor.getByLabel("Name").fill("Beta in the EU");
    await expect(save).toBeDisabled();
    await editor.getByLabel("Date").fill(appToday());
    await expect(save).toBeEnabled();
    await save.click();

    await expect(editor).toHaveCount(0);
    const item = know(page).getByTestId("milestone-item");
    await expect(item.getByTestId("measure-item-name")).toHaveText("Beta in the EU");
    await expect(item.getByTestId("measure-item-detail")).toHaveText(showThisYear(appToday()));
    await expect(item.getByRole("button", { name: "Edit" })).toBeFocused();
    await expect(label(page, "Milestones")).toBeVisible();
    await expect(label(page, "Metrics")).toBeHidden();
    for (const b of await addButtons(page).all()) await expect(b).toBeEnabled();
  });

  test("(c) + Metric shows Direction only for a steady target, and the item reads baseline → target", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "+ Metric" }).click();
    const editor = know(page).getByTestId("metric-editor");
    await expect(editor.getByRole("heading", { name: "New Metric" })).toBeVisible();
    const save = editor.getByRole("button", { name: "Save" });
    const direction = editor.getByLabel(/^Direction/);
    await editor.getByLabel("Name").fill("Uptime");
    await editor.getByLabel("Unit").fill("%");
    await editor.getByLabel("Baseline", { exact: true }).fill("99.9");
    await editor.getByLabel("Target", { exact: true }).fill("99");
    await editor.getByLabel("Target date").fill(nextYear);
    await expect(direction).toBeHidden();
    await expect(save).toBeEnabled();

    await editor.getByLabel("Target", { exact: true }).fill("99.9");
    await expect(direction).toBeVisible();
    await expect(save).toBeDisabled();
    await direction.selectOption("up");
    await expect(save).toBeEnabled();
    await save.click();

    const item = know(page).getByTestId("metric-item");
    await expect(item.getByTestId("measure-item-name")).toHaveText("Uptime");
    await expect(item.getByTestId("measure-item-detail")).toHaveText(`99.9 → 99.9 % by Mar 15, ${thisYear + 1}`);
    await expect(label(page, "Metrics")).toBeVisible();
  });

  test("(d) Edit and Cancel leaves an item, Edit and Save changes it, and Remove drops it", async ({ page }) => {
    await addMilestone(page, "Beta in the EU", appToday());
    const item = know(page).getByTestId("milestone-item");

    await item.getByRole("button", { name: "Edit" }).click();
    const editor = know(page).getByTestId("milestone-editor");
    await expect(editor.getByRole("heading", { name: "Edit Milestone" })).toBeVisible();
    await expect(editor.getByLabel("Name")).toHaveValue("Beta in the EU");
    await editor.getByLabel("Name").fill("Something else");
    await editor.getByRole("button", { name: "Cancel" }).click();
    await expect(editor).toHaveCount(0);
    await expect(item.getByTestId("measure-item-name")).toHaveText("Beta in the EU");
    await expect(item.getByRole("button", { name: "Edit" })).toBeFocused();

    await item.getByRole("button", { name: "Edit" }).click();
    await editor.getByLabel("Name").fill("Beta in the EU and UK");
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(item).toHaveCount(1);
    await expect(item.getByTestId("measure-item-name")).toHaveText("Beta in the EU and UK");

    await item.getByRole("button", { name: "Remove" }).click();
    await expect(item).toHaveCount(0);
    await expect(page.getByRole("button", { name: "+ Milestone" })).toBeFocused();
    await expect(label(page, "Milestones")).toBeHidden();
  });

  test("(e) the Ready card asks for an open editor to be saved", async ({ page }) => {
    await page.getByRole("button", { name: "+ Milestone" }).click();
    await expectItem(page, "Save the Milestone you started", false);
    await expect(page.getByTestId("activation-checklist").getByText("A Milestone or Metric")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Create and activate" })).toBeDisabled();

    const editor = know(page).getByTestId("milestone-editor");
    await editor.getByLabel("Name").fill("Beta in the EU");
    await editor.getByLabel("Date").fill(appToday());
    await editor.getByRole("button", { name: "Save" }).click();
    await expectItem(page, "A Milestone or Metric", true);
    await expect(page.getByTestId("activation-checklist").getByText("Save the Milestone you started")).toHaveCount(0);
  });

  test("(f) Enter in an editor saves it once it can, and never submits the form", async ({ page }) => {
    await page.getByRole("button", { name: "+ Milestone" }).click();
    const editor = know(page).getByTestId("milestone-editor");
    await editor.getByLabel("Name").fill("Beta in the EU");
    await editor.getByLabel("Name").press("Enter");
    await expect(page).toHaveURL(/\/goals\/new$/);
    await expect(editor).toBeVisible();

    await editor.getByLabel("Date").fill(appToday());
    await editor.getByLabel("Name").press("Enter");
    await expect(editor).toHaveCount(0);
    await expect(know(page).getByTestId("milestone-item")).toHaveCount(1);
    await expect(page).toHaveURL(/\/goals\/new$/);
  });

  test("(g) the items left are what Save as Proposed creates", async ({ page }) => {
    await fillWhat(page, "Launch the EU storefront");
    await addMilestone(page, "Beta in the EU", appToday());
    await addMilestone(page, "Dropped milestone", appToday());
    await addMetric(page, "p95 latency", "ms", "1200", "400", nextYear);
    await know(page)
      .getByTestId("milestone-item")
      .filter({ hasText: "Dropped milestone" })
      .getByRole("button", { name: "Remove" })
      .click();
    await page.getByRole("button", { name: "Save as Proposed" }).click();

    await expect(page).toHaveURL(/\/goals\/\d+$/);
    await expect(page.getByTestId("goal-title")).toHaveText("Launch the EU storefront");
    const milestones = page.getByTestId("goal-milestone");
    await expect(milestones).toHaveCount(1);
    await expect(milestones).toContainText("Beta in the EU");
    const metrics = page.getByTestId("goal-metric");
    await expect(metrics).toHaveCount(1);
    await expect(metrics.getByRole("heading")).toHaveText("p95 latency");
  });

  test("(h) a refused submit reopens the editor showing why", async ({ page }) => {
    await fillWhat(page, "Launch the EU storefront");
    await page.getByRole("button", { name: "+ Milestone" }).click();
    await know(page).getByTestId("milestone-editor").getByLabel("Name").fill("Beta in the EU");
    const refused = page.waitForResponse(
      (r) => r.request().method() === "POST" && new URL(r.url()).pathname === "/goals/new",
    );
    await page.getByRole("button", { name: "Save as Proposed" }).click();
    expect((await refused).status()).toBe(422);

    await expect(page.getByLabel("Title")).toHaveValue("Launch the EU storefront");
    const editor = know(page).getByTestId("milestone-editor");
    await expect(editor).toBeVisible();
    await expect(editor.getByLabel("Name")).toHaveValue("Beta in the EU");
    await expect(editor.getByTestId("input-error")).toHaveText("a Milestone needs a date");
    await expect(editor.getByLabel("Date")).toHaveAttribute("aria-invalid", "true");
    await expect(know(page).getByTestId("milestone-row")).toHaveCount(0);
    for (const b of await addButtons(page).all()) await expect(b).toBeDisabled();

    await editor.getByLabel("Date").fill(appToday());
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(know(page).getByTestId("milestone-item")).toHaveCount(1);
    for (const b of await addButtons(page).all()) await expect(b).toBeEnabled();
  });

  test("(i) Finish defining lists the existing Milestone and adds a Metric", async ({ page }) => {
    await fillWhat(page, "Launch the EU storefront");
    await addMilestone(page, "Beta in the EU", appToday());
    await page.getByRole("button", { name: "Save as Proposed" }).click();
    await expect(page).toHaveURL(/\/goals\/\d+$/);
    const goalPage = page.url();
    await page.goto(`${goalPage}/define`);

    const existing = know(page).getByTestId("existing-measures");
    await expect(existing).toContainText("Beta in the EU");
    await expect(existing.getByRole("textbox")).toHaveCount(0);
    await expect(know(page).getByTestId("milestone-item")).toHaveCount(0);
    await addMetric(page, "p95 latency", "ms", "1200", "400", nextYear);
    await page.getByRole("button", { name: "Save as Proposed" }).click();

    await expect(page).toHaveURL(goalPage);
    await expect(page.getByTestId("goal-milestone")).toHaveCount(1);
    await expect(page.getByTestId("goal-metric").getByRole("heading")).toHaveText("p95 latency");
  });

  test("(j) with two refused editors open, Cancel moves focus to the other, and the label waits for a saved item", async ({
    page,
  }) => {
    await fillWhat(page, "Launch the EU storefront");
    await page.getByRole("button", { name: "+ Milestone" }).click();
    await know(page).getByTestId("milestone-editor").getByLabel("Name").fill("One");
    // A second Milestone row, with only a Name, is sent beside the editor's.
    await page.evaluate(() => {
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "milestones[9].name";
      input.value = "Two";
      document.querySelector('[data-testid="goal-form"]')!.appendChild(input);
    });
    const refused = page.waitForResponse(
      (r) => r.request().method() === "POST" && new URL(r.url()).pathname === "/goals/new",
    );
    await page.getByRole("button", { name: "Save as Proposed" }).click();
    expect((await refused).status()).toBe(422);

    const editors = know(page).getByTestId("milestone-editor");
    await expect(editors).toHaveCount(2);
    await expect(editors.nth(0).getByLabel("Name")).toHaveValue("One");
    await expect(editors.nth(1).getByLabel("Name")).toHaveValue("Two");
    await expect(label(page, "Milestones")).toBeHidden();

    await editors.nth(0).getByRole("button", { name: "Cancel" }).click();
    await expect(editors).toHaveCount(1);
    const second = editors.first();
    await expect(second.getByLabel("Name")).toBeFocused();
    await expect(label(page, "Milestones")).toBeHidden();

    await second.getByLabel("Date").fill(appToday());
    await second.getByRole("button", { name: "Save" }).click();
    await expect(editors).toHaveCount(0);
    const item = know(page).getByTestId("milestone-item");
    await expect(item.getByTestId("measure-item-name")).toHaveText("Two");
    await expect(item.getByRole("button", { name: "Edit" })).toBeFocused();
    await expect(label(page, "Milestones")).toBeVisible();
  });
});

function know(page: Page): Locator {
  return page.getByTestId("goal-form-know");
}

function addButtons(page: Page): Locator {
  return know(page).getByRole("button", { name: /^\+ / });
}

function label(page: Page, name: string): Locator {
  return know(page).getByRole("heading", { name, exact: true });
}

// showThisYear writes an ISO date in this year as the app does: "Nov 7".
function showThisYear(iso: string): string {
  const [, m, d] = iso.split("-").map(Number);
  return `${months[m - 1]} ${d}`;
}

async function fillWhat(page: Page, title: string) {
  await page.getByLabel("Title").fill(title);
  await page.getByLabel(/^So What/).fill("Shoppers in the EU can't buy from us yet.");
}

async function addMilestone(page: Page, name: string, date: string) {
  await page.getByRole("button", { name: "+ Milestone" }).click();
  const editor = know(page).getByTestId("milestone-editor");
  await editor.getByLabel("Name").fill(name);
  await editor.getByLabel("Date").fill(date);
  await editor.getByRole("button", { name: "Save" }).click();
  await expect(editor).toHaveCount(0);
}

async function addMetric(page: Page, name: string, unit: string, baseline: string, target: string, date: string) {
  await page.getByRole("button", { name: "+ Metric" }).click();
  const editor = know(page).getByTestId("metric-editor");
  await editor.getByLabel("Name").fill(name);
  await editor.getByLabel("Unit").fill(unit);
  await editor.getByLabel("Baseline", { exact: true }).fill(baseline);
  await editor.getByLabel("Target", { exact: true }).fill(target);
  await editor.getByLabel("Target date").fill(date);
  await editor.getByRole("button", { name: "Save" }).click();
  await expect(editor).toHaveCount(0);
}

// expectItem says the Ready card's checklist item labelled label is done or
// to do.
async function expectItem(page: Page, label: string, done: boolean) {
  const item = page
    .getByTestId("activation-checklist")
    .getByRole("listitem")
    .filter({ has: page.getByText(label, { exact: true }) });
  await expect(item).toHaveAttribute("data-done", String(done));
}
