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
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
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
0x26, an unknown zone 3, a purse that cannot buy every missing point 0x30
(charging nothing), and an undamaged structure 0x31. A paid repair
restores full hit points for one gold a point (the fixture authors no
CostRepair), debits the purse and answers fortress, zone and the new hit
points. A destroyed tower (CanRevive, CostRevive 100) pays the base, then
its missing points counted from one, and stands again; a destroyed stone
without CanRevive refuses 0x32.
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
		{"purse short", jangan, 84, fortressErrRepairGold},
		{"revive leaves nothing", jangan, 85, fortressErrRepairGold},
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

	// Revive: 100 base, then 499 points (500 less the one it counts as).
	c.Gold = testInt64(1000)
	want = wire.NewWriter(14).U8(siege.ActionRepair).U8(1).U32(jangan).U32(85).U32(500).Payload()
	if got := repair(jangan, 85); !bytes.Equal(got, want) {
		t.Fatalf("revive answered % x, want % x", got, want)
	}
	revived := structureByRef(t, rt, 19536)
	if *c.Gold != 1000-100-499 || revived.CurrentHP != 500 || revived.StructureState != 0 {
		t.Fatalf("gold %d hp %d state %d after the revive", *c.Gold, revived.CurrentHP, revived.StructureState)
	}
	rt.Monsters.RestoreStructure(testDivision, stone.Gid, 0, 1)
	if got := repair(jangan, 84); !bytes.Equal(got, refused(fortressErrRepairDestroyed)) {
		t.Fatalf("a stone without CanRevive: % x", got)
	}
}

/*
================
TestStructureRepairCostFollowsTheRecordPrice

4CFD50 with an authored price: CostRepair 20000 over 10000 hit points is
two gold a point, so 2500 missing points cost 5000, and a purse one short
refuses.
================
*/
func TestStructureRepairCostFollowsTheRecordPrice(t *testing.T) {
	row := monster.Instance{Ref: monster.MonsterRef{MaxHP: 10000, ScaleDenom: 100, Structure: true, CostRepair: 20000}, CurrentHP: 7500}
	if restored, price, code := structureRepairCost(row, 5000); code != 0 || restored != 10000 || price != 5000 {
		t.Fatalf("restored %d price %d code %#x", restored, price, code)
	}
	if _, _, code := structureRepairCost(row, 4999); code != fortressErrRepairGold {
		t.Fatalf("a short purse answered %#x", code)
	}
}

/*
================
gateFixture

Jangan's world with one gate on zone 88 (STRUCTURE_POS_JA_GATE_01) and
the pulley RefObjID that names it.
================
*/
func gateFixture(t *testing.T) (*Runtime, uint32, monster.Instance) {
	t.Helper()
	const gateRef, pulleyRef = 19560, 19566
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			gateRef: {RefObjID: gateRef, MaxHP: 2000, ScaleDenom: 100, Structure: true, TypeID4: structureKindGate},
		},
		[]monster.NestRow{
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: gateRef, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 88},
		},
	)
	rt, c, clock := fortressFixtureWithPopulation(t, testFieldFortGate, template)
	enterFortress(t, rt, c)
	rt.Monsters.AdvancePopulation(clock.NowMs() + monster.NestHiveTickMs)
	rt.gatePulleys = map[uint32]uint32{pulleyRef: 88}
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	return rt, jangan, structureByRef(t, rt, gateRef)
}

/*
================
TestGatePulleyOpensAndShutsItsGate

634C90: a pulley naming no gate answers 0x27 to an open request and 0x28
to a shut one, a gate of another fortress 0x29, a destroyed gate 0x3C;
otherwise the requested word becomes the gate's state and the reply
carries the fortress, the zone and that word.
================
*/
func TestGatePulleyOpensAndShutsItsGate(t *testing.T) {
	rt, jangan, gate := gateFixture(t)
	pull := func(refObjID, fortressID uint32, state uint16) []byte {
		out := rt.fortressGatePulley(testDivision, simulation.NpcDef{RefObjID: refObjID},
			siege.Interaction{Action: siege.ActionGate, Fortress: fortressID, Value16: state})
		return out.Frames[0].Payload
	}
	refused := func(code uint8) []byte { return []byte{siege.ActionGate, 2, code} }
	if got := pull(1, jangan, 2); !bytes.Equal(got, refused(fortressErrGateNoZoneOpen)) {
		t.Fatalf("unknown pulley, open: % x", got)
	}
	if got := pull(1, jangan, 0); !bytes.Equal(got, refused(fortressErrGateNoZoneShut)) {
		t.Fatalf("unknown pulley, shut: % x", got)
	}
	if got := pull(19566, jangan+1, 2); !bytes.Equal(got, refused(fortressErrGateWrongFort)) {
		t.Fatalf("another fortress: % x", got)
	}
	want := wire.NewWriter(12).U8(siege.ActionGate).U8(1).U32(jangan).U32(88).U16(2).Payload()
	if got := pull(19566, jangan, 2); !bytes.Equal(got, want) {
		t.Fatalf("open answered % x, want % x", got, want)
	}
	if state := structureByRef(t, rt, gate.Ref.RefObjID).StructureState; state != 2 {
		t.Fatalf("gate state %d after opening", state)
	}
	if got := pull(19566, jangan, 0); got[1] != 1 || structureByRef(t, rt, gate.Ref.RefObjID).StructureState != 0 {
		t.Fatalf("shut answered % x", got)
	}
	rt.Monsters.RestoreStructure(testDivision, gate.Gid, 0, 3)
	if got := pull(19566, jangan, 2); !bytes.Equal(got, refused(fortressErrGateDestroyed)) {
		t.Fatalf("destroyed gate: % x", got)
	}
}

/*
================
TestGatePulleysLoadFromCharacterData

Jangan's three pulleys name its gate zones 88 to 90 in their column 4;
the other fortresses' zones are not served in v1.150.
================
*/
func TestGatePulleysLoadFromCharacterData(t *testing.T) {
	pulleys, err := loadGatePulleys(licensed.RetailTextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(pulleys) != 3 || pulleys[19566] != 88 || pulleys[19567] != 89 || pulleys[19568] != 90 {
		t.Fatalf("pulleys %v", pulleys)
	}
}
