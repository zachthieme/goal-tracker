package domain_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const nudgeDay = 24 * time.Hour

// recipients lists who the messages went to, in order.
func recipients(msgs []email.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.To)
	}
	return out
}

// Someone other than its Owner and Delegates nudges a Check-in overdue Goal:
// its Owner and each Delegate get an email asking for a Check-in, a Delegate's
// naming the Owner they check in for, and the Nudge is kept with who sent it
// and when.
func TestNudgeOfACheckinOverdueGoalEmailsItsOwnerAndDelegates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Lee")    // Owns the Goal
	dee := h.SignInNamed("dee@example.com", "Dee Park")   // a Delegate
	pat := h.SignInNamed("pat@example.com", "Pat Okafor") // nudges
	goal := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.AddDelegate(sam, dee, goal.ID)
	h.Clock.Advance(10 * nudgeDay)

	n, err := h.Service.Nudge(ctx, pat.ID, goal.ID)
	if err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	if n.GoalID != goal.ID || n.Sender.ID != pat.ID || !n.CreatedAt.Equal(h.Clock.Now()) {
		t.Errorf("Nudge = %+v, want Pat's Nudge of the Goal, now", n)
	}

	sent := h.Email.Sent()
	if got := recipients(sent); !slices.Equal(got, []string{"sam@example.com", "dee@example.com"}) {
		t.Fatalf("emailed %v, want the Owner then the Delegate", got)
	}
	if sent[0].AccountID != sam.ID || sent[1].AccountID != dee.ID {
		t.Errorf("emailed Accounts %d and %d, want the Owner's %d then the Delegate's %d", sent[0].AccountID, sent[1].AccountID, sam.ID, dee.ID)
	}
	for _, m := range sent {
		if m.Subject != "Check-in requested: Silent work" {
			t.Errorf("subject to %s = %q", m.To, m.Subject)
		}
		if !strings.HasPrefix(m.Body, "Pat Okafor (pat@example.com) asked for a Check-in on this Goal.") {
			t.Errorf("body to %s doesn't open with who asked:\n%s", m.To, m.Body)
		}
		if !strings.Contains(m.Body, "/goals/"+strconv.FormatInt(goal.ID, 10)+"/checkin") {
			t.Errorf("body to %s doesn't link to the Check-in:\n%s", m.To, m.Body)
		}
	}
	if want := "Silent work: Check-in overdue, no Check-in since activation 10 days ago (every 7 days)"; !strings.Contains(sent[0].Body, want) {
		t.Errorf("the Owner's body doesn't say %q:\n%s", want, sent[0].Body)
	}
	if want := "Silent work, as Delegate for Sam Lee (sam@example.com): Check-in overdue, no Check-in since activation 10 days ago (every 7 days)"; !strings.Contains(sent[1].Body, want) {
		t.Errorf("the Delegate's body doesn't say %q:\n%s", want, sent[1].Body)
	}

	kept, err := h.Service.Nudges(ctx, goal.ID)
	if err != nil {
		t.Fatalf("Nudges: %v", err)
	}
	if len(kept) != 1 || kept[0].ID != n.ID || kept[0].Sender.ID != pat.ID {
		t.Errorf("Nudges = %+v, want just Pat's", kept)
	}
}

