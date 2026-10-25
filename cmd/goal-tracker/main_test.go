package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/web"
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

// clearAuthEnv unsets the sign-in, directory, Chromium and trusted proxy
// variables for the test, so the host's own settings don't leak in.
func clearAuthEnv(t *testing.T) {
	t.Helper()
	for _, v := range slices.Concat(oidcVars, directoryVars, []string{directoryFileVar, "GOAL_TRACKER_SIGNIN", "GOAL_TRACKER_SESSION_KEY", "GOAL_TRACKER_BASE_URL", "GOAL_TRACKER_CHROME", trustedProxiesVar}) {
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
// and base URL set, as it requires; the OIDC variables are left to the test.
func oidcEnv(t *testing.T) {
	t.Helper()
	clearAuthEnv(t)
	t.Setenv("GOAL_TRACKER_SIGNIN", "oidc")
	t.Setenv("GOAL_TRACKER_SESSION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv("GOAL_TRACKER_BASE_URL", "https://goals.example.com")
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

// Whitespace around the OIDC values, as from a stray space in an env file,
// isn't kept.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestOIDCValuesAreTrimmed(t *testing.T) {
	oidcEnv(t)
	t.Setenv("GOAL_TRACKER_OIDC_ISSUER", " http://localhost:9000/application/o/goal-tracker/ ")
	t.Setenv("GOAL_TRACKER_OIDC_CLIENT_ID", "\tgoal-tracker ")
	t.Setenv("GOAL_TRACKER_OIDC_CLIENT_SECRET", " secret\n")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.oidc == nil {
		t.Fatal("OIDC is off with all three variables set")
	}
	if got, want := cfg.oidc.Issuer, "http://localhost:9000/application/o/goal-tracker/"; got != want {
		t.Errorf("issuer %q, want %q", got, want)
	}
	if got, want := cfg.oidc.ClientID, "goal-tracker"; got != want {
		t.Errorf("client ID %q, want %q", got, want)
	}
	if got, want := cfg.oidc.ClientSecret, "secret"; got != want {
		t.Errorf("client secret %q, want %q", got, want)
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

// With the development form chosen, an unset base URL falls back to this
// machine's address on GOAL_TRACKER_ADDR.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestDevSignInDefaultsTheBaseURLToLocalhost(t *testing.T) {
	devEnv(t)
	t.Setenv("GOAL_TRACKER_ADDR", ":9090")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.baseURL, "http://localhost:9090"; got != want {
		t.Errorf("base URL %q, want %q", got, want)
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

// Whitespace around the directory file's path isn't kept either.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestTheDirectoryFileIsTrimmed(t *testing.T) {
	devEnv(t)
	t.Setenv(directoryFileVar, " /etc/goal-tracker/people.csv\t")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.directory == nil || cfg.directory.file != "/etc/goal-tracker/people.csv" {
		t.Errorf("directory = %+v, want the file's path trimmed", cfg.directory)
	}
}

// passwordEnv is clearAuthEnv with password sign-in chosen and a session key
// and base URL set, as it requires.
func passwordEnv(t *testing.T) {
	t.Helper()
	clearAuthEnv(t)
	t.Setenv("GOAL_TRACKER_SIGNIN", "password")
	t.Setenv("GOAL_TRACKER_SESSION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv("GOAL_TRACKER_BASE_URL", "https://goals.example.com")
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

// Password and organization sign-in need a base URL, so links in emails and
// the OIDC callback don't point at localhost and cookies get Secure behind
// TLS.
func TestPasswordAndOIDCSignInNeedABaseURL(t *testing.T) {
	for _, tc := range []struct {
		signIn string
		env    func(*testing.T)
	}{
		{"password", passwordEnv},
		{"oidc", func(t *testing.T) { t.Helper(); oidcEnv(t); setOIDCVars(t) }},
	} {
		for name, value := range map[string]string{"unset": "", "blank": " \t"} {
			t.Run(tc.signIn+"/"+name, func(t *testing.T) {
				tc.env(t)
				t.Setenv("GOAL_TRACKER_BASE_URL", value)

				_, err := loadConfig()
				if err == nil {
					t.Fatalf("loadConfig accepted GOAL_TRACKER_SIGNIN=%s with GOAL_TRACKER_BASE_URL=%q", tc.signIn, value)
				}
				for _, want := range []string{"GOAL_TRACKER_BASE_URL", "GOAL_TRACKER_SIGNIN=" + tc.signIn} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q doesn't name %s", err, want)
					}
				}
			})
		}
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

// OIDC variables of only whitespace aren't set, so password sign-in doesn't
// refuse them.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestPasswordSignInAcceptsBlankOIDCVariables(t *testing.T) {
	passwordEnv(t)
	for _, v := range oidcVars {
		t.Setenv(v, "  ")
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig refused GOAL_TRACKER_SIGNIN=password with blank OIDC variables: %v", err)
	}
	if cfg.oidc != nil {
		t.Errorf("OIDC is on with password sign-in chosen: %+v", cfg.oidc)
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
	if err := run(t.Context(), discardLogger(), []string{"password-link", "first.admin@example.com"}, &out); err != nil {
		t.Fatalf("password-link: %v", err)
	}
	if !regexp.MustCompile(`^https://goals\.example\.com/set-password\?token=[A-Za-z0-9_-]{43}\n$`).MatchString(out.String()) {
		t.Errorf("password-link printed %q, want the link alone on a line", out.String())
	}
}

// The password-link command logs, at INFO, that the command line issued a
// link, for whom and until when, but never the link or its token.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestThePasswordLinkCommandLogsTheLinkItIssued(t *testing.T) {
	passwordEnv(t)
	t.Setenv("GOAL_TRACKER_DB", filepath.Join(t.TempDir(), "goal-tracker.db"))
	t.Setenv("GOAL_TRACKER_BASE_URL", "https://goals.example.com")

	var out, log bytes.Buffer
	if err := run(t.Context(), slog.New(slog.NewTextHandler(&log, nil)), []string{"password-link", "first.admin@example.com"}, &out); err != nil {
		t.Fatalf("password-link: %v", err)
	}
	var issued string
	for line := range strings.SplitSeq(log.String(), "\n") {
		if strings.Contains(line, `msg="set-password link issued"`) {
			issued = line
		}
	}
	for _, want := range []string{"level=INFO", "issuer=command-line", "account_id=", "email=first.admin@example.com", "expires="} {
		if !strings.Contains(issued, want) {
			t.Errorf("the issued line %q lacks %s:\n%s", issued, want, &log)
		}
	}
	_, token, found := strings.Cut(strings.TrimSpace(out.String()), "token=")
	if !found {
		t.Fatalf("password-link printed %q, want a link", out.String())
	}
	for _, secret := range []string{token, "/set-password?token="} {
		if strings.Contains(log.String(), secret) {
			t.Errorf("the log holds %q:\n%s", secret, &log)
		}
	}
}

// The password-link command refuses without a base URL, as the app does, and
// prints nothing rather than a link to localhost.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestThePasswordLinkCommandNeedsABaseURL(t *testing.T) {
	passwordEnv(t)
	t.Setenv("GOAL_TRACKER_DB", filepath.Join(t.TempDir(), "goal-tracker.db"))
	t.Setenv("GOAL_TRACKER_BASE_URL", "")

	var out bytes.Buffer
	err := run(t.Context(), discardLogger(), []string{"password-link", "first.admin@example.com"}, &out)
	if err == nil {
		t.Fatal("password-link ran without GOAL_TRACKER_BASE_URL")
	}
	if !strings.Contains(err.Error(), "GOAL_TRACKER_BASE_URL") {
		t.Errorf("error %q doesn't name GOAL_TRACKER_BASE_URL", err)
	}
	if out.Len() != 0 {
		t.Errorf("password-link printed %q", out.String())
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
	err := run(t.Context(), discardLogger(), []string{"password-link", "first.admin@example.com"}, &out)
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
	err := run(t.Context(), discardLogger(), []string{"password-link", "not-an-email"}, &out)
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

// GOAL_TRACKER_TIMEZONE=Local is the host's zone, not an IANA name: a Report
// block freezes its Goal's zone by name and couldn't read "Local" back, so
// startup stops and asks for the zone by name.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestLocalTimezoneIsRefused(t *testing.T) {
	devEnv(t)
	t.Setenv("GOAL_TRACKER_TIMEZONE", "Local")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig accepted GOAL_TRACKER_TIMEZONE=Local")
	}
	for _, want := range []string{"GOAL_TRACKER_TIMEZONE", "IANA", "America/Los_Angeles"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't name %s", err, want)
		}
	}
}

// A blank GOAL_TRACKER_TIMEZONE is unset, as with every GOAL_TRACKER_*
// variable, and gives UTC rather than the host's zone.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestTimezoneDefaultsToUTC(t *testing.T) {
	devEnv(t)
	t.Setenv("GOAL_TRACKER_TIMEZONE", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.timezone != "UTC" {
		t.Errorf("timezone %q, want UTC", cfg.timezone)
	}
}

//nolint:paralleltest // sets the process environment, through devEnv
func TestANamedTimezoneIsKept(t *testing.T) {
	devEnv(t)
	t.Setenv("GOAL_TRACKER_TIMEZONE", "America/Los_Angeles")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.timezone != "America/Los_Angeles" {
		t.Errorf("timezone %q, want America/Los_Angeles", cfg.timezone)
	}
}

// A GOAL_TRACKER_CHROME that isn't there stops startup with an error naming
// it, rather than PDF downloads failing one by one.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestAMissingChromeStopsStartup(t *testing.T) {
	devEnv(t)
	missing := filepath.Join(t.TempDir(), "chromium")
	t.Setenv("GOAL_TRACKER_CHROME", missing)
	t.Setenv("GOAL_TRACKER_DB", filepath.Join(t.TempDir(), "goal-tracker.db"))

	err := run(t.Context(), discardLogger(), nil, io.Discard)
	if err == nil {
		t.Fatal("goal-tracker started with GOAL_TRACKER_CHROME naming a missing file")
	}
	for _, want := range []string{"GOAL_TRACKER_CHROME", missing} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't name %s", err, want)
		}
	}
}

// Without GOAL_TRACKER_CHROME, the app renders PDFs with a Chromium on PATH,
// and logs which.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestAChromeOnPATHRendersPDFs(t *testing.T) {
	devEnv(t)
	dir := t.TempDir()
	chromium := filepath.Join(dir, "chromium")
	if err := os.WriteFile(chromium, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.chrome != chromium {
		t.Errorf("Chromium %q, want %s from PATH", cfg.chrome, chromium)
	}
	var log bytes.Buffer
	if _, err := pdfDownloads(cfg.chrome, slog.New(slog.NewTextHandler(&log, nil))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), chromium) {
		t.Errorf("startup doesn't log the Chromium it renders PDFs with:\n%s", log.String())
	}
}

