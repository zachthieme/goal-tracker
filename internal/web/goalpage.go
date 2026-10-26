package web

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

func (s *Server) handleViewGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	view, err := s.goalPageView(r.Context(), id, current)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, "could not load goal", err)
		return
	}
	offer := takeUndo(w, r)
	view.Toast = cmp.Or(
		s.linkRemovalToast(r.Context(), offer, current, id),
		s.suggestionDeclineToast(r.Context(), offer, current, id, ""),
	)
	view.Open = goalForm(r.URL.Query().Get("open"))
	view.History = view.History.narrowed(r.URL.Query())
	render(w, r, http.StatusOK, goalPage(&current, view))
}

// goalPageView loads everything the single-Goal page renders for the viewer,
// through the Goal page's loader (ADR 0010). A missing Goal comes back as
// domain.ErrNotFound.
func (s *Server) goalPageView(ctx context.Context, id int64, current domain.Account) (goalView, error) {
	p, err := s.svc.GoalPage(ctx, current.ID, id)
	if err != nil {
		return goalView{}, err
	}
	// Each Metric's readings over time, for its trend against target (CONTEXT.md:
	// the Goal page shows each Metric's trend over time against its target).
	trends := make([]metricTrend, 0, len(p.Metrics))
	for _, m := range p.Metrics {
		trends = append(trends, metricTrend{Metric: m, Readings: p.Readings[m.ID]})
	}
	view := goalView{GoalPage: p, Trends: trends}
	view.History = newHistory(view, s.svc.Timezone(), view.Now)
	return view, nil
}

// goalView is everything the single-Goal page renders: what the Goal page's
// loader read for the viewer (domain.GoalPage), and what web works out for
// the page itself.
type goalView struct {
	domain.GoalPage
	// Trends pairs each Metric with its readings over time, for the trend
	// against target shown on the Goal page (CONTEXT.md: Metric).
	Trends []metricTrend
	// Open is the one form the page shows open in place, from its ?open=
	// address, or "" for none. FormError is why that form's last submit was
	// refused and FormInput what it sent, so it comes back as it was left.
	Open      goalForm
	FormError string
	FormInput url.Values
	// History is the Goal's History as one timeline, newest first, showing the
	// filter and page count the page's address asks for.
	History history
	// Toast is the one-time notice the page carries straight after the viewer
	// removed one of its links or declined one of its Parent suggestions, with
	// an Undo; nil on any other visit.
	Toast *toast
}

// goalForm names a form on the Goal page that opens in place: following
// /goals/{id}?open=<name> renders the page with that one form open, where it
// belongs, with a Cancel back to the plain page. It works without script.
type goalForm string

const (
	formHandoff    goalForm = "handoff"
	formReassign   goalForm = "reassign"
	formDepart     goalForm = "depart"
	formReturn     goalForm = "return"
	formTopLevel   goalForm = "top-level"
	formParentLink goalForm = "parent-link"
	formDelegates  goalForm = "delegates"
	// formMilestones is the Milestones card's Add Milestone form.
	formMilestones goalForm = "milestones"
	formDimensions goalForm = "dimensions"
	formFields     goalForm = "fields"
	// formDraftHighlight is the Draft Highlights block's log form, opened in
	// place from its "+ Log a highlight" link.
	formDraftHighlight goalForm = "draft-highlight"
	// formTitleWhyItMatters edits the Goal's title and Why it matters together, in the
	// page head.
	formTitleWhyItMatters goalForm = "title-why-it-matters"
)

// offers reports whether the viewer may use form on this Goal: whether the
// domain permits them what the form does (domain.GoalPermissions), and, for
// the Dimension values and Fields forms, whether there is any to set.
func (v goalView) offers(form goalForm) bool {
	p := v.Permissions
	switch form {
	case formHandoff:
		return p.HandOff
	case formReassign:
		return p.Reassign
	case formDepart:
		return p.MarkOwnerDeparted
	case formReturn:
		return p.MarkOwnerReturned
	case formTopLevel:
		return p.MarkTopLevel
	case formTitleWhyItMatters:
		return p.EditTitleAndWhyItMatters
	case formParentLink:
		return p.LinkParent
	case formDelegates:
		return p.ManageDelegates
	case formMilestones:
		return p.AddMilestone
	case formDimensions:
		return p.SetValues && len(domain.OfferedDimensions(v.Dimensions)) > 0
	case formFields:
		return p.SetValues && len(domain.OfferedFields(v.Fields)) > 0
	case formDraftHighlight:
		return p.LogDraftHighlight
	}
	return false
}

