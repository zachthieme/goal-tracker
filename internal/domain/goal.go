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
	ID        int64
	Title     string
	SoWhat    string
	Owner     Account
	Lifecycle string
	// Kind is "" until the Owner marks the Goal GoalDated or GoalOngoing.
	Kind string
	// DeliveryDate is the delivery date of a Dated Goal, and the zero time for
	// an Ongoing Goal or a Goal not yet marked.
	DeliveryDate time.Time
	// CadenceDays is how often a Check-in is expected (7 by default).
	CadenceDays int
	CreatedAt   time.Time
	// Ownerless is true when the Owner has departed the org and the Goal has not
	// been reassigned; it is surfaced as prominently as Red (CONTEXT.md:
	// Ownerless). It is derived from the Owner, not stored on the Goal.
	Ownerless bool
	// TopLevel is true when an Admin has marked the Goal as one of the org's
	// root outcomes, so it is never Unaligned (CONTEXT.md: Top-level Goal).
	TopLevel bool
}

// Lifecycle values a Goal can be in (CONTEXT.md: Lifecycle). A Goal starts
// Proposed and is activated through the activation gate; after that its
// Lifecycle changes only in a Check-in (see lifecycle.go).
const (
	LifecycleProposed  = "Proposed"
	LifecycleActive    = "Active"
	LifecycleOnHold    = "On Hold"
	LifecycleDone      = "Done"
	LifecycleCancelled = "Cancelled"
)

// Goal kinds. A Dated Goal has a delivery date; an Ongoing Goal has none and is
// judged against its Metrics (CONTEXT.md: Dated Goal, Ongoing Goal). A fresh
// Proposed Goal is neither yet, carrying the empty kind.
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
// Goal created without a title or a So What.
var ErrValidation = errors.New("validation failed")

// CreateGoalInput is the create-Goal command's input.
type CreateGoalInput struct {
	Title   string
	SoWhat  string
	OwnerID int64
}

// CreateGoal creates a Proposed Goal owned by OwnerID. Title and So What are
// required (CONTEXT.md: So What is required when the Goal is created).
func (s *Service) CreateGoal(ctx context.Context, in CreateGoalInput) (Goal, error) {
	title := strings.TrimSpace(in.Title)
	soWhat := strings.TrimSpace(in.SoWhat)
	if title == "" {
		return Goal{}, fmt.Errorf("%w: a Goal needs a title", ErrValidation)
	}
	if soWhat == "" {
		return Goal{}, fmt.Errorf("%w: a Goal needs a So What", ErrValidation)
	}

	owner, err := s.queries.GetAccount(ctx, in.OwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: owner does not exist", ErrValidation)
		}
		return Goal{}, fmt.Errorf("look up owner: %w", err)
	}

	now := s.clock.Now()
	row, err := s.queries.CreateGoal(ctx, db.CreateGoalParams{
		Title:     title,
		SoWhat:    soWhat,
		OwnerID:   in.OwnerID,
		Lifecycle: LifecycleProposed,
		CreatedAt: now.Format(timeFormat),
	})
	if err != nil {
		return Goal{}, fmt.Errorf("create goal: %w", err)
	}

	// Keep the So What as its first revision so later edits are auditable
	// (CONTEXT.md: So What edits are tracked).
	if _, err := s.queries.CreateSoWhatRevision(ctx, db.CreateSoWhatRevisionParams{
		GoalID:    row.ID,
		SoWhat:    soWhat,
		AuthorID:  in.OwnerID,
		CreatedAt: now.Format(timeFormat),
	}); err != nil {
		return Goal{}, fmt.Errorf("record so what revision: %w", err)
	}

	return goalFromRow(row, owner), nil
}

// SoWhatRevision is one version of a Goal's So What, kept so edits are auditable
// and viewable (CONTEXT.md: So What edits are tracked).
type SoWhatRevision struct {
	ID        int64
	GoalID    int64
	SoWhat    string
	Author    Account
	CreatedAt time.Time
}

