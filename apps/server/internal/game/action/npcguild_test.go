/*
===========================================================================

npcguild_test.go - the guild manager's level-up through the store door

===========================================================================
*/

package action

import (
	"bytes"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
)

// guildManagerGid is the fixture guild manager's object id.
const guildManagerGid = 4001

/*
================
guildManagerFixture

A store-backed character who masters a level 1 guild holding 6000 GP,
beside a selected NPC_EU_GUILD.
================
*/
func guildManagerFixture(t *testing.T) *doorRuntime {
	t.Helper()
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	deps := d.rt.deps.(*enterworld.Deps)
	deps.Guilds = d.authority.Guilds()
	c := d.character
	if _, err := deps.Guilds.CreateGuild(testDivision, enterworld.GuildRecord{Name: "Lanterns", Level: 1, GP: 6000},
		enterworld.GuildMemberRecord{CharID: c.ID, JID: 1, Name: c.Name, Grade: 0, PermMask: 0xffffffff}, c); err != nil {
		t.Fatal(err)
	}
	npc := simulation.NpcDef{ObjectID: guildManagerGid, RefObjID: 7600, Codename: "NPC_EU_GUILD",
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}
	npc.Services = simulation.ResolveNpcServices(npc)
	d.rt.NpcRoster = []simulation.NpcDef{npc}
	d.rt.NpcSpawn.Enabled = true
	d.rt.Selected.Set(testDivision, c.Name, guildManagerGid)
	return d
}

/*
================
TestGuildManagerLevelsTheMastersGuild
================
*/
func TestGuildManagerLevelsTheMastersGuild(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	request := wire.NewWriter(4).U32(guildManagerGid).Payload()
	if out := d.rt.HandleGuildLevelUp(testDivision, c, request); !bytes.Equal(out.Frames[0].Payload, []byte{2, guild.GuildErrLevelUpGoldDeficit}) {
		t.Fatalf("a master without gold answered %x", out.Frames[0].Payload)
	}
	gold := int64(3000000)
	if !d.authority.UpdateCharacters([]*enterworld.Character{c}, "fixture-gold", func() bool { c.Gold = &gold; return true }) {
		t.Fatal("fixture gold refused")
	}
	out := d.rt.HandleGuildLevelUp(testDivision, c, request)
	assertOpcodes(t, out.Frames, opGuildLevelUpResponse, guild.OpGuildUpdatePush, wire.OpPointsUpdate)
	if !bytes.Equal(out.Frames[1].Payload, guild.EncodeGuildLevel3B29(2, 600)) || goldOf(c) != 0 {
		t.Fatalf("level up pushed %x, gold %d", out.Frames[1].Payload, goldOf(c))
	}
	d.rt.Selected.Set(testDivision, c.Name, guildManagerGid+1)
	if out := d.rt.HandleGuildLevelUp(testDivision, c, request); !bytes.Equal(out.Frames[0].Payload, []byte{2, guildNpcRefused}) {
		t.Fatalf("an unselected manager answered %x", out.Frames[0].Payload)
	}
}

/*
================
TestGuildMasterHandsTheGuildToAMember
================
*/
func TestGuildMasterHandsTheGuildToAMember(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	heir := testCharacter()
	heir.Name = "Heir"
	if err := d.authority.CreateCharacter(testDivision, "heir-account", heir); err != nil {
		t.Fatal(err)
	}
	guilds := d.authority.Guilds()
	guildID, _ := guilds.GuildOfCharacter(testDivision, c.ID)
	if _, refusal := guilds.AddGuildMemberAs(testDivision, guildID, c.ID, 0, enterworld.GuildMemberRecord{
		CharID: heir.ID, JID: 2, Name: heir.Name, Grade: guild.JoinerGrade}); refusal.Refused() {
		t.Fatalf("fixture join %v", refusal)
	}
	leave := func(target uint32) OpResult {
		return d.rt.HandleGuildMasterLeave(testDivision, c, wire.NewWriter(8).U32(guildManagerGid).U32(target).Payload())
	}
	if out := leave(99); !bytes.Equal(out.Frames[0].Payload, []byte{2, guild.GuildErrMemberNotFound}) {
		t.Fatalf("an absent member answered %x", out.Frames[0].Payload)
	}
	d.rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	d.rt.Fortresses.Occupy(testDivision, 1, guildID)
	d.rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	if out := leave(2); !bytes.Equal(out.Frames[0].Payload, []byte{2, guildMasterFortressWar}) {
		t.Fatalf("fortress master transfer % X", out.Frames[0].Payload)
	}
	d.rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, false)
	out := leave(2)
	assertOpcodes(t, out.Frames, opGuildMasterLeaveDone, guild.OpGuildUpdatePush, guild.OpGuildUpdatePush)
	_, members, _ := guilds.Guild(testDivision, guildID)
	for _, member := range members {
		if member.JID == 2 && member.Grade != 0 || member.JID == 1 && member.Grade != guild.JoinerGrade {
			t.Fatalf("members after the hand-over %+v", members)
		}
	}
	if out := leave(1); !bytes.Equal(out.Frames[0].Payload, []byte{2, guild.GuildErrPermissionDenied}) {
		t.Fatalf("a former master answered %x", out.Frames[0].Payload)
	}
}