// opens reports whether form is the one the page shows open: the one asked
// for, when the viewer may use it.
func (v goalView) opens(form goalForm) bool {
	return v.Open == form && v.offers(form)
}

// openURL is the Goal page with form open in place.
func (v goalView) openURL(form goalForm) templ.SafeURL {
	return templ.SafeURL(fmt.Sprintf("/goals/%d?open=%s", v.Goal.ID, url.QueryEscape(string(form))))
}

// typedFor is what the refused submit sent as name from the form whose key
// field held id — one of several forms of a kind, such as each Field's — or ""
// when that form wasn't the one sent.
func (v goalView) typedFor(key string, id int64, name string) string {
	if v.FormInput.Get(key) != strconv.FormatInt(id, 10) {
		return ""
	}
	return v.FormInput.Get(name)
}

// goalAction is one item in the Goal page's action menu: its label and where
// it leads.
type goalAction struct {
	Label string
	URL   templ.SafeURL
}

// actions lists the action menu's items the viewer may use, in menu order:
// each opens its form in place, but some lead to pages of their own. Suggest a
// parent, for anyone but the Owner of an Active or Proposed Goal, leads to its
// form (CONTEXT.md: Parent suggestion); Add a child Goal, which anyone may
// use, to the New goal form with this Goal picked as the parent; and the
// Lifecycle changes, last, to the Check-in page with that change picked.
func (v goalView) actions() []goalAction {
	topLevel := "Mark Top-level"
	if v.Goal.TopLevel {
		topLevel = "Unmark Top-level"
	}
	var suggest string
	if v.Permissions.SuggestParent {
		suggest = fmt.Sprintf("/goals/%d/suggest-parent", v.Goal.ID)
	}
	var out []goalAction
	for _, a := range []struct {
		label string
		form  goalForm
		href  string
	}{
		{"Edit title and Why it matters", formTitleWhyItMatters, ""},
		{"Hand off", formHandoff, ""},
		{"Add a delegate", formDelegates, ""},
		{"Link to a parent Goal", formParentLink, ""},
		{"Suggest a parent", "", suggest},
		{"Add a child Goal", "", fmt.Sprintf("/goals/new?parent=%d", v.Goal.ID)},
		{"Edit Dimension values", formDimensions, ""},
		{"Edit Fields", formFields, ""},
		{topLevel, formTopLevel, ""},
		{"Mark owner departed…", formDepart, ""},
		{"Mark returned…", formReturn, ""},
		{"Reassign", formReassign, ""},
	} {
		switch {
		case a.href != "":
			out = append(out, goalAction{a.label, templ.SafeURL(a.href)})
		case a.form != "" && v.offers(a.form):
			out = append(out, goalAction{a.label, v.openURL(a.form)})
		}
	}
	return append(out, v.lifecycleActions()...)
}

// lifecycleActions are the action menu's Lifecycle changes: each one a
// Check-in may make (domain.CheckinPermissions.LifecycleChanges) but the one
// the Check-in form picks by default, resuming an On Hold Goal. Each leads to
// the Check-in page with that change picked, for a Lifecycle change is made in
// a Check-in (CONTEXT.md: Lifecycle).
func (v goalView) lifecycleActions() []goalAction {
	l := lifecycleFormOf(v.Goal.Lifecycle, v.Permissions.CheckinPermissions)
	out := make([]goalAction, 0, len(l.Changes))
	for _, to := range l.Changes {
		if to == l.defaultTo() {
			continue
		}
		out = append(out, goalAction{lifecycleActionLabels[to], templ.SafeURL(fmt.Sprintf("/goals/%d/checkin?lifecycle=%s", v.Goal.ID, url.QueryEscape(to)))})
	}
	return out
}

// lifecycleActionLabels are the action menu's labels for the Lifecycle
// changes, by the Lifecycle each moves the Goal to.
var lifecycleActionLabels = map[string]string{
	domain.LifecycleActive:    "Resume…",
	domain.LifecycleOnHold:    "Put on hold…",
	domain.LifecycleDone:      "Mark done…",
	domain.LifecycleCancelled: "Cancel…",
}

// openActionItems are the Goal's open Action Items, soonest due first, as its
// page lists them.
func (v goalView) openActionItems() []domain.ActionItemListing {
	var out []domain.ActionItemListing
	for _, l := range v.ActionItems {
		if !l.Item.Closed {
			out = append(out, l)
		}
	}
	return out
}

