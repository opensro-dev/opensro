/*
===========================================================================

position_resync.go - the 5 s position resync for one character

Native (the default, under both SRO_POSITION_STREAM settings): every
living character carries a timer of 5 s. When it fires, the character's
position goes out as one 0x3028 if the character stands in another
region than the last broadcast position or more than 10 from it, or is
moving and more than 100 from it; the broadcast position becomes the
new reference. A player mounted on a COS is exempt (the COS sync owns
its rider's position), as is a character that is not alive. The
original was read for this change (#3723): the interval is a stored
5.0 s beside its 4.0 and 0.3 siblings, both thresholds are stored
constants, and the packet is the 0x30E3 layout (region, x/y/z,
heading, gid last) broadcast to the character's viewers and to the
character itself.

This is the bound #3721 left open. The client walks a peer from one
0xB738 by itself, so a peer its own collision stopped at a contact
point, or a long walk's accumulated drift, is corrected at the next
5 s tick that finds the mover past a threshold - never sooner. The
client applies the packet as a source reseed (progress while travel
remains active), so a walking peer re-anchors and walks on.

===========================================================================
*/
package simulation

import (
	"math"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
)

// ResyncIntervalMs is the native per-character resync period.
const ResyncIntervalMs = 5000

// ResyncStandingDistance bounds a settled character's drift from the
// last broadcast position before the resync fires.
const ResyncStandingDistance = 10.0

// ResyncMovingDistance bounds a moving character's drift: a walking
// replica follows the same path, so only a large divergence is worth
// the snap.
const ResyncMovingDistance = 100.0

/*
================
peerResyncState

One character's resync book: the position the last resync carried (or
the position at first sight) and when the next check falls due.
================
*/
type peerResyncState struct {
	last  Spawn
	dueMs int64
}

/*
================
Ticker.runPeerResync

The 5 s resync for one session's character. The book is seeded at
first sight; each due tick re-arms before it decides, so a gated or
settled character is reconsidered 5 s later, never on the next tick,
and only a sent resync moves the reference position.
================
*/
func (t *Ticker) runPeerResync(state *divisionTickState, session SessionSnapshot, nowMs int64) {
	gid := PlayerObjectID(session.CharacterID)
	live := session.World.LiveSpawnAt(nowMs)
	if state.peerResync == nil {
		state.peerResync = make(map[uint32]peerResyncState)
	}
	book, seen := state.peerResync[gid]
	if !seen {
		state.peerResync[gid] = peerResyncState{last: live, dueMs: nowMs + ResyncIntervalMs}
		return
	}
	if nowMs < book.dueMs {
		return
	}
	book.dueMs = nowMs + ResyncIntervalMs
	state.peerResync[gid] = book

	// The native gates: not alive (CombatEligible is LIFE == 1), or
	// mounted on a COS whose own sync owns the rider's position.
	if !session.CombatEligible || session.mountedOnCOS() {
		return
	}
	fire := false
	if session.World.MovingAt(nowMs) {
		fire = resyncWorldDistance(live, book.last) > ResyncMovingDistance
	} else {
		fire = live.RegionID != book.last.RegionID ||
			resyncLocalDistance(live, book.last) > ResyncStandingDistance
	}
	if !fire {
		return
	}
	frame := Frame{
		Opcode: wire.OpObjectPositionUpdate,
		Payload: (wire.ObjectPositionUpdate{
			Position: wire.Position{
				RegionID: live.RegionID,
				X:        float32(live.X),
				Y:        float32(live.Y),
				Z:        float32(live.Z),
				Heading:  live.Angle,
			},
			Gid: gid,
		}).Encode(),
		Current: session.MovementCurrent,
	}
	frames := []Frame{frame}
	t.pushPeerMovement(state, gid, frames)
	t.Push.PushToSession(session.SessionID, frames)
	book.last = live
	state.peerResync[gid] = book
}

/*
================
resyncLocalDistance

The straight 3D distance between two spawns read in one region frame
(the standing test, applied once the regions are known to match).
================
*/
func resyncLocalDistance(a, b Spawn) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

/*
================
resyncWorldDistance

The 3D distance across region frames: the planar part rides the
sector grid (worldgeom.Distance), the height is frame-independent.
================
*/
func resyncWorldDistance(a, b Spawn) float64 {
	planar := worldgeom.Distance(
		worldgeom.RegionXZ{RegionID: a.RegionID, X: a.X, Z: a.Z},
		worldgeom.RegionXZ{RegionID: b.RegionID, X: b.X, Z: b.Z},
	)
	dy := a.Y - b.Y
	return math.Sqrt(planar*planar + dy*dy)
}
