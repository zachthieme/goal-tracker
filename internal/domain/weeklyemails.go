package domain

import (
	"context"
	"time"

	"github.com/zachthieme/goal-tracker/internal/email"
)

// WeeklyEmailsJob is the scheduled job the weekly emails' runs are recorded
// under (Service.RecordScheduledRun).
const WeeklyEmailsJob = "weekly emails"

// QueueWeeklyEmails sends the week's emails for slot, the time they were due:
// it composes the reminders and the parent digests, then, in one transaction,
// records slot as the weekly emails' run and queues them all in the email
// outbox. A slot already recorded, by an earlier run or another process,
// queues nothing, so a slot's emails are queued once, and a run that fails
// partway queues none and leaves the slot to run again.
func (s *Service) QueueWeeklyEmails(ctx context.Context, slot time.Time) error {
	reminders, err := s.weeklyReminders(ctx)
	if err != nil {
		return err
	}
	digests, err := s.weeklyDigestEmails(ctx)
	if err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		recorded, err := tx.RecordScheduledRun(ctx, WeeklyEmailsJob, slot)
		if err != nil || !recorded {
			return err
		}
		return tx.queueEmails(ctx, append(reminders, digests...))
	})
}

// weeklyReminders composes an email to each Owner and Delegate of the Active
// Goals whose Check-in is due — it would be overdue before next week's
// reminder — or that are Check-in overdue already, each linking to its
// Check-in page, which shows the last Check-in's answers as ghost text. It
// also lists every open Action Item the person owns, overdue first, so the
// owner of one is emailed even with no Check-in due. Someone with no Check-in
// due and no open Action Item gets no email, and nobody who has left the
// company is emailed. It reads the Freshness and the Delegates of every Active
// Goal at once, in the same few queries however many Goals there are.
func (s *Service) weeklyReminders(ctx context.Context) ([]email.Message, error) {
	goals, err := s.ListGoals(ctx)
	if err != nil {
		return nil, err
	}
	var active []Goal
	var ids []int64
	for _, g := range goals {
		if g.Lifecycle == LifecycleActive {
			active = append(active, g)
			ids = append(ids, g.ID)
		}
	}
	freshness, err := s.FreshnessByGoal(ctx, active)
	if err != nil {
		return nil, err
	}
	delegates, err := s.ListDelegatesForGoals(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []ReminderItem
	for _, g := range active {
		f := freshness[g.ID]
		if !f.DueBeforeNextReminder() {
			continue
		}
		out = append(out, ReminderItem{To: g.Owner, Goal: g, Freshness: f})
		for _, d := range delegates[g.ID] {
			out = append(out, ReminderItem{To: d, Goal: g, Freshness: f, AsDelegate: true})
		}
	}
	items, err := s.allOpenActionItems(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		out = append(out, ReminderItem{To: items[i].Item.Owner, ActionItem: &items[i]})
	}
	return s.composer.Reminders(out), nil
}

// weeklyDigestEmails composes an email to each parent Owner, a digest of what
// needs their attention: the link requests waiting on them, and problems
// among the Goals that contribute to theirs, one email for each digest the
// domain reads (Service.WeeklyDigests). Someone with nothing to report gets
// no email.
func (s *Service) weeklyDigestEmails(ctx context.Context) ([]email.Message, error) {
	digests, err := s.WeeklyDigests(ctx)
	if err != nil {
		return nil, err
	}
	return s.composer.WeeklyDigests(digests), nil
}
