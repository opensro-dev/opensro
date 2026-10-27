/*
===========================================================================

fortress_construction_test.go - the fortress guild places a barricade
(0x71E1 action 0x0A)

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	testBarricadeRef  = 19565 // STRUCTURE_BARRICADE_01
	testBarricadeZone = 109
	testUnknownRef    = 19999 // a zone whose structure has no reference
	testUnknownZone   = 110
)

/*
================
constructionFixture

The capture fixture's Jangan world plus a vacant barricade site on zone
109 and a site on zone 110 whose structure has no reference, held by guild
77, with the fixture character at a selected fortress aide.
================
*/
func constructionFixture(t *testing.T) (*Runtime, *enterworld.Character, *structureRowStore, uint32) {
	t.Helper()
	rt, c, store, jangan, _ := constructionFixtureWith(t, nil, nil)
	return rt, c, store, jangan
}

/*
================
constructionFixtureWith

constructionFixture with more structure references and nests in the
Jangan world, and its clock.
================
*/
func constructionFixtureWith(t *testing.T, refs map[uint32]monster.MonsterRef, nests []monster.NestRow) (*Runtime, *enterworld.Character, *structureRowStore, uint32, *fakeClock) {
	t.Helper()
	allRefs := map[uint32]monster.MonsterRef{
		19553:            {RefObjID: 19553, MaxHP: 900, ScaleDenom: 100, Structure: true, TypeID4: structureKindFortStone},
		19536:            {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true, TypeID4: structureKindGuardTower},
		testBarricadeRef: {RefObjID: testBarricadeRef, MaxHP: 80000, ScaleDenom: 100, Structure: true, TypeID4: structureKindBarricade},
	}
	for id, ref := range refs {
		allRefs[id] = ref
	}
	template := monster.TemplateFromParts(allRefs, append([]monster.NestRow{
		{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19553, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 84},
		{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62aa, X: 140, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 85},
		{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: testBarricadeRef, RegionID: 0x62aa, X: 180, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: testBarricadeZone, StartVacant: true},
		{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: testUnknownRef, RegionID: 0x62aa, X: 220, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: testUnknownZone, StartVacant: true},
	}, nests...))
	rt, c, clock := fortressFixtureWithPopulation(t, testFieldFortGate, template)
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	guild := int64(77)
	c.GuildID = &guild
	enterFortress(t, rt, c)
	rt.Monsters.AdvancePopulation(clock.NowMs() + monster.NestHiveTickMs)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	rt.Fortresses.Capture(testDivision, jangan, 77, 0)
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 77}, members: []domain.GuildMemberRecord{{CharID: c.ID, FortressRole: fortressRoleCommander}}}
	store := &structureRowStore{rows: map[uint32]domain.FortressStructureRecord{}}
	rt.FortressStore = store
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressAide)
	live := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
	rt.NpcRoster[0].Spawn = live.LiveSpawnAt(clock.NowMs())
	rt.Selected.Set(testDivision, c.Name, rt.NpcRoster[0].ObjectID)
	return rt, c, store, jangan, clock
}

/*
================
construct

One construction completion through the dispatcher, as 703160 sends it.
================
*/
func construct(rt *Runtime, c *enterworld.Character, fortressID, zone uint32) []byte {
	out := rt.HandleFortressInteraction(testDivision, c, wire.NewWriter(9).U8(siege.ActionConstruct).U32(fortressID).U32(zone).Payload())
	if len(out.Frames) == 0 {
		return nil
	}
	return out.Frames[len(out.Frames)-1].Payload
}

