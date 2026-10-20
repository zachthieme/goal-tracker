// The Check-in page's "Add to this check-in" bar (#365): with script on, it
// sits under the page's header with + Insight, + Accomplishment, + Miss, Move
// a date or Milestone and Pause, finish or cancel. The dates and Lifecycle
// sections stay hidden until their button is pressed, or until the page opens
// them as it always has, and then show under their own headings with their
// buttons marked used. The Highlights section always shows, and the bar's
// Highlight buttons stand for its own. Without script the page keeps its
// folded sections.
import type { Locator, Page } from "@playwright/test";

import { expect, signIn, test } from "./fixtures";

// quietGoal is an Active Goal whose Owner can check in Green with only a
// status: Green last time, no Metric to judge off target, nothing contributing
// to it to roll up, no overdue Milestone to open the dates section, and no
// Draft Highlight to offer.
const quietGoal = `
  select g.id, a.email
  from goals g join accounts a on a.id = g.owner_id
  where g.lifecycle = 'Active' and a.departed = 0
    and not exists (select 1 from metrics m where m.goal_id = g.id)
    and not exists (select 1 from links l where l.parent_id = g.id and l.status = 'accepted')
    and not exists (select 1 from milestones m
                    where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
    and not exists (select 1 from draft_highlights d where d.goal_id = g.id)
    and (select c.health from checkins c where c.goal_id = g.id
         order by c.created_at desc, c.id desc limit 1) = 'Green'
  order by g.id
  limit 1`;

// goalWithMetric is an Active Goal with a Metric, and its Owner.
const goalWithMetric = `
  select g.id, a.email
  from goals g join accounts a on a.id = g.owner_id
  where g.lifecycle = 'Active' and a.departed = 0
    and exists (select 1 from metrics m where m.goal_id = g.id)
  order by g.id
  limit 1`;

type Goal = { id: number; email: string };

async function openCheckin(page: Page, goal: Goal, query = ""): Promise<Locator> {
  await page.goto(`/goals/${goal.id}/checkin${query}`);
  const form = page.getByTestId("checkin-form");
  await expect(form).toBeVisible();
  return form;
}

function addBar(page: Page): Locator {
  return page.getByTestId("checkin-add-bar");
}

