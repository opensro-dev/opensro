/*
===========================================================================

fortress_construction.go - the fortress guild places a barricade

0x71E1 action 0x0A, sent by the fortress map window's confirm
(CIFFortressMap_OnConfirmMsgBox 65CD80 -> 703160) as {0x0A, fortress,
zone}: the one 0x71E1 request without a target GID. The v1.188 server
admits it at the
selected aide (service 0x19 in range), checks the zone
and queues QUERY_SIEGE_STRUCT_ADD;
the 0x13 result spawns the zone's structure and answers
{0x0A, 1, fortress, zone}.

Only barricades are built this way: the zone's authored structure must be
TypeID 1/2/5/6 (the native barricade test).

===========================================================================
*/
package action

import (
	"encoding/binary"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/fortress"
)

const (
	// Low bytes of the native construction and upgrade refusals.
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

The native aide admission, then the construction handler in its refusal order: an unknown
zone (3), a zone whose structure has no reference (0x281E), a structure
that is not a barricade (0x281F), a zone of another fortress (0x2820).
Natively there is no holder, role or war check here: the aide
admission is the only gate, and the add charges nothing (the add's
reference +0x19C is MaxHP, not a price).
INFERENCE: a zone that already holds a structure refuses as the request
that could not be queued (2): the add's database row is keyed by zone,
so a second one cannot be inserted. The barricade stands at full hit
points at once: its build time (reference +0x3B8, Param3, characterdata
column 112, read through the native construction timer) is 0 in every v1.150 barricade
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
	return rt.fortressConstructOnZone(division, c, request)
}

/*
================
fortressConstructOnZone

The native structure construction: shared by the 0x71E1
aide completion and the GM /SIEGE STRUCT_BUILD command (case 0x0A).
================
*/
func (rt *Runtime) fortressConstructOnZone(division string, c *enterworld.Character, request siege.Interaction) OpResult {
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
		// Natively the add is queued with the actor's guild id as the owner.
		var guild int64
		if c.GuildID != nil {
			guild = *c.GuildID
		}
		built, _ := rt.structureOnZone(division, world, zone)
		rt.storeStructureRow(division, domain.FortressStructureRecord{FortressID: fortressID, EventStructID: zone,
			RefObjID: ref.RefObjID, OwnerGuildID: guild, HP: built.CurrentHP})
		rt.pushStructureRoster(division, fortressID, world)
		reply := wire.NewWriter(10).U8(request.Action).U8(1).U32(fortressID).U32(zone)
		return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: reply.Payload()}}}
	}
	return fortressRefusal(request.Action, fortressErrInvalid)
}

/*
================
SiegeStructureGM

/SIEGE STRUCT_BUILD|STRUCT_UPGRADE|STRUCT_REPAIR|GATE (sub 0x20, through the
reference server's siege structure command) dispatches to build, upgrade,
repair, or gate.
================
*/
func (rt *Runtime) SiegeStructureGM(division, name string, action uint8, fortressID, eventStructID uint32, param uint8) bool {
	if rt == nil || rt.deps == nil || rt.Fortresses == nil || rt.Monsters == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm := rt.findCharacter(division, name)
	if gm == nil || !gm.GMPrivilege {
		return false
	}
	pushGM := func(frames []wire.Frame) {
		if rt.PushCharacterFrames != nil && len(frames) > 0 {
			rt.PushCharacterFrames(division, gm.Name, frames)
		}
	}
	switch action {
	case siege.ActionConstruct:
		res := rt.fortressConstructOnZone(division, gm, siege.Interaction{
			Action:    siege.ActionConstruct,
			Fortress:  fortressID,
			Reference: eventStructID,
		})
		pushGM(res.Frames)
		return len(res.Frames) == 1 && len(res.Frames[0].Payload) >= 2 && res.Frames[0].Payload[1] == 1
	case siege.ActionUpgrade:
		res := rt.fortressStructureUpgrade(division, gm, siege.Interaction{
			Action:    siege.ActionUpgrade,
			Fortress:  fortressID,
			Reference: eventStructID,
			Value8:    param,
		})
		pushGM(res.Frames)
		return upgradeSucceeded(res)
	case siege.ActionRepair:
		res := rt.fortressStructureRepair(division, gm, siege.Interaction{
			Action:    siege.ActionRepair,
			Fortress:  fortressID,
			Reference: eventStructID,
		})
		pushGM(res.Frames)
		return len(res.Frames) == 2 && len(res.Frames[1].Payload) >= 2 && res.Frames[1].Payload[1] == 1
	case siege.ActionGate:
		// The native GM gate command: resolves the gate by
		// eventStructID directly; returns false without a 0xB1E1 refusal packet
		// when missing, wrong fortress, destroyed, or unchanged.
		foundFortress, world, gate, found := rt.structureByZone(division, eventStructID)
		if !found || foundFortress != fortressID {
			return false
		}
		gate, changed := rt.Monsters.SetGateState(division, gate.Gid, uint16(param))
		if gate.Gid == 0 || !changed {
			return false
		}
		rt.pushFortressWorld(division, world, siege.EncodeStructureState3887(siege.StructureState{
			FortressID: fortressID, GID: gate.Gid, EventStructID: eventStructID, State: gate.StructureState,
		}))
		reply := wire.NewWriter(12).U8(siege.ActionGate).U8(1).U32(fortressID).U32(eventStructID).U16(gate.StructureState)
		pushGM([]wire.Frame{{Opcode: opFortressInteractionResult, Payload: reply.Payload()}})
		return true
	default:
		return false
	}
}

/*
================
SiegePeriodGM

/SIEGE STARTWAR|ENDWAR|STARTTAX|ENDTAX|STARTREQ|ENDREQ (sub 0x21, case 0x1A,
through the native fortress manager's shard message): toggles the
fortress period bit and broadcasts the corresponding 0x3887 edge to the
division.
================
*/
func (rt *Runtime) SiegePeriodGM(division string, periodAction uint8) bool {
	if rt == nil || rt.Fortresses == nil {
		return false
	}
	var (
		period  fortress.Period
		active  bool
		subtype uint8
	)
	switch periodAction {
	case 1:
		period, active, subtype = fortress.PeriodWar, true, siege.SubtypeWarBegin
	case 2:
		period, active, subtype = fortress.PeriodWar, false, siege.SubtypeWarEnd
	case 3:
		period, active, subtype = fortress.PeriodTax, true, siege.SubtypeTaxPeriodBegin
	case 4:
		period, active, subtype = fortress.PeriodTax, false, siege.SubtypeTaxPeriodEnd
	case 5:
		period, active, subtype = fortress.PeriodRequest, true, siege.SubtypeRequestPeriodOpen
	case 6:
		period, active, subtype = fortress.PeriodRequest, false, siege.SubtypeRequestPeriodEnd
	default:
		return false
	}
	changed := rt.Fortresses.SetPeriod(division, period, active)
	frames := []wire.Frame{{Opcode: opFortressWarState, Payload: siege.EncodeEdge3887(subtype)}}
	if period == fortress.PeriodWar && changed {
		for _, batch := range rt.FortressWarChanged(division, rt.Now().UnixMilli(), active) {
			for _, f := range batch.Frames {
				frames = append(frames, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
			}
		}
	}
	if rt.PushCharacterFrames != nil && rt.deps != nil {
		for _, c := range rt.deps.CharactersForDivision(division) {
			if c != nil {
				rt.PushCharacterFrames(division, c.Name, frames)
			}
		}
	}
	return true
}
