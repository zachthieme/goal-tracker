package domain

import (
	"slices"
	"strings"
)

// LifecycleChange is a move between Lifecycle states made in a Check-in
// (CONTEXT.md: Lifecycle). Leaving Active for On Hold or Cancelled carries a
// Reason; reaching Done carries a one-line Outcome, and the same Check-in's
// Metric readings are the Goal's final values. It is the zero value on a
// Check-in that left the Lifecycle alone.
type LifecycleChange struct {
	From    string
	To      string
	Reason  string
	Outcome string
}

// Changed reports whether the Check-in moved the Goal's Lifecycle.
func (c LifecycleChange) Changed() bool {
	return c.To != ""
}

// Lifecycle values a Goal can be in (CONTEXT.md: Lifecycle). A Goal starts
// Proposed and is activated through the activation gate; after that its
// Lifecycle changes only in a Check-in (planLifecycleChange).
//
// Each name starts Lifecycle: the CHECK test on goals.lifecycle and a
// Check-in's lifecycle_from and lifecycle_to finds its values by that prefix
// (enumchecks_test.go), so a value named otherwise goes unchecked, and a
// string constant so named that is no Lifecycle is checked as one.
const (
	LifecycleProposed  = "Proposed"
	LifecycleActive    = "Active"
	LifecycleOnHold    = "On Hold"
	LifecycleDone      = "Done"
	LifecycleCancelled = "Cancelled"
)

// lifecycleRow is one Lifecycle in lifecycleTable: what a Goal in it takes,
// and where a Check-in may move it.
type lifecycleRow struct {
	name string
	// checkins is whether it takes Check-ins.
	checkins bool
	// ended is whether it is an end state, after which the Goal takes no
	// more Draft Highlights or Milestones.
	ended bool
	// suggestable is whether a parent may be suggested for it, or it be
	// suggested as one.
	suggestable bool
	// changesTo are the Lifecycles a Check-in may move it to.
	changesTo []string
}

// lifecycleTable is every Lifecycle, in Lifecycle order, and what a Goal in
// each takes (CONTEXT.md: Lifecycle). Only an Active or On Hold Goal takes
// Check-ins: a Proposed Goal becomes Active through the activation gate, and
// Done and Cancelled are end states. A Check-in may put an Active Goal On
// Hold, mark it Done or Cancel it, and resume an On Hold one to Active or
// Cancel it. A parent is suggested only for a Proposed or Active Goal. Every
// Lifecycle predicate, and every refusal listing Lifecycles, reads it.
var lifecycleTable = []lifecycleRow{
	{name: LifecycleProposed, suggestable: true},
	{name: LifecycleActive, checkins: true, suggestable: true, changesTo: []string{LifecycleOnHold, LifecycleDone, LifecycleCancelled}},
	{name: LifecycleOnHold, checkins: true, changesTo: []string{LifecycleActive, LifecycleCancelled}},
	{name: LifecycleDone, ended: true},
	{name: LifecycleCancelled, ended: true},
}

// lifecycleRowOf is lifecycle's row of lifecycleTable, the zero row, which
// takes nothing and hasn't ended, for a value that is no Lifecycle.
func lifecycleRowOf(lifecycle string) lifecycleRow {
	for _, r := range lifecycleTable {
		if r.name == lifecycle {
			return r
		}
	}
	return lifecycleRow{}
}

// Lifecycles are every Lifecycle a Goal can be in, in Lifecycle order:
// Proposed, Active, On Hold, Done, Cancelled.
func Lifecycles() []string {
	out := make([]string, len(lifecycleTable))
	for i, r := range lifecycleTable {
		out[i] = r.name
	}
	return out
}

// LifecycleTakesCheckins reports whether a Goal in lifecycle takes
// Check-ins: only an Active or On Hold one does (lifecycleTable).
func LifecycleTakesCheckins(lifecycle string) bool {
	return lifecycleRowOf(lifecycle).checkins
}

// LifecycleEnded reports whether lifecycle is an end state of the Lifecycle:
// Done or Cancelled (lifecycleTable).
func LifecycleEnded(lifecycle string) bool {
	return lifecycleRowOf(lifecycle).ended
}

// LifecycleTakesDraftHighlights reports whether a Goal in lifecycle takes
// Draft Highlights: it does until it has ended (LifecycleEnded), when it takes
// no more Check-ins to offer them at (CONTEXT.md: Draft Highlight).
func LifecycleTakesDraftHighlights(lifecycle string) bool {
	return !LifecycleEnded(lifecycle)
}

// LifecycleTakesMilestones reports whether a Goal in lifecycle takes a
// Milestone added outside a Check-in: until it has ended (LifecycleEnded)
// (CONTEXT.md: Milestone).
func LifecycleTakesMilestones(lifecycle string) bool {
	return !LifecycleEnded(lifecycle)
}

// LifecycleSuggestable reports whether a Goal in lifecycle may have a parent
// suggested for it, or be suggested as one: only while it is Proposed or
// Active (lifecycleTable).
func LifecycleSuggestable(lifecycle string) bool {
	return lifecycleRowOf(lifecycle).suggestable
}

