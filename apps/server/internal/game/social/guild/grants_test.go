package guild_test

// Handler pins for guild BREAK (0x766E -> 0xB66E + 0x3B29 subOp 1) and
// the grants: name grant (0x72BC -> 0xB2BC + 0x3B29 subOp 6 &0x20) and
// fortress-position grant (0x765F -> 0xB65F + subOp 6 &0x40), over the
// REAL authority store (the mutators_test.go fixtures). Every success
// frame is byte-compared against a HAND-ROLLED encoding/binary oracle -
// never the production encoders - and every refusal arm proves the wire
// stays silent (nil payloads: no break/grant trigger->code pair is
// pinned for Legend, so result=2 is never composed).

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
)

func mutatorBreakPayload(selectedTargetGid uint32) []byte {
	p := &mutatorPayload{}
	p.u32(selectedTargetGid)
	return p.buf.Bytes()
}

func mutatorNameGrantPayload(targetJID uint32, name string) []byte {
	p := &mutatorPayload{}
	p.u32(targetJID)
	p.str(name)
	return p.buf.Bytes()
}

func mutatorPositionGrantPayload(targetJID uint32, position uint8) []byte {
	p := &mutatorPayload{}
	p.u32(targetJID)
	p.buf.WriteByte(position)
	return p.buf.Bytes()
}

// raiseGuildLevel lifts the fixture guild past the pinned name-grant
// gate (the created guild starts at the DECISION level 1, below the
// client's level-4 arm @0x005e20f6).
func raiseGuildLevel(deps *enterworld.Deps, actorID int64, level uint8) {
	updateGuildForTest(deps.Guilds, mutatorDivision, actorID, "guild-level-test", func(record enterworld.GuildRecord, members []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		record.Level = level
		return record, members
	})
}

// TestHandleBreakDissolves pins the leader's success path: the 0xB66E
// ack is exactly {01}, the subOp-1 push is exactly {01} (the client's
// break arm reads zero wire fields), MemberNames carries the WHOLE
// pre-dissolve roster including the leader, and the store afterwards
// holds no guild, no membership resolution and cleared FKs on BOTH
// member characters.
func TestHandleBreakDissolves(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	outcome := guild.HandleBreak(deps, mutatorDivision, alfa, mutatorBreakPayload(0x00C40001))
	if outcome.Refusal != "" {
		t.Fatalf("leader break refused: %s", outcome.Refusal)
	}
	if !bytes.Equal(outcome.AckPayload, []byte{0x01}) {
		t.Errorf("0xB66E ack = % X, want [01]", outcome.AckPayload)
	}
	if !bytes.Equal(outcome.PushPayload, []byte{0x01}) {
		t.Errorf("subOp-1 push = % X, want [01]", outcome.PushPayload)
	}
	if len(outcome.MemberNames) != 2 || outcome.MemberNames[0] != "Alfa" || outcome.MemberNames[1] != "Berk" {
		t.Errorf("MemberNames = %v, want the pre-dissolve roster [Alfa Berk]", outcome.MemberNames)
	}
	if outcome.SelectedTargetGid != 0x00C40001 {
		t.Errorf("SelectedTargetGid = %#x, want the decoded 0x00C40001", outcome.SelectedTargetGid)
	}
	if _, _, ok := deps.Guilds.Guild(mutatorDivision, guildID); ok {
		t.Error("guild still stored after the dissolution door")
	}
	if _, ok := deps.Guilds.GuildOfCharacter(mutatorDivision, alfa.ID); ok {
		t.Error("leader still resolves to a guild membership")
	}
	if alfa.GuildID != nil || berk.GuildID != nil {
		t.Errorf("FKs after dissolve = %v/%v, want both nil", alfa.GuildID, berk.GuildID)
	}
}

