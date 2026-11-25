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
It is the one packet the original sends a viewer per accepted move
(position_stream.go); the 0x3122 move state rides with it only in the
port-only stream, since the original sends that state on a run/walk
toggle, which the movement lane publishes itself.
================
*/
func (t *Ticker) publishPeerPath(state *divisionTickState, session SessionSnapshot, nowMs int64) {
	gid := PlayerObjectID(session.CharacterID)
	path := pathForPeer(session.World)
	// Every viewer that holds this mover already holds this path: each
	// one got it from the fan-out below when it last changed, or with
	// the spawn that showed it the mover (peer_visibility.go records the
	// path there). Only a changed path needs the walk over its viewers.
	if previous, found := state.moverPaths[gid]; found && previous == path {
		return
	}
	if state.moverPaths == nil {
		state.moverPaths = make(map[uint32]peerPath)
	}
	state.moverPaths[gid] = path
	var frames []Frame
	state.forEachPeerWatcher(gid, func(viewer string) {
		if previous, found := state.peerPaths[viewer][gid]; found && previous == path {
			return
		}
		if frames == nil {
			source := MovementSourceFromSpawn(session.World.LiveSpawnAt(nowMs))
			goal := session.World.Spawn
			ack := Frame{Opcode: OpMovementAck, Payload: BuildMovementAckPayload(gid, MovementRequest{Mode: MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, &source), Current: session.MovementCurrent}
			frames = []Frame{ack}
			if t.PositionStream {
				frames = []Frame{
					{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelMove, Value: session.World.MovementMode}).Encode(), Current: session.MovementCurrent},
					ack,
				}
			}
		}
		t.Push.PushToSession(viewer, frames)
		state.rememberPeerPath(viewer, gid, session.World)
	})
}

//============================================================================

/*
================
forEachPeerWatcher

Calls fn for every viewer whose shown set holds gid, from the inversion
the visibility pass keeps (peer_visibility.go). Each viewer is rechecked
against shownPeers, so the inversion can only skip work, never invent a
viewer; viewer order carries no meaning (each viewer's queue is its own).
fn must not change the shown sets.
================
*/
func (state *divisionTickState) forEachPeerWatcher(gid uint32, fn func(viewer string)) {
	for viewer := range state.peerWatchers[gid] {
		if state.shownPeers[viewer][gid] {
			fn(viewer)
		}
	}
}

/*
================
divisionTickState.dropDepartedMovers

Forgets the last fanned-out path of every player no longer in the
division (moverPaths, publishPeerPath).
================
*/
func (state *divisionTickState) dropDepartedMovers(sessions []SessionSnapshot) {
	if len(state.moverPaths) == 0 {
		return
	}
	if state.liveGids == nil {
		state.liveGids = make(map[uint32]bool)
	}
	clear(state.liveGids)
	for i := range sessions {
		state.liveGids[PlayerObjectID(sessions[i].CharacterID)] = true
	}
	for gid := range state.moverPaths {
		if !state.liveGids[gid] {
			delete(state.moverPaths, gid)
		}
	}
}
