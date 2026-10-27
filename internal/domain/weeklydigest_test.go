package domain_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const digestDay = 24 * time.Hour

// digestChildOf creates an Active Goal owned by owner contributing to parent,
// the link accepted by the parent's Owner.
func digestChildOf(h *testsupport.Harness, owner domain.Account, parent domain.Goal, title string) domain.Goal {
	h.T.Helper()
	child := h.ActiveGoal(owner, title, title+" matters.")
	digestLink(h, child, parent)
	return child
}

// digestLink has child's Owner ask for it to contribute to parent, and
// parent's Owner accept when the request waits on them.
func digestLink(h *testsupport.Harness, child, parent domain.Goal) {
	h.T.Helper()
	link := h.RequestLink(child.Owner, child, parent, "")
	if link.Status == domain.LinkPending {
		if _, err := h.Service.AcceptLink(context.Background(), link.ID, parent.Owner.ID); err != nil {
			h.T.Fatalf("AcceptLink: %v", err)
		}
	}
}

// digestBackToGreenBy is the back-to-Green date of every Path to Green these
// tests write, kept unchanged so no Check-in slips it.
var digestBackToGreenBy = testsupport.Epoch.AddDate(0, 3, 0)

// digestCheckin submits a Check-in setting health, with a Path to Green when
// it isn't Green.
func digestCheckin(h *testsupport.Harness, author domain.Account, goalID int64, health string) {
	h.T.Helper()
	if health == domain.HealthGreen {
		h.Checkin(author, goalID, health, "On track.", "", time.Time{})
		return
	}
	h.Checkin(author, goalID, health, "Behind.", "Add a second engineer.", digestBackToGreenBy)
}

// weeklyDigests reads the week's digests, failing the test on error.
func weeklyDigests(h *testsupport.Harness) []domain.WeeklyDigest {
	h.T.Helper()
	digests, err := h.Service.WeeklyDigests(context.Background())
	if err != nil {
		h.T.Fatalf("WeeklyDigests: %v", err)
	}
	return digests
}

// digestFor is the one digest for to, failing the test when there isn't one.
func digestFor(t *testing.T, digests []domain.WeeklyDigest, to domain.Account) domain.WeeklyDigest {
	t.Helper()
	var out []domain.WeeklyDigest
	for _, d := range digests {
		if d.Recipient.ID == to.ID {
			out = append(out, d)
		}
	}
	if len(out) != 1 {
		t.Fatalf("digests for %s = %d, want 1: %+v", to.Email, len(out), digests)
	}
	return out[0]
}

// problemTitles is each problem's child title, in order.
func problemTitles(d domain.WeeklyDigest) []string {
	var out []string
	for _, p := range d.Problems {
		out = append(out, p.Child.Title)
	}
	return out
}

// A child whose Health got worse this week, to Yellow or to Red, is a problem
// naming the Health it went to; one that was Red a week ago and still is
// isn't, nor is one that stayed Green.
func TestWeeklyDigestsNameChildrenThatGotWorseThisWeek(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	wentRed := digestChildOf(h, kim, parent, "Launch pricing page")
	stillRed := digestChildOf(h, kim, parent, "Cut churn")
	green := digestChildOf(h, kim, parent, "Upsell annual plans")
	wentYellow := digestChildOf(h, kim, parent, "Partner referrals")
	digestCheckin(h, kim, wentRed.ID, domain.HealthGreen)
	digestCheckin(h, kim, stillRed.ID, domain.HealthRed)
	digestCheckin(h, kim, green.ID, domain.HealthGreen)
	h.Clock.Advance(8 * digestDay)
	digestCheckin(h, kim, wentRed.ID, domain.HealthRed)
	digestCheckin(h, kim, stillRed.ID, domain.HealthRed)
	digestCheckin(h, kim, green.ID, domain.HealthGreen)
	digestCheckin(h, kim, wentYellow.ID, domain.HealthYellow)
	h.Clock.Advance(2 * digestDay)

	d := digestFor(t, weeklyDigests(h), pat)

	want := map[string]string{"Partner referrals": domain.HealthYellow, "Launch pricing page": domain.HealthRed}
	if len(d.Problems) != len(want) {
		t.Fatalf("problems = %v, want %v", problemTitles(d), want)
	}
	for _, p := range d.Problems {
		if p.Parent.ID != parent.ID {
			t.Errorf("%s's parent = %q, want %q", p.Child.Title, p.Parent.Title, parent.Title)
		}
		if p.WorsenedTo != want[p.Child.Title] {
			t.Errorf("%s WorsenedTo = %q, want %q", p.Child.Title, p.WorsenedTo, want[p.Child.Title])
		}
	}
}

