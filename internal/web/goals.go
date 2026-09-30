package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// dateLayout is the format the HTML date input (<input type="date">) submits and
// that the domain's calendar dates round-trip through.
const dateLayout = "2006-01-02"

func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		http.Error(w, "could not list goals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, goalsPage(&current, goals))
}

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	_, err := s.svc.CreateGoal(r.Context(), domain.CreateGoalInput{
		Title:   r.FormValue("title"),
		SoWhat:  r.FormValue("so_what"),
		OwnerID: current.ID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "could not create goal", http.StatusInternalServerError)
		return
	}

	// htmx swaps the Goal list in place; a plain form post reloads the page.
	if r.Header.Get("HX-Request") == "true" {
		goals, err := s.svc.ListGoals(r.Context())
		if err != nil {
			http.Error(w, "could not list goals", http.StatusInternalServerError)
			return
		}
		render(w, r, http.StatusOK, goalList(goals))
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

func (s *Server) handleViewGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, err := s.svc.ViewGoal(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}

	parents, err := s.svc.ParentLinks(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load parents", http.StatusInternalServerError)
		return
	}
	children, err := s.svc.ChildLinks(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load children", http.StatusInternalServerError)
		return
	}
	// Candidate parents to contribute to: every other Goal. The domain rejects
	// self-links, duplicates, and cycles when the request is actually made.
	all, err := s.svc.ListGoals(r.Context())
	if err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	candidates := make([]domain.Goal, 0, len(all))
	for _, c := range all {
		if c.ID != g.ID {
			candidates = append(candidates, c)
		}
	}

	milestones, err := s.svc.ListMilestones(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load milestones", http.StatusInternalServerError)
		return
	}
	metrics, err := s.svc.ListMetrics(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load metrics", http.StatusInternalServerError)
		return
	}
	contributors, err := s.svc.ListContributors(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load contributors", http.StatusInternalServerError)
		return
	}
	revisions, err := s.svc.ListSoWhatRevisions(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load So What history", http.StatusInternalServerError)
		return
	}

	render(w, r, http.StatusOK, goalPage(&current, goalView{
		Goal:          g,
		Parents:       parents,
		Children:      children,
		Candidates:    candidates,
		Milestones:    milestones,
		Metrics:       metrics,
		Contributors:  contributors,
		Revisions:     revisions,
		SuggestedDate: domain.SuggestDeliveryDate(s.svc.Now()).Format(dateLayout),
	}))
}

// goalView is everything the single-Goal page renders: the Goal itself, its
// graph links, and the Milestones, Metrics, Contributors, and So What history an
// Owner fills in before activating it.
type goalView struct {
	Goal          domain.Goal
	Parents       []domain.GoalLink
	Children      []domain.GoalLink
	Candidates    []domain.Goal
	Milestones    []domain.Milestone
	Metrics       []domain.Metric
	Contributors  []domain.Account
	Revisions     []domain.SoWhatRevision
	SuggestedDate string
}

// goalIDFromPath parses the {id} path value, writing a 404 and returning ok
// false when it is not a number.
func goalIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// fmtDate renders a calendar date for a date input, or "" for the zero time.
func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

// fmtNum renders a Metric's baseline or target without trailing zeros.
func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseDate reads a date form field in the HTML date-input format. An empty
// field is the zero time, which the domain treats as "no date".
func parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(dateLayout, value)
}

// writeCommandResult redirects back to the Goal on success and surfaces a
// validation error (e.g. an activation gate failure) as 422 with its message.
func writeCommandResult(w http.ResponseWriter, r *http.Request, goalID int64, err error) {
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "could not update goal", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}

func (s *Server) handleMarkGoalDated(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	date, err := parseDate(r.FormValue("delivery_date"))
	if err != nil {
		http.Error(w, "invalid delivery date", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.svc.MarkGoalDated(r.Context(), id, date)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleMarkGoalOngoing(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.MarkGoalOngoing(r.Context(), id)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleSetCadence(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	days, err := strconv.Atoi(r.FormValue("cadence_days"))
	if err != nil {
		http.Error(w, "invalid cadence", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.svc.SetCadence(r.Context(), id, days)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleEditSoWhat(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.EditSoWhat(r.Context(), id, r.FormValue("so_what"), current.ID)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleAddContributor(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.AddContributorByEmail(r.Context(), id, r.FormValue("email"))
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleAddMilestone(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	date, err := parseDate(r.FormValue("target_date"))
	if err != nil {
		http.Error(w, "invalid milestone date", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.svc.AddMilestone(r.Context(), domain.AddMilestoneInput{
		GoalID:     id,
		Name:       r.FormValue("name"),
		TargetDate: date,
	})
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleEditMilestone(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	date, err := parseDate(r.FormValue("target_date"))
	if err != nil {
		http.Error(w, "invalid milestone date", http.StatusUnprocessableEntity)
		return
	}
	m, err := s.svc.EditMilestone(r.Context(), domain.EditMilestoneInput{
		MilestoneID: id,
		Name:        r.FormValue("name"),
		TargetDate:  date,
	})
	if err != nil {
		writeCommandResult(w, r, id, err)
		return
	}
	writeCommandResult(w, r, m.GoalID, nil)
}

func (s *Server) handleAddMetric(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	in, err := metricInputFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	in.GoalID = id
	_, err = s.svc.AddMetric(r.Context(), in)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleEditMetric(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	in, err := metricInputFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	m, err := s.svc.EditMetric(r.Context(), domain.EditMetricInput{
		MetricID:   id,
		Name:       in.Name,
		Unit:       in.Unit,
		Direction:  in.Direction,
		Baseline:   in.Baseline,
		Target:     in.Target,
		TargetDate: in.TargetDate,
	})
	if err != nil {
		writeCommandResult(w, r, id, err)
		return
	}
	writeCommandResult(w, r, m.GoalID, nil)
}

// metricInputFromForm reads the numeric and date fields common to adding and
// editing a Metric. The GoalID is filled in by the caller.
func metricInputFromForm(r *http.Request) (domain.AddMetricInput, error) {
	baseline, err := strconv.ParseFloat(r.FormValue("baseline"), 64)
	if err != nil {
		return domain.AddMetricInput{}, errors.New("invalid baseline")
	}
	target, err := strconv.ParseFloat(r.FormValue("target"), 64)
	if err != nil {
		return domain.AddMetricInput{}, errors.New("invalid target")
	}
	date, err := parseDate(r.FormValue("target_date"))
	if err != nil {
		return domain.AddMetricInput{}, errors.New("invalid target date")
	}
	return domain.AddMetricInput{
		Name:       r.FormValue("name"),
		Unit:       r.FormValue("unit"),
		Direction:  r.FormValue("direction"),
		Baseline:   baseline,
		Target:     target,
		TargetDate: date,
	}, nil
}

func (s *Server) handleActivateGoal(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.ActivateGoal(r.Context(), id)
	writeCommandResult(w, r, id, err)
}
