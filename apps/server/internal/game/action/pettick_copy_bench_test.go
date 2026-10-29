/*
===========================================================================

pettick_copy_bench_test.go - what the pet tick's character reads cost (#1820 I)

Measures the two per-tick character reads near the pet tick on one fixture:
the owner snapshot advancePet takes for every summoned pet every tick
(Character.Snapshot, a deep copy), and characterByCosGID, which walks the
division's characters to find a pet's owner. The fixture owner carries a
full bag so the copy has a production-like size.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// petTickBenchCharacters is the division size the owner lookup walks.
const petTickBenchCharacters = 64

// petTickBenchBagRows is the owner's bag size, near a full v1.150 bag.
const petTickBenchBagRows = 96

/*
================
petTickBenchOwner

A test character with a full bag and a summoned pet.
================
*/
func petTickBenchOwner(id int64) *enterworld.Character {
	c := testCharacter()
	c.ID = id
	c.Name = fmt.Sprintf("owner%02d", id)
	row := c.MissionInventory[0]
	c.MissionInventory = c.MissionInventory[:0]
	for slot := 0; slot < petTickBenchBagRows; slot++ {
		row.Slot = int64(13 + slot)
		c.MissionInventory = append(c.MissionInventory, row)
	}
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	return c
}

/*
================
BenchmarkPetTickOwnerSnapshot

The deep copy advancePet takes per summoned pet per tick (petai.go).
================
*/
func BenchmarkPetTickOwnerSnapshot(b *testing.B) {
	c := petTickBenchOwner(1)
	b.ReportAllocs()
	for b.Loop() {
		if c.Snapshot() == nil {
			b.Fatal("no snapshot")
		}
	}
}

/*
================
BenchmarkPetTickOwnerCompanionSnapshot

The narrow copy advancePet takes per summoned pet per tick since
#2030 (Character.SnapshotCompanion): the same fixture as
BenchmarkPetTickOwnerSnapshot.
================
*/
func BenchmarkPetTickOwnerCompanionSnapshot(b *testing.B) {
	c := petTickBenchOwner(1)
	b.ReportAllocs()
	for b.Loop() {
		if c.SnapshotCompanion() == nil {
			b.Fatal("no snapshot")
		}
	}
}

/*
================
BenchmarkPetTickOwnerByCosGID

characterByCosGID over a division of petTickBenchCharacters, the owner last.
================
*/
func BenchmarkPetTickOwnerByCosGID(b *testing.B) {
	roster := make([]*enterworld.Character, 0, petTickBenchCharacters)
	for id := int64(1); id <= petTickBenchCharacters; id++ {
		roster = append(roster, petTickBenchOwner(id))
	}
	deps := &enterworld.Deps{Characters: enterworld.StaticCharacterSource{testDivision: roster}, Items: testItems()}
	rt := NewRuntime(deps, nil)
	gid := roster[len(roster)-1].ActiveCOS.GID
	b.ReportAllocs()
	for b.Loop() {
		if rt.characterByCosGID(testDivision, gid) == nil {
			b.Fatal("no owner")
		}
	}
}
