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
