/*
===========================================================================

fortresses.go - P11 GET /public/v1/fortresses: who holds every fortress

The community site's fortress owners panel (opensro#3519, opensro-web#141).
Every fortress the shard serves, in catalog order, with its occupying
guild (or none), its tax rate, and the shard's fortress war state.

The war state is shard-wide, as in the original: one period flags byte
covers every fortress (the fortress list's war 1 / request 2 / tax 4),
and every fortress fights in the same window. So the state and the next
registration and war windows are read once, from the same schedule
events P8 publishes (Sources.Schedule), never from a second timetable.

The crest is not served: the port keeps a guild's crest as the wire's
crest parameter only (domain.GuildRecord.CrestParam) and holds no crest
image a page could draw, so the site shows its own emblem instead.

===========================================================================
*/

package publicstats

import (
	"net/http"
	"time"
)

// The schedule kinds of the fortress windows (P8 ScheduleEvent.Kind). The
// public contract owns the kind names: the GameWorld's schedule writes
// them and this answer reads them back.
const (
	ScheduleKindFortress             = "fortress"
	ScheduleKindFortressRegistration = "fortress-registration"
)

// The answer's war states.
const (
	fortressStateWar          = "war"
	fortressStateRegistration = "registration"
	fortressStatePeace        = "peace"
)

/*
================
FortressEntry

One fortress as the GameWorld wiring reads it: the town of its codename in
lower case ("jangan"), the occupying guild's name ("" for none) and its
signed tax rate in percent.
================
*/
type FortressEntry struct {
	Town    string
	Owner   string
	TaxRate int
}

/*
================
fortressWindow

One schedule event as the answer's window, or nil when the schedule has
none of that kind.
================
*/
func fortressWindow(events []ScheduledEvent, kind string) *FortressWindow {
	for _, event := range events {
		if event.Kind != kind {
			continue
		}
		window := &FortressWindow{StartsAt: event.Start.UTC().Format(time.RFC3339), Running: event.Running}
		if !event.End.IsZero() {
			ends := event.End.UTC().Format(time.RFC3339)
			window.EndsAt = &ends
		}
		return window
	}
	return nil
}

/*
================
Service.fortresses
================
*/
func (s *Service) fortresses(*http.Request) (any, error) {
	now := s.now()
	out := FortressesResponse{ServerTime: now.UTC().Format(time.RFC3339), State: fortressStatePeace,
		Fortresses: []FortressRow{}}
	if s.src.Schedule != nil {
		events := s.src.Schedule(now)
		out.War = fortressWindow(events, ScheduleKindFortress)
		out.Registration = fortressWindow(events, ScheduleKindFortressRegistration)
	}
	// The war outranks registration: the original closes registration
	// before the war opens, but a schedule that overlaps them is at war.
	if out.War != nil && out.War.Running {
		out.State = fortressStateWar
	} else if out.Registration != nil && out.Registration.Running {
		out.State = fortressStateRegistration
	}
	if s.src.Fortresses == nil {
		return out, nil
	}
	for _, entry := range s.src.Fortresses() {
		row := FortressRow{Fortress: entry.Town, TaxRate: entry.TaxRate}
		if entry.Owner != "" {
			owner := entry.Owner
			row.Owner = &owner
		}
		out.Fortresses = append(out.Fortresses, row)
	}
	return out, nil
}
