/*
===========================================================================

wiring_caravanai_transport.go - caravan trader transport presentation

Port-only, not native (SRO_AI_CARAVANS, #3704 slice 3f). The caravan
peer leg (wiring_caravanai.go) presents a trader agent as a peer; its
transport COS is a second, differently-shaped entity beside it - the
band-2 spawn row players' transports get, walking a running goal
re-acked as the trail point behind the trader drifts. This file owns
that transport half: the row resolution from shipped data, the spawn
and goal batches, and the leg's transport ack state.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// caravanTransportAckDrift is how far a transport's trail goal may
// move before it is worth another 0xB738 (world units): the trader
// walks, the goal trails behind it, and the beast only needs a new
// goal once the old one is meaningfully stale.
const caravanTransportAckDrift = 4.0

/*
================
caravanPeerLeg.showTransportBatch

First sight of a trader's transport (slice 3f): the band-2 COS spawn
row players' transports get - creature, owner gid and owner name -
then the march frames when the trader is underway. A nil batch means
the creature data did not resolve and the beast stays invisible
rather than spawning broken. COS creatures resolve from the client's
own characterdata by RefObjID, so no reference frames ride ahead
(unlike the agent's item-based peer row).
================
*/
func (leg *caravanPeerLeg) showTransportBatch(viewer caravanPeerViewer, entity caravanai.AgentEntity) *simulation.DivisionFrames {
	row, ok := leg.transportRowFor(entity)
	if !ok {
		return nil
	}
	tgid := simulation.PlayerObjectID(entity.TransportCharacterID)
	row.Gid = tgid
	row.OwnerGid = simulation.PlayerObjectID(entity.CharacterID)
	row.OwnerName = entity.Name
	pose := caravanSpawnPose(entity.TransportPose, leg.heading[tgid])
	if entity.TransportMoving {
		if heading, ok := simulation.HeadingFromMovement(pose, caravanSpawnPose(entity.TransportTarget, 0)); ok {
			leg.heading[tgid] = heading
			pose.Angle = heading
		}
	}
	row.Position = wire.Position{
		RegionID: pose.RegionID,
		X:        float32(pose.X),
		Y:        float32(pose.Y),
		Z:        float32(pose.Z),
		Heading:  pose.Angle,
	}
	batch := &simulation.DivisionFrames{
		OnlyCharacterID: viewer.CharacterID,
		Frames: []simulation.Frame{{
			ScopeGID: tgid, ScopeVisible: true,
			Opcode:  wire.OpSingleObjectSpawn,
			Payload: wire.EncodeCosSpawnBand2(row),
		}},
	}
	if entity.TransportMoving {
		if move := leg.moveTransportBatch(viewer, entity, caravanPeerAck{}); move != nil {
			batch.Frames = append(batch.Frames, move.Frames...)
		}
	}
	return batch
}

/*
================
caravanPeerLeg.moveTransportBatch

While the trader marches, its transport holds a 0xB738 goal at the
trail point behind the trader. That point advances every tick, so
the goal is re-acked only once it has drifted
caravanTransportAckDrift from the last one the viewer holds; a stop
settles the beast where it stands, once. The movement channel byte
(running - transports move at their run speed) rides ahead of the
first goal, as the peer COS lane sends it.
================
*/
func (leg *caravanPeerLeg) moveTransportBatch(viewer caravanPeerViewer, entity caravanai.AgentEntity, ack caravanPeerAck) *simulation.DivisionFrames {
	tgid := simulation.PlayerObjectID(entity.TransportCharacterID)
	tacked := leg.tackedFor(viewer.SessionID)
	if entity.TransportPose.RegionID != ack.Pose.RegionID && ack.Pose != (caravanai.Pose{}) {
		// The ferry hop (or any region jump): settle at the new pose,
		// then let the marching branch below re-goal it.
		ack = caravanPeerAck{Pose: entity.TransportPose}
		tacked[tgid] = ack
	}
	if !entity.TransportMoving {
		tacked[tgid] = caravanPeerAck{Pose: entity.TransportPose}
		if !ack.Moving {
			return nil
		}
		return &simulation.DivisionFrames{
			OnlyCharacterID: viewer.CharacterID,
			Frames:          []simulation.Frame{caravanGoalFrame(tgid, entity.TransportPose, entity.TransportPose)},
		}
	}
	if ack.Moving && caravanPoseDistance(ack.Target, entity.TransportTarget) <= caravanTransportAckDrift {
		tacked[tgid] = caravanPeerAck{Target: entity.TransportTarget, Pose: entity.TransportPose, Moving: true}
		return nil
	}
	frames := make([]simulation.Frame, 0, 2)
	if !ack.Moving {
		frames = append(frames, simulation.Frame{
			Opcode:  wire.OpObjectStateRefresh,
			Payload: (wire.ObjectStateRefresh{Gid: tgid, StateType: wire.StateChannelMove, Value: wire.MoveStateRun}).Encode(),
		})
	}
	frames = append(frames, caravanGoalFrame(tgid, entity.TransportPose, entity.TransportTarget))
	if heading, ok := simulation.HeadingFromMovement(
		caravanSpawnPose(entity.TransportPose, 0), caravanSpawnPose(entity.TransportTarget, 0)); ok {
		leg.heading[tgid] = heading
	}
	tacked[tgid] = caravanPeerAck{Target: entity.TransportTarget, Pose: entity.TransportPose, Moving: true}
	return &simulation.DivisionFrames{OnlyCharacterID: viewer.CharacterID, Frames: frames}
}

