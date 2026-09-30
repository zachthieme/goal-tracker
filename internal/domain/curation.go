package domain

import (
	"context"
	"time"
)

// NarrativeHighlight is a Highlight in a Report's scope: written on a Goal the
// Report Definition selects, since the Report's baseline. The Highlight credits
// the Goal's Owner (CONTEXT.md: Highlight).
type NarrativeHighlight struct {
	Highlight Highlight
	GoalID    int64
	GoalTitle string
	// Section is the narrative section the author picked the Highlight into —
	// HighlightInsight, HighlightAccomplishment, or HighlightMiss — and empty
	// when the author left it out.
	Section string
}

// scopedHighlights returns the Goal's Highlights written since the baseline,
// newest first.
func (s *Service) scopedHighlights(ctx context.Context, g Goal, since func(time.Time) bool) ([]NarrativeHighlight, error) {
	hs, err := s.ListHighlightsByGoal(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	var out []NarrativeHighlight
	for _, hl := range hs {
		if since(hl.CreatedAt) {
			out = append(out, NarrativeHighlight{Highlight: hl, GoalID: g.ID, GoalTitle: g.Title})
		}
	}
	return out, nil
}