// EditSoWhat replaces a Goal's So What, keeping the previous text as a revision.
// The new So What is required (CONTEXT.md: So What). authorID records who made
// the edit.
func (s *Service) EditSoWhat(ctx context.Context, goalID int64, soWhat string, authorID int64) (Goal, error) {
	soWhat = strings.TrimSpace(soWhat)
	if soWhat == "" {
		return Goal{}, fmt.Errorf("%w: a Goal needs a So What", ErrValidation)
	}
	if _, err := s.queries.GetGoal(ctx, goalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: goal %d", ErrNotFound, goalID)
		}
		return Goal{}, fmt.Errorf("look up goal: %w", err)
	}

	if _, err := s.queries.SetGoalSoWhat(ctx, db.SetGoalSoWhatParams{
		SoWhat: soWhat,
		ID:     goalID,
	}); err != nil {
		return Goal{}, fmt.Errorf("set so what: %w", err)
	}
	if _, err := s.queries.CreateSoWhatRevision(ctx, db.CreateSoWhatRevisionParams{
		GoalID:    goalID,
		SoWhat:    soWhat,
		AuthorID:  authorID,
		CreatedAt: s.clock.Now().Format(timeFormat),
	}); err != nil {
		return Goal{}, fmt.Errorf("record so what revision: %w", err)
	}
	return s.loadGoal(ctx, goalID)
}

// ListSoWhatRevisions returns a Goal's So What history, newest first, each with
// its author resolved.
func (s *Service) ListSoWhatRevisions(ctx context.Context, goalID int64) ([]SoWhatRevision, error) {
	rows, err := s.queries.ListSoWhatRevisions(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list so what revisions: %w", err)
	}
	out := make([]SoWhatRevision, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.SoWhatRevision.CreatedAt)
		out = append(out, SoWhatRevision{
			ID:        r.SoWhatRevision.ID,
			GoalID:    r.SoWhatRevision.GoalID,
			SoWhat:    r.SoWhatRevision.SoWhat,
			Author:    accountFromRow(r.Account),
			CreatedAt: createdAt,
		})
	}
	return out, nil
}