// A child On Hold when the recipient's week began had no Health then — the
// Check-in that put it On Hold recorded none — as a Report reads a Goal On
// Hold at its baseline. So one Yellow before it was put On Hold, and resumed
// Yellow or Red this week, got worse to the Health it resumed at this week.
func TestWeeklyDigestsNameAChildResumedYellowOrRedFromOnHoldThisWeek(t *testing.T) {
	t.Parallel()
	for _, resumed := range []string{domain.HealthYellow, domain.HealthRed} {
		t.Run(resumed, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t)
			pat := h.SignIn("pat@example.com")
			kim := h.SignIn("kim@example.com")
			parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
			child := digestChildOf(h, kim, parent, "Partner referrals")
			digestCheckin(h, kim, child.ID, domain.HealthYellow)
			h.Clock.Advance(digestDay)
			putOnHold(h, child)
			h.Clock.Advance(8 * digestDay)
			if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
				GoalID: child.ID, AuthorID: kim.ID, Health: resumed, Status: "Back, behind.",
				PathToGreen: "Add a second engineer.", PathTargetDate: digestBackToGreenBy, Lifecycle: domain.LifecycleActive,
			}); err != nil {
				t.Fatalf("SubmitCheckin resume: %v", err)
			}
			h.Clock.Advance(2 * digestDay)

			d := digestFor(t, weeklyDigests(h), pat)

			if len(d.Problems) != 1 || d.Problems[0].Child.ID != child.ID {
				t.Fatalf("problems = %v, want %s", problemTitles(d), child.Title)
			}
			if got := d.Problems[0].WorsenedTo; got != resumed {
				t.Errorf("WorsenedTo = %q, want %s", got, resumed)
			}
		})
	}
}

// digestSlip moves goal's delivery date a month later in a Yellow Check-in,
// recording a Date Slip of it alone.
func digestSlip(h *testsupport.Harness, goal domain.Goal) {
	h.T.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           goal.Owner.ID,
		Health:             domain.HealthYellow,
		Status:             "Vendor is late.",
		PathToGreen:        "Chase the vendor.",
		PathTargetDate:     digestBackToGreenBy,
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 1, 0),
		DeliveryDateReason: "Vendor is late.",
	}); err != nil {
		h.T.Fatalf("SubmitCheckin with a Date Slip: %v", err)
	}
}

// A child that recorded a Date Slip this week is a problem carrying the slip;
// one whose only slip was recorded before the week started isn't.
func TestWeeklyDigestsNameChildrenThatSlippedThisWeek(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	slippedLastWeek := digestChildOf(h, kim, parent, "Cut churn")
	slippedThisWeek := digestChildOf(h, kim, parent, "Launch pricing page")
	digestSlip(h, slippedLastWeek)
	h.Clock.Advance(8 * digestDay)
	digestCheckin(h, kim, slippedLastWeek.ID, domain.HealthYellow)
	digestCheckin(h, kim, slippedThisWeek.ID, domain.HealthYellow)
	h.Clock.Advance(digestDay)
	digestSlip(h, slippedThisWeek)
	h.Clock.Advance(digestDay)

	d := digestFor(t, weeklyDigests(h), pat)

	if len(d.Problems) != 1 || d.Problems[0].Child.ID != slippedThisWeek.ID {
		t.Fatalf("problems = %v, want only Launch pricing page", problemTitles(d))
	}
	slips := d.Problems[0].DateSlips
	if len(slips) != 1 || slips[0].Reason != "Vendor is late." || slips[0].MilestoneID != 0 || slips[0].BackToGreen {
		t.Errorf("Launch pricing page's Date Slips this week = %+v, want its one delivery date slip", slips)
	}
}

