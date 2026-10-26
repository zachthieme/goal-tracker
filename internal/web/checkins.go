package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleCheckinPage shows the Check-in form on its own page, with the Goal's
// latest Check-in's answers as ghost text. Only whoever may write a Check-in —
// the Owner or a Delegate — may open it (CONTEXT.md: Check-in, Delegate).
func (s *Server) handleCheckinPage(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	view, ok := s.loadCheckinView(w, r, goalID, current)
	if !ok {
		return
	}
	form := checkinFormFromLatest(view)
	if to, ok := lifecycleFromQuery(r, form.Lifecycle); ok {
		form.Lifecycle.To = to
	}
	form.OpenDates = r.URL.Query().Get("open") == "dates"
	render(w, r, http.StatusOK, checkinPage(&current, view, form))
}

// checkinView is what the Check-in page shows around its form, read through
// the Check-in page's loader (ADR 0010): what the loader read, why a No
// change would be refused, as a sentence ("" when one would be recorded or
// isn't on offer), and the clock the page shows its dates against.
type checkinView struct {
	domain.CheckinPage
	NoChangeReason string
	Shown          displayDates
}

// checksIn reports whether the page offers the form: the viewer may check in
// and the Goal takes Check-ins now, as domain.CheckinPermissions.CheckIn says.
func (v checkinView) checksIn() bool {
	return v.Permissions.CheckIn
}

// checkinView reads the Check-in page for goalID as current sees it, through
// its loader, once. A missing Goal comes back as domain.ErrNotFound.
func (s *Server) checkinView(ctx context.Context, goalID int64, current domain.Account) (checkinView, error) {
	p, err := s.svc.CheckinPage(ctx, current.ID, goalID)
	if err != nil {
		return checkinView{}, err
	}
	reason, err := s.noChangeSentence(p.NoChangeRefusal)
	if err != nil {
		return checkinView{}, err
	}
	return checkinView{CheckinPage: p, NoChangeReason: reason, Shown: displayDates{Now: p.Now, Loc: s.svc.Timezone()}}, nil
}

// loadCheckinView is checkinView for a request that shows or posts the
// Check-in form: a missing Goal gets the not-found page, and anyone who may
// not check in on it 403, before anything they sent is read (#528, #560).
// It reports whether the request may go on.
func (s *Server) loadCheckinView(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account) (checkinView, bool) {
	view, err := s.checkinView(r.Context(), goalID, current)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return checkinView{}, false
		}
		s.serverError(w, r, "could not load goal", err)
		return checkinView{}, false
	}
	if !view.CanCheckin {
		s.refuse(w, r, "Can't check in", goalPath(goalID), checkinForbidden(view.Goal))
		return checkinView{}, false
	}
	return view, true
}

// checkinForbidden is the refusal for someone who may not check in on goal,
// whether they opened its Check-in form or clicked No change.
func checkinForbidden(goal domain.Goal) error {
	return domain.Refuse(domain.ErrNotAuthorized, "only the Goal's Owner or a Delegate may check in on %s", goal.Title)
}

// noChangeSentence is why a No change would be refused, refusal, as a page
// says it, or "" for none. An error that isn't a refusal comes back as one.
func (s *Server) noChangeSentence(refusal error) (string, error) {
	switch {
	case errors.Is(refusal, domain.ErrValidation):
		return sentence(s.noChangeReason(refusal)), nil
	case refusal != nil:
		return "", fmt.Errorf("check no change: %w", refusal)
	}
	return "", nil
}

// lifecycleFromQuery is the Lifecycle change the Goal page's menu picked for the
// Check-in (?lifecycle=), when the form offers it and it isn't the form's
// default. Anything else is ignored, so the form opens as it would without it.
func lifecycleFromQuery(r *http.Request, l lifecycleFormData) (string, bool) {
	to := r.URL.Query().Get("lifecycle")
	if to == "" || to == l.defaultTo() {
		return "", false
	}
	for _, c := range l.choices() {
		if c.To == to {
			return to, true
		}
	}
	return "", false
}