// With no Chromium named or on PATH, startup logs that PDF downloads fall back
// to the print page, and doesn't check TMPDIR, which only Chromium needs short.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestWithoutChromePDFDownloadsFallBackToThePrintPage(t *testing.T) {
	devEnv(t)
	t.Setenv("PATH", t.TempDir())

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.chrome != "" {
		t.Errorf("Chromium %q with none on PATH", cfg.chrome)
	}
	t.Setenv("TMPDIR", "/"+strings.Repeat("t", 62))
	var log bytes.Buffer
	if _, err := pdfDownloads(cfg.chrome, slog.New(slog.NewTextHandler(&log, nil))); err != nil {
		t.Errorf("without a Chromium, startup under a TMPDIR too long for one: %v", err)
	}
	if strings.Contains(log.String(), "TMPDIR") {
		t.Errorf("without a Chromium, startup warns of TMPDIR:\n%s", log.String())
	}
	for _, want := range []string{"GOAL_TRACKER_CHROME", "PDF downloads fall back to the print page"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("startup log doesn't say %q:\n%s", want, log.String())
		}
	}
}

// With no Chromium, a TMPDIR too long for one doesn't stop startup: only
// Chromium needs it short.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestWithoutChromeALongTMPDIRDoesNotStopStartup(t *testing.T) {
	devEnv(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMPDIR", "/"+strings.Repeat("t", 62))

	if _, err := loadConfig(); err != nil {
		t.Errorf("without a Chromium, loadConfig under a TMPDIR too long for one: %v", err)
	}
}

