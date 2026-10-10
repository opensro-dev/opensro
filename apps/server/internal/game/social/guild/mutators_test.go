package guild_test

// Handler pins for the consent-free guild mutators over the REAL
// authority store: create (0x7663 -> 0xB663), notice edit (0x777A ->
// 0xB77A + 0x3B29 subOp 5) and kick (0x74B1 -> 0x3B29 subOp 3). Every
// success frame is byte-compared against a HAND-ROLLED oracle (the
// oracle32C4 encoding/binary roller from guildseed_test.go - never the
// production encoder). Refusal arms split two ways (errors.go pins the
// evidence): the PINNED arms answer the hand-rolled {u8 2}{u8 code}
// oracle bytes - create name-length {02 18}, notice empty subject
// {02 22}, notice empty contents {02 23} - and every OTHER arm still
// proves the wire stays silent (nil payloads). Kick refusals are ALL
// silent: the pinned 0x1F code has no pinned S->C carrier frame.

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
)

const mutatorDivision = "global-official"

// mutatorBerkJID is Berk's SEEDED member jid, deliberately different
// from uint32(Berk.ID) so the kick oracle proves the STORED jid is what
// the subOp-3 frame carries.
const mutatorBerkJID = uint32(700077)

func i64(v int64) *int64 { return &v }

func addGuildMemberForTest(
	guilds enterworld.GuildStore,
	divisionID string,
	guildID int64,
	actorID int64,
	member enterworld.GuildMemberRecord,
) bool {
	_, refusal := guilds.AddGuildMemberAs(divisionID, guildID, actorID, 0, member)
	return !refusal.Refused()
}

func updateGuildForTest(
	guilds enterworld.GuildStore,
	divisionID string,
	actorID int64,
	label string,
	update func(
		enterworld.GuildRecord,
		[]enterworld.GuildMemberRecord,
	) (enterworld.GuildRecord, []enterworld.GuildMemberRecord),
) bool {
	_, refusal := guilds.UpdateGuildAs(
		divisionID,
		actorID,
		label,
		enterworld.GuildAuthorization{},
		func(
			record enterworld.GuildRecord,
			members []enterworld.GuildMemberRecord,
		) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			nextRecord, nextMembers := update(record, members)
			return nextRecord, nextMembers, true
		},
	)
	return !refusal.Refused()
}

// mutatorPayload hand-rolls request bodies with encoding/binary (never
// the production writers).
type mutatorPayload struct{ buf bytes.Buffer }

func (p *mutatorPayload) u32(v uint32) { binary.Write(&p.buf, binary.LittleEndian, v) }
func (p *mutatorPayload) str(v string) {
	binary.Write(&p.buf, binary.LittleEndian, uint16(len(v)))
	p.buf.WriteString(v)
}

func mutatorCreatePayload(selectedTargetGid uint32, name string) []byte {
	p := &mutatorPayload{}
	p.u32(selectedTargetGid)
	p.str(name)
	return p.buf.Bytes()
}

func mutatorKickPayload(name string) []byte {
	p := &mutatorPayload{}
	p.str(name)
	return p.buf.Bytes()
}

func mutatorNoticePayload(subject, contents string) []byte {
	p := &mutatorPayload{}
	p.str(subject)
	p.str(contents)
	return p.buf.Bytes()
}

// mutatorCharacter is a creation-shaped record with a pinned level and
// an EXPLICIT model ref so the oracles carry literal values.
func mutatorCharacter(name string, level int64) *enterworld.Character {
	return &enterworld.Character{
		Name:          name,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     i64(enterworld.RaceChina),
		Gender:        i64(enterworld.GenderMale),
		ModelRef:      i64(1907),
		Level:         i64(level),
	}
}

// newMutatorFixture opens a real authority store with the characters
// Alfa (level 12) and Berk (level 6) and composes the handler deps.
func newMutatorFixture(t *testing.T) (*enterworld.Deps, *store.Store, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	authority, err := store.Open(t.TempDir(), store.Options{DefaultSkills: guildSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)
	alfa := mutatorCharacter("Alfa", 12)
	berk := mutatorCharacter("Berk", 6)
	for _, c := range []*enterworld.Character{alfa, berk} {
		if err := authority.CreateCharacter(mutatorDivision, "test-account", c); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", c.Name, err)
		}
	}
	deps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: authority.Characters(),
	}
	deps.Guilds = authority.Guilds()
	return deps, authority, alfa, berk
}

