package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

func TestAddMilestoneListsUnderTheGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	date := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	m, err := h.Service.AddMilestone(context.Background(), domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Beta cut",
		TargetDate: date,
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	if m.ID == 0 {
		t.Error("want a persisted Milestone with a non-zero ID")
	}
	if m.Name != "Beta cut" || !m.TargetDate.Equal(date) {
		t.Errorf("Milestone = %+v", m)
	}

	list, err := h.Service.ListMilestones(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Beta cut" {
		t.Errorf("ListMilestones = %+v", list)
	}
}

func TestAddMilestoneRequiresNameAndDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	cases := map[string]domain.AddMilestoneInput{
		"no name": {GoalID: g.ID, Name: "  ", TargetDate: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)},
		"no date": {GoalID: g.ID, Name: "Beta cut", TargetDate: time.Time{}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.Service.AddMilestone(context.Background(), in); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestEditMilestoneChangesNameAndDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")
	m, err := h.Service.AddMilestone(context.Background(), domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Beta cut",
		TargetDate: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}

	newDate := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	got, err := h.Service.EditMilestone(context.Background(), domain.EditMilestoneInput{
		MilestoneID: m.ID,
		Name:        "GA cut",
		TargetDate:  newDate,
	})
	if err != nil {
		t.Fatalf("EditMilestone: %v", err)
	}
	if got.Name != "GA cut" || !got.TargetDate.Equal(newDate) {
		t.Errorf("EditMilestone = %+v", got)
	}
}

// A Check-in marks a Milestone Done; a Done Milestone is no longer overdue, so
// the Check-in may be Green even past its date.
func TestCheckinMarksMilestoneDone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	if beta.Status != domain.MilestonePlanned {
		t.Fatalf("new Milestone status = %q, want %q", beta.Status, domain.MilestonePlanned)
	}
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:     goal.ID,
		AuthorID:   sam.ID,
		Health:     domain.HealthGreen,
		Status:     "Beta shipped.",
		Milestones: []domain.MilestoneChangeInput{{MilestoneID: beta.ID, Status: domain.MilestoneDone}},
	}); err != nil {
		t.Fatalf("SubmitCheckin marking the Milestone Done: %v", err)
	}
	if got := onlyMilestone(t, h, goal.ID); got.Status != domain.MilestoneDone {
		t.Errorf("Status = %q, want %q", got.Status, domain.MilestoneDone)
	}
}

// Removing a Milestone in a Check-in requires a reason, which is kept.
func TestCheckinRemovesMilestoneWithReason(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)

	in := domain.SubmitCheckinInput{
		GoalID:     goal.ID,
		AuthorID:   sam.ID,
		Health:     domain.HealthGreen,
		Status:     "Dropping the beta.",
		Milestones: []domain.MilestoneChangeInput{{MilestoneID: beta.ID, Status: domain.MilestoneRemoved, RemovedReason: " "}},
	}
	if _, err := h.Service.SubmitCheckin(context.Background(), in); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Removed without a reason: err = %v, want ErrValidation", err)
	}
	if got := onlyMilestone(t, h, goal.ID); got.Status != domain.MilestonePlanned {
		t.Errorf("Status = %q after the rejection, want %q", got.Status, domain.MilestonePlanned)
	}

	in.Milestones[0].RemovedReason = "Customers asked to skip straight to GA."
	if _, err := h.Service.SubmitCheckin(context.Background(), in); err != nil {
		t.Fatalf("Removed with a reason: %v", err)
	}
	got := onlyMilestone(t, h, goal.ID)
	if got.Status != domain.MilestoneRemoved || got.RemovedReason != "Customers asked to skip straight to GA." {
		t.Errorf("Milestone = %+v, want Removed with the reason", got)
	}
}

// Only a Planned Milestone can be marked Done or Removed, and a status must be
// one of the known ones.
func TestCheckinMilestoneStatusMustMoveFromPlanned(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	submit := func(status string) error {
		_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID:     goal.ID,
			AuthorID:   sam.ID,
			Health:     domain.HealthGreen,
			Status:     "Update.",
			Milestones: []domain.MilestoneChangeInput{{MilestoneID: beta.ID, Status: status, RemovedReason: "Scope cut."}},
		})
		return err
	}

	if err := submit("Skipped"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("unknown status: err = %v, want ErrValidation", err)
	}
	if err := submit(domain.MilestoneDone); err != nil {
		t.Fatalf("mark Done: %v", err)
	}
	if err := submit(domain.MilestoneRemoved); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("remove a Done Milestone: err = %v, want ErrValidation", err)
	}
}

