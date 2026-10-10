/*
===========================================================================

cossummoning.go - persistent item-owned companion activation

The summoner item retains the complete companion while its world actor is
absent. Creation, resummoning and cancellation use one character transaction;
the summoner is never consumed and never becomes a second copy of the pet.

===========================================================================
*/
package action

import (
	"math"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	minimumPersistentSummonLevel        = 5
	initialAttackCompanionMode   uint32 = 1
	initialPickupCompanionMode   uint32 = 7
	initialPickupCompanionSlots  uint8  = 28
	initialCompanionSatiety      uint16 = 10000
	cosSummonMinimumLevel        uint8  = 0x6c
	cosSummonUnavailable         uint8  = 0xa4
	cosSummonPetLevel            uint8  = 0xa5
	cosSummonDuplicateFamily     uint8  = 0xa9
	cosSummonCancelDistance      uint8  = 0x97
	// UIIT_MSG_COSPETERR_CANT_SUMMON_FREEBATTLE (item-use code 0xB9).
	cosSummonFreeBattle uint8  = 0xb9
	cosItemStateOpcode  uint16 = 0x3645
	cosItemStateMask    uint8  = 0x40
	cosItemLeaseMask    uint8  = 0x80
	// Spawn sub-state 1: the pet was just called out (854CD0 plays
	// SYSTEM_PET_APPEAR and the summon sound for it).
	cosSpawnFresh uint8 = 1
	// Observers first meet a summoned pet through the peer COS lane on its
	// next tick. Inference: the retail summon broadcast reached everyone in
	// range at once, so a first sight this soon after the summon is that
	// broadcast and carries sub-state 1; later scope entries carry 0.
	petAppearWindowMs = 1000
)

/*
================
persistentSummonUse
================
*/
type persistentSummonUse struct {
	division  string
	character *enterworld.Character
	ref       *enterworld.ItemRef
	row       int
	request   wire.ItemUseRequest
	nowMs     int64
}

/*
================
usePersistentSummoner

493100 validates the retained record and lease; 4E8F20 creates the first
record and 4E8FC0 toggles the existing actor. Family admission is 4FCEF0.
================
*/
func (rt *Runtime) usePersistentSummoner(use persistentSummonUse, result *OpResult) bool {
	c, row := use.character, &use.character.MissionInventory[use.row]
	if row.StackCount != 1 || c.NativeTeleportMode != 0 || rt.PlayerAttackLocked(use.division, c.Name) || rt.objectActionCommitted(use.division, c.Name) {
		return false
	}
	// Native for pets too: CGItemCOSSummoner_Use (493100) tests the battle
	// byte before any family branch and refuses 0x1878 (493183..49318C), and
	// CGObjPC_TogglePersistentSummoner (4E8FC0) opens with the same test
	// (4E8FC9). UIIT_MSG_COS_CAN_NOT_CREATE_BATTLE's text names vehicles,
	// but the server sends it for attack and pickup pets as well.
	if inBattleState(c, use.nowMs) {
		*result = itemUseFailure(cosSummonInBattle)
		return false
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return false
	}
	code := use.ref.AssociatedCharacterCodename
	if row.Summon != nil {
		code = row.Summon.Codename
	}
	ref, ok := refs.CharacterRefByCodename(code)
	if !ok || ref == nil || ref.TidWord&0x7fe != 0x1c6 || ref.TidWord>>11 != uint16(use.ref.TypeIDs[3]+2) {
		return false
	}
	if (&characterEquipRequirements{character: c}).characterLevel() < minimumPersistentSummonLevel || (&characterEquipRequirements{character: c}).characterLevel() < int64(ref.Level) {
		*result = itemUseFailure(cosSummonMinimumLevel)
		return false
	}
	pet := domain.CloneCOS(row.Summon)
	if pet != nil {
		if pet.RefObjID != ref.RefObjID || pet.StateFlags&1 == 0 || pet.CurrentHP == 0 {
			*result = itemUseFailure(cosSummonUnavailable)
			return false
		}
		if pet.Level > 0 && (&characterEquipRequirements{character: c}).characterLevel() < int64(pet.Level) {
			*result = itemUseFailure(cosSummonPetLevel)
			return false
		}
		if ref.TidWord>>11 == 4 && (pet.RentalExpiresAtUnix <= use.nowMs/1000) {
			*result = itemUseFailure(cosSummonUnavailable)
			return false
		}
		if pet.Summoned {
			return rt.togglePersistentCompanion(use, row.Summon, result)
		}
	}
	// INFERENCE: v1.188's pet summoner (493100) has no free-battle check, but
	// the v1.150 client answers item-use code 0xB9 with "Under free battle
	// situation, growth pets cannot be summoned", so the v1.150 server refused
	// a growth (attack) pet to an owner wearing a free-battle cape. Dismissing
	// one already out stays allowed, as only the summon is named.
	if ref.TidWord>>11 == attackPetBand && rt.inFreeBattle(c) {
		*result = itemUseFailure(cosSummonFreeBattle)
		return false
	}
	for _, other := range c.Companions() {
		otherRef, valid := rt.cosReference(other)
		if other.Summoned && valid && otherRef.TidWord == ref.TidWord {
			*result = itemUseFailure(cosSummonDuplicateFamily)
			return false
		}
	}
	gid, ok := enterworld.PersistentCOSObjectID(c, ref.TidWord>>11)
	if !ok {
		return false
	}
	if pet == nil {
		pet = &domain.CharacterCOS{RefObjID: ref.RefObjID, Codename: ref.Codename, Level: ref.Level, CurrentHP: ref.MaxHP, CurrentMP: ref.MaxMP, Satiety: initialCompanionSatiety, StateFlags: 1, CommandMode: initialAttackCompanionMode}
		// 440368/440373 initialize pickup/attack modes to 7/1. The database
		// creation arm overrides reference capacity at 4404B8/4404D1: pickup
		// pets start with 28 slots, attack pets without a bag. Retained records
		// bypass this arm so upgrades and player-selected modes survive.
		if ref.TidWord>>11 == 4 {
			pet.CommandMode = initialPickupCompanionMode
			pet.Container = &domain.COSContainer{Capacity: initialPickupCompanionSlots}
			minutes, present := use.ref.NativeFields.Lookup("itemParam1_29c")
			if !present || math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes <= 0 || minutes > math.MaxInt32/60 || math.Trunc(minutes) != minutes {
				return false
			}
			pet.RentalExpiresAtUnix = use.nowMs/1000 + int64(minutes)*60
		}
	}
	if pet.SummonGeneration == math.MaxUint64 {
		return false
	}
	pet.SummonGeneration++
	pet.GID, pet.InventorySlot = gid, use.request.Slot
	pet.Summoned, pet.Mounted = true, false
	pet.StateFlags |= cosStateSummoned
	pet.NativeBodyStatus = domain.InitialCOSBodyStatus(c.NativeBodyStatus)
	pet.RefreshRentalTimes(use.nowMs / 1000)
	record, err := enterworld.BuildCOSRecord(pet, ref, rt.deps.ItemReferences())
	if err != nil {
		return false
	}
	row.Summon = pet
	rt.petMu.Lock()
	owner := rt.petSessions[petOwnerKey{division: use.division, name: strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if owner != nil {
		rt.bindCompanionSession(use.division, owner, pet).summonedAtMs = use.nowMs
	}
	pose := rt.companionLiveSpawn(use.division, c, pet, use.nowMs)
	walk, run := rt.cosMovementSpeeds(ref, pet, nil)
	spawn := wire.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: wire.EncodeCosSpawnBand2(wire.CosSpawnBand2{
		Band: uint8(ref.TidWord >> 11), RefObjID: ref.RefObjID, Gid: gid, BodyStatus: pet.NativeBodyStatus,
		Position: wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle},
		// The same projected speeds the entry bootstrap publishes for this pet
		// (EntryCompanionMovementSpeeds), so a fresh summon and a relogin agree.
		Walk: walk, Run: run, Scale: ref.Scale, Name: pet.Name, OwnerName: c.Name, OwnerGid: enterworld.ObjectIDForCharacter(c),
		// CICCos_DeserializeSpawnSubState (854CD0): 1 is a fresh summon.
		State: cosSpawnFresh,
	})}
	// Viewers meet the pet through the peer COS lane alone: a spawn sent from
	// here as well reached each of them twice (see the vehicle summon).
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, 1, use.request.TypeWord)}, {Opcode: wire.OpCosRecordCreate, Payload: record}, spawn}}
	result.Frames = append(result.Frames, companionItemStateFrames(c, pet)...)
	return true
}

