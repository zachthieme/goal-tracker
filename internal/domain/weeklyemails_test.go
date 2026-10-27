package domain_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// weeklyEmailsBaseURL is where the weekly emails' links point.
const weeklyEmailsBaseURL = "http://goals.test"

// weeklyEmailsService is a Service on the harness's database and recording
// email sender, whose emails link to the web app at weeklyEmailsBaseURL (the
// harness's own Service has no base URL) and are sent, as the harness's are,
// once queued.
func weeklyEmailsService(h *testsupport.Harness) *domain.Service {
	return domain.NewService(h.DB, h.Clock, h.Email, nil,
		domain.WithBaseURL(weeklyEmailsBaseURL), domain.WithEmailDeliveredOnCommit())
}

// sentTo returns the messages recorded for the address.
func sentTo(rec *email.Recorder, addr string) []email.Message {
	var out []email.Message
	for _, m := range rec.Sent() {
		if m.To == addr {
			out = append(out, m)
		}
	}
	return out
}

// An Owner whose Goal is Check-in overdue is reminded, and the reminder links
// straight to the Goal's Check-in page.
func TestReminderListsCheckinOverdueGoalWithLinkToItsCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(10 * day)

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	msgs := sentTo(h.Email, "sam@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to sam = %d, want 1: %+v", len(msgs), h.Email.Sent())
	}
	body := msgs[0].Body
	if !strings.Contains(body, "Ship search") {
		t.Errorf("reminder does not name the Goal:\n%s", body)
	}
	if !strings.Contains(body, "Ship search: Check-in overdue, no Check-in since activation 10 days ago (every 7 days)") {
		t.Errorf("reminder does not say the Goal is Check-in overdue:\n%s", body)
	}
	link := fmt.Sprintf("http://goals.test/goals/%d/checkin", g.ID)
	if !strings.Contains(body, link) {
		t.Errorf("reminder does not link to %s:\n%s", link, body)
	}
	if strings.Contains(body, "#checkin-form") {
		t.Errorf("reminder links to the Goal page's old #checkin-form anchor:\n%s", body)
	}
}

// A Goal is due when it would be Check-in overdue before next week's reminder
// if nobody checked in: on a weekly cadence, a Goal last checked in 3 days ago
// is due. A Goal on a 14-day cadence checked in 2 days ago is not, and an Owner
// with nothing due gets no email.
func TestReminderListsGoalsDueBeforeNextWeek(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	weekly := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	fortnightly := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	if _, err := h.Service.SetCadence(context.Background(), fortnightly.Owner.ID, fortnightly.ID, 14); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Checkin(sam, weekly.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(1 * day)
	h.Checkin(kim, fortnightly.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(2 * day)

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	msgs := sentTo(h.Email, "sam@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to sam = %d, want 1", len(msgs))
	}
	if body := msgs[0].Body; !strings.Contains(body, "Ship search: Check-in due, last Check-in 3 days ago (every 7 days)") || strings.Contains(body, "overdue") {
		t.Errorf("want Ship search listed as due, not Check-in overdue:\n%s", body)
	}
	if got := sentTo(h.Email, "kim@example.com"); len(got) != 0 {
		t.Errorf("kim has nothing due but was sent %d reminders: %+v", len(got), got)
	}
}

// A Delegate writes Check-ins for the Owner, so they are reminded of the Goals
// they're a Delegate on, alongside the Owner, and told whose Goal it is.
func TestReminderGoesToDelegatesToo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	h.Clock.Advance(10 * day)

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	if got := sentTo(h.Email, "sam@example.com"); len(got) != 1 || got[0].AccountID != sam.ID {
		t.Errorf("reminders to the Owner = %+v, want 1, for their Account %d", got, sam.ID)
	}
	msgs := sentTo(h.Email, "dee@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to the Delegate = %d, want 1", len(msgs))
	}
	if msgs[0].AccountID != dee.ID {
		t.Errorf("Delegate's reminder is for Account %d, want theirs, %d", msgs[0].AccountID, dee.ID)
	}
	body := msgs[0].Body
	link := fmt.Sprintf("http://goals.test/goals/%d/checkin", g.ID)
	if !strings.Contains(body, "Ship search") || !strings.Contains(body, link) {
		t.Errorf("Delegate's reminder does not list the Goal with its Check-in link:\n%s", body)
	}
	if strings.Contains(body, "#checkin-form") {
		t.Errorf("Delegate's reminder links to the Goal page's old #checkin-form anchor:\n%s", body)
	}
	if !strings.Contains(body, "sam@example.com") {
		t.Errorf("Delegate's reminder does not say whose Goal it is:\n%s", body)
	}
}

