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
		{"/GOTOWN without an owner", []byte{0x02}},
		{"/TOTOWN without an owner", nameFrame(0x03, "Remo")},
		{"/MOVETOUSER without an owner", nameFrame(0x08, "Remo")},
		{"/RECALLUSER without an owner", nameFrame(0x11, "Remo")},
		{"/WORLDSTATUS without an owner", []byte{0x04}},
		{"/BAN without a session directory", nameFrame(0x0D, "Remo")},
		{"/INVISIBLE", []byte{0x0E}},
		{"/INVINCIBLE", []byte{0x0F}},
		{"/SETTIME without an owner", []byte{0x0A, 0x0D}},
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

// killOwner records the action-owned /MOBKILL call.
type killOwner struct {
	monsterOwner
	kill    bool
	gid     uint32
	rewards bool
	calls   int
}

func (o *killOwner) KillGMMonster(_, _ string, gid uint32, rewards bool) bool {
	o.calls++
	o.gid, o.rewards = gid, rewards
	return o.kill
}

/*
================
TestMobKillWireAndDispatch

The client composer (50B754..50B77E) sends subcmd 0x14, u32 selected target
id and u16 mode, and only for a mode argument of exactly 0 or 1. Mode 0 asks
for rewards, mode 1 for none; the owner's verdict picks the result-1 or
result-2 ack, and a non-GM never reaches the owner.
================
*/
func TestMobKillWireAndDispatch(t *testing.T) {
	frame := []byte{SubMobKill, 0x78, 0x56, 0x34, 0x12, 0, 0}
	request, err := DecodeGmCommand(frame)
	if err != nil || request.TargetGID != 0x12345678 || request.MobKillMode != 0 {
		t.Fatalf("decode = %+v, %v", request, err)
	}
	for _, bad := range [][]byte{frame[:6], append(append([]byte{}, frame...), 0), {SubMobKill, 1, 0, 0, 0, 2, 0}} {
		if _, err := DecodeGmCommand(bad); err == nil {
			t.Fatalf("a malformed MOBKILL % X decoded", bad)
		}
	}
	gm := gmChar("Gm", true, 0x655e)
	owner := &killOwner{kill: true}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame, owner)
	if !bytes.Equal(out.Ack, []byte{AckResultOK, SubMobKill}) || owner.calls != 1 || owner.gid != 0x12345678 || !owner.rewards {
		t.Fatalf("mode 0: ack %x, owner %+v", out.Ack, owner)
	}
	frame[5] = 1
	HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame, owner)
	if owner.calls != 2 || owner.rewards {
		t.Fatalf("mode 1 asked for rewards: %+v", owner)
	}
	owner.kill = false
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame, owner)
	if !bytes.Equal(out.Ack, []byte{AckResultFail, SubMobKill}) || out.Refusal == "" {
		t.Fatalf("refused kill: ack %x refusal %q", out.Ack, out.Refusal)
	}
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame)
	if !bytes.Equal(out.Ack, []byte{AckResultFail, SubMobKill}) {
		t.Fatalf("no owner: ack %x", out.Ack)
	}
	player := gmChar("Player", false, 0x655e)
	owner.calls = 0
	out = HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, frame, owner)
	if out.Ack != nil || owner.calls != 0 {
		t.Fatal("a non-GM reached the kill owner")
	}
}

// zoeOwner records the action-owned /ZOE call.
type zoeOwner struct {
	monsterOwner
	admit    bool
	ref      uint32
	count    uint8
	zoeCalls int
}

func (o *zoeOwner) KillGMZoe(_, _ string, ref uint32, count uint8) bool {
	o.zoeCalls++
	o.ref, o.count = ref, count
	return o.admit
}

/*
================
TestZoeWireAndDispatch

The client composer (50A6DD..50A707) sends subcmd 0x0C, u32 monster
refObjID and u8 count; the owner's verdict picks the result-1 or result-2
ack, and a non-GM never reaches the owner.
================
*/
func TestZoeWireAndDispatch(t *testing.T) {
	frame := []byte{SubZoe, 0x8D, 0x07, 0, 0, 3}
	request, err := DecodeGmCommand(frame)
	if err != nil || request.RefObjID != 1933 || request.Amount != 3 {
		t.Fatalf("decode = %+v, %v", request, err)
	}
	for _, bad := range [][]byte{frame[:5], append(append([]byte{}, frame...), 0)} {
		if _, err := DecodeGmCommand(bad); err == nil {
			t.Fatalf("a %d-byte ZOE decoded", len(bad))
		}
	}
	gm := gmChar("Gm", true, 0x655e)
	owner := &zoeOwner{admit: true}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame, owner)
	if !bytes.Equal(out.Ack, []byte{AckResultOK, SubZoe}) || owner.zoeCalls != 1 || owner.ref != 1933 || owner.count != 3 {
		t.Fatalf("admitted zoe: ack %x owner %+v", out.Ack, owner)
	}
	owner.admit = false
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, frame, owner)
	if !bytes.Equal(out.Ack, []byte{AckResultFail, SubZoe}) || out.Refusal == "" {
		t.Fatalf("refused zoe: ack %x refusal %q", out.Ack, out.Refusal)
	}
	player := gmChar("Player", false, 0x655e)
	owner.zoeCalls = 0
	out = HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, frame, owner)
	if out.Ack != nil || owner.zoeCalls != 0 {
		t.Fatal("a non-GM reached the zoe owner")
	}
}

