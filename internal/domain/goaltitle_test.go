package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Goal's title can be changed after it is created, by its Owner or an
// Admin, and each change is recorded with the old and new titles (#281).

func TestEditTitleTrimsSavesAndRecordsTheChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	owner := h.SignIn("sam@example.com")
	admin := h.SignIn("ada@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")

	got, err := h.Service.EditTitle(ctx, g.ID, "  Halve outages  ", admin.ID)
	if err != nil {
		t.Fatalf("EditTitle: %v", err)
	}
	if got.Title != "Halve outages" {
		t.Errorf("returned Title = %q, want %q", got.Title, "Halve outages")
	}
	if viewed, err := h.Service.ViewGoal(ctx, g.ID); err != nil || viewed.Title != "Halve outages" {
		t.Errorf("ViewGoal Title = %q (err %v), want %q", viewed.Title, err, "Halve outages")
	}

	revisions, err := h.Service.ListTitleRevisions(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListTitleRevisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d title revisions, want 1: %+v", len(revisions), revisions)
	}
	r := revisions[0]
	if r.GoalID != g.ID || r.OldTitle != "Reduce outages" || r.NewTitle != "Halve outages" || r.Author.ID != admin.ID {
		t.Errorf("revision = %+v, want Goal %d from %q to %q by %d", r, g.ID, "Reduce outages", "Halve outages", admin.ID)
	}
	if !r.CreatedAt.Equal(h.Clock.Now()) {
		t.Errorf("revision CreatedAt = %v, want %v", r.CreatedAt, h.Clock.Now())
	}
}

func TestEditTitleListsRevisionsNewestFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")

	for _, title := range []string{"Halve outages", "End outages"} {
		if _, err := h.Service.EditTitle(ctx, g.ID, title, owner.ID); err != nil {
			t.Fatalf("EditTitle(%q): %v", title, err)
		}
	}
	revisions, err := h.Service.ListTitleRevisions(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListTitleRevisions: %v", err)
	}
	var got []string
	for _, r := range revisions {
		got = append(got, r.OldTitle+" → "+r.NewTitle)
	}
	want := []string{"Halve outages → End outages", "Reduce outages → Halve outages"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("revisions = %q, want %q", got, want)
	}
}

func TestEditTitleRefusesABlankTitle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")

	_, err := h.Service.EditTitle(ctx, g.ID, "   ", owner.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("EditTitle with a blank title: err = %v, want ErrValidation", err)
	}
	if viewed, _ := h.Service.ViewGoal(ctx, g.ID); viewed.Title != "Reduce outages" {
		t.Errorf("Title = %q after a refused edit, want it unchanged", viewed.Title)
	}
	if revisions, _ := h.Service.ListTitleRevisions(ctx, g.ID); len(revisions) != 0 {
		t.Errorf("a refused edit recorded %d title revisions: %+v", len(revisions), revisions)
	}
}

func TestEditTitleOnAMissingGoalIsNotFound(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")

	if _, err := h.Service.EditTitle(context.Background(), 999, "Anything", owner.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("EditTitle on a missing Goal: err = %v, want ErrNotFound", err)
	}
}

func TestRequireGoalOwnerOrAdminAllowsOnlyTheOwnerAndAnAdmin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	owner := h.SignIn("sam@example.com")
	admin := h.SignIn("ada@example.com")
	delegate := h.SignIn("tpm@example.com")
	other := h.SignIn("mel@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(owner, delegate, g.ID)

	for _, tc := range []struct {
		who     string
		actor   domain.Account
		goalID  int64
		wantErr error
	}{
		{"the Owner", owner, g.ID, nil},
		{"an Admin", admin, g.ID, nil},
		{"a Delegate", delegate, g.ID, domain.ErrNotAuthorized},
		{"another Account", other, g.ID, domain.ErrNotAuthorized},
		{"the Owner on a missing Goal", owner, 999, domain.ErrNotFound},
		{"an Admin on a missing Goal", admin, 999, domain.ErrNotFound},
	} {
		err := h.Service.RequireGoalOwnerOrAdmin(ctx, tc.actor.ID, tc.goalID)
		if tc.wantErr == nil && err != nil {
			t.Errorf("%s: err = %v, want allowed", tc.who, err)
		}
		if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: err = %v, want %v", tc.who, err, tc.wantErr)
		}
	}
}

// The Edit title and Why it matters form saves both fields at once, each only
// when it differs, all or nothing (#520).

