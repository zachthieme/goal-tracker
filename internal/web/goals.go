package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// dateLayout is the format the HTML date input (<input type="date">) submits and
// that the domain's calendar dates round-trip through.
const dateLayout = "2006-01-02"

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	_, err := s.svc.CreateGoal(r.Context(), current.ID, domain.CreateGoalInput{
		Title:        r.FormValue("title"),
		WhyItMatters: r.FormValue("why_it_matters"),
		OwnerID:      current.ID,
	})
	if errors.Is(err, domain.ErrNotAuthorized) { // #701
		s.serverError(w, r, "could not create goal", err)
		return
	}
	if err != nil {
		// No Goal exists yet, so the way back is the new Goal form.
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't create Goal", Back: "/goals/new"})
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

// goalIDFromPath parses the {id} path value, writing the Not found page and
// returning ok false when it is not a number.
func (s *Server) goalIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

// fmtDate renders a calendar date as ISO, or "" for the zero time: for a date
// input's value, a value compared as a string, or a query string. A page shows
// a date through dates.go instead (DESIGN.md: Dates and times).
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

// goalPageForm is form on the Goal page of goalID, as current sees it:
// refused for what it sent, it redirects to the Goal page's own address, where
// that form is open, as it was left, saying why above it in one sentence
// (#687). When the page no longer offers the viewer that form, such as Add
// Milestone once the Goal is Done, there is nowhere beside it to say why, so
// the refusal page says it (onPage).
func (s *Server) goalPageForm(goalID int64, current domain.Account, form goalForm) *formOnPage {
	return onPage(goalPath(goalID), string(form), 0, form.fields()).inOneSentence().
		offeredWhen(func(ctx context.Context) (bool, error) {
			view, err := s.goalPageView(ctx, goalID, current)
			return err == nil && view.offers(form), err
		})
}

// answerGoalCommand answers a command on the Goal goalID from its page:
// done, it returns to the Goal page; refused with err, it is answerRefused's,
// headed Can't change Goal and linking Back to the Goal, form coming back in
// place when it is set.
func (s *Server) answerGoalCommand(w http.ResponseWriter, r *http.Request, goalID int64, form refusedForm, err error) {
	if err != nil {
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't change Goal", Back: goalPath(goalID), Form: form})
		return
	}
	http.Redirect(w, r, goalPath(goalID), http.StatusSeeOther)
}

// handleEditWhyItMatters saves the Edit title and Why it matters form, for the Goal's Owner
// or an Admin, as EditTitleAndWhyItMatters does: the title only when the form
// sends one.
func (s *Server) handleEditWhyItMatters(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	var title *string
	if _, sent := r.PostForm["title"]; sent {
		t := r.PostFormValue("title")
		title = &t
	}
	_, err := s.svc.EditTitleAndWhyItMatters(r.Context(), current.ID, id, title, r.PostFormValue("why_it_matters"))
	s.answerGoalCommand(w, r, id, s.goalPageForm(id, current, formTitleWhyItMatters), err)
}

// handleAddMilestone adds a Milestone from the Goal page's Add Milestone form,
// as its Owner or a Delegate. A date that doesn't parse is sent on unparsed,
// so AddMilestoneAsAuthor refuses it beside the form only once it has checked
// who sent it, as handleEditMilestone's is.
func (s *Server) handleAddMilestone(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	// Parsed before the input is built, so Unparsed holds every append.
	var unparsed formProblems
	targetDate := unparsed.date("target_date", r.FormValue("target_date"), "")
	_, err := s.svc.AddMilestoneAsAuthor(r.Context(), current.ID, domain.AddMilestoneInput{
		GoalID:     id,
		Name:       r.FormValue("name"),
		TargetDate: targetDate,
		Unparsed:   unparsed,
	})
	s.answerGoalCommand(w, r, id, s.goalPageForm(id, current, formMilestones), err)
}

// handleEditMilestone saves a Milestone's Edit form on the Goal page. A date
// that doesn't parse is sent on unparsed, so EditMilestone refuses it only
// once it has checked who sent it. A refusal returns no Milestone, so it is
// read again for the refusal page to link Back to its Goal, or Home when it
// can't be read; the path's id is a Milestone's, not a Goal's.
func (s *Server) handleEditMilestone(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	// Parsed before the input is built, so Unparsed holds every append.
	var unparsed formProblems
	targetDate := unparsed.date("target_date", r.FormValue("target_date"), "")
	m, err := s.svc.EditMilestone(r.Context(), current.ID, domain.EditMilestoneInput{
		MilestoneID: id,
		Name:        r.FormValue("name"),
		TargetDate:  targetDate,
		Unparsed:    unparsed,
	})
	if err != nil {
		back := "/home"
		if milestone, lookErr := s.svc.Milestone(r.Context(), id); lookErr == nil {
			back = goalPath(milestone.GoalID)
		}
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't change Goal", Back: back})
		return
	}
	http.Redirect(w, r, goalPath(m.GoalID), http.StatusSeeOther)
}

// handleEditMetric saves a Metric's Edit form on the Goal page. A number or
// date that doesn't parse is sent on unparsed, so EditMetric refuses it only
// once it has checked who sent it. A refusal links Back to the Metric's Goal,
// read again as handleEditMilestone reads its Milestone. The Direction is set
// as the goal form sets a Metric row's (metricDirection): from the baseline
// and target, the posted one counting only when they're equal.
func (s *Server) handleEditMetric(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	// Parsed before the input is built, so Unparsed holds every append.
	var unparsed formProblems
	baseline, baselineOK := readMetricNumber(&unparsed, "baseline", "baseline", r.FormValue("baseline"))
	target, targetOK := readMetricNumber(&unparsed, "target", "target", r.FormValue("target"))
	targetDate := unparsed.date("target_date", r.FormValue("target_date"), "")
	m, err := s.svc.EditMetric(r.Context(), current.ID, domain.EditMetricInput{
		MetricID:   id,
		Name:       r.FormValue("name"),
		Unit:       r.FormValue("unit"),
		Direction:  metricDirection(baseline, target, baselineOK && targetOK, r.FormValue("direction")),
		Baseline:   baseline,
		Target:     target,
		TargetDate: targetDate,
		Unparsed:   unparsed,
	})
	if err != nil {
		back := "/home"
		if metric, lookErr := s.svc.Metric(r.Context(), id); lookErr == nil {
			back = goalPath(metric.GoalID)
		}
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't change Goal", Back: back})
		return
	}
	http.Redirect(w, r, goalPath(m.GoalID), http.StatusSeeOther)
}

// readMetricNumber reads input, a Metric's baseline or target (what) on the
// Goal page or the New goal form, as a number into unparsed. A blank one is refused, as one that doesn't parse is,
// and neither is read.
func readMetricNumber(unparsed *formProblems, input, what, typed string) (float64, bool) {
	if strings.TrimSpace(typed) == "" {
		unparsed.add(input, "a Metric needs a %s", what)
		return 0, false
	}
	return unparsed.number(input, typed, "the "+what)
}

func (s *Server) handleActivateGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.ActivateGoal(r.Context(), current.ID, id)
	s.answerGoalCommand(w, r, id, nil, err)
}