// Someone who has left the company isn't emailed: their Goal is Ownerless, but its
// Delegate is still reminded.
func TestReminderSkipsADepartedOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	if err := h.Service.MarkDeparted(context.Background(), admin.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	if got := sentTo(h.Email, "sam@example.com"); len(got) != 0 {
		t.Errorf("departed Owner was sent %d reminders, want none", len(got))
	}
	if got := sentTo(h.Email, "dee@example.com"); len(got) != 1 {
		t.Errorf("reminders to the Delegate = %d, want 1", len(got))
	}
}

// digestTo returns the one digest sent to addr, failing the test when there
// isn't exactly one.
func digestTo(t *testing.T, rec *email.Recorder, addr string) string {
	t.Helper()
	msgs := sentTo(rec, addr)
	if len(msgs) != 1 {
		t.Fatalf("digests to %s = %d, want 1: %+v", addr, len(msgs), rec.Sent())
	}
	return msgs[0].Body
}

// A parent Owner's digest lists the link requests waiting on them, linking to
// where they decide.
func TestDigestListsPendingLinkRequests(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := h.ActiveGoal(kim, "Launch pricing page", "Buyers can't see prices.")
	h.RequestLink(kim, child, parent, "")

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	for _, want := range []string{"Launch pricing page", "Grow revenue", "kim@example.com", "http://goals.test/links"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest does not mention %q:\n%s", want, body)
		}
	}
	if got := sentTo(h.Email, "kim@example.com"); len(got) != 0 {
		t.Errorf("kim owns no parent Goal but was sent %d digests", len(got))
	}
}

// childOf creates an Active Goal owned by owner contributing to parent, the
// link accepted by the parent's Owner.
func childOf(h *testsupport.Harness, owner domain.Account, parent domain.Goal, title string) domain.Goal {
	h.T.Helper()
	child := h.ActiveGoal(owner, title, title+" matters.")
	link := h.RequestLink(owner, child, parent, "")
	if link.Status == domain.LinkPending {
		if _, err := h.Service.AcceptLink(context.Background(), link.ID, parent.Owner.ID); err != nil {
			h.T.Fatalf("AcceptLink: %v", err)
		}
	}
	return child
}

// backToGreenBy is the back-to-Green date of every Path to Green these tests
// write. Keeping it unchanged from one Check-in to the next records no Date
// Slip of it, which the digest would report.
var backToGreenBy = testsupport.Epoch.AddDate(0, 3, 0)

// digestCheckinHealth submits a Check-in setting health, with a Path to Green
// when it isn't Green.
func digestCheckinHealth(h *testsupport.Harness, author domain.Account, goalID int64, health string) {
	h.T.Helper()
	if health == domain.HealthGreen {
		h.Checkin(author, goalID, health, "On track.", "", time.Time{})
		return
	}
	h.Checkin(author, goalID, health, "Behind.", "Add a second engineer.", backToGreenBy)
}

// The digest names children whose Health got worse this week — to Yellow or to
// Red — but not a child that was already Yellow a week ago and still is.
func TestDigestListsChildrenThatWentYellowOrRedThisWeek(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	wentRed := childOf(h, kim, parent, "Launch pricing page")
	stillYellow := childOf(h, kim, parent, "Cut churn")
	green := childOf(h, kim, parent, "Upsell annual plans")
	wentYellow := childOf(h, kim, parent, "Partner referrals")
	digestCheckinHealth(h, kim, wentRed.ID, domain.HealthGreen)
	digestCheckinHealth(h, kim, stillYellow.ID, domain.HealthYellow)
	digestCheckinHealth(h, kim, green.ID, domain.HealthGreen)
	digestCheckinHealth(h, kim, wentYellow.ID, domain.HealthGreen)
	h.Clock.Advance(8 * day)
	digestCheckinHealth(h, kim, wentRed.ID, domain.HealthRed)
	digestCheckinHealth(h, kim, stillYellow.ID, domain.HealthYellow)
	digestCheckinHealth(h, kim, green.ID, domain.HealthGreen)
	digestCheckinHealth(h, kim, wentYellow.ID, domain.HealthYellow)
	h.Clock.Advance(2 * day)

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	for _, want := range []string{"Updates on Goals contributing to yours:\n\n", "Launch pricing page", "went Red", "Partner referrals", "went Yellow"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest does not mention %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"Cut churn", "Upsell annual plans"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("digest mentions %q, which didn't get worse this week:\n%s", unwanted, body)
		}
	}
}

