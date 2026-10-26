package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// GoalPage is everything the single-Goal page shows one viewer, read by
// Service.GoalPage (ADR 0010). Each field is what the Service method of the
// same name, or the one named beside it, returns for the Goal.
type GoalPage struct {
	// Goal is the Goal, as ViewGoal returns it.
	Goal Goal
	// Parents and Children are its accepted links (ParentLinks, ChildLinks),
	// and LinkHealth the Health of each Active one's latest Check-in, keyed by
	// Goal ID; one with no Check-in has no entry.
	Parents    []GoalLink
	Children   []GoalLink
	LinkHealth map[int64]string
	// Candidates are every other Goal, newest first, as ListGoals lists them:
	// the parents the Goal could be linked to. Linking refuses a duplicate or
	// a cycle when it is asked for.
	Candidates []Goal
	Milestones []Milestone
	Metrics    []Metric
	// Readings are each Metric's readings, earliest first, keyed by Metric ID
	// (ListMetricReadings); a Metric never read has no entry.
	Readings   map[int64][]MetricReading
	Highlights []Highlight
	// DraftHighlights are its pending Draft Highlights
	// (PendingDraftHighlights), read only when CanCheckin, for only its Owner
	// and Delegates may see them.
	DraftHighlights []DraftHighlight
	Contributors    []Account
	Delegates       []Account
	// Viewer is the Account the page is read for. CanCheckin says whether they
	// may write the Goal's Check-ins (RequireCheckinAuthor), whatever its
	// Lifecycle, and Permissions what they may do with it now.
	Viewer      Account
	CanCheckin  bool
	Permissions GoalPermissions
	// NoChangeRefusal is why a No change would be refused (NoChangeRefusal),
	// asked only when the viewer may check in on the Active Goal and it has a
	// Check-in to repeat; nil when one would be recorded or isn't on offer.
	NoChangeRefusal error
	Revisions       []WhyItMattersRevision
	TitleRevisions  []TitleRevision
	Ownership       []Handoff
	ValueHistory    []ValueChange
	Dimensions      []Dimension
	Values          []DimensionValue
	Fields          []Field
	FieldValues     []FieldValue
	// Checkins are its Check-in history, newest first (ListCheckins), and
	// LatestCheckin its latest (LatestCheckin), nil when it has none.
	Checkins      []Checkin
	LatestCheckin *Checkin
	RolledUp      RolledUpHealth
	RunOffGreen   RunOffGreen
	// Ask is its open Ask for help (OpenAskForHelp), nil when it has none, and
	// AskAddressee whom an ask from its Owner goes to (AskAddressee).
	Ask                *AskForHelp
	AskAddressee       *Account
	DateSlips          []DateSlip
	MilestoneChurn     int
	Signals            GoalSignals
	Freshness          Freshness
	Required           []RequiredValue
	Incomplete         []string
	HealthStrip        HealthStrip
	Suggestions        []ParentSuggestion
	Nudges             []Nudge
	LinkEvents         []LinkEvent
	MilestoneAdditions []MilestoneChange
	ActionItems        []ActionItemListing
	ActionReassigns    []ActionItemReassignment
	Threads            []Thread
	Comments           []CommentListing
	// Now is when the page was read, and Timezone the Goal's
	// (GoalTimezone), its Milestones are judged in.
	Now      time.Time
	Timezone *time.Location
	// OverdueMilestone is the Goal's first overdue Milestone — Planned and
	// past its date as of today in Timezone — whatever its Health and with or
	// without a Check-in, as a Green Check-in is refused for it; nil when none
	// is.
	OverdueMilestone *Milestone
}

