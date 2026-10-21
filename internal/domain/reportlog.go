package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// ReportLogEntry is one entry in a Report Definition's Report log: a change
// to it, who made it and when (CONTEXT.md: Report log). Kind says what sort of
// change it is, and Detail what changed in words, before → after, with the
// names as they were at the time. The tool writes it in the same transaction
// as the change, and nothing edits or removes it.
type ReportLogEntry struct {
	ID           int64
	DefinitionID int64
	Actor        Account
	// Kind is ReportLogCreated, ReportLogEdited, ReportLogDelegate,
	// ReportLogNarrative or ReportLogPublished.
	Kind      string
	Detail    string
	CreatedAt time.Time
}

// The kinds of change a Report log records.
const (
	// ReportLogCreated is the Definition being saved for the first time.
	ReportLogCreated = "created"
	// ReportLogEdited is one changed part of the Definition: its title,
	// introduction, how it selects, a rule, a picked Goal, an Also include or
	// Leave out, or a Field it shows.
	ReportLogEdited = "edited"
	// ReportLogDelegate is a Report Delegate added or removed.
	ReportLogDelegate = "delegate"
	// ReportLogNarrative is the next publication's narrative curated:
	// Highlights picked or dropped, and sections whose notes changed.
	ReportLogNarrative = "narrative"
	// ReportLogPublished is a publication.
	ReportLogPublished = "published"
)

// ReportLog returns the Report Definition defID's Report log, newest first.
// Anyone who can open the Report may read it, and nobody can edit it.
// Changes made before the log was kept aren't in it.
func (s *Service) ReportLog(ctx context.Context, defID int64) ([]ReportLogEntry, error) {
	rows, err := s.queries.ListReportLog(ctx, defID)
	if err != nil {
		return nil, fmt.Errorf("list report log: %w", err)
	}
	out := make([]ReportLogEntry, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.ReportLog.CreatedAt)
		out = append(out, ReportLogEntry{
			ID:           r.ReportLog.ID,
			DefinitionID: r.ReportLog.ReportDefinitionID,
			Actor:        accountFromRow(r.Account),
			Kind:         r.ReportLog.Kind,
			Detail:       r.ReportLog.Detail,
			CreatedAt:    createdAt,
		})
	}
	return out, nil
}

