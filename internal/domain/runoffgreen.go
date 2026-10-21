package domain

import (
	"context"
	"strconv"
	"time"
)

// RunOffGreen is how long an Active Goal has been Yellow or Red: "How long an
// Active Goal has been Yellow or Red without a Green Check-in in between,
// counted in weeks of the Owner's Timezone. Moving between Red and Yellow
// doesn't end it; a Green Check-in does. Shown beside the Health ("Red · 5
// wks"), never changing it" (CONTEXT.md: Run off Green). The zero value is no
// run.
type RunOffGreen struct {
	// Since is when the run began: the CreatedAt of its first Yellow or Red
	// Check-in, exact to the instant. It is the zero time when there is no run.
	Since time.Time
	// Weeks counts the calendar weeks, Monday to Sunday in the Goal's
	// timezone (see Service.GoalTimezone), from the one the run began in to
	// this one, both included: a Goal that turned Red today is 1 week off
	// Green. It is 0 when there is no run.
	Weeks int
}

// Running reports whether the Goal is in a Run off Green.
func (r RunOffGreen) Running() bool { return !r.Since.IsZero() }

// Label is the run as it shows beside the Health ("5 wks"), "" when there is
// none.
func (r RunOffGreen) Label() string {
	if !r.Running() {
		return ""
	}
	return RunOffGreenLabel(r.Weeks)
}

// RunOffGreenLabel words a Run off Green of weeks: "1 wk", otherwise "N wks",
// and "" for 0, a Goal not off Green. It is how RunOffGreen.Label and a
// Report block's frozen RunOffGreenWeeks both read.
func RunOffGreenLabel(weeks int) string {
	switch {
	case weeks <= 0:
		return ""
	case weeks == 1:
		return "1 wk"
	}
	return strconv.Itoa(weeks) + " wks"
}

// RunOffGreen reads goalID's Run off Green (CONTEXT.md: Run off Green).
func (s *Service) RunOffGreen(ctx context.Context, goalID int64) (RunOffGreen, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return RunOffGreen{}, err
	}
	checkins, err := s.ListCheckins(ctx, goalID)
	if err != nil {
		return RunOffGreen{}, err
	}
	return s.runOffGreen(g, checkins), nil
}

// RunOffGreenByGoal reads the Run off Green of each of goals, as RunOffGreen
// reads one Goal's, keyed by Goal ID. It runs one query however many Goals
// there are, so a page listing Goals can afford it, and none for no Goals.
func (s *Service) RunOffGreenByGoal(ctx context.Context, goals []Goal) (map[int64]RunOffGreen, error) {
	ids := make([]int64, 0, len(goals))
	for _, g := range goals {
		ids = append(ids, g.ID)
	}
	checkins, err := s.checkinsByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]RunOffGreen, len(goals))
	for _, g := range goals {
		out[g.ID] = s.runOffGreen(g, checkins[g.ID])
	}
	return out, nil
}

// runOffGreen is g's Run off Green read from its Check-ins, newest first. There
// is a run only while g is Active and its latest Check-in is Yellow or Red. It
// began at the first Yellow or Red Check-in after the latest Green one, or
// the first Check-in ever when none was Green; a Check-in with no Health, one
// that took the Goal out of Active, neither begins nor ends it.
func (s *Service) runOffGreen(g Goal, checkins []Checkin) RunOffGreen {
	if g.Lifecycle != LifecycleActive || len(checkins) == 0 || !needsPathToGreen(checkins[0].Health) {
		return RunOffGreen{}
	}
	var since time.Time
	for _, c := range checkins {
		if c.Health == HealthGreen {
			break
		}
		if c.Health != "" {
			since = c.CreatedAt
		}
	}
	loc := s.GoalTimezone(g)
	began, now := runOffGreenWeekStart(orgDate(since, loc)), runOffGreenWeekStart(orgDate(s.clock.Now(), loc))
	return RunOffGreen{Since: since, Weeks: int(now.Sub(began)/(7*24*time.Hour)) + 1}
}

// runOffGreenWeekStart is the Monday that opens the calendar week holding
// date, a calendar date as orgDate gives one.
func runOffGreenWeekStart(date time.Time) time.Time {
	return date.AddDate(0, 0, -(int(date.Weekday())+6)%7)
}
