/*
===========================================================================

monsters_test.go - spawn variants, standings and the drop tables

===========================================================================
*/
package catalogue

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestMonsterVariantsFollowTheSpawnRoll
================
*/
func TestMonsterVariantsFollowTheSpawnRoll(t *testing.T) {
	grades := func(variants []MonsterVariant) string {
		out := ""
		for _, v := range variants {
			out += v.Grade
			if v.Party {
				out += "+party"
			}
			out += ","
		}
		return out
	}
	for _, tc := range []struct {
		name     string
		ref      monster.MonsterRef
		promotes bool
		want     string
	}{
		{"plain nest", monster.MonsterRef{Codename: "MOB_A", Level: 10, TypeID4: 1}, false, "normal,normal+party,"},
		{"champion nest", monster.MonsterRef{Codename: "MOB_A", Level: 10, TypeID4: 1}, true, "normal,champion,giant,normal+party,"},
		{"quest monster", monster.MonsterRef{Codename: "MOB_QT_A", Level: 10, TypeID4: questMonsterTypeID4}, true, "normal,champion,giant,"},
		{"unique", monster.MonsterRef{Codename: "MOB_U", Level: 20, TypeID4: 1, MonsterType: gradeUnique}, true, "unique,"},
	} {
		if got := grades(monsterVariants(tc.ref, tc.promotes)); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	party := monsterVariants(monster.MonsterRef{Codename: "MOB_A", Level: 10, TypeID4: 1}, false)[1]
	normal := monsterVariants(monster.MonsterRef{Codename: "MOB_A", Level: 10, TypeID4: 1}, false)[0]
	if party.GoldMax != normal.GoldMax*9 || party.Attempts != normal.Attempts*9 {
		t.Fatalf("party %+v against normal %+v: gold and attempts are nine times over", party, normal)
	}
}

/*
================
TestUnplacedMonsterIsMissingWithAnIssue
================
*/
func TestUnplacedMonsterIsMissingWithAnIssue(t *testing.T) {
	refs := map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: "MOB_PLACED", Name: "Placed", Level: 5, TypeID4: 1},
		2: {RefObjID: 2, Codename: "MOB_UNIQUE_L2", Name: "Unique", Level: 20, TypeID4: 1, MonsterType: gradeUnique},
		3: {RefObjID: 3, Codename: "MOB_UNUSED", Name: "Unused", Level: 5, TypeID4: 1},
	}
	nests := []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 24230}}}
	monsters, err := BuildMonsters(MonsterSource{Template: monster.TemplateFromParts(refs, nests), ItemID: func(string) uint32 { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	if len(monsters) != 2 || monsters[0].ID != 1 || monsters[1].ID != 2 {
		t.Fatalf("exported %+v, want the placed monster and the unique", monsters)
	}
	if monsters[0].Status != StatusImplemented || len(monsters[0].Regions) != 1 {
		t.Errorf("placed monster %+v", monsters[0])
	}
	if monsters[1].Status != StatusMissing || monsters[1].Issue == "" || monsters[1].Unique == nil {
		t.Errorf("unplaced unique %+v", monsters[1])
	}
}

/*
================
TestDropTablesSumToAnAttempt

Every table's chances are one attempt's outcomes, so they sum to at most
one; the native rare chance is 33 of 32768 draws.
================
*/
func TestDropTablesSumToAnAttempt(t *testing.T) {
	monsters := []Monster{{Level: 30, Country: 3, Table: equipmentTableKey(30, 3), Unique: &UniqueBonus{Table: equipmentTableKey(30, 3)}}}
	itemID := func(codename string) uint32 { return uint32(len(codename)) }
	file := BuildDropTables(monsters, itemID, Header{})
	if math.Abs(file.RareChance-33.0/32768) > 1e-15 {
		t.Fatalf("rare chance %v", file.RareChance)
	}
	sum := func(rows []DropChance) float64 {
		total := 0.0
		for _, row := range rows {
			total += row.Chance
		}
		return total
	}
	table := file.Equipment["30/3"]
	if len(table.Normal) == 0 || len(table.Rare) == 0 || len(file.Special["30/3"]) == 0 {
		t.Fatalf("level 30 tables %+v", table)
	}
	for key, rows := range file.Consumables {
		if s := sum(rows); s > 1+1e-9 {
			t.Errorf("consumables %s sum to %v", key, s)
		}
	}
	if s := sum(table.Normal); s <= 0 || s > 1 {
		t.Errorf("equipment sums to %v", s)
	}
	if len(file.Consumables) != 9 {
		t.Errorf("%d consumable tables for one level, want the nine families", len(file.Consumables))
	}
}
