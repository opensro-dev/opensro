package simulation

import (
	"bytes"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

func TestNpcTravelPublicationLifecycle(t *testing.T) {
	npc := NpcDef{ObjectID: 200001, RefObjID: 7524, AuthoredSpawn: true, Spawn: Spawn{RegionID: 25163, X: 958.04999, Y: -155.03999, Z: 98.970001}}
	gate := NpcDef{ObjectID: 252094, RefObjID: 2094, AuthoredSpawn: true, Spawn: npc.Spawn, Teleport: &TeleportGateBounds{Radius: 100, Height: 200}}
	roster := []NpcDef{npc, gate}
	s := SessionSnapshot{SessionID: "traveller:1", NpcsEnabled: true, PublishedObjects: []uint32{400001}, World: WorldState{Spawn: Spawn{RegionID: 25163, X: 1100, Z: 99}}}
	// Exhaustive publication transition table: desired x admitted, plus no
	// snapshot, disabled, retry after rejection, replacement and live motion.
	for _, tc := range []struct {
		name           string
		near, admitted bool
		opcode         uint16
	}{
		{"enter", true, false, wire.OpSingleObjectSpawn},
		{"stationary", true, true, 0},
		{"leave", false, true, wire.OpObjectDespawn},
		{"outside", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viewer := s
			if !tc.near {
				viewer.World.Spawn.X = 0
			}
			if tc.admitted {
				viewer.PublishedObjects = []uint32{npc.ObjectID, gate.ObjectID, 400001}
			}
			frames := NpcScopeFrames(roster, viewer, 100)
			if tc.opcode == 0 {
				if len(frames) != 0 {
					t.Fatal(frames)
				}
				return
			}
			if len(frames) != 2 {
				t.Fatalf("frames = %+v", frames)
			}
			for i, f := range frames {
				if f.Opcode != tc.opcode || f.ScopeGID != roster[i].ObjectID || f.ScopeVisible != tc.near {
					t.Fatal(f)
				}
				if tc.near {
					want := BuildNpcCreateRow(roster[i], Spawn{})
					if roster[i].Teleport == nil {
						want = append(want, 0)
					}
					if !bytes.Equal(want, f.Payload) {
						t.Fatal("single spawn body/appearance")
					}
				}
			}
		})
	}
	first := NpcScopeFrames(roster, s, 100)
	if len(first) != len(NpcScopeFrames(roster, s, 200)) {
		t.Fatal("rejected publication was cached as admitted")
	}
	s.PublishedObjects = nil
	if len(NpcScopeFrames(roster, s, 100)) != 0 {
		t.Fatal("missing scene authority")
	}
	s.PublishedObjects = []uint32{npc.ObjectID, gate.ObjectID}
	s.NpcsEnabled = false
	if len(NpcScopeFrames(roster, s, 100)) != 2 {
		t.Fatal("disable must withdraw admitted NPCs")
	}
	s.NpcsEnabled = true
	s.World.Spawn.X = 0
	s.World.MoveSegment = &MoveSegment{From: Spawn{RegionID: 25163, X: 1100, Z: 99}, StartedAtMs: 100, ArrivesAtMs: 1100}
	if len(NpcScopeFrames(roster, s, 100)) != 0 {
		t.Fatal("visibility sampled goal instead of live viewer")
	}
	if len(NpcScopeFrames(roster, s, 1100)) != 2 {
		t.Fatal("arrival must remove departed scope")
	}
}

func TestEveryAuthoredNpcHasTravelPublication(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	roster, err := AppendTeleportGates(dir, LoadNpcWorldRoster(dir))
	if err != nil || len(roster) < 150 {
		t.Fatalf("published roster unavailable: %d %v", len(roster), err)
	}
	for _, npc := range roster {
		s := SessionSnapshot{NpcsEnabled: true, PublishedObjects: []uint32{}, World: WorldState{Spawn: npc.Spawn}}
		f := NpcScopeFrames([]NpcDef{npc}, s, 1)
		if len(f) != 1 || f[0].ScopeGID != npc.ObjectID || f[0].Opcode != wire.OpSingleObjectSpawn {
			t.Fatalf("missing %s", npc.Codename)
		}
		s.PublishedObjects = []uint32{npc.ObjectID}
		if len(NpcScopeFrames([]NpcDef{npc}, s, 2)) != 0 {
			t.Fatalf("duplicate %s", npc.Codename)
		}
	}
	t.Logf("audited %d authored NPC/gate placements", len(roster))
}

func TestTickerPublishesNpcAfterTravel(t *testing.T) {
	s := SessionSnapshot{SessionID: "traveller:1", DivisionID: "A", NpcsEnabled: true, PublishedObjects: []uint32{}, World: WorldState{Spawn: Spawn{RegionID: 25163, X: 1000, Z: 99}}}
	source, push := &fakeSource{sessions: []SessionSnapshot{s}}, &fakePusher{}
	ticker := NewTicker(source, push)
	ticker.Roster = []NpcDef{{ObjectID: 200001, RefObjID: 7524, AuthoredSpawn: true, Spawn: s.World.Spawn}}
	ticker.RunTick(100)
	if len(peerFramesTo(push, s.SessionID, wire.OpSingleObjectSpawn)) != 1 {
		t.Fatal("NPC scope owner not wired into production ticker")
	}
}