// newTwoMemberGuildFixture adds the seeded guild: Alfa founds NightWatch
// through the REAL create handler (leader, full mask) and Berk joins
// through the store doors with grade 3, kick-but-not-notice permMask
// (0x2) and the pinned mutatorBerkJID.
func newTwoMemberGuildFixture(t *testing.T) (*enterworld.Deps, *store.Store, *enterworld.Character, *enterworld.Character, int64) {
	t.Helper()
	deps, authority, alfa, berk := newMutatorFixture(t)
	outcome := guild.HandleCreate(deps, mutatorDivision, alfa, mutatorCreatePayload(0, "NightWatch"), nil)
	if outcome.Refusal != "" {
		t.Fatalf("fixture create refused: %s", outcome.Refusal)
	}
	guildID := outcome.GuildID
	if !addGuildMemberForTest(deps.Guilds, mutatorDivision, guildID, alfa.ID, enterworld.GuildMemberRecord{
		CharID: berk.ID, JID: mutatorBerkJID, Name: berk.Name, Grade: 3, Level: 6,
		PermMask: guild.PermMaskKick,
	}) {
		t.Fatal("fixture member join refused")
	}
	return deps, authority, alfa, berk, guildID
}

// createAckOracle hand-rolls the expected 0xB663 body for Alfa founding
// NightWatch as guild id 1: {u8 1} + the block with the documented
// DECISION initial values (guild level 1, GP 0, empty notice, crest 0,
// byte10 0; leader grade 0, level 12, donated 0, permMask 0xFFFFFFFF,
// dwords 0, empty grantName, refObjId 1907, fortressRole 1: the retail
// _Guild_FnAddMember gives MemberClass 0 the commander's SiegeAuthority)
// and the derived offline flag.
func createAckOracle(alfaID int64, alfaOffline uint8) []byte {
	oracle := &oracle32C4{}
	oracle.u8(1) // result
	oracle.u32(1)
	oracle.str("NightWatch")
	oracle.u8(1)  // guild level
	oracle.u32(0) // GP
	oracle.str("")
	oracle.str("")
	oracle.u32(0) // crestParam
	oracle.u8(0)  // byte10
	oracle.u8(1)  // memberCount
	oracle.u32(uint32(alfaID))
	oracle.str("Alfa")
	oracle.u8(0)  // grade: leader
	oracle.u8(12) // level
	oracle.u32(0)
	oracle.u32(0xffffffff)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("")
	oracle.u32(1907)
	oracle.u8(1) // fortressRole: commander
	oracle.u8(alfaOffline)
	oracle.u8(0) // voteCount
	return oracle.buf.Bytes()
}

func TestHandleCreateEmitsB663Oracle(t *testing.T) {
	t.Parallel()
	deps, _, alfa, _ := newMutatorFixture(t)
	online := func(name string) bool { return name == "Alfa" }

	outcome := guild.HandleCreate(deps, mutatorDivision, alfa, mutatorCreatePayload(0x00C40001, "NightWatch"), online)
	if outcome.Refusal != "" {
		t.Fatalf("create refused: %s", outcome.Refusal)
	}
	if outcome.GuildID != 1 || outcome.SelectedTargetGid != 0x00C40001 {
		t.Fatalf("outcome = id %d gid %#x, want 1 / 0xC40001", outcome.GuildID, outcome.SelectedTargetGid)
	}
	if want := createAckOracle(alfa.ID, 0); !bytes.Equal(outcome.AckPayload, want) {
		t.Errorf("0xB663 payload = % X, want the oracle % X", outcome.AckPayload, want)
	}
	if alfa.GuildID == nil || *alfa.GuildID != 1 {
		t.Fatalf("actor FK = %v, want 1", alfa.GuildID)
	}
	record, members, ok := deps.Guilds.Guild(mutatorDivision, 1)
	if !ok || record.Name != "NightWatch" || len(members) != 1 || members[0].CharID != alfa.ID {
		t.Fatalf("stored guild = %+v members %+v", record, members)
	}
}

