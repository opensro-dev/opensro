/*
===========================================================================

unique_nests_test.go - the world map's nest table is the template's

The table lists every nest the population's unique rule names and no
other, in template order, in the client's format.

===========================================================================
*/
package main

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestUniqueNestTableFollowsTheTemplate
================
*/
func TestUniqueNestTableFollowsTheTemplate(t *testing.T) {
	refs := map[uint32]monster.MonsterRef{
		10: {RefObjID: 10, Codename: "MOB_UNIQUE", MonsterType: 3},
		11: {RefObjID: 11, Codename: "MOB_PLAIN", MonsterType: 0},
	}
	nests := []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 10, RegionID: 25000, X: 100.5, Z: 200.25}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 11, RegionID: 25000, X: 1, Z: 3}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 10, RegionID: 0x8007, X: 960, Z: 960}},
		// A nest whose reference the template does not hold.
		{SpawnPoint: monster.SpawnPoint{RefObjID: 12, RegionID: 25257, X: 5, Z: 6}},
	}
	raw, err := json.Marshal(uniqueNestTableFor(monster.TemplateFromParts(refs, nests)))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format":"sro-unique-nests","version":1,"nests":[` +
		`{"refObjId":10,"regionId":25000,"x":100.5,"z":200.25},` +
		`{"refObjId":10,"regionId":32775,"x":960,"z":960}]}`
	if string(raw) != want {
		t.Fatalf("table = %s\nwant    %s", raw, want)
	}
}

/*
================
TestUniqueNestTableOfAnEmptyTemplate
================
*/
func TestUniqueNestTableOfAnEmptyTemplate(t *testing.T) {
	raw, err := json.Marshal(uniqueNestTableFor(monster.Template{}))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"format":"sro-unique-nests","version":1,"nests":[]}` {
		t.Fatalf("empty table = %s", raw)
	}
}
