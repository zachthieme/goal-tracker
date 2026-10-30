// Scenario 2, "Anyone can do this, over any Goals" (docs/scenarios.md), with
// the decisions under "Settled along the way" that go with it. A manager
// builds one Report by a rule over their own team's Goals and another from a
// hand-picked list of Goals they don't own; putting a Goal in a Report gives
// them nothing over it, and another lead can read both Reports but not change,
// curate or publish them until the manager makes them a Report Delegate.
//
// The scenario's Marcus is growth-lead@example.com, whom the seed names Elena
// Petrova (cpo@ is the seed's Marcus Bell).
import type { Page } from "@playwright/test";

import { admin, appToday, expect, type Manifest, requirePick, signIn, test } from "../fixtures";

const manager = "growth-lead@example.com";
// otherLead is another lead, who neither built nor administers the Reports.
const otherLead = "data-lead@example.com";

type SeedGoal = { id: number; title: string };

// pickedGoals are the manifest's watchingGoals.picked: four Active Goals on
// teams other than Growth that the manager neither owns nor is a Delegate on.
// troubled is Yellow or Red, with the Path to Green its Owner last submitted;
// slipped has slipped its delivery date, and is Yellow or Red, so it carries a
// full block in the Report; green has no children and no overdue Milestone;
// and parent has Active child Goals contributing to it, none of them picked.
function pickedGoals(manifest: Manifest) {
  const p = manifest.watchingGoals.picked;
  return {
    troubled: requirePick("watchingGoals.picked.troubled", p.troubled),
    slipped: requirePick("watchingGoals.picked.slipped", p.slipped),
    green: requirePick("watchingGoals.picked.green", p.green),
    parent: requirePick("watchingGoals.picked.parent", p.parent),
    children: p.children,
  };
}

// buildPickedReport saves a Report Definition named name in "Goals I pick"
// mode, picking each of goals by searching for its title, and lands on its
// draft. It returns the Report Definition's id.
async function buildPickedReport(page: Page, name: string, goals: SeedGoal[]): Promise<number> {
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  await builder.getByLabel("Name").fill(name);
  await builder.getByTestId("report-mode").getByText("Goals I pick").click();
  const picker = builder.getByTestId("picked-picker");
  for (const g of goals) {
    await picker.getByLabel("Search Goals: Goals I pick").fill(g.title);
    await picker.getByTestId("report-goal-result").getByRole("button", { name: g.title }).click();
    await expect(picker.getByTestId("picked-chip").filter({ hasText: g.title })).toBeVisible();
  }
  return saveReport(page);
}

// reportDate is the YYYY-MM-DD date as a Report shows it, always with its
// year: "Nov 7, 2026".
function reportDate(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  const month = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"][m - 1];
  return `${month} ${d}, ${y}`;
}