// A Goal is nudged at most once a calendar day in the org's timezone, whoever
// asks: a second Nudge that day is refused, naming who nudged, though a new
// UTC day has begun, and one the next day goes out.
func TestNudgeIsOncePerGoalPerOrgDay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	h := testsupport.New(t)
	svc := domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la), domain.WithEmailDeliveredOnCommit())
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Okafor")
	kim := h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Silent work", "It matters.")

	h.Clock.Set(time.Date(2026, 1, 20, 15, 0, 0, 0, la)) // 23:00 UTC
	if _, err := svc.Nudge(ctx, pat.ID, goal.ID); err != nil {
		t.Fatalf("first Nudge: %v", err)
	}
	h.Clock.Set(time.Date(2026, 1, 20, 20, 0, 0, 0, la)) // 04:00 UTC the next day
	_, err = svc.Nudge(ctx, kim.ID, goal.ID)
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "Pat Okafor already nudged") {
		t.Errorf("second Nudge the same day: err = %v, want ErrValidation naming Pat", err)
	}
	today, err := svc.NudgedToday(ctx, []int64{goal.ID})
	if err != nil {
		t.Fatalf("NudgedToday: %v", err)
	}
	if n, ok := today[goal.ID]; !ok || n.Sender.ID != pat.ID {
		t.Errorf("NudgedToday = %+v, want the Goal nudged by Pat", today)
	}

	h.Clock.Set(time.Date(2026, 1, 21, 9, 0, 0, 0, la))
	if today, _ := svc.NudgedToday(ctx, []int64{goal.ID}); len(today) != 0 {
		t.Errorf("NudgedToday the next day = %+v, want none", today)
	}
	if _, err := svc.Nudge(ctx, kim.ID, goal.ID); err != nil {
		t.Errorf("Nudge the next day: %v", err)
	}
	if kept, _ := svc.Nudges(ctx, goal.ID); len(kept) != 2 {
		t.Errorf("kept %d Nudges, want 2", len(kept))
	}
	if n := len(h.Email.Sent()); n != 2 {
		t.Errorf("sent %d emails, want one per Nudge", n)
	}
}

// Only someone who can't check in nudges: the Owner and a Delegate are
// refused, and nothing is sent or kept.
func TestTheOwnerAndDelegatesCantNudge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.AddDelegate(sam, dee, goal.ID)
	h.Clock.Advance(10 * nudgeDay)

	for _, who := range []domain.Account{sam, dee} {
		if _, err := h.Service.Nudge(ctx, who.ID, goal.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s nudging: err = %v, want ErrNotAuthorized", who.Email, err)
		}
	}
	if kept, _ := h.Service.Nudges(ctx, goal.ID); len(kept) != 0 || len(h.Email.Sent()) != 0 {
		t.Errorf("kept %d Nudges and sent %d emails, want none", len(kept), len(h.Email.Sent()))
	}
}

// A Goal with nothing its Owner answers in a Check-in, neither Check-in
// overdue, past its Path to Green, with a Milestone overdue nor in a Schedule
// conflict, can't be nudged; one only past its Path to Green can, its email
// saying since when and linking to the plain Check-in.
func TestOnlyAGoalWithSomethingToAnswerCanBeNudged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	h.Clock.Advance(5 * nudgeDay)

	_, err := h.Service.Nudge(ctx, pat.ID, fresh.ID)
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "only a Goal that is Check-in overdue, past its Path to Green, has a Milestone overdue or has a Schedule conflict can be nudged") {
		t.Errorf("nudging a fresh Goal: err = %v, want ErrValidation naming what can be nudged", err)
	}
	if _, err := h.Service.Nudge(ctx, pat.ID, stalled.ID); err != nil {
		t.Fatalf("nudging a Path-overdue Goal: %v", err)
	}
	sent := h.Email.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Body, "Stalled recovery: Path to Green overdue since 2026-01-05") {
		t.Fatalf("sent %+v, want one email saying the Path to Green is overdue since 2026-01-05", sent)
	}
	if want := "/goals/" + strconv.FormatInt(stalled.ID, 10) + "/checkin\n"; !strings.HasSuffix(sent[0].Body, want) {
		t.Errorf("email doesn't end with the plain Check-in link %q:\n%s", want, sent[0].Body)
	}
}

