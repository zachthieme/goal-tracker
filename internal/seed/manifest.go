package seed

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// The people the e2e scenarios name: the leader of scenario 1 and 2
// ("Elena", whom the seed names Priya Raman), the Admin, scenario 2's manager
// ("Marcus"), and the leadership scenario 5's project Owner is never one of.
const (
	manifestLeader  = "cto@example.com"
	manifestAdmin   = "admin@example.com"
	manifestManager = "growth-lead@example.com"
)

var manifestLeadership = []string{manifestAdmin, "ceo@example.com", manifestLeader, "cpo@example.com"}

// Manifest is the e2e suite's picks from a seeded company: the Goals and
// people each spec acts on, chosen by role, judged at one instant (the app's
// start, for the suite). e2e/manifest.ts mirrors its JSON: a role with no
// pick is null, and a list with none is empty. A new role is added here, in
// e2e/manifest.ts and in TestE2EScenarioSituations together.
type Manifest struct {
	// QuietGoal is an Active Goal whose Owner can check in Green with only a
	// status: Green last time, no Metric, no accepted child Goals, no overdue
	// Planned Milestone, and no Highlight or Draft Highlight yet.
	QuietGoal *ManifestGoal `json:"quietGoal"`
	// DraftlessGoal is an Active Goal with no Draft Highlight, whose Owner is
	// still here.
	DraftlessGoal *ManifestGoal `json:"draftlessGoal"`
	// MetricGoal is an Active Goal with a Metric and a Check-in, no Draft
	// Highlight and no overdue Planned Milestone, whose Owner is still here.
	MetricGoal *ManifestGoal `json:"metricGoal"`
	// MetricParentGoal is an Active Goal with a Metric, a Check-in and
	// accepted child Goals, whose Owner is still here.
	MetricParentGoal *ManifestGoal `json:"metricParentGoal"`
	// NoChangeGoal is a Goal its Owner can press No change on: Active,
	// checked in before, no accepted child Goals, no overdue Planned
	// Milestone, and no back-to-Green date passed on its latest Check-in.
	NoChangeGoal *ManifestGoal `json:"noChangeGoal"`
	// DueNoChangeGoal is a NoChangeGoal with a 7-day cadence.
	DueNoChangeGoal *ManifestGoal `json:"dueNoChangeGoal"`
	// AccountIDs is every Account's ID by email, for the log lines that name a
	// person by account_id.
	AccountIDs map[string]int64 `json:"accountIDs"`

	FindingTrouble       FindingTroubleRoles       `json:"findingTrouble"`
	UnderstandingTrouble UnderstandingTroubleRoles `json:"understandingTrouble"`
	WatchingGoals        WatchingGoalsRoles        `json:"watchingGoals"`
	WeeklyCheckin        WeeklyCheckinRoles        `json:"weeklyCheckin"`
	BadNewsChain         *BadNewsChain             `json:"badNewsChain"`
	StatingAGoal         StatingAGoalRoles         `json:"statingAGoal"`
}

// ManifestGoal is a picked Goal and its Owner. Name is the Owner's Name, ""
// when they have none.
type ManifestGoal struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// ManifestPerson is a picked person. Name is their Name, "" when they have
// none.
type ManifestPerson struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// FindingTroubleRoles are 01-finding-trouble.spec.ts's picks.
type FindingTroubleRoles struct {
	// CheckinOverdue is a Check-in overdue Platform Goal the leader doesn't
	// own, whose Owner is still here.
	CheckinOverdue *ManifestGoal `json:"checkinOverdue"`
	// Departing is an Active Platform Goal whose Owner the Admin departs: one
	// who owns no Top-level and no Check-in overdue Active Goal, and is
	// neither the leader nor an Admin.
	Departing *ManifestGoal `json:"departing"`
	// Overdue is an Active Platform Goal to put past its Path to Green: not
	// Check-in overdue, no Active child Goals, no overdue Planned Milestone.
	Overdue *ManifestGoal `json:"overdue"`
	// Tiered are the first two Active Platform Goals, to carry a second
	// Dimension's values.
	Tiered []ManifestGoal `json:"tiered"`
	// Unlinked is an Active Goal, not Top-level, contributing to no Goal.
	Unlinked *ManifestGoal `json:"unlinked"`
	// TwoReasons is a Check-in overdue Goal due later than a Goal it
	// contributes to, with its Team.
	TwoReasons *TwoReasonsGoal `json:"twoReasons"`
	// Ownerless are Departing's Owner's Active Goals.
	Ownerless []ManifestGoal `json:"ownerless"`
	// RedWithChildren is a Red Goal, not Check-in overdue, contributing to a
	// Goal and with Active child Goals.
	RedWithChildren *ManifestGoal `json:"redWithChildren"`
	// RedSlipped is a Red Goal whose delivery date has slipped.
	RedSlipped *ManifestGoal `json:"redSlipped"`
	// Lifecycles is every Goal's Lifecycle by Goal ID.
	Lifecycles map[int64]string `json:"lifecycles"`
}

// TwoReasonsGoal is FindingTroubleRoles.TwoReasons: a Goal and its Team.
type TwoReasonsGoal struct {
	ManifestGoal
	Team string `json:"team"`
}