func TestEditTitleAndWhyItMattersRevisesOnlyTheChangedField(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")

	title := "Reduce outages"
	got, err := h.Service.EditTitleAndWhyItMatters(ctx, owner.ID, g.ID, &title, "  Outages cost customers.  ")
	if err != nil {
		t.Fatalf("EditTitleAndWhyItMatters: %v", err)
	}
	if got.Title != "Reduce outages" || got.WhyItMatters != "Outages cost customers." {
		t.Errorf("returned Goal = %q / %q, want %q / %q", got.Title, got.WhyItMatters, "Reduce outages", "Outages cost customers.")
	}
	if revisions, _ := h.Service.ListTitleRevisions(ctx, g.ID); len(revisions) != 0 {
		t.Errorf("an unchanged title recorded %d title revisions: %+v", len(revisions), revisions)
	}
	whyRevisions, _ := h.Service.ListWhyItMattersRevisions(ctx, g.ID)
	whyBefore := len(whyRevisions)

	title = "Halve outages"
	if _, err := h.Service.EditTitleAndWhyItMatters(ctx, owner.ID, g.ID, &title, "Outages cost customers."); err != nil {
		t.Fatalf("EditTitleAndWhyItMatters: %v", err)
	}
	if revisions, _ := h.Service.ListTitleRevisions(ctx, g.ID); len(revisions) != 1 || revisions[0].NewTitle != "Halve outages" {
		t.Errorf("title revisions = %+v, want the one change to %q", revisions, "Halve outages")
	}
	if whyRevisions, _ := h.Service.ListWhyItMattersRevisions(ctx, g.ID); len(whyRevisions) != whyBefore {
		t.Errorf("an unchanged Why it matters went from %d revisions to %d", whyBefore, len(whyRevisions))
	}
}

func TestEditTitleAndWhyItMattersRefusingABlankTitleKeepsTheWhyItMattersToo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")
	whyBefore, _ := h.Service.ListWhyItMattersRevisions(ctx, g.ID)

	blank := "   "
	_, err := h.Service.EditTitleAndWhyItMatters(ctx, owner.ID, g.ID, &blank, "Outages cost customers.")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("EditTitleAndWhyItMatters with a blank title: err = %v, want ErrValidation", err)
	}
	viewed, _ := h.Service.ViewGoal(ctx, g.ID)
	if viewed.Title != "Reduce outages" || viewed.WhyItMatters != "Outages cost trust." {
		t.Errorf("Goal = %q / %q after a refused save, want both unchanged", viewed.Title, viewed.WhyItMatters)
	}
	if revisions, _ := h.Service.ListTitleRevisions(ctx, g.ID); len(revisions) != 0 {
		t.Errorf("a refused save recorded %d title revisions", len(revisions))
	}
	if whyAfter, _ := h.Service.ListWhyItMattersRevisions(ctx, g.ID); len(whyAfter) != len(whyBefore) {
		t.Errorf("a refused save went from %d Why it matters revisions to %d", len(whyBefore), len(whyAfter))
	}
}

func TestEditTitleAndWhyItMattersRefusesAReader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	reader := h.SignIn("mel@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")

	title := "Halve outages"
	_, err := h.Service.EditTitleAndWhyItMatters(ctx, reader.ID, g.ID, &title, "Outages cost customers.")
	if !errors.Is(err, domain.ErrNotAuthorized) {
		t.Fatalf("EditTitleAndWhyItMatters by a reader: err = %v, want ErrNotAuthorized", err)
	}
	if viewed, _ := h.Service.ViewGoal(ctx, g.ID); viewed.Title != "Reduce outages" || viewed.WhyItMatters != "Outages cost trust." {
		t.Errorf("Goal = %q / %q after a refused save, want both unchanged", viewed.Title, viewed.WhyItMatters)
	}
}

func TestEditTitleAndWhyItMattersRefusingABlankWhyItMattersKeepsTheTitleToo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.ActiveGoal(owner, "Reduce outages", "Outages cost trust.")

	title := "Halve outages"
	_, err := h.Service.EditTitleAndWhyItMatters(ctx, owner.ID, g.ID, &title, "   ")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("EditTitleAndWhyItMatters with a blank Why it matters: err = %v, want ErrValidation", err)
	}
	if viewed, _ := h.Service.ViewGoal(ctx, g.ID); viewed.Title != "Reduce outages" {
		t.Errorf("Title = %q after a refused save, want it unchanged", viewed.Title)
	}
	if revisions, _ := h.Service.ListTitleRevisions(ctx, g.ID); len(revisions) != 0 {
		t.Errorf("a refused save recorded %d title revisions", len(revisions))
	}
}
