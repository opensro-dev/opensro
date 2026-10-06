package gmcommand

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

const testDivision = "global-official"

// stubPresence is the OnlineByName facade fake, keyed by lowercase name. It
// also records whether it was consulted at all - the unprivileged test
// proves the gate short-circuits BEFORE any lookup runs.
type stubPresence struct {
	online    map[string]bool
	consulted bool
}

func (p *stubPresence) OnlineByName(_, name string) bool {
	p.consulted = true
	return p.online[strings.ToLower(name)]
}

func testDeps(characters ...*enterworld.Character) *enterworld.Deps {
	return &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: characters},
	}
}

// nameFrame hand-rolls a name-payload 0x75B6 body: {u8 subcmd, u16 len +
// ANSI name}. Asserted against the client composer's exact layout, never
// this package's own encoder.
func nameFrame(subcmd uint8, name string) []byte {
	frame := []byte{subcmd, byte(len(name)), byte(len(name) >> 8)}
	return append(frame, []byte(name)...)
}

func gmChar(name string, gm bool, region int64) *enterworld.Character {
	c := &enterworld.Character{Name: name, GMPrivilege: gm}
	c.World = &enterworld.CharacterWorld{Spawn: &enterworld.WorldSpawn{RegionID: &region}}
	return c
}

// ---- decode ----

func TestMakeItemWireHasExactV1150Width(t *testing.T) {
	payload := []byte{7, 0x86, 0x5e, 0, 0, 20}
	request, err := DecodeGmCommand(payload)
	if err != nil || request.RefObjID != 24198 || request.Amount != 20 || request.HasName {
		t.Fatalf("MAKEITEM: %+v %v", request, err)
	}
	for length := 1; length < 6; length++ {
		if _, err := DecodeGmCommand(payload[:length]); err == nil {
			t.Fatalf("accepted truncated size %d", length)
		}
	}
	if _, err := DecodeGmCommand(append(payload, 0)); err == nil {
		t.Fatal("accepted trailing byte")
	}
}

