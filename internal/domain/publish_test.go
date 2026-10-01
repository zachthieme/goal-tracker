package domain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
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

// A Report Definition's publications are listed newest first, and only its
// own.
func TestListPublicationsPerDefinition(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	mbr := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	wbr := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", RootIDs: []int64{g.ID}})

	first := publish(t, h, boss, mbr, time.Time{})
	h.Clock.Advance(day)
	other := publish(t, h, boss, wbr, time.Time{})
	h.Clock.Advance(day)
	second := publish(t, h, boss, mbr, time.Time{})

	for _, tc := range []struct {
		def  domain.ReportDefinition
		want []int64
	}{
		{mbr, []int64{second.ID, first.ID}},
		{wbr, []int64{other.ID}},
	} {
		pubs, err := h.Service.ListPublications(context.Background(), tc.def.ID)
		if err != nil {
			t.Fatalf("ListPublications: %v", err)
		}
		var got []int64
		for _, p := range pubs {
			got = append(got, p.ID)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s publications %v, want %v", tc.def.Name, got, tc.want)
		}
	}
}

// A Publication freezes each person's Name as it read when published: a later
// change to the Name leaves the snapshot as it was (CONTEXT.md: Name).
func TestPublicationFreezesTheOwnersName(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	ada := h.SignIn("ada.okafor@example.com")
	if err := h.Service.SetName(ctx, ada.ID, "Ada Okafor"); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	if err := h.Service.SetName(ctx, ada.ID, "Ada Mensah"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	owners := publishedOwners(got.Report)
	if len(owners) != 1 || owners[0].Label() != "Ada Okafor" {
		t.Errorf("published Owners %+v, want Ada Okafor as published", owners)
	}
}

// A person published without a Name is frozen as they were shown then, by
// their email's local part, even if they are named later.
func TestPublicationFreezesAnUnnamedOwnerAsShown(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	if err := h.Service.SetName(ctx, sam.ID, "Sam Berg"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	owners := publishedOwners(got.Report)
	if len(owners) != 1 || owners[0].Label() != "sam" || owners[0].LongLabel() != "sam (sam@example.com)" {
		t.Errorf("published Owners %+v, want sam as published", owners)
	}
}

// A snapshot published before Accounts had Names still renders, showing each
// person by email as it always did.
func TestSnapshotFromBeforeNamesShowsEmails(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Lines":[{"Goal":{"ID":1,"Title":"Launch in EU",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","IsAdmin":true,"Departed":false}},"Health":""}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-Names snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	owners := publishedOwners(got.Report)
	if len(owners) != 1 || owners[0].Label() != "boss@example.com" || owners[0].LongLabel() != "boss@example.com" {
		t.Errorf("pre-Names snapshot Owners %+v, want boss@example.com", owners)
	}
}

// publishedOwners is every selected Goal's Owner in a Report, exceptions first.
func publishedOwners(r domain.Report) []domain.Account {
	var out []domain.Account
	for _, b := range r.Exceptions {
		out = append(out, b.Goal.Owner)
	}
	for _, sg := range r.Lines {
		out = append(out, sg.Goal.Owner)
	}
	return out
}

