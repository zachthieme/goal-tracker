package testsupport

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// ReportFactParityPublication is a publication holding a fact of every kind
// (domain.AllFactKinds), each in words no other fact uses, so an output that
// leaves one out can't pass on another's words. Every output of a publication
// checks it shows each of them (CheckShowsEveryReportFact).
func ReportFactParityPublication() domain.Publication {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	publisher := domain.Account{ID: 1, Email: "pat.publisher@example.com", Name: "Pat Publisher"}
	owner := domain.Account{ID: 2, Email: "olu.owner@example.com", Name: "Olu Owner"}
	helper := domain.Account{ID: 3, Email: "hana.helper@example.com", Name: "Hana Helper"}
	chaser := domain.Account{ID: 4, Email: "cy.chaser@example.com", Name: "Cy Chaser"}
	writer := domain.Account{ID: 5, Email: "wren.writer@example.com", Name: "Wren Writer"}
	liner := domain.Account{ID: 6, Email: "lin.liner@example.com", Name: "Lin Liner"}
	planner := domain.Account{ID: 8, Email: "pia.planner@example.com", Name: "Pia Planner"}
	wirer := domain.Account{ID: 9, Email: "will.wirer@example.com", Name: "Will Wirer"}
	launch := domain.ReportBlock{
		Goal: domain.Goal{
			ID: 11, Title: "Launch the Lisbon office", WhyItMatters: "Southern Europe needs a base.",
			Owner: owner, Lifecycle: domain.LifecycleActive, DeliveryDate: day(6, 30),
		},
		Health:           domain.HealthRed,
		PriorHealth:      domain.HealthGreen,
		RunOffGreenWeeks: 4,
		Changes:          []string{"Delivery date slipped 3 weeks"},
		Badges:           []string{domain.BadgeCheckinOverdue},
		Reasons: []domain.ReportReason{
			domain.ReportReasonRed, domain.ReportReasonCheckinOverdue, domain.ReportReasonScheduleConflict,
			domain.ReportReasonTurnedRed, domain.ReportReasonDeliveryLater, domain.ReportReasonDateSlipped,
		},
		PriorDueDates:        []time.Time{day(6, 9)},
		Status:               "Lease talks stalled.",
		PathToGreen:          "Sign the backup lease.",
		PathTargetDate:       day(3, 1),
		PriorPathTargetDates: []time.Time{day(2, 1), day(2, 15)},
		Explanation:          "The fit-out crew is ahead.",
		Milestones: []domain.ReportMilestone{
			{Milestone: domain.Milestone{Name: "Lease signed", TargetDate: day(3, 2), Status: domain.MilestonePlanned}},
			{Milestone: domain.Milestone{Name: "Opening party", TargetDate: day(5, 1), Status: domain.MilestoneRemoved, RemovedReason: "Budget freeze."}},
		},
		Metrics: []domain.ReportMetric{
			{Metric: domain.Metric{Name: "Desks booked", Unit: "desks", Direction: domain.MetricUp, Baseline: 4, Target: 37, TargetDate: day(6, 30)}, Current: 19, Read: true},
			{Metric: domain.Metric{Name: "Local hires", Unit: "people", Direction: domain.MetricUp, Target: 12, TargetDate: day(6, 30)}},
		},
		RolledUp: domain.RolledUpHealth{Health: domain.HealthYellow, Present: true, ActiveChildren: 7, YellowChildren: 2},
		ScheduleConflicts: []domain.ReportScheduleConflict{
			{GoalID: 19, Title: "Staff the Lisbon desk", Owner: planner, Due: day(6, 1)},
			{AsParent: true, GoalID: 20, Title: "Wire the Lisbon network", Owner: wirer, Due: day(7, 15)},
		},
		ContributesTo: []domain.LinkedGoal{{GoalID: 12, Title: "Open in Iberia", Health: domain.HealthRed, InReport: true}},
		Behind:        []domain.LinkedGoal{{GoalID: 13, Title: "Fit out the Lisbon floor", Health: domain.HealthYellow}},
		Fields:        []domain.FieldValue{{Field: domain.Field{Name: "Cost centre", Type: domain.FieldShortText}, Value: "CC-4471"}},
		AsksForHelp:   true,
	}
	iberia := domain.ReportBlock{
		Goal:        domain.Goal{ID: 12, Title: "Open in Iberia", WhyItMatters: "Two markets at once.", Owner: owner, Lifecycle: domain.LifecycleActive},
		Health:      domain.HealthRed,
		AsksForHelp: true, AskAddressee: &helper,
	}
	r := domain.Report{
		Definition:  domain.ReportDefinition{ID: 3, Name: "Iberia MBR", Introduction: "A month of leases and hiring."},
		Baseline:    day(2, 10),
		Previous:    domain.PreviousPublication{ID: 6, PublishedAt: time.Date(2026, 2, 10, 16, 0, 0, 0, time.UTC)},
		ActionItems: []domain.ActionItem{{ID: 1, Text: "Chase the landlord", Owner: chaser, DueDate: day(3, 20)}},
		Narrative: []domain.NarrativeSection{{
			Kind:  domain.HighlightInsight,
			Notes: []string{"Leases move slower in spring."},
			Highlights: []domain.NarrativeHighlight{{
				Highlight: domain.Highlight{Note: "Found a second site.", Owner: writer, Kind: domain.HighlightInsight},
				GoalID:    14, GoalTitle: "Scout Porto", Section: domain.HighlightInsight,
			}},
		}},
		Exceptions: []domain.ReportBlock{launch, iberia},
		Lines: []domain.SelectedGoal{
			{Goal: domain.Goal{ID: 15, Title: "Translate the handbook", Owner: liner}, Health: domain.HealthGreen},
			{Goal: domain.Goal{ID: 16, Title: "Renew the visas", Owner: liner}, Health: domain.HealthGreen},
			{Goal: domain.Goal{ID: 17, Title: "Book the movers", Owner: liner}, Health: domain.HealthGreen},
		},
		MembershipChanges: []domain.MembershipChange{
			{Goal: domain.Goal{ID: 18, Title: "Close the Madrid desk"}, Direction: domain.MembershipLeft, Reason: domain.MembershipRemovedByHand},
		},
		Movement: &domain.ReportMovement{TurnedRed: 1, New: 2},
	}
	at := time.Date(2026, 3, 10, 9, 30, 0, 0, time.UTC)
	return domain.Publication{
		ID: 7, DefinitionID: 3, Name: r.Definition.Name, PublishedBy: publisher, PublishedAt: at,
		Facts: domain.PublicationFacts(r, publisher, at, "Team: Southern Europe"),
	}
}

