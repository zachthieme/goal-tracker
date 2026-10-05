package main

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
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
	for _, v := range slices.Concat(oidcVars, directoryVars, []string{"GOAL_TRACKER_SESSION_KEY", "GOAL_TRACKER_BASE_URL"}) {
		t.Setenv(v, "")
	}
}

func TestPartialOIDCConfigNamesWhatsMissing(t *testing.T) {
	clearAuthEnv(t)
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

func TestFullOIDCConfigRedirectsToTheBaseURLsCallback(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("GOAL_TRACKER_OIDC_ISSUER", "http://localhost:9000/application/o/goal-tracker/")
	t.Setenv("GOAL_TRACKER_OIDC_CLIENT_ID", "goal-tracker")
	t.Setenv("GOAL_TRACKER_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("GOAL_TRACKER_BASE_URL", "https://goals.example.com/")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.oidc == nil {
		t.Fatal("OIDC is off with all three variables set")
	}
	if got, want := cfg.oidc.RedirectURL, "https://goals.example.com/auth/callback"; got != want {
		t.Errorf("redirect URL %q, want %q", got, want)
	}
}

//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestNoOIDCConfigLeavesTheDevelopmentForm(t *testing.T) {
	clearAuthEnv(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.oidc != nil {
		t.Errorf("OIDC is on with none of its variables set: %+v", cfg.oidc)
	}
}

func TestABadSessionKeyIsNamed(t *testing.T) {
	for name, key := range map[string]string{
		"short":      base64.StdEncoding.EncodeToString(make([]byte, 31)),
		"not base64": strings.Repeat("!", 64),
	} {
		t.Run(name, func(t *testing.T) {
			clearAuthEnv(t)
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
	clearAuthEnv(t)
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
			clearAuthEnv(t)
			t.Setenv(set, "value")

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig accepted %s without %s", set, missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error %q doesn't name %s", err, missing)
			}
		})
	}
}

// With neither directory variable set there is no sync; with both, it reads
// the directory at the URL with the token.
//
//nolint:paralleltest // sets the process environment, through clearAuthEnv
func TestDirectorySyncIsOnlyOnWithBothVariables(t *testing.T) {
	clearAuthEnv(t)
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
