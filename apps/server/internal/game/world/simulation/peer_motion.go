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

Retail 0x775CB0 only reseeds the source. The native 0xB738 destination is
required to keep the navigation and locomotion owners active between ticks.
================
*/
func (t *Ticker) publishPeerPath(state *divisionTickState, session SessionSnapshot, nowMs int64) {
	gid := PlayerObjectID(session.CharacterID)
	path := pathForPeer(session.World)
	var frames []Frame
	for viewer, shown := range state.shownPeers {
		if !shown[gid] {
			continue
		}
		if previous, found := state.peerPaths[viewer][gid]; found && previous == path {
			continue
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
	}
}
