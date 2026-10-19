// The Check-in form's Highlight editor, with script on (#362): + Insight,
// + Accomplishment or + Miss opens a Note editor for that kind, Save makes it
// an item with Edit and Remove, and Draft Highlights show as items already
// saved. Nothing is recorded until the Check-in is submitted.
import type { Locator, Page } from "@playwright/test";

import { expect, signIn, test } from "./fixtures";

// quietGoal is an Active Goal whose Owner can check in Green with only a
// status: Green last time, no Metric to judge off target, nothing contributing
// to it to roll up, no overdue Milestone, and no Highlight yet, so the Goal
// page's Highlights are this test's alone.
const quietGoal = `
  select g.id, g.title, a.email, coalesce(a.name, '') as name
  from goals g join accounts a on a.id = g.owner_id
  where g.lifecycle = 'Active' and a.departed = 0
    and not exists (select 1 from metrics m where m.goal_id = g.id)
    and not exists (select 1 from links l where l.parent_id = g.id and l.status = 'accepted')
    and not exists (select 1 from milestones m
                    where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
    and not exists (select 1 from highlights h join checkins c on c.id = h.checkin_id where c.goal_id = g.id)
    and not exists (select 1 from draft_highlights d where d.goal_id = g.id)
    and (select c.health from checkins c where c.goal_id = g.id
         order by c.created_at desc, c.id desc limit 1) = 'Green'
  order by g.id
  limit 1`;

type Goal = { id: number; title: string; email: string; name: string };

