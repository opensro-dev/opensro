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
		enterworld.GuildMemberRecord{CharID: c.ID, JID: 1, Name: c.Name, Grade: 0, PermMask: 0xffffffff, FortressRole: guild.FortressRoleCommander}, c); err != nil {
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
		CharID: heir.ID, JID: 2, Name: heir.Name, Grade: guild.JoinerGrade, FortressRole: guild.FortressRoleGuard}); refusal.Refused() {
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
	var peerFrames []wire.Frame
	d.rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
		if division != testDivision || name != heir.Name {
			t.Fatalf("handover recipient %s/%s", division, name)
		}
		peerFrames = append(peerFrames, frames...)
	}
	out := leave(2)
	assertOpcodes(t, out.Frames, opGuildMasterLeaveDone, guild.OpGuildUpdatePush, guild.OpGuildUpdatePush, guild.OpGuildUpdatePush, guild.OpGuildUpdatePush)
	want := [][]byte{
		guild.EncodeMemberGrade3B29(1, guild.JoinerGrade, guild.JoinerPermMask),
		guild.EncodeMemberGrade3B29(2, guild.LeaderGrade, guild.LeaderPermMask),
		guild.EncodeMemberFortressRole3B29(1, 0),
		guild.EncodeMemberFortressRole3B29(2, guild.FortressRoleCommander),
	}
	if len(peerFrames) != len(want) {
		t.Fatalf("peer received %d frames, want %d", len(peerFrames), len(want))
	}
	for i, payload := range want {
		if !bytes.Equal(out.Frames[i+1].Payload, payload) || !bytes.Equal(peerFrames[i].Payload, payload) {
			t.Fatalf("handover update %d: actor %x peer %x want %x", i, out.Frames[i+1].Payload, peerFrames[i].Payload, payload)
		}
	}
	_, members, _ := guilds.Guild(testDivision, guildID)
	for _, member := range members {
		if member.JID == 2 && member.Grade != 0 || member.JID == 1 && member.Grade != guild.JoinerGrade {
			t.Fatalf("members after the hand-over %+v", members)
		}
		if member.JID == 2 && member.FortressRole != guild.FortressRoleCommander || member.JID == 1 && member.FortressRole != 0 {
			t.Fatalf("roles after the hand-over %+v", members)
		}
	}
	if out := leave(1); !bytes.Equal(out.Frames[0].Payload, []byte{2, guildNpcRefused}) {
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
	guilds, guildID, now, release := longGoneMasterFixture(t, d)
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
TestTheVoteRowLearnsTheCandidates

0x7332 from the vote row (5DA1B0 case 0x1A): with no open vote it is
refused [2][0x06] (v1.188 0x5006); with one, 0xB332 carries the vote's
id, time left and every candidate with its ballots, as the native vote writer writes them,
and a ballot is counted in the next answer.
================
*/
func TestTheVoteRowLearnsTheCandidates(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	guilds, guildID, now, release := longGoneMasterFixture(t, d)
	ask := func(voteID uint32) []byte {
		out := d.rt.HandleGuildVoteParticipate(testDivision, c, wire.NewWriter(4).U32(voteID).Payload())
		assertOpcodes(t, out.Frames, opGuildVoteJoinDone)
		return out.Frames[0].Payload
	}
	if got := ask(1); !bytes.Equal(got, []byte{2, ballotErrNoVote}) {
		t.Fatalf("no open vote answered %x", got)
	}
	d.rt.HandleGuildMasterRelease(testDivision, c, release)
	record, _, _ := guilds.Guild(testDivision, guildID)
	vote := record.Vote
	if vote == nil || len(vote.Candidates) == 0 {
		t.Fatalf("fixture vote %+v has no candidates", vote)
	}
	want := wire.NewWriter(16).U8(1).U32(vote.ID).U32(uint32(vote.EndsAtUnixMs - now)).U8(uint8(len(vote.Candidates)))
	for _, candidate := range vote.Candidates {
		want.U32(candidate).U8(0)
	}
	if got := ask(vote.ID); !bytes.Equal(got, want.Payload()) {
		t.Fatalf("candidates %x, want %x", got, want.Payload())
	}
	d.rt.HandleGuildBallot(testDivision, c, wire.NewWriter(9).U32(guildManagerGid).U32(vote.ID).U8(0).Payload())
	if got := ask(vote.ID); got[len(got)-1-5*(len(vote.Candidates)-1)] != 1 {
		t.Fatalf("the first candidate's ballot is not counted: %x", got)
	}
	if got := ask(vote.ID + 1); !bytes.Equal(got, []byte{2, ballotErrNoVote}) {
		t.Fatalf("another vote id answered %x", got)
	}
}

/*
================
longGoneMasterFixture

A guild whose master has been gone 46 days, with the fixture character a
member seen now: what a release vote needs. Returns the guild door, the
guild, the clock and the release request.
================
*/
func longGoneMasterFixture(t *testing.T, d *doorRuntime) (enterworld.GuildStore, int64, int64, []byte) {
	t.Helper()
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
	return guilds, guildID, now, release
}

/*
================
TestGuildWarehouseOpensAtLevelTwoForOneMember

Natively level 1 refuses with 0x4A; at level 2 the master holds the room,
lists 30 slots and deposits gold into it, whatever is selected meanwhile.
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
	// Another selection leaves the held room usable (the session, not the
	// selection, names the manager; BR-261011-1342-EE58).
	d.rt.Selected.Set(testDivision, c.Name, guildManagerGid+1)
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

/*
================
TestGuildDeltasGoThroughTheGuildPublication

The master's handover publishes its four 0x3B29 deltas to the heir through
PushGuildFrames, naming the guild, so a session without that guild's block
never receives them (guild/baseline.go); none goes out as plain character
frames.
================
*/
func TestGuildDeltasGoThroughTheGuildPublication(t *testing.T) {
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
		CharID: heir.ID, JID: 2, Name: heir.Name, Grade: guild.JoinerGrade, FortressRole: guild.FortressRoleGuard}); refusal.Refused() {
		t.Fatalf("fixture join %v", refusal)
	}
	d.rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode == guild.OpGuildUpdatePush {
				t.Fatal("a guild delta bypassed the guild publication")
			}
		}
	}
	var published []int64
	d.rt.PushGuildFrames = func(division, name string, id int64, frames []wire.Frame) {
		if division != testDivision || name != heir.Name {
			t.Fatalf("delta recipient %s/%s", division, name)
		}
		published = append(published, id)
	}
	out := d.rt.HandleGuildMasterLeave(testDivision, c, wire.NewWriter(8).U32(guildManagerGid).U32(2).Payload())
	if out.Frames[0].Opcode != opGuildMasterLeaveDone || len(published) != 1 || published[0] != guildID {
		t.Fatalf("handover published %v for guild %d", published, guildID)
	}
}
