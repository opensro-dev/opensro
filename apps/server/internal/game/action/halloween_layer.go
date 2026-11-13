/*
===========================================================================

halloween_layer.go - the Halloween event switches the towns' decoration

Port-only, not native in this client build (SRO_HALLOWEEN_EVENT, default
off). The later client decorates Jangan and the other towns for Halloween
through its world layer 5 (layerobjdef.txt, DECO); the build imports
those placements tagged with their layer (scripts/world_layer_import.py)
and the browser client draws a layer while its event group is active
(foundation/world/world-layers.ts).

The v1.150 wire has the switch already: the entered tail lists the active
event groups and 0x3347 (u32 group, u8 state) changes one. Inference,
recorded because no later client executable is available to read: a
layer's id is its event group, so the event runs group 5.

While the window is open an entry lists the group (EntryEventGroups); an
open or close while players are online goes to every open-world division
as one 0x3347. The first tick only learns the state: an entry already
carried it, so a restart sends nothing.

===========================================================================
*/
package action

import (
	"encoding/binary"

	"opensro.online/server/internal/game/world/simulation"
)

// HalloweenEventGroup is the later client's Halloween decoration layer
// (layerobjdef.txt row 5, "할로윈(DECO)"), run as an event group.
const HalloweenEventGroup uint32 = 5

// eventGroupStateOpcode is the event-group change (client 761430):
// u32 group id, u8 state (1 on, 0 off).
const eventGroupStateOpcode uint16 = 0x3347

const (
	eventGroupOff uint8 = 0
	eventGroupOn  uint8 = 1
)

/*
================
Runtime.EntryEventGroups

The event groups an entry lists besides the character's own: the
Halloween group while the window is open, none otherwise.
================
*/
func (rt *Runtime) EntryEventGroups() []uint32 {
	if !rt.Halloween.ActiveAt(rt.Now()) {
		return nil
	}
	return []uint32{HalloweenEventGroup}
}

/*
================
EventGroupStatePayload
================
*/
func EventGroupStatePayload(group uint32, on bool) []byte {
	payload := make([]byte, 5)
	binary.LittleEndian.PutUint32(payload, group)
	payload[4] = eventGroupOff
	if on {
		payload[4] = eventGroupOn
	}
	return payload
}

/*
================
Runtime.advanceHalloweenLayer

Sends the group's change when the window opens or closes. With the event
off this is one branch.
================
*/
func (rt *Runtime) advanceHalloweenLayer(int64) []simulation.DivisionFrames {
	if !rt.Halloween.Enabled() || rt.Monsters == nil {
		return nil
	}
	active := rt.Halloween.ActiveAt(rt.Now())
	known := rt.halloweenLayerKnown
	if known && active == rt.halloweenLayerActive {
		return nil
	}
	rt.halloweenLayerKnown, rt.halloweenLayerActive = true, active
	if !known {
		return nil
	}
	payload := EventGroupStatePayload(HalloweenEventGroup, active)
	populations := rt.Monsters.OpenWorldPopulations()
	out := make([]simulation.DivisionFrames, 0, len(populations))
	for _, population := range populations {
		out = append(out, simulation.DivisionFrames{
			DivisionID: population.Division,
			Frames:     []simulation.Frame{{Opcode: eventGroupStateOpcode, Payload: payload}},
		})
	}
	return out
}
