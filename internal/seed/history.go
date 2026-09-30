package seed

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// profile is how a project's weeks play out. Team Goals and org outcomes have
// no profile of their own: their Owners set Health from what rolls up to them.
type profile int

const (
	// steady projects stay Green and finish their Milestones on time.
	steady profile = iota
	// troubled projects turn Yellow partway through, then Red, and some recover
	// to Yellow.
	troubled
)

// profileDeck is dealt, shuffled, to the projects; any project beyond the deck
// is steady.
var profileDeck = []profile{troubled, troubled, troubled, troubled, troubled, troubled}

// history writes the org's weeks of Check-ins through the domain commands,
// moving the clock forward as it goes.
type history struct {
	svc *domain.Service
	clk *clock.Fixed
	rng *rand.Rand
	end time.Time
}

// run activates the planned Goals and has their Owners check in every week of
// the history. Projects check in first each week, then team Goals, then the org
// outcomes, so each parent's Owner sees what rolled up to them that week. It
// returns how many Check-ins it wrote.
func (h *history) run(ctx context.Context, p *plan) (int, error) {
	for _, g := range p.goals {
		owner, err := h.svc.SignIn(ctx, g.owner)
		if err != nil {
			return 0, fmt.Errorf("sign in Owner %s: %w", g.owner, err)
		}
		g.ownerID = owner.ID
		if _, err := h.svc.ActivateGoal(ctx, g.id); err != nil {
			return 0, fmt.Errorf("activate %q: %w", g.title, err)
		}
		if g.team == "" {
			// Org outcomes move slowly and are reviewed every other week.
			if _, err := h.svc.SetCadence(ctx, g.id, 14); err != nil {
				return 0, fmt.Errorf("set cadence of %q: %w", g.title, err)
			}
		}
		metrics, err := h.svc.ListMetrics(ctx, g.id)
		if err != nil {
			return 0, fmt.Errorf("list metrics of %q: %w", g.title, err)
		}
		g.metrics = metrics
	}

	checkins := 0
	for w := 1; w <= weeks; w++ {
		// Each week's Check-ins land on the same afternoon, a minute apart.
		afternoon := h.end.AddDate(0, 0, -7*(weeks-w)).Add(-3 * time.Hour)
		for i := len(p.goals) - 1; i >= 0; i-- {
			g := p.goals[i]
			h.clk.Set(afternoon.Add(time.Duration(len(p.goals)-1-i) * time.Minute))
			if g.team == "" && (weeks-w)%2 != 0 {
				continue
			}
			if err := h.checkIn(ctx, g, w); err != nil {
				return checkins, fmt.Errorf("week %d, check in on %q: %w", w, g.title, err)
			}
			checkins++
		}
	}
	return checkins, nil
}

// checkIn writes one week's Check-in on g: it marks the Milestones that have
// come due Done, sets the Health the Goal's profile (or, for a parent, its
// roll-up) calls for, and records a reading for each Metric.
func (h *history) checkIn(ctx context.Context, g *plannedGoal, w int) error {
	today := day(h.clk.Now())
	in := domain.SubmitCheckinInput{GoalID: g.id, AuthorID: g.ownerID}

	milestones, err := h.svc.ListMilestones(ctx, g.id)
	if err != nil {
		return err
	}
	behind := g.profile == troubled && w >= g.turn
	for _, m := range milestones {
		if m.Status != domain.MilestonePlanned || m.TargetDate.After(today) {
			continue
		}
		// A troubled project moves each Milestone that comes due while it is
		// behind, once; otherwise the Milestone is done.
		if behind && !g.slipped[m.ID] {
			if g.slipped == nil {
				g.slipped = map[int64]bool{}
			}
			g.slipped[m.ID] = true
			in.Milestones = append(in.Milestones, domain.MilestoneChangeInput{
				MilestoneID: m.ID,
				TargetDate:  m.TargetDate.AddDate(0, 0, 7*(1+h.rng.IntN(2))),
				DateReason:  pick(h.rng, slipReasons),
			})
			continue
		}
		in.Milestones = append(in.Milestones, domain.MilestoneChangeInput{MilestoneID: m.ID, Status: domain.MilestoneDone})
	}
	// The week a troubled project turns, its Owner moves the delivery date.
	if g.profile == troubled && w == g.turn && !g.ongoing {
		g.delivery = g.delivery.AddDate(0, 0, 7*(2+h.rng.IntN(3)))
		in.DeliveryDate = g.delivery
		in.DeliveryDateReason = pick(h.rng, slipReasons)
	}

	if g.level < 3 {
		if err := h.followRollup(ctx, g, &in); err != nil {
			return err
		}
	} else {
		in.Health = g.healthAt(w)
	}
	in.Status = pick(h.rng, statuses[in.Health])
	if in.Health != domain.HealthGreen {
		in.PathToGreen = pick(h.rng, pathsToGreen)
		in.PathTargetDate = today.AddDate(0, 0, 7*(3+h.rng.IntN(4)))
	}

	for _, m := range g.metrics {
		in.Readings = append(in.Readings, domain.MetricReadingInput{MetricID: m.ID, Value: h.reading(g, m, w)})
	}

	_, err = h.svc.SubmitCheckin(ctx, in)
	return err
}

