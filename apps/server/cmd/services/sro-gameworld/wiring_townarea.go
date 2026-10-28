/*
===========================================================================

wiring_townarea.go - the thief transport's town walls (#1985)

wiring_gameplay.go is at the source-size limit, so the port-only
SRO_THIEF_CART_TOWN_AREAS wiring lives here. On (the default), the thief
transport rule also stops a cart at the town parts regioninfo.txt places
in battlefield regions (world/townarea). Off is native: the region rule
alone.

===========================================================================
*/
package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/townarea"
)

/*
================
configureThiefCartTownAreas

Runs after the native TownEntryBlocked wiring. A malformed switch or town
list refuses the boot rather than silently dropping the rule.
================
*/
func configureThiefCartTownAreas(items *action.Runtime, movementRuntime *movement.Runtime, textdataDir string) error {
	on, err := townarea.ThiefCartTownAreasFromEnv()
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	areas, err := townarea.Load(textdataDir)
	if err != nil {
		return fmt.Errorf("thief cart town areas: %w", err)
	}
	items.ThiefCartTownAreas = areas
	movementRuntime.TownAreaEntryBlocked = items.ThiefCargoTownAreaEntered
	log.Infof("job: a thief's transport also stops at %d town wall areas (%s)", areas.Len(),
		townarea.EnvThiefCartTownAreas)
	return nil
}
