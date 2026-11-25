/*
===========================================================================

wiring_caravanai_peer_test.go - the caravan peer-presentation leg

Port-only (#3704 slice 3a): an agent enters a viewer's world as a peer
spawn, marches on 0xB738 goals, and leaves with a despawn, all through
per-viewer batches. These tests pin that frame contract against fake
appearance/reference ports; the shipped-data resolution of the
appearance itself is covered against the real textdata elsewhere.

===========================================================================
*/
package main

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const caravanPeerRefOpcode = 0x7E57

func caravanPeerTestLeg() (*caravanPeerLeg, *[][]uint32) {
	var refCalls [][]uint32
	leg := newCaravanPeerLeg(
		func(entity caravanai.AgentEntity) (simulation.PeerAppearance, bool) {
			return simulation.PeerAppearance{
				RefObjID:  1907,
				Name:      entity.Name,
				JobType:   entity.JobType,
				Equipment: []wire.PlayerEquipItem{{RefObjID: 111}, {RefObjID: 222}},
			}, true
		},
		func(entity caravanai.AgentEntity) (wire.CosSpawnBand2, bool) {
			return wire.CosSpawnBand2{Band: 2, RefObjID: 3911, Walk: 20, Run: 70, Scale: 1}, true
		},
		func(ids []uint32) []simulation.Frame {
			refCalls = append(refCalls, append([]uint32(nil), ids...))
			return []simulation.Frame{{Opcode: caravanPeerRefOpcode}}
		},
	)
	return leg, &refCalls
}

func peerTestViewer() caravanPeerViewer {
	return caravanPeerViewer{
		SessionID:   "session-1",
		CharacterID: 42,
		Pose:        simulation.Spawn{RegionID: 25000, X: 100, Z: 100},
	}
}

func caravanPeerAgent(moving bool, target caravanai.Pose) caravanai.AgentEntity {
	return caravanai.AgentEntity{
		CharacterID: caravanai.AgentCharacterIDBase,
		Name:        "[AI]Trader_Jin",
		JobType:     1,
		ModelCode:   "CHAR_CH_MAN_MERCHANT",
		SuitCode:    "ITEM_CH_M_TRADE_01",
		WeaponCode:  "ITEM_CH_SPEAR_07_C_RARE",
		Pose:        caravanai.Pose{RegionID: 25000, X: 110, Y: 3, Z: 100},
		Target:      target,
		Moving:      moving,
	}
}

func opcodesOf(batch simulation.DivisionFrames) []uint16 {
	opcodes := make([]uint16, 0, len(batch.Frames))
	for _, frame := range batch.Frames {
		opcodes = append(opcodes, frame.Opcode)
	}
	return opcodes
}

/*
================
TestCaravanPeerShowMarchAndDespawn

First sight sends the item references ahead of the spawn row and,
for a marching agent, the movement channel and goal; a changed goal
re-acks alone; stopping settles with one last goal; leaving the
interest neighbourhood despawns.
================
*/
func TestCaravanPeerShowMarchAndDespawn(t *testing.T) {
	leg, refCalls := caravanPeerTestLeg()
	viewer := peerTestViewer()
	targetA := caravanai.Pose{RegionID: 25000, X: 400, Y: 3, Z: 100}
	targetB := caravanai.Pose{RegionID: 25000, X: 700, Y: 3, Z: 100}

	out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{caravanPeerAgent(true, targetA)})
	if len(out) != 1 {
		t.Fatalf("first sight batches = %d, want 1", len(out))
	}
	batch := out[0]
	if batch.OnlyCharacterID != viewer.CharacterID {
		t.Fatalf("batch addressed to character %d, want the viewer %d", batch.OnlyCharacterID, viewer.CharacterID)
	}
	want := []uint16{caravanPeerRefOpcode, wire.OpSingleObjectSpawn, wire.OpObjectStateRefresh, simulation.OpMovementAck}
	if got := opcodesOf(batch); len(got) != len(want) {
		t.Fatalf("first sight opcodes = %x, want %x", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("first sight opcodes = %x, want %x", got, want)
			}
		}
	}
	if len(*refCalls) != 1 || len((*refCalls)[0]) != 2 {
		t.Fatalf("reference calls = %v, want one call with the suit and weapon ids", *refCalls)
	}
	if !batch.Frames[1].ScopeVisible || batch.Frames[1].ScopeGID != simulation.PlayerObjectID(caravanai.AgentCharacterIDBase) {
		t.Fatalf("spawn frame scope = gid %d visible %v, want the agent gid in scope",
			batch.Frames[1].ScopeGID, batch.Frames[1].ScopeVisible)
	}

	// Same goal: silence.
	if out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{caravanPeerAgent(true, targetA)}); len(out) != 0 {
		t.Fatalf("unchanged goal produced %d batches, want none", len(out))
	}
	// New goal: one bare movement ack, no channel byte, no references.
	out = leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{caravanPeerAgent(true, targetB)})
	if len(out) != 1 || len(out[0].Frames) != 1 || out[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("goal change = %+v, want one bare 0xB738", out)
	}
	// Stopping settles on the agent's own pose once.
	settled := caravanPeerAgent(false, caravanai.Pose{})
	settled.Target = settled.Pose
	out = leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{settled})
	if len(out) != 1 || len(out[0].Frames) != 1 || out[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("stop = %+v, want one settling 0xB738", out)
	}
	if out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{settled}); len(out) != 0 {
		t.Fatalf("settled agent produced %d batches, want none", len(out))
	}
	// Leaving the neighbourhood despawns exactly once.
	far := peerTestViewer()
	far.Pose = simulation.Spawn{RegionID: 23687, X: 100, Z: 100}
	out = leg.frames("div", []caravanPeerViewer{far}, []caravanai.AgentEntity{settled})
	if len(out) != 1 || len(out[0].Frames) != 1 || out[0].Frames[0].Opcode != wire.OpObjectDespawn {
		t.Fatalf("departure = %+v, want one 0x36AB despawn", out)
	}
	if len(*refCalls) != 1 {
		t.Fatalf("reference calls = %d, want the whole life to cost one", len(*refCalls))
	}
}

