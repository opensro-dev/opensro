package guild_test

// End-to-end exercise of GP donate (0x740F) over the REAL transport
// against the REAL authority store, on the e2e_guild_wire_test.go
// composition helpers:
//
//	G creates    -> a SYNTHETIC 0x7663 founds the guild over the wire
//	                (G is the grade-0 leader); the store doors seed H
//	                as a member and grant G a 500-SP pool;
//	G donates    -> with BOTH members online, G's 0x740F {u32 120}
//	                answers 0xB40F {01}{u32 120} to G, then fans the
//	                subOp-5 &0x08 guild-GP delta AND the subOp-6 &0x08
//	                donated-GP delta to BOTH live members - the donor
//	                INCLUDED (the deltas are the actor's only state
//	                source; guildExpectFrame pins the exact per-session
//	                frame order);
//	reboot       -> G's fresh 0x32C4 seed carries the credited guild GP
//	                and G's member-row DonatedGP, and G's stored SP is
//	                debited (all three legs of the ONE atomic commit
//	                persisted);
//	G donates 80 -> the post-reboot deltas carry the ACCUMULATED totals
//	                (guild GP 200 / donated 200), with H offline this
//	                time - no frame reaches a dead session.

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
	donateE2ENameG = "e2eGldGamma"
	donateE2ENameH = "e2eGldHind"
	donateE2EJIDH  = uint32(810000) // + the store id; distinct from uint32(id)
)

func donateE2EPayload(amount uint32) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, amount)
	return buf.Bytes()
}

func donateE2EAckOracle(amount uint32) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(0x01)
	binary.Write(buf, binary.LittleEndian, amount)
	return buf.Bytes()
}

func donateE2EGuildGpOracle(newGuildGp uint32) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(0x05)
	buf.WriteByte(0x08)
	binary.Write(buf, binary.LittleEndian, newGuildGp)
	return buf.Bytes()
}

func donateE2EDonorGpOracle(jid uint32, newDonatedGp uint32) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(0x06)
	binary.Write(buf, binary.LittleEndian, jid)
	buf.WriteByte(0x08)
	binary.Write(buf, binary.LittleEndian, newDonatedGp)
	return buf.Bytes()
}

// donateE2ESeedOracle hand-rolls the post-donation 0x32C4 block: guild
// id 1 "DonateBanner" carrying the credited GP, G the grade-0 leader
// with the credited member-row DonatedGP, H untouched.
func donateE2ESeedOracle(golfID int64, hotelJID uint32, guildGp, golfDonated uint32, golfOffline, hotelOffline uint8) []byte {
	oracle := &oracle32C4{}
	oracle.u32(1)
	oracle.str("DonateBanner")
	oracle.u8(1) // level (the create DECISION default)
	oracle.u32(guildGp)
	oracle.str("")
	oracle.str("")
	oracle.u32(0) // crestParam
	oracle.u8(0)  // byte10
	oracle.u8(2)  // memberCount
	oracle.u32(uint32(golfID))
	oracle.str(donateE2ENameG)
	oracle.u8(0) // grade: leader
	oracle.u8(1) // level (the seeded e2e characters persist none)
	oracle.u32(golfDonated)
	oracle.u32(0xffffffff)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("")
	oracle.u32(1907)
	oracle.u8(1) // fortressRole: the leader is the commander
	oracle.u8(golfOffline)
	oracle.u32(hotelJID)
	oracle.str(donateE2ENameH)
	oracle.u8(3) // grade
	oracle.u8(1) // level
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("")
	oracle.u32(1907)
	oracle.u8(0) // fortressRole
	oracle.u8(hotelOffline)
	oracle.u8(0) // voteCount
	return oracle.buf.Bytes()
}

func TestGuildGpDonateEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: donateE2ENameG, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: donateE2ENameH, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	}
	first := startGuildServer(t, dir, seeds)
	golf := guildE2ECharacter(t, first.authority, donateE2ENameG)
	hotel := guildE2ECharacter(t, first.authority, donateE2ENameH)

	// ---- G enters guildless and founds the guild over the wire ----
	connGolf := guildDialWS(t, first.srv)
	guildHelloWS(t, connGolf)
	guildEnterWorldSeeds(t, connGolf, donateE2ENameG)
	guildSendFrame(t, connGolf, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "DonateBanner"))
	ack := guildExpectFrame(t, connGolf, guild.OpGuildCreateAck, "create ack")
	wantAck := append([]byte{0x01}, mutE2EBlockOracle(1, "DonateBanner", []mutE2EMember{
		{jid: uint32(golf.ID), name: donateE2ENameG, grade: 0, perm: 0xffffffff, offline: 0},
	})...)
	if !bytes.Equal(ack, wantAck) {
		t.Fatalf("0xB663 = % X, want the oracle % X", ack, wantAck)
	}

	// ---- store doors: seed H into the guild and grant G the SP pool ----
	const guildID = int64(1)
	hotelJID := donateE2EJIDH + uint32(hotel.ID)
	if !addGuildMemberForTest(first.authority.Guilds(), guildE2EDivision, guildID, golf.ID, enterworld.GuildMemberRecord{
		CharID: hotel.ID, JID: hotelJID, Name: donateE2ENameH, Grade: 3, Level: 1, RefObjID: 1907,
	}) {
		t.Fatal("fixture member join refused")
	}
	first.authority.MutateCharacter(golf, "donate-e2e-sp", func() { sp := int64(500); golf.SkillPoints = &sp })

	// ---- H enters ONLINE; G donates 120 SP into GP ----
	connHotel := guildDialWS(t, first.srv)
	guildHelloWS(t, connHotel)
	guildEnterWorld(t, connHotel, donateE2ENameH)

	guildSendFrame(t, connGolf, guild.OpGuildGpDonateRequest, donateE2EPayload(120))
	if got := guildExpectFrame(t, connGolf, guild.OpGuildGpDonateAck, "donor's 0xB40F"); !bytes.Equal(got, donateE2EAckOracle(120)) {
		t.Fatalf("0xB40F = % X, want % X", got, donateE2EAckOracle(120))
	}
	// The donor-INCLUSION witness: G's next two frames are the subOp-5
	// guild-GP delta then the subOp-6 donated-GP delta (the ack carries
	// no state, so the donor rides both fan-outs).
	if got := guildExpectFrame(t, connGolf, guild.OpGuildUpdatePush, "donor's subOp-5 &0x08"); !bytes.Equal(got, donateE2EGuildGpOracle(120)) {
		t.Fatalf("donor's subOp-5 = % X, want % X", got, donateE2EGuildGpOracle(120))
	}
	if got := guildExpectFrame(t, connGolf, guild.OpGuildUpdatePush, "donor's subOp-6 &0x08"); !bytes.Equal(got, donateE2EDonorGpOracle(uint32(golf.ID), 120)) {
		t.Fatalf("donor's subOp-6 = % X, want % X", got, donateE2EDonorGpOracle(uint32(golf.ID), 120))
	}
	if got := guildExpectFrame(t, connHotel, guild.OpGuildUpdatePush, "member's subOp-5 &0x08"); !bytes.Equal(got, donateE2EGuildGpOracle(120)) {
		t.Fatalf("member's subOp-5 = % X, want % X", got, donateE2EGuildGpOracle(120))
	}
	if got := guildExpectFrame(t, connHotel, guild.OpGuildUpdatePush, "member's subOp-6 &0x08"); !bytes.Equal(got, donateE2EDonorGpOracle(uint32(golf.ID), 120)) {
		t.Fatalf("member's subOp-6 = % X, want % X", got, donateE2EDonorGpOracle(uint32(golf.ID), 120))
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	guildSendFrame(t, connGolf, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connHotel, transport.OpBye, []byte{transport.ByeReasonNormal})
	connGolf.Close()
	connHotel.Close()

	// ---- reboot: all three legs of the atomic commit persisted ----
	guildShutdownServer(t, first.srv)
	first.authority.Close()
	second := startGuildServer(t, dir, nil)
	golf2 := guildE2ECharacter(t, second.authority, donateE2ENameG)
	if golf2.SkillPoints == nil || *golf2.SkillPoints != 380 {
		t.Fatalf("post-reboot donor SP = %v, want 380 (500 - 120)", golf2.SkillPoints)
	}

	connGolf2 := guildDialWS(t, second.srv)
	guildHelloWS(t, connGolf2)
	seed := guildEnterWorld(t, connGolf2, donateE2ENameG)
	wantSeed := donateE2ESeedOracle(golf2.ID, hotelJID, 120, 120, 1, 1)
	if !bytes.Equal(seed, wantSeed) {
		t.Fatalf("post-reboot 0x32C4 = % X, want the donated state % X", seed, wantSeed)
	}

	// ---- a second donation ACCUMULATES; H offline gets nothing ----
	guildSendFrame(t, connGolf2, guild.OpGuildGpDonateRequest, donateE2EPayload(80))
	if got := guildExpectFrame(t, connGolf2, guild.OpGuildGpDonateAck, "post-reboot 0xB40F"); !bytes.Equal(got, donateE2EAckOracle(80)) {
		t.Fatalf("post-reboot 0xB40F = % X, want % X", got, donateE2EAckOracle(80))
	}
	if got := guildExpectFrame(t, connGolf2, guild.OpGuildUpdatePush, "post-reboot subOp-5"); !bytes.Equal(got, donateE2EGuildGpOracle(200)) {
		t.Fatalf("post-reboot subOp-5 = % X, want the accumulated % X", got, donateE2EGuildGpOracle(200))
	}
	if got := guildExpectFrame(t, connGolf2, guild.OpGuildUpdatePush, "post-reboot subOp-6"); !bytes.Equal(got, donateE2EDonorGpOracle(uint32(golf2.ID), 200)) {
		t.Fatalf("post-reboot subOp-6 = % X, want the accumulated % X", got, donateE2EDonorGpOracle(uint32(golf2.ID), 200))
	}

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	guildSendFrame(t, connGolf2, transport.OpBye, []byte{transport.ByeReasonNormal})
	connGolf2.Close()
}