// slip moves goal's delivery date a month later in a Yellow Check-in, recording
// a Date Slip of it alone.
func slip(h *testsupport.Harness, author domain.Account, goal domain.Goal) {
	h.T.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           author.ID,
		Health:             domain.HealthYellow,
		Status:             "Vendor is late.",
		PathToGreen:        "Chase the vendor.",
		PathTargetDate:     backToGreenBy,
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 1, 0),
		DeliveryDateReason: "Vendor is late.",
	}); err != nil {
		h.T.Fatalf("SubmitCheckin with a Date Slip: %v", err)
	}
}

// The digest names a child that recorded a Date Slip this week, but not one
// whose only slip was the week before.
func TestDigestListsChildrenThatSlippedThisWeek(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	slippedLastWeek := childOf(h, kim, parent, "Cut churn")
	slippedThisWeek := childOf(h, kim, parent, "Launch pricing page")
	slip(h, kim, slippedLastWeek)
	h.Clock.Advance(8 * day)
	digestCheckinHealth(h, kim, slippedLastWeek.ID, domain.HealthYellow)
	slip(h, kim, slippedThisWeek)
	h.Clock.Advance(1 * day)

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	if !strings.Contains(body, "Launch pricing page") || !strings.Contains(body, "slipped") {
		t.Errorf("digest does not say Launch pricing page slipped:\n%s", body)
	}
	if strings.Contains(body, "Cut churn") {
		t.Errorf("digest mentions Cut churn, which slipped the week before:\n%s", body)
	}
}

// The digest names a child whose Check-in went overdue this week, but not one
// that was overdue already a week ago, nor one that's fresh.
func TestDigestListsChildrenThatWentCheckinOverdueThisWeek(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	wentOverdue := childOf(h, kim, parent, "Launch pricing page")
	childOf(h, kim, parent, "Cut churn")
	fresh := childOf(h, kim, parent, "Upsell annual plans")
	h.Clock.Advance(11 * day)
	digestCheckinHealth(h, kim, wentOverdue.ID, domain.HealthGreen) // 9 days before the digest
	h.Clock.Advance(8 * day)
	digestCheckinHealth(h, kim, fresh.ID, domain.HealthGreen)
	h.Clock.Advance(1 * day)
	// Cut churn has had no Check-in for 20 days: Check-in overdue since day 8.

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	if !strings.Contains(body, "Launch pricing page") || !strings.Contains(body, "Check-in went overdue, last Check-in 9 days ago (every 7 days)") {
		t.Errorf("digest does not say Launch pricing page's Check-in went overdue:\n%s", body)
	}
	for _, unwanted := range []string{"Cut churn", "Upsell annual plans"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("digest mentions %q, whose Check-in didn't go overdue this week:\n%s", unwanted, body)
		}
	}
}

