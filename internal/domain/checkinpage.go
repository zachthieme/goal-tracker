package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CheckinPage is everything the Check-in page and its form show one viewer,
// read by Service.CheckinPage (ADR 0010). Each field is what the GoalPage
// field of the same name holds for the Goal, but for LinkHealth, which covers
// only its children.
type CheckinPage struct {
	Goal Goal
	// Viewer is the Account the page is read for. CanCheckin says whether they
	// may write the Goal's Check-ins (RequireCheckinAuthor), whatever its
	// Lifecycle. For anyone who may not, the page holds only the Goal and
	// Viewer.
	Viewer     Account
	CanCheckin bool
	// Permissions are what they may do with its Check-ins now, as the Goal
	// page's Permissions say.
	Permissions CheckinPermissions
	// Children are the Goal's accepted children (ChildLinks), for the
	// "Contributed to by" rail, and LinkHealth the Health of each Active one's
	// latest Check-in, keyed by Goal ID; one with no Check-in has no entry.
	Children   []GoalLink
	LinkHealth map[int64]string
	// Milestones are all the Goal's Milestones, soonest first; the form
	// offers the Planned ones. OverdueMilestone is the first overdue one, nil
	// when none is.
	Milestones       []Milestone
	OverdueMilestone *Milestone
	// Metrics are its Metrics, and Readings each one's readings, earliest
	// first, keyed by Metric ID; a Metric never read has no entry.
	Metrics  []Metric
	Readings map[int64][]MetricReading
	// LatestCheckin is its latest Check-in, nil when it has none.
	LatestCheckin *Checkin
	RolledUp      RolledUpHealth
	RunOffGreen   RunOffGreen
	// Ask is its open Ask for help, nil when it has none, and AskAddressee
	// whom an ask from its Owner goes to, nil for no one above the Owner.
	Ask          *AskForHelp
	AskAddressee *Account
	// DraftHighlights are its pending Draft Highlights, oldest first.
	DraftHighlights []DraftHighlight
	// NoChangeRefusal is why a No change would be refused (NoChangeRefusal),
	// given only when the Goal is Active and has a Check-in to repeat; nil
	// when one would be recorded or isn't on offer.
	NoChangeRefusal error
	// Now is when the page was read, and Timezone the Goal's
	// (GoalTimezone), its Milestones and Metrics are judged in.
	Now      time.Time
	Timezone *time.Location
}

// CheckinPage reads everything the Check-in page for the Goal goalID shows
// actorID, running each query once, and works out its Rolled-up Health, Run
// off Green and the verdict on a No change from what it read, as the Service
// methods each field is named for do (ADR 0010). It reads less than GoalPage:
// none of the Goal's history, values or discussion. A missing Goal is
// ErrNotFound. For a viewer who may not check in it reads nothing past who
// they are: the page refuses them.
//
// The commands the page posts to read what they decide on again, inside the
// transaction that writes; nothing here is passed to them.
func (s *Service) CheckinPage(ctx context.Context, actorID, goalID int64) (CheckinPage, error) {
	g, err := s.ViewGoal(ctx, goalID)
	if err != nil {
		return CheckinPage{}, err
	}
	p := CheckinPage{Goal: g, Now: s.clock.Now(), Timezone: s.GoalTimezone(g)}
	account, err := s.goalPageAccounts(ctx, actorID, g, nil, nil, nil)
	if err != nil {
		return CheckinPage{}, err
	}
	actor, err := s.checkinPageActor(ctx, &p, actorID, account)
	if err != nil {
		return CheckinPage{}, err
	}
	if p.CanCheckin, err = goalPagePermits(checkinAuthorRuleRefusal(actor, g)); err != nil || !p.CanCheckin {
		return p, err
	}
	if err := s.readCheckinPageLinks(ctx, &p); err != nil {
		return CheckinPage{}, err
	}
	checkins, err := s.checkinsByGoal(ctx, []int64{goalID})
	if err != nil {
		return CheckinPage{}, fmt.Errorf("load check-ins: %w", err)
	}
	p.RunOffGreen = s.runOffGreen(g, checkins[goalID])
	if p.Milestones, err = s.ListMilestones(ctx, goalID); err != nil {
		return CheckinPage{}, fmt.Errorf("load milestones: %w", err)
	}
	if p.Metrics, err = s.ListMetrics(ctx, goalID); err != nil {
		return CheckinPage{}, fmt.Errorf("load metrics: %w", err)
	}
	if p.Readings, err = s.metricReadingsByMetric(ctx, []int64{goalID}); err != nil {
		return CheckinPage{}, fmt.Errorf("load metric readings: %w", err)
	}
	if p.DraftHighlights, err = s.pendingDraftHighlights(ctx, goalID); err != nil {
		return CheckinPage{}, fmt.Errorf("load draft highlights: %w", err)
	}
	if err := s.readCheckinPageAsk(ctx, &p, account); err != nil {
		return CheckinPage{}, err
	}
	noChange := judgeNoChange(s.noChangeFactsOf(g, p.LatestCheckin, p.RolledUp, p.Milestones, p.Metrics, p.Readings))
	p.OverdueMilestone = noChange.overdueMilestone
	if g.Lifecycle == LifecycleActive && p.LatestCheckin != nil {
		p.NoChangeRefusal = noChange.refusal
	}
	var failed error
	p.Permissions = permitCheckins(actor, g, &noChange, func(refusal error) bool {
		ok, err := goalPagePermits(refusal)
		if err != nil && failed == nil {
			failed = err
		}
		return ok
	})
	if failed != nil {
		return CheckinPage{}, failed
	}
	return p, nil
}

