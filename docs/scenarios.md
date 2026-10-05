# Keystone scenarios

What Goal Tracker is for, as five scenarios: the one experience the product
exists to deliver, and the mechanisms that feed it. Written from `CONTEXT.md`,
the ADRs, the routes and the pages, and confirmed with Zach in October 2026.
Terms are `CONTEXT.md`'s. Judge a proposed change by whether it serves these.

## The point

**Goal Tracker exists so a leader finds trouble.** Everything else is
mechanism: Goals, Check-ins and links are how the trouble gets into the tool,
and the pages a leader reads are how it gets out.

Two kinds of people use it. **Frontline and mid-level managers** own the Goals
and supply the data through Check-ins. **VPs and above** consume it. The
managers' side has to be cheap; the leaders' side has to be easy to read.

Reading isn't reserved for leaders, though. Anyone can build a Report over any
Goals: the ones they own, or ones they only watch.

So there is one keystone experience, a leader finding trouble, and it happens
in two places. The other three scenarios are the mechanisms that feed it. Each
is judged by one question: does it make trouble easier to find, or harder to
hide?

| # | Scenario | Who | Role in the product |
| - | -------- | --- | ------------------- |
| 1 | Finding trouble between reviews | Leader | **The point.** Goals give the data, Risks gives the context. |
| 2 | Understanding trouble at the review | Anyone; here a leader | **The point**, in depth: the detail, the next steps and the surrounding challenges. |
| 3 | The weekly Check-in | Manager, as Owner | Mechanism. Keeps the data fresh at a cost managers will pay. |
| 4 | Reporting bad news | Manager, as Owner | Mechanism. Makes trouble stated, planned for and dated. |
| 5 | Stating a Goal and connecting it | Manager, parent's Owner | Mechanism. Sets what can be judged and who hears about it. |

The people below are roles, not Accounts in the seed.

## 1. Finding trouble between reviews

**Who:** Elena, a VP with about sixty Goals under her org.
**Promise:** she learns what is in trouble and why without anyone telling her,
and each problem sits beside the one action that moves it.

She uses two pages together. **Goals** is the data: what exists and how each
Goal is doing. **Risks** is the context: which of those need attention, why,
and who has to act.

1. The top bar shows a count on **Risks** before she clicks anything.
2. She opens **Goals**. The list is led by Health, problems first. She filters
   to her org by a Dimension and groups by another, and reads the shape: how
   many Red, how many Yellow, which rows carry a Stale or Ownerless mark.
3. She opens **Risks** for the reasons. It says how many Goals need attention,
   in three groups by who has to act: **Owner needs to update** (Stale, Path
   to Green overdue), **Plan doesn't fit** (Unaligned, schedule conflicts),
   **Needs an Admin** (Ownerless, parent On Hold or Cancelled).
4. She narrows it to the same org. Each flagged Goal is one row, worst first,
   with a chip for each reason and one **Fix**.
5. A Goal she doesn't own has gone Stale. The Fix is **Nudge**: its Owner and
   Delegates are asked to check in, and it can't be nudged again that day.
6. A Red Goal catches her eye. She opens it: the Owner's latest status and
   Path to Green, Rolled-up Health beside the Owner's Health, struck-through
   dates, and the History of how it got here.

**It worked if:** a Goal nobody has updated is as loud as a Red one; she could
tell "in trouble" from "silent" from "badly placed"; every row answered "what
do I do about it?"; she didn't wait for the review to find out.

## 2. Understanding trouble at the review

**Who:** Elena again, publishing her own org's Report. It is shared as far up
as it needs to go: her boss, and their boss.
**Promise:** Risks told her which Goals are in trouble. The Report gives the
rest: the detailed data on each, the Owner's next steps, and the context of
what other challenges sit around it. It writes itself from the managers'
Check-ins, so Elena curates and doesn't chase or retype, and a reader two
levels up can follow it without knowing the org.

1. Once, she builds a Report Definition: a name, an introduction, and a Report
   rule such as Team is any of Platform. A rail lists the Goals the rules
   select as she types.
2. Each month she opens the draft. The preview is exactly what readers will
   see: a Health summary, then the exceptions in full (Red, Yellow, Stale,
   slipped, newly Proposed, Lifecycle changes), then every unchanged Green
   Goal on one line.
3. "Changes since" reads against her previous publication, so what's marked is
   what moved since the last review, including Goals that entered or left.
4. She composes the narrative: ticks the Highlights worth pulling into
   Insights, Accomplishments and Misses, each credited to its Goal's Owner, and
   adds notes of her own. It autosaves and the preview follows.
5. She presses **Publish…** and confirms. The publication is a frozen
   snapshot. She shares it from the tool, as Markdown, or printed to PDF.
6. A reader has a question about one Red Goal and comments on its block. The
   Comment goes to that Goal's Owner, who replies in the thread.
7. Elena turns the thread into an Action Item with an owner and a due date.
   Her next publication opens with it still listed, until its owner closes it.

**Anyone can do this, over any Goals.** Marcus builds a Report over his own
team's Goals by a rule. He also builds one that watches eleven critical
projects across the company that his team has a stake in. He owns none of
them. He searches for each by title and hand-picks it, and the Report then
shows him their Health, slips and Paths to Green as their Owners report them.
Putting a Goal in a Report gives him nothing over the Goal itself: only its
Owner or a Delegate checks in on it or changes it. A Report's Goals come from
rules or a hand-picked list, never from following Contributes to links
(ADR 0007), so "what I'm watching" is a list he chooses.

