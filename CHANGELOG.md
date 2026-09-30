# Changelog

**Reading this file.** Every change that ships is logged, internal work included,
and each entry opens with a tag saying who it reaches:

| Tag | Visible to |
| --- | --- |
| `[user]` | someone **using** Goal Tracker: web UI, emails, observable behaviour |
| `[ops]` | someone **running** it: env vars, migrations, deploy steps |
| `[api]` | a **programmatic contract**: the spreadsheet import format, database schema, configuration |
| `[internal]` | nothing externally observable: refactors, CI, tests, docs, plumbing |

`**Breaking changes:**` comes first in a milestone and names the contract it broke.
The format is in [`docs/changelog-conventions.md`](docs/changelog-conventions.md).

### Goal Tracker v1 pilot build — September 30, 2026

**New features:**
- [user] Development sign-in by email, creating an account on first sign-in, with the Admin flag set from config (#2).
- [user] Create a Proposed Goal with a title, a So What, and an Owner, and view it on its own page and in the Goal list (#2).
- [ops] Configure the server with `GOAL_TRACKER_ADDR`, `GOAL_TRACKER_DB`, and `GOAL_TRACKER_ADMINS` (comma-separated admin emails) (#2).
- [user] Define a Proposed Goal: mark it Dated (with a delivery date the picker suggests as the nearest mid-month or end-of-month Monday) or Ongoing, add and edit Milestones (name, date) and Metrics (name, unit, direction, baseline, target, target date), add Contributors, and set the Check-in cadence (7 days by default) (#3).
- [user] Revise a Goal's So What with every revision kept and viewable (#3).
- [user] Activate a Proposed Goal once it meets the minimum standard — a So What, an Owner, and either a delivery date plus a Milestone or Metric (Dated) or a Metric (Ongoing); activation is rejected with each missing item reported as its own error (#3).
- [user] Check in on an Active Goal from its page: set Health (Green/Yellow/Red) and a short status, with the form pre-filled from the latest Check-in; Yellow or Red requires a Path to Green with text and a target date, whose error shows next to the field (#4).
- [user] One-click "no change" records a Check-in repeating the previous values, and the Goal page shows the immutable Check-in history with the current Health, status, and Path to Green taken from the latest Check-in (#4).
- [user] Move the delivery date or a Milestone's date in a Check-in: each change needs a reason and is kept as a Date Slip with its old and new dates; the Goal page shows the date history struck through (~~2026-10-15~~ 2026-11-03), lists every Date Slip with its reason, and shows the Goal's slip count (#5).
- [user] Mark Milestones Done or Removed (Removed needs a reason) and add new Milestones in a Check-in; the Goal page shows each Milestone's status and the Goal's Milestone Churn — Milestones added or removed since it became Active (#5).
- [user] A Check-in can't be Green when it moves the delivery date later, or while any Milestone is overdue (Planned and past its date); the one-click no-change Check-in is refused on the same grounds. A Milestone slip that doesn't move the delivery date doesn't affect Health (#5).
- [user] Record Metric readings in a Check-in: the Check-in form takes a current value for each of the Goal's Metrics, and the Goal page shows each Metric's trend over time against its target (#6).
- [user] Flag an optional Highlight in a Check-in, marked as an Insight, Accomplishment, or Miss with a note; the Goal page lists a Goal's Highlights crediting the Owner, and Highlights are queryable by Goal and by time range for later Report curation (#6).
- [user] Change a Goal's Lifecycle in a Check-in: put an Active Goal On Hold or Cancel it (each needs a reason), or mark it Done (needs a one-line outcome and a final value for every Metric, taken from that Check-in's readings); an On Hold Goal can be resumed to Active or Cancelled. Done and Cancelled are end states (#7).
- [user] Each Lifecycle change shows in the Goal's Check-in history (from → to, with its reason or outcome), and the Goal page shows why the Goal is On Hold or Cancelled, or its outcome once Done (#7).
- [user] Link Goals with "contributes to": request that your Goal contribute to another (with an optional note), auto-accepted when you own both; the parent's Owner accepts or rejects pending requests from their inbox; a Goal page lists its parents and children for navigation and can remove a link. Links that would create a cycle are refused (#8).
- [user] Rolled-up Health: a Goal shows the worst Owner-set Health among its Active children next to the Owner's own Health, on the Goal page and in the Check-in form (#9).
- [user] A Check-in whose Health differs from the Rolled-up Health must carry an explanation of the difference, which is recorded on the Check-in and shown on the Goal page (#9).
- [user] Stale: an Active Goal is Stale once its last Check-in — or its activation, if it has none — is older than its cadence, counted in days of the org's timezone. On Hold Goals are never Stale. A Stale Goal's page flags it, saying how many days it has gone without a Check-in (#10).
- [user] A Goal whose Path to Green target date has passed while it still isn't Green is flagged on its page (#10).
- [user] Beside a Goal's Rolled-up Health, the page says how many of its Active children are Stale (e.g. "3 of 12 Stale"); Stale children never change the Rolled-up Health's color (#10).
- [user] The Goal list marks a Stale Goal's row "Stale" and an overdue one's "Path to Green overdue", as it marks Ownerless Goals (#10).
- [user] Freshness signals: a new page, linked from the Goal list, lists every Stale Goal (longest without an update first) and every Goal whose Path to Green is overdue (#10).
- [ops] `GOAL_TRACKER_TIMEZONE` sets the org's IANA timezone (default `UTC`) that Check-in cadences are counted in; an unknown timezone stops the server at start-up (#10).
- [internal] Migration 0014 records when a Goal becomes Active; Goals activated before it are judged from their creation time (#10).
- [user] An Admin marks a Goal as a Top-level Goal (one of the org's root outcomes) from its page, and can unmark it; the Goal page shows the mark (#11).
- [user] Graph signals: a new page, linked from the Goal list, flags the risks the graph shows that nobody reported — Unaligned Goals (Active, contributing to nothing through an accepted link, and not Top-level), schedule conflicts (a Goal due later than a Goal it contributes to), and Goals contributing to a parent that is On Hold or Cancelled. Done and Cancelled Goals raise no schedule conflict and aren't flagged under a halted parent (#11).
- [user] Each Goal's page flags the graph signals that touch it: Unaligned, a schedule conflict (on both the child and the parent), or a parent that is On Hold or Cancelled (#11).
- [user] Dimensions: an Admin defines Dimensions with fixed value lists and can add, rename, and retire values from the new Dimensions page; retired values stay readable on the Goals that already carry them but aren't offered for new assignments (#12).
- [user] Owners assign a Dimension value to their Goal from the Goal page (one value per Dimension, replacing any prior one), and the Goal list filters by and groups on Dimension values (#12).
- [user] Create a Goal under a parent from the parent's page: the parent's values are offered as pre-checked defaults (kept ones are assigned, but not inherited) and the new Goal requests a "contributes to" link to the parent (#12).
- [user] Delegates: an Owner authorizes people to write Check-ins on a Goal (and revokes them) from the Goal page, without sharing a login; a Delegate or the Owner may submit Check-ins and each Check-in records its author and the Owner it was written for, while non-Delegates are refused (#13).
- [user] "Delegated to me" page lists every Goal an Owner has authorized you to check in on, with a Check-in form for each (#13).
- [user] Hand off a Goal to a new Owner: the current Owner or an Admin starts the Handoff and it takes effect only when the new Owner accepts it from their pending-handoffs inbox (they can also reject it) (#14).
- [user] An Admin marks a person who has left the org as departed, making the Goals they still own Ownerless; Ownerless is shown prominently on the Goal and surfaced on each parent Owner's page, and an Admin reassigns an Ownerless Goal to a present Owner (#14).
- [user] Reports: anyone signed in saves a reusable Report Definition on the new Reports page — root Goals plus a depth, Dimension or Owner filters, or filters alone, plus an introduction — and sees a live draft listing the selected Goals one line each (title, Owner, Health, due date). Traversal follows accepted "contributes to" links only, filters apply after traversal, and a Goal reached through several paths appears once (#15).
- [user] Reports: the draft Report gives each exception the full MBR block and every other Goal one line. A Goal is an exception if it's Red, Yellow, Stale, or Ownerless, or if it was created, had a date slip (delivery date or Milestone), or changed Lifecycle (including activation) since the baseline (#16).
- [user] The baseline is 30 days ago by default. The reader can pick another date with the "Changes since" control, or with `?baseline=YYYY-MM-DD` on `/reports/{id}`. A date that can't be read is refused with 400 (#16).
- [user] An exception block shows the title, the due date with earlier dates struck through, Health, badges (New, New Date, Stale, Ownerless, On Hold), So What, the latest status, and the Path to Green. It also lists the Milestones marked New, Done, or Removed with their earlier dates struck through, the Metrics' latest readings against target, and the Rolled-up Health with the Owner's explanation (#16).
- [user] Publish a Report from its draft page with the new Publish button. Publishing freezes the Report as it reads at that moment. Later Check-ins and edits to its Goals never change a published Report (#17).
- [user] The next draft and publication of a Report Definition show what changed since its previous publication instead of 30 days ago. A date picked with "Changes since" still overrides this, and publishing carries the picked date through (#17).
- [user] A Report Definition's page lists its publications, newest first, with who published each one and when. Each publication opens at `/reports/{id}/publications/{publication}` and says what it read its changes against (#17).
- [internal] Migration 0015 adds `report_publications`, which keeps each published Report's view model as a JSON snapshot. Rows are written once and never updated (#17).
- [user] A Report Definition's draft page lists every Highlight written since the baseline on the Goals it selects, each crediting the Goal's Owner. The author picks which go into the narrative's Insights, Accomplishments, and Misses (whatever each was flagged as), adds their own text to each section, and saves the narrative (#18).
- [user] The draft, the publication page, the print page, and the Markdown export show the narrative after the introduction. Each picked Highlight names the Goal's Owner and the Goal (#18).
- [user] Publishing freezes the narrative with the snapshot. The next publication's narrative starts empty (#18).
- [internal] Migration 0018 adds `narrative_picks` and `narrative_texts`, a Report Definition's draft narrative. Publishing clears them (#18).
- [user] Comment on any Goal in a published Report from its publication page. The comment emails the Goal's current Owner. The Owner and anyone else can reply in the thread, and each reply emails the Owner and everyone else in the thread, except its author and people marked departed (#19).
- [user] A Report's author (whoever saved its Definition, or published that publication) can turn a comment into an Action Item, or create one directly, with an owner and a due date. Action Items raised on a publication are listed on its page (#19).
- [user] Open Action Items appear at the top of each new publication and draft of the same Report Definition, and in its Markdown and print exports. They stay there until closed. Only the Action Item's owner can close it, and closing needs a note. An earlier publication still lists the item as it stood, but its page marks the item "Closed since" (#19).
- [ops] Emails the server sends link to `GOAL_TRACKER_BASE_URL`. This now includes comment alerts, not just the weekly emails (#19).
- [internal] Migration 0016 adds `comments` and 0017 adds `action_items` (#19).
- [user] Download a published Report as Markdown from its publication page. The file has the same content as the snapshot: struck-through dates and removed Milestones, badges, and each exception's full block. Markdown characters that people typed are escaped, so they show as typed (#20).
- [user] Each published Report has a print-friendly page at `/reports/{id}/publications/{publication}/print`. It shows the snapshot without the app's navigation, so you can print it to PDF from the browser (#20).
- [user] Weekly Check-in reminder: each Owner and Delegate gets an email listing their Active Goals that are Stale or will go Stale before next week's reminder, each linking to its pre-filled Check-in form (#21).
- [user] Weekly parent digest: each parent Owner gets an email listing the link requests waiting on them. It also lists the Goals contributing to theirs that went Yellow or Red, recorded a Date Slip, or went Stale in the past week, and those that are Ownerless or have a parent On Hold or Cancelled (#21).
- [ops] `GOAL_TRACKER_REMINDER_DAY` (default `Monday`) and `GOAL_TRACKER_REMINDER_TIME` (default `09:00`, in the org's timezone) set when the weekly emails go out; an unreadable value stops the server at start-up. `GOAL_TRACKER_BASE_URL` sets where links in emails point (default derived from `GOAL_TRACKER_ADDR`, e.g. `http://localhost:8080`). The prototype logs each email instead of delivering it (#21).
- [user] Spreadsheet import: an Admin loads the pilot's Goals from a CSV or XLSX on the new Import page — Goals with Owners (by email, given accounts if new), So What, Dated/Ongoing and delivery dates, Milestones, Metrics, parent links (accepted automatically), and Dimension values. A dry run reports errors row by row and saves nothing; a commit is all-or-nothing, and cycles between imported Goals are rejected. The column format is documented in `docs/import-format.md` with an example file (#22).
- [user] Seed a fake org for demos: `make seed` (or `go run ./cmd/seed`) fills a fresh database with about 50 Goals across six teams (a Team Dimension) and three levels of the graph, with 12 weeks of Check-in history, Date Slips, Milestone Churn, and a mix of Health including Stale and Unaligned Goals. It builds everything through the spreadsheet import and the domain commands. It is deterministic (`-seed`, `-end`) and refuses a database that already has Goals. Documented in the new README (#23).
- [user] A Home page shows your week: the Goals you Own or are a Delegate on that are Stale or due a Check-in before next week's reminder, each with No change and Check in; the link requests and Handoffs waiting on you, with Accept and Reject; your Active Goals by Health; and the Goals delegated to you. Signing in now lands on Home (#34).
- [user] The top bar leads with Home, counting what needs you; it replaces the Pending links, Pending handoffs, and Delegated to me items, whose pages stay (#34).

**Improvements:**
- [user] Once a Goal is Active its dates move only in a Check-in, so every change is explained: the Milestone edit form and the Dated/Ongoing controls apply only while a Goal is Proposed (#5).
- [user] A Check-in that takes a Goal out of Active records no Health, so it needs no Path to Green; a Goal that is On Hold, Done, or Cancelled shows no Health, and an On Hold Goal's Check-in can only resume or Cancel it (#7).
- [user] Every page now wears the new design: a shared stylesheet, IBM Plex and Source Serif type (falling back to system fonts), and a dark top bar that marks the page you're on and shows who is signed in, with Sign out on the right. It fits a phone-width screen without scrolling sideways (#33).
- [user] Checking in has its own page, so the common case takes seconds: a "Nothing changed since <date>?" card repeats the last Check-in in one click, Health is three large Green / Yellow / Red buttons, and the Path to Green shows only for Yellow or Red. The Highlight, dates and Milestones, and Lifecycle sections start collapsed, each summary showing its current value, and open by themselves when they hold an error or something you typed. Cancelling a Goal asks you to confirm. The Goal page links to the Check-in page and keeps a No change button and a compact history with Health badges (#37).
- [user] Published Reports read like an exec document: centred at a reading width with a breadcrumb, the publisher and baseline on one line, and Markdown and Print / PDF buttons; Red, Yellow, Green, and Stale count tiles; open Action Items as text · owner · due with the close form behind a Close button; the narrative in the serif face; exceptions as Needs attention cards with Health and report badges and a status box that marks a missed Path to Green Overdue; and the rest as an On track table of Health · Goal · Owner · Due (#39).
- [user] Each Goal's discussion on a publication is a footer row collapsed by default: "N comments" opens the threads (replies indented), a Comment button opens the form, and Reply and Make action item sit behind their own toggles — all without JS (#39).
- [user] The draft Report page reads baseline → narrative → draft preview, with Publish at the bottom as the primary button ("Publishes a frozen copy readers can comment on.") and publications in a sidebar (#39).
- [user] The Reports page lists saved definitions as cards and keeps the form behind a New report button; its Root Goals picker is a scrolling list with Top-level Goals first, and Depth explains its numbers (#39).
- [user] The print page is set in Source Serif 4 (falling back to Georgia) and marks Health with a shape as well as its name — ■ Red, ▲ Yellow, ● Green — so it survives black-and-white printing (#39).

**Bug fixes:**
- [internal] The build compiles again after the reports (#39) and Check-in (#37) redesigns merged together: each had added an identical `healthClass` helper to the `web` package; the Check-in copy is removed and both pages use the one in `reports.templ`.
- [user] Creating a child Goal from a parent's page is all-or-nothing. If a kept default fails to assign (a value retired after the form loaded, say) or the link can't be requested, no Goal is left behind, and the parent's page comes back with the error and the title, So What, and checked defaults as typed (#26).
- [user] The Check-in form's errors read naturally: cancelling a Goal without a reason says "cancelling a Goal needs a reason", and a Metric reading that isn't a number names the Metric (`"Signups" needs a number`) instead of its database id (#29).
- [user] The Check-in form pre-fills each Metric's reading with its latest value, like the rest of the form, so a "same as last week" Check-in stays one click (#29).
- [user] Spreadsheet import reads an XLSX date cell — what Excel and Google Sheets make of a typed `2026-06-30` — as its date, whatever its display format, instead of rejecting its displayed text (`06-30-26`) as a bad date (#30).
- [user] Spreadsheet import numbers rows as the spreadsheet does: the header is row 1, so the first Goal is row 2, not "Row 1" (#30).
- [user] Spreadsheet import reports every error on a row in one dry run — an unknown Dimension value no longer hides an unknown parent title or a bad Milestone (#30).
- [user] The seeded demo org marks its three org outcomes Top-level Goals, so the Unaligned list on the signals page shows only the five side projects (#31).
- [user] A Check-in validation error is shown next to the field it is about instead of always under the Path to Green: "this Health differs from the Rolled-up Health (Red); explain why" sits by the explanation, a Date Slip's missing reason by that date's reason, an overdue Milestone by its date, a Lifecycle's missing reason or outcome by that field, and a reading error by its Metric. An error about the Check-in as a whole is shown at the top of the form (#28).

**Security:**
- [user] Only a Goal's Owner or an Admin can set its Dimension values. Anyone else who posts to a Goal's dimensions endpoint gets a 403 and the value stays as it was. Hiding the form wasn't enough, because the endpoint accepted the change (#25).

**Code quality:**
- [internal] Split the skeleton's shared files by feature area (one sqlc query file, handler file, templ file, and domain file per area, with route registration in one place), so tickets can land without all editing the same files and campaigns can run tickets in parallel (#24).
- [internal] Remove a Go test binary (`seed.test`, 15 MB) committed by accident with the seed command, and ignore `*.test` (#23).
For usage details, see [README](README.md).
