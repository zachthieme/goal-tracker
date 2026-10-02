# Goal Tracker

A single mechanism for an org to state its goals, update them on a regular cadence, and produce MBR-caliber reports at any cadence. It combines the rigor of an Amazon MBR, the ease of a 5-15, and the org-wide view of OKRs.

## People

**Account**:
A person's identity in the tool, identified by their email address, matched whatever its letter case or surrounding spaces: `Sam@Example.com` and `sam@example.com` are the same Account. Owners, Delegates, Contributors and Admins are all Accounts.
_Avoid_: user, member

**Name**:
What the tool calls a person. It comes from the org's sign-in and is never typed into the tool. A person without one is shown by the part of their email before the `@`. Two people can share a Name, so wherever the web pages show a person, the Name can be clicked, tapped or activated from the keyboard to show their email beside it, and activated again to hide it. Documents without controls, the Markdown export and the Print view, introduce each person as Name (email) on first mention instead.
_Avoid_: display name, full name, username

**Departed**:
An Account whose person has left the org, as recorded by an Admin. A departed person can't sign in or act, but everything they did stays attributed to them. An Admin can reverse it.
_Avoid_: deactivated, deleted, offboarded

## Goals and structure

**Goal**:
The single unit of work being tracked, at any level, from an org-wide outcome to a team project. It may carry Metrics and Milestones.
_Avoid_: Objective, Key Result, Project, Initiative (as distinct types)

**So What**:
The customer problem a Goal addresses and what is expected to change when it succeeds. Required when the Goal is created; edits are tracked.
_Avoid_: rationale, description, why

**Owner**:
The one person accountable for a Goal, who writes its Check-ins and answers for it in Reports.
_Avoid_: co-owner, DRI, lead

**Delegate**:
A person an Owner authorizes to write and submit Check-ins for a Goal and to set its Dimension values and Fields. Accountability stays with the Owner, and each Check-in records who wrote it. When a Goal changes hands through a Handoff, the new Owner chooses which Delegates to keep.
_Avoid_: proxy, editor, TPM (as a role in the tool)

**Handoff**:
The transfer of a Goal to a new Owner, which takes effect only when the new Owner accepts. Every Handoff is kept in the Goal's history with its outcome: accepted, rejected, or cancelled.
_Avoid_: reassignment, transfer

**Ownerless**:
A Goal whose Owner is Departed and hasn't been replaced, either by an Admin reassigning it or by accepting a Handoff the Owner started before leaving. Surfaced as prominently as Red and Stale, and reported to the Owners of its parents.
_Avoid_: orphaned, unassigned

**Admin**:
One of a small set of people who define Dimensions and Fields, reassign Ownerless Goals, and can override links and Lifecycle changes.
_Avoid_: moderator, superuser

**Contributor**:
A person listed on a Goal for information only; carries no update duty.
_Avoid_: member, assignee

**Metric**:
A measured quantity on a Goal with a unit, direction, baseline, target, and target date; its current value is recorded at each Check-in. Never aggregated across Goals.
_Avoid_: KPI, key result, measure

**Milestone**:
A dated checkpoint within a Goal. A Milestone slip that doesn't move the Goal's delivery date does not affect Health.
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

**Unaligned**:
An Active Goal that contributes to no other Goal and is not a Top-level Goal. Allowed, but listed where leadership can see it.
_Avoid_: orphan, root

**Top-level Goal**:
A Goal an Admin has marked as one of the org's root outcomes. It isn't expected to contribute to anything, so it's never Unaligned.
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
An Active Goal missing a value in a Dimension or Field that an Admin has marked required. Flagged and listed, but less prominently than Red or Stale. A Proposed Goal can't become Active while it would be Incomplete.
_Avoid_: invalid, missing data

Teams are not a built-in concept; an org that wants them adds a Team Dimension. The structure of the goal graph comes only from what Goals drive.

## Status

**Health**:
How an active Goal is tracking against its delivery date: Green, Yellow, or Red.
_Avoid_: status, RAG, RYG

**Rolled-up Health**:
The worst Health among a Goal's active children, shown next to the Owner-set Health. When the two differ, the Owner must explain why.
_Avoid_: computed status, aggregate health

**Stale**:
An Active Goal whose last Check-in (or its activation, if it has none) is older than its expected cadence (weekly by default), counted in days of the org's timezone. On Hold Goals are never Stale. Surfaced as prominently as Red; a Rolled-up Health says how many children are Stale ("3 of 12 Stale") without changing its color.
_Avoid_: overdue, missing update

**Lifecycle**:
Where a Goal is in its existence: Proposed, Active, On Hold, Done, or Cancelled. Independent of Health. Leaving Active for any state other than Done requires a reason; reaching Done requires an outcome.
_Avoid_: status, state, New (use Proposed)

**Date Slip**:
A recorded change to a Goal's delivery date or a Milestone's date, always with a reason. The full history is kept, and a Goal's date is shown struck through (~~10/15~~ 11/03).
_Avoid_: New Date (as a status), re-plan, reschedule

**Milestone Churn**:
The number of Milestones added or removed on a Goal since it became Active. Read alongside Date Slips as a scope-creep and capacity signal.
_Avoid_: scope change, replan count

**Path to Green**:
The owner's stated plan, or request for help, to return a Yellow or Red Goal to Green.
It carries a target date for being back to Green. Once that date has passed and the Goal still isn't Green, the Goal is flagged as prominently as Red.
_Avoid_: mitigation, recovery plan, get-well plan

## Updates

**Check-in**:
An owner's routine update to one Goal: Health, a short status, Date Slips, and Milestone changes. It should take minutes and be hard to get wrong.
_Avoid_: status update, 5-15, report

**Highlight**:
An optional note in a Check-in, marked as an Insight, Accomplishment, or Miss, that a Report author may pull into a Report's narrative. A Check-in may carry several, of any mix of kinds, and an author pulls each one separately. The Goal's Owner is credited.
_Avoid_: win, callout, note

**Report**:
A document generated from Check-ins for a chosen set of Goals, at whatever cadence the reader needs, in MBR format. Exceptions (Red, Yellow, Stale, slipped, newly Proposed, Lifecycle changes) get the full treatment; unchanged Green Goals take one line each.
_Avoid_: MBR (a Report may be used as one), review, rollup

**Report Definition**:
A saved, reusable selection of Goals for a Report. Each publication freezes a snapshot, and the next publication marks what changed since the previous one.
_Avoid_: template, saved view, dashboard

**Comment**:
A question or answer about one Goal in a published Report. A Comment starts a thread that is routed to the Goal's Owner, and anyone may reply in it.
_Avoid_: feedback, note, annotation

**Action Item**:
A follow-up raised while a Report is discussed. It has an owner and a due date, belongs to a Report Definition, and carries into each new publication until closed.
_Avoid_: todo, follow-up, task
