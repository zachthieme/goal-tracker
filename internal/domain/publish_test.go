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
