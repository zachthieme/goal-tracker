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
	view, err := s.goalsListView(r)
	if err != nil {
		http.Error(w, "could not list goals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, goalsPage(&current, view))
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

	// htmx swaps the Goal list in place; a plain form post reloads the page. The
	// swap re-renders the default flat list — the propose form carries no
	// filter or grouping to preserve.
	if r.Header.Get("HX-Request") == "true" {
		goals, err := s.svc.ListGoalsWithValues(r.Context())
		if err != nil {
			http.Error(w, "could not list goals", http.StatusInternalServerError)
			return
		}
		render(w, r, http.StatusOK, goalList(goalsListData{Goals: goals}))
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

// goalsListData is everything the Goal list renders: the Goals (filtered), the
// Dimensions offered for filtering and grouping, which values are currently
// selected, and — when grouping — the Goals bucketed by a Dimension's values.
type goalsListData struct {
	Goals      []domain.GoalWithValues
	Dimensions []domain.Dimension
	Selected   map[int64]bool
	GroupID    int64
	Groups     []domain.GoalGroup
}

// goalsListView reads the Goal list's filter (?value=) and grouping (?group=)
// from the request, then loads, filters, and (optionally) groups the Goals.
func (s *Server) goalsListView(r *http.Request) (goalsListData, error) {
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		return goalsListData{}, err
	}
	goals, err := s.svc.ListGoalsWithValues(r.Context())
	if err != nil {
		return goalsListData{}, err
	}

	// Resolve each ?value= to its Dimension, so filtering ORs within a Dimension
	// and ANDs across them.
	dimOfValue := map[int64]int64{}
	for _, d := range dims {
		for _, v := range d.Values {
			dimOfValue[v.ID] = d.ID
		}
	}
	selected := map[int64]bool{}
	byDimension := map[int64][]int64{}
	for _, raw := range r.URL.Query()["value"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if dimID, ok := dimOfValue[id]; ok {
			selected[id] = true
			byDimension[dimID] = append(byDimension[dimID], id)
		}
	}

	view := goalsListData{
		Goals:      domain.FilterGoals(goals, byDimension),
		Dimensions: dims,
		Selected:   selected,
	}
	if raw := r.URL.Query().Get("group"); raw != "" {
		if groupID, err := strconv.ParseInt(raw, 10, 64); err == nil {
			for _, d := range dims {
				if d.ID == groupID {
					view.GroupID = groupID
					view.Groups = domain.GroupGoalsByDimension(view.Goals, d)
				}
			}
		}
	}
	return view, nil
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
	dimensions, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		http.Error(w, "could not load dimensions", http.StatusInternalServerError)
		return
	}
	values, err := s.svc.GoalValues(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load dimension values", http.StatusInternalServerError)
		return
	}
	checkins, err := s.svc.ListCheckins(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load check-ins", http.StatusInternalServerError)
		return
	}
	latest, ok, err := s.svc.LatestCheckin(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load latest check-in", http.StatusInternalServerError)
		return
	}
	var latestPtr *domain.Checkin
	if ok {
		latestPtr = &latest
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
		Dimensions:    dimensions,
		Values:        values,
		Checkins:      checkins,
		LatestCheckin: latestPtr,
		SuggestedDate: domain.SuggestDeliveryDate(s.svc.Now()).Format(dateLayout),
	}))
}

// goalView is everything the single-Goal page renders: the Goal itself, its
// graph links, and the Milestones, Metrics, Contributors, and So What history an
// Owner fills in before activating it.
type goalView struct {
	Goal         domain.Goal
	Parents      []domain.GoalLink
	Children     []domain.GoalLink
	Candidates   []domain.Goal
	Milestones   []domain.Milestone
	Metrics      []domain.Metric
	Contributors []domain.Account
	Revisions    []domain.SoWhatRevision
	// Dimensions are all defined Dimensions, for the value-assignment selects and
	// the defaults offered when creating a child Goal. Values are the values this
	// Goal currently carries, retired ones included so they stay readable.
	Dimensions []domain.Dimension
	Values     []domain.DimensionValue
	// Checkins is the Goal's Check-in history (newest first) and LatestCheckin is
	// the most recent one, carrying the Goal's current Health, status, and Path to
	// Green. LatestCheckin is nil when the Goal has no Check-ins yet.
	Checkins      []domain.Checkin
	LatestCheckin *domain.Checkin
	SuggestedDate string
}

// assignedValue returns the value this Goal carries in the given Dimension, or
// nil if it carries none — used to show the current assignment and preselect the
// assignment control.
func (v goalView) assignedValue(dimensionID int64) *domain.DimensionValue {
	for i := range v.Values {
		if v.Values[i].DimensionID == dimensionID {
			return &v.Values[i]
		}
	}
	return nil
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

// handleAssignGoalValue assigns a Dimension value to the Goal, replacing any
// value it already carries in the same Dimension (CONTEXT.md: Owners assign
// Dimension values to their Goals). An empty selection is a no-op.
func (s *Server) handleAssignGoalValue(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	raw := r.FormValue("value_id")
	if raw == "" {
		writeCommandResult(w, r, id, nil)
		return
	}
	valueID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		http.Error(w, "invalid value", http.StatusUnprocessableEntity)
		return
	}
	writeCommandResult(w, r, id, s.svc.AssignGoalValue(r.Context(), id, valueID))
}

// handleCreateChildGoal creates a Goal under the parent in the path: it is owned
// by the current Account, carries whichever of the parent's values were kept as
// defaults, and requests a "contributes to" link to the parent. The parent's
// values are offered as defaults but not inherited — only the kept ones are
// assigned (CONTEXT.md: Contributes to; the parent's values are offered as
// defaults).
func (s *Server) handleCreateChildGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	parentID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	child, err := s.svc.CreateGoal(r.Context(), domain.CreateGoalInput{
		Title:   r.FormValue("title"),
		SoWhat:  r.FormValue("so_what"),
		OwnerID: current.ID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "could not create child goal", http.StatusInternalServerError)
		return
	}
	for _, raw := range r.Form["value_id"] {
		valueID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if err := s.svc.AssignGoalValue(r.Context(), child.ID, valueID); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}
	if _, err := s.svc.RequestLink(r.Context(), domain.RequestLinkInput{
		ChildID:     child.ID,
		ParentID:    parentID,
		RequesterID: current.ID,
	}); err != nil {
		if errors.Is(err, domain.ErrValidation) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "could not link child goal", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(child.ID, 10), http.StatusSeeOther)
}
