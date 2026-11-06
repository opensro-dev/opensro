/*
===========================================================================

buff_test.go - the Academy cumulated buff over the real transport (#2270)

Earning lands in the ledger through the action lane's kill hook,
persists on exit, shows again at entry (state 1), passes to the
Guardian (0x700C) as a received row that drains for the kill bonus,
refreshes on the minute pulse at the cadence crossings, reports its
expiry (state 7) and clears at the graduation band (state 8).

===========================================================================
*/

package mentor_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
campEnterWorldExpectBuff

campEnterWorldWithSeed, but the caller expects the entry buff legs
(0x3CC1 / 0xB00C) between the camp seed and the drained queue.
================
*/
func campEnterWorldExpectBuff(t *testing.T, c *websocket.Conn, name string) {
	t.Helper()
	campSendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, campE2EDivision, name),
	))
	campConsumeEnterWorldTail(t, c, name)
	campExpectFrame(t, c, mentor.OpTCStatus, "camp seed for "+name)
}

/*
================
campBuffMember

The stored buff-bearing member record of one camp.
================
*/
func campBuffMember(t *testing.T, env campE2EServer, campID, charID int64) domain.TrainingCampMemberRecord {
	t.Helper()
	_, members, ok := env.authority.TrainingCamps().Camp(campE2EDivision, campID)
	if !ok {
		t.Fatalf("camp %d not stored", campID)
	}
	for _, member := range members {
		if member.CharID == charID {
			return member
		}
	}
	t.Fatalf("character %d is not a member of camp %d", charID, campID)
	return domain.TrainingCampMemberRecord{}
}

/*
================
campOpenAndInvite

The master opens the camp and invites one student, who accepts:
the shared prefix of the buff flows.
================
*/
func campOpenAndInvite(t *testing.T, wsM, wsJoiner *websocket.Conn, joinerGID uint32) {
	t.Helper()
	campSendFrame(t, wsM, mentor.OpTCOpenRequest, campU32(0))
	if got := campExpectFrame(t, wsM, mentor.OpTCOpenAnswer, "open ack"); len(got) == 0 || got[0] != 1 {
		t.Fatalf("0xB2EC open ack = % X, want status 1", got)
	}
	campExpectFrame(t, wsM, wire.OpPointsUpdate, "open fee gold refresh")
	campSendFrame(t, wsM, mentor.OpTCInviteRequest, campU32(joinerGID))
	campExpectFrame(t, wsJoiner, 0x3393, "invite prompt")
	campSendFrame(t, wsJoiner, 0x3393, []byte{1, 1})
	campExpectFrame(t, wsJoiner, mentor.OpTCStatus, "joiner camp seed")
	campExpectFrame(t, wsM, mentor.OpTCStatus, "master join push")
}

/*
================
TestBuffWireShapes

The three frame families byte for byte: the 9-byte show, the
byte-only clear, the byte-only pass refusal, and the 12-byte rows.
================
*/
func TestBuffWireShapes(t *testing.T) {
	if got, want := mentor.EncodeBuffStateShow3CC1(mentor.BuffStateEntry, 1440, 100),
		[]byte{1, 0xA0, 0x05, 0, 0, 100, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("0x3CC1 show = % X, want % X", got, want)
	}
	if got, want := mentor.EncodeBuffStateClear3CC1(mentor.BuffStateGraduated), []byte{8}; !bytes.Equal(got, want) {
		t.Fatalf("0x3CC1 clear = % X, want % X", got, want)
	}
	if got, want := mentor.EncodeBuffListB00C(mentor.BuffListPassError, nil), []byte{2}; !bytes.Equal(got, want) {
		t.Fatalf("0xB00C refusal = % X, want % X", got, want)
	}
	got := mentor.EncodeBuffListB00C(mentor.BuffListRefresh, []mentor.BuffListRow{{Minutes: 10, MemberID: 0x11223344, Value: 55}})
	want := []byte{1, 1, 10, 0, 0, 0, 0x44, 0x33, 0x22, 0x11, 55, 0, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("0xB00C rows = % X, want % X", got, want)
	}
}

