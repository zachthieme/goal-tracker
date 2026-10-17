package web

import (
	"testing"
	"time"
)

// datesNow is the clock the date tests read from: 18:52 UTC on Sat 17 Oct 2026.
var datesNow = time.Date(2026, time.October, 17, 18, 52, 0, 0, time.UTC)

// A calendar date reads "Nov 7" in the current year and carries its year in
// any other, past or next.
func TestShowDateCarriesTheYearOnlyOutsideTheCurrentOne(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		d    time.Time
		want string
	}{
		{time.Date(2026, time.November, 7, 0, 0, 0, 0, time.UTC), "Nov 7"},
		{time.Date(2025, time.November, 7, 0, 0, 0, 0, time.UTC), "Nov 7, 2025"},
		{time.Date(2027, time.January, 15, 0, 0, 0, 0, time.UTC), "Jan 15, 2027"},
	} {
		if got := showDate(tc.d, datesNow, time.UTC); got != tc.want {
			t.Errorf("showDate(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// A calendar date is stored as midnight UTC and shows its own day even when
// the org's timezone is west of UTC, where that instant is the evening before.
func TestShowDateKeepsACalendarDateOnItsOwnDayWestOfUTC(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	d := time.Date(2026, time.November, 7, 0, 0, 0, 0, time.UTC)
	if got := showDate(d, datesNow, la); got != "Nov 7" {
		t.Errorf("showDate = %q, want %q", got, "Nov 7")
	}
}

// The current year is the org's: at 03:00 UTC on 1 Jan 2027 it's still 2026 in
// Los Angeles.
func TestShowDateJudgesTheCurrentYearInTheOrgsTimezone(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2027, time.January, 1, 3, 0, 0, 0, time.UTC)
	d := time.Date(2026, time.December, 28, 0, 0, 0, 0, time.UTC)
	if got := showDate(d, now, la); got != "Dec 28" {
		t.Errorf("in Los Angeles: showDate = %q, want %q", got, "Dec 28")
	}
	if got := showDate(d, now, time.UTC); got != "Dec 28, 2026" {
		t.Errorf("in UTC: showDate = %q, want %q", got, "Dec 28, 2026")
	}
}

// A timestamp reads "Nov 7, 15:04" in the org's timezone, on the org's day
// even when its UTC day differs, and carries its year outside the current one.
func TestShowTimeReadsInTheOrgsTimezone(t *testing.T) {
	t.Parallel()

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, time.November, 7, 6, 4, 0, 0, time.UTC), "Nov 7, 15:04"},
		{time.Date(2026, time.November, 6, 20, 30, 0, 0, time.UTC), "Nov 7, 05:30"},
		{time.Date(2025, time.November, 7, 6, 4, 0, 0, time.UTC), "Nov 7, 2025, 15:04"},
	} {
		if got := showTime(tc.at, datesNow, tokyo); got != tc.want {
			t.Errorf("showTime(%v) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

// Report pages always show the year, the current one included.
func TestTheYearVariantsShowTheYearInTheCurrentYear(t *testing.T) {
	t.Parallel()

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if got := showDateYear(time.Date(2026, time.November, 7, 0, 0, 0, 0, time.UTC)); got != "Nov 7, 2026" {
		t.Errorf("showDateYear = %q, want %q", got, "Nov 7, 2026")
	}
	if got := showTimeYear(time.Date(2026, time.November, 6, 20, 30, 0, 0, time.UTC), tokyo); got != "Nov 7, 2026, 05:30" {
		t.Errorf("showTimeYear = %q, want %q", got, "Nov 7, 2026, 05:30")
	}
	if got := showDayOfYear(time.Date(2026, time.November, 6, 20, 30, 0, 0, time.UTC), tokyo); got != "Nov 7, 2026" {
		t.Errorf("showDayOfYear = %q, want %q", got, "Nov 7, 2026")
	}
}

// An instant shown as a date, a Check-in's CreatedAt say, is the org's day it
// fell on, not its UTC day.
func TestShowDayOfIsTheOrgsDay(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.November, 8, 3, 0, 0, 0, time.UTC) // 7 Nov, 19:00 in LA
	if got := showDayOf(at, datesNow, la); got != "Nov 7" {
		t.Errorf("showDayOf = %q, want %q", got, "Nov 7")
	}
	if got := showDayOf(time.Time{}, datesNow, la); got != "" {
		t.Errorf("showDayOf(zero) = %q, want empty", got)
	}
}

// A page's dates follow the year rule, and a Report's always show the year.
func TestDisplayDatesShowTheYearAlwaysOnAReport(t *testing.T) {
	t.Parallel()

	d := time.Date(2026, time.November, 7, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, time.November, 7, 15, 4, 0, 0, time.UTC)
	page := displayDates{Now: datesNow, Loc: time.UTC}
	report := reportDates(time.UTC)
	for _, tc := range []struct{ name, got, want string }{
		{"page date", page.date(d), "Nov 7"},
		{"page timestamp", page.timestamp(at), "Nov 7, 15:04"},
		{"page day of", page.dayOf(at), "Nov 7"},
		{"report date", report.date(d), "Nov 7, 2026"},
		{"report timestamp", report.timestamp(at), "Nov 7, 2026, 15:04"},
		{"report day of", report.dayOf(at), "Nov 7, 2026"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// A date typed or stored as ISO text, a date Field's value say, shows as a
// date; anything else shows as typed.
func TestDisplayDatesShowAnISODateTextAsADate(t *testing.T) {
	t.Parallel()

	page := displayDates{Now: datesNow, Loc: time.UTC}
	for raw, want := range map[string]string{
		"2026-03-01": "Mar 1",
		"2025-03-01": "Mar 1, 2025",
		"soon":       "soon",
		"":           "",
	} {
		if got := page.dateText(raw); got != want {
			t.Errorf("dateText(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := reportDates(time.UTC).dateText("2026-03-01"); got != "Mar 1, 2026" {
		t.Errorf("report dateText = %q, want %q", got, "Mar 1, 2026")
	}
}