// startupEnv is devEnv with a Chromium named, a database, and a listen
// address that can't be listened on, so run returns as it would start
// serving.
func startupEnv(t *testing.T) {
	t.Helper()
	devEnv(t)
	dir := t.TempDir()
	chromium := filepath.Join(dir, "chromium")
	if err := os.WriteFile(chromium, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOAL_TRACKER_CHROME", chromium)
	t.Setenv("GOAL_TRACKER_DB", filepath.Join(dir, "goal-tracker.db"))
	t.Setenv("GOAL_TRACKER_ADDR", "127.0.0.1:-1")
}

// With a Chromium, a TMPDIR too long for Chromium's socket, under which every
// render would abort, stops startup with an error naming it, before the
// database is made or the directory synced, as a missing Chromium does.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestATMPDIRTooLongForChromiumStopsStartup(t *testing.T) {
	startupEnv(t)
	people := filepath.Join(t.TempDir(), "people.csv")
	if err := os.WriteFile(people, []byte("email,name,manager_email\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(directoryFileVar, people)
	tmp := "/" + strings.Repeat("t", 62)
	t.Setenv("TMPDIR", tmp)

	var log bytes.Buffer
	err := run(t.Context(), slog.New(slog.NewTextHandler(&log, nil)), nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "TMPDIR "+tmp) {
		t.Errorf("goal-tracker under the %d-byte TMPDIR: %v; want startup stopped, naming the TMPDIR", len(tmp), err)
	}
	if _, err := os.Stat(os.Getenv("GOAL_TRACKER_DB")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("startup stopped for the TMPDIR made the database: %v", err)
	}
	for line := range strings.Lines(log.String()) {
		if strings.Contains(line, "sync") {
			t.Errorf("startup stopped for the TMPDIR synced the directory: %s", line)
		}
	}
}

// With a Chromium, a TMPDIR long enough that a render killed for time may
// leave Chromium's files in it starts, warning so.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestALongTMPDIRStartsWithAWarning(t *testing.T) {
	startupEnv(t)
	tmp := "/" + strings.Repeat("t", 57)
	t.Setenv("TMPDIR", tmp)

	var log bytes.Buffer
	err := run(t.Context(), slog.New(slog.NewTextHandler(&log, nil)), nil, io.Discard)
	if err == nil || !strings.HasPrefix(err.Error(), "serve:") {
		t.Fatalf("goal-tracker under the %d-byte TMPDIR: %v; want it to start serving", len(tmp), err)
	}
	warned := false
	for line := range strings.Lines(log.String()) {
		warned = warned || strings.Contains(line, "level=WARN") && strings.Contains(line, tmp)
	}
	if !warned {
		t.Errorf("startup under the %d-byte TMPDIR logs no warning naming it:\n%s", len(tmp), log.String())
	}
}

// trustedProxiesVar names the reverse proxies whose X-Forwarded-For the
// sign-in throttle reads.
const trustedProxiesVar = "GOAL_TRACKER_TRUSTED_PROXIES"

// GOAL_TRACKER_TRUSTED_PROXIES is a comma-separated list of CIDRs and bare
// IPs, a bare IP being the one address, an IPv4-mapped IPv6 one its IPv4
// address and an IPv4-mapped CIDR its IPv4 range, with blank entries and the
// space around each ignored.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestTrustedProxiesAreCIDRsOrBareIPs(t *testing.T) {
	devEnv(t)
	t.Setenv(trustedProxiesVar, " 10.0.0.0/8, 192.0.2.1 ,,fd00::/8,2001:db8::1,::ffff:198.51.100.2,::ffff:10.0.0.0/104")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.0.2.1/32"),
		netip.MustParsePrefix("fd00::/8"),
		netip.MustParsePrefix("2001:db8::1/128"),
		netip.MustParsePrefix("198.51.100.2/32"),
		netip.MustParsePrefix("10.0.0.0/8"),
	}
	if !slices.Equal(cfg.trustedProxies, want) {
		t.Errorf("trusted proxies %v, want %v", cfg.trustedProxies, want)
	}
}

