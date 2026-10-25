package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
// into it and the author's own notes.
type NarrativeSection struct {
	// Kind is the section: HighlightInsight, HighlightAccomplishment, or
	// HighlightMiss.
	Kind string
	// Notes are the author's own notes in the section, in the order entered.
	Notes      []string
	Highlights []NarrativeHighlight
}

// UnmarshalJSON reads a NarrativeSection frozen in a Publication's snapshot. A
// snapshot from before a section held several notes has at most one, as Text;
// it reads as the section's only note, so the publication shows it as before.
func (n *NarrativeSection) UnmarshalJSON(data []byte) error {
	type frozen NarrativeSection // without this method, so decoding doesn't recurse
	var f struct {
		frozen
		Text string
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	*n = NarrativeSection(f.frozen)
	if f.Text != "" && n.Notes == nil {
		n.Notes = []string{f.Text}
	}
	return nil
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
// own notes keyed by section (HighlightInsight, HighlightAccomplishment,
// HighlightMiss), each section's in the order entered.
type CurateNarrativeInput struct {
	Picks []NarrativePick
	Notes map[string][]string
	// Offered are the Highlights the compose form offered the author: those in
	// scope at the baseline it was read against. A saved pick of a Highlight
	// it didn't offer, and Picks doesn't pick, is left as saved. Nil means it
	// offered every Highlight, so Picks replaces every saved pick.
	Offered []int64
}

// CurateNarrative sets the narrative of the Report Definition defID's next
// publication, replacing whatever was curated before, except the picks of
// Highlights the compose form didn't offer (in.Offered), which it leaves as
// saved. The author may put a Highlight into any section, whatever it was
// flagged as; each picked Highlight must be on a Goal the Definition selects,
// and picked once. A section may hold any number of notes; a blank one is
// ignored. Only the Definition's creator, a Report Delegate or an Admin may
// curate it (ErrNotAuthorized); actorID is who is curating, whom the Report
// log records with what changed. Re-saving the same narrative changes nothing. Publishing
// freezes the narrative with the snapshot and starts the next one empty.
func (s *Service) CurateNarrative(ctx context.Context, actorID, defID int64, in CurateNarrativeInput) error {
	def, err := s.GetReportDefinition(ctx, defID)
	if err != nil {
		return err
	}
	if err := s.requireReportManager(ctx, actorID, def, "curate"); err != nil {
		return err
	}
	notes, err := s.checkCuratedNarrative(ctx, def, in)
	if err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		picks, err := tx.narrativePicksSaved(ctx, def.ID, in)
		if err != nil {
			return err
		}
		edit, err := tx.narrativeEdit(ctx, def.ID, picks, notes)
		if err != nil {
			return err
		}
		if edit == "" {
			// Re-saving the same narrative changes nothing to log or store.
			return nil
		}
		if err := tx.recordReportLog(ctx, def.ID, actorID, ReportLogNarrative, edit); err != nil {
			return err
		}
		if err := tx.clearNarrative(ctx, def.ID); err != nil {
			return err
		}
		for _, p := range picks {
			if err := tx.queries.AddNarrativePick(ctx, db.AddNarrativePickParams{
				ReportDefinitionID: def.ID,
				HighlightID:        p.HighlightID,
				Section:            p.Section,
			}); err != nil {
				return fmt.Errorf("add narrative pick: %w", err)
			}
		}
		for _, section := range narrativeKinds {
			for _, note := range notes[section] {
				if err := tx.queries.AddNarrativeText(ctx, db.AddNarrativeTextParams{
					ReportDefinitionID: def.ID,
					Section:            section,
					Text:               note,
				}); err != nil {
					return fmt.Errorf("add narrative note: %w", err)
				}
			}
		}
		return nil
	})
}

// checkCuratedNarrative refuses a narrative CurateNarrative can't save for
// def (ErrValidation): a section that isn't one, or a pick of a Highlight not
// on a Goal def selects, or picked twice. It returns the narrative's notes
// keyed by section, each trimmed, the blank ones dropped.
func (s *Service) checkCuratedNarrative(ctx context.Context, def ReportDefinition, in CurateNarrativeInput) (map[string][]string, error) {
	for section := range in.Notes {
		if !validHighlightKind(section) {
			return nil, Refuse(ErrValidation, "%q is not a narrative section", section)
		}
	}
	inScope, err := s.definitionHighlights(ctx, def)
	if err != nil {
		return nil, err
	}
	picked := make(map[int64]bool, len(in.Picks))
	for _, p := range in.Picks {
		if !validHighlightKind(p.Section) {
			return nil, Refuse(ErrValidation, "%q is not a narrative section", p.Section)
		}
		if !inScope[p.HighlightID] {
			return nil, Refuse(ErrValidation, "Highlight %d is not on a Goal this Report selects", p.HighlightID)
		}
		if picked[p.HighlightID] {
			return nil, Refuse(ErrValidation, "Highlight %d is picked twice", p.HighlightID)
		}
		picked[p.HighlightID] = true
	}
	return storedNarrativeNotes(in.Notes), nil
}