// handleSubmitCheckin records a Check-in on the Goal in the path, written by the
// current Account. The form is htmx-driven: on a validation error the form is
// re-rendered in place with the message next to the field it is about (a plain
// post gets the whole Check-in page around it); on success htmx is told to
// reload the Goal page so the new history and current Health show.
func (s *Server) handleSubmitCheckin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	health := r.FormValue("health")
	// Confirm Green does what it says, whatever Health was picked after
	// the warning. Once pressed, a re-rendered form carries the confirmation
	// (off_target_confirmed) while Green is picked, so the Owner isn't asked
	// again after a later error or Add another.
	confirmPressed := r.FormValue("confirm_off_target") != ""
	if confirmPressed {
		health = domain.HealthGreen
	}
	offTargetConfirmed := confirmPressed || r.FormValue("off_target_confirmed") != ""
	status := r.FormValue("status")
	path := r.FormValue("path_to_green")
	explanation := r.FormValue("explanation")
	rawDate := r.FormValue("path_target_date")
	pathReason := r.FormValue("path_target_date_reason")
	asksForHelp := r.FormValue("asks_for_help") == "1"
	highlights := highlightsFromForm(r)
	drafts := draftHighlightsFromForm(r)

	// The form's context is read once, through the Check-in page's loader,
	// which also refuses anyone who may not check in. The reading inputs are
	// named reading_<metricID>, one per Metric on the Goal, and the delivery
	// date and Milestone fields are keyed to the Goal's current state: whether
	// it is Dated, and which Milestones are still Planned. SubmitCheckin reads
	// what it decides on again, inside the transaction that writes.
	view, ok := s.loadCheckinView(w, r, goalID, current)
	if !ok {
		return
	}
	metrics := view.Metrics
	// Values the reader typed, kept so an error re-render shows them again.
	rawReadings := rawReadingsFromForm(r, metrics)
	dates := datesFromForm(r, view.Goal, view.Milestones)
	lifecycle := lifecycleFromForm(r, view)

	// An error from reading the form already names its field; the domain's
	// name theirs (see checkinErrorField) and read as a sentence, without the
	// internal prefix.
	formData := func(err error) checkinFormData {
		var e checkinError
		if err != nil && !errors.As(err, &e) {
			e = checkinError{Field: checkinErrorField(err), Message: sentence(plainReasonShown(err, view.Shown))}
		}
		return view.withContext(checkinFormData{
			GoalID: goalID, Health: health, Status: status, PathToGreen: path, PathTargetDate: rawDate, PathTargetDateReason: pathReason, AsksForHelp: asksForHelp, Explanation: explanation,
			Readings: rawReadings, DraftHighlights: drafts, Highlights: highlights, Dates: dates, Lifecycle: lifecycle, Error: e,
			OffTargetConfirmed: offTargetConfirmed,
		})
	}

	// Add another records nothing: it re-renders the form, everything typed
	// kept, with one more Highlight row. A form that warned a Metric is off
	// target warns again while Green is still picked and unconfirmed, judged
	// by the readings as typed; one that doesn't parse is left for the next
	// submit to report.
	if r.FormValue("add_highlight") != "" {
		d := formData(nil)
		d.Highlights = append(d.Highlights, highlightFormData{})
		if r.FormValue("off_target_warned") != "" && health == domain.HealthGreen && !offTargetConfirmed {
			if readings, err := readingsFromForm(metrics, rawReadings); err == nil {
				d.OffTarget = view.OffTargetMetrics(readings)
			}
		}
		s.renderCheckinFormAgain(w, r, current, view, d)
		return
	}

	date, err := parseDate(rawDate)
	if err != nil {
		s.renderCheckinFormError(w, r, current, view, formData(checkinError{Field: "path_to_green", Message: fmt.Sprintf("%q isn't a date.", rawDate)}))
		return
	}
	readings, err := readingsFromForm(metrics, rawReadings)
	if err != nil {
		s.renderCheckinFormError(w, r, current, view, formData(err))
		return
	}

	in := domain.SubmitCheckinInput{
		GoalID:         goalID,
		AuthorID:       current.ID,
		Health:         health,
		Status:         status,
		PathToGreen:    path,
		PathTargetDate: date,
		// A reason with an earlier, unchanged or first date is dropped.
		PathTargetDateReason: pathReason,
		AsksForHelp:          asksForHelp,
		Explanation:          explanation,
		Readings:             readings,
		// The Lifecycle fields go through as typed; the domain decides which
		// changes a Check-in may make and what each needs.
		Lifecycle:       lifecycle.To,
		LifecycleReason: lifecycle.Reason,
		Outcome:         lifecycle.Outcome,
		// Confirm Green, on the off-target warning, posts this, and a form
		// re-rendered since carries it.
		ConfirmOffTarget: offTargetConfirmed,
	}
	if err := dates.applyTo(&in); err != nil {
		s.renderCheckinFormError(w, r, current, view, formData(err))
		return
	}
	// The offered rows come first, as on the form, so the domain numbers
	// every row as the form's legends do.
	for _, row := range drafts {
		in.Highlights = append(in.Highlights, domain.HighlightInput{Kind: row.Kind, Note: row.Note, DraftHighlightID: row.DraftID, LeftOut: !row.Keep})
	}
	for _, row := range highlights {
		in.Highlights = append(in.Highlights, domain.HighlightInput{Kind: row.Kind, Note: row.Note})
	}

	if _, err = s.svc.SubmitCheckin(r.Context(), in); err != nil {
		// A Green with a Metric off target is a question, not a field's
		// error: the form comes back as typed, warning about the Metrics and
		// offering to save the Green anyway.
		var offTarget *domain.GreenOffTargetError
		if errors.As(err, &offTarget) {
			d := formData(nil)
			d.OffTarget = offTarget.Metrics
			s.renderCheckinFormError(w, r, current, view, d)
			return
		}
		if errors.Is(err, domain.ErrValidation) {
			s.renderCheckinFormError(w, r, current, view, formData(err))
			return
		}
		s.writeCheckinError(w, r, goalID, err)
		return
	}
	s.checkinRedirect(w, r, goalID)
}