// A Check-in adds new Milestones to the Goal, each Planned; each needs a name
// and a date.
func TestCheckinAddsMilestones(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	gaDate := goal.DeliveryDate.AddDate(0, 0, -7)

	bad := domain.SubmitCheckinInput{
		GoalID:        goal.ID,
		AuthorID:      sam.ID,
		Health:        domain.HealthGreen,
		Status:        "Adding a GA gate.",
		NewMilestones: []domain.NewMilestoneInput{{Name: " ", TargetDate: gaDate}},
	}
	if _, err := h.Service.SubmitCheckin(context.Background(), bad); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("new Milestone without a name: err = %v, want ErrValidation", err)
	}
	bad.NewMilestones = []domain.NewMilestoneInput{{Name: "GA"}}
	if _, err := h.Service.SubmitCheckin(context.Background(), bad); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("new Milestone without a date: err = %v, want ErrValidation", err)
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:        goal.ID,
		AuthorID:      sam.ID,
		Health:        domain.HealthGreen,
		Status:        "Adding a GA gate.",
		NewMilestones: []domain.NewMilestoneInput{{Name: "GA", TargetDate: gaDate}},
	}); err != nil {
		t.Fatalf("SubmitCheckin adding a Milestone: %v", err)
	}
	ms, err := h.Service.ListMilestones(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d Milestones, want 2", len(ms))
	}
	ga := ms[1]
	if ga.Name != "GA" || !ga.TargetDate.Equal(gaDate) || ga.Status != domain.MilestonePlanned {
		t.Errorf("added Milestone = %+v, want Planned GA on %s", ga, gaDate)
	}
}

// Milestone Churn counts Milestones added or removed since the Goal became
// Active; the Milestones planned before activation don't count, and marking one
// Done is not churn (CONTEXT.md: Milestone Churn).
func TestMilestoneChurnCountsAddedAndRemovedSinceActive(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)

	churn := func() int {
		t.Helper()
		n, err := h.Service.MilestoneChurn(context.Background(), goal.ID)
		if err != nil {
			t.Fatalf("MilestoneChurn: %v", err)
		}
		return n
	}
	if got := churn(); got != 0 {
		t.Fatalf("churn right after activation = %d, want 0", got)
	}

	later := goal.DeliveryDate.AddDate(0, 0, -7)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Re-planned.",
		NewMilestones: []domain.NewMilestoneInput{
			{Name: "GA", TargetDate: later},
			{Name: "Docs", TargetDate: later},
		},
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: beta.ID, Status: domain.MilestoneRemoved, RemovedReason: "Skipping the beta."},
		},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if got := churn(); got != 3 {
		t.Errorf("churn = %d, want 3 (two added, one removed)", got)
	}

	ms, _ := h.Service.ListMilestones(context.Background(), goal.ID)
	var docs domain.Milestone
	for _, m := range ms {
		if m.Name == "Docs" {
			docs = m
		}
	}
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:     goal.ID,
		AuthorID:   sam.ID,
		Health:     domain.HealthGreen,
		Status:     "Docs landed.",
		Milestones: []domain.MilestoneChangeInput{{MilestoneID: docs.ID, Status: domain.MilestoneDone}},
	}); err != nil {
		t.Fatalf("SubmitCheckin marking Done: %v", err)
	}
	if got := churn(); got != 3 {
		t.Errorf("churn after marking Done = %d, want still 3", got)
	}
}

