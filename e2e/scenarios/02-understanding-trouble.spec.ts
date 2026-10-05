// Scenario 2 in docs/scenarios.md, Understanding trouble at the review: a
// leader builds a Report over her org and publishes it, a reader two levels up
// asks a question on one Goal's block, its Owner answers, and the Action Item
// agreed in the thread carries into the next publication until its owner
// closes it. The watch-list Report is scenario 2's other half (#198).
import { type Locator, type Page } from "@playwright/test";

import { admin, appToday, expect, test } from "../fixtures";

// The leader ("Elena") and the reader two levels up. The seed names them
// Priya Raman and Dana Whitfield; the page is asserted on those Names.
const leader = "cto@example.com";
const reader = "ceo@example.com";

// platformGoals are the Goals a "Team is any of Platform" rule selects on a
// first draft, whose baseline is 30 days back: every Platform Goal less those
// Done or Cancelled before then.
const platformGoals = `
  select g.id, g.title
  from goals g
  join goal_dimension_values gdv on gdv.goal_id = g.id
  join dimension_values dv on dv.id = gdv.dimension_value_id
  join dimensions d on d.id = dv.dimension_id
  where d.name = 'Team' and dv.value = 'Platform'
    and not (g.lifecycle in ('Done', 'Cancelled')
             and coalesce((select max(c.created_at) from checkins c
                           where c.goal_id = g.id and c.lifecycle_to = g.lifecycle), '')
                 < strftime('%Y-%m-%dT%H:%M:%SZ', $now, '-30 days'))
  order by g.id`;

type Goal = { id: number; title: string };

