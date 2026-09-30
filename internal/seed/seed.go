// Package seed builds a fake but realistic org so a developer can demo the
// prototype without real data (ticket #23). It fills a fresh database the way
// the org itself would: an Admin defines a Team Dimension, imports the Goals
// through the spreadsheet import (internal/importer), and marks the org
// outcomes Top-level Goals, then the Owners activate them and write weeks of
// Check-ins through the domain commands — Health, Paths to Green, Metric
// readings, Date Slips, Milestone Churn, and Lifecycle changes.
// Nothing is written to the database directly.
//
// The org is deterministic: the same random seed and the same end date build
// the same org, Check-in for Check-in.
package seed

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/importer"
)

// DefaultSeed is the fixed random seed the seed command uses unless told
// otherwise, so every developer demos the same org.
const DefaultSeed uint64 = 23

// weeks is how many weeks of Check-in history the seed writes, ending at the
// clock's time when Run is called.
const weeks = 12

// dateFormat is how calendar dates are written in the import spreadsheet.
const dateFormat = "2006-01-02"

// ErrNotFresh is returned when the database already holds Goals: the seed only
// builds an org into a fresh database.
var ErrNotFresh = errors.New("the database already has Goals; seed a fresh database")

// Options configures a seed run.
type Options struct {
	// Seed is the random seed; the same seed builds the same org.
	Seed uint64
	// Admin is the email of the Admin who defines the Team Dimension and runs
	// the import. The Service must list it as an Admin.
	Admin string
}

// Summary counts what a seed run built.
type Summary struct {
	Goals    int
	Checkins int
}

// Run builds the fake org through svc. The history ends at clk's current time:
// Run moves clk back to the start of the history and forward week by week as
// the Owners check in, leaving it where it started.
func Run(ctx context.Context, svc *domain.Service, clk *clock.Fixed, opts Options) (Summary, error) {
	existing, err := svc.ListGoals(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("list goals: %w", err)
	}
	if len(existing) > 0 {
		return Summary{}, ErrNotFresh
	}

	end := clk.Now()
	start := end.AddDate(0, 0, -7*weeks)
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed))
	plan := newPlan(rng, day(start))

	clk.Set(start)
	defer clk.Set(end)

	admin, err := svc.SignIn(ctx, opts.Admin)
	if err != nil {
		return Summary{}, fmt.Errorf("sign in Admin: %w", err)
	}
	teamNames := make([]string, 0, len(teams))
	for _, t := range teams {
		teamNames = append(teamNames, t.name)
	}
	if _, err := svc.CreateDimension(ctx, admin.ID, teamDimension, teamNames); err != nil {
		return Summary{}, fmt.Errorf("define the Team Dimension: %w", err)
	}

	rep, err := importer.New(svc).Commit(ctx, admin.ID, "seed.csv", plan.csv())
	if err != nil {
		return Summary{}, fmt.Errorf("import the org: %w", err)
	}
	if !rep.Committed {
		return Summary{}, fmt.Errorf("import the org: %s", rowErrors(rep))
	}
	for i, row := range rep.Rows {
		plan.goals[i].id = row.GoalID
	}
	for _, g := range plan.goals {
		if g.level != 1 {
			continue
		}
		if _, err := svc.MarkTopLevel(ctx, admin.ID, g.id); err != nil {
			return Summary{}, fmt.Errorf("mark %q Top-level: %w", g.title, err)
		}
	}

	hist := &history{svc: svc, clk: clk, rng: rng, end: end}
	checkins, err := hist.run(ctx, plan)
	if err != nil {
		return Summary{}, err
	}
	return Summary{Goals: len(plan.goals), Checkins: checkins}, nil
}

// plannedGoal is one Goal of the org with everything the random seed decided
// about it.
type plannedGoal struct {
	entry
	team       string // "" for an org outcome
	owner      string
	parents    []string // titles of the Goals it contributes to
	delivery   time.Time
	milestones []plannedMilestone
	metricCell string  // import-format Metric with its target date, or ""
	level      int     // 1 for an org outcome, 2 for a team Goal, 3 for a project
	profile    profile // how a project's weeks play out
	// turn is the week a project's profile plays out: a troubled one turns
	// Yellow, a churning one's scope moves, a stale one's Owner goes quiet, and
	// a paused or cancelled one goes On Hold.
	turn    int
	recover int // the week a troubled project recovers to Yellow, or 0

	// Set once the Goal exists.
	id        int64
	ownerID   int64
	metrics   []domain.Metric
	slipped   map[int64]bool // Milestones whose date has moved
	lifecycle string         // the Goal's Lifecycle as the history plays out
}

type plannedMilestone struct {
	name string
	date time.Time
}

// plan is the whole org, outcomes first, then each team's Goals, then the
// projects, so every Goal comes after the Goals it contributes to.
type plan struct {
	goals []*plannedGoal
}

// milestoneNames are the stages a Dated Goal's Milestones are drawn from, in
// order.
var milestoneNames = []string{"Design review", "Prototype", "Beta", "Rollout to 50%", "GA"}

