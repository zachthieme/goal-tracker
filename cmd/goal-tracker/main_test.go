package main

import (
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
