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
	"opensro.online/server/internal/game/item/wire"
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
	resident := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(name)}] != nil
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
		changed := false
		// Legacy records are adopted only by their matching summoner slot.
		// Never fabricate an item or overwrite another retained companion.
		if legacy := c.ActiveCOS; legacy != nil {
			if ref, valid := rt.cosReference(legacy); valid && (ref.TidWord>>11 == 3 || ref.TidWord>>11 == 4) {
				for i := range c.MissionInventory {
					row := &c.MissionInventory[i]
					item, found := rt.deps.ItemReferences().ItemRefByCodename(row.Codename)
					if row.Slot == int64(legacy.InventorySlot) && row.Summon == nil && found && item != nil && wire.IsCosSummoner(item.TypeFlags()) && uint16(item.TypeIDs[3]+2) == ref.TidWord>>11 {
						row.Summon = legacy
						c.ActiveCOS = nil
						changed = true
						break
					}
				}
			}
		}
		now := rt.Now().Unix()
		for i := range c.MissionInventory {
			row := &c.MissionInventory[i]
			pet := row.Summon
			if pet == nil {
				continue
			}
			before := *pet
			changed = pet.RefreshRentalTimes(now) || changed
			ref, valid := rt.cosReference(pet)
			if !valid || row.Slot < 0 || row.Slot > 255 {
				continue
			}
			pet.InventorySlot = uint8(row.Slot)
			// 4FA430 restores only records whose alive and summoned bits are set.
			// A retained corpse remains on the item for revival, not in the world.
			if pet.CurrentHP == 0 || pet.StateFlags&1 == 0 || (ref.TidWord>>11 == 4 && pet.RentalExpiresAtUnix <= now) {
				pet.Summoned = false
				pet.StateFlags &^= cosStateSummoned
			}
			if pet.Summoned {
				pet.GID, _ = enterworld.PersistentCOSObjectID(c, ref.TidWord>>11)
			}
			if before.StateFlags != pet.StateFlags || before.GID != pet.GID || before.Summoned != pet.Summoned || before.RentalRemainingSeconds != pet.RentalRemainingSeconds || before.InventorySlot != pet.InventorySlot {
				changed = true
			}
		}
		pet := c.ActiveCOS
		if pet == nil || !pet.Summoned || pet.CurrentHP == 0 {
			return changed
		}
		ref, found := refs.CharacterRefByCodename(pet.Codename)
		if !found || ref == nil || ref.RefObjID != pet.RefObjID || !ref.CanRide || ref.TidWord&0x7fe != 0x1c6 || (ref.TidWord>>11 != 1 && ref.TidWord>>11 != 2) {
			return changed
		}
		changed = changed || !pet.Mounted
		pet.Mounted = true
		rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(division, c, rt.Now().UnixMilli()))
		return changed
	})
}
