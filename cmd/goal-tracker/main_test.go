package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
)

func TestAppClockIsTheRealClockWhenStartAtIsUnset(t *testing.T) {
	t.Parallel()

	clk, err := appClock("")
	if err != nil {
		t.Fatal(err)
	}
	if clk != (clock.Real{}) {
		t.Fatalf("appClock(\"\") = %#v, want clock.Real{}", clk)
	}
}

func TestAppClockStartsAtAValidStartAt(t *testing.T) {
	t.Parallel()

	clk, err := appClock("2026-10-05T18:05:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := clk.(*clock.Offset); !ok {
		t.Fatalf("appClock = %#v, want a *clock.Offset", clk)
	}
	want := time.Date(2026, 10, 5, 18, 5, 0, 0, time.UTC)
	if got := clk.Now(); got.Before(want) || got.Sub(want) > time.Second {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}

func TestAppClockRejectsAMalformedStartAt(t *testing.T) {
	t.Parallel()

	_, err := appClock("5 Oct 2026")
	if err == nil {
		t.Fatal("appClock accepted a value that isn't RFC 3339")
	}
	if !strings.Contains(err.Error(), "GOAL_TRACKER_START_AT") {
		t.Fatalf("error %q doesn't name GOAL_TRACKER_START_AT", err)
	}
}

// oidcVars are the variables that switch organization sign-in on.
var oidcVars = []string{"GOAL_TRACKER_OIDC_ISSUER", "GOAL_TRACKER_OIDC_CLIENT_ID", "GOAL_TRACKER_OIDC_CLIENT_SECRET"}

// clearAuthEnv unsets the sign-in and directory variables for the test, so the host's own
// settings don't leak in.
func clearAuthEnv(t *testing.T) {
	t.Helper()
	for _, v := range slices.Concat(oidcVars, directoryVars, []string{directoryFileVar, "GOAL_TRACKER_SIGNIN", "GOAL_TRACKER_SESSION_KEY", "GOAL_TRACKER_BASE_URL"}) {
		t.Setenv(v, "")
	}
}

// The sign-in method has to be stated: without GOAL_TRACKER_SIGNIN, or with a
// method it doesn't know, startup stops and lists the ones it accepts.
func TestTheSignInMethodIsRequired(t *testing.T) {
	for name, value := range map[string]string{"unset": "", "unknown": "sso"} {
		t.Run(name, func(t *testing.T) {
			clearAuthEnv(t)
			t.Setenv("GOAL_TRACKER_SIGNIN", value)

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig accepted GOAL_TRACKER_SIGNIN=%q", value)
			}
			for _, want := range []string{"GOAL_TRACKER_SIGNIN", "oidc", "dev"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q doesn't name %s", err, want)
				}
			}
		})
	}
}

// devEnv is clearAuthEnv with the development sign-in form chosen.
func devEnv(t *testing.T) {
	t.Helper()
	clearAuthEnv(t)
	t.Setenv("GOAL_TRACKER_SIGNIN", "dev")
}

// oidcEnv is clearAuthEnv with organization sign-in chosen and a session key
// set, as it requires; the OIDC variables are left to the test.
func oidcEnv(t *testing.T) {
	t.Helper()
	clearAuthEnv(t)
	t.Setenv("GOAL_TRACKER_SIGNIN", "oidc")
	t.Setenv("GOAL_TRACKER_SESSION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
}

func TestPartialOIDCConfigNamesWhatsMissing(t *testing.T) {
	oidcEnv(t)
	t.Setenv("GOAL_TRACKER_OIDC_ISSUER", "http://localhost:9000/application/o/goal-tracker/")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig accepted an OIDC issuer without a client ID or secret")
	}
	for _, missing := range oidcVars[1:] {
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("error %q doesn't name %s", err, missing)
		}
	}
	if strings.Contains(err.Error(), "GOAL_TRACKER_OIDC_ISSUER") {
		t.Errorf("error %q names the issuer, which is set", err)
	}
}

// setOIDCVars sets all three OIDC variables.
func setOIDCVars(t *testing.T) {
	t.Helper()
	t.Setenv("GOAL_TRACKER_OIDC_ISSUER", "http://localhost:9000/application/o/goal-tracker/")
	t.Setenv("GOAL_TRACKER_OIDC_CLIENT_ID", "goal-tracker")
	t.Setenv("GOAL_TRACKER_OIDC_CLIENT_SECRET", "secret")
}

