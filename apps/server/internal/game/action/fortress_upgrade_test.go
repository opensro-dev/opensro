/*
===========================================================================

fortress_upgrade_test.go - the fortress manager raises a structure
(0x71E1 action 0x0B)

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	testPositionRef0 = 22522 // a level-0 defensive position
	testPositionRef1 = 22523 // its level-1 build
	testPositionRef2 = 22524 // level 2
	testPositionZone = 130
	testPositionGold = 5000
	testPositionGP   = 500
	testLevel1Gold   = 10000
	testLevel1GP     = 1000
	testLevel1Build  = 10080 // a week, in minutes
)

/*
================
upgradeGuilds

A guild store holding one guild whose points the upgrade spends.
================
*/
type upgradeGuilds struct {
	fortressGuilds
	gp *uint32
}

/*
================
upgradeGuilds.Guild
================
*/
func (g upgradeGuilds) Guild(division string, id int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	record, members, ok := g.fortressGuilds.Guild(division, id)
	record.GP = *g.gp
	return record, members, ok
}

/*
================
upgradeGuilds.UpdateGuildAs
================
*/
func (g upgradeGuilds) UpdateGuildAs(_ string, _ int64, _ string, _ domain.GuildAuthorization,
	update func(domain.GuildRecord, []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool),
) (domain.GuildSnapshot, domain.GuildRefusal) {
	record := g.guild
	record.GP = *g.gp
	next, members, ok := update(record, g.members)
	if !ok {
		return domain.GuildSnapshot{}, domain.GuildRefusalUpdateRejected
	}
	*g.gp = next.GP
	return domain.GuildSnapshot{Guild: next, Members: members}, domain.GuildRefusalNone
}

/*
================
upgradeFixture

The construction fixture plus a level-0 defensive position standing on
zone 130 whose table row raises it to level 1, and level 1 to level 2, a
guild with points and a member with gold.
================
*/
func upgradeFixture(t *testing.T) (*Runtime, *enterworld.Character, *structureRowStore, uint32, *uint32, *fakeClock) {
	t.Helper()
	positions := map[uint32]monster.MonsterRef{
		testPositionRef0: {RefObjID: testPositionRef0, Codename: "STRUCTURE_SMALL_DEFENSIVE_POSITION_00", MaxHP: 400, ScaleDenom: 100,
			Structure: true, TypeID4: 4, UpgradeGold: testPositionGold, UpgradeGuildPoints: testPositionGP},
		testPositionRef1: {RefObjID: testPositionRef1, Codename: "STRUCTURE_SMALL_DEFENSIVE_POSITION_01", MaxHP: 800, ScaleDenom: 100,
			Structure: true, TypeID4: 4, Level: 1, UpgradeGold: testLevel1Gold, UpgradeGuildPoints: testLevel1GP, BuildMinutes: testLevel1Build},
		testPositionRef2: {RefObjID: testPositionRef2, Codename: "STRUCTURE_SMALL_DEFENSIVE_POSITION_02", MaxHP: 1600, ScaleDenom: 100,
			Structure: true, TypeID4: 4, Level: 2},
	}
	rt, c, store, jangan, clock := constructionFixtureWith(t, positions, []monster.NestRow{
		{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: testPositionRef0, RegionID: 0x62aa, X: 260, Y: 20, Z: 100},
			PolicyPinned: true, MaxCount: 1, EventStructID: testPositionZone},
	})
	rt.structureUpgrades = structureUpgradeTable{
		"STRUCTURE_SMALL_DEFENSIVE_POSITION_00": {"STRUCTURE_SMALL_DEFENSIVE_POSITION_01"},
		"STRUCTURE_SMALL_DEFENSIVE_POSITION_01": {"STRUCTURE_SMALL_DEFENSIVE_POSITION_02"},
	}
	gp := uint32(100000)
	rt.Guilds = upgradeGuilds{fortressGuilds: rt.Guilds.(fortressGuilds), gp: &gp}
	gold := int64(1000000)
	c.Gold = &gold
	return rt, c, store, jangan, &gp, clock
}

