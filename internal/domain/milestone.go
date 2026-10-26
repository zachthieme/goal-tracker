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

// Milestone is a dated checkpoint within a Goal (CONTEXT.md: Milestone).
type Milestone struct {
	ID         int64
	GoalID     int64
	Name       string
	TargetDate time.Time
	// Status is MilestonePlanned until a Check-in marks it MilestoneDone or
	// MilestoneRemoved; RemovedReason explains a removal.
	Status        string
	RemovedReason string
	CreatedAt     time.Time
}

// Milestone statuses. A Planned Milestone past its date is overdue, and Green is
// rejected while one is.
//
// Each name starts Milestone, not MilestoneChange or MilestoneMark, which name
// other groups: the CHECK test on milestones.status finds its values by that
// prefix (enumchecks_test.go), so a value named otherwise goes unchecked.
const (
	MilestonePlanned = "Planned"
	MilestoneDone    = "Done"
	MilestoneRemoved = "Removed"
)

// Milestone marks: what the Goal page, a Report and its Markdown export show
// ahead of each Milestone's date. They are display marks, not a Health: they
// never feed the Goal's Health, which its Owner sets, and none reads as a
// Health's word (CONTEXT.md: Milestone, Health). A Planned Milestone with none
// of them has no mark (MilestoneMarkNone) and reads as on track.
const (
	MilestoneMarkNone    = ""
	MilestoneMarkDone    = MilestoneDone
	MilestoneMarkRemoved = MilestoneRemoved
	MilestoneMarkOverdue = "Overdue"
	MilestoneMarkSlipped = "Slipped"
	MilestoneMarkNew     = "New"
)

// MilestoneMarkOf is the one mark a Milestone shows, the first that applies:
// Done or Removed by its Status; Overdue while overdue on asOf
// (Milestone.OverdueOn), compared with the calendar date asOf falls on in its
// own location, so a caller passes a time in the Goal's timezone to judge by
// its calendar; Slipped
// while Planned and slipped, having prior dates; New while Planned and isNew,
// added within the window being read. Risk outranks newness, so a new
// Milestone that slipped is Slipped. It needs only what a frozen Report
// snapshot holds, so every surface marks a Milestone the same way.
func MilestoneMarkOf(m Milestone, priorDates []time.Time, isNew bool, asOf time.Time) string {
	switch {
	case m.Status == MilestoneDone:
		return MilestoneMarkDone
	case m.Status == MilestoneRemoved:
		return MilestoneMarkRemoved
	case m.OverdueOn(asOf):
		return MilestoneMarkOverdue
	case len(priorDates) > 0:
		return MilestoneMarkSlipped
	case isNew:
		return MilestoneMarkNew
	}
	return MilestoneMarkNone
}

// MilestoneChange is a Milestone a Check-in added, marked Done or marked
// Removed, recorded with that Check-in, or one the Owner or a Delegate added
// from the Goal page while the Goal was Active or On Hold, recorded with no
// Check-in. A Check-in's Milestone date moves are its Date Slips instead.
// Check-ins made before these were recorded have none.
type MilestoneChange struct {
	ID          int64
	MilestoneID int64
	// Kind is MilestoneChangeAdded, MilestoneChangeDone or
	// MilestoneChangeRemoved; Reason explains a removal.
	Kind   string
	Reason string
	// Name is the Milestone's name when the Check-in made the change, kept
	// through later renames, and AddedDate the date it had when added, before
	// any Date Slip moved it.
	Name      string
	AddedDate time.Time
	// CreatedAt is when the change was made. Author is who made it, set only
	// on a change made outside a Check-in; a Check-in's changes are its
	// author's.
	CreatedAt time.Time
	Author    Account
}