/*
================
TestFortressConstructionRefusalOrder

The reference server's construction checks in order: an unknown zone 3, a structure without a reference
0x281E, a structure that is not a barricade 0x281F, a zone of another
fortress 0x2820. A request with no aide selected is refused 3.
================
*/
func TestFortressConstructionRefusalOrder(t *testing.T) {
	rt, c, store, jangan := constructionFixture(t)
	refused := func(code uint8) []byte { return []byte{siege.ActionConstruct, 2, code} }
	cases := []struct {
		name     string
		fortress uint32
		zone     uint32
		want     uint8
	}{
		{"unknown zone", jangan, 999, fortressErrInvalid},
		{"no reference", jangan, testUnknownZone, fortressErrConstructNoReference},
		{"not a barricade", jangan, 85, fortressErrConstructKind},
		{"another fortress", jangan + 1, testBarricadeZone, fortressErrConstructFortress},
	}
	for _, tc := range cases {
		if got := construct(rt, c, tc.fortress, tc.zone); !bytes.Equal(got, refused(tc.want)) {
			t.Errorf("%s: answered % x, want % x", tc.name, got, refused(tc.want))
		}
	}
	rt.Selected.Clear(testDivision, c.Name)
	if got := construct(rt, c, jangan, testBarricadeZone); !bytes.Equal(got, refused(fortressErrInvalid)) {
		t.Errorf("no aide selected: answered % x", got)
	}
	if len(store.rows) != 0 {
		t.Fatalf("a refusal stored a row: %+v", store.rows)
	}
}

/*
================
TestFortressConstructionBuildsTheBarricade

The success reply is {0x0A, 1, fortress, zone}, whoever's guild asks;
the row names the builder's guild. The barricade stands at
full hit points (the build time, Param3, is 0 in every v1.150 barricade
row), its row is stored for the restart, and a second request on the
now occupied zone is refused 2.
================
*/
func TestFortressConstructionBuildsTheBarricade(t *testing.T) {
	rt, c, store, jangan := constructionFixture(t)
	world := instance.Pack(2, 1)
	if _, stands := rt.structureOnZone(testDivision, world, testBarricadeZone); stands {
		t.Fatal("the barricade site did not start vacant")
	}
	// No holder check: a member of another guild builds, and the row is
	// that guild's (the reference server passes the actor's guild).
	builder := int64(78)
	c.GuildID = &builder
	want := wire.NewWriter(10).U8(siege.ActionConstruct).U8(1).U32(jangan).U32(testBarricadeZone).Payload()
	if got := construct(rt, c, jangan, testBarricadeZone); !bytes.Equal(got, want) {
		t.Fatalf("construction answered % x, want % x", got, want)
	}
	built, stands := rt.structureOnZone(testDivision, world, testBarricadeZone)
	if !stands || built.Ref.RefObjID != testBarricadeRef || built.CurrentHP != 80000 {
		t.Fatalf("the barricade does not stand at full hit points: %+v", built)
	}
	row, ok := store.rows[testBarricadeZone]
	if !ok || row.RefObjID != testBarricadeRef || row.FortressID != jangan || row.OwnerGuildID != builder || row.HP != 80000 {
		t.Fatalf("construction not stored: %+v", store.rows)
	}
	if got := construct(rt, c, jangan, testBarricadeZone); !bytes.Equal(got, []byte{siege.ActionConstruct, 2, fortressErrUnknown}) {
		t.Fatalf("an occupied zone answered % x", got)
	}

	// A restart: fresh populations, then the stored rows.
	restarted, _, _, _ := constructionFixture(t)
	restarted.FortressStore = store
	restarted.advanceFortressStructures(restarted.Now().UnixMilli())
	if _, stands := restarted.structureOnZone(testDivision, world, testBarricadeZone); !stands {
		t.Fatal("a restart lost the built barricade")
	}
}

/*
================
TestFortressConstructionIsNotAnOrdinaryRequest

Every other 0x71E1 request leads with the selected NPC's GID, so one
naming it is never read as a construction, whatever byte follows.
================
*/
func TestFortressConstructionIsNotAnOrdinaryRequest(t *testing.T) {
	rt, c, _, jangan := constructionFixture(t)
	aide := rt.NpcRoster[0].ObjectID
	request := wire.NewWriter(9).U32(aide).U8(siege.ActionConstruct).U32(jangan).Payload()
	if _, ok := rt.fortressConstructionRequest(testDivision, c, request); ok {
		t.Fatal("a request naming the selected aide was read as a construction")
	}
	completion := wire.NewWriter(9).U8(siege.ActionConstruct).U32(jangan).U32(testBarricadeZone).Payload()
	if _, ok := rt.fortressConstructionRequest(testDivision, c, completion); !ok {
		t.Fatal("the construction completion was not recognised")
	}
}

