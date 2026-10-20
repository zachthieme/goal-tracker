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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	settle(h)
	h.Checkin(boss, g.ID, domain.HealthYellow, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

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
	// Two weeks out from a day later moves the back-to-Green date later.
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Vendor is gone.", "Find a new vendor.", h.Clock.Now().AddDate(0, 0, 14), "A new vendor takes longer.")
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	program := h.CreateDimension(boss, "Program", "Growth")
	growth := program.Values[0]
	root := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	h.AssignGoalValue(root, growth)
	settle(h)
	early := h.CreateGoal(boss, "Launch in EU", "Expand the market.")
	h.AssignGoalValue(early, growth)
	h.Checkin(boss, root.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
		dimensionRule(program, domain.RuleIs, growth),
	}})

	first := publish(t, h, boss, def, time.Time{})
	if first.Report.Previous.ID != 0 {
		t.Errorf("first publication compares against publication %d, want none", first.Report.Previous.ID)
	}
	if got, want := blockIDs(first.Report), []int64{early.ID}; !sameSet(got, want) {
		t.Errorf("first publication's exceptions %v, want %v (created since 30 days ago)", got, want)
	}

	h.Clock.Advance(7 * day)
	late := h.CreateGoal(boss, "Cut churn", "Keep customers.")
	h.AssignGoalValue(late, growth)
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	mbr := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	wbr := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	ada := h.SignIn("ada.okafor@example.com")
	if err := h.Service.SetName(ctx, ada.ID, "Ada Okafor"); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "ceo@example.com")
	ctx := context.Background()
	ceo := h.SignInNamed("ceo@example.com", "Dana Whitfield")
	g := h.ActiveGoal(ceo, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(ceo, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "ceo@example.com")
	ceo := h.SignInNamed("ceo@example.com", "Dana Whitfield")
	g := h.ActiveGoal(ceo, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(ceo, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

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
			def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Bo Sterling")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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

// A publication freezes the Field names, units and values it showed beside
// each Goal: editing the value, renaming the Field, changing its unit or
// retiring it later doesn't change the published Report (ticket #78).
func TestPublicationFreezesChosenFields(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.SetGoalField(boss, g, budget, "120")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: []int64{budget.ID}})
	pub := h.PublishReport(boss, def)

	h.Clock.Advance(day)
	h.SetGoalField(boss, g, budget, "300")
	// The tool has no Field rename yet; rename it and change its unit in place,
	// as one would.
	if _, err := h.DB.ExecContext(ctx, `UPDATE fields SET name = 'Spend', unit = '€' WHERE id = ?`, budget.ID); err != nil {
		t.Fatalf("rename field: %v", err)
	}
	if err := h.Service.RetireField(ctx, boss.ID, budget.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	fields := got.Report.Exceptions[0].Fields
	if len(fields) != 1 || fields[0].Field.Name != "Budget" || fields[0].Field.Unit != "$" || fields[0].Value != "120" || fields[0].Field.Retired {
		t.Errorf("published Fields %+v, want Budget 120 $ as published", fields)
	}
}

// A publication freezes its membership changes: editing the definition's Also
// include and Leave out afterwards leaves them as published.
func TestPublicationFreezesMembershipChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(boss, "Stays in", "why")
	included := h.ActiveGoal(sam, "Also included", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
		ownerRule(domain.RuleIs, boss),
	}})
	h.PublishReport(boss, def)
	h.Clock.Advance(day)
	listGoal(t, h, def, "include", included)
	pub := h.PublishReport(boss, def)
	if got, want := byGoal(t, pub.Report.MembershipChanges), map[int64]string{included.ID: "Added: " + domain.MembershipPickedByHand}; !reflect.DeepEqual(got, want) {
		t.Fatalf("published membership changes %v, want %v", got, want)
	}

	unlistGoal(t, h, def, "include", included)
	listGoal(t, h, def, "exclude", included)

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if !reflect.DeepEqual(got.Report.MembershipChanges, pub.Report.MembershipChanges) {
		t.Errorf("membership changes after editing the definition %+v, want as published %+v",
			got.Report.MembershipChanges, pub.Report.MembershipChanges)
	}
}