// A digest's week is counted in its recipient's Timezone, or the company's
// for a recipient with none. Across the end of daylight saving the New York
// week, from 2026-10-29 12:00 EDT, starts an hour before the UTC week, so a
// Date Slip recorded in that hour is this week's for a parent Owner in New
// York and last week's for one with no Timezone, though it is the same child's.
func TestWeeklyDigestsCountTheWeekInTheRecipientsTimezone(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	lee := h.SignIn("lee@example.com")
	kim := h.SignIn("kim@example.com")
	h.SetTimezone(pat, "America/New_York")
	pat.Timezone = "America/New_York"
	newYorkParent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	utcParent := h.ActiveGoal(lee, "Enter Japan", "Japan is our next market.")
	child := digestChildOf(h, kim, newYorkParent, "Launch pricing page")
	digestLink(h, child, utcParent)
	h.Clock.Set(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	digestCheckin(h, kim, child.ID, domain.HealthYellow)
	h.Clock.Set(time.Date(2026, 10, 29, 16, 30, 0, 0, time.UTC))
	digestSlip(h, child)
	h.Clock.Set(time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC)) // 12:00 EST

	digests := weeklyDigests(h)

	newYork := digestFor(t, digests, pat)
	if len(newYork.Problems) != 1 || len(newYork.Problems[0].DateSlips) != 1 {
		t.Errorf("the New York Owner's problems = %+v, want Launch pricing page with its Date Slip", newYork.Problems)
	}
	for _, d := range digests {
		if d.Recipient.ID == lee.ID {
			t.Errorf("the Owner with no Timezone has a digest, want none: the slip was last week in UTC: %+v", d)
		}
	}
}

// A child whose Check-in went overdue this week is a problem carrying its
// Freshness; one overdue since before the week started isn't, nor is a fresh
// one.
func TestWeeklyDigestsNameChildrenThatWentCheckinOverdueThisWeek(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	wentOverdue := digestChildOf(h, kim, parent, "Launch pricing page")
	digestChildOf(h, kim, parent, "Cut churn")
	fresh := digestChildOf(h, kim, parent, "Upsell annual plans")
	h.Clock.Advance(11 * digestDay)
	digestCheckin(h, kim, wentOverdue.ID, domain.HealthGreen) // 9 days before the digest
	h.Clock.Advance(8 * digestDay)
	digestCheckin(h, kim, fresh.ID, domain.HealthGreen)
	h.Clock.Advance(digestDay)
	// Cut churn has had no Check-in for 20 days: Check-in overdue since day 8.

	d := digestFor(t, weeklyDigests(h), pat)

	if len(d.Problems) != 1 || d.Problems[0].Child.ID != wentOverdue.ID {
		t.Fatalf("problems = %v, want only Launch pricing page", problemTitles(d))
	}
	p := d.Problems[0]
	if !p.WentCheckinOverdue {
		t.Errorf("Launch pricing page WentCheckinOverdue = false, want true")
	}
	if got, want := p.Freshness.LastCheckinAge(), "last Check-in 9 days ago (every 7 days)"; got != want {
		t.Errorf("Launch pricing page's LastCheckinAge = %q, want %q", got, want)
	}
}

// An Ownerless child is a problem every week, for as long as it lasts.
func TestWeeklyDigestsNameOwnerlessChildren(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := digestChildOf(h, kim, parent, "Launch pricing page")
	digestCheckin(h, kim, child.ID, domain.HealthGreen)
	h.MarkDeparted(admin, kim)
	h.Clock.Advance(30 * digestDay)
	digestCheckin(h, pat, parent.ID, domain.HealthGreen)

	d := digestFor(t, weeklyDigests(h), pat)

	if len(d.Problems) != 1 || !d.Problems[0].Ownerless {
		t.Errorf("problems = %+v, want Launch pricing page, Ownerless", d.Problems)
	}
}

// putOnHold has goal's Owner put it On Hold in a Check-in.
func putOnHold(h *testsupport.Harness, goal domain.Goal) {
	h.T.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          goal.ID,
		AuthorID:        goal.Owner.ID,
		Status:          "Pausing it.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "Budget freeze.",
	}); err != nil {
		h.T.Fatalf("SubmitCheckin On Hold: %v", err)
	}
}