// offTargetSentence says how a Metric missed its target: "Idle capacity is
// 40.88 percent; its target was 20 by Nov 30."
func offTargetSentence(o domain.OffTargetMetric, shown displayDates) string {
	return fmt.Sprintf("%s is %s %s; its target was %s by %s.",
		o.Metric.Name, fmtNum(o.Reading), o.Metric.Unit, fmtNum(o.Metric.Target), shown.date(o.Metric.TargetDate))
}

// checkinErrorField names the form field a Check-in refusal is about: the
// input the domain's refusal names, by the names the form's inputs share
// (domain.InputErrors), or "" for one about the Check-in as a whole, shown at
// the top of the form.
func checkinErrorField(err error) string {
	if problems := domain.InputErrors(err); len(problems) > 0 {
		return problems[0].Input
	}
	return ""
}

// Error returns the message, so a form-reading error can carry its field.
func (e checkinError) Error() string { return e.Message }

// readingField names a Metric's reading input.
func readingField(metricID int64) string {
	return domain.CheckinReadingInput(metricID)
}

// rawReadingsFromForm reads the reading_<metricID> field for each of the Goal's
// Metrics, keyed by Metric ID, keeping the raw text so an error re-render can
// show what the reader typed. A blank field means no reading for that Metric.
func rawReadingsFromForm(r *http.Request, metrics []domain.Metric) map[int64]string {
	out := make(map[int64]string, len(metrics))
	for _, m := range metrics {
		out[m.ID] = strings.TrimSpace(r.FormValue(readingField(m.ID)))
	}
	return out
}

