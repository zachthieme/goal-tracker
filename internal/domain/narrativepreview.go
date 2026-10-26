package domain

import (
	"context"
	"time"
)

// PreviewNarrative drafts the Report for def against baseline, as DraftReport
// does, with the narrative in in place of the one saved, so the author sees
// what they are composing before they save it. Saved picks of Highlights the
// form didn't offer (in.Offered) stay, as saving keeps them. It saves nothing
// and the Report log records nothing. It refuses what CurateNarrative refuses: anyone
// who may not curate def (ErrNotAuthorized), and a narrative CurateNarrative
// couldn't save (ErrValidation).
func (s *Service) PreviewNarrative(ctx context.Context, actorID int64, def ReportDefinition, baseline time.Time, in CurateNarrativeInput) (Report, error) {
	if err := s.reportRule(ctx, actorID, def, curateNarrativeRefusal); err != nil {
		return Report{}, err
	}
	notes, err := s.checkCuratedNarrative(ctx, def, in)
	if err != nil {
		return Report{}, err
	}
	picks, err := s.narrativePicksSaved(ctx, def.ID, in)
	if err != nil {
		return Report{}, err
	}
	sectionOf := make(map[int64]string, len(picks))
	for _, p := range picks {
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
// drops them, and nor do saved picks of Highlights the form didn't offer, as
// saving keeps them.
func (s *Service) NarrativeUnsaved(ctx context.Context, defID int64, in CurateNarrativeInput) (bool, error) {
	picks, err := s.narrativePicksSaved(ctx, defID, in)
	if err != nil {
		return false, err
	}
	edit, err := s.narrativeEdit(ctx, defID, picks, storedNarrativeNotes(in.Notes))
	return edit != "", err
}