// GoalPermissions says what one viewer may do with a Goal now, each by the
// rule the command named beside it enforces, so a page offers just what that
// command would accept from them.
type GoalPermissions struct {
	// CheckIn: they write its Check-ins and it takes them now
	// (SubmitCheckin).
	CheckIn bool
	// AddMilestone: the same people, until it is Done or Cancelled
	// (AddMilestoneAsAuthor); EditMilestones: the same people, while it is
	// Proposed, when a Milestone's date still moves outside a Check-in
	// (EditMilestone).
	AddMilestone   bool
	EditMilestones bool
	// LogDraftHighlight: the same people, while it takes Draft Highlights
	// (LogDraftHighlight).
	LogDraftHighlight bool
	// SetValues: its Owner, a Delegate or an Admin (RequireGoalValueSetter).
	SetValues bool
	// AddContributor, ManageDelegates and LinkParent: its Owner, in any
	// Lifecycle (AddContributor, AddDelegate, RequestLink).
	AddContributor  bool
	ManageDelegates bool
	LinkParent      bool
	// DefineGoal: its Owner, while it is Proposed (DefineGoal, ActivateGoal).
	DefineGoal bool
	// SuggestParent: anyone but its Owner, while it is Active or Proposed
	// (SuggestParent).
	SuggestParent bool
	// EditTitleAndWhyItMatters: its Owner or an Admin
	// (EditTitleAndWhyItMatters).
	EditTitleAndWhyItMatters bool
	// HandOff: its Owner or an Admin, while it has a present Owner
	// (StartHandoff); Reassign: an Admin, once it is Ownerless (ReassignGoal).
	HandOff  bool
	Reassign bool
	// MarkOwnerDeparted and MarkOwnerReturned: an Admin, whichever its Owner
	// isn't yet (MarkDeparted, MarkReturned); MarkTopLevel: an Admin, to mark
	// or unmark it (MarkTopLevel).
	MarkOwnerDeparted bool
	MarkOwnerReturned bool
	MarkTopLevel      bool
}

// GoalPage reads everything the Goal goalID's page shows actorID, running
// each query once, and works out what the page derives — its Rolled-up Health,
// Health strip, Run off Green, freshness, signals and what the viewer may do —
// from what it read, as the Service methods each GoalPage field is named for
// do (ADR 0010). Any signed-in viewer may see any Goal, as with ViewGoal: a
// missing Goal is ErrNotFound.
//
// Walking up past a Departed Manager to the Ask for help's addressee reads
// every Account once more, however far it goes; everyone else the page names
// is read at once.
func (s *Service) GoalPage(ctx context.Context, actorID, goalID int64) (GoalPage, error) {
	g, err := s.ViewGoal(ctx, goalID)
	if err != nil {
		return GoalPage{}, err
	}
	p := GoalPage{Goal: g, Now: s.clock.Now(), Timezone: s.GoalTimezone(g)}
	if err := s.readGoalPageLinks(ctx, &p); err != nil {
		return GoalPage{}, err
	}
	milestones, err := s.queries.ListMilestones(ctx, goalID)
	if err != nil {
		return GoalPage{}, fmt.Errorf("load milestones: list milestones: %w", err)
	}
	p.Milestones = make([]Milestone, 0, len(milestones))
	for _, m := range milestones {
		p.Milestones = append(p.Milestones, milestoneFromRow(m))
	}
	changes, err := s.readGoalPageCheckins(ctx, &p)
	if err != nil {
		return GoalPage{}, err
	}
	if err := s.readGoalPageMetrics(ctx, &p); err != nil {
		return GoalPage{}, err
	}
	if err := s.readGoalPageValues(ctx, &p); err != nil {
		return GoalPage{}, err
	}
	if err := s.readGoalPageHistory(ctx, &p); err != nil {
		return GoalPage{}, err
	}
	items, err := s.queries.ListActionItemsForGoal(ctx, goalID)
	if err != nil {
		return GoalPage{}, fmt.Errorf("load action items: list goal's action items: %w", err)
	}
	reassigns, err := s.queries.ListActionItemReassignmentsForGoal(ctx, goalID)
	if err != nil {
		return GoalPage{}, fmt.Errorf("load action item reassignments: list goal's action item reassignments: %w", err)
	}
	ask, err := s.queries.GetOpenAskForHelp(ctx, goalID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return GoalPage{}, fmt.Errorf("load ask for help: get open ask for help: %w", err)
	}
	asking := err == nil

	account, err := s.goalPageAccounts(ctx, actorID, g, changes, items, reassigns)
	if err != nil {
		return GoalPage{}, err
	}
	if p.MilestoneAdditions, err = milestoneChangesOutsideCheckins(changes, account); err != nil {
		return GoalPage{}, fmt.Errorf("load milestone additions: %w", err)
	}
	if err := s.listGoalPageActionItems(&p, items, reassigns, account); err != nil {
		return GoalPage{}, err
	}
	if p.AskAddressee, err = askAddressee(g.Owner, func(id int64) (Account, bool, error) {
		a, err := account(id)
		if errors.Is(err, ErrNotFound) {
			return Account{}, false, nil
		}
		return a, err == nil, err
	}); err != nil {
		return GoalPage{}, fmt.Errorf("find ask addressee: %w", err)
	}
	if asking {
		p.Ask = &AskForHelp{Goal: goalFromRow(ask.Goal, ask.Account), Health: ask.Health, PathToGreen: ask.PathToGreen, Addressee: p.AskAddressee}
	}

	noChange := s.deriveGoalPage(&p)
	if err := s.permitGoalPage(ctx, &p, actorID, account, noChange); err != nil {
		return GoalPage{}, err
	}
	return p, nil
}

