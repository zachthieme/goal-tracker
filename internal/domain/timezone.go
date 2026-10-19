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
// storing it; the helper that judges a Goal's own dates in its Owner's
// Timezone will sit beside it.
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
