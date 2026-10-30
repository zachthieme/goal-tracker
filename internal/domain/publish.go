package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Publication is a published Report: a Report Definition's Report frozen
// when it was published, as its facts read then, every word and link included
// (CONTEXT.md: Report Definition). It is written once and never changes,
// however the Goals it covers, or how Reports word a fact, change later. It is
// stored as a publication record (publicationrecord.go).
type Publication struct {
	ID           int64
	DefinitionID int64
	// Name is the Report Definition's name as it was published.
	Name string
	// PublishedBy is who published it, as they were shown then, from its
	// byline fact.
	PublishedBy Account
	PublishedAt time.Time
	// Facts are its facts as PublicationFacts listed them when it was
	// published.
	Facts []ReportFact
}

// Covers reports whether the publication holds the Goal goalID, as an
// exception block or a line On track.
func (p Publication) Covers(goalID int64) bool {
	for _, g := range p.Goals() {
		if g.ID == goalID {
			return true
		}
	}
	return false
}

// Goals are the Goals the publication holds, by ID and Title as published:
// its exception blocks' Goals, then its Goals On track.
func (p Publication) Goals() []Goal {
	var out []Goal
	for _, f := range p.Facts {
		switch f.Kind {
		case ReportFactExceptions:
			for _, b := range f.Blocks {
				for _, bf := range b.Facts {
					if bf.Kind == ReportFactBlockHead {
						out = append(out, Goal{ID: bf.Head.GoalID, Title: bf.Head.Title})
					}
				}
			}
		case ReportFactOnTrack:
			for _, sg := range f.Lines {
				out = append(out, Goal{ID: sg.Goal.ID, Title: sg.Goal.Title})
			}
		}
	}
	return out
}

// PreviousPublication identifies the publication a Report reads its changes
// against: the latest earlier publication of the same Report Definition.
type PreviousPublication struct {
	ID          int64
	PublishedAt time.Time
}

// PublishReport publishes the Report Definition defID: it drafts the Report
// against baseline, as DraftReport does — so by default against the previous
// publication — and freezes its facts as they read then (PublicationFacts), its
// curated narrative and how it selects in plain words included. The next
// publication's narrative then starts empty. Only the Definition's creator, a
// Report Delegate or an Admin may publish it (ErrNotAuthorized); actorID
// records who did, in the publication and in the Report log.
//
// Drafting reads a lot, so it runs before the transaction that writes the
// publication (ADR 0009: nothing slow inside a transaction); that transaction
// reads the Definition again and checks again who may publish it, so a Report
// Delegate removed while the Report was drafted is refused. It then refuses
// (ErrValidation) if the Report log has anything newer than when drafting
// began: every change to the Definition or its narrative is logged, so the
// draft may be missing it, and clearing the narrative would lose a newly
// curated one. That also refuses the second of two publishes racing.
func (s *Service) PublishReport(ctx context.Context, actorID, defID int64, baseline time.Time) (Publication, error) {
	draft, err := s.draftPublication(ctx, actorID, defID, baseline)
	if err != nil {
		return Publication{}, err
	}
	return s.writePublication(ctx, draft)
}

// draftedPublication is a Report drafted for publishing, not yet frozen or
// written: PublishReport's first step, run outside any transaction.
type draftedPublication struct {
	actorID   int64
	defID     int64
	report    Report
	publisher Account
	scope     string
	// logged is the id of the Definition's newest Report log entry when
	// drafting began, 0 when it had none.
	logged int64
}

// draftPublication drafts the Report Definition defID's Report for actorID to
// publish, against baseline, with its publisher and how it selects in plain
// words.
func (s *Service) draftPublication(ctx context.Context, actorID, defID int64, baseline time.Time) (draftedPublication, error) {
	// Noted before the Definition is read, so a change made at any point of
	// the draft is newer.
	logged, err := s.queries.LatestReportLogEntryID(ctx, defID)
	if err != nil {
		return draftedPublication{}, fmt.Errorf("latest report log entry: %w", err)
	}
	def, err := s.publishableDefinition(ctx, actorID, defID)
	if err != nil {
		return draftedPublication{}, err
	}
	report, err := s.DraftReport(ctx, def, baseline)
	if err != nil {
		return draftedPublication{}, err
	}
	publisher, err := s.Account(ctx, actorID)
	if err != nil {
		return draftedPublication{}, err
	}
	scope, err := s.reportScope(ctx, report)
	if err != nil {
		return draftedPublication{}, err
	}
	return draftedPublication{actorID: actorID, defID: def.ID, report: report, publisher: publisher, scope: scope, logged: logged}, nil
}

