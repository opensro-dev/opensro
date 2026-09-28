/*
===========================================================================

abnormal_fixture_test.go - status admission for detached gameplay fixtures.

Production prepares source facts before the authority write transaction.
Legacy in-memory fixtures have no store lock, so this adapter retains their
compact setup without making unsafe admission available to the game binary.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
applyPlayerAbnormalInDoor

Only detached fixtures use this adapter. Real-store tests exercise preparation
outside the write and call the same commit operation as production.
================
*/
func (rt *Runtime) applyPlayerAbnormalInDoor(division string, character *enterworld.Character, damaged bool, records []abnormal.Record, now int64) *playerAbnormalOwner {
	if !damaged && len(records) == 0 {
		return nil
	}
	owner := rt.newPlayerAbnormalOwner(division, character, now)
	owner.sources = rt.captureAbnormalSources(division, owner.block, records)
	owner.applyHit(damaged, records)
	return owner
}