/*
================
TestGuildMasterCollectsWarCompensation
================
*/
func TestGuildMasterCollectsWarCompensation(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	request := wire.NewWriter(4).U32(guildManagerGid).Payload()
	if out := d.rt.HandleGuildCompensation(testDivision, c, request); !bytes.Equal(out.Frames[0].Payload, []byte{2, guild.GuildErrNoCompensation}) {
		t.Fatalf("an unowed guild answered %x", out.Frames[0].Payload)
	}
	guilds := d.authority.Guilds()
	if _, refusal := guilds.UpdateGuildAs(testDivision, c.ID, "fixture-compensation", enterworld.GuildAuthorization{},
		func(g enterworld.GuildRecord, m []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			g.WarCompensation = 70000
			return g, m, true
		}); refusal.Refused() {
		t.Fatalf("fixture compensation %v", refusal)
	}
	if out := d.rt.HandleGuildCompensation(testDivision, c, request); !bytes.Equal(out.Frames[0].Payload, wire.NewWriter(5).U8(1).U32(70000).Payload()) {
		t.Fatalf("the quote answered %x", out.Frames[0].Payload)
	}
	before := goldOf(c)
	out := d.rt.HandleGuildCompensationClaim(testDivision, c, request)
	assertOpcodes(t, out.Frames, opGuildCompensationPaid, wire.OpPointsUpdate)
	if goldOf(c) != before+70000 {
		t.Fatalf("gold %d after the claim", goldOf(c))
	}
	if out := d.rt.HandleGuildCompensationClaim(testDivision, c, request); !bytes.Equal(out.Frames[0].Payload, []byte{2, guild.GuildErrNoCompensation}) {
		t.Fatalf("a second claim answered %x", out.Frames[0].Payload)
	}
}

/*
================
TestMemberCallsAReleaseVoteOnALongGoneMaster
================
*/
func TestMemberCallsAReleaseVoteOnALongGoneMaster(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	master := testCharacter()
	master.Name = "Gone"
	if err := d.authority.CreateCharacter(testDivision, "gone-account", master); err != nil {
		t.Fatal(err)
	}
	guilds := d.authority.Guilds()
	guildID, _ := guilds.GuildOfCharacter(testDivision, c.ID)
	if _, refusal := guilds.AddGuildMemberAs(testDivision, guildID, c.ID, 0, enterworld.GuildMemberRecord{
		CharID: master.ID, JID: 2, Name: master.Name, Grade: guild.JoinerGrade}); refusal.Refused() {
		t.Fatalf("fixture join %v", refusal)
	}
	d.rt.HandleGuildMasterLeave(testDivision, c, wire.NewWriter(8).U32(guildManagerGid).U32(2).Payload())
	now := d.rt.Now().UnixMilli()
	release := wire.NewWriter(4).U32(guildManagerGid).Payload()
	if out := d.rt.HandleGuildMasterRelease(testDivision, c, release); !bytes.Equal(out.Frames[0].Payload, []byte{2, guild.GuildErrVoteNotTime}) {
		t.Fatalf("a master never seen leaving was released: %x", out.Frames[0].Payload)
	}
	gone := now - 46*24*60*60*1000
	if !d.authority.UpdateCharacters([]*enterworld.Character{master, c}, "fixture-seen", func() bool {
		master.LastSeenUnixMs, c.LastSeenUnixMs = gone, now
		return true
	}) {
		t.Fatal("fixture last-seen refused")
	}
	out := d.rt.HandleGuildMasterRelease(testDivision, c, release)
	assertOpcodes(t, out.Frames, opGuildMasterReleaseDone, opGuildVotePush)
	record, _, _ := guilds.Guild(testDivision, guildID)
	if record.Vote == nil || !bytes.Equal(out.Frames[1].Payload, guild.EncodeVoteOpened3A6C(record.Vote, now)) {
		t.Fatalf("vote %+v push %x", record.Vote, out.Frames[1].Payload)
	}
	ballot := wire.NewWriter(9).U32(guildManagerGid).U32(record.Vote.ID).U8(0).Payload()
	out = d.rt.HandleGuildBallot(testDivision, c, ballot)
	if !bytes.Equal(out.Frames[1].Payload, guild.EncodeVoteBallot3A6C(record.Vote.ID, 0xff, 0, 1)) {
		t.Fatalf("ballot push %x", out.Frames[1].Payload)
	}
	wrong := wire.NewWriter(9).U32(guildManagerGid).U32(record.Vote.ID + 1).U8(0).Payload()
	if out := d.rt.HandleGuildBallot(testDivision, c, wrong); !bytes.Equal(out.Frames[0].Payload, []byte{2, ballotErrNoVote}) {
		t.Fatalf("a stale vote answered %x", out.Frames[0].Payload)
	}
}

