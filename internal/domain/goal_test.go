package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

func TestCreateGoalIsProposedAndOwned(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")

	g, err := h.Service.CreateGoal(context.Background(), domain.CreateGoalInput{
		Title:   "Cut checkout latency",
		SoWhat:  "Shoppers abandon slow carts; faster checkout lifts conversion.",
		OwnerID: owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	if g.ID == 0 {
		t.Error("want a persisted Goal with a non-zero ID")
	}
	if g.Title != "Cut checkout latency" {
		t.Errorf("Title = %q", g.Title)
	}
	if g.SoWhat == "" {
		t.Error("So What was dropped")
	}
	if g.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Lifecycle = %q, want %q", g.Lifecycle, domain.LifecycleProposed)
	}
	if g.Owner.ID != owner.ID {
		t.Errorf("Owner = %d, want %d", g.Owner.ID, owner.ID)
	}
	if !g.CreatedAt.Equal(testsupport.Epoch) {
		t.Errorf("CreatedAt = %v, want the harness clock %v", g.CreatedAt, testsupport.Epoch)
	}
}

func TestCreateGoalRequiresTitleAndSoWhat(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")

	cases := map[string]domain.CreateGoalInput{
		"no title":   {Title: "  ", SoWhat: "why", OwnerID: owner.ID},
		"no so what": {Title: "A goal", SoWhat: "", OwnerID: owner.ID},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.Service.CreateGoal(context.Background(), in); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestViewGoalReturnsWhatWasCreated(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	created := h.CreateGoal(owner, "Cut checkout latency", "Faster checkout lifts conversion.")

	got, err := h.Service.ViewGoal(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Title != created.Title || got.SoWhat != created.SoWhat {
		t.Errorf("ViewGoal = %+v, want %+v", got, created)
	}
	if got.Owner.Email != "sam@example.com" {
		t.Errorf("Owner.Email = %q, want the signed-in Owner", got.Owner.Email)
	}
}

func TestViewGoalNotFound(t *testing.T) {
	h := testsupport.New(t)
	if _, err := h.Service.ViewGoal(context.Background(), 999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListGoalsShowsCreatedGoals(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	h.CreateGoal(owner, "First goal", "why one")
	h.CreateGoal(owner, "Second goal", "why two")

	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("len(goals) = %d, want 2", len(goals))
	}
	titles := map[string]bool{}
	for _, g := range goals {
		titles[g.Title] = true
		if g.Owner.Email != "sam@example.com" {
			t.Errorf("goal %q has Owner %q", g.Title, g.Owner.Email)
		}
	}
	if !titles["First goal"] || !titles["Second goal"] {
		t.Errorf("missing goals in list: %v", titles)
	}
}

func TestMarkGoalDatedSetsKindAndDeliveryDate(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	date := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	got, err := h.Service.MarkGoalDated(context.Background(), g.ID, date)
	if err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	if got.Kind != domain.GoalDated {
		t.Errorf("Kind = %q, want %q", got.Kind, domain.GoalDated)
	}
	if !got.DeliveryDate.Equal(date) {
		t.Errorf("DeliveryDate = %v, want %v", got.DeliveryDate, date)
	}
}

func TestMarkGoalDatedRequiresADate(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	if _, err := h.Service.MarkGoalDated(context.Background(), g.ID, time.Time{}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestMarkGoalOngoingSetsKindAndClearsDate(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Keep the lights on", "Uptime keeps customers.")

	// Mark Dated first, then Ongoing, to prove the delivery date is cleared.
	if _, err := h.Service.MarkGoalDated(context.Background(), g.ID, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	got, err := h.Service.MarkGoalOngoing(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	if got.Kind != domain.GoalOngoing {
		t.Errorf("Kind = %q, want %q", got.Kind, domain.GoalOngoing)
	}
	if !got.DeliveryDate.IsZero() {
		t.Errorf("DeliveryDate = %v, want zero", got.DeliveryDate)
	}
}

func TestCadenceDefaultsToSevenDays(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	if g.CadenceDays != 7 {
		t.Errorf("CadenceDays = %d, want the 7-day default", g.CadenceDays)
	}
}

func TestSetCadenceChangesTheCheckInInterval(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	got, err := h.Service.SetCadence(context.Background(), g.ID, 14)
	if err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	if got.CadenceDays != 14 {
		t.Errorf("CadenceDays = %d, want 14", got.CadenceDays)
	}
}

func TestSetCadenceRejectsNonPositive(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	if _, err := h.Service.SetCadence(context.Background(), g.ID, 0); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestAddContributorListsThem(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")
	dana := h.SignIn("dana@example.com")

	if err := h.Service.AddContributor(context.Background(), g.ID, dana.ID); err != nil {
		t.Fatalf("AddContributor: %v", err)
	}
	list, err := h.Service.ListContributors(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListContributors: %v", err)
	}
	if len(list) != 1 || list[0].Email != "dana@example.com" {
		t.Errorf("ListContributors = %+v", list)
	}
}

func TestAddContributorRejectsDuplicate(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")
	dana := h.SignIn("dana@example.com")

	if err := h.Service.AddContributor(context.Background(), g.ID, dana.ID); err != nil {
		t.Fatalf("AddContributor: %v", err)
	}
	if err := h.Service.AddContributor(context.Background(), g.ID, dana.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation on duplicate", err)
	}
}

func TestCreateGoalRecordsFirstSoWhatRevision(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	revs, err := h.Service.ListSoWhatRevisions(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListSoWhatRevisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("len(revs) = %d, want 1 at creation", len(revs))
	}
	if revs[0].SoWhat != "Customers wait too long for v2." {
		t.Errorf("first revision = %q", revs[0].SoWhat)
	}
	if revs[0].Author.Email != "sam@example.com" {
		t.Errorf("author = %q, want the Owner", revs[0].Author.Email)
	}
}

func TestEditSoWhatKeepsEveryRevision(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "First why.")

	got, err := h.Service.EditSoWhat(context.Background(), g.ID, "Second why.", owner.ID)
	if err != nil {
		t.Fatalf("EditSoWhat: %v", err)
	}
	if got.SoWhat != "Second why." {
		t.Errorf("current So What = %q, want the edit", got.SoWhat)
	}

	revs, err := h.Service.ListSoWhatRevisions(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListSoWhatRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("len(revs) = %d, want 2 after one edit", len(revs))
	}
	// Newest first: the edit, then the original.
	if revs[0].SoWhat != "Second why." || revs[1].SoWhat != "First why." {
		t.Errorf("revisions = %q, %q", revs[0].SoWhat, revs[1].SoWhat)
	}
}

func TestEditSoWhatRejectsEmpty(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "First why.")

	if _, err := h.Service.EditSoWhat(context.Background(), g.ID, "   ", owner.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}