// UnderstandingTroubleRoles are 02-understanding-trouble.spec.ts's picks.
type UnderstandingTroubleRoles struct {
	// PlatformReport are the Goals a "Team is any of Platform" rule lists
	// against a baseline 30 days back: every Platform Goal less those Done or
	// Cancelled before then.
	PlatformReport []ReportGoal `json:"platformReport"`
	// Slipped is a Green Dated Platform Goal its Owner can check in on
	// plainly, to slip.
	Slipped *ReportGoal `json:"slipped"`
	// Turning is another Platform Goal its Owner can check in on plainly,
	// Yellow first if one is, to turn.
	Turning *ReportGoal `json:"turning"`
	// Moved is a third Platform Goal, to move to another Team.
	Moved *ReportGoal `json:"moved"`
}

// ReportGoal is a Goal as a Report's block shows it. Owner is the Owner's
// Name, or their email when they have none; Status, PathToGreen and Health
// are its latest Check-in's, "" when it has none.
type ReportGoal struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Owner        string `json:"owner"`
	Email        string `json:"email"`
	Lifecycle    string `json:"lifecycle"`
	WhyItMatters string `json:"why_it_matters"`
	DeliveryDate string `json:"delivery_date"`
	Status       string `json:"status"`
	PathToGreen  string `json:"path_to_green"`
	Milestones   int    `json:"milestones"`
	Metrics      int    `json:"metrics"`
	Health       string `json:"health"`
}

// WatchingGoalsRoles are 02-watching-goals.spec.ts's picks.
type WatchingGoalsRoles struct {
	GrowthReport GrowthReport `json:"growthReport"`
	Picked       PickedGoals  `json:"picked"`
}

// GrowthReport are the Growth Goals: InReport those a "Team is any of Growth"
// rule lists against a baseline 30 days back, and FinishedEarlier those Done
// or Cancelled before then, which leave it.
type GrowthReport struct {
	InReport        []ManifestGoal `json:"inReport"`
	FinishedEarlier []ManifestGoal `json:"finishedEarlier"`
}

// PickedGoals are Active, checked-in Goals on a Team other than Growth that
// the manager neither owns nor is a Delegate on, one of each kind.
type PickedGoals struct {
	// Troubled is Yellow or Red, with a Path to Green.
	Troubled *WatchedGoal `json:"troubled"`
	// Slipped is Yellow or Red, and has slipped its delivery date.
	Slipped *WatchedGoal `json:"slipped"`
	// Green has no Active child Goals and no overdue Planned Milestone.
	Green *WatchedGoal `json:"green"`
	// Parent has Active child Goals, none of them another pick.
	Parent *WatchedGoal `json:"parent"`
	// Children are Parent's Active child Goals.
	Children []ManifestGoal `json:"children"`
}

// WatchedGoal is a picked Goal with its latest Check-in's Health and Path to
// Green, and SlippedFrom, the delivery date its first slip moved it from, ""
// when it hasn't slipped.
type WatchedGoal struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Health      string `json:"health"`
	PathToGreen string `json:"path_to_green"`
	SlippedFrom string `json:"slipped_from"`
}

// WeeklyCheckinRoles are 03-weekly-checkin.spec.ts's picks.
type WeeklyCheckinRoles struct {
	// WeekOwners could be the week's Owner, most Active Goals first: at least
	// two Active Goals, exactly one with a Metric, and none a No change would
	// be refused on. Never the leader. Which of their Goals are due is read
	// from Home.
	WeekOwners    []WeekOwner   `json:"weekOwners"`
	DelegateCover DelegateCover `json:"delegateCover"`
}

// WeekOwner is a WeeklyCheckinRoles.WeekOwners Owner and their Active Goals.
type WeekOwner struct {
	ManifestPerson
	Goals []WeekGoal `json:"goals"`
}

// WeekGoal is an Active Goal, and whether it has a Metric.
type WeekGoal struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Metric bool   `json:"metric"`
}

// DelegateCover is a Goal whose Owner is away, and someone to cover it.
type DelegateCover struct {
	// Goal is Active, last checked in more than its cadence and a day ago,
	// with no accepted child Goals, overdue Planned Milestone or Delegate,
	// and an Owner with a Name: the longest unchecked first.
	Goal *ManifestGoal `json:"goal"`
	// Delegate is the first other person by ID, neither Departed nor an
	// Admin, with a Name.
	Delegate *ManifestPerson `json:"delegate"`
}

// BadNewsChain is 04-reporting-bad-news.spec.ts's chain: a project, the team
// Goal it contributes to, and an org outcome the team Goal contributes to
// whose other Active children are all Green.
type BadNewsChain struct {
	Project    BadNewsProject `json:"project"`
	Team       BadNewsGoal    `json:"team"`
	OrgOutcome BadNewsGoal    `json:"orgOutcome"`
}

// BadNewsGoal is a Goal in the chain. Name is its Owner's Name, or their
// email when they have none.
type BadNewsGoal struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	DeliveryDate string `json:"delivery_date"`
}

