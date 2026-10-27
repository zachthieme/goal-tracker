package web_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Reports list reads no Goal's Check-ins, Date Slips or Milestones beyond
// what drafting each Report Definition reads: its count of changes reuses
// what each draft judged (#633). So it runs ListCheckinsForGoals,
// ListDateSlipsForGoals and ListMilestonesForGoals exactly as often as
// DraftReport does over every Definition, and each Definition more adds no
// more to the list than to those drafts, but for reportsListPerDefinition.
func TestReportsListReadsNoGoalHistoryBeyondItsDrafts(t *testing.T) {
	t.Parallel()
	// reportsListPerDefinition is what the list reads for each Definition
	// that drafting it doesn't: loading the Definition (ListReportRules,
	// ListReportRuleValues, ListReportDefinitionGoals and
	// ListReportDefinitionFields) and its last publication
	// (GetReportPublication).
	const reportsListPerDefinition = 5
	history := []string{"ListCheckinsForGoals", "ListDateSlipsForGoals", "ListMilestonesForGoals"}

	type counts struct{ list, drafts map[string]int }
	countFor := func(defs int) counts {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedQueryCountFixture(t, h, 10)
		admin := h.SignIn(queryCountAdmin)
		all := []domain.ReportDefinition{fx.report}
		for i := 1; i < defs; i++ {
			def := h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{
				Name:     fmt.Sprintf("Weekly copy %d", i),
				Mode:     fx.report.Mode,
				Rules:    fx.report.Rules,
				Include:  fx.report.Include,
				FieldIDs: fx.report.FieldIDs,
			})
			h.PublishReport(admin, def)
			all = append(all, def)
		}
		ts, svc, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		queries.take()
		getBody(t, client, ts.URL+"/reports")
		out := counts{list: countQueryNames(queries.take())}
		for _, def := range all {
			if _, err := svc.DraftReport(context.Background(), def, time.Time{}); err != nil {
				t.Fatalf("DraftReport %q: %v", def.Name, err)
			}
		}
		out.drafts = countQueryNames(queries.take())
		for _, name := range history {
			if out.list[name] != out.drafts[name] {
				t.Errorf("the Reports list over %d Definitions ran %s %d times; want %d, as often as drafting them", defs, name, out.list[name], out.drafts[name])
			}
		}
		return out
	}
	total := func(ran map[string]int) int {
		n := 0
		for _, times := range ran {
			n += times
		}
		return n
	}

	one, three := countFor(1), countFor(3)
	listGrew := total(three.list) - total(one.list)
	draftsGrew := total(three.drafts) - total(one.drafts)
	if want := draftsGrew + 2*reportsListPerDefinition; listGrew > want {
		t.Errorf("two more Definitions added %d statements to the Reports list and %d to their drafts; want at most %d", listGrew, draftsGrew, want)
	}
}
