// The Goal page's Draft Highlights with script on (#363): + Log a highlight
// opens the Highlight editor (#362) with an optional Kind, and its Save logs
// the Draft Highlight at once with htmx, with no page load. Each pending one
// has Edit, which opens it in the editor in place, and Remove, both disabled
// while an editor is open (#372). The list shown is always the server's.
// Without script the block is as it was: a link to the log form, and Delete.
import type { Locator, Page } from "@playwright/test";

import { expect, requirePick, signIn, test } from "./fixtures";

// The manifest's draftlessGoal is an Active Goal with no Draft Highlights and
// an Owner who hasn't departed, so the Goal page's Draft Highlights are this
// test's alone.
type Goal = { id: number; email: string };

test.describe("the Goal page's Draft Highlight editor", () => {
  let goal: Goal;

  test.beforeEach(async ({ page, manifest }) => {
    goal = requirePick("draftlessGoal", manifest.draftlessGoal);
    await signIn(page, goal.email);
    await page.goto(`/goals/${goal.id}`);
    // A page load would lose this; an htmx swap keeps it.
    await page.evaluate(() => ((window as unknown as { samePage: boolean }).samePage = true));
  });

  test("(a) + Log a highlight opens the editor, and Save logs it with no page load", async ({ page }) => {
    const drafts = page.getByTestId("goal-draft-highlights");
    await expect(drafts.getByRole("link", { name: "Log a highlight" })).toBeHidden();
    const loads: string[] = [];
    page.on("framenavigated", (f) => {
      if (f === page.mainFrame()) loads.push(f.url());
    });

    await drafts.getByRole("button", { name: "+ Log a highlight" }).click();
    const editor = drafts.getByTestId("highlight-editor");
    await expect(editor.getByRole("heading", { name: "New Draft Highlight" })).toBeVisible();
    await expect(editor.getByLabel("Kind")).toHaveValue("");
    await expect(drafts.getByRole("button", { name: "+ Log a highlight" })).toBeDisabled();
    const save = editor.getByRole("button", { name: "Save" });
    await expect(save).toBeDisabled();
    await editor.getByLabel("Note").fill("Vendor may raise prices.");
    await expect(save).toBeEnabled();
    await save.click();

    await expect(editor).toHaveCount(0);
    await expect(items(page)).toHaveCount(1);
    await expect(items(page)).toContainText("No kind Vendor may raise prices.");
    await expect(drafts.getByRole("button", { name: "+ Log a highlight" })).toBeEnabled();
    await expect(drafts.getByRole("button", { name: "+ Log a highlight" })).toBeFocused();
    await expectSamePage(page, goal);
    expect(loads, "Save loaded no page").toEqual([]);

    // A Cancel closes a new one's editor and logs nothing.
    await drafts.getByRole("button", { name: "+ Log a highlight" }).click();
    await editor.getByLabel("Kind").selectOption("Miss");
    await editor.getByLabel("Note").fill("Never mind.");
    await editor.getByRole("button", { name: "Cancel" }).click();
    await expect(editor).toHaveCount(0);
    await expect(items(page)).toHaveCount(1);
  });

  test("(b) Edit and Save changes its Kind and Note; Edit and Cancel leaves it", async ({ page }) => {
    await logDraft(page, "Insight", "Drills find gaps.");
    const drafts = page.getByTestId("goal-draft-highlights");
    const editor = drafts.getByTestId("highlight-editor");

    await items(page).getByRole("button", { name: "Edit" }).click();
    await expect(editor.getByRole("heading", { name: "Edit Draft Highlight" })).toBeVisible();
    await expect(editor.getByLabel("Kind")).toHaveValue("Insight");
    await expect(editor.getByLabel("Note")).toHaveValue("Drills find gaps.");
    await expect(items(page)).toHaveCount(0);
    await expect(drafts.getByRole("button", { name: "+ Log a highlight" })).toBeDisabled();
    await editor.getByLabel("Kind").selectOption("Miss");
    await editor.getByLabel("Note").fill("Drills found a gap reviews missed.");
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(editor).toHaveCount(0);
    await expect(items(page)).toContainText("Miss Drills found a gap reviews missed.");
    await expect(items(page).getByRole("button", { name: "Edit" })).toBeFocused();

    await items(page).getByRole("button", { name: "Edit" }).click();
    await editor.getByLabel("Note").fill("Something else entirely.");
    await editor.getByLabel("Kind").selectOption("");
    await editor.getByRole("button", { name: "Cancel" }).click();
    await expect(editor).toHaveCount(0);
    await expect(items(page)).toContainText("Miss Drills found a gap reviews missed.");
    await expectSamePage(page, goal);

    await page.reload();
    await expect(items(page)).toHaveCount(1);
    await expect(items(page)).toContainText("Miss Drills found a gap reviews missed.");
  });

  test("(c) Remove takes it off the list, and a reload shows the server's list", async ({ page }) => {
    await logDraft(page, "Accomplishment", "Zero downtime on the cutover.");
    await logDraft(page, "None", "Customers ask for the export weekly.");
    await expect(items(page)).toHaveCount(2);

    const cutover = items(page).filter({ hasText: "Zero downtime" });
    await expect(cutover.getByRole("button", { name: "Delete" })).toHaveCount(0);
    await cutover.getByRole("button", { name: "Remove" }).click();
    await expect(items(page)).toHaveCount(1);
    await expect(items(page)).toContainText("No kind Customers ask for the export weekly.");
    await expectSamePage(page, goal);

    const shown = await items(page).allTextContents();
    await page.reload();
    await expect(items(page)).toHaveText(shown);
  });

  test("(d) a refused Save comes back in the editor with the reason and what was typed", async ({ page }) => {
    await logDraft(page, "Insight", "Drills find gaps.");
    const drafts = page.getByTestId("goal-draft-highlights");
    const editor = drafts.getByTestId("highlight-editor");
    // Save needs a Note, so the page sends none only if what it sends is
    // blanked on the way; the server's refusal is real.
    await page.route("**/draft-highlights*", (route) => route.continue({ postData: "kind=Miss&note=+" }));
    await page.route("**/draft-highlights/*", (route) => route.continue({ postData: "kind=Miss&note=+" }));

    await drafts.getByRole("button", { name: "+ Log a highlight" }).click();
    await editor.getByLabel("Note").fill("Lost on the way.");
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(editor.getByRole("alert")).toHaveText("A Draft Highlight needs a note.");
    await expect(editor.getByLabel("Note")).toBeFocused();
    await expect(editor.getByRole("heading", { name: "New Draft Highlight" })).toBeVisible();
    await expect(editor.getByLabel("Kind")).toHaveValue("Miss");
    await expect(drafts.getByTestId("log-draft-highlight")).toHaveCount(0);
    await editor.getByRole("button", { name: "Cancel" }).click();
    await expect(editor).toHaveCount(0);

    await items(page).getByRole("button", { name: "Edit" }).click();
    await editor.getByLabel("Note").fill("Lost on the way too.");
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(editor.getByRole("alert")).toHaveText("A Draft Highlight needs a note.");
    await expect(editor.getByRole("heading", { name: "Edit Draft Highlight" })).toBeVisible();
    await expect(editor.getByLabel("Kind")).toHaveValue("Miss");
    await expect(items(page)).toHaveCount(0);
    await editor.getByRole("button", { name: "Cancel" }).click();
    await expect(items(page)).toHaveCount(1);
    await expect(items(page)).toContainText("Insight Drills find gaps.");
    await expectSamePage(page, goal);
  });

  test("(f) Remove is disabled while an editor is open, and enabled again after Cancel", async ({ page }) => {
    await logDraft(page, "Insight", "Drills find gaps.");
    await logDraft(page, "Miss", "Runbook was late.");
    const drafts = page.getByTestId("goal-draft-highlights");
    const editor = drafts.getByTestId("highlight-editor");
    const removes = items(page).getByRole("button", { name: "Remove" });
    await expect(removes).toHaveCount(2);

    // A Remove would swap the block and lose what the open editor holds.
    await drafts.getByRole("button", { name: "+ Log a highlight" }).click();
    await editor.getByLabel("Note").fill("Half written.");
    for (const remove of await removes.all()) await expect(remove).toBeDisabled();
    await editor.getByRole("button", { name: "Cancel" }).click();
    for (const remove of await removes.all()) await expect(remove).toBeEnabled();

    await items(page).filter({ hasText: "Drills find gaps." }).getByRole("button", { name: "Edit" }).click();
    await editor.getByLabel("Note").fill("Drills found a gap.");
    await expect(removes).toHaveCount(1);
    await expect(removes).toBeDisabled();
    await editor.getByRole("button", { name: "Cancel" }).click();
    await expect(removes).toHaveCount(2);
    for (const remove of await removes.all()) await expect(remove).toBeEnabled();
    await expect(items(page)).toHaveCount(2);
  });
});

