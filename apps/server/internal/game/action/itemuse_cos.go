/*
===========================================================================

itemuse_cos.go - pet (COS) item use: potions, cures, revival

===========================================================================
*/

package action

import (
	"encoding/binary"

	"opensro.online/server/internal/domain"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// petPotionLane is the 1.1 s lock the pet item use natively starts on the owner
// for TID4 4/5/7.
/*
================
petPotionLane
================
*/
func petPotionLane(tid4 int64) (int, bool) {
	switch tid4 {
	case 4:
		return 0, true
	case 5:
		return 1, true
	case 7:
		return 2, true
	default:
		return 0, false
	}
}

/*
================
readCosGID
================
*/
func readCosGID(tail []byte) (uint32, bool) {
	if len(tail) != 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(tail), true
}

// applyCosItemUse is the native pet item use trimmed to the v1.150 pet UI:
// potions TID4 4/5/7,
// revival TID4 6, and the TID3 2 TID4 7 cure. It runs inside the item-use door.
/*
================
applyCosItemUse
================
*/
func (rt *Runtime) applyCosItemUse(
	divisionID string,
	character *enterworld.Character,
	ref *enterworld.ItemRef,
	family itemUseFamily,
	rowIndex int,
	request wire.ItemUseRequest,
	tail []byte,
	nowMs int64,
	result *OpResult,
) bool {
	switch family {
	case itemUsePetCure:
		return rt.applyPetCure(divisionID, character, ref, rowIndex, request, tail, nowMs, result)
	case itemUsePetRevive:
		return rt.applyPetRevival(character, ref, rowIndex, request, tail, result)
	case itemUsePetFeed:
		return rt.applyPetFeed(petFeedUse{character: character, ref: ref, row: rowIndex, request: request, tail: tail}, result)
	case itemUsePetPotion:
		return rt.applyPetPotion(divisionID, character, ref, rowIndex, request, tail, nowMs, result)
	default:
		return false
	}
}

/*
================
petItemVisualTarget

The object a pet item's sparkle plays on. The native item use
sends 0x305C with the gid the item's handler leaves in its out argument,
the user's by default. The pet item use writes the pet's gid there for the potions
(TID4 4/5/7: the gid taken from pet+8) and for
the HGP potion (TID4 9); the revival scroll (6) and
the pet cure write nothing, so they sparkle on the owner. 0 keeps the user.
================
*/
func petItemVisualTarget(family itemUseFamily, tail []byte) uint32 {
	if family != itemUsePetPotion && family != itemUsePetFeed {
		return 0
	}
	gid, ok := readCosGID(tail)
	if !ok {
		return 0
	}
	return gid
}

/*
================
livePet
================
*/
func (rt *Runtime) livePet(character *enterworld.Character, gid uint32) bool {
	pet := character.CompanionByGID(gid)
	ref, valid := rt.cosReference(pet)
	return valid && ref.TidWord>>11 != domain.PickupPetBand && pet != nil && pet.BroodStage == 0 && pet.Summoned && pet.CurrentHP > 0 && pet.GID == gid && pet.GID != 0
}

/*
================
applyPetCure
================
*/
func (rt *Runtime) applyPetCure(
	divisionID string,
	character *enterworld.Character,
	ref *enterworld.ItemRef,
	rowIndex int,
	request wire.ItemUseRequest,
	tail []byte,
	nowMs int64,
	result *OpResult,
) bool {
	gid, ok := readCosGID(tail)
	if !ok || !rt.livePet(character, gid) {
		*result = itemUseFailure(wire.ErrCodeCosRefused)
		return false
	}
	owner := rt.newCosAbnormalOwnerForPet(divisionID, character, character.CompanionByGID(gid), nowMs)
	stored := [6]int32{}
	for i := range ref.CureLevels {
		stored[i] = int32(ref.CureLevels[i])
	}
	// Natively the abnormal cure runs with the item's level words and null mask/limit.
	// There is no reuse lock on this arm.
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: divisionID, character: character.Name}}
	owner.changed = owner.block.Cure(owner, &stored, nil, nil, -1, random.Rand)
	if random.err != nil {
		return false
	}
	owner.commit()
	remaining := rt.consumeItemUseRow(character, rowIndex)
	published := rt.cosAbnormalPublication(gid, owner)
	frames := []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}
	frames = append(frames, published...)
	*result = OpResult{Frames: frames, Broadcast: published}
	result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
	return true
}