// The kinds of MilestoneChange.
//
// Each name starts MilestoneChange: the CHECK test on milestone_changes.kind
// finds its values by that prefix (enumchecks_test.go), so a value named
// otherwise goes unchecked.
const (
	MilestoneChangeAdded   = "Added"
	MilestoneChangeDone    = MilestoneDone
	MilestoneChangeRemoved = MilestoneRemoved
)

// OverdueOn reports whether m is overdue on today: still Planned and dated
// before it, comparing calendar dates, with today's the date it falls on in its
// own location. A caller passes today in the Goal's timezone (see
// Service.GoalTimezone) to judge by the Goal's calendar, so on its own date a
// Milestone is not yet overdue. Every overdue judgement is this one.
func (m Milestone) OverdueOn(today time.Time) bool {
	return m.Status == MilestonePlanned && m.TargetDate.Format(dateFormat) < today.Format(dateFormat)
}

// overdueMilestone returns the first of milestones overdue on today (see
// Milestone.OverdueOn), or ok false when none is.
func overdueMilestone(milestones []Milestone, today time.Time) (Milestone, bool) {
	for _, m := range milestones {
		if m.OverdueOn(today) {
			return m, true
		}
	}
	return Milestone{}, false
}

// MilestoneOverdueError is a refusal because Milestone is overdue — Planned and
// past its date — naming it and what the missed date rules out, so a caller
// can compose its message with the date as a page shows dates. It is an
// ErrValidation.
type MilestoneOverdueError struct {
	Milestone Milestone
	RulesOut  OverdueRulesOut
}

// OverdueRulesOut is what an overdue Milestone's missed date rules out until
// the Owner responds to it.
type OverdueRulesOut int

const (
	// OverdueRulesOutGreen: the Goal can't be Green.
	OverdueRulesOutGreen OverdueRulesOut = iota + 1
	// OverdueRulesOutNoChange: a No change can't be recorded; a Check-in must
	// move the Milestone's date, mark it Done or remove it.
	OverdueRulesOutNoChange
)

func (e *MilestoneOverdueError) Error() string {
	return fmt.Sprintf("%v: %s", ErrValidation, e.shownReason())
}

func (e *MilestoneOverdueError) Unwrap() error { return ErrValidation }

func (e *MilestoneOverdueError) shownReason() string {
	msg := fmt.Sprintf("Milestone %q is overdue (due %s)", e.Milestone.Name, e.Milestone.TargetDate.Format(dateFormat))
	switch e.RulesOut {
	case OverdueRulesOutGreen:
		msg += ", so the Goal can't be Green"
	case OverdueRulesOutNoChange:
		msg += ": submit a Check-in to move its date, mark it Done or remove it"
	}
	return msg
}

// rejectGreenWhileOverdue refuses a Green Health while any of g's Milestones,
// as they will stand after the Check-in, is overdue as of today in g's
// timezone (see GoalTimezone): a slip is never hidden behind a Green
// (CONTEXT.md: Date Slip).
func (s *Service) rejectGreenWhileOverdue(g Goal, health string, milestones []db.Milestone) error {
	if health != HealthGreen {
		return nil
	}
	today := s.goalToday(g)
	for _, m := range milestones {
		if milestone := milestoneFromRow(m); milestone.OverdueOn(today) {
			overdue := &MilestoneOverdueError{Milestone: milestone, RulesOut: OverdueRulesOutGreen}
			return &InputError{Input: milestoneDateInput(milestones, m), Message: overdue.shownReason(), Err: overdue}
		}
	}
	return nil
}

// milestoneDateInput names the Check-in input that dates m, one of milestones
// as a Check-in leaves them: the Goal's Milestones, then those it adds, which
// have no ID yet and are named by their place among the added.
func milestoneDateInput(milestones []db.Milestone, m db.Milestone) string {
	if m.ID != 0 {
		return CheckinMilestoneInput(m.ID, "date")
	}
	added := 0
	for _, r := range milestones {
		if r.ID != 0 {
			continue
		}
		if r == m {
			return CheckinNewMilestoneInput(added)
		}
		added++
	}
	return ""
}