test.describe("the Goal page's Draft Highlights without script", () => {
  test.use({ javaScriptEnabled: false });

  test("(e) the link opens the log form, and each one has Delete and no Edit", async ({ page, manifest }) => {
    const goal = requirePick("draftlessGoal", manifest.draftlessGoal);
    await signIn(page, goal.email);
    await page.goto(`/goals/${goal.id}`);
    const drafts = page.getByTestId("goal-draft-highlights");
    await expect(drafts.getByRole("button", { name: "+ Log a highlight" })).toBeHidden();
    await drafts.getByRole("link", { name: "Log a highlight" }).click();
    const log = drafts.getByTestId("log-draft-highlight");
    await log.getByLabel("Note").fill("Vendor may raise prices.");
    await log.getByRole("button", { name: "Log Draft Highlight" }).click();
    await expect(page).toHaveURL(new RegExp(`/goals/${goal.id}$`));
    await expect(items(page)).toContainText("No kind Vendor may raise prices.");
    await expect(items(page).getByRole("button", { name: "Delete" })).toBeVisible();
    await expect(items(page).getByRole("button", { name: "Edit" })).toHaveCount(0);
    await expect(items(page).getByRole("button", { name: "Remove" })).toHaveCount(0);

    await items(page).getByRole("button", { name: "Delete" }).click();
    await expect(page).toHaveURL(new RegExp(`/goals/${goal.id}$`));
    await expect(items(page)).toHaveCount(0);
  });
});

function items(page: Page): Locator {
  return page.getByTestId("goal-draft-highlights").getByTestId("draft-highlight");
}

// logDraft logs a Draft Highlight through + Log a highlight, the editor and
// Save, kind "None" for none.
async function logDraft(page: Page, kind: string, note: string) {
  const drafts = page.getByTestId("goal-draft-highlights");
  await drafts.getByRole("button", { name: "+ Log a highlight" }).click();
  const editor = drafts.getByTestId("highlight-editor");
  await editor.getByLabel("Kind").selectOption({ label: kind });
  await editor.getByLabel("Note").fill(note);
  await editor.getByRole("button", { name: "Save" }).click();
  await expect(editor).toHaveCount(0);
  await expect(items(page).filter({ hasText: note })).toHaveCount(1);
}

// expectSamePage checks the Goal page is still the one loaded at the start:
// its address unchanged, and no page load since.
async function expectSamePage(page: Page, goal: Goal) {
  await expect(page).toHaveURL(new RegExp(`/goals/${goal.id}$`));
  expect(await page.evaluate(() => (window as unknown as { samePage?: boolean }).samePage)).toBe(true);
}