/*
================
TestAcademyBuffEarnPersistEntryShow

A student's kills accumulate through the kill hook (state 0 on the
first), the ledger persists through the exit flush, and the next
world entry shows the saved buff again (state 1).
================
*/
func TestAcademyBuffEarnPersistEntryShow(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	gold := int64(500000)
	env := startCampServer(t, dir, []*enterworld.Character{
		{Name: campE2ENameM, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(60), Gold: &gold},
		{Name: campE2ENameS, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
	})
	clock := newCampClock(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	env.mentorInv.Now = clock.now

	wsM := campDialWS(t, env.srv)
	campHelloWS(t, wsM)
	campEnterWorld(t, wsM, campE2ENameM)
	wsS := campDialWS(t, env.srv)
	campHelloWS(t, wsS)
	campEnterWorld(t, wsS, campE2ENameS)

	charM := campE2ECharacter(t, env.authority, campE2ENameM)
	charS := campE2ECharacter(t, env.authority, campE2ENameS)
	gidS := enterworld.ObjectIDForCharacter(charS)
	campOpenAndInvite(t, wsM, wsS, gidS)

	t0 := clock.now().UnixMilli()
	_, _, frames := env.mentorInv.CampKillGrant(campE2EDivision, charS, 1000, 500, 1000, t0)
	if len(frames) != 1 || frames[0].Opcode != mentor.OpTCBuffState ||
		!bytes.Equal(frames[0].Payload, mentor.EncodeBuffStateShow3CC1(mentor.BuffStateKillRefresh, 1440, 10)) {
		t.Fatalf("first kill frames = %v, want the state-0 show [0][1440][10]", frames)
	}
	if _, _, frames := env.mentorInv.CampKillGrant(campE2EDivision, charS, 1000, 500, 1000, t0); len(frames) != 0 {
		t.Fatalf("second kill frames = %v, want none", frames)
	}

	// The exit flush lands the 20 earned points on the stored record.
	campSendFrame(t, wsS, transport.OpBye, []byte{transport.ByeReasonNormal})
	wsS.Close()
	wait.Eventually(t, 5*time.Second, "the exit flush to persist 20 points", func() bool {
		member := campBuffMember(t, env, charM.ID, charS.ID)
		return member.BuffLive && member.BuffValue == 20 && member.BuffWindowStartMs == t0
	})
	// The master hears the student go offline.
	if got := campExpectFrame(t, wsM, mentor.OpTCStatus, "S offline push"); !bytes.Equal(got, mentor.EncodeCampMemberOnlineUpdate3AC5(gidS, true)) {
		t.Fatalf("offline 0x3AC5 = % X", got)
	}

	// Re-entry shows the saved buff: state 1, fresh window, 20 points.
	wsS2 := campDialWS(t, env.srv)
	campHelloWS(t, wsS2)
	campEnterWorldExpectBuff(t, wsS2, campE2ENameS)
	if got := campExpectFrame(t, wsS2, mentor.OpTCBuffState, "S entry buff show"); !bytes.Equal(got,
		mentor.EncodeBuffStateShow3CC1(mentor.BuffStateEntry, 1440, 20)) {
		t.Fatalf("entry 0x3CC1 = % X, want state 1 with 1440 minutes and 20 points", got)
	}
	wiretest.AssertQueueDrained(t, wsS2, "entry buff tail")
	if got := campExpectFrame(t, wsM, mentor.OpTCStatus, "S online push"); !bytes.Equal(got, mentor.EncodeCampMemberOnlineUpdate3AC5(gidS, false)) {
		t.Fatalf("online 0x3AC5 = % X", got)
	}
}

/*
================
TestAcademyBuffPassDrainPulseLevelCap

The pass waits out the client's own gate (an hour or less
accumulated on the window is the byte-only refusal; the pass
lands at 61 minutes) and moves the student's 30 points onto the
Guardian as a received row; the Guardian's kills drain it for
the 10 % bonus with a mode-5 removal at zero. The minute pulse
refreshes at the cadence crossing, reports expiry (state 7) at
the window edge, and the level-40 transition clears the saved
buff (state 8).
================
*/
func TestAcademyBuffPassDrainPulseLevelCap(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	gold := int64(500000)
	env := startCampServer(t, dir, []*enterworld.Character{
		{Name: campE2ENameM, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(60), Gold: &gold},
		{Name: campE2ENameS, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
		{Name: campE2ENameO, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(30)},
	})
	clock := newCampClock(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	env.mentorInv.Now = clock.now

	wsM := campDialWS(t, env.srv)
	campHelloWS(t, wsM)
	campEnterWorld(t, wsM, campE2ENameM)
	wsS := campDialWS(t, env.srv)
	campHelloWS(t, wsS)
	campEnterWorld(t, wsS, campE2ENameS)
	wsO := campDialWS(t, env.srv)
	campHelloWS(t, wsO)
	campEnterWorld(t, wsO, campE2ENameO)

	charM := campE2ECharacter(t, env.authority, campE2ENameM)
	charS := campE2ECharacter(t, env.authority, campE2ENameS)
	charO := campE2ECharacter(t, env.authority, campE2ENameO)
	gidS := enterworld.ObjectIDForCharacter(charS)
	gidO := enterworld.ObjectIDForCharacter(charO)

	campOpenAndInvite(t, wsM, wsS, gidS)
	// The second student joins through the same handshake; S sees it.
	campSendFrame(t, wsM, mentor.OpTCInviteRequest, campU32(gidO))
	campExpectFrame(t, wsO, 0x3393, "O invite prompt")
	campSendFrame(t, wsO, 0x3393, []byte{1, 1})
	campExpectFrame(t, wsO, mentor.OpTCStatus, "O camp seed")
	campExpectFrame(t, wsM, mentor.OpTCStatus, "M sees O join")
	campExpectFrame(t, wsS, mentor.OpTCStatus, "S sees O join")

	t0 := clock.now().UnixMilli()
	for kill := 0; kill < 3; kill++ {
		env.mentorInv.CampKillGrant(campE2EDivision, charS, 1000, 500, 1000, t0)
	}

	// The pass is gated on the window's accumulated time, not on
	// the time left in it: fresh from the first grant, and again
	// at exactly an hour accumulated, it is the byte-only refusal.
	campSendFrame(t, wsS, mentor.OpTCPassBuffRequest, nil)
	if got := campExpectFrame(t, wsS, mentor.OpTCBuffList, "S fresh pass refusal"); !bytes.Equal(got, []byte{2}) {
		t.Fatalf("fresh refusal 0xB00C = % X, want [02]", got)
	}
	clock.advance(60 * time.Minute)
	campSendFrame(t, wsS, mentor.OpTCPassBuffRequest, nil)
	if got := campExpectFrame(t, wsS, mentor.OpTCBuffList, "S one-hour pass refusal"); !bytes.Equal(got, []byte{2}) {
		t.Fatalf("one-hour refusal 0xB00C = % X, want [02]", got)
	}

	// One minute later the confirm the client would now offer is
	// honoured: S's slot 7 clears (state 5), the Guardian's board
	// gains the row (mode 0), and both sides persist at once.
	clock.advance(time.Minute)
	t1 := clock.now().UnixMilli()
	campSendFrame(t, wsS, mentor.OpTCPassBuffRequest, nil)
	if got := campExpectFrame(t, wsS, mentor.OpTCBuffState, "S pass clear"); !bytes.Equal(got, mentor.EncodeBuffStateClear3CC1(mentor.BuffStateRemoval)) {
		t.Fatalf("pass 0x3CC1 = % X, want the state-5 clear", got)
	}
	wantRow := mentor.EncodeBuffListB00C(mentor.BuffListAdd, []mentor.BuffListRow{{Minutes: 1440, MemberID: gidS, Value: 30}})
	if got := campExpectFrame(t, wsM, mentor.OpTCBuffList, "M received row"); !bytes.Equal(got, wantRow) {
		t.Fatalf("pass 0xB00C = % X, want % X", got, wantRow)
	}
	if member := campBuffMember(t, env, charM.ID, charS.ID); member.BuffLive || member.BuffValue != 0 {
		t.Fatalf("stored S after pass = live %v value %d, want cleared", member.BuffLive, member.BuffValue)
	}
	if member := campBuffMember(t, env, charM.ID, charM.ID); len(member.ReservedBuffs) != 1 ||
		member.ReservedBuffs[0].DonorCharID != charS.ID || member.ReservedBuffs[0].Points != 30 ||
		member.ReservedBuffs[0].WindowStartMs != t1 {
		t.Fatalf("stored M after pass = %+v, want one 30-point row from S stamped %d", member.ReservedBuffs, t1)
	}
	// A second pass with nothing saved is the byte-only refusal.
	campSendFrame(t, wsS, mentor.OpTCPassBuffRequest, nil)
	if got := campExpectFrame(t, wsS, mentor.OpTCBuffList, "S empty pass refusal"); !bytes.Equal(got, []byte{2}) {
		t.Fatalf("refusal 0xB00C = % X, want [02]", got)
	}

	// The Guardian drains 10 a kill for 10 % of the grant; the third
	// kill empties the row and removes it; the fourth pays nothing.
	for kill, wantBonus := range []int64{200, 200, 200, 0} {
		bonusExp, bonusSkill, frames := env.mentorInv.CampKillGrant(campE2EDivision, charM, 2000, 1000, 2000, t1)
		if bonusExp != wantBonus || bonusSkill != wantBonus/2 {
			t.Fatalf("guardian kill %d bonus = %d/%d, want %d/%d", kill, bonusExp, bonusSkill, wantBonus, wantBonus/2)
		}
		if kill == 2 {
			want := mentor.EncodeBuffListB00C(mentor.BuffListRemove, []mentor.BuffListRow{{Minutes: 0, MemberID: gidS, Value: 0}})
			if len(frames) != 1 || frames[0].Opcode != mentor.OpTCBuffList || !bytes.Equal(frames[0].Payload, want) {
				t.Fatalf("guardian kill 3 frames = %v, want the mode-5 removal", frames)
			}
		} else if len(frames) != 0 {
			t.Fatalf("guardian kill %d frames = %v, want none", kill, frames)
		}
	}

	// O earns, then the pulse walks the window down: a cadence
	// refresh at 10 minutes left, silence inside the same minute,
	// the state-7 expiry report at the edge. The report freezes the
	// buff - stamp, value and live stand - so the level-cap clear
	// below still has a saved buff to clear.
	for kill := 0; kill < 2; kill++ {
		env.mentorInv.CampKillGrant(campE2EDivision, charO, 1000, 500, 1000, t0)
	}
	env.mentorInv.BuffPulse(t0 + 1430*60_000)
	if got := campExpectFrame(t, wsO, mentor.OpTCBuffState, "O cadence refresh"); !bytes.Equal(got,
		mentor.EncodeBuffStateShow3CC1(mentor.BuffStateKillRefresh, 10, 20)) {
		t.Fatalf("cadence 0x3CC1 = % X, want state 0 with 10 minutes and 20 points", got)
	}
	env.mentorInv.BuffPulse(t0 + 1430*60_000)
	wiretest.AssertQueueDrained(t, wsO, "same-minute pulse silence")
	env.mentorInv.BuffPulse(t0 + 1440*60_000)
	if got := campExpectFrame(t, wsO, mentor.OpTCBuffState, "O expiry report"); !bytes.Equal(got,
		mentor.EncodeBuffStateShow3CC1(mentor.BuffStateExpired, 0, 20)) {
		t.Fatalf("expiry 0x3CC1 = % X, want state 7 with 0 minutes and 20 points", got)
	}
	// The expired buff is frozen, not closed: the next kill earns
	// nothing and shows nothing (the level-cap clear below proves
	// the saved buff still stands to be cleared).
	if bonus, _, frames := env.mentorInv.CampKillGrant(campE2EDivision, charO, 1000, 500, 1000, t0+1440*60_000); bonus != 0 || len(frames) != 0 {
		t.Fatalf("post-expiry kill = bonus %d frames %v, want nothing (frozen)", bonus, frames)
	}

	// The level-40 transition clears the saved buff for everyone to
	// see, and the band stops all further earning.
	env.deps.Update(charO, "test-o-level-40", func() bool {
		lvl := int64(40)
		charO.Level = &lvl
		return true
	})
	wantLevel := mentor.EncodeCampMemberLevelUpdate3AC5(gidO, 40)
	if got := campExpectFrame(t, wsO, mentor.OpTCStatus, "O level frame"); !bytes.Equal(got, wantLevel) {
		t.Fatalf("O level 0x3AC5 = % X, want % X", got, wantLevel)
	}
	if got := campExpectFrame(t, wsO, mentor.OpTCBuffState, "O level-cap clear"); !bytes.Equal(got, mentor.EncodeBuffStateClear3CC1(mentor.BuffStateGraduated)) {
		t.Fatalf("level-cap 0x3CC1 = % X, want the state-8 clear", got)
	}
	for _, peer := range []*websocket.Conn{wsM, wsS} {
		if got := campExpectFrame(t, peer, mentor.OpTCStatus, "peer level frame"); !bytes.Equal(got, wantLevel) {
			t.Fatalf("peer level 0x3AC5 = % X, want % X", got, wantLevel)
		}
	}
	if bonus, _, frames := env.mentorInv.CampKillGrant(campE2EDivision, charO, 1000, 500, 1000, t0+1440*60_000); bonus != 0 || len(frames) != 0 {
		t.Fatalf("level-40 kill = bonus %d frames %v, want nothing", bonus, frames)
	}
	if member := campBuffMember(t, env, charM.ID, charO.ID); member.BuffLive || member.BuffValue != 0 {
		t.Fatalf("stored O after level cap = live %v value %d, want cleared", member.BuffLive, member.BuffValue)
	}
}

/*
================
TestAcademyBuffLoginKeepsWindow

Logging in must not restart the saved buff's 24 h window: the
entry show (state 1) reports the minutes left in the window the
first grant stamped, the stored record keeps that stamp, and the
minute pulse continues the same countdown - where a restarted
window would hand back a fresh 1440.
================
*/
func TestAcademyBuffLoginKeepsWindow(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	gold := int64(500000)
	env := startCampServer(t, dir, []*enterworld.Character{
		{Name: campE2ENameM, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(60), Gold: &gold},
		{Name: campE2ENameS, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
	})
	clock := newCampClock(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	env.mentorInv.Now = clock.now

	wsM := campDialWS(t, env.srv)
	campHelloWS(t, wsM)
	campEnterWorld(t, wsM, campE2ENameM)
	wsS := campDialWS(t, env.srv)
	campHelloWS(t, wsS)
	campEnterWorld(t, wsS, campE2ENameS)

	charM := campE2ECharacter(t, env.authority, campE2ENameM)
	charS := campE2ECharacter(t, env.authority, campE2ENameS)
	gidS := enterworld.ObjectIDForCharacter(charS)
	campOpenAndInvite(t, wsM, wsS, gidS)

	t0 := clock.now().UnixMilli()
	for kill := 0; kill < 2; kill++ {
		env.mentorInv.CampKillGrant(campE2EDivision, charS, 1000, 500, 1000, t0)
	}
	campSendFrame(t, wsS, transport.OpBye, []byte{transport.ByeReasonNormal})
	wsS.Close()
	wait.Eventually(t, 5*time.Second, "the exit flush to persist 20 points", func() bool {
		member := campBuffMember(t, env, charM.ID, charS.ID)
		return member.BuffLive && member.BuffValue == 20 && member.BuffWindowStartMs == t0
	})
	if got := campExpectFrame(t, wsM, mentor.OpTCStatus, "S offline push"); !bytes.Equal(got, mentor.EncodeCampMemberOnlineUpdate3AC5(gidS, true)) {
		t.Fatalf("offline 0x3AC5 = % X", got)
	}

	// A thousand minutes pass offline. The re-entry show must read
	// the window the first grant stamped: 440 minutes left, not a
	// fresh 1440.
	clock.advance(1000 * time.Minute)
	wsS2 := campDialWS(t, env.srv)
	campHelloWS(t, wsS2)
	campEnterWorldExpectBuff(t, wsS2, campE2ENameS)
	if got := campExpectFrame(t, wsS2, mentor.OpTCBuffState, "S entry buff show"); !bytes.Equal(got,
		mentor.EncodeBuffStateShow3CC1(mentor.BuffStateEntry, 440, 20)) {
		t.Fatalf("entry 0x3CC1 = % X, want state 1 with 440 minutes and 20 points", got)
	}
	wiretest.AssertQueueDrained(t, wsS2, "entry buff tail")
	if member := campBuffMember(t, env, charM.ID, charS.ID); !member.BuffLive || member.BuffValue != 20 || member.BuffWindowStartMs != t0 {
		t.Fatalf("stored S after re-entry = live %v value %d stamp %d, want live, 20, the first-grant stamp %d",
			member.BuffLive, member.BuffValue, member.BuffWindowStartMs, t0)
	}
	if got := campExpectFrame(t, wsM, mentor.OpTCStatus, "S online push"); !bytes.Equal(got, mentor.EncodeCampMemberOnlineUpdate3AC5(gidS, false)) {
		t.Fatalf("online 0x3AC5 = % X", got)
	}

	// And the countdown keeps walking from that same stamp: at
	// 1040 minutes in, the cadence refresh says 400, not 1400.
	env.mentorInv.BuffPulse(t0 + 1040*60_000)
	if got := campExpectFrame(t, wsS2, mentor.OpTCBuffState, "S cadence refresh"); !bytes.Equal(got,
		mentor.EncodeBuffStateShow3CC1(mentor.BuffStateKillRefresh, 400, 20)) {
		t.Fatalf("cadence 0x3CC1 = % X, want state 0 with 400 minutes and 20 points", got)
	}
}

/*
================
TestAcademyBuffTimeOverFreezesTheSavedBuff

The state-7 expiry report goes out once and freezes the saved
buff: no kill earns against it, the stored record keeps the
value live on the first-grant stamp (landed by the exit and
entry flushes), and a later login reports the expiry again -
the frozen buff stands until a membership clear, which the
grant-level test pins.
================
*/
func TestAcademyBuffTimeOverFreezesTheSavedBuff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	gold := int64(500000)
	env := startCampServer(t, dir, []*enterworld.Character{
		{Name: campE2ENameM, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(60), Gold: &gold},
		{Name: campE2ENameS, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
	})
	clock := newCampClock(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	env.mentorInv.Now = clock.now

	wsM := campDialWS(t, env.srv)
	campHelloWS(t, wsM)
	campEnterWorld(t, wsM, campE2ENameM)
	wsS := campDialWS(t, env.srv)
	campHelloWS(t, wsS)
	campEnterWorld(t, wsS, campE2ENameS)

	charM := campE2ECharacter(t, env.authority, campE2ENameM)
	charS := campE2ECharacter(t, env.authority, campE2ENameS)
	gidS := enterworld.ObjectIDForCharacter(charS)
	campOpenAndInvite(t, wsM, wsS, gidS)

	t0 := clock.now().UnixMilli()
	for kill := 0; kill < 2; kill++ {
		env.mentorInv.CampKillGrant(campE2EDivision, charS, 1000, 500, 1000, t0)
	}
	env.mentorInv.BuffPulse(t0 + 1440*60_000)
	if got := campExpectFrame(t, wsS, mentor.OpTCBuffState, "S expiry report"); !bytes.Equal(got,
		mentor.EncodeBuffStateShow3CC1(mentor.BuffStateExpired, 0, 20)) {
		t.Fatalf("expiry 0x3CC1 = % X, want state 7 with 0 minutes and 20 points", got)
	}
	// The next minute stays silent: the report is not repeated.
	env.mentorInv.BuffPulse(t0 + 1441*60_000)
	wiretest.AssertQueueDrained(t, wsS, "post-expiry pulse silence")

	// Frozen: a kill past the window earns nothing and shows
	// nothing.
	if bonus, _, frames := env.mentorInv.CampKillGrant(campE2EDivision, charS, 1000, 500, 1000, t0+1441*60_000); bonus != 0 || len(frames) != 0 {
		t.Fatalf("post-expiry kill = bonus %d frames %v, want nothing (frozen)", bonus, frames)
	}

	// A fresh login reports the expiry again: the entry leg is
	// gated on the live flag alone, and the frozen buff still
	// stands. The exit and entry flushes have landed the record
	// by now: live, 20 points, the first-grant stamp.
	clock.advance(1442 * time.Minute)
	campSendFrame(t, wsS, transport.OpBye, []byte{transport.ByeReasonNormal})
	wsS.Close()
	if got := campExpectFrame(t, wsM, mentor.OpTCStatus, "S offline push"); !bytes.Equal(got, mentor.EncodeCampMemberOnlineUpdate3AC5(gidS, true)) {
		t.Fatalf("offline 0x3AC5 = % X", got)
	}
	wsS2 := campDialWS(t, env.srv)
	campHelloWS(t, wsS2)
	campEnterWorldExpectBuff(t, wsS2, campE2ENameS)
	if got := campExpectFrame(t, wsS2, mentor.OpTCBuffState, "S re-entry expiry report"); !bytes.Equal(got,
		mentor.EncodeBuffStateShow3CC1(mentor.BuffStateExpired, 0, 20)) {
		t.Fatalf("re-entry 0x3CC1 = % X, want state 7 with 0 minutes and the frozen 20 points", got)
	}
	if got := campExpectFrame(t, wsM, mentor.OpTCStatus, "S online push"); !bytes.Equal(got, mentor.EncodeCampMemberOnlineUpdate3AC5(gidS, false)) {
		t.Fatalf("online 0x3AC5 = % X", got)
	}
	// The frozen buff cannot be passed either: the window has run
	// out, so no confirm can be offered against it and the pass
	// request is the byte-only refusal.
	campSendFrame(t, wsS2, mentor.OpTCPassBuffRequest, nil)
	if got := campExpectFrame(t, wsS2, mentor.OpTCBuffList, "S frozen pass refusal"); !bytes.Equal(got, []byte{2}) {
		t.Fatalf("frozen pass refusal 0xB00C = % X, want [02]", got)
	}
	if member := campBuffMember(t, env, charM.ID, charS.ID); !member.BuffLive || member.BuffValue != 20 || member.BuffWindowStartMs != t0 {
		t.Fatalf("stored S after expiry = live %v value %d stamp %d, want live, 20, the first-grant stamp %d",
			member.BuffLive, member.BuffValue, member.BuffWindowStartMs, t0)
	}
}