// BadNewsProject is the chain's project, with its Planned Milestones dated
// after today, soonest first, and its Team.
type BadNewsProject struct {
	BadNewsGoal
	Milestones []BadNewsMilestone `json:"milestones"`
	Team       string             `json:"team"`
}

// BadNewsMilestone is a Planned Milestone of the project.
type BadNewsMilestone struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	TargetDate string `json:"target_date"`
}

// StatingAGoalRoles are 05-stating-a-goal.spec.ts's picks.
type StatingAGoalRoles struct {
	// TeamGoals are the leads' Active Goals with a Team, by ID. Which of them
	// shows Rolled-up Health Green, or none, is read from the page.
	TeamGoals []TeamGoal `json:"teamGoals"`
	// ProjectOwner is the first project Owner by email.
	ProjectOwner *ManifestPerson `json:"projectOwner"`
}

// TeamGoal is a lead's team Goal and its Team, with ProjectOwners: the
// people, neither a lead nor the leadership, who own an Active Goal
// contributing to a lead's Goal, those under this one first, then by email.
type TeamGoal struct {
	ManifestGoal
	Team          string           `json:"team"`
	ProjectOwners []ManifestPerson `json:"projectOwners"`
}

// BuildManifest reads the seeded company through svc and picks each role as
// of now: "today" is now's date in UTC, the e2e server's timezone, and
// Check-in overdue, overdue Milestones and a Report's 30-day baseline are
// judged at now.
func BuildManifest(ctx context.Context, svc *domain.Service, now time.Time) (Manifest, error) {
	o, err := readManifestOrg(ctx, svc, now)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		QuietGoal:            manifestGoalOf(o.quietGoal()),
		DraftlessGoal:        manifestGoalOf(o.draftlessGoal()),
		MetricGoal:           manifestGoalOf(o.metricGoal()),
		MetricParentGoal:     manifestGoalOf(o.metricParentGoal()),
		NoChangeGoal:         manifestGoalOf(o.noChangeGoal()),
		DueNoChangeGoal:      manifestGoalOf(o.dueNoChangeGoal()),
		AccountIDs:           map[string]int64{},
		FindingTrouble:       o.findingTrouble(),
		UnderstandingTrouble: o.understandingTrouble(),
		WatchingGoals:        o.watchingGoals(),
		WeeklyCheckin:        o.weeklyCheckin(),
		BadNewsChain:         o.badNewsChain(),
		StatingAGoal:         o.statingAGoal(),
	}
	for _, a := range o.accounts {
		m.AccountIDs[a.Email] = a.ID
	}
	return m, nil
}

// manifestOrg is a seeded company as the choosers read it, through the domain
// Service, judged at now.
type manifestOrg struct {
	now      time.Time
	accounts []domain.Account // by ID
	goals    []*manifestGoal  // by ID
}

// manifestGoal is one Goal with what the choosers read about it.
type manifestGoal struct {
	domain.Goal
	checkins   []domain.Checkin // newest first
	milestones []domain.Milestone
	slips      []domain.DateSlip
	metrics    []domain.Metric
	delegates  []domain.Account
	highlights int
	// drafts is how many Draft Highlights wait on the Goal, as its Owner
	// sees them; read only for an Active Goal whose Owner is still here.
	drafts   int
	children []*manifestGoal // accepted links, any Lifecycle, by ID
	parents  []*manifestGoal // accepted links, any Lifecycle, by ID
	team     string          // "" when it has no Team value
}

// readManifestOrg reads every Account and Goal, and what the choosers read
// about each Goal, through svc.
func readManifestOrg(ctx context.Context, svc *domain.Service, now time.Time) (*manifestOrg, error) {
	accounts, err := svc.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	o := &manifestOrg{now: now, accounts: accounts}
	dims, err := svc.ListDimensions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dimensions: %w", err)
	}
	var teamDim int64
	for _, d := range dims {
		if d.Name == "Team" {
			teamDim = d.ID
		}
	}
	goals, err := svc.ListGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	slices.SortFunc(goals, func(a, b domain.Goal) int { return int(a.ID - b.ID) })
	byID := map[int64]*manifestGoal{}
	for _, g := range goals {
		mg := &manifestGoal{Goal: g}
		o.goals = append(o.goals, mg)
		byID[g.ID] = mg
	}
	for _, g := range o.goals {
		if err := g.read(ctx, svc, byID, teamDim); err != nil {
			return nil, fmt.Errorf("read goal %q: %w", g.Title, err)
		}
	}
	for _, g := range o.goals {
		byID := func(a, b *manifestGoal) int { return int(a.ID - b.ID) }
		slices.SortFunc(g.children, byID)
		slices.SortFunc(g.parents, byID)
	}
	return o, nil
}