// dates is the clock the Goal page shows its dates against: the app clock's
// now in the org's timezone.
func (v goalView) dates() displayDates {
	return displayDates{Now: v.Now, Loc: v.History.Loc}
}

// priorDates returns the dates a Goal's delivery date (milestoneID 0) or one of
// its Milestones held before each of its Date Slips, earliest first, for
// showing struck through ahead of the current date (CONTEXT.md: Date Slip). A
// slip of the back-to-Green date is neither; see pathPriorDates.
func (v goalView) priorDates(milestoneID int64) []time.Time {
	var out []time.Time
	for _, s := range v.DateSlips {
		if s.MilestoneID == milestoneID && !s.BackToGreen {
			out = append(out, s.OldDate)
		}
	}
	return out
}

// pathPriorDates returns the dates the Path to Green's back-to-Green date
// slipped from within the current Run off Green, earliest first (CONTEXT.md:
// Date Slip, Run off Green).
func (v goalView) pathPriorDates() []time.Time {
	return domain.BackToGreenPriorDates(v.DateSlips, v.RunOffGreen)
}

// milestoneMark is the mark a Milestone shows on the Goal page as of today's
// date in the Goal's timezone (CONTEXT.md: Milestone, Timezone). It is New when the Goal's latest Check-in added it,
// or it was added outside a Check-in since then.
func (v goalView) milestoneMark(m domain.Milestone) string {
	return domain.MilestoneMarkOf(m, v.priorDates(m.ID), v.addedSinceLatestCheckin(m.ID), v.Now.In(v.Timezone))
}

// addedSinceLatestCheckin reports whether the Goal's latest Check-in added
// the Milestone, or its Owner or a Delegate added it from the Goal page after
// that Check-in. A Goal with no Check-in has nothing New, and a Milestone
// added while the Goal was Proposed never is.
func (v goalView) addedSinceLatestCheckin(milestoneID int64) bool {
	if len(v.Checkins) == 0 {
		return false
	}
	latest := v.Checkins[0]
	for _, ch := range latest.MilestoneChanges {
		if ch.Kind == domain.MilestoneChangeAdded && ch.MilestoneID == milestoneID {
			return true
		}
	}
	for _, ch := range v.MilestoneAdditions {
		if ch.Kind == domain.MilestoneChangeAdded && ch.MilestoneID == milestoneID && ch.CreatedAt.After(latest.CreatedAt) {
			return true
		}
	}
	return false
}

// lifecycleNote explains the Goal's current Lifecycle from the Check-in that
// moved it there: the outcome of a Done Goal, or the reason it is On Hold or
// Cancelled (CONTEXT.md: Lifecycle). It is empty when there is nothing to say,
// such as for a Goal activated through the activation gate.
func (v goalView) lifecycleNote() string {
	for _, c := range v.Checkins {
		ch := c.LifecycleChange
		if !ch.Changed() {
			continue
		}
		if ch.To != v.Goal.Lifecycle {
			return ""
		}
		switch {
		case ch.Outcome != "":
			return "Outcome: " + ch.Outcome
		case ch.Reason != "":
			return "Reason: " + ch.Reason
		}
		return ""
	}
	return ""
}

// metricTrend pairs a Metric with its readings over time, so the Goal page can
// show each Metric's trend against its target (CONTEXT.md: Metric).
type metricTrend struct {
	Metric   domain.Metric
	Readings []domain.MetricReading
}

// assignedValues returns the values this Goal carries in the given Dimension —
// at most one unless the Dimension takes several — used to show the current
// assignment and preselect the assignment control.
func (v goalView) assignedValues(dimensionID int64) []domain.DimensionValue {
	var out []domain.DimensionValue
	for _, val := range v.Values {
		if val.DimensionID == dimensionID {
			out = append(out, val)
		}
	}
	return out
}

// choices are the values of d the Goal page's form offers: its live ones, and
// any retired one the Goal carries, so saving doesn't silently drop it.
func (v goalView) choices(d domain.Dimension) []domain.DimensionValue {
	var out []domain.DimensionValue
	for _, val := range d.Values {
		if !val.Retired || v.carries(val) {
			out = append(out, val)
		}
	}
	return out
}

// carries reports whether the Goal carries the value in the given Dimension.
func (v goalView) carries(value domain.DimensionValue) bool {
	for _, val := range v.Values {
		if val.ID == value.ID {
			return true
		}
	}
	return false
}

