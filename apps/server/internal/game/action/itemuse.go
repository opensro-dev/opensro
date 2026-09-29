/*
===========================================================================

itemuse.go - inventory admission and atomic consumable dispatch

Validate the authoritative row and its requirements before choosing a family.
Each family commits its effect and inventory consumption through one update.

===========================================================================
*/

package action

import (
	"math"
	"opensro.online/server/internal/game/abnormal"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cosSummonerType
================
*/
func cosSummonerType(typeIDs [4]int64) bool {
	return typeIDs == [4]int64{3, 3, 3, 2}
}

/*
================
consumeItemUseRow

Replace the inventory slice so published snapshots retain their old values.
The caller has already validated the slot and positive stack count.
================
*/
func consumeItemUseRow(character *enterworld.Character, rowIndex int) uint16 {
	row := character.MissionInventory[rowIndex]
	remaining := uint16(row.StackCount - 1)
	if remaining == 0 {
		nextRows := make([]enterworld.InventoryRow, 0, len(character.MissionInventory)-1)
		nextRows = append(nextRows, character.MissionInventory[:rowIndex]...)
		nextRows = append(nextRows, character.MissionInventory[rowIndex+1:]...)
		character.MissionInventory = nextRows
	} else {
		nextRows := append([]enterworld.InventoryRow(nil), character.MissionInventory...)
		nextRows[rowIndex].StackCount = int64(remaining)
		character.MissionInventory = nextRows
	}
	return remaining
}

/*
================
itemUseFailure
================
*/
func itemUseFailure(errorCode uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{
		Opcode:  wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseError(errorCode),
	}}}
}

/*
================
potionType
================
*/
func potionType(typeIDs [4]int64) (uint8, bool) {
	if typeIDs[0] != 3 || typeIDs[1] != 3 || typeIDs[2] != 1 {
		return 0, false
	}
	tid4 := typeIDs[3]
	return uint8(tid4), tid4 >= 1 && tid4 <= 3
}