test.describe("the Check-in page's add bar", () => {
  let goal: Goal;

  test.beforeEach(async ({ page, seedLookup }) => {
    [goal] = seedLookup<Goal>(quietGoal);
    expect(goal, "the seed has a quiet Green Goal").toBeDefined();
    await signIn(page, goal.email);
  });

  test("(a) a fresh Check-in shows the bar, and no section's summary", async ({ page }) => {
    const form = await openCheckin(page, goal);
    const bar = addBar(page);
    await expect(bar).toBeVisible();
    await expect(bar.getByRole("button")).toHaveText([
      "+ Insight",
      "+ Accomplishment",
      "+ Miss",
      "Move a date or Milestone",
      "Pause, finish or cancel",
    ]);
    // The bar comes before the form, under the header.
    const barBox = (await bar.boundingBox())!;
    const formBox = (await form.boundingBox())!;
    expect(barBox.y + barBox.height, "the bar sits above the form").toBeLessThanOrEqual(formBox.y + 1);

    await expect(form.getByTestId("checkin-dates-section")).toBeHidden();
    await expect(form.getByTestId("checkin-lifecycle-section")).toBeHidden();
    // The Highlights section always shows: its heading and add buttons, with
    // nothing in it yet.
    const highlights = form.getByTestId("checkin-highlights");
    await expect(highlights.getByRole("heading", { name: "Highlights" })).toBeVisible();
    await expect(highlights).toContainText("A Report author can pull each one in");
    await expect(highlights.getByTestId("highlight-item")).toHaveCount(0);
    await expect(highlights.getByTestId("highlight-editor")).toHaveCount(0);
    await expect(form.locator("summary")).toHaveCount(3);
    for (const summary of await form.locator("summary").all()) await expect(summary).toBeHidden();
    for (const testID of ["checkin-add-dates", "checkin-add-lifecycle"]) {
      await expect(bar.getByTestId(testID)).not.toHaveAttribute("data-used");
    }
  });

  test("(b) Move a date or Milestone, and Pause, finish or cancel, each show their section", async ({ page }) => {
    const form = await openCheckin(page, goal);
    const bar = addBar(page);

    await bar.getByRole("button", { name: "Move a date or Milestone" }).click();
    const dates = form.getByTestId("checkin-dates-section");
    await expect(dates).toBeVisible();
    await expect(dates.getByRole("heading", { name: "Dates and Milestones" })).toBeFocused();
    await expect(dates.getByLabel("New Milestone name")).toBeVisible();
    await expect(bar.getByTestId("checkin-add-dates")).toHaveAttribute("data-used");
    await expect(form.getByTestId("checkin-lifecycle-section")).toBeHidden();

    await bar.getByRole("button", { name: "Pause, finish or cancel" }).click();
    const lifecycle = form.getByTestId("checkin-lifecycle-section");
    await expect(lifecycle).toBeVisible();
    await expect(lifecycle.getByRole("heading", { name: "Lifecycle" })).toBeFocused();
    await expect(lifecycle.getByRole("radio", { name: "Put On Hold" })).toBeVisible();
    await expect(bar.getByTestId("checkin-add-lifecycle")).toHaveAttribute("data-used");

    // A used button still works: it goes back to its section, which stays.
    await bar.getByRole("button", { name: "Move a date or Milestone" }).click();
    await expect(dates.getByRole("heading", { name: "Dates and Milestones" })).toBeFocused();
    await expect(dates).toBeVisible();
    await expect(lifecycle).toBeVisible();
  });

  test("(c) a section the page opens shows on load, its button used", async ({ page }) => {
    let form = await openCheckin(page, goal, "?open=dates");
    await expect(form.getByTestId("checkin-dates-section")).toBeVisible();
    await expect(addBar(page).getByTestId("checkin-add-dates")).toHaveAttribute("data-used");
    await expect(form.getByTestId("checkin-lifecycle-section")).toBeHidden();
    await expect(addBar(page).getByTestId("checkin-add-lifecycle")).not.toHaveAttribute("data-used");

    form = await openCheckin(page, goal, "?lifecycle=Done");
    const lifecycle = form.getByTestId("checkin-lifecycle-section");
    await expect(lifecycle).toBeVisible();
    await expect(lifecycle.getByRole("radio", { name: "Mark Done" })).toBeChecked();
    await expect(addBar(page).getByTestId("checkin-add-lifecycle")).toHaveAttribute("data-used");
    await expect(form.getByTestId("checkin-dates-section")).toBeHidden();
  });

  test("(d) the bar's + Accomplishment opens the Highlights section's editor", async ({ page }) => {
    const form = await openCheckin(page, goal);
    const shortcuts = addBar(page).getByTestId("checkin-add-highlight");
    await expect(shortcuts).toHaveText(["+ Insight", "+ Accomplishment", "+ Miss"]);

    await addBar(page).getByRole("button", { name: "+ Accomplishment" }).click();
    const highlights = form.getByTestId("checkin-highlights");
    await expect(highlights.getByRole("heading", { name: "Highlights" })).toBeInViewport();
    const editor = highlights.getByTestId("highlight-editor");
    await expect(editor.getByRole("heading", { name: "New Accomplishment" })).toBeVisible();
    await expect(editor.getByLabel("Note")).toBeFocused();
    // One editor at a time: the bar's Highlight buttons wait with the
    // section's own.
    for (const b of await shortcuts.all()) await expect(b).toBeDisabled();
    for (const b of await highlights.getByTestId("highlight-add").all()) await expect(b).toBeDisabled();
    await expect(addBar(page).getByTestId("checkin-add-dates")).toBeEnabled();

    await editor.getByLabel("Note").fill("Cut paging noise by half.");
    await editor.getByRole("button", { name: "Save" }).click();
    await expect(editor).toHaveCount(0);
    await expect(highlights.getByTestId("highlight-item-note")).toHaveText(["Cut paging noise by half."]);
    for (const b of await shortcuts.all()) await expect(b).toBeEnabled();
    for (const b of await shortcuts.all()) await expect(b).not.toHaveAttribute("data-used");
  });

  test("(e) a refused submit shows the Lifecycle section with its error, its button still used", async ({ page }) => {
    const form = await openCheckin(page, goal);
    await addBar(page).getByRole("button", { name: "Pause, finish or cancel" }).click();
    await form.getByTestId("checkin-lifecycle-section").getByRole("radio", { name: "Put On Hold" }).check();
    await form.getByRole("textbox", { name: /^What changed since the last check-in/ }).fill("Waiting on budget.");
    await form.getByRole("button", { name: "Submit check-in" }).click();

    const refused = page.getByTestId("checkin-form").getByTestId("checkin-lifecycle-section");
    await expect(refused.getByRole("alert")).toBeVisible();
    await expect(refused.getByRole("heading", { name: "Lifecycle" })).toBeVisible();
    await expect(refused.getByRole("radio", { name: "Put On Hold" })).toBeChecked();
    await expect(addBar(page).getByTestId("checkin-add-lifecycle")).toHaveAttribute("data-used");
    await expect(page.getByTestId("checkin-form").getByTestId("checkin-dates-section")).toBeHidden();
    await expect(addBar(page).getByTestId("checkin-add-dates")).not.toHaveAttribute("data-used");
    await expect(page).toHaveURL(new RegExp(`/goals/${goal.id}/checkin$`));
  });

  test("(g) at 390×800 the page doesn't scroll sideways, and the bar's buttons do", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await openCheckin(page, goal);
    await expect(addBar(page)).toBeVisible();
    // The page's own width isn't checked: app.css clips html and body, so it
    // never passes the window's. What would scroll the page shows instead as
    // main's content running past main.
    const main = await page.locator("main").evaluate((e) => [e.scrollWidth, e.clientWidth]);
    expect(main[0], "main's content width").toBeLessThanOrEqual(main[1]);
    const row = addBar(page).getByTestId("checkin-add-buttons");
    const scroll = await row.evaluate((e) => [e.scrollWidth, e.clientWidth]);
    expect(scroll[0], "the button row runs past its own width").toBeGreaterThan(scroll[1]);
    // The label sits above the buttons.
    const label = (await addBar(page).getByText("Add to this check-in", { exact: true }).boundingBox())!;
    const first = (await addBar(page).getByRole("button", { name: "+ Insight" }).boundingBox())!;
    expect(first.y, "the buttons sit under the label").toBeGreaterThanOrEqual(label.y + label.height - 1);
  });
});

