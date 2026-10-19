package domain

import (
	"sync"
	"time"
)

// timezones caches the locations loadTimezone has loaded, by name:
// time.LoadLocation reads the zone database on every call.
var timezones sync.Map // string → *time.Location

// loadTimezone is the zone name names, and true, for an IANA name such as
// America/Los_Angeles (CONTEXT.md: Timezone). An empty name, "Local", which is
// the host's zone rather than an IANA name, and any name time.LoadLocation
// refuses give nil and false; an empty name is never passed to it, which
// would return UTC. It is how the directory sync checks a Timezone before
// storing it, and how GoalTimezone reads an Owner's.
func loadTimezone(name string) (*time.Location, bool) {
	if name == "" || name == "Local" {
		return nil, false
	}
	if loc, ok := timezones.Load(name); ok {
		return loc.(*time.Location), true
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	timezones.Store(name, loc)
	return loc, true
}

// GoalTimezone is the timezone g's own dates are judged in: "A Goal's own
// dates and cadence are judged in its Owner's Timezone: whether it is Stale,
// whether a Milestone or the back-to-Green date has passed, whether a Metric is
// off target, and its Run off Green. An Ownerless Goal uses the company's"
// (CONTEXT.md: Timezone). It reads g's current Owner, so after a Handoff the
// new Owner's applies; an Owner with no Timezone uses the company's too.
func (s *Service) GoalTimezone(g Goal) *time.Location {
	if !g.Ownerless {
		if loc, ok := loadTimezone(g.Owner.Timezone); ok {
			return loc
		}
	}
	return s.loc
}

// goalToday is today's calendar date in g's timezone (see GoalTimezone and
// orgDate).
func (s *Service) goalToday(g Goal) time.Time {
	return orgDate(s.clock.Now(), s.GoalTimezone(g))
}