/*
================
togglePersistentCompanion

The item-use handler already holds both authority locks. Entering the public
cancellation handler here would acquire the division lock a second time.
================
*/
func (rt *Runtime) togglePersistentCompanion(use persistentSummonUse, pet *domain.CharacterCOS, result *OpResult) bool {
	owner := rt.liveSpawn(simulation.WorldKey(use.division, use.character.Name), use.character, use.nowMs)
	pose := rt.companionLiveSpawn(use.division, use.character, pet, use.nowMs)
	if pet.Mounted || !worldgeom.SamePlane(owner.RegionID, pose.RegionID) || !(simulation.WorldDistance2D(owner, pose) < cosCancelRange) {
		*result = itemUseFailure(cosSummonCancelDistance)
		return false
	}
	pet.RefreshRentalTimes(use.nowMs / 1000)
	pet.Summoned = false
	pet.StateFlags &^= cosStateSummoned
	frames := rt.retireCosRuntime(use.division, use.character, pet.GID)
	despawn := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()}
	frames = append(frames, wire.Frame{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, 1, use.request.TypeWord)}, despawn)
	*result = OpResult{Frames: append(frames, companionItemStateFrames(use.character, pet)...), Broadcast: []wire.Frame{despawn}}
	return true
}

/*
================
companionItemStateFrames

7654B0 mask 0x40 forwards the state byte to 59C290/54FC80 and the
summoner's rent-state field. Only the owning inventory receives this packet.
================
*/
func companionItemStateFrames(c *enterworld.Character, pet *domain.CharacterCOS) []wire.Frame {
	for i := range c.MissionInventory {
		row := &c.MissionInventory[i]
		if row.Summon == pet && row.Slot >= 13 && row.Slot <= 255 {
			mask := cosItemStateMask
			if pet.RentalExpiresAtUnix != 0 {
				mask |= cosItemLeaseMask
			}
			writer := wire.NewWriter(7).U8(uint8(row.Slot)).U8(mask).U8(domain.COSItemState(pet))
			if mask&cosItemLeaseMask != 0 {
				writer.U32(uint32(pet.RentalRemainingSeconds))
			}
			return []wire.Frame{{Opcode: cosItemStateOpcode, Payload: writer.Payload()}}
		}
	}
	return nil
}
