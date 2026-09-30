package domain

import (
	"context"
	"time"
)

// Report is the draft Report a Report Definition produces, read against a
// baseline (CONTEXT.md: Report). Exceptions get the full MBR block; every other
// selected Goal takes one line.
type Report struct {
	Definition ReportDefinition
	// Baseline is the date changes are read against: the reader's choice, or 30
	// days ago by default. It is a calendar date in the org's timezone.
	Baseline   time.Time
	Exceptions []ReportBlock
	Lines      []SelectedGoal
}

// ReportBlock is one exception Goal's full MBR block.
type ReportBlock struct {
	Goal   Goal
	Health string
}

// DraftReport drafts the Report for def against baseline. A zero baseline
// takes the default, 30 days before today.
func (s *Service) DraftReport(ctx context.Context, def ReportDefinition, baseline time.Time) (Report, error) {
	selected, err := s.SelectGoals(ctx, def)
	if err != nil {
		return Report{}, err
	}
	r := Report{Definition: def, Baseline: baseline}
	for _, sg := range selected {
		if sg.Health == HealthRed || sg.Health == HealthYellow {
			r.Exceptions = append(r.Exceptions, ReportBlock{Goal: sg.Goal, Health: sg.Health})
			continue
		}
		r.Lines = append(r.Lines, sg)
	}
	return r, nil
}
