package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Nudge is a one-off request from someone else to a Goal's Owner and
// Delegates to check in on it with something its Owner answers in a Check-in:
// Stale, past its Path to Green, a Milestone overdue, or a Schedule conflict
// (CONTEXT.md: Nudge). Unlike the weekly reminder, someone sends it.
type Nudge struct {
	ID        int64
	GoalID    int64
	Sender    Account
	CreatedAt time.Time
}

// Nudge asks the Owner and Delegates of the Goal goalID to check in on it,
// emailing each who hasn't left the org; actorID is who asks. Only a Goal
// that is Stale, past its Path to Green, or marked Milestone overdue (see
// Freshness.MilestoneOverdue), or an Active Goal in a Schedule conflict as the
// child or the parent, can be nudged, and only by someone who can't check in
// on it themselves. A Goal is nudged at most once a
// calendar day in the org's timezone, whoever asks.
//
// The Nudge is recorded before any email goes out, so a rollback never leaves
// one sent. When an email can't be sent the Nudge stands, and Nudge returns it
// with an error saying so.
func (s *Service) Nudge(ctx context.Context, actorID, goalID int64) (Nudge, error) {
	goal, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return Nudge{}, err
	}
	sender, err := s.Account(ctx, actorID)
	if err != nil {
		return Nudge{}, err
	}
	delegates, err := s.ListDelegates(ctx, goalID)
	if err != nil {
		return Nudge{}, err
	}
	if goal.Owner.ID == actorID || containsAccount(delegates, actorID) {
		return Nudge{}, fmt.Errorf("%w: you can check in on this Goal yourself, so there's no one to nudge", ErrNotAuthorized)
	}
	fresh, err := s.Freshness(ctx, goalID)
	if err != nil {
		return Nudge{}, err
	}
	signals, err := s.GoalSignals(ctx, goalID)
	if err != nil {
		return Nudge{}, err
	}
	// Only an Active Goal takes the ordinary Check-in a Schedule conflict asks
	// for.
	conflicted := goal.Lifecycle == LifecycleActive && len(signals.ScheduleConflicts) > 0
	if !fresh.Stale && !fresh.PathToGreenOverdue && !fresh.MilestoneOverdue && !conflicted {
		return Nudge{}, fmt.Errorf("%w: only a Goal that is Stale, past its Path to Green, has a Milestone overdue or has a Schedule conflict can be nudged", ErrValidation)
	}
	var conflicts []ScheduleConflict
	if conflicted {
		conflicts = signals.ScheduleConflicts
	}
	to := nudgeRecipients(goal.Owner, delegates)
	if len(to) == 0 {
		return Nudge{}, fmt.Errorf("%w: nobody can check in on this Goal; it needs an Admin to reassign it", ErrValidation)
	}

	now := s.clock.Now()
	today := orgDate(now, s.loc).Format(dateFormat)
	var n Nudge
	err = s.WithinTx(ctx, func(tx *Service) error {
		earlier, err := tx.queries.ListNudgesOn(ctx, today)
		if err != nil {
			return fmt.Errorf("list today's nudges: %w", err)
		}
		for _, r := range earlier {
			if r.Nudge.GoalID == goalID {
				return fmt.Errorf("%w: %s already nudged this Goal today", ErrValidation, accountFromRow(r.Account).Label())
			}
		}
		row, err := tx.queries.CreateNudge(ctx, db.CreateNudgeParams{
			GoalID:    goalID,
			SentBy:    actorID,
			NudgedOn:  today,
			CreatedAt: now.Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("record nudge: %w", err)
		}
		n = nudgeFromRow(row, sender)
		return nil
	})
	if err != nil {
		return Nudge{}, err
	}

	var unsent []string
	for _, r := range to {
		msg := email.Message{
			To:      r.Email,
			Subject: "Check-in requested: " + goal.Title,
			Body:    s.nudgeBody(sender, goal, fresh, conflicts, r.ID != goal.Owner.ID),
		}
		if err := s.email.Send(ctx, msg); err != nil {
			unsent = append(unsent, r.Email)
		}
	}
	if len(unsent) > 0 {
		return n, fmt.Errorf("%w: the Nudge was recorded, but the email to %s couldn't be sent", ErrNudgeNotSent, strings.Join(unsent, ", "))
	}
	return n, nil
}

// ErrNudgeNotSent is a Nudge that was recorded but whose email couldn't go
// out to everyone it was for.
var ErrNudgeNotSent = errors.New("nudge email not sent")

