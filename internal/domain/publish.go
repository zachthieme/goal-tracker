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
	// PublishedBy is who published it, frozen as they were shown then, the
	// same way the snapshot freezes its Owners.
	PublishedBy Account
	PublishedAt time.Time
	// Report is the Report as it read when published, exactly as rendered. It
	// is stored as JSON of the Report view model, so renaming a field of
	// Report (or of a type it holds) drops that field from earlier snapshots.
	Report Report
}

// snapshot is a Publication as stored: the Report as it read when published
// and, beside it, the publisher as they were shown then. A snapshot frozen
// before publishers were has no PublishedBy.
type snapshot struct {
	Report
	PublishedBy *Account `json:",omitempty"`
}

// PreviousPublication identifies the publication a Report reads its changes
// against: the latest earlier publication of the same Report Definition.
type PreviousPublication struct {
	ID          int64
	PublishedAt time.Time
}

// PublishReport publishes the Report Definition defID: it drafts the Report
// against baseline, as DraftReport does — so by default against the previous
// publication — and freezes the result, its curated narrative included. The
// next publication's narrative then starts empty. Anyone signed in may
// publish; actorID records who did.
func (s *Service) PublishReport(ctx context.Context, actorID, defID int64, baseline time.Time) (Publication, error) {
	def, err := s.GetReportDefinition(ctx, defID)
	if err != nil {
		return Publication{}, err
	}
	report, err := s.DraftReport(ctx, def, baseline)
	if err != nil {
		return Publication{}, err
	}
	publisher, err := s.Account(ctx, actorID)
	if err != nil {
		return Publication{}, err
	}
	frozen, err := json.Marshal(snapshot{Report: report, PublishedBy: &publisher})
	if err != nil {
		return Publication{}, fmt.Errorf("freeze report: %w", err)
	}
	var row db.ReportPublication
	if err := s.WithinTx(ctx, func(tx *Service) error {
		if row, err = tx.queries.CreateReportPublication(ctx, db.CreateReportPublicationParams{
			ReportDefinitionID: def.ID,
			PublishedBy:        actorID,
			PublishedAt:        s.clock.Now().Format(timeFormat),
			Snapshot:           string(frozen),
		}); err != nil {
			return fmt.Errorf("create publication: %w", err)
		}
		// The narrative is frozen now; the next publication's starts empty.
		return tx.clearNarrative(ctx, def.ID)
	}); err != nil {
		return Publication{}, err
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
	var snap snapshot
	if err := json.Unmarshal([]byte(row.Snapshot), &snap); err != nil {
		return Publication{}, fmt.Errorf("read publication %d: %w", row.ID, err)
	}
	by, err := s.publisher(ctx, row.PublishedBy, snap)
	if err != nil {
		return Publication{}, err
	}
	publishedAt, _ := time.Parse(timeFormat, row.PublishedAt)
	return Publication{
		ID:           row.ID,
		DefinitionID: row.ReportDefinitionID,
		PublishedBy:  by,
		PublishedAt:  publishedAt,
		Report:       snap.Report,
	}, nil
}

// publisher is who published snap, as they were shown when it was published.
func (s *Service) publisher(ctx context.Context, id int64, snap snapshot) (Account, error) {
	if snap.PublishedBy != nil {
		return *snap.PublishedBy, nil
	}
	// Frozen before publishers were: show them as the snapshot shows its
	// people, as frozen there if they are among them.
	people := snap.people()
	for _, p := range people {
		if p.ID == id {
			return p, nil
		}
	}
	row, err := s.queries.GetAccount(ctx, id)
	if err != nil {
		return Account{}, fmt.Errorf("look up publisher: %w", err)
	}
	by := accountFromRow(row)
	// A snapshot from before Names shows its people by email, its Name set to
	// it (see Account.UnmarshalJSON); show the publisher the same way.
	for _, p := range people {
		if p.Email != "" && p.Name == p.Email {
			by.Name = by.Email
			break
		}
	}
	return by, nil
}

// people is every person the Report shows, as it shows them.
func (r Report) people() []Account {
	var out []Account
	for _, a := range r.ActionItems {
		out = append(out, a.Owner, a.CreatedBy)
	}
	for _, b := range r.Exceptions {
		out = append(out, b.Goal.Owner)
	}
	for _, sg := range r.Lines {
		out = append(out, sg.Goal.Owner)
	}
	for _, n := range r.Narrative {
		for _, h := range n.Highlights {
			out = append(out, h.Highlight.Owner)
		}
	}
	return out
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
