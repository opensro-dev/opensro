/*
===========================================================================

christmas_mode.go - which Christmas event SRO_CHRISTMAS_EVENT runs

Port-only, not native. SRO_CHRISTMAS_EVENT and its window turn "the
Christmas event" on; SRO_CHRISTMAS_EVENT_MODE picks which one:

  - "winter" (or unset): the winter event, Winter Princess and Knights:
    Snow Slaves, snow flakes and the Server Missions that summon the
    White Knights and the Princesses (christmas.go, winter_missions.go). Unset keeps every shard configured before the mode
    existed exactly as it was.
  - "so_ok_2006": So-Ok's Christmas Gift, the December 2006 event
    (sook_christmas.go): jewelry boxes from ordinary monsters, Santa
    So-Ok's random gifts, snowfalls on the winter event's timetable.

Each event reads the one mode and runs only in its own, so the two can
never run at once. An unknown mode runs neither; the composition root
says so at boot.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
)

// EnvChristmasEventMode picks the Christmas event SRO_CHRISTMAS_EVENT runs.
const EnvChristmasEventMode = "SRO_CHRISTMAS_EVENT_MODE"

/*
================
ChristmasMode
================
*/
type ChristmasMode string

const (
	// ChristmasModeWinter is the winter event, the default.
	ChristmasModeWinter ChristmasMode = "winter"
	// ChristmasModeSoOk2006 is So-Ok's Christmas Gift (December 2006).
	ChristmasModeSoOk2006 ChristmasMode = "so_ok_2006"
	// ChristmasModeUnknown is any other value: no Christmas event runs.
	ChristmasModeUnknown ChristmasMode = ""
)

/*
================
ChristmasModeFromEnv

Unset or blank is the winter event; the value is case-insensitive.
================
*/
func ChristmasModeFromEnv() ChristmasMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvChristmasEventMode))) {
	case "", string(ChristmasModeWinter):
		return ChristmasModeWinter
	case string(ChristmasModeSoOk2006):
		return ChristmasModeSoOk2006
	}
	return ChristmasModeUnknown
}