// A snapshot frozen when a narrative section held at most one of the author's
// notes, as Text, keeps it: it reads as that section's only note.
func TestSnapshotFromBeforeSeveralNotesKeepsItsNote(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Narrative":[` +
		`{"Kind":"Insight","Text":"Pricing drives churn.","Highlights":null},` +
		`{"Kind":"Miss","Text":"","Highlights":[{"Highlight":{"ID":1,"Kind":"Miss","Note":"Lost two big accounts."},"GoalID":1,"GoalTitle":"Launch in EU","Section":"Miss"}]}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-several-notes snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	n := got.Report.Narrative
	if len(n) != 2 || n[0].Kind != domain.HighlightInsight || !slices.Equal(n[0].Notes, []string{"Pricing drives churn."}) {
		t.Errorf("pre-several-notes snapshot narrative %+v, want Insights with its one note", n)
	}
	if len(n) == 2 && (len(n[1].Notes) != 0 || len(n[1].Highlights) != 1) {
		t.Errorf("pre-several-notes Misses %+v, want no notes and its Highlight", n[1])
	}
}

// A publication freezes what its blocks say changed, what moved since its
// baseline, and the order of its exceptions: a later Check-in that turns
// another Goal Red changes the next draft, not the publication.
func TestPublicationFreezesWhatChangedAndItsOrder(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	standing := h.ActiveGoal(boss, "Stays Red", "why")
	turned := h.ActiveGoal(boss, "Turns Red", "why")
	later := h.ActiveGoal(boss, "Turns Red later", "why")
	checkinHealth(h, boss, standing, domain.HealthRed)
	checkinHealth(h, boss, turned, domain.HealthGreen)
	checkinHealth(h, boss, later, domain.HealthGreen)
	settle(h)
	checkinHealth(h, boss, standing, domain.HealthRed)
	checkinHealth(h, boss, turned, domain.HealthRed)
	checkinHealth(h, boss, later, domain.HealthGreen)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{standing.ID, turned.ID, later.ID}})
	pub := h.PublishReport(boss, def)
	if got, want := blockIDs(pub.Report), []int64{turned.ID, standing.ID}; !slices.Equal(got, want) {
		t.Fatalf("published exceptions %v, want %v", got, want)
	}

	h.Clock.Advance(day)
	checkinHealth(h, boss, later, domain.HealthRed)
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if got, want := blockIDs(draft), []int64{later.ID, standing.ID, turned.ID}; !slices.Equal(got, want) {
		t.Errorf("next draft's exceptions %v, want %v", got, want)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if ids, want := blockIDs(got.Report), []int64{turned.ID, standing.ID}; !slices.Equal(ids, want) {
		t.Errorf("publication's exceptions %v after the Check-in, want as published %v", ids, want)
	}
	if b := got.Report.Exceptions[0]; !slices.Equal(b.Changes, []string{"Turned Red"}) {
		t.Errorf("publication's first block changes %q, want [Turned Red]", b.Changes)
	}
	if b := got.Report.Exceptions[1]; len(b.Changes) != 0 {
		t.Errorf("publication's standing block changes %q, want none", b.Changes)
	}
	if got.Report.Movement == nil || *got.Report.Movement != (domain.ReportMovement{TurnedRed: 1}) {
		t.Errorf("publication's movement %+v, want 1 turned Red", got.Report.Movement)
	}
}

// A snapshot frozen before Reports said what changed on each block, or
// counted what moved, reads with neither.
func TestSnapshotFromBeforeChangesHasNone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[` +
		`{"Goal":{"ID":1,"Title":"Launch in EU"},"Health":"Red","PriorHealth":"Green","Badges":null}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-changes snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if len(got.Report.Exceptions) != 1 || len(got.Report.Exceptions[0].Changes) != 0 {
		t.Errorf("pre-changes snapshot exceptions %+v, want one block saying no changes", got.Report.Exceptions)
	}
	if got.Report.Movement != nil {
		t.Errorf("pre-changes snapshot movement %+v, want none", got.Report.Movement)
	}
}

// A publication freezes how its Report selected, in the same plain words as
// the "how this Report selects" summary, as it read when published: editing
// the definition's rules changes the next publication's selection, not this
// one's.
func TestPublicationFreezesItsSelection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignInNamed("boss@example.com", "Dana Okafor")
	h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{ownerRule(domain.RuleIs, boss), namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleActive)},
	})
	pub := h.PublishReport(boss, def)
	if got, want := pub.Scope, "Owner: Dana Okafor · Active"; got != want {
		t.Errorf("published selection %q, want %q", got, want)
	}
	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if want := "Owner: Dana Okafor · Active"; got.Scope != want {
		t.Errorf("reloaded selection %q, want %q", got.Scope, want)
	}

	h.Clock.Advance(day)
	updated, err := h.Service.UpdateReportDefinition(ctx, boss.ID, def.ID, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleOnHold)},
	})
	if err != nil {
		t.Fatalf("UpdateReportDefinition: %v", err)
	}
	got, err = h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if want := "Owner: Dana Okafor · Active"; got.Scope != want {
		t.Errorf("selection after editing the rules %q, want as published %q", got.Scope, want)
	}
	if next, want := h.PublishReport(boss, updated), "On Hold"; next.Scope != want {
		t.Errorf("next publication's selection %q, want %q", next.Scope, want)
	}
}

// A picked definition's publication says how many Goals were picked by hand.
func TestPickedPublicationFreezesItsSelection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	one := h.ActiveGoal(boss, "One", "why")
	two := h.ActiveGoal(boss, "Two", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{one.ID, two.ID}})
	if got, want := h.PublishReport(boss, def).Scope, "2 Goals picked by hand"; got != want {
		t.Errorf("published selection %q, want %q", got, want)
	}
}

// A snapshot frozen before selections were has no selection.
func TestSnapshotFromBeforeSelectionsHasNone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = json_remove(snapshot, '$.Scope') WHERE id = ?`, pub.ID); err != nil {
		t.Fatalf("write a pre-selection snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if got.Scope != "" {
		t.Errorf("pre-selection snapshot's selection %q, want none", got.Scope)
	}
	if got.Report.Definition.Name != "MBR" {
		t.Errorf("pre-selection snapshot's Report %q, want it kept", got.Report.Definition.Name)
	}
}