// readingsFromForm parses the Goal's non-blank raw readings into domain input. A
// field that is not a number is rejected with a message quoting what was typed
// and naming the Metric.
func readingsFromForm(metrics []domain.Metric, raw map[int64]string) ([]domain.MetricReadingInput, error) {
	out := make([]domain.MetricReadingInput, 0, len(raw))
	for _, m := range metrics {
		text := raw[m.ID]
		if text == "" {
			continue
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, checkinError{Field: readingField(m.ID), Message: fmt.Sprintf("%q isn't a number for %q.", text, m.Name)}
		}
		out = append(out, domain.MetricReadingInput{MetricID: m.ID, Value: value})
	}
	return out, nil
}

// datesFromForm reads the Check-in's date and Milestone fields as typed: the
// delivery date (for a Dated Goal) and its reason, a row per Planned Milestone
// (milestone_*_<id>), and the new-Milestone rows (new_milestone_name and
// new_milestone_date, paired by position).
func datesFromForm(r *http.Request, goal domain.Goal, milestones []domain.Milestone) checkinDatesFormData {
	d := checkinDatesFormData{Dated: goal.Kind == domain.GoalDated, CurrentDeliveryDate: fmtDate(goal.DeliveryDate)}
	if d.Dated {
		d.DeliveryDate = strings.TrimSpace(r.FormValue("delivery_date"))
		d.DeliveryDateReason = r.FormValue("delivery_date_reason")
	}
	for _, m := range milestones {
		if m.Status != domain.MilestonePlanned {
			continue
		}
		id := strconv.FormatInt(m.ID, 10)
		d.Milestones = append(d.Milestones, milestoneFormRow{
			Milestone:     m,
			Date:          strings.TrimSpace(r.FormValue("milestone_date_" + id)),
			DateReason:    r.FormValue("milestone_date_reason_" + id),
			Status:        r.FormValue("milestone_status_" + id),
			RemovedReason: r.FormValue("milestone_removed_reason_" + id),
		})
	}
	names := r.Form["new_milestone_name"]
	newDates := r.Form["new_milestone_date"]
	for i, name := range names {
		row := newMilestoneFormRow{Name: name}
		if i < len(newDates) {
			row.Date = strings.TrimSpace(newDates[i])
		}
		if strings.TrimSpace(row.Name) == "" && row.Date == "" {
			continue
		}
		d.NewMilestones = append(d.NewMilestones, row)
	}
	return d
}

// applyTo parses the typed dates into the Check-in's date and Milestone
// changes. A date that isn't a date is rejected, next to its field.
func (d checkinDatesFormData) applyTo(in *domain.SubmitCheckinInput) error {
	delivery, err := parseDate(d.DeliveryDate)
	if err != nil {
		return checkinError{Field: "delivery_date", Message: fmt.Sprintf("%q isn't a date.", d.DeliveryDate)}
	}
	in.DeliveryDate = delivery
	in.DeliveryDateReason = d.DeliveryDateReason
	for _, row := range d.Milestones {
		date, err := parseDate(row.Date)
		if err != nil {
			return checkinError{
				Field:   domain.CheckinMilestoneInput(row.Milestone.ID, "date"),
				Message: fmt.Sprintf("%q isn't a date for Milestone %q.", row.Date, row.Milestone.Name),
			}
		}
		in.Milestones = append(in.Milestones, domain.MilestoneChangeInput{
			MilestoneID:   row.Milestone.ID,
			TargetDate:    date,
			DateReason:    row.DateReason,
			Status:        row.Status,
			RemovedReason: row.RemovedReason,
		})
	}
	for i, row := range d.NewMilestones {
		date, err := parseDate(row.Date)
		if err != nil {
			return checkinError{
				Field:   domain.CheckinNewMilestoneInput(i),
				Message: fmt.Sprintf("%q isn't a date for new Milestone %q.", row.Date, row.Name),
			}
		}
		in.NewMilestones = append(in.NewMilestones, domain.NewMilestoneInput{Name: row.Name, TargetDate: date})
	}
	return nil
}

