package domain_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

func TestSignInCreatesAccountOnFirstUse(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)

	acc, err := h.Service.SignIn(context.Background(), "sam@example.com")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if acc.ID == 0 {
		t.Error("want a persisted account with a non-zero ID")
	}
	if acc.Email != "sam@example.com" {
		t.Errorf("Email = %q, want sam@example.com", acc.Email)
	}
	if acc.IsAdmin {
		t.Error("account not in the admin list should not be an Admin")
	}
}

func TestSignInIsIdempotentForTheSameEmail(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)

	first := h.SignIn("sam@example.com")
	second := h.SignIn("sam@example.com")

	if first.ID != second.ID {
		t.Errorf("second sign-in made a new account: %d != %d", first.ID, second.ID)
	}
}

func TestSignInSetsAdminFromConfig(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")

	admin := h.SignIn("boss@example.com")
	if !admin.IsAdmin {
		t.Error("email in the admin list should get the Admin flag")
	}

	regular := h.SignIn("sam@example.com")
	if regular.IsAdmin {
		t.Error("email not in the admin list should not get the Admin flag")
	}
}

// A Departed person can't sign in (CONTEXT.md: Departed).
func TestSignInRefusesADepartedAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if _, err := h.Service.SignIn(context.Background(), "sam@example.com"); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("SignIn as a Departed Account: err = %v, want ErrDeparted", err)
	}
}