// read fills in what the choosers read about g, linking it to its children
// in byID.
func (g *manifestGoal) read(ctx context.Context, svc *domain.Service, byID map[int64]*manifestGoal, teamDim int64) error {
	var err error
	if g.checkins, err = svc.ListCheckins(ctx, g.ID); err != nil {
		return err
	}
	if g.milestones, err = svc.ListMilestones(ctx, g.ID); err != nil {
		return err
	}
	if g.slips, err = svc.ListDateSlips(ctx, g.ID); err != nil {
		return err
	}
	if g.metrics, err = svc.ListMetrics(ctx, g.ID); err != nil {
		return err
	}
	if g.delegates, err = svc.ListDelegates(ctx, g.ID); err != nil {
		return err
	}
	highlights, err := svc.ListHighlightsByGoal(ctx, g.ID)
	if err != nil {
		return err
	}
	g.highlights = len(highlights)
	if g.active() && !g.Owner.Departed {
		drafts, err := svc.PendingDraftHighlights(ctx, g.Owner.ID, g.ID)
		if err != nil {
			return err
		}
		g.drafts = len(drafts)
	}
	children, err := svc.ChildrenOf(ctx, g.ID)
	if err != nil {
		return err
	}
	for _, c := range children {
		child := byID[c.ID]
		g.children = append(g.children, child)
		child.parents = append(child.parents, g)
	}
	values, err := svc.GoalValues(ctx, g.ID)
	if err != nil {
		return err
	}
	for _, v := range values {
		if v.DimensionID == teamDim {
			g.team = v.Value
		}
	}
	return nil
}