test("a leader's Report from Definition to publication, Comment and Action Item", async ({
  as,
  seedLookup,
  serverLog,
}) => {
  test.setTimeout(180_000);
  const reportName = "Platform monthly review";
  const introduction = "Platform's month: where the deploy and cost work stands, and what needs a hand.";

  const elena = await as(leader);
  const typed = await trackTyping(elena);
  let reportURL = "";

  await test.step("1 She builds a Report Definition, and the rail lists the Goals its rule selects", async () => {
    const platform = seedLookup<Goal>(platformGoals);
    expect(platform.length, "the seed has Platform Goals").toBeGreaterThan(0);

    await elena.goto("/reports/new");
    const builder = elena.getByTestId("report-builder");
    await builder.getByLabel("Name").fill(reportName);
    await builder.getByLabel("Introduction").fill(introduction);

    // The rule is picked, not typed, in the blank row that's there by default.
    const rule = builder.getByTestId("report-rule");
    await expect(rule).toHaveCount(1);
    await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
    await rule.getByLabel("Operator").selectOption({ label: "is any of" });
    await rule.getByLabel("Values").selectOption({ label: "Platform" });

    const rail = elena.getByTestId("report-matches");
    await expect(rail.getByTestId("matches-count")).toHaveText(`Matches ${platform.length} Goals`);
    // Exactly those Goals: as many rows as Goals, and each Goal in one.
    await expect(rail.getByTestId("match")).toHaveCount(platform.length);
    for (const g of platform) {
      await expect(rail.getByTestId("match").filter({ hasText: g.title })).toHaveCount(1);
    }

    await builder.getByRole("button", { name: "Save report" }).click();
    await expect(elena.getByTestId("draft-header").getByTestId("report-name")).toHaveText(reportName);
    reportURL = new URL(elena.url()).pathname;
    expect(reportURL).toMatch(/^\/reports\/\d+$/);
  });

  // From the draft to the second publication, every box she types in is
  // recorded, for "she wrote only the introduction and her own notes".
  typed.start();

  await test.step("2 The draft preview shows the Health summary, the exceptions in full, then the rest on track", async () => {
    const draft = preview(elena);
    await expectReportOrder(draft, ["health-summary", "report-introduction", "needs-attention", "on-track-section"]);
    await expect(draft.getByTestId("report-introduction")).toHaveText(introduction);

    // It worked if each exception carried its data, its next steps (the Path
    // to Green) and enough context to stand alone.
    const blocks = draft.getByTestId("report-exception");
    const exceptions: string[] = [];
    // What each block should carry is read from the Goal itself.
    for (const block of await blocks.all()) {
      const goal = goalOfBlock(seedLookup, await block.getAttribute("id"));
      exceptions.push(goal.title);
      await expect(block.getByRole("heading", { level: 3 })).toHaveText(goal.title);
      await expect(block.getByTestId("report-owner")).toContainText(goal.owner);
      await expect(block).toContainText(`· ${goal.lifecycle}`);
      await expect(block.getByTestId("report-so-what")).toHaveText(`So What: ${goal.so_what}`);
      await expect(block.getByTestId("report-status")).toHaveText(`Status: ${goal.status}`);
      if (goal.delivery_date) await expect(block.getByTestId("report-due")).toContainText(goal.delivery_date);
      await expect(block.getByTestId("report-milestone")).toHaveCount(goal.milestones);
      await expect(block.getByTestId("report-metric")).toHaveCount(goal.metrics);
      const health = await healthOf(block.getByTestId("report-health"));
      if (health === "Red" || health === "Yellow") {
        await expect(block.getByTestId("report-path-to-green")).toContainText(`Path to Green: ${goal.path_to_green}`);
      }
    }
    expect(exceptions.length, "Platform has exceptions to show in full").toBeGreaterThan(0);

    // Each On track row is a Goal that isn't an exception, and between them
    // they are every Goal the rule selects.
    const lines = draft.getByTestId("on-track").getByTestId("selected-goal");
    const onTrack: string[] = [];
    for (const line of await lines.all()) {
      const goal = goalOfBlock(seedLookup, await line.getAttribute("id"));
      onTrack.push(goal.title);
      // Not Red or Yellow: those always earn a block. An On Hold Goal has no
      // Health, and stays on one line unless something about it changed.
      expect(["Red", "Yellow"]).not.toContain(await healthOf(line.getByTestId("selected-health")));
    }
    expect(onTrack.filter((t) => exceptions.includes(t))).toEqual([]);
    expect([...exceptions, ...onTrack].sort()).toEqual(
      seedLookup<Goal>(platformGoals)
        .map((g) => g.title)
        .sort(),
    );
  });

  let firstPublication = "";
  // firstPublishedOn is the day the first publication says it was published,
  // in UTC, the server's timezone: the baseline once there's a second draft.
  let firstPublishedOn = "";
  await test.step("3 Publish… opens a confirmation, and confirming it publishes", async () => {
    await publish(elena, reportName);
    firstPublication = new URL(elena.url()).pathname;
    const published = elena.getByTestId("report-published");
    await expect(published).toContainText("by Priya Raman");
    firstPublishedOn = /Published (\d{4}-\d{2}-\d{2}) /.exec((await published.textContent()) ?? "")?.[1] ?? "";
    expect(firstPublishedOn, "the publication says the day it was published").not.toBe("");
  });

  // The Goals the month's changes happen to: one slips, one leaves for another
  // Team, and one's Health turns.
  const slipped = pickCheckinGoal(seedLookup, "Green", "and g.kind = 'Dated'");
  const turning = pickCheckinGoal(seedLookup, "Yellow", `and g.id <> ${slipped.id}`);
  const turnedTo = turning.health === "Yellow" ? "Green" : "Yellow";
  const [moved] = seedLookup<Goal>(
    `select id, title from (${platformGoals}) where id not in (?, ?) order by id limit 1`,
    slipped.id,
    turning.id,
  );

  await test.step("4 Changes since read against her previous publication", async () => {
    // The slip: a later delivery date, with a reason. A later date rules out
    // Green, so the Owner checks in Yellow with a Path to Green.
    const newDate = addDays(slipped.delivery_date, 21);
    const owner = await as(slipped.email);
    await checkIn(owner, slipped.id, {
      health: "Yellow",
      status: "The vendor's API change pushed integration out three weeks.",
      pathToGreen: "Pair with the vendor on the new API; cut the reporting extras.",
      backToGreen: addDays(appToday(), 28),
      deliveryDate: newDate,
      dateReason: "The vendor changed their API under us.",
    });

    // The move: the Admin puts another Platform Goal in a different Team.
    const adminPage = await as(admin);
    await adminPage.goto(`/goals/${moved.id}`);
    await adminPage.getByRole("link", { name: "Edit Dimensions" }).click();
    const team = adminPage.getByRole("combobox", { name: "Team", exact: true });
    await team.selectOption({ label: "Payments" });
    await team.locator("xpath=ancestor::form").getByRole("button", { name: "Save" }).click();
    await expect(
      adminPage.getByTestId("goal-dimension").filter({ hasText: "Team" }).getByTestId("goal-dimension-value"),
    ).toHaveText("Payments");

    // The turn: a third Goal's Health changes, with no slip.
    const turner = await as(turning.email);
    await checkIn(turner, turning.id, {
      health: turnedTo,
      status:
        turnedTo === "Green"
          ? "Load tests pass at twice peak; back on plan."
          : "Two engineers out; the plan is at risk.",
      ...(turnedTo === "Yellow"
        ? { pathToGreen: "Borrow one engineer from Data for a month.", backToGreen: addDays(appToday(), 21) }
        : {}),
    });

    await elena.goto(reportURL);
    const draft = preview(elena);
    await expect(elena.getByTestId("baseline-label")).toHaveText(/^last publication · /);
    await expectReportOrder(draft, ["health-summary", "needs-attention", "membership-changes"]);

    // The slip is marked: New Date, and the old date struck through.
    const slipBlock = draft.locator(`#goal-${slipped.id}`);
    await expect(slipBlock.getByTestId("report-badge").filter({ hasText: "New Date" })).toBeVisible();
    await expect(slipBlock.getByTestId("report-due").locator("del")).toHaveText(slipped.delivery_date);
    await expect(slipBlock.getByTestId("report-due")).toContainText(newDate);

    // The re-teamed Goal left, and says why.
    const left = draft.getByTestId("membership-change").filter({ hasText: moved.title });
    await expect(left.getByTestId("membership-direction")).toHaveText("Left");
    await expect(left.getByTestId("membership-reason")).toHaveText("no longer matches the rules");

    // The Goals that turned Yellow need attention.
    const yellow = [slipped, ...(turnedTo === "Yellow" ? [turning] : [])];
    for (const g of yellow) {
      const block = draft.getByTestId("needs-attention").locator(`#goal-${g.id}`);
      await expect(block).toBeVisible();
      expect(await healthOf(block.getByTestId("report-health"))).toBe("Yellow");
    }
  });

  await test.step("4 Changes since: a Goal whose Health turned shows what it was", async () => {
    const block = preview(elena).getByTestId("needs-attention").locator(`#goal-${turning.id}`);
    expect(await healthOf(block.getByTestId("report-health"))).toBe(turnedTo);
    await expect(block.getByTestId("report-prior-health")).toHaveText(`· was ${turning.health}`);
    // The slipped Goal was Green at the publication, so it reads "was Green".
    await expect(preview(elena).locator(`#goal-${slipped.id}`).getByTestId("report-prior-health")).toHaveText(
      "· was Green",
    );
  });

  // The Highlights Owners flag after the first publication, between them one
  // of each kind.
  const highlights = [
    {
      goal: slipped,
      kind: "Insight",
      note: "Vendor API churn is our biggest schedule risk this half.",
    },
    {
      goal: slipped,
      kind: "Accomplishment",
      note: "Shipped the dual-write path with no customer impact.",
    },
    { goal: turning, kind: "Miss", note: "The capacity plan missed the holiday traffic forecast." },
  ];
  const note = "Two slips this month trace back to vendors; I'm raising it with procurement.";

  await test.step("5 She composes the narrative from the Owners' Highlights, and adds a note of her own", async () => {
    for (const goal of [slipped, turning]) {
      const page = await as(goal.email);
      await checkIn(page, goal.id, {
        health: goal === slipped ? "Yellow" : turnedTo,
        status: "Steady week; notes in the Highlights.",
        highlights: highlights.filter((h) => h.goal === goal),
      });
    }

    await elena.goto(reportURL);
    const curation = elena.getByTestId("narrative-curation");
    // Only the Highlights since the baseline, which is now the first
    // publication: the seed wrote none, so exactly those just flagged.
    await expect(curation.getByRole("heading", { name: `Highlights since ${firstPublishedOn}` })).toBeVisible();
    await expect(curation.getByTestId("curation-highlight")).toHaveCount(highlights.length);
    for (const h of highlights) {
      const row = curation.getByTestId("curation-highlight").filter({ hasText: h.note });
      await row.getByRole("checkbox", { name: h.note }).check();
      await row.getByRole("radio", { name: h.kind, exact: true }).check();
    }
    await curation.getByTestId("section-text-Insight").getByTestId("section-text-summary").click();
    await curation.getByLabel("Insights, note 1").fill(note);
    // It autosaves once she pauses, and the preview follows.
    await expect(preview(elena).getByTestId("narrative-text")).toHaveText([note]);
    await expect(elena.getByTestId("save-result")).toHaveText("Saved");
    await expect(elena.getByTestId("curation-count")).toHaveText(`${highlights.length} of ${highlights.length} in`);

    await elena.reload();
    for (const h of highlights) {
      const row = curation.getByTestId("curation-highlight").filter({ hasText: h.note });
      await expect(row.getByRole("checkbox", { name: h.note })).toBeChecked();
      await expect(row.getByRole("radio", { name: h.kind, exact: true })).toBeChecked();
    }
    await expect(curation.getByLabel("Insights, note 1")).toHaveValue(note);

    // The preview follows: her note, and each Highlight in its section,
    // credited to the Goal's Owner.
    const draft = preview(elena);
    await expectReportOrder(draft, ["health-summary", "report-introduction", "report-narrative", "needs-attention"]);
    const narrative = draft.getByTestId("report-narrative");
    await expect(narrative.getByTestId("narrative-section")).toHaveText(["Insights", "Accomplishments", "Misses"]);
    await expect(narrative.getByTestId("narrative-text")).toHaveText([note]);
    for (const h of highlights) {
      const item = narrative.getByTestId("narrative-highlight").filter({ hasText: h.note });
      await expect(item.getByTestId("highlight-credit")).toHaveText(`— ${h.goal.owner}, ${h.goal.title}`, {
        useInnerText: true,
      });
    }
  });

  let secondPublication = "";
  await test.step("6 The second publication is frozen, and shared as Markdown and printed", async () => {
    await elena.goto(reportURL);
    await publish(elena, reportName);
    secondPublication = new URL(elena.url()).pathname;
    expect(secondPublication).not.toBe(firstPublication);

    // What the publication carries, as the exports must too.
    const content = await coreContent(elena.getByTestId("report-snapshot"));
    expect(content.introduction).toBe(introduction);
    expect(content.narrative).toEqual(expect.arrayContaining([note, ...highlights.map((h) => h.note)]));
    expect(content.exceptions.length).toBeGreaterThan(0);

    // A later Check-in doesn't change it.
    const block = elena.getByTestId("report-snapshot").locator(`#goal-${turning.id}`);
    const before = await block.getByTestId("report-status").textContent();
    const later = "A later Check-in, after the publication was frozen.";
    await checkIn(await as(turning.email), turning.id, {
      health: turnedTo,
      status: later,
    });
    await elena.reload();
    await expect(block.getByTestId("report-status")).toHaveText(before ?? "");
    await expect(elena.getByTestId("report-snapshot")).not.toContainText(later);

    // Markdown is served as an attachment, so it's fetched, not opened.
    const md = await elena.request.get(`${secondPublication}/markdown`);
    expect(md.ok()).toBe(true);
    expect(md.headers()["content-disposition"]).toMatch(/^attachment/);
    const markdown = await md.text();
    expect(markdown).not.toContain(later);
    for (const text of coreTexts(content))
      expect(markdown, "the Markdown carries the publication's content").toContain(text);

    await elena.goto(`${secondPublication}/print`);
    const printed = elena.getByTestId("report-snapshot");
    await expect(elena.getByTestId("report-name")).toHaveText(reportName);
    for (const text of coreTexts(content))
      await expect(printed, "the print view carries the publication's content").toContainText(text);
    await expect(printed).not.toContainText(later);

    // It worked if she wrote only the introduction and her own notes: from
    // the draft to here, the only box she typed in was a section's note.
    typed.stop();
    expect(typed.names()).toContain("text-Insight");
    for (const name of typed.names())
      expect(name, "she typed only the introduction or a note").toMatch(/^(introduction|text-\w+)$/);
  });

  const question = "What would it take to hold the new date if the vendor slips again?";
  const answer = "A fallback to the old API for one more quarter; I'll cost it by Friday.";

  await test.step("7 A reader comments on a Yellow Goal's block, and its Owner replies in the thread", async () => {
    const dana = await as(reader);
    await dana.goto(secondPublication);
    const block = dana.getByTestId("report-snapshot").locator(`#goal-${slipped.id}`);
    expect(await healthOf(block.getByTestId("report-health"))).toBe("Yellow");
    const discussion = block.getByTestId("goal-discussion");
    await discussion.getByTestId("comment-toggle").locator("summary").click();
    await discussion.getByRole("textbox", { name: "Comment" }).fill(question);
    await discussion.getByRole("button", { name: "Comment" }).click();
    await expect(discussion.getByTestId("comment-count")).toHaveText("1 comment");

    // It worked if a question reached the right Owner without anyone looking
    // up who owns what: the reader named no one, and the one email about it
    // went to the Goal's Owner.
    const commentSubject = `subject="Comment on ${slipped.title} in ${reportName}"`;
    await expect
      .poll(() => mailLines(serverLog(), commentSubject))
      .toEqual([expect.stringContaining(`to=${slipped.email} `)]);

    const owner = await as(slipped.email);
    await owner.goto(secondPublication);
    const thread = owner.getByTestId("report-snapshot").locator(`#goal-${slipped.id}`).getByTestId("goal-discussion");
    await thread.getByTestId("comment-count").click();
    await thread.getByTestId("reply-toggle").locator("summary").click();
    await thread.getByTestId("reply-form").getByRole("textbox", { name: "Reply" }).fill(answer);
    await thread.getByTestId("reply-form").getByRole("button", { name: "Reply" }).click();

    await dana.reload();
    await discussion.getByTestId("comment-count").click();
    await expect(discussion.getByTestId("comment-thread").getByTestId("comment")).toHaveText([
      new RegExp(`Dana Whitfield.*${escapeRegExp(question)}`, "s"),
      new RegExp(`${escapeRegExp(slipped.owner)}.*${escapeRegExp(answer)}`, "s"),
    ]);
    const replySubject = `subject="Reply on ${slipped.title} in ${reportName}"`;
    await expect.poll(() => mailLines(serverLog(), replySubject)).toEqual([expect.stringContaining(`to=${reader} `)]);
  });

  const due = addDays(appToday(), 14);
  await test.step("8 She makes the thread an Action Item, which stays listed until its owner closes it", async () => {
    await elena.goto(secondPublication);
    const discussion = elena
      .getByTestId("report-snapshot")
      .locator(`#goal-${slipped.id}`)
      .getByTestId("goal-discussion");
    await discussion.getByTestId("comment-count").click();
    const make = discussion.getByTestId("make-action-item").first();
    await make.locator("summary").click();
    await make.getByLabel("Owner").fill(slipped.email);
    await make.getByLabel("Due").fill(due);
    await make.getByRole("button", { name: "Make Action Item" }).click();
    await expect(elena.getByTestId("raised-action-item")).toContainText(question);

    // It worked if nothing agreed was lost by the next review: the next
    // publication opens with it, and its exports carry it.
    await elena.goto(reportURL);
    await publish(elena, reportName);
    const thirdPublication = new URL(elena.url()).pathname;
    const open = elena.getByTestId("report-action-items").getByTestId("open-action-item").filter({ hasText: question });
    await expect(open).toContainText(slipped.owner);
    await expect(open).toContainText(`due ${due}`);
    await expectReportOrder(elena.getByRole("main"), ["health-summary", "report-action-items", "report-introduction"]);
    const content = await coreContent(elena.getByTestId("report-snapshot"));
    expect(content.actionItems).toEqual([expect.stringContaining(question)]);
    const markdown = await (await elena.request.get(`${thirdPublication}/markdown`)).text();
    expect(markdown).toContain(question);
    await elena.goto(`${thirdPublication}/print`);
    await expect(elena.getByTestId("report-snapshot")).toContainText(question);

    // Its owner closes it with a note, and the publication after no longer
    // lists it.
    const owner = await as(slipped.email);
    await owner.goto(thirdPublication);
    const item = owner.getByTestId("report-action-items").getByTestId("open-action-item").filter({ hasText: question });
    await item.getByTestId("close-toggle").locator("summary").click();
    await item.getByLabel("Closing note").fill("Fallback costed: two sprints, approved.");
    await item.getByRole("button", { name: "Close" }).click();
    // The publication it was listed on stays as frozen, marking it closed.
    await expect(owner).toHaveURL(new RegExp(`${thirdPublication}(#.*)?$`));
    await expect(item.getByTestId("action-item-closed-since")).toBeVisible();
    await owner.goto(secondPublication);
    await expect(
      owner.getByTestId("raised-action-item").filter({ hasText: question }).getByTestId("action-item-note"),
    ).toHaveText("— Fallback costed: two sprints, approved.");

    await elena.goto(reportURL);
    await publish(elena, reportName);
    await expect(elena.getByTestId("report-snapshot")).not.toContainText(question);
    await expect(elena.getByTestId("report-action-items")).toHaveCount(0);
  });
});