func newPlan(rng *rand.Rand, day0 time.Time) *plan {
	p := &plan{}
	add := func(level int, e entry, teamName, owner string, parents []string, deliveryWeeks int) *plannedGoal {
		g := &plannedGoal{entry: e, team: teamName, owner: owner, parents: parents, level: level, lifecycle: domain.LifecycleProposed}
		g.schedule(rng, day0, deliveryWeeks)
		p.goals = append(p.goals, g)
		return g
	}

	for _, o := range outcomes {
		add(1, o.entry, "", o.owner, nil, 0)
	}
	for _, t := range teams {
		for _, e := range t.goals {
			add(2, e, t.name, t.lead, titlesOf(e.parents, outcomeEntries()), 14+rng.IntN(9))
		}
	}
	// Profiles are dealt to the aligned projects only; the Unaligned ones stay
	// steady and Active, so they are always there to be seen.
	var aligned []*plannedGoal
	for _, t := range teams {
		for _, e := range t.projects {
			aligned = append(aligned, add(3, e, t.name, t.people[rng.IntN(len(t.people))], titlesOf(e.parents, t.goals), 14+rng.IntN(11)))
		}
		for _, e := range t.unaligned {
			add(3, e, t.name, t.people[rng.IntN(len(t.people))], nil, 14+rng.IntN(11)).profile = steady
		}
	}
	for i, j := range rng.Perm(len(aligned)) {
		if i < len(profileDeck) {
			dealProfile(rng, day0, aligned[j], profileDeck[i])
		}
	}
	return p
}

// schedule sets a Goal's dates: an Ongoing Goal's Metric targets the end of
// the year; a Dated Goal is due deliveryWeeks after day0 (on the date the picker
// would suggest), with its Milestones spread before that and its Metric, if
// any, targeting the delivery date.
func (g *plannedGoal) schedule(rng *rand.Rand, day0 time.Time, deliveryWeeks int) {
	if g.ongoing {
		g.metricCell = g.metric + " | " + day0.AddDate(0, 0, 7*40).Format(dateFormat)
		return
	}
	g.delivery = domain.SuggestDeliveryDate(day0.AddDate(0, 0, 7*deliveryWeeks))
	g.milestones = planMilestones(rng, day0, g.delivery)
	if g.metric != "" {
		g.metricCell = g.metric + " | " + g.delivery.Format(dateFormat)
	}
}

// dealProfile gives a project its profile and decides when its turns come. A
// finished project is rescheduled to deliver within the history.
func dealProfile(rng *rand.Rand, day0 time.Time, g *plannedGoal, pr profile) {
	g.profile = pr
	if pr == finished {
		g.schedule(rng, day0, 5+rng.IntN(3))
	}
	g.turn = 3 + rng.IntN(5)
	if pr == stale {
		g.turn = weeks - 1 - rng.IntN(5)
	}
	if pr == troubled {
		if rng.IntN(2) == 0 {
			g.recover = g.turn + 4 + rng.IntN(3)
		}
	}
}

// planMilestones spreads two to four Milestones evenly between a week after
// day0 and a week before the delivery date.
func planMilestones(rng *rand.Rand, day0, delivery time.Time) []plannedMilestone {
	n := 2 + rng.IntN(3)
	first := day0.AddDate(0, 0, 7)
	span := delivery.AddDate(0, 0, -7).Sub(first)
	names := milestoneNames[len(milestoneNames)-n:]
	out := make([]plannedMilestone, n)
	for i := range out {
		offset := span * time.Duration(i+1) / time.Duration(n)
		out[i] = plannedMilestone{name: names[i], date: day(first.Add(offset))}
	}
	return out
}

func outcomeEntries() []entry {
	out := make([]entry, len(outcomes))
	for i, o := range outcomes {
		out[i] = o.entry
	}
	return out
}

func titlesOf(indexes []int, entries []entry) []string {
	out := make([]string, len(indexes))
	for i, idx := range indexes {
		out[i] = entries[idx].title
	}
	return out
}

// csv renders the plan in the spreadsheet import format (docs/import-format.md),
// with the Team Dimension as its own column.
func (p *plan) csv() []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"Title", "Owner", "So What", "Kind", "Delivery Date", "Milestones", "Metrics", "Parents", teamDimension})
	for _, g := range p.goals {
		kind, delivery := domain.GoalOngoing, ""
		if !g.ongoing {
			kind, delivery = domain.GoalDated, g.delivery.Format(dateFormat)
		}
		milestones := make([]string, len(g.milestones))
		for i, m := range g.milestones {
			milestones[i] = m.name + " @ " + m.date.Format(dateFormat)
		}
		_ = w.Write([]string{
			g.title, g.owner, g.soWhat, kind, delivery,
			strings.Join(milestones, "; "), g.metricCell, strings.Join(g.parents, "; "), g.team,
		})
	}
	w.Flush()
	return buf.Bytes()
}

func rowErrors(rep importer.Report) string {
	var msgs []string
	for _, row := range rep.Rows {
		for _, e := range row.Errors {
			msgs = append(msgs, fmt.Sprintf("line %d (%s): %s", row.Line, row.Title, e))
		}
	}
	return strings.Join(msgs, "; ")
}

// day truncates t to its calendar date in UTC.
func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