// A publication freezes what its blocks count and name behind their Rolled-up
// Health and what they contribute to: a later Check-in that turns a Red child
// Green changes the next draft, not the publication (#246).
func TestPublicationFreezesItsLinkedGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	s := h.LinkedGoalsReport(boss)
	pub := h.PublishReport(boss, s.Definition)
	h.Clock.Advance(day)
	checkinHealth(h, boss, s.Beta, domain.HealthGreen)

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	b := linkedBlock(t, got.Report, s.Referral)
	if want := []domain.LinkedGoal{
		linked(s.Alpha, domain.HealthRed, true),
		linked(s.Beta, domain.HealthRed, false),
		linked(s.Gamma, domain.HealthYellow, false),
		linked(s.Epsilon, domain.HealthGreen, true),
	}; !slices.Equal(b.Behind, want) {
		t.Errorf("published Behind = %+v, want %+v as it read when published", b.Behind, want)
	}
	if want := []domain.LinkedGoal{linked(s.Growth, domain.HealthRed, true)}; !slices.Equal(b.ContributesTo, want) {
		t.Errorf("published ContributesTo = %+v, want %+v", b.ContributesTo, want)
	}
	if b.RolledUp.RedChildren != 2 || b.RolledUp.YellowChildren != 1 {
		t.Errorf("published RolledUp = %+v, want 2 Red and 1 Yellow as when published", b.RolledUp)
	}

	draft, err := h.Service.DraftReport(ctx, s.Definition, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if ru := linkedBlock(t, draft, s.Referral).RolledUp; ru.RedChildren != 1 {
		t.Errorf("next draft RolledUp = %+v, want 1 Red now Beta payouts is Green", ru)
	}
}

