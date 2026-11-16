/*
===========================================================================

event_groups.go - the seasonal events switch the client's event groups

Port-only, not native in this client build (SRO_HALLOWEEN_EVENT and
SRO_CHRISTMAS_EVENT, both default off). The v1.150 wire has the switch
already: the entered tail lists the active event groups and 0x3347
(u32 group, u8 state) changes one.

  - Halloween runs group 5. The later client decorates the towns through
    its world layer 5 (layerobjdef.txt, DECO); the build imports those
    placements tagged with their layer (scripts/world_layer_import.py) and
    the browser client draws a layer while its event group is active
    (foundation/world/world-layers.ts). Inference, recorded because no
    later client executable is available to read: a layer's id is its
    event group.

  - Christmas runs groups 1 and 7. Group 1 is the v1.150 client's own
    Christmas event: its entry or 0x3347 prints UIIT_MSG_EVENT_START (the
    Japanese row names the stolen-socks event) and turns the snow on
    (client 77B220 / 761430 / 7775A0, foundation/gameplay/
    event-environment.ts). Group 7 is the later client's Christmas
    decoration layer ("크리스마스(DECO)": trees, snowmen, wreaths and gift
    boxes in the towns), by the same layer-is-group inference. Group 7
    stands the whole window; group 1 is on only during a snowfall
    (progression/christmas_snow.go), so its snow and its start and end
    lines come and go with each one, as the client was built for.

  - So-Ok's Christmas Gift (the Christmas switch in mode so_ok_2006)
    runs the same two: group 1 on the winter event's snowfall timetable
    (one weather rule for every Christmas event), group 7 the whole
    window. It adds a port-only group the client knows
    only as the town carols' switch (ChristmasTownCarolsEventGroup): no
    native table names it, so no client draws or plays anything for it
    but the browser's carols in the towns (event-environment.ts).

While a window is open an entry lists its groups (EntryEventGroups); an
open or close while players are online goes to every open-world division
as one 0x3347 per group. The first tick only learns each event's state:
an entry already carried it, so a restart sends nothing.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"time"

	"opensro.online/server/internal/game/world/simulation"
)

// HalloweenEventGroup is the later client's Halloween decoration layer
// (layerobjdef.txt row 5, "할로윈(DECO)"), run as an event group.
const HalloweenEventGroup uint32 = 5

// ChristmasEventGroup is the v1.150 client's Christmas event group: its
// start notice and the event snow (event-environment.ts, group 1).
const ChristmasEventGroup uint32 = 1

// ChristmasDecorationEventGroup is the later client's Christmas
// decoration layer (layerobjdef.txt row 7, "크리스마스(DECO)"), run as an
// event group.
const ChristmasDecorationEventGroup uint32 = 7

// ChristmasTownCarolsEventGroup is port-only, not native: the switch
// the browser client plays the carols in the towns by, the whole window
// of So-Ok's Christmas Gift. Far above every layerobjdef.txt row, so no
// world layer ever answers to it.
const ChristmasTownCarolsEventGroup uint32 = 2006

// eventGroupStateOpcode is the event-group change (client 761430):
// u32 group id, u8 state (1 on, 0 off).
const eventGroupStateOpcode uint16 = 0x3347

const (
	eventGroupOff uint8 = 0
	eventGroupOn  uint8 = 1
)

/*
================
eventGroupWatch

The window state advanceEventGroups last saw for one seasonal event;
the tick goroutine alone touches it.
================
*/
type eventGroupWatch struct {
	known, active bool
}

/*
================
seasonalGroups

One seasonal event's groups that turn on and off together: whether the
event is switched on at all, which of its spans turns them on, and their
watch. Plain values only (the span is an enum, the groups a fixed
array), so the per-tick pass allocates nothing for them.
================
*/
type seasonalGroups struct {
	enabled bool
	span    seasonalSpan
	groups  [2]uint32
	count   int
	watch   *eventGroupWatch
}

/*
================
seasonalSpan

Which instants an event's groups are on.
================
*/
type seasonalSpan uint8