// readGoalPageLinks reads p.Goal's parents and children, the other Goals it
// could contribute to, and the latest Check-in of it and of each Active Goal
// it is linked to, in one read: its own is p.LatestCheckin, the others'
// Health p.LinkHealth, and its children's its Rolled-up Health.
func (s *Service) readGoalPageLinks(ctx context.Context, p *GoalPage) error {
	var err error
	if p.Parents, err = s.ParentLinks(ctx, p.Goal.ID); err != nil {
		return fmt.Errorf("load parents: %w", err)
	}
	if p.Children, err = s.ChildLinks(ctx, p.Goal.ID); err != nil {
		return fmt.Errorf("load children: %w", err)
	}
	all, err := s.ListGoals(ctx)
	if err != nil {
		return fmt.Errorf("load goals: %w", err)
	}
	p.Candidates = make([]Goal, 0, len(all))
	for _, c := range all {
		if c.ID != p.Goal.ID {
			p.Candidates = append(p.Candidates, c)
		}
	}
	ids := []int64{p.Goal.ID}
	for _, l := range slices.Concat(p.Parents, p.Children) {
		if l.Goal.Lifecycle == LifecycleActive && !slices.Contains(ids, l.Goal.ID) {
			ids = append(ids, l.Goal.ID)
		}
	}
	latest, err := s.LatestCheckinsByGoal(ctx, ids)
	if err != nil {
		return fmt.Errorf("load latest check-ins: %w", err)
	}
	if c, ok := latest[p.Goal.ID]; ok {
		p.LatestCheckin = &c
	}
	p.LinkHealth = map[int64]string{}
	for _, l := range slices.Concat(p.Parents, p.Children) {
		if c, ok := latest[l.Goal.ID]; ok && l.Goal.Lifecycle == LifecycleActive {
			p.LinkHealth[l.Goal.ID] = c.Health
		}
	}
	p.RolledUp = s.rollUp(goalsOf(p.Children), latest)
	return nil
}

// readGoalPageCheckins reads p.Goal's Check-in history, returning the
// Milestone changes it read for it, which also hold those made outside a
// Check-in.
func (s *Service) readGoalPageCheckins(ctx context.Context, p *GoalPage) ([]db.ListMilestoneChangesByGoalRow, error) {
	rows, err := s.queries.ListCheckins(ctx, p.Goal.ID)
	if err != nil {
		return nil, fmt.Errorf("load check-ins: list checkins: %w", err)
	}
	changes, err := s.queries.ListMilestoneChangesByGoal(ctx, p.Goal.ID)
	if err != nil {
		return nil, fmt.Errorf("load check-ins: list milestone changes: %w", err)
	}
	discardedBy, err := s.discardedDraftHighlightsByCheckin(ctx, p.Goal.ID)
	if err != nil {
		return nil, fmt.Errorf("load check-ins: %w", err)
	}
	p.Checkins = checkinHistory(rows, changes, discardedBy)
	return changes, nil
}