// NudgedToday returns, of the Goals goalIDs, those nudged today in the org's
// calendar, each with its Nudge, so a page can say who already asked.
func (s *Service) NudgedToday(ctx context.Context, goalIDs []int64) (map[int64]Nudge, error) {
	rows, err := s.queries.ListNudgesOn(ctx, orgDate(s.clock.Now(), s.loc).Format(dateFormat))
	if err != nil {
		return nil, fmt.Errorf("list today's nudges: %w", err)
	}
	wanted := make(map[int64]bool, len(goalIDs))
	for _, id := range goalIDs {
		wanted[id] = true
	}
	out := map[int64]Nudge{}
	for _, r := range rows {
		if wanted[r.Nudge.GoalID] {
			out[r.Nudge.GoalID] = nudgeFromRow(r.Nudge, accountFromRow(r.Account))
		}
	}
	return out, nil
}

// Nudges returns every Nudge of the Goal goalID, oldest first, for its
// History.
func (s *Service) Nudges(ctx context.Context, goalID int64) ([]Nudge, error) {
	rows, err := s.queries.ListNudgesForGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list nudges: %w", err)
	}
	out := make([]Nudge, 0, len(rows))
	for _, r := range rows {
		out = append(out, nudgeFromRow(r.Nudge, accountFromRow(r.Account)))
	}
	return out, nil
}

// nudgeRecipients are who a Nudge of a Goal goes to: its Owner, then its
// Delegates, leaving out anyone who has left the org, so an Ownerless Goal's
// go to its Delegates alone.
func nudgeRecipients(owner Account, delegates []Account) []Account {
	var out []Account
	for _, a := range append([]Account{owner}, delegates...) {
		if !a.Departed {
			out = append(out, a)
		}
	}
	return out
}

// nudgeBody is a Nudge's email: who asked, the Goal's line in the weekly
// reminder's words (a Delegate's naming the Owner they check in for) with the
// first of Stale, Path to Green overdue and Milestone overdue that holds, or
// else Schedule conflict, a line for each of the Goal's Schedule conflicts in
// the Goal page banner's words, and the link to check in, opening its dates
// when there is a conflict. With no hover in an email, people read Name
// (email) at their first mention.
func (s *Service) nudgeBody(sender Account, goal Goal, f Freshness, conflicts []ScheduleConflict, asDelegate bool) string {
	var people Mentions
	var b strings.Builder
	fmt.Fprintf(&b, "%s asked for a Check-in on this Goal.\n\n", people.Of(sender))
	title := goal.Title
	if asDelegate {
		title += ", as Delegate for " + people.Of(goal.Owner)
	}
	switch {
	case f.Stale:
		fmt.Fprintf(&b, "%s: Stale, %d days without a Check-in (cadence: %d days)\n", title, f.DaysSince, f.CadenceDays)
	case f.PathToGreenOverdue:
		fmt.Fprintf(&b, "%s: Path to Green overdue since %s\n", title, f.PathTargetDate.Format(dateFormat))
	case f.MilestoneOverdue:
		fmt.Fprintf(&b, "%s: Milestone %q overdue since %s\n", title, f.OverdueMilestone, f.OverdueMilestoneDate.Format(dateFormat))
	default:
		fmt.Fprintf(&b, "%s: Schedule conflict\n", title)
	}
	for _, c := range conflicts {
		fmt.Fprintf(&b, "Schedule conflict: %s (due %s) contributes to %s (due %s)\n",
			c.Child.Title, c.Child.DeliveryDate.Format(dateFormat), c.Parent.Title, c.Parent.DeliveryDate.Format(dateFormat))
	}
	checkin := fmt.Sprintf("%s/goals/%d/checkin", s.baseURL, goal.ID)
	if len(conflicts) > 0 {
		checkin += "?open=dates"
	}
	fmt.Fprintln(&b, checkin)
	return b.String()
}

// containsAccount reports whether id is one of accounts.
func containsAccount(accounts []Account, id int64) bool {
	for _, a := range accounts {
		if a.ID == id {
			return true
		}
	}
	return false
}

func nudgeFromRow(n db.Nudge, sender Account) Nudge {
	createdAt, _ := time.Parse(timeFormat, n.CreatedAt)
	return Nudge{ID: n.ID, GoalID: n.GoalID, Sender: sender, CreatedAt: createdAt}
}