/*
================
TestCaravanPeerDepartedViewerIsForgotten

A viewer whose session leaves is forgotten without a despawn (there
is nobody to send it to), and when the same viewer returns it is
shown the agent again, references included.
================
*/
func TestCaravanPeerDepartedViewerIsForgotten(t *testing.T) {
	leg, refCalls := caravanPeerTestLeg()
	viewer := peerTestViewer()
	agent := caravanPeerAgent(false, caravanai.Pose{})
	agent.Target = agent.Pose

	if out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{agent}); len(out) != 1 {
		t.Fatalf("first sight batches = %d, want 1", len(out))
	}
	if out := leg.frames("div", nil, []caravanai.AgentEntity{agent}); len(out) != 0 {
		t.Fatalf("departed viewer produced %d batches, want none", len(out))
	}
	out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{agent})
	if len(out) != 1 || len(out[0].Frames) == 0 || out[0].Frames[0].Opcode != caravanPeerRefOpcode {
		t.Fatalf("returning viewer = %+v, want references then spawn again", out)
	}
	if len(*refCalls) != 2 {
		t.Fatalf("reference calls = %d, want 2 (one per session life)", len(*refCalls))
	}
}

func caravanPeerTrader(moving bool, target caravanai.Pose) caravanai.AgentEntity {
	entity := caravanPeerAgent(moving, target)
	entity.TransportCode = "COS_T_CAMEL1"
	entity.TransportCharacterID = caravanai.AgentTransportIDBase
	entity.TransportPose = caravanai.Pose{RegionID: entity.Pose.RegionID, X: entity.Pose.X - 8, Y: entity.Pose.Y, Z: entity.Pose.Z}
	entity.TransportTarget = entity.TransportPose
	entity.TransportMoving = moving
	return entity
}

