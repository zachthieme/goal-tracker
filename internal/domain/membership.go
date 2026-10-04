package domain

import (
	"context"
	"slices"
	"time"
)

// MembershipChange is a Goal that entered or left a Report since its
// Definition's latest publication, and why. The reason is read from the
// definition and the Goal as they are now, never from the publication, and
// never names the attribute that changed: the Goal's own History has that.
type MembershipChange struct {
	Goal Goal
	// Direction is MembershipAdded or MembershipLeft.
	Direction string
	// Reason is one of the Membership reasons below, by its direction.
	Reason string
}

// The directions a membership change takes.
const (
	MembershipAdded = "Added"
	MembershipLeft  = "Left"
)

// The reasons a Goal entered a Report.
const (
	MembershipPickedByHand = "picked by hand"
	MembershipNowMatches   = "now matches the rules"
)

// The reasons a Goal left a Report.
const (
	MembershipFinishedBeforeBaseline = "finished before this report's baseline"
	MembershipRemovedByHand          = "removed by hand"
	MembershipNoLongerMatches        = "no longer matches the rules"
)

// membershipChanges are the Goals that entered or left r since its
// Definition's latest publication, whatever baseline r reads changes against:
// those selected now that it didn't hold, then those it held that aren't. It
// is empty when the Definition has never been published. since reports
// whether an instant falls after r's baseline, as DraftReport reads changes.
func (s *Service) membershipChanges(ctx context.Context, r Report, since func(time.Time) bool) ([]MembershipChange, error) {
	latest, err := s.previousPublication(ctx, r.Definition.ID)
	if err != nil || latest.ID == 0 {
		return nil, err
	}
	pub, err := s.GetPublication(ctx, latest.ID)
	if err != nil {
		return nil, err
	}
	def := r.Definition
	var out []MembershipChange
	for _, g := range r.goals() {
		if pub.Report.covers(g.ID) {
			continue
		}
		reason := MembershipNowMatches
		if slices.Contains(def.Include, g.ID) || slices.Contains(def.Picked, g.ID) {
			reason = MembershipPickedByHand
		}
		out = append(out, MembershipChange{Goal: g, Direction: MembershipAdded, Reason: reason})
	}
	for _, was := range pub.Report.goals() {
		if r.covers(was.ID) {
			continue
		}
		g, err := s.loadGoal(ctx, was.ID)
		if err != nil {
			return nil, err
		}
		reason, err := s.leftReason(ctx, def, g, since)
		if err != nil {
			return nil, err
		}
		out = append(out, MembershipChange{Goal: g, Direction: MembershipLeft, Reason: reason})
	}
	return out, nil
}

// leftReason is why g, no longer selected by def, left its Report: the first
// that holds of finishing before the baseline, being left out or unpicked by
// hand, or else no longer meeting the rules.
func (s *Service) leftReason(ctx context.Context, def ReportDefinition, g Goal, since func(time.Time) bool) (string, error) {
	finished, err := s.finishedBefore(ctx, g, since)
	if err != nil {
		return "", err
	}
	switch {
	case finished:
		return MembershipFinishedBeforeBaseline, nil
	case slices.Contains(def.Exclude, g.ID),
		def.Mode == ReportModePicked && !slices.Contains(def.Picked, g.ID):
		return MembershipRemovedByHand, nil
	default:
		return MembershipNoLongerMatches, nil
	}
}

// goals are the Goals the Report selects, exceptions first.
func (r Report) goals() []Goal {
	out := make([]Goal, 0, len(r.Exceptions)+len(r.Lines))
	for _, b := range r.Exceptions {
		out = append(out, b.Goal)
	}
	for _, sg := range r.Lines {
		out = append(out, sg.Goal)
	}
	return out
}
