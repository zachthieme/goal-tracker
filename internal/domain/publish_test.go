package domain_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Publishing freezes the Report as it reads now, and later edits to its Goals —
// a new Check-in, a revised So What, a slipped date — never change the
// published snapshot (CONTEXT.md: Report Definition).
func TestPublishedReportNeverChanges(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	settle(h)
	h.Checkin(boss, g.ID, domain.HealthYellow, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})

	pub, err := h.Service.PublishReport(ctx, boss.ID, def.ID, time.Time{})
	if err != nil {
		t.Fatalf("PublishReport: %v", err)
	}
	if !pub.PublishedAt.Equal(h.Clock.Now()) || pub.PublishedBy.ID != boss.ID {
		t.Errorf("published at %v by %d, want now by %d", pub.PublishedAt, pub.PublishedBy.ID, boss.ID)
	}
	if len(pub.Report.Exceptions) != 1 || pub.Report.Exceptions[0].Health != domain.HealthYellow ||
		pub.Report.Exceptions[0].Status != "Vendor is late." {
		t.Fatalf("snapshot exceptions %+v, want the Yellow Goal's block", pub.Report.Exceptions)
	}

	h.Clock.Advance(day)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is gone.", "Find a new vendor.", h.Clock.Now().AddDate(0, 0, 14))
	if _, err := h.Service.EditSoWhat(ctx, g.ID, "EU shoppers can't pay in euros.", boss.ID); err != nil {
		t.Fatalf("EditSoWhat: %v", err)
	}
	slipDelivery(h, boss, g, g.DeliveryDate.AddDate(0, 1, 0))

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if !reflect.DeepEqual(got, pub) {
		t.Errorf("published snapshot changed after the Goal was edited\n got %+v\nwant %+v", got, pub)
	}
	b := got.Report.Exceptions[0]
	if b.Health != domain.HealthYellow || b.Goal.SoWhat != "Expand the market." || len(b.PriorDueDates) != 0 {
		t.Errorf("snapshot reads Health %q, So What %q, prior dates %v; want the Goal as published",
			b.Health, b.Goal.SoWhat, b.PriorDueDates)
	}
}

// The next publication of a Report Definition reads its changes against the
// previous publication, not 30 days ago; so does the draft leading up to it.
// A baseline the reader picks still overrides it (CONTEXT.md: Report
// Definition).
func TestNextPublicationComparesAgainstThePrevious(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	root := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	settle(h)
	early := h.CreateGoal(boss, "Launch in EU", "Expand the market.")
	h.RequestLink(boss, early, root, "")
	h.Checkin(boss, root.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{root.ID}, Depth: 1})

	first := publish(t, h, boss, def, time.Time{})
	if first.Report.Previous.ID != 0 {
		t.Errorf("first publication compares against publication %d, want none", first.Report.Previous.ID)
	}
	if got, want := blockIDs(first.Report), []int64{early.ID}; !sameSet(got, want) {
		t.Errorf("first publication's exceptions %v, want %v (created since 30 days ago)", got, want)
	}

	h.Clock.Advance(7 * day)
	late := h.CreateGoal(boss, "Cut churn", "Keep customers.")
	h.RequestLink(boss, late, root, "")
	h.Checkin(boss, root.ID, domain.HealthGreen, "On track.", "", time.Time{})

	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	second := publish(t, h, boss, def, time.Time{})
	for name, r := range map[string]domain.Report{"draft": draft, "second publication": second.Report} {
		if r.Previous.ID != first.ID || !r.Previous.PublishedAt.Equal(first.PublishedAt) {
			t.Errorf("%s compares against %+v, want the first publication (%d at %v)", name, r.Previous, first.ID, first.PublishedAt)
		}
		if want := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC); !r.Baseline.Equal(want) {
			t.Errorf("%s baseline %v, want the first publication's date %v", name, r.Baseline, want)
		}
		if got, want := blockIDs(r), []int64{late.ID}; !sameSet(got, want) {
			t.Errorf("%s exceptions %v, want %v (created since the first publication)", name, got, want)
		}
		if got, want := lineIDs(r), []int64{root.ID, early.ID}; !sameSet(got, want) {
			t.Errorf("%s one-line Goals %v, want %v", name, got, want)
		}
	}

	// A baseline the reader picks, before both Goals were created, overrides
	// the previous publication.
	chosen := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	third := publish(t, h, boss, def, chosen)
	if third.Report.Previous.ID != 0 || !third.Report.Baseline.Equal(chosen) {
		t.Errorf("with a chosen baseline, compares against %+v from %v; want no publication, from %v",
			third.Report.Previous, third.Report.Baseline, chosen)
	}
	if got, want := blockIDs(third.Report), []int64{early.ID, late.ID}; !sameSet(got, want) {
		t.Errorf("with a chosen baseline, exceptions %v, want %v", got, want)
	}
}

// publish publishes def against baseline as actor, failing the test on error.
func publish(t *testing.T, h *testsupport.Harness, actor domain.Account, def domain.ReportDefinition, baseline time.Time) domain.Publication {
	t.Helper()
	p, err := h.Service.PublishReport(context.Background(), actor.ID, def.ID, baseline)
	if err != nil {
		t.Fatalf("PublishReport: %v", err)
	}
	return p
}
