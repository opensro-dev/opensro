/*
===========================================================================

peercos.go - first sight and lifetime visibility for summoned characters

Publish detached pose, life and abnormal state together when an observer
first sees a summon. Existing observers receive the live action publications.

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/item/wire"

// PeerCOS is a detached publication from the pet simulation owner. It contains
// no container, inventory, experience or other owner-private record fields.
/*
================
PeerCOS
================
*/
type PeerCOS struct {
	Mounted bool
	// Fresh: the pet was summoned a moment ago; its first sight is the
	// summon itself (spawn sub-state 1).
	Fresh            bool
	NativeBodyStatus uint8
	LifeState        uint8
	AbnormalVitals   []byte
	Row              wire.CosSpawnBand2
	World            WorldState
	Revision         uint64
	Session          uint64
	Generation       uint64
}

/*
================
shownCOS
================
*/
type shownCOS struct {
	mounted    bool
	ref        uint32
	revision   uint64
	session    uint64
	generation uint64
}

/*
================
frames
================
*/
func (p PeerCOS) frames(nowMs int64, spawn bool) []Frame {
	pose := p.World.LiveSpawnAt(nowMs)
	position := wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle}
	var frames []Frame
	if spawn {
		row := p.Row
		row.BodyStatus = p.NativeBodyStatus
		row.Position = position
		if p.Fresh {
			row.State = 1
		}
		frames = append(frames, Frame{ScopeGID: row.Gid, ScopeVisible: true, Opcode: wire.OpSingleObjectSpawn, Payload: wire.EncodeCosSpawnBand2(row)})
		if len(p.AbnormalVitals) != 0 {
			frames = append(frames, Frame{Opcode: OpVitalsUpdate, Payload: append([]byte(nil), p.AbnormalVitals...)})
		}
		if p.LifeState == wire.LifeStateDead {
			frames = append(frames, Frame{Opcode: OpVitalsUpdate, Payload: HPRefreshPayload(row.Gid, VitalsSourceCombatDamage, 0)},
				Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: row.Gid, StateType: wire.StateChannelLife, Value: wire.LifeStateDead}).Encode()})
		}
		if p.NativeBodyStatus != 0 {
			frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: row.Gid, StateType: wire.StateChannelBody, Value: p.NativeBodyStatus}).Encode()})
		}
	}
	if p.Mounted {
		frames = append(frames, Frame{Opcode: wire.OpCosRideState, Payload: wire.EncodeCosRideState(p.Row.OwnerGid, true, p.Row.Gid)})
	}
	if p.World.MoveSegment.Valid() && nowMs < p.World.MoveSegment.ArrivesAtMs {
		source := MovementSourceFromSpawn(pose)
		goal := p.World.Spawn
		state := wire.ObjectStateRefresh{Gid: p.Row.Gid, StateType: wire.StateChannelMove, Value: RunMode}
		frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: state.Encode()},
			Frame{Opcode: OpMovementAck, Payload: BuildMovementAckPayload(p.Row.Gid, MovementRequest{Mode: MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, &source)})
	} else if !spawn {
		correction := wire.ObjectSourceCorrection{Gid: p.Row.Gid, Position: position}
		frames = append(frames, Frame{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode()})
	}
	return frames
}

/*
================
runPeerCOSVisibility
================
*/
func (t *Ticker) runPeerCOSVisibility(state *divisionTickState, nowMs int64, sessions []SessionSnapshot, live map[string]bool) {
	var actors []SessionSnapshot
	for _, session := range sessions {
		pets := session.Companions
		if pets == nil && session.COS != nil {
			pets = []*PeerCOS{session.COS}
		}
		for _, pet := range pets {
			if pet == nil {
				continue
			}
			actor := session
			actor.COS = pet
			actor.Companions = nil
			actors = append(actors, actor)
		}
	}
	index := buildPeerInterestIndex(actors, nowMs, true)
	var candidates []int
	present := make(map[uint32]bool)
	if state.shownCOS == nil {
		state.shownCOS = make(map[string]map[uint32]shownCOS)
	}
	for _, viewer := range sessions {
		shown := state.shownCOS[viewer.SessionID]
		if shown == nil {
			shown = make(map[uint32]shownCOS)
			state.shownCOS[viewer.SessionID] = shown
		}
		clear(present)
		candidates = index.candidates(&viewer, viewer.World.LiveSpawnAt(nowMs), candidates)
		for _, ownerIndex := range candidates {
			owner := &actors[ownerIndex]
			p := owner.COS
			if owner.SessionID == viewer.SessionID {
				continue
			}
			gid := p.Row.Gid
			// A ride names its rider (777F60 has no absent-actor arm), so a
			// mounted vehicle is shown only to viewers the player lane has
			// shown the rider to this tick. The two lanes read separate world
			// snapshots and the player lane skips unchanged ticks, so the
			// vehicle can briefly land in a cell the rider has not reached.
			if p.Mounted && !state.shownPeers[viewer.SessionID][p.Row.OwnerGid] {
				continue
			}
			present[gid] = true
			old, exists := shown[gid]
			sameLife := exists && old.ref == p.Row.RefObjID && old.session == p.Session && old.generation == p.Generation
			if sameLife && old.revision == p.Revision && old.mounted == p.Mounted {
				continue
			}
			if exists && !sameLife {
				t.Push.PushToSession(viewer.SessionID, []Frame{{ScopeGID: gid, Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: gid}).Encode()}})
			}
			t.Push.PushToSession(viewer.SessionID, p.frames(nowMs, !sameLife))
			shown[gid] = shownCOS{mounted: p.Mounted, ref: p.Row.RefObjID, revision: p.Revision, session: p.Session, generation: p.Generation}
		}
		for gid := range shown {
			if !present[gid] {
				t.Push.PushToSession(viewer.SessionID, []Frame{{ScopeGID: gid, Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: gid}).Encode()}})
				delete(shown, gid)
			}
		}
	}
	for id := range state.shownCOS {
		if !live[id] {
			delete(state.shownCOS, id)
		}
	}
}

/*
================
SpawnFrames

Owner-side relocation uses the same complete spawn as first peer visibility.
================
*/
func (p PeerCOS) SpawnFrames(nowMs int64) []Frame { return p.frames(nowMs, true) }
