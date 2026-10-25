package web_test

import (
	"context"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The sidebar every signed-in page shows loads through one page loader that
// reads each thing once (ADR 0010): Service.Sidebar runs no query twice for
// an Owner, a Delegate or a Manager two levels above an Owner, and as many
// for 50 Goals as for 10.
func TestSidebarReadsEachThingOnce(t *testing.T) {
	t.Parallel()
	// sidebarMax is what Service.Sidebar reads: every Account, the viewer's
	// Delegate Goals, every Active Goal with its latest Check-in, the overdue
	// Milestones, the accepted links, the overdue Action Items, the open Asks
	// for help, and the viewer's open Action Items, link requests, Handoffs
	// and Parent suggestions.
	const sidebarMax = 11
	viewers := []struct{ name, email string }{
		// sam@ Owns Goal 04, which asks for help and is offered to priya@.
		{"an Owner", "sam@example.com"},
		// rita@ is a Delegate on Goal 04, with no one under them.
		{"a Delegate", "rita@example.com"},
		// morgan@ manages priya@ and mia@, who manages sam@.
		{"a Manager", "morgan@example.com"},
	}
	countFor := func(n int) map[string]int {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedGoalPageFixture(t, h, n)
		h.AddDelegate(h.SignIn("sam@example.com"), h.SignIn("rita@example.com"), fx.red.ID)
		_, svc, queries := newQueryCountingServer(t, h)
		out := map[string]int{}
		for _, v := range viewers {
			acc := h.SignIn(v.email)
			queries.take()
			if _, err := svc.Sidebar(context.Background(), acc.ID); err != nil {
				t.Fatalf("Sidebar for %s: %v", v.name, err)
			}
			ran := queries.take()
			for name, times := range countQueryNames(ran) {
				if times > 1 {
					t.Errorf("Sidebar ran %s %d times for %s with %d Goals; want it once", name, times, v.name, n)
				}
			}
			out[v.name] = len(ran)
		}
		return out
	}
	ten, fifty := countFor(10), countFor(50)
	for _, v := range viewers {
		if ten[v.name] > sidebarMax {
			t.Errorf("Sidebar ran %d queries for %s; want at most %d", ten[v.name], v.name, sidebarMax)
		}
		if ten[v.name] != fifty[v.name] {
			t.Errorf("Sidebar ran %d queries for %s with 10 Goals and %d with 50; want the same", ten[v.name], v.name, fifty[v.name])
		}
	}
}