// lifecycleFromForm reads the Check-in's Lifecycle fields: the Lifecycle to move
// the Goal to (empty to leave it alone), and the reason or outcome that change
// needs, with the choices the page's permissions offer.
func lifecycleFromForm(r *http.Request, v checkinView) lifecycleFormData {
	l := lifecycleFormOf(v.Goal.Lifecycle, v.Permissions)
	l.To = r.FormValue("lifecycle")
	l.Reason = r.FormValue("lifecycle_reason")
	l.Outcome = r.FormValue("outcome")
	return l
}

// highlightsFromForm reads the Highlight rows as typed (highlight_kind and
// highlight_note, paired by position), blank rows included so a re-render
// keeps every row and the domain's row numbers match the form's.
func highlightsFromForm(r *http.Request) []highlightFormData {
	kinds := r.Form["highlight_kind"]
	notes := r.Form["highlight_note"]
	out := make([]highlightFormData, max(len(kinds), len(notes)))
	for i := range out {
		if i < len(kinds) {
			out[i].Kind = kinds[i]
		}
		if i < len(notes) {
			out[i].Note = notes[i]
		}
	}
	return out
}

// draftHighlightsFromForm reads the rows offered from Draft Highlights as
// typed, in the order the form showed them: each posts its Draft Highlight's
// id in draft_highlight_id, and its Keep box (draft_highlight_keep, whose
// value is that id, posting nothing when unticked), kind, note and who logged
// it are paired with it by id. An id that isn't one still holds its row, as -1, which names
// no Draft Highlight, so the rows after it keep the numbers the form showed.
func draftHighlightsFromForm(r *http.Request) []highlightFormData {
	ids := r.Form["draft_highlight_id"]
	if len(ids) == 0 {
		return nil
	}
	kept := map[string]bool{}
	for _, id := range r.Form["draft_highlight_keep"] {
		kept[id] = true
	}
	var out []highlightFormData
	for _, raw := range ids {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			id = -1
		}
		row := highlightFormData{
			DraftID: id,
			Keep:    kept[raw],
			Kind:    r.FormValue("draft_highlight_kind_" + raw),
			Note:    r.FormValue("draft_highlight_note_" + raw),
			// Who logged it only labels the row, so it is carried as posted.
			LoggedBy: r.FormValue("draft_highlight_logged_by_" + raw),
		}
		out = append(out, row)
	}
	return out
}

// handleNoChangeCheckin records a Check-in repeating the Goal's previous values
// in one click.
func (s *Server) handleNoChangeCheckin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.SubmitNoChangeCheckin(r.Context(), goalID, current.ID); err != nil {
		s.renderNoChangeRefusal(w, r, current, goalID, err)
		return
	}
	s.checkinRedirect(w, r, goalID)
}

// renderNoChangeRefusal shows why a No change couldn't be recorded. A refusal
// that means "write a full Check-in" (no previous Check-in, a Goal that isn't
// Active, a Health the roll-up or an overdue Milestone now rules out) lands the
// reader on the Goal's Check-in page with the reason as the form's error, a
// sentence reading as the No change card gives it: htmx gets the page with the
// Check-in page's URL pushed (answerPushedPage), a plain post gets it at 422.
// Anything else is refused (refuse), with Back to the Goal: a Goal that
// doesn't exist gets the Not found page, one the reader may not check in on
// 403, and a failure a logged 500.
func (s *Server) renderNoChangeRefusal(w http.ResponseWriter, r *http.Request, current domain.Account, goalID int64, err error) {
	// The Goal is loaded first: the domain checks the Lifecycle before who may
	// check in, so a validation error alone doesn't mean the reader may.
	view, viewErr := s.checkinView(r.Context(), goalID, current)
	switch {
	case viewErr != nil:
		s.refuse(w, r, "Can't check in", goalPath(goalID), viewErr, refuseFailureSaying("The Goal couldn't be loaded, so nothing was recorded. Try again."))
	case errors.Is(err, domain.ErrNotAuthorized), !view.CanCheckin:
		s.refuse(w, r, "Can't check in", goalPath(goalID), checkinForbidden(view.Goal))
	case errors.Is(err, domain.ErrValidation):
		page := checkinPage(&current, view, checkinFormRefused(view, sentence(s.noChangeReason(err))))
		answerPushedPage(w, r, http.StatusUnprocessableEntity, page, "/goals/"+strconv.FormatInt(goalID, 10)+"/checkin")
	default:
		s.refuse(w, r, "Can't check in", goalPath(goalID), err, refuseFailureSaying("The No change couldn't be recorded. Try again."))
	}
}