// AddMilestoneInput is the add-Milestone command's input.
type AddMilestoneInput struct {
	GoalID     int64
	Name       string
	TargetDate time.Time
}

// AddMilestone adds a Milestone to a Goal as part of planning it, recording no
// Milestone change. Name and target date are required. Only the Goal's Owner
// or a Delegate may add one (ErrNotAuthorized), as for AddMilestoneAsAuthor.
func (s *Service) AddMilestone(ctx context.Context, actorID int64, in AddMilestoneInput) (Milestone, error) {
	var out Milestone
	err := s.WithinTx(ctx, func(tx *Service) error {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return Refuse(ErrValidation, "a Milestone needs a name")
		}
		if in.TargetDate.IsZero() {
			return Refuse(ErrValidation, "a Milestone needs a date")
		}
		goal, err := tx.queries.GetGoal(ctx, in.GoalID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Refuse(ErrValidation, "goal does not exist")
			}
			return fmt.Errorf("look up goal: %w", err)
		}
		g := goalFromRow(goal.Goal, goal.Account)
		if err := milestoneEditorRefusal(tx.goalActorFor(ctx, actorID, g.ID), g); err != nil {
			return err
		}
		out, err = tx.createMilestone(ctx, in.GoalID, name, in.TargetDate, goal.Goal.Lifecycle == LifecycleActive)
		return err
	})
	if err != nil {
		return Milestone{}, err
	}
	return out, nil
}

// errMilestonesOwnerOrDelegateOnly is the refusal of anyone but a Goal's
// Owner or a Delegate who tries to change its Milestones.
var errMilestonesOwnerOrDelegateOnly = Refuse(ErrNotAuthorized, "only the Owner or a Delegate may change this Goal's Milestones")

// RequireMilestoneEditor allows actorID to change milestoneID only if they are
// its Goal's Owner or a Delegate, by milestoneEditorRefusal; anyone else is
// refused with ErrNotAuthorized, and a missing Milestone is ErrNotFound.
// EditMilestone checks it for itself, with the Goal's Lifecycle and the
// Milestone's status (milestoneEditRefusal).
func (s *Service) RequireMilestoneEditor(ctx context.Context, actorID, milestoneID int64) error {
	m, err := s.queries.GetMilestone(ctx, milestoneID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: milestone %d", ErrNotFound, milestoneID)
		}
		return fmt.Errorf("look up milestone: %w", err)
	}
	return s.goalRule(ctx, actorID, m.GoalID, milestoneEditorRefusal)
}

// milestonesAddable reports whether a Goal in lifecycle takes a Milestone
// added outside a Check-in: until it is Done or Cancelled (CONTEXT.md:
// Milestone).
func milestonesAddable(lifecycle string) bool {
	switch lifecycle {
	case LifecycleProposed, LifecycleActive, LifecycleOnHold:
		return true
	}
	return false
}