func TestDecodeGmCommandNameArm(t *testing.T) {
	request, err := DecodeGmCommand(nameFrame(SubFindUser, "Remo"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if request.Subcmd != SubFindUser || !request.HasName || request.Name != "Remo" {
		t.Fatalf("decoded %+v", request)
	}
}

func TestDecodeGmCommandNonNameArmSkipsPayload(t *testing.T) {
	// A no-payload / numeric subcommand decodes to {Subcmd, HasName:false}
	// without parsing its (varied) payload.
	request, err := DecodeGmCommand([]byte{0x02})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if request.Subcmd != 0x02 || request.HasName {
		t.Fatalf("decoded %+v", request)
	}
}

func TestDecodeGmCommandRejectsOversizedName(t *testing.T) {
	frame := []byte{SubFindUser, 0x80, 0x00}
	if _, err := DecodeGmCommand(frame); err == nil {
		t.Fatalf("expected the 0x80 name-length bound to reject the frame")
	}
}

// ---- the privilege gate ----

func TestUnprivilegedSenderRefusedAndNothingHappens(t *testing.T) {
	sender := gmChar("Nobody", false, 0x1234)
	target := gmChar("Remo", false, 0x62A8)
	deps := testDeps(sender, target)
	presence := &stubPresence{online: map[string]bool{"remo": true}}

	outcome := HandleGmCommand(deps, presence, testDivision, sender, nameFrame(SubFindUser, "Remo"))

	if outcome.Ack != nil {
		t.Fatalf("unprivileged sender must get NO ack, got % X", outcome.Ack)
	}
	if outcome.Refusal == "" {
		t.Fatalf("unprivileged sender must be refused with a logged reason")
	}
	// Nothing happened: the handler short-circuited before ANY lookup, so
	// the presence facade was never even consulted (nor could any store or
	// world state be touched - the lane has no mutators).
	if presence.consulted {
		t.Fatalf("unprivileged path must not run any lookup (presence was consulted)")
	}
}

// ---- FINDUSER honored ----

func TestPrivilegedFindUserOnlineTargetGuideAck(t *testing.T) {
	sender := gmChar("Gm", true, 0x1000)
	target := gmChar("Remo", false, 0x62A8)
	deps := testDeps(sender, target)
	presence := &stubPresence{online: map[string]bool{"remo": true}}

	outcome := HandleGmCommand(deps, presence, testDivision, sender, nameFrame(SubFindUser, "Remo"))

	// result-1 subcmd-1 guide message: {0x01, 0x01, u16 len + ANSI text}.
	guide := "Remo (region 0x62A8)"
	want := append([]byte{AckResultOK, SubFindUser, byte(len(guide)), byte(len(guide) >> 8)}, []byte(guide)...)
	if !bytes.Equal(outcome.Ack, want) {
		t.Fatalf("FINDUSER ack\n got % X\nwant % X", outcome.Ack, want)
	}
}

func TestPrivilegedFindUserOfflineTargetFailAck(t *testing.T) {
	sender := gmChar("Gm", true, 0x1000)
	offline := gmChar("Remo", false, 0x62A8)
	deps := testDeps(sender, offline)
	presence := &stubPresence{online: map[string]bool{}} // Remo is offline

	outcome := HandleGmCommand(deps, presence, testDivision, sender, nameFrame(SubFindUser, "Remo"))

	if !bytes.Equal(outcome.Ack, EncodeAckFail(SubFindUser)) {
		t.Fatalf("offline FINDUSER must result-2, got % X", outcome.Ack)
	}
}

func TestPrivilegedFindUserUnknownTargetFailAck(t *testing.T) {
	sender := gmChar("Gm", true, 0x1000)
	deps := testDeps(sender)
	presence := &stubPresence{online: map[string]bool{}}

	outcome := HandleGmCommand(deps, presence, testDivision, sender, nameFrame(SubFindUser, "Ghost"))

	if !bytes.Equal(outcome.Ack, EncodeAckFail(SubFindUser)) {
		t.Fatalf("unknown FINDUSER must result-2, got % X", outcome.Ack)
	}
}

// ---- every other command refused, never faked ----

func TestPrivilegedUnhonoredCommandsRefuseWithResult2(t *testing.T) {
	sender := gmChar("Gm", true, 0x1000)
	deps := testDeps(sender)
	presence := &stubPresence{online: map[string]bool{}}

	cases := []struct {
		name  string
		frame []byte
	}{
		{"/GOTOWN", []byte{0x02}},
		{"/WORLDSTATUS", []byte{0x04}},
		{"/BAN", nameFrame(0x0D, "Remo")},
		{"/INVISIBLE", []byte{0x0E}},
		{"/INVINCIBLE", []byte{0x0F}},
		{"/SETTIME", []byte{0x0A, 0x0D}},
		{"/LOADMONSTER", []byte{0x06, 0x34, 0x12, 0x00, 0x00, 0x03, 0x01}},
	}
	for _, tc := range cases {
		outcome := HandleGmCommand(deps, presence, testDivision, sender, tc.frame)
		subcmd := tc.frame[0]
		if !bytes.Equal(outcome.Ack, EncodeAckFail(subcmd)) {
			t.Fatalf("%s: expected result-2 refusal % X, got % X", tc.name, EncodeAckFail(subcmd), outcome.Ack)
		}
	}
}

func TestUnknownSubcommandRefused(t *testing.T) {
	sender := gmChar("Gm", true, 0x1000)
	deps := testDeps(sender)
	presence := &stubPresence{online: map[string]bool{}}

	outcome := HandleGmCommand(deps, presence, testDivision, sender, []byte{0x7F})
	if !bytes.Equal(outcome.Ack, EncodeAckFail(0x7F)) {
		t.Fatalf("unknown subcmd 0x7F must result-2, got % X", outcome.Ack)
	}
}

// ---- encode shapes ----

func TestEncodeAckShapes(t *testing.T) {
	if got := EncodeAckGuide(SubFindUser, "hi"); !bytes.Equal(got, []byte{0x01, 0x01, 0x02, 0x00, 'h', 'i'}) {
		t.Fatalf("guide ack % X", got)
	}
	if got := EncodeAckFail(SubLieName); !bytes.Equal(got, []byte{0x02, 0x19}) {
		t.Fatalf("fail ack % X", got)
	}
}

func TestAliasRefusalCarriesNativeStringAndReason(t *testing.T) {
	sender := gmChar("Gm", true, 0x1000)
	deps := testDeps(sender)
	for _, subcmd := range []byte{SubLieName, SubRealName} {
		result := HandleGmCommand(deps, nil, testDivision, sender, nameFrame(subcmd, "Remo"))
		want := []byte{2, subcmd, 4, 0, 'R', 'e', 'm', 'o', 0}
		if !bytes.Equal(result.Ack, want) {
			t.Fatalf("alias refusal: got % X, want % X", result.Ack, want)
		}
	}
}

// monsterOwner records the action-owned /LOADMONSTER call.
type monsterOwner struct {
	admit             bool
	ref               uint32
	count, monsterTyp uint8
	calls             int
}

func (o *monsterOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *monsterOwner) LoadGMMonsters(_, _ string, ref uint32, count, monsterType uint8) bool {
	o.calls++
	o.ref, o.count, o.monsterTyp = ref, count, monsterType
	return o.admit
}

/*
================
TestLoadMonsterWireAndDispatch

The client composer (50A53E..50A57E) sends subcmd 6, u32 refObjID, u8 count
and u8 type; the owner's verdict picks the result-1 or result-2 ack.
================
*/
func TestLoadMonsterWireAndDispatch(t *testing.T) {
	frame := []byte{SubLoadMonster, 0x34, 0x12, 0, 0, 25, 4}
	request, err := DecodeGmCommand(frame)
	if err != nil || request.RefObjID != 0x1234 || request.Amount != 25 || request.MonsterType != 4 {
		t.Fatalf("decode = %+v, %v", request, err)
	}
	for _, short := range [][]byte{frame[:6], append(append([]byte{}, frame...), 0)} {
		if _, err := DecodeGmCommand(short); err == nil {
			t.Fatalf("a %d-byte LOADMONSTER decoded", len(short))
		}
	}
	gm := gmChar("Gm", true, 0x655e)
	owner := &monsterOwner{admit: true}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame, owner)
	if !bytes.Equal(out.Ack, []byte{AckResultOK, SubLoadMonster}) || owner.calls != 1 || owner.ref != 0x1234 || owner.count != 25 || owner.monsterTyp != 4 {
		t.Fatalf("admitted load: ack %x, owner %+v", out.Ack, owner)
	}
	owner.admit = false
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame, owner)
	if !bytes.Equal(out.Ack, []byte{AckResultFail, SubLoadMonster}) || out.Refusal == "" {
		t.Fatalf("refused load: ack %x refusal %q", out.Ack, out.Refusal)
	}
	player := gmChar("Player", false, 0x655e)
	owner.calls = 0
	out = HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, frame, owner)
	if out.Ack != nil || owner.calls != 0 {
		t.Fatal("a non-GM reached the monster owner")
	}
}
