package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
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

// issueLink issues a set-password link for emailAddr, failing the test on error.
func issueLink(t *testing.T, h *testsupport.Harness, emailAddr string) string {
	t.Helper()
	token, _, err := h.Service.IssuePasswordLink(t.Context(), emailAddr)
	if err != nil {
		t.Fatalf("IssuePasswordLink(%q): %v", emailAddr, err)
	}
	return token
}

// A set-password link works for 72 hours, once, and only while it's the
// Account's newest.
func TestASetPasswordLinkWorksOnceWithin72Hours(t *testing.T) {
	t.Parallel()

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		token, expires, err := h.Service.IssuePasswordLink(t.Context(), "sam@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if want := testsupport.Epoch.Add(72 * time.Hour); !expires.Equal(want) {
			t.Errorf("link expires %v, want %v", expires, want)
		}
		h.Clock.Advance(72*time.Hour - time.Second)
		if _, err := h.Service.PasswordLinkAccount(t.Context(), token); err != nil {
			t.Fatalf("a link a second short of 72 hours old: %v", err)
		}
		h.Clock.Advance(time.Second)
		if _, err := h.Service.PasswordLinkAccount(t.Context(), token); !errors.Is(err, domain.ErrLinkInvalid) {
			t.Errorf("PasswordLinkAccount after 72 hours: %v, want ErrLinkInvalid", err)
		}
		if _, err := h.Service.SetPassword(t.Context(), token, samPassword); !errors.Is(err, domain.ErrLinkInvalid) {
			t.Errorf("SetPassword after 72 hours: %v, want ErrLinkInvalid", err)
		}
	})

	t.Run("used", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		token := issueLink(t, h, "sam@example.com")
		if _, err := h.Service.SetPassword(t.Context(), token, samPassword); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Service.PasswordLinkAccount(t.Context(), token); !errors.Is(err, domain.ErrLinkInvalid) {
			t.Errorf("PasswordLinkAccount once used: %v, want ErrLinkInvalid", err)
		}
		if _, err := h.Service.SetPassword(t.Context(), token, "a different password"); !errors.Is(err, domain.ErrLinkInvalid) {
			t.Errorf("SetPassword once used: %v, want ErrLinkInvalid", err)
		}
	})

	t.Run("replaced", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		older := issueLink(t, h, "sam@example.com")
		newer := issueLink(t, h, "sam@example.com")
		if _, err := h.Service.SetPassword(t.Context(), older, samPassword); !errors.Is(err, domain.ErrLinkInvalid) {
			t.Errorf("SetPassword with a replaced link: %v, want ErrLinkInvalid", err)
		}
		if _, err := h.Service.SetPassword(t.Context(), newer, samPassword); err != nil {
			t.Errorf("SetPassword with the newer link: %v", err)
		}
	})
}

// A Departed person can't be given a set-password link, nor use one issued
// before they left: they can't sign in (CONTEXT.md: Departed).
func TestNoSetPasswordLinkForADepartedPerson(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	kimsLink := issueLink(t, h, "kim@example.com")
	for _, who := range []domain.Account{sam, kim} {
		if err := h.Service.MarkDeparted(t.Context(), boss.ID, who.ID); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, err := h.Service.IssuePasswordLink(t.Context(), "sam@example.com"); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("IssuePasswordLink for a Departed person: %v, want ErrDeparted", err)
	}
	if _, err := h.Service.SetPassword(t.Context(), kimsLink, samPassword); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("SetPassword with a link issued before departing: %v, want ErrDeparted", err)
	}
}

// A password is at least 12 characters, counted as runes so a short one in a
// wide script isn't let through by its bytes, and at most 1024 bytes. A
// refused password leaves the link working.
func TestAPasswordIsTwelveRunesToAThousandAndTwentyFourBytes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		password string
		ok       bool
	}{
		"11 letters":           {"abcdefghijk", false},
		"11 runes of 33 bytes": {"日本語日本語日本語日本", false},
		"12 runes":             {"日本語日本語日本語日本語", true},
		"1024 bytes":           {strings.Repeat("a", 1024), true},
		"1025 bytes":           {strings.Repeat("a", 1025), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t)
			token := issueLink(t, h, "sam@example.com")

			_, err := h.Service.SetPassword(t.Context(), token, tc.password)
			if tc.ok {
				if err != nil {
					t.Fatalf("SetPassword: %v", err)
				}
				return
			}
			if !errors.Is(err, domain.ErrPasswordRules) {
				t.Fatalf("SetPassword: %v, want ErrPasswordRules", err)
			}
			if strings.Contains(err.Error(), tc.password) {
				t.Errorf("the refusal %q repeats the password", err)
			}
			if _, err := h.Service.PasswordLinkAccount(t.Context(), token); err != nil {
				t.Errorf("a refused password used the link up: %v", err)
			}
		})
	}
}

// The password is stored as an argon2id PHC string, never as itself.
func TestAPasswordIsStoredAsAnArgon2idHash(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	acc, err := h.Service.SetPassword(t.Context(), issueLink(t, h, "sam@example.com"), samPassword)
	if err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := h.DB.QueryRowContext(t.Context(), `SELECT password_hash FROM account_passwords WHERE account_id = ?`, acc.ID).Scan(&stored); err != nil {
		t.Fatalf("read the stored hash: %v", err)
	}
	if !strings.HasPrefix(stored, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Errorf("stored hash %q isn't argon2id with m=65536,t=1,p=4", stored)
	}
	if strings.Contains(stored, samPassword) {
		t.Errorf("stored hash %q contains the password", stored)
	}
}

// A password sign-in with an unknown email, as someone with no password yet,
// or with the wrong password fails alike, and creates no Account.
func TestABadPasswordSignInFailsAlikeAndCreatesNoAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("kim@example.com") // has an Account, but no password
	if _, err := h.Service.SetPassword(t.Context(), issueLink(t, h, "sam@example.com"), samPassword); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct{ email, password string }{
		"unknown email":  {"nobody@example.com", samPassword},
		"no password":    {"kim@example.com", samPassword},
		"wrong password": {"sam@example.com", "not sam's password"},
	} {
		if _, err := h.Service.PasswordSignIn(t.Context(), tc.email, tc.password); !errors.Is(err, domain.ErrBadCredentials) {
			t.Errorf("%s: PasswordSignIn = %v, want ErrBadCredentials", name, err)
		}
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a sign-in with an unknown email created an Account (err %v)", err)
	}
}
