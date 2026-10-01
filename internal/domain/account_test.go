package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

func TestSignInCreatesAccountOnFirstUse(t *testing.T) {
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
	h := testsupport.New(t)

	first := h.SignIn("sam@example.com")
	second := h.SignIn("sam@example.com")

	if first.ID != second.ID {
		t.Errorf("second sign-in made a new account: %d != %d", first.ID, second.ID)
	}
}

func TestSignInSetsAdminFromConfig(t *testing.T) {
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