type lowSpecOwner struct {
	monsterOwner
	npcCalls      int
	npcCode       string
	npcOK         bool
	instanceCalls int
	worldID       uint16
	layerID       uint16
	instanceOK    bool
	structCalls   int
	structAction  uint8
	fortressID    uint32
	eventStructID uint32
	structParam   uint8
	structOK      bool
	periodCalls   int
	periodAction  uint8
	periodOK      bool
}

func (o *lowSpecOwner) MoveToNpcGM(_, _, code string) bool {
	o.npcCalls++
	o.npcCode = code
	return o.npcOK
}

func (o *lowSpecOwner) InstanceGM(_ string, worldID, layerID uint16) bool {
	o.instanceCalls++
	o.worldID, o.layerID = worldID, layerID
	return o.instanceOK
}

func (o *lowSpecOwner) SiegeStructureGM(_, _ string, action uint8, fortressID, eventStructID uint32, param uint8) bool {
	o.structCalls++
	o.structAction, o.fortressID, o.eventStructID, o.structParam = action, fortressID, eventStructID, param
	return o.structOK
}

func (o *lowSpecOwner) SiegePeriodGM(_ string, periodAction uint8) bool {
	o.periodCalls++
	o.periodAction = periodAction
	return o.periodOK
}

/*
================
TestLowSpecGmCommandsWireAndDispatch

Verifies wire decoding and HandleGmCommand dispatch for /INSTANCE (0x13),
/movetonpc (0x1F), /SIEGE structure (0x20), /SIEGE period (0x21), and
/flagworld (0x30).
================
*/
func TestLowSpecGmCommandsWireAndDispatch(t *testing.T) {
	gm := gmChar("Gm", true, 0x655e)

	// /flagworld (0x30): 2-byte frame [0x30, u8(arg)], stubbed ack [1, 0x30].
	flagFrame := []byte{SubFlagWorld, 0x05}
	if req, err := DecodeGmCommand(flagFrame); err != nil || req.Subcmd != SubFlagWorld || req.Amount != 5 {
		t.Fatalf("flagworld decode = %+v, %v", req, err)
	}
	if out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, flagFrame); !bytes.Equal(out.Ack, []byte{AckResultOK, SubFlagWorld}) {
		t.Fatalf("flagworld ack = %x", out.Ack)
	}

	// /INSTANCE (0x13): 5-byte frame [0x13, u16(worldID), u16(layerID)].
	instFrame := []byte{SubOpenWorld, 0x02, 0x00, 0x01, 0x00}
	if req, err := DecodeGmCommand(instFrame); err != nil || req.WorldID != 2 || req.LayerID != 1 {
		t.Fatalf("instance decode = %+v, %v", req, err)
	}
	owner := &lowSpecOwner{instanceOK: true, npcOK: true, structOK: true, periodOK: true}
	if out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, instFrame, owner); !bytes.Equal(out.Ack, []byte{AckResultOK, SubOpenWorld}) || owner.instanceCalls != 1 || owner.worldID != 2 || owner.layerID != 1 {
		t.Fatalf("instance dispatch = %x, %+v", out.Ack, owner)
	}

	// /movetonpc (0x1F): sized codename frame.
	npcFrame := append([]byte{SubMoveToNPC, 11, 0}, "NPC_CH_SMITH"[:11]...)
	if req, err := DecodeGmCommand(npcFrame); err != nil || req.Name != "NPC_CH_SMIT" {
		t.Fatalf("movetonpc decode = %+v, %v", req, err)
	}
	if out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, npcFrame, owner); !bytes.Equal(out.Ack, []byte{AckResultOK, SubMoveToNPC}) || owner.npcCalls != 1 || owner.npcCode != "NPC_CH_SMIT" {
		t.Fatalf("movetonpc dispatch = %x, %+v", out.Ack, owner)
	}

	// /SIEGE STRUCT_BUILD (0x20, action 0x0A, 10 bytes) and GATE (action 0x15, 11 bytes).
	buildFrame := []byte{SubSiegeStructure, SiegeActionBuild, 1, 0, 0, 0, 109, 0, 0, 0}
	if req, err := DecodeGmCommand(buildFrame); err != nil || req.SiegeAction != SiegeActionBuild || req.FortressID != 1 || req.EventStructID != 109 {
		t.Fatalf("siege build decode = %+v, %v", req, err)
	}
	gateFrame := []byte{SubSiegeStructure, SiegeActionGate, 1, 0, 0, 0, 84, 0, 0, 0, 2}
	if req, err := DecodeGmCommand(gateFrame); err != nil || req.SiegeAction != SiegeActionGate || req.FortressID != 1 || req.EventStructID != 84 || req.SiegeParam != 2 {
		t.Fatalf("siege gate decode = %+v, %v", req, err)
	}
	if out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, gateFrame, owner); !bytes.Equal(out.Ack, []byte{AckResultOK, SubSiegeStructure}) || owner.structCalls != 1 || owner.structParam != 2 {
		t.Fatalf("siege gate dispatch = %x, %+v", out.Ack, owner)
	}

	// /SIEGE STARTWAR (0x21, 2 bytes).
	periodFrame := []byte{SubSiegePeriod, SiegePeriodStartWar}
	if req, err := DecodeGmCommand(periodFrame); err != nil || req.SiegeAction != SiegePeriodStartWar {
		t.Fatalf("siege period decode = %+v, %v", req, err)
	}
	if out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, periodFrame, owner); !bytes.Equal(out.Ack, []byte{AckResultOK, SubSiegePeriod}) || owner.periodCalls != 1 || owner.periodAction != SiegePeriodStartWar {
		t.Fatalf("siege period dispatch = %x, %+v", out.Ack, owner)
	}
}