// A snapshot frozen before Reports counted Red and Yellow children or named
// linked Goals still loads: its blocks name none, and count only their Stale
// children (#246).
func TestSnapshotFromBeforeLinkedGoalsCountsOnlyCheckinOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	s := h.LinkedGoalsReport(boss)
	pub := h.PublishReport(boss, s.Definition)
	h.FreezeBeforeLinkedGoals(pub, s.Referral)

	got, err := h.Service.GetPublication(context.Background(), pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	for _, b := range got.Report.Exceptions {
		if b.Behind != nil || b.ContributesTo != nil {
			t.Errorf("%q names Behind %+v and ContributesTo %+v, want none", b.Goal.Title, b.Behind, b.ContributesTo)
		}
	}
	if got := linkedBlock(t, got.Report, s.Referral).RolledUp.CountLine(); got != "1 of 3 Stale" {
		t.Errorf("CountLine() = %q, want %q", got, "1 of 3 Stale")
	}
}

// A publication freezes an exception block's Ask for help as it stood: the
// Owner unmarking it and the addressee being renamed later leave the snapshot
// asking of the old Name, while a fresh draft no longer asks.
func TestPublicationFreezesAnAskForHelp(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	mia := h.SignInNamed("mia@example.com", "Mia Chen")
	h.SetManager(sam, mia)
	g := h.ActiveGoal(sam, "Launch in EU", "Expand the market.")
	later := h.Clock.Now().AddDate(0, 1, 0)
	h.AskForHelp(sam, g.ID, domain.HealthRed, "Blocked on legal.", "Need counsel budget.", later)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	h.Checkin(sam, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", later)
	if err := h.Service.SetName(ctx, mia.ID, "Mia Renamed"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if len(got.Report.Exceptions) != 1 {
		t.Fatalf("published %d exception blocks, want 1", len(got.Report.Exceptions))
	}
	if b := got.Report.Exceptions[0]; !b.AsksForHelp || b.AskAddressee == nil || b.AskAddressee.Label() != "Mia Chen" {
		t.Errorf("published block AsksForHelp %v of %+v, want an ask of Mia Chen as published", b.AsksForHelp, b.AskAddressee)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(draft.Exceptions) != 1 || draft.Exceptions[0].AsksForHelp || draft.Exceptions[0].AskAddressee != nil {
		t.Errorf("a fresh draft's blocks %+v, want one with no ask", draft.Exceptions)
	}
}

// A publication keeps the timezone each block's Goal was judged in when it was
// published, so clearing the Owner's Timezone later doesn't move its marks
// (CONTEXT.md: Timezone), while a fresh draft reads the Owner as they are now.
func TestPublicationFreezesTheTimezoneItsGoalWasJudgedIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	g := h.ActiveGoal(kenji, "Ship search", "People can't find things.")
	h.Checkin(kenji, g.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	h.SetTimezone(kenji, "")

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if len(got.Report.Exceptions) != 1 || got.Report.Exceptions[0].Timezone != "Asia/Tokyo" {
		t.Errorf("published blocks %+v, want one with Timezone Asia/Tokyo as published", got.Report.Exceptions)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(draft.Exceptions) != 1 || draft.Exceptions[0].Timezone != "UTC" {
		t.Errorf("a fresh draft's blocks %+v, want one with Timezone UTC, the company's", draft.Exceptions)
	}
}

// A publication freezes each block's Run off Green: two weeks and another Red
// Check-in later, the publication still reads the count it was published
// with, while a fresh draft counts on.
func TestPublicationFreezesTheRunOffGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	checkinHealth(h, boss, g, domain.HealthRed)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	if len(pub.Report.Exceptions) != 1 || pub.Report.Exceptions[0].RunOffGreenWeeks != 1 {
		t.Fatalf("published blocks %+v, want one 1 week off Green", pub.Report.Exceptions)
	}

	h.Clock.Advance(14 * day)
	checkinHealth(h, boss, g, domain.HealthRed)

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if len(got.Report.Exceptions) != 1 || got.Report.Exceptions[0].RunOffGreenWeeks != 1 {
		t.Errorf("publication's blocks %+v, want one still 1 week off Green, as published", got.Report.Exceptions)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(draft.Exceptions) != 1 || draft.Exceptions[0].RunOffGreenWeeks != 3 {
		t.Errorf("a fresh draft's blocks %+v, want one 3 weeks off Green", draft.Exceptions)
	}
}

// A snapshot frozen before Reports showed the Run off Green loads its blocks
// with none.
func TestSnapshotFromBeforeTheRunOffGreenHasNone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	checkinHealth(h, boss, g, domain.HealthRed)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[` +
		`{"Goal":{"ID":1,"Title":"Launch in EU"},"Health":"Red","Badges":null,"PriorDueDates":null}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-run-off-green snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if b := got.Report.Exceptions; len(b) != 1 || b[0].Health != domain.HealthRed || b[0].RunOffGreenWeeks != 0 {
		t.Errorf("pre-run-off-green blocks %+v, want one Red block with no Run off Green", b)
	}
}

// A publication freezes the dates each block's back-to-Green date slipped
// from: a later Check-in slipping it again changes a fresh draft, not the
// publication.
func TestPublicationFreezesTheBackToGreenDatesSlippedFrom(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", date(2026, 3, 6))
	h.Clock.Advance(day)
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", date(2026, 3, 13), "Counsel starts later.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	published := []time.Time{date(2026, 3, 6)}
	if b := pub.Report.Exceptions; len(b) != 1 || !slices.EqualFunc(b[0].PriorPathTargetDates, published, time.Time.Equal) {
		t.Fatalf("published blocks %+v, want one slipped from %v", b, published)
	}

	h.Clock.Advance(day)
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", date(2026, 4, 3), "Counsel quit.")

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if b := got.Report.Exceptions; len(b) != 1 || !slices.EqualFunc(b[0].PriorPathTargetDates, published, time.Time.Equal) {
		t.Errorf("publication's blocks %+v, want one still slipped from %v, as published", b, published)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if want := []time.Time{date(2026, 3, 6), date(2026, 3, 13)}; len(draft.Exceptions) != 1 || !slices.EqualFunc(draft.Exceptions[0].PriorPathTargetDates, want, time.Time.Equal) {
		t.Errorf("a fresh draft's blocks %+v, want one slipped from %v", draft.Exceptions, want)
	}
}

// A snapshot frozen before Reports showed back-to-Green slips loads its blocks
// with none.
func TestSnapshotFromBeforeBackToGreenSlipsHasNone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", date(2026, 3, 6))
	h.Clock.Advance(day)
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", date(2026, 3, 13), "Counsel starts later.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[` +
		`{"Goal":{"ID":1,"Title":"Launch in EU"},"Health":"Red","Badges":null,"PriorDueDates":null,` +
		`"PathToGreen":"Hire counsel.","PathTargetDate":"2026-03-13T00:00:00Z"}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-back-to-Green-slips snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if b := got.Report.Exceptions; len(b) != 1 || !b[0].PathTargetDate.Equal(date(2026, 3, 13)) || len(b[0].PriorPathTargetDates) != 0 {
		t.Errorf("pre-back-to-Green-slips blocks %+v, want one back to Green by 2026-03-13 with no prior dates", b)
	}
}