// checkinPageActor is actorID as the Goal rules judge them on p.Goal, looked
// up through account, and records who they are on p. Whether they are a
// Delegate is looked up once, only when a rule asks: for someone not its
// Owner.
func (s *Service) checkinPageActor(ctx context.Context, p *CheckinPage, actorID int64, account func(id int64) (Account, error)) (goalActor, error) {
	viewer, err := account(actorID)
	found := err == nil
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return goalActor{}, err
	}
	p.Viewer = viewer
	var delegate *bool
	return goalActor{
		id:      actorID,
		account: func() (Account, bool, error) { return viewer, found, nil },
		delegate: func() (bool, error) {
			if delegate == nil {
				ok, err := s.isDelegate(ctx, p.Goal.ID, actorID)
				if err != nil {
					return false, err
				}
				delegate = &ok
			}
			return *delegate, nil
		},
	}, nil
}

// readCheckinPageLinks reads p.Goal's children and, in one read, its latest
// Check-in and each Active child's, from which its Rolled-up Health and its
// children's link Health come.
func (s *Service) readCheckinPageLinks(ctx context.Context, p *CheckinPage) error {
	var err error
	if p.Children, err = s.ChildLinks(ctx, p.Goal.ID); err != nil {
		return fmt.Errorf("load children: %w", err)
	}
	children := goalsOf(p.Children)
	latest, err := s.LatestCheckinsByGoal(ctx, append([]int64{p.Goal.ID}, activeGoalIDs(children)...))
	if err != nil {
		return fmt.Errorf("load latest check-ins: %w", err)
	}
	if c, ok := latest[p.Goal.ID]; ok {
		p.LatestCheckin = &c
	}
	p.LinkHealth = map[int64]string{}
	for _, c := range children {
		if latest, ok := latest[c.ID]; ok && c.Lifecycle == LifecycleActive {
			p.LinkHealth[c.ID] = latest.Health
		}
	}
	p.RolledUp = s.rollUp(children, latest)
	return nil
}

// readCheckinPageAsk reads p.Goal's open Ask for help, and finds whom an ask
// from its Owner goes to through account, as GoalPage does.
func (s *Service) readCheckinPageAsk(ctx context.Context, p *CheckinPage, account func(id int64) (Account, error)) error {
	ask, err := s.queries.GetOpenAskForHelp(ctx, p.Goal.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load ask for help: get open ask for help: %w", err)
	}
	asking := err == nil
	if p.AskAddressee, err = askAddressee(p.Goal.Owner, func(id int64) (Account, bool, error) {
		a, err := account(id)
		if errors.Is(err, ErrNotFound) {
			return Account{}, false, nil
		}
		return a, err == nil, err
	}); err != nil {
		return fmt.Errorf("find ask addressee: %w", err)
	}
	if asking {
		p.Ask = &AskForHelp{Goal: goalFromRow(ask.Goal, ask.Account), Health: ask.Health, PathToGreen: ask.PathToGreen, Addressee: p.AskAddressee}
	}
	return nil
}

// OffTargetMetrics are the Goal's Metrics a Green would leave off target as
// of the day p was read, in its timezone, by the readings typed or, for a
// Metric with none, its latest, as Service.OffTargetMetrics judges them, from
// what p read.
func (p CheckinPage) OffTargetMetrics(typed []MetricReadingInput) []OffTargetMetric {
	return checkinOffTargetMetrics(p.Metrics, p.Readings, typed, orgDate(p.Now, p.Timezone))
}