/*
================
TestCaravanPeerTraderTransportFollows

Slice 3f: a trader's transport spawns beside it as a band-2 COS row
owned by the agent, runs on goals re-acked only as the trail point
drifts, settles when the trader stops, and despawns with it.
================
*/
func TestCaravanPeerTraderTransportFollows(t *testing.T) {
	leg, _ := caravanPeerTestLeg()
	viewer := peerTestViewer()
	agentGID := simulation.PlayerObjectID(caravanai.AgentCharacterIDBase)
	transportGID := simulation.PlayerObjectID(caravanai.AgentTransportIDBase)
	if transportGID == agentGID {
		t.Fatal("transport gid collides with the agent gid")
	}
	target := caravanai.Pose{RegionID: 25000, X: 400, Y: 3, Z: 100}
	trader := caravanPeerTrader(true, target)

	out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{trader})
	if len(out) != 2 {
		t.Fatalf("first sight batches = %d, want the agent and its transport", len(out))
	}
	var spawn *simulation.Frame
	for i := range out[1].Frames {
		if out[1].Frames[i].Opcode == wire.OpSingleObjectSpawn {
			spawn = &out[1].Frames[i]
		}
	}
	if spawn == nil {
		t.Fatalf("transport batch opcodes = %x, want a COS spawn", opcodesOf(out[1]))
	}
	if spawn.ScopeGID != transportGID || binary.LittleEndian.Uint32(spawn.Payload[0:4]) != 3911 ||
		binary.LittleEndian.Uint32(spawn.Payload[4:8]) != transportGID {
		t.Fatalf("transport spawn scope/ref/gid = %d/%d/%d, want %d/3911/%d",
			spawn.ScopeGID, binary.LittleEndian.Uint32(spawn.Payload[0:4]),
			binary.LittleEndian.Uint32(spawn.Payload[4:8]), transportGID, transportGID)
	}
	owner := make([]byte, 4)
	binary.LittleEndian.PutUint32(owner, agentGID)
	found := false
	for i := 0; i+4 <= len(spawn.Payload); i++ {
		if string(spawn.Payload[i:i+4]) == string(owner) {
			found = true
		}
	}
	if !found {
		t.Fatal("transport spawn does not name the agent as its owner")
	}
	want := []uint16{wire.OpSingleObjectSpawn, wire.OpObjectStateRefresh, simulation.OpMovementAck}
	if got := opcodesOf(out[1]); len(got) != len(want) {
		t.Fatalf("transport first sight opcodes = %x, want %x", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("transport first sight opcodes = %x, want %x", got, want)
			}
		}
	}

	// A trail goal that barely drifted is not worth another ack.
	drifted := trader
	drifted.TransportTarget.X += 2
	drifted.TransportPose.X += 2
	if out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{drifted}); len(out) != 0 {
		t.Fatalf("small trail drift produced %d batches, want none", len(out))
	}
	// A real drift re-acks once, bare.
	drifted.TransportTarget.X += 8
	drifted.TransportPose.X += 8
	out = leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{drifted})
	if len(out) != 1 || len(out[0].Frames) != 1 || out[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("trail drift = %+v, want one bare 0xB738", out)
	}
	// Stopping settles the beast where it stands.
	stopped := drifted
	stopped.Moving = false
	stopped.TransportMoving = false
	stopped.Target = stopped.Pose
	stopped.TransportTarget = stopped.TransportPose
	out = leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{stopped})
	settles := 0
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode == simulation.OpMovementAck {
				settles++
			}
		}
	}
	if settles != 2 {
		t.Fatalf("stop produced %d settling goals, want the agent's and the transport's", settles)
	}
	// Leaving sight despawns the pair.
	far := peerTestViewer()
	far.Pose = simulation.Spawn{RegionID: 23687, X: 100, Z: 100}
	out = leg.frames("div", []caravanPeerViewer{far}, []caravanai.AgentEntity{stopped})
	despawned := map[uint32]bool{}
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpObjectDespawn {
				despawned[frame.ScopeGID] = true
			}
		}
	}
	if !despawned[agentGID] || !despawned[transportGID] || len(despawned) != 2 {
		t.Fatalf("departure despawned %v, want the agent and its transport", despawned)
	}
}

/*
================
TestCaravanPeerUnresolvedAppearanceStaysInvisible

An agent whose shipped appearance data does not resolve never
spawns, is retried zero times per tick after the first miss, and
does not block other agents from appearing.
================
*/
func TestCaravanPeerUnresolvedAppearanceStaysInvisible(t *testing.T) {
	resolves := 0
	leg := newCaravanPeerLeg(
		func(entity caravanai.AgentEntity) (simulation.PeerAppearance, bool) {
			resolves++
			if entity.Name == "[AI]Thief_Viper" {
				return simulation.PeerAppearance{}, false
			}
			return simulation.PeerAppearance{RefObjID: 1907, Name: entity.Name}, true
		},
		func(entity caravanai.AgentEntity) (wire.CosSpawnBand2, bool) {
			return wire.CosSpawnBand2{}, false
		},
		func(ids []uint32) []simulation.Frame { return nil },
	)
	viewer := peerTestViewer()
	viper := caravanPeerAgent(false, caravanai.Pose{})
	viper.Name = "[AI]Thief_Viper"
	viper.CharacterID = caravanai.AgentCharacterIDBase + 3
	viper.Target = viper.Pose
	jin := caravanPeerAgent(false, caravanai.Pose{})
	jin.Target = jin.Pose

	out := leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{viper, jin})
	spawns := 0
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpSingleObjectSpawn {
				spawns++
			}
		}
	}
	if spawns != 1 {
		t.Fatalf("spawns = %d, want only the resolvable agent", spawns)
	}
	leg.frames("div", []caravanPeerViewer{viewer}, []caravanai.AgentEntity{viper, jin})
	if resolves != 2 {
		t.Fatalf("appearance resolutions = %d, want 2 (one per agent, misses cached)", resolves)
	}
}