// The digest names a child that is Ownerless — its Owner left the company — so the
// parent's Owner can get it reassigned.
func TestDigestListsOwnerlessChildren(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	childOf(h, kim, parent, "Launch pricing page")
	digestCheckinHealth(h, pat, parent.ID, domain.HealthGreen)
	if err := h.Service.MarkDeparted(context.Background(), admin.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	if !strings.Contains(body, "Launch pricing page") || !strings.Contains(body, "Ownerless") {
		t.Errorf("digest does not say Launch pricing page is Ownerless:\n%s", body)
	}
	if !strings.Contains(body, "is Ownerless: kim has left the company") {
		t.Errorf("digest does not say kim has left the company:\n%s", body)
	}
}

// A child that also contributes to a Goal put On Hold is named in the digest of
// its other parent's Owner, who learns the work may no longer be needed.
func TestDigestListsChildrenWithAParentOnHoldOrCancelled(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	lee := h.SignIn("lee@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	halted := h.ActiveGoal(lee, "Enter Japan", "Japan is our next market.")
	child := childOf(h, kim, parent, "Launch pricing page")
	link := h.RequestLink(kim, child, halted, "")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, lee.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	digestCheckinHealth(h, kim, child.ID, domain.HealthGreen)
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          halted.ID,
		AuthorID:        lee.ID,
		Status:          "Pausing the Japan launch.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "Budget freeze.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	for _, want := range []string{"Launch pricing page", "Enter Japan", "On Hold"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest does not mention %q:\n%s", want, body)
		}
	}
}

// Only Active Goals are checked in on routinely: a Proposed Goal or one On Hold
// is never on a reminder.
func TestReminderSkipsGoalsThatArentActive(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.CreateGoal(sam, "Explore voice search", "Typing is slow on phones.")
	h.OnHoldGoal(sam, "Enter Japan", "Japan is our next market.", "Budget freeze.")
	h.Clock.Advance(30 * day)

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	if got := sentTo(h.Email, "sam@example.com"); len(got) != 0 {
		t.Errorf("sent %d reminders for Goals that aren't Active, want none: %+v", len(got), got)
	}
}

// An email has no hover, so the reminder introduces the Owner a Delegate
// writes for as Name (email) once, and by Name after that (CONTEXT.md: Name).
func TestReminderIntroducesTheOwnerOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignInNamed("sam.berg@example.com", "Sam Berg")
	dee := h.SignIn("dee@example.com")
	for _, title := range []string{"Ship search", "Ship filters"} {
		g := h.ActiveGoal(sam, title, "People can't find things.")
		h.AddDelegate(sam, dee, g.ID)
	}
	h.Clock.Advance(10 * day)

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	msgs := sentTo(h.Email, "dee@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to the Delegate = %d, want 1", len(msgs))
	}
	body := msgs[0].Body
	if n := strings.Count(body, "as Delegate for Sam Berg (sam.berg@example.com)"); n != 1 {
		t.Errorf("reminder introduces the Owner %d times, want once:\n%s", n, body)
	}
	if n := strings.Count(body, "as Delegate for Sam Berg"); n != 2 {
		t.Errorf("reminder names the Owner %d times, want twice:\n%s", n, body)
	}
}

// The digest introduces each child's Owner as Name (email) at their first
// mention and by Name after that; someone without a Name reads as their
// email's local part.
func TestDigestIntroducesEachOwnerOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignInNamed("kim.lee@example.com", "Kim Lee")
	lou := h.SignIn("lou@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	for _, title := range []string{"Launch pricing page", "Launch trials"} {
		h.RequestLink(kim, h.ActiveGoal(kim, title, "Buyers can't see prices."), parent, "")
	}
	h.RequestLink(lou, h.ActiveGoal(lou, "Win back churned", "They left."), parent, "")

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	for _, want := range []string{
		"- Launch pricing page, owned by Kim Lee (kim.lee@example.com), asks to contribute to Grow revenue\n",
		"- Launch trials, owned by Kim Lee, asks to contribute to Grow revenue\n",
		"- Win back churned, owned by lou (lou@example.com), asks to contribute to Grow revenue\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("digest does not read %q:\n%s", want, body)
		}
	}
}

// reminderReport publishes a Report named name covering goals, authored by a
// new author, so a test can raise Action Items on it.
func reminderReport(h *testsupport.Harness, name string, goals ...domain.Goal) domain.Publication {
	h.T.Helper()
	author := h.SignIn(strings.ToLower(name) + "-author@example.com")
	var picked []int64
	for _, g := range goals {
		picked = append(picked, g.ID)
	}
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: name, Mode: domain.ReportModePicked, Picked: picked})
	return h.PublishReport(author, def)
}

// raiseReminderItem raises an open Action Item on pub, owned by owner and due
// on due, about goalID (0 for the whole Report). The email its owner gets
// when it's raised goes to a recorder of its own, so h.Email holds only the
// reminders.
func raiseReminderItem(h *testsupport.Harness, pub domain.Publication, owner domain.Account, goalID int64, text string, due time.Time) domain.ActionItem {
	h.T.Helper()
	svc := domain.NewService(h.DB, h.Clock, email.NewRecorder(), nil, domain.WithEmailDeliveredOnCommit())
	item, err := svc.RaiseActionItem(context.Background(), pub.PublishedBy.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID,
		GoalID:        goalID,
		Text:          text,
		OwnerID:       owner.ID,
		DueDate:       due,
	})
	if err != nil {
		h.T.Fatalf("RaiseActionItem: %v", err)
	}
	return item
}

