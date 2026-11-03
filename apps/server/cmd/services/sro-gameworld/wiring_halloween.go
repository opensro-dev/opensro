/*
===========================================================================

wiring_halloween.go - the Halloween Day event's composition

Reads the event's switch and UTC window once at boot (port-only, not
native; progression/halloween.go owns the rule). On without a valid
window the event stays off and the boot log says so; the entry door's
expired-box cleanup hook is bound in wiring_gameplay.go's register.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/progression"
)

/*
================
halloweenEventFromEnv

The composition read of SRO_HALLOWEEN_EVENT and its window, with the
boot-log lines every other port-only rule gets.
================
*/
func halloweenEventFromEnv() progression.HalloweenEvent {
	event := progression.HalloweenEventFromEnv()
	if event.Enabled() && !event.WindowValid() {
		log.Warnf("halloween: %s is on but %s is not a valid START/END UTC window; the event stays off",
			progression.EnvHalloweenEvent, progression.EnvHalloweenEventWindow)
	}
	if event.Enabled() && event.WindowValid() {
		log.Infof("halloween: the Halloween Day event runs inside its configured window (%s)", progression.EnvHalloweenEvent)
	}
	return event
}

/*
================
halloweenHelmetSealFromEnv

The composition read of SRO_HALLOWEEN_HELMET_SEAL: its own switch, on
after the event ends, so the helmets the event paid stay with the
character that earned them.
================
*/
func halloweenHelmetSealFromEnv() progression.HalloweenHelmetSeal {
	seal := progression.HalloweenHelmetSealFromEnv()
	if seal.Enabled() {
		log.Infof("halloween: the event helmets are sealed at every transfer door (%s)", progression.EnvHalloweenHelmetSeal)
	}
	return seal
}
