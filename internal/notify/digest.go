// Package notify sends the weekly emails: each Owner's and Delegate's reminder
// of their Check-ins due and the open Action Items they own, and each parent
// Owner's digest of problems among the Goals that contribute to theirs. It reads
// everything through the domain Service and queues its emails in the
// Service's email outbox, which sends them once queued and tries again while a
// send fails, so tests drive it with the fake clock and assert on the
// recording sender behind the outbox.
package notify

import (
	"context"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Notifier gathers the weekly emails' facts, has the Service's EmailComposer
// write them, and queues them. Construct it with New.
type Notifier struct {
	svc *domain.Service
}

// New returns a Notifier reading from svc, composing with its EmailComposer,
// whose links point where svc was told the web app is reached, and queueing in
// its email outbox.
func New(svc *domain.Service) *Notifier {
	return &Notifier{svc: svc}
}

// WeeklyJob is the scheduled job the weekly emails' runs are recorded under
// (Service.RecordScheduledRun).
const WeeklyJob = "weekly emails"

// SendWeekly sends the week's emails for slot, the time they were due: it
// composes the reminders and the parent digests, then, in one transaction,
// records slot as the weekly emails' run and queues them all in the email
// outbox. A slot already recorded, by an earlier run or another process,
// queues nothing, so a slot's emails are queued once, and a run that fails
// partway queues none and leaves the slot to run again.
func (n *Notifier) SendWeekly(ctx context.Context, slot time.Time) error {
	reminders, err := n.reminders(ctx)
	if err != nil {
		return err
	}
	digests, err := n.digests(ctx)
	if err != nil {
		return err
	}
	return n.svc.WithinTx(ctx, func(tx *domain.Service) error {
		recorded, err := tx.RecordScheduledRun(ctx, WeeklyJob, slot)
		if err != nil || !recorded {
			return err
		}
		return tx.QueueEmails(ctx, append(reminders, digests...))
	})
}

// SendReminders queues an email to each Owner and Delegate the Active Goals whose Check-in
// is due — it would be overdue before next week's reminder — or that are
// Check-in overdue already, each linking to its Check-in page, which shows the
// last Check-in's answers as ghost text. It also lists every open Action Item
// the person owns, overdue first, so the owner of one is emailed even with no
// Check-in due. Someone with no Check-in due and no open Action Item gets no
// email, and nobody who has left the company is emailed. It queues them now,
// outside the schedule; SendWeekly sends them for a week's slot.
func (n *Notifier) SendReminders(ctx context.Context) error {
	msgs, err := n.reminders(ctx)
	if err != nil {
		return err
	}
	return n.svc.QueueEmails(ctx, msgs)
}

// reminders composes the reminders SendReminders describes. It reads the
// Freshness and the Delegates of every Active Goal at once, in the same few
// queries however many Goals there are.
func (n *Notifier) reminders(ctx context.Context) ([]email.Message, error) {
	goals, err := n.svc.ListGoals(ctx)
	if err != nil {
		return nil, err
	}
	var active []domain.Goal
	var ids []int64
	for _, g := range goals {
		if g.Lifecycle == domain.LifecycleActive {
			active = append(active, g)
			ids = append(ids, g.ID)
		}
	}
	freshness, err := n.svc.FreshnessByGoal(ctx, active)
	if err != nil {
		return nil, err
	}
	delegates, err := n.svc.ListDelegatesForGoals(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []domain.ReminderItem
	for _, g := range active {
		f := freshness[g.ID]
		if !f.DueBeforeNextReminder() {
			continue
		}
		out = append(out, domain.ReminderItem{To: g.Owner, Goal: g, Freshness: f})
		for _, d := range delegates[g.ID] {
			out = append(out, domain.ReminderItem{To: d, Goal: g, Freshness: f, AsDelegate: true})
		}
	}
	items, err := n.svc.AllOpenActionItems(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		out = append(out, domain.ReminderItem{To: items[i].Item.Owner, ActionItem: &items[i]})
	}
	return n.svc.EmailComposer().Reminders(out), nil
}

// SendDigests queues an email to each parent Owner, a digest of what needs
// their attention: the link requests waiting on them, and problems among the
// Goals that contribute to theirs. Someone with nothing to report gets no
// email. It queues them now, outside the schedule; SendWeekly sends them for a
// week's slot.
func (n *Notifier) SendDigests(ctx context.Context) error {
	msgs, err := n.digests(ctx)
	if err != nil {
		return err
	}
	return n.svc.QueueEmails(ctx, msgs)
}

// digests composes the digests SendDigests describes, one email for each the
// domain reads (Service.WeeklyDigests).
func (n *Notifier) digests(ctx context.Context) ([]email.Message, error) {
	digests, err := n.svc.WeeklyDigests(ctx)
	if err != nil {
		return nil, err
	}
	return n.svc.EmailComposer().WeeklyDigests(digests), nil
}