// An Admin reverses a departure: the person's remaining Goals stop being
// Ownerless, they can sign in again, and their Delegate rights come back as they
// were. A Goal reassigned while they were away stays with its new Owner
// (CONTEXT.md: Departed).
func TestMarkReturnedReversesADeparture(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	kept := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	moved := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	delegated := h.ActiveGoal(pat, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(pat, sam, delegated.ID)
	if err := h.Service.MarkDeparted(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	if _, err := h.Service.ReassignGoal(ctx, boss.ID, moved.ID, pat.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}

	if err := h.Service.MarkReturned(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkReturned: %v", err)
	}

	if g, err := h.Service.ViewGoal(ctx, kept.ID); err != nil || g.Ownerless || g.Owner.ID != sam.ID {
		t.Errorf("Goal Sam still owns = %+v, %v; want Sam's and not Ownerless", g, err)
	}
	if g, err := h.Service.ViewGoal(ctx, moved.ID); err != nil || g.Owner.ID != pat.ID {
		t.Errorf("Goal reassigned while Sam was away = %+v, %v; want it still Pat's", g, err)
	}
	if acc, err := h.Service.SignIn(ctx, "sam@example.com"); err != nil || acc.Departed {
		t.Errorf("SignIn after return = %+v, %v; want a present Account", acc, err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   delegated.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Checked in for Pat.",
	}); err != nil {
		t.Errorf("returned Delegate SubmitCheckin: %v", err)
	}
}

// Only an Admin may mark a person returned; the refusal is enforced in the
// domain, not just hidden in the UI.
func TestMarkReturnedOnlyByAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	if err := h.Service.MarkDeparted(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if err := h.Service.MarkReturned(ctx, pat.ID, sam.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin MarkReturned err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.SignIn(ctx, "sam@example.com"); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("Sam can sign in after a refused MarkReturned: err = %v", err)
	}
}

// DepartedAccounts lists every Departed person, whether or not they own a Goal,
// in the order they are shown: by Label. A present person isn't listed, nor is
// one who has returned.
func TestDepartedAccountsListsEveryDepartedPersonByLabel(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	zed := h.SignInNamed("aaron@example.com", "Zed Okafor")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	h.SignIn("lee@example.com")
	delegated := h.ActiveGoal(pat, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(pat, kim, delegated.ID)
	moved := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")

	if got, err := h.Service.DepartedAccounts(ctx); err != nil || len(got) != 0 {
		t.Errorf("DepartedAccounts with nobody Departed = %+v, %v; want none", got, err)
	}

	for _, acc := range []domain.Account{zed, sam, kim, ann} {
		if err := h.Service.MarkDeparted(ctx, boss.ID, acc.ID); err != nil {
			t.Fatalf("MarkDeparted(%s): %v", acc.Email, err)
		}
	}
	if _, err := h.Service.ReassignGoal(ctx, boss.ID, moved.ID, pat.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}
	if err := h.Service.MarkReturned(ctx, boss.ID, ann.ID); err != nil {
		t.Fatalf("MarkReturned: %v", err)
	}

	got, err := h.Service.DepartedAccounts(ctx)
	if err != nil {
		t.Fatalf("DepartedAccounts: %v", err)
	}
	var labels []string
	for _, acc := range got {
		if !acc.Departed {
			t.Errorf("DepartedAccounts lists %s as present", acc.Email)
		}
		labels = append(labels, acc.Label())
	}
	if want := []string{"kim", "sam", "Zed Okafor"}; !slices.Equal(labels, want) {
		t.Errorf("DepartedAccounts = %q, want %q", labels, want)
	}
}

// A Handoff cancelled when its new Owner departed stays cancelled after they
// return, so the Goal stays with the Owner who started it.
func TestMarkReturnedLeavesACancelledHandoffCancelled(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho, err := h.Service.StartHandoff(ctx, domain.StartHandoffInput{GoalID: goal.ID, ToOwnerID: pat.ID, ActorID: sam.ID})
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if err := h.Service.MarkDeparted(ctx, boss.ID, pat.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if err := h.Service.MarkReturned(ctx, boss.ID, pat.ID); err != nil {
		t.Fatalf("MarkReturned: %v", err)
	}

	history, err := h.Service.OwnershipHistory(ctx, goal.ID)
	if err != nil {
		t.Fatalf("OwnershipHistory: %v", err)
	}
	if len(history) != 1 || history[0].Status != domain.HandoffCancelled {
		t.Errorf("history = %+v, want the one Handoff still cancelled", history)
	}
	if _, err := h.Service.AcceptHandoff(ctx, ho.ID, pat.ID, nil); err == nil {
		t.Error("the returned person accepted a Handoff cancelled at their departure")
	}
}

// A person is shown by their Name, or by the part of their email before the @
// until they have one (CONTEXT.md: Name).
func TestAccountLabelIsTheNameOrTheEmailLocalPart(t *testing.T) {
	t.Parallel()

	named := domain.Account{Email: "ada.okafor@example.com", Name: "Ada Okafor"}
	unnamed := domain.Account{Email: "ada.okafor@example.com"}

	if got := named.Label(); got != "Ada Okafor" {
		t.Errorf("named Label() = %q, want Ada Okafor", got)
	}
	if got := unnamed.Label(); got != "ada.okafor" {
		t.Errorf("unnamed Label() = %q, want ada.okafor", got)
	}
	if got := named.LongLabel(); got != "Ada Okafor (ada.okafor@example.com)" {
		t.Errorf("named LongLabel() = %q, want Ada Okafor (ada.okafor@example.com)", got)
	}
	if got := unnamed.LongLabel(); got != "ada.okafor (ada.okafor@example.com)" {
		t.Errorf("unnamed LongLabel() = %q, want ada.okafor (ada.okafor@example.com)", got)
	}
}

// Where there is no hover — an export or an email body — a person's first
// mention reads Name (email), and later mentions use the Name alone.
func TestMentionsIntroduceEachPersonOnce(t *testing.T) {
	t.Parallel()

	ada := domain.Account{Email: "ada.okafor@example.com", Name: "Ada Okafor"}
	sam := domain.Account{Email: "sam@example.com"}
	var m domain.Mentions

	got := []string{m.Of(ada), m.Of(sam), m.Of(ada), m.Of(sam)}
	want := []string{"Ada Okafor (ada.okafor@example.com)", "sam (sam@example.com)", "Ada Okafor", "sam"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mention %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A Name, once set, is what the Account carries wherever it is read; a new
// Account has none.
func TestSetNameGivesAnAccountItsName(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	ada := h.SignIn("ada.okafor@example.com")
	if ada.Name != "" {
		t.Fatalf("new Account Name = %q, want none", ada.Name)
	}

	if err := h.Service.SetName(ctx, ada.ID, "Ada Okafor"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	got, err := h.Service.Account(ctx, ada.ID)
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if got.Name != "Ada Okafor" {
		t.Errorf("Name = %q, want Ada Okafor", got.Name)
	}
	if again := h.SignIn("ada.okafor@example.com"); again.Name != "Ada Okafor" {
		t.Errorf("signed-in Name = %q, want Ada Okafor", again.Name)
	}
}

// An email names one Account whatever its case, so a Departed person can't sign
// in again by changing the case of their email (CONTEXT.md: Account).
func TestSignInRefusesADifferentCaseOfADepartedEmail(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	freya := h.SignIn("freya.nilsen@example.com")
	if err := h.Service.MarkDeparted(ctx, boss.ID, freya.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	before := countAccounts(t, h)

	if _, err := h.Service.SignIn(ctx, "Freya.Nilsen@Example.com"); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("SignIn as a different case of a Departed email: err = %v, want ErrDeparted", err)
	}
	if after := countAccounts(t, h); after != before {
		t.Errorf("accounts = %d after the refused sign-in, want %d", after, before)
	}
}

func countAccounts(t *testing.T, h *testsupport.Harness) int {
	t.Helper()
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	return n
}

func TestSignInWithADifferentCaseReturnsTheExistingAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")

	again := h.SignIn("  Sam@Example.COM ")
	if again.ID != sam.ID {
		t.Errorf("sign-in as a different case made a new account: %d != %d", again.ID, sam.ID)
	}
}

func TestSignInStoresTheEmailTrimmedAndLowercased(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)

	acc := h.SignIn("  Freya.Nilsen@Example.com ")
	if acc.Email != "freya.nilsen@example.com" {
		t.Errorf("Email = %q, want freya.nilsen@example.com", acc.Email)
	}
}

// GOAL_TRACKER_ADMINS matches an email whatever its case, on either side.
func TestSignInSetsAdminFromConfigIgnoringCase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ configured, signIn string }{
		{"boss@example.com", "Boss@Example.com"},
		{"Boss@Example.com", "boss@example.com"},
	} {
		t.Run(tc.configured+" signs in as "+tc.signIn, func(t *testing.T) {
			h := testsupport.New(t, tc.configured)

			if acc := h.SignIn(tc.signIn); !acc.IsAdmin {
				t.Errorf("first sign-in as %q with %q configured: not an Admin", tc.signIn, tc.configured)
			}
		})
	}
}

// Every lookup by email finds the Account whatever case the caller typed
// (CONTEXT.md: Account).
func TestLookupsByEmailIgnoreCase(t *testing.T) {
	t.Parallel()

	t.Run("adding a Delegate", func(t *testing.T) {
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		pat := h.SignIn("pat@example.com")
		goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

		if err := h.Service.AddDelegateByEmail(context.Background(), sam.ID, goal.ID, "Pat@Example.com"); err != nil {
			t.Fatalf("AddDelegateByEmail: %v", err)
		}
		delegates, err := h.Service.ListDelegates(context.Background(), goal.ID)
		if err != nil || len(delegates) != 1 || delegates[0].ID != pat.ID {
			t.Errorf("Delegates = %+v (err %v), want pat", delegates, err)
		}
	})

	t.Run("starting a Handoff", func(t *testing.T) {
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		pat := h.SignIn("pat@example.com")
		goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

		handoff, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, "PAT@example.com", sam.ID)
		if err != nil {
			t.Fatalf("StartHandoffByEmail: %v", err)
		}
		if handoff.To.ID != pat.ID {
			t.Errorf("Handoff to %d, want %d", handoff.To.ID, pat.ID)
		}
	})

	t.Run("reassigning an Ownerless Goal", func(t *testing.T) {
		h := testsupport.New(t, "boss@example.com")
		ctx := context.Background()
		boss := h.SignIn("boss@example.com")
		sam := h.SignIn("sam@example.com")
		pat := h.SignIn("pat@example.com")
		goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
		if err := h.Service.MarkDeparted(ctx, boss.ID, sam.ID); err != nil {
			t.Fatalf("MarkDeparted: %v", err)
		}

		reassigned, err := h.Service.ReassignGoalByEmail(ctx, boss.ID, goal.ID, "Pat@Example.COM")
		if err != nil {
			t.Fatalf("ReassignGoalByEmail: %v", err)
		}
		if reassigned.Owner.ID != pat.ID {
			t.Errorf("Owner = %d, want %d", reassigned.Owner.ID, pat.ID)
		}
	})

	t.Run("naming an Action Item owner", func(t *testing.T) {
		h := testsupport.New(t)
		author := h.SignIn("author@example.com")
		owner := h.SignIn("owner@example.com")
		g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
		def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
		pub := h.PublishReport(author, def)

		item, err := h.Service.RaiseActionItemByEmail(context.Background(), author.ID, domain.RaiseActionItemInput{
			PublicationID: pub.ID, Text: "Chase the vendor.", DueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		}, "Owner@Example.com")
		if err != nil {
			t.Fatalf("RaiseActionItemByEmail: %v", err)
		}
		if item.Owner.ID != owner.ID {
			t.Errorf("Action Item owner = %d, want %d", item.Owner.ID, owner.ID)
		}
	})
}
