/*
===========================================================================

tombgate.go - the Gate of the Sarin Tribe opens on its schedule (#4135)

The Qin-Shi tomb's floor-4 gate (the STORE_TQ_SNAKEGATE building, its
link down to floor 5) is a schedule job on the v1.188 game server,
SCHEDULE_DEF_CHINS_TOMB. The job's start links the gate's teleport and
tells every world notice kind 0x0C10; its end clears the link and
sends 0x0C11. A cleared link is a link the gate does not have, so a
traveller outside the window is refused as for a missing link.
The v1.696 client shows the two kinds as UIIT_TQ_F4_TELEPORT_OPEN and
_CLOSE ("Gate of Sarin Tribe has opened/closed", in v1.150's own
textuisystem) on the banner and in chat (v1.696 client 0x300C cases
0xDA07B8 and 0xDA082B, a client-labelled citation); this port sends
their low bytes on v1.150's 0x3058 event notice, as the Roc's
countdowns do (roc.go), and the browser client decodes them.

The window is the owner's call (2026-11-23), not a native row: the only
_Schedule rows on file for this job are the iSRO-R shard's (define 9:
four daily 6-hour windows from 00:00:01, back to back, so the gate
would never close for long), the vSRO shards carry none, and the native
text says only that the gate "opens for a set time at designated
intervals". The owner chose ten minutes every hour, from one second
past the hour as those rows start.

The gate rides the shipped tomb's switch (SRO_QINSHI_TOMB): with the
tomb off there is no link to schedule.

===========================================================================
*/
package action

import (
	"sync"
	"time"

	"opensro.online/server/internal/game/world/schedule"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// tombGateSource is the gate building whose link the job sets.
	tombGateSource = "STORE_TQ_SNAKEGATE"

	// The 0x3058 kinds: the low bytes of the later 0x0C10 and 0x0C11.
	tombGateKindOpened byte = 0x10
	tombGateKindClosed byte = 0x11

	// The owner's window (2026-11-23): ten minutes, every hour, from
	// one second past it (the iSRO-R rows' start second).
	tombGateStartSecond = 1
	tombGateDuration    = 10 * time.Minute
)

/*
================
tombGateWindow

The gate's schedule row.
================
*/
func tombGateWindow() schedule.Window {
	return schedule.Window{Every: schedule.Hourly, StartSecond: tombGateStartSecond, Duration: tombGateDuration}
}

/*
================
tombGateRuntime

The gate's last observed state, so the tick announces each edge once.
================
*/
type tombGateRuntime struct {
	mu    sync.Mutex
	loc   *time.Location
	known bool
	open  bool
}

/*
================
ConfigureTombGate

Turns the schedule on with the shipped tomb (SRO_QINSHI_TOMB). Off
leaves rt.tombGate nil: the gate's link, where the build carries it,
is never refused here and nothing is announced.
================
*/
func (rt *Runtime) ConfigureTombGate(enabled bool) {
	if !enabled {
		rt.tombGate = nil
		return
	}
	rt.tombGate = &tombGateRuntime{loc: time.Local}
}

/*
================
tombGateShut

True when source is the Sarin gate and its window is closed: the
native job has cleared the gate's link.
================
*/
func (rt *Runtime) tombGateShut(source string) bool {
	g := rt.tombGate
	if g == nil || source != tombGateSource {
		return false
	}
	_, _, in := tombGateWindow().Current(rt.Now(), g.loc)
	return !in
}

/*
================
advanceTombGate

The tick step: at each window edge every division hears the gate open
or close. A server that boots inside a window announces it, as the
schedule executor starts a job whose row is current; one that boots
outside has nothing to say.
================
*/
func (rt *Runtime) advanceTombGate(int64) []simulation.DivisionFrames {
	g := rt.tombGate
	if g == nil || rt.Monsters == nil {
		return nil
	}
	_, _, in := tombGateWindow().Current(rt.Now(), g.loc)
	g.mu.Lock()
	first := !g.known
	changed := first || g.open != in
	g.known, g.open = true, in
	g.mu.Unlock()
	if !changed || first && !in {
		return nil
	}
	kind := tombGateKindClosed
	if in {
		kind = tombGateKindOpened
	}
	var out []simulation.DivisionFrames
	for _, population := range rt.Monsters.OpenWorldPopulations() {
		out = append(out, simulation.DivisionFrames{DivisionID: population.Division,
			Frames: []simulation.Frame{{Opcode: opEventNotice, Payload: []byte{kind}}}})
	}
	return out
}
