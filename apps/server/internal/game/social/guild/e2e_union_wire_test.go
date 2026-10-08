/*
===========================================================================

e2e_union_wire_test.go - the guild union handshake over the wire

Two level-2 guilds found a union through the 0x7379 proposal and its
0x3393 kind-6 answer, both masters hear the union list, a guildless
player and a guildless target are refused with their codes, the union
survives in the store, and the second guild's leaving dissolves it.

===========================================================================
*/
package guild_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	unionE2ENameE = "e2eUniEcho"
	unionE2ENameF = "e2eUniFox"
	unionE2ENameG = "e2eUniGolf"
)

/*
================
startUnionServer

The invite server's composition plus the union lane as its consent arm.
================
*/
func startUnionServer(t *testing.T, dir string, seeds []*enterworld.Character) (*transport.Server, *store.Store, *guild.UnionRuntime, *guild.WarRuntime) {
	t.Helper()
	authority, err := store.Open(dir, store.Options{DefaultSkills: guildSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	for _, seed := range seeds {
		if err := authority.CreateCharacter(guildE2EDivision, "test-account", seed); err != nil {
			t.Fatal(err)
		}
	}
	deps := &enterworld.Deps{Roster: &enterworld.Roster{}, Characters: authority.Characters()}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(deps.Characters)
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.Letters = authority.Letters()
	deps.Guilds = authority.Guilds()
	srv, err := transport.NewServer(transport.Config{
		WTAddr:            "127.0.0.1:0",
		WSAddr:            "127.0.0.1:0",
		CertDir:           t.TempDir(),
		HelloTimeout:      5 * time.Second,
		GracePeriod:       10 * time.Second,
		KeepaliveInterval: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		OutboundQueue:     64,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	directory := presence.NewDirectory(srv.Hub)
	unions := union.New()
	if err := unions.Restore(guildE2EDivision, authority.Alliances()); err != nil {
		t.Fatal(err)
	}
	lane := guild.NewUnionRuntime(deps, directory, unions, nil)
	warOwner, err := guildwar.New(guildE2EDivision, authority.GuildWars())
	if err != nil {
		t.Fatal(err)
	}
	wars := guild.NewWarRuntime(deps, directory, lane, warOwner)
	wars.Near = func(string, *enterworld.Character, *enterworld.Character) bool { return true }
	lane.GuildWars = warOwner
	communitySeeds := community.SeedFramesFunc(directory, deps.Letters)
	deps.CommunitySeedFramesFor = func(divisionID string, character *enterworld.Character) []enterworld.Packet {
		frames := guild.AppendSeedFrame(communitySeeds(divisionID, character), deps.Guilds, directory, divisionID, character)
		if frame, ok := lane.SeedFrame(divisionID, character); ok {
			frames = append(frames, frame)
		}
		if frame, ok := wars.SeedFrame(divisionID, character); ok {
			frames = append(frames, frame)
		}
		return frames
	}
	parties := party.NewRuntime(deps, directory)
	parties.UseLivePose(func(string, *enterworld.Character) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x6A48, X: 900, Z: 900}
	})
	parties.AddConsentArm(lane)
	parties.AddConsentArm(wars)
	lane.PeerPending = parties.Registry().HasPendingInviteFor
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		srv.Hub.BindExclusive(presence.BindKey(divisionID, character.Name), s)
		parties.WorldBound(s, divisionID, character)
	}
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		parties.SessionClosed(s)
		lane.SessionClosed(s)
		wars.SessionClosed(s)
	})
	enterworld.Register(srv.Hub, deps)
	parties.Register(srv.Hub)
	guild.Register(srv.Hub, deps, directory, nil, lane)
	lane.Register(srv.Hub)
	wars.Register(srv.Hub)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guildShutdownServer(t, srv) })
	return srv, authority, lane, wars
}

