/*
===========================================================================

wiring_eventsweep.go - the boot sweep of expired event boxes

Port-only, not native. The entry door removes a finished Halloween
window's jewel boxes from a character when they enter the world
(RemoveExpiredHalloweenBoxes). A character who never enters again keeps
the boxes in the stored record, so once the window has closed the boot
runs the same removal over every stored character before admission
opens (#3587). The removal rule stays in the action runtime; this file
only walks the roster and commits the holders in one transaction.

deleted_characters is not swept: it is a raw archive that nothing
restores, so a box in it can never reach a player. A character pending
deletion is still on the roster, and a restore would bring it back, so
the sweep covers it.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
)

// eventBoxSweepLabel is the sweep's door label in the store's commit log.
const eventBoxSweepLabel = "halloween-box-sweep"

/*
================
sweepExpiredHalloweenBoxes

Removes a finished window's boxes from every stored character of every
division and commits them in one transaction. A sweep before the window
has closed, or with the event off, finds no holder and writes nothing.
Runs before admission, so no session races the roster.
================
*/
func sweepExpiredHalloweenBoxes(authority *store.Store, items *action.Runtime) {
	swept := 0
	for _, divisionID := range authority.DivisionIDs() {
		roster := authority.Characters().CharactersForDivision(divisionID)
		holders := items.HalloweenBoxHolders(roster)
		if len(holders) == 0 {
			continue
		}
		changed := authority.UpdateCharacters(holders, eventBoxSweepLabel, func() bool {
			removed := false
			for _, character := range holders {
				if items.RemoveExpiredHalloweenBoxes(character) {
					removed = true
					swept++
				}
			}
			return removed
		})
		if !changed {
			log.Warnf("halloween: the boot sweep of %s found %d holder(s) but committed nothing", divisionID, len(holders))
		}
	}
	if swept > 0 {
		log.Infof("halloween: the window has closed; the boot sweep took the jewel boxes from %d stored character(s)", swept)
	}
}
