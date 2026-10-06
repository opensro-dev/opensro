/*
===========================================================================

cossatiety.go - online companion hunger commits and shared death publication

The existing session tick supplies elapsed time. Hunger uses the same death
owner as combat, preventing mounted, pickup and abnormal-state leftovers.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
advancePetSatiety

The caller holds the division operation lock. Do not retain a detached pet
copy across the transaction; feeding and death update the same durable HGP.
================
*/
func (rt *Runtime) advancePetSatiety(key petOwnerKey, nowMs int64) []simulation.Frame {
	rt.petMu.Lock()
	state := rt.petSessions[key]
	rt.petMu.Unlock()
	if state == nil {
		return nil
	}
	var frames []simulation.Frame
	rt.deps.Update(state.character, "cos-satiety", func() bool {
		c := state.character
		pet := c.CompanionByGID(key.gid)
		ref, valid := rt.cosReference(pet)
		if valid && (ref.TidWord>>11 == 4 || ref.TidWord>>11 == 5) && pet.RentalExpiresAtUnix != 0 && pet.RentalExpiresAtUnix <= nowMs/1000 {
			if ref.TidWord>>11 == 5 {
				c.RemoveMercenary(pet.GID)
			}
			pet.Summoned = false
			pet.StateFlags &^= cosStateSummoned
			pet.RefreshRentalTimes(nowMs / 1000)
			for _, frame := range rt.retireCosRuntime(key.division, c, pet.GID) {
				frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
			}
			despawn := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()}
			for _, frame := range companionItemStateFrames(c, pet) {
				frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
			}
			state.public = append(state.public, despawn)
			frames = append(frames, simulation.Frame{Opcode: despawn.Opcode, Payload: despawn.Payload})
			return true
		}
		if !valid || !pet.Summoned || pet.CurrentHP == 0 || ref.TidWord>>11 != 3 || ref.SatietyMinutes == 0 {
			state.satiety = companion.SatietyClock{}
			return false
		}
		before := pet.Satiety
		after := state.satiety.Advance(pet.GID, nowMs, ref.SatietyMinutes, before)
		if before == after && !state.satiety.Exhausted {
			return false
		}
		owner := rt.newCosAbnormalOwnerForPet(key.division, c, pet, nowMs)
		pet.Satiety = after
		if companion.PublishSatiety(before, after) {
			frames = append(frames, simulation.Frame{Opcode: cosPetUpdateOpcode,
				Payload: wire.NewWriter(7).U32(pet.GID).U8(cosSatietyUpdateKind).U16(after).Payload()})
		}
		if after == 0 {
			pet.CurrentHP = 0
			owner.fatal, owner.hpChanged = true, true
			owner.commit()
			for _, frame := range owner.private {
				frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
			}
			public := rt.cosAbnormalPublication(pet.GID, owner)
			state.public = append(state.public, public...)
			for _, frame := range public {
				frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
			}
		}
		return true
	})
	return frames
}
