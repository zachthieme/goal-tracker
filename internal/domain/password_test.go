package domain_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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
	link, err := h.Service.IssueOperatorPasswordLink(t.Context(), "sam@example.com")
	if err != nil {
		t.Fatalf("IssueOperatorPasswordLink: %v", err)
	}
	set, err := h.Service.SetPassword(t.Context(), link.Token, samPassword)
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
	link, err := h.Service.IssueOperatorPasswordLink(t.Context(), emailAddr)
	if err != nil {
		t.Fatalf("IssueOperatorPasswordLink(%q): %v", emailAddr, err)
	}
	return link.Token
}

// Only an Admin may issue a set-password link: anyone else is refused with
// ErrNotAuthorized, and no Account or link is made. An Admin's link works as
// the operator's does.
func TestOnlyAnAdminMayIssueASetPasswordLink(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")

	if _, err := h.Service.IssuePasswordLink(t.Context(), sam.ID, "kim@example.com"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("a non-Admin's IssuePasswordLink for a new email: %v, want ErrNotAuthorized", err)
	}
	if acc, err := h.Service.AccountByEmail(t.Context(), "kim@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a non-Admin's IssuePasswordLink made an Account %+v (err %v)", acc, err)
	}
	if link, err := h.Service.IssuePasswordLink(t.Context(), sam.ID, "sam@example.com"); !errors.Is(err, domain.ErrNotAuthorized) || link.Token != "" {
		t.Errorf("a non-Admin's IssuePasswordLink for themself: %+v, %v, want no link and ErrNotAuthorized", link, err)
	}

	link, err := h.Service.IssuePasswordLink(t.Context(), ada.ID, "kim@example.com")
	if err != nil {
		t.Fatalf("an Admin's IssuePasswordLink: %v", err)
	}
	if link.Account.Email != "kim@example.com" || link.Account.ID == 0 {
		t.Errorf("the link is for %+v, want kim@example.com's new Account", link.Account)
	}
	if want := testsupport.Epoch.Add(72 * time.Hour); !link.Expires.Equal(want) {
		t.Errorf("the link expires %v, want %v", link.Expires, want)
	}
	set, err := h.Service.SetPassword(t.Context(), link.Token, samPassword)
	if err != nil {
		t.Fatalf("SetPassword with an Admin's link: %v", err)
	}
	if set.ID != link.Account.ID {
		t.Errorf("the link set the password of %+v, want %+v", set, link.Account)
	}
}

// A set-password link is only issued for a bare email address: anything else
// is refused with ErrNotAnEmail, and no Account is made for it.
func TestASetPasswordLinkIsOnlyForAnEmailAddress(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	for _, notAnEmail := range []string{"ada", "Ada <ada@x.com>", "a b@x.com"} {
		if _, err := h.Service.IssueOperatorPasswordLink(t.Context(), notAnEmail); !errors.Is(err, domain.ErrNotAnEmail) {
			t.Errorf("IssueOperatorPasswordLink(%q) = %v, want ErrNotAnEmail", notAnEmail, err)
		}
		if acc, err := h.Service.AccountByEmail(t.Context(), notAnEmail); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("IssueOperatorPasswordLink(%q) left an Account %+v (err %v)", notAnEmail, acc, err)
		}
	}
	issueLink(t, h, "ada@example.com")
	issueLink(t, h, "  ada@example.com  ")
}

// A set-password link works for 72 hours, once, and only while it's the
// Account's newest.
func TestASetPasswordLinkWorksOnceWithin72Hours(t *testing.T) {
	t.Parallel()

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		link, err := h.Service.IssueOperatorPasswordLink(t.Context(), "sam@example.com")
		if err != nil {
			t.Fatal(err)
		}
		token, expires := link.Token, link.Expires
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

	if _, err := h.Service.IssuePasswordLink(t.Context(), boss.ID, "sam@example.com"); !errors.Is(err, domain.ErrDeparted) {
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

// samWithPassword is a Harness, with adminEmails its Admins, where
// sam@example.com has samPassword.
func samWithPassword(t *testing.T, adminEmails ...string) *testsupport.Harness {
	t.Helper()
	h := testsupport.New(t, adminEmails...)
	if _, err := h.Service.SetPassword(t.Context(), issueLink(t, h, "sam@example.com"), samPassword); err != nil {
		t.Fatal(err)
	}
	return h
}

// failSignIns signs in as sam with a wrong password n times.
func failSignIns(t *testing.T, h *testsupport.Harness, n int) {
	t.Helper()
	for i := range n {
		if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", "not sam's password"); !errors.Is(err, domain.ErrBadCredentials) {
			t.Fatalf("wrong password %d: %v, want ErrBadCredentials", i+1, err)
		}
	}
}

// Five wrong passwords lock the Account for 15 minutes: the right one fails
// until they pass, then works.
func TestFiveWrongPasswordsLockTheAccountFor15Minutes(t *testing.T) {
	t.Parallel()

	h := samWithPassword(t)
	failSignIns(t, h, 5)

	h.Clock.Advance(15*time.Minute - time.Second)
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", samPassword); !errors.Is(err, domain.ErrBadCredentials) {
		t.Errorf("the right password during the lock: %v, want ErrBadCredentials", err)
	}
	h.Clock.Advance(time.Second)
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", samPassword); err != nil {
		t.Errorf("the right password once the lock passed: %v", err)
	}
}

