package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// ReportDefinition is a saved, reusable selection of Goals for a Report
// (CONTEXT.md: Report Definition). It selects Goals by root Goals traversed down
// the accepted contributes-to links to Depth, by Dimension or Owner filters, or
// by filters alone; and carries an introduction the Report's narrative opens
// with. The selection is computed on demand by SelectGoals (ADR-0004), so the
// definition stores the criteria, never the resulting Goal set.
type ReportDefinition struct {
	ID           int64
	Name         string
	Introduction string
	// RootIDs are the Goals traversal starts from; empty for a filter-only
	// definition, which draws from every Goal.
	RootIDs []int64
	// Depth is how many levels of accepted contributes-to links to descend from
	// each root. 0 selects the roots alone. It is ignored when RootIDs is empty.
	Depth int
	// OwnerFilterID keeps only Goals owned by this Account; 0 means no Owner
	// filter.
	OwnerFilterID int64
	// DimensionValueIDs are the Dimension values a selected Goal must match. Within
	// a Dimension they are OR'd, across Dimensions AND'd (the faceted filter of
	// FilterGoals). Empty means no Dimension filter.
	DimensionValueIDs []int64
	// FieldIDs are the Fields the Report shows beside each Goal that has a
	// value in them. Empty, the default, shows none.
	FieldIDs  []int64
	CreatedAt time.Time
}

// SaveReportDefinitionInput is the save-a-Report-Definition command's input.
type SaveReportDefinitionInput struct {
	Name              string
	Introduction      string
	RootIDs           []int64
	Depth             int
	OwnerFilterID     int64
	DimensionValueIDs []int64
	FieldIDs          []int64
}

// SelectedGoal is one Goal chosen by a Report Definition, carrying the fields the
// live draft shows one per line: the Goal (its title, Owner, and due date) and
// its current Health. Health is empty when the Goal has no Check-in yet — a
// Proposed Goal, or an Active one not yet checked in on (CONTEXT.md: Health).
// Fields are its values in the Fields the definition chose, filled in when the
// Report is drafted.
type SelectedGoal struct {
	Goal   Goal
	Health string
	Fields []FieldValue `json:",omitempty"`
}

// SaveReportDefinition saves a reusable Report Definition. Anyone signed in may
// save one (CONTEXT.md: Report Definition); actorID records who created it. A
// definition needs a name and must select something — root Goals, an Owner
// filter, or Dimension-value filters — so a definition that selects the whole org
// by accident is rejected. Depth cannot be negative. Every root Goal, the Owner
// filter, and each Dimension value must exist. Each chosen Field must exist and
// not be Retired, as a Retired Field is no longer offered (CONTEXT.md: Retired).
func (s *Service) SaveReportDefinition(ctx context.Context, actorID int64, in SaveReportDefinitionInput) (ReportDefinition, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ReportDefinition{}, fmt.Errorf("%w: a Report Definition needs a name", ErrValidation)
	}
	if len(in.RootIDs) == 0 && in.OwnerFilterID == 0 && len(in.DimensionValueIDs) == 0 {
		return ReportDefinition{}, fmt.Errorf("%w: a Report Definition needs root Goals or a filter", ErrValidation)
	}
	if in.Depth < 0 {
		return ReportDefinition{}, fmt.Errorf("%w: depth cannot be negative", ErrValidation)
	}

	roots := dedupeIDs(in.RootIDs)
	for _, id := range roots {
		if _, err := s.queries.GetGoal(ctx, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReportDefinition{}, fmt.Errorf("%w: root goal %d does not exist", ErrValidation, id)
			}
			return ReportDefinition{}, fmt.Errorf("look up root goal: %w", err)
		}
	}
	if in.OwnerFilterID != 0 {
		if _, err := s.queries.GetAccount(ctx, in.OwnerFilterID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReportDefinition{}, fmt.Errorf("%w: owner filter account does not exist", ErrValidation)
			}
			return ReportDefinition{}, fmt.Errorf("look up owner filter: %w", err)
		}
	}
	filters := dedupeIDs(in.DimensionValueIDs)
	for _, id := range filters {
		if _, err := s.queries.GetDimensionValue(ctx, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReportDefinition{}, fmt.Errorf("%w: dimension value %d does not exist", ErrValidation, id)
			}
			return ReportDefinition{}, fmt.Errorf("look up dimension value: %w", err)
		}
	}

	fields := dedupeIDs(in.FieldIDs)
	for _, id := range fields {
		row, err := s.queries.GetField(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReportDefinition{}, fmt.Errorf("%w: field %d does not exist", ErrValidation, id)
			}
			return ReportDefinition{}, fmt.Errorf("look up field: %w", err)
		}
		if f := fieldFromRow(row); f.Retired {
			return ReportDefinition{}, fmt.Errorf("%w: %s is retired, so a Report can't be set to show it", ErrValidation, f.Name)
		}
	}

	row, err := s.queries.CreateReportDefinition(ctx, db.CreateReportDefinitionParams{
		Name:          name,
		Introduction:  strings.TrimSpace(in.Introduction),
		Depth:         int64(in.Depth),
		OwnerFilterID: in.OwnerFilterID,
		CreatedBy:     actorID,
		CreatedAt:     s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return ReportDefinition{}, fmt.Errorf("create report definition: %w", err)
	}
	for _, id := range roots {
		if err := s.queries.AddReportDefinitionRoot(ctx, db.AddReportDefinitionRootParams{
			ReportDefinitionID: row.ID,
			GoalID:             id,
		}); err != nil {
			return ReportDefinition{}, fmt.Errorf("add report root: %w", err)
		}
	}
	for _, id := range filters {
		if err := s.queries.AddReportDefinitionFilter(ctx, db.AddReportDefinitionFilterParams{
			ReportDefinitionID: row.ID,
			DimensionValueID:   id,
		}); err != nil {
			return ReportDefinition{}, fmt.Errorf("add report filter: %w", err)
		}
	}
	for _, id := range fields {
		if err := s.queries.AddReportDefinitionField(ctx, db.AddReportDefinitionFieldParams{
			ReportDefinitionID: row.ID,
			FieldID:            id,
		}); err != nil {
			return ReportDefinition{}, fmt.Errorf("add report field: %w", err)
		}
	}
	return s.GetReportDefinition(ctx, row.ID)
}

