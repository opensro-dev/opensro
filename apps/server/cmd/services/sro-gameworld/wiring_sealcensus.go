/*
===========================================================================

wiring_sealcensus.go - the staff seal census of the live shard (#1691)

Port-only, not native (SRO_SEAL_CENSUS, owner-approved from the #1604
P13 vote): with the flag on, the economy service answers the staff
console's census route (/economy/v1/sealcensus, platform/economy
/census.go). A run copies the store outside every door, counts seal
items by degree and tier over the copy, and discards it; nothing runs
on the tick. Off (default) the route does not exist.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/economy"
)

/*
================
wireSealCensus
================
*/
func (game *gameplayPlane) wireSealCensus(application *gameWorldApplication) {
	if !economy.SealCensusFromEnv() {
		return
	}
	if application.economy == nil {
		log.Warnf("seal census: %s is on but the economy aggregates are off (%s=0); the route stays closed",
			economy.EnvSealCensus, economy.EnvSnapshotHours)
		return
	}
	application.economy.SetSealCensusEnabled(true)
	log.Infof("seal census: the staff seal census route is open (%s, port-only, not native)", economy.EnvSealCensus)
}