test.describe("the Check-in form's Highlight editor", () => {
  let goal: Goal;

  test.beforeEach(async ({ page, seedLookup }) => {
    [goal] = seedLookup<Goal>(quietGoal);
    expect(goal, "the seed has a quiet Green Goal").toBeDefined();
    await signIn(page, goal.email);
  });

  test("(a) starts as the Highlights heading and the three add buttons", async ({ page }) => {
    const form = await openCheckin(page, goal);
    const section = form.getByTestId("checkin-highlight-section");
    await expect(section.getByRole("heading", { name: "Highlights" })).toBeVisible();
    await expect(addButtons(form)).toHaveText(["+ Insight", "+ Accomplishment", "+ Miss"]);
    for (const b of await addButtons(form).all()) await expect(b).toBeEnabled();
    await expect(section.locator("summary")).toBeHidden();
    await expect(form.getByTestId("checkin-highlight-row")).toHaveCount(0);
    await expect(form.getByRole("button", { name: "Add another" })).toHaveCount(0);
    await expect(form.getByTestId("highlight-editor")).toHaveCount(0);
    await expect(form.getByTestId("highlight-item")).toHaveCount(0);
  });

  test("(b) + Miss opens a New Miss editor, Save makes an item, and Edit can change its Kind", async ({ page }) => {
    const form = await openCheckin(page, goal);
    await form.getByRole("button", { name: "+ Miss" }).click();
    const editor = form.getByTestId("highlight-editor");
    await expect(editor.getByRole("heading", { name: "New Miss" })).toBeVisible();
    await expect(editor.getByLabel("Kind")).toHaveCount(0);
    // One editor at a time: the add buttons wait while it is open.
    for (const b of await addButtons(form).all()) await expect(b).toBeDisabled();
    const save = editor.getByRole("button", { name: "Save" });
    await expect(save).toBeDisabled();
    await editor.getByLabel("Note").fill("The failover drill slipped a week.");
    await expect(save).toBeEnabled();
    await save.click();

    await expect(editor).toHaveCount(0);
    const item = form.getByTestId("highlight-item");
    await expect(item.getByTestId("highlight-item-kind")).toHaveText("Miss");
    await expect(item.getByTestId("highlight-item-note")).toHaveText("The failover drill slipped a week.");
    for (const b of await addButtons(form).all()) await expect(b).toBeEnabled();

    await item.getByRole("button", { name: "Edit" }).click();
    await expect(editor.getByLabel("Kind")).toHaveValue("Miss");
    await editor.getByLabel("Kind").selectOption("Insight");
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(item.getByTestId("highlight-item-kind")).toHaveText("Insight");
    await expect(item.getByTestId("highlight-item-note")).toHaveText("The failover drill slipped a week.");
  });

  test("(c) Edit and Save changes an item's Note; Edit and Cancel leaves it", async ({ page }) => {
    const form = await openCheckin(page, goal);
    await addHighlight(form, "Accomplishment", "Cut paging noise by half.");
    const item = form.getByTestId("highlight-item");
    const editor = form.getByTestId("highlight-editor");

    await item.getByRole("button", { name: "Edit" }).click();
    await expect(editor.getByLabel("Note")).toHaveValue("Cut paging noise by half.");
    await editor.getByLabel("Note").fill("Cut paging noise by two thirds.");
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(item.getByTestId("highlight-item-note")).toHaveText("Cut paging noise by two thirds.");

    await item.getByRole("button", { name: "Edit" }).click();
    await editor.getByLabel("Note").fill("Something else entirely.");
    await editor.getByLabel("Kind").selectOption("Miss");
    await editor.getByRole("button", { name: "Cancel" }).click();
    await expect(editor).toHaveCount(0);
    await expect(item.getByTestId("highlight-item-kind")).toHaveText("Accomplishment");
    await expect(item.getByTestId("highlight-item-note")).toHaveText("Cut paging noise by two thirds.");
  });

  test("(d) a removed item isn't recorded; the one left is", async ({ page }) => {
    const form = await openCheckin(page, goal);
    await addHighlight(form, "Insight", "Drills find gaps reviews miss.");
    await addHighlight(form, "Accomplishment", "Zero downtime on the cutover.");
    await expect(form.getByTestId("highlight-item")).toHaveCount(2);
    await form
      .getByTestId("highlight-item")
      .filter({ hasText: "Drills find gaps" })
      .getByRole("button", { name: "Remove" })
      .click();
    await expect(form.getByTestId("highlight-item")).toHaveCount(1);

    await submit(page, form, goal);
    await expect(page.getByTestId("goal-highlights").getByTestId("highlight")).toHaveCount(1);
    await expect(page.getByTestId("goal-highlights").getByTestId("highlight")).toContainText(
      "Accomplishment: Zero downtime on the cutover.",
    );
  });

  test("(e) an editor left open with a Note is recorded at submit", async ({ page }) => {
    const form = await openCheckin(page, goal);
    await form.getByRole("button", { name: "+ Insight" }).click();
    await form.getByTestId("highlight-editor").getByLabel("Note").fill("On-call load tracks deploy size.");

    await submit(page, form, goal);
    await expect(page.getByTestId("goal-highlights").getByTestId("highlight")).toHaveCount(1);
    await expect(page.getByTestId("goal-highlights").getByTestId("highlight")).toContainText(
      "Insight: On-call load tracks deploy size.",
    );
  });

  test("(f) a Draft Highlight shows as a saved item, and Remove discards it", async ({ page }) => {
    await logDraft(page, goal, "Miss", "Vendor may raise prices.");
    const form = await openCheckin(page, goal);
    const item = form.getByTestId("highlight-item");
    await expect(item).toHaveCount(1);
    await expect(item.getByTestId("highlight-item-kind")).toHaveText("Miss");
    await expect(item.getByTestId("highlight-item-note")).toHaveText("Vendor may raise prices.");
    await expect(item).toContainText(`Draft · logged by ${goal.name || goal.email.split("@")[0]}`);
    await expect(form.getByTestId("checkin-highlight-row")).toHaveCount(0);

    await item.getByRole("button", { name: "Remove" }).click();
    await expect(item).toHaveCount(0);
    await submit(page, form, goal);

    const entry = page.getByTestId("history-entry").and(page.locator('[data-kind="checkin"]')).first();
    await expect(entry.getByTestId("discarded-draft-highlight")).toHaveText(["Miss: Vendor may raise prices."]);
    await expect(page.getByTestId("draft-highlight")).toHaveCount(0);
    await expect(page.getByTestId("goal-highlights")).toHaveCount(0);
  });

  test("(g) a Draft Highlight with no Kind opens as an editor that needs one", async ({ page }) => {
    await logDraft(page, goal, "None", "Customers ask for the export weekly.");
    const form = await openCheckin(page, goal);
    const editor = form.getByTestId("highlight-editor");
    await expect(editor).toHaveCount(1);
    await expect(editor).toContainText("Draft · logged by");
    await expect(editor.getByLabel("Note")).toHaveValue("Customers ask for the export weekly.");
    await expect(editor.getByLabel("Kind")).toHaveValue("");
    const save = editor.getByRole("button", { name: "Save" });
    await expect(save).toBeDisabled();
    await editor.getByLabel("Kind").selectOption("Insight");
    await expect(save).toBeEnabled();
    await editor.getByLabel("Kind").selectOption("");
    await expect(save).toBeDisabled();

    // Submitted still open with no Kind, the form comes back with the editor
    // open and the reason in it.
    await form.getByTestId("checkin-health").getByText("Green", { exact: true }).click();
    await form.getByRole("textbox", { name: /^What changed since the last check-in/ }).fill("Steady week.");
    await form.getByRole("button", { name: "Submit check-in" }).click();
    const refused = page.getByTestId("checkin-form").getByTestId("highlight-editor");
    await expect(refused.getByRole("alert")).toContainText("needs a kind");
    await expect(refused.getByLabel("Kind")).toBeVisible();
    await expect(refused.getByLabel("Note")).toHaveValue("Customers ask for the export weekly.");
    await expect(page).toHaveURL(new RegExp(`/goals/${goal.id}/checkin$`));
  });
});

