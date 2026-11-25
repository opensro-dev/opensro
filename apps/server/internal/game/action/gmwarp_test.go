package action

import (
	"bytes"
	"encoding/binary"
	"math"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

func warpFrame() []byte {
	p := make([]byte, 17)
	p[0] = 16
	binary.LittleEndian.PutUint16(p[1:], 25416)
	for i, v := range []float32{703, 42, 1575} {
		binary.LittleEndian.PutUint32(p[3+i*4:], math.Float32bits(v))
	}
	binary.LittleEndian.PutUint16(p[15:], 12345)
	return p
}

func TestGMWarpAuthorityAndReentry(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	rt, clock := newTestRuntime(c, testItems())
	installMidMove(rt, c, clock)
	deps := rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(_ *enterworld.Character, r uint16) bool { return r == 25416 }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	var sent, peers []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, f []wire.Frame) { sent = append(sent, f...) }
	rt.PushDivisionPeerFrames = func(_, _ string, f []wire.Frame) { peers = append(peers, f...) }
	before := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
	if out := gmcommand.HandleGmCommand(deps, nil, testDivision, c, warpFrame(), rt); out.Ack != nil || len(sent) != 0 {
		t.Fatal("nonGM warp")
	}
	if missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}) != before {
		t.Fatal("nonGM mutated position")
	}
	c.GMPrivilege = true
	rt.Selected.Set(testDivision, c.Name, 999)
	hp, mp := *c.CurrentHP, *c.CurrentMP
	out := gmcommand.HandleGmCommand(deps, nil, testDivision, c, warpFrame(), rt)
	if !bytes.Equal(out.Ack, []byte{1, 16}) {
		t.Fatalf("warp refused: %+v", out)
	}
	if len(sent) < 7 || sent[0].Opcode != enterworld.OpcodeResetClient || binary.LittleEndian.Uint16(sent[0].Payload) != 25416 {
		t.Fatalf("entry missing: %+v", sent)
	}
	got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
	want := simulation.Spawn{RegionID: 25416, X: 703, Y: 42, Z: 1575, Angle: 12345}
	if got != want || c.World.MoveSegment != nil {
		t.Fatalf("persisted move %+v / %+v", got, c.World.MoveSegment)
	}
	if *c.CurrentHP != hp || *c.CurrentMP != mp {
		t.Fatal("warp changed vitals")
	}
	if _, ok := rt.Selected.Get(testDivision, c.Name); ok {
		t.Fatal("retained stale selection")
	}
	if len(peers) != 1 {
		t.Fatal("peer correction missing")
	}
	correction, err := wire.DecodeObjectSourceCorrection(peers[0].Payload)
	if err != nil || correction.RegionID != 25416 || correction.Heading != 12345 {
		t.Fatalf("peer correction %+v %v", correction, err)
	}
}

func TestGMWarpRefusals(t *testing.T) {
	for _, kind := range []string{"denied-region", "missing-terrain", "stranded", "dead", "malformed", "nonfinite", "out-of-region"} {
		t.Run(kind, func(t *testing.T) {
			c := rebirthTestCharacter(20, 100)
			c.GMPrivilege = true
			rt, _ := newTestRuntime(c, testItems())
			deps := rt.deps.(*enterworld.Deps)
			deps.CanEnterWorldRegion = func(*enterworld.Character, uint16) bool { return kind != "denied-region" }
			deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, kind != "missing-terrain" }
			deps.RelocateStrandedSpawn = func(p simulation.Spawn) (simulation.Spawn, bool, bool) { return p, kind == "stranded", false }
			if kind == "dead" {
				*c.CurrentHP = 0
			}
			p := warpFrame()
			if kind == "malformed" {
				p = append(p, 0)
			}
			if kind == "nonfinite" {
				binary.LittleEndian.PutUint32(p[3:], 0x7fc00000)
			}
			if kind == "out-of-region" {
				binary.LittleEndian.PutUint32(p[3:], math.Float32bits(1920))
			}
			before := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
			sent := false
			rt.PushCharacterFrames = func(string, string, []wire.Frame) { sent = true }
			out := gmcommand.HandleGmCommand(deps, nil, testDivision, c, p, rt)
			if bytes.Equal(out.Ack, []byte{1, 16}) || sent || missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}) != before {
				t.Fatalf("refusal mutated state: %+v", out)
			}
		})
	}
}

type failedWarpEntry struct{ *enterworld.Deps }

func (*failedWarpEntry) ReentryPackets(string, string) ([]enterworld.Packet, bool) { return nil, false }
func TestGMWarpEntryFailureRestoresMovement(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	c.GMPrivilege = true
	rt, clock := newTestRuntime(c, testItems())
	installMidMove(rt, c, clock)
	deps := rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(*enterworld.Character, uint16) bool { return true }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	rt.deps = &failedWarpEntry{deps}
	rt.PushCharacterFrames = func(string, string, []wire.Frame) { t.Fatal("partial entry published") }
	before := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
	previousState := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
	r, _ := gmcommand.DecodeGmCommand(warpFrame())
	if rt.WarpGM(testDivision, c.Name, r.Destination) {
		t.Fatal("failed entry accepted")
	}
	restored := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
	if missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}) != before || restored.Spawn != previousState.Spawn || (restored.MoveSegment == nil || *restored.MoveSegment != *previousState.MoveSegment) {
		t.Fatal("old movement not restored")
	}
}

/*
==================
TestGMWarpCarriesMountedVehicle

Issue #4422: a GM riding a vehicle warps like one on foot. The warp lands
at the destination and the vehicle stays summoned and ridden.
==================
*/
func TestGMWarpCarriesMountedVehicle(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	c.GMPrivilege = true
	rt, _ := newTestRuntime(c, testCosSource(testItems()))
	c.ActiveCOS = &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, Mounted: true,
	}
	deps := rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(*enterworld.Character, uint16) bool { return true }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	var sent []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, f []wire.Frame) { sent = append(sent, f...) }
	out := gmcommand.HandleGmCommand(deps, nil, testDivision, c, warpFrame(), rt)
	if !bytes.Equal(out.Ack, []byte{1, 16}) {
		t.Fatalf("mounted warp refused: %+v", out)
	}
	if len(sent) == 0 || sent[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("entry missing: %+v", sent)
	}
	got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
	if got.RegionID != 25416 || got.X != 703 || got.Z != 1575 {
		t.Fatalf("mounted warp landed at %+v", got)
	}
	if c.ActiveCOS == nil || !c.ActiveCOS.Summoned || !c.ActiveCOS.Mounted {
		t.Fatalf("vehicle lost on warp: %+v", c.ActiveCOS)
	}
}