/*
================
upgradeRequest

One 0x71E1 action 0x0B at the selected aide, as 703130 sends it.
================
*/
func upgradeRequest(rt *Runtime, c *enterworld.Character, fortressID, zone uint32, index uint8) OpResult {
	npc, _ := rt.Selected.Get(testDivision, c.Name)
	return rt.HandleFortressInteraction(testDivision, c,
		wire.NewWriter(14).U32(npc).U8(siege.ActionUpgrade).U32(fortressID).U32(zone).U8(index).Payload())
}

/*
================
upgradeAnswer
================
*/
func upgradeAnswer(t *testing.T, out OpResult) []byte {
	t.Helper()
	for _, f := range out.Frames {
		if f.Opcode == opFortressInteractionResult {
			return f.Payload
		}
	}
	t.Fatalf("no 0xB1E1 in %+v", out.Frames)
	return nil
}

/*
================
TestStructureUpgradeBuildsADefensivePosition

The map window's construction: type 0 on a level-0 defensive position.
The member pays the level-0 structure's gold, the guild its points, the
zone now holds level 1, the row is made now, and the reply names fortress,
zone and type.
================
*/
func TestStructureUpgradeBuildsADefensivePosition(t *testing.T) {
	rt, c, store, jangan, gp, clock := upgradeFixture(t)
	out := upgradeRequest(rt, c, jangan, testPositionZone, 0)
	want := wire.NewWriter(11).U8(siege.ActionUpgrade).U8(1).U32(jangan).U32(testPositionZone).U8(0).Payload()
	if got := upgradeAnswer(t, out); !bytes.Equal(got, want) {
		t.Fatalf("answer % x, want % x", got, want)
	}
	if *c.Gold != 1000000-testPositionGold || *gp != 100000-testPositionGP {
		t.Fatalf("gold %d guild points %d, want %d and %d", *c.Gold, *gp, 1000000-testPositionGold, 100000-testPositionGP)
	}
	built := structureByRef(t, rt, testPositionRef1)
	if built.Nest.EventStructID != testPositionZone || built.CurrentHP != 800 {
		t.Fatalf("zone holds %+v, want level 1 at full hit points", built)
	}
	row := store.rows[testPositionZone]
	if row.RefObjID != testPositionRef1 || row.MadeAtMs != clock.NowMs() {
		t.Fatalf("stored row %+v, want level 1 made at %d", row, clock.NowMs())
	}
}

/*
================
TestStructureUpgradeWaitsOutTheBuildMinutes

Level 1 stands a week before it may be raised again: the list reports the
minutes left, an upgrade inside them refuses 0x2837, and a week later it
goes through.
================
*/
func TestStructureUpgradeWaitsOutTheBuildMinutes(t *testing.T) {
	rt, c, _, jangan, _, clock := upgradeFixture(t)
	if got := upgradeAnswer(t, upgradeRequest(rt, c, jangan, testPositionZone, 0)); got[1] != 1 {
		t.Fatalf("build answer % x", got)
	}
	built := structureByRef(t, rt, testPositionRef1)
	if left := rt.structureMinutesLeft(testDivision, jangan, built); left != testLevel1Build {
		t.Fatalf("minutes left %d, want %d", left, testLevel1Build)
	}
	if got := upgradeAnswer(t, upgradeRequest(rt, c, jangan, testPositionZone, 0)); !bytes.Equal(got, []byte{siege.ActionUpgrade, 2, fortressErrUpgradeTooSoon}) {
		t.Fatalf("early upgrade % x, want the 0x2837 refusal", got)
	}
	clock.Advance(testLevel1Build * time.Minute)
	if left := rt.structureMinutesLeft(testDivision, jangan, built); left != 0 {
		t.Fatalf("minutes left a week later %d, want 0", left)
	}
	if got := upgradeAnswer(t, upgradeRequest(rt, c, jangan, testPositionZone, 0)); got[1] != 1 {
		t.Fatalf("upgrade a week later % x, want success", got)
	}
	structureByRef(t, rt, testPositionRef2)
}