test("(f) Enter in a Metric's reading submits the Check-in", async ({ page, seedLookup }) => {
  const [goal] = seedLookup<Goal>(goalWithMetric);
  expect(goal, "the seed has an Active Goal with a Metric").toBeDefined();
  await signIn(page, goal.email);
  const form = await openCheckin(page, goal);
  await form.getByRole("textbox", { name: /^What changed since the last check-in/ }).fill("Steady week.");
  const reading = form.getByTestId("checkin-readings").getByRole("spinbutton").first();
  await reading.fill("42");
  const posted = page.waitForRequest((r) => r.method() === "POST" && r.url().endsWith(`/goals/${goal.id}/checkins`));
  await reading.press("Enter");
  // The form's first submit, the hidden default, sends it: not Add another.
  const sent = new URLSearchParams((await posted).postData() ?? "");
  expect(sent.get("status")).toBe("Steady week.");
  expect(sent.get((await reading.getAttribute("name"))!)).toBe("42");
  expect(sent.has("add_highlight"), "Add another wasn't the submit pressed").toBe(false);
});

test.describe("the Check-in page without script", () => {
  test.use({ javaScriptEnabled: false });

  test("(h) has no bar, and its sections open by their summaries", async ({ page, seedLookup }) => {
    const [goal] = seedLookup<Goal>(quietGoal);
    expect(goal, "the seed has a quiet Green Goal").toBeDefined();
    await signIn(page, goal.email);
    const form = await openCheckin(page, goal);
    await expect(addBar(page)).toBeHidden();
    await expect(form.locator("summary")).toHaveCount(3);
    for (const summary of await form.locator("summary").all()) await expect(summary).toBeVisible();
    await expect(form.getByTestId("checkin-dates-heading")).toBeHidden();
    await expect(form.getByTestId("checkin-lifecycle-heading")).toBeHidden();

    const dates = form.getByTestId("checkin-dates-section");
    await expect(dates.getByLabel("New Milestone name")).toBeHidden();
    await dates.getByText(/^Change dates or milestones/).click();
    await expect(dates).toHaveJSProperty("open", true);
    await expect(dates.getByLabel("New Milestone name")).toBeVisible();
  });
});
