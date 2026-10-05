package clock_test

import (
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
)

var offsetStart = time.Date(2026, 10, 5, 18, 5, 0, 0, time.UTC)

func TestOffsetReadsItsStartInstantWhenCreated(t *testing.T) {
	t.Parallel()

	c := clock.NewOffset(offsetStart)

	got := c.Now()
	if got.Before(offsetStart) || got.Sub(offsetStart) > time.Second {
		t.Fatalf("Now() = %v, want %v (give or take the moment since creation)", got, offsetStart)
	}
}

func TestOffsetAdvancesWithRealTime(t *testing.T) {
	t.Parallel()

	c := clock.NewOffset(offsetStart)
	before := c.Now()

	time.Sleep(50 * time.Millisecond)

	if elapsed := c.Now().Sub(before); elapsed < 50*time.Millisecond {
		t.Fatalf("Now() advanced %v over a 50ms sleep, want at least 50ms", elapsed)
	}
}

func TestOffsetReturnsUTC(t *testing.T) {
	t.Parallel()

	c := clock.NewOffset(offsetStart.In(time.FixedZone("EDT", -4*60*60)))

	if loc := c.Now().Location(); loc != time.UTC {
		t.Fatalf("Now() is in %v, want UTC", loc)
	}
}