/*
================
TestGuildWarehouseOpensAtLevelTwoForOneMember

5C7440: level 1 refuses with 0x4A; at level 2 the master holds the room,
lists 30 slots and deposits gold into it.
================
*/
func TestGuildWarehouseOpensAtLevelTwoForOneMember(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	request := wire.NewWriter(4).U32(guildManagerGid).Payload()
	if out := d.rt.HandleGuildStorageOpen(testDivision, c, request); !bytes.Equal(out.Frames[0].Payload, []byte{2, guildStorageErrLevel}) {
		t.Fatalf("a level 1 guild opened its warehouse: %x", out.Frames[0].Payload)
	}
	guilds := d.authority.Guilds()
	if _, refusal := guilds.UpdateGuildAs(testDivision, c.ID, "fixture-level", enterworld.GuildAuthorization{},
		func(g enterworld.GuildRecord, m []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			g.Level = 2
			return g, m, true
		}); refusal.Refused() {
		t.Fatalf("fixture level %v", refusal)
	}
	d.rt.characterAdmissions.Store(simulation.WorldKey(testDivision, c.Name), populationAdmission{division: testDivision, name: c.Name})
	if out := d.rt.HandleGuildStorageOpen(testDivision, c, request); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("open answered %x", out.Frames[0].Payload)
	}
	list := d.rt.HandleGuildStorageList(testDivision, c, request)
	assertOpcodes(t, list.Frames, opCommerceItemReferences, wire.OpGuildStorageGold, wire.OpGuildStorageList, opGuildStorageListed)
	if list.Frames[2].Payload[0] != 30 {
		t.Fatalf("a level 2 room holds %d slots", list.Frames[2].Payload[0])
	}
	gold := int64(5000)
	if !d.authority.UpdateCharacters([]*enterworld.Character{c}, "fixture-gold", func() bool { c.Gold = &gold; return true }) {
		t.Fatal("fixture gold refused")
	}
	d.rt.Selected.Set(testDivision, c.Name, guildManagerGid)
	out := d.rt.applyGuildStorageMove(testDivision, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeGuildStorageGoldDeposit, GoldAmount: 1200})
	if out.Frames[0].Payload[0] != 1 || goldOf(c) != 3800 {
		t.Fatalf("deposit %+v gold %d", out.Frames, goldOf(c))
	}
	guildID, _ := guilds.GuildOfCharacter(testDivision, c.ID)
	if record, _, _ := guilds.Guild(testDivision, guildID); record.Storage == nil || record.Storage.Gold != 1200 {
		t.Fatalf("guild room %+v", record.Storage)
	}
	d.rt.HandleGuildStorageClose(testDivision, c, request)
	if out := d.rt.applyGuildStorageMove(testDivision, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeGuildStorageGoldWithdraw, GoldAmount: 1}); out.Frames[0].Payload[0] == 1 {
		t.Fatal("a closed room paid out")
	}
}
