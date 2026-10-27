package domain

import (
	"context"
	"time"

	"github.com/zachthieme/goal-tracker/internal/email"
)

// HoldHashSlots fills every slot of s's hash limiter with a hash that blocks
// until the returned func is called, and returns once all of them are held.
func HoldHashSlots(s *Service) (release func()) {
	n := cap(s.hashes.slots)
	held, freed := make(chan struct{}), make(chan struct{})
	for range n {
		go func() {
			_ = s.hashes.run(context.Background(), func() {
				held <- struct{}{}
				<-freed
			})
		}()
	}
	for range n {
		<-held
	}
	return func() { close(freed) }
}

// OwnersMilestoneEditRefusal is milestoneEditRefusal for g's Owner on one of
// its Milestones whose status is status.
func OwnersMilestoneEditRefusal(g Goal, status string) error {
	owner := goalActor{
		id:       g.Owner.ID,
		account:  func() (Account, bool, error) { return g.Owner, true, nil },
		delegate: func() (bool, error) { return false, nil },
	}
	return milestoneEditRefusal(owner, g, status)
}

// PublishReportChangedMidway is PublishReport with between run after the
// Report is drafted and before the publication is written, as a change made
// by someone else while the Report was being drafted.
func PublishReportChangedMidway(ctx context.Context, s *Service, actorID, defID int64, baseline time.Time, between func()) (Publication, error) {
	draft, err := s.draftPublication(ctx, actorID, defID, baseline)
	if err != nil {
		return Publication{}, err
	}
	between()
	return s.writePublication(ctx, draft)
}

// SetGoalLifecycleFrom is setGoalLifecycle making change on goalID inside a
// transaction of s, as a Check-in makes it, so a test can plan the change
// from a Lifecycle the Goal has already left.
func SetGoalLifecycleFrom(ctx context.Context, s *Service, goalID int64, change LifecycleChange) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		return tx.setGoalLifecycle(ctx, goalID, change)
	})
}

// LifecycleTakesNoCheckinsRefusal is lifecycleTakesNoCheckinsRefusal, the
// refusal of a Check-in on a Goal in lifecycle, one that takes none.
func LifecycleTakesNoCheckinsRefusal(lifecycle string) error {
	return lifecycleTakesNoCheckinsRefusal(lifecycle)
}

// SuggestParentLifecycleSentence is the sentence suggestParentRefusal refuses
// a parent suggestion for a Goal in a Lifecycle that takes none with.
func SuggestParentLifecycleSentence() string {
	return suggestParentLifecycleSentence()
}

// AllOpenActionItems is allOpenActionItems: every open Action Item, by owner
// and then soonest due.
func AllOpenActionItems(ctx context.Context, s *Service) ([]ActionItemListing, error) {
	return s.allOpenActionItems(ctx)
}

// QueueEmails is queueEmails, queueing msgs in s's email outbox together.
func QueueEmails(ctx context.Context, s *Service, msgs []email.Message) error {
	return s.queueEmails(ctx, msgs)
}

// QueueWeeklyReminders queues the weekly Check-in reminders QueueWeeklyEmails
// sends now, outside the schedule, recording no slot.
func QueueWeeklyReminders(ctx context.Context, s *Service) error {
	msgs, err := s.weeklyReminders(ctx)
	if err != nil {
		return err
	}
	return s.queueEmails(ctx, msgs)
}

// QueueWeeklyDigests queues the weekly parent digests QueueWeeklyEmails sends
// now, outside the schedule, recording no slot.
func QueueWeeklyDigests(ctx context.Context, s *Service) error {
	msgs, err := s.weeklyDigestEmails(ctx)
	if err != nil {
		return err
	}
	return s.queueEmails(ctx, msgs)
}