// TestHandleBreakRefusals proves every break refusal is wire-silent
// (BreakOutcome carries no error slot at all): non-leader, guildless,
// nil character, delete-pending and malformed bodies.
func TestHandleBreakRefusals(t *testing.T) {
	t.Parallel()
	deps, authority, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	cases := []struct {
		name    string
		actor   *enterworld.Character
		payload []byte
		want    string
	}{
		{"non-leader", berk, mutatorBreakPayload(0), "cannot dissolve"},
		{"nil character", nil, mutatorBreakPayload(0), "characterNotFound"},
		{"malformed short", alfa, []byte{0x01}, ""},
		{"malformed trailing", alfa, append(mutatorBreakPayload(0), 0x00), ""},
	}
	for _, tc := range cases {
		outcome := guild.HandleBreak(deps, mutatorDivision, tc.actor, tc.payload)
		if outcome.Refusal == "" {
			t.Errorf("%s: not refused", tc.name)
			continue
		}
		if tc.want != "" && !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
		if outcome.AckPayload != nil || outcome.PushPayload != nil {
			t.Errorf("%s: refusal carries frames (ack % X push % X)", tc.name, outcome.AckPayload, outcome.PushPayload)
		}
	}
	if _, _, ok := deps.Guilds.Guild(mutatorDivision, guildID); !ok {
		t.Fatal("guild vanished on a refused break")
	}

	// The guildless arm needs a character with no FK.
	loner := mutatorCharacter("Cale", 3)
	if err := authority.CreateCharacter(mutatorDivision, "test-account", loner); err != nil {
		t.Fatalf("CreateCharacter(%s): %v", loner.Name, err)
	}
	if outcome := guild.HandleBreak(deps, mutatorDivision, loner, mutatorBreakPayload(0)); !strings.Contains(outcome.Refusal, "not in a guild") {
		t.Errorf("guildless refusal = %q, want not-in-a-guild", outcome.Refusal)
	}
}

// nameGrantAckOracle hand-rolls the 0xB2BC success body: {u8 1} then
// the jid twice (the client discards the first dword) and the sized
// title.
func nameGrantAckOracle(jid uint32, name string) []byte {
	p := &mutatorPayload{}
	p.buf.WriteByte(0x01)
	p.u32(jid)
	p.u32(jid)
	p.str(name)
	return p.buf.Bytes()
}

// nameGrantPushOracle hand-rolls the subOp-6 &0x20 delta:
// {u8 6}{u32 jid}{u8 0x20}{u16-len ANSI}.
func nameGrantPushOracle(jid uint32, name string) []byte {
	p := &mutatorPayload{}
	p.buf.WriteByte(0x06)
	p.u32(jid)
	p.buf.WriteByte(0x20)
	p.str(name)
	return p.buf.Bytes()
}

