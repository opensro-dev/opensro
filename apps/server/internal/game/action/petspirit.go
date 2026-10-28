/*
===========================================================================
petspirit.go - port-only Unbroken Spirit, owned by the normal pet tick.

Resolve is transient. HP grows only by the newly earned maximum, and drops
clamp without killing. A successful empowered cast spends stacks atomically.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const petSpiritOpcode uint16 = 0x3fb5
const petSpiritLifetime int64 = 15000

/*
================
spiritBraced
================
*/
func (rt *Runtime) spiritBraced(owner *enterworld.Character, pet *domain.CharacterCOS) bool {
	skill, _, ok := companion.Spirit(pet)
	return rt.petExpansionAllowed(owner, pet) && ok && skill.Option >= 2 && pet.ResolveStacks == companion.ResolveLimit
}

/*
================
petSpiritFrame
================
*/
func petSpiritFrame(pet *domain.CharacterCOS, until, now int64) wire.Frame {
	skill, _, _ := companion.Spirit(pet)
	return wire.Frame{Opcode: petSpiritOpcode, Payload: wire.NewWriter(10).U32(pet.GID).
		U8(pet.ResolveStacks).U8(uint8(skill.Option)).U32(uint32(max(int64(0), until-now))).Payload()}
}

/*
================
setPetSpirit

Called inside the owning character's transaction; immutable reference stays intact.
================
*/
func (rt *Runtime) setPetSpirit(pet *domain.CharacterCOS, stacks uint8) {
	before, ok := rt.cosReference(pet)
	pet.ResolveStacks = stacks
	after, found := rt.cosReference(pet)
	if !ok || !found || pet.CurrentHP == 0 {
		return
	}
	if after.MaxHP > before.MaxHP {
		pet.CurrentHP += after.MaxHP - before.MaxHP
	}
	if after.MaxHP < before.MaxHP && pet.CurrentHP > 0 {
		// Losing the bonus maximum takes the temporary HP with it: the
		// bear keeps only the HP it earned against the smaller maximum,
		// floored at 1 so the drop itself never kills (petspirit.go's
		// "drops clamp without killing", checklist item 1).
		removed := before.MaxHP - after.MaxHP
		if pet.CurrentHP > removed {
			pet.CurrentHP -= removed
		} else {
			pet.CurrentHP = 1
		}
	}
	pet.CurrentHP = min(pet.CurrentHP, after.MaxHP)
}

/*
================
gainPetSpirit

One stack per landed monster impact, including shielded hits; blocks do not count.
================
*/
func (rt *Runtime) gainPetSpirit(division string, owner *enterworld.Character, pet *domain.CharacterCOS, now int64) []wire.Frame {
	if !rt.petExpansionAllowed(owner, pet) || rt.inTown(division, owner) || pet.CurrentHP == 0 {
		return nil
	}
	skill, _, ok := companion.Spirit(pet)
	if !ok {
		return nil
	}
	state := rt.petSessionFor(division, owner.Name, pet.GID)
	if state == nil {
		return nil
	}
	if state.trained.resolveBook != skill.ID && pet.ResolveStacks > 0 {
		rt.setPetSpirit(pet, 0)
	}
	state.trained.resolveBook = skill.ID
	state.trained.resolveUntil = now + petSpiritLifetime
	rt.setPetSpirit(pet, min(companion.ResolveLimit, pet.ResolveStacks+1))
	return []wire.Frame{petSpiritFrame(pet, state.trained.resolveUntil, now)}
}

/*
================
spendPetSpirit

Only successful Roar/Shield write doors call this; refusals preserve Resolve.
================
*/
func (rt *Runtime) spendPetSpirit(owner *enterworld.Character, pet *domain.CharacterCOS) bool {
	skill, _, ok := companion.Spirit(pet)
	if !rt.petExpansionAllowed(owner, pet) || !ok || skill.Option != 3 || pet.ResolveStacks != companion.ResolveLimit {
		return false
	}
	rt.setPetSpirit(pet, 0)
	return true
}

/*
================
advancePetSpirit

Expiry, death, town, slot changes and resummons cannot retain a free heal.
================
*/
func (rt *Runtime) advancePetSpirit(key petOwnerKey, now int64) []simulation.Frame {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil || state.character == nil {
		return nil
	}
	// Runs for every pet per tick: copy the owner only when the pet
	// still holds Resolve stacks to expire (#1943).
	owner := rt.characterSnapshotAdmitted(key.division, state.character, func(c *enterworld.Character) bool {
		pet := c.CompanionByGID(key.gid)
		return pet != nil && pet.ResolveStacks != 0
	})
	if owner == nil {
		return nil
	}
	pet := owner.CompanionByGID(key.gid)
	if pet == nil || pet.ResolveStacks == 0 {
		return nil
	}
	skill, _, learned := companion.Spirit(pet)
	if learned && skill.ID == state.trained.resolveBook && rt.petExpansionAllowed(owner, pet) && pet.Summoned && pet.CurrentHP > 0 &&
		enterworld.CharacterAlive(owner) && !rt.inTown(key.division, owner) && now < state.trained.resolveUntil {
		return nil
	}
	// updated is captured inside the expire door; the companion is never
	// re-read unlocked after it closes (it may be unsummoned by then -
	// checklist item 2).
	var updated *domain.CharacterCOS
	if !rt.deps.UpdateDeferred(state.character, "pet-spirit-expire", func() bool {
		live := state.character.CompanionByGID(key.gid)
		if live == nil {
			return false
		}
		rt.setPetSpirit(live, 0)
		copy := *live
		updated = &copy
		return true
	}) {
		return nil
	}
	state.public = append(state.public, petSpiritFrame(updated, now, now), wire.Frame{Opcode: simulation.OpVitalsUpdate,
		Payload: simulation.HPRefreshPayload(updated.GID, 0, updated.CurrentHP)})
	return simFrames(append([]wire.Frame{petSpiritFrame(updated, now, now), {Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(updated.GID, 0, updated.CurrentHP)}}, rt.petTrainingStateFrames(updated, now)...))
}