/*
================
TestGMSiegeStructureAndPeriodCommands

The GM siege command (/SIEGE STRUCT_BUILD|STRUCT_UPGRADE|STRUCT_REPAIR|GATE) and the shard message
(/SIEGE STARTWAR..ENDREQ) execute without a selected fortress aide and push
their 0xB1E1 / 0x3887 replies.
================
*/
func TestGMSiegeStructureAndPeriodCommands(t *testing.T) {
	rt, c, _, jangan := constructionFixture(t)
	c.GMPrivilege = true
	rt.Selected.Clear(testDivision, c.Name)
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
		pushed = append(pushed, frames...)
	}

	// STRUCT_BUILD (0x0A) builds the vacant barricade without a selected aide;
	// the fortress world (the GM included) hears the roster first, as after
	// any construction, then the GM gets the answer.
	if !rt.SiegeStructureGM(testDivision, c.Name, siege.ActionConstruct, jangan, testBarricadeZone, 0) {
		t.Fatal("GM STRUCT_BUILD failed on vacant barricade zone")
	}
	wantBuild := wire.NewWriter(10).U8(siege.ActionConstruct).U8(1).U32(jangan).U32(testBarricadeZone).Payload()
	if len(pushed) != 2 || pushed[0].Payload[0] != siege.SubtypeStructureRoster || !bytes.Equal(pushed[1].Payload, wantBuild) {
		t.Fatalf("GM STRUCT_BUILD frames = %+v, want the roster then % x", pushed, wantBuild)
	}

	// STRUCT_UPGRADE (0x0B) checks the reference server's refusal codes in order:
	// - unknown zone -> 3 (fortressErrInvalid)
	// - zone without structure reference -> 0x21 (fortressErrUpgradeNoReference)
	// - barricade zone -> 0x22 (fortressErrUpgradeKind)
	// - war active on non-headquarters structure -> 0x18 (fortressErrWarActive)
	// - upgrade index > 3 -> 0x24 (fortressErrUpgradeIndex)
	// - wrong fortress -> 0x23 (fortressErrUpgradeFortress)
	// - full-HP non-barricade structure with "xxx" upgrade target -> 3 (fortressErrInvalid)
	// - damaged structure (CurrentHP != MaxHP) -> 0x39 (fortressErrUpgradeDamaged)
	// - destroyed structure (CurrentHP == 0) -> 3 (fortressErrInvalid)
	upgrade := func(fortress, zone uint32, param uint8) []byte {
		pushed = nil
		if rt.SiegeStructureGM(testDivision, c.Name, siege.ActionUpgrade, fortress, zone, param) {
			t.Fatalf("GM STRUCT_UPGRADE succeeded on fortress=%d zone=%d param=%d", fortress, zone, param)
		}
		if len(pushed) != 1 {
			t.Fatalf("GM STRUCT_UPGRADE frames = %+v", pushed)
		}
		return pushed[0].Payload
	}
	refusedUpgrade := func(code uint8) []byte { return []byte{siege.ActionUpgrade, 2, code} }
	if got := upgrade(jangan, 999, 0); !bytes.Equal(got, refusedUpgrade(fortressErrInvalid)) {
		t.Fatalf("unknown zone upgrade = % x, want % x", got, refusedUpgrade(fortressErrInvalid))
	}
	if got := upgrade(jangan, testUnknownZone, 0); !bytes.Equal(got, refusedUpgrade(fortressErrUpgradeNoReference)) {
		t.Fatalf("no-reference zone upgrade = % x, want % x", got, refusedUpgrade(fortressErrUpgradeNoReference))
	}
	if got := upgrade(jangan, testBarricadeZone, 0); !bytes.Equal(got, refusedUpgrade(fortressErrUpgradeKind)) {
		t.Fatalf("barricade zone upgrade = % x, want % x", got, refusedUpgrade(fortressErrUpgradeKind))
	}
	if !rt.SiegePeriodGM(testDivision, 1) || !rt.Fortresses.WarActive(testDivision) {
		t.Fatal("GM STARTWAR did not activate war period")
	}
	if got := upgrade(jangan, 85, 0); !bytes.Equal(got, refusedUpgrade(fortressErrWarActive)) {
		t.Fatalf("war-active upgrade = % x, want % x", got, refusedUpgrade(fortressErrWarActive))
	}
	if !rt.SiegePeriodGM(testDivision, 2) || rt.Fortresses.WarActive(testDivision) {
		t.Fatal("GM ENDWAR did not clear war period")
	}
	if got := upgrade(jangan, 85, 4); !bytes.Equal(got, refusedUpgrade(fortressErrUpgradeIndex)) {
		t.Fatalf("param>3 upgrade = % x, want % x", got, refusedUpgrade(fortressErrUpgradeIndex))
	}
	if got := upgrade(jangan+1, 85, 0); !bytes.Equal(got, refusedUpgrade(fortressErrUpgradeFortress)) {
		t.Fatalf("wrong-fortress upgrade = % x, want % x", got, refusedUpgrade(fortressErrUpgradeFortress))
	}
	if got := upgrade(jangan, 85, 3); !bytes.Equal(got, refusedUpgrade(fortressErrInvalid)) {
		t.Fatalf("full-HP non-upgradable upgrade = % x, want % x", got, refusedUpgrade(fortressErrInvalid))
	}
	tower := structureByRef(t, rt, 19536)
	rt.Monsters.RestoreStructure(testDivision, tower.Gid, 200, 0)
	if got := upgrade(jangan, 85, 0); !bytes.Equal(got, refusedUpgrade(fortressErrUpgradeDamaged)) {
		t.Fatalf("damaged-structure upgrade = % x, want % x", got, refusedUpgrade(fortressErrUpgradeDamaged))
	}
	rt.Monsters.RestoreStructure(testDivision, tower.Gid, 0, 1)
	if got := upgrade(jangan, 85, 0); !bytes.Equal(got, refusedUpgrade(fortressErrInvalid)) {
		t.Fatalf("destroyed-structure upgrade = % x, want % x", got, refusedUpgrade(fortressErrInvalid))
	}

	// GATE (0x15) toggles gate state on gateFixture (zone 88) and pushes 0xB1E1.
	gateRT, gateFortress, _ := gateFixture(t)
	gateChars := gateRT.deps.(*enterworld.Deps).Characters.(enterworld.StaticCharacterSource)[testDivision]
	gateChars[0].GMPrivilege = true
	pushed = nil
	gateRT.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
		pushed = append(pushed, frames...)
	}
	if !gateRT.SiegeStructureGM(testDivision, gateChars[0].Name, siege.ActionGate, gateFortress, 88, 2) {
		t.Fatal("GM GATE open failed")
	}
	wantGate := wire.NewWriter(12).U8(siege.ActionGate).U8(1).U32(gateFortress).U32(88).U16(2).Payload()
	if len(pushed) != 2 || pushed[0].Opcode != opFortressWarState || !bytes.Equal(pushed[1].Payload, wantGate) {
		t.Fatalf("GM GATE frames = %+v, want % x", pushed, wantGate)
	}

	// STARTWAR (1) and ENDWAR (2) toggle WarActive.
	if !rt.SiegePeriodGM(testDivision, 1) || !rt.Fortresses.WarActive(testDivision) {
		t.Fatal("GM STARTWAR did not activate war period")
	}
	if !rt.SiegePeriodGM(testDivision, 2) || rt.Fortresses.WarActive(testDivision) {
		t.Fatal("GM ENDWAR did not clear war period")
	}
}