// ActivateGoal moves a Proposed Goal to Active once it meets the minimum
// standard (CONTEXT.md: Lifecycle). The Goal must have a So What, an Owner, and
// either a delivery date plus at least one Milestone or Metric (Dated) or at
// least one Metric (Ongoing). Every unmet requirement comes back as its own
// validation error, joined together, so the Owner sees the whole list at once;
// use ActivationReasons to enumerate them. The Goal stays Proposed on rejection.
func (s *Service) ActivateGoal(ctx context.Context, goalID int64) (Goal, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return Goal{}, err
	}

	var reasons []error
	// So What and Owner are guaranteed by CreateGoal/EditSoWhat and the schema,
	// but the gate states the full minimum standard so it still holds if those
	// invariants ever change.
	if strings.TrimSpace(g.SoWhat) == "" {
		reasons = append(reasons, fmt.Errorf("%w: a Goal needs a So What to become Active", ErrValidation))
	}
	if g.Owner.ID == 0 {
		reasons = append(reasons, fmt.Errorf("%w: a Goal needs an Owner to become Active", ErrValidation))
	}

	switch g.Kind {
	case GoalDated:
		if g.DeliveryDate.IsZero() {
			reasons = append(reasons, fmt.Errorf("%w: a Dated Goal needs a delivery date", ErrValidation))
		}
		milestones, metrics, err := s.countMilestonesAndMetrics(ctx, goalID)
		if err != nil {
			return Goal{}, err
		}
		if milestones == 0 && metrics == 0 {
			reasons = append(reasons, fmt.Errorf("%w: a Dated Goal needs at least one Milestone or Metric", ErrValidation))
		}
	case GoalOngoing:
		_, metrics, err := s.countMilestonesAndMetrics(ctx, goalID)
		if err != nil {
			return Goal{}, err
		}
		if metrics == 0 {
			reasons = append(reasons, fmt.Errorf("%w: an Ongoing Goal needs at least one Metric", ErrValidation))
		}
	default:
		reasons = append(reasons, fmt.Errorf("%w: a Goal must be marked Dated or Ongoing to become Active", ErrValidation))
	}

	if len(reasons) > 0 {
		return Goal{}, errors.Join(reasons...)
	}

	if _, err := s.queries.SetGoalLifecycle(ctx, db.SetGoalLifecycleParams{
		Lifecycle: LifecycleActive,
		ID:        goalID,
	}); err != nil {
		return Goal{}, fmt.Errorf("activate goal: %w", err)
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
	acc := accountFromRow(owner)
	return Goal{
		ID:           g.ID,
		Title:        g.Title,
		SoWhat:       g.SoWhat,
		Owner:        acc,
		Lifecycle:    g.Lifecycle,
		Kind:         g.Kind,
		DeliveryDate: deliveryDate,
		CadenceDays:  int(g.CadenceDays),
		CreatedAt:    createdAt,
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

// MarkGoalDated marks the Goal as Dated with the given delivery date, against
// which its Health will be judged (CONTEXT.md: Dated Goal). A Dated Goal needs a
// delivery date, so a zero date is rejected.
func (s *Service) MarkGoalDated(ctx context.Context, goalID int64, deliveryDate time.Time) (Goal, error) {
	if deliveryDate.IsZero() {
		return Goal{}, fmt.Errorf("%w: a Dated Goal needs a delivery date", ErrValidation)
	}
	if err := s.requireProposedToSetDelivery(ctx, goalID); err != nil {
		return Goal{}, err
	}
	if _, err := s.queries.SetGoalKind(ctx, db.SetGoalKindParams{
		Kind:         GoalDated,
		DeliveryDate: deliveryDate.Format(dateFormat),
		ID:           goalID,
	}); err != nil {
		return Goal{}, fmt.Errorf("mark goal dated: %w", err)
	}
	return s.loadGoal(ctx, goalID)
}

// SetCadence sets how often a Check-in is expected on the Goal, in days. It
// must be positive (CONTEXT.md: Check-in cadence, 7 days by default).
func (s *Service) SetCadence(ctx context.Context, goalID int64, days int) (Goal, error) {
	if days <= 0 {
		return Goal{}, fmt.Errorf("%w: the Check-in cadence must be a positive number of days", ErrValidation)
	}
	if _, err := s.queries.SetGoalCadence(ctx, db.SetGoalCadenceParams{
		CadenceDays: int64(days),
		ID:          goalID,
	}); err != nil {
		return Goal{}, fmt.Errorf("set cadence: %w", err)
	}
	return s.loadGoal(ctx, goalID)
}

// AddContributor lists an Account on the Goal as a Contributor: named for
// information only, with no update duty (CONTEXT.md: Contributor). Adding the
// same Account twice is rejected.
func (s *Service) AddContributor(ctx context.Context, goalID, accountID int64) error {
	if _, err := s.queries.GetGoal(ctx, goalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return fmt.Errorf("look up goal: %w", err)
	}
	if _, err := s.queries.GetAccount(ctx, accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: account does not exist", ErrValidation)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	if _, err := s.queries.GetContributor(ctx, db.GetContributorParams{
		GoalID:    goalID,
		AccountID: accountID,
	}); err == nil {
		return fmt.Errorf("%w: already a Contributor", ErrValidation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("look up contributor: %w", err)
	}

	if _, err := s.queries.AddContributor(ctx, db.AddContributorParams{
		GoalID:    goalID,
		AccountID: accountID,
		CreatedAt: s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("add contributor: %w", err)
	}
	return nil
}

// AddContributorByEmail lists the Account with the given email as a Contributor
// on the Goal. It is the web-facing convenience over AddContributor, since the
// tool identifies people by email (CONTEXT.md: development sign-in by email). An
// email with no account is rejected.
func (s *Service) AddContributorByEmail(ctx context.Context, goalID int64, email string) error {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: no account with email %q", ErrValidation, email)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	return s.AddContributor(ctx, goalID, acc.ID)
}

// ListContributors returns the Accounts listed as Contributors on the Goal,
// ordered by email.
func (s *Service) ListContributors(ctx context.Context, goalID int64) ([]Account, error) {
	rows, err := s.queries.ListContributors(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list contributors: %w", err)
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountFromRow(r.Account))
	}
	return out, nil
}

// MarkGoalOngoing marks the Goal as Ongoing, clearing any delivery date; its
// Health is judged against its Metrics instead (CONTEXT.md: Ongoing Goal).
func (s *Service) MarkGoalOngoing(ctx context.Context, goalID int64) (Goal, error) {
	if err := s.requireProposedToSetDelivery(ctx, goalID); err != nil {
		return Goal{}, err
	}
	if _, err := s.queries.SetGoalKind(ctx, db.SetGoalKindParams{
		Kind:         GoalOngoing,
		DeliveryDate: "",
		ID:           goalID,
	}); err != nil {
		return Goal{}, fmt.Errorf("mark goal ongoing: %w", err)
	}
	return s.loadGoal(ctx, goalID)
}

// requireProposedToSetDelivery allows marking a Goal Dated or Ongoing only while
// it is Proposed. After that its delivery date moves only in a Check-in, which
// records the Date Slip and its reason (CONTEXT.md: Date Slip).
func (s *Service) requireProposedToSetDelivery(ctx context.Context, goalID int64) error {
	g, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: goal %d", ErrNotFound, goalID)
		}
		return fmt.Errorf("look up goal: %w", err)
	}
	if g.Goal.Lifecycle != LifecycleProposed {
		return fmt.Errorf("%w: a %s Goal's delivery date changes only in a Check-in, with a reason", ErrValidation, g.Goal.Lifecycle)
	}
	return nil
}