type CoreContent = {
  introduction: string;
  narrative: string[];
  actionItems: string[];
  exceptions: { title: string; pathToGreen: string }[];
};

// coreContent reads what every way of reading a publication must carry: the
// introduction, the narrative's notes and Highlights, the open Action Items,
// and each exception's title with its Path to Green. Not the Health summary or
// the Goals that entered or left.
async function coreContent(report: Locator): Promise<CoreContent> {
  const intro = report.getByTestId("report-introduction");
  const narrative = [
    ...(await report.getByTestId("narrative-text").allTextContents()),
    ...(await report.getByTestId("narrative-highlight").evaluateAll((items) =>
      // The Highlight's own note, without its credit.
      items.map((li) => (li.firstChild?.textContent ?? "").trim()),
    )),
  ].map((t) => t.trim());
  const actionItems = await report
    .getByTestId("open-action-item")
    .evaluateAll((items) => items.map((li) => (li.firstChild?.textContent ?? "").trim()));
  const exceptions = [];
  for (const block of await report.getByTestId("report-exception").all()) {
    const title = (await block.getByRole("heading", { level: 3 }).textContent())?.trim() ?? "";
    const path = (await block.getByTestId("report-path-to-green").count())
      ? ((await block.getByTestId("report-path-to-green").textContent()) ?? "")
      : "";
    const plan = path.match(/Path to Green:\s*(.*?)\s*\(back to Green by/s)?.[1] ?? "";
    exceptions.push({ title, pathToGreen: plan });
  }
  return {
    introduction: (await intro.count()) ? ((await intro.textContent()) ?? "").trim() : "",
    narrative,
    actionItems,
    exceptions,
  };
}

// coreTexts are the strings in a publication's core content, each to be
// found in an export.
function coreTexts(c: CoreContent): string[] {
  return [
    c.introduction,
    ...c.narrative,
    ...c.actionItems,
    ...c.exceptions.flatMap((e) => [e.title, e.pathToGreen]),
  ].filter((t) => t !== "");
}

// mailLines are the lines of the server's log recording an email with the
// subject given, as slog writes it (subject="…").
function mailLines(log: string, subject: string): string[] {
  return log.split("\n").filter((l) => l.includes("email send") && l.includes(subject));
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// trackTyping records each box the page's person types text into, by its
// name, from start on: an input event on a textarea or a text-like input.
// Ticking a box or picking a radio or an option isn't typing.
async function trackTyping(page: Page) {
  const names: string[] = [];
  let on = false;
  await page.context().exposeBinding("recordTyping", (_source, name: string) => {
    if (on) names.push(name);
  });
  await page.context().addInitScript(() => {
    document.addEventListener(
      "input",
      (e) => {
        const t = e.target as HTMLInputElement;
        const text =
          t instanceof HTMLTextAreaElement ||
          (t instanceof HTMLInputElement && !["checkbox", "radio"].includes(t.type));
        if (text) (window as unknown as { recordTyping: (n: string) => void }).recordTyping(t.name);
      },
      true,
    );
  });
  return {
    start: () => (on = true),
    stop: () => (on = false),
    names: () => [...names],
  };
}

// pickCheckinGoal picks a Platform Goal whose Owner can check in on it
// plainly, with the Health given first if one has it: Active, its Owner still
// here, not Stale, with no accepted contributors (so no Rolled-up Health asks
// for an explanation) and no overdue Planned Milestone.
function pickCheckinGoal(seedLookup: SeedLookup, health: string, extra = ""): GoalDetail & { health: string } {
  const [row] = seedLookup<{ id: number }>(
    `select g.id from (${platformGoals}) p join goals g on g.id = p.id join accounts a on a.id = g.owner_id
     where g.lifecycle = 'Active' and a.departed = 0
       and (select max(created_at) from checkins c where c.goal_id = g.id)
           > strftime('%Y-%m-%dT%H:%M:%SZ', $now, '-' || g.cadence_days || ' days')
       and not exists (select 1 from links l where l.parent_id = g.id and l.status = 'accepted')
       and not exists (select 1 from milestones m
                       where m.goal_id = g.id and m.status = 'Planned' and m.target_date < date($now))
       ${extra}
     order by (select health from checkins c where c.goal_id = g.id order by created_at desc, id desc limit 1) = ? desc, g.id
     limit 1`,
    health,
  );
  expect(row, "Platform has a Goal its Owner can check in on").toBeDefined();
  const goal = goalOfBlock(seedLookup, `goal-${row.id}`);
  const [latest] = seedLookup<{ health: string }>(
    "select health from checkins where goal_id = ? order by created_at desc, id desc limit 1",
    row.id,
  );
  return { ...goal, health: latest.health };
}

type CheckinInput = {
  health: string;
  status: string;
  pathToGreen?: string;
  backToGreen?: string;
  deliveryDate?: string;
  dateReason?: string;
  highlights?: { kind: string; note: string }[];
};

// checkIn submits a Check-in on the Goal as the page's person, through the
// Check-in form, and waits for the Goal page.
async function checkIn(page: Page, goalID: number, c: CheckinInput) {
  await page.goto(`/goals/${goalID}/checkin`);
  const form = page.getByTestId("checkin-form");
  await form.getByTestId("checkin-health").getByText(c.health, { exact: true }).click();
  if (c.pathToGreen) await form.getByRole("textbox", { name: "Plan", exact: true }).fill(c.pathToGreen);
  if (c.backToGreen) await form.getByLabel("Back to Green by").fill(c.backToGreen);
  await form.getByRole("textbox", { name: /^Status/ }).fill(c.status);
  if (c.deliveryDate) {
    await form.getByTestId("checkin-dates-section").locator("summary").click();
    await form.getByLabel("Delivery date", { exact: true }).fill(c.deliveryDate);
    await form.getByLabel("Reason (required if the delivery date changes)").fill(c.dateReason ?? "");
  }
  for (const [i, h] of (c.highlights ?? []).entries()) {
    const section = form.getByTestId("checkin-highlight-section");
    if (i === 0) {
      await section.locator("summary").click();
    } else {
      await section.getByRole("button", { name: "Add another" }).click();
    }
    const row = page.getByTestId("checkin-highlight-row").nth(i);
    await row.getByLabel("Kind").selectOption({ label: h.kind });
    await row.getByLabel("Note").fill(h.note);
  }
  await page.getByTestId("checkin-form").getByRole("button", { name: "Submit check-in" }).click();
  await page.waitForURL(new RegExp(`/goals/${goalID}$`));
}

// addDays is the YYYY-MM-DD date days after date.
function addDays(date: string, days: number): string {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

// preview is the draft page's preview: exactly what readers will see.
function preview(page: Page) {
  return page.getByTestId("draft-panes").getByTestId("report-draft");
}

// reportSections are the parts of a Report, published or previewed, in the
// order a reader meets them.
const reportSections = [
  "health-summary",
  "report-action-items",
  "report-introduction",
  "report-narrative",
  "needs-attention",
  "on-track-section",
  "membership-changes",
];

// expectReportOrder checks that each of required is in the Report, and that
// the Report's parts that are there come in reportSections' order.
async function expectReportOrder(report: Locator, required: string[]) {
  for (const id of required) await expect(report.getByTestId(id), `the Report shows ${id}`).toHaveCount(1);
  const present: Locator[] = [];
  for (const id of reportSections) {
    if ((await report.getByTestId(id).count()) > 0) present.push(report.getByTestId(id));
  }
  for (let i = 1; i < present.length; i++) {
    const after = await present[i].elementHandle();
    const follows = await present[i - 1].evaluate(
      (a, b) => !!(b && a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING),
      after,
    );
    expect(
      follows,
      `${await present[i - 1].getAttribute("data-testid")} comes before ${await present[i].getAttribute("data-testid")}`,
    ).toBe(true);
  }
}

// healthOf is the Health a badge names, without the shape the print page
// marks it with.
async function healthOf(badge: Locator): Promise<string> {
  return ((await badge.textContent()) ?? "").replace(/[^A-Za-z ]/g, "").trim();
}

type GoalDetail = Goal & {
  owner: string;
  email: string;
  lifecycle: string;
  so_what: string;
  delivery_date: string;
  status: string;
  path_to_green: string;
  milestones: number;
  metrics: number;
};

// goalOfBlock looks up the Goal a Report's block or line is about, by its
// anchor ("goal-12"), for what it should show.
function goalOfBlock(seedLookup: SeedLookup, anchor: string | null): GoalDetail {
  const id = Number((anchor ?? "").replace("goal-", ""));
  const [goal] = seedLookup<GoalDetail>(
    `select g.id, g.title, coalesce(a.name, a.email) owner, a.email, g.lifecycle, g.so_what, g.delivery_date,
            c.status, c.path_to_green,
            (select count(*) from milestones m where m.goal_id = g.id) milestones,
            (select count(*) from metrics m where m.goal_id = g.id) metrics
     from goals g join accounts a on a.id = g.owner_id
     left join checkins c on c.id = (select id from checkins where goal_id = g.id order by created_at desc, id desc limit 1)
     where g.id = ?`,
    id,
  );
  expect(goal, `a Goal behind ${anchor}`).toBeDefined();
  return goal;
}

type SeedLookup = <T = Record<string, unknown>>(sql: string, ...params: (string | number)[]) => T[];

// publish presses Publish… on the draft page, checks the confirmation names
// the Report, and confirms, landing on the publication.
async function publish(page: Page, reportName: string) {
  const confirm = page.getByTestId("publish-confirm");
  await confirm.getByText("Publish…").click();
  await expect(confirm.getByRole("heading", { name: `Publish ${reportName}?` })).toBeVisible();
  await confirm.getByRole("button", { name: "Publish", exact: true }).click();
  await page.waitForURL(/\/reports\/\d+\/publications\/\d+$/);
}