// replanned arranges an Active Goal with two Planned Milestones, Beta (from
// activation) and Rollout to 50% (added from the Goal page, so not in a
// Check-in), and the Check-in input that adds GA, marks Beta Done and removes
// Rollout to 50% with a reason.
func replanned(t *testing.T, h *testsupport.Harness) (domain.Goal, domain.SubmitCheckinInput) {
	t.Helper()
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	rollout, err := h.Service.AddMilestone(context.Background(), domain.AddMilestoneInput{
		GoalID: goal.ID, Name: "Rollout to 50%", TargetDate: goal.DeliveryDate.AddDate(0, 0, -14),
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	return goal, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Re-planned.",
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: beta.ID, Status: domain.MilestoneDone},
			{MilestoneID: rollout.ID, Status: domain.MilestoneRemoved, RemovedReason: "descoped"},
		},
		NewMilestones: []domain.NewMilestoneInput{{Name: "GA", TargetDate: goal.DeliveryDate.AddDate(0, 0, -7)}},
	}
}

// A Check-in records each Milestone it adds, marks Done or removes, with the
// removal's reason, and its listing carries them.
func TestCheckinRecordsItsMilestoneChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	goal, in := replanned(t, h)
	ms, err := h.Service.ListMilestones(context.Background(), goal.ID)
	if err != nil || len(ms) != 2 {
		t.Fatalf("ListMilestones: %v %v", ms, err)
	}
	h.Clock.Advance(24 * time.Hour)
	c, err := h.Service.SubmitCheckin(context.Background(), in)
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}

	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(checkins) != 1 || checkins[0].ID != c.ID {
		t.Fatalf("ListCheckins = %+v, want the one Check-in", checkins)
	}
	got := checkins[0].MilestoneChanges
	gaDate := goal.DeliveryDate.AddDate(0, 0, -7)
	want := []struct {
		kind, name, reason string
		date               time.Time
	}{
		{domain.MilestoneChangeAdded, "GA", "", gaDate},
		{domain.MilestoneChangeDone, "Beta", "", ms[0].TargetDate},
		{domain.MilestoneChangeRemoved, "Rollout to 50%", "descoped", ms[1].TargetDate},
	}
	if len(got) != len(want) {
		t.Fatalf("MilestoneChanges = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.kind || g.Name != w.name || g.Reason != w.reason || !g.AddedDate.Equal(w.date) || g.MilestoneID == 0 {
			t.Errorf("change %d = %+v, want %s %q (%s) %q", i, g, w.kind, w.name, w.date.Format("2006-01-02"), w.reason)
		}
	}
}

// A Check-in's Milestone changes keep each Milestone's name as it was: renaming
// the added, Done and Removed Milestones afterwards leaves the entry as it read.
func TestCheckinMilestoneChangesKeepTheNameAtTheTime(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	goal, in := replanned(t, h)
	if _, err := h.Service.SubmitCheckin(context.Background(), in); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	ms, err := h.Service.ListMilestones(context.Background(), goal.ID)
	if err != nil || len(ms) != 3 {
		t.Fatalf("ListMilestones: %v %v", ms, err)
	}
	for _, m := range ms {
		if _, err := h.Service.EditMilestone(context.Background(), domain.EditMilestoneInput{
			MilestoneID: m.ID,
			Name:        m.Name + " (renamed)",
			TargetDate:  m.TargetDate,
		}); err != nil {
			t.Fatalf("EditMilestone %q: %v", m.Name, err)
		}
	}

	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil || len(checkins) != 1 {
		t.Fatalf("ListCheckins: %+v %v", checkins, err)
	}
	got := checkins[0].MilestoneChanges
	want := []struct{ kind, name string }{
		{domain.MilestoneChangeAdded, "GA"},
		{domain.MilestoneChangeDone, "Beta"},
		{domain.MilestoneChangeRemoved, "Rollout to 50%"},
	}
	if len(got) != len(want) {
		t.Fatalf("MilestoneChanges = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Name != w.name {
			t.Errorf("change %d = %s %q, want %s %q", i, got[i].Kind, got[i].Name, w.kind, w.name)
		}
	}
}

