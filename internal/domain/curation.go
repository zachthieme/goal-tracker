package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
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

// NarrativeSection is one section of a Report's narrative — its Insights,
// Accomplishments, or Misses — built from the Highlights the author picked
// into it and the author's own text.
type NarrativeSection struct {
	// Kind is the section: HighlightInsight, HighlightAccomplishment, or
	// HighlightMiss.
	Kind       string
	Text       string
	Highlights []NarrativeHighlight
}

// Heading is the section's title as a Report shows it.
func (n NarrativeSection) Heading() string {
	switch n.Kind {
	case HighlightInsight:
		return "Insights"
	case HighlightAccomplishment:
		return "Accomplishments"
	default:
		return "Misses"
	}
}

// narrativeKinds are the narrative's sections in the order a Report shows them.
var narrativeKinds = []string{HighlightInsight, HighlightAccomplishment, HighlightMiss}

// NarrativePick puts one Highlight into a narrative section.
type NarrativePick struct {
	HighlightID int64
	Section     string
}

// CurateNarrativeInput is the curate-a-narrative command's input: the
// Highlights the author picks and the section each goes into, and the author's
// own text keyed by section (HighlightInsight, HighlightAccomplishment,
// HighlightMiss).
type CurateNarrativeInput struct {
	Picks []NarrativePick
	Text  map[string]string
}

// CurateNarrative sets the narrative of the Report Definition defID's next
// publication, replacing whatever was curated before. The author may put a
// Highlight into any section, whatever it was flagged as; each picked
// Highlight must be on a Goal the Definition selects, and picked once. Anyone
// signed in may curate, as anyone may publish. Publishing freezes the
// narrative with the snapshot and starts the next one empty.
func (s *Service) CurateNarrative(ctx context.Context, defID int64, in CurateNarrativeInput) error {
	def, err := s.GetReportDefinition(ctx, defID)
	if err != nil {
		return err
	}
	for section := range in.Text {
		if !validHighlightKind(section) {
			return fmt.Errorf("%w: %q is not a narrative section", ErrValidation, section)
		}
	}
	inScope, err := s.definitionHighlights(ctx, def)
	if err != nil {
		return err
	}
	picked := make(map[int64]bool, len(in.Picks))
	for _, p := range in.Picks {
		if !validHighlightKind(p.Section) {
			return fmt.Errorf("%w: %q is not a narrative section", ErrValidation, p.Section)
		}
		if !inScope[p.HighlightID] {
			return fmt.Errorf("%w: Highlight %d is not on a Goal this Report selects", ErrValidation, p.HighlightID)
		}
		if picked[p.HighlightID] {
			return fmt.Errorf("%w: Highlight %d is picked twice", ErrValidation, p.HighlightID)
		}
		picked[p.HighlightID] = true
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.clearNarrative(ctx, def.ID); err != nil {
			return err
		}
		for _, p := range in.Picks {
			if err := tx.queries.AddNarrativePick(ctx, db.AddNarrativePickParams{
				ReportDefinitionID: def.ID,
				HighlightID:        p.HighlightID,
				Section:            p.Section,
			}); err != nil {
				return fmt.Errorf("add narrative pick: %w", err)
			}
		}
		for _, section := range narrativeKinds {
			text := strings.TrimSpace(in.Text[section])
			if text == "" {
				continue
			}
			if err := tx.queries.SetNarrativeText(ctx, db.SetNarrativeTextParams{
				ReportDefinitionID: def.ID,
				Section:            section,
				Text:               text,
			}); err != nil {
				return fmt.Errorf("set narrative text: %w", err)
			}
		}
		return nil
	})
}

// clearNarrative drops the Report Definition's draft narrative.
func (s *Service) clearNarrative(ctx context.Context, defID int64) error {
	if err := s.queries.ClearNarrativePicks(ctx, defID); err != nil {
		return fmt.Errorf("clear narrative picks: %w", err)
	}
	if err := s.queries.ClearNarrativeTexts(ctx, defID); err != nil {
		return fmt.Errorf("clear narrative texts: %w", err)
	}
	return nil
}

// definitionHighlights returns the ids of every Highlight on a Goal the
// Report Definition selects against its default baseline, whenever it was
// written.
func (s *Service) definitionHighlights(ctx context.Context, def ReportDefinition) (map[int64]bool, error) {
	since, err := s.readAgainst(ctx, &Report{Definition: def}, time.Time{})
	if err != nil {
		return nil, err
	}
	selected, err := s.SelectGoals(ctx, def, since)
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, sg := range selected {
		hs, err := s.ListHighlightsByGoal(ctx, sg.Goal.ID)
		if err != nil {
			return nil, err
		}
		for _, hl := range hs {
			out[hl.ID] = true
		}
	}
	return out, nil
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

// draftNarrative marks each of the Report's in-scope Highlights with the
// section the author picked it into, and builds the Report's narrative from
// them and the author's text: its sections in the order Insights,
// Accomplishments, Misses, leaving out any with neither. A pick of a Highlight
// outside the Report's scope — written before its baseline — is not shown.
func (s *Service) draftNarrative(ctx context.Context, r *Report) error {
	picks, err := s.queries.ListNarrativePicks(ctx, r.Definition.ID)
	if err != nil {
		return fmt.Errorf("list narrative picks: %w", err)
	}
	texts, err := s.queries.ListNarrativeTexts(ctx, r.Definition.ID)
	if err != nil {
		return fmt.Errorf("list narrative texts: %w", err)
	}
	sectionOf := make(map[int64]string, len(picks))
	for _, p := range picks {
		sectionOf[p.HighlightID] = p.Section
	}
	textOf := make(map[string]string, len(texts))
	for _, t := range texts {
		textOf[t.Section] = t.Text
	}
	for i := range r.Highlights {
		r.Highlights[i].Section = sectionOf[r.Highlights[i].Highlight.ID]
	}
	for _, kind := range narrativeKinds {
		sec := NarrativeSection{Kind: kind, Text: textOf[kind]}
		for _, nh := range r.Highlights {
			if nh.Section == kind {
				sec.Highlights = append(sec.Highlights, nh)
			}
		}
		if sec.Text != "" || len(sec.Highlights) > 0 {
			r.Narrative = append(r.Narrative, sec)
		}
	}
	return nil
}