// health is the Goal's current Health — its latest Check-in's — while it is
// Active, or "" when it has none: a Proposed Goal, one not yet checked in on,
// or one On Hold, Done or Cancelled.
func (v goalView) health() string {
	if v.LatestCheckin == nil || v.Goal.Lifecycle != domain.LifecycleActive {
		return ""
	}
	return v.LatestCheckin.Health
}

// Sparkline geometry, in SVG user units: a Metric card's trend is drawn in a
// sparkW × sparkH box, inset by sparkPad so the end dot isn't clipped.
const (
	sparkW   = 240.0
	sparkH   = 56.0
	sparkPad = 5.0
)

// sparkline is a Metric's readings laid out for its card: a point per reading,
// oldest first, and the height of the target line, on a scale that fits both.
type sparkline struct {
	Points  []sparkPoint
	TargetY float64
}

// sparkPoint is one reading's place on the sparkline, with the value and date
// its hover tooltip names.
type sparkPoint struct {
	X, Y  float64
	Title string
}

// polyline is the readings as an SVG polyline's points attribute.
func (s sparkline) polyline() string {
	pts := make([]string, 0, len(s.Points))
	for _, p := range s.Points {
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", p.X, p.Y))
	}
	return strings.Join(pts, " ")
}

// last is the latest reading's point, drawn in the accent colour.
func (s sparkline) last() sparkPoint {
	return s.Points[len(s.Points)-1]
}

// sparkline lays the Metric's readings out against its target, each point
// titled with the day it was read as dates shows it.
func (t metricTrend) sparkline(dates displayDates) sparkline {
	lo, hi := t.Metric.Target, t.Metric.Target
	for _, r := range t.Readings {
		lo, hi = min(lo, r.Value), max(hi, r.Value)
	}
	if lo == hi {
		lo, hi = lo-1, hi+1
	}
	y := func(v float64) float64 {
		return sparkPad + (hi-v)/(hi-lo)*(sparkH-2*sparkPad)
	}
	s := sparkline{TargetY: y(t.Metric.Target)}
	for i, r := range t.Readings {
		x := sparkW / 2
		if n := len(t.Readings); n > 1 {
			x = sparkPad + float64(i)*(sparkW-2*sparkPad)/float64(n-1)
		}
		s.Points = append(s.Points, sparkPoint{
			X: x, Y: y(r.Value),
			Title: fmt.Sprintf("%s: %s %s", dates.dayOf(r.CreatedAt), fmtNum(r.Value), t.Metric.Unit),
		})
	}
	return s
}

// current is the Metric's latest reading, and whether it has one.
func (t metricTrend) current() (float64, bool) {
	if len(t.Readings) == 0 {
		return 0, false
	}
	return t.Readings[len(t.Readings)-1].Value, true
}

// trendLabel describes the sparkline for a screen reader: how many readings,
// which way they moved, and whether that is toward the target.
func (t metricTrend) trendLabel() string {
	m := t.Metric
	target := fmt.Sprintf("the target of %s %s", fmtNum(m.Target), m.Unit)
	n := len(t.Readings)
	switch n {
	case 0:
		return fmt.Sprintf("%s: no readings yet; %s", m.Name, target)
	case 1:
		return fmt.Sprintf("%s: 1 reading of %s %s, against %s", m.Name, fmtNum(t.Readings[0].Value), m.Unit, target)
	}
	first, last := t.Readings[0].Value, t.Readings[n-1].Value
	if first == last {
		return fmt.Sprintf("%s: %d readings, holding at %s %s, against %s", m.Name, n, fmtNum(last), m.Unit, target)
	}
	moved, better := "rising", last > first
	if last < first {
		moved = "falling"
	}
	if m.Direction == domain.MetricDown {
		better = !better
	}
	toward := "away from"
	if better {
		toward = "toward"
	}
	return fmt.Sprintf("%s: %d readings, %s from %s to %s %s, %s %s", m.Name, n, moved, fmtNum(first), fmtNum(last), m.Unit, toward, target)
}

// activationItem is one line of a Proposed Goal's activation checklist: a rule
// activation enforces, whether the Goal meets it yet, and Input, the id of the
// New goal form's field or section that would meet it, "" when none does.
type activationItem struct {
	Label string
	Done  bool
	Input string
}

// activationFacts are what the activation checklist reads, whether from a
// saved Goal or from the New goal form as typed: its Why it matters, whether it has
// an Owner, its Kind and delivery date, how many Milestones and Metrics it
// has, and every required Dimension and Field, set or not.
type activationFacts struct {
	WhyItMatters string
	Owned        bool
	Kind         string
	DeliveryDate time.Time
	Milestones   int
	Metrics      int
	Required     []domain.RequiredValue
	// Unsaved is the measure, Milestone or Metric, whose editor the New goal
	// form has open and unsaved, "" when none is; the Goal page never sets it.
	Unsaved string
}

