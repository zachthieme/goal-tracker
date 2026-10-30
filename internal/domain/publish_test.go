package domain_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Publishing freezes the Report's facts as they read now, and later edits to
// its Goals — a new title, a new Check-in, a revised Why it matters, a slipped
// date, a new Name for its Owner — never change the publication (CONTEXT.md:
// Report Definition).
func TestPublishedReportNeverChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada@example.com", "Ada Okafor")
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	settle(h)
	h.Checkin(ada, g.ID, domain.HealthYellow, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	pub, err := h.Service.PublishReport(ctx, boss.ID, def.ID, time.Time{})
	if err != nil {
		t.Fatalf("PublishReport: %v", err)
	}
	if !pub.PublishedAt.Equal(h.Clock.Now()) || pub.PublishedBy.ID != boss.ID {
		t.Errorf("published at %v by %d, want now by %d", pub.PublishedAt, pub.PublishedBy.ID, boss.ID)
	}
	block := publishedBlock(t, h, pub, g.ID)
	if head := factOfKind(t, block, domain.ReportFactBlockHead).Value.(domain.ReportBlockHeadFact).Head; head.Health != domain.HealthYellow || head.Title != "Launch in EU" ||
		head.Owner.Label() != "Ada Okafor" || factOfKind(t, block, domain.ReportFactStatus).Value.(domain.ReportStatusFact).Text != "Vendor is late." {
		t.Fatalf("published block %v, want the Yellow Goal's", factOutline(block))
	}

	h.Clock.Advance(day)
	if _, err := h.Service.EditTitle(ctx, g.ID, "Launch in the EU", ada.ID); err != nil {
		t.Fatalf("EditTitle: %v", err)
	}
	// Two weeks out from a day later moves the back-to-Green date later.
	h.CheckinMovingPath(ada, g.ID, domain.HealthRed, "Vendor is gone.", "Find a new vendor.", h.Clock.Now().AddDate(0, 0, 14), "A new vendor takes longer.")
	if _, err := h.Service.EditWhyItMatters(ctx, g.ID, "EU shoppers can't pay in euros.", ada.ID); err != nil {
		t.Fatalf("EditWhyItMatters: %v", err)
	}
	slipDelivery(h, ada, g, g.DeliveryDate.AddDate(0, 1, 0))
	if err := h.Service.SetName(ctx, ada.ID, "Ada Mensah"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if !reflect.DeepEqual(got, pub) {
		t.Errorf("publication changed after the Goal was edited\n got %+v\nwant %+v", got, pub)
	}
	block = publishedBlock(t, h, got, g.ID)
	head := factOfKind(t, block, domain.ReportFactBlockHead).Value.(domain.ReportBlockHeadFact).Head
	if head.Health != domain.HealthYellow || head.Title != "Launch in EU" || head.Owner.Label() != "Ada Okafor" || len(head.PriorDue) != 0 ||
		factOfKind(t, block, domain.ReportFactWhyItMatters).Value.(domain.ReportWhyItMattersFact).Text != "Expand the market." {
		t.Errorf("publication reads %+v, Why it matters %q; want the Goal as published",
			head, factOfKind(t, block, domain.ReportFactWhyItMatters).Value.(domain.ReportWhyItMattersFact).Text)
	}
}

// publishedBlock is the facts of pub's exception block of the Goal goalID,
// failing the test when it has none.
func publishedBlock(t *testing.T, h *testsupport.Harness, pub domain.Publication, goalID int64) []domain.ReportFact {
	t.Helper()
	for _, b := range h.PublicationFact(pub, domain.ReportFactExceptions).Value.(domain.ReportExceptionsFact).Blocks {
		if b.GoalID == goalID {
			return b.Facts
		}
	}
	t.Fatalf("publication %d has no exception block of Goal %d", pub.ID, goalID)
	return nil
}

// publishedBlockIDs are the Goals of pub's exception blocks, in order.
func publishedBlockIDs(pub domain.Publication) []int64 {
	var ids []int64
	for _, f := range pub.Facts {
		exceptions, _ := f.Value.(domain.ReportExceptionsFact)
		for _, b := range exceptions.Blocks {
			ids = append(ids, b.GoalID)
		}
	}
	return ids
}

// publishedLineIDs are pub's one-line Goals, On track, in order.
func publishedLineIDs(pub domain.Publication) []int64 {
	var ids []int64
	for _, f := range pub.Facts {
		onTrack, _ := f.Value.(domain.ReportOnTrackFact)
		for _, sg := range onTrack.Lines {
			ids = append(ids, sg.Goal.ID)
		}
	}
	return ids
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
	if since := h.PublicationFact(first, domain.ReportFactByline).Value.(domain.ReportBylineFact).Byline.Since; since.Words != "" {
		t.Errorf("first publication compares against %+v, want no publication", since)
	}
	if got, want := publishedBlockIDs(first), []int64{early.ID}; !sameSet(got, want) {
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
	if draft.Previous.ID != first.ID || !draft.Previous.PublishedAt.Equal(first.PublishedAt) {
		t.Errorf("draft compares against %+v, want the first publication (%d at %v)", draft.Previous, first.ID, first.PublishedAt)
	}
	if want := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC); !draft.Baseline.Equal(want) {
		t.Errorf("draft baseline %v, want the first publication's date %v", draft.Baseline, want)
	}
	if since := h.PublicationFact(second, domain.ReportFactByline).Value.(domain.ReportBylineFact).Byline.Since; since.Words != domain.ReportPreviousPublication || !since.At.Equal(first.PublishedAt) {
		t.Errorf("second publication compares against %+v, want the first publication, at %v", since, first.PublishedAt)
	}
	for name, ids := range map[string][2][]int64{
		"draft":              {blockIDs(draft), lineIDs(draft)},
		"second publication": {publishedBlockIDs(second), publishedLineIDs(second)},
	} {
		if got, want := ids[0], []int64{late.ID}; !sameSet(got, want) {
			t.Errorf("%s exceptions %v, want %v (created since the first publication)", name, got, want)
		}
		if got, want := ids[1], []int64{root.ID, early.ID}; !sameSet(got, want) {
			t.Errorf("%s one-line Goals %v, want %v", name, got, want)
		}
	}

	// A baseline the reader picks, before both Goals were created, overrides
	// the previous publication.
	chosen := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	third := publish(t, h, boss, def, chosen)
	if since := h.PublicationFact(third, domain.ReportFactByline).Value.(domain.ReportBylineFact).Byline.Since; since.Words != "" || !since.At.Equal(chosen) {
		t.Errorf("with a chosen baseline, compares against %+v; want no publication, from %v", since, chosen)
	}
	if got, want := publishedBlockIDs(third), []int64{early.ID, late.ID}; !sameSet(got, want) {
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
	owners := publishedOwners(t, h, got)
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
	owners := publishedOwners(t, h, got)
	if len(owners) != 1 || owners[0].Label() != "sam" || owners[0].LongLabel() != "sam (sam@example.com)" {
		t.Errorf("published Owners %+v, want sam as published", owners)
	}
}

// publishedOwners is every selected Goal's Owner in a publication, exceptions
// first.
func publishedOwners(t *testing.T, h *testsupport.Harness, pub domain.Publication) []domain.Account {
	t.Helper()
	var out []domain.Account
	for _, id := range publishedBlockIDs(pub) {
		out = append(out, factOfKind(t, publishedBlock(t, h, pub, id), domain.ReportFactBlockHead).Value.(domain.ReportBlockHeadFact).Head.Owner)
	}
	for _, f := range pub.Facts {
		onTrack, _ := f.Value.(domain.ReportOnTrackFact)
		for _, sg := range onTrack.Lines {
			out = append(out, sg.Goal.Owner)
		}
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
	fields := factOfKind(t, publishedBlock(t, h, got, g.ID), domain.ReportFactFields).Value.(domain.ReportFieldsFact).Values
	if len(fields) != 1 || fields[0].Field.Name != "Budget" || fields[0].Field.Unit != "$" || fields[0].Value != "120" {
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
	changes := h.PublicationFact(pub, domain.ReportFactMembershipChanges).Value.(domain.ReportMembershipChangesFact).Changes
	if got, want := byGoal(t, changes), map[int64]string{included.ID: "Added: " + domain.MembershipPickedByHand}; !reflect.DeepEqual(got, want) {
		t.Fatalf("published membership changes %v, want %v", got, want)
	}

	unlistGoal(t, h, def, "include", included)
	listGoal(t, h, def, "exclude", included)

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if after := h.PublicationFact(got, domain.ReportFactMembershipChanges).Value.(domain.ReportMembershipChangesFact).Changes; !reflect.DeepEqual(after, changes) {
		t.Errorf("membership changes after editing the definition %+v, want as published %+v", after, changes)
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
	if got, want := publishedBlockIDs(pub), []int64{turned.ID, standing.ID}; !slices.Equal(got, want) {
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
	if ids, want := publishedBlockIDs(got), []int64{turned.ID, standing.ID}; !slices.Equal(ids, want) {
		t.Errorf("publication's exceptions %v after the Check-in, want as published %v", ids, want)
	}
	if changes := factOfKind(t, publishedBlock(t, h, got, turned.ID), domain.ReportFactChanged).Value.(domain.ReportChangedFact).Changes; !slices.Equal(changes, []string{"Turned Red"}) {
		t.Errorf("publication's first block changes %q, want [Turned Red]", changes)
	}
	if slices.ContainsFunc(publishedBlock(t, h, got, standing.ID), isFact(domain.ReportFactChanged)) {
		t.Errorf("publication's standing block lists changes, want none")
	}
	if parts := h.PublicationFact(got, domain.ReportFactMovement).Value.(domain.ReportMovementFact).Line.Parts; !slices.Equal(parts, []string{"1 turned Red"}) {
		t.Errorf("publication's movement %q, want 1 turned Red", parts)
	}
}

// A publication keeps the Schedule conflicts its blocks named, with the dates
// they had when it was published: moving the child's date back before its
// parent's ends the conflict on the next draft, not on the publication.
func TestPublicationFreezesItsScheduleConflicts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	parentDue := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	childDue := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	parent := h.ActiveGoalDueLater(pat, "Open in Iberia", parentDue)
	child := h.ActiveGoalDueLater(sam, "Lisbon office", childDue)
	h.AcceptedLink(child, parent)
	settle(h)
	h.Checkin(sam, child.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(sam, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{child.ID}})
	pub := h.PublishReport(sam, def)

	h.Clock.Advance(day)
	slipDelivery(h, sam, child, parentDue.AddDate(0, 0, -7))
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if b := linkedBlock(t, draft, child); len(b.ScheduleConflicts) != 0 {
		t.Errorf("next draft's Schedule conflicts %+v, want none once the child is due first", b.ScheduleConflicts)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	groups := factOfKind(t, publishedBlock(t, h, got, child.ID), domain.ReportFactScheduleConflict).Value.(domain.ReportScheduleConflictFact).Groups
	if len(groups) != 1 || groups[0].Words != domain.ReportConflictParentEarlier || len(groups[0].Conflicts) != 1 ||
		groups[0].Conflicts[0].GoalID != parent.ID || !groups[0].Conflicts[0].Due.Equal(parentDue) {
		t.Errorf("publication's Schedule conflicts %+v, want the one with Open in Iberia, due %s, as published", groups, parentDue)
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
	if got, want := h.PublicationFact(pub, domain.ReportFactSelection).Value.(domain.ReportSelectionFact).Text, "Owner: Dana Okafor · Active"; got != want {
		t.Errorf("published selection %q, want %q", got, want)
	}
	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if want := "Owner: Dana Okafor · Active"; h.PublicationFact(got, domain.ReportFactSelection).Value.(domain.ReportSelectionFact).Text != want {
		t.Errorf("reloaded selection %q, want %q", h.PublicationFact(got, domain.ReportFactSelection).Value.(domain.ReportSelectionFact).Text, want)
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
	if sel, want := h.PublicationFact(got, domain.ReportFactSelection).Value.(domain.ReportSelectionFact).Text, "Owner: Dana Okafor · Active"; sel != want {
		t.Errorf("selection after editing the rules %q, want as published %q", sel, want)
	}
	if next, want := h.PublicationFact(h.PublishReport(boss, updated), domain.ReportFactSelection).Value.(domain.ReportSelectionFact).Text, "On Hold"; next != want {
		t.Errorf("next publication's selection %q, want %q", next, want)
	}
}

// A publication's selection counts only the Also include and Leave out Goals
// that changed what it selected: one the rules match anyway isn't added, and
// one the rules don't match isn't left out.
func TestPublicationSelectionCountsOnlyOverridesThatChangeIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Dana Okafor")
	lee := h.SignInNamed("lee@example.com", "Lee Chen")
	matched := h.ActiveGoal(boss, "Dana's, matched anyway", "why")
	added := h.ActiveGoal(lee, "Lee's, added", "why")
	leftOut := h.ActiveGoal(boss, "Dana's, left out", "why")
	unmatched := h.ActiveGoal(lee, "Lee's, never matched", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "MBR",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{ownerRule(domain.RuleIs, boss)},
		Include: []int64{matched.ID, added.ID},
		Exclude: []int64{leftOut.ID, unmatched.ID},
	})
	pub := h.PublishReport(boss, def)
	if got, want := h.PublicationFact(pub, domain.ReportFactSelection).Value.(domain.ReportSelectionFact).Text, "Owner: Dana Okafor · 1 added, 1 left out"; got != want {
		t.Errorf("published selection %q, want %q", got, want)
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
	if got, want := h.PublicationFact(h.PublishReport(boss, def), domain.ReportFactSelection).Value.(domain.ReportSelectionFact).Text, "2 Goals picked by hand"; got != want {
		t.Errorf("published selection %q, want %q", got, want)
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
	b := publishedBlock(t, h, got, s.Referral.ID)
	if behind, want := factOfKind(t, b, domain.ReportFactSupportedBy).Value.(domain.ReportSupportedByFact).Goals, []domain.LinkedGoal{
		linked(s.Alpha, domain.HealthRed, true),
		linked(s.Beta, domain.HealthRed, false),
		linked(s.Gamma, domain.HealthYellow, false),
		linked(s.Epsilon, domain.HealthGreen, true),
	}; !slices.Equal(behind, want) {
		t.Errorf("published Supported by = %+v, want %+v as it read when published", behind, want)
	}
	if to, want := factOfKind(t, b, domain.ReportFactContributesTo).Value.(domain.ReportContributesToFact).Goals, []domain.LinkedGoal{linked(s.Growth, domain.HealthRed, true)}; !slices.Equal(to, want) {
		t.Errorf("published Contributes to = %+v, want %+v", to, want)
	}
	if count := factOfKind(t, b, domain.ReportFactRolledUp).Value.(domain.ReportRolledUpFact).RolledUp.Count; count != "2 of 5 Red · 1 Yellow" {
		t.Errorf("published Rolled-up count %q, want 2 Red and 1 Yellow as when published", count)
	}

	draft, err := h.Service.DraftReport(ctx, s.Definition, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if ru := linkedBlock(t, draft, s.Referral).RolledUp; ru.RedChildren != 1 {
		t.Errorf("next draft RolledUp = %+v, want 1 Red now Beta payouts is Green", ru)
	}
}

// A publication freezes an exception block's Ask for help as it stood: the
// Owner unmarking it and the addressee being renamed later leave the
// publication asking of the old Name, while a fresh draft no longer asks.
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
	if ask := factOfKind(t, publishedBlock(t, h, got, g.ID), domain.ReportFactAskForHelp).Value.(domain.ReportAskForHelpFact).Ask; ask.Addressee == nil || ask.Addressee.Label() != "Mia Chen" {
		t.Errorf("published block asks %+v, want an ask of Mia Chen as published", ask)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(draft.Exceptions) != 1 || draft.Exceptions[0].AsksForHelp || draft.Exceptions[0].AskAddressee != nil {
		t.Errorf("a fresh draft's blocks %+v, want one with no ask", draft.Exceptions)
	}
}

// A publication marks each block as judged in its Goal's timezone when it was
// published: published when the back-to-Green date has passed in Asia/Tokyo
// but not yet in UTC, its Path to Green is Overdue, and clearing the Owner's
// Timezone later doesn't move the mark (CONTEXT.md: Timezone), while a fresh
// draft reads the Owner as they are now.
func TestPublicationFreezesTheTimezoneItsGoalWasJudgedIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	g := h.ActiveGoal(kenji, "Ship search", "People can't find things.")
	now := h.Clock.Now()
	backToGreen := time.Date(now.Year(), now.Month(), now.Day()+3, 0, 0, 0, 0, time.UTC)
	h.Checkin(kenji, g.ID, domain.HealthRed, "Blocked.", "Escalate.", backToGreen)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	// 16:00 UTC on the date is 01:00 the next day in Tokyo.
	h.Clock.Set(backToGreen.Add(16 * time.Hour))
	pub := h.PublishReport(boss, def)
	if path := factOfKind(t, publishedBlock(t, h, pub, g.ID), domain.ReportFactPathToGreen).Value.(domain.ReportPathToGreenFact).Path; !path.Overdue {
		t.Fatalf("published Path to Green %+v, want Overdue, its date passed in Tokyo", path)
	}

	h.SetTimezone(kenji, "")

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if path := factOfKind(t, publishedBlock(t, h, got, g.ID), domain.ReportFactPathToGreen).Value.(domain.ReportPathToGreenFact).Path; !path.Overdue {
		t.Errorf("published Path to Green %+v after the Owner's Timezone was cleared, want still Overdue, as published", path)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(draft.Exceptions) != 1 || draft.Exceptions[0].Timezone != "UTC" {
		t.Errorf("a fresh draft's blocks %+v, want one with Timezone UTC, the company's", draft.Exceptions)
	}
	if b := draft.Exceptions[0]; domain.PathToGreenOverdueAt(b, h.Clock.Now()) {
		t.Errorf("a fresh draft's Path to Green is Overdue, want not yet, its date not passed in UTC")
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
	if head := factOfKind(t, publishedBlock(t, h, pub, g.ID), domain.ReportFactBlockHead).Value.(domain.ReportBlockHeadFact).Head; head.RunOffGreen != "1 wk" {
		t.Fatalf("published head %+v, want 1 week off Green", head)
	}

	h.Clock.Advance(14 * day)
	checkinHealth(h, boss, g, domain.HealthRed)

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if head := factOfKind(t, publishedBlock(t, h, got, g.ID), domain.ReportFactBlockHead).Value.(domain.ReportBlockHeadFact).Head; head.RunOffGreen != "1 wk" {
		t.Errorf("publication's head %+v, want still 1 week off Green, as published", head)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(draft.Exceptions) != 1 || draft.Exceptions[0].RunOffGreenWeeks != 3 {
		t.Errorf("a fresh draft's blocks %+v, want one 3 weeks off Green", draft.Exceptions)
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
	if path := factOfKind(t, publishedBlock(t, h, pub, g.ID), domain.ReportFactPathToGreen).Value.(domain.ReportPathToGreenFact).Path; !slices.EqualFunc(path.PriorDates, published, time.Time.Equal) {
		t.Fatalf("published Path to Green %+v, want one slipped from %v", path, published)
	}

	h.Clock.Advance(day)
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", date(2026, 4, 3), "Counsel quit.")

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if path := factOfKind(t, publishedBlock(t, h, got, g.ID), domain.ReportFactPathToGreen).Value.(domain.ReportPathToGreenFact).Path; !slices.EqualFunc(path.PriorDates, published, time.Time.Equal) {
		t.Errorf("publication's Path to Green %+v, want one still slipped from %v, as published", path, published)
	}
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if want := []time.Time{date(2026, 3, 6), date(2026, 3, 13)}; len(draft.Exceptions) != 1 || !slices.EqualFunc(draft.Exceptions[0].PriorPathTargetDates, want, time.Time.Equal) {
		t.Errorf("a fresh draft's blocks %+v, want one slipped from %v", draft.Exceptions, want)
	}
}

// A Report Delegate publishing at the same moment the creator removes them:
// the Report is drafted outside the transaction that publishes it, so who may
// publish is checked again inside it. Either the publication is kept and
// logged before the removal, or it is refused (ErrNotAuthorized); never kept
// after its publisher stopped being a Report Delegate (#570).
func TestPublishRechecksThePublisherInsideItsTransaction(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	alice := h.SignIn("alice@example.com")
	dana := h.SignIn("dana@example.com")
	g := h.ActiveGoal(alice, "Grow revenue", "Revenue funds the roadmap.")
	for i := range 30 {
		def := h.SaveReportDefinition(alice, domain.SaveReportDefinitionInput{
			Name: fmt.Sprintf("MBR %d", i), Mode: domain.ReportModePicked, Picked: []int64{g.ID},
		})
		h.AddReportDelegate(alice, dana, def)
		publish := func() error {
			_, err := h.Service.PublishReport(ctx, dana.ID, def.ID, time.Time{})
			return err
		}
		remove := func() error {
			return h.Service.RemoveReportDelegate(ctx, alice.ID, def.ID, dana.ID)
		}
		errPublish, errRemove := raceHandoffCommands(publish, remove)
		if errRemove != nil {
			t.Fatalf("round %d: RemoveReportDelegate: %v", i, errRemove)
		}
		if errPublish != nil {
			if !errors.Is(errPublish, domain.ErrNotAuthorized) {
				t.Fatalf("round %d: PublishReport err = %v, want ErrNotAuthorized or kept", i, errPublish)
			}
			continue
		}
		log, err := h.Service.ReportLog(ctx, def.ID)
		if err != nil {
			t.Fatalf("ReportLog: %v", err)
		}
		// Newest first: the removal must come before (newer than) the publication.
		kinds := make([]string, 0, len(log))
		for _, e := range log {
			kinds = append(kinds, e.Kind)
		}
		removed := slices.Index(kinds, domain.ReportLogDelegate)
		published := slices.Index(kinds, domain.ReportLogPublished)
		if removed > published {
			t.Fatalf("round %d: the Report log (newest first) reads %v: published by Dana after being removed", i, kinds)
		}
	}
}

// A narrative curated while the Report was being drafted is missing from the
// draft, so the publish refuses rather than freeze the draft and clear the new
// narrative unpublished: nothing is published, the narrative stays saved for
// the next publish, and the Report log records no publication.
func TestPublishRefusesWhenTheNarrativeIsCuratedDuringTheDraft(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	eu := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, eu.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	before, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	signed := highlightID(t, before, "Signed the first EU customer.")

	_, err = domain.PublishReportChangedMidway(ctx, h.Service, boss.ID, def.ID, time.Time{}, func() {
		if err := h.Service.CurateNarrative(ctx, boss.ID, def.ID, domain.CurateNarrativeInput{
			Picks: []domain.NarrativePick{{HighlightID: signed, Section: domain.HighlightAccomplishment}},
			Notes: map[string][]string{domain.HighlightAccomplishment: {"EU is open for business."}},
		}); err != nil {
			t.Fatalf("CurateNarrative: %v", err)
		}
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("PublishReport err = %v, want ErrValidation", err)
	}
	if want := "The Report changed while it was being published. Publish it again."; domain.Reason(err) != want {
		t.Errorf("PublishReport err = %q, want it to say %q", err, want)
	}
	assertUnpublished(t, h, def)
	draft, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	wins, ok := section(draft, domain.HighlightAccomplishment)
	if !ok || !slices.Equal(wins.Notes, []string{"EU is open for business."}) ||
		!slices.Equal(highlightNotes(wins.Highlights), []string{"Signed the first EU customer."}) {
		t.Errorf("draft narrative after the refusal %+v, want the narrative curated during the draft", draft.Narrative)
	}
}

// assertUnpublished fails the test if def has a publication or a "Published"
// entry in its Report log.
func assertUnpublished(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition) {
	t.Helper()
	ctx := context.Background()
	pubs, err := h.Service.ListPublications(ctx, def.ID)
	if err != nil {
		t.Fatalf("ListPublications: %v", err)
	}
	if len(pubs) != 0 {
		t.Errorf("%d publications, want none", len(pubs))
	}
	log, err := h.Service.ReportLog(ctx, def.ID)
	if err != nil {
		t.Fatalf("ReportLog: %v", err)
	}
	for _, e := range log {
		if e.Kind == domain.ReportLogPublished {
			t.Errorf("the Report log records a publication: %+v", e)
		}
	}
}

// A Report Definition edited while its Report was being drafted is refused
// too, rather than published under the name and selection it had before.
func TestPublishRefusesWhenTheDefinitionIsEditedDuringTheDraft(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	dana := h.SignIn("dana@example.com")
	eu := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	h.AddReportDelegate(boss, dana, def)

	_, err := domain.PublishReportChangedMidway(ctx, h.Service, dana.ID, def.ID, time.Time{}, func() {
		if _, err := h.Service.UpdateReportDefinition(ctx, boss.ID, def.ID, domain.SaveReportDefinitionInput{
			Name: "Quarterly review", Mode: domain.ReportModePicked, Picked: []int64{eu.ID},
		}); err != nil {
			t.Fatalf("UpdateReportDefinition: %v", err)
		}
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("PublishReport err = %v, want ErrValidation", err)
	}
	assertUnpublished(t, h, def)

	// Published again, it reads as the Definition does now.
	pub := publish(t, h, dana, def, time.Time{})
	if pub.Name != "Quarterly review" {
		t.Errorf("publication titled %q, want the edited title %q", pub.Name, "Quarterly review")
	}
}

// Of two publishes of one Report Definition racing, both drafted before
// either is written, only the first written is published: the second would
// clear a narrative the first already froze and publish the same Report again.
func TestOnlyOneOfTwoRacingPublishesSucceeds(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	dana := h.SignIn("dana@example.com")
	eu := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	h.AddReportDelegate(boss, dana, def)

	var first domain.Publication
	_, err := domain.PublishReportChangedMidway(ctx, h.Service, dana.ID, def.ID, time.Time{}, func() {
		first = publish(t, h, boss, def, time.Time{})
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("second PublishReport err = %v, want ErrValidation", err)
	}
	pubs, err := h.Service.ListPublications(ctx, def.ID)
	if err != nil {
		t.Fatalf("ListPublications: %v", err)
	}
	if len(pubs) != 1 || pubs[0].ID != first.ID || pubs[0].PublishedBy.ID != boss.ID {
		t.Errorf("publications %+v, want only the first, by boss", pubs)
	}
}
