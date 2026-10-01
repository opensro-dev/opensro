/*
===========================================================================

cosspawn_test.go - companion placement across actor lifecycle boundaries

The sampled position must reach both the owner's wire and peer presentation.
Geometry rejection keeps pets at a valid owner position; vehicles never draw.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCompanionAdmissionSamplesBothFamiliesAndKeepsVehiclesAtOwner
================
*/
func TestCompanionAdmissionSamplesBothFamiliesAndKeepsVehiclesAtOwner(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	draws := 0
	rt.CompanionRoll = func() (uint32, error) {
		draws++
		return 0, nil
	}
	centre := simulation.Spawn{RegionID: 0x62aa, X: 100, Y: 12, Z: 200}
	for _, name := range []string{"ATTACK", "PICKUP"} {
		ref := refs.characters[name]
		pet := &enterworld.CharacterCOS{Codename: name, RefObjID: ref.RefObjID}
		got := rt.companionAdmissionSpawn(pet, centre)
		if got.RegionID != centre.RegionID || got.X < 113 || got.X > 114 || got.Y != centre.Y || got.Z != centre.Z {
			t.Fatalf("%s did not receive native outer-band placement: %+v", name, got)
		}
	}
	if draws != 6 {
		t.Fatalf("two admissions consumed %d draws, want 6", draws)
	}
	vehicle := &enterworld.CharacterCOS{Codename: "COS_T_DHORSE3", RefObjID: 3914}
	if got := rt.companionAdmissionSpawn(vehicle, centre); got != centre || draws != 6 {
		t.Fatal("vehicle consumed a pet radius or draw", got, draws)
	}
}

/*
================
TestCompanionBootstrapMatchesPeerPositionsAndRefusedTravelRestoresFollowers

Inspect encoded object-list positions rather than an implementation helper.
The same identity must be at the same point for its owner and nearby players.
================
*/
func TestCompanionBootstrapMatchesPeerPositionsAndRefusedTravelRestoresFollowers(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	deps := rt.deps.(*enterworld.Deps)
	deps.EntryCompanionSpawn = rt.EntryCompanionSpawn
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	assertCompanionBootstrapPositions(t, rt, c)
	before := rt.CompanionPresentations(testDivision, c.Name)
	previous := rt.relocateReturningPet(testDivision, c, simulation.Spawn{RegionID: 0x62aa, X: 300, Y: 20, Z: 400})
	assertCompanionBootstrapPositions(t, rt, c)
	rt.restoreCompanionRelocation(previous)
	if after := rt.CompanionPresentations(testDivision, c.Name); !reflect.DeepEqual(before, after) {
		t.Fatal("refused travel changed a pet's pose, movement or generation")
	}
	assertCompanionBootstrapPositions(t, rt, c)
}

/*
================
assertCompanionBootstrapPositions
================
*/
func assertCompanionBootstrapPositions(t *testing.T, rt *Runtime, c *enterworld.Character) {
	t.Helper()
	packets, ok := rt.deps.ReentryPackets(testDivision, c.Name)
	if !ok {
		t.Fatal("companion bootstrap refused")
	}
	peers := rt.CompanionPresentations(testDivision, c.Name)
	for _, peer := range peers {
		found := false
		for _, packet := range packets {
			if packet.NativeOpcode != enterworld.OpcodeObjectListChunk || len(packet.Payload) < 24 {
				continue
			}
			payload := make([]byte, len(packet.Payload))
			for i, value := range packet.Payload {
				payload[i] = byte(value)
			}
			if binary.LittleEndian.Uint32(payload[4:]) != peer.Row.Gid {
				continue
			}
			got := wire.Position{
				RegionID: binary.LittleEndian.Uint16(payload[8:]),
				X:        math.Float32frombits(binary.LittleEndian.Uint32(payload[10:])),
				Y:        math.Float32frombits(binary.LittleEndian.Uint32(payload[14:])),
				Z:        math.Float32frombits(binary.LittleEndian.Uint32(payload[18:])),
				Heading:  binary.LittleEndian.Uint16(payload[22:]),
			}
			pose := peer.World.Spawn
			want := wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle}
			if got != want {
				t.Fatalf("pet %d owner position %+v differs from peer %+v", peer.Row.Gid, got, want)
			}
			found = true
		}
		if !found {
			t.Fatalf("bootstrap omitted pet %d", peer.Row.Gid)
		}
	}
}