// checklist restates the minimum standard domain.ActivateGoal enforces, so the
// Owner sees what's missing before they try: a Why it matters, an Owner, Dated with a
// delivery date or Ongoing, a Milestone or Metric for a Dated Goal or a Metric
// for an Ongoing one, and a value in each required Dimension and Field
// (CONTEXT.md: Incomplete). The server still enforces the rules on activation.
// Each item names the form's input that meets it: the delivery date once Dated
// is chosen, the Kind group until then, and How you'll know for a Milestone or
// Metric; nothing on the form makes its creator the Owner. While the form has
// a Milestone or Metric editor open, How you'll know's item asks for it to be
// saved, linked to the editor.
func (f activationFacts) checklist() []activationItem {
	kindInput := domain.InputKind
	if f.Kind == domain.GoalDated {
		kindInput = domain.InputDeliveryDate
	}
	items := []activationItem{
		{"Why it matters", strings.TrimSpace(f.WhyItMatters) != "", domain.InputWhyItMatters},
		{"Owner", f.Owned, ""},
		{"Dated with a delivery date, or Ongoing", f.Kind == domain.GoalOngoing || (f.Kind == domain.GoalDated && !f.DeliveryDate.IsZero()), kindInput},
	}
	switch {
	case f.Unsaved != "":
		items = append(items, activationItem{"Save the " + f.Unsaved + " you started", false, goalFormEditorID})
	case f.Kind == domain.GoalOngoing:
		items = append(items, activationItem{"A Metric", f.Metrics > 0, goalFormKnowID})
	default:
		items = append(items, activationItem{"A Milestone or Metric", f.Milestones+f.Metrics > 0, goalFormKnowID})
	}
	for _, r := range f.Required {
		items = append(items, activationItem{"A value in " + r.Name, r.Set, requiredInput(r)})
	}
	return items
}

// requiredInput is the New goal form's input for a required Dimension or
// Field: the Dimension's select or group, or the Field's input.
func requiredInput(r domain.RequiredValue) string {
	if r.FieldID != 0 {
		return domain.FieldInput(r.FieldID)
	}
	return dimensionInput(domain.Dimension{ID: r.DimensionID})
}

// ready reports whether every activation checklist item is done.
func (f activationFacts) ready() bool {
	for _, item := range f.checklist() {
		if !item.Done {
			return false
		}
	}
	return true
}

// activationFacts are the saved Goal's, for its activation checklist.
func (v goalView) activationFacts() activationFacts {
	g := v.Goal
	return activationFacts{
		WhyItMatters: g.WhyItMatters,
		Owned:        g.Owner.ID != 0,
		Kind:         g.Kind,
		DeliveryDate: g.DeliveryDate,
		Milestones:   len(v.Milestones),
		Metrics:      len(v.Metrics),
		Required:     v.Required,
	}
}

// activationChecklist is the Proposed Goal's activation checklist.
func (v goalView) activationChecklist() []activationItem {
	return v.activationFacts().checklist()
}

// readyToActivate reports whether every activation checklist item is done.
func (v goalView) readyToActivate() bool {
	return v.activationFacts().ready()
}

// ownershipOutcome names an ownership change's outcome for the Goal page's
// Ownership history: a Handoff's status, or that an Admin reassigned the Goal.
func ownershipOutcome(status string) string {
	if status == domain.HandoffReassigned {
		return "reassigned by an Admin"
	}
	return status
}

// valueChangeText says what a change in the Goal page's Value history did: a
// value added or removed in a several-values Dimension, or a Dimension's or
// Field's value set, changed or cleared.
func valueChangeText(c domain.ValueChange) string {
	switch {
	case c.Several && c.Before == "":
		return c.Attribute + ": added " + c.After
	case c.Several:
		return c.Attribute + ": removed " + c.Before
	case c.Before == "":
		return c.Attribute + ": set to " + c.After
	case c.After == "":
		return c.Attribute + ": cleared (was " + c.Before + ")"
	default:
		return c.Attribute + ": " + c.Before + " → " + c.After
	}
}

// valueOptionLabel is a value as a select offers it, a retired one marked so.
func valueOptionLabel(v domain.DimensionValue) string {
	if v.Retired {
		return v.Value + " (retired)"
	}
	return v.Value
}