// AddMilestoneAsAuthor adds a Milestone to a Goal outside a Check-in, from the
// Goal page, as authorID: only its Owner or a Delegate may, and only while the
// Goal is Proposed, Active or On Hold. Name and target date are required. An
// addition while the Goal is Active or On Hold is recorded as a Milestone
// change with its author and no Check-in, for the Goal's history; one while it
// is Proposed is part of its planning, and isn't. Only an addition while
// Active counts toward Milestone Churn (CONTEXT.md: Milestone, Milestone
// Churn). The Goal is read in the same transaction as the addition, so a
// Check-in that ends it meanwhile can't let one through.
func (s *Service) AddMilestoneAsAuthor(ctx context.Context, authorID int64, in AddMilestoneInput) (Milestone, error) {
	var out Milestone
	err := s.WithinTx(ctx, func(tx *Service) error {
		goal, err := tx.queries.GetGoal(ctx, in.GoalID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: goal %d", ErrNotFound, in.GoalID)
			}
			return fmt.Errorf("look up goal: %w", err)
		}
		g := goalFromRow(goal.Goal, goal.Account)
		if err := milestoneAdditionRefusal(tx.goalActorFor(ctx, authorID, g.ID), g); err != nil {
			return err
		}
		lifecycle := g.Lifecycle
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return Refuse(ErrValidation, "a Milestone needs a name")
		}
		if in.TargetDate.IsZero() {
			return Refuse(ErrValidation, "a Milestone needs a date")
		}
		m, err := tx.createMilestone(ctx, goal.Goal.ID, name, in.TargetDate, lifecycle == LifecycleActive)
		if err != nil {
			return err
		}
		if lifecycle != LifecycleProposed {
			if err := tx.recordMilestoneChange(ctx, nil, authorID, m.ID, m.Name, MilestoneChangeAdded, ""); err != nil {
				return err
			}
		}
		out = m
		return nil
	})
	if err != nil {
		return Milestone{}, err
	}
	return out, nil
}

// createMilestone inserts a Planned Milestone. addedWhileActive marks one added
// once its Goal was Active, which counts toward Milestone Churn.
func (s *Service) createMilestone(ctx context.Context, goalID int64, name string, targetDate time.Time, addedWhileActive bool) (Milestone, error) {
	var active int64
	if addedWhileActive {
		active = 1
	}
	row, err := s.queries.CreateMilestone(ctx, db.CreateMilestoneParams{
		GoalID:           goalID,
		Name:             name,
		TargetDate:       targetDate.Format(dateFormat),
		CreatedAt:        s.clock.Now().Format(timeFormat),
		AddedWhileActive: active,
	})
	if err != nil {
		return Milestone{}, fmt.Errorf("create milestone: %w", err)
	}
	return milestoneFromRow(row), nil
}

// EditMilestoneInput is the edit-Milestone command's input.
type EditMilestoneInput struct {
	MilestoneID int64
	Name        string
	TargetDate  time.Time
}

// EditMilestone changes a Milestone's name and date. Both are required. Only
// its Goal's Owner or a Delegate may edit it (ErrNotAuthorized; see
// RequireMilestoneEditor), and only while the Goal is Proposed and the
// Milestone Planned (ErrValidation), by milestoneEditRefusal: after that its
// date moves in a Check-in as a Date Slip, and it keeps its name.
func (s *Service) EditMilestone(ctx context.Context, actorID int64, in EditMilestoneInput) (Milestone, error) {
	var row db.Milestone
	err := s.WithinTx(ctx, func(tx *Service) error {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return Refuse(ErrValidation, "a Milestone needs a name")
		}
		if in.TargetDate.IsZero() {
			return Refuse(ErrValidation, "a Milestone needs a date")
		}
		current, err := tx.queries.GetMilestone(ctx, in.MilestoneID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: milestone %d", ErrNotFound, in.MilestoneID)
			}
			return fmt.Errorf("look up milestone: %w", err)
		}
		if err := tx.goalRule(ctx, actorID, current.GoalID, func(a goalActor, g Goal) error {
			return milestoneEditRefusal(a, g, current.Status)
		}); err != nil {
			return err
		}
		row, err = tx.queries.UpdateMilestone(ctx, db.UpdateMilestoneParams{
			Name:       name,
			TargetDate: in.TargetDate.Format(dateFormat),
			ID:         in.MilestoneID,
		})
		if err != nil {
			return fmt.Errorf("update milestone: %w", err)
		}
		return nil
	})
	if err != nil {
		return Milestone{}, err
	}
	return milestoneFromRow(row), nil
}

