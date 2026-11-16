/*
===========================================================================

wiring_christmas.go - the Christmas events' composition

Reads the switch, the UTC window and the mode once at boot (port-only,
not native; progression/christmas.go and sook_christmas.go own the
rules, progression/christmas_mode.go which one runs,
action/event_groups.go the event groups they run). On without a valid
window an event stays off and the boot log says so.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

/*
================
christmasEventFromEnv

The composition read of SRO_CHRISTMAS_EVENT and its window, with the
boot-log lines every other port-only rule gets.
================
*/
func christmasEventFromEnv() progression.ChristmasEvent {
	event := progression.ChristmasEventFromEnv()
	if event.Enabled() && !event.WindowValid() {
		log.Warnf("christmas: %s is on but %s is not a valid START/END UTC window; the event stays off",
			progression.EnvChristmasEvent, progression.EnvChristmasEventWindow)
	}
	if event.Enabled() && event.WindowValid() {
		log.Infof("christmas: the Christmas event runs inside its configured window (%s)", progression.EnvChristmasEvent)
	}
	return event
}

/*
================
sookChristmasFromEnv

The composition read of So-Ok's Christmas Gift (SRO_CHRISTMAS_EVENT in
mode so_ok_2006) with its boot-log lines. A box chance or gift table that
does not parse, or a gift naming an item the data does not hold, keeps
the event's defaults and says so: a typo never hands out nothing.
================
*/
func sookChristmasFromEnv(items enterworld.ItemRefSource) progression.SoOkChristmas {
	mode := progression.ChristmasModeFromEnv()
	if mode == progression.ChristmasModeUnknown {
		log.Warnf("christmas: %s names no Christmas event (winter or so_ok_2006); no Christmas event runs", progression.EnvChristmasEventMode)
	}
	event := progression.SoOkChristmasFromEnv()
	if mode != progression.ChristmasModeSoOk2006 {
		return event
	}
	if err := event.ConfigErr(); err != nil {
		log.Warnf("christmas: %v; the default is kept", err)
	}
	if items != nil {
		for _, gift := range event.Gifts() {
			if _, ok := items.ItemRefByCodename(gift.Codename); !ok {
				log.Warnf("christmas: %s names %s, which the item data does not hold; So-Ok's default gift table is kept", progression.EnvSoOkChristmasGifts, gift.Codename)
				event = event.WithGifts(progression.SoOkChristmasDefaultGifts())
				break
			}
		}
	}
	if event.Enabled() && !event.WindowValid() {
		log.Warnf("christmas: %s is on but %s is not a valid START/END UTC window; So-Ok's Christmas Gift stays off",
			progression.EnvChristmasEvent, progression.EnvChristmasEventWindow)
	}
	if event.Enabled() && event.WindowValid() {
		log.Infof("christmas: So-Ok's Christmas Gift runs inside its configured window (%s=%s, box chance %.2f%%)",
			progression.EnvChristmasEventMode, progression.ChristmasModeSoOk2006, event.BoxChancePercent())
	}
	return event
}

/*
================
sookChristmasNpcLook

So-ok's Santa look for So-Ok's Christmas Gift's window, or nil when the
event does not run in this boot's posture (the dressed roster is built
once, so the window read at boot is the one that dresses her).
================
*/
func sookChristmasNpcLook() *enterworld.NpcLookConfig {
	event := progression.SoOkChristmasFromEnv()
	startsAt, endsAt, valid := event.Window()
	if !event.Enabled() || !valid {
		return nil
	}
	return &enterworld.NpcLookConfig{
		Codename:     progression.SoOkChristmasHostCodename,
		LookCodename: progression.SoOkChristmasLookCodename,
		StartsAt:     startsAt,
		EndsAt:       endsAt,
	}
}