/*
================
TestCompanionAdmissionWaitsForReadyAndResumePreservesItsPose
================
*/
func TestCompanionAdmissionWaitsForReadyAndResumePreservesItsPose(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, clock := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	rt.bindPetSession(testDivision, c, 101, false)
	pet := c.Companions()[0]
	pose := rt.EntryCompanionSpawn(testDivision, c, pet)
	if frames := rt.advancePets(clock.NowMs()); len(frames) != 0 || len(rt.CompanionPresentations(testDivision, c.Name)) != 0 {
		t.Fatal("pet ticked or became visible before its owner completed scene admission")
	}
	rt.BindPetSession(testDivision, c, 101)
	if len(rt.CompanionPresentations(testDivision, c.Name)) != 1 {
		t.Fatal("scene readiness failed to admit the companion")
	}
	rt.bindPetSession(testDivision, c, 101, false)
	if got := rt.EntryCompanionSpawn(testDivision, c, pet); got != pose {
		t.Fatal("resume rerolled an existing companion", got, pose)
	}
}

/*
================
TestCompanionBootstrapSerializesRestoredLeaseState

Build takes an early character snapshot to validate admission. Restoration
may expire a summon afterwards; that snapshot must never resurrect it in
the public object list or the populated inventory-item body.
================
*/
func TestCompanionBootstrapSerializesRestoredLeaseState(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, clock := newTestRuntime(c, refs)
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	c.Companions()[0].RentalExpiresAtUnix = clock.Now().Unix() - 1
	deps := rt.deps.(*enterworld.Deps)
	deps.RestoreEntryEffects = rt.RestoreTimedSkillJobs
	result := enterworld.Build(deps, enterworld.BootstrapRequest{DivisionID: testDivision, CharacterName: c.Name})
	if result.NativeResult != 1 || result.Character.Companions()[0].Summoned {
		t.Fatal("bootstrap serialized the pre-restoration companion snapshot", result)
	}
	for _, packet := range result.Packets {
		if packet.NativeOpcode == wire.OpCosRecordCreate {
			t.Fatal("expired pickup pet was restored to the new client scene")
		}
	}
}

/*
================
TestCompanionAdmissionFallsBackOnBlockedGeometryAndEntropyFailure
================
*/
func TestCompanionAdmissionFallsBackOnBlockedGeometryAndEntropyFailure(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	centre := simulation.Spawn{RegionID: 0x62aa, X: 100, Y: 12, Z: 200}
	pet := &enterworld.CharacterCOS{Codename: "PICKUP", RefObjID: 951}
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.ConstrainCompanionSpawn = func(from, to simulation.Spawn) simulation.Spawn {
		if from != centre || from == to {
			t.Fatal("geometry did not receive the owner-to-candidate segment")
		}
		return from
	}
	if got := rt.companionAdmissionSpawn(pet, centre); got != centre {
		t.Fatal("blocked pet placement did not fall back to centre", got)
	}
	rt.ConstrainCompanionSpawn = nil
	rt.CompanionRoll = func() (uint32, error) { return 0, errors.New("entropy unavailable") }
	if got := rt.companionAdmissionSpawn(pet, centre); got != centre {
		t.Fatal("failed draw moved the pet", got)
	}
}

/*
================
TestCompanionAdmissionNormalizesSectorCrossings
================
*/
func TestCompanionAdmissionNormalizesSectorCrossings(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	centre := simulation.Spawn{RegionID: 0x62aa, X: 1919, Y: 12, Z: 200}
	pet := &enterworld.CharacterCOS{Codename: "PICKUP", RefObjID: 951}
	got := rt.companionAdmissionSpawn(pet, centre)
	if got.RegionID != 0x62ab || got.X < 12 || got.X > 13 || got.Z != centre.Z {
		t.Fatal("spawn radius crossed a sector without normalization", got)
	}
}

/*
================
TestCompanionTravelAndResumeUseTheAdmittedPosition
================
*/
func TestCompanionTravelAndResumeUseTheAdmittedPosition(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, clock := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	destination := simulation.Spawn{RegionID: 0x62aa, X: 100, Y: 12, Z: 200}
	rt.relocateReturningPet(testDivision, c, destination)
	for _, pet := range c.Companions() {
		got := rt.companionLiveSpawn(testDivision, c, pet, clock.NowMs())
		if got.X < 113 || got.X > 114 || got.RegionID != destination.RegionID {
			t.Fatal("travel collapsed the pet onto the owner", got)
		}
		rt.BindPetSession(testDivision, c, 101)
		if resumed := rt.companionLiveSpawn(testDivision, c, pet, clock.NowMs()); resumed != got {
			t.Fatal("transport resume rerolled a resident companion", got, resumed)
		}
	}
}
