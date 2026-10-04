package web

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// goalFormView is the New goal page: what was typed and, after a refused
// submit, each problem. A problem names the input it refuses by its form name
// — the name the domain's InputError carries — or none when it is the whole
// submit's.
type goalFormView struct {
	Title      string
	SoWhat     string
	Milestones []milestoneRow
	Metrics    []metricRow
	Problems   []*domain.InputError
}

// milestoneRow is one Milestone row of the New goal form, as typed.
type milestoneRow struct {
	Name string
	Date string
}

// metricRow is one Metric row of the New goal form, as typed. Direction is the
// Direction select's choice, which counts only when Baseline and Target are
// equal.
type metricRow struct {
	Name       string
	Unit       string
	Baseline   string
	Target     string
	Direction  string
	TargetDate string
}

// bad is why the named input was refused, or "" when it wasn't.
func (v goalFormView) bad(input string) string {
	for _, p := range v.Problems {
		if p.Input == input {
			return p.Message
		}
	}
	return ""
}

// milestoneRows are the Milestone rows to show: those typed, or one blank row
// when there are none.
func (v goalFormView) milestoneRows() []milestoneRow {
	if len(v.Milestones) == 0 {
		return []milestoneRow{{}}
	}
	return v.Milestones
}

// metricRows are the Metric rows to show: those typed, or one blank row when
// there are none.
func (v goalFormView) metricRows() []metricRow {
	if len(v.Metrics) == 0 {
		return []metricRow{{}}
	}
	return v.Metrics
}

// rowNames names the i-th row's inputs by the domain's names for them
// (domain.MilestoneInput, domain.MetricInput).
func rowNames(input func(i int, part string) string, i int) func(part string) string {
	return func(part string) string { return input(i, part) }
}

// templateRowNames names a <template> row's inputs, section[__i__].part, for
// script to number when it adds the row.
func templateRowNames(section string) func(part string) string {
	return func(part string) string { return section + "[__i__]." + part }
}

// handleNewGoalForm renders the New goal page, empty.
func (s *Server) handleNewGoalForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	render(w, r, http.StatusOK, goalFormPage(&current, goalFormView{}))
}

// errUnparsed rolls back a New goal submit the domain accepted when the
// handler refused some of its values.
var errUnparsed = errors.New("the New goal form has values that don't parse")

// handleCreateDefinedGoal submits the New goal page: the Goal is created as
// defined and the post lands on its page, which is the confirmation, so there
// is no toast. A refused submit comes back as the form, 422, as typed, with
// every problem listed at the top and marked on its input. A value the
// handler can't parse is refused the same way, under its input's name, in the
// same 422 as the domain's problems.
func (s *Server) handleCreateDefinedGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v := goalFormView{Title: r.FormValue("title"), SoWhat: r.FormValue("so_what")}
	in := domain.DefinedGoalInput{
		Title:   v.Title,
		SoWhat:  v.SoWhat,
		OwnerID: current.ID,
	}
	var unparsed []*domain.InputError
	refuse := func(input, format string, args ...any) {
		unparsed = append(unparsed, &domain.InputError{Input: input, Message: fmt.Sprintf(format, args...)})
	}
	date := func(input, value string) time.Time {
		d, err := parseDate(strings.TrimSpace(value))
		if err != nil {
			refuse(input, "%q isn't a date", value)
		}
		return d
	}
	number := func(input, what, value string) (float64, bool) {
		value = strings.TrimSpace(value)
		if value == "" {
			refuse(input, "a Metric needs a %s", what)
			return 0, false
		}
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			refuse(input, "the %s must be a number, not %q", what, value)
			return 0, false
		}
		return n, true
	}

	// Rows are numbered afresh, all-blank ones dropped, so the domain's
	// problems and the page shown again name the same inputs.
	for i, row := range formRows(r.PostForm, "milestones") {
		m := milestoneRow{Name: row["name"], Date: row["date"]}
		v.Milestones = append(v.Milestones, m)
		in.Milestones = append(in.Milestones, domain.MilestoneDefinition{Name: m.Name, Date: date(domain.MilestoneInput(i, "date"), m.Date)})
	}
	for i, row := range formRows(r.PostForm, "metrics") {
		m := metricRow{Name: row["name"], Unit: row["unit"], Baseline: row["baseline"], Target: row["target"], Direction: row["direction"], TargetDate: row["target_date"]}
		v.Metrics = append(v.Metrics, m)
		baseline, baselineOK := number(domain.MetricInput(i, "baseline"), "baseline", m.Baseline)
		target, targetOK := number(domain.MetricInput(i, "target"), "target", m.Target)
		in.Metrics = append(in.Metrics, domain.MetricDefinition{
			Name:       m.Name,
			Unit:       m.Unit,
			Direction:  metricDirection(baseline, target, baselineOK && targetOK, m.Direction),
			Baseline:   baseline,
			Target:     target,
			TargetDate: date(domain.MetricInput(i, "target_date"), m.TargetDate),
		})
	}

	// The domain checks what parsed even when something didn't, so every
	// problem comes back together; with any unparsed, nothing it wrote is
	// kept. Defining a Goal sends no email, so rolling it back recalls it all.
	var g domain.Goal
	err := s.svc.WithinTx(r.Context(), func(tx *domain.Service) error {
		var err error
		g, err = tx.CreateDefinedGoal(r.Context(), in)
		if err == nil && len(unparsed) > 0 {
			return errUnparsed
		}
		return err
	})
	if err == nil {
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", g.ID), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, domain.ErrValidation) && !errors.Is(err, errUnparsed) {
		http.Error(w, "could not create goal", http.StatusInternalServerError)
		return
	}
	v.Problems = goalFormProblems(err, unparsed)
	render(w, r, http.StatusUnprocessableEntity, goalFormPage(&current, v))
}