// GetReportDefinition returns the Report Definition with the given id, its roots
// and filters resolved. It returns ErrNotFound if none exists.
func (s *Service) GetReportDefinition(ctx context.Context, id int64) (ReportDefinition, error) {
	row, err := s.queries.GetReportDefinition(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReportDefinition{}, fmt.Errorf("%w: report definition %d", ErrNotFound, id)
		}
		return ReportDefinition{}, fmt.Errorf("get report definition: %w", err)
	}
	return s.reportDefinitionFromRow(ctx, row)
}

// ListReportDefinitions returns every saved Report Definition, ordered by name,
// each with its roots and filters resolved.
func (s *Service) ListReportDefinitions(ctx context.Context) ([]ReportDefinition, error) {
	rows, err := s.queries.ListReportDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list report definitions: %w", err)
	}
	out := make([]ReportDefinition, 0, len(rows))
	for _, row := range rows {
		def, err := s.reportDefinitionFromRow(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, nil
}

func (s *Service) reportDefinitionFromRow(ctx context.Context, row db.ReportDefinition) (ReportDefinition, error) {
	roots, err := s.queries.ListReportDefinitionRoots(ctx, row.ID)
	if err != nil {
		return ReportDefinition{}, fmt.Errorf("list report roots: %w", err)
	}
	filters, err := s.queries.ListReportDefinitionFilters(ctx, row.ID)
	if err != nil {
		return ReportDefinition{}, fmt.Errorf("list report filters: %w", err)
	}
	fields, err := s.queries.ListReportDefinitionFields(ctx, row.ID)
	if err != nil {
		return ReportDefinition{}, fmt.Errorf("list report fields: %w", err)
	}
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	return ReportDefinition{
		ID:                row.ID,
		Name:              row.Name,
		Introduction:      row.Introduction,
		RootIDs:           roots,
		Depth:             int(row.Depth),
		OwnerFilterID:     row.OwnerFilterID,
		DimensionValueIDs: filters,
		FieldIDs:          fields,
		CreatedAt:         createdAt,
	}, nil
}

// SelectGoals returns the Goals a Report Definition selects, each once, with the
// fields the live draft lists per line (CONTEXT.md: Report Definition). It first
// gathers candidates — the roots traversed to Depth, or every Goal when there are
// no roots — then applies the Owner and Dimension filters after traversal. A Goal
// reached through several paths appears once.
func (s *Service) SelectGoals(ctx context.Context, def ReportDefinition) ([]SelectedGoal, error) {
	candidates, err := s.reportCandidates(ctx, def)
	if err != nil {
		return nil, err
	}

	// Group the Dimension-value filters by their Dimension to build the faceted
	// filter FilterGoals applies (values OR'd within a Dimension, AND'd across).
	selected := map[int64][]int64{}
	for _, valueID := range def.DimensionValueIDs {
		val, err := s.queries.GetDimensionValue(ctx, valueID)
		if err != nil {
			return nil, fmt.Errorf("look up filter value: %w", err)
		}
		selected[val.DimensionID] = append(selected[val.DimensionID], valueID)
	}

	var valuesByGoal map[int64][]DimensionValue
	if len(selected) > 0 {
		valuesByGoal, err = s.goalValuesByGoal(ctx)
		if err != nil {
			return nil, err
		}
	}

	out := make([]SelectedGoal, 0, len(candidates))
	for _, g := range candidates {
		if def.OwnerFilterID != 0 && g.Owner.ID != def.OwnerFilterID {
			continue
		}
		if len(selected) > 0 {
			gv := GoalWithValues{Goal: g, Values: valuesByGoal[g.ID]}
			if !goalMatchesFilter(gv, selected) {
				continue
			}
		}
		health := ""
		latest, ok, err := s.LatestCheckin(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			health = latest.Health
		}
		out = append(out, SelectedGoal{Goal: g, Health: health})
	}
	return out, nil
}

// reportCandidates returns the Goals a definition draws from before filtering:
// the roots traversed to Depth, or every Goal for a filter-only definition.
func (s *Service) reportCandidates(ctx context.Context, def ReportDefinition) ([]Goal, error) {
	if len(def.RootIDs) > 0 {
		return s.SelectDescendants(ctx, def.RootIDs, def.Depth)
	}
	return s.ListGoals(ctx)
}

// goalValuesByGoal returns every Goal's assigned Dimension values keyed by Goal
// id, for applying a Report Definition's Dimension filter across the candidates.
func (s *Service) goalValuesByGoal(ctx context.Context) (map[int64][]DimensionValue, error) {
	valRows, err := s.queries.ListAllGoalValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goal values: %w", err)
	}
	byGoal := make(map[int64][]DimensionValue, len(valRows))
	for _, r := range valRows {
		byGoal[r.GoalID] = append(byGoal[r.GoalID], dimensionValueFromRow(r.DimensionValue))
	}
	return byGoal, nil
}

// dedupeIDs returns ids with duplicates removed, keeping first-seen order.
func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
