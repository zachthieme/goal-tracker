package domain_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Since a rules definition's latest publication, a Goal entered its Report
// when added to Also include (picked by hand) or when it came to meet the
// rules; one left when put in Leave out (removed by hand), when it stopped
// meeting the rules or was taken out of Also include (no longer matches), or
// when it finished before the baseline. A Goal that stayed is no change.
func TestMembershipChangesOfARulesDefinition(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	stays := h.ActiveGoal(boss, "Stays in", "why")
	included := h.ActiveGoal(boss, "Also included since", "why")
	nowMatches := h.ActiveGoal(boss, "Recovered since", "why")
	leftOut := h.ActiveGoal(boss, "Left out since", "why")
	noLongerMatches := h.ActiveGoal(boss, "Red since", "why")
	takenOut := h.ActiveGoal(boss, "Taken out of Also include", "why")
	finished := h.ActiveGoal(boss, "Done before the publication", "why")
	settle(h)
	red := func(g domain.Goal) {
		h.Checkin(boss, g.ID, domain.HealthRed, "Blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 1, 0))
	}
	green := func(g domain.Goal) { h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{}) }
	green(stays)
	red(included)
	red(nowMatches)
	green(leftOut)
	green(noLongerMatches)
	red(takenOut)
	// Done since the default baseline, so the publication reports its finish.
	moveLifecycle(t, h, boss, finished, domain.LifecycleDone)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "MBR",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{namedRule(domain.RuleHealth, domain.RuleIsNot, domain.HealthRed)},
		Include: []int64{takenOut.ID},
	})
	pub := h.PublishReport(boss, def)
	if got, want := append(blockIDs(pub.Report), lineIDs(pub.Report)...), []int64{stays.ID, leftOut.ID, noLongerMatches.ID, takenOut.ID, finished.ID}; !sameSet(got, want) {
		t.Fatalf("published %v, want %v", got, want)
	}
	h.Clock.Advance(day)

	listGoal(t, h, def, "include", included)
	green(nowMatches)
	listGoal(t, h, def, "exclude", leftOut)
	red(noLongerMatches)
	unlistGoal(t, h, def, "include", takenOut)

	got := membershipChanges(t, h, def, time.Time{})
	want := map[int64]string{
		included.ID:        "Added: " + domain.MembershipPickedByHand,
		nowMatches.ID:      "Added: " + domain.MembershipNowMatches,
		leftOut.ID:         "Left: " + domain.MembershipRemovedByHand,
		noLongerMatches.ID: "Left: " + domain.MembershipNoLongerMatches,
		takenOut.ID:        "Left: " + domain.MembershipNoLongerMatches,
		finished.ID:        "Left: " + domain.MembershipFinishedBeforeBaseline,
	}
	assertMembershipChanges(t, got, want)
}

// Since a picked definition's latest publication, a Goal added to its list
// entered the Report, picked by hand, and one dropped from it left, removed by
// hand.
func TestMembershipChangesOfAPickedDefinition(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	stays := h.ActiveGoal(boss, "Stays in", "why")
	dropped := h.ActiveGoal(boss, "Dropped since", "why")
	picked := h.ActiveGoal(boss, "Picked since", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{stays.ID, dropped.ID}})
	h.PublishReport(boss, def)
	h.Clock.Advance(day)

	listGoal(t, h, def, "picked", picked)
	unlistGoal(t, h, def, "picked", dropped)

	got := membershipChanges(t, h, def, time.Time{})
	want := map[int64]string{
		picked.ID:  "Added: " + domain.MembershipPickedByHand,
		dropped.ID: "Left: " + domain.MembershipRemovedByHand,
	}
	assertMembershipChanges(t, got, want)
}

// With no publication, a Report has no membership changes, whatever its
// baseline. With some, they compare against the latest, even when the reader
// picks a baseline of their own.
func TestMembershipChangesCompareAgainstTheLatestPublication(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	stays := h.ActiveGoal(boss, "Stays in", "why")
	pickedBefore := h.ActiveGoal(boss, "Picked before the latest publication", "why")
	picked := h.ActiveGoal(boss, "Picked since", "why")
	settle(h)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{stays.ID}})
	chosen := testsupport.Epoch
	for name, baseline := range map[string]time.Time{"default": {}, "chosen": chosen} {
		if got := membershipChanges(t, h, def, baseline); len(got) != 0 {
			t.Errorf("unpublished, against the %s baseline: membership changes %v, want none", name, got)
		}
	}

	h.PublishReport(boss, def)
	h.Clock.Advance(day)
	listGoal(t, h, def, "picked", pickedBefore)
	h.PublishReport(boss, def)
	h.Clock.Advance(day)
	listGoal(t, h, def, "picked", picked)

	got := membershipChanges(t, h, def, chosen)
	assertMembershipChanges(t, got, map[int64]string{picked.ID: "Added: " + domain.MembershipPickedByHand})
}

// listGoal names g in def's stored list ("include", "exclude" or "picked"), as
// editing the definition would.
func listGoal(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition, list string, g domain.Goal) {
	t.Helper()
	if _, err := h.DB.Exec(`INSERT INTO report_definition_goals (report_definition_id, list, goal_id) VALUES (?, ?, ?)`,
		def.ID, list, g.ID); err != nil {
		t.Fatalf("list goal %d in %s: %v", g.ID, list, err)
	}
}

// unlistGoal takes g out of def's stored list, as editing the definition would.
func unlistGoal(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition, list string, g domain.Goal) {
	t.Helper()
	if _, err := h.DB.Exec(`DELETE FROM report_definition_goals WHERE report_definition_id = ? AND list = ? AND goal_id = ?`,
		def.ID, list, g.ID); err != nil {
		t.Fatalf("unlist goal %d from %s: %v", g.ID, list, err)
	}
}

// membershipChanges drafts def, re-read as stored, against baseline and
// returns its membership changes, each as "Direction: reason" by Goal.
func membershipChanges(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition, baseline time.Time) map[int64]string {
	t.Helper()
	ctx := context.Background()
	def, err := h.Service.GetReportDefinition(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetReportDefinition: %v", err)
	}
	r, err := h.Service.DraftReport(ctx, def, baseline)
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	return byGoal(t, r.MembershipChanges)
}

// byGoal is each membership change as "Direction: reason", by Goal, failing
// the test if a Goal changes twice.
func byGoal(t *testing.T, changes []domain.MembershipChange) map[int64]string {
	t.Helper()
	out := make(map[int64]string, len(changes))
	for _, c := range changes {
		if prev, ok := out[c.Goal.ID]; ok {
			t.Errorf("Goal %d changed twice: %s and %s: %s", c.Goal.ID, prev, c.Direction, c.Reason)
		}
		out[c.Goal.ID] = fmt.Sprintf("%s: %s", c.Direction, c.Reason)
	}
	return out
}

func assertMembershipChanges(t *testing.T, got, want map[int64]string) {
	t.Helper()
	for id, w := range want {
		if got[id] != w {
			t.Errorf("Goal %d: membership change %q, want %q", id, got[id], w)
		}
	}
	for id, g := range got {
		if _, ok := want[id]; !ok {
			t.Errorf("Goal %d: unexpected membership change %q", id, g)
		}
	}
}