// An Owner with a Check-in due and open Action Items gets one reminder holding
// both: the Check-in first, then the items they own, overdue first, each with
// its Report, the Goal it is about, its due date, and a link to it.
func TestReminderListsOpenActionItemsAfterCheckins(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	pub := reminderReport(h, "MBR", g)
	h.Clock.Advance(10 * day) // 2026-01-12
	raiseReminderItem(h, pub, sam, 0, "Move the MBR to Tuesdays", time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	raiseReminderItem(h, pub, sam, g.ID, "Book the legal review", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	msgs := sentTo(h.Email, "sam@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to sam = %d, want 1: %+v", len(msgs), h.Email.Sent())
	}
	if got, want := msgs[0].Subject, "Your Check-ins and Action Items this week"; got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
	body := msgs[0].Body
	checkin := fmt.Sprintf("These Goals need a Check-in:\n\n- Ship search: Check-in overdue, no Check-in since activation 10 days ago (every 7 days)\n  http://goals.test/goals/%d/checkin\n", g.ID)
	items := fmt.Sprintf("\nAction Items you own:\n\n"+
		"- Book the legal review (MBR, about Ship search): overdue since 2026-01-05\n  http://goals.test/reports/%d/publications/%d#action-items\n"+
		"- Move the MBR to Tuesdays (MBR): due 2026-01-20\n  http://goals.test/reports/%d/publications/%d#action-items\n",
		pub.DefinitionID, pub.ID, pub.DefinitionID, pub.ID)
	if want := checkin + items; body != want {
		t.Errorf("reminder body =\n%s\nwant\n%s", body, want)
	}
}

// Someone who owns an open Action Item but no Goal is still reminded, with a
// reminder that holds only their Action Items.
func TestReminderReachesAnActionItemOwnerWithNoCheckinDue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	pub := reminderReport(h, "MBR", g)
	raiseReminderItem(h, pub, kim, g.ID, "Book the legal review", time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC))

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	msgs := sentTo(h.Email, "kim@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to kim = %d, want 1: %+v", len(msgs), h.Email.Sent())
	}
	if got, want := msgs[0].Subject, "Your Action Items this week"; got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
	want := fmt.Sprintf("Action Items you own:\n\n"+
		"- Book the legal review (MBR, about Ship search): due 2026-01-09\n  http://goals.test/reports/%d/publications/%d#action-items\n", pub.DefinitionID, pub.ID)
	if body := msgs[0].Body; body != want {
		t.Errorf("reminder body =\n%s\nwant\n%s", body, want)
	}
}

// An Owner with a Check-in due and no open Action Item gets the reminder as it
// was before Action Items: its subject names only Check-ins, and it has no
// Action Items part. A closed Action Item isn't listed.
func TestReminderWithNoOpenActionItemsListsOnlyCheckins(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	pub := reminderReport(h, "MBR", g)
	item := raiseReminderItem(h, pub, sam, g.ID, "Book the legal review", time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC))
	if _, err := h.Service.CloseActionItem(context.Background(), sam.ID, item.ID, "Booked for March."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	h.Clock.Advance(10 * day)

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	msgs := sentTo(h.Email, "sam@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to sam = %d, want 1: %+v", len(msgs), h.Email.Sent())
	}
	if got, want := msgs[0].Subject, "Your Check-ins this week"; got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
	if body := msgs[0].Body; strings.Contains(body, "Action Items you own:") || strings.Contains(body, "Book the legal review") {
		t.Errorf("reminder lists Action Items, want none:\n%s", body)
	}
}

// An Action Item's owner who has since left the company isn't emailed about it.
func TestReminderSkipsADepartedActionItemOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	pub := reminderReport(h, "MBR", g)
	raiseReminderItem(h, pub, kim, g.ID, "Book the legal review", time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC))
	if err := h.Service.MarkDeparted(context.Background(), admin.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if err := domain.QueueWeeklyReminders(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyReminders: %v", err)
	}

	if got := sentTo(h.Email, "kim@example.com"); len(got) != 0 {
		t.Errorf("departed owner was sent %d reminders, want none: %+v", len(got), got)
	}
}