// MilestoneChangeInput is one Milestone's change in a Check-in (CONTEXT.md:
// Check-in carries Date Slips and Milestone changes). Only a Planned Milestone
// can change. TargetDate moves its date, recording a Date Slip that needs
// DateReason; the zero time, or its current date, leaves it unchanged. Status
// marks it MilestoneDone or MilestoneRemoved (which needs RemovedReason); empty
// or MilestonePlanned leaves it Planned.
type MilestoneChangeInput struct {
	MilestoneID   int64
	TargetDate    time.Time
	DateReason    string
	Status        string
	RemovedReason string
}

// NewMilestoneInput is a Milestone added in a Check-in. Name and date are
// required.
type NewMilestoneInput struct {
	Name       string
	TargetDate time.Time
}

// milestoneStatusChange is a validated Done or Removed marking waiting to be
// written with its Check-in, with the Milestone's name to record it under.
type milestoneStatusChange struct {
	milestoneID   int64
	name          string
	status        string
	removedReason string
}

// planMilestoneStatus validates a Check-in's new status for a Planned Milestone.
// Empty or Planned is no change (nil); Removed needs a reason.
func planMilestoneStatus(m db.Milestone, status, removedReason string) (*milestoneStatusChange, error) {
	switch status {
	case "", MilestonePlanned:
		return nil, nil
	case MilestoneDone:
		return &milestoneStatusChange{milestoneID: m.ID, name: m.Name, status: MilestoneDone}, nil
	case MilestoneRemoved:
		reason := strings.TrimSpace(removedReason)
		if reason == "" {
			return nil, inputError(CheckinMilestoneInput(m.ID, "removed_reason"), "removing Milestone %q needs a reason", m.Name)
		}
		return &milestoneStatusChange{milestoneID: m.ID, name: m.Name, status: MilestoneRemoved, removedReason: reason}, nil
	default:
		return nil, Refuse(ErrValidation, "a Milestone must be %q, %q, or %q", MilestonePlanned, MilestoneDone, MilestoneRemoved)
	}
}

// milestonePlan is a Check-in's validated Milestone changes: the Date Slips
// they make, the Milestones they mark Done or Removed, the Milestones they add,
// and the Goal's Milestones as they will stand once applied.
type milestonePlan struct {
	slips     []slipPlan
	statuses  []milestoneStatusChange
	added     []NewMilestoneInput
	resulting []db.Milestone
}

// planMilestoneChanges validates a Check-in's Milestone changes against the
// Goal's Milestones — each must name a Milestone on this Goal, at most once —
// and the Milestones it adds, each of which needs a name and a date.
func (s *Service) planMilestoneChanges(ctx context.Context, goalID int64, changes []MilestoneChangeInput, added []NewMilestoneInput) (milestonePlan, error) {
	rows, err := s.queries.ListMilestones(ctx, goalID)
	if err != nil {
		return milestonePlan{}, fmt.Errorf("list milestones: %w", err)
	}
	index := make(map[int64]int, len(rows))
	for i, r := range rows {
		index[r.ID] = i
	}
	seen := make(map[int64]bool, len(changes))
	plan := milestonePlan{resulting: rows}
	for _, ch := range changes {
		i, ok := index[ch.MilestoneID]
		if !ok {
			return milestonePlan{}, Refuse(ErrValidation, "milestone %d is not on this Goal", ch.MilestoneID)
		}
		m := &plan.resulting[i]
		if seen[m.ID] {
			return milestonePlan{}, Refuse(ErrValidation, "Milestone %q is changed twice", m.Name)
		}
		seen[m.ID] = true
		if m.Status != MilestonePlanned {
			return milestonePlan{}, Refuse(ErrValidation, "Milestone %q is already %s", m.Name, m.Status)
		}
		slip, err := planMilestoneSlip(*m, ch.TargetDate, ch.DateReason)
		if err != nil {
			return milestonePlan{}, err
		}
		if slip != nil {
			plan.slips = append(plan.slips, *slip)
			m.TargetDate = slip.newDate.Format(dateFormat)
		}
		change, err := planMilestoneStatus(*m, ch.Status, ch.RemovedReason)
		if err != nil {
			return milestonePlan{}, err
		}
		if change != nil {
			plan.statuses = append(plan.statuses, *change)
			m.Status = change.status
		}
	}
	for i, a := range added {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return milestonePlan{}, inputError(CheckinNewMilestoneInput(i), "a new Milestone needs a name")
		}
		if a.TargetDate.IsZero() {
			return milestonePlan{}, inputError(CheckinNewMilestoneInput(i), "new Milestone %q needs a date", name)
		}
		plan.added = append(plan.added, NewMilestoneInput{Name: name, TargetDate: a.TargetDate})
		plan.resulting = append(plan.resulting, db.Milestone{Name: name, TargetDate: a.TargetDate.Format(dateFormat), Status: MilestonePlanned})
	}
	return plan, nil
}