// narrativePicksSaved are the picks saving in leaves in the Report Definition
// defID's narrative: those it picks, and each saved pick of a Highlight the
// compose form didn't offer (in.Offered), as it was saved.
func (s *Service) narrativePicksSaved(ctx context.Context, defID int64, in CurateNarrativeInput) ([]NarrativePick, error) {
	if in.Offered == nil {
		return in.Picks, nil
	}
	saved, err := s.queries.ListNarrativePicks(ctx, defID)
	if err != nil {
		return nil, fmt.Errorf("list narrative picks: %w", err)
	}
	inPlay := make(map[int64]bool, len(in.Offered)+len(in.Picks))
	for _, id := range in.Offered {
		inPlay[id] = true
	}
	for _, p := range in.Picks {
		inPlay[p.HighlightID] = true
	}
	picks := slices.Clone(in.Picks)
	for _, p := range saved {
		if !inPlay[p.HighlightID] {
			picks = append(picks, NarrativePick{HighlightID: p.HighlightID, Section: p.Section})
		}
	}
	return picks, nil
}

// storedNarrativeNotes are a narrative's notes, keyed by section, as they are
// stored: each trimmed, the blank ones and any of a section that isn't one
// dropped.
func storedNarrativeNotes(in map[string][]string) map[string][]string {
	notes := make(map[string][]string, len(narrativeKinds))
	for _, section := range narrativeKinds {
		for _, note := range in[section] {
			if note = strings.TrimSpace(note); note != "" {
				notes[section] = append(notes[section], note)
			}
		}
	}
	return notes
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
	goals := make([]Goal, 0, len(selected))
	for _, sg := range selected {
		goals = append(goals, sg.Goal)
	}
	byGoal, err := s.highlightsByGoal(ctx, goalIDsOf(goals))
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, hs := range byGoal {
		for _, hl := range hs {
			out[hl.ID] = true
		}
	}
	return out, nil
}

// scopedHighlights are those of hs, the Goal g's Highlights, written since the
// baseline, in hs's order: newest first.
func scopedHighlights(g Goal, hs []Highlight, since func(time.Time) bool) []NarrativeHighlight {
	var out []NarrativeHighlight
	for _, hl := range hs {
		if since(hl.CreatedAt) {
			out = append(out, NarrativeHighlight{Highlight: hl, GoalID: g.ID, GoalTitle: g.Title})
		}
	}
	return out
}

// draftNarrative builds the Report's narrative from the one saved for its
// Definition (see narrateReport).
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
	notesOf := make(map[string][]string, len(narrativeKinds))
	for _, t := range texts {
		notesOf[t.Section] = append(notesOf[t.Section], t.Text)
	}
	narrateReport(r, sectionOf, notesOf)
	return nil
}

// narrateReport marks each of the Report's in-scope Highlights with the
// section the author picked it into (sectionOf, by Highlight), and builds the
// Report's narrative from them and the author's notes (notesOf, by section):
// its sections in the order Insights, Accomplishments, Misses, leaving out
// any with neither. A pick of a Highlight outside the Report's scope —
// written before its baseline — is not shown.
func narrateReport(r *Report, sectionOf map[int64]string, notesOf map[string][]string) {
	for i := range r.Highlights {
		r.Highlights[i].Section = sectionOf[r.Highlights[i].Highlight.ID]
	}
	for _, kind := range narrativeKinds {
		sec := NarrativeSection{Kind: kind, Notes: notesOf[kind]}
		for _, nh := range r.Highlights {
			if nh.Section == kind {
				sec.Highlights = append(sec.Highlights, nh)
			}
		}
		if len(sec.Notes) > 0 || len(sec.Highlights) > 0 {
			r.Narrative = append(r.Narrative, sec)
		}
	}
}