// The digest names a child whose back-to-Green date slipped this week, with
// the dates and the reason (CONTEXT.md: Date Slip).
func TestDigestListsChildrenWhoseBackToGreenDateSlippedThisWeek(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := childOf(h, kim, parent, "Launch pricing page")
	h.Checkin(kim, child.ID, domain.HealthYellow, "Behind.", "Add a second engineer.", time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	h.Clock.Advance(8 * day)
	h.CheckinMovingPath(kim, child.ID, domain.HealthYellow, "Still behind.", "Add a second engineer.", time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), "The engineer starts in February.")
	h.Clock.Advance(1 * day)

	if err := domain.QueueWeeklyDigests(context.Background(), weeklyEmailsService(h)); err != nil {
		t.Fatalf("QueueWeeklyDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	if want := "slipped its back-to-Green date from 2026-01-20 to 2026-02-03 (The engineer starts in February.)"; !strings.Contains(body, want) {
		t.Errorf("digest does not say %q:\n%s", want, body)
	}
}

// fortnightlyCheckedIn sets goal to a 14-day cadence and checks it in now, so
// it has no Check-in due before next week's reminder.
func fortnightlyCheckedIn(h *testsupport.Harness, goal domain.Goal) {
	h.T.Helper()
	if _, err := h.Service.SetCadence(context.Background(), goal.Owner.ID, goal.ID, 14); err != nil {
		h.T.Fatalf("SetCadence: %v", err)
	}
	digestCheckinHealth(h, goal.Owner, goal.ID, domain.HealthGreen)
}

// A week's slot queues its reminders, then its digests, and records the slot
// as run: the same slot run again, by a late tick or another copy of the app,
// sends nothing more.
func TestQueueWeeklyEmailsSendsRemindersThenDigestsOncePerSlot(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := h.ActiveGoal(kim, "Launch pricing page", "Buyers can't see prices.")
	fortnightlyCheckedIn(h, parent)
	fortnightlyCheckedIn(h, child)
	h.RequestLink(kim, child, parent, "")
	svc := weeklyEmailsService(h)
	// Monday's slot, 3 days after sam's Goal was activated: its Check-in is
	// due before next week's.
	slot := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	h.Clock.Set(slot)

	if err := svc.QueueWeeklyEmails(context.Background(), slot); err != nil {
		t.Fatalf("QueueWeeklyEmails: %v", err)
	}

	sent := h.Email.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent %d emails, want sam's reminder and pat's digest: %+v", len(sent), sent)
	}
	if sent[0].To != "sam@example.com" || sent[0].Subject != "Your Check-ins this week" {
		t.Errorf("first email went to %s about %q, want sam's reminder", sent[0].To, sent[0].Subject)
	}
	if sent[1].To != "pat@example.com" || !strings.Contains(sent[1].Body, "Launch pricing page") {
		t.Errorf("second email went to %s:\n%s\nwant pat's digest of the link request", sent[1].To, sent[1].Body)
	}

	if err := svc.QueueWeeklyEmails(context.Background(), slot); err != nil {
		t.Fatalf("QueueWeeklyEmails for the same slot again: %v", err)
	}
	if got := len(h.Email.Sent()); got != 2 {
		t.Errorf("sent %d emails after the slot ran twice, want no more than 2", got)
	}
}

// A week's slot that fails part way, recording the slot as run and then
// failing to queue its emails, queues nothing and leaves the slot unrecorded,
// so a later run of it isn't skipped (#630).
func TestQueueWeeklyEmailsFailingPartWayQueuesNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := h.ActiveGoal(kim, "Launch pricing page", "Buyers can't see prices.")
	fortnightlyCheckedIn(h, parent)
	fortnightlyCheckedIn(h, child)
	h.RequestLink(kim, child, parent, "")
	svc := weeklyEmailsService(h)
	slot := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	h.Clock.Set(slot)
	h.FailWrites(testsupport.Insert, "email_outbox")

	err := svc.QueueWeeklyEmails(context.Background(), slot)
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("QueueWeeklyEmails: err = %v, want the injected failure", err)
	}
	if sent := h.Email.Sent(); len(sent) != 0 {
		t.Errorf("sent %d emails from a failed run, want none: %+v", len(sent), sent)
	}
	var runs int
	if err := h.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM scheduled_runs`).Scan(&runs); err != nil {
		t.Fatalf("count scheduled_runs: %v", err)
	}
	if runs != 0 {
		t.Errorf("scheduled_runs holds %d rows after a failed run, want none so the slot runs again", runs)
	}
}
