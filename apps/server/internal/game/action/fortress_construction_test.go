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
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			19553:            {RefObjID: 19553, MaxHP: 900, ScaleDenom: 100, Structure: true, TypeID4: structureKindFortStone},
			19536:            {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true, TypeID4: structureKindGuardTower},
			testBarricadeRef: {RefObjID: testBarricadeRef, MaxHP: 80000, ScaleDenom: 100, Structure: true, TypeID4: structureKindBarricade},
		},
		[]monster.NestRow{
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19553, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 84},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62aa, X: 140, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 85},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: testBarricadeRef, RegionID: 0x62aa, X: 180, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: testBarricadeZone, StartVacant: true},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: testUnknownRef, RegionID: 0x62aa, X: 220, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: testUnknownZone, StartVacant: true},
		},
	)
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
	return rt, c, store, jangan
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

6341B0 in order: an unknown zone 3, a structure without a reference
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
	// that guild's (6226B0 passes the actor's guild).
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
