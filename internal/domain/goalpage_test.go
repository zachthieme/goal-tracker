package domain_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Goal page's loader shows any signed-in viewer any Goal, as ViewGoal
// does, and refuses a Goal that doesn't exist with ErrNotFound, as ViewGoal
// does (ADR 0010).
func TestGoalPageRefusesAGoalViewGoalRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	rita := h.SignIn("rita@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")

	if _, err := h.Service.ViewGoal(ctx, g.ID+1000); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ViewGoal of a missing Goal: err = %v, want ErrNotFound", err)
	}
	if _, err := h.Service.GoalPage(ctx, rita.ID, g.ID+1000); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GoalPage of a missing Goal: err = %v, want ErrNotFound", err)
	}
	p, err := h.Service.GoalPage(ctx, rita.ID, g.ID)
	if err != nil {
		t.Fatalf("GoalPage for a reader with no rights: %v", err)
	}
	if p.Goal.Title != "Ship search" {
		t.Errorf("GoalPage's Goal = %q, want Ship search", p.Goal.Title)
	}
}

// The Goal page's loader offers each viewer what the separate permission
// rules allow: the Owner and a Delegate may check in and set values, an
// Admin may only set values, and a reader with no rights may do neither; only
// those who may check in see the pending Draft Highlights.
func TestGoalPagePermitsWhatTheRulesAllow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	rita := h.SignIn("rita@example.com")
	admin := h.SignIn("admin@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, morgan, g.ID)
	if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: g.ID, AuthorID: morgan.ID, Kind: domain.HighlightInsight, Note: "Search is slow on mobile.",
	}); err != nil {
		t.Fatalf("LogDraftHighlight: %v", err)
	}

	for _, v := range []struct {
		name                   string
		viewer                 domain.Account
		checkIn, setValues     bool
		draftHighlightsVisible int
	}{
		{"the Owner", sam, true, true, 1},
		{"a Delegate", morgan, true, true, 1},
		{"an Admin", admin, false, true, 0},
		{"a reader", rita, false, false, 0},
	} {
		p, err := h.Service.GoalPage(ctx, v.viewer.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage for %s: %v", v.name, err)
		}
		if p.CanCheckin != v.checkIn {
			t.Errorf("CanCheckin for %s = %t, want %t", v.name, p.CanCheckin, v.checkIn)
		}
		if p.Permissions.SetValues != v.setValues {
			t.Errorf("Permissions.SetValues for %s = %t, want %t", v.name, p.Permissions.SetValues, v.setValues)
		}
		if len(p.DraftHighlights) != v.draftHighlightsVisible {
			t.Errorf("%s sees %d Draft Highlights, want %d", v.name, len(p.DraftHighlights), v.draftHighlightsVisible)
		}
	}
}

// The Goal page's loader says what each viewer may do with the Goal, by the
// rule each command enforces: on an Active Goal the Owner may do everything
// but an Admin's own acts and suggest a parent, a Delegate what a Check-in
// author may, set values and suggest a parent, an Admin set values, edit, hand
// off, mark the Owner departed, mark Top-level and suggest a parent, and a
// reader suggest a parent. Only while the Goal is Proposed may its Owner
// define it, and its Owner and Delegates edit its Milestones, and no one checks
// in on it yet. Once the
// Goal is Done no one may check in, add a Milestone, log a Draft Highlight or
// suggest a parent, the Owner included.
func TestGoalPagePermissionsFollowEachCommandsRule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	rita := h.SignIn("rita@example.com")
	admin := h.SignIn("admin@example.com")
	proposed := h.CreateGoal(sam, "Ship billing", "People can't pay.")
	active := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	done := h.ActiveGoal(sam, "Ship sign-in", "People can't get in.")
	for _, g := range []domain.Goal{proposed, active, done} {
		h.AddDelegate(sam, morgan, g.ID)
	}
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)

	owner := domain.GoalPermissions{
		CheckIn: true, AddMilestone: true, LogDraftHighlight: true, SetValues: true,
		AddContributor: true, ManageDelegates: true, LinkParent: true,
		EditTitleAndWhyItMatters: true, HandOff: true,
	}
	delegate := domain.GoalPermissions{
		CheckIn: true, AddMilestone: true, LogDraftHighlight: true, SetValues: true,
		SuggestParent: true,
	}
	anAdmin := domain.GoalPermissions{
		SetValues: true, EditTitleAndWhyItMatters: true, HandOff: true,
		MarkOwnerDeparted: true, MarkTopLevel: true, SuggestParent: true,
	}
	reader := domain.GoalPermissions{SuggestParent: true}
	ownerOfProposed := owner
	ownerOfProposed.CheckIn = false
	ownerOfProposed.DefineGoal, ownerOfProposed.EditMilestones = true, true
	delegateOnProposed := delegate
	delegateOnProposed.CheckIn, delegateOnProposed.EditMilestones = false, true
	ownerOfDone := owner
	ownerOfDone.CheckIn, ownerOfDone.AddMilestone, ownerOfDone.LogDraftHighlight = false, false, false
	adminOnDone := anAdmin
	adminOnDone.SuggestParent = false
	for _, v := range []struct {
		name   string
		viewer domain.Account
		goal   domain.Goal
		want   domain.GoalPermissions
	}{
		{"the Owner of the Proposed Goal", sam, proposed, ownerOfProposed},
		{"a Delegate on the Proposed Goal", morgan, proposed, delegateOnProposed},
		{"an Admin on the Proposed Goal", admin, proposed, anAdmin},
		{"a reader of the Proposed Goal", rita, proposed, reader},
		{"the Owner of the Active Goal", sam, active, owner},
		{"a Delegate on the Active Goal", morgan, active, delegate},
		{"an Admin on the Active Goal", admin, active, anAdmin},
		{"a reader of the Active Goal", rita, active, reader},
		{"the Owner of the Done Goal", sam, done, ownerOfDone},
		{"a Delegate on the Done Goal", morgan, done, domain.GoalPermissions{SetValues: true}},
		{"an Admin on the Done Goal", admin, done, adminOnDone},
		{"a reader of the Done Goal", rita, done, domain.GoalPermissions{}},
	} {
		p, err := h.Service.GoalPage(ctx, v.viewer.ID, v.goal.ID)
		if err != nil {
			t.Fatalf("GoalPage for %s: %v", v.name, err)
		}
		if p.Permissions != v.want {
			t.Errorf("Permissions for %s = %+v, want %+v", v.name, p.Permissions, v.want)
		}
	}
}