// LifecycleChangesFrom are the Lifecycles a Check-in may move a Goal in
// lifecycle to (lifecycleTable): an Active Goal may go On Hold, be marked
// Done or be Cancelled, and an On Hold one resumed to Active or Cancelled.
// Every other Lifecycle is closed to Check-ins, so has none.
func LifecycleChangesFrom(lifecycle string) []string {
	return slices.Clone(lifecycleRowOf(lifecycle).changesTo)
}

// lifecycleNamesWhere joins the Lifecycles selects picks with " or ", in
// Lifecycle order, as a refusal names them: Proposed or Active.
func lifecycleNamesWhere(selects func(string) bool) string {
	var names []string
	for _, l := range Lifecycles() {
		if selects(l) {
			names = append(names, l)
		}
	}
	return strings.Join(names, " or ")
}

// lifecycleNamesWhereWithArticle is lifecycleNamesWhere with "a" or "an"
// before it, agreeing with the first name: an Active or On Hold.
func lifecycleNamesWhereWithArticle(selects func(string) bool) string {
	names := lifecycleNamesWhere(selects)
	if names != "" && strings.ContainsRune("AEIOU", rune(names[0])) {
		return "an " + names
	}
	return "a " + names
}

// lifecycleNameWithArticle is lifecycle with "a" or "an" before it, as
// lifecycleNamesWhereWithArticle picks it: an Active, a Done.
func lifecycleNameWithArticle(lifecycle string) string {
	return lifecycleNamesWhereWithArticle(func(l string) bool { return l == lifecycle })
}

// lifecycleTakesNoCheckinsRefusal refuses a Check-in on a Goal in lifecycle,
// one LifecycleTakesCheckins says takes none, naming the Lifecycles that do.
func lifecycleTakesNoCheckinsRefusal(lifecycle string) error {
	return Refuse(ErrValidation, "a %s Goal can't be checked in on: only %s Goal takes Check-ins", lifecycle, lifecycleNamesWhereWithArticle(LifecycleTakesCheckins))
}

// lifecycleMoveRefusal refuses a Check-in moving a Goal in from to to, a
// Lifecycle LifecycleChangesFrom doesn't offer.
func lifecycleMoveRefusal(from, to string) error {
	return inputError(CheckinInputLifecycle, "a Check-in can't move %s Goal to %q", lifecycleNameWithArticle(from), to)
}

// lifecycleKeepRefusal refuses a Check-in leaving a Goal in lifecycle as it
// is where that Lifecycle must change in one: an On Hold Goal's Check-in
// resumes or Cancels it (CONTEXT.md: Lifecycle).
func lifecycleKeepRefusal(lifecycle string) error {
	if lifecycle == LifecycleOnHold {
		return inputError(CheckinInputLifecycle, "an On Hold Goal can only be resumed or Cancelled in a Check-in")
	}
	return nil
}

// planLifecycleChange plans a Check-in's move of a Goal from its current
// Lifecycle to to, one the Check-in's rule has already allowed
// (lifecycleKeptRefusal, lifecycleChangeRefusal): it returns the change to
// record, or the zero change when the Check-in leaves the Lifecycle alone,
// refusing a move that lacks its reason or outcome. Going On Hold and being
// Cancelled each need a reason, and being marked Done a one-line outcome;
// being resumed to Active needs neither.
func planLifecycleChange(current, to, reason, outcome string) (LifecycleChange, error) {
	reason = strings.TrimSpace(reason)
	outcome = strings.TrimSpace(outcome)
	if to == "" || to == current {
		return LifecycleChange{}, nil
	}
	change := LifecycleChange{From: current, To: to, Reason: reason}
	switch to {
	case LifecycleCancelled:
		if reason == "" {
			return LifecycleChange{}, inputError(CheckinInputLifecycleReason, "cancelling a Goal needs a reason")
		}
	case LifecycleOnHold:
		if reason == "" {
			return LifecycleChange{}, inputError(CheckinInputLifecycleReason, "putting a Goal On Hold needs a reason")
		}
	case LifecycleDone:
		if outcome == "" {
			return LifecycleChange{}, inputError(CheckinInputOutcome, "marking a Goal Done needs an outcome")
		}
		if strings.ContainsAny(outcome, "\r\n") {
			return LifecycleChange{}, inputError(CheckinInputOutcome, "a Goal's outcome is one line")
		}
		change.Outcome = outcome
	case LifecycleActive:
		// Resuming is not leaving Active, so no reason is needed.
	}
	return change, nil
}

// resultingLifecycle is the Lifecycle the Goal is in once a Check-in with this
// change is recorded.
func (c LifecycleChange) resultingLifecycle(current string) string {
	if c.Changed() {
		return c.To
	}
	return current
}

// requireFinalValues checks that a Done Check-in's readings give a final value
// for every one of metrics, the Goal's, naming the first Metric that has none.
func requireFinalValues(metrics []Metric, readings []MetricReadingInput) error {
	read := make(map[int64]bool, len(readings))
	for _, rd := range readings {
		read[rd.MetricID] = true
	}
	for _, m := range metrics {
		if !read[m.ID] {
			return inputError(CheckinReadingInput(m.ID), "marking a Goal Done needs a final value for every Metric; %q has none", m.Name)
		}
	}
	return nil
}
