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

// An actor ID that names no Account is no Admin: it may not mark a person
// Departed, and the refused attempt leaves them present.
func TestMarkDepartedRefusesAnActorWithNoAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	nobody := boss.ID + sam.ID + 1000

	if err := h.Service.MarkDeparted(ctx, nobody, sam.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("MarkDeparted by an actor with no Account: err = %v, want ErrNotAuthorized", err)
	}
	if acc, err := h.Service.Account(ctx, sam.ID); err != nil || acc.Departed {
		t.Errorf("Sam after a refused MarkDeparted = %+v, %v; want present", acc, err)
	}
}

// MarkDeparted and MarkReturned look the person up before asking who is
// acting, so an account id that names no Account is refused "account does not
// exist" whoever asks, a non-Admin included, and nothing changes.
func TestMarkDepartedAndReturnedRefuseAMissingAccountWhoeverAsks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	nobody := boss.ID + pat.ID + 1000

	for name, mark := range map[string]func(actorID, accountID int64) error{
		"MarkDeparted": func(actorID, accountID int64) error { return h.Service.MarkDeparted(ctx, actorID, accountID) },
		"MarkReturned": func(actorID, accountID int64) error { return h.Service.MarkReturned(ctx, actorID, accountID) },
	} {
		err := mark(pat.ID, nobody)
		if !errors.Is(err, domain.ErrValidation) || domain.Reason(err) != "account does not exist" {
			t.Errorf("%s by a non-Admin of no Account: err %v, want ErrValidation \"account does not exist\"", name, err)
		}
	}
	departed, err := h.Service.DepartedAccounts(ctx)
	if err != nil || len(departed) != 0 {
		t.Errorf("Departed Accounts after the refused marks = %+v (err %v), want none", departed, err)
	}
	for _, acc := range []domain.Account{boss, pat} {
		if got, err := h.Service.Account(ctx, acc.ID); err != nil || got != acc {
			t.Errorf("%s after the refused marks = %+v (err %v), want %+v", acc.Email, got, err, acc)
		}
	}
}

