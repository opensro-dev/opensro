/*
===========================================================================

itemuse.go - inventory admission and atomic consumable dispatch

Validate the authoritative row and its requirements before choosing a family.
Each family commits its effect and inventory consumption through one update.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"math"
	"slices"

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

// zombiePotionHPFloor is the v1.150 rule as reported by the owner from the
// original client: reversed potion damage stops at one HP. The server binary
// is not available to check it.
const zombiePotionHPFloor int64 = 1

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

// statRecallTopItem and statRecallTopLevel: under the statrecall preset the
// top recall scroll serves 81-110 (owner, 2026-10-26, #1609), since v1.150
// ships no tier past 90.
const (
	statRecallTopItem  = "ITEM_MALL_STATS_POINT_RECALL_81LV"
	statRecallTopLevel = 110
)

/*
================
statRecallWindow

Port-only, not native (#1609). A recall scroll's itemdata carries its tier
as two level requirements, the first and last level (9258: 81 and 90);
the native any-of walk (itemUseRequirements) admits from the first and
never stops at the last, so while the mall sells the scrolls this holds
the character to the last too: a level-85 cannot spend the 30-silk 1-40
scroll. The top scroll's last level is statRecallTopLevel.
================
*/
func statRecallWindow(character *enterworld.Character, ref *enterworld.ItemRef) bool {
	levels, last := 0, int64(0)
	for slot := 0; slot < 4; slot++ {
		if ref.ReqQuadTypes[slot] == 1 {
			levels++
			last = max(last, ref.ReqQuadValues[slot])
		}
	}
	if ref.Codename == statRecallTopItem {
		levels, last = 2, statRecallTopLevel
	}
	// One level requirement names no last level.
	if levels < 2 {
		return true
	}
	requirements := characterEquipRequirements{character: character}
	return requirements.characterLevel() <= last
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
Other client 6961B0 families remain refused until their effect, prerequisites,
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
	// A name-change scroll the closure admitted: the rename queues after
	// this door closes, then the scroll is spent (namechange.go).
	var queuedName string
	var queuedNowMs int64
	// The object the 0x3449 sparkle plays on: the user, unless the item
	// names a pet (petItemVisualTarget).
	var visualGID uint32
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
		// itself, as natively.
		if family != itemUseResurrection && !enterworld.CharacterAlive(character) {
			result = itemUseFailure(wire.ErrCodeItemUseDead)
			return false
		}
		if family == itemUseUnsupported {
			result.DiagnosticRefusal = "item-use: unsupported reference family " + ref.Codename
			return false
		}
		if family == itemUseNameChange && !rt.NameChangeScroll {
			// Port-only (#1689): with the flag off the scroll is the
			// native game - an item no family serves.
			result.DiagnosticRefusal = "item-use: name change scroll is off (" + EnvNameChangeScroll + ")"
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
			// A quest tool may grant its result before it is spent (the
			// warehouse amulets): find the used row again by its slot.
			rowIndex = slices.IndexFunc(character.MissionInventory, func(row enterworld.InventoryRow) bool {
				return row.Slot == int64(request.Slot) && row.Codename == ref.Codename
			})
			if rowIndex < 0 {
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
		if family == itemUseReverseReturn {
			// The tail is the u8 point the client's choice box picked (6971B0),
			// or the port-only map choice 7 with its u32 point id (reversemap.go).
			target := reverseReturnTarget{}
			switch {
			case len(tail) == 1:
				target.choice = tail[0]
			case len(tail) == reverseMapTailBytes && tail[0] == reverseReturnMapChoice:
				target.choice, target.point = tail[0], binary.LittleEndian.Uint32(tail[1:])
			default:
				return false
			}
			return rt.beginReverseReturnScroll(divisionID, character, ref, rowIndex, target, nowMs, &result)
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
		if family == itemUseStallDecoration {
			// Natively item-use case 3: the item's reference id becomes the
			// character's permanent stall decoration; an open stall keeps the
			// booth it opened with until it reopens.
			if len(tail) != 0 {
				return false
			}
			character.StallDecoration = ref.RefObjID
			remaining := rt.consumeItemUseRow(character, rowIndex)
			result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}}
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
		if family == itemUseNameChange {
			// The queue door takes the store's own lock, so it cannot
			// run inside this character door: admit here, queue and
			// spend after the door closes (finishNameChangeUse).
			queuedName = rt.admitNameChangeUse(divisionID, character, tail, nowMs, &result)
			queuedNowMs = nowMs
			return false
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
		if family == itemUseGlobalChat {
			return rt.useGlobalChatting(globalChatUse{division: divisionID, character: character, row: rowIndex,
				request: request}, tail, &result, &after)
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
			if rt.StatRecallWindows && !statRecallWindow(character, ref) {
				result = itemUseFailure(wire.ErrCodeItemUseLevelRequired)
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
			// applies to: "Can only be used on growth pets" (the item's
			// TT_DESC). A summoned growth pet lives on its summoner row, not
			// in ActiveCOS (the transport), so look among the companions.
			seconds := itemParam1Seconds(ref)
			if seconds <= 0 || nowMs > math.MaxInt64-seconds*1000 || rt.summonedAttackPet(character) == nil {
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
				// A fresh use has its whole window left; client 6E6E00 seeds the
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
				result = itemUseFailure(cosSummonPosture) // posture admission precedes actor creation.
				return false
			}
			// The refusal sequence mirrors the persistent lane's native
			// order: posture, teleport, the seated motion busy, battle,
			// then the casting and motion-change locks.
			if teleportBlocks(character.NativeTeleportMode) {
				result = itemUseFailure(0x69) // v1.150 consumes this byte silently.
				return false
			}
			if rt.playerMotionState(divisionID, character, nowMs) == simulation.MotionSitting {
				result = itemUseFailure(cosSummonBusy)
				return false
			}
			if inBattleState(character, nowMs) {
				result = itemUseFailure(cosSummonInBattle) // riding and transport summons refuse battle.
				return false
			}
			if rt.PlayerAttackLocked(divisionID, character.Name) || rt.objectActionCommitted(divisionID, character.Name) {
				result = itemUseFailure(cosSummonBusy) // shared motion-change lock.
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
			// A murderer may not summon a riding horse, 0x1876; the
			// transport summon skips the check.
			if cosRef.TidWord>>11 == cosBandRiding && murderer(character) {
				result = itemUseFailure(errCodeMurdererTransport)
				return false
			}
			if cosRef.TidWord>>11 == cosBandTransport && !transportJob(character) {
				result = itemUseFailure(errCodeCantActivateCart)
				return false
			}
			// Port-only, not native (denreturn.go): no transport during the
			// Bandit Den countdown; the same 0x69 as the native mode-1 refusal.
			if rt.denTransportRefused(divisionID, character, cosRef.TidWord>>11 == cosBandTransport) {
				result = itemUseFailure(0x69)
				return false
			}
			// #721, the summon side of the stolen-goods rule: a thief (job
			// state 2) cannot summon a transport (TID 1/2/3/2) in a protected
			// region (IsBattleField clear), the Thieves' Den included: v1.188
			// item use answers 0x1895, the
			// client's error 149, whose English text names the Thieves' Den.
			if cosRef.TidWord>>11 == cosBandTransport && enterworld.DressedJob(character) == domain.JobThief &&
				townRegion(rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, rt.Now().UnixMilli()).RegionID) {
				result = itemUseFailure(errCodeThiefCartInTown)
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
			// The creation arm keeps the reference capacity
			// (+0x224) for everything but pets, so a transport carries a
			// cargo bag of its authored size; a riding horse authors none.
			if cosRef.InventoryCapacity > 0 {
				pet.Container = &domain.COSContainer{Capacity: cosRef.InventoryCapacity}
			}
			// Validate the complete private record before inventory debit. Native
			// summoning automatically binds both riding and transport vehicles.
			record, recordErr := enterworld.BuildCOSRecord(pet, cosRef, rt.deps.ItemReferences())
			if recordErr != nil {
				return false
			}
			// The admitted summon retires the transform before binding.
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
				OwnerName: enterworld.PartyMemberName(character),
				OwnerGid:  enterworld.ObjectIDForCharacter(character),
			})
			rt.rememberTransportCOS(divisionID, character, live)
			// Only the owner hears the summon here. Viewers meet the vehicle
			// through the peer COS lane (runPeerCOSVisibility), which spawns
			// it, binds the ride and keeps its own record of what each viewer
			// holds; a second spawn from this door reached every viewer
			// twice, and the client drops the session on a duplicate gid.
			frames := []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}
			frames = append(frames, rt.cosContainerReferenceFrames(pet)...)
			result = OpResult{
				Frames: append(frames,
					wire.Frame{Opcode: wire.OpCosRecordCreate, Payload: record},
					wire.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: spawn},
					wire.Frame{Opcode: wire.OpCosRideState, Payload: wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(character), true, gid)},
				),
			}
			result.Frames = append(result.Frames, rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(divisionID, character, nowMs))...)
			result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
			return true
		}

		if family == itemUsePetPotion || family == itemUsePetCure || family == itemUsePetRevive || family == itemUsePetFeed {
			visualGID = petItemVisualTarget(family, tail)
			return rt.applyCosItemUse(divisionID, character, ref, family, rowIndex, request, tail, nowMs, &result)
		}
		if len(tail) != 0 {
			return false
		}
		if family == itemUseCure {
			// TID3 2: the reuse lane 0x100 (universal pill) or 0x40 (other
			// cures) refuses with the reuse delay; the cure result is ignored,
			// the use always succeeds and the reuse lock holds the lane for 20.1 s
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
		// TID3 1: the lane check precedes the amount; the recovery amount
		// sizes the potion; both amounts <= 0 is error 2; otherwise the item
		// is consumed even into a full gauge (the recovery is clamped).
		tid4, _ := potionType(ref.TypeIDs)
		if character.ItemUseCooldowns[tid4-1] > nowMs {
			result = itemUseFailure(wire.ErrCodeItemReuseDelay)
			return false
		}
		stats, _, statsErr := rt.playerCombatStats(divisionID, character)
		if statsErr != nil {
			return false
		}
		maxHP, maxMP, currentHP, currentMP := keeperVitalsFrom(character, stats, nil)
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
			// reason 0x20 instead of healing; MP still recovers. The reversed
			// damage never kills: HP stops at the native floor.
			if zombie {
				nextHP = max(currentHP-amount.hp, zombiePotionHPFloor)
			} else {
				// Natively potion steps go through the same recovery
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
		// A changed HP is broadcast; MP rides the actor's vitals frame.
		var public []wire.Frame
		if nextHP != currentHP {
			public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate,
				Payload: simulation.HPRefreshPayload(enterworld.ObjectIDForCharacter(character), 0, uint32(nextHP))})
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
		rt.publishItemUseVisual(character, used, visualGID, &result)
	}
	if queuedName != "" {
		rt.finishNameChangeUse(divisionID, character, request, used, queuedName, queuedNowMs, &result)
	}
	return result
}