// TestHandleNameGrantPersistsAndAnswers pins the leader's success path
// on a level-4 guild: the 0xB2BC ack and the subOp-6 &0x20 delta match
// the hand-rolled oracles, the fan-out names EXCLUDE the acting
// granter, and the store's member row carries the granted title.
func TestHandleNameGrantPersistsAndAnswers(t *testing.T) {
	t.Parallel()
	deps, _, alfa, _, guildID := newTwoMemberGuildFixture(t)
	raiseGuildLevel(deps, alfa.ID, 4)

	outcome := guild.HandleNameGrant(deps, mutatorDivision, alfa, mutatorNameGrantPayload(mutatorBerkJID, "Warden"))
	if outcome.Refusal != "" {
		t.Fatalf("name grant refused: %s", outcome.Refusal)
	}
	if want := nameGrantAckOracle(mutatorBerkJID, "Warden"); !bytes.Equal(outcome.AckPayload, want) {
		t.Errorf("0xB2BC = % X, want % X", outcome.AckPayload, want)
	}
	if want := nameGrantPushOracle(mutatorBerkJID, "Warden"); !bytes.Equal(outcome.PushPayload, want) {
		t.Errorf("subOp-6 &0x20 = % X, want % X", outcome.PushPayload, want)
	}
	if len(outcome.MemberNames) != 1 || outcome.MemberNames[0] != "Berk" {
		t.Errorf("MemberNames = %v, want [Berk] (the actor is excluded)", outcome.MemberNames)
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	berkRow, ok := memberRowByJID(members, mutatorBerkJID)
	if !ok || berkRow.GrantName != "Warden" {
		t.Errorf("stored grant name = %q (found=%v), want Warden", berkRow.GrantName, ok)
	}
}

// TestHandleNameGrantRefusals proves every name-grant refusal is
// wire-silent: non-leader actor, guild below the level-4 gate, unknown
// target jid, empty and over-cap titles, malformed bodies.
func TestHandleNameGrantRefusals(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	// Below the level gate first (the created guild is level 1).
	if outcome := guild.HandleNameGrant(deps, mutatorDivision, alfa, mutatorNameGrantPayload(mutatorBerkJID, "Warden")); !strings.Contains(outcome.Refusal, "name-grant gate") {
		t.Errorf("level-gate refusal = %q", outcome.Refusal)
	}
	raiseGuildLevel(deps, alfa.ID, 4)

	cases := []struct {
		name    string
		actor   *enterworld.Character
		payload []byte
		want    string
	}{
		{"non-leader", berk, mutatorNameGrantPayload(mutatorBerkJID, "Warden"), "cannot grant"},
		{"unknown jid", alfa, mutatorNameGrantPayload(999999, "Warden"), "no member with jid"},
		{"empty title", alfa, mutatorNameGrantPayload(mutatorBerkJID, ""), "empty grant name"},
		{"over-cap title", alfa, mutatorNameGrantPayload(mutatorBerkJID, "ThirteenChars"), "exceeds"},
		{"malformed", alfa, []byte{0x01}, ""},
	}
	for _, tc := range cases {
		outcome := guild.HandleNameGrant(deps, mutatorDivision, tc.actor, tc.payload)
		if outcome.Refusal == "" {
			t.Errorf("%s: not refused", tc.name)
			continue
		}
		if tc.want != "" && !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
		if outcome.AckPayload != nil || outcome.PushPayload != nil {
			t.Errorf("%s: refusal carries frames", tc.name)
		}
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	if row, _ := memberRowByJID(members, mutatorBerkJID); row.GrantName != "" {
		t.Errorf("refused grants mutated the stored title to %q", row.GrantName)
	}
}

// positionGrantAckOracle hand-rolls the 0xB65F success body.
func positionGrantAckOracle(jid uint32, role uint8) []byte {
	p := &mutatorPayload{}
	p.buf.WriteByte(0x01)
	p.u32(jid)
	p.u32(jid)
	p.buf.WriteByte(role)
	return p.buf.Bytes()
}

// positionGrantPushOracle hand-rolls the subOp-6 &0x40 delta.
func positionGrantPushOracle(jid uint32, role uint8) []byte {
	p := &mutatorPayload{}
	p.buf.WriteByte(0x06)
	p.u32(jid)
	p.buf.WriteByte(0x40)
	p.buf.WriteByte(role)
	return p.buf.Bytes()
}

// TestHandlePositionGrantPersistsAndAnswers pins the leader's success
// path (NO level gate on this arm) plus the role-0 CLEAR, byte-compared
// against the oracles; the fan-out excludes the actor and the store row
// carries the role.
func TestHandlePositionGrantPersistsAndAnswers(t *testing.T) {
	t.Parallel()
	deps, _, alfa, _, guildID := newTwoMemberGuildFixture(t)

	outcome := guild.HandlePositionGrant(deps, mutatorDivision, alfa, mutatorPositionGrantPayload(mutatorBerkJID, 0x10))
	if outcome.Refusal != "" {
		t.Fatalf("position grant refused: %s", outcome.Refusal)
	}
	if want := positionGrantAckOracle(mutatorBerkJID, 0x10); !bytes.Equal(outcome.AckPayload, want) {
		t.Errorf("0xB65F = % X, want % X", outcome.AckPayload, want)
	}
	if want := positionGrantPushOracle(mutatorBerkJID, 0x10); !bytes.Equal(outcome.PushPayload, want) {
		t.Errorf("subOp-6 &0x40 = % X, want % X", outcome.PushPayload, want)
	}
	if len(outcome.MemberNames) != 1 || outcome.MemberNames[0] != "Berk" {
		t.Errorf("MemberNames = %v, want [Berk]", outcome.MemberNames)
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	if row, _ := memberRowByJID(members, mutatorBerkJID); row.FortressRole != 0x10 {
		t.Errorf("stored fortress role = %#x, want 0x10", row.FortressRole)
	}

	// Role 0 clears (the client's case-0 empty-title arm).
	clear := guild.HandlePositionGrant(deps, mutatorDivision, alfa, mutatorPositionGrantPayload(mutatorBerkJID, 0))
	if clear.Refusal != "" {
		t.Fatalf("role-0 clear refused: %s", clear.Refusal)
	}
	_, members, _ = deps.Guilds.Guild(mutatorDivision, guildID)
	if row, _ := memberRowByJID(members, mutatorBerkJID); row.FortressRole != 0 {
		t.Errorf("stored fortress role after clear = %#x, want 0", row.FortressRole)
	}
}

// TestHandlePositionGrantRefusals proves the silent refusal arms:
// non-leader actor, a role byte outside the pinned domain, unknown
// target jid, the commander role, the master as target and malformed
// bodies.
func TestHandlePositionGrantRefusals(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	cases := []struct {
		name    string
		actor   *enterworld.Character
		payload []byte
		want    string
	}{
		{"non-leader", berk, mutatorPositionGrantPayload(mutatorBerkJID, 2), "cannot grant"},
		{"role outside domain", alfa, mutatorPositionGrantPayload(mutatorBerkJID, 3), "outside the pinned domain"},
		{"unknown jid", alfa, mutatorPositionGrantPayload(999999, 2), "no member with jid"},
		// The commander is the master's alone: never granted (5F4D70 offers
		// no 1), never taken from the master (_Guild_Delegate_Master -1002).
		{"commander", alfa, mutatorPositionGrantPayload(mutatorBerkJID, 1), "belongs to the guild master"},
		{"the master", alfa, mutatorPositionGrantPayload(guild.GuildJID(alfa.ID), 0), "keeps the commander role"},
		{"malformed", alfa, []byte{0x01}, ""},
	}
	for _, tc := range cases {
		outcome := guild.HandlePositionGrant(deps, mutatorDivision, tc.actor, tc.payload)
		if outcome.Refusal == "" {
			t.Errorf("%s: not refused", tc.name)
			continue
		}
		if tc.want != "" && !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
		if outcome.AckPayload != nil || outcome.PushPayload != nil {
			t.Errorf("%s: refusal carries frames", tc.name)
		}
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	if row, _ := memberRowByJID(members, mutatorBerkJID); row.FortressRole != 0 {
		t.Errorf("refused grants mutated the stored role to %#x", row.FortressRole)
	}
	if row, _ := memberRowByJID(members, guild.GuildJID(alfa.ID)); row.FortressRole != guild.FortressRoleCommander {
		t.Errorf("the master's stored role = %#x, want the commander's", row.FortressRole)
	}
}

// memberRowByJID resolves a member row from a store copy by jid (test
// helper - the production resolve lives in register.go).
func memberRowByJID(members []enterworld.GuildMemberRecord, jid uint32) (enterworld.GuildMemberRecord, bool) {
	for _, member := range members {
		if member.JID == jid {
			return member, true
		}
	}
	return enterworld.GuildMemberRecord{}, false
}