/*
================
applyPetRevival
================
*/
func (rt *Runtime) applyPetRevival(
	character *enterworld.Character,
	ref *enterworld.ItemRef,
	rowIndex int,
	request wire.ItemUseRequest,
	tail []byte,
	result *OpResult,
) bool {
	if len(tail) != 1 {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return false
	}
	var pet *enterworld.CharacterCOS
	for i := range character.MissionInventory {
		row := &character.MissionInventory[i]
		if row.Slot == int64(tail[0]) {
			if pet != nil {
				return false
			}
			pet = row.Summon
		}
	}
	if pet == nil && character.ActiveCOS != nil && character.ActiveCOS.InventorySlot == tail[0] {
		pet = character.ActiveCOS
	}
	if pet == nil {
		*result = itemUseFailure(wire.ErrCodeCosRefused)
		return false
	}
	// Bit 0 already set: the COS is not a revival candidate (native answer 3).
	if pet.StateFlags&1 != 0 {
		*result = itemUseFailure(wire.ErrCodeCosRefused)
		return false
	}
	cosRef, exists := rt.cosReference(pet)
	if !exists || cosRef == nil || cosRef.RefObjID != pet.RefObjID {
		return false
	}
	pet.StateFlags |= 1
	// HP is CCOSData+0x2C from ref+0x19C; MP is +0x30 from ref+0x1A0.
	pet.CurrentHP = cosRef.MaxHP
	pet.CurrentMP = cosRef.MaxMP
	// Hunger percent is (word * 100) / 10000. Below 30, set 3000 (0xBB8).
	if uint32(pet.Satiety)*100/10000 < 30 {
		pet.Satiety = 3000
	}
	remaining := rt.consumeItemUseRow(character, rowIndex)
	gid := pet.GID
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
	}}
	// Dormant pets may retain a GID now used by another summoner of the same
	// family. Only the currently live actor owns a world-vitals publication.
	if pet.Summoned && character.CompanionByGID(gid) == pet {
		vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(gid, simulation.Vitals{CurrentHP: pet.CurrentHP, CurrentMP: pet.CurrentMP})}
		life := wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelLife, Value: wire.LifeStateAlive}.Encode()}
		result.Frames = append(result.Frames, vitals, life)
		result.Broadcast = []wire.Frame{vitals, life}
	}
	result.Frames = append(result.Frames, companionItemStateFrames(character, pet)...)
	result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
	return true
}

/*
================
applyPetPotion
================
*/
func (rt *Runtime) applyPetPotion(
	divisionID string,
	character *enterworld.Character,
	ref *enterworld.ItemRef,
	rowIndex int,
	request wire.ItemUseRequest,
	tail []byte,
	nowMs int64,
	result *OpResult,
) bool {
	gid, ok := readCosGID(tail)
	if !ok || !rt.livePet(character, gid) {
		*result = itemUseFailure(wire.ErrCodeCosTarget)
		return false
	}
	lane, laneOK := petPotionLane(ref.TypeIDs[3])
	if !laneOK {
		return false
	}
	if character.PetPotionCooldowns[lane] > nowMs {
		*result = itemUseFailure(wire.ErrCodeItemReuseDelay)
		return false
	}
	stats, _, statsErr := rt.playerCombatStats(divisionID, character)
	if statsErr != nil {
		return false
	}
	// Natively the recovery amount reads the item owner's STR/INT/level (PC
	// vfuncs), then the COS recovery reductions and effective maxima apply.
	maxHP, maxMP, _, _ := keeperVitalsFrom(character, stats, nil)
	amount, valid := computePotionAmount(ref, stats.Level, stats.Strength, stats.Intellect, maxHP, maxMP)
	if !valid {
		return false
	}
	if amount.hp <= 0 && amount.mp <= 0 {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return false
	}
	pet := character.CompanionByGID(gid)
	owner := rt.newCosAbnormalOwnerForPet(divisionID, character, character.CompanionByGID(gid), nowMs)
	if owner.ref == nil {
		return false
	}
	beforeHP := pet.CurrentHP
	nextHP, nextMP := int64(pet.CurrentHP), int64(pet.CurrentMP)
	zombie := owner.block.Mask&abnormal.Zombie.Bit() != 0
	if amount.hp > 0 {
		if zombie {
			nextHP = max(nextHP-amount.hp, 0)
		} else {
			nextHP = combat.RecoverVital(nextHP, int64(owner.MaxHP()), amount.hp, owner.Param(combat.HPRecoveryReductionParameter))
		}
	}
	if amount.mp > 0 {
		nextMP = combat.RecoverVital(nextMP, int64(owner.MaxMP()), amount.mp, owner.Param(combat.MPRecoveryReductionParameter))
	}
	pet.CurrentHP = uint32(nextHP)
	pet.CurrentMP = uint32(nextMP)
	character.PetPotionCooldowns[lane] = nowMs + 1100
	remaining := rt.consumeItemUseRow(character, rowIndex)
	frames := []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
		{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(gid, simulation.Vitals{CurrentHP: pet.CurrentHP, CurrentMP: pet.CurrentMP})},
	}
	var public []wire.Frame
	if nextHP > 0 && pet.CurrentHP != beforeHP {
		public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.HPRefreshPayload(gid, 0, pet.CurrentHP)})
	}
	if nextHP == 0 {
		owner.changed = owner.block.ClearAll(owner)
		owner.fatal = true
		owner.commit()
		public = rt.cosAbnormalPublication(gid, owner)
		frames = append(frames, public...)
		frames = append(frames, owner.private...)
	}
	frames = append(frames, rt.updateQuestInventory(character)...)
	*result = OpResult{Frames: frames, Broadcast: public}
	return true
}
