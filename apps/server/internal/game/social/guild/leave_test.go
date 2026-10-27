package guild_test

// Handler pins for the guild LEAVE mutator (0x756E -> 0xB56E + 0x3B29
// subOp 3 kind 1) over the REAL authority store, on the mutators_test.go
// fixture (Alfa the grade-0 leader, Berk the grade-3 member with the
// pinned mutatorBerkJID). Success frames byte-compare against
// HAND-ROLLED oracles; EVERY refusal is fully silent - leave has no
// answered refusal arm at all: the classic 0x36/0x1E codes are only
// PROBABLE for Legend.

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
)

// mutatorLeavePayload hand-rolls the 0x756E body {u32} with
// encoding/binary (never the production writers).
func mutatorLeavePayload(selectedTargetGid uint32) []byte {
	p := &mutatorPayload{}
	p.u32(selectedTargetGid)
	return p.buf.Bytes()
}

func TestHandleLeaveEmitsAckAndSubOp3OracleAndClearsFK(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	outcome := guild.HandleLeave(deps, mutatorDivision, berk, mutatorLeavePayload(0x00C40007))
	if outcome.Refusal != "" {
		t.Fatalf("leave refused: %s", outcome.Refusal)
	}
	// The actor's 0xB56E ack is exactly {u8 1} - result=2 is never
	// composed on any arm.
	if !bytes.Equal(outcome.AckPayload, []byte{0x01}) {
		t.Errorf("0xB56E payload = % X, want [01]", outcome.AckPayload)
	}
	// {u8 3}{u32 jid}{u8 1} - kind 1 = exit (NOT kick's 2), with the
	// STORED member jid, NOT uint32(ID).
	leaveOracle := &mutatorPayload{}
	leaveOracle.buf.WriteByte(3)
	leaveOracle.u32(mutatorBerkJID)
	leaveOracle.buf.WriteByte(1)
	if !bytes.Equal(outcome.PushPayload, leaveOracle.buf.Bytes()) {
		t.Errorf("subOp-3 payload = % X, want the oracle % X", outcome.PushPayload, leaveOracle.buf.Bytes())
	}
	// ONE frame serves everyone: the fan-out names the PRE-REMOVAL
	// list, so the leaver is included (their client's jid==me arm does
	// the full guild reset).
	if len(outcome.MemberNames) != 2 || outcome.MemberNames[0] != "Alfa" || outcome.MemberNames[1] != "Berk" {
		t.Errorf("fan-out names = %v, want [Alfa Berk]", outcome.MemberNames)
	}
	if outcome.SelectedTargetGid != 0x00C40007 {
		t.Errorf("decoded gid = %#x, want 0x%X", outcome.SelectedTargetGid, 0x00C40007)
	}
	// The atomic door's pair: FK cleared AND member row gone.
	if berk.GuildID != nil {
		t.Fatalf("leaver FK = %v, want nil", *berk.GuildID)
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	if len(members) != 1 || members[0].CharID != alfa.ID {
		t.Fatalf("members after leave = %+v, want the leader only", members)
	}
}

// TestHandleLeaveRefusalsStaySilent pins that EVERY leave refusal is
// fully silent - no ack, no push, and (by construction: LeaveOutcome
// has no ErrorPayload slot) no result=2 answer. The leader arm is the
// policy one: grade 0 cannot leave, and whether a Legend leader-leave
// should refuse or disband stays an undecided frontier - dissolve
// belongs to the break door (0x766E / subOp 1), which this lane does
// not implement.
func TestHandleLeaveRefusalsStaySilent(t *testing.T) {
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
		{"leader cannot leave", alfa, mutatorLeavePayload(0), "leader cannot leave"},
		{"not in a guild", outsider, mutatorLeavePayload(0), "not in a guild"},
		{"malformed short body", berk, []byte{0x07, 0x00}, "payload"},
		{"malformed trailing byte", berk, append(mutatorLeavePayload(0), 0x00), "payload"},
		{"nil body", berk, nil, "payload"},
	}
	for _, tc := range cases {
		outcome := guild.HandleLeave(deps, mutatorDivision, tc.actor, tc.payload)
		if outcome.Refusal == "" || outcome.AckPayload != nil || outcome.PushPayload != nil {
			t.Errorf("%s: outcome = %+v, want a silent refusal", tc.name, outcome)
			continue
		}
		if !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
	}

	// The identity arms refuse before the body is even decoded.
	if outcome := guild.HandleLeave(deps, mutatorDivision, nil, mutatorLeavePayload(0)); outcome.Refusal != "characterNotFound" {
		t.Errorf("nil-character refusal = %q, want characterNotFound", outcome.Refusal)
	}
	pending := mutatorCharacter("Dora", 3)
	pending.DeletePending = true
	if outcome := guild.HandleLeave(deps, mutatorDivision, pending, mutatorLeavePayload(0)); outcome.Refusal != "deletePending" {
		t.Errorf("delete-pending refusal = %q, want deletePending", outcome.Refusal)
	}

	// No refusal touched the store: both rows and both FKs intact.
	if _, members, _ := deps.Guilds.Guild(mutatorDivision, guildID); len(members) != 2 {
		t.Fatalf("refusals mutated the member set: %+v", members)
	}
	if alfa.GuildID == nil || berk.GuildID == nil {
		t.Fatalf("refusals cleared an FK: alfa=%v berk=%v", alfa.GuildID, berk.GuildID)
	}
}