func TestFullOIDCConfigRedirectsToTheBaseURLsCallback(t *testing.T) {
	oidcEnv(t)
	setOIDCVars(t)
	t.Setenv("GOAL_TRACKER_BASE_URL", "https://goals.example.com/")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.signIn != signInOIDC {
		t.Errorf("sign-in method %q, want %q", cfg.signIn, signInOIDC)
	}
	if cfg.oidc == nil {
		t.Fatal("OIDC is off with all three variables set")
	}
	if got, want := cfg.oidc.RedirectURL, "https://goals.example.com/auth/callback"; got != want {
		t.Errorf("redirect URL %q, want %q", got, want)
	}
}

// Organization sign-in needs a session key, so sessions survive a restart and
// every instance signs them alike.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestOIDCSignInNeedsASessionKey(t *testing.T) {
	oidcEnv(t)
	setOIDCVars(t)
	t.Setenv("GOAL_TRACKER_SESSION_KEY", "")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig accepted GOAL_TRACKER_SIGNIN=oidc without a session key")
	}
	if !strings.Contains(err.Error(), "GOAL_TRACKER_SESSION_KEY") {
		t.Errorf("error %q doesn't name GOAL_TRACKER_SESSION_KEY", err)
	}
}

// With the development form chosen, any OIDC variable stops startup, so a
// half-meant organization sign-in can't fall back to the open form.
func TestDevSignInRefusesOIDCVariables(t *testing.T) {
	for _, set := range oidcVars {
		t.Run(set, func(t *testing.T) {
			devEnv(t)
			t.Setenv(set, "value")

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig accepted GOAL_TRACKER_SIGNIN=dev with %s set", set)
			}
			if !strings.Contains(err.Error(), set) {
				t.Errorf("error %q doesn't name %s", err, set)
			}
		})
	}
}

// An OIDC client secret of only whitespace counts as unset, and organization
// sign-in names it as missing.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestABlankOIDCClientSecretIsMissing(t *testing.T) {
	oidcEnv(t)
	setOIDCVars(t)
	t.Setenv("GOAL_TRACKER_OIDC_CLIENT_SECRET", "  ")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig accepted an OIDC client secret of only whitespace")
	}
	if !strings.Contains(err.Error(), "GOAL_TRACKER_OIDC_CLIENT_SECRET") {
		t.Errorf("error %q doesn't name GOAL_TRACKER_OIDC_CLIENT_SECRET", err)
	}
}

// OIDC variables of only whitespace aren't set, so the development form
// doesn't refuse them.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestDevSignInAcceptsBlankOIDCVariables(t *testing.T) {
	devEnv(t)
	for _, v := range oidcVars {
		t.Setenv(v, "  ")
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig refused GOAL_TRACKER_SIGNIN=dev with blank OIDC variables: %v", err)
	}
	if cfg.oidc != nil {
		t.Errorf("OIDC is on with the development form chosen: %+v", cfg.oidc)
	}
}

//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestDevSignInLeavesTheDevelopmentForm(t *testing.T) {
	devEnv(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.signIn != signInDev {
		t.Errorf("sign-in method %q, want %q", cfg.signIn, signInDev)
	}
	if cfg.oidc != nil {
		t.Errorf("OIDC is on with the development form chosen: %+v", cfg.oidc)
	}
}

func TestABadSessionKeyIsNamed(t *testing.T) {
	for name, key := range map[string]string{
		"short":      base64.StdEncoding.EncodeToString(make([]byte, 31)),
		"not base64": strings.Repeat("!", 64),
	} {
		t.Run(name, func(t *testing.T) {
			devEnv(t)
			t.Setenv("GOAL_TRACKER_SESSION_KEY", key)

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig accepted GOAL_TRACKER_SESSION_KEY=%q", key)
			}
			if !strings.Contains(err.Error(), "GOAL_TRACKER_SESSION_KEY") {
				t.Errorf("error %q doesn't name GOAL_TRACKER_SESSION_KEY", err)
			}
		})
	}
}

func TestAGoodSessionKeyIsUsed(t *testing.T) {
	devEnv(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	t.Setenv("GOAL_TRACKER_SESSION_KEY", base64.StdEncoding.EncodeToString(key))

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.sessionKey) != string(key) {
		t.Errorf("session key %q, want %q", cfg.sessionKey, key)
	}
}