// A Check-in that fails validation records none of its Milestone changes.
func TestRejectedCheckinRecordsNoMilestoneChange(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	goal, in := replanned(t, h)
	in.Health = domain.HealthYellow // with no Path to Green
	if _, err := h.Service.SubmitCheckin(context.Background(), in); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Yellow without a Path to Green: err = %v, want ErrValidation", err)
	}

	h.Checkin(h.SignIn("sam@example.com"), goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(checkins) != 1 || len(checkins[0].MilestoneChanges) != 0 {
		t.Errorf("ListCheckins = %+v, want one Check-in with no Milestone changes", checkins)
	}
	ms, err := h.Service.ListMilestones(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(ms) != 2 || ms[0].Status != domain.MilestonePlanned || ms[1].Status != domain.MilestonePlanned {
		t.Errorf("Milestones = %+v after the rejection, want the two still Planned", ms)
	}
}

// Milestone Churn counts from the Milestones, not from the changes a Check-in
// records: Rollout to 50% added while Active, then GA added and Rollout to 50%
// removed in a Check-in, is churn of 3, and it stays 3 for a Goal whose
// Check-in has no recorded changes, as an older one doesn't.
func TestMilestoneChurnIsUnchangedByRecordedChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	goal, in := replanned(t, h)
	if _, err := h.Service.SubmitCheckin(context.Background(), in); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	churn := func() int {
		t.Helper()
		n, err := h.Service.MilestoneChurn(context.Background(), goal.ID)
		if err != nil {
			t.Fatalf("MilestoneChurn: %v", err)
		}
		return n
	}
	if got := churn(); got != 3 {
		t.Errorf("churn = %d, want 3 (Rollout to 50%% and GA added, Rollout to 50%% removed)", got)
	}
	if _, err := h.DB.Exec(`DELETE FROM milestone_changes`); err != nil {
		t.Fatalf("delete milestone changes: %v", err)
	}
	if got := churn(); got != 3 {
		t.Errorf("churn without recorded changes = %d, want still 3", got)
	}
}

// A Milestone's mark is the first that applies of Done, Removed, Red (Planned
// and past its date as of the day), Yellow (Planned and slipped), and New
// (Planned and added within the window); a Planned Milestone that is none of
// these has no mark and reads as on track (CONTEXT.md: Milestone).
func TestMilestoneMarkTakesTheFirstThatApplies(t *testing.T) {
	t.Parallel()

	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	asOf := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
	slipped := []time.Time{day("2026-09-01")}
	milestone := func(status, target string) domain.Milestone {
		return domain.Milestone{Name: "Vendor sign-off", Status: status, TargetDate: day(target)}
	}
	cases := []struct {
		name  string
		m     domain.Milestone
		prior []time.Time
		isNew bool
		asOf  time.Time
		want  string
	}{
		{"done", milestone(domain.MilestoneDone, "2026-11-01"), nil, false, asOf, "Done"},
		{"done outranks overdue, slipped and new", milestone(domain.MilestoneDone, "2026-09-30"), slipped, true, asOf, "Done"},
		{"removed", milestone(domain.MilestoneRemoved, "2026-11-01"), nil, false, asOf, "Removed"},
		{"removed outranks overdue", milestone(domain.MilestoneRemoved, "2026-09-30"), slipped, true, asOf, "Removed"},
		{"overdue is red", milestone(domain.MilestonePlanned, "2026-10-03"), nil, false, asOf, "Red"},
		{"red outranks yellow and new", milestone(domain.MilestonePlanned, "2026-10-03"), slipped, true, asOf, "Red"},
		{"due as of the day is not overdue", milestone(domain.MilestonePlanned, "2026-10-04"), nil, false, asOf, ""},
		{"overdue compares the calendar date of as of in UTC", milestone(domain.MilestonePlanned, "2026-10-04"), nil, false,
			time.Date(2026, 10, 4, 20, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60)), "Red"},
		{"slipped is yellow", milestone(domain.MilestonePlanned, "2026-11-20"), slipped, false, asOf, "Yellow"},
		{"yellow outranks new", milestone(domain.MilestonePlanned, "2026-11-20"), slipped, true, asOf, "Yellow"},
		{"new", milestone(domain.MilestonePlanned, "2026-11-20"), nil, true, asOf, "New"},
		{"on track has no mark", milestone(domain.MilestonePlanned, "2026-12-15"), nil, false, asOf, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.MilestoneMarkOf(c.m, c.prior, c.isNew, c.asOf); got != c.want {
				t.Errorf("MilestoneMarkOf = %q, want %q", got, c.want)
			}
		})
	}
}

