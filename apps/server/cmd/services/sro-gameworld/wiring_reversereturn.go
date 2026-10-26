/*
===========================================================================

wiring_reversereturn.go - the reverse return scroll's map destinations

wiring_gameplay.go is at the source-size limit, so the port-only
SRO_REVERSE_RETURN_MAP wiring lives here. Off is native: the scroll offers
the last recall and last death points only. On, the action runtime serves
the imported VSRO optional-teleport table when the game data carries it
(world/optionalteleport), else its inferred table. The client receives
whichever points the server published; it is not told which source
built them.

===========================================================================
*/
package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/world/optionalteleport"
)

/*
================
configureReverseReturnMap

Runs after ConfigurePortals and the monster authority. A malformed import
refuses the boot rather than falling back to the inferred table silently.
================
*/
func configureReverseReturnMap(items *action.Runtime, textdataDir string) error {
	if !action.ReverseReturnMapFromEnv() {
		return nil
	}
	imported, err := optionalteleport.Load(textdataDir)
	if err != nil {
		return fmt.Errorf("reverse return map: %w", err)
	}
	items.ConfigureReverseReturnMap(true, imported)
	source := "inferred"
	if len(imported) > 0 {
		source = "imported " + optionalteleport.File
	}
	log.Infof("reverse return: map destinations ON (%s), %d %s points", action.EnvReverseReturnMap,
		len(items.ReverseReturnMapPoints()), source)
	return nil
}