// directoryVars are the variables that switch the directory sync on.
var directoryVars = []string{"GOAL_TRACKER_DIRECTORY_URL", "GOAL_TRACKER_DIRECTORY_TOKEN"}

// Setting one of the directory variables without the other stops startup,
// naming the one that's missing.
func TestPartialDirectoryConfigNamesWhatsMissing(t *testing.T) {
	for i, set := range directoryVars {
		missing := directoryVars[1-i]
		t.Run(set, func(t *testing.T) {
			devEnv(t)
			t.Setenv(set, "value")

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig accepted %s without %s", set, missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error %q doesn't name %s", err, missing)
			}
			if strings.Contains(err.Error(), set) {
				t.Errorf("error %q names %s, which is set", err, set)
			}
		})
	}
}

// With neither directory variable set there is no sync; with both, it reads
// the directory at the URL with the token.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestDirectorySyncIsOnlyOnWithBothVariables(t *testing.T) {
	devEnv(t)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.directory != nil {
		t.Errorf("the directory sync is on with neither variable set: %+v", cfg.directory)
	}

	t.Setenv("GOAL_TRACKER_DIRECTORY_URL", "http://localhost:9000")
	t.Setenv("GOAL_TRACKER_DIRECTORY_TOKEN", "token")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.directory == nil || cfg.directory.url != "http://localhost:9000" || cfg.directory.token != "token" {
		t.Errorf("directory = %+v, want the URL and token set", cfg.directory)
	}
}

// directoryFileVar names the CSV file the directory sync can read instead of
// Authentik. It's kept out of directoryVars, which are Authentik's pair.
const directoryFileVar = "GOAL_TRACKER_DIRECTORY_FILE"

// The file alone switches the directory sync on, reading that file.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestDirectorySyncReadsTheFileAlone(t *testing.T) {
	devEnv(t)
	t.Setenv(directoryFileVar, "/etc/goal-tracker/people.csv")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.directory == nil || cfg.directory.file != "/etc/goal-tracker/people.csv" || cfg.directory.url != "" {
		t.Errorf("directory = %+v, want the file and no Authentik URL", cfg.directory)
	}
}

// The file and Authentik are two sources; setting the file with either
// Authentik variable stops startup, naming both.
func TestDirectoryFileWithAuthentikNamesBothSources(t *testing.T) {
	for _, v := range directoryVars {
		t.Run(v, func(t *testing.T) {
			devEnv(t)
			t.Setenv(directoryFileVar, "people.csv")
			t.Setenv(v, "value")

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig accepted %s with %s", directoryFileVar, v)
			}
			for _, name := range []string{directoryFileVar, v} {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error %q doesn't name %s", err, name)
				}
			}
		})
	}
}

// A directory token of only whitespace counts as unset, so the URL without it
// stops startup, naming the token, instead of failing every sync.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestABlankDirectoryTokenIsMissing(t *testing.T) {
	devEnv(t)
	t.Setenv("GOAL_TRACKER_DIRECTORY_URL", "http://localhost:9000")
	t.Setenv("GOAL_TRACKER_DIRECTORY_TOKEN", " \t")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig accepted a directory token of only whitespace")
	}
	if !strings.Contains(err.Error(), "missing GOAL_TRACKER_DIRECTORY_TOKEN") {
		t.Errorf("error %q doesn't name GOAL_TRACKER_DIRECTORY_TOKEN as missing", err)
	}
}

// Directory variables of only whitespace are all unset: there is no sync.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestBlankDirectoryVariablesLeaveTheSyncOff(t *testing.T) {
	devEnv(t)
	for _, v := range append(slices.Clone(directoryVars), directoryFileVar) {
		t.Setenv(v, "  ")
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.directory != nil {
		t.Errorf("the directory sync is on with its variables blank: %+v", cfg.directory)
	}
}

// Whitespace around the directory URL and token, as from a stray space in an
// env file, isn't kept.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestDirectoryVariablesAreTrimmed(t *testing.T) {
	devEnv(t)
	t.Setenv("GOAL_TRACKER_DIRECTORY_URL", " http://localhost:9000 ")
	t.Setenv("GOAL_TRACKER_DIRECTORY_TOKEN", "\ttoken \n")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.directory == nil || cfg.directory.url != "http://localhost:9000" || cfg.directory.token != "token" {
		t.Errorf("directory = %+v, want the URL and token trimmed", cfg.directory)
	}
}

