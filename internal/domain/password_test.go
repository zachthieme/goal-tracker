package domain_test

import (
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// samPassword is a password long enough to be accepted.
const samPassword = "correct horse battery"

// A set-password link lets the person choose a password, and they sign in
// with it afterwards.
func TestASetPasswordLinkLetsThePersonSignIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	token, _, err := h.Service.IssuePasswordLink(t.Context(), "sam@example.com")
	if err != nil {
		t.Fatalf("IssuePasswordLink: %v", err)
	}
	set, err := h.Service.SetPassword(t.Context(), token, samPassword)
	if err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	acc, err := h.Service.PasswordSignIn(t.Context(), "Sam@Example.com", samPassword)
	if err != nil {
		t.Fatalf("PasswordSignIn: %v", err)
	}
	if acc.ID != set.ID || acc.Email != "sam@example.com" {
		t.Errorf("signed in as %+v, want the Account the link set (%+v)", acc, set)
	}
}
