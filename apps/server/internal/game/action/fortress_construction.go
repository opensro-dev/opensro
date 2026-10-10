/*
===========================================================================

fortress_construction.go - the fortress guild places a barricade

0x71E1 action 0x0A, sent by the fortress map window's confirm
(CIFFortressMap_OnConfirmMsgBox 65CD80 -> 703160) as {0x0A, fortress,
zone}: the one 0x71E1 request without a target GID. The v1.188 server
(CSiegeFortressMgr_HandleStructureConstruction 6341B0) admits it at the
selected aide (519E60 case 0xA: service 0x19 in range), checks the zone
and queues QUERY_SIEGE_STRUCT_ADD (CSiegeFortress_RequestStructAdd 6226B0);
the 0x13 result (6232C0) spawns the zone's structure and answers
{0x0A, 1, fortress, zone}.

Only barricades are built this way: the zone's authored structure must be
TypeID 1/2/5/6 (TID_IsBarricadeStructure 489170).

===========================================================================
*/
package action

import (
	"encoding/binary"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
)

const (
	// Low bytes of 6341B0's refusals.
	fortressErrConstructNoReference uint8 = 0x1e // 0x281E
	fortressErrConstructKind        uint8 = 0x1f // 0x281F
	fortressErrConstructFortress    uint8 = 0x20 // 0x2820
	// structureKindBarricade is TypeID4 6 of a structure (TypeID3 5).
	structureKindBarricade uint8 = 6
)

/*
================
fortressConstructionRequest

Recognises the construction completion without sniffing its first byte
alone: every other 0x71E1 request names the selected NPC in its first
four bytes, the completion names none. A 9-byte payload led by 0x0A that
does not name the selected NPC is the completion.
================
*/
func (rt *Runtime) fortressConstructionRequest(division string, c *enterworld.Character, payload []byte) (siege.Interaction, bool) {
	if len(payload) != 9 || payload[0] != siege.ActionConstruct || rt.Selected == nil {
		return siege.Interaction{}, false
	}
	if selected, ok := rt.Selected.Get(division, c.Name); ok && binary.LittleEndian.Uint32(payload[:4]) == selected {
		return siege.Interaction{}, false
	}
	request, err := siege.DecodeConstruction(payload)
	return request, err == nil
}

/*
================
fortressConstruct

519E60's aide admission, then 6341B0 in its refusal order: an unknown
zone (3), a zone whose structure has no reference (0x281E), a structure
that is not a barricade (0x281F), a zone of another fortress (0x2820).
Natively there is no holder, role or war check here: 519E60's aide
admission is the only gate, and the add charges nothing (6226B0's
reference +0x19C is MaxHP, not a price).
INFERENCE: a zone that already holds a structure refuses as the request
that could not be queued (2): the add's database row is keyed by zone,
so a second one cannot be inserted. The barricade stands at full hit
points at once: its build time (reference +0x3B8, Param3, characterdata
column 112, read through 61E000's timer) is 0 in every v1.150 barricade
row. Its row is stored at once, and the reply names fortress and zone.
================
*/
func (rt *Runtime) fortressConstruct(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	selected, ok := rt.Selected.Get(division, c.Name)
	if !ok {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	npc, ok := rt.npcForCurrentViewer(division, c, selected)
	if !ok || !npc.Services.Has(siege.InteractionService(request.Action)) || !rt.npcWithinHitRange(division, c, npc) {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	if rt.Fortresses == nil || rt.Monsters == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	zone := request.Reference
	for fortressID, world := range rt.fortressWorlds() {
		ref, occupied, found := rt.Monsters.StructureZone(division, world, zone)
		if !found {
			continue
		}
		switch {
		case ref.RefObjID == 0:
			return fortressRefusal(request.Action, fortressErrConstructNoReference)
		case ref.TypeID4 != structureKindBarricade:
			return fortressRefusal(request.Action, fortressErrConstructKind)
		case fortressID != request.Fortress:
			return fortressRefusal(request.Action, fortressErrConstructFortress)
		case occupied:
			return fortressRefusal(request.Action, fortressErrUnknown)
		}
		if !rt.Monsters.SetStructureOccupant(division, world, zone, ref.RefObjID, rt.Now().UnixMilli()) {
			return fortressRefusal(request.Action, fortressErrUnknown)
		}
		// 6226B0 queues the add with CGObjPC_GetGuildID(actor) as the owner.
		var guild int64
		if c.GuildID != nil {
			guild = *c.GuildID
		}
		built, _ := rt.structureOnZone(division, world, zone)
		rt.storeStructureRow(division, domain.FortressStructureRecord{FortressID: fortressID, EventStructID: zone,
			RefObjID: ref.RefObjID, OwnerGuildID: guild, HP: built.CurrentHP})
		reply := wire.NewWriter(10).U8(request.Action).U8(1).U32(fortressID).U32(zone)
		return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: reply.Payload()}}}
	}
	return fortressRefusal(request.Action, fortressErrInvalid)
}