// readGoalPageMetrics reads p.Goal's Metrics and all their readings.
func (s *Service) readGoalPageMetrics(ctx context.Context, p *GoalPage) error {
	var err error
	if p.Metrics, err = s.ListMetrics(ctx, p.Goal.ID); err != nil {
		return fmt.Errorf("load metrics: %w", err)
	}
	if p.Readings, err = s.metricReadingsByMetric(ctx, []int64{p.Goal.ID}); err != nil {
		return fmt.Errorf("load metric readings: %w", err)
	}
	return nil
}

// readGoalPageValues reads every Dimension and Field, p.Goal's values in
// them, and which required ones it has a value in.
func (s *Service) readGoalPageValues(ctx context.Context, p *GoalPage) error {
	dims, err := s.queries.ListDimensions(ctx)
	if err != nil {
		return fmt.Errorf("load dimensions: list dimensions: %w", err)
	}
	dimValues, err := s.queries.ListAllDimensionValues(ctx)
	if err != nil {
		return fmt.Errorf("load dimensions: list dimension values: %w", err)
	}
	p.Dimensions = dimensionsWithValues(dims, dimValues)
	if p.Values, err = s.GoalValues(ctx, p.Goal.ID); err != nil {
		return fmt.Errorf("load dimension values: %w", err)
	}
	if p.Fields, err = s.ListFields(ctx); err != nil {
		return fmt.Errorf("load fields: %w", err)
	}
	if p.FieldValues, err = s.GoalFields(ctx, p.Goal.ID); err != nil {
		return fmt.Errorf("load field values: %w", err)
	}
	carried := make(map[int64]bool, len(p.Values))
	for _, v := range p.Values {
		carried[v.DimensionID] = true
	}
	set := make(map[int64]bool, len(p.FieldValues))
	for _, v := range p.FieldValues {
		set[v.Field.ID] = true
	}
	p.Required = requiredValues(dims, p.Fields, carried, set)
	// Only an Active Goal is Incomplete (Incomplete).
	if p.Goal.Lifecycle == LifecycleActive {
		for _, r := range p.Required {
			if !r.Set {
				p.Incomplete = append(p.Incomplete, r.Name)
			}
		}
	}
	return nil
}

// readGoalPageHistory reads the rest of what p.Goal's History and discussion
// list, each in its own query.
func (s *Service) readGoalPageHistory(ctx context.Context, p *GoalPage) error {
	id := p.Goal.ID
	var err error
	for _, read := range []struct {
		what string
		read func() error
	}{
		{"highlights", func() error { p.Highlights, err = s.ListHighlightsByGoal(ctx, id); return err }},
		{"contributors", func() error { p.Contributors, err = s.ListContributors(ctx, id); return err }},
		{"delegates", func() error { p.Delegates, err = s.ListDelegates(ctx, id); return err }},
		{"Why it matters history", func() error { p.Revisions, err = s.ListWhyItMattersRevisions(ctx, id); return err }},
		{"title history", func() error { p.TitleRevisions, err = s.ListTitleRevisions(ctx, id); return err }},
		{"ownership history", func() error { p.Ownership, err = s.OwnershipHistory(ctx, id); return err }},
		{"value history", func() error { p.ValueHistory, err = s.ValueHistory(ctx, id); return err }},
		{"date slips", func() error { p.DateSlips, err = s.ListDateSlips(ctx, id); return err }},
		{"milestone churn", func() error { p.MilestoneChurn, err = s.MilestoneChurn(ctx, id); return err }},
		{"parent suggestions", func() error { p.Suggestions, err = s.ParentSuggestions(ctx, id); return err }},
		{"nudges", func() error { p.Nudges, err = s.Nudges(ctx, id); return err }},
		{"link events", func() error { p.LinkEvents, err = s.LinkEvents(ctx, id); return err }},
		{"comment threads", func() error { p.Threads, err = s.GoalThreads(ctx, id); return err }},
		{"comments", func() error { p.Comments, err = s.GoalComments(ctx, id); return err }},
	} {
		if err := read.read(); err != nil {
			return fmt.Errorf("load %s: %w", read.what, err)
		}
	}
	return nil
}