// On an Ownerless Goal an Admin may reassign it and mark its Owner returned,
// but not hand it off or mark its Owner departed again.
func TestGoalPagePermitsAnAdminToReassignAnOwnerlessGoal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	admin := h.SignIn("admin@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.MarkDeparted(admin, sam)

	p, err := h.Service.GoalPage(ctx, admin.ID, g.ID)
	if err != nil {
		t.Fatalf("GoalPage: %v", err)
	}
	want := domain.GoalPermissions{
		SetValues: true, EditTitleAndWhyItMatters: true, Reassign: true,
		MarkOwnerReturned: true, MarkTopLevel: true, SuggestParent: true,
	}
	if p.Permissions != want {
		t.Errorf("Permissions for an Admin on an Ownerless Goal = %+v, want %+v", p.Permissions, want)
	}
}

// A viewer ID that names no Account is neither the Owner, a Delegate nor an
// Admin, so the Goal page's loader offers them neither Check in nor setting
// values, and none of the pending Draft Highlights.
func TestGoalPagePermitsNothingToAViewerWithNoAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: g.ID, AuthorID: sam.ID, Kind: domain.HighlightInsight, Note: "Search is slow on mobile.",
	}); err != nil {
		t.Fatalf("LogDraftHighlight: %v", err)
	}

	p, err := h.Service.GoalPage(ctx, sam.ID+1000, g.ID)
	if err != nil {
		t.Fatalf("GoalPage for a viewer with no Account: %v", err)
	}
	if p.CanCheckin {
		t.Errorf("CanCheckin for a viewer with no Account = true, want false")
	}
	if p.Permissions != (domain.GoalPermissions{}) {
		t.Errorf("Permissions for a viewer with no Account = %+v, want none", p.Permissions)
	}
	if len(p.DraftHighlights) != 0 {
		t.Errorf("a viewer with no Account sees %d Draft Highlights, want none", len(p.DraftHighlights))
	}
}