// metricDirection is a Metric row's direction: down when the target is below
// the baseline, up when above, and the Direction select's choice when they're
// equal (a hold-steady Metric). When either doesn't parse it can't be told,
// and the row is refused for that alone.
func metricDirection(baseline, target float64, parsed bool, chosen string) string {
	switch {
	case !parsed:
		return domain.MetricUp
	case target < baseline:
		return domain.MetricDown
	case target > baseline:
		return domain.MetricUp
	}
	return chosen
}

// goalFormProblems are a refused submit's problems in the order the page
// shows its inputs: the domain's, each displaced by the handler's under the
// same input, since a value that didn't parse reached the domain as blank.
func goalFormProblems(err error, unparsed []*domain.InputError) []*domain.InputError {
	refused := map[string]bool{}
	for _, p := range unparsed {
		refused[p.Input] = true
	}
	var problems []*domain.InputError
	for _, p := range domain.InputErrors(err) {
		if !refused[p.Input] {
			problems = append(problems, p)
		}
	}
	problems = append(problems, unparsed...)
	if len(problems) == 0 {
		problems = []*domain.InputError{{Message: strings.TrimPrefix(err.Error(), domain.ErrValidation.Error()+": ")}}
	}
	slices.SortStableFunc(problems, func(a, b *domain.InputError) int {
		return slices.Compare(goalFormPlace(a.Input), goalFormPlace(b.Input))
	})
	return problems
}

// goalFormPlace is where an input sits on the New goal page, to sort its
// problems by: Title, So What, then each Milestone row's inputs and each
// Metric row's, in turn. Any other input sorts after those.
func goalFormPlace(input string) []int {
	switch input {
	case domain.InputTitle:
		return []int{0}
	case domain.InputSoWhat:
		return []int{1}
	}
	sections := []struct {
		name  string
		parts []string
	}{
		{"milestones", []string{"name", "date"}},
		{"metrics", []string{"name", "unit", "baseline", "target", "direction", "target_date"}},
	}
	for s, section := range sections {
		var i int
		var part string
		if _, err := fmt.Sscanf(input, section.name+"[%d].%s", &i, &part); err == nil {
			return []int{2 + s, i, slices.Index(section.parts, part)}
		}
	}
	return []int{2 + len(sections)}
}

// formRows gathers a repeatable section's rows, posted as section[i].part,
// in the order of their i, leaving out each row whose every part is blank. A
// row's i is only its place: rows added and removed by script leave gaps.
func formRows(form url.Values, section string) []map[string]string {
	byIndex := map[int]map[string]string{}
	for name, values := range form {
		rest, ok := strings.CutPrefix(name, section+"[")
		if !ok {
			continue
		}
		index, part, ok := strings.Cut(rest, "].")
		if !ok {
			continue
		}
		i, err := strconv.Atoi(index)
		if err != nil || i < 0 {
			continue
		}
		if byIndex[i] == nil {
			byIndex[i] = map[string]string{}
		}
		byIndex[i][part] = values[0]
	}
	var rows []map[string]string
	for _, i := range slices.Sorted(maps.Keys(byIndex)) {
		row := byIndex[i]
		blank := true
		for _, value := range row {
			if strings.TrimSpace(value) != "" {
				blank = false
			}
		}
		if !blank {
			rows = append(rows, row)
		}
	}
	return rows
}
