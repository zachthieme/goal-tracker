package domain

import (
	"context"
	"fmt"
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

// planLifecycleChange validates a Check-in's requested Lifecycle against the
// Goal's current one and returns the change to record, or the zero change when
// the Check-in leaves it alone. An Active Goal may go On Hold or be Cancelled,
// each with a reason, or be marked Done with a one-line outcome; an On Hold Goal
// may be resumed to Active or Cancelled with a reason. Every other Lifecycle is
// closed to Check-ins: a Proposed Goal becomes Active through the activation
// gate, and Done and Cancelled are end states.
func planLifecycleChange(current, to, reason, outcome string) (LifecycleChange, error) {
	reason = strings.TrimSpace(reason)
	outcome = strings.TrimSpace(outcome)
	if !LifecycleTakesCheckins(current) {
		return LifecycleChange{}, Refuse(ErrValidation, "only an Active Goal can be checked in on")
	}
	if to == "" || to == current {
		if current == LifecycleOnHold {
			return LifecycleChange{}, inputError(CheckinInputLifecycle, "an On Hold Goal can only be resumed or Cancelled in a Check-in")
		}
		return LifecycleChange{}, nil
	}

	change := LifecycleChange{From: current, To: to, Reason: reason}
	switch {
	case current == LifecycleActive && (to == LifecycleOnHold || to == LifecycleCancelled),
		current == LifecycleOnHold && to == LifecycleCancelled:
		if reason == "" && to == LifecycleCancelled {
			return LifecycleChange{}, inputError(CheckinInputLifecycleReason, "cancelling a Goal needs a reason")
		}
		if reason == "" {
			return LifecycleChange{}, inputError(CheckinInputLifecycleReason, "putting a Goal On Hold needs a reason")
		}
	case current == LifecycleActive && to == LifecycleDone:
		if outcome == "" {
			return LifecycleChange{}, inputError(CheckinInputOutcome, "marking a Goal Done needs an outcome")
		}
		if strings.ContainsAny(outcome, "\r\n") {
			return LifecycleChange{}, inputError(CheckinInputOutcome, "a Goal's outcome is one line")
		}
		change.Outcome = outcome
	case current == LifecycleOnHold && to == LifecycleActive:
		// Resuming is not leaving Active, so no reason is needed.
	default:
		return LifecycleChange{}, inputError(CheckinInputLifecycle, "a Check-in can't move a %s Goal to %q", current, to)
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
