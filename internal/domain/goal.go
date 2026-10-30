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

// Goal is the single unit of work being tracked (CONTEXT.md: Goal).
type Goal struct {
	ID           int64
	Title        string
	WhyItMatters string
	Owner        Account
	Lifecycle    string
	// Kind is "" until the Owner marks the Goal GoalDated or GoalOngoing.
	Kind string
	// DeliveryDate is the delivery date of a Dated Goal, and the zero time for
	// an Ongoing Goal or a Goal not yet marked.
	DeliveryDate time.Time
	// CadenceDays is how often a Check-in is expected (7 by default).
	CadenceDays int
	CreatedAt   time.Time
	// ActivatedAt is when the Goal became Active, the zero time until then. A
	// Goal activated before this was recorded reads its creation time instead.
	ActivatedAt time.Time
	// Ownerless is true when the Owner has departed the org and the Goal has not
	// been reassigned; it is surfaced as prominently as Red (CONTEXT.md:
	// Ownerless). It is derived from the Owner, not stored on the Goal.
	Ownerless bool
	// TopLevel is true when an Admin has marked the Goal as one of the org's
	// root outcomes, so it is never Unlinked (CONTEXT.md: Top-level Goal).
	TopLevel bool
}

// Goal kinds. A Dated Goal has a delivery date; an Ongoing Goal has none and is
// judged against its Metrics (CONTEXT.md: Dated Goal, Ongoing Goal). A fresh
// Proposed Goal is neither yet, carrying the empty kind.
//
// Each name starts Goal: the CHECK test on goals.kind finds its values by that
// prefix (enumchecks_test.go), so a value named otherwise goes unchecked.
const (
	GoalDated   = "Dated"
	GoalOngoing = "Ongoing"
)

// dateFormat is how calendar dates (delivery dates, target dates) are stored,
// as distinct from the timestamp format used for instants.
const dateFormat = "2006-01-02"

// SuggestDeliveryDate suggests a delivery date for the date picker: the nearest
// Monday on or after `from` that sits at a natural monthly checkpoint — either
// mid-month (the Monday nearest the 15th) or end-of-month (the last Monday of
// the month). Teams tend to deliver on those cadence points, so the picker
// prefills the closest one rather than an arbitrary day.
func SuggestDeliveryDate(from time.Time) time.Time {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	var best time.Time
	// Look across this month and the next two so there is always a candidate on
	// or after `from`, even late in a month.
	for offset := 0; offset < 3; offset++ {
		month := from.AddDate(0, offset, 0)
		for _, cand := range []time.Time{midMonthMonday(month), lastMondayOfMonth(month)} {
			if cand.Before(from) {
				continue
			}
			if best.IsZero() || cand.Before(best) {
				best = cand
			}
		}
	}
	return best
}

// midMonthMonday returns the Monday nearest the 15th of the given month.
func midMonthMonday(t time.Time) time.Time {
	fifteenth := time.Date(t.Year(), t.Month(), 15, 0, 0, 0, 0, t.Location())
	return nearestMonday(fifteenth)
}

// lastMondayOfMonth returns the last Monday of the given month.
func lastMondayOfMonth(t time.Time) time.Time {
	firstOfNext := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
	lastDay := firstOfNext.AddDate(0, 0, -1)
	offsetBack := (int(lastDay.Weekday()) - int(time.Monday) + 7) % 7
	return lastDay.AddDate(0, 0, -offsetBack)
}

// nearestMonday returns the Monday closest to d, preferring the earlier Monday
// on a tie.
func nearestMonday(d time.Time) time.Time {
	offsetBack := (int(d.Weekday()) - int(time.Monday) + 7) % 7
	if offsetBack <= 3 {
		return d.AddDate(0, 0, -offsetBack)
	}
	return d.AddDate(0, 0, 7-offsetBack)
}

// ErrValidation is returned when a command's input is not acceptable, e.g. a
// Goal created without a title or a Why it matters.
var ErrValidation = errors.New("validation failed")

// CreateGoalInput is the create-Goal command's input.
type CreateGoalInput struct {
	Title        string
	WhyItMatters string
	OwnerID      int64
}

