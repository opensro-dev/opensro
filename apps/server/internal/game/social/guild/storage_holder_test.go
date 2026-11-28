/*
===========================================================================

storage_holder_test.go - the warehouse holder's locks on the guild lane

===========================================================================
*/
package guild_test

// Handler pins for the warehouse holder's locks (#4490 U10-04/U10-06):
// while a member holds the guild warehouse open, he cannot leave or be
// expelled, the guild cannot be dissolved over him, and a permission
// update cannot clear his storage right. The first three answer
// {2, 0x48} on their lane (UIIT_MSG_GUILD_WAREHOUSE_INTERRUPT), the
// last {2, 0x4C} on 0xB44E (UIIT_MSG_GUILD_WAREHOUSE_USE_ING) - the
// reference server's refusals, read at its guild jobs. The holder is
// the storageHolder argument the register glue resolves through the
// action owner ("" when nobody holds the room).

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
)

/*
================
grantPayload

Hand-rolls one 0x744E body (never the production writers).
================
*/
func grantPayload(jid uint32, mask uint32) []byte {
	return binary.LittleEndian.AppendUint32(
		binary.LittleEndian.AppendUint32([]byte{1}, jid), mask)
}

/*
================
TestStorageHolderCannotBeExpelled
================
*/
func TestStorageHolderCannotBeExpelled(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	outcome := guild.HandleKick(deps, mutatorDivision, alfa, mutatorKickPayload("Berk"), "Berk")
	if outcome.Refusal == "" || outcome.PushPayload != nil {
		t.Fatalf("kicking the holder answered %+v", outcome)
	}
	if !bytes.Equal(outcome.ErrorPayload, []byte{2, guild.GuildErrStorageHeld}) {
		t.Fatalf("0xB4B1 = % X, want [02 48]", outcome.ErrorPayload)
	}
	if _, member := deps.Guilds.GuildOfCharacter(mutatorDivision, berk.ID); !member {
		t.Fatal("the holder was expelled")
	}
	// Another member holding the room does not shield Berk.
	outcome = guild.HandleKick(deps, mutatorDivision, alfa, mutatorKickPayload("Berk"), "Alfa")
	if outcome.Refusal != "" {
		t.Fatalf("kicking a non-holder refused: %s", outcome.Refusal)
	}
	if _, members, _ := deps.Guilds.Guild(mutatorDivision, guildID); len(members) != 1 {
		t.Fatalf("members after the kick = %d, want 1", len(members))
	}
}

/*
================
TestStorageKickOfHolderNeedsAnAdmittedKicker

The holder's refusal is worded only for a kicker the door would admit;
a member without the kick bit keeps the door's silent answer, exactly
as before the holder was asked about.
================
*/
func TestStorageKickOfHolderNeedsAnAdmittedKicker(t *testing.T) {
	t.Parallel()
	deps, authority, alfa, berk, guildID := newTwoMemberGuildFixture(t)
	cid := mutatorCharacter("Cid", 5)
	if err := authority.CreateCharacter(mutatorDivision, "test-account", cid); err != nil {
		t.Fatal(err)
	}
	if !addGuildMemberForTest(deps.Guilds, mutatorDivision, guildID, alfa.ID, enterworld.GuildMemberRecord{
		CharID: cid.ID, JID: 700088, Name: cid.Name, Grade: 3, Level: 5,
	}) {
		t.Fatal("fixture member join refused")
	}
	outcome := guild.HandleKick(deps, mutatorDivision, cid, mutatorKickPayload("Berk"), "Berk")
	if outcome.ErrorPayload != nil {
		t.Fatalf("a bit-less kicker was answered % X", outcome.ErrorPayload)
	}
	if _, member := deps.Guilds.GuildOfCharacter(mutatorDivision, berk.ID); !member {
		t.Fatal("the holder was expelled")
	}
}

