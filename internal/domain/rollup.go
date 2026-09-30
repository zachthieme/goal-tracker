package domain

import "context"

// RolledUpHealth is a Goal's Rolled-up Health: the worst Owner-set Health among
// its Active children, computed when read (ADR-0003, CONTEXT.md: Rolled-up
// Health). It sits next to the Owner-set Health, and when the two differ the
// Owner has to explain why. Present is false when the Goal has no Active child
// that has set a Health yet — there is nothing to roll up.
type RolledUpHealth struct {
	Health  string
	Present bool
}

// healthRank orders Health from best to worst so the worst is the maximum:
// Green < Yellow < Red. An unknown value ranks below Green so it never wins.
func healthRank(health string) int {
	switch health {
	case HealthGreen:
		return 1
	case HealthYellow:
		return 2
	case HealthRed:
		return 3
	default:
		return 0
	}
}

// RolledUpHealth computes goalID's Rolled-up Health from its Active children's
// Owner-set Health (ADR-0003). Health is set by the Owner and never computed, so
// this reads each Active child's latest Check-in and keeps the worst. Only
// Active children count: a Proposed child has no Health, and a child with no
// Check-in yet has none either, so both are skipped. When no Active child has a
// Health, Present is false. The traversal is one level — each parent shows the
// roll-up of its own direct children, which itself already reflects the levels
// below it.
func (s *Service) RolledUpHealth(ctx context.Context, goalID int64) (RolledUpHealth, error) {
	children, err := s.ChildrenOf(ctx, goalID)
	if err != nil {
		return RolledUpHealth{}, err
	}
	var out RolledUpHealth
	for _, child := range children {
		if child.Lifecycle != LifecycleActive {
			continue
		}
		latest, ok, err := s.LatestCheckin(ctx, child.ID)
		if err != nil {
			return RolledUpHealth{}, err
		}
		if !ok {
			continue
		}
		if !out.Present || healthRank(latest.Health) > healthRank(out.Health) {
			out = RolledUpHealth{Health: latest.Health, Present: true}
		}
	}
	return out, nil
}