/*
================
TestStructureUpgradeRefusalsAfterTheTarget

After the target, in order: a member who is not commander or battle
commander (0x2833), short of gold (0x2835), a guild short of points
(0x2836). Nothing is paid on a refusal.
================
*/
func TestStructureUpgradeRefusalsAfterTheTarget(t *testing.T) {
	rt, c, _, jangan, gp, _ := upgradeFixture(t)
	refused := func(code uint8) []byte { return []byte{siege.ActionUpgrade, 2, code} }
	guilds := rt.Guilds.(upgradeGuilds)
	guilds.members = []domain.GuildMemberRecord{{CharID: c.ID}}
	rt.Guilds = guilds
	if got := upgradeAnswer(t, upgradeRequest(rt, c, jangan, testPositionZone, 0)); !bytes.Equal(got, refused(fortressErrUpgradeRole)) {
		t.Fatalf("plain member % x, want the role refusal", got)
	}
	guilds.members = []domain.GuildMemberRecord{{CharID: c.ID, FortressRole: fortressRoleBattleCommander}}
	rt.Guilds = guilds
	poor := int64(testPositionGold - 1)
	c.Gold = &poor
	if got := upgradeAnswer(t, upgradeRequest(rt, c, jangan, testPositionZone, 0)); !bytes.Equal(got, refused(fortressErrUpgradeGold)) {
		t.Fatalf("short of gold % x, want the gold refusal", got)
	}
	rich := int64(1000000)
	c.Gold = &rich
	*gp = testPositionGP - 1
	if got := upgradeAnswer(t, upgradeRequest(rt, c, jangan, testPositionZone, 0)); !bytes.Equal(got, refused(fortressErrUpgradeGuildPoints)) {
		t.Fatalf("guild short of points % x, want the guild-point refusal", got)
	}
	if *c.Gold != 1000000 || *gp != testPositionGP-1 {
		t.Fatalf("a refusal paid: gold %d points %d", *c.Gold, *gp)
	}
	structureByRef(t, rt, testPositionRef0)
}

/*
================
TestStructureMadeTimeSurvivesTheTickSave

The periodic save rewrites a structure's row from the live structure; it
keeps the made time the upgrade stamped.
================
*/
func TestStructureMadeTimeSurvivesTheTickSave(t *testing.T) {
	rt, c, store, jangan, _, clock := upgradeFixture(t)
	upgradeRequest(rt, c, jangan, testPositionZone, 0)
	made := store.rows[testPositionZone].MadeAtMs
	built := structureByRef(t, rt, testPositionRef1)
	rt.Monsters.RestoreStructure(testDivision, built.Gid, 100, 0)
	clock.Advance(time.Minute)
	rt.saveFortressStructures(testDivision)
	if row := store.rows[testPositionZone]; row.HP != 100 || row.MadeAtMs != made {
		t.Fatalf("saved row %+v, want hp 100 and made at %d", row, made)
	}
}

/*
================
TestStructureUpgradesLoadFromTheClientTable

The shipped siegestructupgradedata.txt: a level-0 defensive position
builds into level 1, a guard tower turns into four element towers, and
"xxx" is no target.
================
*/
func TestStructureUpgradesLoadFromTheClientTable(t *testing.T) {
	table, err := loadStructureUpgrades(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := table["STRUCTURE_SMALL_DEFENSIVE_POSITION_00"]; got != [4]string{"STRUCTURE_SMALL_DEFENSIVE_POSITION_01"} {
		t.Fatalf("defensive position targets %q", got)
	}
	want := [4]string{"STRUCTURE_LIGHTNING_TOWER_02", "STRUCTURE_BLAZE_TOWER_02", "STRUCTURE_FREEZING_TOWER_02", "STRUCTURE_DARK_TOWER_02"}
	if got := table["STRUCTURE_GUARD_TOWER_01"]; got != want {
		t.Fatalf("guard tower targets %q, want %q", got, want)
	}
	if _, listed := table["STRUCTURE_BARRICADE_01"]; listed {
		t.Fatal("a barricade has an upgrade row")
	}
}
