package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"

	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestStructuresTakeOnlyBasicAttacksDuringTheWar

52BF90: outside the war a structure refuses everything; during it only the
weapon's basic attack lands, and never from the guild holding the fortress.
================
*/
func TestStructuresTakeOnlyBasicAttacksDuringTheWar(t *testing.T) {
	textdataDir := gamedatatest.TextdataDir(t)
	rt := NewRuntime(&enterworld.Deps{
		Items:  enterworld.NewTextdataItems(textdataDir),
		Skills: enterworld.NewTextdataSkills(textdataDir),
	}, nil)
	if err := rt.ConfigurePortals(textdataDir); err != nil {
		t.Fatal(err)
	}
	c := shippedEuropeanSwordsman()
	basic, _, why := rt.resolveBasicAttack(c)
	if why != "" {
		t.Fatalf("no basic attack: %s", why)
	}
	tower := monster.Instance{Gid: 7, Ref: monster.MonsterRef{Structure: true, TypeID4: 2}, Nest: monster.NestRow{WorldCode: "INS_FORT_JA"}}
	if code := rt.structureAttackRefusal(testDivision, c, tower, basic.ID, 0); code != structureRefusedOutsideWar {
		t.Fatalf("outside the war = %#x, want %#x", code, structureRefusedOutsideWar)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	if code := rt.structureAttackRefusal(testDivision, c, tower, basic.ID, 0); code != 0 {
		t.Fatalf("a basic attack during the war = %#x", code)
	}
	if code := rt.structureAttackRefusal(testDivision, c, tower, basic.ID+1, 0); code != structureRefusedSkill {
		t.Fatalf("another skill = %#x, want %#x", code, structureRefusedSkill)
	}
	guild := int64(77)
	c.GuildID = &guild
	var fortressID uint32
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			fortressID = record.ID
		}
	}
	if !rt.Fortresses.Occupy(testDivision, fortressID, guild) {
		t.Fatal("Jangan fortress missing")
	}
	if code := rt.structureAttackRefusal(testDivision, c, tower, basic.ID, 0); code != structureRefusedOwnStructure {
		t.Fatalf("the holder's own tower = %#x, want %#x", code, structureRefusedOwnStructure)
	}
	gate := tower
	gate.Ref.TypeID4 = structureKindGate
	if code := rt.structureAttackRefusal(testDivision, c, gate, basic.ID, 0); code != 0 {
		t.Fatalf("the holder's gate = %#x, want admitted", code)
	}
}

/*
================
TestAnOpenGateCannotBeStruck

52C121..52C141: during the war a gate whose state word a pulley set
refuses every attacker 0x3042; shut (word 0), it takes the basic attack.
================
*/
func TestAnOpenGateCannotBeStruck(t *testing.T) {
	textdataDir := gamedatatest.TextdataDir(t)
	rt := NewRuntime(&enterworld.Deps{
		Items:  enterworld.NewTextdataItems(textdataDir),
		Skills: enterworld.NewTextdataSkills(textdataDir),
	}, nil)
	if err := rt.ConfigurePortals(textdataDir); err != nil {
		t.Fatal(err)
	}
	c := shippedEuropeanSwordsman()
	basic, _, why := rt.resolveBasicAttack(c)
	if why != "" {
		t.Fatalf("no basic attack: %s", why)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	gate := monster.Instance{Gid: 9, Ref: monster.MonsterRef{Structure: true, TypeID4: structureKindGate},
		Nest: monster.NestRow{WorldCode: "INS_FORT_JA"}, StructureState: 2}
	if code := rt.structureAttackRefusal(testDivision, c, gate, basic.ID, 0); code != structureRefusedOpenGate {
		t.Fatalf("an open gate answered %#x, want %#x", code, structureRefusedOpenGate)
	}
	gate.StructureState = 0
	if code := rt.structureAttackRefusal(testDivision, c, gate, basic.ID, 0); code != 0 {
		t.Fatalf("a shut gate answered %#x", code)
	}
}