// A sign-in whose context has ended before it starts gives up with the
// context's error, not ErrBadCredentials, and doesn't count toward the
// lockout: five wrong passwords afterwards still lock the Account, not four.
// It fails at the account lookup, so it never reaches the hash limiter;
// TestASignInThatGivesUpWaitingForAHashSlotCountsForNothing covers that.
func TestASignInThatGivesUpBeforeHashingCountsForNothing(t *testing.T) {
	t.Parallel()

	h := samWithPassword(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for name, email := range map[string]string{
		"has a password": "sam@example.com",
		"unknown email":  "nobody@example.com",
	} {
		_, err := h.Service.PasswordSignIn(ctx, email, "not sam's password")
		if !errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrBadCredentials) {
			t.Errorf("%s: PasswordSignIn with an ended context = %v, want context.Canceled", name, err)
		}
	}

	failSignIns(t, h, 4)
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", samPassword); err != nil {
		t.Fatalf("the right password after the given-up sign-in and 4 wrong ones: %v", err)
	}
	failSignIns(t, h, 5)
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", samPassword); !errors.Is(err, domain.ErrBadCredentials) {
		t.Errorf("the right password after 5 wrong ones: %v, want ErrBadCredentials", err)
	}
}

// A sign-in whose context ends while it waits for a hash slot gives up with
// the context's error, not ErrBadCredentials, on each path that hashes: an
// unknown email, an Account with no password, and an Account with one. The
// Account with one is locked, where a sign-in that went on without its hash
// would answer ErrBadCredentials, which the web layer counts toward the IP
// hold.
func TestASignInThatGivesUpWaitingForAHashSlotCountsForNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := samWithPassword(t)
		if _, err := h.Service.EnsureAccount(t.Context(), "ann@example.com"); err != nil {
			t.Fatal(err)
		}
		failSignIns(t, h, 5)

		for _, email := range []string{"nobody@example.com", "ann@example.com", "sam@example.com"} {
			release := domain.HoldHashSlots(h.Service)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error)
			go func() {
				_, err := h.Service.PasswordSignIn(ctx, email, "not sam's password")
				done <- err
			}()
			synctest.Wait()

			cancel()
			err := <-done
			release()
			if !errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrBadCredentials) {
				t.Errorf("%s: PasswordSignIn cancelled waiting for a hash slot = %v, want context.Canceled", email, err)
			}
		}
	})
}

// A successful sign-in clears the wrong passwords before it, so four, a
// success, then four more doesn't lock the Account.
func TestASuccessfulSignInResetsTheWrongPasswordCount(t *testing.T) {
	t.Parallel()

	h := samWithPassword(t)
	failSignIns(t, h, 4)
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", samPassword); err != nil {
		t.Fatalf("the right password after 4 wrong ones: %v", err)
	}
	failSignIns(t, h, 4)
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", samPassword); err != nil {
		t.Errorf("the right password after 4 more wrong ones: %v", err)
	}
}

// Using a new set-password link lifts a lock at once: how an Admin unlocks
// someone.
func TestANewPasswordLiftsTheLock(t *testing.T) {
	t.Parallel()

	h := samWithPassword(t)
	failSignIns(t, h, 5)
	if _, err := h.Service.SetPassword(t.Context(), issueLink(t, h, "sam@example.com"), "a brand new password"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", "a brand new password"); err != nil {
		t.Errorf("the new password while the old lock would still hold: %v", err)
	}
}

// A Departed person with the right password is told they've departed; with a
// wrong one, they fail like anyone (CONTEXT.md: Departed).
func TestADepartedPersonsPasswordSignInSaysTheyDeparted(t *testing.T) {
	t.Parallel()

	h := samWithPassword(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam, err := h.Service.AccountByEmail(t.Context(), "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", samPassword); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("the right password: %v, want ErrDeparted", err)
	}
	if _, err := h.Service.PasswordSignIn(t.Context(), "sam@example.com", "not sam's password"); !errors.Is(err, domain.ErrBadCredentials) {
		t.Errorf("a wrong password: %v, want ErrBadCredentials", err)
	}
}

// Of two uses of one set-password link at once, only one sets the password.
func TestASetPasswordLinkUsedTwiceAtOnceWorksOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	token := issueLink(t, h, "sam@example.com")

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Go(func() {
			_, results[i] = h.Service.SetPassword(t.Context(), token, fmt.Sprintf("password number %d", i))
		})
	}
	wg.Wait()

	if results[0] == nil && results[1] == nil {
		t.Fatal("both uses of the link set the password")
	}
	if results[0] != nil && results[1] != nil {
		t.Fatalf("neither use of the link set the password: %v; %v", results[0], results[1])
	}
}