/*
================
unionE2EGuild

A level-2 guild led by master.
================
*/
func unionE2EGuild(t *testing.T, authority *store.Store, name string, master *enterworld.Character) int64 {
	t.Helper()
	id, err := authority.Guilds().CreateGuild(guildE2EDivision, enterworld.GuildRecord{Name: name, Level: 2}, enterworld.GuildMemberRecord{
		CharID: master.ID, JID: uint32(500000 + master.ID), Name: master.Name, Grade: 0, PermMask: 0xffffffff, RefObjID: 1907,
	}, master)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

/*
================
unionE2EListOracle

The 0x341E bytes of a two-guild union: id, no emblem, the leading guild,
and a row per guild with its master and one member.
================
*/
func unionE2EListOracle(allianceID, leader int64, rows [][2]any) []byte {
	oracle := &oracle32C4{}
	oracle.u32(uint32(allianceID))
	oracle.u32(0)
	oracle.u32(uint32(leader))
	oracle.u8(uint8(len(rows)))
	for _, row := range rows {
		oracle.u32(uint32(row[0].(int64)))
		names := row[1].([2]string)
		oracle.str(names[0])
		oracle.u8(2)
		oracle.str(names[1])
		oracle.u32(1907)
		oracle.u8(1)
	}
	return oracle.buf.Bytes()
}

/*
================
TestUnionFoundAndDissolveOverWire
================
*/
func TestUnionFoundAndDissolveOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: unionE2ENameE, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: unionE2ENameF, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: unionE2ENameG, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	}
	srv, authority, lane, _ := startUnionServer(t, dir, seeds)
	echo := guildE2ECharacter(t, authority, unionE2ENameE)
	fox := guildE2ECharacter(t, authority, unionE2ENameF)
	golf := guildE2ECharacter(t, authority, unionE2ENameG)
	alpha := unionE2EGuild(t, authority, "UniAlpha", echo)
	bravo := unionE2EGuild(t, authority, "UniBravo", fox)

	connE := guildDialWS(t, srv)
	guildHelloWS(t, connE)
	guildEnterWorldWithWar(t, connE, unionE2ENameE)
	connF := guildDialWS(t, srv)
	guildHelloWS(t, connF)
	guildEnterWorldWithWar(t, connF, unionE2ENameF)
	connG := guildDialWS(t, srv)
	guildHelloWS(t, connG)
	guildEnterWorldSeeds(t, connG, unionE2ENameG)

	// 5C6550: a guildless proposer answers 0x0D, a guildless target 0x25.
	guildSendFrame(t, connG, guild.OpUnionInvite, invE2EU32(enterworld.ObjectIDForCharacter(fox)))
	if got := guildExpectFrame(t, connG, guild.OpUnionInviteResult, "guildless proposer"); !bytes.Equal(got, []byte{2, 0x0D}) {
		t.Fatalf("guildless proposer answered % X", got)
	}
	guildSendFrame(t, connE, guild.OpUnionInvite, invE2EU32(enterworld.ObjectIDForCharacter(golf)))
	if got := guildExpectFrame(t, connE, guild.OpUnionInviteResult, "guildless target"); !bytes.Equal(got, []byte{2, 0x25}) {
		t.Fatalf("guildless target answered % X", got)
	}

	gidE := enterworld.ObjectIDForCharacter(echo)
	guildSendFrame(t, connE, guild.OpUnionInvite, invE2EU32(enterworld.ObjectIDForCharacter(fox)))
	if got := guildExpectFrame(t, connF, guild.OpInvitationProposal, "union prompt"); !bytes.Equal(got, guild.EncodeUnionPrompt3393(gidE)) {
		t.Fatalf("union prompt % X", got)
	}
	guildSendFrame(t, connF, guild.OpInvitationProposal, []byte{1, 1})
	want := unionE2EListOracle(alpha, alpha, [][2]any{
		{alpha, [2]string{"UniAlpha", unionE2ENameE}},
		{bravo, [2]string{"UniBravo", unionE2ENameF}},
	})
	for _, conn := range []struct {
		name string
		list []byte
	}{
		{unionE2ENameE, guildExpectFrame(t, connE, guild.OpUnionList, "leader's union list")},
		{unionE2ENameF, guildExpectFrame(t, connF, guild.OpUnionList, "joiner's union list")},
	} {
		if !bytes.Equal(conn.list, want) {
			t.Fatalf("%s's union list % X, want % X", conn.name, conn.list, want)
		}
	}
	if names, code := lane.UnionChatAudience(guildE2EDivision, guildE2ECharacter(t, authority, unionE2ENameE)); code != 0 || len(names) != 1 || names[0] != unionE2ENameF {
		t.Fatalf("union chat reaches %v (%#x)", names, code)
	}

	stored := union.New()
	if err := stored.Restore(guildE2EDivision, authority.Alliances()); err != nil {
		t.Fatal(err)
	}
	if !stored.Allied(guildE2EDivision, alpha, bravo) {
		t.Fatal("the union is not in the store")
	}

	guildSendFrame(t, connF, guild.OpUnionLeave, nil)
	dissolved := guild.EncodeAllyRemoved3B29(guild.UnionRemovedDissolved, 0)
	if got := guildExpectFrame(t, connE, guild.OpGuildUpdatePush, "leader hears the dissolution"); !bytes.Equal(got, dissolved) {
		t.Fatalf("leader heard % X", got)
	}
	if got := guildExpectFrame(t, connF, guild.OpGuildUpdatePush, "leaver hears the dissolution"); !bytes.Equal(got, dissolved) {
		t.Fatalf("leaver heard % X", got)
	}
	if lane.Unions().Allied(guildE2EDivision, alpha, bravo) {
		t.Fatal("a one-guild union survived")
	}
}
