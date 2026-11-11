/*
===========================================================================

e2e_memberstatus_wire_test.go - tests for memberstatus.go over the wire

===========================================================================
*/

package guild_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/transport"
)

/*
================
memberStatusEnter

Enters name in the world and returns its 0x32C4 seed, then reads the
login push to its own row, which follows the world bind.
================
*/
func memberStatusEnter(t *testing.T, c *websocket.Conn, name string, selfJID uint32) []byte {
	t.Helper()
	guildEnterWorldBootstrap(t, c, name)
	seed := guildExpectFrame(t, c, guild.OpGuildInfo, "guild info seed "+name)
	wiretest.ActivateWorld(t, c, "enter world "+name)
	self := guildExpectFrame(t, c, guild.OpGuildUpdatePush, "own login push "+name)
	if want := guild.EncodeMemberOnline3B29(selfJID, 0); !bytes.Equal(self, want) {
		t.Fatalf("%s's own login push = % X, want % X", name, self, want)
	}
	wiretest.AssertQueueDrained(t, c, "world-bound tail "+name)
	return seed
}

/*
================
TestGuildMemberOnlineStatusFollowsLoginAndLogout

The roster's offline byte follows the members: a login flips the row to
online for every online member, the member itself included, and a logout
flips it back for the others. Without the pushes the roster froze at
seed time, and the entering member read itself offline.
================
*/
func TestGuildMemberOnlineStatusFollowsLoginAndLogout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	level := func(value int64) *int64 { return &value }
	seeds := []*enterworld.Character{
		{Name: guildE2ENameA, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: level(12)},
		{Name: guildE2ENameB, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: level(6)},
	}
	server := startGuildServerWith(t, dir, seeds, true)
	alfa := guildE2ECharacter(t, server.authority, guildE2ENameA)
	bravo := guildE2ECharacter(t, server.authority, guildE2ENameB)
	alfaJID, bravoJID := uint32(500000+alfa.ID), uint32(500000+bravo.ID)
	guildID, err := server.authority.Guilds().CreateGuild(guildE2EDivision, enterworld.GuildRecord{
		Name: "WallWatch", Level: 1, GP: 5000, NoticeSubject: "e2e subject", NoticeContents: "e2e contents", CrestParam: 0x1234,
	}, enterworld.GuildMemberRecord{
		CharID: alfa.ID, JID: alfaJID, Name: guildE2ENameA, Grade: 0, Level: 12, DonatedGP: 300, PermMask: 0xffffffff, GrantName: "Warden", RefObjID: 1907,
	}, alfa)
	if err != nil {
		t.Fatalf("fixture guild create: %v", err)
	}
	if !addGuildMemberForTest(server.authority.Guilds(), guildE2EDivision, guildID, alfa.ID, enterworld.GuildMemberRecord{
		CharID: bravo.ID, JID: bravoJID, Name: guildE2ENameB, Grade: 3, Level: 6, DonatedGP: 80, RefObjID: 1907,
	}) {
		t.Fatal("fixture member join refused")
	}

	connAlfa := guildDialWS(t, server.srv)
	guildHelloWS(t, connAlfa)
	if got, want := memberStatusEnter(t, connAlfa, guildE2ENameA, alfaJID), guildE2EOracle(alfa.ID, bravo.ID, 1, 1); !bytes.Equal(got, want) {
		t.Fatalf("A's seed = % X, want % X", got, want)
	}

	connBravo := guildDialWS(t, server.srv)
	guildHelloWS(t, connBravo)
	if got, want := memberStatusEnter(t, connBravo, guildE2ENameB, bravoJID), guildE2EOracle(alfa.ID, bravo.ID, 0, 1); !bytes.Equal(got, want) {
		t.Fatalf("B's seed = % X, want A online % X", got, want)
	}
	if got, want := guildExpectFrame(t, connAlfa, guild.OpGuildUpdatePush, "B's login at A"), guild.EncodeMemberOnline3B29(bravoJID, 0); !bytes.Equal(got, want) {
		t.Fatalf("B's login at A = % X, want % X", got, want)
	}

	guildSendFrame(t, connBravo, transport.OpBye, []byte{transport.ByeReasonNormal})
	if got, want := guildExpectFrame(t, connAlfa, guild.OpGuildUpdatePush, "B's logout at A"), guild.EncodeMemberOnline3B29(bravoJID, 1); !bytes.Equal(got, want) {
		t.Fatalf("B's logout at A = % X, want % X", got, want)
	}
	wiretest.AssertQueueDrained(t, connAlfa, "after B's logout")
	guildSendFrame(t, connAlfa, transport.OpBye, []byte{transport.ByeReasonNormal})
}