/*
================
caravanPoseDistance

The world distance between two poses (worldgeom handles the
cross-region expansion).
================
*/
func caravanPoseDistance(a, b caravanai.Pose) float64 {
	return worldgeom.Distance(
		worldgeom.RegionXZ{RegionID: a.RegionID, X: float64(a.X), Z: float64(a.Z)},
		worldgeom.RegionXZ{RegionID: b.RegionID, X: float64(b.X), Z: float64(b.Z)},
	)
}

/*
================
caravanPeerLeg.transportRowFor

The resolved COS row of one agent's transport, built once per agent
from the shard's shipped data (the per-entity identity - gid, owner,
position - is stamped per viewer at show time). A miss is cached
exactly like an appearance miss: the resolver logged it.
================
*/
func (leg *caravanPeerLeg) transportRowFor(entity caravanai.AgentEntity) (wire.CosSpawnBand2, bool) {
	if row, ok := leg.transportRow[entity.CharacterID]; ok {
		if row == nil {
			return wire.CosSpawnBand2{}, false
		}
		return *row, true
	}
	row, ok := leg.resolveTransport(entity)
	if !ok {
		leg.transportRow[entity.CharacterID] = nil
		return wire.CosSpawnBand2{}, false
	}
	leg.transportRow[entity.CharacterID] = &row
	return row, true
}

/*
================
caravanPeerLeg.tackedFor
================
*/
func (leg *caravanPeerLeg) tackedFor(sessionID string) map[uint32]caravanPeerAck {
	tacked, ok := leg.tacked[sessionID]
	if !ok {
		tacked = make(map[uint32]caravanPeerAck)
		leg.tacked[sessionID] = tacked
	}
	return tacked
}

/*
================
caravanTransportRow

Resolves one trader's transport row from the shard's shipped data:
the creature reference by codename, with its tid band (band 2 for
the trade transports), speeds and scale - the same fields a player's
summon stamps (action/itemuse.go). The leg stamps gid, owner and
position per viewer. An unresolved codename logs once (the leg
caches the miss) and the beast never appears.
================
*/
func (game *gameplayPlane) caravanTransportRow(entity caravanai.AgentEntity) (wire.CosSpawnBand2, bool) {
	if game.deps == nil {
		return wire.CosSpawnBand2{}, false
	}
	source, ok := game.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return wire.CosSpawnBand2{}, false
	}
	ref, found := source.CharacterRefByCodename(entity.TransportCode)
	if !found || ref == nil || ref.RefObjID == 0 {
		log.WithFields(log.Fields{
			"agent":     entity.Name,
			"transport": entity.TransportCode,
		}).Warn("caravan-ai: agent transport data did not resolve; transport stays invisible")
		return wire.CosSpawnBand2{}, false
	}
	return wire.CosSpawnBand2{
		Band:     uint8(ref.TidWord >> 11),
		RefObjID: ref.RefObjID,
		Walk:     ref.WalkSpeed,
		Run:      ref.RunSpeed,
		Scale:    ref.Scale,
	}, true
}
