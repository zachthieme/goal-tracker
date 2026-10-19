package domain_test

import (
	"context"
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// askLine signs in an Admin and the given people, each the Manager of the one
// before it, and returns them freshly read, so each carries its ManagerID.
func askLine(t *testing.T, h *testsupport.Harness, emails ...string) (admin domain.Account, line []domain.Account) {
	t.Helper()
	admin = h.SignIn("admin@example.com")
	for _, e := range emails {
		line = append(line, h.SignIn(e))
	}
	for i := 0; i+1 < len(line); i++ {
		h.SetManager(line[i], line[i+1])
	}
	for i := range line {
		line[i] = askReloadAccount(t, h, line[i])
	}
	return admin, line
}

func askReloadAccount(t *testing.T, h *testsupport.Harness, a domain.Account) domain.Account {
	t.Helper()
	got, err := h.Service.Account(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Account(%s): %v", a.Email, err)
	}
	return got
}

func askMarkDeparted(t *testing.T, h *testsupport.Harness, admin domain.Account, people ...domain.Account) {
	t.Helper()
	for _, p := range people {
		if err := h.Service.MarkDeparted(context.Background(), admin.ID, p.ID); err != nil {
			t.Fatalf("MarkDeparted(%s): %v", p.Email, err)
		}
	}
}

func askAddresseeOf(t *testing.T, h *testsupport.Harness, owner domain.Account) *domain.Account {
	t.Helper()
	got, err := h.Service.AskAddressee(context.Background(), askReloadAccount(t, h, owner))
	if err != nil {
		t.Fatalf("AskAddressee(%s): %v", owner.Email, err)
	}
	return got
}

func askWantAddressee(t *testing.T, got *domain.Account, want domain.Account) {
	t.Helper()
	if got == nil {
		t.Fatalf("addressee = none, want %s", want.Email)
	}
	if got.ID != want.ID {
		t.Errorf("addressee = %s, want %s", got.Email, want.Email)
	}
}

// An ask for help is addressed to the Owner's Manager (CONTEXT.md: Ask for
// help).
func TestAskAddresseeIsTheOwnersManager(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	_, line := askLine(t, h, "sam@example.com", "mia@example.com", "vp@example.com")

	askWantAddressee(t, askAddresseeOf(t, h, line[0]), line[1])
}

// Past anyone who has left the company, the ask goes to the next Manager
// above: one Departed Manager, or two in a row.
func TestAskAddresseeSkipsDepartedManagers(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin, line := askLine(t, h, "sam@example.com", "mia@example.com", "dir@example.com", "vp@example.com")

	askMarkDeparted(t, h, admin, line[1])
	askWantAddressee(t, askAddresseeOf(t, h, line[0]), line[2])

	askMarkDeparted(t, h, admin, line[2])
	askWantAddressee(t, askAddresseeOf(t, h, line[0]), line[3])
}

// An Owner with no one above them who hasn't left gives no addressee: no
// Manager at all, only Departed ones, their own Manager, or a two-person cycle
// with the other person Departed (ADR 0008: a Chain stops at anyone it has
// already reached).
func TestAskAddresseeNoneAboveTheOwner(t *testing.T) {
	t.Parallel()

	t.Run("no Manager", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t, "admin@example.com")
		_, line := askLine(t, h, "sam@example.com")
		if got := askAddresseeOf(t, h, line[0]); got != nil {
			t.Errorf("addressee = %s, want none", got.Email)
		}
	})
	t.Run("only Departed Managers above", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t, "admin@example.com")
		admin, line := askLine(t, h, "sam@example.com", "mia@example.com", "vp@example.com")
		askMarkDeparted(t, h, admin, line[1], line[2])
		if got := askAddresseeOf(t, h, line[0]); got != nil {
			t.Errorf("addressee = %s, want none", got.Email)
		}
	})
	t.Run("their own Manager", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t, "admin@example.com")
		_, line := askLine(t, h, "sam@example.com")
		h.SetManager(line[0], line[0])
		if got := askAddresseeOf(t, h, line[0]); got != nil {
			t.Errorf("addressee = %s, want none", got.Email)
		}
	})
	t.Run("a cycle with the other person Departed", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t, "admin@example.com")
		admin, line := askLine(t, h, "sam@example.com", "mia@example.com")
		h.SetManager(line[1], line[0])
		askMarkDeparted(t, h, admin, line[1])
		if got := askAddresseeOf(t, h, line[0]); got != nil {
			t.Errorf("addressee = %s, want none", got.Email)
		}
	})
}

func askGoalTitles(asks []domain.AskForHelp) []string {
	var out []string
	for _, a := range asks {
		out = append(out, a.Goal.Title)
	}
	return out
}