// changesOutsideCheckins returns the Milestone changes recorded on goalID
// outside any Check-in, failing the test on error.
func changesOutsideCheckins(t *testing.T, h *testsupport.Harness, goalID int64) []domain.MilestoneChange {
	t.Helper()
	got, err := h.Service.MilestoneChangesOutsideCheckins(context.Background(), goalID)
	if err != nil {
		t.Fatalf("MilestoneChangesOutsideCheckins: %v", err)
	}
	return got
}

// The Owner can add a Milestone to an Active Goal outside a Check-in. It is
// listed, recorded as added by the Owner at the time it was added with no
// Check-in, and counts toward Milestone Churn (CONTEXT.md: Milestone,
// Milestone Churn).
func TestAddMilestoneAsAuthorRecordsAnAdditionToAnActiveGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Rivera")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(48 * time.Hour)
	date := goal.DeliveryDate.AddDate(0, 0, -7)

	m, err := h.Service.AddMilestoneAsAuthor(context.Background(), sam.ID, domain.AddMilestoneInput{
		GoalID: goal.ID, Name: " GA ", TargetDate: date,
	})
	if err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	if m.Name != "GA" || !m.TargetDate.Equal(date) || m.GoalID != goal.ID {
		t.Errorf("Milestone = %+v", m)
	}
	if ms, _ := h.Service.ListMilestones(context.Background(), goal.ID); len(ms) != 2 {
		t.Errorf("ListMilestones = %+v, want Beta and GA", ms)
	}

	got := changesOutsideCheckins(t, h, goal.ID)
	if len(got) != 1 {
		t.Fatalf("changes outside Check-ins = %+v, want the one addition", got)
	}
	c := got[0]
	if c.Kind != domain.MilestoneChangeAdded || c.MilestoneID != m.ID || c.Name != "GA" ||
		!c.AddedDate.Equal(date) || !c.CreatedAt.Equal(h.Clock.Now()) || c.Author.ID != sam.ID || c.Author.Name != "Sam Rivera" {
		t.Errorf("change = %+v, want GA added by Sam now", c)
	}
	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil || len(checkins) != 1 || len(checkins[0].MilestoneChanges) != 0 {
		t.Errorf("ListCheckins = %+v %v, want the Check-in without the addition", checkins, err)
	}
	if n, _ := h.Service.MilestoneChurn(context.Background(), goal.ID); n != 1 {
		t.Errorf("churn = %d, want 1", n)
	}
}

// A Delegate can add a Milestone to an On Hold Goal outside a Check-in. It is
// recorded as theirs, but an On Hold Goal isn't Active, so it isn't Milestone
// Churn.
func TestAddMilestoneAsAuthorByADelegateWhileOnHold(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	goal := h.OnHoldGoal(sam, "Ship v2", "Customers wait too long.", "Waiting on the vendor.")
	h.AddDelegate(sam, dana, goal.ID)

	m, err := h.Service.AddMilestoneAsAuthor(context.Background(), dana.ID, domain.AddMilestoneInput{
		GoalID: goal.ID, Name: "Vendor sign-off", TargetDate: goal.DeliveryDate.AddDate(0, 0, -30),
	})
	if err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	got := changesOutsideCheckins(t, h, goal.ID)
	if len(got) != 1 || got[0].MilestoneID != m.ID || got[0].Author.ID != dana.ID {
		t.Errorf("changes outside Check-ins = %+v, want Vendor sign-off added by Dana", got)
	}
	if n, _ := h.Service.MilestoneChurn(context.Background(), goal.ID); n != 0 {
		t.Errorf("churn = %d, want 0", n)
	}
}