// handleAssignGoalValue sets the Goal's Dimension values (CONTEXT.md: Owners
// and their Delegates set a Goal's Dimension values). A form naming a
// dimension_id and a new_value, an Extendable Dimension's add-a-value input,
// sets the value so named, adding it to the list when it's new. One naming a
// dimension_id without a new_value, a several-values Dimension's checkboxes or
// a one-value Dimension's select, saves its chosen value_ids together, so an
// unchecked value is removed and the select's "None" clears the Dimension. A
// single value_id without a dimension_id replaces any value the Goal already
// carries in its Dimension, and an empty one is a no-op.
// Anyone but the Owner, a Delegate or an Admin is refused.
func (s *Server) handleAssignGoalValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	if rawDim := r.FormValue("dimension_id"); rawDim != "" {
		dimensionID, err := strconv.ParseInt(rawDim, 10, 64)
		if err != nil {
			http.Error(w, "invalid dimension", http.StatusUnprocessableEntity)
			return
		}
		edit := domain.DimensionValuesEdit{GoalID: id, DimensionID: dimensionID}
		if newValue, ok := r.Form["new_value"]; ok {
			edit.NewValue, edit.NameRequired, edit.Join = newValue[0], true, true
		} else {
			for _, raw := range r.Form["value_id"] {
				if raw == "" {
					continue
				}
				valueID, err := strconv.ParseInt(raw, 10, 64)
				if err != nil {
					http.Error(w, "invalid value", http.StatusUnprocessableEntity)
					return
				}
				edit.ValueIDs = append(edit.ValueIDs, valueID)
			}
		}
		_, err = s.svc.SetGoalDimensionValues(r.Context(), current.ID, edit)
		s.answerGoalCommand(w, r, id, s.goalPageForm(id, current, formDimensions), err)
		return
	}
	raw := r.FormValue("value_id")
	if raw == "" {
		http.Redirect(w, r, goalPath(id), http.StatusSeeOther)
		return
	}
	valueID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		http.Error(w, "invalid value", http.StatusUnprocessableEntity)
		return
	}
	s.answerGoalCommand(w, r, id, s.goalPageForm(id, current, formDimensions), s.svc.AssignGoalValue(r.Context(), current.ID, id, valueID))
}