// A child that also contributes to Goals put On Hold is a problem under its
// other parent, naming its halted parents newest first.
func TestWeeklyDigestsNameChildrenWithAParentOnHold(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	lee := h.SignIn("lee@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	japan := h.ActiveGoal(lee, "Enter Japan", "Japan is our next market.")
	korea := h.ActiveGoal(lee, "Enter Korea", "Korea is the one after.")
	child := digestChildOf(h, kim, parent, "Launch pricing page")
	// Linked oldest first, so only sorting puts the newer Korea first.
	digestLink(h, child, japan)
	digestLink(h, child, korea)
	digestCheckin(h, kim, child.ID, domain.HealthGreen)
	putOnHold(h, japan)
	putOnHold(h, korea)

	d := digestFor(t, weeklyDigests(h), pat)

	if len(d.Problems) != 1 || d.Problems[0].Child.ID != child.ID {
		t.Fatalf("problems = %v, want Launch pricing page", problemTitles(d))
	}
	var halted []string
	for _, hp := range d.Problems[0].HaltedParents {
		halted = append(halted, hp.Parent.Title+" "+hp.Parent.Lifecycle)
	}
	if want := []string{"Enter Korea " + domain.LifecycleOnHold, "Enter Japan " + domain.LifecycleOnHold}; !slices.Equal(halted, want) {
		t.Errorf("halted parents = %v, want %v, newest first", halted, want)
	}
}

// A child contributing to two Goals of the same Owner is a problem under each,
// in one digest, its parents in ListGoals order, newest first.
func TestWeeklyDigestsNameAChildUnderEachOfItsParents(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	older := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	newer := h.ActiveGoal(pat, "Cut costs", "Margins matter.")
	child := digestChildOf(h, kim, older, "Launch pricing page")
	digestLink(h, child, newer)
	digestCheckin(h, kim, child.ID, domain.HealthGreen)
	h.Clock.Advance(8 * digestDay)
	digestCheckin(h, kim, child.ID, domain.HealthRed)

	d := digestFor(t, weeklyDigests(h), pat)

	var under []string
	for _, p := range d.Problems {
		if p.Child.ID != child.ID || p.WorsenedTo != domain.HealthRed {
			t.Errorf("problem %+v, want Launch pricing page gone Red", p)
		}
		under = append(under, p.Parent.Title)
	}
	if want := []string{"Cut costs", "Grow revenue"}; !slices.Equal(under, want) {
		t.Errorf("Launch pricing page is a problem under %v, want %v", under, want)
	}
}

// A parent Owner who has left the company gets no digest, though a child of
// their Goal went Red and a link request waits on them.
func TestWeeklyDigestsLeaveOutADepartedParentOwner(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := digestChildOf(h, kim, parent, "Launch pricing page")
	h.RequestLink(kim, h.ActiveGoal(kim, "Launch trials", "Buyers want to try."), parent, "")
	digestCheckin(h, kim, child.ID, domain.HealthGreen)
	h.Clock.Advance(8 * digestDay)
	digestCheckin(h, kim, child.ID, domain.HealthRed)
	h.MarkDeparted(admin, pat)

	if digests := weeklyDigests(h); len(digests) != 0 {
		t.Errorf("digests = %+v, want none: the only parent Owner has left", digests)
	}
}

// A parent Owner's digest holds the link requests waiting on them, oldest
// first. Recipients come in the order they are first met: those with link
// requests by their newest Goal, then those with problems, so lee comes before
// pat though pat's Goal is the newer.
func TestWeeklyDigestsHoldPendingLinkRequestsFirst(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	lee := h.SignIn("lee@example.com")
	kim := h.SignIn("kim@example.com")
	requested := h.ActiveGoal(lee, "Enter Japan", "Japan is our next market.")
	// pat's Goal is newer than lee's, so pat comes first in ListGoals.
	problems := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := digestChildOf(h, kim, problems, "Launch pricing page")
	digestCheckin(h, kim, child.ID, domain.HealthGreen)
	h.RequestLink(kim, h.ActiveGoal(kim, "Translate the site", "Buyers read Japanese."), requested, "")
	h.Clock.Advance(8 * digestDay)
	digestCheckin(h, kim, child.ID, domain.HealthRed)
	h.RequestLink(kim, h.ActiveGoal(kim, "Hire in Tokyo", "We need people there."), requested, "")

	digests := weeklyDigests(h)

	var to []string
	for _, d := range digests {
		to = append(to, d.Recipient.Email)
	}
	if want := []string{"lee@example.com", "pat@example.com"}; !slices.Equal(to, want) {
		t.Fatalf("digests go to %v, want %v", to, want)
	}
	var asks []string
	for _, l := range digests[0].PendingLinks {
		asks = append(asks, l.Child.Title+" to "+l.Parent.Title)
	}
	if want := []string{"Translate the site to Enter Japan", "Hire in Tokyo to Enter Japan"}; !slices.Equal(asks, want) {
		t.Errorf("lee's link requests = %v, want %v, oldest first", asks, want)
	}
	if len(digests[0].Problems) != 0 || len(digests[1].PendingLinks) != 0 || len(digests[1].Problems) != 1 {
		t.Errorf("digests = %+v, want lee's link requests and pat's one problem", digests)
	}
}