// An Ownerless Goal's Nudge goes to its Delegates alone, a Departed one left
// out; with none left it's refused, as only an Admin's reassignment helps.
func TestNudgeOfAnOwnerlessGoalGoesToItsPresentDelegates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	gus := h.SignIn("gus@example.com")
	pat := h.SignIn("pat@example.com")
	delegated := h.ActiveGoal(sam, "Delegated work", "It matters.")
	h.AddDelegate(sam, dee, delegated.ID)
	h.AddDelegate(sam, gus, delegated.ID)
	alone := h.ActiveGoal(sam, "Lonely work", "It matters.")
	h.AddDelegate(sam, gus, alone.ID)
	for _, gone := range []domain.Account{sam, gus} {
		if err := h.Service.MarkDeparted(ctx, ada.ID, gone.ID); err != nil {
			t.Fatalf("MarkDeparted %s: %v", gone.Email, err)
		}
	}
	h.Clock.Advance(10 * nudgeDay)

	if _, err := h.Service.Nudge(ctx, pat.ID, delegated.ID); err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	if got := recipients(h.Email.Sent()); !slices.Equal(got, []string{"dee@example.com"}) {
		t.Errorf("emailed %v, want only the present Delegate", got)
	}
	_, err := h.Service.Nudge(ctx, pat.ID, alone.ID)
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "nobody can check in on this Goal; it needs an Admin to reassign it") {
		t.Errorf("nudging a Goal nobody can check in on: err = %v, want ErrValidation saying it needs an Admin", err)
	}
}

// A Departed Delegate of a Goal with a present Owner isn't emailed.
func TestNudgeLeavesOutADepartedDelegate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	gus := h.SignIn("gus@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.AddDelegate(sam, gus, goal.ID)
	if err := h.Service.MarkDeparted(ctx, ada.ID, gus.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * nudgeDay)

	if _, err := h.Service.Nudge(ctx, pat.ID, goal.ID); err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	if got := recipients(h.Email.Sent()); !slices.Equal(got, []string{"sam@example.com"}) {
		t.Errorf("emailed %v, want only the Owner", got)
	}
}

// A Nudge whose email can't be sent stands with no error: its email waits in
// the outbox, unsent, and goes out once mail is back, as a comment alert does.
func TestNudgeStandsAndItsEmailIsRetriedWhenMailIsDown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * nudgeDay)
	down := domain.NewService(h.DB, h.Clock, failingSender{}, nil, domain.WithEmailDeliveredOnCommit())

	if _, err := down.Nudge(ctx, pat.ID, goal.ID); err != nil {
		t.Fatalf("Nudge: %v, want it recorded though its email could not be sent", err)
	}
	if kept, _ := h.Service.Nudges(ctx, goal.ID); len(kept) != 1 {
		t.Errorf("kept %d Nudges, want 1", len(kept))
	}
	if sent := h.Email.Sent(); len(sent) != 0 {
		t.Fatalf("sent %+v while mail was down, want nothing", sent)
	}

	h.Clock.Advance(time.Minute)
	h.DeliverEmail()
	sent := h.Email.Sent()
	if len(sent) != 1 || sent[0].To != "sam@example.com" || sent[0].AccountID != sam.ID || sent[0].Subject != "Check-in requested: Silent work" {
		t.Errorf("sent %+v once mail was back, want the Nudge's email to sam, for their Account", sent)
	}
}

// A Green Goal past a Milestone's date can be nudged, as its Owner answers it
// in a Check-in (CONTEXT.md: Nudge), its email naming the Milestone and its
// date.
func TestAGoalMarkedMilestoneOverdueCanBeNudged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal, beta := h.GreenGoalWithOverdueMilestone(sam, "Ship search")

	if _, err := h.Service.Nudge(context.Background(), pat.ID, goal.ID); err != nil {
		t.Fatalf("nudging a Goal marked Milestone overdue: %v", err)
	}
	sent := h.Email.Sent()
	want := `Ship search: Milestone "Beta" overdue since ` + beta.TargetDate.Format("2006-01-02")
	if len(sent) != 1 || !strings.Contains(sent[0].Body, want) {
		t.Errorf("sent %+v, want one email saying %q", sent, want)
	}
}

