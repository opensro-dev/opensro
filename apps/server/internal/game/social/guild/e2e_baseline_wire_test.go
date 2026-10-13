/*
===========================================================================

e2e_baseline_wire_test.go - a guild delta skips a session without the block

The joiner race over the wire: D is online without a guild block when the
store makes D a member, as a join commit is visible before the joiner's
0x32C4 is queued. The master's notice edit then fans its subOp-5 delta to
the roster: the master, holding the block from its 0xB663, receives it; D
receives none.

===========================================================================
*/

package guild_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/transport"
)

/*
================
framesUntilPong

Every frame D receives before the answer to a ping it sends now: the ping
fences everything queued to D earlier.
================
*/
func framesUntilPong(t *testing.T, c *websocket.Conn) []uint16 {
	t.Helper()
	guildSendFrame(t, c, transport.OpPing, []byte{9})
	var opcodes []uint16
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("reading until pong: %v", err)
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatal(err)
		}
		if f.Opcode == transport.OpPong {
			return opcodes
		}
		opcodes = append(opcodes, f.Opcode)
	}
}

/*
================
TestGuildDeltaSkipsASessionWithoutTheBlock
================
*/
func TestGuildDeltaSkipsASessionWithoutTheBlock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	server := startGuildServer(t, dir, []*enterworld.Character{
		{Name: guildMutE2ENameC, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: guildMutE2ENameD, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	})
	charlie := guildE2ECharacter(t, server.authority, guildMutE2ENameC)
	delta := guildE2ECharacter(t, server.authority, guildMutE2ENameD)

	connCharlie := guildDialWS(t, server.srv)
	guildHelloWS(t, connCharlie)
	guildEnterWorldSeeds(t, connCharlie, guildMutE2ENameC)
	connDelta := guildDialWS(t, server.srv)
	guildHelloWS(t, connDelta)
	guildEnterWorldSeeds(t, connDelta, guildMutE2ENameD)

	guildSendFrame(t, connCharlie, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "SteelBanner"))
	if ack := guildExpectFrame(t, connCharlie, guild.OpGuildCreateAck, "create ack"); len(ack) == 0 || ack[0] != 1 {
		t.Fatalf("create refused % X", ack)
	}
	// The join commits, but D's session never received this guild's block.
	if !addGuildMemberForTest(server.authority.Guilds(), guildE2EDivision, 1, charlie.ID, enterworld.GuildMemberRecord{
		CharID: delta.ID, JID: guildMutE2EJIDD + uint32(delta.ID), Name: guildMutE2ENameD, Grade: 3, Level: 1, RefObjID: 1907,
	}) {
		t.Fatal("fixture member join refused")
	}

	guildSendFrame(t, connCharlie, guild.OpGuildNoticeEditRequest, mutatorNoticePayload("Changed", "Body"))
	for {
		if f := guildNextFrame(t, connCharlie, "the master's notice delta"); f.Opcode == guild.OpGuildUpdatePush {
			break
		}
	}
	for _, opcode := range framesUntilPong(t, connDelta) {
		if opcode == guild.OpGuildUpdatePush {
			t.Fatal("a guild delta reached a session that holds no block of the guild")
		}
	}
}
