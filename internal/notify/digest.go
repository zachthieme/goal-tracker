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

// reminderItem is one Goal on a person's reminder. asDelegate is set when the
// person is reminded as the Goal's Delegate rather than its Owner.
type reminderItem struct {
	goal       domain.Goal
	freshness  domain.Freshness
	asDelegate bool
}

// SendReminders emails each Owner and Delegate the Active Goals whose Check-in
// is due — it would go Stale before next week's reminder — or that are Stale
// already, each linking to its pre-filled Check-in. Someone with nothing due
// gets no email, and nobody who has left the org is emailed.
func (n *Notifier) SendReminders(ctx context.Context) error {
	goals, err := n.svc.ListGoals(ctx)
	if err != nil {
		return err
	}
	var out outbox[reminderItem]
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
		out.add(g.Owner, reminderItem{goal: g, freshness: f})
		delegates, err := n.svc.ListDelegates(ctx, g.ID)
		if err != nil {
			return err
		}
		for _, d := range delegates {
			out.add(d, reminderItem{goal: g, freshness: f, asDelegate: true})
		}
	}
	return out.send(ctx, n.sender, "Your Check-ins this week", n.reminderBody)
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
		title := it.goal.Title
		if it.asDelegate {
			title += " (as Delegate for " + it.goal.Owner.Email + ")"
		}
		state := "Check-in due"
		if it.freshness.Stale {
			state = "Stale"
		}
		fmt.Fprintf(&b, "- %s: %s, %d days since its last Check-in (cadence: %d days)\n  %s\n",
			title, state, it.freshness.DaysSince, it.freshness.CadenceDays, n.checkinURL(it.goal.ID))
	}
	return b.String()
}

// checkinURL links to a Goal's Check-in form, which the Goal page pre-fills
// from its latest Check-in.
func (n *Notifier) checkinURL(goalID int64) string {
	return fmt.Sprintf("%s/goals/%d#checkin-form", n.baseURL, goalID)
}

// outbox collects the items bound for each recipient, in the order recipients
// were first seen, so each person gets one email however many items they have.
// It never collects for someone who has left the org.
type outbox[T any] struct {
	order []string
	items map[string][]T
}

func (o *outbox[T]) add(to domain.Account, item T) {
	if to.Departed {
		return
	}
	if o.items == nil {
		o.items = map[string][]T{}
	}
	if _, ok := o.items[to.Email]; !ok {
		o.order = append(o.order, to.Email)
	}
	o.items[to.Email] = append(o.items[to.Email], item)
}

// send emails each recipient one message whose body renders their items. A
// failed send doesn't stop the rest; every failure is returned together.
func (o *outbox[T]) send(ctx context.Context, sender email.Sender, subject string, body func([]T) string) error {
	var errs []error
	for _, addr := range o.order {
		if err := sender.Send(ctx, email.Message{
			To:      addr,
			Subject: subject,
			Body:    body(o.items[addr]),
		}); err != nil {
			errs = append(errs, fmt.Errorf("send %q to %s: %w", subject, addr, err))
		}
	}
	return errors.Join(errs...)
}