// recordReportLog writes one entry of kind to the Report Definition defID's
// Report log, made by actorID now. Call it within the change's transaction,
// so the change never commits without it.
func (s *Service) recordReportLog(ctx context.Context, defID, actorID int64, kind, detail string) error {
	if err := s.queries.RecordReportLogEntry(ctx, db.RecordReportLogEntryParams{
		ReportDefinitionID: defID,
		ActorID:            actorID,
		Kind:               kind,
		Detail:             detail,
		CreatedAt:          s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("record report log: %w", err)
	}
	return nil
}

// reportDefinitionEdits says, one entry per changed part, how in changes the
// saved Definition def, in words: its title and how it selects, before →
// after; that its introduction changed, summarised rather than quoted; each
// rule added and removed; each Goal added to or removed from its picked
// Goals, its Also include or its Leave out, by its title; and each Field it
// shows added or removed. Saving def unchanged changes nothing.
func (s *Service) reportDefinitionEdits(ctx context.Context, def ReportDefinition, in SaveReportDefinitionInput) ([]string, error) {
	var edits []string
	if in.Name != def.Name {
		edits = append(edits, fmt.Sprintf("Title: %s → %s", def.Name, in.Name))
	}
	if strings.TrimSpace(in.Introduction) != def.Introduction {
		edits = append(edits, "Introduction changed")
	}
	if in.Mode != def.Mode {
		edits = append(edits, fmt.Sprintf("Selects by: %s → %s", reportModeWords(def.Mode), reportModeWords(in.Mode)))
	}
	added, removed := changedReportRules(def.Rules, in.Rules)
	for _, change := range []struct {
		label string
		rules []ReportRule
	}{{"Rule added", added}, {"Rule removed", removed}} {
		for _, rule := range change.rules {
			words, err := s.ruleScope(ctx, rule)
			if err != nil {
				return nil, err
			}
			edits = append(edits, change.label+": "+words)
		}
	}
	goalEdits, err := s.reportGoalListEdits(ctx, []reportGoalListChange{
		{"Picked Goal", def.Picked, in.Picked, true},
		{"Also include", def.Include, in.Include, false},
		{"Leave out", def.Exclude, in.Exclude, false},
	})
	if err != nil {
		return nil, err
	}
	edits = append(edits, goalEdits...)
	fieldEdits, err := s.reportFieldEdits(ctx, def.FieldIDs, in.FieldIDs)
	if err != nil {
		return nil, err
	}
	return append(edits, fieldEdits...), nil
}

// reportModeWords is how the Report log says a Definition selects its Goals.
func reportModeWords(mode string) string {
	if mode == ReportModePicked {
		return "picked Goals"
	}
	return "rules"
}

// reportFieldEdits says each Field a Definition shows that it didn't before,
// and each it no longer shows, by its name: "Field shown added: Budget".
func (s *Service) reportFieldEdits(ctx context.Context, before, after []int64) ([]string, error) {
	name := func(id int64) (string, error) {
		row, err := s.queries.GetField(ctx, id)
		if err != nil {
			return "", fmt.Errorf("look up field: %w", err)
		}
		return row.Name, nil
	}
	var edits []string
	for _, change := range []struct {
		label    string
		ids, not []int64
	}{{"Field shown added", after, before}, {"Field shown removed", before, after}} {
		for _, id := range change.ids {
			if slices.Contains(change.not, id) {
				continue
			}
			n, err := name(id)
			if err != nil {
				return nil, err
			}
			edits = append(edits, change.label+": "+n)
		}
	}
	return edits, nil
}

// reportGoalListChange is one of a Definition's lists of Goals, by the name
// the Report log gives it, before and after an edit. ordered says the list's
// order is the Report's, as the picked Goals' is.
type reportGoalListChange struct {
	label         string
	before, after []int64
	ordered       bool
}

// reportGoalListEdits says each Goal added to and removed from each of lists,
// by its title: "Also include added: Alpha"; and, of an ordered list, that
// the Goals it kept were reordered.
func (s *Service) reportGoalListEdits(ctx context.Context, lists []reportGoalListChange) ([]string, error) {
	var ids []int64
	for _, l := range lists {
		ids = append(ids, l.before...)
		ids = append(ids, l.after...)
	}
	titles, err := s.reportLogGoalTitles(ctx, ids)
	if err != nil {
		return nil, err
	}
	var edits []string
	for _, l := range lists {
		for _, id := range l.after {
			if !slices.Contains(l.before, id) {
				edits = append(edits, l.label+" added: "+titles[id])
			}
		}
		for _, id := range l.before {
			if !slices.Contains(l.after, id) {
				edits = append(edits, l.label+" removed: "+titles[id])
			}
		}
		kept := func(ids, other []int64) []int64 {
			return slices.DeleteFunc(slices.Clone(ids), func(id int64) bool { return !slices.Contains(other, id) })
		}
		if l.ordered && !slices.Equal(kept(l.before, l.after), kept(l.after, l.before)) {
			edits = append(edits, l.label+"s reordered")
		}
	}
	return edits, nil
}

// reportLogGoalTitles reads the titles of the Goals ids name, keyed by id, in
// one query.
func (s *Service) reportLogGoalTitles(ctx context.Context, ids []int64) (map[int64]string, error) {
	titles := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return titles, nil
	}
	rows, err := s.queries.ListGoalsByID(ctx, dedupeIDs(ids))
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	for _, r := range rows {
		titles[r.Goal.ID] = r.Goal.Title
	}
	return titles, nil
}

