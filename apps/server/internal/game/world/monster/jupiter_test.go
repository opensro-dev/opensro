/*
===========================================================================

jupiter_test.go - the Jupiter populations' admission and world join.

The switches' boot/refusal legs (off never reads, the temple requires
the field before any read) are jupiter_boot_test.go; this file owns
the data level: which rows the two populations admit and which world
each nest joins.

===========================================================================
*/
package monster

import (
	"encoding/json"
	"testing"
)

/*
================
jupiterSource

One Jupiter nest row in the given world, encoded as the import writes
it; spawnType is the database's second spawn kind when nonzero.
================
*/
func jupiterSource(t *testing.T, codename string, world, spawnType int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 41001, WorldID: world, RegionID: 21000,
		X: 100, Y: 0, Z: 200, Radius: 400, GenerateRadius: 300, MaxCount: 10,
		Respawn: true, RespawnDelayMinSec: 6, RespawnDelayMaxSec: 8, SpawnType: spawnType,
		Controls: TacticsControls{ChangeTarget: 2, SightRange: 130}}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
TestJupiterAdmitsItsGradesAcrossTheWindow
================
*/
func TestJupiterAdmitsItsGradesAcrossTheWindow(t *testing.T) {
	for _, grade := range []uint8{jupiterGradeOrdinary, 1, 3, gradeElite, jupiterGradeUnique} {
		for _, level := range []uint8{jupiterFirstLevel, 115, jupiterLastLevel} {
			template := tombTemplate("MOB_JUPITER_SOLDIER", level, grade)
			got, err := appendJupiterData(template, jupiterSource(t, "MOB_JUPITER_SOLDIER", 90, 0))
			if err != nil {
				t.Fatalf("grade %d level %d refused: %v", grade, level, err)
			}
			if len(template.Nests) != 1 || len(got.Nests) != 2 {
				t.Fatalf("grade %d level %d: population did not append immutably", grade, level)
			}
			nest := got.Nests[1]
			if nest.WorldCode != "GROUP_JUPITER_FIELD" || nest.RegionID != 21000 || nest.MaxCount != 10 || !nest.Respawn {
				t.Fatalf("grade %d level %d: authored nest values lost: %+v", grade, level, nest)
			}
		}
	}
}

/*
================
TestJupiterAdmitsTheFieldGuest

The source places one Arabia kind in the field (level 120, grade 3);
it joins by name on the family's window, like the cap-110 field's
Apis, in the field population.
================
*/
func TestJupiterAdmitsTheFieldGuest(t *testing.T) {
	template := tombTemplate(jupiterGuestCode, jupiterLastLevel, 3)
	got, err := appendJupiterData(template, jupiterSource(t, jupiterGuestCode, 90, 0))
	if err != nil {
		t.Fatalf("the field guest refused: %v", err)
	}
	if got.Nests[1].WorldCode != "GROUP_JUPITER_FIELD" {
		t.Fatalf("guest nest joined %q, want GROUP_JUPITER_FIELD", got.Nests[1].WorldCode)
	}
}

/*
================
TestJupiterRefusesOtherGradesLevelsAndNames
================
*/
func TestJupiterRefusesOtherGradesLevelsAndNames(t *testing.T) {
	cases := []struct {
		name     string
		codename string
		level    uint8
		grade    uint8
	}{
		{"grade 2", "MOB_JUPITER_SOLDIER", 115, 2},
		{"grade 4", "MOB_JUPITER_SOLDIER", 115, 4},
		{"grade 7", "MOB_JUPITER_SOLDIER", 115, 7},
		{"below the window", "MOB_JUPITER_SOLDIER", jupiterFirstLevel - 1, 0},
		{"above the window", "MOB_JUPITER_SOLDIER", jupiterLastLevel + 1, 0},
		{"temple species", "MOB_GOD_SOLDIER", 115, 0},
		{"field species", "MOB_SD_FIELD", 115, 0},
		{"an area NPC", "NPC_JUPITER_SOLDIER_01", 115, 0},
		{"the guest below the window", jupiterGuestCode, jupiterFirstLevel - 1, 3},
		{"the guest at a foreign grade", jupiterGuestCode, jupiterLastLevel, 2},
		{"another Arabia kind", "MOB_ARABIA_SOLDIER", 115, 0},
	}
	for _, c := range cases {
		template := tombTemplate(c.codename, c.level, c.grade)
		if _, err := appendJupiterData(template, jupiterSource(t, c.codename, 90, 0)); err == nil {
			t.Fatalf("%s admitted", c.name)
		}
		if _, err := appendJupiterTempleData(template, jupiterSource(t, c.codename, 86, 0)); err == nil {
			t.Fatalf("%s admitted to the temple", c.name)
		}
	}
}

/*
================
TestJupiterNestsJoinTheirOwnWorlds

The field world and every temple world take their nests; a row naming
any other world is refused - the Jupiter populations must not leak
onto another world.
================
*/
func TestJupiterNestsJoinTheirOwnWorlds(t *testing.T) {
	for _, world := range []int{86, 87, 88, 89, 90, 93, 94} {
		template := tombTemplate("MOB_JUPITER_SOLDIER", 115, 0)
		got, err := appendJupiterData(template, jupiterSource(t, "MOB_JUPITER_SOLDIER", world, 0))
		if err != nil {
			t.Fatalf("world %d refused: %v", world, err)
		}
		if code := got.Nests[1].WorldCode; code == "" || code == "INS_DEFAULT" {
			t.Fatalf("world %d nest joined %q", world, code)
		}
	}
	template := tombTemplate("MOB_JUPITER_SOLDIER", 115, 0)
	got, err := appendJupiterTempleData(template, jupiterSource(t, "MOB_JUPITER_SOLDIER", 88, 0))
	if err != nil {
		t.Fatalf("temple world nest refused: %v", err)
	}
	if got.Nests[1].WorldCode != "GROUP_JUPITER_A2" {
		t.Fatalf("temple nest joined %q, want GROUP_JUPITER_A2", got.Nests[1].WorldCode)
	}
	for _, world := range []int{0, 1, 85, 91, 92, 95, cap110FieldWorldID} {
		template := tombTemplate("MOB_JUPITER_SOLDIER", 115, 0)
		if _, err := appendJupiterData(template, jupiterSource(t, "MOB_JUPITER_SOLDIER", world, 0)); err == nil {
			t.Fatalf("jupiter row in world %d admitted", world)
		}
	}
}

/*
================
TestJupiterCarriesTheSecondSpawnKind

The import carries the database's second spawn kind on the row when
nonzero; the consumer preserves it on the nest instead of refusing
or dropping it.
================
*/
func TestJupiterCarriesTheSecondSpawnKind(t *testing.T) {
	template := tombTemplate("MOB_JUPITER_CAPTAIN", 115, 0)
	got, err := appendJupiterData(template, jupiterSource(t, "MOB_JUPITER_CAPTAIN", 90, 1))
	if err != nil {
		t.Fatalf("kind-1 nest refused: %v", err)
	}
	if got.Nests[1].SpawnType != 1 {
		t.Fatalf("spawn kind = %d, want 1 carried", got.Nests[1].SpawnType)
	}
	got, err = appendJupiterData(template, jupiterSource(t, "MOB_JUPITER_CAPTAIN", 90, 0))
	if err != nil {
		t.Fatalf("kind-0 nest refused: %v", err)
	}
	if got.Nests[1].SpawnType != 0 {
		t.Fatalf("spawn kind = %d, want 0", got.Nests[1].SpawnType)
	}
}
