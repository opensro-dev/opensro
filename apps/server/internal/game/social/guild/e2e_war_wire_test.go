/*
===========================================================================

e2e_war_wire_test.go - the two-master guild-war handshake and surrender

Actual WebSocket packets cross the production consent dispatcher and durable
store. The clock advances explicitly across the native 60-second boundary.

===========================================================================
*/
package guild_test

import (
	"bytes"
	"encoding/binary"
	"github.com/gorilla/websocket"
	wiretest "opensro.online/server/internal/game/internal"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/guildwar"
)

/*
================
TestGuildWarDeclarationCombatAndDelayedSurrenderOverWire
================
*/
func TestGuildWarDeclarationCombatAndDelayedSurrenderOverWire(t *testing.T) {
	var seeds []*enterworld.Character
	for _, name := range []string{"WarMasterA", "WarMasterB", "WarMasterC"} {
		gold, race, gender := int64(1000), int64(enterworld.RaceChina), int64(enterworld.GenderMale)
		seeds = append(seeds, &enterworld.Character{Name: name, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: &race, Gender: &gender, Gold: &gold})
	}
	srv, authority, _, lane := startUnionServer(t, t.TempDir(), seeds)
	var now atomic.Int64
	now.Store(10000)
	lane.Now = func() time.Time { return time.UnixMilli(now.Load()) }
	first, second, stranger := guildE2ECharacter(t, authority, seeds[0].Name), guildE2ECharacter(t, authority, seeds[1].Name), guildE2ECharacter(t, authority, seeds[2].Name)
	a := unionE2EGuild(t, authority, "WarRed", first)
	b := unionE2EGuild(t, authority, "WarBlue", second)
	unionE2EGuild(t, authority, "WarOther", stranger)
	ca, cb, cc := guildDialWS(t, srv), guildDialWS(t, srv), guildDialWS(t, srv)
	guildHelloWS(t, ca)
	guildEnterWorldWithWar(t, ca, first.Name)
	guildHelloWS(t, cb)
	guildEnterWorldWithWar(t, cb, second.Name)
	guildHelloWS(t, cc)
	guildEnterWorldWithWar(t, cc, stranger.Name)
	declaration := wire.NewWriter(32).Str("WarBlue").U8(0).U32(guildwar.UnlimitedPeriod).U8(1).U32(400).Payload()
	wantProposal := wire.NewWriter(32).U8(10).U32(enterworld.ObjectIDForCharacter(first)).Str("WarRed").U8(0).U32(guildwar.UnlimitedPeriod).U8(1).U32(400).Payload()
	guildSendFrame(t, ca, guild.OpGuildWarDeclare, declaration)
	if got := guildExpectFrame(t, cb, guild.OpInvitationProposal, "war proposal"); !bytes.Equal(got, wantProposal) {
		t.Fatalf("proposal % X", got)
	}
	guildSendFrame(t, cb, guild.OpInvitationProposal, []byte{2, 0})
	if got := guildExpectFrame(t, ca, guild.OpGuildWarDeclareResult, "declined war"); !bytes.Equal(got, []byte{2, 0x16}) {
		t.Fatalf("declined % X", got)
	}
	if *first.Gold != 1000 || *second.Gold != 1000 {
		t.Fatal("decline spent stakes")
	}
	guildSendFrame(t, ca, guild.OpGuildWarDeclare, declaration)
	guildExpectFrame(t, cb, guild.OpInvitationProposal, "expiring proposal")
	now.Store(40001)
	if lane.HasPendingInvite(guildE2EDivision, second.Name) {
		t.Fatal("expired proposal remained pending")
	}
	if got := guildExpectFrame(t, ca, guild.OpGuildWarDeclareResult, "proposal timeout"); !bytes.Equal(got, []byte{2, 0x10}) {
		t.Fatalf("timeout % X", got)
	}
	now.Store(10000)
	guildSendFrame(t, ca, guild.OpGuildWarDeclare, declaration)
	guildExpectFrame(t, cb, guild.OpInvitationProposal, "second proposal")
	guildSendFrame(t, cb, guild.OpInvitationProposal, []byte{1, 1})
	guildExpectFrame(t, ca, wire.OpPointsUpdate, "declarer stake")
	guildExpectFrame(t, cb, wire.OpPointsUpdate, "recipient stake")
	begin := guildExpectFrame(t, ca, guild.OpGuildUpdatePush, "declarer begin")
	if len(begin) < 30 || begin[0] != 0x19 || binary.LittleEndian.Uint32(begin[5:]) != guildwar.UnlimitedPeriod || begin[9] != 1 || binary.LittleEndian.Uint32(begin[10:]) != 800 {
		t.Fatalf("begin % X", begin)
	}
	guildExpectFrame(t, cb, guild.OpGuildUpdatePush, "recipient begin")
	if got := guildExpectFrame(t, ca, guild.OpGuildWarDeclareResult, "accepted war"); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("accepted % X", got)
	}
	war, exists := lane.Authority.Find(guildE2EDivision, a, b)
	if !exists || *guildE2ECharacter(t, authority, first.Name).Gold != 600 || *guildE2ECharacter(t, authority, second.Name).Gold != 600 {
		t.Fatal("war and stakes did not commit")
	}
	lane.RecordKill(guildE2EDivision, domain.GuildWarCombat{WarID: war.ID, KillerID: first.ID, VictimID: second.ID, Score: 100}, now.Load())
	for _, conn := range []struct {
		payload []byte
		mode    byte
	}{{guildExpectFrame(t, ca, guild.OpGuildUpdatePush, "killer score"), 1}, {guildExpectFrame(t, cb, guild.OpGuildUpdatePush, "victim score"), 2}} {
		if len(conn.payload) < 14 || conn.payload[0] != 0x1d || conn.payload[1] != conn.mode || binary.LittleEndian.Uint32(conn.payload[6:]) != 100 {
			t.Fatalf("score % X", conn.payload)
		}
	}
	seed, ok := lane.SeedFrame(guildE2EDivision, first)
	if !ok || len(seed.Payload) < 30 || seed.Payload[0] != 1 || uint32(seed.Payload[22])|uint32(seed.Payload[23])<<8|uint32(seed.Payload[24])<<16|uint32(seed.Payload[25])<<24 != 100 {
		t.Fatalf("reentry seed %+v", seed)
	}
	// Explicitly approved exception: a third guild cannot end this war.
	guildSendFrame(t, cc, guild.OpGuildWarSurrender, invE2EU32(war.ID))
	if got := guildExpectFrame(t, cc, guild.OpGuildWarSurrenderResult, "unrelated guild"); !bytes.Equal(got, []byte{2, 2}) {
		t.Fatalf("stranger % X", got)
	}
	guildSendFrame(t, ca, guild.OpGuildWarSurrender, invE2EU32(war.ID))
	if got := guildExpectFrame(t, ca, guild.OpGuildUpdatePush, "own surrender"); !bytes.Equal(got, wire.NewWriter(5).U8(0x1a).U32(war.ID).Payload()) {
		t.Fatalf("own surrender % X", got)
	}
	if got := guildExpectFrame(t, cb, guild.OpGuildUpdatePush, "enemy surrender"); !bytes.Equal(got, wire.NewWriter(5).U8(0x1b).U32(war.ID).Payload()) {
		t.Fatalf("enemy surrender % X", got)
	}
	// Shard jobs belong to the requesting character. The opposing master
	// can also request surrender while the first sixty-second job is alive.
	guildSendFrame(t, cb, guild.OpGuildWarSurrender, invE2EU32(war.ID))
	if got := guildExpectFrame(t, ca, guild.OpGuildUpdatePush, "opposing surrender"); !bytes.Equal(got, wire.NewWriter(5).U8(0x1b).U32(war.ID).Payload()) {
		t.Fatalf("opposing surrender % X", got)
	}
	guildExpectFrame(t, cb, guild.OpGuildUpdatePush, "second own surrender")
	lane.Tick(69999)
	if _, ok := lane.Authority.Find(guildE2EDivision, a, b); !ok {
		t.Fatal("war ended before sixty seconds")
	}
	lane.Tick(70000)
	end := wire.NewWriter(9).U8(0x1c).U32(war.ID).U32(uint32(b)).Payload()
	if got := guildExpectFrame(t, ca, guild.OpGuildUpdatePush, "surrender settlement"); !bytes.Equal(got, end) {
		t.Fatalf("end % X", got)
	}
	guildExpectFrame(t, cb, guild.OpGuildUpdatePush, "winner settlement")
	if got := guildExpectFrame(t, ca, guild.OpGuildWarSurrenderResult, "surrender result"); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("result % X", got)
	}
	lane.Tick(70001)
	winner, _, _ := authority.Guilds().Guild(guildE2EDivision, b)
	if winner.WarCompensation != 800 || len(lane.Authority.Wars(guildE2EDivision, a)) != 0 {
		t.Fatal("settlement not exactly once")
	}
}

/*
================
guildEnterWorldWithWar
================
*/
func guildEnterWorldWithWar(t *testing.T, c *websocket.Conn, name string) {
	t.Helper()
	guildEnterWorldBootstrap(t, c, name)
	guildExpectFrame(t, c, guild.OpGuildInfo, "guild seed")
	guildExpectFrame(t, c, guild.OpGuildWarSeed, "war seed")
	wiretest.ActivateWorld(t, c, "enter world "+name)
	wiretest.AssertQueueDrained(t, c, "war entry tail "+name)
}
