/*
===========================================================================

fortress_structures_test.go - the fortress manager's structure services

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestFortressStructureListNamesEveryZone

634850 / 61E000: Jangan's two standing structures (zones 84 and 85) list
with zero construction minutes, in zone order; an unknown fortress answers
code 3; and the dispatcher hands action 0x18 to the NPC service admission
(service 0x19) instead of refusing it as unknown.
================
*/
func TestFortressStructureListNamesEveryZone(t *testing.T) {
	rt, c, _, _ := captureFixture(t)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	out := rt.fortressStructureList(testDivision, c, siege.Interaction{Action: siege.ActionStructureQuery, Fortress: jangan})
	want := wire.NewWriter(32).U8(siege.ActionStructureQuery).U8(1).U32(jangan).U8(2).U32(84).U32(0).U32(85).U32(0).Payload()
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("structure list %+v, want % x", out.Frames, want)
	}
	missing := rt.fortressStructureList(testDivision, c, siege.Interaction{Action: siege.ActionStructureQuery, Fortress: 999})
	if len(missing.Frames) != 1 || !bytes.Equal(missing.Frames[0].Payload, []byte{siege.ActionStructureQuery, 2, fortressErrInvalid}) {
		t.Fatalf("unknown fortress answered %+v", missing.Frames)
	}
	// No manager is selected: the request reaches the service admission,
	// which refuses with code 3 rather than the unknown-action code 2.
	request := wire.NewWriter(9).U32(17).U8(siege.ActionStructureQuery).U32(jangan).Payload()
	routed := rt.HandleFortressInteraction(testDivision, c, request)
	if len(routed.Frames) != 1 || !bytes.Equal(routed.Frames[0].Payload, []byte{siege.ActionStructureQuery, 2, fortressErrInvalid}) {
		t.Fatalf("dispatcher answered %+v", routed.Frames)
	}
}

/*
================
structureByRef

The fixture structure of a kind, by its RefObjID.
================
*/
func structureByRef(t *testing.T, rt *Runtime, refObjID uint32) monster.Instance {
	t.Helper()
	for _, row := range rt.Monsters.WorldStructures(testDivision, instance.Pack(2, 1)) {
		if row.Ref.RefObjID == refObjID {
			return row
		}
	}
	t.Fatalf("no structure %d", refObjID)
	return monster.Instance{}
}

/*
================
TestFortressStructureRepairRestoresFullHP

634950 / 4CFD50 in order: a war refuses 0x18, a zone of another fortress
0x26, an unknown zone 3, a destroyed structure 0x32, a purse that cannot
buy every missing point 0x30 (charging nothing), and an undamaged
structure 0x31. A paid repair restores full hit points for one gold a
point (the fixture's records author no repair price), debits the purse
and answers fortress, zone and the new hit points.
================
*/
func TestFortressStructureRepairRestoresFullHP(t *testing.T) {
	rt, c, _, _ := captureFixture(t)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	stone := structureByRef(t, rt, 19553)
	rt.Monsters.RestoreStructure(testDivision, stone.Gid, 300, 0)
	tower := structureByRef(t, rt, 19536)
	rt.Monsters.RestoreStructure(testDivision, tower.Gid, 0, 1)
	repair := func(fortressID, zone uint32) []byte {
		out := rt.fortressStructureRepair(testDivision, c, siege.Interaction{Action: siege.ActionRepair, Fortress: fortressID, Reference: zone})
		return out.Frames[len(out.Frames)-1].Payload
	}
	refused := func(code uint8) []byte { return []byte{siege.ActionRepair, 2, code} }
	c.Gold = testInt64(100)

	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	if got := repair(jangan, 84); !bytes.Equal(got, refused(fortressErrWarActive)) {
		t.Fatalf("war: % x", got)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, false)
	for _, tc := range []struct {
		name     string
		fortress uint32
		zone     uint32
		code     uint8
	}{
		{"other fortress", jangan + 1, 84, fortressErrOtherFortress},
		{"unknown zone", jangan, 999, fortressErrInvalid},
		{"destroyed", jangan, 85, fortressErrRepairDestroyed},
		{"purse short", jangan, 84, fortressErrRepairGold},
	} {
		if got := repair(tc.fortress, tc.zone); !bytes.Equal(got, refused(tc.code)) {
			t.Fatalf("%s: % x, want code %#x", tc.name, got, tc.code)
		}
	}
	if *c.Gold != 100 || structureByRef(t, rt, 19553).CurrentHP != 300 {
		t.Fatal("a refusal charged or healed")
	}

	c.Gold = testInt64(1000)
	want := wire.NewWriter(14).U8(siege.ActionRepair).U8(1).U32(jangan).U32(84).U32(900).Payload()
	if got := repair(jangan, 84); !bytes.Equal(got, want) {
		t.Fatalf("repair answered % x, want % x", got, want)
	}
	if *c.Gold != 400 || structureByRef(t, rt, 19553).CurrentHP != 900 {
		t.Fatalf("gold %d hp %d after the repair", *c.Gold, structureByRef(t, rt, 19553).CurrentHP)
	}
	if got := repair(jangan, 84); !bytes.Equal(got, refused(fortressErrRepairFull)) {
		t.Fatalf("full: % x", got)
	}
}

/*
================
TestStructureRepairCostFollowsTheRecordPrice

4CFD50 with an authored price: 20000 over 10000 hit points is two gold a
point, so 2500 missing points cost 5000, and a purse one short refuses.
================
*/
func TestStructureRepairCostFollowsTheRecordPrice(t *testing.T) {
	row := monster.Instance{Ref: monster.MonsterRef{MaxHP: 10000, ScaleDenom: 100, Structure: true, RepairPrice: 20000}, CurrentHP: 7500}
	if restored, price, code := structureRepairCost(row, 5000); code != 0 || restored != 2500 || price != 5000 {
		t.Fatalf("restored %d price %d code %#x", restored, price, code)
	}
	if _, _, code := structureRepairCost(row, 4999); code != fortressErrRepairGold {
		t.Fatalf("a short purse answered %#x", code)
	}
}