// checkinFields are a Check-in form's fields, valid for any Active Goal: Yellow,
// so no overdue Milestone or Rolled-up Health refuses it, with a Path to
// Green back by three weeks from the app's today.
function checkinFields(): Record<string, string> {
  const back = new Date(Date.parse(appToday()) + 21 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
  return {
    health: "Yellow",
    status: "Written by someone who doesn't own the Goal",
    path_to_green: "Not mine to promise",
    path_target_date: back,
  };
}

// buildRuleReport saves a Report Definition named name with the rule Team is
// any of Growth, and lands on its draft. It returns the Report Definition's id.
async function buildRuleReport(page: Page, name: string): Promise<number> {
  await page.goto("/reports/new");
  const builder = page.getByTestId("report-builder");
  await builder.getByLabel("Name").fill(name);
  await builder.getByTestId("report-mode").getByText("Goals matching rules").click();
  const rule = builder.getByTestId("report-rule").first();
  await rule.getByLabel("What the rule tests").selectOption({ label: "Team" });
  await rule.getByLabel("Operator").selectOption({ label: "is any of" });
  await rule.getByTestId("rule-values").getByRole("checkbox", { name: "Growth", exact: true }).check();
  return saveReport(page);
}

// saveReport presses the builder's Save report and returns the id of the
// Report Definition whose draft it lands on.
async function saveReport(page: Page): Promise<number> {
  await page.getByTestId("report-builder").getByRole("button", { name: "Save report" }).click();
  await page.waitForURL(/\/reports\/\d+$/);
  return Number(new URL(page.url()).pathname.split("/").pop());
}

// reportGoalTitles are the titles of the Goals the draft Report holds: each
// exception's block and each On track row.
async function reportGoalTitles(page: Page): Promise<string[]> {
  const draft = page.getByTestId("report-draft");
  const blocks = await draft.getByTestId("report-exception").locator("h3").allTextContents();
  const lines = await draft.getByTestId("selected-goal").locator(".rp-track-goal > a").allTextContents();
  return [...blocks, ...lines].map((title) => title.trim());
}

// goalCount is how the draft's meta line counts n Goals: " · 6 Goals · ".
function goalCount(n: number): string {
  return ` · ${n} ${n === 1 ? "Goal" : "Goals"} · `;
}

test("a manager builds Reports over Goals they don't own, and gets nothing over them", async ({
  page,
  as,
  manifest,
}) => {
  // Three people and two Reports, start to finish.
  test.slow();
  await signIn(page, manager);
  const ruleReportName = "Growth team";
  const pickedReportName = "Critical projects we depend on";
  let ruleReport = 0;
  let pickedReport = 0;

  await test.step("New starts with the manager's own org, in view in its value list", async () => {
    const me = { id: requirePick(`accountIDs[${manager}]`, manifest.accountIDs[manager]) };
    // The cut-off was seen at both widths (ticket #314).
    for (const width of [1280, 390]) {
      await page.setViewportSize({ width, height: 720 });
      await page.goto("/reports/new");
      const values = page.getByTestId("report-rule").first().getByTestId("rule-values");
      const checked = values.locator("input:checked");
      await expect(checked).toHaveAttribute("value", `org=${me.id}`);
      await expect(checked).toHaveAttribute("type", "radio");
      // It worked if: the chosen author is scrolled to the list's top row,
      // or, near the list's end, the list is scrolled as far as it goes with
      // the author wholly in view. Left unscrolled, it can sit out of view
      // below the list's bottom edge. The script scrolls again once web
      // fonts load, so poll.
      await expect
        .poll(
          async () =>
            await values.locator("[data-rule-values]").evaluate((list: HTMLElement) => {
              const option = list.querySelector("input:checked")!.closest("label")!.getBoundingClientRect();
              const box = list.getBoundingClientRect();
              const atTop = Math.abs(option.top - box.top - list.clientTop) <= 1;
              const atEnd = list.scrollTop >= list.scrollHeight - list.clientHeight - 1;
              const inView =
                option.top >= box.top + list.clientTop - 1 &&
                option.bottom <= box.top + list.clientTop + list.clientHeight + 1;
              return atTop || (atEnd && inView);
            }),
          { message: `at ${width}px the author is the list's top row, or in view at its end` },
        )
        .toBe(true);
    }
    await page.setViewportSize({ width: 1280, height: 720 });
  });

  await test.step("1 A rule Report lists the team's Goals, less those finished before the baseline", async () => {
    // The Team = Growth Goals: inReport those a Report reading against its
    // default baseline, 30 days before the app starts, lists, every one but
    // those Done or Cancelled more than 30 days ago, and finishedEarlier
    // those that leave it.
    const { inReport, finishedEarlier } = manifest.watchingGoals.growthReport;
    expect(finishedEarlier.length, "watchingGoals.growthReport.finishedEarlier has a Growth Goal").toBeGreaterThan(0);

    ruleReport = await buildRuleReport(page, ruleReportName);

    await expect(page.getByTestId("report-meta")).toContainText(goalCount(inReport.length));
    // It worked if: the Report lists every Team = Growth Goal, and a Goal
    // finished before the baseline leaves it.
    const titles = await reportGoalTitles(page);
    expect(titles.sort()).toEqual(inReport.map((g) => g.title).sort());
    for (const g of finishedEarlier) expect(titles, `"${g.title}" finished before the baseline`).not.toContain(g.title);
  });

  const picks = pickedGoals(manifest);
  const picked = [picks.troubled, picks.slipped, picks.green, picks.parent];

  await test.step("2 A hand-picked Report shows the Health, slips and Paths to Green of Goals from other teams", async () => {
    pickedReport = await buildPickedReport(page, pickedReportName, picked);

    // The draft lists exactly the picked Goals.
    await expect(page.getByTestId("report-meta")).toContainText(goalCount(picked.length));
    expect((await reportGoalTitles(page)).sort()).toEqual(picked.map((g) => g.title).sort());

    // It worked if: the Report shows him their Health and Paths to Green as
    // their Owners report them.
    const preview = page.getByTestId("report-draft");
    const troubled = preview.getByTestId("report-exception").filter({ hasText: picks.troubled.title });
    await expect(troubled.getByTestId("report-health")).toHaveText(new RegExp(`${picks.troubled.health}$`));
    await expect(troubled.getByTestId("report-path-to-green")).toContainText(picks.troubled.path_to_green);

    // ... and their slips: the earlier delivery date, struck through.
    const slipped = preview.getByTestId("report-exception").filter({ hasText: picks.slipped.title });
    await expect(slipped.getByTestId("report-due").getByRole("deletion").first()).toHaveText(
      reportDate(picks.slipped.slipped_from),
    );
  });

  await test.step("3 No links followed: the picked parent's children aren't in the Report", async () => {
    expect(picks.children.length).toBeGreaterThan(0);
    const titles = await reportGoalTitles(page);
    expect(titles).toContain(picks.parent.title);
    // It worked if: a Report's Goals come from a hand-picked list, never
    // from following Contributes to links (ADR 0007).
    for (const child of picks.children) {
      expect(titles, `child Goal "${child.title}" isn't in the Report`).not.toContain(child.title);
      await expect(page.getByTestId("report-draft").getByRole("link", { name: child.title, exact: true })).toHaveCount(
        0,
      );
    }
  });

  await test.step("4 Picking a Goal gives the manager no power over it", async () => {
    // It worked if: only its Owner or a Delegate checks in on it or changes
    // it.
    for (const g of picked) {
      await page.goto(`/goals/${g.id}`);
      await expect(page.getByRole("heading", { level: 1, name: g.title })).toBeVisible();
      const actions = page.getByTestId("goal-actions");
      await expect(actions.getByTestId("checkin-link")).toHaveCount(0);
      await expect(actions.getByRole("button", { name: "Check in: no change" })).toHaveCount(0);
      await expect(page.getByTestId("open-milestones")).toHaveCount(0);
      await expect(page.getByTestId("open-delegates")).toHaveCount(0);

      const more = page.getByTestId("goal-more");
      await more.getByTitle("More actions").click();
      const offered = await more.getByRole("link").allTextContents();
      for (const owners of ["Hand off", "Add a delegate", "Contribute to a Goal", "Edit Dimension values"]) {
        expect(offered, `"${g.title}" offers the manager no "${owners}"`).not.toContain(owners);
      }
      // Anyone may suggest a parent for an Active Goal, or start a Goal that
      // contributes to it.
      expect(offered).toEqual(expect.arrayContaining(["Suggest a parent", "New contributing Goal"]));
    }

    // A Check-in posted straight to the Goal is refused, and records nothing.
    const target = picks.green;
    await page.goto(`/goals/${target.id}`);
    const latestCheckin = page.getByTestId("history-entry").and(page.locator('[data-kind="checkin"]')).first();
    const before = await latestCheckin.textContent();
    const res = await page.request.post(`/goals/${target.id}/checkins`, {
      form: checkinFields(),
      maxRedirects: 0,
    });
    expect(res.status(), "a non-Owner's Check-in is refused").toBe(403);
    await page.reload();
    expect(await latestCheckin.textContent(), "the latest Check-in is unchanged").toBe(before);
  });

  await test.step("5 Every Report Definition is visible to everyone; only its creator, a Report Delegate or an Admin changes it", async () => {
    const reports = [
      { id: ruleReport, name: ruleReportName },
      { id: pickedReport, name: pickedReportName },
    ];
    const lead = await as(otherLead);

    // Every Report Definition is visible to everyone.
    await lead.goto("/reports");
    for (const r of reports) {
      await expect(lead.getByTestId("report-row").getByRole("link", { name: r.name, exact: true })).toBeVisible();
    }

    // Only its creator, a Report Delegate or an Admin may change it, curate
    // its narrative or publish it; anyone else still reads the draft.
    for (const r of reports) {
      await lead.goto(`/reports/${r.id}`);
      await expect(lead.getByTestId("report-name")).toHaveText(r.name);
      await expect(lead.getByTestId("report-draft")).toBeVisible();
      await expect(lead.getByTestId("edit-definition")).toHaveCount(0);
      await expect(lead.getByTestId("narrative-curation")).toHaveCount(0);
      await expect(lead.getByTestId("publish-confirm")).toHaveCount(0);
      await expect(lead.getByTestId("add-report-delegate")).toHaveCount(0);
      // The header has no History menu; History is in the narrow column, for
      // anyone who can open the Report.
      await expect(lead.getByTestId("draft-header").getByTestId("report-history")).toHaveCount(0);
      await expect(lead.getByTestId("draft-side").getByTestId("report-history")).toBeVisible();
      expect((await lead.request.get(`/reports/${r.id}/edit`)).status(), `GET edit of "${r.name}"`).toBe(403);
      const post = await lead.request.post(`/reports/${r.id}/edit`, {
        form: {
          id: String(r.id),
          name: "Taken over",
          introduction: "",
          mode: "picked",
          picked: String(picks.green.id),
        },
        maxRedirects: 0,
      });
      expect(post.status(), `POST edit of "${r.name}"`).toBe(403);
      const curate = await lead.request.post(`/reports/${r.id}/narrative`, {
        form: { "text-Insight": "Taken over" },
        maxRedirects: 0,
      });
      expect(curate.status(), `POST narrative of "${r.name}"`).toBe(403);
      const publish = await lead.request.post(`/reports/${r.id}/publications`, { maxRedirects: 0 });
      expect(publish.status(), `POST publish of "${r.name}"`).toBe(403);
    }

    // The manager makes the other lead a Report Delegate on the picked Report.
    await page.goto(`/reports/${pickedReport}`);
    const delegates = page.getByTestId("report-delegates");
    await expect(delegates.getByTestId("no-report-delegates")).toBeVisible();
    await delegates.getByLabel("Report Delegate email").fill(otherLead);
    await delegates.getByRole("button", { name: "Add Delegate" }).click();
    await expect(page.getByTestId("report-delegates").getByTestId("report-delegate")).toHaveCount(1);

    // The other lead, now a Report Delegate, composes a note, saves it and
    // publishes it.
    await lead.goto(`/reports/${pickedReport}`);
    await expect(lead.getByTestId("edit-definition").first()).toBeVisible();
    await expect(lead.getByTestId("add-report-delegate")).toHaveCount(0);
    const curation = lead.getByTestId("narrative-curation");
    await curation.getByTestId("section-text-Insight").getByTestId("section-text-summary").click();
    await curation.getByLabel("Insights, note 1").fill("Two of these are what our launch waits on.");
    await curation.getByRole("button", { name: "Save narrative" }).click();
    await expect(lead.getByTestId("save-result")).toHaveText("Saved");
    await lead.reload();
    await expect(curation.getByLabel("Insights, note 1")).toHaveValue("Two of these are what our launch waits on.");
    await lead.getByTestId("publish-confirm").getByText("Publish…").click();
    await lead.getByTestId("report-publish").getByRole("button", { name: "Publish" }).click();
    await lead.waitForURL(new RegExp(`/reports/${pickedReport}/publications/\\d+$`));
    await expect(lead.getByTestId("report-name")).toHaveText(pickedReportName);
    await expect(lead.getByTestId("report-narrative").getByTestId("narrative-text")).toHaveText(
      "Two of these are what our launch waits on.",
    );

    // The draft's History reads newest first: the Draft, then her
    // publication, linking to it, then the narrative she saved for it.
    const publication = new URL(lead.url()).pathname;
    await lead.goto(`/reports/${pickedReport}`);
    const history = lead.getByTestId("draft-side").getByTestId("report-history");
    await expect(history.getByTestId("report-history-draft")).toContainText("Draft");
    const entries = history.getByTestId("report-history-entry");
    await expect(entries.first().getByRole("link", { name: "Published", exact: true })).toHaveAttribute(
      "href",
      publication,
    );
    await expect(entries.nth(1)).toContainText("Narrative: Insights notes changed");
    await expect(history.getByText("Published", { exact: true })).toHaveCount(1);

    // The Admin sees Edit definition and can save an edit.
    const adminPage = await as(admin);
    await adminPage.goto(`/reports/${ruleReport}`);
    await adminPage.getByTestId("edit-definition").first().click();
    await adminPage.waitForURL(`**/reports/${ruleReport}/edit`);
    await adminPage.getByTestId("report-builder").getByLabel("Name").fill("Growth team, as the Admin left it");
    expect(await saveReport(adminPage)).toBe(ruleReport);
    await expect(adminPage.getByTestId("report-name")).toHaveText("Growth team, as the Admin left it");
  });
});
