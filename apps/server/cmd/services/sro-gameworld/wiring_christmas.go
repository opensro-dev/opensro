/*
===========================================================================

wiring_christmas.go - the Christmas event's composition

Reads the event's switch and UTC window once at boot (port-only, not
native; progression/christmas.go owns the rule, action/event_groups.go
the event groups it runs). On without a valid window the event stays off
and the boot log says so.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

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
