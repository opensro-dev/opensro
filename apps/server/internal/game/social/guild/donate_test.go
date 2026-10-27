package guild_test

// Handler pins for GP donate (0x740F -> 0xB40F + 0x3B29 subOp-5 &0x08 +
// subOp-6 &0x08) over the REAL authority store (the mutators_test.go
// fixtures). Every success frame is byte-compared against a HAND-ROLLED
// encoding/binary oracle - never the production encoders - and every
// refusal arm proves the wire stays fully silent (GpDonateOutcome has no
// error slot at all: no donate trigger->code pair is pinned in either
// dump, so result=2 is never composed).

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
)

func mutatorDonatePayload(amount uint32) []byte {
	p := &mutatorPayload{}
	p.u32(amount)
	return p.buf.Bytes()
}

// donateAckOracle hand-rolls the 0xB40F success body {u8 1}{u32 amount}
// (the display-only echo the client CNetProcess_OnResponse0xB40F guide
// line formats).
func donateAckOracle(amount uint32) []byte {
	p := &mutatorPayload{}
	p.buf.WriteByte(0x01)
	p.u32(amount)
	return p.buf.Bytes()
}

// donateGuildGpOracle hand-rolls the subOp-5 &0x08 guild-GP delta
// {u8 5}{u8 0x08}{u32 newGuildGp}.
func donateGuildGpOracle(newGuildGp uint32) []byte {
	p := &mutatorPayload{}
	p.buf.WriteByte(0x05)
	p.buf.WriteByte(0x08)
	p.u32(newGuildGp)
	return p.buf.Bytes()
}

// donateDonorGpOracle hand-rolls the subOp-6 &0x08 donated-GP delta
// {u8 6}{u32 jid}{u8 0x08}{u32 newDonatedGp}.
func donateDonorGpOracle(jid uint32, newDonatedGp uint32) []byte {
	p := &mutatorPayload{}
	p.buf.WriteByte(0x06)
	p.u32(jid)
	p.buf.WriteByte(0x08)
	p.u32(newDonatedGp)
	return p.buf.Bytes()
}

// grantSkillPoints installs a persisted SP pool on a fixture character
// (the donation source - the SP->GP exchange the UIIT donate family
// pins).
func grantSkillPoints(authority *store.Store, c *enterworld.Character, sp int64) {
	authority.MutateCharacter(c, "donate-test-sp", func() { c.SkillPoints = &sp })
}

// TestHandleGpDonateSuccess pins the in-guild member's success path: the
// 0xB40F ack echoes the donated amount, the subOp-5 delta carries the
// NEW guild GP, the subOp-6 delta carries the donor's STORED jid and NEW
// donated total, MemberNames carries the WHOLE roster INCLUDING the
// donating actor (both deltas are silent client-side data writes and the
// ack carries no state), and the store afterwards holds the debited SP
// and both credited GP fields. Berk (grade 3, no leader bit) donates -
// proving the arm is in-guild-only with NO grade or permission gate (the
// CCharacterDependentData_IsInGuild pin).
func TestHandleGpDonateSuccess(t *testing.T) {
	t.Parallel()
	deps, authority, _, berk, guildID := newTwoMemberGuildFixture(t)
	grantSkillPoints(authority, berk, 500)

	outcome := guild.HandleGpDonate(deps, mutatorDivision, berk, mutatorDonatePayload(120))
	if outcome.Refusal != "" {
		t.Fatalf("member donate refused: %s", outcome.Refusal)
	}
	if want := donateAckOracle(120); !bytes.Equal(outcome.AckPayload, want) {
		t.Errorf("0xB40F ack = % X, want % X", outcome.AckPayload, want)
	}
	if want := donateGuildGpOracle(120); !bytes.Equal(outcome.GuildGpPushPayload, want) {
		t.Errorf("subOp-5 &0x08 = % X, want % X", outcome.GuildGpPushPayload, want)
	}
	if want := donateDonorGpOracle(mutatorBerkJID, 120); !bytes.Equal(outcome.DonorGpPushPayload, want) {
		t.Errorf("subOp-6 &0x08 = % X, want % X", outcome.DonorGpPushPayload, want)
	}
	if len(outcome.MemberNames) != 2 || outcome.MemberNames[0] != "Alfa" || outcome.MemberNames[1] != "Berk" {
		t.Errorf("MemberNames = %v, want the whole roster [Alfa Berk] including the donor", outcome.MemberNames)
	}

	record, members, ok := deps.Guilds.Guild(mutatorDivision, guildID)
	if !ok {
		t.Fatal("guild vanished after the donation door")
	}
	if record.GP != 120 {
		t.Errorf("stored guild GP = %d, want 120", record.GP)
	}
	for _, member := range members {
		if member.CharID == berk.ID && member.DonatedGP != 120 {
			t.Errorf("stored donor DonatedGP = %d, want 120", member.DonatedGP)
		}
	}
	if berk.SkillPoints == nil || *berk.SkillPoints != 380 {
		t.Errorf("donor SP after = %v, want 380 (500 - 120)", berk.SkillPoints)
	}

	// A second donation ACCUMULATES: the deltas carry the new totals,
	// never the per-donation amounts.
	second := guild.HandleGpDonate(deps, mutatorDivision, berk, mutatorDonatePayload(80))
	if second.Refusal != "" {
		t.Fatalf("second donate refused: %s", second.Refusal)
	}
	if want := donateAckOracle(80); !bytes.Equal(second.AckPayload, want) {
		t.Errorf("second 0xB40F ack = % X, want the per-donation amount % X", second.AckPayload, want)
	}
	if want := donateGuildGpOracle(200); !bytes.Equal(second.GuildGpPushPayload, want) {
		t.Errorf("second subOp-5 = % X, want the accumulated total % X", second.GuildGpPushPayload, want)
	}
	if want := donateDonorGpOracle(mutatorBerkJID, 200); !bytes.Equal(second.DonorGpPushPayload, want) {
		t.Errorf("second subOp-6 = % X, want the accumulated total % X", second.DonorGpPushPayload, want)
	}
	if berk.SkillPoints == nil || *berk.SkillPoints != 300 {
		t.Errorf("donor SP after the second donation = %v, want 300", berk.SkillPoints)
	}
}