const (
	halloweenWindow seasonalSpan = iota
	christmasSnowfall
	christmasWindow
	sookChristmasSnowfall
	sookChristmasWindow
)

/*
================
Runtime.seasonalSpanActive
================
*/
func (rt *Runtime) seasonalSpanActive(span seasonalSpan, now time.Time) bool {
	switch span {
	case halloweenWindow:
		return rt.Halloween.ActiveAt(now)
	case christmasSnowfall:
		return rt.Christmas.SnowingAt(now)
	case christmasWindow:
		return rt.Christmas.ActiveAt(now)
	case sookChristmasSnowfall:
		return rt.SoOkChristmas.SnowingAt(now)
	case sookChristmasWindow:
		return rt.SoOkChristmas.ActiveAt(now)
	}
	return false
}

/*
================
Runtime.seasonalGroups

Every seasonal event that drives event groups, in the order their
changes go out.
================
*/
func (rt *Runtime) seasonalGroups() [5]seasonalGroups {
	return [5]seasonalGroups{
		{enabled: rt.Halloween.Enabled(), span: halloweenWindow, groups: [2]uint32{HalloweenEventGroup}, count: 1, watch: &rt.halloweenGroups},
		// The native group falls only during a snowfall (its snow and its
		// start and end lines); the decoration stands the whole window.
		{enabled: rt.Christmas.Enabled(), span: christmasSnowfall, groups: [2]uint32{ChristmasEventGroup}, count: 1, watch: &rt.christmasSnow},
		{enabled: rt.Christmas.Enabled(), span: christmasWindow, groups: [2]uint32{ChristmasDecorationEventGroup}, count: 1, watch: &rt.christmasGroups},
		// So-Ok's Christmas Gift: the winter event's snowfalls; the
		// decoration and the town carols the whole window. The two
		// Christmas events never run together (progression/christmas_mode.go).
		{enabled: rt.SoOkChristmas.Enabled(), span: sookChristmasSnowfall, groups: [2]uint32{ChristmasEventGroup}, count: 1, watch: &rt.sookChristmasSnow},
		{enabled: rt.SoOkChristmas.Enabled(), span: sookChristmasWindow, groups: [2]uint32{ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup}, count: 2, watch: &rt.sookChristmasGroups},
	}
}

/*
================
Runtime.EntryEventGroups

The event groups an entry lists besides the character's own: each
seasonal event's groups while its window is open, none otherwise.
================
*/
func (rt *Runtime) EntryEventGroups() []uint32 {
	var out []uint32
	now := rt.Now()
	for _, seasonal := range rt.seasonalGroups() {
		if rt.seasonalSpanActive(seasonal.span, now) {
			out = append(out, seasonal.groups[:seasonal.count]...)
		}
	}
	return out
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
Runtime.advanceEventGroups

Sends each seasonal event's group changes when its window opens or
closes. With every event off this is one branch per event.
================
*/
func (rt *Runtime) advanceEventGroups(int64) []simulation.DivisionFrames {
	if rt.Monsters == nil {
		return nil
	}
	var frames []simulation.Frame
	for _, seasonal := range rt.seasonalGroups() {
		if !seasonal.enabled {
			continue
		}
		active := rt.seasonalSpanActive(seasonal.span, rt.Now())
		known := seasonal.watch.known
		if known && active == seasonal.watch.active {
			continue
		}
		*seasonal.watch = eventGroupWatch{known: true, active: active}
		if !known {
			continue
		}
		for _, group := range seasonal.groups[:seasonal.count] {
			frames = append(frames, simulation.Frame{Opcode: eventGroupStateOpcode, Payload: EventGroupStatePayload(group, active)})
		}
	}
	if len(frames) == 0 {
		return nil
	}
	populations := rt.Monsters.OpenWorldPopulations()
	out := make([]simulation.DivisionFrames, 0, len(populations))
	for _, population := range populations {
		out = append(out, simulation.DivisionFrames{DivisionID: population.Division, Frames: frames})
	}
	return out
}
