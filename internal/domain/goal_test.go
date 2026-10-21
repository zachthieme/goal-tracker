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
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")

	g, err := h.Service.CreateGoal(context.Background(), domain.CreateGoalInput{
		Title:        "Cut checkout latency",
		WhyItMatters: "Shoppers abandon slow carts; faster checkout lifts conversion.",
		OwnerID:      owner.ID,
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
	if g.WhyItMatters == "" {
		t.Error("Why it matters was dropped")
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

func TestCreateGoalRequiresTitleAndWhyItMatters(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")

	cases := map[string]domain.CreateGoalInput{
		"no title":          {Title: "  ", WhyItMatters: "why", OwnerID: owner.ID},
		"no why it matters": {Title: "A goal", WhyItMatters: "", OwnerID: owner.ID},
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
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	created := h.CreateGoal(owner, "Cut checkout latency", "Faster checkout lifts conversion.")

	got, err := h.Service.ViewGoal(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Title != created.Title || got.WhyItMatters != created.WhyItMatters {
		t.Errorf("ViewGoal = %+v, want %+v", got, created)
	}
	if got.Owner.Email != "sam@example.com" {
		t.Errorf("Owner.Email = %q, want the signed-in Owner", got.Owner.Email)
	}
}

func TestViewGoalNotFound(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	if _, err := h.Service.ViewGoal(context.Background(), 999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListGoalsShowsCreatedGoals(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	date := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	got, err := h.Service.MarkGoalDated(context.Background(), g.Owner.ID, g.ID, date)
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
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	if _, err := h.Service.MarkGoalDated(context.Background(), g.Owner.ID, g.ID, time.Time{}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestMarkGoalOngoingSetsKindAndClearsDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Keep the lights on", "Uptime keeps customers.")

	// Mark Dated first, then Ongoing, to prove the delivery date is cleared.
	if _, err := h.Service.MarkGoalDated(context.Background(), g.Owner.ID, g.ID, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	got, err := h.Service.MarkGoalOngoing(context.Background(), g.Owner.ID, g.ID)
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
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	if g.CadenceDays != 7 {
		t.Errorf("CadenceDays = %d, want the 7-day default", g.CadenceDays)
	}
}

func TestSetCadenceChangesTheCheckInInterval(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	got, err := h.Service.SetCadence(context.Background(), g.Owner.ID, g.ID, 14)
	if err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	if got.CadenceDays != 14 {
		t.Errorf("CadenceDays = %d, want 14", got.CadenceDays)
	}
}

// Only a Goal's Owner may set its cadence: anyone else, a Delegate and an
// Admin included, is refused and the cadence stays as it was.
func TestSetCadenceAdmitsOnlyTheOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")
	ctx := context.Background()

	for name, actor := range notTheOwner(h, owner, g.ID) {
		if _, err := h.Service.SetCadence(ctx, actor.ID, g.ID, 14); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s: err = %v, want ErrNotAuthorized", name, err)
		}
	}
	if got, _ := h.Service.ViewGoal(ctx, g.ID); got.CadenceDays != 7 {
		t.Errorf("CadenceDays after refusals = %d, want 7", got.CadenceDays)
	}
	if _, err := h.Service.SetCadence(ctx, owner.ID, g.ID, 14); err != nil {
		t.Errorf("Owner: %v", err)
	}
}

// notTheOwner signs in the people an Owner-only command refuses on goalID: a
// stranger, one of its Delegates, and an Admin. The harness must list
// admin@example.com as an Admin.
func notTheOwner(h *testsupport.Harness, owner domain.Account, goalID int64) map[string]domain.Account {
	delegate := h.SignIn("tpm@example.com")
	h.AddDelegate(owner, delegate, goalID)
	return map[string]domain.Account{
		"a stranger": h.SignIn("pat@example.com"),
		"a Delegate": delegate,
		"an Admin":   h.SignIn("admin@example.com"),
	}
}

func TestSetCadenceRejectsNonPositive(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	if _, err := h.Service.SetCadence(context.Background(), g.Owner.ID, g.ID, 0); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestAddContributorListsThem(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestCreateGoalRecordsFirstWhyItMattersRevision(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	revs, err := h.Service.ListWhyItMattersRevisions(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListWhyItMattersRevisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("len(revs) = %d, want 1 at creation", len(revs))
	}
	if revs[0].WhyItMatters != "Customers wait too long for v2." {
		t.Errorf("first revision = %q", revs[0].WhyItMatters)
	}
	if revs[0].Author.Email != "sam@example.com" {
		t.Errorf("author = %q, want the Owner", revs[0].Author.Email)
	}
}

// A Goal whose first Why it matters revision can't be recorded is not kept, so
// every Goal has a Definition log to start from (#421).
func TestCreateGoalKeepsNothingWhenItsFirstRevisionFails(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	ctx := context.Background()
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_revision BEFORE INSERT ON why_it_matters_revisions
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}

	if _, err := h.Service.CreateGoal(ctx, domain.CreateGoalInput{
		Title:        "Ship v2",
		WhyItMatters: "Customers wait too long for v2.",
		OwnerID:      owner.ID,
	}); err == nil {
		t.Fatal("CreateGoal succeeded despite the injected failure")
	}

	goals, err := h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 0 {
		t.Errorf("goals after a failed create = %d, want none", len(goals))
	}
}

func TestEditWhyItMattersKeepsEveryRevision(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "First why.")

	got, err := h.Service.EditWhyItMatters(context.Background(), g.ID, "Second why.", owner.ID)
	if err != nil {
		t.Fatalf("EditWhyItMatters: %v", err)
	}
	if got.WhyItMatters != "Second why." {
		t.Errorf("current Why it matters = %q, want the edit", got.WhyItMatters)
	}

	revs, err := h.Service.ListWhyItMattersRevisions(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListWhyItMattersRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("len(revs) = %d, want 2 after one edit", len(revs))
	}
	// Newest first: the edit, then the original.
	if revs[0].WhyItMatters != "Second why." || revs[1].WhyItMatters != "First why." {
		t.Errorf("revisions = %q, %q", revs[0].WhyItMatters, revs[1].WhyItMatters)
	}
}

func TestEditWhyItMattersRejectsEmpty(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "First why.")

	if _, err := h.Service.EditWhyItMatters(context.Background(), g.ID, "   ", owner.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestSuggestDeliveryDatePicksNearestUpcomingCheckpointMonday(t *testing.T) {
	t.Parallel()

	// The suggestion is the nearest upcoming Monday that sits at a mid-month
	// (the Monday nearest the 15th) or end-of-month (the last Monday) checkpoint.
	cases := []struct {
		from string
		want string
	}{
		{"2026-01-05", "2026-01-12"}, // mid-January Monday, just ahead
		{"2026-01-20", "2026-01-26"}, // past mid-month, so the last Monday of January
		{"2026-01-27", "2026-02-16"}, // past both January checkpoints, so mid-February
	}
	for _, c := range cases {
		t.Run(c.from, func(t *testing.T) {
			from, _ := time.Parse("2006-01-02", c.from)
			got := domain.SuggestDeliveryDate(from)
			if got.Format("2006-01-02") != c.want {
				t.Errorf("SuggestDeliveryDate(%s) = %s, want %s", c.from, got.Format("2006-01-02"), c.want)
			}
			if got.Weekday() != time.Monday {
				t.Errorf("suggestion %s is not a Monday", got.Format("2006-01-02"))
			}
			if got.Before(from) {
				t.Errorf("suggestion %s is before %s", got.Format("2006-01-02"), c.from)
			}
		})
	}
}

func TestAddContributorByEmailResolvesTheAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")
	h.SignIn("dana@example.com")

	if err := h.Service.AddContributorByEmail(context.Background(), g.ID, "dana@example.com"); err != nil {
		t.Fatalf("AddContributorByEmail: %v", err)
	}
	list, err := h.Service.ListContributors(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListContributors: %v", err)
	}
	if len(list) != 1 || list[0].Email != "dana@example.com" {
		t.Errorf("ListContributors = %+v", list)
	}
}

func TestAddContributorByEmailRejectsUnknownEmail(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	if err := h.Service.AddContributorByEmail(context.Background(), g.ID, "nobody@example.com"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation for an unknown email", err)
	}
}

// Only a Goal's Owner passes RequireGoalOwner: another Account and a Delegate
// are refused, and a missing Goal is not found.
func TestRequireGoalOwnerAdmitsOnlyTheOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	other := h.SignIn("pat@example.com")
	delegate := h.SignIn("tpm@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "Faster checkout lifts conversion.")
	h.AddDelegate(owner, delegate, g.ID)
	ctx := context.Background()

	if err := h.Service.RequireGoalOwner(ctx, owner.ID, g.ID); err != nil {
		t.Errorf("Owner: err = %v, want nil", err)
	}
	if err := h.Service.RequireGoalOwner(ctx, other.ID, g.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("another Account: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RequireGoalOwner(ctx, delegate.ID, g.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Delegate: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RequireGoalOwner(ctx, owner.ID, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing Goal: err = %v, want ErrNotFound", err)
	}
}

// Only a Goal's Owner may mark it Dated or Ongoing: anyone else, a Delegate
// and an Admin included, is refused and its Kind stays as it was.
func TestMarkGoalKindAdmitsOnlyTheOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")
	ctx := context.Background()
	due := testsupport.Epoch.AddDate(0, 6, 0)

	for name, actor := range notTheOwner(h, owner, g.ID) {
		if _, err := h.Service.MarkGoalDated(ctx, actor.ID, g.ID, due); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s marking it Dated: err = %v, want ErrNotAuthorized", name, err)
		}
		if _, err := h.Service.MarkGoalOngoing(ctx, actor.ID, g.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s marking it Ongoing: err = %v, want ErrNotAuthorized", name, err)
		}
	}
	if got, _ := h.Service.ViewGoal(ctx, g.ID); got.Kind != "" {
		t.Errorf("Kind after refusals = %q, want none", got.Kind)
	}
	if _, err := h.Service.MarkGoalDated(ctx, owner.ID, g.ID, due); err != nil {
		t.Errorf("Owner marking it Dated: %v", err)
	}
	if _, err := h.Service.MarkGoalOngoing(ctx, owner.ID, g.ID); err != nil {
		t.Errorf("Owner marking it Ongoing: %v", err)
	}
}