// What the Goal page's loader works out for a Goal with a parent and a child,
// Check-ins, Milestones and a Metric — its Rolled-up Health, Run off Green,
// freshness, Health strip and signals — is what the separate Service methods
// give for the same Goal.
func TestGoalPageDerivesWhatTheServiceMethodsGive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Grow revenue", "The company needs it.")
	g := h.ActiveChildOf(sam, parent, "Ship search", "People can't find things.")
	child := h.ActiveChildOf(sam, g, "Index the catalogue", "Search needs it.")
	metric, err := h.Service.AddMetric(ctx, sam.ID, domain.AddMetricInput{
		GoalID: g.ID, Name: "Searches", Unit: "a day", Direction: domain.MetricUp,
		Baseline: 10, Target: 100, TargetDate: h.Clock.Now().AddDate(0, 2, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, sam.ID, domain.AddMilestoneInput{
		GoalID: g.ID, Name: "Beta", TargetDate: h.Clock.Now().AddDate(0, 0, 10),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track.",
		Readings: []domain.MetricReadingInput{{MetricID: metric.ID, Value: 40}},
	}); err != nil {
		t.Fatalf("SubmitCheckin Green: %v", err)
	}
	h.Clock.Advance(8 * 24 * time.Hour)
	h.Checkin(sam, child.ID, domain.HealthYellow, "Slow going.", "Add an indexer.", h.Clock.Now().AddDate(0, 0, 7))
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthRed, Status: "Blocked on indexing.",
		PathToGreen: "Index the catalogue first.", PathTargetDate: h.Clock.Now().AddDate(0, 0, 3),
		Explanation: "The index is the blocker, not the child's pace.",
		Readings:    []domain.MetricReadingInput{{MetricID: metric.ID, Value: 45}},
	}); err != nil {
		t.Fatalf("SubmitCheckin Red: %v", err)
	}
	h.Clock.Advance(10 * 24 * time.Hour)

	p, err := h.Service.GoalPage(ctx, sam.ID, g.ID)
	if err != nil {
		t.Fatalf("GoalPage: %v", err)
	}
	if !p.RolledUp.Present || p.RolledUp.Health != domain.HealthYellow {
		t.Errorf("Rolled-up Health = %+v, want Yellow from the child", p.RolledUp)
	}
	if p.RunOffGreen.Weeks < 2 {
		t.Errorf("Run off Green = %+v, want a run of 2 weeks or more", p.RunOffGreen)
	}
	if !p.Freshness.CheckinOverdue || !p.Freshness.PathToGreenOverdue {
		t.Errorf("Freshness = %+v, want Check-in overdue with its Path to Green overdue", p.Freshness)
	}

	rollup, err := h.Service.RolledUpHealth(ctx, g.ID)
	if err != nil {
		t.Fatalf("RolledUpHealth: %v", err)
	}
	same(t, "Rolled-up Health", p.RolledUp, rollup)
	run, err := h.Service.RunOffGreen(ctx, g.ID)
	if err != nil {
		t.Fatalf("RunOffGreen: %v", err)
	}
	same(t, "Run off Green", p.RunOffGreen, run)
	fresh, err := h.Service.Freshness(ctx, g.ID)
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	same(t, "freshness", p.Freshness, fresh)
	strip, err := h.Service.HealthStrip(ctx, g.ID)
	if err != nil {
		t.Fatalf("HealthStrip: %v", err)
	}
	same(t, "Health strip", p.HealthStrip, strip)
	signals, err := h.Service.GoalSignals(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalSignals: %v", err)
	}
	same(t, "signals", p.Signals, signals)
	checkins, err := h.Service.ListCheckins(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	same(t, "Check-ins", p.Checkins, checkins)
	additions, err := h.Service.MilestoneChangesOutsideCheckins(ctx, g.ID)
	if err != nil {
		t.Fatalf("MilestoneChangesOutsideCheckins: %v", err)
	}
	same(t, "Milestone additions", p.MilestoneAdditions, additions)
	refusal := h.Service.NoChangeRefusal(ctx, g.ID)
	if refusal == nil || p.NoChangeRefusal == nil || p.NoChangeRefusal.Error() != refusal.Error() {
		t.Errorf("No change refusal = %v, want NoChangeRefusal's %v", p.NoChangeRefusal, refusal)
	}
}

// same fails the test unless the loader's got is what the Service method of
// the same name gave, want.
func same[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GoalPage's %s = %+v, want the Service method's %+v", what, got, want)
	}
}

// The Goal page carries the Goal's first overdue Planned Milestone, as of today
// in its timezone, with or without a Check-in and whatever its Health, so the
// Check-in form never starts from a Green the Milestone rules out (#566).
func TestGoalPageCarriesTheOverdueMilestone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	overdue := func() *domain.Milestone {
		t.Helper()
		p, err := h.Service.GoalPage(ctx, sam.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage: %v", err)
		}
		return p.OverdueMilestone
	}

	if m := overdue(); m != nil {
		t.Errorf("before Beta's date, OverdueMilestone = %+v, want none", *m)
	}
	h.Clock.Advance(100 * 24 * time.Hour) // past Beta's date
	if m := overdue(); m == nil || m.Name != "Beta" {
		t.Errorf("with no Check-in, OverdueMilestone = %+v, want Beta", m)
	}
	h.Checkin(sam, g.ID, domain.HealthYellow, "Slipping.", "Add a reviewer.", h.Clock.Now().AddDate(0, 0, 7))
	if m := overdue(); m == nil || m.Name != "Beta" {
		t.Errorf("after a Yellow Check-in, OverdueMilestone = %+v, want Beta", m)
	}
}