// TestHandleCreateRefusals pins BOTH refusal postures: the evidenced
// name-length arms answer the hand-rolled 0xB663 {u8 2}{u8 0x18} oracle
// (INVALID_GUILDNAME_LEN - the pinned v1.188 0x4C18 emit covers the
// empty and the over-cap name), and every other arm stays fully silent
// (nil error payload - the trigger/code pairs are unpinned).
func TestHandleCreateRefusals(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk := newMutatorFixture(t)
	if outcome := guild.HandleCreate(deps, mutatorDivision, alfa, mutatorCreatePayload(0, "NightWatch"), nil); outcome.Refusal != "" {
		t.Fatalf("setup create refused: %s", outcome.Refusal)
	}

	nameLenError := []byte{0x02, 0x18}
	cases := []struct {
		name      string
		actor     *enterworld.Character
		payload   []byte
		want      string
		wantError []byte // nil = the arm stays fully silent
	}{
		{"already in guild", alfa, mutatorCreatePayload(0, "SecondBand"), "already in guild", nil},
		{"duplicate name case-insensitive", berk, mutatorCreatePayload(0, "nightwatch"), "already exists", nil},
		{"name over 12 bytes", berk, mutatorCreatePayload(0, "ThirteenChars"), "exceeds the 12 cap", nameLenError},
		{"empty name", berk, mutatorCreatePayload(0, ""), "guild name empty", nameLenError},
		{"malformed body", berk, []byte{0x01}, "payload", nil},
	}
	for _, tc := range cases {
		outcome := guild.HandleCreate(deps, mutatorDivision, tc.actor, tc.payload, nil)
		if outcome.Refusal == "" {
			t.Errorf("%s: not refused", tc.name)
			continue
		}
		if outcome.AckPayload != nil {
			t.Errorf("%s: refusal carries %d ack byte(s) - only ErrorPayload may answer", tc.name, len(outcome.AckPayload))
		}
		if !bytes.Equal(outcome.ErrorPayload, tc.wantError) {
			t.Errorf("%s: error payload = % X, want % X", tc.name, outcome.ErrorPayload, tc.wantError)
		}
		if !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
	}
	if berk.GuildID != nil {
		t.Fatalf("refused creations set Berk's FK to %v", *berk.GuildID)
	}
}

func TestHandleNoticeEditEmitsAckAndSubOp5Oracle(t *testing.T) {
	t.Parallel()
	deps, _, alfa, _, guildID := newTwoMemberGuildFixture(t)

	outcome := guild.HandleNoticeEdit(deps, mutatorDivision, alfa, mutatorNoticePayload("watch the wall", "and hold it"))
	if outcome.Refusal != "" {
		t.Fatalf("notice edit refused: %s", outcome.Refusal)
	}
	if !bytes.Equal(outcome.AckPayload, []byte{0x01}) {
		t.Errorf("0xB77A payload = % X, want [01]", outcome.AckPayload)
	}
	pushOracle := &mutatorPayload{}
	pushOracle.buf.WriteByte(5)
	pushOracle.buf.WriteByte(0x10)
	pushOracle.str("watch the wall")
	pushOracle.str("and hold it")
	if !bytes.Equal(outcome.PushPayload, pushOracle.buf.Bytes()) {
		t.Errorf("subOp-5 payload = % X, want the oracle % X", outcome.PushPayload, pushOracle.buf.Bytes())
	}
	// The fan-out names EVERY member including the acting editor (the
	// actor's client writes no notice fields on compose).
	if len(outcome.MemberNames) != 2 || outcome.MemberNames[0] != "Alfa" || outcome.MemberNames[1] != "Berk" {
		t.Errorf("fan-out names = %v, want [Alfa Berk]", outcome.MemberNames)
	}
	record, _, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	if record.NoticeSubject != "watch the wall" || record.NoticeContents != "and hold it" {
		t.Fatalf("stored notice = %q / %q", record.NoticeSubject, record.NoticeContents)
	}
}

