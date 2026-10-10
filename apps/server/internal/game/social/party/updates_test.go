/*
===========================================================================

updates_test.go - party map publication independent of entity visibility

===========================================================================
*/
package party

import (
	"encoding/binary"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

/*
================
updateDeps
================
*/
type updateDeps struct {
	vitalsDeps
	characters []*domain.Character
}

/*
================
CharactersForDivision
================
*/
func (d updateDeps) CharactersForDivision(string) []*domain.Character { return d.characters }

/*
================
Read
================
*/
func (d updateDeps) Read(_ string, read func()) { read() }

/*
================
TestMemberUpdatesFollowLivePositionOutsideInterest
================
*/
func TestMemberUpdatesFollowLivePositionOutsideInterest(t *testing.T) {
	first := &domain.Character{ID: 1, Name: "MapFirst"}
	second := &domain.Character{ID: 2, Name: "MapSecond"}
	runtime := NewRuntime(updateDeps{characters: []*domain.Character{first, second}}, nil)
	_, refusal := runtime.Registry().Form(testDivision, Member{MemberID: simulation.PlayerObjectID(1), Name: first.Name}, Member{MemberID: simulation.PlayerObjectID(2), Name: second.Name}, PartyOptionExpShare)
	if refusal != "" {
		t.Fatal(refusal)
	}
	world := simulation.DefaultWorldState(simulation.ChinaStartProfile())
	sessions := []simulation.SessionView{
		{DivisionID: testDivision, CharacterID: 1, World: world, WorldInstance: domain.DefaultWorldInstance},
		{DivisionID: testDivision, CharacterID: 2, World: world, WorldInstance: domain.DefaultWorldInstance},
	}
	firstBatch := runtime.MemberUpdates(sessions, 1000)
	if len(firstBatch) != 2 || len(firstBatch[0].Frames) != 2 {
		t.Fatalf("initial deltas = %+v", firstBatch)
	}
	if got := runtime.MemberUpdates(sessions, 2000); len(got) != 0 {
		t.Fatal("unchanged roster was republished")
	}
	far := world.Spawn
	far.RegionID++
	far.X = 800
	sessions[1].World.Spawn = far
	sessions[1].World.MoveSegment = &simulation.MoveSegment{From: simulation.Spawn{RegionID: far.RegionID, X: 600, Y: far.Y, Z: far.Z}, StartedAtMs: 2000, ArrivesAtMs: 4000}
	sessions[1].WorldInstance = 0x20001
	updates := runtime.MemberUpdates(sessions, 3000)
	if len(updates) != 2 {
		t.Fatalf("deltas did not reach both party members: %+v", updates)
	}
	for _, batch := range updates {
		if batch.OnlyCharacterID != 1 && batch.OnlyCharacterID != 2 {
			t.Fatal("party delta escaped membership")
		}
		if len(batch.Frames) != 1 || batch.Frames[0].Opcode != OpPartyUpdate {
			t.Fatal("wrong delta envelope")
		}
		body := batch.Frames[0].Payload
		if len(body) != 20 || body[0] != 6 || binary.LittleEndian.Uint32(body[1:5]) != simulation.PlayerObjectID(2) {
			t.Fatalf("wrong member delta: %x", body)
		}
		if got := int16(binary.LittleEndian.Uint16(body[10:12])); got != 700 {
			t.Fatalf("map x=%d, want live 700, not goal 800", got)
		}
		if got := binary.LittleEndian.Uint32(body[16:20]); got != 0x20001 {
			t.Fatalf("world=%x", got)
		}
	}
	if got := runtime.MemberUpdates(sessions, 3100); len(got) != 0 {
		t.Fatal("cadence failed")
	}
	runtime.MemberUpdates(nil, 4000)
	if len(runtime.updates.rows) != 0 {
		t.Fatal("departed roster retained update state")
	}
}
