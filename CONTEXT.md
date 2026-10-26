# Goal Tracker

A single mechanism for a company to state its goals, update them on a regular cadence, and produce MBR-caliber reports at any cadence. It combines the rigor of an Amazon MBR, the ease of a 5-15, and the company-wide view of OKRs.

## People

**Company**:
The whole organization using the tool: everyone who signs in, its directory of people and Managers, and its timezone, used for company-wide dates and for anyone without a Timezone of their own. "Company-wide" means across all of it.
_Avoid_: org, the org, organization (an **Org** is one person's part of it)

**Account**:
A person's identity in the tool, identified by their email address, matched whatever its letter case or surrounding spaces: `Sam@Example.com` and `sam@example.com` are the same Account. The first time a person signs in through the company's sign-in, their Account is linked to who that sign-in says they are; after that it finds them even if their email changes, and an email the company gives to someone new can't sign in as the Account it once named. Owners, Delegates and Admins are all Accounts.
_Avoid_: user, member

**Name**:
What the tool calls a person. It comes from the company's sign-in and is never typed into the tool. A person without one is shown by the part of their email before the `@`. Two people can share a Name, so wherever the web pages show a person, the Name can be clicked, tapped or activated from the keyboard to show their email beside it, and activated again to hide it. Documents without controls, the Markdown export and the Print view, introduce each person as Name (email) on first mention instead.
_Avoid_: display name, full name, username

**Manager**:
The person someone reports to, as the company's directory records it. It is read from the directory and never set in the tool. Someone at the top of the company, or whom the directory gives no manager, has none.
_Avoid_: boss, supervisor, lead, reports to (it collides with Report)

**Timezone**:
Where a person works, as the company's directory records it; read from the directory and never set in the tool. A person without one uses the company's. A Goal's own dates and cadence are judged in its Owner's Timezone: whether its Check-in is overdue, whether a Milestone or the back-to-Green date has passed, whether a Metric is off target, and its Run off Green. An Ownerless Goal uses the company's. A Parent digest counts its past week in the Timezone of the parent Owner it goes to. Company-wide dates, such as a Report's baseline, stay in the company's timezone.
_Avoid_: locale, location, region

**Org**:
A person and everyone below them: the people whose Manager is that person, whose Manager's Manager is, and so on at every depth. "Priya's org" includes Priya; a viewer's own is "My org". An org scopes what a page or Report shows by each Goal's Owner, never by its Delegates, and never limits or ranks a Contributes to link (ADR 0002). When a Manager changes, an org simply finds different Goals.
_Avoid_: Chain, team, reporting line, org chart, direct reports; "the org" for the whole company

**Departed**:
An Account whose person has left the company, as recorded by an Admin. A departed person can't sign in or act, but everything they did stays attributed to them. An Admin can reverse it.
_Avoid_: deactivated, deleted, offboarded

## Goals and structure

**Goal**:
The single unit of work being tracked, at any level, from a company-wide outcome to a team project. It may carry Metrics and Milestones. Its title can be changed by its Owner or an Admin in any Lifecycle, and each change is kept in the Goal's history with the old and new titles.
_Avoid_: Objective, Key Result, Project, Initiative (as distinct types)

**Why it matters**:
The customer problem a Goal addresses and what is expected to change when it succeeds. Required when the Goal is created; edits are tracked.
_Avoid_: so what, rationale, description

**Owner**:
The one person accountable for a Goal, who writes its Check-ins and answers for it in Reports.
_Avoid_: co-owner, DRI, lead

**Delegate**:
A person an Owner authorizes to write and submit Check-ins for a Goal, to add its Milestones and edit them (only while the Goal is Proposed and the Milestone Planned, as for the Owner), and to set its Dimension values and Fields. Accountability stays with the Owner, and each Check-in records who wrote it. When a Goal changes hands through a Handoff, the new Owner chooses which Delegates to keep. A Delegate is always a Goal's; a Report Definition has Report Delegates.
_Avoid_: proxy, editor, TPM (as a role in the tool)

**Handoff**:
The transfer of a Goal to a new Owner, which takes effect only when the new Owner accepts. Every Handoff is kept in the Goal's history with its outcome: accepted, rejected, or cancelled.
_Avoid_: reassignment, transfer

**Ownerless**:
A Goal whose Owner is Departed and hasn't been replaced, either by an Admin reassigning it or by accepting a Handoff the Owner started before leaving. Surfaced as prominently as Red and Check-in overdue, and reported to the Owners of its parents.
_Avoid_: orphaned, unassigned

**Admin**:
One of a small set of people who define Dimensions and Fields, reassign Ownerless Goals, can edit any Goal's title and Why it matters, and can override links and Lifecycle changes. An Admin can make another person an Admin, or take the role from another Admin, but never from themselves, so there is always at least one.
_Avoid_: moderator, superuser

**Metric**:
A measured quantity on a Goal with a unit, direction, baseline, target, and target date; its current value is recorded at each Check-in. Never aggregated across Goals.
_Avoid_: KPI, key result, measure

**Milestone**:
A dated checkpoint within a Goal. A Milestone slip that doesn't move the Goal's delivery date does not affect Health.
The Goal's Owner or a Delegate can add a Milestone at any time while the Goal is Proposed, Active or On Hold, from the Goal page or in a Check-in; one added from the Goal page once the Goal is Active is kept in the Goal's history with who added it. A Milestone is edited (renamed or re-dated) only while its Goal is Proposed and it is Planned; after that, moving its date (a Date Slip) and marking it Done or Removed happen only in a Check-in, and it keeps its name.
Wherever Milestones are listed (the Goal page, a Report, its Markdown export), each reads mark, date, name, with one mark, the first that applies: **Done**; **Removed**; **Overdue**, still Planned and past its date; **Slipped**, still Planned and slipped at least once; **New**, still Planned and added within the window (since the Report's baseline, or on the Goal page by the Goal's latest Check-in or since it). A Planned Milestone with none of these has no mark and reads as on track. The marks are display marks, not a Health (see **Health**): they never feed the Goal's Health, which its Owner sets, and though Overdue and Slipped are shown in the Red and Yellow tints, each shows its word rather than a Health's shape. An Active Goal whose latest Check-in is Green while a Milestone is overdue is marked Milestone overdue beside its Health on the Goal list and on Follow-ups. The Health stays the Owner's, and a No change is refused until they respond (see **Check-in**).
_Avoid_: task, deliverable, phase

**Dated Goal**:
A Goal with a delivery date; its Health is judged against that date and its Milestones.
_Avoid_: project, time-bound goal

**Ongoing Goal**:
A Goal with no delivery date; its Health is judged against its Metrics' targets, so it must have at least one Metric.
_Avoid_: KTLO, BAU, evergreen

**Contributes to**:
The relationship from a child Goal to a parent Goal it helps achieve. A Goal may have many parents and many children, and the resulting graph has no cycles. The link exists only once the parent's Owner accepts it.
_Avoid_: rolls up to, parent/child hierarchy, alignment

**Parent suggestion**:
A proposal, from someone other than a Goal's Owner, that the Goal contribute to a particular parent. Only the Goal's Owner decides it: accepting it requests the link as if the Owner had, declining it ends it. Each is kept in the Goal's history with its outcome.
_Avoid_: link suggestion, recommendation, proposed link

**Unlinked**:
An Active Goal that contributes to no other Goal and is not a Top-level Goal. Allowed, but listed where leadership can see it.
_Avoid_: unaligned, orphan, root

**Top-level Goal**:
A Goal an Admin has marked as one of the company's root outcomes. It isn't expected to contribute to anything, so it's never Unlinked.
_Avoid_: root, north star, company goal

**Dimension**:
An admin-defined attribute whose values come from a list (e.g. pillar, quarter, goal kind), used to filter and group Goals. The Admin decides whether a Goal takes one value or several, and whether the list is Fixed or Extendable. A Goal with several values appears under each of them when grouped.
_Avoid_: tag, label, category

**Fixed**:
A Dimension whose list only an Admin can add to.
_Avoid_: closed, locked

**Extendable**:
A Dimension whose list can be added to by anyone setting a Goal's value in it. Renaming, retiring and merging values stays with Admins.
_Avoid_: open, free-form, user-defined

**Field**:
An admin-defined attribute whose value is entered directly on a Goal rather than chosen from a list (e.g. budget, notes). A Field holds a number, a short text, a long text, or a date. It describes a Goal and is never an update: status belongs in a Check-in. It is not used to filter or group, and numbers in a Field are never added up across Goals. Changes to a Goal's Fields and Dimension values are kept in the Goal's history.
_Avoid_: custom field, property, typed Dimension

**Retired**:
A Dimension, a Dimension's value, or a Field that an Admin has withdrawn. It is no longer offered when setting a Goal's values, but stays readable on the Goals that already carry it. An Admin can reverse it.
_Avoid_: deleted, archived, disabled

**Incomplete**:
An Active Goal missing a value in a Dimension or Field that an Admin has marked required. Flagged and listed, but less prominently than Red or Check-in overdue. A Proposed Goal can't become Active while it would be Incomplete.
_Avoid_: invalid, missing data

Teams are not a built-in concept; a company that wants them adds a Team Dimension. The structure of the goal graph comes only from what Goals drive.

## Status

**Health**:
How an active Goal is tracking against its delivery date: Green, Yellow, or Red.
_Avoid_: status, RAG, RYG

**Rolled-up Health**:
The worst Health among a Goal's active children, shown next to the Owner-set Health. When the two differ, the Owner must explain why.
_Avoid_: computed status, aggregate health

**Run off Green**:
How long an Active Goal has been Yellow or Red without a Green Check-in in between, counted in weeks of the Owner's Timezone. Moving between Red and Yellow doesn't end it; a Green Check-in does. Shown beside the Health ("Red · 5 wks"), never changing it.
_Avoid_: time in status, red streak, days red

**Check-in overdue**:
An Active Goal whose last Check-in (or its activation, if it has none) is older than its expected cadence (weekly by default), counted in days of the Owner's Timezone. On Hold Goals are never Check-in overdue. Surfaced as prominently as Red; a Rolled-up Health says how many children are Check-in overdue ("3 of 12 overdue") without changing its color.
_Avoid_: stale, missing update, Check-in due (that's a Check-in coming up within the cadence)

**Lifecycle**:
Where a Goal is in its existence: Proposed, Active, On Hold, Done, or Cancelled. Independent of Health. Leaving Active for any state other than Done requires a reason; reaching Done requires an outcome.
_Avoid_: status, state, New (use Proposed)

**Date Slip**:
A recorded change to a Goal's delivery date, a Milestone's date, or a Path to Green's back-to-Green date, always with a reason. The full history is kept, and a Goal's date is shown struck through (~~10/15~~ 11/03). A Milestone's date shows current first, then the date it last slipped from struck through (11/03 ~~10/15~~), and how many times it slipped once that is more than once (11/20 ~~11/03~~ (3)). A date is never shown struck through beside itself: a back-to-Green date moved back earlier, which is no Date Slip, strikes through the most recent earlier date that differs.
_Avoid_: New Date (as a status), re-plan, reschedule

**Milestone Churn**:
The number of Milestones added or removed on a Goal since it became Active. Read alongside Date Slips as a scope-creep and capacity signal.
_Avoid_: scope change, replan count

**Schedule conflict**:
A Goal due after a Goal it contributes to, so the parent's date can't be met as planned. Either Goal's Owner can resolve it: the child's by moving its date earlier or re-planning, the parent's by accepting the later date.
_Avoid_: date mismatch, timeline clash

**Path to Green**:
The owner's stated plan, or request for help, to return a Yellow or Red Goal to Green.
It carries a target date for being back to Green; moving that date later is a Date Slip. Once that date has passed and the Goal still isn't Green, the Goal is flagged as prominently as Red.
A Path to Green marked as asking for help is an **Ask for help**, addressed up the Owner's reporting line: to their Manager, or, past anyone who has left the company or has no account, to the next Manager above. An Owner with no one above them has no addressee; the ask still shows on Follow-ups and in Reports. It stays open until the Owner unmarks it in a later Check-in or the Run off Green ends; an answer alone doesn't close it.
_Avoid_: mitigation, recovery plan, get-well plan

## Updates

**Check-in**:
An owner's routine update to one Goal: Health, a short status, Date Slips, and Milestone changes. It should take minutes and be hard to get wrong.
A Check-in is where a Milestone's date moves and where it is marked Done or Removed; it can add Milestones too, though a Milestone can also be added from the Goal page between Check-ins.
A Green Check-in with an off-target Metric asks the Owner to confirm, and a No change can't repeat it.
A No change is refused while a Milestone is overdue, or the Path to Green's back-to-Green date has passed, whatever the Health: the Owner or a Delegate has to respond to the missed date.
_Avoid_: status update, 5-15, report

**Nudge**:
A one-off request from someone else to a Goal's Owner and Delegates to check in on a Goal with something its Owner answers in a Check-in: Check-in overdue, past its Path to Green, a Milestone overdue, or a Schedule conflict. A Goal can be nudged at most once a day, and each Nudge is kept in the Goal's history.
_Avoid_: reminder (that's the weekly email), ping, chase

**Highlight**:
An optional note in a Check-in, marked as an Insight, Accomplishment, or Miss, that a Report author may pull into a Report's narrative. A Check-in may carry several, of any mix of kinds, and an author pulls each one separately. The Goal's Owner is credited.
_Avoid_: win, callout, note

**Draft Highlight**:
A Highlight logged on a Goal between Check-ins, waiting for the next Check-in. Only the Goal's Owner and Delegates can log one, or see, edit or delete a pending one; an edit isn't recorded in the history. The next Check-in offers each one; those kept become that Check-in's Highlights, and the rest are discarded, which the Goal's history records.
_Avoid_: note, jot, log entry

**Report**:
A document generated from Check-ins for a chosen set of Goals, at whatever cadence the reader needs, in MBR format. Exceptions (Red, Yellow, Check-in overdue, slipped, newly Proposed, Lifecycle changes, and Health changes since the baseline, a recovery to Green included) get the full treatment; unchanged Green Goals take one line each. Exceptions come most urgent first: Ownerless, then by Health (Red, Yellow, none, Green), then a Path to Green overdue, then Check-in overdue, then those that changed since the baseline, then by delivery date; each opens with what changed since the baseline. A Report opens with a line counting what moved since its baseline: Goals that turned Red or Yellow, came back to Green, slipped, changed Lifecycle, are new, entered or left. An exception block counts its Red, Yellow and Check-in overdue children beside its Rolled-up Health, names the Red and Yellow ones and the Goals in the same Report it contributes to or that contribute to it, and links to each; these links never decide which Goals are in a Report. A Goal whose Health changed shows the Health it had at the baseline beside its own ("Yellow · was Green"); its Health at the baseline is that of its latest Check-in before it, so a Goal with no Check-in by then shows none.
_Avoid_: MBR (a Report may be used as one), review, rollup

**Report Definition**:
A saved, reusable selection of Goals for a Report, made either by Report rules or by hand-picking Goals, never by following Contributes to links. Each publication freezes a snapshot, and the next publication marks what changed since the previous one, including Goals that entered or left the Report. A Goal that was Done or Cancelled before a Report's baseline leaves it, however it was selected, so finished work is reported once. Anyone signed in may create one. Only its creator, its Report Delegates and Admins may change it, curate its narrative or publish it; every change is kept in its Report log. It has a creator, not an Owner, and nothing changes hands when the creator leaves.
_Avoid_: template, saved view, dashboard

**Report Delegate**:
A person a Report Definition's creator or an Admin authorizes to change it, curate its narrative and publish it: everything the creator may do except add or remove Report Delegates. Each change they make is kept in the Report log with their name.
_Avoid_: co-owner, editor, collaborator

**Report log**:
The record of everything that changed on a Report Definition, newest first, each with who and when: an edit to its title, rules or picked Goals (before and after), a Report Delegate added or removed, a change to the next publication's narrative (Highlights picked or dropped, a section's notes changed), and each publication. Anyone who can open the Report can read it: the draft shows it merged with the publications, each linking to its own, as its History. It records; it doesn't undo.
_Avoid_: Definition log (a Goal's), history (the draft's History, which merges it with the publications), audit trail

**Report rule**:
A condition on a Goal's Dimension values, Owner, Owner's org, Lifecycle, Health or whether it's Top-level, that decides which Goals a Report Definition selects. A Goal must meet every rule, and meets a rule by having any of its values. An org rule reads "Owner is in the org of Priya": its Owner is in any named person's org, by the Managers as they are at each draft. Health means the Owner-set Health, and a Goal with no Health meets only "is not" rules on it. A rule-based Definition can also list Goals to **Also include** by hand and Goals to **Leave out** even when they match.
_Avoid_: filter, query, criteria

**Comment**:
A question or answer about one Goal, made on the Goal itself or on a published Report, and kept in the Goal's history. A Comment starts a thread that is routed to the Goal's Owner, and anyone may reply in it.
_Avoid_: feedback, note, annotation

**Action Item**:
A follow-up raised while a Report is discussed. It has an owner and a due date, belongs to a Report Definition, and carries into each new publication until closed.
It may be about one Goal in the Report: one raised from a Comment is about that Comment's Goal, and one raised directly may name a Goal or stay about the Report as a whole.
Only its owner closes it, with a note. While it is open, it may be handed to a new owner by the Report Definition's creator or a Report Delegate, whoever published the publication it was raised on, an Admin, or the Definition creator's Manager.
An open Action Item past its due date is overdue. It doesn't change any Goal's Health.
_Avoid_: todo, follow-up, task