// Without GOAL_TRACKER_TRUSTED_PROXIES, no proxy is trusted.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestNoProxyIsTrustedByDefault(t *testing.T) {
	devEnv(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.trustedProxies) != 0 {
		t.Errorf("trusted proxies %v, want none", cfg.trustedProxies)
	}
}

// An entry in GOAL_TRACKER_TRUSTED_PROXIES that is neither a CIDR nor an IP,
// or is an IPv4-mapped CIDR wider than /96 (it has no IPv4 range), stops
// startup with an error naming the variable and the entry.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestAnInvalidTrustedProxyStopsStartup(t *testing.T) {
	for _, bad := range []string{"proxy.internal", "10.0.0.0/33", "10.0.0.300", "::ffff:0.0.0.0/80"} {
		t.Run(bad, func(t *testing.T) {
			startupEnv(t)
			t.Setenv(trustedProxiesVar, "10.0.0.0/8,"+bad)

			err := run(t.Context(), discardLogger(), nil, io.Discard)
			if err == nil {
				t.Fatalf("goal-tracker started with %s=10.0.0.0/8,%s", trustedProxiesVar, bad)
			}
			for _, want := range []string{trustedProxiesVar, bad} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q doesn't name %s", err, want)
				}
			}
		})
	}
}

// With trusted proxies, startup logs that the sign-in throttle reads their
// X-Forwarded-For, and which they are.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestTrustedProxiesAreLogged(t *testing.T) {
	startupEnv(t)
	t.Setenv(trustedProxiesVar, "10.0.0.0/8,192.0.2.1")

	var log bytes.Buffer
	err := run(t.Context(), slog.New(slog.NewTextHandler(&log, nil)), nil, io.Discard)
	if err == nil || !strings.HasPrefix(err.Error(), "serve:") {
		t.Fatalf("goal-tracker with trusted proxies: %v; want it to start serving", err)
	}
	logged := false
	for line := range strings.Lines(log.String()) {
		logged = logged || strings.Contains(line, "X-Forwarded-For") && strings.Contains(line, "10.0.0.0/8") && strings.Contains(line, "192.0.2.1/32")
	}
	if !logged {
		t.Errorf("startup with trusted proxies logs no line naming X-Forwarded-For and them:\n%s", log.String())
	}
}

