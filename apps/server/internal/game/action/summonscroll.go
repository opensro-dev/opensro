/*
===========================================================================

summonscroll.go - the Item Mall summon scrolls: personal guild soldiers

Port-only, not native (owner ask 2026-11-12, issue #3257): the later
mall sells three summon scrolls v1.150's mall never listed -
ITEM_ETC_E050729_SUMMON_SCROLL, ..._ONE_SCROLL (1인용) and
..._TWO_SCROLL (2인용). The items ship in v1.150's data as dead
3/3/9/0 quest tools: no associated character, Param1 -1, named by no
native family. Behind SRO_MALL_CATEGORIES (default off) the mall lists
them in Premium's Others tab (commerce/mallcategories.go) and this
file serves their use; off, the items keep the native game - the
quest-tool family has no handler for them and the mall lists nothing.

The effect is authored on the guild-soldier lane (mercenary.go), the
one native mechanism that summons character-owned soldiers from a
consumed scroll: ONE and the plain scroll call one soldier, TWO calls
two, each at the player's own level row of their country's guild
soldier group (COS_GUILD_CH_SOLDIER1 / COS_GUILD_EU_SOLDIER1, the
groups the guild scrolls name). The scroll's own row carries no
duration, so the soldiers stand for the guild soldiers' native
1,200 s (domain.MercenarySummonSeconds, the guild scrolls' Param1
20 minutes) and share the guild lane's summon cooldown and its
0x7458 dismissal: the soldiers live in c.Mercenaries, so no new
persisted state exists. Refusals mirror the mercenary switch minus
its guild arms (no guild, master or union is asked of a personal
scroll). Prices are authored in mallcategories.go.

===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// The three scrolls and the soldier count each one calls. Admission is
// by codename (itemuse_admission.go): the shipped 3/3/9/0 type is the
// quest tools', and only these three rows leave it.
const (
	SummonScrollItem    = "ITEM_ETC_E050729_SUMMON_SCROLL"
	SummonScrollOneItem = "ITEM_ETC_E050729_SUMMON_ONE_SCROLL"
	SummonScrollTwoItem = "ITEM_ETC_E050729_SUMMON_TWO_SCROLL"
)

var summonScrollCounts = map[string]int{
	SummonScrollItem:    1,
	SummonScrollOneItem: 1,
	SummonScrollTwoItem: 2,
}

// The guild-soldier groups a personal scroll draws from, by the
// owner's country: the same base rows the guild soldier scrolls name
// (ITEM_ETC_GUILD_CH_SOLDIER1_SCROLL / _EU_ in the shipped itemdata).
var summonScrollSoldierBases = []string{
	"COS_GUILD_CH_SOLDIER1_001",
	"COS_GUILD_EU_SOLDIER1_001",
}

/*
================
SummonScrollCount

The soldiers a scroll codename calls; zero for any other item.
================
*/
func SummonScrollCount(codename string) int {
	return summonScrollCounts[codename]
}

/*
================
useSummonScroll

One use spends the scroll and stands the scroll's count of soldiers
beside the owner. Every record is encoded before any state changes,
exactly as the guild lane orders it; the flag is re-checked here so a
scroll bought while the categories stood open is dead stock once
they close.
================
*/
func (rt *Runtime) useSummonScroll(use persistentSummonUse, result *OpResult) bool {
	c := use.character
	if on, err := commerce.MallCategoriesFromEnv(); err != nil || !on {
		result.DiagnosticRefusal = "item-use: summon scrolls are off (" + commerce.EnvMallCategories + ")"
		return false
	}
	count := SummonScrollCount(use.ref.Codename)
	refs, referenceSource := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	var base *enterworld.CharacterRef
	var baseCode string
	if referenceSource {
		for _, code := range summonScrollSoldierBases {
			candidate, found := refs.CharacterRefByCodename(code)
			if found && candidate != nil && int(candidate.Parameters.Country) == int(enterworld.NativeCountryByte9C(c)) {
				base, baseCode = candidate, code
				break
			}
		}
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
	case count == 0:
		refusal = cosSummonBusy
	case base == nil:
		refusal = mercenaryCountryMismatch
	case motion == simulation.MotionWall || motion == mercenaryMotionRestricted || rt.PlayerAttackLocked(use.division, c.Name) || rt.objectActionCommitted(use.division, c.Name):
		refusal = cosSummonBusy
	case len(c.Mercenaries) != 0:
		refusal = mercenaryAlreadySummoned
	case c.MercenarySummonUntilMs != 0:
		refusal = mercenaryCooldownActive
	}
	if refusal != 0 {
		*result = itemUseFailure(refusal)
		return false
	}
	ref, valid := enterworld.MercenaryReference(refs, baseCode, (&characterEquipRequirements{character: c}).characterLevel())
	if !valid {
		*result = itemUseFailure(mercenaryUnknownLevel)
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
			Level: ref.Level, CurrentHP: ref.MaxHP, CurrentMP: ref.MaxMP, StateFlags: 3,
			Summoned: true, SummonGeneration: 1, Satiety: initialCompanionSatiety,
			RentalExpiresAtUnix: use.nowMs/1000 + domain.MercenarySummonSeconds,
			NativeBodyStatus:    domain.InitialCOSBodyStatus(c.NativeBodyStatus)}
		// The reference server initializes current vitals before applying guild maximum-HP modifiers.
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
	for _, pet := range soldiers {
		if owner != nil {
			rt.bindCompanionSession(use.division, owner, pet).summonedAtMs = use.nowMs
		}
		pose := rt.companionLiveSpawn(use.division, c, pet, use.nowMs)
		frames = append(frames, wire.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: wire.EncodeCosSpawnBand2(wire.CosSpawnBand2{
			Band: domain.MercenaryBand, RefObjID: ref.RefObjID, Gid: pet.GID, BodyStatus: pet.NativeBodyStatus,
			Position: wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle},
			Walk:     ref.WalkSpeed, Run: ref.RunSpeed, Scale: ref.Scale, OwnerName: enterworld.PartyMemberName(c), HoldType: enterworld.DressedJob(c), PvpState: c.PVPState(),
			OwnerModelRef: enterworld.CharacterModelRef(c, nil), OwnerGid: enterworld.ObjectIDForCharacter(c), State: cosSpawnFresh,
		})})
	}
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	result.Frames = append(result.Frames, frames...)
	result.Frames = append(result.Frames, MercenaryCooldownFrames(c, use.nowMs)...)
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}