// passwordEnv is clearAuthEnv with password sign-in chosen and a session key
// set, as it requires.
func passwordEnv(t *testing.T) {
	t.Helper()
	clearAuthEnv(t)
	t.Setenv("GOAL_TRACKER_SIGNIN", "password")
	t.Setenv("GOAL_TRACKER_SESSION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
}

//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestPasswordSignInIsAccepted(t *testing.T) {
	passwordEnv(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.signIn != signInPassword {
		t.Errorf("sign-in method %q, want %q", cfg.signIn, signInPassword)
	}
	if cfg.oidc != nil {
		t.Errorf("OIDC is on with password sign-in chosen: %+v", cfg.oidc)
	}
}

// Password sign-in needs a session key, as organization sign-in does.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestPasswordSignInNeedsASessionKey(t *testing.T) {
	passwordEnv(t)
	t.Setenv("GOAL_TRACKER_SESSION_KEY", "")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig accepted GOAL_TRACKER_SIGNIN=password without a session key")
	}
	if !strings.Contains(err.Error(), "GOAL_TRACKER_SESSION_KEY") {
		t.Errorf("error %q doesn't name GOAL_TRACKER_SESSION_KEY", err)
	}
}

// With password sign-in chosen, any OIDC variable stops startup.
func TestPasswordSignInRefusesOIDCVariables(t *testing.T) {
	for _, set := range oidcVars {
		t.Run(set, func(t *testing.T) {
			passwordEnv(t)
			t.Setenv(set, "value")

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig accepted GOAL_TRACKER_SIGNIN=password with %s set", set)
			}
			if !strings.Contains(err.Error(), set) {
				t.Errorf("error %q doesn't name %s", err, set)
			}
		})
	}
}

// discardLogger is a logger that writes nowhere.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// goal-tracker password-link <email> prints a set-password link at the base
// URL, creating the Account in a migrated database, and exits.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestThePasswordLinkCommandPrintsALink(t *testing.T) {
	passwordEnv(t)
	t.Setenv("GOAL_TRACKER_DB", filepath.Join(t.TempDir(), "goal-tracker.db"))
	t.Setenv("GOAL_TRACKER_BASE_URL", "https://goals.example.com")

	var out bytes.Buffer
	if err := run(discardLogger(), []string{"password-link", "first.admin@example.com"}, &out); err != nil {
		t.Fatalf("password-link: %v", err)
	}
	if !regexp.MustCompile(`^https://goals\.example\.com/set-password\?token=[A-Za-z0-9_-]{43}\n$`).MatchString(out.String()) {
		t.Errorf("password-link printed %q, want the link alone on a line", out.String())
	}
}

// The password-link command refuses unless GOAL_TRACKER_SIGNIN=password, and
// prints nothing.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestThePasswordLinkCommandNeedsPasswordSignIn(t *testing.T) {
	devEnv(t)
	t.Setenv("GOAL_TRACKER_DB", filepath.Join(t.TempDir(), "goal-tracker.db"))

	var out bytes.Buffer
	err := run(discardLogger(), []string{"password-link", "first.admin@example.com"}, &out)
	if err == nil {
		t.Fatal("password-link ran with GOAL_TRACKER_SIGNIN=dev")
	}
	if !strings.Contains(err.Error(), "GOAL_TRACKER_SIGNIN=password") {
		t.Errorf("error %q doesn't say it needs GOAL_TRACKER_SIGNIN=password", err)
	}
	if out.Len() != 0 {
		t.Errorf("password-link printed %q", out.String())
	}
}

// The password-link command refuses anything that isn't an email address,
// prints nothing, and leaves no Account for it.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestThePasswordLinkCommandNeedsAnEmailAddress(t *testing.T) {
	passwordEnv(t)
	dbPath := filepath.Join(t.TempDir(), "goal-tracker.db")
	t.Setenv("GOAL_TRACKER_DB", dbPath)

	var out bytes.Buffer
	err := run(discardLogger(), []string{"password-link", "not-an-email"}, &out)
	if !errors.Is(err, domain.ErrNotAnEmail) {
		t.Errorf("password-link not-an-email: %v, want ErrNotAnEmail", err)
	}
	if out.Len() != 0 {
		t.Errorf("password-link printed %q", out.String())
	}

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	svc := domain.NewService(sqlDB, clock.Real{}, nil, nil)
	if _, err := svc.AccountByEmail(t.Context(), "not-an-email"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("password-link not-an-email left an Account (err %v)", err)
	}
}
