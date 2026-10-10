package guild_test

// End-to-end exercise of guild BREAK (0x766E) and the grants (0x72BC /
// 0x765F) over the REAL transport against the REAL authority store, on
// the e2e_guild_wire_test.go composition helpers:
//
//	G creates    -> a SYNTHETIC 0x7663 founds the guild over the wire
//	                (G is the grade-0 leader); the store door raises the
//	                guild to level 4 (the pinned name-grant arm gate);
//	H seeded in  -> membership installed through the store doors with a
//	                jid distinct from uint32(id);
//	G grants     -> with BOTH members online, G's 0x72BC answers 0xB2BC
//	                to G ONLY and fans the subOp-6 &0x20 delta to H
//	                ONLY; G's 0x765F answers 0xB65F and fans &0x40 the
//	                same way - the actor-exclusion is proven by G's
//	                very NEXT frame after each ack being the next
//	                elicited answer (guildExpectFrame fails loud on any
//	                interleaved 0x3B29);
//	reboot       -> G's fresh 0x32C4 seed carries the granted title AND
//	                the fortress role on H's row (both mutations
//	                persisted through the guild door);
//	G breaks     -> with both online again, G's 0x766E answers 0xB66E
//	                {01} to G then fans ONE payload-less 0x3B29 subOp-1
//	                frame to both live members;
//	G relogs     -> NO 0x32C4 rides G's seeds (every FK cleared by the
//	                atomic dissolution door), proven by G's next
//	                elicited frame being the 0xB663 answer to a fresh
//	                create - which also pins the watermark at id 2
//	                (the dissolved id 1 is never reissued).

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/transport"
)

const (
	grantE2ENameG = "e2eGldGolf"
	grantE2ENameH = "e2eGldHotel"
	grantE2EJIDH  = uint32(800000) // + the store id; distinct from uint32(id) to pin the STORED jid
)

func grantE2ENameGrantPayload(targetJID uint32, name string) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, targetJID)
	mutE2ESized(buf, name)
	return buf.Bytes()
}

func grantE2EPositionGrantPayload(targetJID uint32, position uint8) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, targetJID)
	buf.WriteByte(position)
	return buf.Bytes()
}

// grantE2ESeedOracle hand-rolls the post-grant 0x32C4 block: guild id 1
// "GrantBanner" at level 4, G the grade-0 leader and H carrying the
// granted title + fortress role.
func grantE2ESeedOracle(golfID int64, hotelJID uint32, golfOffline, hotelOffline uint8) []byte {
	oracle := &oracle32C4{}
	oracle.u32(1)
	oracle.str("GrantBanner")
	oracle.u8(4)  // the raised level (the name-grant gate)
	oracle.u32(0) // GP
	oracle.str("")
	oracle.str("")
	oracle.u32(0) // crestParam
	oracle.u8(0)  // byte10
	oracle.u8(2)  // memberCount
	oracle.u32(uint32(golfID))
	oracle.str(grantE2ENameG)
	oracle.u8(0) // grade: leader
	oracle.u8(1) // level (the seeded e2e characters persist none)
	oracle.u32(0)
	oracle.u32(0xffffffff)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("")
	oracle.u32(1907)
	oracle.u8(1) // fortressRole: the leader is the commander
	oracle.u8(golfOffline)
	oracle.u32(hotelJID)
	oracle.str(grantE2ENameH)
	oracle.u8(3) // grade
	oracle.u8(1) // level
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("Warden") // the persisted grant
	oracle.u32(1907)
	oracle.u8(0x10) // the persisted fortress role
	oracle.u8(hotelOffline)
	oracle.u8(0) // voteCount
	return oracle.buf.Bytes()
}

func TestGuildBreakAndGrantsEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: grantE2ENameG, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: grantE2ENameH, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	}
	first := startGuildServer(t, dir, seeds)
	golf := guildE2ECharacter(t, first.authority, grantE2ENameG)
	hotel := guildE2ECharacter(t, first.authority, grantE2ENameH)

	// ---- G enters guildless and founds the guild over the wire ----
	connGolf := guildDialWS(t, first.srv)
	guildHelloWS(t, connGolf)
	guildEnterWorldSeeds(t, connGolf, grantE2ENameG)
	guildSendFrame(t, connGolf, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "GrantBanner"))
	ack := guildExpectFrame(t, connGolf, guild.OpGuildCreateAck, "create ack")
	wantAck := append([]byte{0x01}, mutE2EBlockOracle(1, "GrantBanner", []mutE2EMember{
		{jid: uint32(golf.ID), name: grantE2ENameG, grade: 0, perm: 0xffffffff, offline: 0},
	})...)
	if !bytes.Equal(ack, wantAck) {
		t.Fatalf("0xB663 = % X, want the oracle % X", ack, wantAck)
	}

	// ---- store doors: raise the level past the name-grant gate and
	// seed H into the guild ----
	const guildID = int64(1)
	updateGuildForTest(first.authority.Guilds(), guildE2EDivision, golf.ID, "grant-e2e-level", func(record enterworld.GuildRecord, members []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		record.Level = 4
		return record, members
	})
	hotelJID := grantE2EJIDH + uint32(hotel.ID)
	if !addGuildMemberForTest(first.authority.Guilds(), guildE2EDivision, guildID, golf.ID, enterworld.GuildMemberRecord{
		CharID: hotel.ID, JID: hotelJID, Name: grantE2ENameH, Grade: 3, Level: 1, RefObjID: 1907,
	}) {
		t.Fatal("fixture member join refused")
	}

	// ---- H enters ONLINE; G grants the title then the position ----
	connHotel := guildDialWS(t, first.srv)
	guildHelloWS(t, connHotel)
	guildEnterWorld(t, connHotel, grantE2ENameH)

	guildSendFrame(t, connGolf, guild.OpGuildNameGrantRequest, grantE2ENameGrantPayload(hotelJID, "Warden"))
	wantB2BC := &bytes.Buffer{}
	wantB2BC.WriteByte(0x01)
	binary.Write(wantB2BC, binary.LittleEndian, hotelJID)
	binary.Write(wantB2BC, binary.LittleEndian, hotelJID)
	mutE2ESized(wantB2BC, "Warden")
	if got := guildExpectFrame(t, connGolf, guild.OpGuildNameGrantAck, "granter's 0xB2BC"); !bytes.Equal(got, wantB2BC.Bytes()) {
		t.Fatalf("0xB2BC = % X, want % X", got, wantB2BC.Bytes())
	}
	wantNameDelta := &bytes.Buffer{}
	wantNameDelta.WriteByte(0x06)
	binary.Write(wantNameDelta, binary.LittleEndian, hotelJID)
	wantNameDelta.WriteByte(0x20)
	mutE2ESized(wantNameDelta, "Warden")
	if got := guildExpectFrame(t, connHotel, guild.OpGuildUpdatePush, "target's subOp-6 &0x20"); !bytes.Equal(got, wantNameDelta.Bytes()) {
		t.Fatalf("target's 0x3B29 = % X, want % X", got, wantNameDelta.Bytes())
	}

	guildSendFrame(t, connGolf, guild.OpGuildPositionGrantRequest, grantE2EPositionGrantPayload(hotelJID, 0x10))
	// The actor-exclusion witness: G's very NEXT frame is the 0xB65F
	// ack - guildExpectFrame fails loud had the &0x20 delta landed on
	// the granter too.
	wantB65F := &bytes.Buffer{}
	wantB65F.WriteByte(0x01)
	binary.Write(wantB65F, binary.LittleEndian, hotelJID)
	binary.Write(wantB65F, binary.LittleEndian, hotelJID)
	wantB65F.WriteByte(0x10)
	if got := guildExpectFrame(t, connGolf, guild.OpGuildPositionGrantAck, "granter's 0xB65F"); !bytes.Equal(got, wantB65F.Bytes()) {
		t.Fatalf("0xB65F = % X, want % X", got, wantB65F.Bytes())
	}
	wantRoleDelta := &bytes.Buffer{}
	wantRoleDelta.WriteByte(0x06)
	binary.Write(wantRoleDelta, binary.LittleEndian, hotelJID)
	wantRoleDelta.WriteByte(0x40)
	wantRoleDelta.WriteByte(0x10)
	if got := guildExpectFrame(t, connHotel, guild.OpGuildUpdatePush, "target's subOp-6 &0x40"); !bytes.Equal(got, wantRoleDelta.Bytes()) {
		t.Fatalf("target's 0x3B29 = % X, want % X", got, wantRoleDelta.Bytes())
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	guildSendFrame(t, connGolf, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connHotel, transport.OpBye, []byte{transport.ByeReasonNormal})
	connGolf.Close()
	connHotel.Close()

	// ---- reboot: both grants persisted through the guild door ----
	guildShutdownServer(t, first.srv)
	first.authority.Close()
	second := startGuildServer(t, dir, nil)
	golf2 := guildE2ECharacter(t, second.authority, grantE2ENameG)

	connGolf2 := guildDialWS(t, second.srv)
	guildHelloWS(t, connGolf2)
	seed := guildEnterWorld(t, connGolf2, grantE2ENameG)
	wantSeed := grantE2ESeedOracle(golf2.ID, hotelJID, 1, 1)
	if !bytes.Equal(seed, wantSeed) {
		t.Fatalf("post-reboot 0x32C4 = % X, want the granted state % X", seed, wantSeed)
	}

	// ---- H enters ONLINE, then G breaks: ack to G, subOp-1 to both ----
	connHotel2 := guildDialWS(t, second.srv)
	guildHelloWS(t, connHotel2)
	guildEnterWorld(t, connHotel2, grantE2ENameH)

	guildSendFrame(t, connGolf2, guild.OpGuildBreakRequest, leaveE2EPayload(0))
	if got := guildExpectFrame(t, connGolf2, guild.OpGuildBreakAck, "leader's 0xB66E"); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("leader's 0xB66E = % X, want [01]", got)
	}
	// ONE payload-less subOp-1 frame serves both live members - the
	// client's break arm reads the announced guild name from its OWN
	// local state and drops the whole guild block.
	if got := guildExpectFrame(t, connGolf2, guild.OpGuildUpdatePush, "leader's subOp 1"); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("leader's 0x3B29 = % X, want [01]", got)
	}
	if got := guildExpectFrame(t, connHotel2, guild.OpGuildUpdatePush, "member's subOp 1"); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("member's 0x3B29 = % X, want the SAME frame [01]", got)
	}
	guildSendFrame(t, connGolf2, transport.OpBye, []byte{transport.ByeReasonNormal})
	connGolf2.Close()

	// ---- G relogs: NO 0x32C4, and a fresh create answers as guild 2 ----
	connGolf3 := guildDialWS(t, second.srv)
	guildHelloWS(t, connGolf3)
	guildEnterWorldSeeds(t, connGolf3, grantE2ENameG)
	guildSendFrame(t, connGolf3, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "AfterBreak"))
	// The very NEXT frame is the create ack: no 0x32C4 rode the seeds
	// (guildExpectFrame fails loud on any other opcode), the leader's
	// FK is clear (a linked character's create refuses), and the
	// watermark allocates 2 - the dissolved id 1 is never reissued.
	ack2 := guildExpectFrame(t, connGolf3, guild.OpGuildCreateAck, "post-break create ack")
	wantAck2 := append([]byte{0x01}, mutE2EBlockOracle(2, "AfterBreak", []mutE2EMember{
		{jid: uint32(golf2.ID), name: grantE2ENameG, grade: 0, perm: 0xffffffff, offline: 0},
	})...)
	if !bytes.Equal(ack2, wantAck2) {
		t.Fatalf("post-break 0xB663 = % X, want the oracle % X", ack2, wantAck2)
	}

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	guildSendFrame(t, connHotel2, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connGolf3, transport.OpBye, []byte{transport.ByeReasonNormal})
}