// CheckShowsEveryReportFact checks that text, what the output named output
// shows of ReportFactParityPublication, shows every fact the publication
// lists, and every fact of each of its blocks, each but those of the kinds
// notShown: its label and its words (reportFactWords). It fails naming each
// kind the fixture lacks, so a new kind of fact is checked once it has one,
// and each fact whose words text lacks.
func CheckShowsEveryReportFact(t testing.TB, output, text string, notShown ...domain.ReportFactKind) {
	t.Helper()
	listed := map[domain.ReportFactKind]bool{}
	var check func(where string, facts []domain.ReportFact)
	check = func(where string, facts []domain.ReportFact) {
		for _, f := range facts {
			listed[f.Kind()] = true
			if ex, ok := f.Value.(domain.ReportExceptionsFact); ok {
				for _, b := range ex.Blocks {
					check(fmt.Sprintf(" of block %d", b.GoalID), b.Facts)
				}
			}
			if slices.Contains(notShown, f.Kind()) {
				continue
			}
			for _, w := range append([]string{f.Label}, reportFactWords(t, f)...) {
				if w != "" && !strings.Contains(text, w) {
					t.Errorf("the %s lacks %q from the %q fact%s:\n%s", output, w, f.Kind(), where, text)
				}
			}
		}
	}
	check("", ReportFactParityPublication().Facts)
	for _, kind := range domain.AllFactKinds {
		if !listed[kind] {
			t.Errorf("the fixture holds no %q fact, so the test can't check the %s shows one", kind, output)
		}
	}
}

