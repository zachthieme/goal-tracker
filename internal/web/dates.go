package web

import "time"

// The pages show every date and time in one of two formats (DESIGN.md: Dates
// and times), and this file is the only place that formats them for display:
//
//   - a date reads "Nov 7", or "Nov 7, 2025" when it falls outside the current
//     year;
//   - a timestamp reads "Nov 7, 15:04", 24-hour in the org's timezone, under
//     the same year rule.
//
// The current year is the org's, judged from the app clock. Report pages always
// show the year, through the ...Year variants. Form inputs, values compared as
// strings and query strings stay ISO (fmtDate), and datetime= attributes stay
// RFC 3339.

const (
	showDateLayout     = "Jan 2"
	showDateYearLayout = "Jan 2, 2006"
	showClockLayout    = ", 15:04"
)

// showDate renders a calendar date, stored as midnight UTC, as "Nov 7", with
// its year when it isn't the current year in loc at now. It's never shifted to
// another zone. The zero time renders as "".
func showDate(d, now time.Time, loc *time.Location) string {
	if d.IsZero() {
		return ""
	}
	if d.Year() != now.In(loc).Year() {
		return d.Format(showDateYearLayout)
	}
	return d.Format(showDateLayout)
}

// showTime renders an instant as "Nov 7, 15:04", 24-hour on the org's clock in
// loc, with its year when it isn't the current year there at now. The zero time
// renders as "".
func showTime(t, now time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	local := t.In(loc)
	if local.Year() != now.In(loc).Year() {
		return local.Format(showDateYearLayout + showClockLayout)
	}
	return local.Format(showDateLayout + showClockLayout)
}

// showDayOf renders the day an instant fell on in the org's timezone, a
// Check-in's CreatedAt say, as a date: "Nov 7".
func showDayOf(t, now time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	y, m, d := t.In(loc).Date()
	return showDate(time.Date(y, m, d, 0, 0, 0, 0, time.UTC), now, loc)
}

// showDateYear renders a calendar date as a Report page shows it, always with
// its year: "Nov 7, 2026".
func showDateYear(d time.Time) string {
	if d.IsZero() {
		return ""
	}
	return d.Format(showDateYearLayout)
}

// showTimeYear renders an instant as a Report page shows it, always with its
// year: "Nov 7, 2026, 15:04" in loc.
func showTimeYear(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(showDateYearLayout + showClockLayout)
}

// showDayOfYear renders the day an instant fell on in loc as a Report page
// shows it, always with its year: "Nov 7, 2026".
func showDayOfYear(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(showDateYearLayout)
}

// displayDates is the clock a page shows its dates against: the app clock's
// Now (Service.Now) and the org's timezone (Service.Timezone). Year is set on
// Report pages, which always show the year.
type displayDates struct {
	Now  time.Time
	Loc  *time.Location
	Year bool
}

// displayDates is the clock the pages show dates against now.
func (s *Server) displayDates() displayDates {
	return displayDates{Now: s.svc.Now(), Loc: s.svc.Timezone()}
}

// reportDates is the clock a Report page shows its dates against: every one
// with its year, its times in loc.
func reportDates(loc *time.Location) displayDates {
	return displayDates{Loc: loc, Year: true}
}

// date renders a calendar date (showDate, or showDateYear on a Report).
func (p displayDates) date(d time.Time) string {
	if p.Year {
		return showDateYear(d)
	}
	return showDate(d, p.Now, p.Loc)
}

// timestamp renders an instant (showTime, or showTimeYear on a Report).
func (p displayDates) timestamp(t time.Time) string {
	if p.Year {
		return showTimeYear(t, p.Loc)
	}
	return showTime(t, p.Now, p.Loc)
}

// dayOf renders the day an instant fell on in the org's timezone (showDayOf,
// or showDayOfYear on a Report).
func (p displayDates) dayOf(t time.Time) string {
	if p.Year {
		return showDayOfYear(t, p.Loc)
	}
	return showDayOf(t, p.Now, p.Loc)
}

// dateText renders a date held as ISO text, as a date input submits it and a
// date Field stores it, as a date; anything that isn't one renders as typed.
func (p displayDates) dateText(raw string) string {
	d, err := time.Parse(dateLayout, raw)
	if err != nil {
		return raw
	}
	return p.date(d)
}