// The HTTP server bounds how long a request may take to arrive, to be
// answered, and to sit idle, so a slow or stalled client can't hold a
// connection open. A PDF download, which may wait for a render slot and then
// render, still has time to be answered.
func TestTheHTTPServerTimesOutSlowConnections(t *testing.T) {
	t.Parallel()

	s := newHTTPServer(":0", http.NotFoundHandler())
	for name, d := range map[string]time.Duration{
		"ReadHeaderTimeout": s.ReadHeaderTimeout,
		"ReadTimeout":       s.ReadTimeout,
		"WriteTimeout":      s.WriteTimeout,
		"IdleTimeout":       s.IdleTimeout,
	} {
		if d <= 0 {
			t.Errorf("%s is %v, want it set", name, d)
		}
	}
	if s.WriteTimeout <= web.PDFRenderTimeout {
		t.Errorf("WriteTimeout %v doesn't outlast a PDF download's %v wait and render", s.WriteTimeout, web.PDFRenderTimeout)
	}
}

// listenAddrs is a log handler that sends the address each "goal-tracker
// listening" record names, so a test learns the port run bound.
type listenAddrs chan string

func (l listenAddrs) Enabled(context.Context, slog.Level) bool { return true }
func (l listenAddrs) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l listenAddrs) WithGroup(string) slog.Handler            { return l }

