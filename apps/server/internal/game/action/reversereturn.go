/*
===========================================================================

reversereturn.go - the reverse return scroll: back to the last recall point
or to where the player died

The beginner guides (NPC_EU_ADVICE3, NPC_CH_SOLDIER_EM1) hold service 0x1E
(CGObjNPC_SpawnAndConfigureServices 4C6350), so their select grant carries
capability 0x20000000; the talk window then lists two rows (5D4410, action
0x2B) and a click sends 0x7495 [u32 gid][u8 5][u8 choice] (6FEF10): 2 is the
last recall point, 3 the place of the last death.

CGObjPC_HandleTeleportUseRequest0x705A_Body (4F2B50) case 5 is free: the
NPC must offer a teleport service (CGObj_HasTeleportService 485D00: 8, 9,
0x1C or 0x1E, else 3), then 0x1C17 under operation mask 0x40000, 0x1C10
with a transport out, 0x1C20 past max level 20, 2 for any other choice or
a missing point (0x1C21 for one outside any world), and the move. Its
own 0x1E test (4F36A4) asks the player's service set, which is always
empty, so it never refuses.

ITEM_MALL_REVERSE_RETURN_SCROLL (3/3/3/3) is used from the bag. A right
click opens a two-row choice box (message box 0x1E); CGInterface_OnMsgBoxResult
(6971B0) answers it with CIFInventory_ExecuteItemAction, which sends 0x75BD
[slot][u16 type][u8 choice]: 2 the last recall point, 3 the last death
point. 755E40 (type 4 = 3) starts the cast as a return scroll's.
CGItemExpendable_UseReverseReturnScroll (v1.188 4A00C0) reads the byte: the
shared return admissions, the recorded point (0x1885 / 0x1886 when absent),
the timed cast, and the answer on the item-use channel (0xB5BD with the
scroll's slot, category-1 notice 390). v1.188's choice 7 (a saved point
and its u32) has no v1.150 sender and is refused.

INFERENCE: a teleport gate also lists the rows while the player holds a
scroll, and the click runs the same scroll rule.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// gateReverseReturn is the 0x7495 type the reverse return rows send.
	gateReverseReturn uint8 = 5
	// The two 0x7495 type-5 choices (5DA1B0 case 0x2B rows 1 and 2).
	reverseReturnLastRecall uint8 = 2
	reverseReturnLastDeath  uint8 = 3

	// guideReturnMaxLevel is the highest max level 4F2B50 case 5 serves.
	guideReturnMaxLevel int64 = 20

	// The low bytes of 4F2B50 case 5's refusals.
	errCodeTeleportService uint8 = 0x03
	errCodeGuideQuestBlock uint8 = 0x17
	errCodeGuideTransport  uint8 = 0x10
	errCodeGuideLevel      uint8 = 0x20
	errCodeGuideNoPoint    uint8 = 0x02

	// operationMaskQuestTravel is the operation mask bit 0x1C17 refuses.
	operationMaskQuestTravel uint32 = 0x40000

	// The low bytes of 4A00C0's 0x1885 and 0x1886.
	errCodeNoRecallPoint uint8 = 0x85
	errCodeNoDeathPoint  uint8 = 0x86
)

/*
================
reverseReturnScrollRow

The bag row of the first reverse return scroll the player holds.
================
*/
func (rt *Runtime) reverseReturnScrollRow(c *enterworld.Character) (int, *enterworld.ItemRef, bool) {
	refs := rt.deps.ItemReferences()
	if refs == nil {
		return 0, nil, false
	}
	found, slot := -1, int64(0)
	var held *enterworld.ItemRef
	for index, row := range c.MissionInventory {
		if !inventory.InBag(c, row.Slot) || row.StackCount < 1 || found >= 0 && row.Slot >= slot {
			continue
		}
		ref, ok := refs.ItemRefByCodename(row.Codename)
		if !ok || ref == nil || ref.RefObjID != row.RefObjID || ref.TypeIDs != [4]int64{3, 3, 3, 3} {
			continue
		}
		found, slot, held = index, row.Slot, ref
	}
	return found, held, found >= 0
}

/*
================
reverseReturnCapability

A teleport gate's extra select bit while the player holds a reverse return
scroll (INFERENCE, see the file header). The guides' bit is their service
0x1E, already in TalkFlags. The caller holds the division lock.
================
*/
func (rt *Runtime) reverseReturnCapability(npc simulation.NpcDef, c *enterworld.Character) uint32 {
	if npc.TalkFlags&simulation.NpcTalkFlagTeleport == 0 {
		return 0
	}
	if _, _, held := rt.reverseReturnScrollRow(c); !held {
		return 0
	}
	return simulation.NpcTalkFlagReverseReturn
}

/*
================
reverseReturnPoint

The recorded point a choice names, or the refusal for a missing one.
================
*/
func reverseReturnPoint(c *enterworld.Character, choice uint8) (travelPoint, uint8) {
	var point *domain.WorldPoint
	refusal := errCodeNoRecallPoint
	if c.World != nil && choice == reverseReturnLastRecall {
		point = c.World.LastRecallPoint
	}
	if choice == reverseReturnLastDeath {
		refusal = errCodeNoDeathPoint
		if c.World != nil {
			point = c.World.LastDeathPoint
		}
	}
	if point == nil || point.RegionID == nil || *point.RegionID == 0 {
		return travelPoint{}, refusal
	}
	// The point keeps its GameWorldID; a recorded world is type 0, entered
	// on layer 1. Absent means the field.
	world := instance.ID(domain.DefaultWorldInstance)
	if point.World != 0 {
		world = instance.Pack(instance.DefinitionID(point.World), portalWorldLayer)
	}
	return travelPoint{spawn: missionSpawnFromWorld(&point.WorldSpawn, simulation.Spawn{}), world: world}, 0
}