// CreateGoal creates a Proposed Goal owned by OwnerID, made by actorID. Who
// may make it is goalCreationRefusal's rule: its Owner, or the Admin whose
// import creates it (ForImportBy). Title and Why it matters are required
// (CONTEXT.md: Why it matters is required when the Goal is created).
func (s *Service) CreateGoal(ctx context.Context, actorID int64, in CreateGoalInput) (Goal, error) {
	var row db.Goal
	var owner db.Account
	err := s.WithinTx(ctx, func(tx *Service) error {
		if err := goalCreationRefusal(tx.goalCreatorActor(ctx, actorID), in.OwnerID); err != nil {
			return err
		}
		title := strings.TrimSpace(in.Title)
		whyItMatters := strings.TrimSpace(in.WhyItMatters)
		if title == "" {
			return Refuse(ErrValidation, "a Goal needs a title")
		}
		if whyItMatters == "" {
			return Refuse(ErrValidation, "a Goal needs to say why it matters")
		}

		var err error
		owner, err = tx.queries.GetAccount(ctx, in.OwnerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Refuse(ErrValidation, "owner does not exist")
			}
			return fmt.Errorf("look up owner: %w", err)
		}

		now := tx.clock.Now()
		row, err = tx.queries.CreateGoal(ctx, db.CreateGoalParams{
			Title:        title,
			WhyItMatters: whyItMatters,
			OwnerID:      in.OwnerID,
			Lifecycle:    LifecycleProposed,
			CreatedAt:    now.Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create goal: %w", err)
		}

		// Keep the Why it matters as its first revision so later edits are auditable
		// (CONTEXT.md: Why it matters edits are tracked). It commits with the Goal,
		// so no Goal is ever without one (#421).
		if _, err := tx.queries.CreateWhyItMattersRevision(ctx, db.CreateWhyItMattersRevisionParams{
			GoalID:       row.ID,
			WhyItMatters: whyItMatters,
			AuthorID:     in.OwnerID,
			CreatedAt:    now.Format(timeFormat),
		}); err != nil {
			return fmt.Errorf("record why it matters revision: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	s.recordImportedGoal(row.ID)

	return goalFromRow(row, owner), nil
}

// WhyItMattersRevision is one version of a Goal's Why it matters, kept so edits are auditable
// and viewable (CONTEXT.md: Why it matters edits are tracked).
type WhyItMattersRevision struct {
	ID           int64
	GoalID       int64
	WhyItMatters string
	Author       Account
	CreatedAt    time.Time
}

// EditWhyItMatters replaces a Goal's Why it matters, keeping the previous text as a revision.
// The new Why it matters is required (CONTEXT.md: Why it matters). authorID records who made
// the edit, and must be the Goal's Owner or an Admin (ErrNotAuthorized; see
// RequireGoalOwnerOrAdmin). The text and its revision are written together or
// not at all, in the caller's transaction when it has one.
func (s *Service) EditWhyItMatters(ctx context.Context, goalID int64, whyItMatters string, authorID int64) (Goal, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		whyItMatters = strings.TrimSpace(whyItMatters)
		if whyItMatters == "" {
			return Refuse(ErrValidation, "a Goal needs to say why it matters")
		}
		if err := tx.RequireGoalOwnerOrAdmin(ctx, authorID, goalID); err != nil {
			return err
		}
		if _, err := tx.queries.SetGoalWhyItMatters(ctx, db.SetGoalWhyItMattersParams{
			WhyItMatters: whyItMatters,
			ID:           goalID,
		}); err != nil {
			return fmt.Errorf("set why it matters: %w", err)
		}
		if _, err := tx.queries.CreateWhyItMattersRevision(ctx, db.CreateWhyItMattersRevisionParams{
			GoalID:       goalID,
			WhyItMatters: whyItMatters,
			AuthorID:     authorID,
			CreatedAt:    tx.clock.Now().Format(timeFormat),
		}); err != nil {
			return fmt.Errorf("record why it matters revision: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}

// ListWhyItMattersRevisions returns a Goal's Why it matters history, newest first, each with
// its author resolved.
func (s *Service) ListWhyItMattersRevisions(ctx context.Context, goalID int64) ([]WhyItMattersRevision, error) {
	rows, err := s.queries.ListWhyItMattersRevisions(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list why it matters revisions: %w", err)
	}
	out := make([]WhyItMattersRevision, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.WhyItMattersRevision.CreatedAt)
		out = append(out, WhyItMattersRevision{
			ID:           r.WhyItMattersRevision.ID,
			GoalID:       r.WhyItMattersRevision.GoalID,
			WhyItMatters: r.WhyItMattersRevision.WhyItMatters,
			Author:       accountFromRow(r.Account),
			CreatedAt:    createdAt,
		})
	}
	return out, nil
}

// ActivateGoal moves a Proposed Goal to Active once it meets the minimum
// standard (CONTEXT.md: Lifecycle). The Goal must have a Why it matters, an Owner, and
// either a delivery date plus at least one Milestone or Metric (Dated) or at
// least one Metric (Ongoing), and a value in every Dimension and Field an Admin
// has marked required (CONTEXT.md: Incomplete). Every unmet requirement comes back as its own
// validation error, joined together, so the Owner sees the whole list at once;
// use ActivationReasons to enumerate them. The Goal stays Proposed on rejection.
// Only the Goal's Owner may activate it (ErrNotAuthorized), and only while it
// is Proposed: any other Lifecycle is refused (ErrValidation) before the
// standard is checked. The checks and the write run in one transaction.
func (s *Service) ActivateGoal(ctx context.Context, actorID, goalID int64) (Goal, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		g, err := tx.openGoalCommand(ctx, actorID, goalID, activateGoalRefusal, nil)
		if err != nil {
			return err
		}
		reqs, err := tx.judgeGoalActivation(ctx, g)
		if err != nil {
			return err
		}
		if err := activationRefusal(reqs); err != nil {
			return err
		}
		if err := tx.queries.ActivateGoal(ctx, db.ActivateGoalParams{
			ActivatedAt: tx.clock.Now().Format(timeFormat),
			ID:          goalID,
		}); err != nil {
			return fmt.Errorf("activate goal: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}

// countMilestonesAndMetrics returns how many Milestones and Metrics a Goal has.
func (s *Service) countMilestonesAndMetrics(ctx context.Context, goalID int64) (milestones, metrics int, err error) {
	ms, err := s.queries.ListMilestones(ctx, goalID)
	if err != nil {
		return 0, 0, fmt.Errorf("list milestones: %w", err)
	}
	mt, err := s.queries.ListMetrics(ctx, goalID)
	if err != nil {
		return 0, 0, fmt.Errorf("list metrics: %w", err)
	}
	return len(ms), len(mt), nil
}

// ActivationReasons splits an error from ActivateGoal into the individual
// validation reasons it joins together, so a caller can show each missing item
// separately. A non-activation error is returned as a single-element slice.
func ActivationReasons(err error) []error {
	if err == nil {
		return nil
	}
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		return joined.Unwrap()
	}
	return []error{err}
}

// ViewGoal returns the Goal with the given id, with its Owner resolved. This is
// the Goal-view query. It returns ErrNotFound if no such Goal exists.
func (s *Service) ViewGoal(ctx context.Context, id int64) (Goal, error) {
	row, err := s.queries.GetGoal(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: goal %d", ErrNotFound, id)
		}
		return Goal{}, fmt.Errorf("get goal: %w", err)
	}
	return goalFromRow(row.Goal, row.Account), nil
}

// ListGoals returns every Goal, newest first, each with its Owner resolved.
func (s *Service) ListGoals(ctx context.Context) ([]Goal, error) {
	rows, err := s.queries.ListGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}

func goalFromRow(g db.Goal, owner db.Account) Goal {
	createdAt, _ := time.Parse(timeFormat, g.CreatedAt)
	var deliveryDate time.Time
	if g.DeliveryDate != "" {
		deliveryDate, _ = time.Parse(dateFormat, g.DeliveryDate)
	}
	var activatedAt time.Time
	switch {
	case g.ActivatedAt != "":
		activatedAt, _ = time.Parse(timeFormat, g.ActivatedAt)
	case g.Lifecycle != LifecycleProposed:
		activatedAt = createdAt
	}
	acc := accountFromRow(owner)
	return Goal{
		ID:           g.ID,
		Title:        g.Title,
		WhyItMatters: g.WhyItMatters,
		Owner:        acc,
		Lifecycle:    g.Lifecycle,
		Kind:         g.Kind,
		DeliveryDate: deliveryDate,
		CadenceDays:  int(g.CadenceDays),
		CreatedAt:    createdAt,
		ActivatedAt:  activatedAt,
		Ownerless:    acc.Departed,
		TopLevel:     g.TopLevel != 0,
	}
}

// loadGoal re-reads a Goal with its Owner resolved, for returning the current
// state after a command has changed it.
func (s *Service) loadGoal(ctx context.Context, id int64) (Goal, error) {
	row, err := s.queries.GetGoal(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: goal %d", ErrNotFound, id)
		}
		return Goal{}, fmt.Errorf("get goal: %w", err)
	}
	return goalFromRow(row.Goal, row.Account), nil
}

// RequireGoalOwner returns ErrNotAuthorized unless actorID is the Goal's Owner
// (a Delegate and an Admin are refused too), or the Admin whose import created
// it, by goalOwnerRefusal; ErrNotFound if no such Goal exists. The commands
// only the Owner may make — MarkGoalDated, SetCadence, MarkGoalOngoing and the
// like — check it for themselves, inside their transaction.
func (s *Service) RequireGoalOwner(ctx context.Context, actorID, goalID int64) error {
	_, err := s.openGoalCommand(ctx, actorID, goalID, func(a goalActor, g Goal) error {
		return goalOwnerRefusal(a, g, "change it")
	}, nil)
	return err
}

// MarkGoalDated marks the Goal as Dated with the given delivery date, against
// which its Health will be judged (CONTEXT.md: Dated Goal). Only the Goal's
// Owner may mark it (ErrNotAuthorized), and only while it is Proposed
// (setDeliveryRefusal); then a zero date is rejected, since a Dated Goal needs
// a delivery date.
func (s *Service) MarkGoalDated(ctx context.Context, actorID, goalID int64, deliveryDate time.Time) (Goal, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.openGoalCommand(ctx, actorID, goalID, setDeliveryRefusal, nil); err != nil {
			return err
		}
		if deliveryDate.IsZero() {
			return Refuse(ErrValidation, "a Dated Goal needs a delivery date")
		}
		if _, err := tx.queries.SetGoalKind(ctx, db.SetGoalKindParams{
			Kind:         GoalDated,
			DeliveryDate: deliveryDate.Format(dateFormat),
			ID:           goalID,
		}); err != nil {
			return fmt.Errorf("mark goal dated: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}

// MaxCadenceDays is the longest Check-in cadence a Goal may have, in days.
const MaxCadenceDays = 90

// SetCadence sets how often a Check-in is expected on the Goal, in days. It
// must be positive and at most MaxCadenceDays (CONTEXT.md: Check-in cadence,
// 7 days by default). Only the Goal's Owner may set it (ErrNotAuthorized).
func (s *Service) SetCadence(ctx context.Context, actorID, goalID int64, days int) (Goal, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.RequireGoalOwner(ctx, actorID, goalID); err != nil {
			return err
		}
		if days <= 0 {
			return Refuse(ErrValidation, "the Check-in cadence must be a positive number of days")
		}
		if days > MaxCadenceDays {
			return Refuse(ErrValidation, "the Check-in cadence can be at most %d days", MaxCadenceDays)
		}
		if _, err := tx.queries.SetGoalCadence(ctx, db.SetGoalCadenceParams{
			CadenceDays: int64(days),
			ID:          goalID,
		}); err != nil {
			return fmt.Errorf("set cadence: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}

// MarkGoalOngoing marks the Goal as Ongoing, clearing any delivery date; its
// Health is judged against its Metrics instead (CONTEXT.md: Ongoing Goal).
// Only the Goal's Owner may mark it (ErrNotAuthorized), and only while it is
// Proposed (setDeliveryRefusal).
func (s *Service) MarkGoalOngoing(ctx context.Context, actorID, goalID int64) (Goal, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.openGoalCommand(ctx, actorID, goalID, setDeliveryRefusal, nil); err != nil {
			return err
		}
		if _, err := tx.queries.SetGoalKind(ctx, db.SetGoalKindParams{
			Kind:         GoalOngoing,
			DeliveryDate: "",
			ID:           goalID,
		}); err != nil {
			return fmt.Errorf("mark goal ongoing: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}
