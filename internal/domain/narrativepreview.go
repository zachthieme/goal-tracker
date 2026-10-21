package domain

import (
	"context"
	"time"
)

// PreviewNarrative drafts the Report for def against baseline, as DraftReport
// does, with the narrative in in place of the one saved, so the author sees
// what they are composing before they save it. It saves nothing and the
// Report log records nothing. It refuses what CurateNarrative refuses: anyone
// who may not curate def (ErrNotAuthorized), and a narrative CurateNarrative
// couldn't save (ErrValidation).
func (s *Service) PreviewNarrative(ctx context.Context, actorID int64, def ReportDefinition, baseline time.Time, in CurateNarrativeInput) (Report, error) {
	if err := s.requireReportManager(ctx, actorID, def, "curate"); err != nil {
		return Report{}, err
	}
	notes, err := s.checkCuratedNarrative(ctx, def, in)
	if err != nil {
		return Report{}, err
	}
	sectionOf := make(map[int64]string, len(in.Picks))
	for _, p := range in.Picks {
		sectionOf[p.HighlightID] = p.Section
	}
	return s.draftReport(ctx, def, baseline, func(r *Report) error {
		narrateReport(r, sectionOf, notes)
		return nil
	})
}

// NarrativeUnsaved reports whether saving in as the Report Definition defID's
// narrative would change the one saved: whether the author composing it has
// unsaved changes. Blank notes and spacing around one don't count, as saving
// drops them.
func (s *Service) NarrativeUnsaved(ctx context.Context, defID int64, in CurateNarrativeInput) (bool, error) {
	edit, err := s.narrativeEdit(ctx, defID, in.Picks, storedNarrativeNotes(in.Notes))
	return edit != "", err
}
