# Keep one Service, and give a page that repeats reads a page loader

In October 2026 a code review (#429) flagged that `domain.Service` had 336 methods with no internal boundaries, and proposed a read model per page in front of it. By the time #411 had batched the list pages it had 389 methods (238 exported) across 53 files in `internal/domain`. Batching made the list pages flat: Home makes 41 queries, the Goal list 39 and Follow-ups 44, the same for 10 Goals as for 50, and the `querycount_*_test.go` tests pin those counts. The Goal page did not get flatter. Its view model (`goalPageView` in `internal/web/goalpage.go`) calls about 47 `Service` methods, each loading what it needs, so one view runs 82–88 queries, only about 50 of them distinct:

- `GetGoal` 7 times, `GetAccount` 4 times, `ListMilestoneChangesByGoal` 4 times, and `ListCheckins`, `ListChildGoals`, `ListAccountsUnder` and `ListDelegatedGoals` 3 times each;
- the whole-org reads `ListGoals` and `ListActiveGoalsWithLatestCheckin` twice each.

(Measured on `main` at 71a65bf with a probe through `newQueryCountingServer` and `seedQueryCountFixture(t, h, 10)`.)

We decided to keep `Service` one type, with one file per concept, and not to add a general read-model layer. The method count alone is not a problem worth fixing. The per-concept files are what keep campaign waves free of conflicts (CLAUDE.md, Code layout), and splitting the type would touch almost every file in `internal/domain` and `internal/web`. List pages are already flat through batching, and ADR 0009 keeps queries in-process and cheap, so a repeated query is a cost in tidiness before it is a performance problem. Instead, a page that repeats reads, as the Goal page does, gets its own page loader.

## Consequences

- **What a page loader is:** one `Service` method that returns everything the page shows. It lives in its own `internal/domain/<page>.go` file and reads each thing once. It reuses the existing pure helpers, and checks the viewer's permission once, as the separate methods do today. The web handler builds the view from that one result, and a `querycount` test pins the loader's query count.
- **A page earns a loader;** it isn't given one in advance. A page gets one only when a probe shows it repeating reads. Mutators, and pages that don't repeat reads, stay on the ordinary `Service` methods.
- **The Goal page is the first** (#462).
- **The Check-in page is the second** (#560). Its form had been assembled on four paths: the page, the error re-render, "Add another" and the refused No change. They ran the whole Goal page loader or a handful of separate `Service` calls, 49–63 queries each. Now each path calls `Service.CheckinPage` once, for 24 queries, or 28–31 when the command it posted refused. Checking it against the second-loader trigger below: the two loaders share the people lookup (`goalPageAccounts`) and the pure helpers that judge the Rolled-up Health, Run off Green and No change (`rollUp`, `runOffGreen`, `noChangeFactsOf`, `judgeNoChange`). They share no type and no reading layer, because each reads only what its own page shows: 11 queries for the Check-in page against the Goal page's 36. Sharing more would make the Check-in page read the Goal page's history.
- **What triggers a revisit:** any one of
  - a page whose query count grows with the number of Goals, which batching should handle first;
  - a second page needing a loader with the same shape as the first, at which point consider whether loaders should share anything.

  "`Service` has too many methods" is not a trigger.