// A Milestone added to a Proposed Goal is part of planning it: it is listed
// but recorded as no change, and isn't churn.
func TestAddMilestoneAsAuthorWhileProposedRecordsNoChange(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Ship v2", "why")

	if _, err := h.Service.AddMilestoneAsAuthor(context.Background(), sam.ID, domain.AddMilestoneInput{
		GoalID: goal.ID, Name: "Beta", TargetDate: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	if ms, _ := h.Service.ListMilestones(context.Background(), goal.ID); len(ms) != 1 || ms[0].Name != "Beta" {
		t.Errorf("ListMilestones = %+v, want Beta", ms)
	}
	if got := changesOutsideCheckins(t, h, goal.ID); len(got) != 0 {
		t.Errorf("changes outside Check-ins = %+v, want none", got)
	}
	if n, _ := h.Service.MilestoneChurn(context.Background(), goal.ID); n != 0 {
		t.Errorf("churn = %d, want 0", n)
	}
}

// Only the Goal's Owner or a Delegate may add a Milestone outside a Check-in,
// and a Departed Delegate no longer may; a refused addition changes nothing.
func TestAddMilestoneAsAuthorRefusesAnyoneElse(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "why")
	h.AddDelegate(sam, dana, goal.ID)
	if err := h.Service.MarkDeparted(context.Background(), admin.ID, dana.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	for name, actor := range map[string]domain.Account{
		"a stranger":          h.SignIn("pat@example.com"),
		"an Admin":            admin,
		"a Departed Delegate": dana,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.Service.AddMilestoneAsAuthor(context.Background(), actor.ID, domain.AddMilestoneInput{
				GoalID: goal.ID, Name: "GA", TargetDate: goal.DeliveryDate,
			})
			if !errors.Is(err, domain.ErrNotAuthorized) || !strings.Contains(err.Error(), "only the Owner or a Delegate may change this Goal's Milestones") {
				t.Errorf("err = %v, want the Milestone editor refusal", err)
			}
		})
	}
	if ms, _ := h.Service.ListMilestones(context.Background(), goal.ID); len(ms) != 1 {
		t.Errorf("ListMilestones = %+v, want only Beta", ms)
	}
	if got := changesOutsideCheckins(t, h, goal.ID); len(got) != 0 {
		t.Errorf("changes outside Check-ins = %+v, want none", got)
	}
}

// A Done or Cancelled Goal takes no new Milestones.
func TestAddMilestoneAsAuthorRefusesAnEndedGoal(t *testing.T) {
	t.Parallel()

	for _, lifecycle := range []string{domain.LifecycleDone, domain.LifecycleCancelled} {
		t.Run(lifecycle, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Ship v2", "why")
			h.EndGoalInCheckin(sam, goal.ID, lifecycle)

			_, err := h.Service.AddMilestoneAsAuthor(context.Background(), sam.ID, domain.AddMilestoneInput{
				GoalID: goal.ID, Name: "GA", TargetDate: goal.DeliveryDate,
			})
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
			if ms, _ := h.Service.ListMilestones(context.Background(), goal.ID); len(ms) != 1 {
				t.Errorf("ListMilestones = %+v, want only Beta", ms)
			}
		})
	}
}

// A Milestone added outside a Check-in needs a name and a date.
func TestAddMilestoneAsAuthorRequiresNameAndDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "why")

	for name, in := range map[string]domain.AddMilestoneInput{
		"no name": {GoalID: goal.ID, Name: "  ", TargetDate: goal.DeliveryDate},
		"no date": {GoalID: goal.ID, Name: "GA"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.Service.AddMilestoneAsAuthor(context.Background(), sam.ID, in); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
	if got := changesOutsideCheckins(t, h, goal.ID); len(got) != 0 {
		t.Errorf("changes outside Check-ins = %+v, want none", got)
	}
}

// RequireMilestoneEditor allows a Milestone's Goal's Owner and Delegates, and
// refuses anyone else; a missing Milestone is not found.
func TestRequireMilestoneEditor(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "why")
	h.AddDelegate(sam, dana, goal.ID)
	beta := onlyMilestone(t, h, goal.ID)
	ctx := context.Background()

	if err := h.Service.RequireMilestoneEditor(ctx, sam.ID, beta.ID); err != nil {
		t.Errorf("the Owner: %v", err)
	}
	if err := h.Service.RequireMilestoneEditor(ctx, dana.ID, beta.ID); err != nil {
		t.Errorf("a Delegate: %v", err)
	}
	if err := h.Service.RequireMilestoneEditor(ctx, pat.ID, beta.ID); !errors.Is(err, domain.ErrNotAuthorized) ||
		!strings.Contains(err.Error(), "only the Owner or a Delegate may change this Goal's Milestones") {
		t.Errorf("anyone else: err = %v, want the Milestone editor refusal", err)
	}
	if err := h.Service.RequireMilestoneEditor(ctx, sam.ID, beta.ID+100); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a missing Milestone: err = %v, want ErrNotFound", err)
	}
}