// followRollup sets a parent's Health from its Rolled-up Health. Most weeks the
// Owner agrees with it; some weeks they judge the risk below contained and
// report one step better, explaining why (ADR 0003).
func (h *history) followRollup(ctx context.Context, g *plannedGoal, in *domain.SubmitCheckinInput) error {
	rollup, err := h.svc.RolledUpHealth(ctx, g.id)
	if err != nil {
		return err
	}
	in.Health = domain.HealthGreen
	if !rollup.Present {
		return nil
	}
	in.Health = rollup.Health
	if h.rng.Float64() < 0.3 {
		switch rollup.Health {
		case domain.HealthRed:
			in.Health = domain.HealthYellow
		case domain.HealthYellow:
			in.Health = domain.HealthGreen
		}
	}
	in.Explanation = pick(h.rng, explanations)
	return nil
}

// healthAt is the Health a project's Owner reports in week w.
func (g *plannedGoal) healthAt(w int) string {
	if g.profile != troubled || w < g.turn {
		return domain.HealthGreen
	}
	if w < g.turn+2 || (g.recover > 0 && w >= g.recover) {
		return domain.HealthYellow
	}
	return domain.HealthRed
}

// reading is a Metric's value in week w: steady progress from its baseline
// toward its target, slower on a troubled project, with a little noise.
func (h *history) reading(g *plannedGoal, m domain.Metric, w int) float64 {
	progress := 0.7 * float64(w) / weeks
	if g.profile == troubled && w >= g.turn {
		progress = 0.7 * float64(g.turn) / weeks
	}
	progress += (h.rng.Float64() - 0.5) * 0.06
	v := m.Baseline + (m.Target-m.Baseline)*progress
	return math.Round(v*100) / 100
}

func pick(rng *rand.Rand, options []string) string {
	return options[rng.IntN(len(options))]
}

var statuses = map[string][]string{
	domain.HealthGreen: {
		"On track; this week's work landed as planned.",
		"Good week: shipped what we committed to and nothing new is blocking us.",
		"Steady progress, no surprises.",
		"On plan. Next Milestone is staffed and moving.",
	},
	domain.HealthYellow: {
		"At risk: a dependency is late and we're absorbing it for now.",
		"Behind by about a week; we lost two people to an incident.",
		"Scope turned out larger than estimated; replanning the back half.",
		"Vendor sign-off is slower than expected.",
	},
	domain.HealthRed: {
		"Off track: the critical path is blocked and we can't hit the date as planned.",
		"Blocked on a platform change another team hasn't prioritised.",
		"Two key engineers moved to incident response; the date is not achievable.",
	},
}

var pathsToGreen = []string{
	"Pull in a second engineer from the platform pool for three weeks.",
	"Cut the stretch scope and ship the core flow first.",
	"Escalating the dependency at the next staff meeting; need a decision on priority.",
	"Run the migration in two phases so the first half can ship on its own.",
	"Ask leadership to trade one of this quarter's smaller Goals for the capacity.",
}

var slipReasons = []string{
	"A dependency from another team landed late.",
	"Security review found issues we have to fix before launch.",
	"Lost a week to an incident on the critical path.",
	"Scope grew after customer interviews.",
	"Vendor contract took longer to sign than planned.",
}

var explanations = []string{
	"The at-risk work below is off the critical path; the outcome is still on track.",
	"The trouble below is contained to one project and has a credible Path to Green.",
	"Other contributors are ahead of plan and cover the shortfall.",
}
