/*
===========================================================================

peer_motion.go - reliable destination admission for visible players

The visibility lane seeds a newly shown mover. This owner sends subsequent
paths once per viewer, before position samples, and shares that admission
record with visibility so a newly spawned mover does not restart twice.

===========================================================================
*/
package simulation

import "opensro.online/server/internal/game/item/wire"

/*
================
peerPath

Detached path identity; snapshot pointers can change without a new move.
================
*/
type peerPath struct {
	segment   MoveSegment
	goal      Spawn
	mode      uint8
	walk, run float32
}

/*
================
pathForPeer
================
*/
func pathForPeer(world WorldState) peerPath {
	path := peerPath{goal: world.Spawn, mode: world.MovementMode}
	path.walk, path.run = world.MovementSpeeds()
	if world.MoveSegment != nil {
		path.segment = *world.MoveSegment
	}
	return path
}

/*
================
rememberPeerPath
================
*/
func (state *divisionTickState) rememberPeerPath(viewer string, gid uint32, world WorldState) {
	if state.peerPaths == nil {
		state.peerPaths = make(map[string]map[uint32]peerPath)
	}
	if state.peerPaths[viewer] == nil {
		state.peerPaths[viewer] = make(map[uint32]peerPath)
	}
	state.peerPaths[viewer][gid] = pathForPeer(world)
}

/*
================
publishPeerPath

The client's 0x775CB0 only reseeds the source. The native 0xB738 destination is
required to keep the navigation and locomotion owners active between ticks.
================
*/
func (t *Ticker) publishPeerPath(state *divisionTickState, session SessionSnapshot, nowMs int64) {
	gid := PlayerObjectID(session.CharacterID)
	path := pathForPeer(session.World)
	var frames []Frame
	state.forEachPeerWatcher(gid, func(viewer string) {
		if previous, found := state.peerPaths[viewer][gid]; found && previous == path {
			return
		}
		if frames == nil {
			source := MovementSourceFromSpawn(session.World.LiveSpawnAt(nowMs))
			goal := session.World.Spawn
			frames = []Frame{
				{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelMove, Value: session.World.MovementMode}).Encode(), Current: session.MovementCurrent},
				{Opcode: OpMovementAck, Payload: BuildMovementAckPayload(gid, MovementRequest{Mode: MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, &source), Current: session.MovementCurrent},
			}
		}
		t.Push.PushToSession(viewer, frames)
		state.rememberPeerPath(viewer, gid, session.World)
	})
}

//============================================================================

/*
================
indexPeerWatchers

Inverts shownPeers for the session legs. Each moving peer used to scan
every viewer's shown set (sessions x viewers lookups per tick, the
quadratic that dominated the legs at a few hundred sessions); with the
inversion it visits only the viewers that show it. The slices are
reused across ticks: a gid's list is truncated, never freed, and the
gids a division ever showed bound the map.
================
*/
func (state *divisionTickState) indexPeerWatchers() {
	if state.peerWatchers == nil {
		state.peerWatchers = make(map[uint32][]string)
	}
	for gid, viewers := range state.peerWatchers {
		state.peerWatchers[gid] = viewers[:0]
	}
	for viewer, shown := range state.shownPeers {
		for gid, on := range shown {
			if on {
				state.peerWatchers[gid] = append(state.peerWatchers[gid], viewer)
			}
		}
	}
	state.peerWatchersValid = true
}

/*
================
dropPeerWatchers

Ends the inversion's validity: outside the session legs shownPeers may
change, and forEachPeerWatcher falls back to the full scan.
================
*/
func (state *divisionTickState) dropPeerWatchers() {
	state.peerWatchersValid = false
}

/*
================
forEachPeerWatcher

Calls fn for every viewer whose shown set holds gid. Each candidate is
rechecked against shownPeers, so a stale inversion can only skip work,
never invent a viewer; viewer order was a map walk before and carries
no meaning (each viewer's queue is its own).
================
*/
func (state *divisionTickState) forEachPeerWatcher(gid uint32, fn func(viewer string)) {
	if !state.peerWatchersValid {
		for viewer, shown := range state.shownPeers {
			if shown[gid] {
				fn(viewer)
			}
		}
		return
	}
	for _, viewer := range state.peerWatchers[gid] {
		if state.shownPeers[viewer][gid] {
			fn(viewer)
		}
	}
}