// today is now's date in UTC, the e2e server's timezone.
func (o *manifestOrg) today() time.Time {
	y, m, d := o.now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// find is the first Goal, by ID, that meets every one of rules.
func (o *manifestOrg) find(rules ...func(*manifestGoal) bool) *manifestGoal {
	for _, g := range o.goals {
		if manifestMeetsAll(g, rules) {
			return g
		}
	}
	return nil
}

// findAll is every Goal, by ID, that meets every one of rules.
func (o *manifestOrg) findAll(rules ...func(*manifestGoal) bool) []*manifestGoal {
	var out []*manifestGoal
	for _, g := range o.goals {
		if manifestMeetsAll(g, rules) {
			out = append(out, g)
		}
	}
	return out
}

func manifestMeetsAll[T any](v T, rules []func(T) bool) bool {
	for _, r := range rules {
		if !r(v) {
			return false
		}
	}
	return true
}

func (g *manifestGoal) active() bool { return g.Lifecycle == domain.LifecycleActive }

// ownerHere is whether g's Owner hasn't departed.
func (g *manifestGoal) ownerHere() bool { return !g.Owner.Departed }

func (g *manifestGoal) checkedIn() bool { return len(g.checkins) > 0 }

// health is the Health of the Goal's latest Check-in, "" when it has none.
func (g *manifestGoal) health() string {
	if len(g.checkins) == 0 {
		return ""
	}
	return g.checkins[0].Health
}

func (g *manifestGoal) pathToGreen() string {
	if len(g.checkins) == 0 {
		return ""
	}
	return g.checkins[0].PathToGreen
}

func (g *manifestGoal) troubled() bool {
	return g.health() == domain.HealthYellow || g.health() == domain.HealthRed
}

func (g *manifestGoal) hasChildren() bool { return len(g.children) > 0 }

func (g *manifestGoal) activeChildren() []*manifestGoal {
	var out []*manifestGoal
	for _, c := range g.children {
		if c.active() {
			out = append(out, c)
		}
	}
	return out
}

// plannedMilestones are the Goal's Planned Milestones, soonest first.
func (g *manifestGoal) plannedMilestones() []domain.Milestone {
	var out []domain.Milestone
	for _, m := range g.milestones {
		if m.Status == domain.MilestonePlanned {
			out = append(out, m)
		}
	}
	return out
}

// deliverySlips are the Goal's Date Slips of its delivery date, oldest first.
func (g *manifestGoal) deliverySlips() []domain.DateSlip {
	var out []domain.DateSlip
	for _, s := range g.slips {
		if s.OnDeliveryDate() {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b domain.DateSlip) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

// overdueMilestone is whether g has a Planned Milestone dated before today.
func (o *manifestOrg) overdueMilestone(g *manifestGoal) bool {
	return slices.ContainsFunc(g.plannedMilestones(), func(m domain.Milestone) bool { return m.TargetDate.Before(o.today()) })
}

// pathOverdue is whether g's latest Check-in has a back-to-Green date, and it
// is before today.
func (o *manifestOrg) pathOverdue(g *manifestGoal) bool {
	if len(g.checkins) == 0 {
		return false
	}
	date := g.checkins[0].PathTargetDate
	return !date.IsZero() && date.Before(o.today())
}

// lastUpdate is the Goal's last Check-in, or its activation, or its creation.
func (g *manifestGoal) lastUpdate() time.Time {
	switch {
	case len(g.checkins) > 0:
		return g.checkins[0].CreatedAt
	case !g.ActivatedAt.IsZero():
		return g.ActivatedAt
	default:
		return g.CreatedAt
	}
}

// checkinOverdueBy is whether more than g's cadence and extraDays have passed
// since its last update: whole days of time, not of the calendar.
func (o *manifestOrg) checkinOverdueBy(g *manifestGoal, extraDays int) bool {
	return o.now.Sub(g.lastUpdate()) > time.Duration(g.CadenceDays+extraDays)*24*time.Hour
}

// isCheckinOverdue is whether more than g's cadence has passed since its last
// update.
func (o *manifestOrg) isCheckinOverdue(g *manifestGoal) bool { return o.checkinOverdueBy(g, 0) }

// checkedInWithinCadence is whether g's last Check-in is less than its cadence
// ago. A Goal with no Check-in fails it.
func (o *manifestOrg) checkedInWithinCadence(g *manifestGoal) bool {
	return len(g.checkins) > 0 && g.checkins[0].CreatedAt.After(o.now.Add(-time.Duration(g.CadenceDays)*24*time.Hour))
}

// ownedBy is every Goal the Account owns, by ID.
func (o *manifestOrg) ownedBy(id int64) []*manifestGoal {
	return o.findAll(func(g *manifestGoal) bool { return g.Owner.ID == id })
}

func manifestGoalOf(g *manifestGoal) *ManifestGoal {
	if g == nil {
		return nil
	}
	mg := g.pick()
	return &mg
}

// pick is g as a ManifestGoal.
func (g *manifestGoal) pick() ManifestGoal {
	return ManifestGoal{ID: g.ID, Title: g.Title, Email: g.Owner.Email, Name: g.Owner.Name}
}

func manifestGoals(gs []*manifestGoal) []ManifestGoal {
	out := make([]ManifestGoal, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.pick())
	}
	return out
}

func manifestPerson(a domain.Account) ManifestPerson {
	return ManifestPerson{Email: a.Email, Name: a.Name}
}

// ownerName is the Owner's Name, or their email when they have none.
func (g *manifestGoal) ownerName() string {
	if g.Owner.Name != "" {
		return g.Owner.Name
	}
	return g.Owner.Email
}

func manifestDate(d time.Time) string {
	if d.IsZero() {
		return ""
	}
	return d.Format(dateFormat)
}

func (g *manifestGoal) noMetric() bool         { return len(g.metrics) == 0 }
func (g *manifestGoal) noDraftHighlight() bool { return g.drafts == 0 }

// activeHere is the choosers' commonest rule: an Active Goal whose Owner is
// still here.
func activeHere(g *manifestGoal) bool { return g.active() && g.ownerHere() }

func (o *manifestOrg) quietGoal() *manifestGoal {
	return o.find(activeHere, (*manifestGoal).noMetric, func(g *manifestGoal) bool {
		return !g.hasChildren() && !o.overdueMilestone(g) && g.highlights == 0 && g.noDraftHighlight() &&
			g.health() == domain.HealthGreen
	})
}

func (o *manifestOrg) draftlessGoal() *manifestGoal {
	return o.find(activeHere, (*manifestGoal).noDraftHighlight)
}

func (o *manifestOrg) metricGoal() *manifestGoal {
	return o.find(activeHere, func(g *manifestGoal) bool {
		return !g.noMetric() && g.checkedIn() && g.noDraftHighlight() && !o.overdueMilestone(g)
	})
}

func (o *manifestOrg) metricParentGoal() *manifestGoal {
	return o.find(activeHere, func(g *manifestGoal) bool { return !g.noMetric() && g.checkedIn() && g.hasChildren() })
}

// canPressNoChange is whether g's Owner can press No change: checked in
// before, no accepted child Goals, no overdue Planned Milestone, and no
// back-to-Green date passed.
func (o *manifestOrg) canPressNoChange(g *manifestGoal) bool {
	return g.checkedIn() && !g.hasChildren() && !o.overdueMilestone(g) && !o.pathOverdue(g)
}

func (o *manifestOrg) noChangeGoal() *manifestGoal {
	return o.find(activeHere, o.canPressNoChange)
}

func (o *manifestOrg) dueNoChangeGoal() *manifestGoal {
	return o.find(activeHere, o.canPressNoChange, func(g *manifestGoal) bool { return g.CadenceDays == 7 })
}

func (o *manifestOrg) findingTrouble() FindingTroubleRoles {
	platform := func(g *manifestGoal) bool { return g.team == "Platform" }
	active := (*manifestGoal).active
	checkinOverdue := o.find(active, platform, o.isCheckinOverdue, func(g *manifestGoal) bool {
		return g.Owner.Email != manifestLeader && g.ownerHere()
	})
	departing := o.find(active, platform, func(g *manifestGoal) bool {
		a := g.Owner
		if a.Email == manifestLeader || a.Email == manifestAdmin || a.IsAdmin {
			return false
		}
		return !slices.ContainsFunc(o.ownedBy(a.ID), func(h *manifestGoal) bool {
			return h.TopLevel || (h.active() && o.isCheckinOverdue(h))
		})
	})
	var departingEmail string
	var ownerless []*manifestGoal
	if departing != nil {
		departingEmail = departing.Owner.Email
		ownerless = o.findAll(active, func(g *manifestGoal) bool { return g.Owner.Email == departingEmail })
	}
	notDeparting := func(g *manifestGoal) bool { return g.Owner.Email != departingEmail }
	overdue := o.find(active, platform, notDeparting, func(g *manifestGoal) bool {
		return g.Owner.Email != manifestLeader && g.ownerHere() && g != checkinOverdue && !o.isCheckinOverdue(g) &&
			len(g.activeChildren()) == 0 && !o.overdueMilestone(g)
	})
	tiered := o.findAll(active, platform)
	tiered = tiered[:min(2, len(tiered))]
	unlinked := o.find(active, notDeparting, func(g *manifestGoal) bool { return !g.TopLevel && len(g.parents) == 0 })
	twoReasons := o.find(active, notDeparting, o.isCheckinOverdue, func(g *manifestGoal) bool {
		return g.team != "" && !g.DeliveryDate.IsZero() &&
			slices.ContainsFunc(g.parents, func(p *manifestGoal) bool {
				return !p.DeliveryDate.IsZero() && g.DeliveryDate.After(p.DeliveryDate)
			})
	})
	redWithChildren := o.find(active, notDeparting, func(g *manifestGoal) bool {
		return g.health() == domain.HealthRed && !o.isCheckinOverdue(g) && len(g.parents) > 0 && len(g.activeChildren()) > 0
	})
	redSlipped := o.find(active, func(g *manifestGoal) bool {
		return g.health() == domain.HealthRed && !g.DeliveryDate.IsZero() && len(g.deliverySlips()) > 0
	})

	roles := FindingTroubleRoles{
		CheckinOverdue:  manifestGoalOf(checkinOverdue),
		Departing:       manifestGoalOf(departing),
		Overdue:         manifestGoalOf(overdue),
		Tiered:          manifestGoals(tiered),
		Unlinked:        manifestGoalOf(unlinked),
		Ownerless:       manifestGoals(ownerless),
		RedWithChildren: manifestGoalOf(redWithChildren),
		RedSlipped:      manifestGoalOf(redSlipped),
		Lifecycles:      map[int64]string{},
	}
	if twoReasons != nil {
		roles.TwoReasons = &TwoReasonsGoal{ManifestGoal: twoReasons.pick(), Team: twoReasons.team}
	}
	for _, g := range o.goals {
		roles.Lifecycles[g.ID] = g.Lifecycle
	}
	return roles
}

// teamReport are the Goals a "Team is any of team" Report lists against its
// default baseline, 30 days back: inReport every Goal on the team less those
// Done or Cancelled before the baseline, and finishedEarlier those.
func (o *manifestOrg) teamReport(team string) (inReport, finishedEarlier []*manifestGoal) {
	baseline := o.now.AddDate(0, 0, -30)
	for _, g := range o.goals {
		if g.team != team {
			continue
		}
		if domain.LifecycleEnded(g.Lifecycle) {
			var finished time.Time
			for _, c := range g.checkins {
				if c.LifecycleChange.To == g.Lifecycle && c.CreatedAt.After(finished) {
					finished = c.CreatedAt
				}
			}
			if finished.Before(baseline) {
				finishedEarlier = append(finishedEarlier, g)
				continue
			}
		}
		inReport = append(inReport, g)
	}
	return inReport, finishedEarlier
}

// reportGoal is g as a Report's block shows it.
func (g *manifestGoal) reportGoal() ReportGoal {
	r := ReportGoal{
		ID: g.ID, Title: g.Title, Owner: g.ownerName(), Email: g.Owner.Email, Lifecycle: g.Lifecycle,
		WhyItMatters: g.WhyItMatters, DeliveryDate: manifestDate(g.DeliveryDate),
		Milestones: len(g.milestones), Metrics: len(g.metrics),
	}
	if len(g.checkins) > 0 {
		r.Status, r.PathToGreen, r.Health = g.checkins[0].Status, g.checkins[0].PathToGreen, g.checkins[0].Health
	}
	return r
}

func reportGoalOf(g *manifestGoal) *ReportGoal {
	if g == nil {
		return nil
	}
	r := g.reportGoal()
	return &r
}

func (o *manifestOrg) understandingTrouble() UnderstandingTroubleRoles {
	platform, _ := o.teamReport("Platform")
	roles := UnderstandingTroubleRoles{PlatformReport: make([]ReportGoal, 0, len(platform))}
	for _, g := range platform {
		roles.PlatformReport = append(roles.PlatformReport, g.reportGoal())
	}
	slipped := o.pickCheckinGoal(platform, domain.HealthGreen, func(g *manifestGoal) bool { return g.Kind == domain.GoalDated })
	turning := o.pickCheckinGoal(platform, domain.HealthYellow, func(g *manifestGoal) bool { return g != slipped })
	var moved *manifestGoal
	if i := slices.IndexFunc(platform, func(g *manifestGoal) bool { return g != slipped && g != turning }); i >= 0 {
		moved = platform[i]
	}
	roles.Slipped, roles.Turning, roles.Moved = reportGoalOf(slipped), reportGoalOf(turning), reportGoalOf(moved)
	return roles
}

// pickCheckinGoal is the first of candidates that meets extra and that its
// Owner can check in on plainly, with the Health given first if one has it:
// Active, its Owner still here, checked in within its cadence, with no
// accepted child Goals and no overdue Planned Milestone.
func (o *manifestOrg) pickCheckinGoal(candidates []*manifestGoal, health string, extra func(*manifestGoal) bool) *manifestGoal {
	var first *manifestGoal
	for _, g := range candidates {
		if !activeHere(g) || !o.checkedInWithinCadence(g) || g.hasChildren() || o.overdueMilestone(g) || !extra(g) {
			continue
		}
		if g.health() == health {
			return g
		}
		if first == nil {
			first = g
		}
	}
	return first
}

func (o *manifestOrg) watchingGoals() WatchingGoalsRoles {
	inReport, finishedEarlier := o.teamReport("Growth")
	roles := WatchingGoalsRoles{
		GrowthReport: GrowthReport{InReport: manifestGoals(inReport), FinishedEarlier: manifestGoals(finishedEarlier)},
	}

	candidates := o.findAll((*manifestGoal).active, (*manifestGoal).checkedIn, func(g *manifestGoal) bool {
		return g.team != "" && g.team != "Growth" && g.Owner.Email != manifestManager &&
			!slices.ContainsFunc(g.delegates, func(a domain.Account) bool { return a.Email == manifestManager })
	})
	first := func(meets func(*manifestGoal) bool) *manifestGoal {
		if i := slices.IndexFunc(candidates, meets); i >= 0 {
			return candidates[i]
		}
		return nil
	}
	slipped := first(func(g *manifestGoal) bool { return len(g.deliverySlips()) > 0 && g.troubled() })
	troubled := first(func(g *manifestGoal) bool { return g != slipped && g.troubled() && g.pathToGreen() != "" })
	green := first(func(g *manifestGoal) bool {
		return g.health() == domain.HealthGreen && len(g.activeChildren()) == 0 && !o.overdueMilestone(g)
	})
	others := []*manifestGoal{slipped, troubled, green}
	parent := first(func(g *manifestGoal) bool {
		return len(g.activeChildren()) > 0 && !slices.Contains(others, g) &&
			!slices.ContainsFunc(g.activeChildren(), func(c *manifestGoal) bool { return slices.Contains(others, c) })
	})
	roles.Picked = PickedGoals{
		Troubled: watchedGoal(troubled),
		Slipped:  watchedGoal(slipped),
		Green:    watchedGoal(green),
		Parent:   watchedGoal(parent),
		Children: []ManifestGoal{},
	}
	if parent != nil {
		roles.Picked.Children = manifestGoals(parent.activeChildren())
	}
	return roles
}

func watchedGoal(g *manifestGoal) *WatchedGoal {
	if g == nil {
		return nil
	}
	w := &WatchedGoal{ID: g.ID, Title: g.Title, Health: g.health(), PathToGreen: g.pathToGreen()}
	if slips := g.deliverySlips(); len(slips) > 0 {
		w.SlippedFrom = manifestDate(slips[0].OldDate)
	}
	return w
}

func (o *manifestOrg) weeklyCheckin() WeeklyCheckinRoles {
	type owner struct {
		account domain.Account
		active  []*manifestGoal
	}
	byOwner := map[int64]*owner{}
	var owners []*owner
	for _, g := range o.goals {
		if !activeHere(g) || g.Owner.Email == manifestLeader {
			continue
		}
		if byOwner[g.Owner.ID] == nil {
			byOwner[g.Owner.ID] = &owner{account: g.Owner}
			owners = append(owners, byOwner[g.Owner.ID])
		}
		byOwner[g.Owner.ID].active = append(byOwner[g.Owner.ID].active, g)
	}
	// At least two Active Goals, exactly one with a Metric, and none a No
	// change would be refused on. Most Active Goals first.
	owners = slices.DeleteFunc(owners, func(w *owner) bool {
		metrics := 0
		for _, g := range w.active {
			if !g.noMetric() {
				metrics++
			}
			if g.hasChildren() || o.overdueMilestone(g) || o.pathOverdue(g) {
				return true
			}
		}
		return len(w.active) < 2 || metrics != 1
	})
	slices.SortStableFunc(owners, func(a, b *owner) int {
		if len(a.active) != len(b.active) {
			return len(b.active) - len(a.active)
		}
		return int(a.account.ID - b.account.ID)
	})
	roles := WeeklyCheckinRoles{WeekOwners: make([]WeekOwner, 0, len(owners))}
	for _, w := range owners {
		wo := WeekOwner{ManifestPerson: manifestPerson(w.account)}
		for _, g := range w.active {
			wo.Goals = append(wo.Goals, WeekGoal{ID: g.ID, Title: g.Title, Metric: !g.noMetric()})
		}
		roles.WeekOwners = append(roles.WeekOwners, wo)
	}

	// The Goal whose Owner is away, the longest unchecked first.
	var goal *manifestGoal
	for _, g := range o.goals {
		if !activeHere(g) || g.Owner.Name == "" || !g.checkedIn() || !o.checkinOverdueBy(g, 1) ||
			g.hasChildren() || o.overdueMilestone(g) || len(g.delegates) > 0 {
			continue
		}
		if goal == nil || g.checkins[0].CreatedAt.Before(goal.checkins[0].CreatedAt) {
			goal = g
		}
	}
	if goal != nil {
		roles.DelegateCover.Goal = manifestGoalOf(goal)
		for _, a := range o.accounts {
			if !a.Departed && !a.IsAdmin && a.Name != "" && a.Email != goal.Owner.Email {
				p := manifestPerson(a)
				roles.DelegateCover.Delegate = &p
				break
			}
		}
	}
	return roles
}

// badNewsChain is the first project and team Goal, in (team Goal ID, project
// ID) order, that meet every rule, and the lowest-ID org outcome that
// qualifies. Goals whose Owner has departed still count as children, since
// Rolled-up Health counts them, but are never the project or the team Goal.
func (o *manifestOrg) badNewsChain() *BadNewsChain {
	type chain struct{ project, team *manifestGoal }
	activeGreen := func(g *manifestGoal) bool { return g.active() && g.health() == domain.HealthGreen }
	// An org outcome rolls up Green over a Green team Goal when every other
	// Active child with a Health is Green too.
	greenOrgOutcomes := func(team *manifestGoal) []*manifestGoal {
		var out []*manifestGoal
		for _, org := range team.parents {
			if !slices.ContainsFunc(org.children, func(c *manifestGoal) bool {
				return c != team && c.active() && c.checkedIn() && c.health() != domain.HealthGreen
			}) {
				out = append(out, org)
			}
		}
		return out
	}
	futurePlanned := func(g *manifestGoal) []domain.Milestone {
		var out []domain.Milestone
		for _, m := range g.plannedMilestones() {
			if m.TargetDate.After(o.today()) {
				out = append(out, m)
			}
		}
		return out
	}
	rules := []func(chain) bool{
		func(c chain) bool { return activeGreen(c.project) && c.project.Kind == domain.GoalDated },
		func(c chain) bool { return len(futurePlanned(c.project)) >= 2 },
		func(c chain) bool { return !o.overdueMilestone(c.project) },
		func(c chain) bool { return c.project.team != "" },
		func(c chain) bool { return activeGreen(c.team) },
		func(c chain) bool { return c.project.Owner.Email != c.team.Owner.Email },
		// The team Goal's other Active children are all Green.
		func(c chain) bool {
			return !slices.ContainsFunc(c.team.children, func(k *manifestGoal) bool {
				return k != c.project && k.active() && k.health() != domain.HealthGreen
			})
		},
		// An overdue Milestone would refuse its Owner's Green on its own.
		func(c chain) bool { return !o.overdueMilestone(c.team) },
		func(c chain) bool { return len(greenOrgOutcomes(c.team)) > 0 },
	}
	for _, team := range o.goals {
		for _, project := range team.children {
			c := chain{project, team}
			if project.Owner.Departed || team.Owner.Departed || !manifestMeetsAll(c, rules) {
				continue
			}
			p := BadNewsProject{BadNewsGoal: project.badNewsGoal(), Milestones: []BadNewsMilestone{}, Team: project.team}
			for _, m := range futurePlanned(project) {
				p.Milestones = append(p.Milestones, BadNewsMilestone{ID: m.ID, Name: m.Name, TargetDate: manifestDate(m.TargetDate)})
			}
			return &BadNewsChain{Project: p, Team: team.badNewsGoal(), OrgOutcome: greenOrgOutcomes(team)[0].badNewsGoal()}
		}
	}
	return nil
}

func (g *manifestGoal) badNewsGoal() BadNewsGoal {
	return BadNewsGoal{ID: g.ID, Title: g.Title, Email: g.Owner.Email, Name: g.ownerName(), DeliveryDate: manifestDate(g.DeliveryDate)}
}

func (o *manifestOrg) statingAGoal() StatingAGoalRoles {
	lead := func(a domain.Account) bool { return strings.HasSuffix(strings.ToLower(a.Email), "-lead@example.com") }
	teamGoals := o.findAll(activeHere, func(g *manifestGoal) bool { return lead(g.Owner) && g.team != "" })

	// A project is an Active Goal, its Owner still here and neither a lead
	// nor the leadership, contributing to a lead's Goal.
	type project struct {
		owner  domain.Account
		parent *manifestGoal
	}
	var projects []project
	for _, g := range o.goals {
		if !activeHere(g) || lead(g.Owner) || slices.Contains(manifestLeadership, g.Owner.Email) {
			continue
		}
		for _, p := range g.parents {
			if lead(p.Owner) {
				projects = append(projects, project{g.Owner, p})
			}
		}
	}
	// projectOwners are the projects' Owners, each once, those under the team
	// Goal first, then by email.
	projectOwners := func(team *manifestGoal) []ManifestPerson {
		elsewhere := func(p project) bool { return p.parent != team }
		slices.SortStableFunc(projects, func(a, b project) int {
			return cmp.Or(compareBool(elsewhere(a), elsewhere(b)), strings.Compare(a.owner.Email, b.owner.Email))
		})
		out := []ManifestPerson{}
		for _, p := range projects {
			if !slices.ContainsFunc(out, func(q ManifestPerson) bool { return q.Email == p.owner.Email }) {
				out = append(out, manifestPerson(p.owner))
			}
		}
		return out
	}

	roles := StatingAGoalRoles{TeamGoals: make([]TeamGoal, 0, len(teamGoals))}
	for _, g := range teamGoals {
		roles.TeamGoals = append(roles.TeamGoals, TeamGoal{ManifestGoal: g.pick(), Team: g.team, ProjectOwners: projectOwners(g)})
	}
	if owners := projectOwners(nil); len(owners) > 0 {
		roles.ProjectOwner = &owners[0]
	}
	return roles
}

// compareBool orders false before true.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}