// apply writes the plan's Date Slips, added Milestones, and Done and Removed
// markings against checkinID, recording each addition and marking as one of
// the Check-in's Milestone changes, made by its author authorID. Milestones
// added in a Check-in are added while the Goal is Active, so they count toward
// its Milestone Churn.
func (p milestonePlan) apply(ctx context.Context, tx *Service, goalID, checkinID, authorID int64) error {
	for _, slip := range p.slips {
		if err := tx.recordSlip(ctx, goalID, checkinID, slip); err != nil {
			return err
		}
	}
	for _, a := range p.added {
		m, err := tx.createMilestone(ctx, goalID, a.Name, a.TargetDate, true)
		if err != nil {
			return err
		}
		if err := tx.recordMilestoneChange(ctx, &checkinID, authorID, m.ID, m.Name, MilestoneChangeAdded, ""); err != nil {
			return err
		}
	}
	for _, change := range p.statuses {
		if err := tx.recordMilestoneStatus(ctx, checkinID, authorID, change); err != nil {
			return err
		}
	}
	return nil
}

// recordMilestoneStatus writes a Check-in's Done or Removed marking and
// records it against checkinID, made by its author authorID.
func (s *Service) recordMilestoneStatus(ctx context.Context, checkinID, authorID int64, c milestoneStatusChange) error {
	if err := s.queries.SetMilestoneStatus(ctx, db.SetMilestoneStatusParams{
		Status:        c.status,
		RemovedReason: c.removedReason,
		ID:            c.milestoneID,
	}); err != nil {
		return fmt.Errorf("set milestone status: %w", err)
	}
	return s.recordMilestoneChange(ctx, &checkinID, authorID, c.milestoneID, c.name, c.status, c.removedReason)
}

// recordMilestoneChange records that authorID made a Milestone change of kind
// to milestoneID, in the Check-in checkinID or, when it is nil, outside one,
// under the name it has now so a later rename leaves the record as it was,
// stamped with the Service's clock.
func (s *Service) recordMilestoneChange(ctx context.Context, checkinID *int64, authorID, milestoneID int64, name, kind, reason string) error {
	if _, err := s.queries.CreateMilestoneChange(ctx, db.CreateMilestoneChangeParams{
		CheckinID:   checkinID,
		AuthorID:    &authorID,
		MilestoneID: milestoneID,
		Kind:        kind,
		Reason:      reason,
		Name:        name,
		CreatedAt:   s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("record milestone change: %w", err)
	}
	return nil
}

// MilestoneChangesOutsideCheckins returns the Milestone changes recorded on a
// Goal outside any Check-in — the Milestones its Owner or a Delegate added
// from the Goal page while it was Active or On Hold — oldest first, each with
// its author and when it was made. A Check-in's own changes come with it from
// ListCheckins.
func (s *Service) MilestoneChangesOutsideCheckins(ctx context.Context, goalID int64) ([]MilestoneChange, error) {
	rows, err := s.queries.ListMilestoneChangesByGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list milestone changes: %w", err)
	}
	authors := map[int64]Account{}
	return milestoneChangesOutsideCheckins(rows, func(id int64) (Account, error) {
		if author, ok := authors[id]; ok {
			return author, nil
		}
		row, err := s.queries.GetAccount(ctx, id)
		if err != nil {
			return Account{}, err
		}
		authors[id] = accountFromRow(row)
		return authors[id], nil
	})
}

