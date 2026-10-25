package domain_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// sidebarFixture is a small Org for the sidebar's counts: lead@ over rep@,
// over sub@, and out@ outside it. Ten days after their Goals were made, a
// Goal nobody has checked in on since is due a Check-in and Check-in overdue.
type sidebarFixture struct {
	h                   *testsupport.Harness
	lead, rep, sub, out domain.Account
}

// seedSidebarFixture seeds, for lead@, one of each thing that needs them: a
// Goal of their own due a Check-in, one they are a Delegate on, rep@'s Ask
// for help walking up to them, an open Action Item, and a link request, a
// Handoff and a Parent suggestion to decide. Across the Org it flags lead@'s
// Goal (Check-in overdue, Unlinked), rep@'s asking one (Red), sub@'s
// Top-level one only by an overdue Action Item, and sub@'s other one
// (Check-in overdue), and leaves rep@'s Top-level Green one clear. Outside
// it, out@'s two Goals are flagged: one sub@ is a Delegate on, and one lead@
// is.
func seedSidebarFixture(t *testing.T) sidebarFixture {
	t.Helper()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	fx := sidebarFixture{h: h, lead: h.SignIn("lead@example.com"), rep: h.SignIn("rep@example.com"), sub: h.SignIn("sub@example.com"), out: h.SignIn("out@example.com")}
	h.SetManager(fx.rep, fx.lead)
	h.SetManager(fx.sub, fx.rep)

	leadGoal := h.ActiveGoal(fx.lead, "Lead's goal", "It matters.")
	repAsking := h.ActiveGoal(fx.rep, "Rep's asking goal", "It matters.")
	repClear := h.MarkTopLevel(admin, h.ActiveGoal(fx.rep, "Rep's clear goal", "It matters."))
	subTop := h.MarkTopLevel(admin, h.ActiveGoal(fx.sub, "Sub's top goal", "It matters."))
	h.ActiveGoal(fx.sub, "Sub's late goal", "It matters.")
	outGoal := h.ActiveGoal(fx.out, "Out's goal", "It matters.")
	outDelegated := h.ActiveGoal(fx.out, "Out's delegated goal", "It matters.")
	h.AddDelegate(fx.out, fx.sub, outGoal.ID)
	h.AddDelegate(fx.out, fx.lead, outDelegated.ID)

	h.RequestLink(fx.out, outGoal, leadGoal, "It feeds yours.")
	h.StartHandoff(fx.out, outDelegated.ID, fx.lead)
	h.SuggestParent(fx.out, leadGoal, repClear, "It rolls up there.")

	pub := h.PublishReport(admin, h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{
		Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{leadGoal.ID, subTop.ID},
	}))
	for _, item := range []struct {
		goal  domain.Goal
		owner domain.Account
		days  int
	}{{leadGoal, fx.lead, 30}, {subTop, fx.sub, 3}} {
		if _, err := h.Service.RaiseActionItem(ctx, admin.ID, domain.RaiseActionItemInput{
			PublicationID: pub.ID, GoalID: item.goal.ID, Text: "Follow it up.", OwnerID: item.owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, item.days),
		}); err != nil {
			t.Fatalf("RaiseActionItem on %q: %v", item.goal.Title, err)
		}
	}

	h.Clock.Advance(10 * 24 * time.Hour)
	h.AskForHelp(fx.rep, repAsking.ID, domain.HealthRed, "Blocked on a vendor.", "Find a second vendor.", h.Clock.Now().AddDate(0, 0, 14))
	h.Checkin(fx.rep, repClear.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(fx.sub, subTop.ID, domain.HealthGreen, "On track.", "", time.Time{})
	return fx
}

// sidebarOf reads acc's sidebar, failing the test on error.
func sidebarOf(t *testing.T, fx sidebarFixture, acc domain.Account) domain.Sidebar {
	t.Helper()
	sb, err := fx.h.Service.Sidebar(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("Sidebar for %s: %v", acc.Email, err)
	}
	return sb
}

// The sidebar's needs-you count is Home's: the Goals someone Owns or is a
// Delegate on that are due a Check-in before the next reminder, the Asks for
// help to them, their open Action Items, and the link requests, Handoffs and
// Parent suggestions waiting on them.
func TestSidebarCountsWhatNeedsYouAsHomeDoes(t *testing.T) {
	t.Parallel()
	fx := seedSidebarFixture(t)

	for _, c := range []struct {
		who  domain.Account
		want int
	}{
		// Lead's goal and Out's delegated goal due, rep@'s ask, an Action
		// Item, a link request, a Handoff and a Parent suggestion.
		{fx.lead, 7},
		// Rep's asking goal is checked in on today, and its ask goes to
		// lead@; rep@ has nothing else waiting.
		{fx.rep, 0},
		// Sub's late goal and Out's goal, as its Delegate, due, and the
		// Action Item on Sub's top goal.
		{fx.sub, 3},
		// Out's two Goals due.
		{fx.out, 2},
	} {
		if got := sidebarOf(t, fx, c.who).NeedsYou; got != c.want {
			t.Errorf("Sidebar(%s).NeedsYou = %d, want %d", c.who.Email, got, c.want)
		}
	}
}