// TestHandleGpDonateRefusals proves every donate refusal is fully
// wire-silent (no ack, no deltas) and mutates nothing: a zero amount,
// an amount over the donor's SP, a donor with NO SP field at all, a
// guildless character, nil/delete-pending identities and malformed
// bodies.
func TestHandleGpDonateRefusals(t *testing.T) {
	t.Parallel()
	deps, authority, alfa, berk, guildID := newTwoMemberGuildFixture(t)
	grantSkillPoints(authority, berk, 100)

	pending := mutatorCharacter("Dora", 5)
	pending.DeletePending = true

	cases := []struct {
		name    string
		actor   *enterworld.Character
		payload []byte
		want    string
	}{
		{"zero amount", berk, mutatorDonatePayload(0), "donation door refused"},
		{"over SP", berk, mutatorDonatePayload(101), "donation door refused"},
		{"no SP field", alfa, mutatorDonatePayload(1), "donation door refused"},
		{"nil character", nil, mutatorDonatePayload(1), "characterNotFound"},
		{"delete pending", pending, mutatorDonatePayload(1), "deletePending"},
		{"malformed short", berk, []byte{0x01}, ""},
		{"malformed trailing", berk, append(mutatorDonatePayload(1), 0x00), ""},
	}
	for _, tc := range cases {
		outcome := guild.HandleGpDonate(deps, mutatorDivision, tc.actor, tc.payload)
		if outcome.Refusal == "" {
			t.Errorf("%s: not refused", tc.name)
			continue
		}
		if tc.want != "" && !strings.Contains(outcome.Refusal, tc.want) {
			t.Errorf("%s: refusal %q missing %q", tc.name, outcome.Refusal, tc.want)
		}
		if outcome.AckPayload != nil || outcome.GuildGpPushPayload != nil || outcome.DonorGpPushPayload != nil {
			t.Errorf("%s: refusal carries frames (ack % X gp % X donor % X)", tc.name, outcome.AckPayload, outcome.GuildGpPushPayload, outcome.DonorGpPushPayload)
		}
	}

	record, _, ok := deps.Guilds.Guild(mutatorDivision, guildID)
	if !ok || record.GP != 0 {
		t.Errorf("guild GP after the refusals = %d (ok=%v), want the untouched 0", record.GP, ok)
	}
	if berk.SkillPoints == nil || *berk.SkillPoints != 100 {
		t.Errorf("donor SP after the refusals = %v, want the untouched 100", berk.SkillPoints)
	}

	// The guildless arm needs a character with no FK.
	loner := mutatorCharacter("Ekko", 3)
	if err := authority.CreateCharacter(mutatorDivision, "test-account", loner); err != nil {
		t.Fatalf("CreateCharacter(%s): %v", loner.Name, err)
	}
	if outcome := guild.HandleGpDonate(deps, mutatorDivision, loner, mutatorDonatePayload(1)); !strings.Contains(outcome.Refusal, "not in a guild") {
		t.Errorf("guildless refusal = %q, want not-in-a-guild", outcome.Refusal)
	}
}