**It worked if:** each exception carried its data, its next steps (the Path
to Green) and enough context to stand alone; the narrative and the other
exceptions showed what else the org is up against; she wrote only the
introduction and her own notes; a reader far above the work understood it
cold; a question reached the right Owner without anyone looking up who owns
what; nothing agreed was lost by the next review.

## 3. The weekly Check-in

**Who:** Priya, a frontline manager who owns four Active Goals.
**Promise:** a routine week takes minutes and is hard to get wrong. This is
the price of scenarios 1 and 2, so it has to stay low.

1. She opens the tool. It lands on **Home**, "Your week", where **Needs you**
   lists her Check-ins due, each with its Health and why it's listed. The Home
   item in the top bar carries the count.
2. Three Goals haven't changed. She presses **No change** on each. One click
   records a Check-in repeating last week's values, and the row leaves the list.
3. The fourth moved. She presses **Check in**. The form comes prefilled from
   her latest Check-in: Health, Status, a reading for each Metric. She updates
   the Status and one reading and submits.
4. During the week she had logged a Draft Highlight on that Goal. The form
   offers it, ticked to keep, and it becomes one of this Check-in's Highlights,
   which Elena may later pull into her Report.
5. Home says "You're all caught up."

**It worked if:** four Goals took under five minutes; nothing she didn't change
had to be retyped; the optional parts (Highlights, dates and Milestones,
Lifecycle) stayed folded away until she needed them.

**When Priya is on holiday,** a Delegate she authorized checks in for her from
"Goals delegated to you". The Check-in records who wrote it, and the Goal
doesn't go Stale just because its Owner is away.

## 4. Reporting bad news

**Who:** Priya, the week a vendor delay puts a project behind.
**Promise:** she can't report trouble vaguely or bury it. The tool makes her
say what's wrong, what the plan is and by when, and carries it upward.

1. In the Check-in she sets Health to Yellow. **Path to Green** appears and is
   required: her plan, or her ask for help, and a date for being back to Green.
2. She opens the dates section and moves the delivery date three weeks out.
   The move needs a reason and is kept as a Date Slip. Moving the date later
   rules out Green for this Check-in.
3. She moves one Milestone the same way and marks another Done.
4. On the Goal page the delivery date now reads ~~10/15~~ 11/03, the slipped
   Milestone shows Yellow, and the History has the Check-in with its slips.
5. Marcus owns the team Goal hers contributes to. His Goal's Rolled-up Health
   turns Yellow, beside the Green he last set. His next Check-in asks him to
   explain if he still says Green.
6. If Priya's back-to-Green date passes and the Goal still isn't Green, it is
   flagged as prominently as Red and appears on Elena's Risks page.

**It worked if:** she couldn't go Yellow without a plan and a date; the slip
shows wherever the date does and can't be quietly overwritten; Marcus couldn't
stay Green over a Yellow child without saying why; one Red child didn't turn
every ancestor Red.

## 5. Stating a Goal and connecting it

**Who:** Priya starting a new project; Marcus, a mid-level manager whose team
Goal it serves.
**Promise:** a Goal can't become Active until it can be judged, and it joins
the graph only by the parent Owner's consent. Without a date or a Metric there
is nothing for trouble to be measured against.

1. Priya presses **New goal**. One page, in sections: What and why (Title, So
   What), Delivery (Dated or Ongoing, date, cadence), How you'll know
   (Milestones, Metrics), Contributes to, Where it fits (Dimensions, Fields).
2. A **Ready to activate** card stays beside the form and ticks off the
   minimum standard as she types: a So What; Dated with a delivery date and at
   least one Milestone or Metric, or Ongoing with at least one Metric; a value
   in everything an Admin marked required.
3. She isn't sure of the Metrics yet, so she saves as Proposed and comes back
   through **Finish defining**. Or she meets the checklist and presses
   **Create and activate**.
4. She picked Marcus's Goal under Contributes to. The page told her Marcus
   would be asked. The link is a request until he accepts.
5. Marcus sees it on Home under **Requests** and accepts. Her Goal now counts
   toward his Rolled-up Health.
6. Had she picked no parent, the Active Goal would be Unaligned: allowed, and
   listed on Risks.

**It worked if:** she always knew what was still missing; she wasn't asked
what type of thing it is (objective, key result, project); the Dimensions she
set are what let Elena filter to her org in scenarios 1 and 2.

## What I left out, and why

Each of these supports a scenario above rather than standing as one:

- **Handoff, Departed, Ownerless reassignment.** Keeps a Goal answerable when
  people move. Ownerless itself is a Risks signal in scenario 1.
- **Admin setup:** Dimensions, Fields, required values, Top-level Goals.
  Occasional. It enables the filters and rules in 1 and 2.
- **Spreadsheet import.** It may not stay in the product, so no scenario
  depends on it.
- **Parent suggestions.** A side door into scenario 5 for a non-Owner.
- **Goal table editing and CSV download.** Bulk upkeep of Dimension values.
- **Weekly emails.** The reminder and the Parent digest back up Home; Home is
  the front door.

## Settled along the way

- Anyone may build a Report over any Goals, including ones they don't own.
- Every Report Definition is visible to everyone; only its creator or an
  Admin may change it.
- Home is the front door. The weekly emails back it up.
- A Delegate is cover for an Owner who is away, not a standing role.