// milestoneChangesOutsideCheckins is MilestoneChangesOutsideCheckins read from
// a Goal's Milestone changes, each author looked up with account.
func milestoneChangesOutsideCheckins(rows []db.ListMilestoneChangesByGoalRow, account func(id int64) (Account, error)) ([]MilestoneChange, error) {
	var out []MilestoneChange
	for _, r := range rows {
		if r.MilestoneChange.CheckinID != nil || r.MilestoneChange.AuthorID == nil {
			continue
		}
		c := milestoneChangeFromRow(r)
		author, err := account(*r.MilestoneChange.AuthorID)
		if err != nil {
			return nil, fmt.Errorf("look up milestone change author: %w", err)
		}
		c.Author = author
		out = append(out, c)
	}
	return out, nil
}

// MilestoneChurn returns the number of Milestones added or removed on a Goal
// since it became Active — a scope-creep and capacity signal read alongside its
// Date Slips (CONTEXT.md: Milestone Churn). Marking a Milestone Done is not
// churn.
func (s *Service) MilestoneChurn(ctx context.Context, goalID int64) (int, error) {
	n, err := s.queries.CountMilestoneChurn(ctx, goalID)
	if err != nil {
		return 0, fmt.Errorf("count milestone churn: %w", err)
	}
	return int(n), nil
}

// ListMilestones returns a Goal's Milestones, earliest target date first.
func (s *Service) ListMilestones(ctx context.Context, goalID int64) ([]Milestone, error) {
	rows, err := s.queries.ListMilestones(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	out := make([]Milestone, 0, len(rows))
	for _, r := range rows {
		out = append(out, milestoneFromRow(r))
	}
	return out, nil
}

// MilestonesByGoal returns each of goalIDs' Milestones, keyed by Goal ID, as
// ListMilestones returns one Goal's, in one query, so a page or file listing
// many Goals reads them all at once. A Goal with no Milestones has no entry.
func (s *Service) MilestonesByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Milestone, error) {
	return s.milestonesByGoal(ctx, goalIDs)
}

// milestonesByGoal returns each of goalIDs' Milestones, as ListMilestones
// reads one Goal's, keyed by Goal ID, in one query. No Goals reads nothing.
func (s *Service) milestonesByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Milestone, error) {
	out := map[int64][]Milestone{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListMilestonesForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	for _, r := range rows {
		out[r.GoalID] = append(out[r.GoalID], milestoneFromRow(r))
	}
	return out, nil
}

func milestoneChangeFromRow(r db.ListMilestoneChangesByGoalRow) MilestoneChange {
	addedDate, _ := time.Parse(dateFormat, r.AddedDate)
	createdAt, _ := time.Parse(timeFormat, r.MilestoneChange.CreatedAt)
	return MilestoneChange{
		ID:          r.MilestoneChange.ID,
		MilestoneID: r.MilestoneChange.MilestoneID,
		Kind:        r.MilestoneChange.Kind,
		Reason:      r.MilestoneChange.Reason,
		Name:        r.MilestoneChange.Name,
		AddedDate:   addedDate,
		CreatedAt:   createdAt,
	}
}

func milestoneFromRow(m db.Milestone) Milestone {
	targetDate, _ := time.Parse(dateFormat, m.TargetDate)
	createdAt, _ := time.Parse(timeFormat, m.CreatedAt)
	return Milestone{
		ID:            m.ID,
		GoalID:        m.GoalID,
		Name:          m.Name,
		TargetDate:    targetDate,
		Status:        m.Status,
		RemovedReason: m.RemovedReason,
		CreatedAt:     createdAt,
	}
}
