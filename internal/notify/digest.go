// Package notify sends the weekly emails: each Owner's and Delegate's Check-in
// reminder, and each parent Owner's digest of problems among the Goals that
// contribute to theirs. It reads everything through the domain Service and
// sends through the injected email sender, so tests drive it with the fake
// clock and assert on the recording sender.
package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Notifier composes and sends the weekly emails. Construct it with New.
type Notifier struct {
	svc    *domain.Service
	sender email.Sender
	// baseURL is where the web app is reached, so emails can link into it.
	baseURL string
	// loc is the org's timezone, the calendar a week is counted in.
	loc *time.Location
}

// New returns a Notifier reading from svc and sending through sender. baseURL
// is the web app's address (e.g. http://localhost:8080) that links in emails
// point at; loc is the org's timezone.
func New(svc *domain.Service, sender email.Sender, baseURL string, loc *time.Location) *Notifier {
	return &Notifier{svc: svc, sender: sender, baseURL: strings.TrimRight(baseURL, "/"), loc: loc}
}

// reminderItem is one Goal on a person's reminder.
type reminderItem struct {
	goal      domain.Goal
	freshness domain.Freshness
}

// SendReminders emails each Owner the Goals whose Check-in is due or that are
// Stale, each linking to its pre-filled Check-in.
func (n *Notifier) SendReminders(ctx context.Context) error {
	goals, err := n.svc.ListGoals(ctx)
	if err != nil {
		return err
	}
	byEmail := map[string][]reminderItem{}
	var order []string
	for _, g := range goals {
		if g.Lifecycle != domain.LifecycleActive {
			continue
		}
		f, err := n.svc.Freshness(ctx, g.ID)
		if err != nil {
			return err
		}
		if !dueBeforeNextReminder(f) {
			continue
		}
		addr := g.Owner.Email
		if _, ok := byEmail[addr]; !ok {
			order = append(order, addr)
		}
		byEmail[addr] = append(byEmail[addr], reminderItem{goal: g, freshness: f})
	}
	var errs []error
	for _, addr := range order {
		if err := n.sender.Send(ctx, email.Message{
			To:      addr,
			Subject: "Your Check-ins this week",
			Body:    n.reminderBody(byEmail[addr]),
		}); err != nil {
			errs = append(errs, fmt.Errorf("send reminder to %s: %w", addr, err))
		}
	}
	return errors.Join(errs...)
}

// daysPerWeek is how far apart the weekly emails are, in days of the org's
// calendar.
const daysPerWeek = 7

// dueBeforeNextReminder reports whether a Goal's Check-in is due: it is Stale
// already, or it would go Stale before next week's reminder if nobody checked
// in (CONTEXT.md: Stale).
func dueBeforeNextReminder(f domain.Freshness) bool {
	return f.Stale || f.DaysSince+daysPerWeek > f.CadenceDays
}

func (n *Notifier) reminderBody(items []reminderItem) string {
	var b strings.Builder
	b.WriteString("These Goals need a Check-in:\n\n")
	for _, it := range items {
		state := "Check-in due"
		if it.freshness.Stale {
			state = "Stale"
		}
		fmt.Fprintf(&b, "- %s: %s, %d days since its last Check-in (cadence: %d days)\n  %s\n",
			it.goal.Title, state, it.freshness.DaysSince, it.freshness.CadenceDays, n.checkinURL(it.goal.ID))
	}
	return b.String()
}

// checkinURL links to a Goal's Check-in form, which the Goal page pre-fills
// from its latest Check-in.
func (n *Notifier) checkinURL(goalID int64) string {
	return fmt.Sprintf("%s/goals/%d#checkin-form", n.baseURL, goalID)
}
