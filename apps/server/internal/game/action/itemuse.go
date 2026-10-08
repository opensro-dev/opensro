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

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/recovery"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	cosSummonBusy     uint8 = 5
	cosSummonInBattle uint8 = 0x78
	cosSummonPosture  uint8 = 0x7a
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
The caller has already validated the slot and positive stack count. An item
in the operator's unlimited set (UnlimitedItems, the beta starter kit) takes
effect without being spent.
================
*/
func (rt *Runtime) consumeItemUseRow(character *enterworld.Character, rowIndex int) uint16 {
	row := character.MissionInventory[rowIndex]
	if rt.UnlimitedItems[row.Codename] {
		return uint16(row.StackCount)
	}
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
	// Check the wire's maximum here; the authority door checks the current
	// character capacity before a persisted row can authorize an effect.
	if err != nil || character == nil || !inventory.IsBagSlot(request.Slot, inventory.MaxBagEnd) {
		return itemUseFailure(wire.ErrCodeInvalidRequest)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	result := itemUseFailure(wire.ErrCodeInvalidRequest)
	// A use that revives the player publishes after its commit (revivalFrames
	// rebinds the resident region, which reads the committed character).
	var after func()
	var used *enterworld.ItemRef
	var mercenaryContext domain.MercenaryContext
	update := func() bool {
		if character.DeletePending || rt.deps.ItemReferences() == nil ||
			!inventory.IsBagSlot(request.Slot, inventory.BagEnd(character)) {
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
		used = ref
		// Only the resurrection scroll is usable dead; it refuses the living
		// itself (49FF20).
		if family != itemUseResurrection && !enterworld.CharacterAlive(character) {
			result = itemUseFailure(wire.ErrCodeItemUseDead)
			return false
		}
		if family == itemUseUnsupported {
			result.DiagnosticRefusal = "item-use: unsupported reference family " + ref.Codename
			return false
		}
		if code := itemUseRequirements(character, ref); code != 0 {
			result = itemUseFailure(code)
			return false
		}
		nowMs := rt.Now().UnixMilli()
		if family == itemUseMercenary {
			if len(tail) != 0 {
				return false
			}
			return rt.useMercenaryScroll(persistentSummonUse{division: divisionID, character: character, ref: ref, row: rowIndex, request: request, nowMs: nowMs}, mercenaryContext, &result)
		}
		if family == itemUseStructureRepair {
			return rt.useStructureRepair(character, skillItemUse{division: divisionID, ref: ref, row: rowIndex, request: request, nowMs: nowMs}, tail, &result)
		}
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
			remaining := rt.consumeItemUseRow(character, rowIndex)
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
			remaining := rt.consumeItemUseRow(character, rowIndex)
			result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}}
			if changed {
				result.Frames = append(result.Frames, berserkPointsFrame(character, 0))
			}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}
		if family == itemUseComposite {
			if len(tail) != 0 {
				return false
			}
			return rt.useCompositeScroll(divisionID, character, ref, rowIndex, request, nowMs, &result)
		}
		if family == itemUseReturn {
			return rt.beginReturnScroll(divisionID, character, ref, rowIndex, request, nowMs, &result)
		}
		if family == itemUseRepairHammer {
			if len(tail) != 0 {
				return false
			}
			repaired := rt.hammerRepair(divisionID, character)
			if len(repaired.actor) == 0 {
				result = itemUseFailure(errCodeNothingToRepair)
				return false
			}
			remaining := rt.consumeItemUseRow(character, rowIndex)
			result = OpResult{Frames: append([]wire.Frame{{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}, repaired.actor...),
				Broadcast: repaired.public}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}
		if family == itemUseFirework {
			if len(tail) != 0 {
				return false
			}
			remaining := rt.consumeItemUseRow(character, rowIndex)
			result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}
		if family == itemUsePremiumTicket || family == itemUseSkillTimeTicket {
			return rt.usePremiumTicket(skillItemUse{
				division: divisionID, ref: ref, row: rowIndex, request: request, nowMs: nowMs,
			}, character, tail, family == itemUseSkillTimeTicket, &result)
		}
		if family == itemUseGenderTool {
			return rt.useGenderTool(skillItemUse{
				division: divisionID, ref: ref, row: rowIndex, request: request, nowMs: nowMs,
			}, character, tail, &result)
		}
		if family == itemUseSkinChange {
			return rt.useSkinChangeScroll(skillItemUse{
				division: divisionID, ref: ref, row: rowIndex, request: request, nowMs: nowMs,
			}, character, tail, &result)
		}
		if family == itemUseWarehouseTicket {
			if len(tail) != 0 || rt.storageAuthority == nil {
				return false
			}
			remaining := rt.consumeItemUseRow(character, rowIndex)
			result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			after = func() { rt.openRemoteStorage(divisionID, character) }
			return true
		}
		if family == itemUseStatRecall {
			// The scroll is spent only when a point came back.
			if len(tail) != 0 || rt.RecallStatPoints == nil {
				return false
			}
			recalled, ok := rt.RecallStatPoints(character)
			if !ok {
				result.DiagnosticRefusal = "item-use: no stat point to recall"
				return false
			}
			remaining := rt.consumeItemUseRow(character, rowIndex)
			result = OpResult{Frames: append([]wire.Frame{{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}, recalled...)}
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}
		if family == itemUseResurrection {
			return rt.useResurrectionScroll(character, skillItemUse{
				division: divisionID, ref: ref, row: rowIndex, request: request, nowMs: nowMs,
			}, tail, &result, &after)
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
			remaining := rt.consumeItemUseRow(character, rowIndex)
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

		if family == itemUsePetExtension {
			return rt.extendCompanionLease(companionLeaseUse{character: character, ref: ref, row: rowIndex, request: request, tail: tail, nowUnix: nowMs / 1000}, &result)
		}
		if family == itemUsePersistentSummoner {
			if len(tail) != 0 {
				return false
			}
			return rt.usePersistentSummoner(persistentSummonUse{division: divisionID, character: character, ref: ref, row: rowIndex, request: request, nowMs: nowMs}, &result)
		}
		if family == itemUseSummoner {
			if len(tail) != 0 {
				return false
			}
			if character.NativeBodyStatus == 6 || character.NativeBodyStatus == 7 {
				result = itemUseFailure(cosSummonPosture) // 49C0B6: posture admission precedes actor creation.
				return false
			}
			if rt.PlayerAttackLocked(divisionID, character.Name) || rt.objectActionCommitted(divisionID, character.Name) {
				result = itemUseFailure(cosSummonBusy) // 49BE65 -> 49BCDB: shared motion-change lock.
				return false
			}
			if inBattleState(character, nowMs) {
				result = itemUseFailure(cosSummonInBattle) // 49B9F0: riding and transport summons refuse battle.
				return false
			}
			// v1.188 49B9F0 checks teleport mode before creating the companion.
			if teleportBlocks(character.NativeTeleportMode) {
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
			cosRef, found := enterworld.SummonCharacterReference(characters, ref, (&characterEquipRequirements{character: character}).characterLevel())
			if !found ||
				(cosRef.TidWord>>11 != 1 && cosRef.TidWord>>11 != 2) || !cosRef.CanRide || cosRef.MaxHP == 0 {
				return false
			}
			// 49BF24: a murderer may not summon a riding horse, 0x1876; the
			// transport summon skips the check.
			if cosRef.TidWord>>11 == cosBandRiding && murderer(character) {
				result = itemUseFailure(errCodeMurdererTransport)
				return false
			}
			if cosRef.TidWord>>11 == cosBandTransport && !transportJob(character) {
				result = itemUseFailure(errCodeCantActivateCart)
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
			pet := &domain.CharacterCOS{
				NativeBodyStatus: domain.InitialCOSBodyStatus(character.NativeBodyStatus),
				GID:              gid,
				RefObjID:         cosRef.RefObjID,
				Codename:         cosRef.Codename,
				Name:             name,
				CurrentHP:        cosRef.MaxHP,
				CurrentMP:        cosRef.MaxMP,
				Summoned:         true,
				Mounted:          true,
				StateFlags:       3,
			}
			// Validate the complete private record before inventory debit. Native
			// 4FB2C0 automatically binds both riding and transport vehicles.
			record, recordErr := enterworld.BuildCOSRecord(pet, cosRef, rt.deps.ItemReferences())
			if recordErr != nil {
				return false
			}
			// 49BE8F: the admitted summon retires the transform before binding.
			rt.endTransform(divisionID, character, nowMs)
			character.ActiveCOS = pet
			remaining := rt.consumeItemUseRow(character, rowIndex)
			spawn := wire.EncodeCosSpawnBand2(wire.CosSpawnBand2{
				BodyStatus: character.ActiveCOS.NativeBodyStatus,
				Band:       uint8(cosRef.TidWord >> 11),
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
			// Only the owner hears the summon here. Viewers meet the vehicle
			// through the peer COS lane (runPeerCOSVisibility), which spawns
			// it, binds the ride and keeps its own record of what each viewer
			// holds; a second spawn from this door reached every viewer
			// twice, and the client drops the session on a duplicate gid.
			result = OpResult{
				Frames: []wire.Frame{
					{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
					{Opcode: wire.OpCosRecordCreate, Payload: record},
					{Opcode: wire.OpSingleObjectSpawn, Payload: spawn},
					{Opcode: wire.OpCosRideState, Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(character), true, gid)},
				},
			}
			result.Frames = append(result.Frames, rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(divisionID, character, nowMs))...)
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}

		if family == itemUsePetPotion || family == itemUsePetCure || family == itemUsePetRevive || family == itemUsePetFeed {
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
			remaining := rt.consumeItemUseRow(character, rowIndex)
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
		block := rt.playerAbnormal(divisionID, character.Name)
		zombie := block != nil && block.Mask&abnormal.Zombie.Bit() != 0
		credit := recovery.Amount{HP: amount.hp, MP: amount.mp}
		if !zombie {
			credit = rt.admitPotionRecovery(divisionID, character, recovery.Admission{
				Current: recovery.Amount{HP: currentHP, MP: currentMP},
				Maximum: recovery.Amount{HP: maxHP, MP: maxMP}, Credit: credit, Absolute: amount.absolute,
			}, nowMs)
		}
		if credit.HP > 0 {
			// Mask bit 0x20 deals the whole HP amount through vfunc +308 with
			// reason 0x20 instead of healing; MP still recovers.
			if zombie {
				nextHP = max(currentHP-amount.hp, 0)
			} else {
				// 49A5B0 routes potion steps through the same 4A86A0 recovery
				// reduction as skills. Panic applies after potion sizing.
				reduction, _ := stats.Param(combat.HPRecoveryReductionParameter)
				nextHP = combat.RecoverVital(currentHP, maxHP, credit.HP, reduction)
			}
		}
		if credit.MP > 0 {
			reduction, _ := stats.Param(combat.MPRecoveryReductionParameter)
			nextMP = combat.RecoverVital(currentMP, maxMP, credit.MP, reduction)
		}

		character.CurrentHP = &nextHP
		character.CurrentMP = &nextMP
		character.ItemUseCooldowns[tid4-1] = nowMs + duration
		remaining := rt.consumeItemUseRow(character, rowIndex)

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
		if nextHP > 0 && nextHP != currentHP {
			public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate,
				Payload: simulation.HPRefreshPayload(enterworld.ObjectIDForCharacter(character), 0, uint32(nextHP))})
		}
		if nextHP == 0 {
			effects, progression := rt.settlePlayerDeathInDoor(divisionID, character, deathKiller{}, nowMs)
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
	}
	committed := false
	mercenaryRef := enterworld.ItemRef{TypeIDs: [4]int64{3, 3, 12, 1}}
	if request.TypeWord == mercenaryRef.TypeFlags() {
		if store, ok := rt.deps.GuildAuthority().(domain.MercenaryStore); ok {
			committed = store.UpdateMercenaryOwner(divisionID, character, func(context domain.MercenaryContext) bool {
				mercenaryContext = context
				return update()
			})
		}
	} else {
		committed = rt.deps.Update(character, "item-use", update)
	}
	if committed && after != nil {
		after()
	}
	if committed && used != nil {
		rt.publishItemUseVisual(character, used, &result)
	}
	return result
}

/*
================
publishItemUseVisual

CGObjPC_HandleUseItem (v1.188 510980) ends every successful use the same
way: the 0xB04C success, then 0x305C {gid, item reference} to the nearby
sessions, v1.150's 0x3449 (the client's external item effect, 74F540):
the potion sparkle, the scroll glow, the firework. Observers resolve the
reference before the visual names it.
================
*/
func (rt *Runtime) publishItemUseVisual(character *enterworld.Character, ref *enterworld.ItemRef, result *OpResult) {
	at := -1
	for index, frame := range result.Frames {
		if frame.Opcode == wire.OpItemUseResponse && len(frame.Payload) > 0 && frame.Payload[0] == wire.ResultSuccess {
			at = index
			break
		}
	}
	if at < 0 {
		return
	}
	visual := wire.Frame{Opcode: wire.OpItemUseVisual,
		Payload: wire.NewWriter(8).U32(enterworld.ObjectIDForCharacter(character)).U32(ref.RefObjID).Payload()}
	result.Frames = append(result.Frames[:at+1], append([]wire.Frame{visual}, result.Frames[at+1:]...)...)
	result.Broadcast = append(result.Broadcast,
		rt.commerceReferences([]inventory.Item{{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags()}}, nil),
		visual)
}