func (l listenAddrs) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "goal-tracker listening" {
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "addr" {
			l <- a.Value.String()
			return false
		}
		return true
	})
	return nil
}

// serveEnv is devEnv with no Chromium, a database, and a free port to listen
// on.
func serveEnv(t *testing.T) {
	t.Helper()
	devEnv(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GOAL_TRACKER_DB", filepath.Join(t.TempDir(), "goal-tracker.db"))
	t.Setenv("GOAL_TRACKER_ADDR", "127.0.0.1:0")
}

// startRun runs the app until ctx is done, returning the address it listens
// on and where run's error arrives.
func startRun(t *testing.T, ctx context.Context) (string, <-chan error) {
	t.Helper()
	listening := make(listenAddrs, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, slog.New(listening), nil, io.Discard) }()
	select {
	case addr := <-listening:
		return addr, done
	case err := <-done:
		t.Fatalf("run returned before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("run didn't start listening within 10s")
	}
	return "", nil
}

// Asked to stop, as SIGINT or SIGTERM asks, the app stops taking connections
// but finishes the request in flight, then run returns nil.
//
//nolint:paralleltest // sets the process environment, through devEnv
func TestStoppingFinishesTheRequestInFlight(t *testing.T) {
	serveEnv(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	addr, done := startRun(t, ctx)

	// A sign-in whose body is slow to come: the server asks for it, with a
	// 100 Continue, once the route has begun reading it.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	form := url.Values{"email": {"sam@example.com"}}.Encode()
	if _, err := fmt.Fprintf(conn, "POST /signin HTTP/1.1\r\nHost: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\nExpect: 100-continue\r\n\r\n", addr, len(form)); err != nil {
		t.Fatal(err)
	}
	responses := bufio.NewReader(conn)
	if line, err := responses.ReadString('\n'); err != nil || line != "HTTP/1.1 100 Continue\r\n" {
		t.Fatalf("the server answered the sign-in's headers with %q (%v), want 100 Continue", line, err)
	}
	if _, err := responses.ReadString('\n'); err != nil {
		t.Fatal(err)
	}

	stop()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		probe, err := net.Dial("tcp", addr)
		if err != nil {
			break // no longer taking connections
		}
		_ = probe.Close()
		if time.Now().After(deadline) {
			t.Fatal("the app still takes connections 10s after being asked to stop")
		}
	}
	select {
	case err := <-done:
		t.Fatalf("run returned %v with a request in flight", err)
	default:
	}

	if _, err := io.WriteString(conn, form); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(responses, nil)
	if err != nil {
		t.Fatalf("the sign-in in flight got no response: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("the sign-in in flight: status %d, want 303", resp.StatusCode)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run, asked to stop: %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run didn't return within 10s of its last request finishing")
	}
}

// A request still in flight when the shutdown's grace runs out is cut off,
// and serve says so.
func TestARequestOutlastingTheGraceIsCutOff(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	began, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	s := newHTTPServer(ln.Addr().String(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(began)
		<-release
	}))
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, s, ln, 50*time.Millisecond) }()

	cut := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
		cut <- err
	}()
	<-began
	stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("serve, with a request outlasting the grace: %v, want the deadline exceeded", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve didn't return within 10s of a 50ms grace")
	}
	select {
	case err := <-cut:
		if err == nil {
			t.Error("the request outlasting the grace got a response")
		}
	case <-time.After(10 * time.Second):
		t.Error("the request outlasting the grace is still open 10s on")
	}
}