// The open asks are those of Active Goals whose latest Check-in is Yellow or
// Red and asks for help, Red first. A later Check-in that doesn't ask, a
// Green one, or one that puts the Goal On Hold closes it.
func TestOpenAsksForHelp(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ctx := context.Background()
	_, line := askLine(t, h, "sam@example.com", "mia@example.com")
	sam, mia := line[0], line[1]

	yellow := h.ActiveGoal(sam, "a yellow ask", "So what.")
	h.AskForHelp(sam, yellow.ID, domain.HealthYellow, "Short a reviewer.", "Borrow one.", pathDate)
	red := h.ActiveGoal(sam, "Z red ask", "So what.")
	h.AskForHelp(sam, red.ID, domain.HealthRed, "Blocked.", "Need budget.", pathDate)

	unasked := h.ActiveGoal(sam, "Unasked since", "So what.")
	h.AskForHelp(sam, unasked.ID, domain.HealthYellow, "Short.", "Borrow one.", pathDate)
	h.Checkin(sam, unasked.ID, domain.HealthYellow, "Still short.", "Hire.", pathDate)

	green := h.ActiveGoal(sam, "Back to Green", "So what.")
	h.AskForHelp(sam, green.ID, domain.HealthYellow, "Short.", "Borrow one.", pathDate)
	h.Checkin(sam, green.ID, domain.HealthGreen, "Fixed.", "", pathDate)

	held := h.ActiveGoal(sam, "Put On Hold", "So what.")
	h.AskForHelp(sam, held.ID, domain.HealthRed, "Blocked.", "Need budget.", pathDate)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: held.ID, AuthorID: sam.ID, Status: "Pausing.",
		Lifecycle: domain.LifecycleOnHold, LifecycleReason: "No budget this quarter.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}

	asks, err := h.Service.OpenAsksForHelp(ctx)
	if err != nil {
		t.Fatalf("OpenAsksForHelp: %v", err)
	}
	got := askGoalTitles(asks)
	if want := []string{"Z red ask", "a yellow ask"}; !slices.Equal(got, want) {
		t.Fatalf("open asks = %q, want %q", got, want)
	}
	if asks[0].Health != domain.HealthRed || asks[0].PathToGreen != "Need budget." {
		t.Errorf("red ask = %s %q, want Red \"Need budget.\"", asks[0].Health, asks[0].PathToGreen)
	}
	askWantAddressee(t, asks[0].Addressee, mia)

	if _, ok, err := h.Service.OpenAskForHelp(ctx, red.ID); err != nil || !ok {
		t.Errorf("OpenAskForHelp(red) = ok %v, err %v; want the open ask", ok, err)
	}
	for _, g := range []domain.Goal{unasked, green, held} {
		if _, ok, err := h.Service.OpenAskForHelp(ctx, g.ID); err != nil || ok {
			t.Errorf("OpenAskForHelp(%q) = ok %v, err %v; want none", g.Title, ok, err)
		}
	}
}

// A Manager's asks are only those addressed to them; a Departed Manager, who
// is skipped over, has none.
func TestAsksForHelpTo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ctx := context.Background()
	admin, line := askLine(t, h, "sam@example.com", "mia@example.com", "vp@example.com")
	sam, mia, vp := line[0], line[1], line[2]
	_, other := askLine(t, h, "pat@example.com", "dee@example.com", "vp@example.com")
	pat, dee := other[0], other[1]

	mine := h.ActiveGoal(sam, "Sam's ask", "So what.")
	h.AskForHelp(sam, mine.ID, domain.HealthYellow, "Short.", "Borrow one.", pathDate)
	theirs := h.ActiveGoal(pat, "Pat's ask", "So what.")
	h.AskForHelp(pat, theirs.ID, domain.HealthRed, "Blocked.", "Need budget.", pathDate)

	for _, tc := range []struct {
		who  domain.Account
		want []string
	}{
		{mia, []string{"Sam's ask"}},
		{dee, []string{"Pat's ask"}},
		{vp, nil},
		{sam, nil},
	} {
		asks, err := h.Service.AsksForHelpTo(ctx, tc.who.ID)
		if err != nil {
			t.Fatalf("AsksForHelpTo(%s): %v", tc.who.Email, err)
		}
		if got := askGoalTitles(asks); !slices.Equal(got, tc.want) {
			t.Errorf("AsksForHelpTo(%s) = %q, want %q", tc.who.Email, got, tc.want)
		}
	}

	askMarkDeparted(t, h, admin, mia)
	if asks, err := h.Service.AsksForHelpTo(ctx, mia.ID); err != nil || len(asks) != 0 {
		t.Errorf("AsksForHelpTo(Departed mia) = %q, %v; want none", askGoalTitles(asks), err)
	}
	if asks, err := h.Service.AsksForHelpTo(ctx, vp.ID); err != nil || !slices.Equal(askGoalTitles(asks), []string{"Sam's ask"}) {
		t.Errorf("AsksForHelpTo(vp) past Departed mia = %q, %v; want Sam's ask", askGoalTitles(asks), err)
	}
}

// An answer alone doesn't close an ask: the addressee's Comment on the Goal
// leaves it open (CONTEXT.md: Ask for help).
func TestAskForHelpStaysOpenAfterAnAnswer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ctx := context.Background()
	_, line := askLine(t, h, "sam@example.com", "mia@example.com")
	sam, mia := line[0], line[1]
	g := h.ActiveGoal(sam, "Sam's ask", "So what.")
	h.AskForHelp(sam, g.ID, domain.HealthYellow, "Short.", "Borrow one.", pathDate)

	if _, err := h.Service.AddGoalComment(ctx, mia.ID, g.ID, "Take Lee for two weeks."); err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	asks, err := h.Service.AsksForHelpTo(ctx, mia.ID)
	if err != nil {
		t.Fatalf("AsksForHelpTo: %v", err)
	}
	if got := askGoalTitles(asks); !slices.Equal(got, []string{"Sam's ask"}) {
		t.Errorf("asks after an answer = %q, want the ask still open", got)
	}
}
