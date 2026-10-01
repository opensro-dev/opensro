/*
===========================================================================

cosrestore.go - restore the riding relation before entry serialization

A newly admitted actor recreates its saved vehicle beside the owner, then
binds it before bootstrap projects speed and ride state. An existing logical
actor retains its current parked vehicle on repeated transport admission.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"strings"
)

/*
================
restoreCharacterCOS

4FA430 recreates saved nonpersistent vehicles and calls 4EC640 when usable.
The caller holds the division lock. This updates authority before packets
are composed; bootstrap never invents a relation solely for presentation.
================
*/
func (rt *Runtime) restoreCharacterCOS(division, name string) {
	rt.petMu.Lock()
	resident := rt.petSessions[petOwnerKey{division, strings.ToLower(name)}] != nil
	rt.petMu.Unlock()
	if resident {
		return
	}
	c := rt.findCharacter(division, name)
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if c == nil || !ok {
		return
	}
	rt.deps.Update(c, "restore-cos-ride", func() bool {
		pet := c.ActiveCOS
		if pet == nil || !pet.Summoned || pet.CurrentHP == 0 {
			return false
		}
		ref, found := refs.CharacterRefByCodename(pet.Codename)
		if !found || ref == nil || ref.RefObjID != pet.RefObjID || !ref.CanRide || ref.TidWord&0x7fe != 0x1c6 || (ref.TidWord>>11 != 1 && ref.TidWord>>11 != 2) {
			return false
		}
		changed := !pet.Mounted
		pet.Mounted = true
		rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(division, c, rt.Now().UnixMilli()))
		return changed
	})
}
