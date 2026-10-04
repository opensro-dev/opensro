package simulation

import (
	"encoding/binary"
	"math"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

func TestPeerCOSVisibilityLifecycleAndLateJoin(t *testing.T) {
	owner := peerSession("owner", "A", 1, "Owner")
	viewer := peerSession("viewer", "A", 2, "Viewer")
	other := peerSession("other", "B", 3, "Other")
	from := owner.World.Spawn
	goal := from
	goal.X += 100
	owner.COS = &PeerCOS{Row: wire.CosSpawnBand2{Band: 4, RefObjID: 9, Gid: 100, Name: "Pet", OwnerName: "Owner", OwnerGid: PlayerObjectID(1), Run: 100},
		World: WorldState{Spawn: goal, MoveSegment: &MoveSegment{From: from, StartedAtMs: 1000, ArrivesAtMs: 2000}}, Revision: 1, Session: 1, Generation: 1}
	source := &fakeSource{sessions: []SessionSnapshot{owner, viewer, other}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(1500)
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 2 || len(peerFramesTo(push, "viewer", OpMovementAck)) != 1 {
		t.Fatal("peer needs player spawn, pet spawn and current pet movement")
	}
	rows := peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)
	pet := rows[1]
	if binary.LittleEndian.Uint32(pet[4:8]) != 100 || math.Float32frombits(binary.LittleEndian.Uint32(pet[10:14])) != float32(from.X+50) {
		t.Fatal("pet first sight used the destination instead of its live position")
	}
	if len(peerFramesTo(push, "owner", OpMovementAck)) != 0 || len(peerFramesTo(push, "other", OpMovementAck)) != 0 {
		t.Fatal("pet movement leaked to owner duplicate or another division")
	}
	push.toSession = nil
	ticker.RunTick(1600)
	if len(peerFramesTo(push, "viewer", OpMovementAck)) != 0 || len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 0 {
		t.Fatal("unchanged pet republished")
	}
	late := peerSession("late", "A", 4, "Late")
	source.sessions = append(source.sessions, late)
	ticker.RunTick(1700)
	if len(peerFramesTo(push, "late", OpMovementAck)) != 1 {
		t.Fatal("late viewer did not receive in-flight pet movement")
	}
	push.toSession = nil
	// Same GID and reference, but a replacement summon must be a new lifetime.
	replacement := *owner.COS
	replacement.Generation++
	for i := range source.sessions {
		if source.sessions[i].SessionID == "owner" {
			source.sessions[i].COS = &replacement
		}
	}
	ticker.RunTick(1800)
	if len(peerFramesTo(push, "viewer", wire.OpObjectDespawn)) != 1 || len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 1 {
		t.Fatal("same-identity replacement did not despawn before respawn")
	}
	push.toSession = nil
	for i := range source.sessions {
		if source.sessions[i].SessionID == "owner" {
			source.sessions[i].COS = nil
		}
	}
	ticker.RunTick(1900)
	if len(peerFramesTo(push, "viewer", wire.OpObjectDespawn)) != 1 {
		t.Fatal("dismissed pet did not despawn")
	}
	push.toSession = nil
	ticker.RunTick(2000)
	if len(peerFramesTo(push, "viewer", wire.OpObjectDespawn)) != 0 {
		t.Fatal("despawn repeated")
	}
}

func TestPeerCOSInstanceDepartureAndReentry(t *testing.T) {
	owner := peerSession("owner", "A", 1, "Owner")
	viewer := peerSession("viewer", "A", 2, "Viewer")
	owner.WorldInstance, viewer.WorldInstance = 0x10001, 0x20001
	owner.COS = &PeerCOS{Row: wire.CosSpawnBand2{Band: 4, RefObjID: 9, Gid: 100, Name: "Pet", OwnerName: "Owner", OwnerGid: PlayerObjectID(1)}, World: owner.World, Revision: 1, Session: 1, Generation: 1}
	source := &fakeSource{sessions: []SessionSnapshot{owner, viewer}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(1000)
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 0 {
		t.Fatal("pet crossed instance")
	}
	source.sessions[1].WorldInstance = owner.WorldInstance
	ticker.RunTick(1100)
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 2 {
		t.Fatal("peer and pet missing on entry")
	}
	push.toSession = nil
	source.sessions[1].WorldInstance = 0x20001
	ticker.RunTick(1200)
	if len(peerFramesTo(push, "viewer", wire.OpObjectDespawn)) != 2 {
		t.Fatal("peer and pet retained after exit")
	}
	push.toSession = nil
	source.sessions[1].WorldInstance = owner.WorldInstance
	ticker.RunTick(1300)
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 2 {
		t.Fatal("old pet revision suppressed reentry")
	}
}

func TestMountedCOSLateViewerReceivesBodyBeforeRide(t *testing.T) {
	owner := peerSession("owner", "A", 1, "Owner")
	viewer := peerSession("viewer", "A", 2, "Viewer")
	owner.COS = &PeerCOS{Mounted: true, NativeBodyStatus: 4,
		Row:   wire.CosSpawnBand2{Band: 2, RefObjID: 9, Gid: 100, OwnerGid: PlayerObjectID(1)},
		World: owner.World, Session: 1, Generation: 1}
	source := &fakeSource{sessions: []SessionSnapshot{owner, viewer}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(1000)
	rows := peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)
	if len(rows) != 2 || rows[1][31] != 4 {
		t.Fatal("missing initialized COS spawn", rows)
	}
	rides := peerFramesTo(push, "viewer", wire.OpCosRideState)
	if len(rides) != 1 {
		t.Fatal("missing late-viewer mount relation", rides)
	}
	frames := owner.COS.frames(1000, true)
	if len(frames) != 3 || frames[0].Opcode != wire.OpSingleObjectSpawn || frames[1].Opcode != wire.OpObjectStateRefresh || frames[2].Opcode != wire.OpCosRideState {
		t.Fatal("invalid spawn/status/ride order", frames)
	}
	push.toSession = nil
	ticker.RunTick(1100)
	if len(peerFramesTo(push, "viewer", wire.OpCosRideState)) != 0 {
		t.Fatal("unchanged ride repeated")
	}
}

/*
================
TestMountedCOSFollowsItsRiderVisibility

A mounted vehicle whose snapshot pose reaches a viewer before its rider's
does must not publish its ride to that viewer: the ride names a rider the
viewer does not hold. Once the rider is shown, the vehicle and ride follow.
================
*/
func TestMountedCOSFollowsItsRiderVisibility(t *testing.T) {
	owner := peerSession("owner", "A", 1, "Owner")
	viewer := peerSession("viewer", "A", 2, "Viewer")
	near := owner.World
	owner.World.Spawn.RegionID ^= 0x0101 // a region far from the viewer
	owner.COS = &PeerCOS{Mounted: true, Row: wire.CosSpawnBand2{Band: 1, RefObjID: 9, Gid: 100, OwnerGid: PlayerObjectID(1)},
		World: near, Session: 1, Generation: 1}
	source := &fakeSource{sessions: []SessionSnapshot{owner, viewer}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(1000)
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 0 || len(peerFramesTo(push, "viewer", wire.OpCosRideState)) != 0 {
		t.Fatal("vehicle published ahead of its rider")
	}
	source.sessions[0].World = near
	ticker.RunTick(1100)
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 2 || len(peerFramesTo(push, "viewer", wire.OpCosRideState)) != 1 {
		t.Fatal("rider and vehicle missing once both are in view")
	}
}

/*
================
TestFreshCOSFirstSightCarriesTheSummonSubState

A pet seen right after its summon spawns with sub-state 1; the same pet
entering a later viewer's scope spawns with 0.
================
*/
func TestFreshCOSFirstSightCarriesTheSummonSubState(t *testing.T) {
	pet := PeerCOS{Fresh: true, Row: wire.CosSpawnBand2{Band: 3, RefObjID: 9, Gid: 100, OwnerGid: PlayerObjectID(1)}, Session: 1, Generation: 1}
	frames := pet.frames(1000, true)
	if row := frames[0].Payload; row[len(row)-1] != 1 {
		t.Fatalf("fresh summon sub-state = %d", row[len(row)-1])
	}
	pet.Fresh = false
	if row := pet.frames(5000, true)[0].Payload; row[len(row)-1] != 0 {
		t.Fatalf("later scope entry sub-state = %d", row[len(row)-1])
	}
}