// healthzServer is a stand-in for the app on the loopback, answering
// GET /healthz with status, and the port it listens on.
func healthzServer(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// goal-tracker healthcheck, with the app answering /healthz with a 200,
// succeeds and prints nothing. It reads no configuration but
// GOAL_TRACKER_ADDR, so it runs without what serving needs (a sign-in method,
// a named timezone), and opens no database.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestTheHealthcheckCommandPassesOnA200(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("GOAL_TRACKER_TIMEZONE", "Local")
	dbPath := filepath.Join(t.TempDir(), "goal-tracker.db")
	t.Setenv("GOAL_TRACKER_DB", dbPath)
	t.Setenv("GOAL_TRACKER_ADDR", ":"+healthzServer(t, http.StatusOK))

	var out bytes.Buffer
	if err := run(t.Context(), discardLogger(), []string{"healthcheck"}, &out); err != nil {
		t.Fatalf("healthcheck against a 200: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("healthcheck printed %q", out.String())
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("healthcheck touched the database at GOAL_TRACKER_DB (stat: %v)", err)
	}
}

// goal-tracker healthcheck fails, saying why, when the app answers /healthz
// with anything but a 200, or when nothing is listening on its port.
//
//nolint:paralleltest // sets the process environment
func TestTheHealthcheckCommandFailsWhenTheAppIsntHealthy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, closedPort, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()

	for name, tc := range map[string]struct{ port, want string }{
		"503":               {healthzServer(t, http.StatusServiceUnavailable), "503"},
		"nothing listening": {closedPort, "connection refused"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GOAL_TRACKER_ADDR", ":"+tc.port)

			var out bytes.Buffer
			err := run(t.Context(), discardLogger(), []string{"healthcheck"}, &out)
			if err == nil {
				t.Fatal("healthcheck passed")
			}
			if !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "\n") {
				t.Errorf("healthcheck: %q, want one line saying %q", err, tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("healthcheck printed %q", out.String())
			}
		})
	}
}

// goal-tracker healthcheck checks the port GOAL_TRACKER_ADDR names, on the
// loopback, whatever host it names: the app listens there in each case.
//
//nolint:paralleltest // sets the process environment
func TestTheHealthcheckCommandChecksTheLoopbackAtGOAL_TRACKER_ADDRsPort(t *testing.T) {
	port := healthzServer(t, http.StatusOK)
	for _, addr := range []string{":" + port, "0.0.0.0:" + port, "goals.example.invalid:" + port} {
		t.Run(addr, func(t *testing.T) {
			t.Setenv("GOAL_TRACKER_ADDR", addr)

			if err := run(t.Context(), discardLogger(), []string{"healthcheck"}, io.Discard); err != nil {
				t.Errorf("healthcheck with GOAL_TRACKER_ADDR=%s: %v", addr, err)
			}
		})
	}
}

// goal-tracker healthcheck fails, saying it timed out, when something on its
// port accepts the connection and never answers: the image's HEALTHCHECK
// doesn't hang on a wedged app. The check here waits a fraction of the
// shipped healthcheckTimeout, and the test's own deadline, well past that,
// only stops a check with no timeout hanging the run.
//
//nolint:paralleltest // sets the process environment
func TestTheHealthcheckCommandTimesOutWhenTheAppNeverAnswers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- conn // held open, never answered
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		for {
			select {
			case conn := <-accepted:
				_ = conn.Close()
			default:
				return
			}
		}
	})
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOAL_TRACKER_ADDR", ":"+port)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err = checkHealth(ctx, addrFromEnv(), 200*time.Millisecond)
	if err == nil {
		t.Fatal("healthcheck passed")
	}
	if !strings.Contains(err.Error(), "Client.Timeout exceeded") || strings.Contains(err.Error(), "\n") {
		t.Errorf("healthcheck: %q, want one line saying it timed out (Client.Timeout exceeded)", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("healthcheck waited %v, want about its 200ms timeout", waited)
	}
}

