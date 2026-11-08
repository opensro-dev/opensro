/*
===========================================================================

wiring_alexandria.go - the SRO_ALEXANDRIA switch on the gameplay plane

Port-only, not native: Alexandria's regions exist only with
SRO_ALEXANDRIA. wiring_gameplay.go is at the source-size limit, so the
plane's two Alexandria hooks live here: the water validator hides the
regions while the switch is off, and the world-entry blob tells the
client whether its M window shows Alexandria's wider map.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/movement"
)

/*
================
wireAlexandria

wiring.go's alexandria.Check has already refused a malformed value, so
the switch is read here without its error.
================
*/
func wireAlexandria(water *movement.WaterValidator, deps *enterworld.Deps, textdataDir string) error {
	hidden, err := alexandria.Hidden(textdataDir)
	if err != nil {
		return err
	}
	water.HideAlexandria(hidden)
	deps.Alexandria, _ = alexandria.EnabledFromEnv()
	return nil
}