/*
================
TestStorageHolderCannotLeave
================
*/
func TestStorageHolderCannotLeave(t *testing.T) {
	t.Parallel()
	deps, _, _, berk, _ := newTwoMemberGuildFixture(t)

	outcome := guild.HandleLeave(deps, mutatorDivision, berk, mutatorLeavePayload(0), "Berk")
	if outcome.Refusal == "" || !bytes.Equal(outcome.AckPayload, []byte{2, guild.GuildErrStorageHeld}) {
		t.Fatalf("the holder's leave answered %+v", outcome)
	}
	if _, member := deps.Guilds.GuildOfCharacter(mutatorDivision, berk.ID); !member {
		t.Fatal("the holder left")
	}
	outcome = guild.HandleLeave(deps, mutatorDivision, berk, mutatorLeavePayload(0), "Alfa")
	if outcome.Refusal != "" {
		t.Fatalf("a non-holder's leave refused: %s", outcome.Refusal)
	}
}

/*
================
TestStorageHeldGuildCannotBeDissolved
================
*/
func TestStorageHeldGuildCannotBeDissolved(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)

	outcome := guild.HandleBreak(deps, mutatorDivision, alfa, mutatorBreakPayload(0), "Berk")
	if outcome.Refusal == "" || !bytes.Equal(outcome.AckPayload, []byte{2, guild.GuildErrStorageHeld}) {
		t.Fatalf("breaking a held guild answered %+v", outcome)
	}
	if _, _, found := deps.Guilds.Guild(mutatorDivision, guildID); !found {
		t.Fatal("a held guild was dissolved")
	}
	// A member who could never dissolve hears nothing new.
	outcome = guild.HandleBreak(deps, mutatorDivision, berk, mutatorBreakPayload(0), "Berk")
	if outcome.Refusal == "" || outcome.AckPayload != nil {
		t.Fatalf("a member's break answered %+v", outcome)
	}
	// Unheld, the dissolve runs as before.
	outcome = guild.HandleBreak(deps, mutatorDivision, alfa, mutatorBreakPayload(0), "")
	if outcome.Refusal != "" {
		t.Fatalf("an unheld break refused: %s", outcome.Refusal)
	}
	if _, _, found := deps.Guilds.Guild(mutatorDivision, guildID); found {
		t.Fatal("an unheld guild survived its break")
	}
}

/*
================
TestStorageHolderKeepsHisStorageRight
================
*/
func TestStorageHolderKeepsHisStorageRight(t *testing.T) {
	t.Parallel()
	deps, _, alfa, _, guildID := newTwoMemberGuildFixture(t)
	rights := guild.PermMaskKick | guild.PermMaskStorage

	// Berk gains the storage right, then holds the room.
	outcome := guild.HandlePermissionUpdate(deps, mutatorDivision, alfa, grantPayload(mutatorBerkJID, rights), "")
	if outcome.Refusal != "" {
		t.Fatalf("granting the storage right refused: %s", outcome.Refusal)
	}
	clearing := grantPayload(mutatorBerkJID, guild.PermMaskKick)
	outcome = guild.HandlePermissionUpdate(deps, mutatorDivision, alfa, clearing, "Berk")
	if outcome.Refusal == "" || !bytes.Equal(outcome.ErrorPayload, []byte{2, guild.GuildErrStorageRightHeld}) {
		t.Fatalf("clearing the holder's right answered %+v", outcome)
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	for _, member := range members {
		if member.JID == mutatorBerkJID && member.PermMask != rights {
			t.Fatalf("the holder's rights moved to %#x under the refusal", member.PermMask)
		}
	}
	// A grant that keeps the right applies while he holds the room...
	keeping := grantPayload(mutatorBerkJID, rights|guild.PermMaskUnionChat)
	if outcome = guild.HandlePermissionUpdate(deps, mutatorDivision, alfa, keeping, "Berk"); outcome.Refusal != "" {
		t.Fatalf("a right-keeping grant refused: %s", outcome.Refusal)
	}
	// ...and the clearing grant lands once the room is free.
	if outcome = guild.HandlePermissionUpdate(deps, mutatorDivision, alfa, clearing, ""); outcome.Refusal != "" {
		t.Fatalf("clearing a free member's right refused: %s", outcome.Refusal)
	}
	_, members, _ = deps.Guilds.Guild(mutatorDivision, guildID)
	for _, member := range members {
		if member.JID == mutatorBerkJID && member.PermMask != guild.PermMaskKick {
			t.Fatalf("stored rights %#x, want the kick bit only", member.PermMask)
		}
	}
}