// A Publication freezes its publisher alongside its Owners: renaming the
// publisher later leaves the byline as it read when published, from either
// GetPublication or ListPublications.
func TestPublicationFreezesThePublishersName(t *testing.T) {
	h := testsupport.New(t, "ceo@example.com")
	ctx := context.Background()
	ceo := h.SignInNamed("ceo@example.com", "Dana Whitfield")
	g := h.ActiveGoal(ceo, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(ceo, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(ceo, def)

	if err := h.Service.SetName(ctx, ceo.ID, "Dana Renamed"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	for name, got := range readPublication(t, h, def, pub.ID) {
		if got.PublishedBy.ID != ceo.ID || got.PublishedBy.LongLabel() != "Dana Whitfield (ceo@example.com)" {
			t.Errorf("%s: published by %+v, want Dana Whitfield as published", name, got.PublishedBy)
		}
	}
}

// readPublication reads the publication id of def back both ways a Publication
// is read, keyed by how it was read.
func readPublication(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition, id int64) map[string]domain.Publication {
	t.Helper()
	ctx := context.Background()
	got, err := h.Service.GetPublication(ctx, id)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	out := map[string]domain.Publication{"GetPublication": got}
	pubs, err := h.Service.ListPublications(ctx, def.ID)
	if err != nil {
		t.Fatalf("ListPublications: %v", err)
	}
	for _, p := range pubs {
		if p.ID == id {
			out["ListPublications"] = p
		}
	}
	if _, ok := out["ListPublications"]; !ok {
		t.Fatalf("ListPublications has no publication %d", id)
	}
	return out
}

// A publisher with no Name when publishing is frozen as they were shown then,
// by their email's local part, even if they are named later.
func TestPublicationFreezesAnUnnamedPublisherAsShown(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	if err := h.Service.SetName(context.Background(), boss.ID, "Bo Sterling"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	for name, got := range readPublication(t, h, def, pub.ID) {
		if got.PublishedBy.Label() != "boss" || got.PublishedBy.LongLabel() != "boss (boss@example.com)" {
			t.Errorf("%s: published by %+v, want boss as published", name, got.PublishedBy)
		}
	}
}

// A Publication from before publishers were frozen shows a publisher who
// appears in its snapshot exactly as the snapshot froze them, even after a
// rename.
func TestUnfrozenPublisherShowsAsFrozenInTheSnapshot(t *testing.T) {
	h := testsupport.New(t, "ceo@example.com")
	ceo := h.SignInNamed("ceo@example.com", "Dana Whitfield")
	g := h.ActiveGoal(ceo, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(ceo, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(ceo, def)
	unfreezePublisher(t, h, pub)

	if err := h.Service.SetName(context.Background(), ceo.ID, "Dana Renamed"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	for name, got := range readPublication(t, h, def, pub.ID) {
		if got.PublishedBy.ID != ceo.ID || got.PublishedBy.LongLabel() != "Dana Whitfield (ceo@example.com)" {
			t.Errorf("%s: published by %+v, want Dana Whitfield as the snapshot froze them", name, got.PublishedBy)
		}
	}
}

// unfreezePublisher rewrites pub's stored snapshot as it was before publishers
// were frozen: the Report alone.
func unfreezePublisher(t *testing.T, h *testsupport.Harness, pub domain.Publication) {
	t.Helper()
	var stored string
	if err := h.DB.QueryRow(`SELECT snapshot FROM report_publications WHERE id = ?`, pub.ID).Scan(&stored); err != nil {
		t.Fatalf("read the snapshot: %v", err)
	}
	var snap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stored), &snap); err != nil {
		t.Fatalf("decode the snapshot: %v", err)
	}
	if _, ok := snap["PublishedBy"]; !ok {
		t.Fatalf("snapshot %s has no frozen publisher to remove", stored)
	}
	delete(snap, "PublishedBy")
	before, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("encode the snapshot: %v", err)
	}
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, string(before), pub.ID); err != nil {
		t.Fatalf("write the snapshot: %v", err)
	}
}

// A snapshot published before Accounts had Names shows its publisher by email,
// as it shows its other people, whether or not the publisher appears in it.
func TestSnapshotFromBeforeNamesShowsThePublisherByEmail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owner string
	}{
		{"publisher owns a Goal", "boss@example.com"},
		{"publisher owns nothing", "ada.okafor@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			boss := h.SignInNamed("boss@example.com", "Bo Sterling")
			owner := h.SignInNamed(tc.owner, "Someone Named")
			g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
			def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
			pub := h.PublishReport(boss, def)
			before := fmt.Sprintf(`{"Definition":{"ID":%d,"Name":"MBR"},"Lines":[{"Goal":{"ID":%d,"Title":"Launch in EU",`+
				`"Owner":{"ID":%d,"Email":%q,"IsAdmin":false,"Departed":false}},"Health":""}]}`, def.ID, g.ID, owner.ID, owner.Email)
			if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
				t.Fatalf("write a pre-Names snapshot: %v", err)
			}

			for name, got := range readPublication(t, h, def, pub.ID) {
				if got.PublishedBy.ID != boss.ID || got.PublishedBy.Label() != "boss@example.com" ||
					got.PublishedBy.LongLabel() != "boss@example.com" {
					t.Errorf("%s: published by %+v, want boss@example.com", name, got.PublishedBy)
				}
			}
		})
	}
}

// A Publication from before publishers were frozen, published after Names,
// whose publisher appears nowhere in its snapshot, shows the publisher as
// their Account reads now: there is nothing frozen to show instead.
func TestUnfrozenPublisherNotInTheSnapshotShowsTheirAccount(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Bo Sterling")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	unfreezePublisher(t, h, pub)

	if err := h.Service.SetName(context.Background(), boss.ID, "Bo Renamed"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	for name, got := range readPublication(t, h, def, pub.ID) {
		if got.PublishedBy.ID != boss.ID || got.PublishedBy.LongLabel() != "Bo Renamed (boss@example.com)" {
			t.Errorf("%s: published by %+v, want Bo Renamed from their Account", name, got.PublishedBy)
		}
	}
}