// GOAL_TRACKER_LOG_FORMAT=json logs one JSON object a line, for a log
// collector to parse; text logs slog's key=value lines. Either is read
// whatever its case, and with the space around it trimmed.
func TestTheLogFormatIsJSONOrText(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"json", " JSON "} {
		var buf bytes.Buffer
		logger, err := newLogger(&buf, "", format)
		if err != nil {
			t.Fatalf("newLogger with format %q: %v", format, err)
		}
		logger.Info("hello", "who", "ops")
		var rec map[string]any
		if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
			t.Fatalf("format %q logged %q, want a JSON line: %v", format, buf.String(), err)
		}
		if rec["msg"] != "hello" || rec["level"] != "INFO" || rec["who"] != "ops" {
			t.Errorf("format %q logged %v, want an INFO hello from ops", format, rec)
		}
	}

	for _, format := range []string{"text", "Text"} {
		var buf bytes.Buffer
		logger, err := newLogger(&buf, "", format)
		if err != nil {
			t.Fatalf("newLogger with format %q: %v", format, err)
		}
		logger.Info("hello", "who", "ops")
		if line := buf.String(); !strings.Contains(line, "level=INFO msg=hello who=ops\n") || json.Valid(buf.Bytes()) {
			t.Errorf("format %q logged %q, want a text line", format, line)
		}
	}
}

// GOAL_TRACKER_LOG_LEVEL is the least severe record logged: at warn, an INFO
// record is dropped and a WARN kept. It is read whatever its case, and with
// the space around it trimmed.
func TestTheLogLevelDropsLessSevereRecords(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"warn", " WARN "} {
		var buf bytes.Buffer
		logger, err := newLogger(&buf, level, "")
		if err != nil {
			t.Fatalf("newLogger at level %q: %v", level, err)
		}
		logger.Info("routine")
		logger.Warn("worrying")
		if log := buf.String(); strings.Contains(log, "routine") || !strings.Contains(log, "level=WARN msg=worrying") {
			t.Errorf("at level %q logged %q, want only the WARN", level, log)
		}
	}
	for level, want := range map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "error": slog.LevelError} {
		logger, err := newLogger(io.Discard, level, "")
		if err != nil {
			t.Fatalf("newLogger at level %q: %v", level, err)
		}
		if !logger.Enabled(t.Context(), want) || logger.Enabled(t.Context(), want-1) {
			t.Errorf("at level %q, logs from %v up isn't so", level, want)
		}
	}
}

// Unset, the log is text at INFO, as it always was: an INFO record is logged,
// a DEBUG one isn't.
func TestTheLogDefaultsToTextAtInfo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := newLogger(&buf, " ", "")
	if err != nil {
		t.Fatalf("newLogger with nothing set: %v", err)
	}
	logger.Debug("detail")
	logger.Info("routine")
	if log := buf.String(); strings.Contains(log, "detail") || !strings.Contains(log, "level=INFO msg=routine\n") {
		t.Errorf("with nothing set logged %q, want only the INFO, as text", log)
	}
}

// A log level or format the app doesn't know stops startup with an error
// naming the variable and the values it takes.
func TestABadLogSettingIsNamed(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		level, format string
		want          []string
	}{
		"level":  {"loud", "", []string{"GOAL_TRACKER_LOG_LEVEL", "debug, info, warn, error", `"loud"`}},
		"format": {"", "xml", []string{"GOAL_TRACKER_LOG_FORMAT", "text, json", `"xml"`}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newLogger(io.Discard, tc.level, tc.format)
			if err == nil {
				t.Fatalf("newLogger accepted level %q, format %q", tc.level, tc.format)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q doesn't name %s", err, want)
				}
			}
		})
	}
}