// noChangeReason is why a No change was refused, as the Check-in form shows it,
// with an overdue Milestone's due date as the page shows dates. A Green
// refused for an off-target Metric names each miss as the form's off-target
// warning does, with its dates as the page shows them, and says how to keep
// the Green; the domain's refusal only carries the Metrics.
func (s *Server) noChangeReason(err error) string {
	var offTarget *domain.GreenOffTargetError
	if !errors.As(err, &offTarget) {
		return plainReasonShown(err, s.displayDates())
	}
	missed := make([]string, 0, len(offTarget.Metrics))
	for _, o := range offTarget.Metrics {
		missed = append(missed, offTargetSentence(o, s.displayDates()))
	}
	return strings.Join(missed, " ") + " To keep Green, submit a Check-in that confirms it, or update the Health."
}

// plainReasonShown is why a domain refusal refused, as a page shows it
// (domain.Reason), but an overdue Milestone's refusal, whose own text is only
// a log form, is composed from the Milestone and what its missed date rules
// out, with its due date as the page shows dates.
func plainReasonShown(err error, shown displayDates) string {
	var overdue *domain.MilestoneOverdueError
	if !errors.As(err, &overdue) {
		return domain.Reason(err)
	}
	msg := fmt.Sprintf("Milestone %q is overdue (due %s)", overdue.Milestone.Name, shown.date(overdue.Milestone.TargetDate))
	switch overdue.RulesOut {
	case domain.OverdueRulesOutGreen:
		msg += ", so the Goal can't be Green"
	case domain.OverdueRulesOutNoChange:
		msg += ": submit a Check-in to move its date, mark it Done or remove it"
	}
	return msg
}

// renderCheckinFormError re-renders the Check-in form with a validation message.
// For an htmx request it returns the form alone for htmx to swap in place
// (answerSwap); a plain post gets the whole Check-in page, view, around it at 422 (#587). The form keeps the
// context the Check-in page's loader read for it (checkinView.withContext):
// the Goal's Rolled-up Health beside the Owner's Health (ADR-0003), its Run
// off Green for the Path to Green's heading, the latest Check-in's answers as
// ghost text, and whom an ask would go to.
func (s *Server) renderCheckinFormError(w http.ResponseWriter, r *http.Request, current domain.Account, view checkinView, data checkinFormData) {
	if isHTMX(r) {
		answerSwap(w, r, checkinForm(data))
		return
	}
	render(w, r, http.StatusUnprocessableEntity, checkinPage(&current, view, data))
}

// renderCheckinFormAgain re-renders the Check-in form as typed, with no error
// (Add another): htmx swaps in the form alone, a plain post gets the whole
// Check-in page, view, around it. The form keeps the context the page's loader
// read for it, as renderCheckinFormError's does.
func (s *Server) renderCheckinFormAgain(w http.ResponseWriter, r *http.Request, current domain.Account, view checkinView, data checkinFormData) {
	if isHTMX(r) {
		answerSwap(w, r, checkinForm(data))
		return
	}
	render(w, r, http.StatusOK, checkinPage(&current, view, data))
}

// checkinRedirect returns the reader to the Goal page after a successful
// Check-in: htmx reloads the whole page (so history and current Health
// refresh), a plain post follows a 303 (answerRedirect).
func (s *Server) checkinRedirect(w http.ResponseWriter, r *http.Request, goalID int64) {
	answerRedirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10))
}

// writeCheckinError answers a Check-in on goalID the domain refused with the
// refusal page, linking Back to the Goal.
func (s *Server) writeCheckinError(w http.ResponseWriter, r *http.Request, goalID int64, err error) {
	s.refuse(w, r, "Can't check in", goalPath(goalID), err)
}
