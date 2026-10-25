package web

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/seed"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// seedSidebarCountsExtras adds to the seeded company what it has none of, so
// each thing Needs you counts is waiting on someone: a Manager who left, an
// Ask for help, one whose walk passes that Manager, an Action Item, a link
// request, a Handoff and a Parent suggestion waiting on a decision, and a
// Delegate.
func seedSidebarCountsExtras(t *testing.T, h *testsupport.Harness, adminEmail string) {
	t.Helper()
	ctx := context.Background()
	admin := h.SignIn(adminEmail)
	activeGoals := func() []domain.Goal {
		goals, err := h.Service.ListGoals(ctx)
		if err != nil {
			t.Fatalf("ListGoals: %v", err)
		}
		var active []domain.Goal
		for _, g := range goals {
			if g.Lifecycle == domain.LifecycleActive && !g.Owner.Departed {
				active = append(active, g)
			}
		}
		return active
	}
	// No Owner the seed keeps has a Departed Manager, so a Manager with one
	// of their own leaves, and an ask past them walks on up.
	active := activeGoals()
	for _, g := range active {
		if g.Owner.ManagerID == nil {
			continue
		}
		m, err := h.Service.Account(ctx, *g.Owner.ManagerID)
		if err != nil {
			t.Fatalf("Account(%d): %v", *g.Owner.ManagerID, err)
		}
		if m.ID != admin.ID && m.ManagerID != nil && !m.Departed {
			h.MarkDeparted(admin, m)
			break
		}
	}
	active = activeGoals()
	managerDeparted := func(g domain.Goal) bool {
		if g.Owner.ManagerID == nil {
			return false
		}
		m, err := h.Service.Account(ctx, *g.Owner.ManagerID)
		if err != nil {
			t.Fatalf("Account(%d): %v", *g.Owner.ManagerID, err)
		}
		return m.Departed
	}
	used := map[int64]bool{}
	pick := func(what string, ok func(domain.Goal) bool) domain.Goal {
		t.Helper()
		for _, g := range active {
			if !used[g.ID] && ok(g) {
				used[g.ID] = true
				return g
			}
		}
		t.Fatalf("the seeded company has no Active Goal %s", what)
		return domain.Goal{}
	}
	leaf := func(g domain.Goal) bool { return len(h.ChildrenOf(g)) == 0 }
	pathDate := h.Clock.Now().AddDate(0, 0, 14)

	asked := pick("whose Owner has a Manager", func(g domain.Goal) bool { return g.Owner.ManagerID != nil && !managerDeparted(g) })
	h.AskForHelp(asked.Owner, asked.ID, domain.HealthRed, "Blocked on the vendor.", "Escalate the contract.", pathDate)
	walked := pick("whose Owner's Manager is Departed", managerDeparted)
	h.AskForHelp(walked.Owner, walked.ID, domain.HealthYellow, "Short of hands.", "Borrow a reviewer.", pathDate)

	report := h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{
		Name: "Sidebar counts",
		Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{{
			Attribute: domain.RuleLifecycle,
			Op:        domain.RuleIsAnyOf,
			Values:    []string{domain.LifecycleActive},
		}},
	})
	pub := h.PublishReport(admin, report)
	item := pick("for an Action Item", func(domain.Goal) bool { return true })
	if _, err := h.Service.RaiseActionItem(ctx, admin.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: item.ID, Text: "Name the second vendor.", OwnerID: item.Owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}

	handedOff := pick("to hand off", func(domain.Goal) bool { return true })
	h.StartHandoff(handedOff.Owner, handedOff.ID, pick("of someone else", func(g domain.Goal) bool { return g.Owner.ID != handedOff.Owner.ID }).Owner)
	child := pick("with no children", leaf)
	parent := pick("of someone else", func(g domain.Goal) bool { return g.Owner.ID != child.Owner.ID })
	h.RequestLink(child.Owner, child, parent, "It unblocks theirs.")
	suggested := pick("with no children", leaf)
	suggestedParent := pick("of someone else", func(g domain.Goal) bool { return g.Owner.ID != suggested.Owner.ID })
	h.SuggestParent(suggestedParent.Owner, suggested, suggestedParent, "It unblocks mine.")

	// The seed has no Delegates: someone with no one under them, whose
	// Follow-ups are Mine, becomes one on someone else's Unlinked Goal.
	unlinked := map[int64]bool{}
	for _, g := range h.Unlinked() {
		unlinked[g.ID] = true
	}
	delegated := pick("that is Unlinked", func(g domain.Goal) bool { return unlinked[g.ID] })
	delegate := pick("of someone with no one under them", func(g domain.Goal) bool {
		under, err := h.Service.HasAnyoneUnder(ctx, g.Owner.ID)
		if err != nil {
			t.Fatalf("HasAnyoneUnder(%s): %v", g.Owner.Email, err)
		}
		return !under && g.Owner.ID != delegated.Owner.ID
	}).Owner
	h.AddDelegate(delegated.Owner, delegate, delegated.ID)
}

// The sidebar's counts are the pages they summarise (#465): over the seeded
// company, some of its Owners Departed, with what seedSidebarCountsExtras
// adds, at its end and 3 and 10 days on, each
// person's Needs you count is what their Home lists, and their Follow-ups
// count is how many Goals a bare Follow-ups page lists them. So the badges
// can't drift from either page's definition.
func TestSidebarCountsWhatHomeAndFollowUpsList(t *testing.T) {
	t.Parallel()

	const adminEmail = "admin@example.com"
	h := testsupport.New(t, adminEmail)
	ctx := context.Background()
	if _, err := seed.Run(ctx, h.Service, h.Clock, seed.Options{Seed: seed.DefaultSeed, Admin: adminEmail}); err != nil {
		t.Fatalf("seed.Run: %v", err)
	}
	seedSidebarCountsExtras(t, h, adminEmail)
	rows, err := db.New(h.DB).ListAskWalkAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("the seeded company has %d Accounts; want everyone it seeds", len(rows))
	}
	s := NewServer(h.Service)
	daysOn := 0
	for _, days := range []int{0, 3, 10} {
		h.Clock.Advance(time.Duration(days-daysOn) * day)
		daysOn = days
		for _, row := range rows {
			current, err := h.Service.Account(ctx, row.ID)
			if err != nil {
				t.Fatalf("Account(%d): %v", row.ID, err)
			}
			sb, err := h.Service.Sidebar(ctx, current.ID)
			if err != nil {
				t.Fatalf("Sidebar for %s, %d days on: %v", current.Email, days, err)
			}
			home, err := s.loadHome(ctx, current.ID)
			if err != nil {
				t.Fatalf("loadHome for %s, %d days on: %v", current.Email, days, err)
			}
			if !sb.NeedsYouRead || sb.NeedsYou != home.NeedsYou() {
				t.Errorf("%s, %d days on: the sidebar's Needs you counts %d (read: %v), Home lists %d", current.Email, days, sb.NeedsYou, sb.NeedsYouRead, home.NeedsYou())
			}
			page, err := s.loadFollowUpsPage(ctx, url.Values{}, current)
			if err != nil {
				t.Fatalf("loadFollowUpsPage for %s, %d days on: %v", current.Email, days, err)
			}
			listed := map[int64]bool{}
			for _, r := range page.Rows {
				listed[r.Goal.ID] = true
			}
			if !sb.FollowUpsRead || sb.FollowUps != len(listed) {
				t.Errorf("%s, %d days on: the sidebar's Follow-ups counts %d (read: %v), a bare Follow-ups page lists %d Goals", current.Email, days, sb.FollowUps, sb.FollowUpsRead, len(listed))
			}
		}
	}
}
