/*
===========================================================================

accrual_test.go - passive guild GP accrual over the real authority store

The native shape (issue #4051): hunted SP set aside per member is
credited at 10 to the member's current guild; a leave keeps the
remainder (it rides into the next guild), a guilded exit credits it and
a guildless exit drops it. Presence is nil here -
the fan-out is presence-guarded and the wire deltas are pinned by the
donate tests; this file pins the credit behaviour.

===========================================================================
*/

package guild_test

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
)

// guildGP reads the stored guild GP and one member's DonatedGP.
func guildGP(t *testing.T, deps *enterworld.Deps, guildID, charID int64) (uint32, uint32) {
	t.Helper()
	record, members, ok := deps.Guilds.Guild(mutatorDivision, guildID)
	if !ok {
		t.Fatal("guild vanished")
	}
	for _, member := range members {
		if member.CharID == charID {
			return record.GP, member.DonatedGP
		}
	}
	t.Fatalf("member %d not in guild %d", charID, guildID)
	return 0, 0
}

// TestGpAccrualThresholdAndFlush pins the accrual lifecycle: below 10
// nothing lands; the SP that crosses 10 credits the whole set-aside;
// a sub-threshold remainder waits for the session close; and none of it
// ever touches the earner's SP.
func TestGpAccrualThresholdAndFlush(t *testing.T) {
	t.Parallel()
	deps, authority, _, berk, guildID := newTwoMemberGuildFixture(t)
	grantSkillPoints(authority, berk, 500)
	berk.GuildID = &guildID

	accrual := guild.NewGpAccrual(deps, nil)

	accrual.NoteSPMinted(mutatorDivision, berk, 9)
	accrual.Drain()
	if gp, donated := guildGP(t, deps, guildID, berk.ID); gp != 0 || donated != 0 {
		t.Fatalf("9 SP set aside: GP = %d/%d, want 0/0 below the threshold", gp, donated)
	}

	accrual.NoteSPMinted(mutatorDivision, berk, 1)
	accrual.Drain()
	if gp, donated := guildGP(t, deps, guildID, berk.ID); gp != 10 || donated != 10 {
		t.Fatalf("10 SP set aside: GP = %d/%d, want 10/10 after the flush", gp, donated)
	}
	if berk.SkillPoints == nil || *berk.SkillPoints != 500 {
		t.Fatalf("earner SP = %v, want the untouched 500 (accrual never debits)", berk.SkillPoints)
	}

	accrual.NoteSPMinted(mutatorDivision, berk, 6)
	accrual.Drain()
	if gp, _ := guildGP(t, deps, guildID, berk.ID); gp != 10 {
		t.Fatalf("remainder drained early: GP = %d, want 10", gp)
	}
	accrual.SessionClosed(mutatorDivision, berk.ID)
	if gp, donated := guildGP(t, deps, guildID, berk.ID); gp != 16 || donated != 16 {
		t.Fatalf("after the remainder flush: GP = %d/%d, want 16/16", gp, donated)
	}

	// A second close of nothing is a no-op (send-and-zero).
	accrual.SessionClosed(mutatorDivision, berk.ID)
	accrual.Drain()
	if gp, _ := guildGP(t, deps, guildID, berk.ID); gp != 16 {
		t.Fatalf("after the empty flush: GP = %d, want 16", gp)
	}
}

// leaveGuildForTest removes berk through the authority's leave door, as
// the 0x756E handler does, and clears the cached guild id.
func leaveGuildForTest(t *testing.T, deps *enterworld.Deps, character *enterworld.Character) {
	t.Helper()
	if _, refusal := deps.GuildAuthority().LeaveGuild(mutatorDivision, character.ID); refusal.Refused() {
		t.Fatalf("fixture leave refused: %v", refusal)
	}
	character.GuildID = nil
}

