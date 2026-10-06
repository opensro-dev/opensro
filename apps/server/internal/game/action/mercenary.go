/*
===========================================================================

mercenary.go - guild-soldier scroll creation and group dismissal

One consumed scroll creates the native guild-level count of character-owned
soldiers. The owner cooldown survives the group's death or dismissal.

===========================================================================
*/
package action

import (
	"math"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opMercenaryDismiss           uint16 = 0x7458
	mercenaryQuestOperationMask  uint32 = 0x10000
	mercenaryQuestRestricted     uint8  = 0x5f
	mercenaryDressPending        uint8  = 0x9d
	mercenaryUnknownLevel        uint8  = 0x11
	mercenaryMotionRestricted    uint8  = 0x0f
	mercenaryMasterRequired      uint8  = 0xbc
	mercenaryCooldownActive      uint8  = 0xbf
	mercenaryAlreadySummoned     uint8  = 0xc9
	mercenaryCountryMismatch     uint8  = 0xd2
	mercenaryMounted             uint8  = 0xc8
	guildMasterMercenaryCooldown uint8  = 0x55
)

/*
================
useMercenaryScroll

49AFE0's TID4=1 arm; 4FCEF0 validates the guild family before 4A2CE0
selects the owner's level row. No live state changes before every record
has passed encoding and identity validation.
================
*/
func (rt *Runtime) useMercenaryScroll(use persistentSummonUse, context domain.MercenaryContext, result *OpResult) bool {
	c := use.character
	refs, referenceSource := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	var base *enterworld.CharacterRef
	if referenceSource && use.ref.AssociatedCharacterCodename != "" && use.ref.AssociatedCharacterCodename != "xxx" {
		base, _ = refs.CharacterRefByCodename(use.ref.AssociatedCharacterCodename)
	}
	refusal := uint8(0)
	motion := rt.playerMotionState(use.division, c, use.nowMs)
	switch {
	case rt.QuestTravelBlocks != nil && rt.QuestTravelBlocks(c)&mercenaryQuestOperationMask != 0:
		refusal = mercenaryQuestRestricted
	case teleportBlocks(c.NativeTeleportMode):
		refusal = 0x69
	case motion == simulation.MotionSitting:
		refusal = cosSummonBusy
	case rt.jobDressPending(use.division, c.Name):
		refusal = mercenaryDressPending
	case c.ActiveCOS != nil && c.ActiveCOS.Mounted:
		refusal = mercenaryMounted
	case base == nil:
		refusal = cosSummonBusy
	case motion == simulation.MotionWall || motion == mercenaryMotionRestricted || rt.PlayerAttackLocked(use.division, c.Name) || rt.objectActionCommitted(use.division, c.Name):
		refusal = cosSummonBusy
	case context.Guild.ID == 0 || context.Guild.Level < 3:
		refusal = cosSummonBusy
	case !context.Master:
		refusal = mercenaryMasterRequired
	case len(c.Mercenaries) != 0:
		refusal = mercenaryAlreadySummoned
	case c.MercenarySummonUntilMs != 0:
		refusal = mercenaryCooldownActive
	}
	if refusal != 0 {
		*result = itemUseFailure(refusal)
		return false
	}
	count := domain.MercenaryCount(context.Guild.Level, context.UnionMaster)
	if count == 0 {
		*result = itemUseFailure(cosSummonBusy)
		return false
	}
	if int(base.Parameters.Country) != enterworld.NativeCountryByte9C(c) {
		*result = itemUseFailure(mercenaryCountryMismatch)
		return false
	}
	ref, valid := enterworld.MercenaryReference(refs, use.ref.AssociatedCharacterCodename, (&characterEquipRequirements{character: c}).characterLevel())
	if !valid {
		*result = itemUseFailure(mercenaryUnknownLevel)
		return false
	}
	minutes, present := use.ref.NativeFields.Lookup("itemParam1_29c")
	if !present || math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes <= 0 || minutes > math.MaxInt32/60 || math.Trunc(minutes) != minutes {
		return false
	}
	soldiers := make([]*domain.CharacterCOS, 0, count)
	var frames []wire.Frame
	for slot := 0; slot < count; slot++ {
		gid, valid := domain.MercenaryObjectID(c.ID, slot)
		if !valid {
			return false
		}
		pet := &domain.CharacterCOS{GID: gid, RefObjID: ref.RefObjID, Codename: ref.Codename,
			MercenaryAttributes: context.Guild.Byte10, Level: ref.Level, CurrentHP: ref.MaxHP, CurrentMP: ref.MaxMP, StateFlags: 3,
			Summoned: true, SummonGeneration: 1, Satiety: initialCompanionSatiety,
			RentalExpiresAtUnix: use.nowMs/1000 + int64(minutes)*60,
			NativeBodyStatus:    domain.InitialCOSBodyStatus(c.NativeBodyStatus)}
		// 4D97A0 initializes current vitals before applying guild maximum-HP modifiers.
		pet.RefreshRentalTimes(use.nowMs / 1000)
		record, err := enterworld.BuildCOSRecord(pet, ref, rt.deps.ItemReferences())
		if err != nil {
			return false
		}
		soldiers = append(soldiers, pet)
		frames = append(frames, wire.Frame{Opcode: wire.OpCosRecordCreate, Payload: record})
	}
	remaining := rt.consumeItemUseRow(c, use.row)
	c.Mercenaries = soldiers
	c.MercenarySummonUntilMs = (use.nowMs/1000 + domain.MercenarySummonSeconds) * 1000
	rt.petMu.Lock()
	owner := rt.petSessions[petOwnerKey{division: use.division, name: strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if owner != nil {
		owner.mercenaryPenalty = mercenaryPenaltyClock{lastMs: use.nowMs}
	}
	for _, pet := range soldiers {
		if owner != nil {
			rt.bindCompanionSession(use.division, owner, pet).summonedAtMs = use.nowMs
		}
		pose := rt.companionLiveSpawn(use.division, c, pet, use.nowMs)
		frames = append(frames, wire.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: wire.EncodeCosSpawnBand2(wire.CosSpawnBand2{
			Band: domain.MercenaryBand, RefObjID: ref.RefObjID, Gid: pet.GID, BodyStatus: pet.NativeBodyStatus,
			Position: wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle},
			Walk:     ref.WalkSpeed, Run: ref.RunSpeed, Scale: ref.Scale, OwnerName: context.Guild.Name, HoldType: enterworld.DressedJob(c), PvpState: c.PVPState(),
			OwnerModelRef: enterworld.CharacterModelRef(c, nil), OwnerGid: enterworld.ObjectIDForCharacter(c), State: cosSpawnFresh,
		})})
	}
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	result.Frames = append(result.Frames, frames...)
	result.Frames = append(result.Frames, MercenaryCooldownFrames(c, use.nowMs)...)
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

/*
================
MercenaryCooldownFrames

651D30 publishes owner timed job (2, 4); v1.150 reads it through 3792.
================
*/
func MercenaryCooldownFrames(c *enterworld.Character, nowMs int64) []wire.Frame {
	if c == nil || c.MercenarySummonUntilMs == 0 {
		return nil
	}
	seconds := uint32(int32(c.MercenarySummonUntilMs/1000 - nowMs/1000))
	return []wire.Frame{{Opcode: opTimedJobState, Payload: wire.NewWriter(6).U8(2).U8(4).U32(seconds).Payload()}}
}

/*
================
HandleMercenaryDismiss

517D20 / 4FDB00: empty 7458 request, every living soldier released,
no success acknowledgement. The independently stored cooldown remains.
================
*/
func (rt *Runtime) HandleMercenaryDismiss(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 0 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	var removed []*domain.CharacterCOS
	if !rt.deps.Update(c, "mercenary-dismiss", func() bool {
		if c.DeletePending {
			return false
		}
		var kept []*domain.CharacterCOS
		for _, pet := range c.Mercenaries {
			if pet != nil && pet.Summoned && pet.CurrentHP > 0 {
				removed = append(removed, pet)
			} else {
				kept = append(kept, pet)
			}
		}
		c.Mercenaries = kept
		return len(removed) > 0
	}) {
		return OpResult{}
	}
	var result OpResult
	for _, pet := range removed {
		result.Frames = append(result.Frames, rt.retireCosRuntime(division, c, pet.GID)...)
		frame := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()}
		result.Frames = append(result.Frames, frame)
		result.Broadcast = append(result.Broadcast, frame)
	}
	return result
}
