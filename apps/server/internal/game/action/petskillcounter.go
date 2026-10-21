/*
===========================================================================
petskillcounter.go - port-only owner HUD counters, from authoritative pet memory.

The existing pet tick sends changed snapshots privately. Counts never persist;
the 40-attack cycle preserves both the eight- and ten-hit passive progress.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const petSkillCounterOpcode uint16 = 0x3fb6

/*
================
petSkillCounterFrame

Caller holds the division's read/write door. No audience/global GID lookup.
================
*/
func petSkillCounterFrame(state *petSession, gid uint32) wire.Frame {
	if state == nil || state.character == nil {
		return wire.Frame{}
	}
	pet := state.character.CompanionByGID(gid)
	if pet == nil || companion.PetKind(pet.Codename) == "" {
		return wire.Frame{}
	}
	attacks, bear, owner, resolve := 0, 0, 0, uint8(0)
	wolf, ownerExecute, blood, ownerBlood := uint8(0), uint8(0), uint8(0), uint8(0)
	hunt, target := uint8(0), uint32(0)
	if pet.Summoned && pet.CurrentHP > 0 && enterworld.CharacterAlive(state.character) {
		attacks = state.trained.attacks % 40
		bear, owner = state.trained.counterHits[0], state.trained.counterHits[1]
		resolve = pet.ResolveStacks
		wolf, ownerExecute = state.wolfHits[0], state.wolfHits[1]
		blood, ownerBlood = state.wolf.blood[0], state.wolf.blood[1]
		hunt, target = state.wolf.huntHits, state.wolf.huntTarget
	}
	return wire.Frame{Opcode: petSkillCounterOpcode, Payload: wire.NewWriter(17).U32(pet.GID).
		U8(uint8(attacks)).U8(uint8(bear)).U8(uint8(owner)).U8(resolve).U8(wolf).U8(ownerExecute).U8(blood).U8(ownerBlood).U8(hunt).U32(target).Payload()}
}