// nudgeConflict arranges a Schedule conflict: kim's Late piece, due July 1,
// contributes to sam's Launch, due June 1, the link kim asked for and sam
// accepted. pat is someone else.
func nudgeConflict(t *testing.T) (h *testsupport.Harness, pat domain.Account, late, launch domain.Goal) {
	t.Helper()
	h = testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Lee")
	kim := h.SignInNamed("kim@example.com", "Kim Wu")
	pat = h.SignInNamed("pat@example.com", "Pat Okafor")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch = h.ActiveGoalDue(sam, "Launch", june)
	late = h.ActiveGoalDue(kim, "Late piece", june.AddDate(0, 1, 0))
	link := h.RequestLink(kim, late, launch, "")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	return h, pat, late, launch
}

// A fresh Active Goal in a Schedule conflict can be nudged, as its Owner
// answers it in a Check-in (CONTEXT.md: Nudge): its email names the conflict
// in the Goal page banner's words and opens the Check-in's dates.
func TestAGoalInAScheduleConflictCanBeNudged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, pat, late, _ := nudgeConflict(t)

	if _, err := h.Service.Nudge(ctx, pat.ID, late.ID); err != nil {
		t.Fatalf("nudging a Goal in a Schedule conflict: %v", err)
	}
	if kept, _ := h.Service.Nudges(ctx, late.ID); len(kept) != 1 {
		t.Errorf("kept %d Nudges, want 1", len(kept))
	}
	sent := h.Email.Sent()
	if got := recipients(sent); !slices.Equal(got, []string{"kim@example.com"}) {
		t.Fatalf("emailed %v, want the child's Owner", got)
	}
	body := sent[0].Body
	for _, want := range []string{
		"Late piece: Schedule conflict\n",
		"Schedule conflict: Late piece (due 2026-07-01) contributes to Launch (due 2026-06-01)\n",
		"/goals/" + strconv.FormatInt(late.ID, 10) + "/checkin?open=dates\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("email doesn't hold %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Path to Green") {
		t.Errorf("email names a Path to Green the Goal isn't past:\n%s", body)
	}
}

// Either Goal of a Schedule conflict can be nudged, as either Owner can
// resolve it (CONTEXT.md: Schedule conflict).
func TestTheParentInAScheduleConflictCanBeNudged(t *testing.T) {
	t.Parallel()

	h, pat, _, launch := nudgeConflict(t)

	if _, err := h.Service.Nudge(context.Background(), pat.ID, launch.ID); err != nil {
		t.Fatalf("nudging the parent in a Schedule conflict: %v", err)
	}
	if got := recipients(h.Email.Sent()); !slices.Equal(got, []string{"sam@example.com"}) {
		t.Errorf("emailed %v, want the parent's Owner", got)
	}
}

// A Proposed Goal in a Schedule conflict can't be nudged, as only an Active
// Goal takes an ordinary Check-in.
func TestAProposedGoalInAScheduleConflictCantBeNudged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	pat := h.SignIn("pat@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.ActiveGoalDue(sam, "Launch", june)
	proposed := h.CreateGoal(kim, "Late plan", "It matters.")
	if _, err := h.Service.MarkGoalDated(ctx, proposed.Owner.ID, proposed.ID, june.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	link := h.RequestLink(kim, proposed, launch, "")
	if _, err := h.Service.AcceptLink(ctx, link.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	if signals, err := h.Service.GoalSignals(ctx, proposed.ID); err != nil || len(signals.ScheduleConflicts) != 1 {
		t.Fatalf("GoalSignals = %+v, %v; want the Proposed Goal in one Schedule conflict", signals, err)
	}

	if _, err := h.Service.Nudge(ctx, pat.ID, proposed.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("nudging a Proposed Goal in a Schedule conflict: err = %v, want ErrValidation", err)
	}
	if kept, _ := h.Service.Nudges(ctx, proposed.ID); len(kept) != 0 || len(h.Email.Sent()) != 0 {
		t.Errorf("kept %d Nudges and sent %d emails, want none", len(kept), len(h.Email.Sent()))
	}
}