// TestHandleNoticeEditRefusals pins BOTH refusal postures: the
// evidenced empty-field arms answer the hand-rolled 0xB77A
// {u8 2}{u8 code} oracle - empty subject {02 22}
// (INVALID_MASTER_COMMENT_TITLE), empty contents {02 23}
// (INVALID_MASTER_COMMENT), the client compose validator's own guards -
// Permission and missing membership now preserve verified wire reasons.
// Unverified client-limit and malformed arms still send no native notice.
func TestHandleNoticeEditRefusals(t *testing.T) {
	t.Parallel()
	deps, authority, alfa, berk, guildID := newTwoMemberGuildFixture(t)
	outsider := mutatorCharacter("Cale", 3)
	if err := authority.CreateCharacter(mutatorDivision, "test-account", outsider); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		actor     *enterworld.Character
		payload   []byte
		want      string
		wantError []byte // nil = the arm stays fully silent
	}{

		{"filter before permission", berk, mutatorNoticePayload("SYSOBJECTS", "body"), "notice text rejected", nil},
		{"filter subject", alfa, mutatorNoticePayload("bad'", "body"), "notice text rejected", nil},
		{"filter contents", alfa, mutatorNoticePayload("title", "SYSOBJECTS"), "notice text rejected", nil},
		{"mask lacks the notice bit", berk, mutatorNoticePayload("s", "c"), "lacks the notice-edit bit", []byte{2, 0x1e}},
		{"not in a guild", outsider, mutatorNoticePayload("s", "c"), "not in a guild", []byte{2, 0x0d}},
		{"malformed body", berk, []byte{0x02, 0x00, 'S'}, "payload", nil},
		// Alfa holds the full mask, so these reach the evidenced
		// empty-field arms.
		{"permission precedes empty text", berk, mutatorNoticePayload("", ""), "lacks the notice-edit bit", []byte{2, 0x1e}},
		{"empty subject", alfa, mutatorNoticePayload("", "c"), "empty notice subject", []byte{0x02, 0x22}},
		{"empty contents", alfa, mutatorNoticePayload("s", ""), "empty notice contents", []byte{0x02, 0x23}},
	}
	for _, tc := range cases {
		outcome := guild.HandleNoticeEdit(deps, mutatorDivision, tc.actor, tc.payload)
		if outcome.Refusal == "" || outcome.AckPayload != nil || outcome.PushPayload != nil {
			t.Errorf("%s: outcome = %+v, want a refusal with no ack/push", tc.name, outcome)
			continue
		}
		if !bytes.Equal(outcome.ErrorPayload, tc.wantError) {
			t.Errorf("%s: error payload = % X, want % X", tc.name, outcome.ErrorPayload, tc.wantError)
		}
		if !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
	}
	// No refusal - answered or silent - may have touched the stored
	// notice (the fixture created it empty).
	if record, _, _ := deps.Guilds.Guild(mutatorDivision, guildID); record.NoticeSubject != "" || record.NoticeContents != "" {
		t.Fatalf("refusals mutated the stored notice: %q / %q", record.NoticeSubject, record.NoticeContents)
	}

	// The client edit caps: 127-byte subject and 1023-byte contents PASS,
	// one byte more refuses - SILENTLY (no code is pinned for
	// over-length; 0x22/0x23 are the empty-field strings).
	deps2, _, alfa2, _, _ := newTwoMemberGuildFixture(t)
	if outcome := guild.HandleNoticeEdit(deps2, mutatorDivision, alfa2, mutatorNoticePayload(strings.Repeat("s", 127), strings.Repeat("c", 1023))); outcome.Refusal != "" {
		t.Errorf("at-cap notice refused: %s", outcome.Refusal)
	}
	if outcome := guild.HandleNoticeEdit(deps2, mutatorDivision, alfa2, mutatorNoticePayload(strings.Repeat("s", 128), "c")); !strings.Contains(outcome.Refusal, "subject") || outcome.ErrorPayload != nil {
		t.Errorf("over-cap subject outcome = %+v, want the SILENT subject refusal", outcome)
	}
	if outcome := guild.HandleNoticeEdit(deps2, mutatorDivision, alfa2, mutatorNoticePayload("s", strings.Repeat("c", 1024))); !strings.Contains(outcome.Refusal, "contents") || outcome.ErrorPayload != nil {
		t.Errorf("over-cap contents outcome = %+v, want the SILENT contents refusal", outcome)
	}
}