// The sidebar's Follow-ups count is how many Goals a bare Follow-ups page
// lists: those its signals flag in the person's default scope, each once
// however many flag it. That is My org, by Owner, for someone with anyone
// under them, and Mine, Delegate Goals included, for anyone else.
func TestSidebarCountsFollowUpsInTheDefaultScope(t *testing.T) {
	t.Parallel()
	fx := seedSidebarFixture(t)

	for _, c := range []struct {
		who  domain.Account
		want int
	}{
		// Lead's goal, Rep's asking goal, Sub's top goal and Sub's late
		// goal; not Rep's clear goal, nor Out's delegated goal, which lead@
		// is only a Delegate on.
		{fx.lead, 4},
		// Rep's asking goal, Sub's top goal and Sub's late goal.
		{fx.rep, 3},
		// Mine: Sub's top goal, Sub's late goal and Out's goal, as its
		// Delegate.
		{fx.sub, 3},
		// Mine: Out's goal and Out's delegated goal.
		{fx.out, 2},
	} {
		if got := sidebarOf(t, fx, c.who).FollowUps; got != c.want {
			t.Errorf("Sidebar(%s).FollowUps = %d, want %d", c.who.Email, got, c.want)
		}
	}
}

// The sidebar's My org lists the people directly under someone by Manager, as
// DirectlyUnder does: not those further down, and no one for someone with no
// one under them.
func TestSidebarListsThePeopleDirectlyUnderSomeone(t *testing.T) {
	t.Parallel()
	fx := seedSidebarFixture(t)

	for _, c := range []struct {
		who  domain.Account
		want []string
	}{
		{fx.lead, []string{"rep@example.com"}},
		{fx.rep, []string{"sub@example.com"}},
		{fx.sub, nil},
	} {
		var got []string
		for _, a := range sidebarOf(t, fx, c.who).DirectlyUnder {
			got = append(got, a.Email)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("Sidebar(%s).DirectlyUnder = %q, want %q", c.who.Email, got, c.want)
		}
	}
}

// A failed read leaves out only what it feeds: Sidebar reports the error and
// still returns, as it read them, the parts the read doesn't feed. The
// Handoffs feed only the needs-you count, the overdue Milestones only the
// Follow-ups count, the Delegate Goals both counts, and every Account all
// three.
func TestSidebarLeavesOutOnlyWhatAFailedReadFeeds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		table                          string
		needsYou, followUps, underRead bool
	}{
		{"handoffs", false, true, true},
		{"milestones", true, false, true},
		{"delegates", false, false, true},
		{"accounts", false, false, false},
	} {
		t.Run(c.table, func(t *testing.T) {
			t.Parallel()
			fx := seedSidebarFixture(t)
			if _, err := fx.h.DB.Exec(`ALTER TABLE ` + c.table + ` RENAME TO gone`); err != nil {
				t.Fatalf("rename %s away: %v", c.table, err)
			}

			sb, err := fx.h.Service.Sidebar(context.Background(), fx.lead.ID)
			if err == nil {
				t.Error("Sidebar: no error, want the failed read's")
			}
			if sb.NeedsYouRead != c.needsYou || (c.needsYou && sb.NeedsYou != 7) {
				t.Errorf("Sidebar.NeedsYou = %d (read %t), want read %t, 7 when read", sb.NeedsYou, sb.NeedsYouRead, c.needsYou)
			}
			if sb.FollowUpsRead != c.followUps || (c.followUps && sb.FollowUps != 4) {
				t.Errorf("Sidebar.FollowUps = %d (read %t), want read %t, 4 when read", sb.FollowUps, sb.FollowUpsRead, c.followUps)
			}
			var under []string
			for _, a := range sb.DirectlyUnder {
				under = append(under, a.Email)
			}
			var wantUnder []string
			if c.underRead {
				wantUnder = []string{"rep@example.com"}
			}
			if sb.DirectlyUnderRead != c.underRead || !slices.Equal(under, wantUnder) {
				t.Errorf("Sidebar.DirectlyUnder = %q (read %t), want %q, read %t", under, sb.DirectlyUnderRead, wantUnder, c.underRead)
			}
		})
	}
}