// changedReportRules returns the rules in after that weren't in before, and
// those in before that aren't in after. A rule whose values changed is one
// removed and another added; the order of the rules and of their values
// doesn't matter.
func changedReportRules(before, after []ReportRule) (added, removed []ReportRule) {
	count := map[string]int{}
	for _, r := range before {
		count[reportRuleKey(r)]++
	}
	for _, r := range after {
		if k := reportRuleKey(r); count[k] > 0 {
			count[k]--
		} else {
			added = append(added, r)
		}
	}
	for _, r := range before {
		if k := reportRuleKey(r); count[k] > 0 {
			count[k]--
			removed = append(removed, r)
		}
	}
	return added, removed
}

// reportRuleKey is what a rule tests, the same however its values are
// ordered or repeated.
func reportRuleKey(r ReportRule) string {
	values := slices.Clone(dedupeIDs(r.Values))
	slices.Sort(values)
	return fmt.Sprintf("%s|%d|%s|%s", r.Attribute, r.DimensionID, r.Op, strings.Join(values, ","))
}

// narrativeEdit says in one entry how saving picks and notes changes the
// Report Definition defID's draft narrative: each Highlight picked into a
// section, moved to another or dropped, by its note, then each section whose
// notes changed, summarised rather than quoted. notes are as stored: trimmed,
// blank ones left out. It is "" when nothing changed.
func (s *Service) narrativeEdit(ctx context.Context, defID int64, picks []NarrativePick, notes map[string][]string) (string, error) {
	oldPicks, err := s.queries.ListNarrativePicks(ctx, defID)
	if err != nil {
		return "", fmt.Errorf("list narrative picks: %w", err)
	}
	oldTexts, err := s.queries.ListNarrativeTexts(ctx, defID)
	if err != nil {
		return "", fmt.Errorf("list narrative texts: %w", err)
	}
	oldSection := make(map[int64]string, len(oldPicks))
	ids := make([]int64, 0, len(oldPicks)+len(picks))
	for _, p := range oldPicks {
		oldSection[p.HighlightID] = p.Section
		ids = append(ids, p.HighlightID)
	}
	newSection := make(map[int64]string, len(picks))
	for _, p := range picks {
		newSection[p.HighlightID] = p.Section
		ids = append(ids, p.HighlightID)
	}
	noteOf, err := s.reportLogHighlightNotes(ctx, ids)
	if err != nil {
		return "", err
	}
	heading := func(section string) string { return NarrativeSection{Kind: section}.Heading() }

	var parts []string
	for _, p := range picks {
		switch was, ok := oldSection[p.HighlightID]; {
		case !ok:
			parts = append(parts, fmt.Sprintf("picked %s into %s", noteOf(p.HighlightID), heading(p.Section)))
		case was != p.Section:
			parts = append(parts, fmt.Sprintf("moved %s to %s", noteOf(p.HighlightID), heading(p.Section)))
		}
	}
	for _, p := range oldPicks {
		if _, ok := newSection[p.HighlightID]; !ok {
			parts = append(parts, "dropped "+noteOf(p.HighlightID))
		}
	}
	oldNotes := make(map[string][]string, len(narrativeKinds))
	for _, t := range oldTexts {
		oldNotes[t.Section] = append(oldNotes[t.Section], t.Text)
	}
	for _, section := range narrativeKinds {
		if !slices.Equal(oldNotes[section], notes[section]) {
			parts = append(parts, heading(section)+" notes changed")
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "Narrative: " + strings.Join(parts, "; "), nil
}

// reportLogHighlightNotes reads the notes of the Highlights ids name in one
// query, returning how the Report log names each: its note in quotes, or "a
// Highlight" for one it can't find.
func (s *Service) reportLogHighlightNotes(ctx context.Context, ids []int64) (func(int64) string, error) {
	notes := make(map[int64]string, len(ids))
	if len(ids) > 0 {
		rows, err := s.queries.ListReportLogHighlightNotes(ctx, dedupeIDs(ids))
		if err != nil {
			return nil, fmt.Errorf("list highlight notes: %w", err)
		}
		for _, r := range rows {
			notes[r.ID] = r.Note
		}
	}
	return func(id int64) string {
		if note, ok := notes[id]; ok {
			return "“" + note + "”"
		}
		return "a Highlight"
	}, nil
}
