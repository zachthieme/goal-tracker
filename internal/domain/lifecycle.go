package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// LifecycleChange is a move between Lifecycle states made in a Check-in
// (CONTEXT.md: Lifecycle). Leaving Active for On Hold or Cancelled carries a
// Reason; reaching Done carries a one-line Outcome, and the same Check-in's
// Metric readings are the Goal's final values. It is the zero value on a
// Check-in that left the Lifecycle alone.
type LifecycleChange struct {
	From    string
	To      string
	Reason  string
	Outcome string
}

// Changed reports whether the Check-in moved the Goal's Lifecycle.
func (c LifecycleChange) Changed() bool {
	return c.To != ""
}

// LifecycleTakesCheckins reports whether a Goal in lifecycle takes
// Check-ins: only an Active or On Hold one does, for a Proposed Goal becomes
// Active through the activation gate, and Done and Cancelled are end states
// (CONTEXT.md: Lifecycle).
func LifecycleTakesCheckins(lifecycle string) bool {
	return lifecycle == LifecycleActive || lifecycle == LifecycleOnHold
}

// LifecycleTakesDraftHighlights reports whether a Goal in lifecycle takes
// Draft Highlights: it does until it is Done or Cancelled, when it takes no
// more Check-ins to offer them at (CONTEXT.md: Draft Highlight).
func LifecycleTakesDraftHighlights(lifecycle string) bool {
	return lifecycle != LifecycleDone && lifecycle != LifecycleCancelled
}

// lifecycleKeepRefusal refuses a Check-in leaving a Goal in lifecycle as it
// is where that Lifecycle must change in one: an On Hold Goal's Check-in
// resumes or Cancels it (CONTEXT.md: Lifecycle).
func lifecycleKeepRefusal(lifecycle string) error {
	if lifecycle == LifecycleOnHold {
		return inputError(CheckinInputLifecycle, "an On Hold Goal can only be resumed or Cancelled in a Check-in")
	}
	return nil
}

// planLifecycleChange validates a Check-in's requested Lifecycle against the
// Goal's current one and returns the change to record, or the zero change when
// the Check-in leaves it alone. It allows the moves LifecycleChangesFrom
// offers: going On Hold or being Cancelled, each with a reason, being marked
// Done with a one-line outcome, or being resumed to Active.
func planLifecycleChange(current, to, reason, outcome string) (LifecycleChange, error) {
	reason = strings.TrimSpace(reason)
	outcome = strings.TrimSpace(outcome)
	if !LifecycleTakesCheckins(current) {
		return LifecycleChange{}, lifecycleTakesNoCheckinsRefusal(current)
	}
	if to == "" || to == current {
		return LifecycleChange{}, lifecycleKeepRefusal(current)
	}

	if !slices.Contains(LifecycleChangesFrom(current), to) {
		return LifecycleChange{}, inputError(CheckinInputLifecycle, "a Check-in can't move a %s Goal to %q", current, to)
	}
	change := LifecycleChange{From: current, To: to, Reason: reason}
	switch to {
	case LifecycleCancelled:
		if reason == "" {
			return LifecycleChange{}, inputError(CheckinInputLifecycleReason, "cancelling a Goal needs a reason")
		}
	case LifecycleOnHold:
		if reason == "" {
			return LifecycleChange{}, inputError(CheckinInputLifecycleReason, "putting a Goal On Hold needs a reason")
		}
	case LifecycleDone:
		if outcome == "" {
			return LifecycleChange{}, inputError(CheckinInputOutcome, "marking a Goal Done needs an outcome")
		}
		if strings.ContainsAny(outcome, "\r\n") {
			return LifecycleChange{}, inputError(CheckinInputOutcome, "a Goal's outcome is one line")
		}
		change.Outcome = outcome
	case LifecycleActive:
		// Resuming is not leaving Active, so no reason is needed.
	}
	return change, nil
}

// resultingLifecycle is the Lifecycle the Goal is in once a Check-in with this
// change is recorded.
func (c LifecycleChange) resultingLifecycle(current string) string {
	if c.Changed() {
		return c.To
	}
	return current
}

// requireFinalValues checks that a Done Check-in's readings give a final value
// for every Metric on the Goal, naming the first Metric that has none.
func (s *Service) requireFinalValues(ctx context.Context, goalID int64, readings []MetricReadingInput) error {
	metrics, err := s.queries.ListMetrics(ctx, goalID)
	if err != nil {
		return fmt.Errorf("list metrics: %w", err)
	}
	read := make(map[int64]bool, len(readings))
	for _, rd := range readings {
		read[rd.MetricID] = true
	}
	for _, m := range metrics {
		if !read[m.ID] {
			return inputError(CheckinReadingInput(m.ID), "marking a Goal Done needs a final value for every Metric; %q has none", m.Name)
		}
	}
	return nil
}