/*
================
publishItemUseVisual

The native item use (v1.188) ends every successful use the same
way: the 0xB04C success, then 0x305C {gid, item reference} to the nearby
sessions, v1.150's 0x3449 (the client's external item effect, 74F540).
The gid is the user's, unless the item's handler named another object
(petItemVisualTarget); target 0 is the user:
the potion sparkle, the scroll glow, the firework. The native client holds
every item record; the browser holds only the references it was sent, so
both audiences, the user and the observers, get the item's reference right
before the visual names it (peer_references.go). The user's copy matters
when the used row was the last of its kind the browser knew of: a repeat
of a known reference is harmless.
================
*/
func (rt *Runtime) publishItemUseVisual(character *enterworld.Character, ref *enterworld.ItemRef, target uint32, result *OpResult) {
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
	reference := rt.commerceReferences([]inventory.Item{{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags()}}, nil)
	if target == 0 {
		target = enterworld.ObjectIDForCharacter(character)
	}
	visual := wire.Frame{Opcode: wire.OpItemUseVisual,
		Payload: wire.NewWriter(8).U32(target).U32(ref.RefObjID).Payload()}
	result.Frames = append(result.Frames[:at+1], append([]wire.Frame{reference, visual}, result.Frames[at+1:]...)...)
	result.Broadcast = append(result.Broadcast, reference, visual)
}