// goalPageAccounts reads at once the viewer and everyone else the page names
// by ID alone: the Owner's Manager, whoever added a Milestone outside a
// Check-in, and the people of the Action Items about the Goal and their
// reassignments. It returns a lookup of them which, asked for anyone else,
// such as a Manager further up, reads every Account once and answers from
// them from then on; one who doesn't exist is ErrNotFound.
func (s *Service) goalPageAccounts(ctx context.Context, actorID int64, g Goal, changes []db.ListMilestoneChangesByGoalRow, items []db.ListActionItemsForGoalRow, reassigns []db.ActionItemReassignment) (func(id int64) (Account, error), error) {
	ids := []int64{actorID}
	if g.Owner.ManagerID != nil {
		ids = append(ids, *g.Owner.ManagerID)
	}
	for _, c := range changes {
		if c.MilestoneChange.AuthorID != nil {
			ids = append(ids, *c.MilestoneChange.AuthorID)
		}
	}
	for _, r := range items {
		ids = append(ids, r.ActionItem.OwnerID, r.ActionItem.CreatedBy)
	}
	for _, r := range reassigns {
		ids = append(ids, r.FromOwnerID, r.ToOwnerID, r.ReassignedBy)
	}
	slices.Sort(ids)
	rows, err := s.queries.ListAccountsByIDs(ctx, slices.Compact(ids))
	if err != nil {
		return nil, fmt.Errorf("load people: list accounts: %w", err)
	}
	byID := make(map[int64]Account, len(rows))
	for _, r := range rows {
		byID[r.ID] = accountFromRow(r)
	}
	everyone := false
	return func(id int64) (Account, error) {
		if a, ok := byID[id]; ok {
			return a, nil
		}
		if !everyone {
			// Someone further up, past a Departed Manager: read everyone once,
			// as OpenAsksForHelp does, so a walk of any length is one query.
			rows, err := s.queries.ListAskWalkAccounts(ctx)
			if err != nil {
				return Account{}, fmt.Errorf("load people: list accounts: %w", err)
			}
			for _, r := range rows {
				byID[r.ID] = accountFromRow(r)
			}
			everyone = true
			if a, ok := byID[id]; ok {
				return a, nil
			}
		}
		return Account{}, fmt.Errorf("%w: account %d", ErrNotFound, id)
	}, nil
}

// listGoalPageActionItems lists the Action Items about p.Goal, from items,
// and their reassignments, from reassigns, as ActionItemsAbout and
// ActionItemReassignmentsAbout do, with the people they name looked up with
// account.
func (s *Service) listGoalPageActionItems(p *GoalPage, items []db.ListActionItemsForGoalRow, reassigns []db.ActionItemReassignment, account func(id int64) (Account, error)) error {
	// An item's latest reassignment is its last, for they are oldest first.
	latest := map[int64]*db.ActionItemReassignment{}
	for i := range reassigns {
		latest[reassigns[i].ActionItemID] = &reassigns[i]
	}
	rows := map[int64]db.ActionItem{}
	p.ActionItems = make([]ActionItemListing, 0, len(items))
	for _, r := range items {
		rows[r.ActionItem.ID] = r.ActionItem
		item, err := actionItemOf(r.ActionItem, r.GoalTitle, latest[r.ActionItem.ID], account)
		if err != nil {
			return err
		}
		p.ActionItems = append(p.ActionItems, s.actionItemListingOf(item, r.ActionItem, r.ReportName))
	}
	p.ActionReassigns = make([]ActionItemReassignment, 0, len(reassigns))
	for _, r := range reassigns {
		re, err := actionItemReassignmentOf(r, rows[r.ActionItemID], account)
		if err != nil {
			return err
		}
		p.ActionReassigns = append(p.ActionReassigns, re)
	}
	return nil
}