// writePublication freezes draft's facts (PublicationFacts) as of now and
// writes them as a publication, in its own transaction, and starts the next
// publication's narrative empty. The clock is read once: the instant it is
// published is the one its blocks are read as of.
func (s *Service) writePublication(ctx context.Context, draft draftedPublication) (Publication, error) {
	var row db.ReportPublication
	if err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.publishableDefinition(ctx, draft.actorID, draft.defID); err != nil {
			return err
		}
		logged, err := tx.queries.LatestReportLogEntryID(ctx, draft.defID)
		if err != nil {
			return fmt.Errorf("latest report log entry: %w", err)
		}
		if logged != draft.logged {
			return Refuse(ErrValidation, "The Report changed while it was being published. Publish it again.")
		}
		now := s.clock.Now()
		frozen, err := freezePublication(draft.report.Definition.Name, PublicationFacts(draft.report, draft.publisher, now, draft.scope))
		if err != nil {
			return err
		}
		if row, err = tx.queries.CreateReportPublication(ctx, db.CreateReportPublicationParams{
			ReportDefinitionID: draft.defID,
			PublishedBy:        draft.actorID,
			PublishedAt:        now.Format(timeFormat),
			Snapshot:           string(frozen),
		}); err != nil {
			return fmt.Errorf("create publication: %w", err)
		}
		if err := tx.recordReportLog(ctx, draft.defID, draft.actorID, ReportLogPublished, "Published"); err != nil {
			return err
		}
		// The narrative is frozen now; the next publication's starts empty.
		return tx.clearNarrative(ctx, draft.defID)
	}); err != nil {
		return Publication{}, err
	}
	return publicationFromRow(row)
}

// publishableDefinition returns the Report Definition defID if actorID may
// publish it (publishReportRefusal).
func (s *Service) publishableDefinition(ctx context.Context, actorID, defID int64) (ReportDefinition, error) {
	def, err := s.GetReportDefinition(ctx, defID)
	if err != nil {
		return ReportDefinition{}, err
	}
	if err := s.reportRule(ctx, actorID, def, publishReportRefusal); err != nil {
		return ReportDefinition{}, err
	}
	return def, nil
}

// GetPublication returns the published Report with the given id, or
// ErrNotFound if none exists.
func (s *Service) GetPublication(ctx context.Context, id int64) (Publication, error) {
	row, err := s.queries.GetReportPublication(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Publication{}, fmt.Errorf("%w: publication %d", ErrNotFound, id)
		}
		return Publication{}, fmt.Errorf("get publication: %w", err)
	}
	return publicationFromRow(row)
}

// ListPublications returns the Report Definition defID's publications, newest
// first.
func (s *Service) ListPublications(ctx context.Context, defID int64) ([]Publication, error) {
	rows, err := s.queries.ListReportPublications(ctx, defID)
	if err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	out := make([]Publication, 0, len(rows))
	for _, row := range rows {
		p, err := publicationFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// publicationFromRow reads a stored publication from its record
// (thawPublication), refusing a record it can't read.
func publicationFromRow(row db.ReportPublication) (Publication, error) {
	name, facts, err := thawPublication([]byte(row.Snapshot))
	if err != nil {
		return Publication{}, fmt.Errorf("read publication %d: %w", row.ID, err)
	}
	publishedAt, _ := time.Parse(timeFormat, row.PublishedAt)
	p := Publication{ID: row.ID, DefinitionID: row.ReportDefinitionID, Name: name, PublishedAt: publishedAt, Facts: facts}
	for _, f := range facts {
		if f.Kind == ReportFactByline {
			p.PublishedBy = f.Byline.PublishedBy
		}
	}
	return p, nil
}

// previousPublication returns the latest publication of the Report Definition
// defID, or the zero PreviousPublication when it has never been published.
func (s *Service) previousPublication(ctx context.Context, defID int64) (PreviousPublication, error) {
	row, err := s.queries.LatestReportPublication(ctx, defID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PreviousPublication{}, nil
		}
		return PreviousPublication{}, fmt.Errorf("latest publication: %w", err)
	}
	publishedAt, _ := time.Parse(timeFormat, row.PublishedAt)
	return PreviousPublication{ID: row.ID, PublishedAt: publishedAt}, nil
}
