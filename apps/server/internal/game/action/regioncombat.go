/*
===========================================================================

regioncombat.go - the runtime's battlefield table

Every action owner asks the same table whether a region is a town or a
battlefield. The table is the operator's derived data
(worldgeom.RegionCombatFile); without it no region is known.

===========================================================================
*/
package action

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	worldgeom "opensro.online/server/internal/game/world"
)

/*
================
LoadRegionCombat

The battlefield table from the derived data, for the runtime and its
monster population. Without it the runtime serves with no region known,
and says so once.
================
*/
func (rt *Runtime) LoadRegionCombat(derivedDir string) error {
	table, err := worldgeom.LoadRegionCombat(derivedDir)
	if err != nil {
		return fmt.Errorf("region combat: %w", err)
	}
	if table == nil {
		log.Warnf("region combat: no %s in the derived game data; no region is a town or a battlefield, so player combat is off",
			worldgeom.RegionCombatFile)
	}
	rt.ConfigureRegionCombat(table)
	return nil
}

/*
================
ConfigureRegionCombat

Installs the battlefield table before the runtime serves; nil leaves every
region unknown.
================
*/
func (rt *Runtime) ConfigureRegionCombat(table *worldgeom.RegionCombat) {
	rt.regionCombat = table
	if rt.Monsters != nil {
		rt.Monsters.SetRegionCombat(table)
	}
}

/*
================
townRegion

A town is a region where players may not fight (the safe zones).
================
*/
func (rt *Runtime) townRegion(regionID uint16) bool {
	return rt.regionCombat.Town(regionID)
}

/*
================
battlefieldRegion

A region whose battlefield flag is set: players may fight there.
================
*/
func (rt *Runtime) battlefieldRegion(regionID uint16) bool {
	return rt.regionCombat.Battlefield(regionID)
}
