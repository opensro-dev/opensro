/*
===========================================================================

fortress_gates.go - a fortress gate opened and shut by its pulley

The pulleys (STRUCTURE_GATE_PULLEY_*) are NPCs with service 0x1F; each
names its gate's event zone in its characterdata column 4
(STRUCTURE_POS_JA_GATE_01). CSiegeFortressMgr_HandleGatePulleyOperation
(634C90) reads the gate through that link (the pulley reference's
struct-ext +0x48) and sets the gate's state word.

The port's gates do not block movement as objects yet, so the state is the
gate's published look and record: an open gate is not yet walkable.

===========================================================================
*/
package action

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// Low bytes of the pulley refusals (634C90).
	fortressErrGateNoZoneOpen       uint8 = 0x27 // 0x2827, an open request
	fortressErrGateNoZoneShut       uint8 = 0x28 // 0x2828, a shut request
	fortressErrGateWrongFort        uint8 = 0x29 // 0x2829
	fortressErrGateDestroyed        uint8 = 0x3c // 0x283C
	pulleyCodenamePrefix                  = "STRUCTURE_GATE_PULLEY_"
	characterOriginalCodenameColumn       = 4
	eventZoneIDColumn                     = 1
	eventZoneCodenameColumn               = 2
)

/*
================
loadGatePulleys

Each pulley's RefObjID and the event zone its column 4 names. A pulley
whose zone v1.150 does not serve (the Bandit and Hotan fortresses) is left
out: 634C90 answers its request 0x27 or 0x28, as for a missing record.
================
*/
func loadGatePulleys(dir string) (map[uint32]uint32, error) {
	zones := make(map[string]uint32)
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "eventzonedata.txt")) {
		if len(row) <= eventZoneCodenameColumn || row[0] != "1" {
			continue
		}
		if id, err := strconv.ParseUint(row[eventZoneIDColumn], 10, 32); err == nil {
			zones[strings.TrimSpace(row[eventZoneCodenameColumn])] = uint32(id)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "characterdata_*.txt"))
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]uint32)
	for _, file := range files {
		for _, row := range enterworld.ReadTextdataFile(file) {
			if len(row) <= characterOriginalCodenameColumn || row[0] != "1" || !strings.HasPrefix(row[2], pulleyCodenamePrefix) {
				continue
			}
			refObjID, err := strconv.ParseUint(row[1], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("gate pulley %q has no RefObjID", row[2])
			}
			if zone, ok := zones[strings.TrimSpace(row[characterOriginalCodenameColumn])]; ok {
				out[uint32(refObjID)] = zone
			}
		}
	}
	return out, nil
}

/*
================
fortressGatePulley

634C90 in its refusal order: a pulley with no gate zone answers 0x27 for
an open request and 0x28 for a shut one, a gate of another fortress 0x29,
no gate on the zone 3, a destroyed gate 0x3C. CGObjSiegeStruct_SetState
(4CF860) applies the state mask and reports whether it changed. Only a
change broadcasts 0x3887 and queues success (634E0F); 6232C0's 0x15 result
carries the resulting state with the fortress and zone, not the request.
================
*/
func (rt *Runtime) fortressGatePulley(division string, pulley simulation.NpcDef, request siege.Interaction) OpResult {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	state := request.Value16
	zone, ok := rt.gatePulleys[pulley.RefObjID]
	if !ok {
		if state != 0 {
			return fortressRefusal(request.Action, fortressErrGateNoZoneOpen)
		}
		return fortressRefusal(request.Action, fortressErrGateNoZoneShut)
	}
	fortressID, world, gate, found := rt.structureByZone(division, zone)
	if !found {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	if fortressID != request.Fortress {
		return fortressRefusal(request.Action, fortressErrGateWrongFort)
	}
	gate, changed := rt.Monsters.SetGateState(division, gate.Gid, state)
	if gate.Gid == 0 {
		return fortressRefusal(request.Action, fortressErrGateDestroyed)
	}
	if !changed {
		return OpResult{}
	}
	rt.pushFortressWorld(division, world, siege.EncodeStructureState3887(siege.StructureState{
		FortressID: fortressID, GID: gate.Gid, EventStructID: zone, State: gate.StructureState,
	}))
	reply := wire.NewWriter(12).U8(request.Action).U8(1).U32(fortressID).U32(zone).U16(gate.StructureState)
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: reply.Payload()}}}
}
