/*
===========================================================================

monsters_test.go - spawn variants, standings and the drop tables

===========================================================================
*/
package catalogue

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/quest"
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
fakeSummonSkills

Default skill 900 summons reference 20; anything else is a plain skill.
================
*/
type fakeSummonSkills struct{ enterworld.SkillDataSource }

func (fakeSummonSkills) SkillByID(id uint32) (enterworld.SkillRow, bool) {
	row := enterworld.SkillRow{ID: id}
	if id == 900 {
		row.Summon = monster.SummonSkill{Present: true}
		row.Summon.Entries[0] = monster.SummonEntry{RefObjID: 20}
	}
	return row, true
}

/*
================
TestOnlyProducibleMonstersAreListed

A nest with a cap places its monster; a nest pinned to zero places
nothing; a unique's other incarnation with no nest (the _L2 row) is not
listed; a placed unique's summon and a quest script's monster are.
================
*/
func TestOnlyProducibleMonstersAreListed(t *testing.T) {
	refs := map[uint32]monster.MonsterRef{
		1:  {RefObjID: 1, Codename: "MOB_PLACED", Name: "Placed", Level: 5, TypeID4: 1},
		2:  {RefObjID: 2, Codename: "MOB_CH_TIGERWOMAN", Name: "Tiger Girl", Level: 20, TypeID4: 1, MonsterType: gradeUnique, DefaultSkillIDs: [10]uint32{900}},
		3:  {RefObjID: 3, Codename: "MOB_CH_TIGERWOMAN_L2", Name: "Tiger Girl", Level: 60, TypeID4: 1, MonsterType: gradeUnique, DefaultSkillIDs: [10]uint32{900}},
		4:  {RefObjID: 4, Codename: "MOB_PINNED_EMPTY", Name: "Empty", Level: 5, TypeID4: 1},
		20: {RefObjID: 20, Codename: "MOB_SUMMONED", Name: "Summoned", Level: 20, TypeID4: 1},
	}
	next := uint32(100)
	for _, codename := range quest.ScriptMonsterCodenames() {
		refs[next] = monster.MonsterRef{RefObjID: next, Codename: codename, Name: codename, Level: 10, TypeID4: 1}
		next++
	}
	nests := []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 24230}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 2, RegionID: 24231}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 4, RegionID: 24232}, PolicyPinned: true, MaxCount: 0},
	}
	monsters, err := BuildMonsters(MonsterSource{Template: monster.TemplateFromParts(refs, nests),
		ItemID: func(string) uint32 { return 0 }, Skills: fakeSummonSkills{}})
	if err != nil {
		t.Fatal(err)
	}
	listed := map[uint32]Monster{}
	for _, m := range monsters {
		listed[m.ID] = m
		if m.Status != StatusImplemented {
			t.Errorf("%s listed as %s", m.Codename, m.Status)
		}
	}
	for _, id := range []uint32{1, 2, 20, 100, 101, 102, 103} {
		if _, ok := listed[id]; !ok {
			t.Errorf("reference %d is producible but not listed", id)
		}
	}
	for _, id := range []uint32{3, 4} {
		if m, ok := listed[id]; ok {
			t.Errorf("%s is listed, but nothing spawns it", m.Codename)
		}
	}
	if len(listed[1].Regions) != 1 || listed[2].Unique == nil || len(listed[20].Regions) != 0 {
		t.Errorf("placed %+v, unique %+v, summoned %+v", listed[1], listed[2], listed[20])
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