/*
================
hasTeleportService

CGObj_HasTeleportService (485D00): a teleport, recall, gate or reverse
return service.
================
*/
func hasTeleportService(npc simulation.NpcDef) bool {
	return npc.Services.Has(simulation.NpcServiceTeleport) || npc.Services.Has(simulation.NpcServiceResurrectPoint) ||
		npc.Services.Has(simulation.NpcServiceTeleportGate) || npc.Services.Has(simulation.NpcServiceReverseReturn)
}

/*
================
handleReverseReturn

0x7495 type 5 from the selected NPC: a teleport gate spends a held scroll
(the inference above), every other teleport service NPC runs 4F2B50's free
case 5.
================
*/
func (rt *Runtime) handleReverseReturn(division string, c *enterworld.Character, gid uint32, choice uint8) OpResult {
	if c == nil {
		return portalFailure(2)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return portalFailure(2)
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok {
		return portalFailure(2)
	}
	// 4A8E10 before the service test, as every 4F2B50 kind.
	if !rt.npcWithinHitRange(division, c, npc) {
		return portalFailure(hitRangeTooFar)
	}
	if !hasTeleportService(npc) {
		return portalFailure(errCodeTeleportService)
	}
	if _, _, held := rt.reverseReturnScrollRow(c); held && !npc.Services.Has(simulation.NpcServiceReverseReturn) {
		if choice != reverseReturnLastRecall && choice != reverseReturnLastDeath {
			return portalFailure(2)
		}
		return rt.scrollReverseReturn(division, c, choice)
	}
	return rt.guideReverseReturn(division, c, choice)
}

/*
================
scrollReverseReturn

The held scroll's reverse return from a teleport gate. The caller holds
the division lock.
================
*/
func (rt *Runtime) scrollReverseReturn(division string, c *enterworld.Character, choice uint8) OpResult {
	result := itemUseFailure(wire.ErrCodeInvalidRequest)
	var used *enterworld.ItemRef
	now := rt.Now().UnixMilli()
	committed := rt.deps.Update(c, "reverse-return", func() bool {
		if c.DeletePending {
			return false
		}
		if !enterworld.CharacterAlive(c) {
			result = itemUseFailure(wire.ErrCodeItemUseDead)
			return false
		}
		row, ref, held := rt.reverseReturnScrollRow(c)
		if !held {
			return false
		}
		used = ref
		return rt.beginReverseReturnScroll(division, c, ref, row, choice, now, &result)
	})
	if committed && used != nil {
		rt.publishItemUseVisual(c, used, &result)
	}
	return result
}

/*
================
beginReverseReturnScroll

4A00C0: the scroll in bag row `row` starts its cast to the chosen point.
The caller holds the character's update.
================
*/
func (rt *Runtime) beginReverseReturnScroll(division string, c *enterworld.Character, ref *enterworld.ItemRef, row int, choice uint8, now int64, result *OpResult) bool {
	if choice != reverseReturnLastRecall && choice != reverseReturnLastDeath {
		return false
	}
	duration, ok := returnScrollDuration(ref)
	if !ok || !rt.returnScrollAdmission(division, c, result) {
		return false
	}
	destination, refusal := reverseReturnPoint(c, choice)
	if refusal != 0 {
		*result = itemUseFailure(refusal)
		return false
	}
	return rt.startReturnCast(returnCast{division: division, character: c, row: row,
		slot: uint8(c.MissionInventory[row].Slot), typeWord: ref.TypeFlags(), duration: duration,
		destination: &destination, now: now}, result)
}

/*
================
guideReverseReturn

4F2B50 case 5: the free move to the chosen point, refused in the native
order. The caller holds the division lock.
================
*/
func (rt *Runtime) guideReverseReturn(division string, c *enterworld.Character, choice uint8) OpResult {
	if rt.QuestTravelBlocks != nil && rt.QuestTravelBlocks(c)&operationMaskQuestTravel != 0 {
		return portalFailure(errCodeGuideQuestBlock)
	}
	if rt.hasSummonedTransportCOS(c) {
		return portalFailure(errCodeGuideTransport)
	}
	if c.MaxLevel != nil && *c.MaxLevel > guideReturnMaxLevel {
		return portalFailure(errCodeGuideLevel)
	}
	if choice != reverseReturnLastRecall && choice != reverseReturnLastDeath {
		return portalFailure(errCodeGuideNoPoint)
	}
	// 0x1C21 is a recorded point without a world; a port point is
	// always in the world it was recorded in.
	point, refusal := reverseReturnPoint(c, choice)
	if refusal != 0 {
		return portalFailure(errCodeGuideNoPoint)
	}
	return rt.commitGateTravel(gateTravel{division: division, character: c, destination: point.spawn, world: point.world, reason: "guide-reverse-return"}, func() (int64, OpResult, bool) {
		if c.DeletePending || !enterworld.CharacterAlive(c) {
			return 0, portalFailure(2), false
		}
		return 0, OpResult{}, true
	})
}