/*
==================
HandleItemUse

HandleItemUse is the server authority for the common v1.150 potion path:
strict 0x75BD decode, selected-row/type echo validation, itemdata-driven
HP/MP recovery, one-unit stack consumption, then the exact 0xB5BD success
row followed by the normal 0x33A6 vitals refresh.

admittedItemUseFamily owns the closed recovery/summoner/pet-skill dispatch.
Other sub_6961b0 families remain refused until their effect, prerequisites,
cooldown and result conversation are implemented together.
==================
*/
func (rt *Runtime) HandleItemUse(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	request, tail, err := wire.ReadItemUseRequest(payload)
	if err != nil || character == nil || !inventory.IsBagSlot(request.Slot) {
		return itemUseFailure(wire.ErrCodeInvalidRequest)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	result := itemUseFailure(wire.ErrCodeInvalidRequest)
	rt.deps.Update(character, "item-use", func() bool {
		if character.DeletePending || rt.deps.ItemReferences() == nil {
			return false
		}
		if !enterworld.CharacterAlive(character) {
			result = itemUseFailure(wire.ErrCodeItemUseDead)
			return false
		}

		rowIndex := -1
		for index := range character.MissionInventory {
			if character.MissionInventory[index].Slot == int64(request.Slot) {
				if rowIndex >= 0 {
					return false // Ambiguous persisted slot identity cannot authorize use.
				}
				rowIndex = index
			}
		}
		if rowIndex < 0 {
			return false
		}
		row := character.MissionInventory[rowIndex]
		if row.StackCount < 1 || row.StackCount > 0xFFFF {
			return false
		}

		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(row.Codename)
		if !ok || ref == nil ||
			ref.RefObjID == 0 || ref.RefObjID != row.RefObjID || ref.Codename != row.Codename ||
			ref.TypeFlags() != row.TypeFlags ||
			request.TypeWord != row.TypeFlags {
			return false
		}
		family := admittedItemUseFamily(ref)
		if family == itemUseUnsupported {
			result.DiagnosticRefusal = "item-use: unsupported reference family " + ref.Codename
			return false
		}
		if code := itemUseRequirements(character, ref); code != 0 {
			result = itemUseFailure(code)
			return false
		}
		nowMs := rt.Now().UnixMilli()
		if family == itemUseQuestTool {
			if len(tail) != 0 || rt.UseQuestItem == nil || character.NativeTeleportMode != 0 {
				return false
			}
			at := rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, nowMs)
			frames, admitted := rt.UseQuestItem(character, ref.Codename, at, nowMs)
			if !admitted {
				result.Frames = append(result.Frames, frames...)
				return false
			}
			remaining := consumeItemUseRow(character, rowIndex)
			result.Frames = []wire.Frame{{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}
			result.Frames = append(result.Frames, frames...)
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}
		if family == itemUseBerserk {
			// Native49B710: no recovery cooldown; a full gauge still consumes the item.
			if character.NativeBodyStatus == 1 {
				result = itemUseFailure(0xce)
				return false
			}
			changed := character.ModifyBerserkPoints(5)
			remaining := consumeItemUseRow(character, rowIndex)
			result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}}
			if changed {
				result.Frames = append(result.Frames, berserkPointsFrame(character, 0))
			}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}
		if family == itemUseReturn {
			return rt.beginReturnScroll(divisionID, character, ref, rowIndex, request, nowMs, &result)
		}

		if family == itemUseSkill {
			if len(tail) != 0 {
				return false
			}
			return rt.useSkillItem(character, skillItemUse{
				division: divisionID, ref: ref, row: rowIndex, request: request, nowMs: nowMs,
			}, &result)
		}

		if family == itemUseMonsterCapsule {
			if len(tail) != 0 {
				return false
			}
			return rt.useMonsterCapsule(divisionID, character, ref, rowIndex, request, nowMs, &result)
		}

		if family == itemUsePetSkill {
			// The window is a pet skill's usage time, so it needs the pet it
			// applies to. Without a live COS there is nothing to skill and no
			// row to raise.
			seconds := itemParam1Seconds(ref)
			if seconds <= 0 || nowMs > math.MaxInt64-seconds*1000 || character.ActiveCOS == nil ||
				!character.ActiveCOS.Summoned || character.ActiveCOS.CurrentHP == 0 {
				return false
			}
			pet := character.ActiveCOS
			characters, hasCharacters := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
			gid, gidOK := enterworld.CosObjectIDForCharacter(character)
			if !hasCharacters || !gidOK || pet.GID != gid {
				return false
			}
			petRef, exists := characters.CharacterRefByCodename(pet.Codename)
			if !exists || petRef == nil || petRef.RefObjID != pet.RefObjID || petRef.Codename != pet.Codename {
				return false
			}
			if !petSkillWindowHasCapacity(character, ref) {
				return false
			}
			remaining := consumeItemUseRow(character, rowIndex)
			character.PetSkillWindows = upsertPetSkillWindow(
				character.PetSkillWindows, ref.RefObjID, ref.Codename, nowMs+seconds*1000)
			rt.petSkillWindows.track(divisionID, character.Name)
			result = OpResult{Frames: []wire.Frame{
				{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
				// A fresh use has its whole window left; sub_6E6E00 seeds the
				// row's elapsed accumulator to limit minus this value.
				{Opcode: wire.OpCosStateRefresh, Payload: wire.EncodeCosSummonTimer3691(ref.RefObjID, uint32(seconds), 0)},
			}}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}

		if family == itemUseSummoner {
			// v1.188 49B9F0 checks teleport mode before creating the companion.
			if character.NativeTeleportMode == 1 {
				result = itemUseFailure(0x69) // 49BB2B; v1.150 consumes this byte silently.
				return false
			}
			characters, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
			if !ok || ref.AssociatedCharacterCodename == "" {
				return false
			}
			if character.ActiveCOS != nil && character.ActiveCOS.Summoned {
				result = itemUseFailure(wire.ErrCodeMultipleCOS)
				return false
			}
			cosRef, found := characters.CharacterRefByCodename(ref.AssociatedCharacterCodename)
			if !found || cosRef == nil || cosRef.RefObjID == 0 || cosRef.Codename != ref.AssociatedCharacterCodename ||
				cosRef.TidWord>>11 != 2 || cosRef.MaxHP == 0 {
				return false
			}
			gid, gidOK := enterworld.CosObjectIDForCharacter(character)
			if !gidOK {
				return false
			}
			live := rt.liveSpawn(
				simulation.WorldKey(divisionID, character.Name),
				character,
				rt.Now().UnixMilli(),
			)
			name := cosRef.Name
			if name == "" {
				name = cosRef.Codename
			}
			// 49BE8F: summoning a COS ends the transform (any word but 4).
			rt.endTransform(divisionID, character, rt.Now().UnixMilli())
			character.ActiveCOS = &domain.CharacterCOS{
				NativeBodyStatus: domain.InitialCOSBodyStatus(character.NativeBodyStatus),
				GID:              gid,
				RefObjID:         cosRef.RefObjID,
				Codename:         cosRef.Codename,
				Name:             name,
				CurrentHP:        cosRef.MaxHP,
				CurrentMP:        cosRef.MaxMP,
				Summoned:         true,
			}
			remaining := consumeItemUseRow(character, rowIndex)
			spawn := wire.EncodeCosSpawnBand2(wire.CosSpawnBand2{
				BodyStatus: character.ActiveCOS.NativeBodyStatus,
				RefObjID:   cosRef.RefObjID,
				Gid:        gid,
				Position: wire.Position{
					RegionID: live.RegionID,
					X:        float32(live.X),
					Y:        float32(live.Y),
					Z:        float32(live.Z),
					Heading:  live.Angle,
				},
				Walk:      cosRef.WalkSpeed,
				Run:       cosRef.RunSpeed,
				Scale:     cosRef.Scale,
				Name:      name,
				OwnerName: character.Name,
				OwnerGid:  enterworld.ObjectIDForCharacter(character),
			})
			rt.rememberTransportCOS(divisionID, character, live)
			result = OpResult{
				Frames: []wire.Frame{
					{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
					{Opcode: wire.OpCosRecordCreate, Payload: wire.EncodeCosRecordCreateBand2(gid, cosRef.RefObjID, cosRef.MaxHP, cosRef.MaxMP, 0, false)},
					{Opcode: wire.OpSingleObjectSpawn, Payload: spawn},
				},
				Broadcast: []wire.Frame{{Opcode: wire.OpSingleObjectSpawn, Payload: spawn}},
			}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}

		if family == itemUsePetPotion || family == itemUsePetCure || family == itemUsePetRevive {
			return rt.applyCosItemUse(divisionID, character, ref, family, rowIndex, request, tail, nowMs, &result)
		}
		if len(tail) != 0 {
			return false
		}
		if family == itemUseCure {
			// 49B710 TID3 2: 4EB410 lane 0x100 (universal pill) or 0x40 (other
			// cures) refuses with the reuse delay; the cure result is ignored,
			// the use always succeeds and 4E0AB0 locks the lane for 20.1 s
			// (pill) or 1.1 s.
			lane, lockMs := 1, int64(1100)
			if ref.TypeIDs[3] == 1 {
				lane, lockMs = 0, 20100
			}
			if character.ItemCureCooldowns[lane] > nowMs {
				result = itemUseFailure(wire.ErrCodeItemReuseDelay)
				return false
			}
			owner := rt.newPlayerAbnormalOwner(divisionID, character, nowMs)
			var levels *[6]int32
			var pill *[3]int32
			limit := -1
			if ref.TypeIDs[3] == 1 {
				stored := [3]int32{int32(ref.CureMask), int32(ref.CureChance), int32(ref.CureGradeSub)}
				pill = &stored
				limit = 1
			} else {
				stored := [6]int32{}
				for i := range ref.CureLevels {
					stored[i] = int32(ref.CureLevels[i])
				}
				levels = &stored
			}
			random := &abnormalRandom{rt: rt, actor: criticalActor{division: divisionID, character: character.Name}}
			owner.changed = owner.block.Cure(owner, levels, nil, pill, limit, random.Rand)
			if random.err != nil {
				return false
			}
			owner.commit()
			character.ItemCureCooldowns[lane] = nowMs + lockMs
			remaining := consumeItemUseRow(character, rowIndex)
			published := rt.playerAbnormalPublication(divisionID, character, owner)
			frames := []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}
			frames = append(frames, published.actor...)
			frames = append(frames, published.public...)
			result = OpResult{Frames: frames, Broadcast: published.public}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}
		if family != itemUseRecovery {
			return false // A future enum arm must not fall through into recovery.
		}
		// The cooldown debit and recovery/consumption share this update door.
		// Refused requests neither extend a deadline nor emit success frames.
		// 49B710 TID3 1: the lane check (4EB410) precedes the amount; 49AA70
		// sizes the potion; both amounts <= 0 is error 2; otherwise the item
		// is consumed even into a full gauge (4EF450 clamps the recovery).
		tid4, _ := potionType(ref.TypeIDs)
		if character.ItemUseCooldowns[tid4-1] > nowMs {
			result = itemUseFailure(wire.ErrCodeItemReuseDelay)
			return false
		}
		stats, _, statsErr := rt.playerCombatStats(divisionID, character)
		if statsErr != nil {
			return false
		}
		maxHP, maxMP, currentHP, currentMP := rt.playerKeeperVitals(divisionID, character)
		amount, validAmount := computePotionAmount(ref, stats.Level, stats.Strength, stats.Intellect, maxHP, maxMP)
		duration, validRecovery := recoveryCooldownDuration(character, amount.absolute)
		if !validAmount || !validRecovery || nowMs > math.MaxInt64-duration {
			return false
		}
		if amount.hp <= 0 && amount.mp <= 0 {
			result = itemUseFailure(wire.ErrCodeInvalidRequest)
			return false
		}
		nextHP, nextMP := currentHP, currentMP
		if amount.hp > 0 {
			// Mask bit 0x20 deals the whole HP amount through vfunc +308 with
			// reason 0x20 instead of healing; MP still recovers.
			if block := rt.playerAbnormal(divisionID, character.Name); block != nil && block.Mask&abnormal.Zombie.Bit() != 0 {
				nextHP = max(currentHP-amount.hp, 0)
			} else {
				nextHP = min(currentHP+amount.hp, max(maxHP, currentHP))
			}
		}
		if amount.mp > 0 {
			nextMP = min(currentMP+amount.mp, max(maxMP, currentMP))
		}

		character.CurrentHP = &nextHP
		character.CurrentMP = &nextMP
		character.ItemUseCooldowns[tid4-1] = nowMs + duration
		remaining := consumeItemUseRow(character, rowIndex)

		frames := []wire.Frame{
			{
				Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(
					request.Slot,
					remaining,
					request.TypeWord,
				),
			},
			{
				Opcode: simulation.OpVitalsUpdate,
				Payload: simulation.VitalsRefreshPayload(
					enterworld.ObjectIDForCharacter(character),
					simulation.Vitals{
						CurrentHP: uint32(nextHP),
						CurrentMP: uint32(nextMP),
					},
				),
			},
		}
		// A lethal zombie potion dies through the shared death path; every
		// observer sees the retired effects, the zero baseline and 0x3122.
		var public []wire.Frame
		if nextHP == 0 {
			effects, progression := rt.settlePlayerDeathInDoor(divisionID, character, nowMs)
			public = append(public, effects...)
			if owner := rt.clearPlayerAbnormalInDoor(divisionID, character, nowMs); owner != nil {
				owner.fatal = true
				public = append(public, rt.playerAbnormalPublication(divisionID, character, owner).public...)
			} else {
				life := beginFatalLifePublication(enterworld.ObjectIDForCharacter(character))
				baseline := life.publishDeathBaseline()
				dead := life.publishDead()
				public = append(public, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload}, wire.Frame{Opcode: dead.opcode, Payload: dead.payload})
			}
			frames = append(frames, public...)
			frames = append(frames, progression...)
		}
		frames = append(frames, rt.updateQuestInventory(character)...)
		result = OpResult{Frames: frames, Broadcast: public}
		return true
	})
	return result
}
