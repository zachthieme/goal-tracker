package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Publication is a published Report: a snapshot of a Report Definition's
// Report frozen when it was published (CONTEXT.md: Report Definition). It is
// written once and never changes, however the Goals it covers change later.
type Publication struct {
	ID           int64
	DefinitionID int64
	PublishedBy  Account
	PublishedAt  time.Time
	// Report is the Report as it read when published, exactly as rendered. It
	// is stored as JSON of the Report view model, so renaming a field of
	// Report (or of a type it holds) drops that field from earlier snapshots.
	Report Report
}

// PreviousPublication identifies the publication a Report reads its changes
// against: the latest earlier publication of the same Report Definition.
type PreviousPublication struct {
	ID          int64
	PublishedAt time.Time
}

// PublishReport publishes the Report Definition defID: it drafts the Report
// against baseline, as DraftReport does — so by default against the previous
// publication — and freezes the result. Anyone signed in may publish; actorID
// records who did.
func (s *Service) PublishReport(ctx context.Context, actorID, defID int64, baseline time.Time) (Publication, error) {
	def, err := s.GetReportDefinition(ctx, defID)
	if err != nil {
		return Publication{}, err
	}
	report, err := s.DraftReport(ctx, def, baseline)
	if err != nil {
		return Publication{}, err
	}
	snapshot, err := json.Marshal(report)
	if err != nil {
		return Publication{}, fmt.Errorf("freeze report: %w", err)
	}
	row, err := s.queries.CreateReportPublication(ctx, db.CreateReportPublicationParams{
		ReportDefinitionID: def.ID,
		PublishedBy:        actorID,
		PublishedAt:        s.clock.Now().Format(timeFormat),
		Snapshot:           string(snapshot),
	})
	if err != nil {
		return Publication{}, fmt.Errorf("create publication: %w", err)
	}
	return s.publicationFromRow(ctx, row)
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
	return s.publicationFromRow(ctx, row)
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
		p, err := s.publicationFromRow(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Service) publicationFromRow(ctx context.Context, row db.ReportPublication) (Publication, error) {
	var report Report
	if err := json.Unmarshal([]byte(row.Snapshot), &report); err != nil {
		return Publication{}, fmt.Errorf("read publication %d: %w", row.ID, err)
	}
	by, err := s.queries.GetAccount(ctx, row.PublishedBy)
	if err != nil {
		return Publication{}, fmt.Errorf("look up publisher: %w", err)
	}
	publishedAt, _ := time.Parse(timeFormat, row.PublishedAt)
	return Publication{
		ID:           row.ID,
		DefinitionID: row.ReportDefinitionID,
		PublishedBy:  accountFromRow(by),
		PublishedAt:  publishedAt,
		Report:       report,
	}, nil
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