// deriveGoalPage works out what p shows that is computed when read, from what
// GoalPage has read into it, returning the verdict on a No change on its Goal,
// judged from the same facts NoChangeRefusal reads.
func (s *Service) deriveGoalPage(p *GoalPage) noChangeVerdict {
	g := p.Goal
	var last lastCheckin
	if c := p.LatestCheckin; c != nil {
		last = lastCheckin{at: c.CreatedAt, health: c.Health, pathTargetDate: c.PathTargetDate}
	}
	p.Freshness = s.judgeFreshness(g, last)
	s.markMilestoneOverdue(&p.Freshness, g, last.health, p.Milestones)
	p.RunOffGreen = s.runOffGreen(g, p.Checkins)
	p.HealthStrip = s.healthStrip(g, p.Checkins)
	p.Signals = goalSignalsOf(g, goalsOf(p.Parents), goalsOf(p.Children))
	noChange := judgeNoChange(s.noChangeFactsOf(g, p.LatestCheckin, p.RolledUp, p.Milestones, p.Metrics, p.Readings))
	p.OverdueMilestone = noChange.overdueMilestone
	return noChange
}

// permitGoalPage decides what p's viewer, actorID, may do on its Goal, and,
// for one who may check in, reads the pending Draft Highlights only they see
// and gives them noChange's refusal of a No change on it.
func (s *Service) permitGoalPage(ctx context.Context, p *GoalPage, actorID int64, account func(id int64) (Account, error), noChange noChangeVerdict) error {
	g := p.Goal
	viewer, err := account(actorID)
	found := err == nil
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return err
	}
	p.Viewer = viewer
	delegate := func() (bool, error) {
		return slices.ContainsFunc(p.Delegates, func(d Account) bool { return d.ID == actorID }), nil
	}
	refusal := Refuse(ErrNotAuthorized, "the author does not exist")
	if found {
		refusal = checkinAuthorRefusal(viewer, g.Owner.ID, delegate)
	}
	if p.CanCheckin, err = goalPagePermits(refusal); err != nil {
		return err
	}
	admin := func() error {
		if !found {
			return Refuse(ErrNotAuthorized, "not an Admin")
		}
		return adminRefusal(viewer)
	}
	setValues, err := goalPagePermits(goalValueSetterRefusal(actorID, g.Owner.ID, delegate, admin))
	if err != nil {
		return err
	}
	isAdmin := admin() == nil
	owns := actorID == g.Owner.ID
	proposed := g.Lifecycle == LifecycleProposed
	p.Permissions = GoalPermissions{
		CheckIn:                  p.CanCheckin && LifecycleTakesCheckins(g.Lifecycle),
		AddMilestone:             p.CanCheckin && milestonesAddable(g.Lifecycle),
		EditMilestones:           p.CanCheckin && proposed,
		LogDraftHighlight:        p.CanCheckin && LifecycleTakesDraftHighlights(g.Lifecycle),
		SetValues:                setValues,
		AddContributor:           owns,
		ManageDelegates:          owns,
		LinkParent:               owns,
		DefineGoal:               owns && proposed,
		SuggestParent:            found && !owns && suggestable(g.Lifecycle),
		EditTitleAndWhyItMatters: owns || isAdmin,
		HandOff:                  (owns || isAdmin) && !g.Ownerless,
		Reassign:                 isAdmin && g.Ownerless,
		MarkOwnerDeparted:        isAdmin && !g.Owner.Departed,
		MarkOwnerReturned:        isAdmin && g.Owner.Departed,
		MarkTopLevel:             isAdmin,
	}
	if !p.CanCheckin {
		return nil
	}
	if p.DraftHighlights, err = s.pendingDraftHighlights(ctx, g.ID); err != nil {
		return fmt.Errorf("load draft highlights: %w", err)
	}
	if g.Lifecycle == LifecycleActive && p.LatestCheckin != nil {
		p.NoChangeRefusal = noChange.refusal
	}
	return nil
}

// goalPagePermits reads a refusal from one of the permission rules as whether
// it allows: true for none, false for ErrNotAuthorized, and any other error
// as it is.
func goalPagePermits(refusal error) (bool, error) {
	switch {
	case refusal == nil:
		return true, nil
	case errors.Is(refusal, ErrNotAuthorized):
		return false, nil
	}
	return false, refusal
}

// goalsOf are the Goals of links, in order.
func goalsOf(links []GoalLink) []Goal {
	out := make([]Goal, 0, len(links))
	for _, l := range links {
		out = append(out, l.Goal)
	}
	return out
}