// A Departed Admin can't act (CONTEXT.md: Departed), so every Admin command
// refuses them as the rule for an Admin does — marking people departed or
// returned, defining Dimensions and Fields — and the refused attempts change
// nothing.
func TestAdminCommandsRefuseADepartedAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com", "ada@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	h.MarkDeparted(boss, pat)
	h.MarkDeparted(boss, ada)

	if err := h.Service.MarkDeparted(ctx, ada.ID, sam.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("MarkDeparted by a Departed Admin: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.MarkReturned(ctx, ada.ID, pat.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("MarkReturned by a Departed Admin: err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.CreateDimension(ctx, ada.ID, "Pillar", []string{"Growth"}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("CreateDimension by a Departed Admin: err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.CreateField(ctx, ada.ID, "Budget", domain.FieldNumber, "USD"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("CreateField by a Departed Admin: err = %v, want ErrNotAuthorized", err)
	}
	if acc, err := h.Service.Account(ctx, sam.ID); err != nil || acc.Departed {
		t.Errorf("Sam after a refused MarkDeparted = %+v, %v; want present", acc, err)
	}
	if acc, err := h.Service.Account(ctx, pat.ID); err != nil || !acc.Departed {
		t.Errorf("Pat after a refused MarkReturned = %+v, %v; want Departed", acc, err)
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

// adminEmails is who Admins lists, in its order.
func adminEmails(t *testing.T, h *testsupport.Harness) []string {
	t.Helper()
	admins, err := h.Service.Admins(context.Background())
	if err != nil {
		t.Fatalf("Admins: %v", err)
	}
	var out []string
	for _, a := range admins {
		out = append(out, a.Email)
	}
	return out
}

// An Admin makes another person an Admin by their email, whatever its case or
// surrounding spaces, and Admins then lists them, by Label (CONTEXT.md: Admin).
func TestMakeAdminGivesAPersonTheAdminRoleByEmail(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignInNamed("sam@example.com", "Abe Sampson")

	got, err := h.Service.MakeAdmin(ctx, boss.ID, "  Sam@Example.COM ")
	if err != nil {
		t.Fatalf("MakeAdmin: %v", err)
	}
	if got.ID != sam.ID || !got.IsAdmin {
		t.Errorf("MakeAdmin = %+v, want Sam's Account as an Admin", got)
	}
	if want := []string{"sam@example.com", "boss@example.com"}; !slices.Equal(adminEmails(t, h), want) {
		t.Errorf("Admins = %q, want %q", adminEmails(t, h), want)
	}
	if acc, err := h.Service.Account(ctx, sam.ID); err != nil || !acc.IsAdmin {
		t.Errorf("Sam's Account after MakeAdmin = %+v, %v; want an Admin", acc, err)
	}
}

// MakeAdmin never creates an Account: an email no one has, a typo say, is
// refused and leaves no stray Account behind.
func TestMakeAdminRefusesAnEmailWithNoAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	before := countAccounts(t, h)

	if _, err := h.Service.MakeAdmin(ctx, boss.ID, "nobody@example.com"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("MakeAdmin for an email with no Account: err = %v, want ErrValidation", err)
	}
	if after := countAccounts(t, h); after != before {
		t.Errorf("accounts = %d after the refused MakeAdmin, want %d", after, before)
	}
}

// A Departed person can't be made an Admin: they can't sign in or act
// (CONTEXT.md: Departed).
func TestMakeAdminRefusesADepartedPerson(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	if err := h.Service.MarkDeparted(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if _, err := h.Service.MakeAdmin(ctx, boss.ID, "sam@example.com"); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("MakeAdmin for a Departed person: err = %v, want ErrDeparted", err)
	}
	if acc, err := h.Service.Account(ctx, sam.ID); err != nil || acc.IsAdmin {
		t.Errorf("Sam's Account after the refused MakeAdmin = %+v, %v; want a non-Admin", acc, err)
	}
}

// Making an Admin an Admin again is no error: they simply stay one.
func TestMakeAdminOfAnAdminIsNoError(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com", "ada@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	ada := h.SignIn("ada@example.com")

	got, err := h.Service.MakeAdmin(ctx, boss.ID, "ada@example.com")
	if err != nil || got.ID != ada.ID || !got.IsAdmin {
		t.Errorf("MakeAdmin of an Admin = %+v, %v; want Ada's Account as an Admin, no error", got, err)
	}
	if want := []string{"ada@example.com", "boss@example.com"}; !slices.Equal(adminEmails(t, h), want) {
		t.Errorf("Admins = %q, want %q", adminEmails(t, h), want)
	}
}

// Only an Admin may give or take the Admin role; the refusal is enforced in the
// domain, not just hidden in the UI, and changes nothing.
func TestMakeAndRemoveAdminOnlyByAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	h.SignIn("sam@example.com")

	if _, err := h.Service.MakeAdmin(ctx, pat.ID, "sam@example.com"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin MakeAdmin err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RemoveAdmin(ctx, pat.ID, boss.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RemoveAdmin err = %v, want ErrNotAuthorized", err)
	}
	if want := []string{"boss@example.com"}; !slices.Equal(adminEmails(t, h), want) {
		t.Errorf("Admins after the refusals = %q, want %q", adminEmails(t, h), want)
	}
}

// An Admin takes the Admin role from another Admin, who is then no longer
// listed. Taking it from someone who isn't an Admin changes nothing.
func TestRemoveAdminTakesTheRoleFromAnotherAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com", "ada@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")

	if err := h.Service.RemoveAdmin(ctx, boss.ID, ada.ID); err != nil {
		t.Fatalf("RemoveAdmin: %v", err)
	}
	if want := []string{"boss@example.com"}; !slices.Equal(adminEmails(t, h), want) {
		t.Errorf("Admins = %q, want %q", adminEmails(t, h), want)
	}
	if err := h.Service.RemoveAdmin(ctx, boss.ID, sam.ID); err != nil {
		t.Errorf("RemoveAdmin of a non-Admin: %v, want no error", err)
	}
}

// No Admin can take the role from themselves, so there is always at least one
// (CONTEXT.md: Admin); nor from an Account that doesn't exist.
func TestRemoveAdminRefusesYourselfAndAnUnknownAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")

	if err := h.Service.RemoveAdmin(ctx, boss.ID, boss.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("RemoveAdmin of yourself: err = %v, want ErrValidation", err)
	}
	if want := []string{"boss@example.com"}; !slices.Equal(adminEmails(t, h), want) {
		t.Errorf("Admins after removing yourself = %q, want %q", adminEmails(t, h), want)
	}
	if err := h.Service.RemoveAdmin(ctx, boss.ID, boss.ID+1000); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("RemoveAdmin of an unknown account: err = %v, want ErrValidation", err)
	}
}

// Two first OIDC sign-ins can race to link one new subject to different
// Accounts. The one whose link loses on the unique subject is refused as a
// subject conflict, not with a raw database error (#442).
func TestSignInWithOIDCRefusesALinkThatLosesARace(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	// Between this sign-in's lookup of the subject and its link, the rival's
	// sign-in links the subject first.
	if _, err := h.DB.Exec(`CREATE TRIGGER rival_links_first BEFORE UPDATE OF oidc_subject ON accounts
		WHEN OLD.email = 'sam@example.com'
		BEGIN UPDATE accounts SET oidc_subject = NEW.oidc_subject WHERE email = 'kim@example.com'; END`); err != nil {
		t.Fatalf("install racing trigger: %v", err)
	}

	acc, err := h.Service.SignInWithOIDC(ctx, "subject-1", "sam@example.com")
	var conflict *domain.OIDCSubjectConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("SignInWithOIDC = %+v, %v; want an *OIDCSubjectConflictError", acc, err)
	}
	if acc.ID != 0 {
		t.Errorf("SignInWithOIDC returned Account %d with the refusal, want none", acc.ID)
	}
	if conflict.AccountID != sam.ID {
		t.Errorf("conflict names Account %d, want sam's %d", conflict.AccountID, sam.ID)
	}
}