async function openCheckin(page: Page, goal: Goal): Promise<Locator> {
  await page.goto(`/goals/${goal.id}/checkin`);
  const form = page.getByTestId("checkin-form");
  await expect(form).toBeVisible();
  return form;
}

function addButtons(form: Locator): Locator {
  return form.getByTestId("highlight-add");
}

// addHighlight writes a Highlight through its kind's add button, the Note and
// Save.
async function addHighlight(form: Locator, kind: string, note: string) {
  await form.getByRole("button", { name: `+ ${kind}` }).click();
  const editor = form.getByTestId("highlight-editor");
  await editor.getByLabel("Note").fill(note);
  await editor.getByRole("button", { name: "Save" }).click();
  await expect(editor).toHaveCount(0);
}

// submit records the Check-in as Green with a status and waits for the Goal
// page.
async function submit(page: Page, form: Locator, goal: Goal) {
  await form.getByTestId("checkin-health").getByText("Green", { exact: true }).click();
  await form.getByRole("textbox", { name: /^What changed since the last check-in/ }).fill("Steady week.");
  await form.getByRole("button", { name: "Submit check-in" }).click();
  await page.waitForURL(new RegExp(`/goals/${goal.id}$`));
}

// logDraft logs a Draft Highlight from the Goal page, kind "None" for none.
async function logDraft(page: Page, goal: Goal, kind: string, note: string) {
  await page.goto(`/goals/${goal.id}`);
  const drafts = page.getByTestId("goal-draft-highlights");
  await drafts.getByRole("link", { name: "Log a highlight" }).click();
  const log = drafts.getByTestId("log-draft-highlight");
  await log.getByLabel("Kind").selectOption({ label: kind });
  await log.getByLabel("Note").fill(note);
  await log.getByRole("button", { name: "Log Draft Highlight" }).click();
  await expect(page.getByTestId("draft-highlight")).toContainText(note);
}