// reportFactWords are the words a fact shows in every output beside its
// label: its titles, names, notes, numbers and fixed phrases. Dates are left to
// each output's own tests, as each writes them its own way.
func reportFactWords(t testing.TB, f domain.ReportFact) []string {
	t.Helper()
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	var out []string
	goals := func(gs []domain.LinkedGoal) {
		for _, g := range gs {
			out = append(out, g.Title)
		}
	}
	switch v := f.Value.(type) {
	case domain.ReportBylineFact:
		out = append(out, v.Byline.PublishedBy.Label(), v.Byline.Since.Words)
	case domain.ReportSelectionFact:
		out = append(out, v.Text)
	case domain.ReportHealthCountsFact:
		h := v.Counts
		out = append(out,
			fmt.Sprintf("%d %s", h.Red, domain.HealthRed), fmt.Sprintf("%d %s", h.Yellow, domain.HealthYellow),
			fmt.Sprintf("%d %s", h.Green, domain.HealthGreen), fmt.Sprintf("%d %s", h.CheckinOverdue, domain.BadgeCheckinOverdue))
	case domain.ReportMovementFact:
		out = append(out, v.Line.Since.Words)
		out = append(out, v.Line.Parts...)
	case domain.ReportActionItemsFact:
		for _, item := range v.Items {
			out = append(out, item.Text, item.Owner.Label())
		}
	case domain.ReportIntroductionFact:
		out = append(out, v.Text)
	case domain.ReportNarrativeFact:
		for _, sec := range v.Sections {
			out = append(out, sec.Heading)
			out = append(out, sec.Notes...)
			for _, nh := range sec.Highlights {
				out = append(out, nh.Highlight.Note, nh.Highlight.Owner.Label(), nh.GoalTitle)
			}
		}
	case domain.ReportExceptionsFact:
		// Each block's own facts are checked one by one.
	case domain.ReportOnTrackFact:
		for _, sg := range v.Lines {
			out = append(out, sg.Goal.Title, sg.Goal.Owner.Label())
		}
	case domain.ReportMembershipChangesFact:
		for _, c := range v.Changes {
			out = append(out, c.Goal.Title, c.Direction, c.Reason)
		}
	case domain.ReportBlockHeadFact:
		h := v.Head
		out = append(out, h.Health, h.RunOffGreen, h.WasHealth, h.Title, h.Owner.Label(), h.Lifecycle)
		out = append(out, h.Badges...)
	case domain.ReportChangedFact:
		out = append(out, v.Changes...)
	case domain.ReportScheduleConflictFact:
		for _, g := range v.Groups {
			out = append(out, g.Words)
			for _, c := range g.Conflicts {
				out = append(out, c.Title, c.Owner.Label(), g.DueWords)
			}
		}
	case domain.ReportContributesToFact:
		goals(v.Goals)
	case domain.ReportFieldsFact:
		for _, fv := range v.Values {
			out = append(out, fv.Field.Name, fv.Value)
		}
	case domain.ReportWhyItMattersFact:
		out = append(out, v.Text)
	case domain.ReportStatusFact:
		out = append(out, v.Text)
	case domain.ReportPathToGreenFact:
		p := v.Path
		out = append(out, p.Plan, p.ByWords, domain.SlipsWords(p.Slips))
		if p.Overdue {
			out = append(out, p.OverdueWords)
		}
	case domain.ReportAskForHelpFact:
		if v.Ask.Addressee != nil {
			out = append(out, v.Ask.Addressee.Label())
		} else {
			out = append(out, v.Ask.Words)
		}
	case domain.ReportMilestonesFact:
		for _, m := range v.Marks {
			out = append(out, m.Milestone.Name, m.Mark, m.Milestone.RemovedReason)
		}
	case domain.ReportMetricsFact:
		for _, m := range v.Metrics {
			out = append(out, m.Metric.Name, m.Reading(num), m.AgainstWords, num(m.Metric.Target), m.Metric.Unit)
		}
	case domain.ReportRolledUpFact:
		out = append(out, v.RolledUp.Health, v.RolledUp.Count)
	case domain.ReportSupportedByFact:
		goals(v.Goals)
	case domain.ReportExplanationFact:
		out = append(out, v.Text)
	default:
		t.Errorf("CheckShowsEveryReportFact doesn't know the words of a %q fact", f.Kind())
	}
	return out
}
