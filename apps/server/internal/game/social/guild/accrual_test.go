/*
===========================================================================

accrual_test.go - passive guild GP accrual over the real authority store

The native shape (issue #4051): hunted SP set aside per member flushes
to the guild at 10, remainders flush on leave/session close, and points
never land on a guild they were not earned in. Presence is nil here -
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
// a sub-threshold remainder waits for FlushCharacter; and none of it
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
	accrual.FlushCharacter(mutatorDivision, berk.ID)
	if gp, donated := guildGP(t, deps, guildID, berk.ID); gp != 16 || donated != 16 {
		t.Fatalf("after the remainder flush: GP = %d/%d, want 16/16", gp, donated)
	}

	// A second flush of nothing is a no-op (send-and-zero).
	accrual.FlushCharacter(mutatorDivision, berk.ID)
	accrual.Drain()
	if gp, _ := guildGP(t, deps, guildID, berk.ID); gp != 16 {
		t.Fatalf("after the empty flush: GP = %d, want 16", gp)
	}
}

// TestGpAccrualIgnoresGuildlessAndForeignGuild proves a guildless
// character records nothing, and points earned under a guild the
// character no longer belongs to are dropped - never re-homed onto
// the guild of the day.
func TestGpAccrualIgnoresGuildlessAndForeignGuild(t *testing.T) {
	t.Parallel()
	deps, _, _, berk, guildID := newTwoMemberGuildFixture(t)

	accrual := guild.NewGpAccrual(deps, nil)

	guildless := *berk
	guildless.GuildID = nil
	accrual.NoteSPMinted(mutatorDivision, &guildless, 25)
	accrual.Drain()
	if gp, _ := guildGP(t, deps, guildID, berk.ID); gp != 0 {
		t.Fatalf("guildless mint landed: GP = %d, want 0", gp)
	}

	foreign := guildID + 999
	stale := *berk
	stale.GuildID = &foreign
	accrual.NoteSPMinted(mutatorDivision, &stale, 25)
	accrual.Drain()
	if gp, donated := guildGP(t, deps, guildID, berk.ID); gp != 0 || donated != 0 {
		t.Fatalf("foreign-guild mint landed: GP = %d/%d, want 0/0", gp, donated)
	}
}