func TestHandleKickEmitsSubOp3OracleAndClearsFK(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	outcome := guild.HandleKick(deps, mutatorDivision, alfa, mutatorKickPayload("Berk"))
	if outcome.Refusal != "" {
		t.Fatalf("kick refused: %s", outcome.Refusal)
	}
	// {u8 3}{u32 jid}{u8 2} with the STORED member jid, NOT uint32(ID).
	kickOracle := &mutatorPayload{}
	kickOracle.buf.WriteByte(3)
	kickOracle.u32(mutatorBerkJID)
	kickOracle.buf.WriteByte(2)
	if !bytes.Equal(outcome.PushPayload, kickOracle.buf.Bytes()) {
		t.Errorf("subOp-3 payload = % X, want the oracle % X", outcome.PushPayload, kickOracle.buf.Bytes())
	}
	// ONE frame serves everyone: the fan-out names the PRE-REMOVAL list,
	// including the kicked player and the acting kicker.
	if len(outcome.MemberNames) != 2 || outcome.MemberNames[0] != "Alfa" || outcome.MemberNames[1] != "Berk" {
		t.Errorf("fan-out names = %v, want [Alfa Berk]", outcome.MemberNames)
	}
	if outcome.KickedName != "Berk" {
		t.Errorf("kicked name = %q", outcome.KickedName)
	}
	if berk.GuildID != nil {
		t.Fatalf("kicked FK = %v, want nil", *berk.GuildID)
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	if len(members) != 1 || members[0].CharID != alfa.ID {
		t.Fatalf("members after kick = %+v, want the leader only", members)
	}
}

func TestHandleKickRefusalsStaySilent(t *testing.T) {
	t.Parallel()
	deps, authority, alfa, berk, guildID := newTwoMemberGuildFixture(t)
	outsider := mutatorCharacter("Cale", 3)
	if err := authority.CreateCharacter(mutatorDivision, "test-account", outsider); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		actor   *enterworld.Character
		payload []byte
		want    string
	}{
		// Berk carries the kick bit (fixture permMask 0x2) but the
		// leader is grade 0 - unkickable.
		{"kick the leader", berk, mutatorKickPayload("Alfa"), "cannot kick the leader"},
		{"kick yourself", berk, mutatorKickPayload("Berk"), "cannot kick yourself"},
		{"unknown member", alfa, mutatorKickPayload("Nobody"), "no member named"},
		// The EXACT-match decision: a case-mismatched name resolves to
		// no member (never EqualFold).
		{"case-mismatched name", alfa, mutatorKickPayload("berk"), "no member named"},
		{"not in a guild", outsider, mutatorKickPayload("Berk"), "not in a guild"},
		{"malformed body", alfa, []byte{0x04, 0x00, 'B'}, "payload"},
	}
	for _, tc := range cases {
		outcome := guild.HandleKick(deps, mutatorDivision, tc.actor, tc.payload)
		if outcome.Refusal == "" || outcome.PushPayload != nil {
			t.Errorf("%s: outcome = %+v, want a silent refusal", tc.name, outcome)
			continue
		}
		if !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
	}

	// The mask arm: strip Berk's kick bit and prove the leader-grade
	// check never masked the permission check.
	updateGuildForTest(deps.Guilds, mutatorDivision, berk.ID, "guild-perm-strip-test", func(record enterworld.GuildRecord, members []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		for i := range members {
			if members[i].Name == "Berk" {
				members[i].PermMask = 0
			}
		}
		return record, members
	})
	if outcome := guild.HandleKick(deps, mutatorDivision, berk, mutatorKickPayload("Alfa")); !strings.Contains(outcome.Refusal, "lacks the kick bit") {
		t.Errorf("maskless kick outcome = %+v, want the permMask refusal", outcome)
	}
	if _, members, _ := deps.Guilds.Guild(mutatorDivision, guildID); len(members) != 2 {
		t.Fatalf("refusals mutated the member set: %+v", members)
	}
}