// TestGpAccrualIgnoresGuildlessMint proves a guildless character
// records nothing (natively only a guild pointer adds, 0x4E561C).
func TestGpAccrualIgnoresGuildlessMint(t *testing.T) {
	t.Parallel()
	deps, _, _, berk, guildID := newTwoMemberGuildFixture(t)
	accrual := guild.NewGpAccrual(deps, nil)

	guildless := *berk
	guildless.GuildID = nil
	accrual.NoteSPMinted(mutatorDivision, &guildless, 25)
	accrual.Drain()
	accrual.SessionClosed(mutatorDivision, berk.ID)
	if gp, _ := guildGP(t, deps, guildID, berk.ID); gp != 0 {
		t.Fatalf("guildless mint landed: GP = %d, want 0", gp)
	}
}

// TestGpAccrualLeaveKeepsTheRemainder pins the native leave rule: no
// leave-path code touches the set-aside, so a leave credits nothing, a
// due set-aside waits while guildless, and the next guild receives it.
func TestGpAccrualLeaveKeepsTheRemainder(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)
	berk.GuildID = &guildID
	accrual := guild.NewGpAccrual(deps, nil)

	accrual.NoteSPMinted(mutatorDivision, berk, 6)
	leaveGuildForTest(t, deps, berk)
	accrual.Drain()
	if gp, _, ok := deps.Guilds.Guild(mutatorDivision, guildID); !ok || gp.GP != 0 {
		t.Fatalf("leave credited the old guild: GP = %d, want 0", gp.GP)
	}

	// Rejoin: the carried 6 plus 4 new SP cross the threshold and land
	// on the guild the member is in now.
	if !addGuildMemberForTest(deps.Guilds, mutatorDivision, guildID, alfa.ID, enterworld.GuildMemberRecord{
		CharID: berk.ID, JID: mutatorBerkJID, Name: berk.Name, Grade: 3, Level: 6,
	}) {
		t.Fatal("fixture rejoin refused")
	}
	berk.GuildID = &guildID
	accrual.NoteSPMinted(mutatorDivision, berk, 4)
	accrual.Drain()
	if gp, donated := guildGP(t, deps, guildID, berk.ID); gp != 10 || donated != 10 {
		t.Fatalf("carried remainder: GP = %d/%d, want 10/10", gp, donated)
	}

	// A due set-aside whose member left before the tick waits, then lands.
	accrual.NoteSPMinted(mutatorDivision, berk, 12)
	leaveGuildForTest(t, deps, berk)
	accrual.Drain()
	if record, _, _ := deps.Guilds.Guild(mutatorDivision, guildID); record.GP != 10 {
		t.Fatalf("guildless drain credited: GP = %d, want 10", record.GP)
	}
	if !addGuildMemberForTest(deps.Guilds, mutatorDivision, guildID, alfa.ID, enterworld.GuildMemberRecord{
		CharID: berk.ID, JID: mutatorBerkJID, Name: berk.Name, Grade: 3, Level: 6,
	}) {
		t.Fatal("fixture second rejoin refused")
	}
	accrual.Drain()
	if record, _, _ := deps.Guilds.Guild(mutatorDivision, guildID); record.GP != 22 {
		t.Fatalf("waiting set-aside: GP = %d, want 22", record.GP)
	}
}

// TestGpAccrualGuildlessExitDropsTheRemainder pins the exit flush's
// guild test (0x4DEF9F): a remainder is credited only while guilded.
func TestGpAccrualGuildlessExitDropsTheRemainder(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)
	berk.GuildID = &guildID
	accrual := guild.NewGpAccrual(deps, nil)

	accrual.NoteSPMinted(mutatorDivision, berk, 4)
	leaveGuildForTest(t, deps, berk)
	accrual.SessionClosed(mutatorDivision, berk.ID)
	if !addGuildMemberForTest(deps.Guilds, mutatorDivision, guildID, alfa.ID, enterworld.GuildMemberRecord{
		CharID: berk.ID, JID: mutatorBerkJID, Name: berk.Name, Grade: 3, Level: 6,
	}) {
		t.Fatal("fixture rejoin refused")
	}
	berk.GuildID = &guildID
	accrual.NoteSPMinted(mutatorDivision, berk, 6)
	accrual.SessionClosed(mutatorDivision, berk.ID)
	if gp, _ := guildGP(t, deps, guildID, berk.ID); gp != 6 {
		t.Fatalf("after a guildless exit and a guilded one: GP = %d, want 6 (the 4 dropped)", gp)
	}
}
