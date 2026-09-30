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

// SendWeekly sends the week's emails: the Check-in reminders, then the parent
// digests. A failure in one doesn't stop the other; both are returned.
func (n *Notifier) SendWeekly(ctx context.Context) error {
	return errors.Join(n.SendReminders(ctx), n.SendDigests(ctx))
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

// dateFormat is how calendar dates read in emails.
const dateFormat = "2006-01-02"

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
		fmt.Fprintf(&b, "- %s: %s, %d days without a Check-in (cadence: %d days)\n  %s\n",
			title, state, it.freshness.DaysSince, it.freshness.CadenceDays, n.checkinURL(it.goal.ID))
	}
	return b.String()
}

// goalURL links to a Goal's page.
func (n *Notifier) goalURL(goalID int64) string {
	return fmt.Sprintf("%s/goals/%d", n.baseURL, goalID)
}

// checkinURL links to a Goal's Check-in form, which the Goal page pre-fills
// from its latest Check-in.
func (n *Notifier) checkinURL(goalID int64) string {
	return fmt.Sprintf("%s/goals/%d#checkin-form", n.baseURL, goalID)
}

// digestEntry is one item on a parent Owner's digest: a link request waiting
// on them, or a problem with a Goal that contributes to one of theirs.
type digestEntry struct {
	pendingLink *domain.Link
	problem     *childProblem
}

// childProblem is a Goal contributing to one of the recipient's Goals, and what
// went wrong with it.
type childProblem struct {
	parent, child domain.Goal
	reasons       []string
}

// SendDigests emails each parent Owner a digest of what needs their attention:
// the link requests waiting on them, and problems among the Goals that
// contribute to theirs. Someone with nothing to report gets no email.
func (n *Notifier) SendDigests(ctx context.Context) error {
	goals, err := n.svc.ListGoals(ctx)
	if err != nil {
		return err
	}
	weekStart := n.svc.Now().In(n.loc).AddDate(0, 0, -daysPerWeek)
	var out outbox[digestEntry]
	seen := map[int64]bool{}
	for _, g := range goals {
		if seen[g.Owner.ID] {
			continue
		}
		seen[g.Owner.ID] = true
		links, err := n.svc.PendingLinkRequests(ctx, g.Owner.ID)
		if err != nil {
			return err
		}
		for i := range links {
			out.add(g.Owner, digestEntry{pendingLink: &links[i]})
		}
	}
	for _, parent := range goals {
		children, err := n.svc.ChildrenOf(ctx, parent.ID)
		if err != nil {
			return err
		}
		for _, child := range children {
			reasons, err := n.childProblems(ctx, child, weekStart)
			if err != nil {
				return err
			}
			if len(reasons) > 0 {
				out.add(parent.Owner, digestEntry{problem: &childProblem{parent: parent, child: child, reasons: reasons}})
			}
		}
	}
	return out.send(ctx, n.sender, "Your weekly digest", n.digestBody)
}

// childProblems says what's wrong with child, a Goal that contributes to one
// of the recipient's. Some are changes since weekStart, a week ago: its Health
// got worse, to Yellow or to Red; it recorded a Date Slip; it went Stale. Others
// hold for as long as they last, so the digest names them every week: the child
// is Ownerless (no record says when its Owner left), or one of its parents is
// On Hold or Cancelled. A Done or Cancelled child has nothing left at risk.
func (n *Notifier) childProblems(ctx context.Context, child domain.Goal, weekStart time.Time) ([]string, error) {
	if child.Lifecycle == domain.LifecycleDone || child.Lifecycle == domain.LifecycleCancelled {
		return nil, nil
	}
	var reasons []string
	if child.Lifecycle == domain.LifecycleActive {
		checkins, err := n.svc.ListCheckins(ctx, child.ID)
		if err != nil {
			return nil, err
		}
		if now, before := healthAt(checkins, time.Time{}), healthAt(checkins, weekStart); worsened(before, now) {
			reasons = append(reasons, "went "+now)
		}
	}
	slips, err := n.svc.ListDateSlips(ctx, child.ID)
	if err != nil {
		return nil, err
	}
	for _, sl := range slips {
		if !sl.CreatedAt.After(weekStart) {
			continue
		}
		what := "its delivery date"
		if sl.MilestoneID != 0 {
			what = "a Milestone"
		}
		reasons = append(reasons, fmt.Sprintf("slipped %s from %s to %s (%s)",
			what, sl.OldDate.Format(dateFormat), sl.NewDate.Format(dateFormat), sl.Reason))
	}
	f, err := n.svc.Freshness(ctx, child.ID)
	if err != nil {
		return nil, err
	}
	if wentStaleThisWeek(f) {
		reasons = append(reasons, fmt.Sprintf("went Stale, %d days without a Check-in", f.DaysSince))
	}
	if child.Ownerless {
		reasons = append(reasons, "is Ownerless: "+child.Owner.Email+" has left the org")
	}
	signals, err := n.svc.GoalSignals(ctx, child.ID)
	if err != nil {
		return nil, err
	}
	for _, hp := range signals.HaltedParents {
		reasons = append(reasons, fmt.Sprintf("contributes to %s, which is %s", hp.Parent.Title, hp.Parent.Lifecycle))
	}
	return reasons, nil
}

// wentStaleThisWeek reports whether a Goal is Stale now but wasn't a week ago:
// it has been Stale for no more than a week's days (CONTEXT.md: Stale).
func wentStaleThisWeek(f domain.Freshness) bool {
	return f.Stale && f.DaysSince-daysPerWeek <= f.CadenceDays
}

// healthAt is the Health a Goal's Check-ins (newest first) had set as of t, or
// its current Health when t is the zero time. It is "" when none had set one.
func healthAt(checkins []domain.Checkin, t time.Time) string {
	for _, c := range checkins {
		if c.Health == "" || (!t.IsZero() && c.CreatedAt.After(t)) {
			continue
		}
		return c.Health
	}
	return ""
}

// worsened reports whether Health went from before to a worse now that is
// Yellow or Red: Green to Yellow, Yellow to Red, or no Health yet to either.
func worsened(before, now string) bool {
	rank := map[string]int{domain.HealthGreen: 1, domain.HealthYellow: 2, domain.HealthRed: 3}
	return (now == domain.HealthYellow || now == domain.HealthRed) && rank[now] > rank[before]
}

func (n *Notifier) digestBody(entries []digestEntry) string {
	var b strings.Builder
	var pending []*domain.Link
	var problems []*childProblem
	for _, e := range entries {
		if e.pendingLink != nil {
			pending = append(pending, e.pendingLink)
		}
		if e.problem != nil {
			problems = append(problems, e.problem)
		}
	}
	if len(pending) > 0 {
		fmt.Fprintf(&b, "Link requests waiting on you (%s/links):\n\n", n.baseURL)
		for _, l := range pending {
			fmt.Fprintf(&b, "- %s (%s) asks to contribute to %s\n", l.Child.Title, l.Child.Owner.Email, l.Parent.Title)
		}
		b.WriteString("\n")
	}
	if len(problems) > 0 {
		b.WriteString("Goals contributing to yours that need your attention:\n\n")
		for _, p := range problems {
			fmt.Fprintf(&b, "- %s (%s), contributing to %s: %s\n  %s\n",
				p.child.Title, p.child.Owner.Email, p.parent.Title, strings.Join(p.reasons, "; "), n.goalURL(p.child.ID))
		}
	}
	return b.String()
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
